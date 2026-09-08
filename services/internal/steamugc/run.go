// run.go is the ISteamUGC create/update loop used by cmd/pmt-steamugc.

package steamugc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

// Request is the helper stdin JSON.
type Request struct {
	AppID           uint32   `json:"appId"`
	Folder          string   `json:"folder"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Preview         string   `json:"preview"`
	PublishedFileID string   `json:"publishedFileId"`
	ChangeNote      string   `json:"changeNote"`
	Visibility      int32    `json:"visibility"`
	ExtraPreviews   []string `json:"extraPreviews,omitempty"`
	ExtraVideos     []string `json:"extraVideos,omitempty"`
}

// Response is the helper stdout JSON.
type Response struct {
	PublishedFileID     string `json:"publishedFileId"`
	NeedsLegalAgreement bool   `json:"needsLegalAgreement"`
	Error               string `json:"error,omitempty"`
}

type createItemResult struct {
	Result    int32
	_         [4]byte
	FileID    uint64
	NeedLegal uint8
	_         [7]byte
}

type submitResult struct {
	Result    int32
	NeedLegal uint8
	_         [3]byte
	FileID    uint64
}

// Run publishes or updates a Workshop item. libPath is steam_api64.dll / .so / .dylib.
func Run(libPath string, req Request) Response {
	if req.Visibility == 0 {
		req.Visibility = visPrivate
	}
	l, err := openLib(libPath)
	if err != nil {
		return Response{Error: err.Error()}
	}
	defer l.close()
	if err := l.init(); err != nil {
		return Response{Error: err.Error()}
	}
	defer l.shutdown()
	ugc := l.ugc()
	if ugc == 0 {
		return Response{Error: "SteamAPI_SteamUGC_v021 returned nil"}
	}
	fileID, err := parseID(req.PublishedFileID)
	if err != nil {
		return Response{Error: err.Error()}
	}
	needLegal := false
	if fileID == 0 {
		var created createItemResult
		call := l.createItem(ugc, req.AppID, fileTypeFirst)
		if err := l.waitCall(call, unsafe.Pointer(&created), int32(unsafe.Sizeof(created)), ugcCreateItemCB); err != nil {
			return Response{Error: err.Error()}
		}
		if created.Result != eResultOK {
			return Response{Error: fmt.Sprintf("CreateItem EResult %d", created.Result)}
		}
		fileID = created.FileID
		needLegal = created.NeedLegal != 0
	}
	handle := l.startUpdate(ugc, req.AppID, fileID)
	if handle == 0 {
		return Response{Error: "StartItemUpdate failed"}
	}
	if !l.setTitle(ugc, handle, cstr(req.Title)) {
		return Response{Error: "SetItemTitle failed"}
	}
	if !l.setDesc(ugc, handle, cstr(req.Description)) {
		return Response{Error: "SetItemDescription failed"}
	}
	if !l.setVis(ugc, handle, req.Visibility) {
		return Response{Error: "SetItemVisibility failed"}
	}
	if !l.setContent(ugc, handle, cstr(req.Folder)) {
		return Response{Error: "SetItemContent failed"}
	}
	clearExtraPreviews(l, ugc, handle)
	if req.Preview != "" {
		if !l.setPreview(ugc, handle, cstr(req.Preview)) {
			return Response{Error: "SetItemPreview failed"}
		}
	}
	if err := applyExtraPreviews(l, ugc, handle, req); err != nil {
		return Response{Error: err.Error()}
	}
	var submitted submitResult
	call := l.submit(ugc, handle, cstr(req.ChangeNote))
	if err := l.waitCall(call, unsafe.Pointer(&submitted), int32(unsafe.Sizeof(submitted)), ugcSubmitCB); err != nil {
		return Response{Error: err.Error()}
	}
	if submitted.Result != eResultOK {
		return Response{Error: fmt.Sprintf("SubmitItemUpdate EResult %d", submitted.Result)}
	}
	if submitted.FileID != 0 {
		fileID = submitted.FileID
	}
	if submitted.NeedLegal != 0 {
		needLegal = true
	}
	return Response{
		PublishedFileID:     strconv.FormatUint(fileID, 10),
		NeedsLegalAgreement: needLegal,
	}
}

// WriteAppID writes steam_appid.txt into dir (the helper process cwd).
func WriteAppID(dir string, appID uint32) error {
	return os.WriteFile(filepath.Join(dir, "steam_appid.txt"),
		[]byte(strconv.FormatUint(uint64(appID), 10)+"\n"), 0o644)
}

const (
	previewTypeImage int32 = iota
	maxExtraPreviews       = 32
)

// clearExtraPreviews drops every additional preview on the update handle.
// RemoveItemPreview can fail at index 0 (e.g. YouTube first) while higher
// indices still hold items; sweep high→low and repeat so re-publish replaces
// rather than appends.
func clearExtraPreviews(l *lib, ugc uintptr, handle uint64) {
	for range 3 {
		for i := uint32(maxExtraPreviews - 1); i != ^uint32(0); i-- {
			l.removePreview(ugc, handle, i)
		}
	}
	for range maxExtraPreviews {
		l.removePreview(ugc, handle, 0)
	}
}

func applyExtraPreviews(l *lib, ugc uintptr, handle uint64, req Request) error {
	for _, p := range req.ExtraPreviews {
		if !l.addPreviewFile(ugc, handle, cstr(p), previewTypeImage) {
			return fmt.Errorf("AddItemPreviewFile failed")
		}
	}
	for _, id := range req.ExtraVideos {
		if !l.addPreviewVideo(ugc, handle, cstr(id)) {
			return fmt.Errorf("AddItemPreviewVideo failed")
		}
	}
	return nil
}

func parseID(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	return strconv.ParseUint(s, 10, 64)
}

// EncodeResponse writes JSON + newline to stdout.
func EncodeResponse(out *os.File, resp Response) {
	enc := json.NewEncoder(out)
	_ = enc.Encode(resp)
}
