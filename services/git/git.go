// Package git is the IDE's source control. It runs the git installed on this
// machine over each mod's repository and reads git's machine output. It
// reimplements nothing of git and uses no git library.
//
// git runs with English messages, which the package reads, with no prompt for
// a password and with no optional locks, so a read never takes the index
// lock. The rules it applies:
//
//   - A method names a repository by its root folder.
//   - Run sends git:changed after a command, also when git failed, since a
//     failed command can leave the repository changed. A read sends nothing.
//   - A merge that stops on a conflict is not an error: the markers stay in
//     the files for the editor to resolve.
//   - Deleting a branch with commits no other branch has is refused, with no
//     error, unless it is forced.
//   - A remote's HEAD, which names its default branch, is left out of a
//     commit's refs.
//   - A removed run of lines has no line of its own, so it is marked on the
//     line before it.
//
// # Robustness
//
// A missing git is an error on every call. A command that exits nonzero is an
// error carrying the text git wrote, and so is a commit date that does not
// parse. DiffFile refuses a file git does not track.
//
// The package reads past a mod that is broken or in no repository, a
// repository with no commit (an empty log), a ref of an unknown kind, a
// status record of an unknown kind, letters that mark no change, and a ref
// date that does not parse (the zero time). Places stay empty while no
// workspace is open.
package git

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/workspace"
)

// Service runs the git installed on this machine.
type Service struct {
	Workspace *workspace.Service
}

// Repository is one git repository among the workspace's mods.
type Repository struct {
	Root string `json:"root"`

	// Mods holds the ids of its mods, in load order.
	Mods []string `json:"mods"`
}

// Remote is one remote of a repository.
type Remote struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// The events the git service sends.
const (
	EventOutput = "git:output"

	// EventChanged carries the folder of the repository a command changed.
	EventChanged = "git:changed"
)

func init() {
	application.RegisterEvent[string](EventOutput)
	application.RegisterEvent[string](EventChanged)
}

var findGit = sync.OnceValues(func() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git is not installed: %w", err)
	}

	return path, nil
})

// IsInstalled reports whether git is on the PATH.
func (s *Service) IsInstalled() bool {
	_, err := findGit()

	return err == nil
}

// Run runs git in a folder and sends git:changed, also when git failed.
func (s *Service) Run(dir string, args ...string) (string, error) {
	stdout, err := s.run(dir, args...)
	application.Get().Event.Emit(EventChanged, dir)

	return stdout, err
}

// run is Run without the event. Each line git writes to standard error is
// sent as git:output, and a nonzero exit is an error with that text.
func (s *Service) run(dir string, args ...string) (string, error) {
	path, err := findGit()
	if err != nil {
		return "", err
	}

	cmd := exec.Command(path, slices.Concat([]string{"-C", dir}, args)...)
	hideConsole(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Wait closes the pipe, so standard error is read to its end first.
	reader := bufio.NewReader(io.TeeReader(stderrPipe, &stderr))
	for {
		line, err := reader.ReadString('\n')
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			application.Get().Event.Emit(EventOutput, line)
		}

		if err != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		if text := strings.TrimSpace(stderr.String()); text != "" {
			return stdout.String(), errors.New(text)
		}

		return stdout.String(), err
	}

	return stdout.String(), nil
}

// FindRepository returns the root of the repository that holds a folder, or
// false if there is none.
func (s *Service) FindRepository(dir string) (string, bool) {
	root, err := s.run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}

	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(root))), true
}

// ListRepositories returns the repositories the open workspace's mods are in,
// in load order.
func (s *Service) ListRepositories() []Repository {
	var repositories []Repository
	for _, mod := range s.Workspace.ListMods() {
		if mod.Broken {
			continue
		}

		root, ok := s.FindRepository(mod.Dir)
		if !ok {
			continue
		}

		position := slices.IndexFunc(repositories, func(repository Repository) bool { return repository.Root == root })
		if position < 0 {
			repositories = append(repositories, Repository{Root: root})
			position = len(repositories) - 1
		}

		repositories[position].Mods = append(repositories[position].Mods, mod.ID)
	}

	return repositories
}

// ReadFile returns a file's text at a revision, decoded as files on disk are.
// The revision `:0` is the staged version.
func (s *Service) ReadFile(dir, revision, path string) (string, error) {
	stdout, err := s.run(dir, "show", revision+":"+filepath.ToSlash(path))

	return files.DecodeText([]byte(stdout)).Text, err
}

// ListRemotes returns each remote of a repository with its fetch address.
func (s *Service) ListRemotes(root string) ([]Remote, error) {
	stdout, err := s.run(root, "remote", "-v")
	if err != nil {
		return nil, err
	}

	// Each remote has a fetch line and a push line.
	remotes := []Remote{}
	for line := range strings.Lines(stdout) {
		label, rest, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if address, ok := strings.CutSuffix(rest, " (fetch)"); ok {
			remotes = append(remotes, Remote{Label: label, URL: address})
		}
	}

	return remotes, nil
}

// Discard throws away the working changes of files, deletes those git does
// not track, and ends the review of an agent's edits to each. restore knows
// only tracked files and clean only the others, so both run.
func (s *Service) Discard(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	stdout, err := s.run(root, slices.Concat([]string{"ls-files", "-z", "--"}, paths)...)
	if err != nil {
		return err
	}

	if tracked := splitRecords(stdout); len(tracked) > 0 {
		if _, err := s.run(root, slices.Concat([]string{"restore", "--worktree", "--"}, tracked)...); err != nil {
			return err
		}
	}

	if _, err := s.Run(root, slices.Concat([]string{"clean", "-f", "--"}, paths)...); err != nil {
		return err
	}

	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}

		_ = s.Workspace.KeepFile(path)
	}

	return nil
}

// Merge merges a branch into the current one. A conflict is no error: its
// markers stay in the files.
func (s *Service) Merge(root, name string) error {
	// A conflict exits nonzero too, but says so on standard output.
	stdout, err := s.Run(root, "merge", "--no-edit", name)
	if err != nil && !strings.Contains(stdout, "CONFLICT") {
		return err
	}

	return nil
}

// Pull pulls a remote branch, written as origin/main, into the current one.
func (s *Service) Pull(root, ref string) error {
	remote, branch, _ := strings.Cut(ref, "/")
	_, err := s.Run(root, "pull", remote, branch)

	return err
}

// DeleteBranch deletes a branch. Unless forced, git refuses a branch with
// commits no other branch has, which gives false and no error.
func (s *Service) DeleteBranch(root, name string, force bool) (bool, error) {
	flag := "-d"
	if force {
		flag = "-D"
	}

	_, err := s.Run(root, "branch", flag, name)
	if err != nil && strings.Contains(err.Error(), "not fully merged") {
		return false, nil
	}

	return err == nil, err
}

func (s *Service) locateChanges(root string, changes []Change) {
	paths := make([]string, len(changes))
	for position, change := range changes {
		paths[position] = change.Path
	}

	for position, place := range s.locate(root, paths) {
		changes[position].Place = place
	}
}

// locate returns the place of each path, or empty places while no workspace
// is open.
func (s *Service) locate(root string, paths []string) []workspace.Place {
	places := make([]workspace.Place, len(paths))
	ws, err := s.Workspace.Session()
	if err != nil {
		return places
	}

	mods := ws.Mods.Load()
	for position, path := range paths {
		places[position] = ws.Locate(mods, filepath.Join(root, path))
		places[position].IsDir = strings.HasSuffix(path, "/")
	}

	return places
}
