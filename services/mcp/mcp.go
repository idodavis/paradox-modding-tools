// Package mcp is PMT's server for coding agents. It serves PMT's tools, its instructions and its starter tasks over MCP
// to the agents on this computer, and passes on to the windows what an agent asks them to show.
package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// Service is PMT's MCP server. It listens from the app's start to its end, on this computer only, and answers only the
// requests that carry its token.
type Service struct {
	Workspace *workspace.Service
	LSP       *lsp.Service
	Health    *health.Service
	Graph     *graph.Service
	Wiki      *wiki.Service
	Build     string       // the app's version, which the server reports
	server    *http.Server // nil until the server listens
	mu        sync.Mutex   // guards the fields below
	token     string
	view      View
	pages     []Page
	pending   map[string]func() (any, error) // the changes the write tools previewed, by the token that applies each
}

// A Task is one starter task: a prompt PMT offers an agent, and the chip of the page shown that it carries.
type Task struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Chip  string `json:"chip"` // file, page, mod or event
}

// Tasks holds the starter tasks. Each text works with the chip attached, and without it through describe_view.
var Tasks = []Task{
	{Name: "fix-problems", Title: "Fix this file's problems", Chip: "file",
		Text: "Fix the problems of the file I have open in PMT: the attached file, or the one describe_view names. " +
			"List them with diagnose, change only what each one needs, and diagnose the file again before you finish."},
	{Name: "explain-page", Title: "Explain this page", Chip: "page",
		Text: "Explain the PMT page I have in front of me: what it shows here and what its controls do. " +
			"describe_view says which page and what it shows, and read_guide's pages topic describes each page."},
	{Name: "localize", Title: "Write the missing localization", Chip: "mod",
		Text: "Write the localization my mod is missing: the attached mod, or the one describe_view names. " +
			"build_report's coverage and missing-message findings say which keys and languages lack text. Write " +
			"the text in the mod's own localization files, then diagnose them."},
	{Name: "trace-event", Title: "Why does this event fire?", Chip: "event",
		Text: "Why does this event fire? Take the attached event, or the one describe_view names. Draw its Event " +
			"Graph with build_graph, follow what fires it back to where it starts, and tell me in plain words. " +
			"show can open the graph for me."},
}

// ServerName is the name PMT's server goes by in an agent's session and in the commands that register it.
const ServerName = "pmt"

// settingPort is the setting that holds the port the server listens on.
const settingPort = "agents.port"

// tokenFile is the file in PMT's app folder that holds the token.
const tokenFile = "mcp-token"

// The environment variables that give PMT's address and token to every program PMT starts.
const (
	variableURL   = "PMT_MCP_URL"
	variableToken = "PMT_MCP_TOKEN"
)

// Variables holds the environment variables of PMT's address and token, as the Terminal's settings list them. Their
// values are PMT's own environment's, so the rows carry none.
var Variables = []terminal.Variable{
	{Name: variableURL, Help: "The address of PMT's tools, for an agent run in the shell"},
	{Name: variableToken, Help: "The token those tools ask for"},
}

// instructions is what the server tells an agent when it connects. Its first paragraph stands alone, since Codex may
// read no more, and the whole stays under Claude's 4,096 characters.
const instructions = `PMT (Paradox Modding Tools) is the user's IDE for modding Crusader Kings III, Victoria 3 and ` +
	`Europa Universalis V. Their workspace is open in it: one game install and the mods loaded with it. PMT's tools ` +
	`answer from PMT's own reading of the game's script, the one behind its editor and its Health page. Find a name ` +
	`with them and never invent one; diagnose a file before calling a change done; write only in the workspace's mods.

Working rules:
- Names: definition, references, find_symbols and complete say what exists and where. A name they cannot find does ` +
	`not exist in this workspace.
- Problems: diagnose a file, or your own text before you write it, and fix the problems you brought in.
- Writing: only the workspace's mods change; the game's files are read only. rename, apply_edit and write_scaffold ` +
	`answer with a preview and a token, and change the files when called again with that token. A file with ` +
	`changes the user has not saved is left alone.
- The user's own: saving the editor's text, uploading to Steam and Settings. Never do these for them.
- Showing: show brings a file, a page or a Wiki look-up to the front in PMT; describe_view says what the user has in ` +
	`front of them.

Call read_guide for PMT's pages and their controls, how to make a change with PMT, and the open workspace's mods and ` +
	`folders.`

// ServiceStartup reads the token, making one at the first start, and starts listening on the port the settings
// name. A port another program holds leaves the server off until the next start, and PMT's log says so. It returns
// an error if the token cannot be read or made.
func (mcp *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	content, err := os.ReadFile(filepath.Join(mcp.Workspace.Config.Dirs.App, tokenFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		_, err = mcp.CreateToken()
	case err == nil:
		mcp.setToken(strings.TrimSpace(string(content)))
	}
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", mcp.readAddress())
	if err != nil {
		application.Get().Logger.Error("PMT's tools for coding agents are off", "error", err)
		return nil
	}
	mcp.server = &http.Server{Handler: mcp.newHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = mcp.server.Serve(listener) }()
	return nil
}

// ServiceShutdown stops the server.
func (mcp *Service) ServiceShutdown() error {
	if mcp.server == nil {
		return nil
	}
	return mcp.server.Close()
}

// ReadURL returns the address an agent reaches the server at, on the port the settings name.
func (mcp *Service) ReadURL() string {
	return "http://" + mcp.readAddress() + "/mcp"
}

// readAddress returns the host and port the server listens on: this computer's loopback address, at the port the
// settings name.
func (mcp *Service) readAddress() string {
	port, _ := mcp.Workspace.Config.LookupSetting("", settingPort)
	return net.JoinHostPort("127.0.0.1", port)
}

// ReadToken returns the token an agent sends to the server.
func (mcp *Service) ReadToken() string {
	mcp.mu.Lock()
	defer mcp.mu.Unlock()
	return mcp.token
}

// CreateToken makes a new token, which the server asks for from then on, and returns it. Every agent registered with
// the old token must be registered again. It returns an error if the token cannot be saved.
func (mcp *Service) CreateToken() (string, error) {
	token := rand.Text()
	if err := files.WriteAtomic(filepath.Join(mcp.Workspace.Config.Dirs.App, tokenFile), []byte(token+"\n")); err != nil {
		return "", err
	}
	mcp.setToken(token)
	return token, nil
}

// ListTasks returns the starter tasks, which an empty session offers as buttons.
func (mcp *Service) ListTasks() []Task { return Tasks }

// setToken keeps the token the server asks for. PMT's own environment carries it and the server's address, so every
// program PMT starts, a shell or an agent, has them.
func (mcp *Service) setToken(token string) {
	mcp.mu.Lock()
	mcp.token = token
	mcp.mu.Unlock()
	_ = os.Setenv(variableURL, mcp.ReadURL())
	_ = os.Setenv(variableToken, token)
}

// newHandler returns the server's HTTP handler. It answers MCP at /mcp. A request without the token is refused, as is
// one sent from a web page of another site; the SDK refuses one whose Host is not this computer.
func (mcp *Service) newHandler() http.Handler {
	server := sdk.NewServer(
		&sdk.Implementation{Name: ServerName, Title: "Paradox Modding Tools", Version: mcp.Build},
		&sdk.ServerOptions{Instructions: instructions},
	)
	mcp.addEditorTools(server)
	mcp.addPageTools(server)
	mcp.addAppTools(server)
	addPrompts(server)
	streamable := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true})
	gate := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		sent := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(sent), []byte(mcp.ReadToken())) != 1 {
			http.Error(writer, "PMT's token is missing or wrong", http.StatusUnauthorized)
			return
		}
		streamable.ServeHTTP(writer, request)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", http.NewCrossOriginProtection().Handler(gate))
	return mux
}

// addPrompts adds the starter tasks to the server as prompts.
func addPrompts(server *sdk.Server) {
	for _, task := range Tasks {
		server.AddPrompt(&sdk.Prompt{Name: task.Name, Title: task.Title},
			func(context.Context, *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
				return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{
					{Role: "user", Content: &sdk.TextContent{Text: task.Text}},
				}}, nil
			})
	}
}
