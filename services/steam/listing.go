// Package steam publishes mods to the Steam Workshop. It keeps a mod's listing in the mod's own files and compares an
// upload with Steam. It uploads from a second copy of the app, which loads the game's own copy of Steam's library.
package steam

import (
	"archive/zip"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Preview is one picture or video in the Workshop page's gallery.
type Preview struct {
	Path  string `json:"path"`
	Type  string `json:"type"`  // image, video
	URL   string `json:"url"`   // a video's
	Video string `json:"video"` // a video's id
	Order int    `json:"order"`
}

// A Listing is what a mod's Workshop page shows, as the mod's own files hold it.
type Listing struct {
	WorkshopID       string    `json:"workshopId"`
	Title            string    `json:"title"`
	Version          string    `json:"version"`
	SupportedVersion string    `json:"supportedVersion"`
	Description      string    `json:"description"`
	BBCode           string    `json:"bbcode"` // the description as Steam and Paradox Mods take it
	Source           string    `json:"source"` // markdown, bbcode, steam
	DescriptionPath  string    `json:"descriptionPath"`
	Changelog        string    `json:"changelog"`
	ChangelogPath    string    `json:"changelogPath"`
	Thumbnail        string    `json:"thumbnail"`
	Previews         []Preview `json:"previews"`
	Tags             []string  `json:"tags"`
	DLC              []string  `json:"dlc"`
	Visibility       string    `json:"visibility"` // private, friends, public, unlisted; chosen at each upload
}

// An Upload is the set of files a mod's next upload sends.
type Upload struct {
	Files    []files.File `json:"files"`
	Bytes    int64        `json:"bytes"`    // the total size of the files
	Excluded []string     `json:"excluded"` // the rels the rules left out
}

// A Snapshot is the record of what the last upload of an item sent. The next compare reads it to see what changed.
type Snapshot struct {
	WorkshopID string            `json:"workshopId"`
	UploadedAt time.Time         `json:"uploadedAt"`
	Files      map[string]string `json:"files"` // the hash of each file, by rel
	Listing    Listing           `json:"listing"`
	Dir        string            `json:"dir"`        // the folder holding the files: a copy of the upload, or Steam's own
	Downloaded bool              `json:"downloaded"` // the files are Steam's copy of the item rather than an upload's
}

// Details is what Steam reports about an item.
type Details struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Visibility  string    `json:"visibility"`
	Tags        []string  `json:"tags"`
	Subscribers int       `json:"subscribers"`
}

// A Comparison is what the next upload changes against the last one, next to what Steam reports about the item.
type Comparison struct {
	Added      []string  `json:"added"`   // the rels of the files the last upload did not have
	Changed    []string  `json:"changed"` // the rels of the files that differ from the last upload's
	Removed    []string  `json:"removed"` // the rels of the files the last upload had and the next one lacks
	Listing    []string  `json:"listing"` // the listing's fields that differ from the last upload's: title, description …
	Last       Listing   `json:"last"`    // the last upload's listing
	UploadedAt time.Time `json:"uploadedAt"`
	Downloaded bool      `json:"downloaded"` // the last upload's files are Steam's copy, downloaded
	Steam      *Details  `json:"steam,omitempty"`
}

// The sources a description may be kept in, and the types of preview.
const (
	sourceMarkdown = "markdown"
	sourceBBCode   = "bbcode"
	sourceSteam    = "steam"
	previewImage   = "image"
	previewVideo   = "video"
)

// The files a mod keeps for its Workshop page, from the mod's folder, and the file that names what the mod's
// repository leaves out.
const (
	workshopDir   = "workshop"
	previewsDir   = workshopDir + "/previews"
	videosFile    = previewsDir + "/videos.txt"
	changelogFile = workshopDir + "/changelog.bbcode"
	dlcFile       = workshopDir + "/dlc.txt"
	gitignoreFile = ".gitignore"
	ignoreFile    = ".workshop-ignore"
)

// maxPreviews is the most pictures and videos Steam takes in an item's gallery.
const maxPreviews = 10

// previewTypes are the types a preview may have.
var previewTypes = []string{previewImage, previewVideo}

// contentExtensions are the extensions of a gallery's pictures by their content type.
var contentExtensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// descriptionFiles are the files a description may be kept in, in the order they are tried.
var descriptionFiles = []struct{ source, rel string }{
	{sourceMarkdown, workshopDir + "/description.md"},
	{sourceBBCode, workshopDir + "/description.bbcode"},
}

// ReadListing reads a mod's listing from its descriptor and its workshop folder. The descriptor is read from the
// mod's folder, so the listing is as the last write left it. It returns an error if the workspace has no such mod or a
// file cannot be read.
func (stm *Service) ReadListing(modID string) (Listing, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Listing{}, err
	}
	descriptor, err := workspace.ReadDescriptor(ws.Game, mod.Dir)
	if err != nil {
		return Listing{}, err
	}
	listing := Listing{
		WorkshopID:       cmp.Or(mod.WorkshopID, descriptor.WorkshopID),
		Title:            descriptor.Name,
		Version:          descriptor.Version,
		SupportedVersion: descriptor.SupportedVersion,
		Thumbnail:        cmp.Or(descriptor.Thumbnail, mod.Thumbnail),
		Source:           sourceSteam,
		Tags:             append([]string{}, descriptor.Tags...),
	}
	for _, row := range descriptionFiles {
		file := files.Join(mod.Dir, row.rel)
		text, err := files.ReadText(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Listing{}, err
		}
		listing.Source, listing.Description, listing.DescriptionPath = row.source, text.Text, file
		break
	}
	// Steam and Paradox Mods take BBCode, so a Markdown description is converted and any other is sent as written
	listing.BBCode = listing.Description
	if listing.Source == sourceMarkdown {
		listing.BBCode = stm.ToBBCode(listing.Description)
	}
	changelog := files.Join(mod.Dir, changelogFile)
	text, err := files.ReadText(changelog)
	if err == nil {
		listing.Changelog, listing.ChangelogPath = text.Text, changelog
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Listing{}, err
	}
	dlc, err := files.ReadText(files.Join(mod.Dir, dlcFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Listing{}, err
	}
	listing.DLC = strings.Fields(dlc.Text)
	listing.Previews, err = listPreviews(mod.Dir)
	return listing, err
}

// listPreviews returns the gallery of a mod: its pictures in the order of their names, then its videos in the
// order of the list that names them.
func listPreviews(dir string) ([]Preview, error) {
	entries, err := os.ReadDir(files.Join(dir, previewsDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	previews := []Preview{}
	for _, entry := range entries {
		rel := path.Join(previewsDir, entry.Name())
		if !entry.IsDir() && rel != videosFile {
			previews = append(previews, Preview{Path: files.Join(dir, rel), Type: previewImage})
		}
	}
	urls, err := listVideos(dir)
	for _, url := range urls {
		previews = append(previews, Preview{Type: previewVideo, URL: url, Video: toVideoID(url)})
	}
	for position := range previews {
		previews[position].Order = position
	}
	return previews, err
}

// listVideos returns the URLs of a mod's gallery videos, one per line of its list. A mod with no list has none.
func listVideos(dir string) ([]string, error) {
	text, err := files.ReadText(files.Join(dir, videosFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return strings.Fields(text.Text), nil
}

// WriteListing writes a mod's listing back to its files: the descriptor's fields and the workshop folder's. It writes
// only the files whose text differs from the current listing's.
func (stm *Service) WriteListing(modID string, listing Listing) error {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	current, err := stm.ReadListing(modID)
	if err != nil {
		return err
	}
	// a thumbnail picked from elsewhere is copied into the folder the game keeps it in
	if listing.Thumbnail != "" && !files.IsUnder(mod.Dir, listing.Thumbnail) {
		content, err := os.ReadFile(listing.Thumbnail)
		if err != nil {
			return err
		}
		thumbnail := files.Join(mod.Dir, path.Join(cmp.Or(ws.Game.ThumbnailDir, workshopDir), filepath.Base(listing.Thumbnail)))
		if err := stm.writeFile(thumbnail, string(content)); err != nil {
			return err
		}
		listing.Thumbnail = thumbnail
	}
	if err := writeDescriptor(ws.Game, mod, current, listing); err != nil {
		return err
	}
	texts := []struct {
		file, text string
		differs    bool
	}{
		{current.DescriptionPath, listing.Description, listing.Description != current.Description},
		{files.Join(mod.Dir, changelogFile), listing.Changelog, listing.Changelog != current.Changelog},
		{files.Join(mod.Dir, dlcFile), strings.Join(listing.DLC, "\n") + "\n", !slices.Equal(listing.DLC, current.DLC)},
	}
	for _, row := range texts {
		if !row.differs || row.file == "" {
			continue
		}
		if err := stm.writeFile(row.file, row.text); err != nil {
			return err
		}
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// writeDescriptor writes the descriptor's fields of a listing if any of them differs from the current listing's. It
// reads the descriptor again first, so the fields the listing does not hold stay as they are. It returns an error if
// the descriptor cannot be read or written.
func writeDescriptor(row *game.Game, mod workspace.Mod, current, listing Listing) error {
	if listing.Title == current.Title && listing.Version == current.Version &&
		listing.SupportedVersion == current.SupportedVersion && slices.Equal(listing.Tags, current.Tags) &&
		listing.Thumbnail == current.Thumbnail && listing.WorkshopID == current.WorkshopID {
		return nil
	}
	descriptor, err := workspace.ReadDescriptor(row, mod.Dir)
	if err != nil {
		return err
	}
	descriptor.Name, descriptor.Tags, descriptor.WorkshopID = listing.Title, listing.Tags, listing.WorkshopID
	descriptor.Version, descriptor.SupportedVersion, descriptor.Thumbnail =
		listing.Version, listing.SupportedVersion, listing.Thumbnail
	return workspace.WriteDescriptor(row, mod.Dir, descriptor)
}

// writeFile writes a text into a file inside a mod's folder in UTF-8. The write goes through the workspace, which
// refuses a path outside the mods.
func (stm *Service) writeFile(file, text string) error {
	return stm.Workspace.Write(file, files.Text{Text: text, Encoding: files.EncodingUTF8})
}

// ListUpload returns the files a mod's next upload sends: the mod's files except those the rules leave out and those
// that the mod's .gitignore and its own exclusions cover. The rules keep the .metadata folder, which holds the
// descriptor of Vic3 and EU5, though it starts with a dot.
func (stm *Service) ListUpload(modID string) (Upload, error) {
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return Upload{}, err
	}
	walked, err := files.Walk(files.Root{Dir: mod.Dir, Origin: mod.ID})
	if err != nil {
		return Upload{}, err
	}
	gitignore, err := files.ReadText(files.Join(mod.Dir, gitignoreFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Upload{}, err
	}
	patterns := append(lo.Map(Rules, func(rule Rule, _ int) string { return rule.Pattern }), "!"+game.MetadataDir+"/")
	ignores := []files.Ignore{
		files.ParseIgnore(strings.Join(patterns, "\n")), files.ParseIgnore(gitignore.Text), files.ParseIgnore(mod.Ignore),
	}
	upload := Upload{Files: []files.File{}, Excluded: []string{}}
	for _, file := range walked {
		if slices.ContainsFunc(ignores, func(ignore files.Ignore) bool { return ignore.Match(file.Rel, false) }) {
			upload.Excluded = append(upload.Excluded, file.Rel)
			continue
		}
		stat, err := os.Stat(file.Path)
		if err != nil {
			return Upload{}, err
		}
		upload.Files = append(upload.Files, file)
		upload.Bytes += stat.Size()
	}
	return upload, nil
}

// WriteZip writes the files of a mod's next upload as a zip at the given path. A zip it fails to write is removed.
func (stm *Service) WriteZip(modID, path string) error {
	upload, err := stm.ListUpload(modID)
	if err != nil {
		return err
	}
	zipFile, err := os.Create(path)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(zipFile)
	for _, file := range upload.Files {
		var content []byte
		var entry io.Writer
		if content, err = os.ReadFile(file.Path); err == nil {
			if entry, err = archive.Create(file.Rel); err == nil {
				_, err = entry.Write(content)
			}
		}
		if err != nil {
			break
		}
	}
	if err = errors.Join(err, archive.Close(), zipFile.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

// WriteIgnore writes a mod's upload exclusions to the .workshop-ignore file in its folder.
func (stm *Service) WriteIgnore(modID string) error {
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	return stm.writeFile(files.Join(mod.Dir, ignoreFile), mod.Ignore)
}

// ReadIgnore returns the text of the .workshop-ignore file in a mod's folder, or "" if there is none.
func (stm *Service) ReadIgnore(modID string) (string, error) {
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return "", err
	}
	text, err := files.ReadText(files.Join(mod.Dir, ignoreFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return text.Text, err
}

// LoadSnapshot loads the snapshot saved for a Workshop item. It returns false if there is none, or if the item has no
// id.
func LoadSnapshot(cacheDir, workshopID string) (Snapshot, bool) {
	snapshot, err := app.LoadJSON[Snapshot](resolveSnapshotPath(cacheDir, workshopID))
	return snapshot, err == nil && workshopID != "" && snapshot.WorkshopID == workshopID
}

// SaveSnapshot saves the snapshot of an upload under the id of its Workshop item.
func SaveSnapshot(cacheDir string, snapshot Snapshot) error {
	return app.SaveJSON(resolveSnapshotPath(cacheDir, snapshot.WorkshopID), snapshot)
}

// resolveSnapshotPath returns the path of the file that keeps the snapshot of a Workshop item.
func resolveSnapshotPath(cacheDir, workshopID string) string {
	return filepath.Join(cacheDir, "snapshot-"+workshopID+".json")
}

// resolveSnapshotDir returns the folder that keeps a copy of the files the last upload of a Workshop item sent.
func resolveSnapshotDir(cacheDir, workshopID string) string {
	return filepath.Join(cacheDir, "snapshot-"+workshopID)
}

// Compare lists what an upload and a listing change against the snapshot of the last upload: the files added, changed
// and removed, and the listing fields that differ. It carries the given Steam details beside them, nil if there are
// none.
func Compare(listing Listing, upload Upload, snapshot Snapshot, details *Details) Comparison {
	hashes := buildHashes(upload.Files)
	rels := slices.Sorted(maps.Keys(hashes))
	return Comparison{
		Added: lo.Filter(rels, func(rel string, _ int) bool {
			_, ok := snapshot.Files[rel]
			return !ok
		}),
		Changed: lo.Filter(rels, func(rel string, _ int) bool {
			last, ok := snapshot.Files[rel]
			return ok && last != hashes[rel]
		}),
		Removed: lo.Filter(slices.Sorted(maps.Keys(snapshot.Files)), func(rel string, _ int) bool {
			_, ok := hashes[rel]
			return !ok
		}),
		Listing:    compareListing(listing, snapshot.Listing),
		Last:       snapshot.Listing,
		UploadedAt: snapshot.UploadedAt,
		Downloaded: snapshot.Downloaded,
		Steam:      details,
	}
}

// compareListing returns the names of the fields in which a listing differs from the last one sent, in the order
// the fields are shown.
func compareListing(listing, last Listing) []string {
	fields := []struct {
		field   string
		differs bool
	}{
		{"title", listing.Title != last.Title},
		{"description", listing.Description != last.Description},
		{"changelog", listing.Changelog != last.Changelog},
		{"tags", !slices.Equal(listing.Tags, last.Tags)},
		{"dlc", !slices.Equal(listing.DLC, last.DLC)},
		{"previews", !slices.Equal(listing.Previews, last.Previews)},
		{"visibility", listing.Visibility != last.Visibility},
	}
	differing := []string{}
	for _, row := range fields {
		if row.differs {
			differing = append(differing, row.field)
		}
	}
	return differing
}

// buildHashes returns the SHA-256 of each of the given files by its rel. A file that cannot be
// read has the hash "".
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
