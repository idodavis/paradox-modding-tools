package steam

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

// Params is everything a second copy of the app needs to upload a mod or to work on an item.
type Params struct {
	Game        string  `json:"game"`
	LibraryPath string  `json:"libraryPath"` // the game's copy of Steam's library
	AppID       int     `json:"appId"`
	UploadDir   string  `json:"uploadDir"`
	WorkshopID  string  `json:"workshopId"` // "" creates the item
	Listing     Listing `json:"listing"`
}

// The arguments that start the app as a second copy, one per kind of work.
const (
	publishFlag  = "--steam-publish"
	detailsFlag  = "--steam-details"
	statusFlag   = "--steam-status"
	downloadFlag = "--steam-download"
	previewsFlag = "--steam-previews"
)

// copyTurn is held while a second copy of the app runs, so that callers never start several copies at once.
var copyTurn sync.Mutex

// copyWork is the work of a second copy by the flag it was started with. Each prints its answer on standard output.
var copyWork = map[string]func(client *Client, params Params) error{
	publishFlag:  publish,
	downloadFlag: download,
	detailsFlag:  func(client *Client, params Params) error { return printJSON(client.FetchDetails(params.WorkshopID)) },
	previewsFlag: func(client *Client, params Params) error { return printJSON(client.fetchPreviews(params.WorkshopID)) },
	statusFlag: func(client *Client, _ Params) error {
		if !client.isOnline() {
			return errors.New("steam is running but offline: go online to upload")
		}
		return nil
	},
}

// StartPublish starts a second copy of the app to upload a mod and passes each step of its progress to the given
// function. It returns the item's id, which the copy creates if the parameters name none, even if the upload fails
// after that.
func StartPublish(params Params, progress func(Progress)) (workshopID string, err error) {
	err = startCopy(publishFlag, params, func(line string) {
		var step Progress
		if _, err := strconv.ParseUint(line, 10, 64); err == nil {
			workshopID = line
		} else if json.Unmarshal([]byte(line), &step) == nil {
			progress(step)
		}
	})
	return workshopID, err
}

// fetchDetails returns an item's details, read from Steam by a second copy of the app. It returns nil if the copy
// fails, as it does when Steam is not running.
func fetchDetails(params Params) *Details {
	var details *Details
	err := startCopy(detailsFlag, params, func(line string) {
		var reported Details
		if json.Unmarshal([]byte(line), &reported) == nil {
			details = &reported
		}
	})
	if err != nil {
		return nil
	}
	return details
}

// startCopy starts a second copy of the app with the given flag, writes the parameters to it as JSON and calls the
// given function, if any, with each line it prints. One copy runs at a time: a call waits until the copy before it
// has exited. It returns an error if the copy cannot start or exits with a nonzero code; that error is what the copy
// printed on standard error, or its exit status.
func startCopy(flag string, params Params, onLine func(line string)) error {
	copyTurn.Lock()
	defer copyTurn.Unlock()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, flag)
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
		return errors.Join(scanErr, errors.New(cmp.Or(strings.TrimSpace(stderr.String()), err.Error())))
	}
	return scanErr
}

// runCopy is a second copy's run: it reads the parameters from standard input, opens Steam's library and does the
// given work. It returns the exit code.
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

// publish uploads a mod, creating its item if the parameters name none, and saves a snapshot of the upload. The
// uploaded files become the item's one kept copy for the next compare, in place of any copy kept before, uploaded or
// downloaded. It prints the progress as JSON lines, then the item's id, even after an error, so the next upload goes
// to that item.
func publish(client *Client, params Params) (err error) {
	workshopID := params.WorkshopID
	defer func() {
		if workshopID != "" {
			fmt.Println(workshopID)
		}
	}()
	if workshopID == "" {
		if workshopID, err = client.CreateItem(); err != nil {
			return err
		}
	}
	lines := json.NewEncoder(os.Stdout)
	if err := client.Submit(workshopID, params.Listing, params.UploadDir, func(step Progress) { _ = lines.Encode(step) }); err != nil {
		return err
	}
	dirs, err := app.ResolveDirs()
	if err != nil {
		return err
	}
	return keepSnapshot(dirs.Cache, params.UploadDir, Snapshot{
		WorkshopID: workshopID, UploadedAt: time.Now(), Listing: params.Listing,
	})
}

// download has Steam download an item, copies its files into PMT's cache folder and saves them as the item's
// snapshot, with its title and description. The copy replaces any copy kept before, uploaded or downloaded, once it is
// whole; a failed download or copy leaves the one before as it was. Steam's own folder of the item is not changed.
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
	listing := Listing{WorkshopID: params.WorkshopID, Title: details.Title, Description: details.Description}
	return keepSnapshot(dirs.Cache, dir, Snapshot{
		WorkshopID: params.WorkshopID, UploadedAt: details.UpdatedAt, Listing: listing, Downloaded: true,
	})
}

// keepSnapshot keeps the files of the given folder as an item's one kept copy, in place of the copy kept before, and
// saves the item's snapshot with their hashes. The folder is moved, so it must be in PMT's cache folder. If the move
// fails, the folder is deleted.
func keepSnapshot(cacheDir, dir string, snapshot Snapshot) error {
	snapshot.Dir = resolveSnapshotDir(cacheDir, snapshot.WorkshopID)
	if err := errors.Join(removeCacheDir(cacheDir, snapshot.Dir), os.Rename(dir, snapshot.Dir)); err != nil {
		return errors.Join(err, removeCacheDir(cacheDir, dir))
	}
	walked, err := files.Walk(files.Root{Dir: snapshot.Dir})
	if err != nil {
		return err
	}
	snapshot.Files = buildHashes(walked)
	return SaveSnapshot(cacheDir, snapshot)
}

// printJSON prints a value on standard output as one JSON line. It returns the given error instead if there is one.
func printJSON[T any](value T, err error) error {
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(value)
}

// toExitCode returns the exit code for an error: 0 for none, and 1 for any, which it prints on standard error first.
func toExitCode(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
