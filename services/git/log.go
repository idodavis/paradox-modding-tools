package git

import "time"

// A Commit is one commit of the log, with what the graph draws of it.
type Commit struct {
	Hash    string    `json:"hash"`
	Short   string    `json:"short"` // the abbreviated hash git chose
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Refs    []string  `json:"refs"` // the refs pointing here, decorated
}

// ListCommits returns the commits reachable from the given refs, newest first, up to the given limit.
func (git *Service) ListCommits(dir string, limit int, refs []string) ([]Commit, error) {
	panic("stub")
}

// ListCommitFiles returns the files one commit changed.
func (git *Service) ListCommitFiles(dir, hash string) ([]Change, error) { panic("stub") }
