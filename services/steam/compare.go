// compare.go compares a listing's next upload with Steam's item.

package steam

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// Snapshot is what PMT keeps of a Workshop item's files as Steam has them.
type Snapshot struct {
	WorkshopID string    `json:"workshopId"`
	UpdatedAt  time.Time `json:"updatedAt"`

	// Dir is the folder Steam keeps the item's files in.
	Dir string `json:"dir"`

	// Files is the SHA-256 of each file, by its Rel path.
	Files map[string]string `json:"files"`
}

// Details is what Steam reports about an item.
type Details struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Visibility  string    `json:"visibility"`
	Owned       bool      `json:"owned"`
	Tags        []string  `json:"tags"`
	Previews    []Preview `json:"previews"`

	// DLC and Dependencies are nil when Steam's library cannot read them.
	DLC []string `json:"dlc"`

	// DLCCount is how many DLC the item requires, which may be more than DLC
	// holds.
	DLCCount int `json:"dlcCount"`

	Dependencies []string `json:"dependencies"`
}

// Difference is one thing the next upload changes on Steam.
type Difference struct {
	// Field is title, description, tags, previews, dlc, dependencies, version
	// or file.
	Field string `json:"field"`

	Rel     string   `json:"rel,omitempty"`
	Status  string   `json:"status"`
	Before  string   `json:"before,omitempty"`
	After   string   `json:"after,omitempty"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`

	// Gained and Lost are lines, or paragraphs for the description.
	Gained int `json:"gained"`
	Lost   int `json:"lost"`
}

// Comparison is what the next upload changes against what Steam has.
type Comparison struct {
	Page []Difference `json:"page"`

	// Files is the version first, then each file by its path.
	Files []Difference `json:"files"`

	Upload    Upload    `json:"upload"`
	CheckedAt time.Time `json:"checkedAt"`
	Steam     *Details  `json:"steam,omitempty"`

	// Reason is why Steam's side is missing, or "".
	Reason string `json:"reason"`

	// Status is new, same, changed, or unknown when Steam could not be asked.
	Status string `json:"status"`
}

const (
	statusNew     = "new"
	statusUnknown = "unknown"
	statusSame    = "same"
	statusChanged = "changed"
)

const (
	changeAdded   = "added"
	changeChanged = "changed"
	changeRemoved = "removed"
)

// CompareUpload compares the next upload of a mod's listing with what Steam
// has.
func (s *Service) CompareUpload(modID, key string) (Comparison, error) {
	ws, mod, listing, err := s.findListing(modID, key)
	if err != nil {
		return Comparison{}, err
	}

	src, err := s.openSource(ws, mod, listing)
	if err != nil {
		return Comparison{}, err
	}
	defer func() { _ = src.close() }()

	upload, exclusions, err := s.buildUpload(mod, src)
	if err != nil {
		return Comparison{}, err
	}

	comparison := Comparison{Page: []Difference{}, Files: []Difference{}, Upload: upload, Status: statusNew}
	if listing.WorkshopID == "" {
		return comparison, nil
	}

	answer := s.resolveAnswer(ws, listing.WorkshopID)
	comparison.Steam, comparison.Reason, comparison.CheckedAt = answer.Steam, answer.Reason, answer.CheckedAt
	if answer.Steam != nil {
		modIDs := slices.Collect(maps.Keys(s.listModTitles()))
		comparison.Page = comparePage(listing, *answer.Steam, modIDs, sync.OnceValue(s.listSteamNames))
	}

	if answer.Reason == "" {
		if comparison.Files, err = s.compareFiles(ws, exclusions, listing, src, upload); err != nil {
			return Comparison{}, err
		}
	}

	comparison.Status = toStatus(comparison)

	return comparison, nil
}

// FetchItem asks Steam again about a listing's Workshop item and returns the
// comparison.
func (s *Service) FetchItem(modID, key string) (Comparison, error) {
	listing, err := s.ReadListing(modID, key)
	if err != nil {
		return Comparison{}, err
	}

	s.removeAnswer(listing.WorkshopID)

	return s.CompareUpload(modID, key)
}

// CompareFile returns one file as Steam has it and as the upload sends it, ""
// for a missing side.
func (s *Service) CompareFile(modID, key, rel string) ([]string, error) {
	ws, mod, listing, err := s.findListing(modID, key)
	if err != nil {
		return nil, err
	}

	src, err := s.openSource(ws, mod, listing)
	if err != nil {
		return nil, err
	}
	defer func() { _ = src.close() }()

	snapshot, err := s.loadSnapshot(ws, listing.WorkshopID)
	if err != nil {
		return nil, err
	}

	steamText, err := files.LoadText(files.Join(snapshot.Dir, rel))
	if err != nil {
		return nil, err
	}

	text, err := src.loadText(rel)
	if err != nil {
		return nil, err
	}

	return []string{steamText.Text, text.Text}, nil
}

// resolveAnswer returns Steam's answer about a Workshop item, kept or fetched.
// Calls for one item share one fetch.
func (s *Service) resolveAnswer(ws *workspace.Session, workshopID string) Comparison {
	s.mu.Lock()
	answer, ok := s.answers[workshopID]
	s.mu.Unlock()
	if ok {
		return answer
	}

	shared, _, _ := s.flight.Do(workshopID, func() (any, error) { return s.fetchAnswer(ws, workshopID), nil })
	answer, _ = shared.(Comparison)

	return answer
}

// fetchAnswer asks Steam about a Workshop item and keeps the answer. A failure
// becomes the answer's reason.
func (s *Service) fetchAnswer(ws *workspace.Session, workshopID string) Comparison {
	answer := Comparison{CheckedAt: time.Now()}
	params, err := s.buildParams(ws)
	if err == nil {
		params.WorkshopID = workshopID

		var details Details
		err = startCopy(context.Background(), detailsFlag, params, func(line string) {
			_ = json.Unmarshal([]byte(line), &details)
		})
		if err == nil {
			answer.Steam = &details
			params.UpdatedAt = details.UpdatedAt
			err = refreshSnapshot(s.Workspace.Config.Dirs.Cache, params)
		}
	}

	if err != nil {
		answer.Reason = err.Error()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.answers == nil {
		s.answers = map[string]Comparison{}
	}
	s.answers[workshopID] = answer

	return answer
}

func (s *Service) removeAnswer(workshopID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.answers, workshopID)
}

func refreshSnapshot(cacheDir string, params Params) error {
	if snapshot, ok := lookupSnapshot(cacheDir, params.WorkshopID); ok && snapshot.UpdatedAt.Equal(params.UpdatedAt) {
		return nil
	}

	return startCopy(context.Background(), downloadFlag, params, nil)
}

// listSteamNames returns the names of the workspace's mods and DLCs by their
// Steam ids.
func (s *Service) listSteamNames() map[string]string {
	names := s.listModTitles()
	dlcs, _ := s.ListDLC()
	for _, dlc := range dlcs {
		names[dlc.ID] = dlc.Name
	}

	return names
}

// comparePage lists what an upload changes on its item's page. An empty
// description changes nothing.
func comparePage(listing Listing, details Details, modIDs []string, names func() map[string]string) []Difference {
	differences := []Difference{}
	if listing.Title != details.Title {
		differences = append(differences, Difference{
			Field: "title", Status: changeChanged, Before: details.Title, After: listing.Title,
		})
	}

	before, after := toPlainText(details.Description), toPlainText(listing.Description)
	if listing.Description != "" && before != after {
		gained, lost := files.CountLines(toParagraphs(before), toParagraphs(after))
		differences = append(differences, Difference{
			Field: "description", Status: changeChanged, Before: before, After: after, Gained: gained, Lost: lost,
		})
	}

	return append(differences, compareLists(listing, details, modIDs, names)...)
}

// compareLists lists what an upload changes in the lists of its item's page,
// leaving out a list Steam's library cannot read.
func compareLists(listing Listing, details Details, modIDs []string, names func() map[string]string) []Difference {
	tagsAdded, tagsRemoved := diffTags(toSentTags(listing), details.Tags)
	labelsAdded, labelsRemoved := diffLabels(listing.Previews, details.Previews)
	dlcAdded, dlcRemoved := diffDLC(listing.DLC, details)
	requiredAdded, requiredRemoved := diffRequired(listing.Dependencies, details.Dependencies, modIDs)

	var dlcNote []string
	if missing := details.DLCCount - len(details.DLC); missing > 0 {
		dlcNote = []string{fmt.Sprintf("and %d more that Steam did not list", missing)}
	}

	// A list is named when its entries are Steam ids.
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

		kept := slices.DeleteFunc(slices.Clone(row.steam), func(entry string) bool { return slices.Contains(row.removed, entry) })
		after := slices.Concat(kept, row.added)
		if row.named {
			row.steam, after = toNames(row.steam, names()), toNames(after, names())
			row.added, row.removed = toNames(row.added, names()), toNames(row.removed, names())
		}

		differences = append(differences, Difference{
			Field: row.field, Status: changeChanged, Before: strings.Join(slices.Concat(row.steam, row.note), "\n"),
			After: strings.Join(slices.Concat(after, row.note), "\n"), Added: row.added, Removed: row.removed,
		})
	}

	return differences
}

func toNames(ids []string, names map[string]string) []string {
	named := make([]string, len(ids))
	for position, id := range ids {
		named[position] = cmp.Or(names[id], id)
	}

	return named
}

func listLabels(previews []Preview) []string {
	labels := make([]string, len(previews))
	for position, preview := range previews {
		labels[position] = toLabel(preview)
	}

	return labels
}

func toPlainText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for position, line := range lines {
		lines[position] = strings.TrimRight(line, " \t")
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

var blankLines = regexp.MustCompile(`\n{2,}`)

// toParagraphs returns a text with each paragraph on one line.
func toParagraphs(text string) string {
	paragraphs := blankLines.Split(text, -1)
	for position, paragraph := range paragraphs {
		paragraphs[position] = strings.ReplaceAll(paragraph, "\n", " ")
	}

	return strings.Join(paragraphs, "\n")
}

// compareFiles lists what an upload changes in its item's files. It returns an
// error if PMT has no snapshot of the item.
func (s *Service) compareFiles(
	ws *workspace.Session, exclusions []exclusion, listing Listing, src source, upload Upload,
) ([]Difference, error) {
	snapshot, err := s.loadSnapshot(ws, listing.WorkshopID)
	if err != nil {
		return nil, err
	}

	maps.DeleteFunc(snapshot.Files, func(rel, _ string) bool {
		_, excluded := findExclusion(exclusions, rel)

		return excluded
	})

	differences := []Difference{}
	if descriptor, _ := workspace.ReadDescriptor(ws.Game, snapshot.Dir); descriptor.Version != listing.Version {
		differences = append(differences, Difference{
			Field: "version", Status: changeChanged, Before: descriptor.Version, After: listing.Version,
		})
	}

	hashes := s.buildUploadHashes(src, upload.Files)
	rels := slices.Concat(slices.Collect(maps.Keys(hashes)), slices.Collect(maps.Keys(snapshot.Files)))
	slices.Sort(rels)
	rels = slices.Compact(rels)

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
			counts := s.countChange(files.Join(snapshot.Dir, rel), last, src, rel, hash)
			difference.Gained, difference.Lost = counts.Gained, counts.Lost
		}

		differences = append(differences, difference)
	}

	return differences, nil
}

// buildUploadHashes returns the SHA-256 of each file of an upload, by its Rel
// path. It hashes only the files that changed since an earlier call.
func (s *Service) buildUploadHashes(src source, list []files.File) map[string]string {
	keys := make([]string, len(list))
	for position, file := range list {
		keys[position] = src.hashKey(file)
	}

	hashes := make(map[string]string, len(list))
	var changed []files.File
	s.mu.Lock()
	for position, file := range list {
		if hash, ok := s.hashes[keys[position]]; ok && keys[position] != "" {
			hashes[file.Rel] = hash
		} else {
			changed = append(changed, file)
		}
	}
	s.mu.Unlock()

	fresh := buildHashes(src, changed)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashes == nil {
		s.hashes = map[string]string{}
	}

	for position, file := range list {
		if hash, ok := fresh[file.Rel]; ok {
			hashes[file.Rel] = hash
			if keys[position] != "" {
				s.hashes[keys[position]] = hash
			}
		}
	}

	return hashes
}

// countChange returns how many lines a file gains and loses from Steam's copy
// to the upload's. A side without the file reads as empty, with the hash "".
func (s *Service) countChange(steamFile, steamHash string, src source, rel, hash string) Difference {
	key := steamHash + " " + hash
	s.mu.Lock()
	counts, ok := s.lines[key]
	s.mu.Unlock()
	if ok {
		return counts
	}

	steamText, _ := files.LoadText(steamFile)

	// A file left out of the upload counts as empty, even while it is still
	// on disk.
	var text files.Text
	if hash != "" {
		text, _ = src.loadText(rel)
	}

	counts.Gained, counts.Lost = files.CountLines(steamText.Text, text.Text)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lines == nil {
		s.lines = map[string]Difference{}
	}
	s.lines[key] = counts

	return counts
}

func toStatus(comparison Comparison) string {
	switch {
	case comparison.Reason != "":
		return statusUnknown
	case len(comparison.Page)+len(comparison.Files) == 0:
		return statusSame
	}

	return statusChanged
}

// loadSnapshot loads an item's snapshot, having Steam download the item again
// if its folder is gone. It returns an error if PMT has no snapshot.
func (s *Service) loadSnapshot(ws *workspace.Session, workshopID string) (Snapshot, error) {
	cacheDir := s.Workspace.Config.Dirs.Cache
	snapshot, ok := lookupSnapshot(cacheDir, workshopID)
	if !ok {
		return Snapshot{}, errors.New("PMT has not read the item's files from Steam: check again to download them")
	}

	if files.IsFolder(snapshot.Dir) {
		return snapshot, nil
	}

	params, err := s.buildParams(ws)
	if err != nil {
		return Snapshot{}, err
	}

	params.WorkshopID, params.UpdatedAt = workshopID, snapshot.UpdatedAt
	if err := startCopy(context.Background(), downloadFlag, params, nil); err != nil {
		return Snapshot{}, err
	}

	snapshot, _ = lookupSnapshot(cacheDir, workshopID)

	return snapshot, nil
}

// lookupSnapshot returns the snapshot saved for an item, or false if there is
// none or the id is empty.
func lookupSnapshot(cacheDir, workshopID string) (Snapshot, bool) {
	snapshot, err := app.LoadJSON[Snapshot](resolveSnapshotPath(cacheDir, workshopID))

	return snapshot, err == nil && workshopID != "" && snapshot.WorkshopID == workshopID
}

func resolveSnapshotPath(cacheDir, workshopID string) string {
	return filepath.Join(cacheDir, snapshotPrefix+workshopID+".json")
}

// buildHashes returns the SHA-256 of each file of a source, by its Rel path. A
// file that cannot be read has the hash "".
func buildHashes(src source, list []files.File) map[string]string {
	hashes := make([]string, len(list))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, file := range list {
		group.Go(func() error {
			handle, err := src.open(file.Rel)
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
