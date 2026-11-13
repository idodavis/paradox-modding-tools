// diff.go finds the lines a text changes from its last commit.

package git

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// LineChange is one run of lines changed from the last commit. A removed run
// has no line left, so it is marked on the line before it.
type LineChange struct {
	// Kind is "added", "changed" or "removed".
	Kind string `json:"kind"`

	// Start and End count lines from 1.
	Start int `json:"start"`
	End   int `json:"end"`
}

// DiffFile returns the runs of lines the text changes from the file's last
// commit. It returns an error if git does not track the file.
func (s *Service) DiffFile(path, text string) ([]LineChange, error) {
	// git reads a path that starts with ./ from the folder it runs in.
	dir, rel := filepath.Dir(path), "./"+filepath.Base(path)
	committed, err := s.ReadFile(dir, "HEAD", rel)
	if err != nil {
		return nil, err
	}

	diff := diffmatchpatch.New()
	// go-diff's line mode makes each line a rune, so runes count lines.
	first, second, _ := diff.DiffLinesToRunes(
		strings.ReplaceAll(committed, "\r\n", "\n"),
		strings.ReplaceAll(text, "\r\n", "\n"),
	)

	changes := []LineChange{}
	line, added, removed := 1, 0, 0
	for _, piece := range diff.DiffMainRunes(first, second, false) {
		count := utf8.RuneCountInString(piece.Text)
		switch piece.Type {
		case diffmatchpatch.DiffInsert:
			added += count

		case diffmatchpatch.DiffDelete:
			removed += count

		case diffmatchpatch.DiffEqual:
			changes = appendRun(changes, line, added, removed)
			line, added, removed = line+added+count, 0, 0
		}
	}

	return appendRun(changes, line, added, removed), nil
}

func appendRun(changes []LineChange, line, added, removed int) []LineChange {
	switch {
	case added > 0:
		kind := "added"
		if removed > 0 {
			kind = "changed"
		}

		return append(changes, LineChange{Kind: kind, Start: line, End: line + added - 1})

	case removed > 0:
		marked := max(line-1, 1)

		return append(changes, LineChange{Kind: "removed", Start: marked, End: marked})
	}

	return changes
}
