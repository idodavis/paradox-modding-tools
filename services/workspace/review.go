// review.go keeps the review of the files a coding agent edits: the text
// each had before the agent's edits, until the user keeps or undoes them.

package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Hunk is one run of lines an edit changed in a file, an agent's or the
// game's.
type Hunk struct {
	// Before holds the run's lines before the edit and After its lines after
	// it.
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// A Review is a file under review as the page lists it.
type Review struct {
	Place

	// Added and Removed count the lines the file's text adds and removes
	// against its baseline.
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// A fileReview is one file's baseline: its text from before the agent's
// edits that are not kept yet. It is written in the file's own line endings,
// so that writing it back is exact.
type fileReview struct {
	path     string
	baseline string

	// isNew is true if the agent created the file.
	isNew bool

	// turns holds the file's text from before each turn of an agent that
	// changed it since the review started, the earliest turn first.
	turns []turnText
}

// A turnText is a file's text from before one turn of an agent changed it,
// under the page's id for that turn.
type turnText struct {
	turn string
	text string
}

// unsavedText is the error of a change to a file whose text the editor
// holds unsaved.
const unsavedText = "it has unsaved text in the editor: save or revert it first"

// tabSpaces is what an agent's diff can write in place of each tab of a
// file's line.
const tabSpaces = "  "

// TrackEdits starts the review of a file an agent edits, given the edits one
// turn's tool call made to it as hunks, earliest first. The baseline is the
// file's text on disk with the edits undone, or "" for a file the agent
// created that is not on disk yet. A file that has a review already keeps its
// baseline, and that same text is kept as the file's text from before the
// turn, unless the turn has one. A file outside the workspace is reviewed
// like any other, and its folder is watched from then on, so that its changes
// on disk are seen. It returns an error, and starts no review, if no
// workspace is open, the file is the game's or an external mod's or cannot be
// read, an edit has no hunk, or a hunk's lines are not in the file once. A
// file under review whose edits cannot be undone keeps no text for the turn.
func (s *Service) TrackEdits(path string, edits [][]Hunk, isNew bool, turn string) error {
	ws, err := s.Session()
	if err != nil {
		return err
	}

	place := ws.Locate(ws.Mods.Load(), path)
	if err := ws.checkWritable(place); err != nil {
		return err
	}

	review, err := ws.findReview(path)
	isTracked := err == nil
	if isTracked && review.findTurn(turn) >= 0 {
		return nil
	}

	turnBaseline, err := revertFile(path, edits, isNew)
	if err != nil && isTracked {
		return nil
	}

	if err != nil {
		return err
	}

	// a review or a turn another call added meanwhile is kept
	key := files.ToKey(path)
	ws.reviewsMu.Lock()
	review, isTracked = ws.reviews[key]
	if !isTracked {
		review = fileReview{path: path, baseline: turnBaseline, isNew: isNew}
	}

	if review.findTurn(turn) < 0 {
		review.turns = append(review.turns, turnText{turn: turn, text: turnBaseline})
		ws.reviews[key] = review
	}
	ws.reviewsMu.Unlock()

	if !isTracked {
		if place.Root == "" {
			_ = ws.watcher.notifier.Add(filepath.Dir(path))
		}

		application.Get().Event.Emit(EventReviewChanged, path)
	}

	return nil
}

// ListReviews returns the files under review, sorted by path, each with the
// lines it adds and removes against its baseline. A file that is gone counts
// as removing all its baseline's lines. It returns none while no workspace is
// open.
func (s *Service) ListReviews() []Review {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	ws.reviewsMu.Lock()
	listed := slices.Collect(maps.Values(ws.reviews))
	ws.reviewsMu.Unlock()

	slices.SortFunc(listed, func(first, second fileReview) int { return strings.Compare(first.path, second.path) })

	mods := ws.Mods.Load()
	reviews := make([]Review, len(listed))
	for position, review := range listed {
		text, _ := files.ReadText(review.path)
		reviews[position] = Review{Place: ws.Locate(mods, review.path)}
		reviews[position].Added, reviews[position].Removed = files.CountLines(
			unifyEndings(review.baseline), unifyEndings(text.Text),
		)
	}

	return reviews
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

// KeepFile ends a file's review and leaves the file as it is on disk. A file
// with no review is left alone. It returns an error if no workspace is open.
func (s *Service) KeepFile(path string) error {
	ws, err := s.Session()
	if err != nil {
		return err
	}

	ws.endReview(path)

	return nil
}

// UndoFile takes back an agent's changes to a file under review. With no
// turn it writes the file's baseline, or moves a file the agent created to
// PMT's trash, and ends the review. With a turn's id it writes the file's
// text from before that turn, which takes back the later turns' changes too;
// the review goes on unless that text is the baseline. The first turn of a
// review is undone as the whole review is. It returns an error, and changes
// nothing, if no workspace is open, the file has no review or cannot be
// read, the turn did not change it, or the editor holds unsaved text of it.
func (s *Service) UndoFile(path, turn string) error {
	ws, err := s.FindWritable(path)
	if err != nil {
		return err
	}

	review, err := ws.findReview(path)
	if err != nil {
		return err
	}

	position := review.findTurn(turn)
	if turn != "" && position < 0 {
		return fmt.Errorf("%s was not changed in that turn", path)
	}

	text, err := ws.readSaved(path)
	if err != nil {
		return err
	}

	if position > 0 {
		turnBaseline := review.turns[position].text
		if err := s.Write(path, files.Text{Text: turnBaseline, Encoding: text.Encoding}); err != nil {
			return err
		}

		review.turns = review.turns[:position]
		ws.reviewsMu.Lock()
		ws.reviews[files.ToKey(path)] = review
		ws.reviewsMu.Unlock()

		return nil
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

// findTurn returns the position of a turn among the turns that changed the
// file. It returns -1 if the turn did not change it, and for "".
func (f fileReview) findTurn(turn string) int {
	return slices.IndexFunc(f.turns, func(candidate turnText) bool { return turn != "" && candidate.turn == turn })
}

// readSaved returns a file's text on disk. It returns an error if the editor
// holds other text for the file, which is unsaved; line endings do not count.
func (s *Session) readSaved(path string) (files.Text, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return files.Text{}, err
	}

	draft, isOpen := s.LookupBuffer(path)
	if isOpen && unifyEndings(draft) != unifyEndings(text.Text) {
		return files.Text{}, errors.New(unsavedText)
	}

	return text, nil
}

// isSettled reports whether the review is over: the file's lines on disk are
// the baseline's, whatever their line endings, or the file is gone and the
// agent created it or its baseline is empty, as it is for a file an agent
// reports as changed from nothing. A file that cannot be read for another
// reason is not settled.
func (f fileReview) isSettled() bool {
	text, err := files.ReadText(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return f.isNew || f.baseline == ""
	}

	return err == nil && unifyEndings(text.Text) == unifyEndings(f.baseline)
}

// unifyEndings returns a text with each line ending as \n.
func unifyEndings(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
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

// settleReviews ends the review of each changed file that is settled, and
// sends review:changed for one that is not, since its counts changed. The
// watcher's changes include every write PMT makes.
func (s *Session) settleReviews(changes []FileChange) {
	for _, change := range changes {
		review, err := s.findReview(change.Path)
		if err != nil {
			continue
		}

		if review.isSettled() {
			s.endReview(change.Path)

			continue
		}

		application.Get().Event.Emit(EventReviewChanged, change.Path)
	}
}

// revertFile returns a file's text on disk with an agent's edits undone, in
// the file's own line endings. It is "" for a file the agent created that is
// not on disk yet. It returns an error if the file cannot be read or the
// edits cannot be undone in it.
func revertFile(path string, edits [][]Hunk, isNew bool) (string, error) {
	if isNew && !files.IsFile(path) {
		return "", nil
	}

	text, err := files.ReadText(path)
	if err != nil {
		return "", err
	}

	lines, err := revertEdits(text.Text, edits)
	if err != nil {
		return "", err
	}

	return strings.Join(lines, jomini.LineEnding(text.Text)), nil
}

// revertEdits returns the lines of a text with an agent's edits undone, each
// given as its hunks, the earliest edit first. It returns an error if an edit
// has no hunk, or a hunk's lines are not in the text once.
func revertEdits(text string, edits [][]Hunk) ([]string, error) {
	var lines []string
	if text != "" {
		lines = strings.Split(unifyEndings(text), "\n")
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
