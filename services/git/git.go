// Package git is the IDE's source control service. It runs the git installed on this machine over each mod's
// repository: status, refs and log, a file at a revision, and the commands the Source Control view offers.
package git

import (
	"context"

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

// ServiceStartup finds git on this machine. It returns an error if git is not installed.
func (git *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	panic("stub")
}

// A Repository is one mod's repository: the folder that holds its .git.
type Repository struct {
	Dir string `json:"dir"`
}

// FindGit looks git up on the PATH. It returns an error if git is not installed.
func FindGit() (string, error) { panic("stub") }

// Run runs git in a repository with the given arguments and returns its standard output. Each line of output is
// also sent as a git:output event as it arrives. A nonzero exit is an error that carries the text of standard
// error.
func (git *Service) Run(repository Repository, args ...string) (string, error) { panic("stub") }

// FindRepository returns the repository that holds a folder, or false if the folder is in none.
func (git *Service) FindRepository(dir string) (Repository, bool) { panic("stub") }

// ReadFile returns a file's text at a revision, as `git show <revision>:<path>` prints it. The revision `:0`
// reads the staged version.
func (git *Service) ReadFile(dir, revision, path string) (string, error) { panic("stub") }

// Stage stages the given files and emits git:changed.
func (git *Service) Stage(dir string, paths []string) error { panic("stub") }

// Unstage unstages the given files and emits git:changed.
func (git *Service) Unstage(dir string, paths []string) error { panic("stub") }

// StageText stages the given text as a file's content, so that one block of a diff can be staged on its own, and
// emits git:changed.
func (git *Service) StageText(dir, path, text string) error { panic("stub") }

// Discard throws away the working changes of the given files and emits git:changed.
func (git *Service) Discard(dir string, paths []string) error { panic("stub") }

// CreateCommit commits what is staged with the given message and emits git:changed.
func (git *Service) CreateCommit(dir, message string) error { panic("stub") }

// CreateBranch creates a branch, switches to it, and emits git:changed.
func (git *Service) CreateBranch(dir, name string) error { panic("stub") }

// SwitchBranch switches to a branch and emits git:changed.
func (git *Service) SwitchBranch(dir, name string) error { panic("stub") }

// Merge merges a branch into the current one and emits git:changed. A conflict leaves the markers in the files
// for the editor to resolve.
func (git *Service) Merge(dir, name string) error { panic("stub") }
