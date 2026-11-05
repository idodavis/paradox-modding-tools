// process.go serves the stream each window opens to an agent and runs the agent's program.

package agent

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/workspace"
)

// streamName is the name a window opens an agent's stream by.
const streamName = "agent"

// An opening is a window's first message on an agent's stream: the agent to start, and the active file, whose mod's
// folder the agent starts in. The file may be "".
type opening struct {
	Agent string `json:"agent"`
	Path  string `json:"path"`
}

// A Start is Go's first message on an agent's stream: the folder the agent started in and the workspace's other mod
// folders, or the error that kept it from starting.
type Start struct {
	Folder      string   `json:"folder"`
	Directories []string `json:"directories"`
	Error       string   `json:"error"`
}

// A program is one agent's running program, with its pipes and the group that holds it and every program it starts.
type program struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	stderr  io.ReadCloser
	group   group
}

// ServiceStartup serves the stream that each window opens to an agent.
func (agt *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	application.Get().HandleStream(streamName, agt.serve)
	return nil
}

// serve serves one window's stream to an agent. The window's first message names the agent and the active file. Closing
// the stream ends the agent and every program it started, and the agent's end closes the stream.
func (agt *Service) serve(conn *application.StreamConn) {
	defer func() { _ = conn.Close() }()
	var first opening
	if conn.ReceiveJSON(&first) != nil {
		return
	}
	folder, origin := agt.Workspace.ResolveFolder(first.Path)
	running, err := agt.startProgram(first.Agent, folder)
	if err != nil {
		_ = conn.SendJSON(Start{Error: err.Error()})
		return
	}
	defer running.end()
	directories := lo.FilterMap(agt.Workspace.ListMods(), func(mod workspace.Mod, _ int) (string, bool) {
		return mod.Dir, !mod.Broken && mod.ID != origin
	})
	if conn.SendJSON(Start{Folder: folder, Directories: directories}) != nil {
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

// startProgram starts an agent's program in a folder, with the Node the agents run on first on its PATH. It returns an
// error if findAgent fails or the program cannot be started.
func (agt *Service) startProgram(id, folder string) (*program, error) {
	agent, err := agt.findAgent(id)
	if err != nil {
		return nil, err
	}
	command := exec.Command(agent.Program, agent.Args...)
	command.Dir = folder
	if node := agt.DescribeNode(); node.Path != "" {
		command.Env = addToPath(os.Environ(), filepath.Dir(node.Path))
	}
	prepareCommand(command)
	running := &program{command: command}
	if running.input, err = command.StdinPipe(); err != nil {
		return nil, err
	}
	if running.output, err = command.StdoutPipe(); err != nil {
		return nil, err
	}
	if running.stderr, err = command.StderrPipe(); err != nil {
		return nil, err
	}
	if running.group, err = startGroup(command); err != nil {
		return nil, err
	}
	return running, nil
}

// end ends the program and every program it started, and waits for it.
func (program *program) end() {
	program.group.end()
	_ = program.command.Wait()
}

// addToPath returns an environment with a folder put first on its PATH.
func addToPath(env []string, dir string) []string {
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// sendLines sends each line an agent writes as one message, then closes the stream when the agent ends.
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

// logLines writes each line of an agent's error output to PMT's log, under the agent's id, until the agent ends.
func logLines(id string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		application.Get().Logger.Info(scanner.Text(), "agent", id)
	}
}
