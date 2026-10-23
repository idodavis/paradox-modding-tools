package steam

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

// Params is everything the publishing process needs: the game, the mod's upload folder, the item and the listing.
type Params struct {
	Game        string  `json:"game"`
	LibraryPath string  `json:"libraryPath"`
	AppID       int     `json:"appId"`
	UploadDir   string  `json:"uploadDir"`
	WorkshopID  string  `json:"workshopId"` // "" creates the item
	Listing     Listing `json:"listing"`
}

// The arguments that start the app as a second copy: one uploads, the other reads an item's details.
const (
	publishFlag = "--steam-publish"
	detailsFlag = "--steam-details"
)

// StartPublish starts a second copy of the app as the publishing process, hands it the parameters on standard
// input, and reports its progress lines until it exits. It returns the item's id, which the process creates when
// the parameters name none.
func StartPublish(executable string, params Params, progress func(Progress)) (workshopID string, err error) {
	err = startProcess(executable, publishFlag, params, func(line string) {
		var step Progress
		if _, err := strconv.ParseUint(line, 10, 64); err == nil {
			workshopID = line
		} else if json.Unmarshal([]byte(line), &step) == nil {
			progress(step)
		}
	})
	return workshopID, err
}

// fetchDetails returns an item's details by asking a second copy of the app to read them from Steam. It returns nil
// if the second copy fails, as it does when Steam is not running.
func fetchDetails(params Params) *Details {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	var details *Details
	err = startProcess(executable, detailsFlag, params, func(line string) {
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

// startProcess starts a second copy of the app with the given argument and writes the input to its standard input
// as JSON. It calls the given function with each line the copy prints, until it exits. It returns an error if the
// copy cannot be started, if its output cannot be read, or if it exits with a nonzero code, in which case the
// error holds what it printed on standard error.
func startProcess(executable, argument string, input any, onLine func(line string)) error {
	command := exec.Command(executable, argument)
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
	if err := errors.Join(json.NewEncoder(stdin).Encode(input), stdin.Close()); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		onLine(lines.Text())
	}
	scanErr := lines.Err()
	if err := command.Wait(); err != nil {
		return errors.Join(scanErr, fmt.Errorf("the second copy failed: %w: %s", err, strings.TrimSpace(stderr.String())))
	}
	return scanErr
}

// RunPublish is the main function of the publishing process. It reads the parameters, opens Steam's library,
// creates the item if none is named, submits the upload, and writes progress as JSON lines. It returns the exit
// code.
func RunPublish() int {
	client, params, err := openClient()
	if err != nil {
		return toExitCode(err)
	}
	defer client.Close()
	workshopID := params.WorkshopID
	// the item's id is the last line, printed even after an error so that the next upload goes to the same item
	defer func() {
		if workshopID != "" {
			fmt.Println(workshopID)
		}
	}()
	if workshopID == "" {
		if workshopID, err = client.CreateItem(); err != nil {
			return toExitCode(err)
		}
	}
	lines := json.NewEncoder(os.Stdout)
	if err := client.Submit(workshopID, params.Listing, params.UploadDir, func(step Progress) { _ = lines.Encode(step) }); err != nil {
		return toExitCode(err)
	}
	dirs, err := app.ResolveDirs()
	if err != nil {
		return toExitCode(err)
	}
	uploaded, err := files.Walk(files.Root{Dir: params.UploadDir}, nil)
	if err != nil {
		return toExitCode(err)
	}
	snapshot := Snapshot{WorkshopID: workshopID, UploadedAt: time.Now(), Files: buildHashes(uploaded), Listing: params.Listing}
	return toExitCode(SaveSnapshot(dirs.Cache, snapshot))
}

// runDetails is the main function of the process that reads an item's details. It reads the parameters, opens
// Steam's library and prints the details of the item the parameters name as one JSON line. It returns the exit
// code.
func runDetails() int {
	client, params, err := openClient()
	if err != nil {
		return toExitCode(err)
	}
	defer client.Close()
	details, err := client.FetchDetails(params.WorkshopID)
	if err != nil {
		return toExitCode(err)
	}
	return toExitCode(json.NewEncoder(os.Stdout).Encode(details))
}

// openClient reads the parameters from standard input and opens Steam's library as they say.
func openClient() (*Client, Params, error) {
	var params Params
	if err := json.NewDecoder(os.Stdin).Decode(&params); err != nil {
		return nil, Params{}, fmt.Errorf("the parameters did not arrive: %w", err)
	}
	client, err := Open(params.LibraryPath, params.AppID)
	return client, params, err
}

// toExitCode returns the exit code for an error: 0 for none, and 1 for any, after printing it on standard error.
func toExitCode(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
