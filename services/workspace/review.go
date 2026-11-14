// review.go keeps the review of the files a coding agent edits: the text
// each had before the agent's edits, until the user keeps or undoes them.

package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Hunk is one run of lines an agent's edit changed in a file.
type Hunk struct {
	// Before holds the run's lines before the edit and After its lines after
	// it.
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// A fileReview is one file's baseline: its text from before the agent's
// edits that are not kept yet. It is written in the file's own line endings,
// so that writing it back is exact.
type fileReview struct {
	path     string
	baseline string

	// isNew is true if the agent created the file.
	isNew bool
}

// unsavedText is the error of a change to a file whose text the editor
// holds unsaved.
const unsavedText = "it has unsaved text in the editor: save or revert it first"

// tabSpaces is what an agent's diff can write in place of each tab of a
// file's line.
const tabSpaces = "  "

// TrackEdits starts the review of a file, given each of the agent's edits to
// it as hunks, the earliest edit first. The baseline is the file's text on
// disk with the edits undone, the latest edit and its last hunk first. A file
// the agent created that is not on disk yet has the baseline "", and its
// edits are not read. A file that has a review already is left as it is. A
// baseline equal to the file's text starts the review as well, since the
// agent's write follows. It returns an error, and starts no review, if no
// workspace is open, the file is outside the workspace's mods, it cannot be
// read, an edit has no hunk, or a hunk's lines are not in the file once.
func (s *Service) TrackEdits(path string, edits [][]Hunk, isNew bool) error {
	ws, err := s.FindWritable(path)
	if err != nil {
		return err
	}

	if _, err := ws.findReview(path); err == nil {
		return nil
	}

	baseline := ""
	if !isNew || files.IsFile(path) {
		text, err := files.ReadText(path)
		if err != nil {
			return err
		}

		lines, err := revertEdits(text.Text, edits)
		if err != nil {
			return err
		}

		baseline = strings.Join(lines, jomini.LineEnding(text.Text))
	}

	// a review another call started meanwhile is kept
	key := files.ToKey(path)
	ws.reviewsMu.Lock()
	_, isTracked := ws.reviews[key]
	if !isTracked {
		ws.reviews[key] = fileReview{path: path, baseline: baseline, isNew: isNew}
	}
	ws.reviewsMu.Unlock()

	if !isTracked {
		application.Get().Event.Emit(EventReviewChanged, path)
	}

	return nil
}

// ListReviews returns the places of the files under review, sorted by path.
// It returns none while no workspace is open.
func (s *Service) ListReviews() []Place {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	ws.reviewsMu.Lock()
	paths := make([]string, 0, len(ws.reviews))
	for _, review := range ws.reviews {
		paths = append(paths, review.path)
	}
	ws.reviewsMu.Unlock()

	slices.Sort(paths)

	mods := ws.Mods.Load()
	places := make([]Place, len(paths))
	for position, path := range paths {
		places[position] = ws.Locate(mods, path)
	}

	return places
}

// Baseline returns a file's baseline. It returns an error if no workspace is
// open or the file has no review.
func (s *Service) Baseline(path string) (string, error) {
	ws, err := s.Session()
	if err != nil {
		return "", err
	}

	review, err := ws.findReview(path)

	return review.baseline, err
}

// SetBaseline replaces a file's baseline with the given text. The review ends
// if that is the file's text on disk. It returns an error if no workspace is
// open or the file has no review.
func (s *Service) SetBaseline(path, text string) error {
	ws, err := s.Session()
	if err != nil {
		return err
	}

	review, err := ws.findReview(path)
	if err != nil {
		return err
	}

	review.baseline = text
	if review.isSettled() {
		ws.endReview(path)

		return nil
	}

	ws.reviewsMu.Lock()
	ws.reviews[files.ToKey(path)] = review
	ws.reviewsMu.Unlock()

	application.Get().Event.Emit(EventReviewChanged, path)

	return nil
}

// KeepFile ends a file's review and leaves the file as the agent wrote it. A
// file with no review is left alone. It returns an error if no workspace is
// open.
func (s *Service) KeepFile(path string) error {
	ws, err := s.Session()
	if err != nil {
		return err
	}

	ws.endReview(path)

	return nil
}

// UndoFile writes a file's baseline in the file's encoding, or moves a file
// the agent created to PMT's trash, and ends its review. It returns an error,
// and changes nothing, if no workspace is open, the file has no review, is
// outside the workspace's mods or cannot be read, or has unsaved text in the
// editor.
func (s *Service) UndoFile(path string) error {
	ws, err := s.FindWritable(path)
	if err != nil {
		return err
	}

	review, err := ws.findReview(path)
	if err != nil {
		return err
	}

	text, err := files.ReadText(path)
	if err != nil {
		return err
	}

	if draft, isOpen := ws.LookupBuffer(path); isOpen && draft != text.Text {
		return errors.New(unsavedText)
	}

	if review.isNew {
		_, err = s.Delete(path)
	} else {
		err = s.Write(path, files.Text{Text: review.baseline, Encoding: text.Encoding})
	}
	if err != nil {
		return err
	}

	ws.endReview(path)

	return nil
}

// isSettled reports whether the review is over: the file's text on disk is
// the baseline, or the agent created the file and it is gone. A file that
// cannot be read for another reason is not settled.
func (f fileReview) isSettled() bool {
	text, err := files.ReadText(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return f.isNew
	}

	return err == nil && text.Text == f.baseline
}

// findReview returns a file's review. It returns an error if the file has
// none.
func (s *Session) findReview(path string) (fileReview, error) {
	s.reviewsMu.Lock()
	defer s.reviewsMu.Unlock()

	review, ok := s.reviews[files.ToKey(path)]
	if !ok {
		return fileReview{}, fmt.Errorf("%s is not under review", path)
	}

	return review, nil
}

// endReview drops a file's review and sends review:changed. A file with no
// review is left alone.
func (s *Session) endReview(path string) {
	key := files.ToKey(path)

	s.reviewsMu.Lock()
	_, isTracked := s.reviews[key]
	delete(s.reviews, key)
	s.reviewsMu.Unlock()

	if isTracked {
		application.Get().Event.Emit(EventReviewChanged, path)
	}
}

// settleReviews ends the review of each changed file that is settled. The
// watcher's changes include every write PMT makes.
func (s *Session) settleReviews(changes []FileChange) {
	for _, change := range changes {
		review, err := s.findReview(change.Path)
		if err == nil && review.isSettled() {
			s.endReview(change.Path)
		}
	}
}

// revertEdits returns the lines of a text with an agent's edits undone, each
// given as its hunks, the earliest edit first. It returns an error if an edit
// has no hunk, or a hunk's lines are not in the text once.
func revertEdits(text string, edits [][]Hunk) ([]string, error) {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	}

	var err error

	// the latest edit and the last hunk go back first
	for _, hunks := range slices.Backward(edits) {
		if len(hunks) == 0 {
			return nil, errors.New("the agent sent no lines for one of its edits")
		}

		for _, hunk := range slices.Backward(hunks) {
			if lines, err = revertHunk(lines, hunk); err != nil {
				return nil, err
			}
		}
	}

	return lines, nil
}

// revertHunk returns a file's lines with one hunk undone. The lines come back
// unchanged if the hunk is undone already. It returns an error if the hunk's
// lines after the edit are in the file more than once, or if neither its
// lines after nor its lines before are there.
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

// listStarts returns every index in a file's lines where a run of a diff's
// lines starts.
func listStarts(lines, run []string) []int {
	var starts []int
	for start := range lines {
		if start+len(run) <= len(lines) && slices.EqualFunc(lines[start:start+len(run)], run, isSameLine) {
			starts = append(starts, start)
		}
	}

	return starts
}

// isSameLine reports whether a file's line equals a diff's line, reading the
// file's tabs as two spaces.
func isSameLine(fileLine, diffLine string) bool {
	return fileLine == diffLine || strings.ReplaceAll(fileLine, "\t", tabSpaces) == diffLine
}

// restoreLines returns a hunk's lines from before the edit, written as the
// file writes them. It is given the file's lines that match the hunk's lines
// after the edit. A line that is also one after the edit is the file's own
// copy. Any other line, in a file that writes tabs, has its leading pairs of
// spaces turned back into tabs.
func restoreLines(hunk Hunk, fileLines []string) []string {
	own := make(map[string]string, len(hunk.After))
	hasTabs := false
	for offset, line := range hunk.After {
		own[line] = fileLines[offset]
		hasTabs = hasTabs || fileLines[offset] != line
	}

	lines := make([]string, len(hunk.Before))
	for position, line := range hunk.Before {
		fileLine, isOwn := own[line]
		switch {
		case isOwn:
			lines[position] = fileLine

		case hasTabs:
			pairs := (len(line) - len(strings.TrimLeft(line, " "))) / len(tabSpaces)
			lines[position] = strings.Repeat("\t", pairs) + line[pairs*len(tabSpaces):]

		default:
			lines[position] = line
		}
	}

	return lines
}
