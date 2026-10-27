// Package terminal runs shells under the system's own pseudo-terminal: Windows' pseudo console, or a Linux pty. It
// keeps each shell's recent output and the screen a window last drew, so that another window can take a shell over.
package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
)

// A Shell is one program running under a pseudo-terminal, together with the output it has written. Its starter fills
// in the id, the program and its arguments, the folder and the origin, then calls Start. No exported field changes
// after that.
type Shell struct {
	ID      string   `json:"id"`
	Program string   `json:"program"` // the program's path
	Args    []string `json:"args"`    // the arguments the program starts with
	Name    string   `json:"name"`    // the program's file name without its extension, such as pwsh
	Folder  string   `json:"folder"`  // the folder it starts in
	Origin  string   `json:"origin"`  // the mod whose folder it starts in; "" for another folder
	Build   int      `json:"build"`   // the Windows build its pseudo console comes with; 0 on Linux

	console   *console
	consoleMu sync.Mutex // serializes the writes, the resizes and the close of the console
	closed    bool       // set under both locks, so that either one guards a read of it

	outputMu sync.Mutex    // guards the fields below
	output   []byte        // the newest output, from historyBytes to twice that
	end      int64         // the offset just past the newest byte of output
	ack      int64         // the furthest offset of the output that a window has drawn
	change   chan struct{} // closed and made again at each change of the output, the ack or closed
	ended    bool
	screen   Screen
	columns  int
	rows     int
	done     chan struct{} // closed once the output is read to its end
}

// A Screen is a shell's screen as a window drew it: the terminal's own text for it, which draws it again, and the
// offset of the output it holds. It also carries the shell's size.
type Screen struct {
	Text    string `json:"text"`
	Offset  int64  `json:"offset"`
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
}

// A Program is a shell this system has, with the arguments it starts with.
type Program struct {
	Name string   `json:"name"` // the name the Settings page lists it by, such as PowerShell 7
	Path string   `json:"path"`
	Args []string `json:"args"` // such as --login for Git Bash, which then runs the user's profile
}

// A Variable is one environment variable that every shell is given.
type Variable struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Help   string `json:"help"`
	System string `json:"-"` // the system it is given on, as runtime.GOOS names it; "" for both
}

// historyBytes is how much of a shell's newest output is kept for a window that starts to show it.
const historyBytes = 4 << 20

// aheadBytes is how far a shell's output may run past what a window has drawn. Past it, the output is not read until a
// window draws more, so the shell's program waits.
const aheadBytes = 512 << 10

// environment lists the variables every shell is given over PMT's own environment.
var environment = []Variable{
	{Name: "COLORTERM", Value: "truecolor", Help: "Full colors, from the theme's terminal colors"},
	{Name: "TERM", Value: "xterm-256color", System: "linux",
		Help: "The kind of terminal, by which programs choose their colors and keys"},
	// Claude Code's documented fix for the fragments a pseudo console leaves on its full screen
	{Name: "CLAUDE_CODE_ALT_SCREEN_FULL_REPAINT", Value: "1", System: "windows",
		Help: "Claude Code redraws its whole screen, which Windows' pseudo console needs"},
}

// errClosed is the error of a write to a shell that has ended.
var errClosed = errors.New("the shell has ended")

// ListEnvironment returns the variables every shell is given on this system.
func ListEnvironment() []Variable {
	return lo.Filter(environment, func(row Variable, _ int) bool {
		return row.System == "" || row.System == runtime.GOOS
	})
}

// Start starts the shell's program in its folder, under a new pseudo-terminal of the given size. It returns an error
// if the program cannot be started.
func (shell *Shell) Start(columns, rows int) error {
	shell.Name = strings.TrimSuffix(filepath.Base(shell.Program), filepath.Ext(shell.Program))
	shell.columns, shell.rows = columns, rows
	shell.change = make(chan struct{})
	shell.done = make(chan struct{})
	console, err := startConsole(shell, buildEnvironment())
	if err != nil {
		return err
	}
	shell.console = console
	go shell.receive()
	// the program's exit closes the console, which ends the output
	go func() {
		console.wait()
		shell.Close()
	}()
	return nil
}

// buildEnvironment returns PMT's own environment with the variables of ListEnvironment set over it. On Windows a
// variable's name matches without case.
func buildEnvironment() []string {
	env := os.Environ()
	for _, row := range ListEnvironment() {
		env = slices.DeleteFunc(env, func(entry string) bool {
			name, _, _ := strings.Cut(entry, "=")
			return name == row.Name || runtime.GOOS == "windows" && strings.EqualFold(name, row.Name)
		})
		env = append(env, row.Name+"="+row.Value)
	}
	return env
}

// receive keeps the console's output until the console closes, then marks the shell ended. Before each read it waits
// for a window that has fallen behind.
func (shell *Shell) receive() {
	defer close(shell.done)
	chunk := make([]byte, 32<<10)
	for {
		shell.waitWindow()
		n, err := shell.console.read(chunk)
		if n > 0 {
			shell.outputMu.Lock()
			shell.output = append(shell.output, chunk[:n]...)
			// cut only past twice the history, so a flood of output does not copy the history for every chunk
			if len(shell.output) > 2*historyBytes {
				shell.output = bytes.Clone(shell.output[len(shell.output)-historyBytes:])
			}
			shell.end += int64(n)
			shell.emitChange()
			shell.outputMu.Unlock()
		}
		if err != nil {
			break
		}
	}
	shell.outputMu.Lock()
	shell.ended = true
	shell.emitChange()
	shell.outputMu.Unlock()
}

// waitWindow waits while the output is more than aheadBytes past the furthest offset a window has drawn. A shell that
// is closing does not wait, since a pseudo console does not close until its output is read.
func (shell *Shell) waitWindow() {
	for {
		shell.outputMu.Lock()
		ahead := shell.end-shell.ack > aheadBytes && !shell.closed
		change := shell.change
		shell.outputMu.Unlock()
		if !ahead {
			return
		}
		<-change
	}
}

// emitChange wakes everything that waits for a change of the output, the ack or closed. The caller holds outputMu.
func (shell *Shell) emitChange() {
	close(shell.change)
	shell.change = make(chan struct{})
}

// Read returns the output the shell has written from the given offset on, and the offset just past it. It waits until
// there is some. Output that the history no longer holds is skipped. It returns io.EOF once the shell has ended and
// all of its output is read, and the context's error if the context ends first.
func (shell *Shell) Read(ctx context.Context, offset int64) ([]byte, int64, error) {
	for {
		shell.outputMu.Lock()
		start := shell.end - int64(len(shell.output))
		offset = max(offset, start)
		if offset < shell.end {
			output, end := bytes.Clone(shell.output[offset-start:]), shell.end
			shell.outputMu.Unlock()
			return output, end, nil
		}
		ended, change := shell.ended, shell.change
		shell.outputMu.Unlock()
		if ended {
			return nil, offset, io.EOF
		}
		select {
		case <-change:
		case <-ctx.Done():
			return nil, offset, ctx.Err()
		}
	}
}

// Acknowledge records that a window has drawn the output up to the given offset, which lets the output run aheadBytes
// past it. An offset already passed changes nothing.
func (shell *Shell) Acknowledge(offset int64) {
	shell.outputMu.Lock()
	defer shell.outputMu.Unlock()
	if offset > shell.ack {
		shell.ack = offset
		shell.emitChange()
	}
}

// Write sends the given bytes to the program, as if they were typed. It returns an error once the shell has ended.
func (shell *Shell) Write(input []byte) (int, error) {
	shell.consoleMu.Lock()
	defer shell.consoleMu.Unlock()
	if shell.closed {
		return 0, errClosed
	}
	return shell.console.write(input)
}

// Resize changes the size of the shell's terminal. A size it already has, a size under one cell and a shell that has
// ended change nothing.
func (shell *Shell) Resize(columns, rows int) error {
	shell.consoleMu.Lock()
	defer shell.consoleMu.Unlock()
	shell.outputMu.Lock()
	unchanged := columns == shell.columns && rows == shell.rows
	shell.outputMu.Unlock()
	empty := columns < 1 || rows < 1
	if shell.closed || unchanged || empty {
		return nil
	}
	if err := shell.console.resize(columns, rows); err != nil {
		return err
	}
	shell.outputMu.Lock()
	shell.columns, shell.rows = columns, rows
	shell.outputMu.Unlock()
	return nil
}

// Screen returns the screen a window last kept for the shell, with the shell's current size. With none, or one older
// than the history, it is an empty screen at the history's start.
func (shell *Shell) Screen() Screen {
	shell.outputMu.Lock()
	defer shell.outputMu.Unlock()
	screen := shell.screen
	if start := shell.end - int64(len(shell.output)); screen.Offset < start {
		screen = Screen{Offset: start}
	}
	screen.Columns, screen.Rows = shell.columns, shell.rows
	return screen
}

// SetScreen keeps the screen a window drew for the shell, for the next window that shows it. The size it carries is
// not kept, since the shell's own size is the one that counts.
func (shell *Shell) SetScreen(screen Screen) {
	shell.outputMu.Lock()
	defer shell.outputMu.Unlock()
	shell.screen = Screen{Text: screen.Text, Offset: screen.Offset}
}

// Close ends the shell's program, and on Windows every process the program started. Only the first call does
// anything.
func (shell *Shell) Close() {
	shell.consoleMu.Lock()
	defer shell.consoleMu.Unlock()
	if shell.closed {
		return
	}
	// wakes a read that waits for a window, since a pseudo console does not close until its output is read
	shell.outputMu.Lock()
	shell.closed = true
	shell.emitChange()
	shell.outputMu.Unlock()
	shell.console.close()
}

// Wait waits until the shell has ended and its output is read to its end.
func (shell *Shell) Wait() {
	<-shell.done
}
