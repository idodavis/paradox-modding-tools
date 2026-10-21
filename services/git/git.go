// Package git is the IDE's source control service. It runs the git installed on this machine over each mod's
// repository: status, refs and log, a file at a revision, and the commands the Source Control view offers.
package git

import (
	"bufio"
	"context"
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
)

// The events the git service sends.
const (
	EventOutput  = "git:output"  // a line git printed
	EventChanged = "git:changed" // a repository's state changed by a command PMT ran; the data is its folder
)

func init() {
	application.RegisterEvent[string](EventOutput)
	application.RegisterEvent[string](EventChanged)
}

// Service is the git service. It keeps the path of the git executable found at startup.
type Service struct {
	git string // the git executable's path
}

// ServiceStartup finds git on this machine. It never fails: when git is not installed, it keeps that error and
// every method returns it.
func (git *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	git.git, _ = FindGit() // FindGit keeps the error, which Run returns while the path is empty
	return nil
}

// findGit is the one lookup of git on the PATH, made on the first call and kept.
var findGit = sync.OnceValues(func() (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git is not installed: %w", err)
	}
	return path, nil
})

// FindGit looks git up on the PATH. It returns an error if git is not installed.
func FindGit() (string, error) { return findGit() }

// Run runs git in a folder with the given arguments and returns its standard output. Each line of standard
// error is also sent as a git:output event as it arrives. A nonzero exit is an error that carries the text of
// standard error.
func (git *Service) Run(dir string, args ...string) (string, error) {
	// an empty path means startup found no git, or never ran; FindGit answers both with what it kept
	path := git.git
	if path == "" {
		var err error
		if path, err = FindGit(); err != nil {
			return "", err
		}
	}
	cmd := exec.Command(path, slices.Concat([]string{"-C", dir}, args)...)
	// English messages, since the package reads them; no index lock from a read, so a stage is never blocked
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
	emitLines(io.TeeReader(stderrPipe, &stderr)) // read to its end before Wait, which closes the pipe
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
	root, err := git.resolveRepository(dir)
	return root, err == nil
}

// ReadFile returns a file's text at a revision, as `git show <revision>:<path>` prints it. The revision `:0`
// reads the staged version.
func (git *Service) ReadFile(dir, revision, path string) (string, error) {
	root, err := git.resolveRepository(dir)
	if err != nil {
		return "", err
	}
	return git.Run(root, "show", revision+":"+filepath.ToSlash(path))
}

// Stage stages the given files and emits git:changed.
func (git *Service) Stage(dir string, paths []string) error {
	return git.apply(dir, slices.Concat([]string{"add", "--"}, paths)...)
}

// Unstage unstages the given files and emits git:changed.
func (git *Service) Unstage(dir string, paths []string) error {
	return git.apply(dir, slices.Concat([]string{"reset", "-q", "--"}, paths)...)
}

// Discard throws away the working changes of the given files and emits git:changed.
func (git *Service) Discard(dir string, paths []string) error {
	root, err := git.resolveRepository(dir)
	if err != nil {
		return err
	}
	// restore knows only the files git tracks; an untracked file is removed instead
	stdout, err := git.Run(root, slices.Concat([]string{"ls-files", "-z", "--"}, paths)...)
	if err != nil {
		return err
	}
	tracked := splitRecords(stdout)
	untracked := lo.Without(paths, tracked...)
	if len(untracked) > 0 {
		if _, err := git.Run(root, slices.Concat([]string{"clean", "-f", "--"}, untracked)...); err != nil {
			return err
		}
	}
	if len(tracked) > 0 {
		if _, err := git.Run(root, slices.Concat([]string{"restore", "--worktree", "--"}, tracked)...); err != nil {
			return err
		}
	}
	emit(EventChanged, root)
	return nil
}

// CreateCommit commits what is staged with the given message and emits git:changed.
func (git *Service) CreateCommit(dir, message string) error {
	return git.apply(dir, "commit", "-m", message)
}

// CreateBranch creates a branch, switches to it, and emits git:changed.
func (git *Service) CreateBranch(dir, name string) error {
	return git.apply(dir, "switch", "-c", name)
}

// SwitchBranch switches to a branch and emits git:changed.
func (git *Service) SwitchBranch(dir, name string) error {
	return git.apply(dir, "switch", name)
}

// Merge merges a branch into the current one and emits git:changed. A conflict leaves the markers in the files
// for the editor to resolve.
func (git *Service) Merge(dir, name string) error {
	root, err := git.resolveRepository(dir)
	if err != nil {
		return err
	}
	// a conflict exits nonzero too, announced on standard output, and the status shows it
	stdout, err := git.Run(root, "merge", "--no-edit", name)
	if err != nil && !strings.Contains(stdout, "CONFLICT") {
		return err
	}
	emit(EventChanged, root)
	return nil
}

// emitLines sends each line a stream prints as a git:output event as it arrives. An empty line is not sent.
func emitLines(stream io.Reader) {
	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			emit(EventOutput, line)
		}
		if err != nil {
			return
		}
	}
}

// emit sends an event with its text. Outside the running app, as in a test, there is nobody to send it to.
func emit(event, text string) {
	if pmt := application.Get(); pmt != nil {
		pmt.Event.Emit(event, text)
	}
}

// resolveRepository returns the repository that holds a folder. The error is git's if the folder is in none.
func (git *Service) resolveRepository(dir string) (string, error) {
	root, err := git.Run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.FromSlash(strings.TrimSpace(root))), nil
}

// apply runs a command that changes a repository and emits git:changed with the repository's folder. The error is
// git's if the command fails or the folder is in no repository.
func (git *Service) apply(dir string, args ...string) error {
	root, err := git.resolveRepository(dir)
	if err != nil {
		return err
	}
	if _, err := git.Run(root, args...); err != nil {
		return err
	}
	emit(EventChanged, root)
	return nil
}
