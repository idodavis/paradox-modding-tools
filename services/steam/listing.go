// Package steam publishes mods to the Steam Workshop. It keeps each mod's listings in the mod's own files and compares
// them with Steam.
package steam

import (
	"archive/zip"
	"bytes"
	"cmp"
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
	Files    []files.File      `json:"files"`
	Bytes    int64             `json:"bytes"`    // the total size of the files
	Excluded map[string]string `json:"excluded"` // the paths the rules leave out, each with why
}

// An exclusion is a set of ignore rules and why an upload leaves out the files they cover.
type exclusion struct {
	ignore files.Ignore
	reason string
}

// A source is a folder, or an older listing's zip, whose files are read where they are, by their Rel paths. An upload
// sends a zip's descriptor with the listing's name, versions and Workshop id.
type source struct {
	files      fs.FS     // the files, by their Rel paths
	root       string    // the folder, or the zip
	descriptor string    // the Rel path of the descriptor the upload sends in place of the zip's; "" for a folder
	text       []byte    // that descriptor's text
	archive    io.Closer // the open zip; nil for a folder
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

// ReadListing returns one of a mod's listings. It returns an error if the workspace has no such mod or listing, or a
// file cannot be read.
func (stm *Service) ReadListing(modID, key string) (Listing, error) {
	_, _, listing, err := stm.findListing(modID, key)
	return listing, err
}

// findListing returns the open workspace, one of its mods and one of the mod's listings. It returns an error if the
// workspace has no such mod or listing, or a file cannot be read.
func (stm *Service) findListing(modID, key string) (*workspace.Session, workspace.Mod, Listing, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return nil, workspace.Mod{}, Listing{}, err
	}
	dir, err := findListingDir(mod.Dir, key)
	if err != nil {
		return nil, workspace.Mod{}, Listing{}, err
	}
	listing, err := loadRecord(mod, dir, key)
	if err != nil {
		return nil, workspace.Mod{}, Listing{}, err
	}
	versions := stm.listVersionTags(ws.Game.SteamAppID)
	listing.Tags = toCategoryTags(versions, listing.Tags)
	listing.VersionTag = resolveVersionTag(versions, listing.SupportedVersion)
	mods := stm.Workspace.ListMods()
	listing.Dependencies = lo.Map(mod.Descriptor.Dependencies, func(dependency workspace.Dependency, _ int) Dependency {
		required, _ := workspace.FindMod(mods, dependency)
		return Dependency{Name: dependency.ID, WorkshopID: required.ResolveWorkshopID()}
	})
	listing, err = loadPage(dir, listing)
	return ws, mod, listing, err
}

// resolveListingDir returns the folder that holds the page files of one of a mod's listings.
func resolveListingDir(modDir, key string) string {
	return files.Join(modDir, path.Join(workshopDir, key))
}

// findListingDir returns the folder that holds the page files of one of a mod's listings. It returns an error if the
// mod has no such older listing.
func findListingDir(modDir, key string) (string, error) {
	dir := resolveListingDir(modDir, key)
	if key != "" && !isListing(modDir, key) {
		return "", fmt.Errorf("the mod has no listing for game %s", key)
	}
	return dir, nil
}

// isListing reports whether a mod has an older listing for a game version.
func isListing(modDir, key string) bool {
	return files.IsFile(files.Join(resolveListingDir(modDir, key), listingFile))
}

// loadRecord returns the fields of a listing that its descriptor or its listing.json holds. An older listing's paths
// come back as full paths. It returns an error if listing.json cannot be read.
func loadRecord(mod workspace.Mod, dir, key string) (Listing, error) {
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
	return toRecord(Listing{
		WorkshopID:       mod.ResolveWorkshopID(),
		Title:            mod.Descriptor.Name,
		Version:          mod.Descriptor.Version,
		SupportedVersion: mod.Descriptor.SupportedVersion,
		Thumbnail:        cmp.Or(mod.Descriptor.Thumbnail, mod.Thumbnail),
		Tags:             mod.Descriptor.Tags,
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

// loadPage returns a listing with its page files read from its folder. It returns an error if a file cannot be read.
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
	ws, mod, current, err := stm.findListing(modID, key)
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

// writeChanges writes the files of a listing whose text differs from the current listing's. It returns an error if a
// file cannot be read or written.
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

// writeRecord writes a listing's record to its descriptor or listing.json if it changed.
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
	listing.VersionTag = resolveVersionTag(stm.listVersionTags(ws.Game.SteamAppID), listing.SupportedVersion)
	mod.Descriptor.Name, mod.Descriptor.Tags, mod.Descriptor.WorkshopID = listing.Title, toSentTags(listing), listing.WorkshopID
	mod.Descriptor.Version, mod.Descriptor.SupportedVersion, mod.Descriptor.Thumbnail =
		listing.Version, listing.SupportedVersion, listing.Thumbnail
	if err := ws.WriteDescriptor(mod.ID, mod.Descriptor); err != nil {
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
	keys := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (string, bool) {
		return entry.Name(), isListing(mod.Dir, entry.Name())
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
func (stm *Service) CreateListing(modID, supportedVersion, zipPath string) (Listing, error) {
	ws, mod, own, err := stm.findListing(modID, "")
	if err != nil {
		return Listing{}, err
	}
	key, supported, err := resolveListingVersion(mod.Dir, supportedVersion)
	if err != nil {
		return Listing{}, err
	}
	src, descriptor, err := openZip(ws.Game, zipPath)
	if err != nil {
		return Listing{}, err
	}
	if err := src.close(); err != nil {
		return Listing{}, err
	}
	return stm.createListing(ws, mod, Listing{
		Key: key, Title: own.Title + " (" + key + ")", Version: descriptor.Version, SupportedVersion: supported,
		Tags: own.Tags, Thumbnail: own.Thumbnail, Description: own.Description, DLC: own.DLC, Source: zipPath,
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
	if isListing(modDir, key) {
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
func (stm *Service) ImportListing(modID, item, zipPath string) (Listing, error) {
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
	snapshot, err := stm.loadSnapshot(ws, workshopID)
	if err != nil {
		return Listing{}, err
	}
	descriptor, err := workspace.ReadDescriptor(ws.Game, snapshot.Dir)
	if err != nil {
		return Listing{}, fmt.Errorf("the item holds no descriptor of a mod: %w", err)
	}
	key, supported, err := resolveListingVersion(mod.Dir, descriptor.SupportedVersion)
	if err != nil {
		return Listing{}, fmt.Errorf("PMT cannot make a listing for the game version the item supports: %w", err)
	}
	walked, err := files.Walk(files.Root{Dir: snapshot.Dir})
	if err == nil {
		err = writeArchive(zipPath, walked)
	}
	if err != nil {
		return Listing{}, err
	}
	details := answer.Steam
	return stm.createListing(ws, mod, Listing{
		Key: key, WorkshopID: workshopID, Title: details.Title, Description: details.Description,
		Tags:     toCategoryTags(stm.listVersionTags(ws.Game.SteamAppID), details.Tags),
		Previews: details.Previews, DLC: details.DLC, Version: descriptor.Version, SupportedVersion: supported,
		Source: zipPath,
	})
}

// LinkItem links a mod's listing to a Workshop item, or unlinks it for "". It returns an error if the text names no
// item.
func (stm *Service) LinkItem(modID, key, item string) error {
	workshopID, err := toWorkshopID(item)
	if err != nil {
		return err
	}
	ws, mod, current, err := stm.findListing(modID, key)
	if err != nil {
		return err
	}
	listing := current
	listing.WorkshopID = workshopID
	if err := stm.writeChanges(ws, mod, current, listing); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
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
	ws, mod, listing, err := stm.findListing(modID, key)
	if err != nil {
		return Upload{}, err
	}
	src, err := stm.openSource(ws, mod, listing)
	if err != nil {
		return Upload{}, err
	}
	defer func() { _ = src.close() }()
	upload, _, err := stm.buildUpload(mod, src)
	return upload, err
}

// buildUpload returns the files that the next upload of a mod sends from a source and those it leaves out, and the
// exclusions that decide which are left out. It returns an error if a file cannot be read.
func (stm *Service) buildUpload(mod workspace.Mod, src source) (Upload, []exclusion, error) {
	exclusions, err := stm.buildExclusions(mod)
	if err != nil {
		return Upload{}, nil, err
	}
	walked, err := src.list(mod.ID)
	if err != nil {
		return Upload{}, nil, err
	}
	upload := Upload{Files: []files.File{}, Excluded: map[string]string{}}
	for _, file := range walked {
		if reason, excluded := findExclusion(exclusions, file.Rel); excluded {
			upload.Excluded[file.Rel] = reason
			continue
		}
		stat, err := fs.Stat(src.files, file.Rel)
		if err != nil {
			return Upload{}, nil, err
		}
		upload.Files = append(upload.Files, file)
		upload.Bytes += stat.Size()
	}
	return upload, exclusions, nil
}

// buildExclusions returns what every upload of a mod leaves out: each rule, the mod's .gitignore and the mod's own
// patterns. It returns an error if the .gitignore cannot be read.
func (stm *Service) buildExclusions(mod workspace.Mod) ([]exclusion, error) {
	gitignore, err := files.LoadText(files.Join(mod.Dir, gitignoreFile))
	if err != nil {
		return nil, err
	}
	keepMetadata := "\n!" + game.MetadataDir + "/"
	exclusions := lo.Map(stm.ListRules(), func(rule Rule, _ int) exclusion {
		return exclusion{ignore: files.ParseIgnore(rule.Pattern + keepMetadata), reason: rule.Reason}
	})
	return append(exclusions,
		exclusion{ignore: files.ParseIgnore(gitignore.Text), reason: "The mod's .gitignore names it."},
		exclusion{ignore: files.ParseIgnore(mod.Ignore), reason: "One of the mod's own patterns names it."},
	), nil
}

// findExclusion returns why an upload leaves out a file: the reason of the first exclusion that covers it. It returns
// false if none does.
func findExclusion(exclusions []exclusion, rel string) (string, bool) {
	row, ok := lo.Find(exclusions, func(row exclusion) bool { return row.ignore.Match(rel, false) })
	return row.reason, ok
}

// openSource opens the files a mod's listing uploads: the mod's folder, or an older listing's zip. The caller closes
// the source. It returns an error if an older listing names no zip, or its zip cannot be read or holds no descriptor.
func (stm *Service) openSource(ws *workspace.Session, mod workspace.Mod, listing Listing) (source, error) {
	if listing.Key == "" {
		return source{files: os.DirFS(mod.Dir), root: mod.Dir}, nil
	}
	if listing.Source == "" {
		return source{}, fmt.Errorf("the listing for game %s names no zip to take its files from", listing.Key)
	}
	src, descriptor, err := openZip(ws.Game, listing.Source)
	if err != nil {
		return source{}, err
	}
	descriptor.Name, descriptor.Version, descriptor.SupportedVersion, descriptor.WorkshopID =
		listing.Title, listing.Version, listing.SupportedVersion, listing.WorkshopID
	text := files.DecodeText(src.text)
	if text.Text, err = workspace.EncodeDescriptor(src.descriptor, text.Text, descriptor); err != nil {
		return source{}, errors.Join(err, src.close())
	}
	src.text = text.Encode()
	return src, nil
}

// openZip opens a zip of a mod's files as a source, from the one folder that holds them all, and reads the descriptor
// in it. The caller closes the source. It returns an error if the zip cannot be read or holds no descriptor.
func openZip(row *game.Game, zipPath string) (source, workspace.Descriptor, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return source{}, workspace.Descriptor{}, err
	}
	folder, err := fs.Sub(&archive.Reader, cmp.Or(files.FindZipFolder(&archive.Reader), "."))
	src := source{files: folder, root: zipPath, descriptor: row.ModDescriptor, archive: archive}
	if err == nil {
		src.text, err = fs.ReadFile(folder, row.ModDescriptor)
	}
	var descriptor workspace.Descriptor
	if err == nil {
		descriptor, err = workspace.ParseDescriptor(row.ModDescriptor, files.DecodeText(src.text).Text)
	}
	if err != nil {
		return source{}, workspace.Descriptor{}, errors.Join(
			fmt.Errorf("the zip holds no descriptor of a mod: %w", err), archive.Close(),
		)
	}
	return src, descriptor, nil
}

// list returns the files of the source as an upload's, from the given origin. It skips the folders files.IsHiddenDir
// reports, as files.Walk does.
func (src source) list(origin string) ([]files.File, error) {
	if src.archive == nil {
		return files.Walk(files.Root{Dir: src.root, Origin: origin})
	}
	list := []files.File{}
	err := fs.WalkDir(src.files, ".", func(rel string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() && rel != "." && files.IsHiddenDir(entry.Name()):
			return fs.SkipDir
		case !entry.IsDir():
			filePath := files.Join(src.root, rel)
			list = append(list, files.File{
				Path: filePath, Key: files.ToKey(filePath), Rel: rel, Origin: origin, Format: files.Classify(rel),
			})
		}
		return nil
	})
	slices.SortFunc(list, func(file, other files.File) int { return strings.Compare(file.Rel, other.Rel) })
	return list, err
}

// hashKey returns what the hash of one of the source's files is kept by: its path, size and time. It returns "" for a
// file it cannot read, and for the descriptor a zip's upload rewrites, whose hash is never kept.
func (src source) hashKey(file files.File) string {
	stat, err := fs.Stat(src.files, file.Rel)
	if err != nil || file.Rel == src.descriptor {
		return ""
	}
	return fmt.Sprintf("%s %d %d", file.Path, stat.Size(), stat.ModTime().UnixNano())
}

// open opens one of the source's files as the upload sends it.
func (src source) open(rel string) (io.ReadCloser, error) {
	if rel == src.descriptor {
		return io.NopCloser(bytes.NewReader(src.text)), nil
	}
	return src.files.Open(rel)
}

// loadText reads one of the source's files into its text as the upload sends it. A missing file, or no Rel path,
// gives empty text and no error, as files.LoadText does.
func (src source) loadText(rel string) (files.Text, error) {
	switch {
	case rel == "":
		return files.Text{}, nil
	case src.archive == nil:
		return files.LoadText(files.Join(src.root, rel))
	}
	file, err := src.open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return files.Text{}, nil
	}
	if err != nil {
		return files.Text{}, err
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(file)
	return files.DecodeText(content), err
}

// close closes the source's zip, if it has one.
func (src source) close() error {
	if src.archive == nil {
		return nil
	}
	return src.archive.Close()
}

// WriteZip writes the files of a mod's next upload as a zip.
func (stm *Service) WriteZip(modID, target string) error {
	upload, err := stm.ListUpload(modID, "")
	if err != nil {
		return err
	}
	return writeArchive(target, upload.Files)
}

// writeArchive writes the files as a zip, each stored under its Rel path. It removes the zip if writing fails.
func writeArchive(target string, list []files.File) error {
	zipFile, err := os.Create(target)
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
		return errors.Join(err, os.Remove(target))
	}
	return nil
}

// writeArchiveFile writes one file into a zip, keeping its modified time. It returns an error if the file cannot be
// read or the zip cannot take it.
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
