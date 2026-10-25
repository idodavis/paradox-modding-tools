package steam

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/workspace"
)

// The events the Workshop service sends.
const (
	EventProgress       = "steam:progress"  // an upload's progress; the data is a Progress
	EventListingChanged = "release:changed" // a mod's listing files were written; the data is the mod's id
)

// init registers the types of the events' data with Wails.
func init() {
	application.RegisterEvent[Progress](EventProgress)
	application.RegisterEvent[string](EventListingChanged)
}

// Service is the Wails service that publishes mods to the Steam Workshop.
type Service struct {
	Workspace *workspace.Service
}

// ServePublish does a second copy's work if the app was started as one, by the flag it was started with. It reports
// whether it did and the exit code to exit with.
func ServePublish(args []string) (exitCode int, served bool) {
	for flag, work := range copyWork {
		if slices.Contains(args, flag) {
			return runCopy(work), true
		}
	}
	return 0, false
}

// findMod returns the open workspace and one of its mods, with the record the config holds now. Nothing is read from
// the mod's folder, so its descriptor is the one the workspace read when it opened. It returns an error if no
// workspace is open or the workspace does not have the mod.
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

// CreateDescriptionFile creates a mod's description file in the given source format, markdown or bbcode. It does
// nothing if the mod has a description file already. It returns an error if the source is neither format or the
// workspace has no such mod.
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
// its URL. It returns an error if the preview has no known type, the folder has a picture of that name already or the
// video is listed already.
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
	content, err := os.ReadFile(picture)
	if err != nil {
		return err
	}
	return stm.writePicture(dir, path.Join(previewsDir, filepath.Base(picture)), content)
}

// writePicture writes a picture into a mod's folder. It returns an error if there is a file at that path already.
func (stm *Service) writePicture(dir, rel string, content []byte) error {
	target := files.Join(dir, rel)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("the mod has %s already", rel)
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

// RemovePreview deletes a picture from a mod's previews folder, or removes a video from its list. It returns an error
// if the preview has no known type or is not there.
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
// is outside that folder or does not exist.
func (stm *Service) deletePicture(dir, picture string) error {
	if !files.IsUnder(files.Join(dir, previewsDir), picture) {
		return fmt.Errorf("%s is not in the mod's previews folder", picture)
	}
	if _, err := os.Stat(picture); err != nil {
		return err
	}
	_, err := stm.Workspace.Delete(picture)
	return err
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

// CompareUpload compares what a mod's next upload would send with the last upload and with what Steam has. Steam's
// details are nil if the mod has no item yet or the second copy cannot read them.
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
	snapshot, _ := LoadSnapshot(stm.Workspace.Config.Dirs.Cache, listing.WorkshopID)
	var details *Details
	if params, err := stm.buildParams(ws); err == nil && listing.WorkshopID != "" {
		params.WorkshopID = listing.WorkshopID
		details = fetchDetails(params)
	}
	return Compare(listing, upload, snapshot, details), nil
}

// CompareFile returns a file of a mod's upload as the last upload sent it and as the mod has it now, in that order,
// by its path from the mod's folder. A side without the file is "". It returns an error if the workspace has no such
// mod or a file cannot be read.
func (stm *Service) CompareFile(modID, rel string) ([]string, error) {
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return nil, err
	}
	snapshot, _ := LoadSnapshot(stm.Workspace.Config.Dirs.Cache, cmp.Or(mod.WorkshopID, mod.Descriptor.WorkshopID))
	sides := []string{"", ""}
	for position, dir := range []string{snapshot.Dir, mod.Dir} {
		if dir == "" {
			continue
		}
		text, err := files.ReadText(files.Join(dir, rel))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sides[position] = text.Text
	}
	return sides, nil
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
	if err := startCopy(statusFlag, params, nil); err != nil {
		return err.Error()
	}
	return ""
}

// FetchItem downloads a mod's Workshop item as the mod's snapshot and returns the next upload compared with it. It
// returns an error if the mod has no item or the download fails.
func (stm *Service) FetchItem(modID string) (Comparison, error) {
	params, err := stm.buildItemParams(modID)
	if err != nil {
		return Comparison{}, err
	}
	if err := startCopy(downloadFlag, params, nil); err != nil {
		return Comparison{}, err
	}
	return stm.CompareUpload(modID)
}

// FetchPreviews copies the pictures and videos of a mod's Workshop item that the mod lacks into its gallery. It
// returns an error if the mod has no item, a download fails or the gallery would pass what Steam takes.
func (stm *Service) FetchPreviews(modID string) error {
	params, err := stm.buildItemParams(modID)
	if err != nil {
		return err
	}
	var fetched []Preview
	err = startCopy(previewsFlag, params, func(line string) { _ = json.Unmarshal([]byte(line), &fetched) })
	if err != nil {
		return err
	}
	_, mod, err := stm.findMod(modID)
	if err != nil {
		return err
	}
	have, err := listPreviews(mod.Dir)
	if err != nil {
		return err
	}
	held := lo.SliceToMap(have, func(preview Preview) (string, bool) { return toLabel(preview), true })
	count := len(have)
	for _, preview := range fetched {
		if held[toLabel(preview)] {
			continue
		}
		switch {
		case preview.Type == previewVideo:
			err = stm.addVideo(mod.Dir, preview.URL)
		case count >= maxPreviews:
			err = fmt.Errorf("the gallery holds %d previews already, which is all Steam takes", maxPreviews)
		default:
			err = stm.fetchPicture(mod.Dir, preview)
		}
		if err != nil {
			return err
		}
		count++
	}
	application.Get().Event.Emit(EventListingChanged, modID)
	return nil
}

// toLabel returns what tells one preview of a gallery from another: a picture's file name or a video's id.
func toLabel(preview Preview) string {
	if preview.Type == previewVideo {
		return previewVideo + ":" + toVideoID(preview.URL)
	}
	return previewImage + ":" + filepath.Base(preview.Path)
}

// fetchPicture downloads one of Steam's gallery pictures into a mod's previews folder, under Steam's file name. A name
// with no picture extension gets the one of the content Steam serves, else .png.
func (stm *Service) fetchPicture(dir string, preview Preview) error {
	client := http.Client{Timeout: time.Minute}
	response, err := client.Get(preview.URL)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", preview.URL, response.Status)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	name := filepath.Base(preview.Path)
	if !slices.Contains(game.PictureExtensions, strings.ToLower(filepath.Ext(name))) {
		name += cmp.Or(contentExtensions[response.Header.Get("Content-Type")], ".png")
	}
	return stm.writePicture(dir, path.Join(previewsDir, name), content)
}

// buildItemParams returns a second copy's parameters for a mod's Workshop item. It returns an error if the mod has no
// item or the game has no Steam library on this system.
func (stm *Service) buildItemParams(modID string) (Params, error) {
	ws, _, err := stm.findMod(modID)
	if err != nil {
		return Params{}, err
	}
	listing, err := stm.ReadListing(modID)
	if err != nil {
		return Params{}, err
	}
	if listing.WorkshopID == "" {
		return Params{}, fmt.Errorf("the mod %s has no Workshop item", modID)
	}
	params, err := stm.buildParams(ws)
	params.WorkshopID = listing.WorkshopID
	return params, err
}

// Publish uploads a mod to the Workshop in a second copy of the app. The visibility is private, friends, public or
// unlisted, or "" to keep the item's own. The steam:progress event reports each stage. For a mod with no item yet, the
// new item's id is saved in the mod's record, even if the upload then fails. It returns an error if the visibility is
// none of those or the upload fails.
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
	if params.UploadDir, err = createUploadDir(stm.Workspace.Config.Dirs.Cache, upload); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(params.UploadDir) }()
	listing.Visibility = visibility
	params.WorkshopID, params.Listing = listing.WorkshopID, listing
	pmt := application.Get()
	workshopID, err := StartPublish(params, func(step Progress) { pmt.Event.Emit(EventProgress, step) })
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

// createUploadDir copies the files of an upload into a new folder under the given one, so that Steam is sent those
// files and no others. It returns the new folder's path, and removes the folder if the copy fails.
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
