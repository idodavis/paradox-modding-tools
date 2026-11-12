// log.go lists a repository's commits and the files of a commit.

package git

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Commit is one commit of the log.
type Commit struct {
	Hash string `json:"hash"`

	// Short is the abbreviated hash.
	Short string `json:"short"`

	Parents []string  `json:"parents"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
	Refs    []Ref     `json:"refs"`
}

// ListCommits returns up to the given number of commits reachable from the
// refs, or from HEAD with none, newest first. A repository with no commit
// gives an empty list.
func (s *Service) ListCommits(dir string, limit int, refs []string) ([]Commit, error) {
	// The closing "--" keeps a ref named like a file from being read as a path.
	args := slices.Concat([]string{
		"log", fmt.Sprintf("--max-count=%d", limit), "--decorate=full", "--format=%H%x00%h%x00%P%x00%aI%x00%s%x00%D%x1e",
	}, refs, []string{"--"})
	stdout, err := s.run(dir, args...)
	if err != nil {
		if _, headErr := s.run(dir, "rev-parse", "--verify", "-q", "HEAD"); headErr != nil {
			return []Commit{}, nil
		}

		return nil, err
	}

	commits := []Commit{}
	for record := range strings.SplitSeq(stdout, "\x1e") {
		if record = strings.TrimSpace(record); record == "" {
			continue
		}

		fields := strings.Split(record, "\x00")
		date, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			return nil, err
		}

		decorated := []Ref{}
		for decoration := range strings.SplitSeq(fields[5], ", ") {
			name, current := strings.CutPrefix(decoration, "HEAD -> ")
			name, refType := classifyRef(strings.TrimPrefix(name, "tag: "))
			// A remote's HEAD names its default branch and is left out.
			isRemoteHead := refType == "remote" && strings.HasSuffix(name, "/HEAD")
			if name != "" && !isRemoteHead {
				decorated = append(decorated, Ref{Name: name, Type: refType, Current: current})
			}
		}

		commits = append(commits, Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Parents: strings.Fields(fields[2]),
			Date:    date,
			Subject: fields[4],
			Refs:    decorated,
		})
	}

	return commits, nil
}

// ListCommitFiles returns the files a commit changed, a renamed one with its
// old path in From.
func (s *Service) ListCommitFiles(root, hash string) ([]Change, error) {
	stdout, err := s.run(root, "show", "--name-status", "--format=", "-z", hash)
	if err != nil {
		return nil, err
	}

	changes := []Change{}
	// A rename's letter is followed by its old path, then its new one.
	records := splitRecords(stdout)
	for len(records) >= 2 {
		change := Change{Status: classifyStatus(records[0]), Path: records[1]}
		records = records[2:]
		if change.Status == "renamed" {
			change.From, change.Path = change.Path, records[0]
			records = records[1:]
		}

		changes = append(changes, change)
	}

	s.locateChanges(root, changes)

	return changes, nil
}
