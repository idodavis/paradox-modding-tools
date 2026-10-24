package git

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// createApp makes the Wails app the service sends its events through, once for all the tests.
var createApp = sync.OnceFunc(func() { application.New(application.Options{}) })

// The fixture's branch and the file the tests change.
const (
	branch = "main"
	file   = "a.txt"
)

// TestReadStatus reads the status through a file's life: untracked, staged, committed, modified, renamed and
// deleted.
func TestReadStatus(t *testing.T) {
	git, dir := createRepository(t)
	check := func(want ...Change) {
		t.Helper()
		status, err := git.ReadStatus(dir)
		must(t, err)
		if status.Branch != branch || !slices.Equal(status.Changes, want) {
			t.Fatalf("status %+v, want changes %+v", status, want)
		}
	}
	writeFile(t, dir, file, "one\n")
	check(Change{Path: file, Status: "untracked", Unstaged: true})
	must(t, git.Stage(dir, []string{file}))
	check(Change{Path: file, Status: "added", Staged: true})
	must(t, git.CreateCommit(dir, "add"))
	check()
	writeFile(t, dir, file, "two\n")
	check(Change{Path: file, Status: "modified", Unstaged: true})
	must(t, git.Stage(dir, []string{file}))
	must(t, git.CreateCommit(dir, "change"))
	target := "b.txt"
	_, err := git.Run(dir, "mv", file, target)
	must(t, err)
	check(Change{Path: target, From: file, Status: "renamed", Staged: true})
	must(t, git.CreateCommit(dir, "move"))
	must(t, os.Remove(filepath.Join(dir, target)))
	check(Change{Path: target, Status: "deleted", Unstaged: true})
}

// TestReadFile reads the staged and the committed version of a file, and fails on a path no revision holds.
func TestReadFile(t *testing.T) {
	git, dir := createRepository(t)
	writeFile(t, dir, file, "old\n")
	must(t, git.Stage(dir, []string{file}))
	must(t, git.CreateCommit(dir, "old"))
	writeFile(t, dir, file, "new\n")
	must(t, git.Stage(dir, []string{file}))
	for revision, want := range map[string]string{":0": "new\n", "HEAD": "old\n"} {
		text, err := git.ReadFile(dir, revision, file)
		must(t, err)
		if text != want {
			t.Fatalf("%s holds %q, want %q", revision, text, want)
		}
	}
	if _, err := git.ReadFile(dir, "HEAD", "missing.txt"); err == nil {
		t.Fatal("a path no revision holds read without an error")
	}
}

// TestListRefs lists the branches after a second one is created, the new one current.
func TestListRefs(t *testing.T) {
	git, dir := createRepository(t)
	writeFile(t, dir, file, "one\n")
	must(t, git.Stage(dir, []string{file}))
	must(t, git.CreateCommit(dir, "first"))
	must(t, git.CreateBranch(dir, "feature"))
	hash, err := git.Run(dir, "rev-parse", "HEAD")
	must(t, err)
	hash = strings.TrimSpace(hash)
	refs, err := git.ListRefs(dir)
	must(t, err)
	want := []Ref{
		{Name: "feature", Type: "branch", Hash: hash, Current: true},
		{Name: branch, Type: "branch", Hash: hash},
	}
	if !slices.Equal(refs, want) {
		t.Fatalf("refs %+v, want %+v", refs, want)
	}
}

// TestListCommits lists nothing before the first commit, then two commits newest first, the newer one's parent
// the older, and honors the limit.
func TestListCommits(t *testing.T) {
	git, dir := createRepository(t)
	commits, err := git.ListCommits(dir, 10, []string{branch})
	must(t, err)
	if len(commits) != 0 {
		t.Fatalf("commits %+v before the first commit, want none", commits)
	}
	subjects := []string{"first", "second"}
	for _, subject := range subjects {
		writeFile(t, dir, file, subject)
		must(t, git.Stage(dir, []string{file}))
		must(t, git.CreateCommit(dir, subject))
	}
	commits, err = git.ListCommits(dir, 10, []string{branch})
	must(t, err)
	if len(commits) != 2 {
		t.Fatalf("commits %+v, want two", commits)
	}
	newest, oldest := commits[0], commits[1]
	if newest.Subject != subjects[1] || !slices.Equal(newest.Parents, []string{oldest.Hash}) ||
		newest.Author != "Tester" || newest.Date.IsZero() || !strings.HasPrefix(newest.Hash, newest.Short) ||
		!slices.Equal(newest.Refs, []string{branch}) {
		t.Fatalf("newest commit %+v", newest)
	}
	if oldest.Subject != subjects[0] || len(oldest.Parents) != 0 || len(oldest.Refs) != 0 {
		t.Fatalf("oldest commit %+v", oldest)
	}
	commits, err = git.ListCommits(dir, 1, []string{branch})
	must(t, err)
	if len(commits) != 1 || commits[0].Hash != newest.Hash {
		t.Fatalf("commits %+v, want the newest alone", commits)
	}
}

// createRepository makes a throwaway repository on the fixture's branch, read with no global or system git
// config, and returns the service and the repository's folder. The test is skipped when git is not installed.
func createRepository(t *testing.T) (*Service, string) {
	t.Helper()
	if _, err := findGit(); err != nil {
		t.Skip(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	createApp()
	git := &Service{}
	dir := t.TempDir()
	_, err := git.Run(dir, "init", "-q", "-b", branch)
	must(t, err)
	settings := [][2]string{{"user.name", "Tester"}, {"user.email", "tester@example.com"}, {"core.autocrlf", "false"}}
	for _, setting := range settings {
		_, err := git.Run(dir, "config", setting[0], setting[1])
		must(t, err)
	}
	return git, dir
}

// writeFile writes a file's text under the repository's folder.
func writeFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o644))
}

// must ends the test when a step of its setup fails.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
