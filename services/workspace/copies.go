// copies.go keeps a copy of each game file a mod replaces, tells what the
// game's patches did to the game file under a mod's file, and merges the
// game's changes into a mod's file.

package workspace

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// MergeGameFile applies the game's changes to a game file to the mod's file
// that replaces it, and returns how many of the changes did not fit. The
// changes are the lines that differ between the kept copy of the game file
// and the game's file now, each with the unchanged lines around it that make
// it unique in the copy. A change fits where its copy's lines are in the mod's
// file once, wherever that is, and one the mod made already fits as it is.
// There is no looser match: a change whose lines are in the mod's file twice,
// or that the mod edited, is left out and counted.
//
// It starts the file's review before it writes, so that the review shows each
// change to keep or undo. It writes nothing, and starts no review, if no change
// is made. It returns an error, and writes nothing, if no workspace is open,
// the path is not a mod's file that replaces a game file, there is no kept copy
// to merge from, a file is binary, or the editor holds the mod's file unsaved.
func (s *Service) MergeGameFile(path string) (int, error) {
	ws, err := s.FindWritable(path)
	if err != nil {
		return 0, err
	}

	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	replaced, ok := findReplaced(mods, place.File)
	if !ok {
		return 0, fmt.Errorf("%s does not replace a file of the game", path)
	}

	mod, _ := findMod(*ws.Order.Load(), place.Origin)
	_, copyPath, ok := patch.FindCopy(ws.patchDir, *ws.Patches.Load(), mod.Start, replaced.Rel)
	if !ok {
		return 0, fmt.Errorf("PMT has no kept copy of %s to merge from", replaced.Rel)
	}

	base, err := patch.ReadCopy(copyPath)
	if err != nil {
		return 0, fmt.Errorf("cannot read the kept copy %s: %w", copyPath, err)
	}

	latest, err := files.ReadText(replaced.Path)
	if err != nil {
		return 0, err
	}

	text, err := ws.readSaved(path)
	if err != nil {
		return 0, err
	}

	lines, applied, unfit := mergeChanges(files.DecodeText(base).Text, latest.Text, text.Text)
	if applied == 0 {
		return unfit, nil
	}

	if err = s.TrackEdits(path, nil, false); err != nil {
		return 0, err
	}

	merged := files.Text{Text: strings.Join(lines, jomini.LineEnding(text.Text)), Encoding: text.Encoding}

	return unfit, s.Write(path, merged)
}

// mergeChanges returns the lines of a mod's text with the changes from a base
// text to a newer one applied, as MergeGameFile says. It also returns how many
// changes it made and how many did not fit. The line endings of the three texts
// are unified, and the lines come back without endings.
func mergeChanges(base, latest, text string) ([]string, int, int) {
	lines := strings.Split(unifyEndings(text), "\n")
	applied, unfit := 0, 0
	for _, change := range listChanges(unifyEndings(base), unifyEndings(latest)) {
		// revertHunk puts a hunk's lines before the edit where its lines after the
		// edit are, so the change goes in with its sides swapped.
		placed, err := revertHunk(lines, Hunk{Before: change.After, After: change.Before})
		switch {
		case err != nil:
			unfit++

		case !slices.Equal(placed, lines):
			applied++
			lines = placed
		}
	}

	return lines, applied, unfit
}

// A changedRun is one run of changed lines between a base text and a newer
// one.
type changedRun struct {
	// baseStart and baseEnd bound the run's lines in the base, and latestStart
	// and latestEnd its lines in the newer text. A line counts from 0, and an
	// end is the line after the run's last.
	baseStart, baseEnd     int
	latestStart, latestEnd int

	// head and tail count the unchanged lines of the base before and after the
	// run that make its lines unique in the base.
	head, tail int
}

// listChanges returns the changes from a base text to a newer one as hunks, in
// their order in the text. A hunk is a run of changed lines with the unchanged
// lines around it as its anchors. The anchors grow, a line more on each side
// at a time, until the hunk's lines before the edit are in the base exactly
// once, or the text ends. Changes whose anchors share or touch a line are one
// hunk.
func listChanges(base, latest string) []Hunk {
	baseLines, latestLines := strings.Split(base, "\n"), strings.Split(latest, "\n")
	runs := listRuns(base, latest)
	for position, run := range runs {
		head, tail := min(1, run.baseStart), min(1, len(baseLines)-run.baseEnd)
		for len(listStarts(baseLines, baseLines[run.baseStart-head:run.baseEnd+tail])) > 1 {
			head, tail = min(head+1, run.baseStart), min(tail+1, len(baseLines)-run.baseEnd)
		}

		runs[position].head, runs[position].tail = head, tail
	}

	var hunks []Hunk
	for position := 0; position < len(runs); position++ {
		first, last := runs[position], runs[position]
		end := last.baseEnd + last.tail
		for position+1 < len(runs) && runs[position+1].baseStart-runs[position+1].head <= end {
			position++
			last = runs[position]
			end = max(end, last.baseEnd+last.tail)
		}

		start := first.baseStart - first.head
		hunks = append(hunks, Hunk{
			Before: baseLines[start:end],
			After:  latestLines[first.latestStart-first.head : last.latestEnd+end-last.baseEnd],
		})
	}

	return hunks
}

// listRuns returns the runs of changed lines from a base text to a newer one,
// in their order, with no anchors yet.
func listRuns(base, latest string) []changedRun {
	dmp := diffmatchpatch.New()

	// go-diff's line mode makes each line one character. A text with a newline
	// added ends every line with one, so a diff's lines are its newlines.
	first, second, table := dmp.DiffLinesToChars(base+"\n", latest+"\n")
	diffs := dmp.DiffCharsToLines(dmp.DiffMain(first, second, false), table)

	var runs []changedRun
	baseAt, latestAt, isOpen := 0, 0, false
	for _, diff := range diffs {
		count := strings.Count(diff.Text, "\n")
		if diff.Type == diffmatchpatch.DiffEqual {
			baseAt, latestAt, isOpen = baseAt+count, latestAt+count, false

			continue
		}

		if !isOpen {
			runs = append(runs, changedRun{baseStart: baseAt, latestStart: latestAt})
			isOpen = true
		}

		if diff.Type == diffmatchpatch.DiffDelete {
			baseAt += count
		} else {
			latestAt += count
		}

		runs[len(runs)-1].baseEnd, runs[len(runs)-1].latestEnd = baseAt, latestAt
	}

	return runs
}

// FindGameChange returns what the game's patches did to the game file under a
// mod's file after the version the mod is made for, and the version of the
// newest patch that lists the file. The game file is the one the mod's file
// replaces, else the one at the mod's file's own path, which a patch may
// have removed. It also returns the version of the kept copy the game's
// changes can be merged from, "" where PMT keeps none or the game added or
// removed the file, which leaves nothing to merge. It returns false if no
// patch of the mod's range lists the file, and for a file of no mod.
func (s *Session) FindGameChange(mods *index.Index, place Place) (change patch.GameFile, version, base string, ok bool) {
	rel := place.Rel
	if replaced, isReplaced := findReplaced(mods, place.File); isReplaced {
		rel = replaced.Rel
	}

	mod, _ := findMod(*s.Order.Load(), place.Origin)
	patches := *s.Patches.Load()
	change, version, ok = patches.ListRange(mod.Start, s.Schema.Version).FindFile(rel)
	if ok && change.Status == schema.StatusChanged {
		base, _, _ = patch.FindCopy(s.patchDir, patches, mod.Start, rel)
	}

	return change, version, base, ok
}

// findReplaced returns the game's file that a mod's file replaces: the last
// file of the game at the same path in load order, as a DLC's file is read
// over the base game's. It returns false if the game has no file there.
func findReplaced(mods *index.Index, file files.File) (files.File, bool) {
	loaded, hidden, ok := mods.FindLoaded(file.Rel)
	if ok {
		hidden = append(hidden, loaded)
	}

	for _, other := range slices.Backward(hidden) {
		if other.Origin == files.OriginVanilla {
			return other, true
		}
	}

	return files.File{}, false
}

// keepCopies keeps a copy of each game file that a mod's file replaces, as it
// is at the install's version, and deletes the copies no mod's range can use.
// The copies are shared by every workspace of the game, so the mods of each
// decide which stay. A file that cannot be copied is left out, and its error
// is returned after the rest are kept.
func (s *Session) keepCopies(records app.Config) error {
	version := s.Schema.Version
	if version == "" {
		return nil
	}

	mods := s.Mods.Load()
	var failure error
	for _, position := range mods.ListOwnFiles() {
		if replaced, ok := findReplaced(mods, mods.Files[position]); ok {
			failure = errors.Join(failure, patch.KeepCopy(s.patchDir, version, replaced))
		}
	}

	patches := *s.Patches.Load()
	workshop := s.Game.ListWorkshopRoots()
	var starts []string
	for _, workspace := range records.Workspaces {
		install, row, err := findInstall(records, workspace.Install)
		if err != nil || row.ID != s.Game.ID {
			continue
		}

		for _, recorded := range workspace.Mods {
			mod := readMod(row, workshop, recorded)
			start, _ := patches.ResolveStart(mod.Descriptor.SupportedVersion, mod.Base, resolveVersion(row, install))
			starts = append(starts, start)
		}
	}

	patch.DeleteCopies(s.patchDir, append(slices.Clone(s.installVersions), version), starts)

	return failure
}
