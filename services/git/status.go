// status.go reads a repository's status from git, and lists its branches, remote branches and tags.

package git

import (
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/services/workspace"
)

// A Change is one changed file as git status reports it.
type Change struct {
	Path     string `json:"path"`
	From     string `json:"from,omitempty"` // a rename's old path
	Status   string `json:"status"`         // modified, added, deleted, renamed, untracked, conflict
	Staged   bool   `json:"staged"`         // the change is in the index
	Unstaged bool   `json:"unstaged"`       // the change is in the working tree
	// Place is where the file is in the open workspace; it is empty while none is open.
	Place workspace.Place `json:"place"`
}

// A Status is what git status reports for one repository.
type Status struct {
	Branch   string   `json:"branch"`   // "" when detached
	Upstream string   `json:"upstream"` // "" when there is none
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Changes  []Change `json:"changes"`
	// Ignored is where the files and folders git ignores are in the open workspace. They are empty places while none
	// is open.
	Ignored []workspace.Place `json:"ignored"`
}

// A Ref is one branch, remote branch or tag.
type Ref struct {
	Name string `json:"name"`
	Type string `json:"type"` // branch, remote, tag; "" for another name a commit is decorated with
	Hash string `json:"hash"`
	// Date is when the commit it points to was made, or when an annotated tag was. It is the zero time if the object
	// has no date.
	Date    time.Time `json:"date"`
	Current bool      `json:"current"`
}

// refTypes is the type of a ref by the prefix of its full name.
var refTypes = map[string]string{"refs/heads/": "branch", "refs/remotes/": "remote", "refs/tags/": "tag"}

// fieldCounts is how many fields precede the path (which may hold spaces) in a status record, by its first letter.
var fieldCounts = map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1, '!': 1}

// statuses is the word for what was done to a file, by the letters git prints for it.
var statuses = []struct{ letters, status string }{
	{"A", "added"}, {"D", "deleted"}, {"MT", "modified"}, {"RC", "renamed"},
}

// ReadStatus returns the status of the repository at the given root folder. The error is git's if there is none.
func (git *Service) ReadStatus(root string) (Status, error) {
	stdout, err := git.run(root, "status", "--porcelain=v2", "-z", "--branch", "--ignored")
	if err != nil {
		return Status{}, err
	}
	status := Status{Changes: []Change{}}
	ignored := []string{}
	records := splitRecords(stdout)
	for i := 0; i < len(records); i++ {
		record := records[i]
		switch record[0] {
		case '#':
			parseHeader(&status, record)
		case '1', 'u', '?':
			status.Changes = append(status.Changes, parseChange(record))
		case '2':
			change := parseChange(record)
			i++ // a rename's original path is the record after it
			change.From = records[i]
			status.Changes = append(status.Changes, change)
		case '!':
			ignored = append(ignored, record[2:])
		}
	}
	git.locateChanges(root, status.Changes)
	status.Ignored = git.locate(root, ignored)
	return status, nil
}

// ListRefs returns a repository's branches, remote branches and tags. The error is git's if the folder is in no
// repository.
func (git *Service) ListRefs(dir string) ([]Ref, error) {
	stdout, err := git.run(dir, "for-each-ref",
		"--format=%(refname)%00%(objectname)%00%(creatordate:iso-strict)%00%(HEAD)")
	if err != nil {
		return nil, err
	}
	refs := []Ref{}
	for line := range strings.Lines(stdout) {
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\x00")
		date, _ := time.Parse(time.RFC3339, fields[2])
		if name, refType := classifyRef(fields[0]); refType != "" {
			refs = append(refs, Ref{Name: name, Type: refType, Hash: fields[1], Date: date, Current: fields[3] == "*"})
		}
	}
	return refs, nil
}

// splitRecords splits what git prints with -z into its records, each ended by a NUL.
func splitRecords(stdout string) []string {
	records := strings.Split(stdout, "\x00")
	return records[:len(records)-1]
}

// parseHeader sets what one header of the status says: the branch, its upstream, or how far they differ.
func parseHeader(status *Status, record string) {
	fields := strings.Fields(record)
	switch fields[1] {
	case "branch.head":
		if fields[2] != "(detached)" {
			status.Branch = fields[2]
		}
	case "branch.upstream":
		status.Upstream = fields[2]
	case "branch.ab":
		status.Ahead, _ = strconv.Atoi(fields[2])
		status.Behind, _ = strconv.Atoi(strings.TrimPrefix(fields[3], "-"))
	}
}

// parseChange returns the change one status record describes. A rename record names only the new path.
func parseChange(record string) Change {
	fields := strings.SplitN(record, " ", fieldCounts[record[0]]+1)
	change := Change{Path: fields[len(fields)-1]}
	switch record[0] {
	case '?':
		change.Status, change.Unstaged = "untracked", true
	case 'u':
		change.Status = "conflict"
	default:
		xy := fields[1]
		change.Status = classifyStatus(xy)
		change.Staged, change.Unstaged = xy[0] != '.', xy[1] != '.'
	}
	return change
}

// classifyStatus returns the word for what was done to a file, from the letters git prints for it. Letters that mark
// no change give "".
func classifyStatus(letters string) string {
	for _, letter := range letters {
		for _, row := range statuses {
			if strings.ContainsRune(row.letters, letter) {
				return row.status
			}
		}
	}
	return ""
}

// classifyRef returns the short name of a full ref, such as main for refs/heads/main, and its type. A ref of no known
// type comes back whole, with no type.
func classifyRef(ref string) (string, string) {
	for prefix, refType := range refTypes {
		if short, ok := strings.CutPrefix(ref, prefix); ok {
			return short, refType
		}
	}
	return ref, ""
}
