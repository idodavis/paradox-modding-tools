// revert.go gives a file back its text from before an agent's edits, by undoing each run of lines the edits changed.

package workspace

import (
	"errors"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Hunk is one run of lines an agent's edit changed in a file.
type Hunk struct {
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// tabSpaces is what an agent's diff can write in place of each tab of a file's line.
const tabSpaces = "  "

// RevertHunks gives a file of the open workspace back its text from before an agent's edits, given as each edit's
// hunks, the earliest edit first. A file the earliest edit made goes to PMT's trash when it ends empty; any other is
// written in its own line endings. It returns an error, and changes nothing, if no workspace is open, the file cannot
// be read, it has unsaved text in the editor, an edit has no hunk, or a hunk's lines are not in the file once.
func (wsp *Service) RevertHunks(path string, edits [][]Hunk, isNew bool) error {
	session, err := wsp.Session()
	if err != nil {
		return err
	}
	text, err := files.ReadText(path)
	if err != nil {
		return err
	}
	if draft, open := session.LookupBuffer(path); open && draft != text.Text {
		return errors.New("it has unsaved text in the editor: save or revert it first")
	}
	// an empty text has no lines, and a line that ends in \r\n reads as one that ends in \n
	var lines []string
	if text.Text != "" {
		lines = strings.Split(strings.ReplaceAll(text.Text, "\r\n", "\n"), "\n")
	}
	// the edits go back the latest first, and each edit's hunks the last first
	for _, hunks := range slices.Backward(edits) {
		if len(hunks) == 0 {
			return errors.New("the agent sent no lines for one of its edits")
		}
		for _, hunk := range slices.Backward(hunks) {
			if lines, err = revertHunk(lines, hunk); err != nil {
				return err
			}
		}
	}
	if isNew && strings.Join(lines, "") == "" {
		_, err = wsp.Delete(path)
		return err
	}
	return wsp.Write(path, files.Text{Text: strings.Join(lines, jomini.LineEnding(text.Text)), Encoding: text.Encoding})
}

// revertHunk returns a file's lines with one hunk taken back: its lines after the change, found once in the file, in
// place of its lines before. A hunk whose lines before are in the file once, and its lines after nowhere, is undone
// already, and the lines come back as they are. It returns an error when the lines after are in the file more than
// once, or when neither are.
func revertHunk(lines []string, hunk Hunk) ([]string, error) {
	starts := listStarts(lines, hunk.After)
	switch {
	case len(starts) == 1:
		start, end := starts[0], starts[0]+len(hunk.After)
		return slices.Concat(lines[:start], restoreLines(hunk, lines[start:end]), lines[end:]), nil
	case len(starts) > 1:
		return nil, errors.New("its edited lines are in it more than once")
	case len(hunk.Before) > 0 && len(listStarts(lines, hunk.Before)) == 1:
		return lines, nil
	}
	return nil, errors.New("it changed since the agent's edit")
}

// listStarts lists where a run of a diff's lines starts in a file's lines, each place it is. An empty run starts at
// every line.
func listStarts(lines, run []string) []int {
	var starts []int
	for start := range lines {
		if start+len(run) <= len(lines) && slices.EqualFunc(lines[start:start+len(run)], run, isSameLine) {
			starts = append(starts, start)
		}
	}
	return starts
}

// isSameLine reports whether a file's line is a diff's line, each tab of the file's read as the spaces a diff writes.
func isSameLine(fileLine, diffLine string) bool {
	return fileLine == diffLine || strings.ReplaceAll(fileLine, "\t", tabSpaces) == diffLine
}

// restoreLines returns a hunk's lines before the change as the file writes them, from the file's lines that hold its
// lines after. A line the change kept is the file's own. A line it took out gets the file's tabs back in its indent
// when the file wrote tabs where the diff wrote spaces.
func restoreLines(hunk Hunk, fileLines []string) []string {
	own := make(map[string]string, len(hunk.After))
	hasTabs := false
	for offset, line := range hunk.After {
		own[line] = fileLines[offset]
		hasTabs = hasTabs || fileLines[offset] != line
	}
	return lo.Map(hunk.Before, func(line string, _ int) string {
		if kept, ok := own[line]; ok {
			return kept
		}
		if !hasTabs {
			return line
		}
		// each pair of spaces at the start of the line is one of the file's tabs
		pairs := (len(line) - len(strings.TrimLeft(line, " "))) / len(tabSpaces)
		return strings.Repeat("\t", pairs) + line[pairs*len(tabSpaces):]
	})
}
