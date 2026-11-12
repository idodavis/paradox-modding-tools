// listing.go reads, writes and imports a mod's listings and their uploads.

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

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// Preview is one picture or video in the Workshop page's gallery.
type Preview struct {
	Path string `json:"path"`

	// Type is image or video.
	Type string `json:"type"`

	// URL and Video are the address and the id of a video.
	URL   string `json:"url"`
	Video string `json:"video"`
	Bytes int64  `json:"bytes"`
}

// Dependency is one mod that a listing's mod depends on.
type Dependency struct {
	Name string `json:"name"`

	// WorkshopID is the Workshop id of the workspace mod it names, or "".
	WorkshopID string `json:"workshopId"`
}

// Listing is one of a mod's Workshop pages, as the mod's own files hold it.
type Listing struct {
	// Key is "" for the mod's own listing, else the game version of an older
	// one.
	Key string `json:"key"`

	WorkshopID       string `json:"workshopId"`
	Title            string `json:"title"`
	Version          string `json:"version"`
	SupportedVersion string `json:"supportedVersion"`

	// Description and Changelog are BBCode.
	Description string `json:"description"`
	Changelog   string `json:"changelog"`

	Thumbnail string    `json:"thumbnail"`
	Previews  []Preview `json:"previews"`

	// Tags are the category tags. VersionTag is "" for none.
	Tags       []string `json:"tags"`
	VersionTag string   `json:"versionTag"`

	DLC          []string     `json:"dlc"`
	Dependencies []Dependency `json:"dependencies"`

	// Source is the zip of an older listing, or "" for the mod's folder.
	Source string `json:"source"`
}

// Upload is the files that a mod's next upload sends.
type Upload struct {
	Files []files.File `json:"files"`

	Bytes int64 `json:"bytes"`

	// Excluded maps each path the rules leave out to why.
	Excluded map[string]string `json:"excluded"`
}

type exclusion struct {
	ignore files.Ignore
	reason string
}

// source is a folder, or an older listing's zip, whose files are read where
// they are.
type source struct {
	files fs.FS
	root  string

	// descriptor is the Rel path of the descriptor the upload sends in place of
	// the zip's, or "" for a folder. Its text is text.
	descriptor string
	text       []byte

	// archive is the open zip, or nil for a folder.
	archive io.Closer
}

const (
	previewImage = "image"
	previewVideo = "video"
)

const (
	workshopDir     = ".workshop"
	descriptionFile = "description.bbcode"
	changelogFile   = "changelog.bbcode"
	dlcFile         = "dlc.txt"
	previewsDir     = "previews"
	videosFile      = previewsDir + "/videos.txt"
	listingFile     = "listing.json"
)

const (
	gitignoreFile = ".gitignore"
	ignoreFile    = ".workshop-ignore"
)

// ReadListing returns one of a mod's listings.
func (s *Service) ReadListing(modID, key string) (Listing, error) {
	_, _, listing, err := s.findListing(modID, key)

	return listing, err
}

func (s *Service) findListing(modID, key string) (*workspace.Session, workspace.Mod, Listing, error) {
	ws, mod, err := s.findMod(modID)
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

	versions := s.listVersionTags(ws.Game.SteamAppID)
	listing.Tags = toCategoryTags(versions, listing.Tags)
	listing.VersionTag = resolveVersionTag(versions, listing.SupportedVersion)

	mods := s.Workspace.ListMods()
	listing.Dependencies = make([]Dependency, len(mod.Descriptor.Dependencies))
	for position, dependency := range mod.Descriptor.Dependencies {
		// Vic3 and EU5 descriptors name a dependency by an id, so a mod the
		// workspace has is shown by its name.
		required, _ := workspace.FindMod(mods, dependency)
		listing.Dependencies[position] = Dependency{
			Name: cmp.Or(required.Descriptor.Name, dependency.ID), WorkshopID: required.ResolveWorkshopID(),
		}
	}

	listing, err = loadPage(dir, listing)

	return ws, mod, listing, err
}

func resolveListingDir(modDir, key string) string {
	return files.Join(modDir, path.Join(workshopDir, key))
}

// findListingDir returns the folder of a listing's page files, or an error if
// the mod has no such older listing.
func findListingDir(modDir, key string) (string, error) {
	dir := resolveListingDir(modDir, key)
	if key != "" && !isListing(modDir, key) {
		return "", fmt.Errorf("the mod has no listing for game %s", key)
	}

	return dir, nil
}

func isListing(modDir, key string) bool {
	return files.IsFile(files.Join(resolveListingDir(modDir, key), listingFile))
}

// loadRecord returns the fields of a listing that its descriptor or its
// listing.json holds. An older listing's paths come back as full paths.
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

// listPreviews returns a listing's gallery, pictures first and then videos.
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

func listVideos(dir string) ([]string, error) {
	text, err := files.LoadText(files.Join(dir, videosFile))

	return strings.Fields(text.Text), err
}

// WriteListing writes the changed parts of a mod's listing, but not its gallery
// or Workshop id.
func (s *Service) WriteListing(modID, key string, listing Listing) error {
	ws, mod, current, err := s.findListing(modID, key)
	if err != nil {
		return err
	}

	listing.WorkshopID = current.WorkshopID
	if err := s.writeChanges(ws, mod, current, listing); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)

	return nil
}

func (s *Service) writeChanges(ws *workspace.Session, mod workspace.Mod, current, listing Listing) error {
	listing.Key = current.Key
	dir := resolveListingDir(mod.Dir, listing.Key)
	if listing.Thumbnail != "" && !files.IsUnder(mod.Dir, listing.Thumbnail) {
		content, err := os.ReadFile(listing.Thumbnail)
		if err != nil {
			return err
		}

		folder := dir
		if listing.Key == "" {
			folder = files.Join(mod.Dir, ws.Game.ThumbnailDir)
		}

		thumbnail := filepath.Join(folder, filepath.Base(listing.Thumbnail))
		if err := s.writeFile(thumbnail, string(content)); err != nil {
			return err
		}
		listing.Thumbnail = thumbnail
	}

	if err := s.writeRecord(ws, mod, current, listing); err != nil {
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

		if err := s.writeFile(files.Join(dir, row.file), row.text); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) writeRecord(ws *workspace.Session, mod workspace.Mod, current, listing Listing) error {
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

		return s.writeFile(files.Join(resolveListingDir(mod.Dir, listing.Key), listingFile), string(content)+"\n")
	}

	listing.VersionTag = resolveVersionTag(s.listVersionTags(ws.Game.SteamAppID), listing.SupportedVersion)
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

	return s.Workspace.UpdateMod(ws.ID, mod.Mod)
}

// toSentTags returns the tags an upload sends, with the version tag last.
func toSentTags(listing Listing) []string {
	if listing.VersionTag == "" {
		return listing.Tags
	}

	return append(slices.Clip(listing.Tags), listing.VersionTag)
}

func (s *Service) writeFile(file, text string) error {
	return s.Workspace.Write(file, files.Text{Text: text, Encoding: files.EncodingUTF8})
}

// ListListings returns a mod's listings, its own first and then the older ones,
// newest game version first.
func (s *Service) ListListings(modID string) ([]Listing, error) {
	_, mod, err := s.findMod(modID)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(files.Join(mod.Dir, workshopDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	keys := []string{}
	for _, entry := range entries {
		if isListing(mod.Dir, entry.Name()) {
			keys = append(keys, entry.Name())
		}
	}
	slices.SortFunc(keys, func(first, second string) int { return game.CompareVersions(second, first) })

	listings := []Listing{}
	for _, key := range append([]string{""}, keys...) {
		listing, err := s.ReadListing(modID, key)
		if err != nil {
			return nil, err
		}

		listings = append(listings, listing)
	}

	return listings, nil
}

// CreateListing makes an older listing of a mod for a game version from a zip.
// It returns an error if the version is invalid or taken.
func (s *Service) CreateListing(modID, supportedVersion, zipPath string) (Listing, error) {
	ws, mod, own, err := s.findListing(modID, "")
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

	return s.createListing(ws, mod, Listing{
		Key: key, Title: own.Title + " (" + key + ")", Version: descriptor.Version, SupportedVersion: supported,
		Tags: own.Tags, Thumbnail: own.Thumbnail, Description: own.Description, DLC: own.DLC, Source: zipPath,
	})
}

// listingVersion matches 1.19, 1.19.2 or 1.19.*.
var listingVersion = regexp.MustCompile(`^\d+\.\d+(\.\d+){0,2}(\.\*)?$`)

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

func (s *Service) createListing(ws *workspace.Session, mod workspace.Mod, listing Listing) (Listing, error) {
	if err := s.writeChanges(ws, mod, Listing{Key: listing.Key}, listing); err != nil {
		return Listing{}, err
	}

	if err := s.addPreviews(resolveListingDir(mod.Dir, listing.Key), listing.Previews); err != nil {
		return Listing{}, err
	}
	application.Get().Event.Emit(EventListingChanged, mod.ID)

	return s.ReadListing(mod.ID, listing.Key)
}

// ImportListing makes an older listing of a mod from a Workshop item, saving
// its files as a zip. It returns an error if the item's version is taken.
func (s *Service) ImportListing(modID, item, zipPath string) (Listing, error) {
	ws, mod, err := s.findMod(modID)
	if err != nil {
		return Listing{}, err
	}

	workshopID, err := toWorkshopID(item)
	if err != nil || workshopID == "" {
		return Listing{}, cmp.Or(err, errors.New("name the Workshop item by its id or the address of its page"))
	}

	s.removeAnswer(workshopID)
	answer := s.resolveAnswer(ws, workshopID)
	if answer.Reason != "" {
		return Listing{}, errors.New(answer.Reason)
	}

	snapshot, err := s.loadSnapshot(ws, workshopID)
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

	return s.createListing(ws, mod, Listing{
		Key: key, WorkshopID: workshopID, Title: details.Title, Description: details.Description,
		Tags:     toCategoryTags(s.listVersionTags(ws.Game.SteamAppID), details.Tags),
		Previews: details.Previews, DLC: details.DLC, Version: descriptor.Version, SupportedVersion: supported,
		Source: zipPath,
	})
}

// LinkItem links a mod's listing to a Workshop item, or unlinks it for "".
func (s *Service) LinkItem(modID, key, item string) error {
	workshopID, err := toWorkshopID(item)
	if err != nil {
		return err
	}

	ws, mod, current, err := s.findListing(modID, key)
	if err != nil {
		return err
	}

	listing := current
	listing.WorkshopID = workshopID
	if err := s.writeChanges(ws, mod, current, listing); err != nil {
		return err
	}

	if current.WorkshopID != workshopID {
		_ = removeCacheDir(s.Workspace.Config.Dirs.Cache, resolveSnapshotPath(s.Workspace.Config.Dirs.Cache, current.WorkshopID))
	}
	application.Get().Event.Emit(EventListingChanged, modID)

	return nil
}

// toWorkshopID returns the id of the item a text names by its id or its page's
// address. It returns "" for an empty text and an error for any other.
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

// ListUpload returns the files the next upload sends and leaves out.
func (s *Service) ListUpload(modID, key string) (Upload, error) {
	ws, mod, listing, err := s.findListing(modID, key)
	if err != nil {
		return Upload{}, err
	}

	src, err := s.openSource(ws, mod, listing)
	if err != nil {
		return Upload{}, err
	}
	defer func() { _ = src.close() }()

	upload, _, err := s.buildUpload(mod, src)

	return upload, err
}

// buildUpload returns the files the next upload sends and leaves out, with the
// exclusions that decide which.
func (s *Service) buildUpload(mod workspace.Mod, src source) (Upload, []exclusion, error) {
	exclusions, err := s.buildExclusions(mod)
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

func (s *Service) buildExclusions(mod workspace.Mod) ([]exclusion, error) {
	gitignore, err := files.LoadText(files.Join(mod.Dir, gitignoreFile))
	if err != nil {
		return nil, err
	}

	keepMetadata := "\n!" + game.MetadataDir + "/"
	exclusions := []exclusion{}
	for _, rule := range s.ListRules() {
		if rule.Pattern == gitignoreFile {
			exclusions = append(exclusions, exclusion{ignore: files.ParseIgnore(gitignore.Text), reason: rule.Reason})
			continue
		}

		exclusions = append(exclusions, exclusion{ignore: files.ParseIgnore(rule.Pattern + keepMetadata), reason: rule.Reason})
	}

	return append(exclusions,
		exclusion{ignore: files.ParseIgnore(mod.Ignore), reason: "One of the mod's own patterns names it."}), nil
}

func findExclusion(exclusions []exclusion, rel string) (string, bool) {
	for _, row := range exclusions {
		if row.ignore.Match(rel, false) {
			return row.reason, true
		}
	}

	return "", false
}

// openSource opens the files a listing uploads: the mod's folder, or an older
// listing's zip. The caller closes it.
func (s *Service) openSource(ws *workspace.Session, mod workspace.Mod, listing Listing) (source, error) {
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

// openZip opens a zip of a mod's files as a source, from the one folder that
// holds them all, and reads its descriptor. The caller closes the source.
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

// list returns the source's files as an upload's, skipping hidden folders as
// files.Walk does.
func (s source) list(origin string) ([]files.File, error) {
	if s.archive == nil {
		return files.Walk(files.Root{Dir: s.root, Origin: origin})
	}

	list := []files.File{}
	err := fs.WalkDir(s.files, ".", func(rel string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err

		case entry.IsDir() && rel != "." && files.IsHiddenDir(entry.Name()):
			return fs.SkipDir

		case !entry.IsDir():
			filePath := files.Join(s.root, rel)
			list = append(list, files.File{
				Path: filePath, Key: files.ToKey(filePath), Rel: rel, Origin: origin, Format: files.Classify(rel),
			})
		}

		return nil
	})
	slices.SortFunc(list, func(file, other files.File) int { return strings.Compare(file.Rel, other.Rel) })

	return list, err
}

// hashKey returns what a file's hash is kept by: its path, size and time. It
// returns "" for an unreadable file and for the descriptor a zip's upload
// rewrites.
func (s source) hashKey(file files.File) string {
	stat, err := fs.Stat(s.files, file.Rel)
	if err != nil || file.Rel == s.descriptor {
		return ""
	}

	return fmt.Sprintf("%s %d %d", file.Path, stat.Size(), stat.ModTime().UnixNano())
}

func (s source) open(rel string) (io.ReadCloser, error) {
	if rel == s.descriptor {
		return io.NopCloser(bytes.NewReader(s.text)), nil
	}

	return s.files.Open(rel)
}

// loadText reads one of the source's files. A missing file gives empty text and
// no error, as files.LoadText does.
func (s source) loadText(rel string) (files.Text, error) {
	switch {
	case rel == "":
		return files.Text{}, nil
	case s.archive == nil:
		return files.LoadText(files.Join(s.root, rel))
	}

	file, err := s.open(rel)
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

func (s source) close() error {
	if s.archive == nil {
		return nil
	}

	return s.archive.Close()
}

// ExportDescription writes a listing's description to a file, which may be
// anywhere.
func (s *Service) ExportDescription(target, text string) error {
	return files.WriteText(target, files.Text{Text: text, Encoding: files.EncodingUTF8})
}

// WriteZip writes the files of a mod's next upload as a zip.
func (s *Service) WriteZip(modID, target string) error {
	upload, err := s.ListUpload(modID, "")
	if err != nil {
		return err
	}

	return writeArchive(target, upload.Files)
}

// writeArchive writes files as a zip, each under its Rel path, and removes the
// zip if writing fails.
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

// writeArchiveFile writes one file into a zip, keeping its modified time.
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

// WriteIgnore writes a mod's own exclusions to its .workshop-ignore file.
func (s *Service) WriteIgnore(modID string) error {
	_, mod, err := s.findMod(modID)
	if err != nil {
		return err
	}

	return s.writeFile(files.Join(mod.Dir, ignoreFile), mod.Ignore)
}

// ReadIgnore returns the text of a mod's .workshop-ignore file, or "" if there
// is none.
func (s *Service) ReadIgnore(modID string) (string, error) {
	_, mod, err := s.findMod(modID)
	if err != nil {
		return "", err
	}

	text, err := files.LoadText(files.Join(mod.Dir, ignoreFile))

	return text.Text, err
}
