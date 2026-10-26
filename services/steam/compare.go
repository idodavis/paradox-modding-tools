// compare.go compares a listing's next upload with what Steam has of its item, the page and the files.

package steam

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samber/lo"
	"github.com/sergi/go-diff/diffmatchpatch"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Snapshot is PMT's kept copy of a Workshop item's files as Steam has them.
type Snapshot struct {
	WorkshopID string            `json:"workshopId"`
	UpdatedAt  time.Time         `json:"updatedAt"` // when Steam last updated the item, as of the download
	Files      map[string]string `json:"files"`     // the hash of each file, by its path
}

// Details is what Steam reports about an item.
type Details struct {
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Visibility   string    `json:"visibility"`
	Tags         []string  `json:"tags"`
	Subscribers  int       `json:"subscribers"`
	Previews     []Preview `json:"previews"`
	DLC          []string  `json:"dlc"`          // nil when Steam's library cannot read them
	DLCCount     int       `json:"dlcCount"`     // how many DLC it requires, which may be more than DLC holds
	Dependencies []string  `json:"dependencies"` // the Workshop ids of the items it requires, or nil as for DLC
}

// A Difference is one thing the next upload changes on Steam.
type Difference struct {
	Field   string   `json:"field"` // title, description, tags, previews, dlc, dependencies, version or file
	Rel     string   `json:"rel,omitempty"`
	Status  string   `json:"status"` // added, changed or removed
	Before  string   `json:"before,omitempty"`
	After   string   `json:"after,omitempty"`
	Added   []string `json:"added,omitempty"`   // the tags, pictures, DLC or items the upload adds
	Removed []string `json:"removed,omitempty"` // those it takes away
	Gained  int      `json:"gained"`            // the lines the upload adds, or paragraphs for the description
	Lost    int      `json:"lost"`
}

// A Comparison is what the next upload changes against what Steam has of the item.
type Comparison struct {
	Page      []Difference `json:"page"`
	Files     []Difference `json:"files"`     // the version first, then each file by its path
	CheckedAt time.Time    `json:"checkedAt"` // when Steam was last asked for the item
	Steam     *Details     `json:"steam,omitempty"`
	Reason    string       `json:"reason"` // why Steam's side is missing, or ""
	Status    string       `json:"status"` // new, same, changed, or unknown when Steam could not be asked
}

// The states of a mod's next upload against its Workshop item.
const (
	statusNew     = "new"     // the listing has no item yet
	statusUnknown = "unknown" // Steam could not be asked for the item or its files
	statusSame    = "same"    // the next upload sends what Steam has
	statusChanged = "changed" // the next upload changes files or the page
)

// What a difference does to Steam's side.
const (
	changeAdded   = "added"
	changeChanged = "changed"
	changeRemoved = "removed"
)

// blankLines matches the blank lines between two paragraphs of a normalized text.
var blankLines = regexp.MustCompile(`\n{2,}`)

// CompareUpload compares the next upload of a mod's listing with what Steam has. It returns an error if there is no
// such mod or listing, or a file cannot be read.
func (stm *Service) CompareUpload(modID, key string) (Comparison, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Comparison{}, err
	}
	listing, err := stm.ReadListing(modID, key)
	if err != nil {
		return Comparison{}, err
	}
	upload, err := stm.buildUpload(ws, mod, listing)
	if err != nil {
		return Comparison{}, err
	}
	comparison := Comparison{Page: []Difference{}, Files: []Difference{}, Status: statusNew}
	if listing.WorkshopID == "" {
		return comparison, nil
	}
	answer := stm.resolveAnswer(ws, listing.WorkshopID)
	comparison.Steam, comparison.Reason, comparison.CheckedAt = answer.Steam, answer.Reason, answer.CheckedAt
	if answer.Steam != nil {
		comparison.Page = comparePage(listing, *answer.Steam, stm.listModWorkshopIDs(), stm.listSteamNames())
	}
	if answer.Reason == "" {
		if comparison.Files, err = stm.compareFiles(ws.Game, mod, listing, upload); err != nil {
			return Comparison{}, err
		}
	}
	comparison.Status = toStatus(comparison)
	return comparison, nil
}

// FetchItem asks Steam again about the Workshop item of one of a mod's listings and returns the comparison. It returns
// an error as CompareUpload does.
func (stm *Service) FetchItem(modID, key string) (Comparison, error) {
	listing, err := stm.ReadListing(modID, key)
	if err != nil {
		return Comparison{}, err
	}
	stm.removeAnswer(listing.WorkshopID)
	return stm.CompareUpload(modID, key)
}

// resolveAnswer returns Steam's answer about a Workshop item, kept or fetched. Calls for one item while Steam is asked
// wait for the same answer.
func (stm *Service) resolveAnswer(ws *workspace.Session, workshopID string) Comparison {
	stm.mu.Lock()
	answer, ok := stm.answers[workshopID]
	stm.mu.Unlock()
	if ok {
		return answer
	}
	shared, _, _ := stm.flight.Do(workshopID, func() (any, error) { return stm.fetchAnswer(ws, workshopID), nil })
	return shared.(Comparison)
}

// fetchAnswer asks Steam about a Workshop item, keeps the answer and returns it. A failure is kept as the answer's
// reason.
func (stm *Service) fetchAnswer(ws *workspace.Session, workshopID string) Comparison {
	answer := Comparison{CheckedAt: time.Now()}
	params, err := stm.buildParams(ws)
	if err == nil {
		params.WorkshopID = workshopID
		var details Details
		if details, err = fetchDetails(params); err == nil {
			answer.Steam = &details
			err = refreshSnapshot(stm.Workspace.Config.Dirs.Cache, params, details)
		}
	}
	if err != nil {
		answer.Reason = err.Error()
	}
	stm.mu.Lock()
	defer stm.mu.Unlock()
	if stm.answers == nil {
		stm.answers = map[string]Comparison{}
	}
	stm.answers[workshopID] = answer
	return answer
}

// removeAnswer drops the answer kept for a Workshop item, so the next comparison asks Steam again.
func (stm *Service) removeAnswer(workshopID string) {
	stm.mu.Lock()
	defer stm.mu.Unlock()
	delete(stm.answers, workshopID)
}

// refreshSnapshot downloads an item's files into PMT's kept copy, unless that copy is up to date. It returns an error
// if the download fails.
func refreshSnapshot(cacheDir string, params Params, details Details) error {
	if snapshot, ok := LoadSnapshot(cacheDir, params.WorkshopID); ok && snapshot.UpdatedAt.Equal(details.UpdatedAt) {
		return nil
	}
	return startCopy(context.Background(), downloadFlag, params, nil)
}

// listSteamNames returns the names of the open workspace's mods and DLCs by their Steam ids. It leaves out the DLCs if
// a descriptor cannot be read.
func (stm *Service) listSteamNames() map[string]string {
	names := map[string]string{}
	for _, mod := range stm.Workspace.State().Mods {
		if workshopID := mod.ResolveWorkshopID(); workshopID != "" {
			names[workshopID] = mod.Descriptor.Name
		}
	}
	dlcs, _ := stm.ListDLC()
	for _, dlc := range dlcs {
		names[dlc.ID] = dlc.Name
	}
	return names
}

// comparePage lists what an upload of a listing changes on its item's page. An empty description changes nothing. The
// description's counts are of paragraphs.
func comparePage(listing Listing, details Details, modIDs []string, names map[string]string) []Difference {
	differences := []Difference{}
	if listing.Title != details.Title {
		differences = append(differences, Difference{
			Field: "title", Status: changeChanged, Before: details.Title, After: listing.Title,
		})
	}
	before, after := toPlainText(details.Description), toPlainText(listing.Description)
	if listing.Description != "" && before != after {
		gained, lost := countLines(toParagraphs(before), toParagraphs(after))
		differences = append(differences, Difference{
			Field: "description", Status: changeChanged, Before: before, After: after, Gained: gained, Lost: lost,
		})
	}
	return append(differences, compareLists(listing, details, modIDs, names)...)
}

// compareLists lists what an upload of a listing changes in the lists of its item's page. Each difference holds Steam's
// list and the list after the upload, one entry a line. A list that Steam's library cannot read is left out.
func compareLists(listing Listing, details Details, modIDs []string, names map[string]string) []Difference {
	tagsAdded, tagsRemoved := diffTags(toSentTags(listing), details.Tags)
	labelsAdded, labelsRemoved := diffLabels(listing.Previews, details.Previews)
	dlcAdded, dlcRemoved := diffDLC(listing.DLC, details)
	requiredAdded, requiredRemoved := diffRequired(listing.Dependencies, details.Dependencies, modIDs)
	var dlcNote []string
	if missing := details.DLCCount - len(details.DLC); missing > 0 {
		dlcNote = []string{fmt.Sprintf("and %d more that Steam did not list", missing)}
	}
	// per list: Steam's entries, the changes, a closing note, and whether Steam's side is known and holds Steam ids
	lists := []struct {
		field                       string
		steam, added, removed, note []string
		known, named                bool
	}{
		{"tags", details.Tags, tagsAdded, tagsRemoved, nil, true, false},
		{"previews", listLabels(details.Previews), labelsAdded, labelsRemoved, nil, true, false},
		{"dlc", details.DLC, dlcAdded, dlcRemoved, dlcNote, details.DLC != nil, true},
		{"dependencies", details.Dependencies, requiredAdded, requiredRemoved, nil, details.Dependencies != nil, true},
	}
	differences := []Difference{}
	for _, row := range lists {
		if !row.known || len(row.added)+len(row.removed) == 0 {
			continue
		}
		after := slices.Concat(lo.Without(row.steam, row.removed...), row.added)
		if row.named {
			row.steam, after = toNames(row.steam, names), toNames(after, names)
			row.added, row.removed = toNames(row.added, names), toNames(row.removed, names)
		}
		differences = append(differences, Difference{
			Field: row.field, Status: changeChanged, Before: strings.Join(slices.Concat(row.steam, row.note), "\n"),
			After: strings.Join(slices.Concat(after, row.note), "\n"), Added: row.added, Removed: row.removed,
		})
	}
	return differences
}

// toNames returns the name of each Steam id, or the id itself if it has none.
func toNames(ids []string, names map[string]string) []string {
	return lo.Map(ids, func(id string, _ int) string { return cmp.Or(names[id], id) })
}

// listLabels returns the label of each preview of a gallery, in its order.
func listLabels(previews []Preview) []string {
	return lo.Map(previews, func(preview Preview, _ int) string { return toLabel(preview) })
}

// listRequired returns the Workshop ids of a listing's dependencies that have one.
func listRequired(dependencies []Dependency) []string {
	return lo.FilterMap(dependencies, func(dependency Dependency, _ int) (string, bool) {
		return dependency.WorkshopID, dependency.WorkshopID != ""
	})
}

// toPlainText returns a text without Windows line ends, trailing spaces or trailing blank lines, so that two copies of
// it compare equal.
func toPlainText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for position, line := range lines {
		lines[position] = strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// toParagraphs returns a text with each paragraph on one line.
func toParagraphs(text string) string {
	paragraphs := blankLines.Split(text, -1)
	for position, paragraph := range paragraphs {
		paragraphs[position] = strings.ReplaceAll(paragraph, "\n", " ")
	}
	return strings.Join(paragraphs, "\n")
}

// countLines returns how many lines a change from one text to another adds and how many it removes.
func countLines(before, after string) (gained, lost int) {
	// go-diff's line mode turns each line into one rune, so a piece's runes count its lines
	diff := diffmatchpatch.New()
	first, second, _ := diff.DiffLinesToRunes(before, after)
	for _, piece := range diff.DiffMainRunes(first, second, false) {
		switch piece.Type {
		case diffmatchpatch.DiffInsert:
			gained += utf8.RuneCountInString(piece.Text)
		case diffmatchpatch.DiffDelete:
			lost += utf8.RuneCountInString(piece.Text)
		}
	}
	return gained, lost
}

// compareFiles lists what an upload changes in its item's files. It returns an error if there is no kept copy or the
// .gitignore cannot be read.
func (stm *Service) compareFiles(
	row *game.Game, mod workspace.Mod, listing Listing, upload Upload,
) ([]Difference, error) {
	cacheDir := stm.Workspace.Config.Dirs.Cache
	snapshot, ok := LoadSnapshot(cacheDir, listing.WorkshopID)
	if !ok {
		return nil, errors.New("PMT has no copy of the item's files: check again to download them")
	}
	ignores, err := buildIgnores(mod)
	if err != nil {
		return nil, err
	}
	maps.DeleteFunc(snapshot.Files, func(rel, _ string) bool { return isExcluded(ignores, rel) })
	snapshotDir := resolveSnapshotDir(cacheDir, listing.WorkshopID)
	differences := []Difference{}
	// a kept copy with no descriptor has no version
	if descriptor, _ := workspace.ReadDescriptor(row, snapshotDir); descriptor.Version != listing.Version {
		differences = append(differences, Difference{
			Field: "version", Status: changeChanged, Before: descriptor.Version, After: listing.Version,
		})
	}
	sent := lo.SliceToMap(upload.Files, func(file files.File) (string, string) { return file.Rel, file.Path })
	hashes := stm.buildUploadHashes(upload.Files)
	rels := lo.Uniq(slices.Concat(slices.Collect(maps.Keys(hashes)), slices.Collect(maps.Keys(snapshot.Files))))
	slices.Sort(rels)
	for _, rel := range rels {
		hash, ours := hashes[rel]
		last, theirs := snapshot.Files[rel]
		difference := Difference{Field: "file", Rel: rel}
		switch {
		case !theirs:
			difference.Status = changeAdded
		case !ours:
			difference.Status = changeRemoved
		case hash != last:
			difference.Status = changeChanged
		default:
			continue
		}
		if files.Classify(rel).IsText() {
			counts := stm.countChange(lo.Ternary(theirs, files.Join(snapshotDir, rel), ""), last, sent[rel], hash)
			difference.Gained, difference.Lost = counts.Gained, counts.Lost
		}
		differences = append(differences, difference)
	}
	return differences, nil
}

// buildUploadHashes returns the SHA-256 of each file of an upload by its path. Only the files that changed since the
// last comparison are hashed again.
func (stm *Service) buildUploadHashes(list []files.File) map[string]string {
	keys := make([]string, len(list))
	for position, file := range list {
		if stat, err := os.Stat(file.Path); err == nil {
			keys[position] = fmt.Sprintf("%s %d %d", file.Path, stat.Size(), stat.ModTime().UnixNano())
		}
	}
	hashes := make(map[string]string, len(list))
	var changed []files.File
	stm.mu.Lock()
	for position, file := range list {
		if hash, ok := stm.hashes[keys[position]]; ok && keys[position] != "" {
			hashes[file.Rel] = hash
		} else {
			changed = append(changed, file)
		}
	}
	stm.mu.Unlock()
	fresh := buildHashes(changed)
	stm.mu.Lock()
	defer stm.mu.Unlock()
	if stm.hashes == nil {
		stm.hashes = map[string]string{}
	}
	for position, file := range list {
		if hash, ok := fresh[file.Rel]; ok {
			hashes[file.Rel] = hash
			stm.hashes[keys[position]] = hash
		}
	}
	return hashes
}

// countChange returns how many lines a file gains and loses from Steam's copy to the upload's. A side without the file
// has "" for its path and hash. The counts are kept for each pair of hashes.
func (stm *Service) countChange(steamFile, steamHash, file, hash string) Difference {
	key := steamHash + " " + hash
	stm.mu.Lock()
	counts, ok := stm.lines[key]
	stm.mu.Unlock()
	if ok {
		return counts
	}
	steamText, _ := files.LoadText(steamFile)
	text, _ := files.LoadText(file)
	counts.Gained, counts.Lost = countLines(steamText.Text, text.Text)
	stm.mu.Lock()
	defer stm.mu.Unlock()
	if stm.lines == nil {
		stm.lines = map[string]Difference{}
	}
	stm.lines[key] = counts
	return counts
}

// toStatus returns whether a comparison is unknown, the same or changed.
func toStatus(comparison Comparison) string {
	switch {
	case comparison.Reason != "":
		return statusUnknown
	case len(comparison.Page)+len(comparison.Files) == 0:
		return statusSame
	}
	return statusChanged
}

// CompareFile returns one file as Steam has it and as the upload sends it, "" for a missing side. It returns an error
// if there is no such mod or listing, or a file cannot be read.
func (stm *Service) CompareFile(modID, key, rel string) ([]string, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return nil, err
	}
	listing, err := stm.ReadListing(modID, key)
	if err != nil {
		return nil, err
	}
	dir, err := stm.resolveSource(ws, mod, listing)
	if err != nil {
		return nil, err
	}
	snapshotDir := resolveSnapshotDir(stm.Workspace.Config.Dirs.Cache, listing.WorkshopID)
	sides := make([]string, 2)
	for position, folder := range []string{snapshotDir, dir} {
		text, err := files.LoadText(files.Join(folder, rel))
		if err != nil {
			return nil, err
		}
		sides[position] = text.Text
	}
	return sides, nil
}

// LoadSnapshot loads the snapshot saved for a Workshop item. It returns false if there is none or the id is empty.
func LoadSnapshot(cacheDir, workshopID string) (Snapshot, bool) {
	snapshot, err := app.LoadJSON[Snapshot](resolveSnapshotPath(cacheDir, workshopID))
	return snapshot, err == nil && workshopID != "" && snapshot.WorkshopID == workshopID
}

// SaveSnapshot saves a snapshot under the id of its Workshop item.
func SaveSnapshot(cacheDir string, snapshot Snapshot) error {
	return app.SaveJSON(resolveSnapshotPath(cacheDir, snapshot.WorkshopID), snapshot)
}

// resolveSnapshotPath returns the path of the file that keeps the snapshot of a Workshop item.
func resolveSnapshotPath(cacheDir, workshopID string) string {
	return filepath.Join(cacheDir, "snapshot-"+workshopID+".json")
}

// resolveSnapshotDir returns the folder that keeps PMT's copy of a Workshop item's files.
func resolveSnapshotDir(cacheDir, workshopID string) string {
	return filepath.Join(cacheDir, "snapshot-"+workshopID)
}

// buildHashes returns the SHA-256 of each file by its path. A file that cannot be read has the hash "".
func buildHashes(list []files.File) map[string]string {
	hashes := make([]string, len(list))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, file := range list {
		group.Go(func() error {
			handle, err := os.Open(file.Path)
			if err != nil {
				return nil
			}
			digest := sha256.New()
			_, err = io.Copy(digest, handle)
			_ = handle.Close()
			if err == nil {
				hashes[position] = hex.EncodeToString(digest.Sum(nil))
			}
			return nil
		})
	}
	_ = group.Wait()
	byRel := make(map[string]string, len(list))
	for position, file := range list {
		byRel[file.Rel] = hashes[position]
	}
	return byRel
}
