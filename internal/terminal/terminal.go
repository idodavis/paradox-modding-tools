// Package terminal runs shells under the system's own pseudo-terminal:
// Windows' pseudo console, or a Linux pty. It keeps each shell's recent
// output and the screen a window last drew, so that another window can take
// a shell over. It also lists the shells the system has, and the variables
// every shell is given.
//
// # A shell's life
//
// A Shell is filled in and then started, as an exec.Cmd is.
//
//   - One goroutine reads the pseudo-terminal for as long as it is open. It
//     keeps the newest 4 MiB of output, with the offset of every byte the
//     shell has written. Read returns the output past an offset, and waits
//     until there is some.
//   - A window tells the shell with Acknowledge how far it has drawn the
//     output. While the output runs more than 512 KiB past the furthest a
//     window has drawn, the goroutine stops reading, so the program waits on
//     its own writes. A shell that is closing is read on, since a pseudo
//     console does not close until its output is read.
//   - Write sends bytes as typed, and Resize changes the size. Both take one
//     lock with Close, so a resize never reaches a closed console.
//   - Close runs once and ends the program, and on Windows every process the
//     program started too. The program's own exit closes the console as
//     well. Wait returns once the output is read to its end.
//   - SetScreen keeps the screen a window drew and the offset of the output
//     it holds. Screen returns it with the shell's current size. A window
//     that starts to show the shell draws that screen, then the output past
//     its offset.
//
// # Windows
//
// The pseudo console is made by CreatePseudoConsole over two pipes, and the
// program starts with it as a process attribute. The program also starts
// with STARTF_USESTDHANDLES: with no standard handles of its own, it would
// write to the handles PMT was given rather than to the console.
//
// A console starts on the system's code page. Right after the program
// starts, PMT attaches to its console for a moment and sets both code pages
// to UTF-8, so that UTF-8 output such as git's prints right.
//
// ListPrograms gives these shells where they are installed, PMT's pick
// first:
//
//   - PowerShell 7: pwsh on the PATH, else pwsh.exe in the folder its
//     install key names.
//   - Windows PowerShell, in the system folder.
//   - Git Bash: bin\bash.exe in Git for Windows' install folder, found from
//     the folder of git on the PATH, else from its install key for all users
//     or for this one. It starts as bash --login -i, a login shell as VS
//     Code starts it, so that the user's profile and prompt apply.
//   - WSL: wsl.exe, while WSL has a default distribution. Without one,
//     wsl.exe only offers to install WSL.
//
// # Linux
//
// The pty is /dev/ptmx, unlocked, with its other side as the program's three
// standard files. The program leads a session of its own, with the pty as
// its controlling terminal. Every ioctl goes through SyscallConn, which
// keeps the file non-blocking, so that Close wakes a read that is waiting.
//
// ListPrograms gives $SHELL, then each shell /etc/shells lists, then sh,
// each file once and only if it exists.
//
// # The environment
//
// Every shell gets PMT's own environment, with these variables set over it:
//
//   - COLORTERM=truecolor, for full colors.
//   - On Linux, TERM=xterm-256color.
//   - On Windows, CLAUDE_CODE_ALT_SCREEN_FULL_REPAINT=1: Claude Code's
//     documented fix for the fragments a pseudo console leaves on its full
//     screen.
//
// On Windows a variable's name matches PMT's own in any letter case, as
// Windows matches it.
//
// # Robustness
//
// Start returns an error if the program cannot be started, and Write returns
// one once the shell has ended.
//
// Read skips the output the history no longer holds. Resize changes nothing
// for a size under one cell, for the size the shell already has, or once
// the shell has ended. Screen gives an empty screen at the history's start
// in place of one older than the history. A process that has a console of
// its own, as a dev build has, cannot attach to a shell's, so a dev build
// keeps the system's code page. A handle that will not close is ignored. A
// missing install key, or a missing /etc/shells, lists no shell from it.
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
)

// Shell is one program running under a pseudo-terminal, with the output it
// has written. ID, Program, Args, Folder and Origin are set before Start,
// which sets Name and Build. No exported field changes after Start.
type Shell struct {
	ID string `json:"id"`

	// Program is the program's path, and Args are the arguments it starts
	// with.
	Program string   `json:"program"`
	Args    []string `json:"args"`

	// Name is the program's file name without its extension, such as pwsh.
	Name string `json:"name"`

	// Folder is the folder the program starts in, and Origin is the mod
	// whose folder that is. Origin is "" for a folder of no mod.
	Folder string `json:"folder"`
	Origin string `json:"origin"`

	// Build is the Windows build the pseudo console comes with. It is 0 on
	// Linux.
	Build int `json:"build"`

	console *console

	// done is closed once the output is read to its end.
	done chan struct{}

	// consoleMu serializes the writes, the resizes and the close of the
	// console.
	consoleMu sync.Mutex

	// closed is set with both locks held, so that holding either lock is
	// enough to read it.
	closed bool

	// outputMu guards the fields below.
	outputMu sync.Mutex

	// output is the newest output, up to twice historyBytes. end is the
	// offset just past its newest byte, and ack is the furthest offset a
	// window has drawn.
	output []byte
	end    int64
	ack    int64

	// change is closed and replaced at each change of the output, the ack or
	// closed.
	change chan struct{}

	// ended is set once the output has ended, and screen is the screen a
	// window last kept, without its size.
	ended  bool
	screen Screen

	// columns and rows are the terminal's size. After Start, like closed, they
	// change only with both locks held.
	columns int
	rows    int
}

// Screen is what a window drew of a shell, which the next window to show the
// shell draws first.
type Screen struct {
	// Text is the text that redraws the screen, and Offset is the offset of
	// the output it shows.
	Text   string `json:"text"`
	Offset int64  `json:"offset"`

	// Columns and Rows are the shell's size.
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

// Program is a shell this system has, with the arguments it starts with.
type Program struct {
	// Name is the shell's name as PMT lists it, such as PowerShell 7.
	Name string `json:"name"`

	Path string `json:"path"`

	// Args are the arguments the shell starts with, such as --login for Git
	// Bash, which then runs the user's profile.
	Args []string `json:"args"`
}

// Variable is one environment variable that every shell is given.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Help  string `json:"help"`

	// System is the system the variable is given on, as runtime.GOOS names
	// it. It is "" for both.
	System string `json:"-"`
}

// environment lists the variables every shell is given over PMT's own
// environment.
var environment = []Variable{
	{Name: "COLORTERM", Value: "truecolor", Help: "Full colors, from the theme's terminal colors"},
	{Name: "TERM", Value: "xterm-256color", System: "linux",
		Help: "The kind of terminal, by which programs choose their colors and keys"},
	{Name: "CLAUDE_CODE_ALT_SCREEN_FULL_REPAINT", Value: "1", System: "windows",
		Help: "Claude Code redraws its whole screen, which Windows' pseudo console needs"},
}

// ListEnvironment returns the variables every shell is given on this system.
func ListEnvironment() []Variable {
	var variables []Variable
	for _, row := range environment {
		if row.System == "" || row.System == runtime.GOOS {
			variables = append(variables, row)
		}
	}

	return variables
}

// Start starts the shell's program in its folder, under a new
// pseudo-terminal of the given size, and sets Name and Build.
func (s *Shell) Start(columns, rows int) error {
	s.Name = strings.TrimSuffix(filepath.Base(s.Program), filepath.Ext(s.Program))
	s.columns, s.rows = columns, rows
	s.change = make(chan struct{})
	s.done = make(chan struct{})

	console, err := startConsole(s, buildEnvironment())
	if err != nil {
		return err
	}

	s.console = console
	go s.receive()

	// The program's exit closes the console, which ends the output.
	go func() {
		console.wait()
		s.Close()
	}()

	return nil
}

// buildEnvironment returns PMT's own environment with the variables of
// ListEnvironment set over it. On Windows a variable's name matches in any
// letter case.
func buildEnvironment() []string {
	env := os.Environ()
	for _, row := range ListEnvironment() {
		env = slices.DeleteFunc(env, func(entry string) bool {
			name, _, _ := strings.Cut(entry, "=")

			return name == row.Name || (runtime.GOOS == "windows" && strings.EqualFold(name, row.Name))
		})
		env = append(env, row.Name+"="+row.Value)
	}

	return env
}

// historyBytes is how much of a shell's newest output is kept for a window
// that starts to show it.
const historyBytes = 4 << 20

// receive keeps the console's output until the console closes, then marks
// the shell ended.
func (s *Shell) receive() {
	defer close(s.done)

	chunk := make([]byte, 32<<10)
	for {
		s.waitWindow()

		count, err := s.console.read(chunk)
		if count > 0 {
			s.outputMu.Lock()
			s.output = append(s.output, chunk[:count]...)

			// The output is cut only past twice the history, so that a flood
			// of output does not copy the history for every chunk.
			if len(s.output) > 2*historyBytes {
				s.output = bytes.Clone(s.output[len(s.output)-historyBytes:])
			}

			s.end += int64(count)
			s.emitChange()
			s.outputMu.Unlock()
		}

		if err != nil {
			break
		}
	}

	s.outputMu.Lock()
	s.ended = true
	s.emitChange()
	s.outputMu.Unlock()
}

// aheadBytes is how far a shell's output may run past what a window has
// drawn. Past it, the output is not read until a window draws more, so the
// shell's program waits.
const aheadBytes = 512 << 10

// waitWindow waits while the output runs more than aheadBytes past the
// furthest offset a window has drawn. It does not wait for a shell that is
// closing, since a pseudo console does not close until its output is read.
func (s *Shell) waitWindow() {
	for {
		s.outputMu.Lock()
		ahead := s.end-s.ack > aheadBytes && !s.closed
		change := s.change
		s.outputMu.Unlock()

		if !ahead {
			return
		}

		<-change
	}
}

// emitChange wakes everything that waits for a change of the output, the ack
// or closed. The caller holds outputMu.
func (s *Shell) emitChange() {
	close(s.change)
	s.change = make(chan struct{})
}

// Read returns the output from an offset on and the offset just past it,
// waiting until there is some. Output the history no longer holds is skipped.
// It returns io.EOF once the shell has ended and its output is all read.
func (s *Shell) Read(ctx context.Context, offset int64) ([]byte, int64, error) {
	for {
		s.outputMu.Lock()
		start := s.end - int64(len(s.output))
		offset = max(offset, start)
		if offset < s.end {
			output, end := bytes.Clone(s.output[offset-start:]), s.end
			s.outputMu.Unlock()

			return output, end, nil
		}

		ended, change := s.ended, s.change
		s.outputMu.Unlock()

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

// Acknowledge records that a window has drawn the output up to the given
// offset, which lets the output run aheadBytes past it. An offset already
// passed changes nothing.
func (s *Shell) Acknowledge(offset int64) {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()

	if offset > s.ack {
		s.ack = offset
		s.emitChange()
	}
}

// errClosed is the error of a write to a shell that has ended.
var errClosed = errors.New("the shell has ended")

// Write sends the given bytes to the program, as if they were typed. It
// returns an error once the shell has ended.
func (s *Shell) Write(input []byte) (int, error) {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()

	if s.closed {
		return 0, errClosed
	}

	return s.console.write(input)
}

// Resize changes the size of the shell's terminal. A size it already has, a
// size under one cell and a shell that has ended change nothing.
func (s *Shell) Resize(columns, rows int) error {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()

	unchanged := columns == s.columns && rows == s.rows
	if s.closed || unchanged || columns < 1 || rows < 1 {
		return nil
	}

	if err := s.console.resize(columns, rows); err != nil {
		return err
	}

	s.outputMu.Lock()
	s.columns, s.rows = columns, rows
	s.outputMu.Unlock()

	return nil
}

// Screen returns the screen a window last kept for the shell, with the
// shell's current size. With none, or one older than the history, it is an
// empty screen at the history's start.
func (s *Shell) Screen() Screen {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()

	screen := s.screen
	if start := s.end - int64(len(s.output)); screen.Offset < start {
		screen = Screen{Offset: start}
	}

	screen.Columns, screen.Rows = s.columns, s.rows

	return screen
}

// SetScreen keeps the screen a window drew for the shell, for the next window
// that shows it. The size it carries is not kept, since the shell's own size
// is the one that counts.
func (s *Shell) SetScreen(screen Screen) {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()

	s.screen = Screen{Text: screen.Text, Offset: screen.Offset}
}

// Close ends the shell's program, and on Windows every process the program
// started. Only the first call does anything.
func (s *Shell) Close() {
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()

	if s.closed {
		return
	}

	// Marking the shell closed wakes the goroutine that waits for a window,
	// so that it reads the output on while the console closes.
	s.outputMu.Lock()
	s.closed = true
	s.emitChange()
	s.outputMu.Unlock()

	s.console.close()
}

// Wait waits until the shell has ended and its output is read to its end.
func (s *Shell) Wait() {
	<-s.done
}
