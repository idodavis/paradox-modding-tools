// Package steam publishes mods to the Steam Workshop. It keeps each mod's
// listings in the mod's own files, compares the next upload with what Steam
// has, and uploads from a second copy of the app that loads the game's own
// copy of Steam's library.
//
// It applies these rules:
//
//   - A mod has its own listing, whose key is "", and an older listing for
//     each older game version, whose key is that version. An older listing is
//     a separate Workshop item.
//   - An upload leaves out files that start with a dot, though not .metadata,
//     the coding agents' instruction files, and what the mod's .gitignore and
//     its own patterns cover.
//   - The compare asks Steam only when no answer is kept for the item, and
//     downloads the item only when Steam's update time differs from the
//     snapshot's.
//   - A picture of 1 MB or more is sent as a smaller copy. A GIF that big is
//     refused, since shrinking it would lose its animation.
//   - An item owned by another Steam user is refused. A new item's id is
//     linked to the listing as soon as Steam makes it.
//   - The functions of Steam's library that read and change DLC and required
//     items are optional. Without them an upload leaves both as Steam has them.
//
// # Robustness
//
// A library that does not load, lacks a required function or has no signed-in
// user stops the second copy with an error. A canceled upload ends the copy,
// and the error says Steam may already have applied part of it. A tags page
// that cannot be fetched falls back to the tags kept in the cache. A DLC
// descriptor with no Steam id is skipped, and a file that cannot be hashed has
// the hash "".
package steam

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/git"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service that publishes mods to the Steam Workshop.
type Service struct {
	Workspace *workspace.Service
	Git       *git.Service

	// flight runs one ask of Steam per item, and one fetch of tags per game.
	flight singleflight.Group

	// mu guards the maps below. answers is keyed by Workshop id, hashes by a
	// file's path, size and time, lines by the hashes of a file's two copies,
	// and tagFetches by the app ids of the games whose tags were fetched.
	mu         sync.Mutex
	answers    map[string]Comparison
	hashes     map[string]string
	lines      map[string]Difference
	tagFetches map[int]bool
}

// The events the service sends. EventProgress carries a Progress, and
// EventListingChanged a mod's id.
const (
	EventProgress       = "steam:progress"
	EventListingChanged = "release:changed"
)

// The cache names of a snapshot, and of the folders an upload copies its files
// and pictures into.
const (
	snapshotPrefix = "snapshot-"
	copyPrefix     = "copy-"
	picturesPrefix = "pictures-"
)

func init() {
	application.RegisterEvent[Progress](EventProgress)
	application.RegisterEvent[string](EventListingChanged)
}

// ServePublish does a second copy's work if the app was started as one. It
// returns the exit code and whether it did.
func ServePublish(args []string) (exitCode int, served bool) {
	for flag, work := range copyWork {
		if slices.Contains(args, flag) {
			return runCopy(work), true
		}
	}

	return 0, false
}

// ServiceStartup deletes, in the background, the copies an upload left in the
// cache when the app stopped during it.
func (s *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	cacheDir := s.Workspace.Config.Dirs.Cache
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}

	stale := []string{}
	for _, entry := range entries {
		if isStale(entry.Name()) {
			stale = append(stale, filepath.Join(cacheDir, entry.Name()))
		}
	}

	go func() {
		for _, dir := range stale {
			_ = removeCacheDir(cacheDir, dir)
		}
	}()

	return nil
}

func isStale(name string) bool {
	return strings.HasPrefix(name, copyPrefix) || strings.HasPrefix(name, picturesPrefix)
}

func (s *Service) findMod(modID string) (*workspace.Session, workspace.Mod, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, workspace.Mod{}, err
	}

	mod, ok := s.Workspace.LookupMod(modID)
	if !ok {
		return nil, workspace.Mod{}, fmt.Errorf("the mod %s is not in the open workspace", modID)
	}

	return ws, mod, nil
}

func toChoiceError(text string, choices []string, kind string) error {
	if slices.Contains(choices, text) {
		return nil
	}

	return fmt.Errorf("%q is not a %s: use %s", text, kind, strings.Join(choices, " or "))
}

func (s *Service) changeListing(modID, key string, change func(dir string) error) error {
	_, mod, err := s.findMod(modID)
	if err != nil {
		return err
	}

	dir, err := findListingDir(mod.Dir, key)
	if err != nil {
		return err
	}

	if err := change(dir); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)

	return nil
}

var previewTypes = []string{previewImage, previewVideo}

// AddPreview adds a picture or a video to a listing's gallery. It returns an
// error if the type is unknown or the gallery has the preview already.
func (s *Service) AddPreview(modID, key string, preview Preview) error {
	if err := toChoiceError(preview.Type, previewTypes, "type of preview"); err != nil {
		return err
	}

	return s.changeListing(modID, key, func(dir string) error {
		if preview.Type == previewImage {
			content, err := os.ReadFile(preview.Path)
			if err != nil {
				return err
			}

			return s.writePicture(dir, path.Join(previewsDir, filepath.Base(preview.Path)), content)
		}

		return s.addVideo(dir, preview.URL)
	})
}

func (s *Service) writePicture(dir, rel string, content []byte) error {
	target := files.Join(dir, rel)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("the listing has %s already", rel)
	}

	return s.writeFile(target, string(content))
}

func (s *Service) addVideo(dir, url string) error {
	urls, err := listVideos(dir)
	if err != nil {
		return err
	}

	if slices.Contains(urls, url) {
		return fmt.Errorf("the listing has the video %s already", url)
	}

	return s.writeVideos(dir, append(urls, url))
}

// RemovePreview removes a picture or a video from a listing's gallery. It
// returns an error if the type is unknown or the gallery lacks the preview.
func (s *Service) RemovePreview(modID, key string, preview Preview) error {
	if err := toChoiceError(preview.Type, previewTypes, "type of preview"); err != nil {
		return err
	}

	return s.changeListing(modID, key, func(dir string) error {
		if preview.Type == previewImage {
			return s.deletePicture(dir, preview.Path)
		}

		urls, err := listVideos(dir)
		if err != nil {
			return err
		}

		if !slices.Contains(urls, preview.URL) {
			return fmt.Errorf("the listing has no video %s", preview.URL)
		}

		return s.writeVideos(dir, slices.DeleteFunc(urls, func(listed string) bool { return listed == preview.URL }))
	})
}

func (s *Service) deletePicture(dir, picture string) error {
	if !files.IsUnder(files.Join(dir, previewsDir), picture) {
		return fmt.Errorf("%s is not in the listing's previews folder", picture)
	}

	if _, err := os.Stat(picture); err != nil {
		return err
	}
	_, err := s.Workspace.Delete(picture)

	return err
}

func (s *Service) writeVideos(dir string, urls []string) error {
	return s.writeFile(files.Join(dir, videosFile), strings.Join(urls, "\n")+"\n")
}

// FetchPreviews copies the gallery of a listing's Workshop item into the
// listing, adding what it lacks. It returns an error if the listing has no
// item.
func (s *Service) FetchPreviews(modID, key string) error {
	ws, _, listing, err := s.findListing(modID, key)
	if err != nil {
		return err
	}

	if listing.WorkshopID == "" {
		return fmt.Errorf("the listing of the mod %s has no Workshop item", modID)
	}

	s.removeAnswer(listing.WorkshopID)
	answer := s.resolveAnswer(ws, listing.WorkshopID)
	if answer.Steam == nil {
		return errors.New(answer.Reason)
	}

	return s.changeListing(modID, key, func(dir string) error { return s.addPreviews(dir, answer.Steam.Previews) })
}

func (s *Service) addPreviews(dir string, steamPreviews []Preview) error {
	current, err := listPreviews(dir)
	if err != nil {
		return err
	}

	labels := map[string]bool{}
	for _, preview := range current {
		labels[toLabel(preview)] = true
	}

	for _, preview := range steamPreviews {
		if labels[toLabel(preview)] {
			continue
		}

		if preview.Type == previewVideo {
			err = s.addVideo(dir, preview.URL)
		} else {
			err = s.fetchPicture(dir, preview)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// toLabel returns what tells one preview from another: a video's watch link, or
// a picture's file name without its extension.
func toLabel(preview Preview) string {
	if preview.Type == previewVideo {
		return watchLink + toVideoID(preview.URL)
	}
	name := filepath.Base(preview.Path)

	return strings.TrimSuffix(name, filepath.Ext(name))
}

var contentExtensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

func (s *Service) fetchPicture(dir string, preview Preview) error {
	content, err := app.FetchURL(preview.URL, time.Minute)
	if err != nil {
		return err
	}

	name := filepath.Base(preview.Path)
	if !slices.Contains(game.PictureExtensions, strings.ToLower(filepath.Ext(name))) {
		name += cmp.Or(contentExtensions[http.DetectContentType(content)], ".png")
	}

	return s.writePicture(dir, path.Join(previewsDir, name), content)
}

// DescribeSteam returns why Steam cannot take an upload for the workspace's
// game now, or "" if it can.
func (s *Service) DescribeSteam() string {
	ws, err := s.Workspace.Session()
	if err != nil {
		return err.Error()
	}

	params, err := s.buildParams(ws)
	if err != nil {
		return err.Error()
	}

	if err := startCopy(context.Background(), statusFlag, params, nil); err != nil {
		return err.Error()
	}

	return ""
}

// Publish uploads a mod's listing to the Workshop. An empty visibility keeps
// the item's own.
func (s *Service) Publish(ctx context.Context, modID, key, visibility string) error {
	if visibility != "" {
		if err := toChoiceError(visibility, visibilities, "visibility"); err != nil {
			return err
		}
	}

	ws, mod, listing, err := s.findListing(modID, key)
	if err != nil {
		return err
	}

	src, err := s.openSource(ws, mod, listing)
	if err != nil {
		return err
	}
	defer func() { _ = src.close() }()

	upload, _, err := s.buildUpload(mod, src)
	if err != nil {
		return err
	}

	params, err := s.buildParams(ws)
	if err != nil {
		return err
	}

	// Steam is sent copies, so it gets these files and no others.
	cacheDir := s.Workspace.Config.Dirs.Cache
	pictures, err := os.MkdirTemp(cacheDir, picturesPrefix)
	if err != nil {
		return err
	}
	defer func() { _ = removeCacheDir(cacheDir, pictures) }()

	if listing, err = resolvePictures(pictures, listing); err != nil {
		return err
	}

	if params.UploadDir, err = copyFiles(cacheDir, src, upload.Files); err != nil {
		return err
	}
	defer func() { _ = removeCacheDir(cacheDir, params.UploadDir) }()

	titles := s.listModTitles()
	params.WorkshopID, params.Listing, params.Visibility = listing.WorkshopID, listing, visibility
	params.ModWorkshopIDs = slices.AppendSeq(make([]string, 0, len(titles)), maps.Keys(titles))

	pmt := application.Get()
	var linkErr error
	err = StartPublish(ctx, params, func(step Progress) { pmt.Event.Emit(EventProgress, step) }, func(workshopID string) {
		linkErr = s.LinkItem(modID, key, workshopID)
	})

	// Steam's item may have changed, so the next comparison asks Steam again.
	s.removeAnswer(listing.WorkshopID)
	if err != nil && ctx.Err() != nil {
		err = errors.New("the upload was stopped: Steam may already have applied part of it")
	}

	return errors.Join(err, linkErr)
}

// listModTitles returns the titles of the workspace's mods by their Workshop
// ids, leaving out a mod that has none.
func (s *Service) listModTitles() map[string]string {
	titles := map[string]string{}
	for _, mod := range s.Workspace.ListMods() {
		if workshopID := mod.ResolveWorkshopID(); workshopID != "" {
			titles[workshopID] = mod.Descriptor.Name
		}
	}

	return titles
}

// buildParams returns a second copy's parameters for the workspace's game. It
// returns an error if the game has no Steam library on this system.
func (s *Service) buildParams(ws *workspace.Session) (Params, error) {
	library, ok := ws.Game.SteamLibrary[runtime.GOOS]
	if !ok {
		return Params{}, fmt.Errorf("%s has no Steam library on %s", ws.Game.Name, runtime.GOOS)
	}

	return Params{
		AppID: ws.Game.SteamAppID, LibraryPath: files.Join(ws.InstallPath, library),
	}, nil
}

// copyFiles copies files of a source into a new cache folder, each under its
// Rel path, and returns the folder. If a copy fails, it deletes the folder.
func copyFiles(cacheDir string, src source, list []files.File) (string, error) {
	dir, err := os.MkdirTemp(cacheDir, copyPrefix)
	if err != nil {
		return "", err
	}

	for _, file := range list {
		if err := copyFile(src, file.Rel, files.Join(dir, file.Rel)); err != nil {
			return "", errors.Join(err, removeCacheDir(cacheDir, dir))
		}
	}

	return dir, nil
}

func copyFile(src source, rel, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	content, err := src.open(rel)
	if err != nil {
		return err
	}
	defer func() { _ = content.Close() }()

	file, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, content)

	return errors.Join(err, file.Close())
}

// removeCacheDir deletes a file or folder in the cache. It deletes nothing for
// a path outside the cache, and returns an error.
func removeCacheDir(cacheDir, dir string) error {
	if !files.IsUnder(cacheDir, dir) {
		return fmt.Errorf("%s is not in PMT's cache folder, so it is not deleted", dir)
	}

	return os.RemoveAll(dir)
}
