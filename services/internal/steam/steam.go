// Package steam publishes a mod to the Steam Workshop, keeps what a mod holds for its Workshop
// page, and converts its description between Markdown and the Workshop's BBCode.
//
// A publish runs in a second copy of this same executable, started with ServeArg, which main
// hands to Serve before anything else starts: Steam ties a process to one game's app id for as
// long as it lives, so each publish gets its own process, and a crash inside Steam's library
// costs that process rather than the editor. The child loads the game's own copy of Steam's
// library, where the game table says the install keeps it, so PMT ships none. The parent
// writes the request on the child's stdin and reads the answer from its stdout.
//
// steam.go is both ends of that exchange; ugc.go is the ISteamUGC calls; steam_windows.go and
// steam_unix.go open the library and lay out the one result struct the platforms pack
// differently; workshop.go is the mod's Workshop files; bbcode.go is the description markup.
package steam

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
)

// ServeArg is the argument that makes this executable the publishing child.
const ServeArg = "steam-publish"

const publishTimeout = 15 * time.Minute

// Request is one publish: the item's content and page, and the item to update (none creates one).
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

// Response is what Steam answered.
type Response struct {
	PublishedFileID     string `json:"publishedFileId"`
	NeedsLegalAgreement bool   `json:"needsLegalAgreement"`
	Error               string `json:"error,omitempty"`
}

// eresultHints is advice for the Steam results a modder can act on.
var eresultHints = map[int]string{
	9: "steam workshop item was not found (EResult 9). if you deleted it, unlink the workshop id and publish again to create a new private item",
	25: "steam workshop limit exceeded (EResult 25). extra preview images must be under 1 MB each (png/jpg/gif/webp). " +
		"shrink files in workshop/previews, wait a few minutes if you hit a rate limit, then publish again",
}

// Library is where the game's copy of Steam's library is in an install, or why this system
// cannot publish for that game.
func Library(info *game.GameInfo, installPath string) (string, error) {
	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("workshop publish is not supported on macOS yet")
	}
	rel, ok := info.SteamLibrary[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("%s has no %s build, so publish it from Windows", info.ShortName, runtime.GOOS)
	}
	library := filepath.Join(installPath, filepath.FromSlash(rel))
	if _, err := os.Stat(library); err != nil {
		return "", fmt.Errorf("steam's library is not at %s", library)
	}
	return library, nil
}

// Publish runs one publish in a child process over library and returns Steam's answer, a
// known failure turned into advice.
func Publish(library string, req Request) (Response, error) {
	// Steam reads the app id from steam_appid.txt in the working directory
	dir, err := os.MkdirTemp("", "pmt-steam-*")
	if err != nil {
		return Response{}, err
	}
	defer os.RemoveAll(dir)
	appID := strconv.FormatUint(uint64(req.AppID), 10) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "steam_appid.txt"), []byte(appID), 0o644); err != nil {
		return Response{}, err
	}

	self, err := os.Executable()
	if err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	child := exec.CommandContext(ctx, self, ServeArg, library)
	child.Dir = dir
	child.Stdin = bytes.NewReader(body)
	out, err := child.Output()

	var resp Response
	if len(out) > 0 {
		_ = json.Unmarshal(out, &resp)
	}
	if err != nil && resp.Error == "" {
		// the child states its reason on stdout; one that crashed before that left only stderr
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			resp.Error = strings.TrimSpace(string(exit.Stderr))
		}
		resp.Error = cmp.Or(resp.Error, err.Error())
	}
	if resp.Error != "" {
		return Response{}, errors.New(cmp.Or(eresultHint(resp.Error), resp.Error))
	}
	if resp.PublishedFileID == "" {
		return Response{}, fmt.Errorf("steam returned no published file id")
	}
	return resp, nil
}

// eresultHint is the advice for the EResult a failure names, "" where there is none.
func eresultHint(failure string) string {
	_, after, ok := strings.Cut(failure, "EResult ")
	if !ok {
		return ""
	}
	var code int
	if _, err := fmt.Sscan(after, &code); err != nil {
		return ""
	}
	return eresultHints[code]
}

// Serve is the child, and reports served false for any other run: it reads a Request from
// stdin, publishes it through the library named after ServeArg, writes the Response to
// stdout, and returns the exit code.
func Serve() (exitCode int, served bool) {
	if len(os.Args) < 2 || os.Args[1] != ServeArg {
		return 0, false
	}
	resp := Response{Error: "no steam library given"}
	if len(os.Args) > 2 {
		var req Request
		if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
			resp.Error = err.Error()
		} else {
			resp = publish(os.Args[2], req)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(resp)
	if resp.Error != "" {
		return 1, true
	}
	return 0, true
}
