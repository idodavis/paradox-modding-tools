// publish.go starts the second copy of the app and does its work.

package steam

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

// Params is what a second copy needs to do its work.
type Params struct {
	LibraryPath string `json:"libraryPath"`
	AppID       int    `json:"appId"`
	UploadDir   string `json:"uploadDir"`

	// WorkshopID is "" when the upload creates the item.
	WorkshopID string `json:"workshopId"`

	// UpdatedAt is when Steam last updated the item, which a download waits
	// for.
	UpdatedAt time.Time `json:"updatedAt"`

	Listing Listing `json:"listing"`

	// Visibility is private, friends, public or unlisted. "" keeps the item's
	// own.
	Visibility string `json:"visibility"`

	// ModWorkshopIDs are the Workshop ids of the workspace's mods. An upload
	// removes no other required item.
	ModWorkshopIDs []string `json:"modWorkshopIds"`
}

// The arguments that start the app as a second copy, one per kind of work.
const (
	publishFlag  = "--steam-publish"
	detailsFlag  = "--steam-details"
	statusFlag   = "--steam-status"
	downloadFlag = "--steam-download"
)

// copyTurn lets one second copy run at a time.
var copyTurn sync.Mutex

// copyWork maps each flag to the work a second copy does. The work prints any
// result on standard output.
var copyWork = map[string]func(client *Client, params Params) error{
	publishFlag:  publish,
	downloadFlag: download,
	detailsFlag: func(client *Client, params Params) error {
		details, err := client.FetchDetails(params.WorkshopID)
		if err != nil {
			return err
		}

		return json.NewEncoder(os.Stdout).Encode(details)
	},
	statusFlag: func(*Client, Params) error {
		if user := api.userInterface(); user == 0 || !api.isLoggedOn(user) {
			return errors.New("steam is running but offline: go online to upload")
		}

		return nil
	},
}

// startCopy starts a second copy for one kind of work and hands each line it
// prints to a function, if any. A canceled context kills the copy.
func startCopy(ctx context.Context, flag string, params Params, handle func(line string)) error {
	copyTurn.Lock()
	defer copyTurn.Unlock()

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	command := exec.CommandContext(ctx, executable, flag)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}

	if err := command.Start(); err != nil {
		return err
	}

	if err := errors.Join(json.NewEncoder(stdin).Encode(params), stdin.Close()); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()

		return err
	}

	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if handle != nil {
			handle(lines.Text())
		}
	}
	scanErr := lines.Err()

	if err := command.Wait(); err != nil {
		return errors.Join(scanErr, errors.New(cmp.Or(decodeCopyError(stderr.String()), err.Error())))
	}

	return scanErr
}

// decodeCopyError returns the error a second copy printed last on standard
// error. It returns the whole output if the copy crashed.
func decodeCopyError(output string) string {
	text := strings.TrimSpace(output)

	var message string
	if json.Unmarshal([]byte(text[strings.LastIndex(text, "\n")+1:]), &message) == nil {
		return message
	}

	return text
}

// runCopy reads a second copy's parameters from standard input, does its work
// and returns the exit code.
func runCopy(work func(client *Client, params Params) error) int {
	var params Params
	if err := json.NewDecoder(os.Stdin).Decode(&params); err != nil {
		return toExitCode(fmt.Errorf("the parameters did not arrive: %w", err))
	}

	client, err := openLibrary(params.LibraryPath, params.AppID)
	if err != nil {
		return toExitCode(err)
	}
	defer client.Close()

	return toExitCode(work(client, params))
}

// publish uploads a mod, creating its item if it has none. It prints a new
// item's id at once, even on an error, then the progress as JSON lines.
func publish(client *Client, params Params) error {
	workshopID := params.WorkshopID
	if workshopID == "" {
		created, err := client.CreateItem()
		if created != "" {
			fmt.Println(created)
		}

		if err != nil {
			return err
		}

		workshopID = created
	}

	lines := json.NewEncoder(os.Stdout)

	return client.Submit(workshopID, params, func(step Progress) { _ = lines.Encode(step) })
}

// download has Steam download an item and saves its file hashes and folder as
// the item's snapshot. If the download fails, the earlier snapshot stays.
func download(client *Client, params Params) error {
	folder, err := client.fetchFolder(params.WorkshopID, params.UpdatedAt)
	if err != nil {
		return err
	}

	dirs, err := app.ResolveDirs()
	if err != nil {
		return err
	}

	walked, err := files.Walk(files.Root{Dir: folder})
	if err != nil {
		return err
	}

	return app.SaveJSON(resolveSnapshotPath(dirs.Cache, params.WorkshopID), Snapshot{
		WorkshopID: params.WorkshopID, UpdatedAt: params.UpdatedAt, Dir: folder,
		Files: buildHashes(source{files: os.DirFS(folder), root: folder}, walked),
	})
}

// toExitCode prints an error on standard error as one JSON line and returns 1,
// or returns 0 for no error.
func toExitCode(err error) int {
	if err == nil {
		return 0
	}

	message, _ := json.Marshal(err.Error())
	fmt.Fprintln(os.Stderr, string(message))

	return 1
}
