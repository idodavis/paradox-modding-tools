// Package git is the IDE's source control service. It runs the git installed on this machine over each mod's
// repository: status, refs and log, a file at a revision, and the commands the Source Control view offers.
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

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/workspace"
)

// The events the git service sends.
const (
	EventOutput  = "git:output"  // a line git printed
	EventChanged = "git:changed" // a repository's state changed by a command PMT ran; the data is its folder
)

// init registers the git events with the application.
func init() {
	application.RegisterEvent[string](EventOutput)
	application.RegisterEvent[string](EventChanged)
}

// Service is the git service. It runs the git installed on this machine, which it finds on the PATH on first use.
type Service struct {
	Workspace *workspace.Service
}

// A Repository is one git repository among the workspace's mods: its root folder, and the mods in it in load order.
type Repository struct {
	Root string   `json:"root"`
	Mods []string `json:"mods"` // the mods' ids
}

// A Remote is one remote of a repository: its label and the address it fetches from.
type Remote struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// findGit is the one lookup of git on the PATH, made on the first call and kept.
var findGit = sync.OnceValues(func() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git is not installed: %w", err)
	}
	return path, nil
})

// Run runs git in a folder with the given arguments and returns its standard output, as run does. It then emits
// git:changed with the folder, also when git failed, since a failed command can leave the repository changed.
func (git *Service) Run(dir string, args ...string) (string, error) {
	stdout, err := git.run(dir, args...)
	application.Get().Event.Emit(EventChanged, dir)
	return stdout, err
}

// run runs git in a folder with the given arguments and returns its standard output. Each line of standard
// error is also sent as a git:output event as it arrives. A nonzero exit is an error that carries the text of
// standard error, and so is git not being installed. It sends no git:changed.
func (git *Service) run(dir string, args ...string) (string, error) {
	path, err := findGit()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, slices.Concat([]string{"-C", dir}, args)...)
	// English messages, since the package reads them; and a read never takes the index lock
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
	// standard error is read to its end before Wait, which closes the pipe
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

// FindRepository returns the root folder of the repository that holds a folder, or false if the folder is in none.
func (git *Service) FindRepository(dir string) (string, bool) {
	root, err := git.run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(root))), true
}

// ListRepositories returns the repositories the open workspace's mods are in, in the load order of their first mod,
// each with the mods in it. A mod in none is left out, and so is one whose folder is missing. It is empty while no
// workspace is open.
func (git *Service) ListRepositories() []Repository {
	var repositories []Repository
	for _, mod := range git.Workspace.ListMods() {
		if mod.Broken {
			continue
		}
		root, ok := git.FindRepository(mod.Dir)
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

// ReadFile returns a file's text at a revision, as `git show <revision>:<path>` prints it, decoded as the files on disk
// are. The path is from the repository's root. The revision `:0` reads the staged version.
func (git *Service) ReadFile(dir, revision, path string) (string, error) {
	stdout, err := git.run(dir, "show", revision+":"+filepath.ToSlash(path))
	return files.DecodeText([]byte(stdout)).Text, err
}

// ListRemotes returns the remotes of the repository at the given root folder, each with the address it fetches from,
// in the order git lists them. The error is git's if the folder is in no repository.
func (git *Service) ListRemotes(root string) ([]Remote, error) {
	stdout, err := git.run(root, "remote", "-v")
	if err != nil {
		return nil, err
	}
	// each remote has a line "<label>\t<address> (fetch)", and its push line after it
	remotes := []Remote{}
	for line := range strings.Lines(stdout) {
		label, rest, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if address, ok := strings.CutSuffix(rest, " (fetch)"); ok {
			remotes = append(remotes, Remote{Label: label, URL: address})
		}
	}
	return remotes, nil
}

// Discard throws away the working changes of the given files in the repository at the given root folder, and emits
// git:changed. A file git does not track is deleted. A file's path is absolute or from the root. With no files it does
// nothing. It returns git's error if a command fails.
func (git *Service) Discard(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	// restore knows only the files git tracks, and clean deletes only the ones it does not
	stdout, err := git.run(root, slices.Concat([]string{"ls-files", "-z", "--"}, paths)...)
	if err != nil {
		return err
	}
	if tracked := splitRecords(stdout); len(tracked) > 0 {
		if _, err := git.run(root, slices.Concat([]string{"restore", "--worktree", "--"}, tracked)...); err != nil {
			return err
		}
	}
	_, err = git.Run(root, slices.Concat([]string{"clean", "-f", "--"}, paths)...)
	return err
}

// Merge merges the given branch into the current one of the repository at the given root folder, and emits
// git:changed. A conflict is not an error: it leaves the markers in the files for the editor to resolve. Any other
// failure returns git's error.
func (git *Service) Merge(root, name string) error {
	// a conflict exits nonzero too, announced on standard output, and the status shows it
	stdout, err := git.run(root, "merge", "--no-edit", name)
	if err != nil && !strings.Contains(stdout, "CONFLICT") {
		return err
	}
	application.Get().Event.Emit(EventChanged, root)
	return nil
}

// Pull pulls one remote branch, given with its remote as in origin/main, into the current branch of the repository at
// the given root folder, and emits git:changed. It returns git's error if the pull fails.
func (git *Service) Pull(root, ref string) error {
	remote, branch, _ := strings.Cut(ref, "/")
	_, err := git.Run(root, "pull", remote, branch)
	return err
}

// DeleteBranch deletes a branch of the repository at the given root folder, and emits git:changed. Unless forced, git
// refuses a branch with commits no other branch has: DeleteBranch then returns false and no error. Any other failure
// returns git's error.
func (git *Service) DeleteBranch(root, name string, force bool) (bool, error) {
	_, err := git.Run(root, "branch", lo.Ternary(force, "-D", "-d"), name)
	// git says such a branch is "not fully merged"
	if err != nil && strings.Contains(err.Error(), "not fully merged") {
		return false, nil
	}
	return err == nil, err
}

// locateChanges gives each change the place of its file in the open workspace, from the root folder of the
// repository, which git writes a change's path from. The changes keep an empty place if no workspace is open.
func (git *Service) locateChanges(root string, changes []Change) {
	paths := lo.Map(changes, func(change Change, _ int) string { return change.Path })
	for position, place := range git.locate(root, paths) {
		changes[position].Place = place
	}
}

// locate returns the place in the open workspace of each given path, which git writes from the root folder of the
// repository. A path ending in a slash is a folder. The places are empty if no workspace is open.
func (git *Service) locate(root string, paths []string) []workspace.Place {
	places := make([]workspace.Place, len(paths))
	ws, err := git.Workspace.Session()
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
