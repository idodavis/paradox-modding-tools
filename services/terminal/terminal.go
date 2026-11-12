// Package terminal is the IDE's terminal service. It starts the shells of the
// Terminal panel, keeps each until it ends, streams each to the window that
// shows it, and finds the files that a shell's output names.
//
// A shell starts in the folder the workspace gives for the active file, with
// the program the caller names, else the `terminal.shell` setting, else the
// first shell the system has. A window opens the stream `terminal` to a shell
// by naming it in its first message. It is sent the shell's screen, then the
// output from that screen on. Every shell is given PMT's address and token for
// coding agents, then the engine's own variables.
//
// A file name in a line of output is a link when a line follows it in one of
// the forms VS Code's terminal knows (`path:12:3`, `path(12,3)`,
// `path, line 12`) and the file is under a root of the workspace.
//
// # Robustness
//
// A shell id that no running shell has, an empty command, a program that cannot
// start and a system with no shell are errors. A stream that fails to read or
// write ends quietly, as does a message for an unknown shell. A resize that the
// shell refuses is skipped. A link name under no root of the workspace, and any
// link past the tenth in a line or in a line of over 2000 bytes, is left out.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/mcp"
	"paradox-modding-tools/services/workspace"
)

// Service runs the shells of the Terminal panel.
type Service struct {
	Workspace *workspace.Service
	Config    *app.ConfigFile

	// mu guards shells and count, which numbers the next shell.
	mu     sync.Mutex
	shells []*terminal.Shell
	count  int
}

// message is what a window sends on a shell's stream.
type message struct {
	Shell string `json:"shell"`
	Input string `json:"input"`

	// Binary is bytes, a character each, as xterm sends a mouse report.
	Binary  string `json:"binary"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`

	// Offset is how far the window has drawn the shell's output.
	Offset int64 `json:"offset"`
}

// The events the terminal service sends. Their data is the shell's id.
const (
	EventShellStarted = "shell:started"
	EventShellExited  = "shell:exited"
)

func init() {
	application.RegisterEvent[string](EventShellStarted)
	application.RegisterEvent[string](EventShellExited)
}

const streamName = "terminal"

// ServiceStartup serves the stream that each window opens to a shell.
func (s *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	application.Get().HandleStream(streamName, s.serve)

	return nil
}

// ServiceShutdown ends every shell.
func (s *Service) ServiceShutdown() error {
	for _, shell := range s.ListShells() {
		shell.Close()
	}

	return nil
}

// ListShells returns the running shells, the oldest first.
func (s *Service) ListShells() []*terminal.Shell {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.shells)
}

// ListPrograms returns the shells this system has, PMT's pick first.
func (s *Service) ListPrograms() []terminal.Program { return terminal.ListPrograms() }

// ListEnvironment returns the variables every shell is given.
func (s *Service) ListEnvironment() []terminal.Variable {
	return append(slices.Clone(mcp.Variables), terminal.ListEnvironment()...)
}

// Start starts a shell that runs a program in the active file's folder. A
// program of "" is the default shell. It returns an error if none can start.
func (s *Service) Start(path, program string, columns, rows int) (*terminal.Shell, error) {
	program, args, err := s.resolveProgram(program)
	if err != nil {
		return nil, err
	}

	return s.Run(path, append([]string{program}, args...), columns, rows)
}

const settingShell = "terminal.shell"

// resolveProgram returns the program to run and its arguments. A program of ""
// is the setting's, else the first one listed. It returns an error if none.
func (s *Service) resolveProgram(program string) (string, []string, error) {
	if program == "" {
		program, _ = s.Config.LookupSetting("", settingShell)
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

// Run starts a shell that runs a command in the active file's folder. It
// returns an error if the command is empty or cannot start.
func (s *Service) Run(path string, command []string, columns, rows int) (*terminal.Shell, error) {
	if len(command) == 0 {
		return nil, errors.New("no program was given to run")
	}

	folder, origin := s.Workspace.ResolveFolder(path)
	s.mu.Lock()
	s.count++
	shell := &terminal.Shell{ID: strconv.Itoa(s.count), Program: command[0], Args: command[1:], Folder: folder, Origin: origin}
	s.mu.Unlock()

	if err := shell.Start(columns, rows); err != nil {
		return nil, fmt.Errorf("%s could not be started: %w", command[0], err)
	}

	s.mu.Lock()
	s.shells = append(s.shells, shell)
	s.mu.Unlock()

	go func() {
		shell.Wait()

		s.mu.Lock()
		s.shells = slices.DeleteFunc(s.shells, func(other *terminal.Shell) bool { return other == shell })
		s.mu.Unlock()

		application.Get().Event.Emit(EventShellExited, shell.ID)
	}()
	application.Get().Event.Emit(EventShellStarted, shell.ID)

	return shell, nil
}

// Close ends a shell and, on Windows, every process it started. It returns an
// error for an unknown id.
func (s *Service) Close(id string) error {
	shell, err := s.findShell(id)
	if err != nil {
		return err
	}
	shell.Close()

	return nil
}

// SetScreen keeps a window's screen for a shell, which the next window draws
// first. It returns an error for an unknown id.
func (s *Service) SetScreen(id string, screen terminal.Screen) error {
	shell, err := s.findShell(id)
	if err != nil {
		return err
	}
	shell.SetScreen(screen)

	return nil
}

// findShell returns the running shell with the id, or an error.
func (s *Service) findShell(id string) (*terminal.Shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, shell := range s.shells {
		if shell.ID == id {
			return shell, nil
		}
	}

	return nil, fmt.Errorf("no shell %s is running", id)
}

// serve sends a window the shell's screen, then its output, until either side
// closes the stream.
func (s *Service) serve(conn *application.StreamConn) {
	defer func() { _ = conn.Close() }()

	var first message
	if conn.ReceiveJSON(&first) != nil {
		return
	}

	shell, err := s.findShell(first.Shell)
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

// send streams a shell's output until the shell ends or the stream closes.
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
