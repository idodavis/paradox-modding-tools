// gitservice.go runs the git installed on this machine for the IDE's Source control view (O-R9): one
// repository at a time, each command's output told to the frontend's Git output.

package services

import (
	"bytes"
	"cmp"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/jomini"
)

// GitService is the Wails RPC shell for source control. Every method but Repositories takes a
// repository's root; git runs one command at a time, so two never fight over the index's lock.
type GitService struct {
	App *AppService
	mu  sync.Mutex
}

// GitOutput is one git command and what it wrote: its errors and progress, and for a command that
// changes something, what it printed.
type GitOutput struct {
	Root    string `json:"root"`
	Command string `json:"command"`
	Output  string `json:"output,omitempty"`
	Failed  bool   `json:"failed,omitempty"`
}

// GitRepository is one repository holding a workspace's mod, named after the mod.
type GitRepository struct {
	Root   string   `json:"root"`
	Name   string   `json:"name"`
	Origin string   `json:"origin"` // the mod's, for its color
	Mods   []string `json:"mods"`   // every one of the workspace's mods it holds, by id
}

// GitChange is one changed file.
type GitChange struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Folder string `json:"folder"` // its folder from the repository's root, slash separated; "" at the root
	Letter string `json:"letter"` // A, M, D, R (renamed), U (untracked) or ! (in conflict)
}

// GitStatus is a repository's branch and its changed files.
type GitStatus struct {
	Branch    string      `json:"branch"` // "" while HEAD is detached
	Commit    string      `json:"commit"` // HEAD's short hash; "" before the first commit
	Upstream  string      `json:"upstream,omitempty"`
	Ahead     int         `json:"ahead"`
	Behind    int         `json:"behind"`
	Staged    []GitChange `json:"staged"`
	Changes   []GitChange `json:"changes"`
	Conflicts []GitChange `json:"conflicts"`
	Ignored   []string    `json:"ignored"` // what .gitignore matches, a folder once, ending in its separator
}

// GitCommit is one commit of the history.
type GitCommit struct {
	Hash    string   `json:"hash"`
	Short   string   `json:"short"`
	Subject string   `json:"subject"`
	Date    string   `json:"date"` // RFC 3339
	Head    bool     `json:"head,omitempty"`
	Refs    []string `json:"refs,omitempty"` // the branches and tags at it: main, origin/main, tag: v1
	Parents []string `json:"parents"`        // their hashes, the first parent first
}

// GitRemote is one of a repository's remotes: its name and its address.
type GitRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// GitBranch is a local or remote branch, or a tag.
type GitBranch struct {
	Name     string `json:"name"` // main, origin/main, v1.0
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Date     string `json:"date"` // its last commit's, RFC 3339
	Current  bool   `json:"current,omitempty"`
}

// GitBranches is a repository's local and remote branches and tags, the newest first.
type GitBranches struct {
	Local  []GitBranch `json:"local"`
	Remote []GitBranch `json:"remote"`
	Tags   []GitBranch `json:"tags"`
}

// hideConsole keeps git's console window from showing; only Windows opens one (gitservice_windows.go sets it)
var hideConsole = func(*exec.Cmd) {}

// run runs git in a repository and tells the Git output the command and what it wrote to stderr, and when
// logged (a command that changes something) what it printed. git never asks for a password here, writes its
// messages in English, and takes no optional lock: a status rewriting the index would wake the file watcher,
// which reads the repository again, for ever.
func (s *GitService) run(root, stdin string, logged bool, args ...string) (string, error) {
	// git -C "" runs where PMT runs
	if root == "" {
		return "", errors.New("no folder to run git in")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = strings.NewReader(stdin)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	hideConsole(cmd)
	err := cmd.Run()
	told := errs.String()
	if logged {
		told = out.String() + told
	}
	emit(EventGitOutput, GitOutput{Root: root, Command: "git " + strings.Join(args, " "), Output: strings.TrimSpace(told), Failed: err != nil})
	// git says why on stderr, or for some failures (a merge's conflicts) on stdout
	if why := strings.TrimSpace(cmp.Or(errs.String(), out.String())); err != nil && why != "" {
		return out.String(), errors.New(why)
	}
	return out.String(), err
}

// Available says whether git is on this machine's PATH.
func (s *GitService) Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Repositories returns each repository holding one of the workspace's mods, once, named after the
// first mod in it; none when git is missing.
func (s *GitService) Repositories(workspaceID string) ([]GitRepository, error) {
	ws, _, err := s.App.workspace(workspaceID)
	if err != nil || !s.Available() {
		return nil, nil
	}
	var out []GitRepository
	for _, mod := range ws.Mods {
		top, err := s.run(mod.Path, "", false, "rev-parse", "--show-toplevel")
		if err != nil || mod.IsBroken {
			continue
		}
		root := filepath.Clean(filepath.FromSlash(strings.TrimSpace(top)))
		at := slices.IndexFunc(out, func(repo GitRepository) bool { return strings.EqualFold(repo.Root, root) })
		if at < 0 {
			out = append(out, GitRepository{Root: root, Name: mod.Name, Origin: mod.ID})
			at = len(out) - 1
		}
		out[at].Mods = append(out[at].Mods, mod.ID)
	}
	return out, nil
}

// Init makes a mod's folder a repository, its first branch named as git's config says.
func (s *GitService) Init(folder string) error {
	_, err := s.run(folder, "", true, "init")
	return err
}

// change is a file of the status, by its path from the root.
func change(root, rel, letter string) GitChange {
	folder := path.Dir(rel)
	if folder == "." {
		folder = ""
	}
	return GitChange{Path: filepath.Join(root, filepath.FromSlash(rel)), Name: path.Base(rel), Folder: folder, Letter: letter}
}

// letters turns porcelain's status letters into the view's: C (copied) shows as A, T (type change) as M.
var letters = map[byte]string{'A': "A", 'M': "M", 'D': "D", 'R': "R", 'C': "A", 'T': "M"}

// Status returns the branch, how far it is from its upstream, the staged, changed and conflicted files,
// and what .gitignore matches (porcelain v2).
func (s *GitService) Status(root string) (*GitStatus, error) {
	out, err := s.run(root, "", false, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all", "--ignored=matching")
	if err != nil {
		return nil, err
	}
	status := &GitStatus{Staged: []GitChange{}, Changes: []GitChange{}, Conflicts: []GitChange{}, Ignored: []string{}}
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		switch {
		case strings.HasPrefix(entry, "# branch.oid "):
			if oid := strings.TrimPrefix(entry, "# branch.oid "); oid != "(initial)" {
				status.Commit = oid[:min(7, len(oid))]
			}
		case strings.HasPrefix(entry, "# branch.head "):
			if head := strings.TrimPrefix(entry, "# branch.head "); head != "(detached)" {
				status.Branch = head
			}
		case strings.HasPrefix(entry, "# branch.upstream "):
			status.Upstream = strings.TrimPrefix(entry, "# branch.upstream ")
		case strings.HasPrefix(entry, "# branch.ab "):
			counts := strings.Fields(strings.TrimPrefix(entry, "# branch.ab "))
			if len(counts) == 2 {
				status.Ahead, _ = strconv.Atoi(strings.TrimPrefix(counts[0], "+"))
				status.Behind, _ = strconv.Atoi(strings.TrimPrefix(counts[1], "-"))
			}
		case strings.HasPrefix(entry, "1 "), strings.HasPrefix(entry, "2 "):
			// 1 XY sub mH mI mW hH hI path; 2 adds a score, and the old path follows as its own entry
			fields := strings.SplitN(entry, " ", map[byte]int{'1': 9, '2': 10}[entry[0]])
			rel := fields[len(fields)-1]
			if entry[0] == '2' {
				i++
			}
			if x := entry[2]; x != '.' {
				status.Staged = append(status.Staged, change(root, rel, letters[x]))
			}
			if y := entry[3]; y != '.' {
				status.Changes = append(status.Changes, change(root, rel, letters[y]))
			}
		case strings.HasPrefix(entry, "u "):
			fields := strings.SplitN(entry, " ", 11)
			status.Conflicts = append(status.Conflicts, change(root, fields[10], "!"))
		case strings.HasPrefix(entry, "? "):
			status.Changes = append(status.Changes, change(root, entry[2:], "U"))
		case strings.HasPrefix(entry, "! "):
			ignored := filepath.Join(root, filepath.FromSlash(entry[2:]))
			if strings.HasSuffix(entry, "/") {
				ignored += string(filepath.Separator)
			}
			status.Ignored = append(status.Ignored, ignored)
		}
	}
	return status, nil
}

// Log returns the newest commits of HEAD's history and its upstream's, at most count, each before its
// parents (the graph's order); none before the first commit.
func (s *GitService) Log(root string, count int) ([]GitCommit, error) {
	if _, err := s.run(root, "", false, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return []GitCommit{}, nil
	}
	args := []string{"log", "--topo-order", "-n", strconv.Itoa(count), "--format=%H%x1f%h%x1f%s%x1f%cI%x1f%D%x1f%P%x1e", "HEAD"}
	if _, err := s.run(root, "", false, "rev-parse", "--verify", "-q", "@{upstream}"); err == nil {
		args = append(args, "@{upstream}")
	}
	out, err := s.run(root, "", false, args...)
	if err != nil {
		return nil, err
	}
	commits := []GitCommit{}
	for _, record := range strings.Split(out, "\x1e") {
		fields := strings.Split(strings.TrimSpace(record), "\x1f")
		if len(fields) != 6 {
			continue
		}
		commit := GitCommit{Hash: fields[0], Short: fields[1], Subject: fields[2], Date: fields[3], Parents: strings.Fields(fields[5])}
		for _, ref := range strings.Split(fields[4], ", ") {
			if branch, head := strings.CutPrefix(ref, "HEAD -> "); head || ref == "HEAD" {
				commit.Head = true
				ref = branch
			}
			if ref != "" {
				commit.Refs = append(commit.Refs, ref)
			}
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// CommitFiles returns the files a commit changed against its first parent (a merge's too); a first commit's
// are all added.
func (s *GitService) CommitFiles(root, hash string) ([]GitChange, error) {
	args := []string{"diff-tree", "-r", "-z", "-M", "--name-status", "--no-commit-id", "--root", hash}
	if _, err := s.run(root, "", false, "rev-parse", "--verify", "-q", hash+"^1"); err == nil {
		args = []string{"diff", "-z", "-M", "--name-status", hash + "^1", hash}
	}
	out, err := s.run(root, "", false, args...)
	if err != nil {
		return nil, err
	}
	// a status, then its path; a rename's (R100) the old path, then the new
	changes := []GitChange{}
	entries := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	for i := 0; i+1 < len(entries); i += 2 {
		status := entries[i]
		if status != "" && (status[0] == 'R' || status[0] == 'C') {
			i++
		}
		if status != "" && i+1 < len(entries) {
			changes = append(changes, change(root, entries[i+1], letters[status[0]]))
		}
	}
	return changes, nil
}

// Show returns a file's text at a revision, or staged when rev is ""; empty when the file is not there.
func (s *GitService) Show(root, rev, file string) (*FileText, error) {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return nil, err
	}
	object := rev + ":" + filepath.ToSlash(rel)
	if _, err := s.run(root, "", false, "cat-file", "-e", object); err != nil {
		return &FileText{}, nil
	}
	out, err := s.run(root, "", false, "show", object)
	if err != nil {
		return nil, err
	}
	text := textOf([]byte(out), file, rel)
	return &text, nil
}

// Stage stages files, their removal included.
func (s *GitService) Stage(root string, files []string) error {
	_, err := s.run(root, "", true, append([]string{"add", "--"}, files...)...)
	return err
}

// Unstage takes files out of the next commit; their changes stay in the working tree.
func (s *GitService) Unstage(root string, files []string) error {
	_, err := s.run(root, "", true, append([]string{"restore", "--staged", "--"}, files...)...)
	return err
}

// Discard throws away files' unstaged changes; a file git does not track is deleted.
func (s *GitService) Discard(root string, files []string) error {
	tracked, err := s.run(root, "", false, append([]string{"ls-files", "-z", "--"}, files...)...)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, rel := range strings.Split(tracked, "\x00") {
		known[filepath.Join(root, filepath.FromSlash(rel))] = true
	}
	var restore []string
	for _, file := range files {
		if known[filepath.Clean(file)] {
			restore = append(restore, file)
		} else if err := os.Remove(file); err != nil {
			return err
		}
	}
	if len(restore) == 0 {
		return nil
	}
	_, err = s.run(root, "", true, append([]string{"restore", "--"}, restore...)...)
	return err
}

// StageText makes text a file's staged copy, as one block of it is staged or unstaged: the frontend
// sends the staged copy with the block applied. git's filters for the path apply (line endings).
func (s *GitService) StageText(root, file, text string, bom, legacy bool) error {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	data, err := jomini.Encode(text, bom, legacy)
	if err != nil {
		return err
	}
	hash, err := s.run(root, string(data), false, "hash-object", "-w", "--stdin", "--path="+rel)
	if err != nil {
		return err
	}
	// a file staged before keeps its mode (an executable bit); a new one is a plain file
	mode := "100644"
	if staged, err := s.run(root, "", false, "ls-files", "-s", "--", rel); err == nil && len(staged) > 6 {
		mode = staged[:6]
	}
	_, err = s.run(root, "", true, "update-index", "--add", "--cacheinfo", mode+","+strings.TrimSpace(hash)+","+rel)
	return err
}

// Commit commits what is staged; amend replaces the last commit instead.
func (s *GitService) Commit(root, message string, amend bool) error {
	args := []string{"commit", "-F", "-"}
	if amend {
		args = append(args, "--amend")
	}
	_, err := s.run(root, message, true, args...)
	return err
}

// Branches returns the local and remote branches and the tags, each with its last commit's date, a
// local branch with how far it is from its upstream.
func (s *GitService) Branches(root string) (*GitBranches, error) {
	out, err := s.run(root, "", false, "for-each-ref", "--sort=-creatordate",
		"--format=%(refname)%1f%(refname:short)%1f%(upstream:short)%1f%(upstream:track,nobracket)%1f%(creatordate:iso-strict)%1f%(HEAD)",
		"refs/heads", "refs/remotes", "refs/tags")
	if err != nil {
		return nil, err
	}
	branches := &GitBranches{Local: []GitBranch{}, Remote: []GitBranch{}, Tags: []GitBranch{}}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\x1f")
		if len(fields) != 6 || strings.HasSuffix(fields[0], "/HEAD") {
			continue
		}
		branch := GitBranch{Name: fields[1], Upstream: fields[2], Date: fields[4], Current: fields[5] == "*"}
		// the track reads "ahead 2, behind 1", either alone, or "gone"
		for _, part := range strings.Split(fields[3], ", ") {
			if n, ok := strings.CutPrefix(part, "ahead "); ok {
				branch.Ahead, _ = strconv.Atoi(n)
			} else if n, ok := strings.CutPrefix(part, "behind "); ok {
				branch.Behind, _ = strconv.Atoi(n)
			}
		}
		switch {
		case strings.HasPrefix(fields[0], "refs/heads/"):
			branches.Local = append(branches.Local, branch)
		case strings.HasPrefix(fields[0], "refs/remotes/"):
			branches.Remote = append(branches.Remote, branch)
		default:
			branches.Tags = append(branches.Tags, branch)
		}
	}
	return branches, nil
}

// Checkout switches to a branch, a remote branch (as a local one tracking it), a tag or a commit; with
// branch, it creates that branch at ref (HEAD when ref is "") and switches to it.
func (s *GitService) Checkout(root, ref, branch string) error {
	args := []string{"checkout", ref}
	if branch != "" {
		args = []string{"checkout", "-b", branch}
		if ref != "" {
			args = append(args, ref)
		}
	}
	_, err := s.run(root, "", true, args...)
	return err
}

// DeleteBranch deletes a local branch; force deletes one whose commits are merged nowhere.
func (s *GitService) DeleteBranch(root, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := s.run(root, "", true, "branch", flag, branch)
	return err
}

// Fetch fetches every remote, dropping the branches they deleted.
func (s *GitService) Fetch(root string) error {
	_, err := s.run(root, "", true, "fetch", "--all", "--prune")
	return err
}

// Pull pulls the upstream, or a remote's branch when both are given.
func (s *GitService) Pull(root, remote, branch string) error {
	args := []string{"pull"}
	if remote != "" && branch != "" {
		args = append(args, remote, branch)
	}
	_, err := s.run(root, "", true, args...)
	return err
}

// Push pushes the branch to its upstream; a branch with none goes to the first remote and tracks it.
func (s *GitService) Push(root string) error {
	if _, err := s.run(root, "", false, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		_, err := s.run(root, "", true, "push")
		return err
	}
	remotes, err := s.run(root, "", false, "remote")
	if err != nil {
		return err
	}
	remote, _, _ := strings.Cut(strings.TrimSpace(remotes), "\n")
	if remote == "" {
		return errors.New("this repository has no remote to push to")
	}
	_, err = s.run(root, "", true, "push", "--set-upstream", remote, "HEAD")
	return err
}

// Remotes returns the repository's remotes, each with the address it fetches from.
func (s *GitService) Remotes(root string) ([]GitRemote, error) {
	out, err := s.run(root, "", false, "remote", "-v")
	if err != nil {
		return nil, err
	}
	remotes := []GitRemote{}
	// each line is "name<TAB>address (fetch)", and again for push
	for line := range strings.Lines(out) {
		name, rest, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if url, fetch := strings.CutSuffix(rest, " (fetch)"); fetch {
			remotes = append(remotes, GitRemote{Name: name, URL: url})
		}
	}
	return remotes, nil
}

// AddRemote adds a remote by its name and address: Publish asks for one where the repository has none.
func (s *GitService) AddRemote(root, name, url string) error {
	_, err := s.run(root, "", true, "remote", "add", "--", name, url)
	return err
}

// SetRemoteURL changes the address a remote fetches from and pushes to.
func (s *GitService) SetRemoteURL(root, name, url string) error {
	_, err := s.run(root, "", true, "remote", "set-url", "--", name, url)
	return err
}

// RemoveRemote removes a remote, with its remote branches and their settings.
func (s *GitService) RemoveRemote(root, name string) error {
	_, err := s.run(root, "", true, "remote", "remove", "--", name)
	return err
}

// Merge merges a branch, tag or commit into the current branch; conflicts are left in the files.
func (s *GitService) Merge(root, ref string) error {
	_, err := s.run(root, "", true, "merge", ref)
	return err
}
