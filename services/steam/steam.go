package steam

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The events the Workshop service sends.
const (
	EventProgress       = "steam:progress"  // an upload's progress; the data is a Progress
	EventListingChanged = "release:changed" // a mod's listing files were written; the data is the mod's id
)

func init() {
	application.RegisterEvent[Progress](EventProgress)
	application.RegisterEvent[string](EventListingChanged)
}

// Service is the Workshop service behind the Publish page: a mod's listing, the files its next upload sends, the
// compare with Steam, and publishing itself.
type Service struct {
	Config    *app.ConfigFile // the cache folder and the mods' records
	Workspace *workspace.Service
}

// ServePublish runs the second copy's work if the app was started as one: with the --steam-publish flag it
// uploads, and with the --steam-details flag it reads an item's details from Steam. It reports whether it did and
// the exit code to exit with.
func ServePublish(args []string) (exitCode int, served bool) {
	switch {
	case slices.Contains(args, publishFlag):
		return RunPublish(), true
	case slices.Contains(args, detailsFlag):
		return runDetails(), true
	}
	return 0, false
}

// findMod returns the open workspace and one of its mods. The mod's record is read from the config and its
// descriptor from its folder, so both are as the last write left them. It returns an error if no workspace is open,
// the workspace does not have the mod, or the mod's descriptor cannot be read.
func (stm *Service) findMod(modID string) (*workspace.Session, workspace.Mod, error) {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return nil, workspace.Mod{}, err
	}
	modRecord, ok := stm.Config.LookupMod(ws.ID, modID)
	if !ok {
		return nil, workspace.Mod{}, fmt.Errorf("the mod %s is not in the open workspace", modID)
	}
	descriptor, err := workspace.ReadDescriptor(ws.Game, modRecord.Dir)
	return ws, workspace.Mod{Mod: modRecord, Descriptor: descriptor}, err
}

// CreateDescriptionFile creates a mod's description file in the given source format, markdown or bbcode, if the
// mod has none yet.
func (stm *Service) CreateDescriptionFile(modID, source string) error {
	rel := ""
	for _, row := range descriptionFiles {
		if row.source == source {
			rel = row.rel
		}
	}
	if rel == "" {
		return fmt.Errorf("%q is not a source of a description: use %s or %s", source, sourceMarkdown, sourceBBCode)
	}
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	for _, row := range descriptionFiles {
		_, err := os.Stat(files.Join(mod.Dir, row.rel))
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := stm.writeFile(files.Join(mod.Dir, rel), ""); err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// AddPreview adds a preview to a mod: a picture is copied into the mod's previews folder, a video is recorded by
// its URL.
func (stm *Service) AddPreview(modID string, preview Preview) error {
	if err := toPreviewTypeError(preview.Type); err != nil {
		return err
	}
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	if preview.Type == previewImage {
		err = stm.copyPicture(mod.Dir, preview.Path)
	} else {
		err = stm.addVideo(mod.Dir, preview.URL)
	}
	if err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// copyPicture copies a picture into the previews folder of a mod's folder, keeping its file name. It returns an
// error if the folder has a picture of that name.
func (stm *Service) copyPicture(dir, picture string) error {
	target := files.Join(dir, path.Join(previewsDir, filepath.Base(picture)))
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("the mod's previews have a picture named %s already", filepath.Base(picture))
	}
	content, err := os.ReadFile(picture)
	if err != nil {
		return err
	}
	return stm.writeFile(target, string(content))
}

// addVideo adds a video to the list of a mod's gallery videos. It returns an error if the list has the URL.
func (stm *Service) addVideo(dir, url string) error {
	urls, err := listVideos(dir)
	if err != nil {
		return err
	}
	if slices.Contains(urls, url) {
		return fmt.Errorf("the mod has the video %s already", url)
	}
	return stm.writeVideos(dir, append(urls, url))
}

// RemovePreview deletes a preview from the mod's previews folder and its listing.
func (stm *Service) RemovePreview(modID string, preview Preview) error {
	if err := toPreviewTypeError(preview.Type); err != nil {
		return err
	}
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	if preview.Type == previewImage {
		err = stm.deletePicture(mod.Dir, preview.Path)
	} else {
		err = stm.deleteVideo(mod.Dir, preview.URL)
	}
	if err != nil {
		return err
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// deletePicture deletes a picture from the previews folder of a mod's folder. It returns an error if the picture
// is not there.
func (stm *Service) deletePicture(dir, picture string) error {
	if !files.IsUnder(files.Join(dir, previewsDir), picture) {
		return fmt.Errorf("%s is not in the mod's previews folder", picture)
	}
	if _, err := os.Stat(picture); err != nil {
		return err
	}
	return stm.Workspace.Delete(picture)
}

// deleteVideo deletes a video from the list of a mod's gallery videos. It returns an error if the list does not
// have the URL.
func (stm *Service) deleteVideo(dir, url string) error {
	urls, err := listVideos(dir)
	if err != nil {
		return err
	}
	if !slices.Contains(urls, url) {
		return fmt.Errorf("the mod has no video %s", url)
	}
	return stm.writeVideos(dir, slices.DeleteFunc(urls, func(listed string) bool { return listed == url }))
}

// toPreviewTypeError returns an error unless the given text is a type of preview.
func toPreviewTypeError(previewType string) error {
	if slices.Contains(previewTypes, previewType) {
		return nil
	}
	return fmt.Errorf("%q is not a type of preview: use %s", previewType, strings.Join(previewTypes, " or "))
}

// writeVideos writes the list of a mod's gallery videos, one URL per line.
func (stm *Service) writeVideos(dir string, urls []string) error {
	return stm.writeFile(files.Join(dir, videosFile), strings.Join(urls, "\n")+"\n")
}

// CompareUpload compares what a mod's next upload would send with the last upload and with what Steam has.
func (stm *Service) CompareUpload(modID string) (Comparison, error) {
	ws, _, err := stm.findMod(modID)
	if err != nil {
		return Comparison{}, err
	}
	listing, err := stm.ReadListing(modID)
	if err != nil {
		return Comparison{}, err
	}
	upload, err := stm.ListUpload(modID)
	if err != nil {
		return Comparison{}, err
	}
	snapshot, _ := LoadSnapshot(stm.Config.Dirs.Cache, listing.WorkshopID)
	var details *Details
	if params, err := stm.buildParams(ws); err == nil && listing.WorkshopID != "" {
		params.WorkshopID = listing.WorkshopID
		details = fetchDetails(params)
	}
	return Compare(listing, upload, snapshot, details), nil
}

// Publish uploads a mod to the Workshop in a second copy of the app, with the visibility chosen for this upload:
// private, friends, public or unlisted, or "" to keep the item's own. The steam:progress event reports each stage,
// and it returns when the upload ends. For a mod with no item yet, the new item's id is written to the mod's
// record.
func (stm *Service) Publish(modID, visibility string) error {
	if err := toVisibilityError(visibility); err != nil {
		return err
	}
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	listing, err := stm.ReadListing(modID)
	if err != nil {
		return err
	}
	upload, err := stm.ListUpload(modID)
	if err != nil {
		return err
	}
	params, err := stm.buildParams(ws)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if params.UploadDir, err = createUploadDir(stm.Config.Dirs.Cache, upload); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(params.UploadDir) }()
	listing.Visibility = visibility
	params.WorkshopID, params.Listing = listing.WorkshopID, listing
	pmt := application.Get()
	workshopID, err := StartPublish(executable, params, func(step Progress) { pmt.Event.Emit(EventProgress, step) })
	if workshopID != "" && listing.WorkshopID == "" {
		mod.WorkshopID = workshopID
		err = errors.Join(err, stm.Workspace.UpdateMod(ws.ID, mod.Mod))
	}
	return err
}

// buildParams returns the parameters of a second copy that name the open workspace's game: its id, its app id and
// the path of the game's copy of Steam's library in the workspace's install. It returns an error if the game has no
// such library on this operating system.
func (stm *Service) buildParams(ws *workspace.Session) (Params, error) {
	library, ok := ws.Game.SteamLibrary[runtime.GOOS]
	if !ok {
		return Params{}, fmt.Errorf("%s has no Steam library on %s", ws.Game.Name, runtime.GOOS)
	}
	return Params{
		Game: ws.Game.ID, AppID: ws.Game.SteamAppID, LibraryPath: files.Join(stm.Workspace.State().Install.Path, library),
	}, nil
}

// createUploadDir copies the files of an upload set into a new folder under the given one, so that Steam is sent
// those files and no others. It returns the new folder's path, and removes the folder if the copy fails.
func createUploadDir(cacheDir string, upload Upload) (string, error) {
	dir, err := os.MkdirTemp(cacheDir, "upload-")
	if err != nil {
		return "", err
	}
	for _, file := range upload.Files {
		target := files.Join(dir, file.Rel)
		content, err := os.ReadFile(file.Path)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(target), 0o755)
		}
		if err == nil {
			err = os.WriteFile(target, content, 0o644)
		}
		if err != nil {
			return "", errors.Join(err, os.RemoveAll(dir))
		}
	}
	return dir, nil
}

// ListRules returns the rules that say what an upload leaves out.
func (stm *Service) ListRules() []Rule { return Rules }
