package git

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/samber/lo"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// A LineChange is one run of lines a text changes from its file's last commit, by its lines in the text. A run of
// removed lines has no line of its own, so it is marked on the line before it, or on the first line.
type LineChange struct {
	Kind  string `json:"kind"` // "added", "changed" or "removed"
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// DiffFile returns the runs of lines a text changes from its file's text at the last commit, for the editor's change
// bars. A line that ends in \r\n is read as one that ends in \n in both texts. It returns an error if the file is in
// no repository or git does not track it.
func (git *Service) DiffFile(path, text string) ([]LineChange, error) {
	// git reads a path that starts with ./ from the folder it runs in
	dir, rel := filepath.Dir(path), "./"+filepath.Base(path)
	// a file git does not track is not in the last commit, which --quiet keeps git from printing as an error
	if _, err := git.Run(dir, "rev-parse", "--verify", "--quiet", "HEAD:"+rel); err != nil {
		return nil, err
	}
	committed, err := git.ReadFile(dir, "HEAD", rel)
	if err != nil {
		return nil, err
	}
	diff := diffmatchpatch.New()
	// go-diff's line mode turns each line into one rune, so a piece's runes count its lines
	first, second, _ := diff.DiffLinesToRunes(
		strings.ReplaceAll(committed, "\r\n", "\n"),
		strings.ReplaceAll(text, "\r\n", "\n"),
	)
	changes := []LineChange{}
	// the text's line where the pieces since the last unchanged run start, and the lines they add and remove
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

// appendRun adds to the list the run of lines that starts at the given line, adding and removing the given numbers of
// lines. Lines added are a run of their own, changed when lines were removed too; lines only removed are marked on
// the line before them, or on the first. A run that adds and removes nothing adds nothing.
func appendRun(changes []LineChange, line, added, removed int) []LineChange {
	switch {
	case added > 0:
		kind := lo.Ternary(removed > 0, "changed", "added")
		return append(changes, LineChange{Kind: kind, Start: line, End: line + added - 1})
	case removed > 0:
		marked := max(line-1, 1)
		return append(changes, LineChange{Kind: "removed", Start: marked, End: marked})
	}
	return changes
}
