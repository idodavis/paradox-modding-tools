package git

import (
	"strconv"
	"strings"
)

// A Change is one changed file as git status reports it.
type Change struct {
	Path     string `json:"path"`
	From     string `json:"from,omitempty"` // a rename's old path
	Status   string `json:"status"`         // modified, added, deleted, renamed, untracked, conflict
	Staged   bool   `json:"staged"`         // the change is in the index
	Unstaged bool   `json:"unstaged"`       // the change is in the working tree
}

// A Status is what git status reports for one repository.
type Status struct {
	Branch   string   `json:"branch"`   // "" when detached
	Upstream string   `json:"upstream"` // "" when there is none
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Changes  []Change `json:"changes"`
}

// refTypes is the type of a ref by the prefix of its full name.
var refTypes = map[string]string{"refs/heads/": "branch", "refs/remotes/": "remote", "refs/tags/": "tag"}

// fieldCounts is the count of fields before the path, which may hold spaces, in each status record.
var fieldCounts = map[byte]int{'1': 8, '2': 9, 'u': 10, '?': 1}

// statuses is the word for what was done to a file, by the letters git prints for it.
var statuses = []struct{ letters, status string }{
	{"A", "added"}, {"D", "deleted"}, {"MT", "modified"}, {"RC", "renamed"},
}

// ReadStatus returns a repository's status. The error is git's if the folder is in no repository.
func (git *Service) ReadStatus(dir string) (Status, error) {
	stdout, err := git.Run(dir, "status", "--porcelain=v2", "-z", "--branch")
	if err != nil {
		return Status{}, err
	}
	status := Status{Changes: []Change{}}
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
		}
	}
	return status, nil
}

// A Ref is one branch, remote branch or tag.
type Ref struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // branch, remote, tag
	Hash    string `json:"hash"`
	Current bool   `json:"current"`
}

// ListRefs returns a repository's branches, remote branches and tags. The error is git's if the folder is in no
// repository.
func (git *Service) ListRefs(dir string) ([]Ref, error) {
	stdout, err := git.Run(dir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(HEAD)")
	if err != nil {
		return nil, err
	}
	refs := []Ref{}
	for line := range strings.Lines(stdout) {
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\x00")
		for prefix, refType := range refTypes {
			if name, ok := strings.CutPrefix(fields[0], prefix); ok {
				refs = append(refs, Ref{Name: name, Type: refType, Hash: fields[1], Current: fields[2] == "*"})
			}
		}
	}
	return refs, nil
}

// splitRecords splits what git prints with -z into its records, each ended by a NUL.
func splitRecords(stdout string) []string {
	records := strings.Split(stdout, "\x00")
	return records[:len(records)-1]
}

// parseHeader sets what one header of the status says: the branch, its upstream, or how far they differ. Any
// other header says nothing a Status holds.
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

// parseChange returns the change one status record describes: its path, which sides hold it and what was done to
// it. A rename record names only the new path.
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

// classifyStatus returns the word for what was done to a file, from the letters git prints for it, the index's
// letter first. Letters that mark no change give "".
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
