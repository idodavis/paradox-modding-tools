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
)

// A Progress is one step of an upload, as the steam:progress event reports it.
type Progress struct {
	Stage string `json:"stage"` // preparing, uploading, committing or done
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// A Client is a connection to the game's own copy of Steam's library, loaded in a second copy of the app.
type Client struct {
	ugc   uintptr // Steam's Workshop interface
	utils uintptr // Steam's utilities interface, for the results of calls
	appID int
}

// Steam's own numbers, as its Workshop headers give them.
const (
	resultOK               = 1              // the result code of a call that worked
	resultNotFound         = 9              // the result code of an item Steam does not find
	communityFileType      = 0              // the file type of a mod's item
	imagePreviewType       = 0              // the type of preview that is a picture
	videoPreviewType       = 1              // the type of preview that is a YouTube video
	subscriptionsStatistic = 0              // the statistic that counts an item's subscribers
	invalidHandle          = math.MaxUint64 // what an update or a query handle is when Steam made none
	invalidCall            = 0              // what a call is when Steam started none
	pollInterval           = 100 * time.Millisecond
	downloadTimeout        = 10 * time.Minute
	watchLink              = "https://www.youtube.com/watch?v=" // the link to watch a video, before its id
)

// The stages of an upload that a Progress names.
const (
	stagePreparing  = "preparing"
	stageUploading  = "uploading"
	stageCommitting = "committing"
	stageDone       = "done"
)

// The call results Steam fills in, each with its callback id, its size and the offsets of the fields read. Sizes and
// offsets after an 8-byte field follow the packing.
const (
	resultCode = 0

	createItemResultID        = 3403 // CreateItemResult_t
	createItemResultBytes     = 2*packBytes + 8
	createItemResultFile      = packBytes     // the id of the item created
	createItemResultAgreement = packBytes + 8 // whether the user must accept the Workshop's agreement

	submitItemUpdateResultID        = 3404 // SubmitItemUpdateResult_t
	submitItemUpdateResultBytes     = 16
	submitItemUpdateResultAgreement = 4

	addDependencyResultID    = 3412 // AddUGCDependencyResult_t
	removeDependencyResultID = 3413 // RemoveUGCDependencyResult_t
	dependencyResultBytes    = packBytes + 16

	addAppDependencyResultID    = 3414 // AddAppDependencyResult_t
	removeAppDependencyResultID = 3415 // RemoveAppDependencyResult_t
	appDependencyResultBytes    = 2*packBytes + 8

	appDependenciesResultID    = 3416 // GetAppDependenciesResult_t
	appDependenciesResultBytes = packBytes + 144
	appDependenciesResultApps  = packBytes + 8   // the app ids, room for 32
	appDependenciesResultCount = packBytes + 136 // how many of them it holds
	appDependenciesResultTotal = packBytes + 140 // how many the item requires, which may be more
	appDependenciesMax         = 32

	queryCompletedID    = 3401 // SteamUGCQueryCompleted_t
	queryCompletedBytes = 280
)

// The size of Steam's SteamUGCDetails_t, with room to spare, and the offsets of the fields read.
const (
	detailsBytes       = 10 << 10
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
	detailsFile        = (detailsTags + detailsTagsBytes + packBytes - 1) / packBytes * packBytes
	detailsChildren    = detailsFile + 2*8 + 260 + 2*4 + 256 + 3*4
)

// errAgreement is the error for a user who has not accepted the Workshop's legal agreement.
var errAgreement = errors.New("the Steam user has not accepted the Workshop's legal agreement yet")

// visibilities are the words of a listing's visibility, in the order of Steam's numbers for them.
var visibilities = []string{"public", "friends", "private", "unlisted"}

// updateStages are the stages that Steam's update statuses fall into, indexed by the status. Status 0 is no job.
var updateStages = []string{"", stagePreparing, stagePreparing, stageUploading, stageUploading, stageCommitting}

// api holds Steam's functions, each bound to the loaded library once.
var api struct {
	ugcInterface        func() uintptr
	utilsInterface      func() uintptr
	userInterface       func() uintptr
	isLoggedOn          func(user uintptr) bool
	initialize          func() bool
	shutdown            func()
	runCallbacks        func()
	isCompleted         func(utils uintptr, call uint64, failed *bool) bool
	getResult           func(utils uintptr, call uint64, result unsafe.Pointer, size, callbackID int32, failed *bool) bool
	createItem          func(ugc uintptr, appID uint32, fileType int32) uint64
	startUpdate         func(ugc uintptr, appID uint32, id uint64) uint64
	setTitle            func(ugc uintptr, update uint64, text string) bool
	setDescription      func(ugc uintptr, update uint64, text string) bool
	setTags             func(ugc uintptr, update uint64, tags unsafe.Pointer) bool
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
	getAppDependencies  func(ugc uintptr, id uint64) uint64
	addDependency       func(ugc uintptr, parent, child uint64) uint64
	removeDependency    func(ugc uintptr, parent, child uint64) uint64
	createQuery         func(ugc uintptr, ids *uint64, count uint32) uint64
	sendQuery           func(ugc uintptr, query uint64) uint64
	getQueryResult      func(ugc uintptr, query uint64, index uint32, details unsafe.Pointer) bool
	getStatistic        func(ugc uintptr, query uint64, index uint32, statistic int32, value *uint64) bool
	releaseQuery        func(ugc uintptr, query uint64) bool
	setLongDescription  func(ugc uintptr, query uint64, enable bool) bool
	setPreviewsReturned func(ugc uintptr, query uint64, enable bool) bool
	setChildrenReturned func(ugc uintptr, query uint64, enable bool) bool
	countPreviews       func(ugc uintptr, query uint64, index uint32) uint32
	getPreview          func(ugc uintptr, query uint64, index, preview uint32, address *byte, addressBytes uint32,
		name *byte, nameBytes uint32, previewType *int32) bool
	getChildren  func(ugc uintptr, query uint64, index uint32, ids *uint64, count uint32) bool
	downloadItem func(ugc uintptr, id uint64, highPriority bool) bool
	getInstall   func(ugc uintptr, id uint64, size *uint64, folder *byte, folderBytes uint32, stamp *uint32) bool
}

// bindings maps the name of each of Steam's functions to its field of api.
var bindings = map[string]any{
	"SteamAPI_SteamUGC_v016":                              &api.ugcInterface,
	"SteamAPI_SteamUtils_v010":                            &api.utilsInterface,
	"SteamAPI_SteamUser_v021":                             &api.userInterface,
	"SteamAPI_ISteamUser_BLoggedOn":                       &api.isLoggedOn,
	"SteamAPI_Init":                                       &api.initialize,
	"SteamAPI_Shutdown":                                   &api.shutdown,
	"SteamAPI_RunCallbacks":                               &api.runCallbacks,
	"SteamAPI_ISteamUtils_IsAPICallCompleted":             &api.isCompleted,
	"SteamAPI_ISteamUtils_GetAPICallResult":               &api.getResult,
	"SteamAPI_ISteamUGC_CreateItem":                       &api.createItem,
	"SteamAPI_ISteamUGC_StartItemUpdate":                  &api.startUpdate,
	"SteamAPI_ISteamUGC_SetItemTitle":                     &api.setTitle,
	"SteamAPI_ISteamUGC_SetItemDescription":               &api.setDescription,
	"SteamAPI_ISteamUGC_SetItemTags":                      &api.setTags,
	"SteamAPI_ISteamUGC_SetItemContent":                   &api.setContent,
	"SteamAPI_ISteamUGC_SetItemPreview":                   &api.setPreview,
	"SteamAPI_ISteamUGC_AddItemPreviewFile":               &api.addPreviewFile,
	"SteamAPI_ISteamUGC_AddItemPreviewVideo":              &api.addPreviewVideo,
	"SteamAPI_ISteamUGC_RemoveItemPreview":                &api.removePreview,
	"SteamAPI_ISteamUGC_SetItemVisibility":                &api.setVisibility,
	"SteamAPI_ISteamUGC_SubmitItemUpdate":                 &api.submit,
	"SteamAPI_ISteamUGC_GetItemUpdateProgress":            &api.getProgress,
	"SteamAPI_ISteamUGC_AddAppDependency":                 &api.addAppDependency,
	"SteamAPI_ISteamUGC_RemoveAppDependency":              &api.removeAppDependency,
	"SteamAPI_ISteamUGC_CreateQueryUGCDetailsRequest":     &api.createQuery,
	"SteamAPI_ISteamUGC_SendQueryUGCRequest":              &api.sendQuery,
	"SteamAPI_ISteamUGC_GetQueryUGCResult":                &api.getQueryResult,
	"SteamAPI_ISteamUGC_GetQueryUGCStatistic":             &api.getStatistic,
	"SteamAPI_ISteamUGC_ReleaseQueryUGCRequest":           &api.releaseQuery,
	"SteamAPI_ISteamUGC_SetReturnLongDescription":         &api.setLongDescription,
	"SteamAPI_ISteamUGC_SetReturnAdditionalPreviews":      &api.setPreviewsReturned,
	"SteamAPI_ISteamUGC_GetQueryUGCNumAdditionalPreviews": &api.countPreviews,
	"SteamAPI_ISteamUGC_GetQueryUGCAdditionalPreview":     &api.getPreview,
	"SteamAPI_ISteamUGC_DownloadItem":                     &api.downloadItem,
	"SteamAPI_ISteamUGC_GetItemInstallInfo":               &api.getInstall,
}

// optionalBindings maps the functions that a library may lack to their fields of api. A missing one stays nil, and an
// upload then leaves the DLC and the required items as they are.
var optionalBindings = map[string]any{
	"SteamAPI_ISteamUGC_GetAppDependencies":  &api.getAppDependencies,
	"SteamAPI_ISteamUGC_AddDependency":       &api.addDependency,
	"SteamAPI_ISteamUGC_RemoveDependency":    &api.removeDependency,
	"SteamAPI_ISteamUGC_SetReturnChildren":   &api.setChildrenReturned,
	"SteamAPI_ISteamUGC_GetQueryUGCChildren": &api.getChildren,
}

// Open loads Steam's library and starts it for a game. It returns an error if the library does not load or lacks a
// function, or if Steam is not running and signed in.
func Open(libraryPath string, appID int) (*Client, error) {
	handle, err := loadLibrary(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("steam's library %s did not load: %w", libraryPath, err)
	}
	if err := os.Setenv("SteamAppId", strconv.Itoa(appID)); err != nil {
		return nil, err
	}
	for name, function := range bindings {
		if err := lookupFunction(handle, name, function); err != nil {
			return nil, err
		}
	}
	for name, function := range optionalBindings {
		_ = lookupFunction(handle, name, function)
	}
	if !api.initialize() {
		return nil, errors.New("steam is not running, or no user who owns the game is signed in to it")
	}
	return &Client{ugc: api.ugcInterface(), utils: api.utilsInterface(), appID: appID}, nil
}

// lookupFunction binds a field of api to a function of the loaded library. It returns an error if the library does not
// export the function.
func lookupFunction(handle uintptr, name string, function any) error {
	symbol, err := findSymbol(handle, name)
	if err != nil {
		return fmt.Errorf("steam's library has no %s: %w", name, err)
	}
	purego.RegisterFunc(function, symbol)
	return nil
}

// Close shuts the library down.
func (client *Client) Close() { api.shutdown() }

// isOnline reports whether a Steam user is logged on.
func (client *Client) isOnline() bool {
	user := api.userInterface()
	return user != 0 && api.isLoggedOn(user)
}

// toItemID returns a Workshop id as a number. It returns an error if the id is not a number.
func toItemID(workshopID string) (uint64, error) {
	id, err := strconv.ParseUint(workshopID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a Workshop item's id", workshopID)
	}
	return id, nil
}

// fetchQuery asks Steam for an item's page and returns the query's handle, which must be released. It returns an error
// if Steam does not answer.
func (client *Client) fetchQuery(id uint64) (uint64, error) {
	query := api.createQuery(client.ugc, &id, 1)
	if query == invalidHandle {
		return 0, errors.New("steam did not start the query")
	}
	api.setLongDescription(client.ugc, query, true)
	api.setPreviewsReturned(client.ugc, query, true)
	if api.setChildrenReturned != nil {
		api.setChildrenReturned(client.ugc, query, true)
	}
	if _, err := client.fetchResult(api.sendQuery(client.ugc, query), queryCompletedID, queryCompletedBytes, nil); err != nil {
		api.releaseQuery(client.ugc, query)
		return 0, err
	}
	return query, nil
}

// FetchDetails fetches an item's details from Steam. It returns an error if the id is not a Workshop id, or Steam does
// not find the item or fails a call.
func (client *Client) FetchDetails(workshopID string) (Details, error) {
	id, err := toItemID(workshopID)
	if err != nil {
		return Details{}, err
	}
	query, err := client.fetchQuery(id)
	if err != nil {
		return Details{}, err
	}
	defer api.releaseQuery(client.ugc, query)
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
	details.Previews = client.listQueryPreviews(query)
	details.Dependencies = client.listChildren(query, raw)
	details.DLC, details.DLCCount, err = client.fetchDLC(id)
	return details, err
}

// listQueryPreviews returns the gallery of the item that an answered query is for.
func (client *Client) listQueryPreviews(query uint64) []Preview {
	previews := []Preview{}
	for index := range api.countPreviews(client.ugc, query, 0) {
		var address, name [1024]byte
		var previewType int32
		addressBytes, nameBytes := uint32(len(address)), uint32(len(name))
		if !api.getPreview(client.ugc, query, 0, index, &address[0], addressBytes, &name[0], nameBytes, &previewType) {
			continue
		}
		switch previewType {
		case imagePreviewType:
			previews = append(previews, Preview{Type: previewImage, URL: toText(address[:]), Path: toText(name[:])})
		case videoPreviewType:
			id := toText(address[:])
			previews = append(previews, Preview{Type: previewVideo, URL: watchLink + id, Video: id})
		}
	}
	return previews
}

// listChildren returns the Workshop ids of the items that a queried item requires. It returns nil if Steam's library
// cannot read or change them.
func (client *Client) listChildren(query uint64, raw []byte) []string {
	if !canChangeDependencies() {
		return nil
	}
	count := binary.LittleEndian.Uint32(raw[detailsChildren:])
	ids := make([]uint64, count)
	if count > 0 && !api.getChildren(client.ugc, query, 0, &ids[0], count) {
		return nil
	}
	return lo.Map(ids, func(id uint64, _ int) string { return strconv.FormatUint(id, 10) })
}

// canChangeDependencies reports whether Steam's library can both read the items an item requires and change them.
func canChangeDependencies() bool {
	return api.setChildrenReturned != nil && api.getChildren != nil && api.addDependency != nil &&
		api.removeDependency != nil
}

// fetchDLC returns the app ids of up to 32 DLCs that an item requires, and how many it requires in all. It returns nil
// if Steam's library cannot read them, and an error if Steam fails the call.
func (client *Client) fetchDLC(id uint64) ([]string, int, error) {
	if api.getAppDependencies == nil {
		return nil, 0, nil
	}
	call := api.getAppDependencies(client.ugc, id)
	result, err := client.fetchResult(call, appDependenciesResultID, appDependenciesResultBytes, nil)
	if err == nil {
		err = toResultError(result, resultCode)
	}
	if err != nil {
		return nil, 0, err
	}
	dlc := make([]string, min(binary.LittleEndian.Uint32(result[appDependenciesResultCount:]), appDependenciesMax))
	for position := range dlc {
		appID := binary.LittleEndian.Uint32(result[appDependenciesResultApps+4*position:])
		dlc[position] = strconv.FormatUint(uint64(appID), 10)
	}
	total := int(binary.LittleEndian.Uint32(result[appDependenciesResultTotal:]))
	return dlc, max(total, len(dlc)), nil
}

// fetchFolder has Steam download an item and returns its folder once the copy is up to date. It returns an error if the
// id is not a number, or Steam refuses or does not finish the download.
func (client *Client) fetchFolder(workshopID string, updatedAt time.Time) (string, error) {
	id, err := toItemID(workshopID)
	if err != nil {
		return "", err
	}
	if !api.downloadItem(client.ugc, id, true) {
		return "", errors.New("steam refused to download the item")
	}
	folder := make([]byte, 1024)
	for deadline := time.Now().Add(downloadTimeout); time.Now().Before(deadline); time.Sleep(pollInterval) {
		api.runCallbacks()
		var size uint64
		var stamp uint32
		if api.getInstall(client.ugc, id, &size, &folder[0], uint32(len(folder)), &stamp) && int64(stamp) >= updatedAt.Unix() {
			return toText(folder), nil
		}
	}
	return "", errors.New("steam did not finish downloading the item")
}

// decodeDetails decodes an item's page from the bytes Steam filled in.
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

// CreateItem creates a Workshop item for the app and returns its id. It returns the id with an error if the Steam user
// has not accepted the Workshop's legal agreement yet.
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

// Submit uploads a folder and a listing to a Workshop item, reporting each step. It returns an error if Steam refuses
// a part.
func (client *Client) Submit(workshopID string, params Params, progress func(Progress)) error {
	id, err := toItemID(workshopID)
	if err != nil {
		return err
	}
	progress(Progress{Stage: stagePreparing})
	// Steam may not answer a query for an item it just made, which has nothing yet
	details := Details{Previews: []Preview{}, DLC: []string{}}
	if canChangeDependencies() {
		details.Dependencies = []string{}
	}
	if params.WorkshopID != "" {
		if details, err = client.FetchDetails(workshopID); err != nil {
			return err
		}
	}
	listing := params.Listing
	if err := client.setDependencies(id, listing, details, params.ModWorkshopIDs); err != nil {
		return err
	}
	update := api.startUpdate(client.ugc, uint32(client.appID), id)
	if update == invalidHandle {
		return errors.New("steam did not start the update")
	}
	err = errors.Join(
		client.setTexts(update, listing),
		client.setVisibility(update, params.Visibility),
		client.setFiles(update, listing, details.Previews, params.UploadDir),
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

// setDependencies sets the DLC and the items that an item requires to the listing's. What Steam's library cannot read
// is left as it is. It returns an error if Steam fails a change.
func (client *Client) setDependencies(id uint64, listing Listing, details Details, modIDs []string) error {
	var errs []error
	if details.DLC != nil {
		added, removed := diffDLC(listing.DLC, details)
		errs = append(errs,
			client.applyChanges(removed, func(dlc uint64) uint64 {
				return api.removeAppDependency(client.ugc, id, uint32(dlc))
			}, removeAppDependencyResultID, appDependencyResultBytes),
			client.applyChanges(added, func(dlc uint64) uint64 {
				return api.addAppDependency(client.ugc, id, uint32(dlc))
			}, addAppDependencyResultID, appDependencyResultBytes),
		)
	}
	if details.Dependencies != nil {
		added, removed := diffRequired(listing.Dependencies, details.Dependencies, modIDs)
		errs = append(errs,
			client.applyChanges(removed, func(child uint64) uint64 {
				return api.removeDependency(client.ugc, id, child)
			}, removeDependencyResultID, dependencyResultBytes),
			client.applyChanges(added, func(child uint64) uint64 {
				return api.addDependency(client.ugc, id, child)
			}, addDependencyResultID, dependencyResultBytes),
		)
	}
	return errors.Join(errs...)
}

// diffTags returns the tags that an upload adds to an item and the ones it removes. Two tags that differ only in case
// are the same tag, as they are to Steam, which keeps a tag in the case it already knows.
func diffTags(sent, steam []string) (added, removed []string) {
	missing := func(tags []string) func(string, int) bool {
		return func(tag string, _ int) bool {
			return !slices.ContainsFunc(tags, func(one string) bool { return strings.EqualFold(one, tag) })
		}
	}
	return lo.Filter(sent, missing(steam)), lo.Filter(steam, missing(sent))
}

// diffDLC returns the DLCs that an upload adds to an item and the ones it removes. It removes none if Steam reported
// only some of the item's DLCs.
func diffDLC(dlc []string, details Details) (added, removed []string) {
	added, removed = lo.Difference(dlc, details.DLC)
	if details.DLCCount > len(details.DLC) {
		removed = nil
	}
	return added, removed
}

// diffRequired returns the items that an upload adds to an item's required items and the ones it removes. It removes
// only the workspace's own mods.
func diffRequired(dependencies []Dependency, steam, modIDs []string) (added, removed []string) {
	added, removed = lo.Difference(listRequired(dependencies), steam)
	return added, lo.Intersect(removed, modIDs)
}

// diffLabels returns the labels of the previews that an upload adds to Steam's gallery and the ones it removes.
func diffLabels(previews, steam []Preview) (added, removed []string) {
	return lo.Difference(listLabels(previews), listLabels(steam))
}

// applyChanges calls one of Steam's functions that change what an item requires, once per id, and waits for each
// result. It returns an error if an id is not a number or Steam fails a call.
func (client *Client) applyChanges(ids []string, change func(id uint64) uint64, callbackID, size int32) error {
	for _, text := range ids {
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return fmt.Errorf("%q is not a Steam id", text)
		}
		result, err := client.fetchResult(change(value), callbackID, size, nil)
		if err == nil {
			err = toResultError(result, resultCode)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// setTexts sets the title, the description and the tags of an update. An empty description keeps Steam's own. It
// returns an error if Steam refuses a text.
func (client *Client) setTexts(update uint64, listing Listing) error {
	errs := []error{toError(api.setTitle(client.ugc, update, listing.Title), "title")}
	if listing.Description != "" {
		errs = append(errs, toError(api.setDescription(client.ugc, update, listing.Description), "description"))
	}
	tags := toSentTags(listing)
	pointers := make([]*byte, len(tags))
	for position, tag := range tags {
		var err error
		if pointers[position], err = syscall.BytePtrFromString(tag); err != nil {
			return fmt.Errorf("the tag %q is not text Steam takes: %w", tag, err)
		}
	}
	// Steam's array of strings is a pointer to the strings and their count
	array := [2]uintptr{uintptr(unsafe.Pointer(unsafe.SliceData(pointers))), uintptr(len(pointers))}
	errs = append(errs, toError(api.setTags(client.ugc, update, unsafe.Pointer(&array)), "tags"))
	runtime.KeepAlive(pointers)
	return errors.Join(errs...)
}

// setVisibility sets the visibility of an update. An empty one keeps the item's own. Steam refuses an unknown one.
func (client *Client) setVisibility(update uint64, visibility string) error {
	if visibility == "" {
		return nil
	}
	number := slices.Index(visibilities, visibility)
	return toError(api.setVisibility(client.ugc, update, int32(number)), "visibility")
}

// setFiles sets the content folder, the thumbnail and the gallery of an update. A changed gallery replaces all of
// Steam's previews. It returns an error if Steam refuses a part.
func (client *Client) setFiles(update uint64, listing Listing, previous []Preview, uploadDir string) error {
	errs := []error{toError(api.setContent(client.ugc, update, uploadDir), "content folder")}
	if listing.Thumbnail != "" {
		errs = append(errs, toError(api.setPreview(client.ugc, update, listing.Thumbnail), "thumbnail"))
	}
	if added, removed := diffLabels(listing.Previews, previous); len(added)+len(removed) == 0 {
		return errors.Join(errs...)
	}
	for index := range slices.Backward(previous) {
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

// toVideoID returns the id of a video from its address. A text that is not a video's address is returned as it is.
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

// fetchResult waits for one of Steam's calls to finish and returns its result. While it waits it calls a function, if
// any. It returns an error if Steam started no call or the call failed.
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

// toResultError returns an error unless a result's code says the call worked. An item Steam does not find gets an
// error of its own.
func toResultError(result []byte, offset int) error {
	switch code := binary.LittleEndian.Uint32(result[offset:]); code {
	case resultOK:
		return nil
	case resultNotFound:
		return errors.New("steam does not find the item on the Workshop")
	default:
		return fmt.Errorf("steam answered with the result code %d", code)
	}
}
