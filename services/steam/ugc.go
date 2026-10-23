package steam

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/samber/lo"

	"paradox-modding-tools/services/app"
)

// A Progress is one step of an upload, the data of the steam:progress event.
type Progress struct {
	Stage string `json:"stage"` // preparing, uploading, committing, done
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// A Client is the game's own copy of Steam's library, loaded in the second copy of the app.
type Client struct {
	ugc   uintptr // Steam's Workshop interface
	utils uintptr // Steam's utilities interface, which answers when a call has finished
	appID int
}

// Steam's own numbers, as its Workshop headers give them.
const (
	resultOK               = 1              // the result code of a call that worked
	communityFileType      = 0              // the file type of a mod's item
	imagePreviewType       = 0              // the type of preview that is a picture
	subscriptionsStatistic = 0              // the statistic that counts an item's subscribers
	invalidHandle          = math.MaxUint64 // what an update or a query handle is when Steam made none
	invalidCall            = 0              // what a call is when Steam started none
	pollInterval           = 100 * time.Millisecond
)

// The stages of an upload that a Progress names.
const (
	stagePreparing  = "preparing"
	stageUploading  = "uploading"
	stageCommitting = "committing"
	stageDone       = "done"
)

// The call results Steam fills in, each with its callback id, its size and the offsets of the fields this reads. The
// results and the details are packed to packBytes, so their sizes and the offsets after an 8-byte field follow it.
// The result code is the first field of every call result.
const (
	resultCode = 0

	createItemResultID        = 3403 // CreateItemResult_t
	createItemResultBytes     = 2*packBytes + 8
	createItemResultFile      = packBytes     // the new item's id
	createItemResultAgreement = packBytes + 8 // whether the user must accept the Workshop's agreement

	submitItemUpdateResultID        = 3404 // SubmitItemUpdateResult_t
	submitItemUpdateResultBytes     = 16
	submitItemUpdateResultAgreement = 4

	addAppDependencyResultID    = 3414 // AddAppDependencyResult_t
	removeAppDependencyResultID = 3415 // RemoveAppDependencyResult_t
	appDependencyResultBytes    = 2*packBytes + 8

	queryCompletedID    = 3401 // SteamUGCQueryCompleted_t
	queryCompletedBytes = 280
)

// SteamUGCDetails_t: its size, with room to spare, and the offsets of the fields this reads. The text fields come
// before the first 8-byte field, the owner's id, so they sit at the same offsets in every packing.
const (
	detailsBytes       = 10 << 10 // the struct is a little under 9.8 KB
	detailsResult      = 8
	detailsTitle       = 24
	detailsTitleBytes  = 129
	detailsDescription = 153
	detailsDescBytes   = 8000
	detailsOwner       = (detailsDescription + detailsDescBytes + packBytes - 1) / packBytes * packBytes
	detailsUpdated     = detailsOwner + 12
	detailsVisibility  = detailsOwner + 20
	detailsTags        = detailsOwner + 27
	detailsTagsBytes   = 1025
)

// errAgreement is the error for a user who has not accepted the Workshop's legal agreement.
var errAgreement = errors.New("the Steam user has not accepted the Workshop's legal agreement yet")

// visibilities are the words of a listing's visibility, in the order of Steam's numbers for them.
var visibilities = []string{"public", "friends", "private", "unlisted"}

// updateStages are the stages that Steam's update statuses fall into, indexed by the status. Status 0 is no job.
var updateStages = []string{"", stagePreparing, stagePreparing, stageUploading, stageUploading, stageCommitting}

// api holds Steam's functions, which purego binds to the loaded library once.
var api struct {
	ugcInterface        func() uintptr
	utilsInterface      func() uintptr
	initialize          func() bool
	shutdown            func()
	runCallbacks        func()
	isCompleted         func(utils uintptr, call uint64, failed *bool) bool
	getResult           func(utils uintptr, call uint64, result unsafe.Pointer, size, callbackID int32, failed *bool) bool
	createItem          func(ugc uintptr, appID uint32, fileType int32) uint64
	startUpdate         func(ugc uintptr, appID uint32, id uint64) uint64
	setTitle            func(ugc uintptr, update uint64, text string) bool
	setDescription      func(ugc uintptr, update uint64, text string) bool
	setTags             func(ugc uintptr, update uint64, tags unsafe.Pointer, allowAdminTags bool) bool
	setContent          func(ugc uintptr, update uint64, dir string) bool
	setPreview          func(ugc uintptr, update uint64, file string) bool
	addPreviewFile      func(ugc uintptr, update uint64, file string, previewType int32) bool
	addPreviewVideo     func(ugc uintptr, update uint64, videoID string) bool
	removePreview       func(ugc uintptr, update uint64, index uint32) bool
	setVisibility       func(ugc uintptr, update uint64, visibility int32) bool
	submit              func(ugc uintptr, update uint64, note string) uint64
	getProgress         func(ugc uintptr, update uint64, done, total *uint64) int32
	addAppDependency    func(ugc uintptr, id uint64, appID uint32) uint64
	removeAppDependency func(ugc uintptr, id uint64, appID uint32) uint64
	createQuery         func(ugc uintptr, ids *uint64, count uint32) uint64
	sendQuery           func(ugc uintptr, query uint64) uint64
	getQueryResult      func(ugc uintptr, query uint64, index uint32, details unsafe.Pointer) bool
	getStatistic        func(ugc uintptr, query uint64, index uint32, statistic int32, value *uint64) bool
	releaseQuery        func(ugc uintptr, query uint64) bool
}

// bindings are the names of Steam's functions and the field of api that each one is bound to.
var bindings = map[string]any{
	"SteamAPI_SteamUGC_v016":                          &api.ugcInterface,
	"SteamAPI_SteamUtils_v010":                        &api.utilsInterface,
	"SteamAPI_Init":                                   &api.initialize,
	"SteamAPI_Shutdown":                               &api.shutdown,
	"SteamAPI_RunCallbacks":                           &api.runCallbacks,
	"SteamAPI_ISteamUtils_IsAPICallCompleted":         &api.isCompleted,
	"SteamAPI_ISteamUtils_GetAPICallResult":           &api.getResult,
	"SteamAPI_ISteamUGC_CreateItem":                   &api.createItem,
	"SteamAPI_ISteamUGC_StartItemUpdate":              &api.startUpdate,
	"SteamAPI_ISteamUGC_SetItemTitle":                 &api.setTitle,
	"SteamAPI_ISteamUGC_SetItemDescription":           &api.setDescription,
	"SteamAPI_ISteamUGC_SetItemTags":                  &api.setTags,
	"SteamAPI_ISteamUGC_SetItemContent":               &api.setContent,
	"SteamAPI_ISteamUGC_SetItemPreview":               &api.setPreview,
	"SteamAPI_ISteamUGC_AddItemPreviewFile":           &api.addPreviewFile,
	"SteamAPI_ISteamUGC_AddItemPreviewVideo":          &api.addPreviewVideo,
	"SteamAPI_ISteamUGC_RemoveItemPreview":            &api.removePreview,
	"SteamAPI_ISteamUGC_SetItemVisibility":            &api.setVisibility,
	"SteamAPI_ISteamUGC_SubmitItemUpdate":             &api.submit,
	"SteamAPI_ISteamUGC_GetItemUpdateProgress":        &api.getProgress,
	"SteamAPI_ISteamUGC_AddAppDependency":             &api.addAppDependency,
	"SteamAPI_ISteamUGC_RemoveAppDependency":          &api.removeAppDependency,
	"SteamAPI_ISteamUGC_CreateQueryUGCDetailsRequest": &api.createQuery,
	"SteamAPI_ISteamUGC_SendQueryUGCRequest":          &api.sendQuery,
	"SteamAPI_ISteamUGC_GetQueryUGCResult":            &api.getQueryResult,
	"SteamAPI_ISteamUGC_GetQueryUGCStatistic":         &api.getStatistic,
	"SteamAPI_ISteamUGC_ReleaseQueryUGCRequest":       &api.releaseQuery,
}

// Open loads Steam's library from the given path and initializes it for the game's app id. Steam must be running
// and signed in.
func Open(libraryPath string, appID int) (*Client, error) {
	handle, err := loadLibrary(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("steam's library %s did not load: %w", libraryPath, err)
	}
	if err := os.Setenv("SteamAppId", strconv.Itoa(appID)); err != nil {
		return nil, err
	}
	for name, function := range bindings {
		purego.RegisterLibFunc(function, handle, name)
	}
	if !api.initialize() {
		return nil, errors.New("steam did not start: it must be running with a signed-in user who owns the game")
	}
	return &Client{ugc: api.ugcInterface(), utils: api.utilsInterface(), appID: appID}, nil
}

// Close shuts the library down.
func (client *Client) Close() { api.shutdown() }

// FetchDetails reads an item's details from Steam.
func (client *Client) FetchDetails(workshopID string) (Details, error) {
	id, err := strconv.ParseUint(workshopID, 10, 64)
	if err != nil {
		return Details{}, fmt.Errorf("%q is not a Workshop item's id", workshopID)
	}
	query := api.createQuery(client.ugc, &id, 1)
	if query == invalidHandle {
		return Details{}, errors.New("steam did not start the query")
	}
	defer api.releaseQuery(client.ugc, query)
	if _, err := client.fetchResult(api.sendQuery(client.ugc, query), queryCompletedID, queryCompletedBytes, nil); err != nil {
		return Details{}, err
	}
	raw := make([]byte, detailsBytes)
	if !api.getQueryResult(client.ugc, query, 0, unsafe.Pointer(&raw[0])) {
		return Details{}, fmt.Errorf("steam has no details of the item %s", workshopID)
	}
	if err := toResultError(raw, detailsResult); err != nil {
		return Details{}, err
	}
	details := decodeDetails(raw)
	var subscribers uint64
	api.getStatistic(client.ugc, query, 0, subscriptionsStatistic, &subscribers)
	details.Subscribers = int(subscribers)
	return details, nil
}

// decodeDetails reads the fields of an item's details from the bytes Steam filled in.
func decodeDetails(raw []byte) Details {
	visibility := int(binary.LittleEndian.Uint32(raw[detailsVisibility:]))
	tags := toText(raw[detailsTags : detailsTags+detailsTagsBytes])
	details := Details{
		Title:       toText(raw[detailsTitle : detailsTitle+detailsTitleBytes]),
		Description: toText(raw[detailsDescription : detailsDescription+detailsDescBytes]),
		UpdatedAt:   time.Unix(int64(binary.LittleEndian.Uint32(raw[detailsUpdated:])), 0),
		Tags:        strings.FieldsFunc(tags, func(r rune) bool { return r == ',' }),
	}
	if visibility < len(visibilities) {
		details.Visibility = visibilities[visibility]
	}
	return details
}

// toText returns the text of a fixed-size field of Steam's, which ends at the first zero byte.
func toText(field []byte) string {
	text, _, _ := bytes.Cut(field, []byte{0})
	return string(text)
}

// CreateItem creates a new Workshop item for the app and returns its id.
func (client *Client) CreateItem() (string, error) {
	call := api.createItem(client.ugc, uint32(client.appID), communityFileType)
	result, err := client.fetchResult(call, createItemResultID, createItemResultBytes, nil)
	if err == nil {
		err = toResultError(result, resultCode)
	}
	if err != nil {
		return "", err
	}
	workshopID := strconv.FormatUint(binary.LittleEndian.Uint64(result[createItemResultFile:]), 10)
	if result[createItemResultAgreement] != 0 {
		return workshopID, errAgreement
	}
	return workshopID, nil
}

// Submit uploads a folder and a listing to an item, calling the progress function until Steam commits.
func (client *Client) Submit(workshopID string, listing Listing, uploadDir string, progress func(Progress)) error {
	id, err := strconv.ParseUint(workshopID, 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a Workshop item's id", workshopID)
	}
	dirs, err := app.ResolveDirs()
	if err != nil {
		return err
	}
	last, _ := LoadSnapshot(dirs.Cache, workshopID)
	progress(Progress{Stage: stagePreparing})
	if err := client.setDependencies(id, listing.DLC, last.Listing.DLC); err != nil {
		return err
	}
	update := api.startUpdate(client.ugc, uint32(client.appID), id)
	if update == invalidHandle {
		return errors.New("steam did not start the update")
	}
	err = errors.Join(
		client.setTexts(update, listing),
		client.setVisibility(update, listing.Visibility),
		client.setFiles(update, listing, last.Listing.Previews, uploadDir),
	)
	if err != nil {
		return err
	}
	var done, total uint64
	report := func() {
		if status := api.getProgress(client.ugc, update, &done, &total); status > 0 {
			stage := updateStages[min(int(status), len(updateStages)-1)]
			progress(Progress{Stage: stage, Done: int64(done), Total: int64(total)})
		}
	}
	call := api.submit(client.ugc, update, listing.Changelog)
	result, err := client.fetchResult(call, submitItemUpdateResultID, submitItemUpdateResultBytes, report)
	if err == nil && result[submitItemUpdateResultAgreement] != 0 {
		err = errAgreement
	}
	if err == nil {
		err = toResultError(result, resultCode)
	}
	if err != nil {
		return err
	}
	progress(Progress{Stage: stageDone, Done: int64(total), Total: int64(total)})
	return nil
}

// setDependencies makes the DLCs an item requires the given ones, by adding those the previous list did not have
// and removing those it had that the given list drops.
func (client *Client) setDependencies(id uint64, dlc, previous []string) error {
	added, removed := lo.Difference(dlc, previous)
	return errors.Join(
		client.changeDependencies(id, added, api.addAppDependency, addAppDependencyResultID),
		client.changeDependencies(id, removed, api.removeAppDependency, removeAppDependencyResultID),
	)
}

// changeDependencies calls one of Steam's functions that changes the DLCs an item requires, once for each of the
// given DLC ids, and waits for each call to end.
func (client *Client) changeDependencies(
	id uint64, dlc []string, change func(ugc uintptr, id uint64, appID uint32) uint64, callbackID int32,
) error {
	for _, dlcID := range dlc {
		appID, err := strconv.ParseUint(dlcID, 10, 32)
		if err != nil {
			return fmt.Errorf("%q is not a DLC's id", dlcID)
		}
		result, err := client.fetchResult(change(client.ugc, id, uint32(appID)), callbackID, appDependencyResultBytes, nil)
		if err == nil {
			err = toResultError(result, resultCode)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// setTexts sets the title, the description and the tags of an update. A listing whose source is Steam's own sends
// no description.
func (client *Client) setTexts(update uint64, listing Listing) error {
	errs := []error{toError(api.setTitle(client.ugc, update, listing.Title), "title")}
	if listing.Source != sourceSteam {
		errs = append(errs, toError(api.setDescription(client.ugc, update, listing.BBCode), "description"))
	}
	pointers := make([]*byte, len(listing.Tags))
	for position, tag := range listing.Tags {
		var err error
		if pointers[position], err = syscall.BytePtrFromString(tag); err != nil {
			return fmt.Errorf("the tag %q is not text Steam takes: %w", tag, err)
		}
	}
	// Steam's array of strings is a pointer to the strings and their count
	array := [2]uintptr{uintptr(unsafe.Pointer(unsafe.SliceData(pointers))), uintptr(len(pointers))}
	errs = append(errs, toError(api.setTags(client.ugc, update, unsafe.Pointer(&array), false), "tags"))
	runtime.KeepAlive(pointers)
	return errors.Join(errs...)
}

// setVisibility sets the visibility of an update to the given word. The empty word leaves the item's own. It
// returns an error for any other word that is not a visibility.
func (client *Client) setVisibility(update uint64, visibility string) error {
	if err := toVisibilityError(visibility); err != nil || visibility == "" {
		return err
	}
	number := slices.Index(visibilities, visibility)
	return toError(api.setVisibility(client.ugc, update, int32(number)), "visibility")
}

// toVisibilityError returns an error unless the given text is a visibility or is empty.
func toVisibilityError(visibility string) error {
	if visibility == "" || slices.Contains(visibilities, visibility) {
		return nil
	}
	return fmt.Errorf("%q is not a visibility: use %s", visibility, strings.Join(visibilities, ", "))
}

// setFiles sets the content folder, the thumbnail and the gallery of an update. When the listing's gallery differs
// from the previous one, the previous previews are removed, the last first, and the listing's are added.
func (client *Client) setFiles(update uint64, listing Listing, previous []Preview, uploadDir string) error {
	errs := []error{toError(api.setContent(client.ugc, update, uploadDir), "content folder")}
	if listing.Thumbnail != "" {
		errs = append(errs, toError(api.setPreview(client.ugc, update, listing.Thumbnail), "thumbnail"))
	}
	if slices.Equal(listing.Previews, previous) {
		return errors.Join(errs...)
	}
	for index := len(previous) - 1; index >= 0; index-- {
		errs = append(errs, toError(api.removePreview(client.ugc, update, uint32(index)), "removal of a preview"))
	}
	for _, preview := range listing.Previews {
		if preview.Type == previewVideo {
			errs = append(errs, toError(api.addPreviewVideo(client.ugc, update, toVideoID(preview.URL)), "video"))
		} else {
			errs = append(errs, toError(api.addPreviewFile(client.ugc, update, preview.Path, imagePreviewType), "picture"))
		}
	}
	return errors.Join(errs...)
}

// toError returns an error naming the part of an update that Steam did not take, or nil if it took it.
func toError(ok bool, part string) error {
	if ok {
		return nil
	}
	return fmt.Errorf("steam refused the %s", part)
}

// toVideoID returns the id of a video from its URL, which is what Steam takes for a gallery video. A text that is
// not a URL of a video is returned as it is.
func toVideoID(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}
	if id := parsed.Query().Get("v"); id != "" {
		return id
	}
	if parsed.Host == "youtu.be" {
		return strings.TrimPrefix(parsed.Path, "/")
	}
	return link
}

// fetchResult waits for one of Steam's calls to finish and returns the bytes of its result, which the given
// callback id and size describe. While it waits it runs Steam's callbacks and calls the given function, if any,
// every poll. It returns an error if Steam started no call or says the call failed.
func (client *Client) fetchResult(call uint64, callbackID, size int32, poll func()) ([]byte, error) {
	if call == invalidCall {
		return nil, errors.New("steam did not start the call")
	}
	for {
		var failed bool
		completed := api.isCompleted(client.utils, call, &failed)
		if failed {
			return nil, errors.New("steam failed the call")
		}
		if completed {
			break
		}
		api.runCallbacks()
		if poll != nil {
			poll()
		}
		time.Sleep(pollInterval)
	}
	var failed bool
	result := make([]byte, size)
	if !api.getResult(client.utils, call, unsafe.Pointer(&result[0]), size, callbackID, &failed) || failed {
		return nil, errors.New("steam could not return the result of the call")
	}
	return result, nil
}

// toResultError returns an error unless the result code at the given offset of a result says it worked.
func toResultError(result []byte, offset int) error {
	if code := binary.LittleEndian.Uint32(result[offset:]); code != resultOK {
		return fmt.Errorf("steam answered with the result code %d", code)
	}
	return nil
}
