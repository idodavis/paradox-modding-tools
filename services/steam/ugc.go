// ugc.go calls the Workshop functions of Steam's library.

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
)

// Progress is one step of an upload.
type Progress struct {
	// Stage is preparing, uploading, committing or done.
	Stage string `json:"stage"`

	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// Client is a connection to the game's own copy of Steam's library.
type Client struct {
	// ugc is Steam's Workshop interface, and utils its utilities interface.
	ugc   uintptr
	utils uintptr

	appID int
}

const (
	resultOK          = 1
	resultNotFound    = 9
	communityFileType = 0
	imagePreviewType  = 0
	videoPreviewType  = 1

	// invalidHandle and invalidCall are what Steam returns when it could not
	// create a handle or start a call.
	invalidHandle = math.MaxUint64
	invalidCall   = 0

	pollInterval    = 100 * time.Millisecond
	downloadTimeout = 10 * time.Minute
	watchLink       = "https://www.youtube.com/watch?v="
)

const (
	stagePreparing  = "preparing"
	stageUploading  = "uploading"
	stageCommitting = "committing"
	stageDone       = "done"
)

// The call results Steam fills in: callback ids, sizes and the offsets of the
// fields read. Offsets after an 8-byte field follow the packing.
const (
	resultCode = 0

	// CreateItemResult_t.
	createItemResultID        = 3403
	createItemResultBytes     = 2*packBytes + 8
	createItemResultFile      = packBytes
	createItemResultAgreement = packBytes + 8

	// SubmitItemUpdateResult_t.
	submitItemUpdateResultID        = 3404
	submitItemUpdateResultBytes     = 16
	submitItemUpdateResultAgreement = 4

	// AddUGCDependencyResult_t and RemoveUGCDependencyResult_t.
	addDependencyResultID    = 3412
	removeDependencyResultID = 3413
	dependencyResultBytes    = packBytes + 16

	// AddAppDependencyResult_t and RemoveAppDependencyResult_t.
	addAppDependencyResultID    = 3414
	removeAppDependencyResultID = 3415
	appDependencyResultBytes    = 2*packBytes + 8

	// GetAppDependenciesResult_t. It has room for 32 app ids, and its total may
	// be more than its count.
	appDependenciesResultID    = 3416
	appDependenciesResultBytes = packBytes + 144
	appDependenciesResultApps  = packBytes + 8
	appDependenciesResultCount = packBytes + 136
	appDependenciesResultTotal = packBytes + 140
	appDependenciesMax         = 32

	// SteamUGCQueryCompleted_t.
	queryCompletedID    = 3401
	queryCompletedBytes = 280
)

// The size of Steam's SteamUGCDetails_t, with room to spare, and its offsets.
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

var errAgreement = errors.New("the Steam user has not accepted the Workshop's legal agreement yet")

// visibilities are in the order of Steam's numbers for them.
var visibilities = []string{"public", "friends", "private", "unlisted"}

// api holds Steam's functions, each bound to the loaded library once.
var api struct {
	ugcInterface        func() uintptr
	utilsInterface      func() uintptr
	userInterface       func() uintptr
	isLoggedOn          func(user uintptr) bool
	steamID             func(user uintptr) uint64
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

// bindings maps each of Steam's function names to its field of api.
var bindings = map[string]any{
	"SteamAPI_SteamUGC_v016":                              &api.ugcInterface,
	"SteamAPI_SteamUtils_v010":                            &api.utilsInterface,
	"SteamAPI_SteamUser_v021":                             &api.userInterface,
	"SteamAPI_ISteamUser_BLoggedOn":                       &api.isLoggedOn,
	"SteamAPI_ISteamUser_GetSteamID":                      &api.steamID,
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
	"SteamAPI_ISteamUGC_ReleaseQueryUGCRequest":           &api.releaseQuery,
	"SteamAPI_ISteamUGC_SetReturnLongDescription":         &api.setLongDescription,
	"SteamAPI_ISteamUGC_SetReturnAdditionalPreviews":      &api.setPreviewsReturned,
	"SteamAPI_ISteamUGC_GetQueryUGCNumAdditionalPreviews": &api.countPreviews,
	"SteamAPI_ISteamUGC_GetQueryUGCAdditionalPreview":     &api.getPreview,
	"SteamAPI_ISteamUGC_DownloadItem":                     &api.downloadItem,
	"SteamAPI_ISteamUGC_GetItemInstallInfo":               &api.getInstall,
}

// optionalBindings maps the functions a library may lack. A missing one stays
// nil, and an upload then leaves the DLC and required items as they are.
var optionalBindings = map[string]any{
	"SteamAPI_ISteamUGC_GetAppDependencies":  &api.getAppDependencies,
	"SteamAPI_ISteamUGC_AddDependency":       &api.addDependency,
	"SteamAPI_ISteamUGC_RemoveDependency":    &api.removeDependency,
	"SteamAPI_ISteamUGC_SetReturnChildren":   &api.setChildrenReturned,
	"SteamAPI_ISteamUGC_GetQueryUGCChildren": &api.getChildren,
}

// Open loads Steam's library and starts it for a game. It returns an error if
// the library does not load or lacks a function, or Steam is not signed in.
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

func lookupFunction(handle uintptr, name string, function any) error {
	symbol, err := findSymbol(handle, name)
	if err != nil {
		return fmt.Errorf("steam's library has no %s: %w", name, err)
	}
	purego.RegisterFunc(function, symbol)

	return nil
}

// Close shuts the library down.
func (c *Client) Close() { api.shutdown() }

func toItemID(workshopID string) (uint64, error) {
	id, err := strconv.ParseUint(workshopID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a Workshop item's id", workshopID)
	}

	return id, nil
}

// FetchDetails fetches an item's details from Steam. It returns an error if
// Steam does not find the item.
func (c *Client) FetchDetails(workshopID string) (Details, error) {
	id, err := toItemID(workshopID)
	if err != nil {
		return Details{}, err
	}

	query := api.createQuery(c.ugc, &id, 1)
	if query == invalidHandle {
		return Details{}, errors.New("steam did not start the query")
	}
	defer api.releaseQuery(c.ugc, query)

	api.setLongDescription(c.ugc, query, true)
	api.setPreviewsReturned(c.ugc, query, true)
	if api.setChildrenReturned != nil {
		api.setChildrenReturned(c.ugc, query, true)
	}

	if _, err := c.fetchResult(api.sendQuery(c.ugc, query), queryCompletedID, queryCompletedBytes, nil); err != nil {
		return Details{}, err
	}

	raw := make([]byte, detailsBytes)
	if !api.getQueryResult(c.ugc, query, 0, unsafe.Pointer(&raw[0])) {
		return Details{}, fmt.Errorf("steam has no details of the item %s", workshopID)
	}

	if err := toResultError(raw, detailsResult); err != nil {
		return Details{}, err
	}

	details := decodeDetails(raw)

	// A contributor the owner added counts as not the owner.
	user := api.userInterface()
	details.Owned = user != 0 && binary.LittleEndian.Uint64(raw[detailsOwner:]) == api.steamID(user)
	details.Previews = c.listQueryPreviews(query)
	details.Dependencies = c.listChildren(query, raw)
	details.DLC, details.DLCCount, err = c.fetchDLC(id)

	return details, err
}

func (c *Client) listQueryPreviews(query uint64) []Preview {
	previews := []Preview{}
	for index := range api.countPreviews(c.ugc, query, 0) {
		var address, name [1024]byte
		var previewType int32
		addressBytes, nameBytes := uint32(len(address)), uint32(len(name))
		if !api.getPreview(c.ugc, query, 0, index, &address[0], addressBytes, &name[0], nameBytes, &previewType) {
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

// listChildren returns the Workshop ids of the items a queried item requires,
// or nil if Steam's library cannot read them.
func (c *Client) listChildren(query uint64, raw []byte) []string {
	if !canChangeDependencies() {
		return nil
	}

	count := binary.LittleEndian.Uint32(raw[detailsChildren:])
	ids := make([]uint64, count)
	if count > 0 && !api.getChildren(c.ugc, query, 0, &ids[0], count) {
		return nil
	}

	children := make([]string, len(ids))
	for position, id := range ids {
		children[position] = strconv.FormatUint(id, 10)
	}

	return children
}

func canChangeDependencies() bool {
	return api.setChildrenReturned != nil && api.getChildren != nil && api.addDependency != nil &&
		api.removeDependency != nil
}

// fetchDLC returns the app ids of up to 32 DLCs an item requires, and how many
// it requires in all. It returns nil if Steam's library cannot read them.
func (c *Client) fetchDLC(id uint64) ([]string, int, error) {
	if api.getAppDependencies == nil {
		return nil, 0, nil
	}

	call := api.getAppDependencies(c.ugc, id)
	result, err := c.fetchOK(call, appDependenciesResultID, appDependenciesResultBytes, nil)
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

// fetchFolder has Steam download an item and returns its folder once the copy
// is up to date.
func (c *Client) fetchFolder(workshopID string, updatedAt time.Time) (string, error) {
	id, err := toItemID(workshopID)
	if err != nil {
		return "", err
	}

	if !api.downloadItem(c.ugc, id, true) {
		return "", errors.New("steam refused to download the item")
	}

	folder := make([]byte, 1024)
	for deadline := time.Now().Add(downloadTimeout); time.Now().Before(deadline); time.Sleep(pollInterval) {
		api.runCallbacks()

		var size uint64
		var stamp uint32
		if api.getInstall(c.ugc, id, &size, &folder[0], uint32(len(folder)), &stamp) && int64(stamp) >= updatedAt.Unix() {
			return toText(folder), nil
		}
	}

	return "", errors.New("steam did not finish downloading the item")
}

func decodeDetails(raw []byte) Details {
	visibility := int(binary.LittleEndian.Uint32(raw[detailsVisibility:]))
	tags := toText(raw[detailsTags : detailsTags+detailsTagsBytes])
	details := Details{
		Title:       toText(raw[detailsTitle : detailsTitle+detailsTitleBytes]),
		Description: toText(raw[detailsDescription : detailsDescription+detailsDescBytes]),
		UpdatedAt:   time.Unix(int64(binary.LittleEndian.Uint32(raw[detailsUpdated:])), 0),
		Tags:        strings.FieldsFunc(tags, func(char rune) bool { return char == ',' }),
	}
	if visibility < len(visibilities) {
		details.Visibility = visibilities[visibility]
	}

	return details
}

// toText returns a fixed-size field's text, which ends at the first zero byte.
func toText(field []byte) string {
	text, _, _ := bytes.Cut(field, []byte{0})

	return string(text)
}

// CreateItem creates a Workshop item and returns its id. It returns the id with
// an error if the user has not accepted the Workshop's legal agreement.
func (c *Client) CreateItem() (string, error) {
	call := api.createItem(c.ugc, uint32(c.appID), communityFileType)
	result, err := c.fetchOK(call, createItemResultID, createItemResultBytes, nil)
	if err != nil {
		return "", err
	}

	workshopID := strconv.FormatUint(binary.LittleEndian.Uint64(result[createItemResultFile:]), 10)
	if result[createItemResultAgreement] != 0 {
		return workshopID, errAgreement
	}

	return workshopID, nil
}

// updateStages maps Steam's update statuses to stages. Status 0 is no job.
var updateStages = []string{"", stagePreparing, stagePreparing, stageUploading, stageUploading, stageCommitting}

// Submit uploads a folder and a listing to a Workshop item, reporting each
// step.
func (c *Client) Submit(workshopID string, params Params, progress func(Progress)) error {
	id, err := toItemID(workshopID)
	if err != nil {
		return err
	}
	progress(Progress{Stage: stagePreparing})

	// Steam may not answer a query for an item it just made.
	details := Details{Previews: []Preview{}, DLC: []string{}}
	if canChangeDependencies() {
		details.Dependencies = []string{}
	}

	if params.WorkshopID != "" {
		if details, err = c.FetchDetails(workshopID); err != nil {
			return err
		}

		if !details.Owned {
			return errors.New("the Workshop item belongs to another Steam user, so PMT cannot update it")
		}
	}

	listing := params.Listing
	if err := c.setDependencies(id, listing, details, params.ModWorkshopIDs); err != nil {
		return err
	}

	update := api.startUpdate(c.ugc, uint32(c.appID), id)
	if update == invalidHandle {
		return errors.New("steam did not start the update")
	}

	err = errors.Join(
		c.setTexts(update, listing),
		c.setVisibility(update, params.Visibility),
		c.setFiles(update, listing, details.Previews, params.UploadDir),
	)
	if err != nil {
		return err
	}

	var done, total uint64
	report := func() {
		if status := api.getProgress(c.ugc, update, &done, &total); status > 0 {
			stage := updateStages[min(int(status), len(updateStages)-1)]
			progress(Progress{Stage: stage, Done: int64(done), Total: int64(total)})
		}
	}

	call := api.submit(c.ugc, update, listing.Changelog)
	result, err := c.fetchOK(call, submitItemUpdateResultID, submitItemUpdateResultBytes, report)
	if err == nil && result[submitItemUpdateResultAgreement] != 0 {
		err = errAgreement
	}

	if err != nil {
		return err
	}
	progress(Progress{Stage: stageDone, Done: int64(total), Total: int64(total)})

	return nil
}

func (c *Client) setDependencies(id uint64, listing Listing, details Details, modIDs []string) error {
	var errs []error
	if details.DLC != nil {
		added, removed := diffDLC(listing.DLC, details)
		errs = append(errs,
			c.applyChanges(removed, func(dlc uint64) uint64 {
				return api.removeAppDependency(c.ugc, id, uint32(dlc))
			}, removeAppDependencyResultID, appDependencyResultBytes),
			c.applyChanges(added, func(dlc uint64) uint64 {
				return api.addAppDependency(c.ugc, id, uint32(dlc))
			}, addAppDependencyResultID, appDependencyResultBytes),
		)
	}

	if details.Dependencies != nil {
		added, removed := diffRequired(listing.Dependencies, details.Dependencies, modIDs)
		errs = append(errs,
			c.applyChanges(removed, func(child uint64) uint64 {
				return api.removeDependency(c.ugc, id, child)
			}, removeDependencyResultID, dependencyResultBytes),
			c.applyChanges(added, func(child uint64) uint64 {
				return api.addDependency(c.ugc, id, child)
			}, addDependencyResultID, dependencyResultBytes),
		)
	}

	return errors.Join(errs...)
}

// diffTags returns the tags an upload adds and removes. Tags that differ only
// in case are the same, because Steam keeps a tag in the case it has.
func diffTags(sent, steam []string) (added, removed []string) {
	missing := func(tags, others []string) []string {
		result := []string{}
		for _, tag := range tags {
			if !slices.ContainsFunc(others, func(one string) bool { return strings.EqualFold(one, tag) }) {
				result = append(result, tag)
			}
		}

		return result
	}

	return missing(sent, steam), missing(steam, sent)
}

// diffDLC returns the DLCs an upload adds and removes. It removes none if Steam
// listed only some.
func diffDLC(dlc []string, details Details) (added, removed []string) {
	added, removed = diffLists(dlc, details.DLC)
	if details.DLCCount > len(details.DLC) {
		removed = nil
	}

	return added, removed
}

// diffRequired returns the required items an upload adds and removes. It
// removes only the workspace's own mods.
func diffRequired(dependencies []Dependency, steam, modIDs []string) (added, removed []string) {
	required := []string{}
	for _, dependency := range dependencies {
		if dependency.WorkshopID != "" {
			required = append(required, dependency.WorkshopID)
		}
	}
	added, removed = diffLists(required, steam)

	own := []string{}
	for _, id := range removed {
		if slices.Contains(modIDs, id) && !slices.Contains(own, id) {
			own = append(own, id)
		}
	}

	return added, own
}

func diffLabels(previews, steam []Preview) (added, removed []string) {
	return diffLists(listLabels(previews), listLabels(steam))
}

// diffLists returns the entries only the first list has, and those only the
// second has.
func diffLists(first, second []string) (added, removed []string) {
	added, removed = []string{}, []string{}
	for _, entry := range first {
		if !slices.Contains(second, entry) {
			added = append(added, entry)
		}
	}

	for _, entry := range second {
		if !slices.Contains(first, entry) {
			removed = append(removed, entry)
		}
	}

	return added, removed
}

func (c *Client) applyChanges(ids []string, change func(id uint64) uint64, callbackID, size int32) error {
	for _, steamID := range ids {
		number, err := strconv.ParseUint(steamID, 10, 64)
		if err != nil {
			return fmt.Errorf("%q is not a Steam id", steamID)
		}

		if _, err := c.fetchOK(change(number), callbackID, size, nil); err != nil {
			return err
		}
	}

	return nil
}

// setTexts sets the title, description and tags of an update. An empty
// description keeps Steam's own.
func (c *Client) setTexts(update uint64, listing Listing) error {
	errs := []error{toError(api.setTitle(c.ugc, update, listing.Title), "title")}
	if listing.Description != "" {
		errs = append(errs, toError(api.setDescription(c.ugc, update, listing.Description), "description"))
	}

	tags := toSentTags(listing)
	pointers := make([]*byte, len(tags))
	for position, tag := range tags {
		var err error
		if pointers[position], err = syscall.BytePtrFromString(tag); err != nil {
			return fmt.Errorf("the tag %q is not text Steam takes: %w", tag, err)
		}
	}

	// Steam's array of strings is a pointer to the strings and their count.
	array := [2]uintptr{uintptr(unsafe.Pointer(unsafe.SliceData(pointers))), uintptr(len(pointers))}
	errs = append(errs, toError(api.setTags(c.ugc, update, unsafe.Pointer(&array)), "tags"))
	runtime.KeepAlive(pointers)

	return errors.Join(errs...)
}

// setVisibility sets an update's visibility. An empty one keeps the item's own.
func (c *Client) setVisibility(update uint64, visibility string) error {
	if visibility == "" {
		return nil
	}

	number := slices.Index(visibilities, visibility)

	return toError(api.setVisibility(c.ugc, update, int32(number)), "visibility")
}

// setFiles sets the content folder, thumbnail and gallery of an update. A
// changed gallery replaces Steam's.
func (c *Client) setFiles(update uint64, listing Listing, previous []Preview, uploadDir string) error {
	errs := []error{toError(api.setContent(c.ugc, update, uploadDir), "content folder")}
	if listing.Thumbnail != "" {
		errs = append(errs, toError(api.setPreview(c.ugc, update, listing.Thumbnail), "thumbnail"))
	}

	if added, removed := diffLabels(listing.Previews, previous); len(added)+len(removed) == 0 {
		return errors.Join(errs...)
	}

	for index := range slices.Backward(previous) {
		errs = append(errs, toError(api.removePreview(c.ugc, update, uint32(index)), "removal of a preview"))
	}

	for _, preview := range listing.Previews {
		if preview.Type == previewVideo {
			errs = append(errs, toError(api.addPreviewVideo(c.ugc, update, toVideoID(preview.URL)), "video"))
		} else {
			errs = append(errs, toError(api.addPreviewFile(c.ugc, update, preview.Path, imagePreviewType), "picture"))
		}
	}

	return errors.Join(errs...)
}

func toError(ok bool, part string) error {
	if ok {
		return nil
	}

	return fmt.Errorf("steam refused the %s", part)
}

// toVideoID returns the id of a video from its address, or the text as it is.
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

// fetchResult waits for one of Steam's calls and returns its result, calling
// poll meanwhile if it is not nil.
func (c *Client) fetchResult(call uint64, callbackID, size int32, poll func()) ([]byte, error) {
	if call == invalidCall {
		return nil, errors.New("steam did not start the call")
	}

	for {
		var failed bool
		completed := api.isCompleted(c.utils, call, &failed)
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
	if !api.getResult(c.utils, call, unsafe.Pointer(&result[0]), size, callbackID, &failed) || failed {
		return nil, errors.New("steam could not return the result of the call")
	}

	return result, nil
}

func (c *Client) fetchOK(call uint64, callbackID, size int32, poll func()) ([]byte, error) {
	result, err := c.fetchResult(call, callbackID, size, poll)
	if err != nil {
		return nil, err
	}

	return result, toResultError(result, resultCode)
}

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
