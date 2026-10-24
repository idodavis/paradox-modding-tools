package git

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
)

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

// ListCommits returns the commits reachable from the given refs, newest first, up to the given limit. With no refs
// it starts from HEAD. It returns an empty list for a repository with no commit and for a folder in no repository.
func (git *Service) ListCommits(dir string, limit int, refs []string) ([]Commit, error) {
	// the closing "--" keeps a ref named like a file from being read as a path
	args := slices.Concat([]string{
		"log", fmt.Sprintf("--max-count=%d", limit), "--format=%H%x00%h%x00%P%x00%an%x00%aI%x00%s%x00%D%x1e",
	}, refs, []string{"--"})
	stdout, err := git.Run(dir, args...)
	if err != nil {
		// a repository before its first commit has no log, which is not an error
		if _, headErr := git.Run(dir, "rev-parse", "--verify", "-q", "HEAD"); headErr != nil {
			return []Commit{}, nil
		}
		return nil, err
	}
	commits := []Commit{}
	for record := range strings.SplitSeq(stdout, "\x1e") {
		if record = strings.TrimSpace(record); record == "" {
			continue
		}
		commit, err := parseCommit(record)
		if err != nil {
			return nil, err
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// ListCommitFiles returns the files changed by the commit with the given hash. A renamed file carries its old path.
func (git *Service) ListCommitFiles(dir, hash string) ([]Change, error) {
	stdout, err := git.Run(dir, "show", "--name-status", "--format=", "-z", hash)
	if err != nil {
		return nil, err
	}
	changes := []Change{}
	// a record is a letter then the path; a rename's letter is followed by its old path, then its new one
	records := splitRecords(stdout)
	for len(records) >= 2 {
		change := Change{Status: classifyStatus(records[0]), Path: records[1], Staged: true}
		records = records[2:]
		if change.Status == "renamed" {
			change.From, change.Path = change.Path, records[0]
			records = records[1:]
		}
		changes = append(changes, change)
	}
	return changes, nil
}

// parseCommit returns the commit one log record describes. The error is the date's if it does not parse.
func parseCommit(record string) (Commit, error) {
	fields := strings.Split(record, "\x00")
	date, err := time.Parse(time.RFC3339, fields[4])
	if err != nil {
		return Commit{}, err
	}
	return Commit{
		Hash:    fields[0],
		Short:   fields[1],
		Parents: strings.Fields(fields[2]),
		Author:  fields[3],
		Date:    date,
		Subject: fields[5],
		// the decoration writes the current branch as "HEAD -> main"
		Refs: lo.FilterMap(strings.Split(fields[6], ", "), func(ref string, _ int) (string, bool) {
			return strings.TrimPrefix(ref, "HEAD -> "), ref != ""
		}),
	}, nil
}
