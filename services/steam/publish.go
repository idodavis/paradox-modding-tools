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
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

// Params is what a second copy of the app needs to upload a mod or work on an item.
type Params struct {
	LibraryPath string  `json:"libraryPath"` // the game's copy of Steam's library
	AppID       int     `json:"appId"`
	UploadDir   string  `json:"uploadDir"`
	WorkshopID  string  `json:"workshopId"` // "" creates the item
	Listing     Listing `json:"listing"`
	Visibility  string  `json:"visibility"` // private, friends, public or unlisted; "" keeps the item's own
	// ModWorkshopIDs are the Workshop ids of the workspace's mods. An upload removes no other required item.
	ModWorkshopIDs []string `json:"modWorkshopIds"`
}

// The arguments that start the app as a second copy, one per kind of work.
const (
	publishFlag  = "--steam-publish"
	detailsFlag  = "--steam-details"
	statusFlag   = "--steam-status"
	downloadFlag = "--steam-download"
)

// copyTurn lets only one second copy of the app run at a time.
var copyTurn sync.Mutex

// copyWork maps each flag to the work a second copy does. Each work prints its answer on standard output.
var copyWork = map[string]func(client *Client, params Params) error{
	publishFlag:  publish,
	downloadFlag: download,
	detailsFlag:  func(client *Client, params Params) error { return printJSON(client.FetchDetails(params.WorkshopID)) },
	statusFlag: func(client *Client, _ Params) error {
		if !client.isOnline() {
			return errors.New("steam is running but offline: go online to upload")
		}
		return nil
	},
}

// StartPublish starts a second copy of the app to upload a mod. It reports each step of the upload, and the id of an
// item it creates as soon as Steam makes it. A canceled context ends the copy. It returns an error if the copy fails.
func StartPublish(ctx context.Context, params Params, progress func(Progress), link func(workshopID string)) error {
	return startCopy(ctx, publishFlag, params, func(line string) {
		var step Progress
		if _, err := strconv.ParseUint(line, 10, 64); err == nil {
			link(line)
		} else if json.Unmarshal([]byte(line), &step) == nil {
			progress(step)
		}
	})
}

// fetchDetails reads an item's details from Steam through a second copy of the app. It returns an error if Steam is
// not running or does not find the item.
func fetchDetails(params Params) (Details, error) {
	var details Details
	err := startCopy(context.Background(), detailsFlag, params, func(line string) {
		_ = json.Unmarshal([]byte(line), &details)
	})
	return details, err
}

// startCopy starts a second copy of the app for one kind of work and hands each line it prints to a function, if any.
// One copy runs at a time. A canceled context kills the copy. It returns an error if the copy cannot start or fails.
func startCopy(ctx context.Context, flag string, params Params, onLine func(line string)) error {
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
		if onLine != nil {
			onLine(lines.Text())
		}
	}
	scanErr := lines.Err()
	if err := command.Wait(); err != nil {
		return errors.Join(scanErr, errors.New(cmp.Or(decodeCopyError(stderr.String()), err.Error())))
	}
	return scanErr
}

// decodeCopyError returns the error a second copy printed last on standard error. It returns the whole output if the
// copy crashed, and "" if it printed nothing.
func decodeCopyError(output string) string {
	text := strings.TrimSpace(output)
	var message string
	if json.Unmarshal([]byte(text[strings.LastIndex(text, "\n")+1:]), &message) == nil {
		return message
	}
	return text
}

// runCopy does a second copy's work with the parameters on standard input and returns the exit code.
func runCopy(work func(client *Client, params Params) error) int {
	var params Params
	if err := json.NewDecoder(os.Stdin).Decode(&params); err != nil {
		return toExitCode(fmt.Errorf("the parameters did not arrive: %w", err))
	}
	client, err := Open(params.LibraryPath, params.AppID)
	if err != nil {
		return toExitCode(err)
	}
	defer client.Close()
	return toExitCode(work(client, params))
}

// publish uploads a mod, creating its item if it has none. It prints a created item's id at once, even on an error,
// then the progress as JSON lines. It returns an error if the item cannot be created or the upload fails.
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

// download has Steam download an item and keeps a copy of its files in PMT's cache as the item's snapshot. A failed
// download leaves the copy kept before as it was.
func download(client *Client, params Params) error {
	details, err := client.FetchDetails(params.WorkshopID)
	if err != nil {
		return err
	}
	folder, err := client.fetchFolder(params.WorkshopID, details.UpdatedAt)
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
	dir, err := copyFiles(dirs.Cache, walked)
	if err != nil {
		return err
	}
	return keepSnapshot(dirs.Cache, dir, Snapshot{WorkshopID: params.WorkshopID, UpdatedAt: details.UpdatedAt})
}

// keepSnapshot moves a folder in PMT's cache into place as an item's kept copy and saves the item's snapshot. If the
// move fails, the folder is deleted.
func keepSnapshot(cacheDir, dir string, snapshot Snapshot) error {
	kept := resolveSnapshotDir(cacheDir, snapshot.WorkshopID)
	if err := moveCacheDir(cacheDir, dir, kept); err != nil {
		return err
	}
	walked, err := files.Walk(files.Root{Dir: kept})
	if err != nil {
		return err
	}
	snapshot.Files = buildHashes(walked)
	return SaveSnapshot(cacheDir, snapshot)
}

// printJSON prints a value on standard output as one JSON line. It returns the error instead, if there is one.
func printJSON[T any](value T, err error) error {
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(value)
}

// toExitCode prints an error on standard error as one JSON line and returns 1. It returns 0 for no error.
func toExitCode(err error) int {
	if err == nil {
		return 0
	}
	message, _ := json.Marshal(err.Error())
	fmt.Fprintln(os.Stderr, string(message))
	return 1
}
