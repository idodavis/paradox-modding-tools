// Package terminal is the IDE's terminal service. It starts and keeps the shells of the IDE's panel, streams each one
// to the window that shows it, and finds the files that the shells' output names.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// Service is the terminal service. It holds every shell PMT runs until the shell ends.
type Service struct {
	Workspace *workspace.Service // the open workspace, for a shell's folder and the files its output names
	Config    *app.ConfigFile    // the settings, for the program a shell runs
	mu        sync.Mutex         // guards the fields below
	shells    []*terminal.Shell  // in the order they started
	count     int                // how many shells have started, which numbers the next
}

// A message is one message that a window sends on a shell's stream. The first names the shell; each of the others
// carries what was typed, the window's new size or how far the window has drawn the output.
type message struct {
	Shell   string `json:"shell"`
	Input   string `json:"input"`
	Binary  string `json:"binary"` // bytes, a character each, as xterm sends a mouse report
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
	Offset  int64  `json:"offset"` // the offset of the output that the window has drawn up to
}

// The events the terminal service sends.
const (
	EventShellStarted = "shell:started" // a shell started; the data is its id
	EventShellExited  = "shell:exited"  // a shell ended; the data is its id
)

// streamName is the name a window opens a shell's stream by.
const streamName = "terminal"

// settingShell is the setting that names the default shell.
const settingShell = "terminal.shell"

func init() {
	application.RegisterEvent[string](EventShellStarted)
	application.RegisterEvent[string](EventShellExited)
}

// ServiceStartup serves the stream that each window opens to a shell.
func (trm *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	application.Get().HandleStream(streamName, trm.serve)
	return nil
}

// ServiceShutdown ends every shell.
func (trm *Service) ServiceShutdown() error {
	for _, shell := range trm.ListShells() {
		shell.Close()
	}
	return nil
}

// ListShells returns the shells that are running, the first started first.
func (trm *Service) ListShells() []*terminal.Shell {
	trm.mu.Lock()
	defer trm.mu.Unlock()
	return slices.Clone(trm.shells)
}

// ListPrograms returns the shells this system has, PMT's pick first.
func (trm *Service) ListPrograms() []terminal.Program { return terminal.ListPrograms() }

// ListEnvironment returns the variables every shell is given on this system.
func (trm *Service) ListEnvironment() []terminal.Variable { return terminal.ListEnvironment() }

// Start starts a shell of the given size running the program at the given path, or for "" the default shell, in the
// folder resolveFolder gives for the file at the given path, which may be "". It returns an error if this system has
// no shell or the program cannot be started.
func (trm *Service) Start(path, program string, columns, rows int) (*terminal.Shell, error) {
	program, args, err := trm.resolveProgram(program)
	if err != nil {
		return nil, err
	}
	folder, origin := trm.resolveFolder(path)
	trm.mu.Lock()
	trm.count++
	shell := &terminal.Shell{ID: strconv.Itoa(trm.count), Program: program, Args: args, Folder: folder, Origin: origin}
	trm.mu.Unlock()
	if err := shell.Start(columns, rows); err != nil {
		return nil, fmt.Errorf("%s could not be started: %w", program, err)
	}
	trm.mu.Lock()
	trm.shells = append(trm.shells, shell)
	trm.mu.Unlock()
	go func() {
		shell.Wait()
		trm.mu.Lock()
		trm.shells = slices.DeleteFunc(trm.shells, func(other *terminal.Shell) bool { return other == shell })
		trm.mu.Unlock()
		application.Get().Event.Emit(EventShellExited, shell.ID)
	}()
	application.Get().Event.Emit(EventShellStarted, shell.ID)
	return shell, nil
}

// resolveProgram returns the program at the given path, or for "" the default shell (the setting's, else PMT's pick),
// with the arguments ListPrograms gives it; any other program has none. It returns an error if no program is named
// and this system has no shell.
func (trm *Service) resolveProgram(program string) (string, []string, error) {
	if program == "" {
		program, _ = trm.Config.LookupSetting("", settingShell)
	}
	programs := terminal.ListPrograms()
	for _, row := range programs {
		if row.Path == program {
			return row.Path, row.Args, nil
		}
	}
	if program != "" {
		return program, nil, nil
	}
	if len(programs) == 0 {
		return "", nil, errors.New("no shell was found on this system")
	}
	return programs[0].Path, programs[0].Args, nil
}

// resolveFolder returns the folder a new shell starts in, and the id of the mod it is the folder of. It is the folder
// of the mod that holds the file at the given path, else of the workspace's first mod, a mod whose folder is missing
// passed over. With neither it is the home folder, and the id is "".
func (trm *Service) resolveFolder(path string) (string, string) {
	mods := trm.Workspace.State().Mods
	if path != "" {
		origin := trm.Workspace.Locate(path).Origin
		mods = append(lo.Filter(mods, func(mod workspace.Mod, _ int) bool { return mod.ID == origin }), mods...)
	}
	if mod, ok := lo.Find(mods, func(mod workspace.Mod) bool { return !mod.Broken }); ok {
		return mod.Dir, mod.ID
	}
	home, _ := os.UserHomeDir()
	return home, ""
}

// Close ends the shell with the given id, and on Windows every process it started. It returns an error if no shell
// has that id.
func (trm *Service) Close(id string) error {
	shell, err := trm.findShell(id)
	if err != nil {
		return err
	}
	shell.Close()
	return nil
}

// SetScreen keeps the screen a window drew for a shell, which the next window to show the shell draws first. It
// returns an error if no shell has the given id.
func (trm *Service) SetScreen(id string, screen terminal.Screen) error {
	shell, err := trm.findShell(id)
	if err != nil {
		return err
	}
	shell.SetScreen(screen)
	return nil
}

// findShell returns the running shell with the given id. It returns an error if no shell has it.
func (trm *Service) findShell(id string) (*terminal.Shell, error) {
	trm.mu.Lock()
	defer trm.mu.Unlock()
	if shell, ok := lo.Find(trm.shells, func(shell *terminal.Shell) bool { return shell.ID == id }); ok {
		return shell, nil
	}
	return nil, fmt.Errorf("no shell %s is running", id)
}

// serve serves one window's stream to a shell. The window names the shell, then sends what is typed, its size and how
// far it has drawn. It is sent the shell's screen as JSON, then the shell's output from that screen's offset on, until
// the window or the shell closes the stream.
func (trm *Service) serve(conn *application.StreamConn) {
	defer func() { _ = conn.Close() }()
	var first message
	if conn.ReceiveJSON(&first) != nil {
		return
	}
	shell, err := trm.findShell(first.Shell)
	if err != nil {
		return
	}
	screen := shell.Screen()
	if conn.SendJSON(screen) != nil {
		return
	}
	go send(conn, shell, screen.Offset)
	for {
		var next message
		if conn.ReceiveJSON(&next) != nil {
			return
		}
		if next.Columns > 0 && next.Rows > 0 {
			_ = shell.Resize(next.Columns, next.Rows)
		}
		shell.Acknowledge(next.Offset)
		input := []byte(next.Input)
		for _, char := range next.Binary {
			input = append(input, byte(char))
		}
		if len(input) == 0 {
			continue
		}
		if _, err := shell.Write(input); err != nil {
			return
		}
	}
}

// send sends a shell's output from the given offset on, until the shell ends or the stream closes.
func send(conn *application.StreamConn, shell *terminal.Shell, offset int64) {
	defer func() { _ = conn.Close() }()
	for {
		output, next, err := shell.Read(conn.Context(), offset)
		if err != nil || conn.Send(output) != nil {
			return
		}
		offset = next
	}
}
