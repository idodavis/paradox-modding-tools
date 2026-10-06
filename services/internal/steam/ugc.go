// ugc.go is the child's side of a publish or a download: Steam's library opened and its flat
// ISteamUGC functions bound, then one Workshop item created where there is none, updated, and
// submitted, or one item's page read and its files downloaded. The library is the game's own copy,
// built with whatever SDK the game shipped, so the two accessors with an interface version in their
// name are looked up by the version it holds.

package steam

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	queryCompletedCallback = 3401
	createItemCallback     = 3403
	submitItemCallback     = 3404
	resultOK               = 1
	fileTypeCommunity      = 0
	visibilityPrivate      = 2
	previewTypeImage       = 0
	previewTypeYouTube     = 1
	maxExtraPreviews       = 32
	callTimeout            = 10 * time.Minute
	invalidQuery           = ^uint64(0)
)

// ugcAPI is Steam's library with the functions a publish and a download call bound to it.
type ugcAPI struct {
	unload             func()
	init               func() error
	shutdown           func()
	runCallbacks       func()
	ugc                func() uintptr
	utils              func() uintptr
	createItem         func(uintptr, uint32, int32) uint64
	startUpdate        func(uintptr, uint32, uint64) uint64
	setTitle           func(uintptr, uint64, *byte) bool
	setDescription     func(uintptr, uint64, *byte) bool
	setVisibility      func(uintptr, uint64, int32) bool
	setContent         func(uintptr, uint64, *byte) bool
	setPreview         func(uintptr, uint64, *byte) bool
	addPreviewFile     func(uintptr, uint64, *byte, int32) bool
	addPreviewVideo    func(uintptr, uint64, *byte) bool
	removePreview      func(uintptr, uint64, uint32) bool
	submit             func(uintptr, uint64, *byte) uint64
	createDetailsQuery func(uintptr, *uint64, uint32) uint64
	longDescription    func(uintptr, uint64, bool) bool
	withPreviews       func(uintptr, uint64, bool) bool
	sendQuery          func(uintptr, uint64) uint64
	queryResult        func(uintptr, uint64, uint32, *byte) bool
	previewCount       func(uintptr, uint64, uint32) uint32
	previewAt          func(uintptr, uint64, uint32, uint32, *byte, uint32, *byte, uint32, *int32) bool
	releaseQuery       func(uintptr, uint64) bool
	downloadItem       func(uintptr, uint64, bool) bool
	installInfo        func(uintptr, uint64, *uint64, *byte, uint32, *uint32) bool
	callDone           func(uintptr, uint64, *byte) bool
	callResult         func(uintptr, uint64, unsafe.Pointer, int32, int32, *byte) bool
}

// submitItemResult is SubmitItemUpdateResult_t, which both packings lay out alike; the file id
// is bytes because at 4-byte packing it need not sit on an 8-byte boundary.
type submitItemResult struct {
	Result    int32
	NeedLegal bool
	_         [3]byte
	FileID    [8]byte
}

// queryCompleted is SteamUGCQueryCompleted_t, which both packings lay out alike.
type queryCompleted struct {
	Handle   uint64
	Result   int32
	Returned uint32
	Total    uint32
	Cached   bool
	_        [259]byte // the next page's cursor, and padding
}

// SteamUGCDetails_t: its title and description sit at the same offsets at both packings; what
// follows shifts with the packing, 8 on Windows and 4 elsewhere. The size has room for later SDKs.
const (
	detailsTitle       = 24
	detailsDescription = detailsTitle + 129
	detailsOwner       = detailsDescription + 8000 // before alignment
	detailsSize        = 16 << 10
)

// openUGC opens the library at path and binds every function a publish calls, or says which
// one it lacks.
func openUGC(path string) (*ugcAPI, error) {
	handle, closeLibrary, err := openLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("load steam api: %w", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		closeLibrary()
		return nil, err
	}

	api := &ugcAPI{unload: closeLibrary}
	for _, binding := range []struct {
		fn   any
		name string
	}{
		{&api.shutdown, "SteamAPI_Shutdown"},
		{&api.runCallbacks, "SteamAPI_RunCallbacks"},
		{&api.ugc, versioned(contents, "SteamAPI_SteamUGC_v")},
		{&api.utils, versioned(contents, "SteamAPI_SteamUtils_v")},
		{&api.createItem, "SteamAPI_ISteamUGC_CreateItem"},
		{&api.startUpdate, "SteamAPI_ISteamUGC_StartItemUpdate"},
		{&api.setTitle, "SteamAPI_ISteamUGC_SetItemTitle"},
		{&api.setDescription, "SteamAPI_ISteamUGC_SetItemDescription"},
		{&api.setVisibility, "SteamAPI_ISteamUGC_SetItemVisibility"},
		{&api.setContent, "SteamAPI_ISteamUGC_SetItemContent"},
		{&api.setPreview, "SteamAPI_ISteamUGC_SetItemPreview"},
		{&api.addPreviewFile, "SteamAPI_ISteamUGC_AddItemPreviewFile"},
		{&api.addPreviewVideo, "SteamAPI_ISteamUGC_AddItemPreviewVideo"},
		{&api.removePreview, "SteamAPI_ISteamUGC_RemoveItemPreview"},
		{&api.submit, "SteamAPI_ISteamUGC_SubmitItemUpdate"},
		{&api.createDetailsQuery, "SteamAPI_ISteamUGC_CreateQueryUGCDetailsRequest"},
		{&api.longDescription, "SteamAPI_ISteamUGC_SetReturnLongDescription"},
		{&api.withPreviews, "SteamAPI_ISteamUGC_SetReturnAdditionalPreviews"},
		{&api.previewCount, "SteamAPI_ISteamUGC_GetQueryUGCNumAdditionalPreviews"},
		{&api.previewAt, "SteamAPI_ISteamUGC_GetQueryUGCAdditionalPreview"},
		{&api.sendQuery, "SteamAPI_ISteamUGC_SendQueryUGCRequest"},
		{&api.queryResult, "SteamAPI_ISteamUGC_GetQueryUGCResult"},
		{&api.releaseQuery, "SteamAPI_ISteamUGC_ReleaseQueryUGCRequest"},
		{&api.downloadItem, "SteamAPI_ISteamUGC_DownloadItem"},
		{&api.installInfo, "SteamAPI_ISteamUGC_GetItemInstallInfo"},
		{&api.callDone, "SteamAPI_ISteamUtils_IsAPICallCompleted"},
		{&api.callResult, "SteamAPI_ISteamUtils_GetAPICallResult"},
	} {
		if err := bind(binding.fn, handle, binding.name); err != nil {
			closeLibrary()
			return nil, err
		}
	}

	// SDKs from 1.59 on export SteamAPI_InitFlat, which says why it failed; the games' older
	// copies export SteamAPI_Init, which only says whether
	var initFlat func(*[1024]byte) int32
	var initBare func() bool
	switch {
	case bind(&initFlat, handle, "SteamAPI_InitFlat") == nil:
		api.init = func() error {
			var message [1024]byte
			if initFlat(&message) != 0 {
				return fmt.Errorf("SteamAPI_InitFlat: %s", cText(message[:]))
			}
			return nil
		}
	case bind(&initBare, handle, "SteamAPI_Init") == nil:
		api.init = func() error {
			if !initBare() {
				return fmt.Errorf("SteamAPI_Init failed: is Steam running and signed in to an account that owns the game?")
			}
			return nil
		}
	default:
		closeLibrary()
		return nil, fmt.Errorf("steam's library has neither SteamAPI_InitFlat nor SteamAPI_Init")
	}
	return api, nil
}

// bind points fn at the library's function called name.
func bind(fn any, handle uintptr, name string) error {
	address, err := lookupSymbol(handle, name)
	if err != nil || address == 0 {
		return fmt.Errorf("steam's library has no %s", name)
	}
	purego.RegisterFunc(fn, address)
	return nil
}

// versioned is the name in library that is prefix plus the highest version number it holds,
// or prefix alone where it holds none, which bind then reports as missing.
func versioned(library []byte, prefix string) string {
	best, bestVersion := prefix, -1
	for rest := library; ; {
		at := bytes.Index(rest, []byte(prefix))
		if at < 0 {
			return best
		}
		rest = rest[at+len(prefix):]
		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		if version, err := strconv.Atoi(string(rest[:digits])); err == nil && version > bestVersion {
			best, bestVersion = prefix+string(rest[:digits]), version
		}
	}
}

// waitCall runs Steam's callbacks until call completes, then copies its result into dest.
func (api *ugcAPI) waitCall(call uint64, dest unsafe.Pointer, size, callback int32) error {
	if call == 0 {
		return fmt.Errorf("steam api call failed to start")
	}
	utils := api.utils()
	if utils == 0 {
		return fmt.Errorf("steam's utils interface is nil")
	}
	deadline := time.Now().Add(callTimeout)
	var failed byte
	for time.Now().Before(deadline) {
		api.runCallbacks()
		if api.callDone(utils, call, &failed) {
			if failed != 0 {
				return fmt.Errorf("steam api call failed")
			}
			if !api.callResult(utils, call, dest, size, callback, &failed) || failed != 0 {
				return fmt.Errorf("steam api call result failed")
			}
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("steam api call timed out")
}

// start opens library, starts Steam's API on it and returns the UGC interface, with stop to
// undo both.
func start(library string) (api *ugcAPI, ugc uintptr, stop func(), err error) {
	if api, err = openUGC(library); err != nil {
		return nil, 0, nil, err
	}
	if err := api.init(); err != nil {
		api.unload()
		return nil, 0, nil, err
	}
	stop = func() {
		api.shutdown()
		api.unload()
	}
	if ugc = api.ugc(); ugc == 0 {
		stop()
		return nil, 0, nil, fmt.Errorf("steam's ugc interface is nil")
	}
	return api, ugc, stop, nil
}

// publish creates the item where req names none, then sets its content and page and submits it. A new
// item starts private; an update leaves the visibility its owner set on Steam.
func publish(library string, req Request) Response {
	fileID, err := parseFileID(req.PublishedFileID)
	if err != nil {
		return Response{Error: err.Error()}
	}
	api, ugc, stop, err := start(library)
	if err != nil {
		return Response{Error: err.Error()}
	}
	defer stop()

	needLegal, isNew := false, fileID == 0
	if isNew {
		var created createItemResult
		call := api.createItem(ugc, req.AppID, fileTypeCommunity)
		if err := api.waitCall(call, unsafe.Pointer(&created), int32(unsafe.Sizeof(created)), createItemCallback); err != nil {
			return Response{Error: err.Error()}
		}
		if created.Result != resultOK {
			return Response{Error: fmt.Sprintf("CreateItem EResult %d", created.Result)}
		}
		fileID = binary.LittleEndian.Uint64(created.FileID[:])
		needLegal = created.NeedLegal
	}

	update := api.startUpdate(ugc, req.AppID, fileID)
	if update == 0 {
		return Response{Error: "StartItemUpdate failed"}
	}
	if !api.setTitle(ugc, update, cString(req.Title)) {
		return Response{Error: "SetItemTitle failed"}
	}
	if !req.KeepDescription && !api.setDescription(ugc, update, cString(req.Description)) {
		return Response{Error: "SetItemDescription failed"}
	}
	if isNew && !api.setVisibility(ugc, update, visibilityPrivate) {
		return Response{Error: "SetItemVisibility failed"}
	}
	if !api.setContent(ugc, update, cString(req.Folder)) {
		return Response{Error: "SetItemContent failed"}
	}
	if !req.KeepExtras {
		api.clearExtraPreviews(ugc, update)
	}
	if req.Preview != "" && !api.setPreview(ugc, update, cString(req.Preview)) {
		return Response{Error: "SetItemPreview failed"}
	}
	for _, image := range req.ExtraPreviews {
		if !api.addPreviewFile(ugc, update, cString(image), previewTypeImage) {
			return Response{Error: "AddItemPreviewFile failed"}
		}
	}
	for _, video := range req.ExtraVideos {
		if !api.addPreviewVideo(ugc, update, cString(video)) {
			return Response{Error: "AddItemPreviewVideo failed"}
		}
	}

	var submitted submitItemResult
	call := api.submit(ugc, update, cString(req.ChangeNote))
	if err := api.waitCall(call, unsafe.Pointer(&submitted), int32(unsafe.Sizeof(submitted)), submitItemCallback); err != nil {
		return Response{Error: err.Error()}
	}
	if submitted.Result != resultOK {
		return Response{Error: fmt.Sprintf("SubmitItemUpdate EResult %d", submitted.Result)}
	}
	if returned := binary.LittleEndian.Uint64(submitted.FileID[:]); returned != 0 {
		fileID = returned
	}
	return Response{
		PublishedFileID:     strconv.FormatUint(fileID, 10),
		NeedsLegalAgreement: needLegal || submitted.NeedLegal,
	}
}

// download reads the item's page, then has Steam download its files and waits until the copy
// installed is the item's latest update.
func download(library string, req Request) Response {
	fileID, err := parseFileID(req.PublishedFileID)
	if err != nil || fileID == 0 {
		return Response{Error: fmt.Sprintf("no workshop item to download: %q", req.PublishedFileID)}
	}
	api, ugc, stop, err := start(library)
	if err != nil {
		return Response{Error: err.Error()}
	}
	defer stop()

	query := api.createDetailsQuery(ugc, &fileID, 1)
	if query == invalidQuery {
		return Response{Error: "CreateQueryUGCDetailsRequest failed"}
	}
	defer api.releaseQuery(ugc, query)
	// without it Steam cuts the description to 255 bytes
	api.longDescription(ugc, query, true)
	api.withPreviews(ugc, query, true)
	var done queryCompleted
	call := api.sendQuery(ugc, query)
	if err := api.waitCall(call, unsafe.Pointer(&done), int32(unsafe.Sizeof(done)), queryCompletedCallback); err != nil {
		return Response{Error: err.Error()}
	}
	if done.Result != resultOK {
		return Response{Error: fmt.Sprintf("SendQueryUGCRequest EResult %d", done.Result)}
	}
	raw := make([]byte, detailsSize)
	if done.Returned == 0 || !api.queryResult(ugc, query, 0, &raw[0]) {
		return Response{Error: fmt.Sprintf("steam returned no page for item %d", fileID)}
	}
	if result := int32(binary.LittleEndian.Uint32(raw[8:])); result != resultOK {
		return Response{Error: fmt.Sprintf("GetQueryUGCResult EResult %d", result)}
	}
	pack := 8
	if runtime.GOOS != "windows" {
		pack = 4
	}
	// the owner's 64-bit id, then the times created and updated
	updated := binary.LittleEndian.Uint32(raw[(detailsOwner+pack-1)/pack*pack+12:])
	page := Response{
		PublishedFileID: req.PublishedFileID,
		Title:           cText(raw[detailsTitle:detailsDescription]), Description: cText(raw[detailsDescription:detailsOwner]),
		Updated: int64(updated),
	}
	for i := range api.previewCount(ugc, query, 0) {
		var address, name [1024]byte
		var kind int32
		if !api.previewAt(ugc, query, 0, i, &address[0], uint32(len(address)), &name[0], uint32(len(name)), &kind) {
			continue
		}
		switch kind {
		case previewTypeImage:
			page.Extras = append(page.Extras, Extra{URL: cText(address[:]), Name: cText(name[:])})
		case previewTypeYouTube:
			page.Extras = append(page.Extras, Extra{Video: cText(address[:])})
		}
	}
	if req.PageOnly {
		return page
	}

	if !api.downloadItem(ugc, fileID, true) {
		return Response{Error: "DownloadItem failed"}
	}
	var folder [1024]byte
	for deadline := time.Now().Add(callTimeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		api.runCallbacks()
		var size uint64
		var stamp uint32
		if api.installInfo(ugc, fileID, &size, &folder[0], uint32(len(folder)), &stamp) && stamp >= updated {
			page.Folder = cText(folder[:])
			return page
		}
	}
	return Response{Error: "steam did not finish downloading the item"}
}

// clearExtraPreviews drops every extra preview on the update, so a publish replaces them
// rather than adding to them. Removing at 0 can fail while higher indices still hold items
// (a YouTube id first), so it sweeps high to low three times before removing at 0.
func (api *ugcAPI) clearExtraPreviews(ugc uintptr, update uint64) {
	for range 3 {
		for i := uint32(maxExtraPreviews - 1); i != ^uint32(0); i-- {
			api.removePreview(ugc, update, i)
		}
	}
	for range maxExtraPreviews {
		api.removePreview(ugc, update, 0)
	}
}

func parseFileID(id string) (uint64, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == "0" {
		return 0, nil
	}
	return strconv.ParseUint(id, 10, 64)
}

// cString is text as a NUL-terminated C string.
func cString(text string) *byte {
	terminated := append([]byte(text), 0)
	return &terminated[0]
}

// cText is the C string at the start of buffer.
func cText(buffer []byte) string {
	text, _, _ := bytes.Cut(buffer, []byte{0})
	return string(text)
}
