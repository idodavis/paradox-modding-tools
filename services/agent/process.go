// process.go serves the agent stream.

package agent

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const streamName = "agent"

// opening is a window's first message on the stream: the agent to start and
// the active file, which may be "".
type opening struct {
	Agent string `json:"agent"`
	Path  string `json:"path"`
}

// Start is Go's first message on the stream: the folder the agent started in
// and the other mod folders, or the error that kept it from starting.
type Start struct {
	Folder      string   `json:"folder"`
	Directories []string `json:"directories"`
	Error       string   `json:"error"`
}

type program struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	stderr  io.ReadCloser
	group   group
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

	folder, origin := s.Workspace.ResolveFolder(first.Path)
	running, err := s.startProgram(first.Agent, folder)
	if err != nil {
		_ = conn.SendJSON(Start{Error: err.Error()})

		return
	}
	defer running.end()

	directories := []string{}
	for _, mod := range s.Workspace.ListMods() {
		if !mod.Broken && mod.ID != origin {
			directories = append(directories, mod.Dir)
		}
	}

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

// startProgram starts an agent's program in a folder. It returns an error if
// the agent cannot start.
func (s *Service) startProgram(id, folder string) (*program, error) {
	agent, err := s.findAgent(id)
	if err != nil {
		return nil, err
	}

	command := exec.Command(agent.Program, agent.Args...)
	command.Dir = folder
	if node := s.DescribeNode(); node.Path != "" {
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

func (p *program) end() {
	p.group.end()
	_ = p.command.Wait()
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

func logLines(id string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(nil, 1<<20)

	for scanner.Scan() {
		application.Get().Logger.Info(scanner.Text(), "agent", id)
	}
}
