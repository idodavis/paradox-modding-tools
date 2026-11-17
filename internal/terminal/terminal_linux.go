// terminal_linux.go runs a shell on a Linux pseudo-terminal and lists the
// shells this system has.

package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// console is one Linux pseudo-terminal with the program that runs on it.
type console struct {
	// master is the side of the terminal that PMT reads and writes. The
	// program has the other side.
	master *os.File

	command *exec.Cmd
}

// ListPrograms returns the shells this system has, PMT's pick first.
func ListPrograms() []Program {
	paths := []string{os.Getenv("SHELL")}
	if text, err := os.ReadFile("/etc/shells"); err == nil {
		for line := range strings.SplitSeq(string(text), "\n") {
			if line = strings.TrimSpace(line); strings.HasPrefix(line, "/") {
				paths = append(paths, line)
			}
		}
	}

	if path, err := exec.LookPath("sh"); err == nil {
		paths = append(paths, path)
	}

	var programs []Program
	var stats []os.FileInfo
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil || stat.IsDir() {
			continue
		}

		// /bin/bash and /usr/bin/bash are often one file.
		if slices.ContainsFunc(stats, func(other os.FileInfo) bool { return os.SameFile(other, stat) }) {
			continue
		}

		stats = append(stats, stat)
		programs = append(programs, Program{Name: filepath.Base(path), Path: path})
	}

	return programs
}

// startConsole starts the shell's program on a new pseudo-terminal of the
// shell's size, with the given environment.
func startConsole(shell *Shell, env []string) (*console, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*console, error) {
		_ = master.Close()

		return nil, err
	}

	console := &console{master: master}

	// The other side of the terminal starts locked, and its number names its
	// file.
	if err := console.control(func(fd int) error { return unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0) }); err != nil {
		return fail(err)
	}

	var number int
	err = console.control(func(fd int) (err error) {
		number, err = unix.IoctlGetInt(fd, unix.TIOCGPTN)

		return err
	})
	if err != nil {
		return fail(err)
	}

	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return fail(err)
	}

	// The program holds its own copies of the other side.
	defer func() { _ = slave.Close() }()

	if err := console.resize(shell.columns, shell.rows); err != nil {
		return fail(err)
	}

	console.command = exec.Command(shell.Program, shell.Args...)
	console.command.Dir, console.command.Env = shell.Folder, env
	console.command.Stdin, console.command.Stdout, console.command.Stderr = slave, slave, slave

	// The terminal is the program's standard input, so Ctty 0 makes it the
	// new session's controlling terminal.
	console.command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := console.command.Start(); err != nil {
		return fail(err)
	}

	return console, nil
}

// control runs the given function with the terminal's descriptor. Unlike
// File.Fd, it leaves the file non-blocking, so that closing it still wakes a
// read that is waiting.
func (c *console) control(run func(fd int) error) error {
	conn, err := c.master.SyscallConn()
	if err != nil {
		return err
	}

	var inner error
	if err := conn.Control(func(fd uintptr) { inner = run(int(fd)) }); err != nil {
		return err
	}

	return inner
}

// read reads the terminal's output. It returns an error once every program
// has let go of the terminal, or once it is closed.
func (c *console) read(chunk []byte) (int, error) {
	return c.master.Read(chunk)
}

// write writes the given bytes to the terminal's input.
func (c *console) write(input []byte) (int, error) {
	return c.master.Write(input)
}

// resize changes the terminal's size, which the program hears of as SIGWINCH.
func (c *console) resize(columns, rows int) error {
	size := &unix.Winsize{Col: uint16(columns), Row: uint16(rows)}

	return c.control(func(fd int) error { return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, size) })
}

// close closes the terminal, which hangs up on the program's session.
func (c *console) close() {
	_ = c.master.Close()
}

// wait waits until the program exits.
func (c *console) wait() {
	_ = c.command.Wait()
}
