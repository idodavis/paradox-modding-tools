// steam.go is both ends of a publish or a download: the parent that starts the child and the
// child's request loop.

// Package steam publishes a mod to the Steam Workshop and downloads one back, keeps what a mod
// holds for its Workshop page, and converts its description between Markdown and the Workshop's
// BBCode.
//
// A publish or a download runs in a second copy of this same executable, started with serveArg,
// which main hands to Serve before anything else starts: Steam ties a process to one game's app
// id for as long as it lives, so each request gets its own process, and a crash inside Steam's
// library costs that process rather than the editor. The child loads the game's own copy of Steam's
// library, where the game table says the install keeps it, so PMT ships none. The parent
// writes the request on the child's stdin and reads the answer from its stdout.
package steam

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
)

// serveArg is the argument that makes this executable the publishing child.
const serveArg = "steam-publish"

const publishTimeout = 15 * time.Minute

// Request is one publish, or with Download the download of an item, or with PageOnly the read of its page.
type Request struct {
	AppID           uint32   `json:"appId"`
	Folder          string   `json:"folder"` // the item's content
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	KeepDescription bool     `json:"keepDescription,omitempty"` // leave Steam's description as it is
	Preview         string   `json:"preview"`                   // the listing image
	PublishedFileID string   `json:"publishedFileId"`           // the item to update; none creates one
	ChangeNote      string   `json:"changeNote"`
	ExtraPreviews   []string `json:"extraPreviews,omitempty"` // extra images
	ExtraVideos     []string `json:"extraVideos,omitempty"`
	KeepExtras      bool     `json:"keepExtras,omitempty"` // leave Steam's extra images and videos as they are
	Download        bool     `json:"download,omitempty"`
	PageOnly        bool     `json:"pageOnly,omitempty"`
	Status          bool     `json:"status,omitempty"` // only whether Steam can take an upload now
}

// Progress is a publish's upload as Steam tells it.
type Progress struct {
	Stage string `json:"stage"` // in words
	Done  uint64 `json:"done"`  // bytes
	Total uint64 `json:"total"` // bytes
}

// progressPrefix starts a progress line on the child's stderr; any other line there is what it said failing.
const progressPrefix = "progress "

// Response is what Steam answered.
type Response struct {
	PublishedFileID     string  `json:"publishedFileId"`
	NeedsLegalAgreement bool    `json:"needsLegalAgreement"`
	Folder              string  `json:"folder,omitempty"` // a download: where Steam put the item
	Title               string  `json:"title,omitempty"`  // the item's page as Steam has it
	Description         string  `json:"description,omitempty"`
	Updated             int64   `json:"updated,omitempty"` // Unix seconds
	Extras              []Extra `json:"extras,omitempty"`
	Error               string  `json:"error,omitempty"`
}

// eresultHints is advice for the Steam results a modder can act on.
var eresultHints = map[int]string{
	9: "steam workshop item was not found (EResult 9). if you deleted it, unlink the workshop id and publish again to create a new private item",
	25: "steam workshop limit exceeded (EResult 25). extra preview images must be under 1 MB each (png/jpg/gif/webp). " +
		"shrink files in workshop/previews, wait a few minutes if you hit a rate limit, then publish again",
}

// Library returns where the game's copy of Steam's library is in an install, or why this system cannot publish for
// that game.
func Library(info *game.Game, installPath string) (string, error) {
	rel, ok := info.SteamLibrary[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("%s has no %s build, so publish it from Windows", info.ShortName, runtime.GOOS)
	}
	library := inventory.Join(installPath, rel)
	if _, err := os.Stat(library); err != nil {
		return "", fmt.Errorf("steam's library is not at %s", library)
	}
	return library, nil
}

// Run starts the child over a library, hands it the request on stdin and returns the Response it writes, a known
// failure turned into advice; progress, when given, hears the upload move.
func Run(library string, req Request, progress func(Progress)) (Response, error) {
	// Steam reads the app id from steam_appid.txt in the working directory
	dir, err := os.MkdirTemp("", "pmt-steam-*")
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
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
	child := exec.CommandContext(ctx, self, serveArg, library)
	child.Dir = dir
	child.Stdin = bytes.NewReader(body)
	var out bytes.Buffer
	child.Stdout = &out
	stderr, err := child.StderrPipe()
	if err != nil {
		return Response{}, err
	}
	if err := child.Start(); err != nil {
		return Response{}, err
	}
	crashText := relayStderr(stderr, progress)
	err = child.Wait()

	var response Response
	if out.Len() > 0 {
		_ = json.Unmarshal(out.Bytes(), &response)
	}
	if err != nil && response.Error == "" {
		// the child states its reason on stdout; one that crashed before that left only stderr
		response.Error = cmp.Or(strings.TrimSpace(crashText), err.Error())
	}
	if response.Error != "" {
		return Response{}, errors.New(cmp.Or(eresultHint(response.Error), response.Error))
	}
	if response.PublishedFileID == "" && !req.Status {
		return Response{}, fmt.Errorf("steam returned no published file id")
	}
	return response, nil
}

// relayStderr runs in the editor: it reads the child's stderr as it comes, tells each progress
// line to progress (which may be nil) and returns every other line, which is what a crash said.
func relayStderr(stderr io.Reader, progress func(Progress)) string {
	var crashText strings.Builder
	for lines := bufio.NewScanner(stderr); lines.Scan(); {
		if line, ok := strings.CutPrefix(lines.Text(), progressPrefix); ok {
			var step Progress
			if json.Unmarshal([]byte(line), &step) == nil && progress != nil {
				progress(step)
			}
		} else {
			crashText.WriteString(lines.Text() + "\n")
		}
	}
	return crashText.String()
}

// eresultHint returns the advice for the EResult a failure names, "" where there is none.
func eresultHint(failure string) string {
	_, rest, ok := strings.Cut(failure, "EResult ")
	if !ok {
		return ""
	}
	var code int
	if _, err := fmt.Sscan(rest, &code); err != nil {
		return ""
	}
	return eresultHints[code]
}

// Serve runs the child's whole life: it reads a Request from stdin, publishes or downloads it through the library named
// after serveArg and writes the Response to stdout; served is false for any other run.
func Serve() (exitCode int, served bool) {
	if len(os.Args) < 2 || os.Args[1] != serveArg {
		return 0, false
	}
	response := Response{Error: "no steam library given"}
	if len(os.Args) > 2 {
		var req Request
		switch err := json.NewDecoder(os.Stdin).Decode(&req); {
		case err != nil:
			response.Error = err.Error()
		case req.Status:
			response = status(os.Args[2])
		case req.Download:
			response = download(os.Args[2], req)
		default:
			response = publish(os.Args[2], req)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(response)
	if response.Error != "" {
		return 1, true
	}
	return 0, true
}
