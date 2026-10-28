// Package steam publishes mods to the Steam Workshop. It keeps each mod's listings in the mod's own files and compares
// them with Steam.
package steam

import (
	"archive/zip"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Preview is one picture or video in the Workshop page's gallery.
type Preview struct {
	Path  string `json:"path"`
	Type  string `json:"type"`  // image or video
	URL   string `json:"url"`   // the address of a video
	Video string `json:"video"` // the id of a video
	Bytes int64  `json:"bytes"` // the size of a picture, up to 1 MB on Steam
}

// A Dependency is one mod that a listing's mod depends on.
type Dependency struct {
	Name       string `json:"name"`       // as the descriptor names it
	WorkshopID string `json:"workshopId"` // the Workshop id of the workspace mod it names, or ""
}

// A Listing is one of a mod's Workshop pages, as the mod's own files hold it.
type Listing struct {
	Key              string       `json:"key"` // "" for the mod's own listing, else the game version of an older one
	WorkshopID       string       `json:"workshopId"`
	Title            string       `json:"title"`
	Version          string       `json:"version"`
	SupportedVersion string       `json:"supportedVersion"`
	Description      string       `json:"description"` // BBCode
	Changelog        string       `json:"changelog"`   // BBCode, the next upload's change note
	Thumbnail        string       `json:"thumbnail"`
	Previews         []Preview    `json:"previews"`
	Tags             []string     `json:"tags"`       // the category tags
	VersionTag       string       `json:"versionTag"` // the version tag the supported version picks, or "" for none
	DLC              []string     `json:"dlc"`
	Dependencies     []Dependency `json:"dependencies"`
	Source           string       `json:"source"` // the zip of an older listing, or "" for the mod's folder
}

// An Upload is the files that a mod's next upload sends.
type Upload struct {
	Files    []files.File `json:"files"`
	Bytes    int64        `json:"bytes"`    // the total size of the files
	Excluded []string     `json:"excluded"` // the paths that the rules leave out
}

// The types of preview.
const (
	previewImage = "image"
	previewVideo = "video"
)

// The files of a listing's page. They are in the mod's .workshop folder, or in a folder inside it for an older listing.
const (
	workshopDir     = ".workshop"
	descriptionFile = "description.bbcode"
	changelogFile   = "changelog.bbcode"
	dlcFile         = "dlc.txt"
	previewsDir     = "previews"
	videosFile      = previewsDir + "/videos.txt"
	listingFile     = "listing.json" // an older listing's own fields
)

// The files in a mod's folder that list what git and the uploads leave out.
const (
	gitignoreFile = ".gitignore"
	ignoreFile    = ".workshop-ignore"
)

// previewTypes are the types a preview may have.
var previewTypes = []string{previewImage, previewVideo}

// contentExtensions maps the content type of a picture to its file extension.
var contentExtensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// listingVersion matches the game version of an older listing, such as 1.19, 1.19.2 or 1.19.*.
var listingVersion = regexp.MustCompile(`^\d+\.\d+(\.\d+){0,2}(\.\*)?$`)

// extractTurn keeps two extractions of a listing's zip from running at once.
var extractTurn sync.Mutex

// ReadListing returns one of a mod's listings. The mod's own listing comes from its descriptor, and an older one from
// its listing.json. It returns an error if the workspace has no such mod or listing, or a file cannot be read.
func (stm *Service) ReadListing(modID, key string) (Listing, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Listing{}, err
	}
	dir, err := findListingDir(mod.Dir, key)
	if err != nil {
		return Listing{}, err
	}
	listing, err := loadRecord(ws.Game, mod, dir, key)
	if err != nil {
		return Listing{}, err
	}
	versions := stm.listVersionTags(ws.Game.SteamAppID)
	listing.Tags = toCategoryTags(versions, listing.Tags)
	listing.VersionTag = resolveVersionTag(versions, listing.SupportedVersion)
	listing.Dependencies = resolveDependencies(stm.Workspace.State().Mods, mod.Descriptor.Dependencies)
	return loadPage(dir, listing)
}

// resolveListingDir returns the folder that holds the page files of one of a mod's listings.
func resolveListingDir(modDir, key string) string {
	return files.Join(modDir, path.Join(workshopDir, key))
}

// findListingDir returns the folder that holds the page files of one of a mod's listings. It returns an error if the
// mod has no such older listing.
func findListingDir(modDir, key string) (string, error) {
	dir := resolveListingDir(modDir, key)
	if key != "" && !files.IsFolder(dir) {
		return "", fmt.Errorf("the mod has no listing for game %s", key)
	}
	return dir, nil
}

// loadRecord returns the fields of a listing that its descriptor or its listing.json holds. An older listing's paths
// come back as full paths. It returns an error if the file cannot be read.
func loadRecord(row *game.Game, mod workspace.Mod, dir, key string) (Listing, error) {
	if key != "" {
		listing, err := app.LoadJSON[Listing](filepath.Join(dir, listingFile))
		listing = toRecord(listing)
		listing.Key = key
		for _, file := range []*string{&listing.Thumbnail, &listing.Source} {
			if *file != "" && !filepath.IsAbs(*file) {
				*file = files.Join(mod.Dir, *file)
			}
		}
		return listing, err
	}
	descriptor, err := workspace.ReadDescriptor(row, mod.Dir)
	if err != nil {
		return Listing{}, err
	}
	mod.Descriptor = descriptor
	return toRecord(Listing{
		WorkshopID:       mod.ResolveWorkshopID(),
		Title:            descriptor.Name,
		Version:          descriptor.Version,
		SupportedVersion: descriptor.SupportedVersion,
		Thumbnail:        cmp.Or(descriptor.Thumbnail, mod.Thumbnail),
		Tags:             descriptor.Tags,
	}), nil
}

// toRecord returns a copy of a listing with only the fields that its descriptor or its listing.json holds.
func toRecord(listing Listing) Listing {
	return Listing{
		WorkshopID:       listing.WorkshopID,
		Title:            listing.Title,
		Version:          listing.Version,
		SupportedVersion: listing.SupportedVersion,
		Thumbnail:        listing.Thumbnail,
		Tags:             append([]string{}, listing.Tags...),
		Source:           listing.Source,
	}
}

// resolveDependencies returns a descriptor's dependencies, each with the Workshop id of the workspace mod it names.
func resolveDependencies(mods []workspace.Mod, dependencies []workspace.Dependency) []Dependency {
	return lo.Map(dependencies, func(dependency workspace.Dependency, _ int) Dependency {
		mod, _ := workspace.FindMod(mods, dependency)
		return Dependency{Name: dependency.ID, WorkshopID: mod.ResolveWorkshopID()}
	})
}

// loadPage returns a listing with its page files read from its folder. A missing file reads as no text. It returns an
// error if a file cannot be read.
func loadPage(dir string, listing Listing) (Listing, error) {
	texts := map[string]*string{descriptionFile: &listing.Description, changelogFile: &listing.Changelog}
	for file, text := range texts {
		content, err := files.LoadText(files.Join(dir, file))
		if err != nil {
			return Listing{}, err
		}
		*text = content.Text
	}
	dlc, err := files.LoadText(files.Join(dir, dlcFile))
	if err != nil {
		return Listing{}, err
	}
	listing.DLC = strings.Fields(dlc.Text)
	listing.Previews, err = listPreviews(dir)
	return listing, err
}

// listPreviews returns the gallery in a listing's folder, pictures first and then videos.
func listPreviews(dir string) ([]Preview, error) {
	entries, err := os.ReadDir(files.Join(dir, previewsDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	previews := []Preview{}
	for _, entry := range entries {
		rel := path.Join(previewsDir, entry.Name())
		if entry.IsDir() || rel == videosFile {
			continue
		}
		stat, err := entry.Info()
		if err != nil {
			return nil, err
		}
		previews = append(previews, Preview{Path: files.Join(dir, rel), Type: previewImage, Bytes: stat.Size()})
	}
	urls, err := listVideos(dir)
	for _, url := range urls {
		previews = append(previews, Preview{Type: previewVideo, URL: url, Video: toVideoID(url)})
	}
	return previews, err
}

// listVideos returns the addresses of the videos in a listing's gallery. A folder with no list of videos has none.
func listVideos(dir string) ([]string, error) {
	text, err := files.LoadText(files.Join(dir, videosFile))
	return strings.Fields(text.Text), err
}

// WriteListing writes the changed parts of a mod's listing, but its gallery and Workshop id. It returns an error if
// there is no such mod or listing, or a file cannot be written.
func (stm *Service) WriteListing(modID, key string, listing Listing) error {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	current, err := stm.ReadListing(modID, key)
	if err != nil {
		return err
	}
	listing.WorkshopID = current.WorkshopID
	if err := stm.writeChanges(ws, mod, current, listing); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// writeChanges writes the files of a listing whose text differs from the current listing's. A thumbnail from outside
// the mod is copied into it first. It returns an error if a file cannot be read or written.
func (stm *Service) writeChanges(ws *workspace.Session, mod workspace.Mod, current, listing Listing) error {
	listing.Key = current.Key
	dir := resolveListingDir(mod.Dir, listing.Key)
	if listing.Thumbnail != "" && !files.IsUnder(mod.Dir, listing.Thumbnail) {
		content, err := os.ReadFile(listing.Thumbnail)
		if err != nil {
			return err
		}
		folder := lo.Ternary(listing.Key == "", files.Join(mod.Dir, ws.Game.ThumbnailDir), dir)
		thumbnail := filepath.Join(folder, filepath.Base(listing.Thumbnail))
		if err := stm.writeFile(thumbnail, string(content)); err != nil {
			return err
		}
		listing.Thumbnail = thumbnail
	}
	if err := stm.writeRecord(ws, mod, current, listing); err != nil {
		return err
	}
	texts := []struct {
		file, text string
		differs    bool
	}{
		{descriptionFile, listing.Description, listing.Description != current.Description},
		{changelogFile, listing.Changelog, listing.Changelog != current.Changelog},
		{dlcFile, strings.Join(listing.DLC, "\n") + "\n", !slices.Equal(listing.DLC, current.DLC)},
	}
	for _, row := range texts {
		if !row.differs {
			continue
		}
		if err := stm.writeFile(files.Join(dir, row.file), row.text); err != nil {
			return err
		}
	}
	return nil
}

// writeRecord writes a listing's record to its descriptor or listing.json if it changed. It returns an error if a
// file cannot be written.
func (stm *Service) writeRecord(ws *workspace.Session, mod workspace.Mod, current, listing Listing) error {
	if reflect.DeepEqual(toRecord(listing), toRecord(current)) {
		return nil
	}
	if listing.Key != "" {
		record := toRecord(listing)
		for _, file := range []*string{&record.Thumbnail, &record.Source} {
			if rel, ok := files.ToRel(mod.Dir, *file); ok {
				*file = rel
			}
		}
		content, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return err
		}
		return stm.writeFile(files.Join(resolveListingDir(mod.Dir, listing.Key), listingFile), string(content)+"\n")
	}
	descriptor, err := workspace.ReadDescriptor(ws.Game, mod.Dir)
	if err != nil {
		return err
	}
	listing.VersionTag = resolveVersionTag(stm.listVersionTags(ws.Game.SteamAppID), listing.SupportedVersion)
	descriptor.Name, descriptor.Tags, descriptor.WorkshopID = listing.Title, toSentTags(listing), listing.WorkshopID
	descriptor.Version, descriptor.SupportedVersion, descriptor.Thumbnail =
		listing.Version, listing.SupportedVersion, listing.Thumbnail
	if err := workspace.WriteDescriptor(ws.Game, mod.Dir, descriptor); err != nil {
		return err
	}
	if listing.WorkshopID == current.WorkshopID {
		return nil
	}
	mod.WorkshopID = listing.WorkshopID
	return stm.Workspace.UpdateMod(ws.ID, mod.Mod)
}

// toSentTags returns the tags an upload of a listing sends, with the version tag last.
func toSentTags(listing Listing) []string {
	if listing.VersionTag == "" {
		return listing.Tags
	}
	return append(slices.Clip(listing.Tags), listing.VersionTag)
}

// writeFile writes a text in UTF-8 to a file in a mod's folder. It returns an error for a file outside the mods.
func (stm *Service) writeFile(file, text string) error {
	return stm.Workspace.Write(file, files.Text{Text: text, Encoding: files.EncodingUTF8})
}

// ListListings returns a mod's listings, its own first and then the older ones, newest game version first. It returns
// an error if the workspace has no such mod or a listing cannot be read.
func (stm *Service) ListListings(modID string) ([]Listing, error) {
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(files.Join(mod.Dir, workshopDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// a folder that holds a listing.json is an older listing
	keys := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (string, bool) {
		_, err := os.Stat(filepath.Join(resolveListingDir(mod.Dir, entry.Name()), listingFile))
		return entry.Name(), entry.IsDir() && err == nil
	})
	slices.SortFunc(keys, func(first, second string) int { return semver.Compare("v"+second, "v"+first) })
	listings := []Listing{}
	for _, key := range append([]string{""}, keys...) {
		listing, err := stm.ReadListing(modID, key)
		if err != nil {
			return nil, err
		}
		listings = append(listings, listing)
	}
	return listings, nil
}

// CreateListing makes an older listing of a mod for a game version from a zip. It returns an error if the version is
// not valid or taken, or the zip holds no descriptor.
func (stm *Service) CreateListing(modID, supportedVersion, zip string) (Listing, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Listing{}, err
	}
	key, supported, err := resolveListingVersion(mod.Dir, supportedVersion)
	if err != nil {
		return Listing{}, err
	}
	own, err := stm.ReadListing(modID, "")
	if err != nil {
		return Listing{}, err
	}
	_, descriptor, err := loadZip(ws.Game, stm.Workspace.Config.Dirs.Cache, mod.ID+"/"+key, zip)
	if err != nil {
		return Listing{}, err
	}
	return stm.createListing(ws, mod, Listing{
		Key: key, Title: own.Title + " (" + key + ")", Version: descriptor.Version, SupportedVersion: supported,
		Tags: own.Tags, Thumbnail: own.Thumbnail, Description: own.Description, DLC: own.DLC, Source: zip,
	})
}

// resolveListingVersion returns the key and supported version of an older listing, such as 1.19.* for 1.19. It returns
// an error if the version is not valid or taken.
func resolveListingVersion(modDir, version string) (key, supported string, err error) {
	supported = strings.TrimSpace(version)
	if !listingVersion.MatchString(supported) {
		return "", "", fmt.Errorf("%q is not a game version: write one such as 1.19 or 1.19.2", version)
	}
	if strings.Count(supported, ".") == 1 {
		supported += ".*"
	}
	key = strings.TrimSuffix(supported, ".*")
	if files.IsFolder(resolveListingDir(modDir, key)) {
		return "", "", fmt.Errorf("the mod has a listing for game %s already", key)
	}
	return key, supported, nil
}

// createListing writes an older listing of a mod, downloads its gallery and returns it as stored. It returns an error
// if a file cannot be written or a picture cannot be downloaded.
func (stm *Service) createListing(ws *workspace.Session, mod workspace.Mod, listing Listing) (Listing, error) {
	if err := stm.writeChanges(ws, mod, Listing{Key: listing.Key}, listing); err != nil {
		return Listing{}, err
	}
	if err := stm.addPreviews(resolveListingDir(mod.Dir, listing.Key), listing.Previews); err != nil {
		return Listing{}, err
	}
	application.Get().Event.Emit(EventListingChanged, mod.ID)
	return stm.ReadListing(mod.ID, listing.Key)
}

// ImportListing makes an older listing of a mod from a Workshop item, saving its files as a zip. It returns an error
// if Steam has no such item or its version is taken.
func (stm *Service) ImportListing(modID, item, zip string) (Listing, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Listing{}, err
	}
	workshopID, err := toWorkshopID(item)
	if err != nil || workshopID == "" {
		return Listing{}, cmp.Or(err, errors.New("name the Workshop item by its id or the address of its page"))
	}
	stm.removeAnswer(workshopID)
	answer := stm.resolveAnswer(ws, workshopID)
	if answer.Reason != "" {
		return Listing{}, errors.New(answer.Reason)
	}
	cacheDir := stm.Workspace.Config.Dirs.Cache
	if _, ok := LoadSnapshot(cacheDir, workshopID); !ok {
		return Listing{}, errors.New("PMT has no copy of the item's files: try the import again")
	}
	snapshotDir := resolveSnapshotDir(cacheDir, workshopID)
	descriptor, err := workspace.ReadDescriptor(ws.Game, snapshotDir)
	if err != nil {
		return Listing{}, fmt.Errorf("the item holds no descriptor of a mod: %w", err)
	}
	key, supported, err := resolveListingVersion(mod.Dir, descriptor.SupportedVersion)
	if err != nil {
		return Listing{}, fmt.Errorf("PMT cannot make a listing for the game version the item supports: %w", err)
	}
	walked, err := files.Walk(files.Root{Dir: snapshotDir})
	if err == nil {
		err = writeArchive(zip, walked)
	}
	if err != nil {
		return Listing{}, err
	}
	details := answer.Steam
	return stm.createListing(ws, mod, Listing{
		Key: key, WorkshopID: workshopID, Title: details.Title, Description: details.Description,
		Tags:     toCategoryTags(stm.listVersionTags(ws.Game.SteamAppID), details.Tags),
		Previews: details.Previews, DLC: details.DLC, Version: descriptor.Version, SupportedVersion: supported,
		Source: zip,
	})
}

// LinkItem links a mod's listing to a Workshop item, or unlinks it for "". It returns an error if the text names no
// item.
func (stm *Service) LinkItem(modID, key, item string) error {
	workshopID, err := toWorkshopID(item)
	if err != nil {
		return err
	}
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	current, err := stm.ReadListing(modID, key)
	if err != nil {
		return err
	}
	listing := current
	listing.WorkshopID = workshopID
	if err := stm.writeChanges(ws, mod, current, listing); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return stm.removeItemCopy(current.WorkshopID)
}

// removeItemCopy deletes PMT's copy of a Workshop item, unless a mod in any workspace still links it. It does nothing
// for an empty id.
func (stm *Service) removeItemCopy(workshopID string) error {
	mods := lo.FlatMap(stm.Workspace.Config.Config().Workspaces, func(ws app.Workspace, _ int) []app.Mod { return ws.Mods })
	if workshopID == "" || slices.ContainsFunc(mods, func(mod app.Mod) bool { return mod.WorkshopID == workshopID }) {
		return nil
	}
	stm.removeAnswer(workshopID)
	cacheDir := stm.Workspace.Config.Dirs.Cache
	return errors.Join(
		removeCacheDir(cacheDir, resolveSnapshotDir(cacheDir, workshopID)),
		removeCacheDir(cacheDir, resolveSnapshotPath(cacheDir, workshopID)),
	)
}

// toWorkshopID returns the id of the Workshop item that a text names by its id or its page's address. It returns ""
// for an empty text and an error for a text that names no item.
func toWorkshopID(item string) (string, error) {
	text := strings.TrimSpace(item)
	if page, err := url.Parse(text); err == nil && page.Query().Has("id") {
		text = page.Query().Get("id")
	}
	if text == "" {
		return "", nil
	}
	id, err := toItemID(text)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(id, 10), nil
}

// ListUpload returns the files the next upload of a mod's listing sends and leaves out. It returns an error if there
// is no such mod or listing, or a file cannot be read.
func (stm *Service) ListUpload(modID, key string) (Upload, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return Upload{}, err
	}
	listing, err := stm.ReadListing(modID, key)
	if err != nil {
		return Upload{}, err
	}
	return stm.buildUpload(ws, mod, listing)
}

// buildUpload returns the files that the next upload of a listing of a mod sends, and the ones it leaves out. It
// returns an error if the listing's zip cannot be read or a file cannot be read.
func (stm *Service) buildUpload(ws *workspace.Session, mod workspace.Mod, listing Listing) (Upload, error) {
	dir, err := stm.resolveSource(ws, mod, listing)
	if err != nil {
		return Upload{}, err
	}
	ignores, err := buildIgnores(mod)
	if err != nil {
		return Upload{}, err
	}
	walked, err := files.Walk(files.Root{Dir: dir, Origin: mod.ID})
	if err != nil {
		return Upload{}, err
	}
	upload := Upload{Files: []files.File{}, Excluded: []string{}}
	for _, file := range walked {
		if isExcluded(ignores, file.Rel) {
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

// buildIgnores returns what every upload of a mod leaves out, by the rules, the mod's .gitignore and its own
// exclusions. The rules keep the .metadata folder. It returns an error if the .gitignore cannot be read.
func buildIgnores(mod workspace.Mod) ([]files.Ignore, error) {
	gitignore, err := files.LoadText(files.Join(mod.Dir, gitignoreFile))
	if err != nil {
		return nil, err
	}
	patterns := append(lo.Map(listRules(), func(rule Rule, _ int) string { return rule.Pattern }), "!"+game.MetadataDir+"/")
	return []files.Ignore{
		files.ParseIgnore(strings.Join(patterns, "\n")), files.ParseIgnore(gitignore.Text), files.ParseIgnore(mod.Ignore),
	}, nil
}

// isExcluded reports whether any of the ignores covers a file.
func isExcluded(ignores []files.Ignore, rel string) bool {
	return slices.ContainsFunc(ignores, func(ignore files.Ignore) bool { return ignore.Match(rel, false) })
}

// resolveSource returns the folder a mod's listing uploads from. It returns an error if an older listing's zip is
// missing or unreadable.
func (stm *Service) resolveSource(ws *workspace.Session, mod workspace.Mod, listing Listing) (string, error) {
	if listing.Key == "" {
		return mod.Dir, nil
	}
	if listing.Source == "" {
		return "", fmt.Errorf("the listing for game %s names no zip to take its files from", listing.Key)
	}
	dir, descriptor, err := loadZip(ws.Game, stm.Workspace.Config.Dirs.Cache, mod.ID+"/"+listing.Key, listing.Source)
	if err != nil {
		return "", err
	}
	if descriptor.Name == listing.Title && descriptor.Version == listing.Version &&
		descriptor.SupportedVersion == listing.SupportedVersion && descriptor.WorkshopID == listing.WorkshopID {
		return dir, nil
	}
	descriptor.Name, descriptor.Version, descriptor.SupportedVersion, descriptor.WorkshopID =
		listing.Title, listing.Version, listing.SupportedVersion, listing.WorkshopID
	return dir, workspace.WriteDescriptor(ws.Game, dir, descriptor)
}

// loadZip returns the cache folder holding a zip's files and the mod's descriptor in it, extracting the zip if it
// changed. It returns an error if the zip cannot be read or holds no descriptor.
func loadZip(row *game.Game, cacheDir, listing, zip string) (string, workspace.Descriptor, error) {
	extractTurn.Lock()
	defer extractTurn.Unlock()
	stat, err := os.Stat(zip)
	if err != nil {
		return "", workspace.Descriptor{}, err
	}
	digest := sha256.Sum256([]byte(listing))
	dir := filepath.Join(cacheDir, "listing-"+hex.EncodeToString(digest[:8]))
	stamp := fmt.Sprintf("%s\n%d\n%d\n", files.ToKey(zip), stat.Size(), stat.ModTime().UnixNano())
	if kept, err := os.ReadFile(dir + ".stamp"); err != nil || string(kept) != stamp || !files.IsFolder(dir) {
		if err := extractZip(cacheDir, zip, dir, stamp); err != nil {
			return "", workspace.Descriptor{}, err
		}
	}
	descriptor, err := workspace.ReadDescriptor(row, dir)
	if err != nil {
		return "", workspace.Descriptor{}, fmt.Errorf("the zip holds no descriptor of a mod: %w", err)
	}
	return dir, descriptor, nil
}

// extractZip extracts a zip into a listing's cache folder. It returns an error if the zip cannot be read or names a
// file outside its folder.
func extractZip(cacheDir, zip, dir, stamp string) error {
	temp, err := os.MkdirTemp(cacheDir, "copy-")
	if err != nil {
		return err
	}
	if err := writeEntries(zip, temp); err != nil {
		return errors.Join(err, removeCacheDir(cacheDir, temp))
	}
	if err := moveCacheDir(cacheDir, temp, dir); err != nil {
		return err
	}
	return os.WriteFile(dir+".stamp", []byte(stamp), 0o644)
}

// writeEntries writes the files of a zip into a folder. A top folder that holds every file is dropped from the paths.
// Links are skipped. It returns an error if the zip cannot be read or names a path outside the folder.
func writeEntries(zipFile, dir string) error {
	archive, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	entries := lo.Filter(archive.File, func(entry *zip.File, _ int) bool {
		return !entry.FileInfo().IsDir() && entry.Mode()&fs.ModeSymlink == 0
	})
	top, _, _ := strings.Cut(lo.FirstOr(entries, &zip.File{}).Name, "/")
	if !lo.EveryBy(entries, func(entry *zip.File) bool { return strings.HasPrefix(entry.Name, top+"/") }) {
		top = ""
	}
	for _, entry := range entries {
		target := files.Join(dir, strings.TrimPrefix(entry.Name, top+"/"))
		if !files.IsUnder(dir, target) {
			return fmt.Errorf("the zip names %s, which is outside its own folder", entry.Name)
		}
		if err := writeEntry(entry, target); err != nil {
			return err
		}
	}
	return nil
}

// writeEntry writes one file of a zip to disk, making its folder first.
func writeEntry(entry *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, reader)
	return errors.Join(err, file.Close())
}

// WriteZip writes the files of a mod's next upload as a zip. A zip it fails to write is removed.
func (stm *Service) WriteZip(modID, path string) error {
	upload, err := stm.ListUpload(modID, "")
	if err != nil {
		return err
	}
	return writeArchive(path, upload.Files)
}

// writeArchive writes files as a zip, each at its path from its own folder. A zip it fails to write is removed.
func writeArchive(path string, list []files.File) error {
	zipFile, err := os.Create(path)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(zipFile)
	for _, file := range list {
		if err = writeArchiveFile(archive, file); err != nil {
			break
		}
	}
	if err = errors.Join(err, archive.Close(), zipFile.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

// writeArchiveFile writes one file into a zip, compressed, keeping its modified time. It returns an error if the file
// cannot be read or the zip cannot take it.
func writeArchiveFile(archive *zip.Writer, file files.File) error {
	stat, err := os.Stat(file.Path)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return err
	}
	header.Name, header.Method = file.Rel, zip.Deflate
	entry, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return err
	}
	_, err = entry.Write(content)
	return err
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
	text, err := files.LoadText(files.Join(mod.Dir, ignoreFile))
	return text.Text, err
}
