// process.go serves the agent stream.

package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
)

const streamName = "agent"

// opening is a window's first message on the stream: the agent to start and
// the active file, which may be "".
type opening struct {
	Agent string `json:"agent"`
	Path  string `json:"path"`
}

// Start is Go's first message on the stream: where the agent started, or the
// error that kept it from starting.
type Start struct {
	// Folder is the folder the program runs in, and Directories are the other
	// folders it may work in.
	Folder      string   `json:"folder"`
	Directories []string `json:"directories"`

	// Plans is the folder where the agent is to keep its plan files, under
	// the workspace's folder. It is "" with no workspace open.
	Plans string `json:"plans"`

	// Meta is the metadata the window gives each of the agent's sessions, nil
	// for an agent that takes none.
	Meta map[string]any `json:"meta"`

	Error string `json:"error"`
}

// program is one running agent program and the group that holds what it
// starts.
type program struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	stderr  io.ReadCloser
	group   group

	// agent is the agent the program runs.
	agent Agent

	// ended makes end run once, since both the stream and an uninstall may
	// end the program.
	ended sync.Once
}

// ServiceStartup serves the agent stream.
func (s *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	application.Get().HandleStream(streamName, s.serve)

	return nil
}

// serve serves one window's stream until either side ends it.
func (s *Service) serve(conn *application.StreamConn) {
	defer func() { _ = conn.Close() }()

	var first opening
	if conn.ReceiveJSON(&first) != nil {
		return
	}

	running, start, err := s.startProgram(first)
	if err != nil {
		_ = conn.SendJSON(Start{Error: err.Error()})

		return
	}

	s.programs.Store(running, struct{}{})
	defer func() {
		s.programs.Delete(running)
		running.end()
	}()

	if conn.SendJSON(start) != nil {
		return
	}

	go sendLines(conn, running.output)
	go logLines(first.Agent, running.stderr)

	for {
		message, err := conn.Receive()
		if err != nil {
			return
		}

		if _, err := running.input.Write(append(message, '\n')); err != nil {
			return
		}
	}
}

// startProgram starts the program of the agent an opening names, where that
// agent starts, and returns it with the Start to send. It fetches nothing, so
// an agent never waits on the network to start.
func (s *Service) startProgram(first opening) (*program, Start, error) {
	node := s.DescribeNode()
	agent, err := s.findAgent(first.Agent, node)
	if err != nil {
		return nil, Start{}, err
	}

	start, args := s.placeCompletion(first.Path), agent.Args
	if !agent.Completion {
		var extra []string
		if start, extra, err = s.placeChat(agent); err != nil {
			return nil, Start{}, err
		}

		args = slices.Concat(args, extra)
	}

	command := exec.Command(agent.Program, args...)
	command.Dir = start.Folder
	command.Env = os.Environ()
	if node.Path != "" {
		command.Env = addToPath(command.Env, filepath.Dir(node.Path))
	}
	command.Env = append(command.Env, agent.Env...)
	prepareCommand(command)

	running := &program{command: command, agent: agent}
	if running.input, err = command.StdinPipe(); err != nil {
		return nil, Start{}, err
	}
	if running.output, err = command.StdoutPipe(); err != nil {
		return nil, Start{}, err
	}
	if running.stderr, err = command.StderrPipe(); err != nil {
		return nil, Start{}, err
	}
	if running.group, err = startGroup(command); err != nil {
		return nil, Start{}, err
	}

	return running, start, nil
}

// placeChat returns where an agent's chat starts, and the arguments its
// program takes for that beyond its own. The chat starts in the workspace's
// folder, made if it is missing, with the folders of the user's own mods as
// its other folders: an agent whose row has a DirArg takes them as arguments,
// any other over the protocol. The workspace's read-only folders are never
// among them, and an agent whose row guards is given the rules that refuse
// its file changes there, as arguments or as its sessions' metadata. With no
// workspace open the chat starts in the home folder. It returns an error if
// the workspace's folder cannot be made.
func (s *Service) placeChat(agent Agent) (Start, []string, error) {
	if _, err := s.Workspace.Session(); err != nil {
		return s.placeCompletion(""), nil, nil
	}

	folder, err := s.Workspace.MakeFolder()
	if err != nil {
		return Start{}, nil, err
	}

	start := Start{Folder: folder, Plans: filepath.Join(folder, plansFolder), Directories: []string{}}
	for _, mod := range s.Workspace.ListMods() {
		if !mod.Broken && !mod.External && files.ToKey(mod.Dir) != files.ToKey(folder) {
			start.Directories = append(start.Directories, mod.Dir)
		}
	}

	var args []string
	if agent.DirArg != "" {
		for _, dir := range start.Directories {
			args = append(args, agent.DirArg, dir)
		}
	}

	readOnly := s.Workspace.ListReadOnly()
	switch {
	case agent.DenyArg != "":
		// A rule ends at a closing parenthesis, so the parentheses of a path
		// such as C:/Program Files (x86) are written as ?, any one character.
		plain := strings.NewReplacer("(", "?", ")", "?")
		for _, dir := range readOnly {
			args = append(args, fmt.Sprintf(agent.DenyArg, plain.Replace(filepath.ToSlash(dir))))
		}

	case agent.Guards:
		config := s.Workspace.Config
		start.Meta = writeSettings(
			readOnly, config.ListSetting("", "agents.shellAllowed"), config.ListSetting("", "agents.shellRefused"),
		)
	}

	return start, args, nil
}

// plansFolder is the folder under the workspace's folder where agents keep
// their plan files.
const plansFolder = "plans"

// writeSettings returns the session metadata that carries Claude's settings.
// Claude's adapter reads the options of a session's Claude under
// claudeCode.options. The settings keep Claude from changing anything in the
// given folders, by a deny rule each, which holds in every mode, the one that
// asks nothing included. They also have Claude write its plan files in the
// plans folder of the folder it starts in, where it would use its own folder
// in the user's home. The user's shell rules go in too, so that Claude runs a
// command that starts with an allowed start without asking, and refuses one
// that starts with a refused start, in every mode.
//
// A rule names a path from the system's root with two slashes, and a Windows
// drive as a folder of its letter: Edit(//c/Games/CK3/**) for C:\Games\CK3.
// A shell rule names the command's start: Bash(git status:*).
func writeSettings(dirs, allowed, refused []string) map[string]any {
	deny := make([]string, len(dirs), len(dirs)+len(refused))
	for position, dir := range dirs {
		path := filepath.ToSlash(dir)
		if volume := filepath.VolumeName(dir); len(volume) == 2 {
			path = "/" + strings.ToLower(volume[:1]) + path[2:]
		}

		deny[position] = "Edit(/" + path + "/**)"
	}

	allow := make([]string, len(allowed))
	for position, start := range allowed {
		allow[position] = "Bash(" + start + ":*)"
	}

	for _, start := range refused {
		deny = append(deny, "Bash("+start+":*)")
	}

	settings := map[string]any{
		"permissions":    map[string]any{"allow": allow, "deny": deny},
		"plansDirectory": plansFolder,
	}

	return map[string]any{"claudeCode": map[string]any{"options": map[string]any{"settings": settings}}}
}

// placeCompletion returns where a program that completes code starts: in the
// folder of the mod that holds the given file, else the first mod that has a
// folder, else the home folder, with every other mod's folder beside it. It
// changes no file, so an external mod's folder is one of them.
func (s *Service) placeCompletion(path string) Start {
	folder, origin := s.Workspace.ResolveFolder(path)
	start := Start{Folder: folder, Directories: []string{}}
	for _, mod := range s.Workspace.ListMods() {
		if !mod.Broken && mod.ID != origin {
			start.Directories = append(start.Directories, mod.Dir)
		}
	}

	return start
}

// end ends the program and everything it started, and waits for it. A second
// call waits for the first.
func (p *program) end() {
	p.ended.Do(func() {
		p.group.end()
		_ = p.command.Wait()
	})
}

// listPrograms returns the running programs of an agent.
func (s *Service) listPrograms(id string) []*program {
	var running []*program
	s.programs.Range(func(key, _ any) bool {
		if candidate := key.(*program); candidate.agent.ID == id {
			running = append(running, candidate)
		}

		return true
	})

	return running
}

func addToPath(env []string, dir string) []string {
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// sendLines sends each line the agent writes as one message, then closes the
// stream.
func sendLines(conn *application.StreamConn, output io.Reader) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReaderSize(output, 1<<16)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && conn.Send(trimmed) != nil {
			return
		}

		if err != nil {
			return
		}
	}
}

// logLines writes each line of an agent's error output to PMT's log. A line
// over one megabyte ends the logging, and the rest is read and dropped, so
// that the agent never waits on a full pipe.
func logLines(id string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(nil, 1<<20)

	for scanner.Scan() {
		application.Get().Logger.Info(scanner.Text(), "agent", id)
	}

	_, _ = io.Copy(io.Discard, stderr)
}
