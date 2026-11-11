// steam.go is the Wails service that publishes mods to the Steam Workshop.

package steam

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/singleflight"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/git"
	"paradox-modding-tools/services/workspace"
)

// The events the Workshop service sends.
const (
	EventProgress       = "steam:progress"  // the progress of an upload, as a Progress
	EventListingChanged = "release:changed" // a mod's listing files were written, with the mod's id
)

// The names of what the service keeps in PMT's cache: an item's snapshot, and the folders an upload copies its files
// and pictures into, which it deletes when it ends.
const (
	snapshotPrefix = "snapshot-"
	copyPrefix     = "copy-"
	picturesPrefix = "pictures-"
)

// init registers the type of each event's data with Wails.
func init() {
	application.RegisterEvent[Progress](EventProgress)
	application.RegisterEvent[string](EventListingChanged)
}

// Service is the Wails service that publishes mods to the Steam Workshop.
type Service struct {
	Workspace  *workspace.Service    // the open workspace
	Git        *git.Service          // reads a mod's commits for a change note's draft
	flight     singleflight.Group    // runs one ask of Steam per item, and one fetch of tags per game, at a time
	mu         sync.Mutex            // guards the maps below
	answers    map[string]Comparison // Steam's side of each item, by Workshop id
	hashes     map[string]string     // the SHA-256 of each file, by its path, size and time
	lines      map[string]Difference // a file's added and removed lines, by the hashes of its two copies
	tagFetches map[int]bool          // the app ids of the games whose tags were fetched in this run
}

// ServePublish does a second copy's work if the app was started as one. It returns the exit code and whether it did.
func ServePublish(args []string) (exitCode int, served bool) {
	for flag, work := range copyWork {
		if slices.Contains(args, flag) {
			return runCopy(work), true
		}
	}
	return 0, false
}

// ServiceStartup deletes, in the background, the copies an upload left in
// PMT's cache when the app stopped during it. A cache folder it cannot read
// stays as it is.
func (stm *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	cacheDir := stm.Workspace.Config.Dirs.Cache
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	stale := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (string, bool) {
		return filepath.Join(cacheDir, entry.Name()), isStale(entry.Name())
	})
	go func() {
		for _, dir := range stale {
			_ = removeCacheDir(cacheDir, dir)
		}
	}()
	return nil
}

// isStale reports whether an entry of PMT's cache is an upload's copy, which
// only an upload that ended early leaves.
func isStale(name string) bool {
	return strings.HasPrefix(name, copyPrefix) || strings.HasPrefix(name, picturesPrefix)
}

// findMod returns the open workspace and one of its mods. It returns an error if no workspace is open or it has no
// such mod.
func (stm *Service) findMod(modID string) (*workspace.Session, workspace.Mod, error) {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return nil, workspace.Mod{}, err
	}
	mod, ok := stm.Workspace.LookupMod(modID)
	if !ok {
		return nil, workspace.Mod{}, fmt.Errorf("the mod %s is not in the open workspace", modID)
	}
	return ws, mod, nil
}

// toChoiceError returns an error that lists the choices, unless a text is one of them.
func toChoiceError(text string, choices []string, kind string) error {
	if slices.Contains(choices, text) {
		return nil
	}
	return fmt.Errorf("%q is not a %s: use %s", text, kind, strings.Join(choices, " or "))
}

// changeListing runs a change on the folder of one of a mod's listings, then tells the page that the listing changed.
// It returns the change's error, or an error if the workspace has no such mod or listing.
func (stm *Service) changeListing(modID, key string, change func(dir string) error) error {
	_, mod, err := stm.findMod(modID)
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

// AddPreview adds a picture or a video to the gallery of one of a mod's listings. It returns an error if the preview's
// type is unknown, the mod has no such listing, or the gallery has the preview already.
func (stm *Service) AddPreview(modID, key string, preview Preview) error {
	if err := toChoiceError(preview.Type, previewTypes, "type of preview"); err != nil {
		return err
	}
	return stm.changeListing(modID, key, func(dir string) error {
		if preview.Type == previewImage {
			content, err := os.ReadFile(preview.Path)
			if err != nil {
				return err
			}
			return stm.writePicture(dir, path.Join(previewsDir, filepath.Base(preview.Path)), content)
		}
		return stm.addVideo(dir, preview.URL)
	})
}

// writePicture writes a picture into a listing's folder. It returns an error if the file exists already.
func (stm *Service) writePicture(dir, rel string, content []byte) error {
	target := files.Join(dir, rel)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("the listing has %s already", rel)
	}
	return stm.writeFile(target, string(content))
}

// addVideo adds a video to a listing's gallery. It returns an error if the gallery has it already.
func (stm *Service) addVideo(dir, url string) error {
	urls, err := listVideos(dir)
	if err != nil {
		return err
	}
	if slices.Contains(urls, url) {
		return fmt.Errorf("the listing has the video %s already", url)
	}
	return stm.writeVideos(dir, append(urls, url))
}

// RemovePreview removes a picture or a video from the gallery of one of a mod's listings. It returns an error if the
// preview's type is unknown, the mod has no such listing, or the gallery does not have the preview.
func (stm *Service) RemovePreview(modID, key string, preview Preview) error {
	if err := toChoiceError(preview.Type, previewTypes, "type of preview"); err != nil {
		return err
	}
	return stm.changeListing(modID, key, func(dir string) error {
		if preview.Type == previewImage {
			return stm.deletePicture(dir, preview.Path)
		}
		urls, err := listVideos(dir)
		if err != nil {
			return err
		}
		if !slices.Contains(urls, preview.URL) {
			return fmt.Errorf("the listing has no video %s", preview.URL)
		}
		return stm.writeVideos(dir, slices.DeleteFunc(urls, func(listed string) bool { return listed == preview.URL }))
	})
}

// deletePicture deletes a picture from a listing's previews folder. It returns an error if the picture is not there.
func (stm *Service) deletePicture(dir, picture string) error {
	if !files.IsUnder(files.Join(dir, previewsDir), picture) {
		return fmt.Errorf("%s is not in the listing's previews folder", picture)
	}
	if _, err := os.Stat(picture); err != nil {
		return err
	}
	_, err := stm.Workspace.Delete(picture)
	return err
}

// writeVideos writes the list of a listing's gallery videos.
func (stm *Service) writeVideos(dir string, urls []string) error {
	return stm.writeFile(files.Join(dir, videosFile), strings.Join(urls, "\n")+"\n")
}

// FetchPreviews copies the gallery of a listing's Workshop item into the listing, adding what it lacks. It returns an
// error if the listing has no item, Steam cannot be asked or a download fails.
func (stm *Service) FetchPreviews(modID, key string) error {
	ws, _, listing, err := stm.findListing(modID, key)
	if err != nil {
		return err
	}
	if listing.WorkshopID == "" {
		return fmt.Errorf("the listing of the mod %s has no Workshop item", modID)
	}
	stm.removeAnswer(listing.WorkshopID)
	answer := stm.resolveAnswer(ws, listing.WorkshopID)
	if answer.Steam == nil {
		return errors.New(answer.Reason)
	}
	return stm.changeListing(modID, key, func(dir string) error { return stm.addPreviews(dir, answer.Steam.Previews) })
}

// addPreviews adds each of the given previews that a listing's gallery lacks. It returns an error if a download fails.
func (stm *Service) addPreviews(dir string, steamPreviews []Preview) error {
	current, err := listPreviews(dir)
	if err != nil {
		return err
	}
	labels := lo.SliceToMap(current, func(preview Preview) (string, bool) { return toLabel(preview), true })
	for _, preview := range steamPreviews {
		if labels[toLabel(preview)] {
			continue
		}
		if preview.Type == previewVideo {
			err = stm.addVideo(dir, preview.URL)
		} else {
			err = stm.fetchPicture(dir, preview)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// toLabel returns what tells one preview from another: a video's watch link, or a picture's file name without its
// extension.
func toLabel(preview Preview) string {
	if preview.Type == previewVideo {
		return watchLink + toVideoID(preview.URL)
	}
	name := filepath.Base(preview.Path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// fetchPicture downloads one of Steam's gallery pictures into a listing's folder. It returns an error if the download
// fails or the picture is there already.
func (stm *Service) fetchPicture(dir string, preview Preview) error {
	content, err := app.FetchURL(preview.URL, time.Minute)
	if err != nil {
		return err
	}
	name := filepath.Base(preview.Path)
	if !slices.Contains(game.PictureExtensions, strings.ToLower(filepath.Ext(name))) {
		name += cmp.Or(contentExtensions[http.DetectContentType(content)], ".png")
	}
	return stm.writePicture(dir, path.Join(previewsDir, name), content)
}

// DescribeSteam returns why Steam cannot take an upload for the open workspace's game now, or "" if it can.
func (stm *Service) DescribeSteam() string {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return err.Error()
	}
	params, err := stm.buildParams(ws)
	if err != nil {
		return err.Error()
	}
	if err := startCopy(context.Background(), statusFlag, params, nil); err != nil {
		return err.Error()
	}
	return ""
}

// Publish uploads a mod's listing to the Workshop. An empty visibility keeps the item's own. It returns an error if
// the upload fails or is canceled.
func (stm *Service) Publish(ctx context.Context, modID, key, visibility string) error {
	if visibility != "" {
		if err := toChoiceError(visibility, visibilities, "visibility"); err != nil {
			return err
		}
	}
	ws, mod, listing, err := stm.findListing(modID, key)
	if err != nil {
		return err
	}
	src, err := stm.openSource(ws, mod, listing)
	if err != nil {
		return err
	}
	defer func() { _ = src.close() }()
	upload, _, err := stm.buildUpload(mod, src)
	if err != nil {
		return err
	}
	params, err := stm.buildParams(ws)
	if err != nil {
		return err
	}
	// Steam gets copies of the upload's files and pictures, and no others
	cacheDir := stm.Workspace.Config.Dirs.Cache
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
	params.WorkshopID, params.Listing, params.Visibility = listing.WorkshopID, listing, visibility
	params.ModWorkshopIDs = lo.Keys(stm.listModTitles())
	pmt := application.Get()
	var linkErr error
	err = StartPublish(ctx, params, func(step Progress) { pmt.Event.Emit(EventProgress, step) }, func(workshopID string) {
		linkErr = stm.LinkItem(modID, key, workshopID)
	})
	// Steam's item may have changed, so the next comparison asks Steam again
	stm.removeAnswer(listing.WorkshopID)
	if err != nil && ctx.Err() != nil {
		err = errors.New("the upload was stopped: Steam may already have applied part of it")
	}
	return errors.Join(err, linkErr)
}

// listModTitles returns the titles of the open workspace's mods by their Workshop ids. A mod with no Workshop id is
// left out.
func (stm *Service) listModTitles() map[string]string {
	return lo.FilterSliceToMap(stm.Workspace.ListMods(), func(mod workspace.Mod) (string, string, bool) {
		workshopID := mod.ResolveWorkshopID()
		return workshopID, mod.Descriptor.Name, workshopID != ""
	})
}

// buildParams returns a second copy's parameters for the open workspace's game. It returns an error if the game has
// no Steam library on this operating system.
func (stm *Service) buildParams(ws *workspace.Session) (Params, error) {
	library, ok := ws.Game.SteamLibrary[runtime.GOOS]
	if !ok {
		return Params{}, fmt.Errorf("%s has no Steam library on %s", ws.Game.Name, runtime.GOOS)
	}
	return Params{
		AppID: ws.Game.SteamAppID, LibraryPath: files.Join(ws.InstallPath, library),
	}, nil
}

// copyFiles copies files of a source into a new folder in PMT's cache, each under its Rel path, and returns the
// folder. If a copy fails, it deletes the folder and returns the error.
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

// copyFile copies one file of a source to the target path, making its folder first.
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

// removeCacheDir deletes a file or a folder in PMT's cache. A missing one is no error. It returns an error and deletes
// nothing for a path outside the cache.
func removeCacheDir(cacheDir, dir string) error {
	if !files.IsUnder(cacheDir, dir) {
		return fmt.Errorf("%s is not in PMT's cache folder, so it is not deleted", dir)
	}
	return os.RemoveAll(dir)
}
