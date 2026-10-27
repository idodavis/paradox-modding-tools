package terminal

import (
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"paradox-modding-tools/internal/files"
)

// console is one Windows pseudo console with the program that runs under it.
type console struct {
	handle  windows.Handle // the pseudo console
	input   windows.Handle // the end of its input pipe that PMT writes
	output  windows.Handle // the end of its output pipe that PMT reads
	process windows.Handle
}

// utf8CodePage is the console code page of UTF-8.
const utf8CodePage = 65001

// The console calls that golang.org/x/sys/windows does not wrap.
var (
	kernel32      = windows.NewLazySystemDLL("kernel32.dll")
	attachConsole = kernel32.NewProc("AttachConsole")
	freeConsole   = kernel32.NewProc("FreeConsole")
)

// attachMu holds PMT to one console at a time, since a process is attached to one console at most.
var attachMu sync.Mutex

// ListPrograms returns the shells this system has, PMT's pick first: PowerShell 7, as the PATH or else its install key
// finds it, then Windows PowerShell, then Git Bash and WSL where they are installed.
func ListPrograms() []Program {
	var programs []Program
	if path, ok := findPowerShell7(); ok {
		programs = append(programs, Program{Name: "PowerShell 7", Path: path})
	}
	system, err := windows.GetSystemDirectory()
	path := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	if err == nil && files.IsFile(path) {
		programs = append(programs, Program{Name: "Windows PowerShell", Path: path})
	}
	if path, ok := findGitBash(); ok {
		// a login shell, as VS Code starts it, so that the user's profile and prompt apply
		programs = append(programs, Program{Name: "Git Bash", Path: path, Args: []string{"--login", "-i"}})
	}
	if path := filepath.Join(system, "wsl.exe"); err == nil && hasWSL() && files.IsFile(path) {
		programs = append(programs, Program{Name: "WSL", Path: path})
	}
	return programs
}

// findPowerShell7 returns the path of PowerShell 7's pwsh.exe: the one on the PATH, else the one an install key of
// PowerShell 7 names. It returns false if neither exists.
func findPowerShell7() (string, bool) {
	if path, err := exec.LookPath("pwsh"); err == nil {
		return path, true
	}
	versions, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\PowerShellCore\InstalledVersions`,
		registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return "", false
	}
	defer func() { _ = versions.Close() }()
	names, _ := versions.ReadSubKeyNames(-1)
	for _, name := range names {
		version, err := registry.OpenKey(versions, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		dir, _, err := version.GetStringValue("InstallDir")
		_ = version.Close()
		if path := filepath.Join(dir, "pwsh.exe"); err == nil && files.IsFile(path) {
			return path, true
		}
	}
	return "", false
}

// findGitBash returns the path of Git for Windows' bash.exe in its install folder: the folder of git on the PATH, else
// the one its install key names, for all users or for this one. It returns false if none holds it.
func findGitBash() (string, bool) {
	var dirs []string
	// git.exe sits in the install folder's cmd or bin folder
	if git, err := exec.LookPath("git"); err == nil {
		dirs = append(dirs, filepath.Dir(filepath.Dir(git)))
	}
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		key, err := registry.OpenKey(root, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		dir, _, err := key.GetStringValue("InstallPath")
		_ = key.Close()
		if err == nil {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		if path := filepath.Join(dir, "bin", "bash.exe"); files.IsFile(path) {
			return path, true
		}
	}
	return "", false
}

// hasWSL reports whether WSL has a distribution that wsl.exe starts by default. Without one, wsl.exe only offers to
// install WSL.
func hasWSL() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Lxss`,
		registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = key.Close() }()
	_, _, err = key.GetStringValue("DefaultDistribution")
	return err == nil
}

// startConsole starts the shell's program under a new pseudo console of the shell's size, with the given environment,
// and sets the shell's build. The console's code pages are set to UTF-8. It returns an error if any step fails.
func startConsole(shell *Shell, env []string) (*console, error) {
	var inRead, outWrite windows.Handle
	console := &console{}
	if err := windows.CreatePipe(&inRead, &console.input, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&console.output, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, console.input)
		return nil, err
	}
	// the pseudo console keeps its own copies of the two ends it is given
	defer closeHandles(inRead, outWrite)
	size := windows.Coord{X: int16(shell.columns), Y: int16(shell.rows)}
	if err := windows.CreatePseudoConsole(size, inRead, outWrite, 0, &console.handle); err != nil {
		closeHandles(console.input, console.output)
		return nil, err
	}
	process, err := startProcess(console.handle, shell, env)
	if err != nil {
		console.close()
		closeHandles(console.output)
		return nil, err
	}
	console.process = process.Process
	setUTF8(process.ProcessId)
	shell.Build = int(windows.RtlGetVersion().BuildNumber)
	return console, nil
}

// startProcess starts the shell's program with its arguments in its folder, with the given environment, attached to
// the given pseudo console. It returns the process and its id. It returns an error if the program cannot be started.
func startProcess(handle windows.Handle, shell *Shell, env []string) (windows.ProcessInformation, error) {
	var process windows.ProcessInformation
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return process, err
	}
	defer attributes.Delete()
	// the attribute takes the pseudo console's handle itself as its value, not a pointer to it
	value := *(*unsafe.Pointer)(unsafe.Pointer(&handle))
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, value, unsafe.Sizeof(handle)); err != nil {
		return process, err
	}
	startup := &windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	// with no handles of its own, a program would write to the handles PMT was given instead of to the console
	startup.Flags = windows.STARTF_USESTDHANDLES
	line := windows.ComposeCommandLine(append([]string{shell.Program}, shell.Args...))
	commandLine, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return process, err
	}
	folder, err := windows.UTF16PtrFromString(shell.Folder)
	if err != nil {
		return process, err
	}
	block, err := buildBlock(env)
	if err != nil {
		return process, err
	}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	err = windows.CreateProcess(nil, commandLine, nil, nil, false, flags, &block[0], folder, &startup.StartupInfo, &process)
	if err != nil {
		return process, err
	}
	closeHandles(process.Thread)
	return process, nil
}

// buildBlock returns the given NAME=value strings as the block of UTF-16 strings that CreateProcess takes.
func buildBlock(env []string) ([]uint16, error) {
	var block []uint16
	for _, entry := range env {
		text, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, text...)
	}
	return append(block, 0), nil
}

// setUTF8 sets the code pages of the given process's console to UTF-8, so that UTF-8 output such as git's prints
// right. PMT attaches to the console for a moment, retrying for two seconds while the process starts; a dev build,
// which has a console of its own, cannot attach.
func setUTF8(processID uint32) {
	attachMu.Lock()
	defer attachMu.Unlock()
	for began := time.Now(); time.Since(began) < 2*time.Second; time.Sleep(5 * time.Millisecond) {
		attached, _, err := attachConsole.Call(uintptr(processID))
		if attached != 0 {
			_ = windows.SetConsoleOutputCP(utf8CodePage)
			_ = windows.SetConsoleCP(utf8CodePage)
			_, _, _ = freeConsole.Call()
			return
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return
		}
	}
}

// read reads the console's output. Once the output has ended, it lets go of the pipe and returns the error.
func (console *console) read(chunk []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(console.output, chunk, &n, nil)
	if err != nil {
		closeHandles(console.output)
	}
	return int(n), err
}

// write writes the given bytes to the console's input.
func (console *console) write(input []byte) (int, error) {
	var n uint32
	err := windows.WriteFile(console.input, input, &n, nil)
	return int(n), err
}

// resize changes the size of the pseudo console, which then draws its screen again.
func (console *console) resize(columns, rows int) error {
	return windows.ResizePseudoConsole(console.handle, windows.Coord{X: int16(columns), Y: int16(rows)})
}

// close closes the pseudo console, which ends every process attached to it, and its input pipe.
func (console *console) close() {
	windows.ClosePseudoConsole(console.handle)
	closeHandles(console.input)
}

// wait waits until the program exits, then lets go of its process.
func (console *console) wait() {
	_, _ = windows.WaitForSingleObject(console.process, windows.INFINITE)
	closeHandles(console.process)
}

// closeHandles closes the given handles. An error is passed over, since nothing can be done about a handle that will
// not close.
func closeHandles(handles ...windows.Handle) {
	for _, handle := range handles {
		_ = windows.CloseHandle(handle)
	}
}
