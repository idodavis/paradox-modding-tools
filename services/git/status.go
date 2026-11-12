// status.go reads a repository's status, branches and tags.

package git

import (
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/services/workspace"
)

// Change is one changed file as git status reports it.
type Change struct {
	Path string `json:"path"`

	// From is a rename's old path.
	From string `json:"from,omitempty"`

	// Status is modified, added, deleted, renamed, untracked or conflict.
	Status string `json:"status"`

	// Staged and Unstaged mark a change in the index and in the working tree.
	Staged   bool `json:"staged"`
	Unstaged bool `json:"unstaged"`

	// Place is empty while no workspace is open.
	Place workspace.Place `json:"place"`
}

// Status is what git status reports for one repository.
type Status struct {
	// Branch is "" when HEAD is detached.
	Branch string `json:"branch"`

	// Upstream is "" when the branch follows none.
	Upstream string `json:"upstream"`

	Ahead   int      `json:"ahead"`
	Behind  int      `json:"behind"`
	Changes []Change `json:"changes"`

	// Ignored holds empty places while no workspace is open.
	Ignored []workspace.Place `json:"ignored"`
}

// Ref is one branch, remote branch or tag.
type Ref struct {
	Name string `json:"name"`

	// Type is branch, remote or tag, or "" for any other name a commit carries.
	Type string `json:"type"`

	Hash string `json:"hash"`

	// Date is the commit's date, or an annotated tag's own. It is the zero time
	// without one.
	Date time.Time `json:"date"`

	Current bool `json:"current"`
}

// ReadStatus returns the status of a repository, with each untracked file on
// its own. It returns git's error if the folder is in no repository.
func (s *Service) ReadStatus(root string) (Status, error) {
	stdout, err := s.run(root, "status", "--porcelain=v2", "-z", "--branch", "--ignored", "--untracked-files=all")
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
			// A rename's old path is the record after it.
			i++
			change.From = records[i]
			status.Changes = append(status.Changes, change)

		case '!':
			ignored = append(ignored, record[2:])
		}
	}

	s.locateChanges(root, status.Changes)
	status.Ignored = s.locate(root, ignored)

	return status, nil
}

// ListRefs returns the branches, remote branches and tags of a repository.
func (s *Service) ListRefs(dir string) ([]Ref, error) {
	stdout, err := s.run(dir, "for-each-ref",
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

func splitRecords(stdout string) []string {
	records := strings.Split(stdout, "\x00")

	return records[:len(records)-1]
}

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

// fieldCounts is how many fields come before the path, by a record's letter.
var fieldCounts = map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1, '!': 1}

// parseChange returns the change one status record describes.
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

var statuses = []struct{ letters, status string }{
	{"A", "added"}, {"D", "deleted"}, {"MT", "modified"}, {"RC", "renamed"},
}

// classifyStatus returns the word for git's letters, or "" if they mark no
// change.
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

var refTypes = map[string]string{"refs/heads/": "branch", "refs/remotes/": "remote", "refs/tags/": "tag"}

// classifyRef returns the short name and type of a full ref. A ref of no known
// type comes back whole, with no type.
func classifyRef(ref string) (string, string) {
	for prefix, refType := range refTypes {
		if short, ok := strings.CutPrefix(ref, prefix); ok {
			return short, refType
		}
	}

	return ref, ""
}
