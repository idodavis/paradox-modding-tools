package git

// A Change is one changed file as git status reports it.
type Change struct {
	Path     string `json:"path"`
	From     string `json:"from,omitempty"` // a rename's old path
	Status   string `json:"status"`         // modified, added, deleted, renamed, untracked, conflict
	Staged   bool   `json:"staged"`         // the change is in the index
	Unstaged bool   `json:"unstaged"`       // the change is in the working tree
}

// A Status is a repository's state: its branch, its upstream and how far they differ, and its changes.
type Status struct {
	Branch   string   `json:"branch"`   // "" when detached
	Upstream string   `json:"upstream"` // "" when there is none
	Ahead    int      `json:"ahead"`
	Behind   int      `json:"behind"`
	Changes  []Change `json:"changes"`
}

// ReadStatus returns a repository's status.
func (git *Service) ReadStatus(dir string) (Status, error) { panic("stub") }

// A Ref is one branch, remote branch or tag.
type Ref struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // branch, remote, tag
	Hash    string `json:"hash"`
	Current bool   `json:"current"`
}

// ListRefs returns a repository's branches, remote branches and tags.
func (git *Service) ListRefs(dir string) ([]Ref, error) { panic("stub") }
