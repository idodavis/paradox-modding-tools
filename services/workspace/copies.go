// copies.go records the hash of the game's files by game version, keeps a copy
// of each game file a mod replaces, tells which of those files the game
// changed within a mod's range, and merges the game's changes into a mod's
// file.

package workspace

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/app"
)

// copyHashes holds, for each game version PMT has seen, the SHA-256 of the
// content of each game file at that version, by the file's path from the
// game's folder. A version holds every file the game reads that the index scan
// read, and the files a mod replaced when PMT saw it, whether the scan read
// them or not.
type copyHashes map[string]map[string]string

// The record of the copies in a folder of patches, and the folder of the copies
// under it. A copy is gzipped and named by the hash of its content, so a game
// file that did not change between versions is kept once.
const (
	copiesFile = "copies.gob"
	copiesDir  = "copies"
	copyExt    = ".gz"
)

// ResolveCopiesFile returns the file of a folder of patches that records the
// hash of each kept game file at each version of the game.
func ResolveCopiesFile(dir string) string {
	return filepath.Join(dir, copiesFile)
}

// FindCopy returns the text of the kept copy of a game file that is a valid
// base for a three-way compare of a mod's file. The file is named by its path
// from the game's folder. It returns false if there is no such copy, or if the
// file is a picture or other binary file, of which only the hash is kept. It
// returns an error if no workspace is open or the copy cannot be read.
func (s *Service) FindCopy(modID, rel string) (string, bool, error) {
	ws, err := s.Session()
	if err != nil {
		return "", false, err
	}

	start, _ := ws.ResolveRange(modID)
	_, copyPath, ok := ws.FindCopyFile(start, rel)
	if !ok {
		return "", false, nil
	}

	content, err := unpackCopy(copyPath)
	if err != nil {
		return "", false, fmt.Errorf("cannot read the kept copy %s: %w", copyPath, err)
	}

	if files.IsBinary(content) {
		return "", false, nil
	}

	return files.DecodeText(content).Text, true, nil
}

// MergeGameFile applies the game's changes to a game file to the mod's file
// that replaces it, and returns how many of the changes did not fit. The
// changes are the lines that differ between the kept copy FindCopy gives and
// the game's file now, each with the unchanged lines around it that make it
// unique in the copy. A change fits where its copy's lines are in the mod's
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

	base, ok, err := s.FindCopy(place.Origin, replaced.Rel)
	if err != nil {
		return 0, err
	}

	if !ok {
		return 0, fmt.Errorf("PMT has no kept copy of %s to merge from", replaced.Rel)
	}

	latest, err := files.ReadText(replaced.Path)
	if err != nil {
		return 0, err
	}

	text, err := ws.readSaved(path)
	if err != nil {
		return 0, err
	}

	lines, applied, unfit := mergeChanges(base, latest.Text, text.Text)
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
// once, or the text ends. Its lines before are the base's and its lines after
// are the newer text's. Changes whose anchors share or touch a line are one
// hunk.
func listChanges(base, latest string) []Hunk {
	baseLines, latestLines := strings.Split(base, "\n"), strings.Split(latest, "\n")
	runs := listRuns(base, latest)
	for position := range runs {
		runs[position].head, runs[position].tail = growAnchors(baseLines, runs[position])
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

// growAnchors returns how many unchanged lines before and after a run make
// its lines in the base unique, as revertHunk reads them. It starts with one
// line on each side and adds one on each side, until the run's lines with its
// anchors are in the base once. A side stops at the start or the end of the
// text, and the whole text is always unique.
func growAnchors(lines []string, changed changedRun) (int, int) {
	head, tail := min(1, changed.baseStart), min(1, len(lines)-changed.baseEnd)
	for len(listStarts(lines, lines[changed.baseStart-head:changed.baseEnd+tail])) > 1 {
		head = min(head+1, changed.baseStart)
		tail = min(tail+1, len(lines)-changed.baseEnd)
	}

	return head, tail
}

// unpackCopy returns the content a gzipped copy holds.
func unpackCopy(path string) ([]byte, error) {
	packed, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = packed.Close() }()

	unpacked, err := gzip.NewReader(packed)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(unpacked)
}

// ListStaleFiles returns the game files a mod's files replace that changed
// within the mod's range, by their path from the game's folder. A file the
// range's patches list has the status they give. Any other whose kept copy at
// the range's start differs from the game's file now is "modified", or
// "deleted" if the game no longer has it. A mod with no range, or no kept copy
// to compare, gives only what the patches list.
//
// Finding them reads every file of the mod, and the editor asks for each file
// of a mod. So the answer is kept for the mods' index, the patches and the load
// order it was made from, and the caller must not change it.
func (s *Session) ListStaleFiles(modID string) []GameFile {
	mods, patches, order := s.Mods.Load(), s.Patches.Load(), s.Order.Load()

	s.stale.Lock()
	defer s.stale.Unlock()

	if s.stale.mods != mods || s.stale.patches != patches || s.stale.order != order {
		s.stale.mods, s.stale.patches, s.stale.order = mods, patches, order
		s.stale.files = map[string][]GameFile{}
	}

	if stale, ok := s.stale.files[modID]; ok {
		return stale
	}

	stale := s.listStale(modID)
	s.stale.files[modID] = stale

	return stale
}

// listStale reads the stale files of a mod, as ListStaleFiles says.
func (s *Session) listStale(modID string) []GameFile {
	start, end := s.ResolveRange(modID)
	listed := map[string]string{}
	for _, patch := range s.Patches.Load().ListRange(start, end) {
		for _, file := range patch.GameFiles {
			listed[file.Rel] = file.Status
		}
	}

	mods := s.Mods.Load()
	var stale []GameFile
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		if file.Origin != modID {
			continue
		}

		rel, replaced := file.Rel, false
		if gameFile, ok := findReplaced(mods, file); ok {
			rel, replaced = gameFile.Rel, true
		}

		status, ok := listed[rel]
		if !ok {
			status = s.compareCopy(start, end, rel, replaced)
		}

		if status != "" {
			stale = append(stale, GameFile{Rel: rel, Status: status})
		}
	}

	return stale
}

// compareCopy returns "modified" if a game file's hash at the end version
// differs from its kept copy at the start, "deleted" if the file is no longer
// replaced because the game dropped it, and "" if it is the same or there is no
// copy to compare.
func (s *Session) compareCopy(start, end, rel string, replaced bool) string {
	_, base, ok := s.FindBase(start, rel)
	current := (*s.copies.Load())[end][rel]

	switch {
	case !ok:
		return ""
	case !replaced:
		return FileDeleted
	case current != "" && current != base:
		return FileModified
	}

	return ""
}

// FindBase returns the version and the recorded hash of a game file that is a
// valid base for a mod whose range starts at the given version. That is the
// hash at the start, or at the latest earlier version if every patch after
// that version, up to the start, lists the files it changed and none lists
// this one. A version between with no patch at all is not known, and is read
// as having changed nothing. It returns false if there is no such hash. A copy
// of the file is kept only if a mod replaced it, which FindCopyFile says.
func (s *Session) FindBase(start, rel string) (version, hash string, ok bool) {
	record := *s.copies.Load()
	version, found := findNearest(record, start)
	hash, held := record[version][rel]
	if !found || !held {
		return "", "", false
	}

	for _, patch := range s.Patches.Load().ListRange(version, start) {
		listsRel := slices.ContainsFunc(patch.GameFiles, func(file GameFile) bool { return file.Rel == rel })
		if len(patch.GameFiles) == 0 || listsRel {
			return "", "", false
		}
	}

	return version, hash, true
}

// FindCopyFile returns the version and the file of the kept copy of a game file
// that is a valid base for a mod whose range starts at the given version, as
// FindBase says. It returns false if FindBase finds none, if the file is a
// picture or other binary file, or if no copy of it was kept, which is so of a
// file no mod replaced when PMT saw that version.
func (s *Session) FindCopyFile(start, rel string) (version, file string, ok bool) {
	version, hash, ok := s.FindBase(start, rel)
	if !ok || !files.Classify(rel).IsText() {
		return "", "", false
	}

	file = filepath.Join(s.patchDir, copiesDir, hash+copyExt)
	if !files.IsFile(file) {
		return "", "", false
	}

	return version, file, true
}

// findNearest returns the latest version of the record that is not after the
// start. It returns false if the start is "" or the record has none.
func findNearest(record copyHashes, start string) (string, bool) {
	if start == "" {
		return "", false
	}

	nearest := ""
	for version := range record {
		isBefore := game.CompareVersions(version, start) <= 0
		if isBefore && (nearest == "" || game.CompareVersions(version, nearest) > 0) {
			nearest = version
		}
	}

	return nearest, nearest != ""
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

// saveCopies records the hash of each game file the index scan read, for the
// install's version, and keeps a copy of each game file a mod replaces. It then
// deletes what no range can use and saves the record, which the session then
// holds. A file that cannot be copied is left out, and its error is returned
// after the rest are kept.
func (s *Session) saveCopies(records app.Config) error {
	version := s.Schema.Version
	if version == "" {
		s.copies.Store(&copyHashes{})

		return nil
	}

	// A record that cannot be read starts again, and the copies it named stay
	// until it records their hashes again.
	file := ResolveCopiesFile(s.patchDir)
	record, _ := files.LoadGob[copyHashes](file)
	if record == nil {
		record = copyHashes{}
	}

	hashes := ListHashes(s.Install)
	mods := s.Mods.Load()
	var failure error
	for _, position := range mods.ListOwnFiles() {
		gameFile, replaced := findReplaced(mods, mods.Files[position])
		if !replaced {
			continue
		}

		hash, err := writeCopy(s.patchDir, gameFile)
		failure = errors.Join(failure, err)
		if err == nil {
			hashes[gameFile.Rel] = hash
		}
	}

	record[version] = hashes
	s.deleteCopies(record, records)
	s.copies.Store(&record)

	return errors.Join(failure, files.SaveGob(file, record))
}

// ListHashes returns the hash of each file of the game that the index scan
// read and the game reads, by the file's path from the game's folder. A file
// the scan did not read has none, such as a picture or a localization file in
// another language.
func ListHashes(install *index.Index) map[string]string {
	hashes := map[string]string{}

	for position, file := range install.Files {
		if file.Hash != "" && install.IsLoaded(int32(position)) {
			hashes[file.Rel] = file.Hash
		}
	}

	return hashes
}

// CompareHashes returns the game files that differ between the hashes of an
// older version and the current ones: those the older lacks are added, those
// whose hash differs are modified, and those the older has that the install no
// longer has are deleted. A file the install has that the scan did not read
// has no current hash, and is none of these.
func CompareHashes(install *index.Index, older, current map[string]string) []GameFile {
	var changes []GameFile

	for rel, hash := range current {
		earlier, ok := older[rel]

		switch {
		case !ok:
			changes = append(changes, GameFile{Rel: rel, Status: FileAdded})

		case earlier != hash:
			changes = append(changes, GameFile{Rel: rel, Status: FileModified})
		}
	}

	for rel := range older {
		_, hasCurrent := current[rel]
		if _, _, loaded := install.FindLoaded(rel); !hasCurrent && !loaded {
			changes = append(changes, GameFile{Rel: rel, Status: FileDeleted})
		}
	}

	slices.SortFunc(changes, func(first, second GameFile) int { return strings.Compare(first.Rel, second.Rel) })

	return changes
}

// writeCopy returns the hash of a game file's content, which is the file's
// Hash where the scan read it, else the file is read and hashed. If the file is
// text, it also gzips the content into the folder of copies under that hash,
// unless a copy is there already. A picture or other binary file is hashed
// only, since a compare and merge reads text.
func writeCopy(dir string, file files.File) (string, error) {
	var content []byte
	var err error

	hash := file.Hash
	if hash == "" {
		if content, err = os.ReadFile(file.Path); err != nil {
			return "", err
		}

		hash = files.HashContent(content)
	}

	target := filepath.Join(dir, copiesDir, hash+copyExt)
	if !file.Format.IsText() || files.IsFile(target) {
		return hash, nil
	}

	if content == nil {
		if content, err = os.ReadFile(file.Path); err != nil {
			return "", err
		}
	}

	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, err = writer.Write(content)
	if err = errors.Join(err, writer.Close()); err != nil {
		return "", err
	}

	if err = files.WriteAtomic(target, packed.Bytes()); err != nil {
		return "", err
	}

	return hash, nil
}

// deleteCopies drops from the record each version that no install of the game
// is on and that no mod's range start needs, and deletes each copy that no
// version left in the record names. A mod needs the latest version at or before
// its range start, which is where FindCopy looks. It reads the mods of every
// workspace of the game, since the copies are shared. A copy that cannot be
// deleted goes at a later open.
func (s *Session) deleteCopies(record copyHashes, records app.Config) {
	needed := append(slices.Clone(s.installVersions), s.Schema.Version)
	patches := *s.Patches.Load()
	workshop := s.Game.ListWorkshopRoots()
	for _, workspace := range records.Workspaces {
		install, row, err := findInstall(records, workspace.Install)
		if err != nil || row.ID != s.Game.ID {
			continue
		}

		end := resolveVersion(row, install)
		for _, recorded := range workspace.Mods {
			start := patches.resolveStart(readMod(row, workshop, recorded), end)
			if version, ok := findNearest(record, start); ok {
				needed = append(needed, version)
			}
		}
	}

	maps.DeleteFunc(record, func(version string, _ map[string]string) bool {
		return !slices.Contains(needed, version)
	})

	named := map[string]bool{}
	for _, hashes := range record {
		for _, hash := range hashes {
			named[hash] = true
		}
	}

	folder := filepath.Join(s.patchDir, copiesDir)
	entries, _ := os.ReadDir(folder)
	for _, entry := range entries {
		if !named[strings.TrimSuffix(entry.Name(), copyExt)] {
			_ = os.Remove(filepath.Join(folder, entry.Name()))
		}
	}
}
