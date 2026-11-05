// publish.go starts the second copy of the app that talks to Steam, relays its progress, and does the copy's work: an
// upload or a download of one Workshop item.

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
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

// Params is what a second copy of the app needs to upload a mod or work on an item.
type Params struct {
	LibraryPath    string    `json:"libraryPath"` // the game's copy of Steam's library
	AppID          int       `json:"appId"`
	UploadDir      string    `json:"uploadDir"`
	WorkshopID     string    `json:"workshopId"` // "" creates the item
	UpdatedAt      time.Time `json:"updatedAt"`  // when Steam last updated the item, which a download waits for
	Listing        Listing   `json:"listing"`
	Visibility     string    `json:"visibility"`     // private, friends, public or unlisted; "" keeps the item's own
	ModWorkshopIDs []string  `json:"modWorkshopIds"` // the Workshop ids of the workspace's mods; an upload removes no other required item
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

// copyWork maps each flag to the work a second copy does. A work prints any result on standard output.
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

// startCopy starts a second copy of the app for one kind of work and hands each line it prints to a function, if any.
// A canceled context kills the copy. It returns an error if the copy cannot start or fails.
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
// then the progress as JSON lines.
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

// download has Steam download an item, and saves the hashes of its files and the folder Steam keeps them in as the
// item's snapshot. If Steam's download fails, the earlier snapshot stays.
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
	return SaveSnapshot(dirs.Cache, Snapshot{
		WorkshopID: params.WorkshopID, UpdatedAt: params.UpdatedAt, Dir: folder,
		Files: buildHashes(source{files: os.DirFS(folder), root: folder}, walked),
	})
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
