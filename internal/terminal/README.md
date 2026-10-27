# terminal

The package runs shells under the system's own pseudo-terminal: Windows' pseudo console (ConPTY), or a Linux pty. It
keeps each shell's recent output and the screen a window last drew, so that another window can take a shell over.

It uses `golang.org/x/sys` and nothing else of a terminal library's: the calls are few, and each library tried crashed
on a second close, left a reader blocked, or pulled in far more than it gave (`.superpowers/sdd/parity/research/
terminal.md`). It knows no workspace and no Wails, so its Linux half builds and runs anywhere Linux does.

| file | holds |
|---|---|
| `terminal.go` | `Shell`, `Screen`, `Program`, `Variable`; the environment every shell is given; the shell's `Start`, `Write`, `Resize`, `Read`, `Acknowledge`, `Screen`, `SetScreen`, `Close`, `Wait` |
| `terminal_windows.go` | the pseudo console and its process, the console's UTF-8 code pages, and the shells Windows has |
| `terminal_linux.go` | the pty and its session, and the shells Linux has |

## A shell's life

A `Shell` is filled in and then started, as an `exec.Cmd` is: its starter sets the id, the program and its arguments,
the folder and the mod it is the folder of, and `Start` opens the pseudo-terminal at a size and starts the program in
it. No exported field changes after `Start`, so a `Shell` is what the page is sent.

- **Output.** One goroutine reads the pseudo-terminal for as long as it is open, and keeps the newest 4 MiB with the
  offset of every byte the shell has written. `Read` returns the output past an offset, waiting until there is some,
  and skips what the history no longer holds.
- **Pace.** A window tells the shell with `Acknowledge` how far it has drawn the output. While the output runs more
  than 512 KiB past the furthest a window has drawn, the goroutine stops reading, so the program waits on its own
  writes, as VS Code's terminal makes it. A shell that is closing is read on, since a pseudo console does not close
  until its output is read.
- **Input and size.** `Write` sends bytes as typed, and `Resize` changes the size; both, and `Close`, take one lock, so
  a resize never reaches a closed console.
- **The end.** `Close` runs once and ends the program; on Windows it ends every process the program started too. The
  program's own exit closes the console as well. `Wait` returns once the output is read to its end.
- **The screen.** `SetScreen` keeps the screen a window serialized and the offset of the output it holds; `Screen`
  returns it with the shell's current size. A window that starts to show the shell draws that screen, then the output
  past its offset. A screen older than the history is given as an empty one at the history's start, so that the
  window's count of the output it draws stays true.

## Windows

The pseudo console is `CreatePseudoConsole` over two pipes, and the program starts with the console as a process
attribute. `STARTF_USESTDHANDLES` is set, or the program writes to PMT's own handles rather than to the console. Closing
the pseudo console ends the process tree.

A console starts on the system's code page (437 here), which turns a program's raw UTF-8 bytes, as git and rg write
them, into mojibake. Right after the program starts, PMT attaches to its console for a moment and sets both code pages
to UTF-8. A process that has a console of its own, as a dev build has, cannot attach, so a dev build keeps the
system's code page.

The shells, as VS Code finds them:

- PowerShell 7: `pwsh` on the PATH, else the install folder its install key names. PMT's pick.
- Windows PowerShell, in System32.
- Git Bash: `bin\bash.exe` in Git for Windows' install folder, the folder of `git` on the PATH, else the one its
  install key names. It starts as `bash --login -i`, so the user's profile and prompt apply.
- WSL: `wsl.exe`, which starts the default distribution's shell, listed while WSL has one. It is one entry, so a
  distribution that serves another program, as Docker Desktop's does, is not listed of its own.

## Linux

The pty is `/dev/ptmx`, unlocked, with its other side as the program's three standard files. The program leads a
session of its own with the pty as its controlling terminal. Every ioctl goes through `SyscallConn`, which keeps the
file non-blocking, so that `Close` wakes a reader that is waiting. Closing the pty hangs up on the session.

The shells: `$SHELL`, then each shell `/etc/shells` lists, then `sh`, each file once.

## The environment

Every shell gets PMT's own environment, with these over it: `COLORTERM=truecolor`; on Linux `TERM=xterm-256color`; on
Windows `CLAUDE_CODE_ALT_SCREEN_FULL_REPAINT=1`, Claude Code's documented fix for the fragments a pseudo console leaves
on its full screen. `ListEnvironment` lists them for Settings.
