// Package mcp is PMT's server for coding agents. It serves PMT's tools, its
// instructions and its starter tasks over MCP to the agents on this computer,
// and passes on to the windows what an agent asks them to show.
//
// The tools hold no logic of their own: each answers through the method of
// another service that answers the same question for a page. The server
// applies these rules:
//
//   - It listens on this computer's loopback address, at the port of the
//     setting agents.port, and answers a request only if it carries the token.
//   - A tool that changes no file is marked read only, so the assistant pane
//     can answer an agent's request to use it. rename, apply_edit and
//     write_scaffold are not marked.
//   - Those three change only the workspace's mods. Called without a token,
//     each answers with the change it would make and a token. Called again
//     with that token, it makes the change once.
//   - A name the workspace does not have gets an error that says where a
//     right one comes from, never an empty answer that reads as "none".
//
// # Robustness
//
// A request without the token, or sent from a web page of another site, is
// refused. A request with no Origin, as an agent's is, passes. If another
// program holds the port, the server stays off until the next start and the
// log says so.
//
// show checks the whole request and sends nothing if any part is wrong. A
// write tool refuses a file that is not a mod's, and one with text the user
// has not saved. A token that no preview holds is refused. A limit of zero or
// less, or one left out, is read as the tool's default. The picks of a coat
// are not checked.
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
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// Service is PMT's MCP server.
type Service struct {
	Workspace *workspace.Service
	LSP       *lsp.Service
	Health    *health.Service
	Graph     *graph.Service
	Wiki      *wiki.Service
	Draw      *draw.Service

	// Build is the app's version.
	Build string

	// server is nil until the server listens.
	server *http.Server

	// mu guards the fields below.
	mu    sync.Mutex
	token string
	view  View
	pages []Page

	// pending holds the changes the write tools previewed, by token.
	pending map[string]func() (any, error)
}

// Task is one starter task: a prompt PMT offers an agent.
type Task struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Text  string `json:"text"`

	// Chip is file, page, mod or event.
	Chip string `json:"chip"`

	// Edits is true for a task that changes the file it is about.
	Edits bool `json:"edits"`
}

// Tool is one of PMT's tools, as an agent is told of it.
type Tool struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	ReadOnly bool   `json:"readOnly"`
}

// Access is what an agent needs to reach the server.
type Access struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Tasks holds the starter tasks.
var Tasks = []Task{
	{Name: "fix-problems", Title: "Fix this file's problems", Chip: "file", Edits: true,
		Text: "Fix the problems of the file I have open in PMT: the attached file, or the one describe_view names. " +
			"List them with diagnose, change only what each one needs, and diagnose the file again before you finish."},
	{Name: "explain-file", Title: "Explain this file", Chip: "file",
		Text: "Explain the file I have open in PMT: the attached file, or the one describe_view names. Go through it " +
			"entry by entry and say what each one does, in the game's own words. Use hover and definition for each " +
			"name you explain."},
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

// ServerName is the name PMT's server goes by in an agent's session.
const ServerName = "pmt"

const tokenFile = "mcp-token"

const (
	variableURL   = "PMT_MCP_URL"
	variableToken = "PMT_MCP_TOKEN"
)

// Variables lists the environment variables of the address and token.
var Variables = []terminal.Variable{
	{Name: variableURL, Help: "The address of PMT's tools, for an agent run in the shell"},
	{Name: variableToken, Help: "The token those tools ask for"},
}

// ServiceStartup reads the token, making one at the first start, and starts
// listening. It returns an error if the token cannot be read or made.
func (s *Service) ServiceStartup(context.Context, application.ServiceOptions) error {
	content, err := os.ReadFile(filepath.Join(s.Workspace.Config.Dirs.App, tokenFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		_, err = s.CreateToken()
	case err == nil:
		s.setToken(strings.TrimSpace(string(content)))
	}
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", s.readAddress())
	if err != nil {
		application.Get().Logger.Error("PMT's tools for coding agents are off", "error", err)

		return nil
	}

	s.server = &http.Server{Handler: s.newHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.server.Serve(listener) }()

	return nil
}

// CreateToken makes a new token and returns it. Every agent registered with
// the old one must be registered again. It returns an error if the token
// cannot be saved.
func (s *Service) CreateToken() (string, error) {
	token := rand.Text()
	if err := files.WriteAtomic(filepath.Join(s.Workspace.Config.Dirs.App, tokenFile), []byte(token+"\n")); err != nil {
		return "", err
	}

	s.setToken(token)

	return token, nil
}

// setToken keeps the token and puts it and the address in PMT's environment.
func (s *Service) setToken(token string) {
	s.mu.Lock()
	s.token = token
	s.mu.Unlock()

	_ = os.Setenv(variableURL, s.readURL())
	_ = os.Setenv(variableToken, token)
}

func (s *Service) readURL() string {
	return "http://" + s.readAddress() + "/mcp"
}

const settingPort = "agents.port"

func (s *Service) readAddress() string {
	port, _ := s.Workspace.Config.LookupSetting("", settingPort)

	return net.JoinHostPort("127.0.0.1", port)
}

// newHandler returns the HTTP handler that answers MCP at /mcp. It refuses a
// request without the token and one from a web page of another site.
func (s *Service) newHandler() http.Handler {
	server := s.newServer()
	streamable := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server },
		&sdk.StreamableHTTPOptions{Stateless: true})
	gate := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		bearer := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(bearer), []byte(s.readToken())) != 1 {
			http.Error(writer, "PMT's token is missing or wrong", http.StatusUnauthorized)

			return
		}

		streamable.ServeHTTP(writer, request)
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", http.NewCrossOriginProtection().Handler(gate))

	return mux
}

// instructions is what the server tells an agent on connecting. Its first
// paragraph stands alone, since Codex may read no more.
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
- Before overriding or adding a definition, read_guide rules says which copy the game keeps; read_guide workspace ` +
	`says whether the user is new to modding.

Call read_guide for PMT's pages and their controls, how to make a change with PMT, and the open workspace's mods and ` +
	`folders.`

func (s *Service) newServer() *sdk.Server {
	server := sdk.NewServer(
		&sdk.Implementation{Name: ServerName, Title: "Paradox Modding Tools", Version: s.Build},
		&sdk.ServerOptions{Instructions: instructions},
	)
	s.addEditorTools(server)
	s.addPageTools(server)
	s.addAppTools(server)

	for _, task := range Tasks {
		server.AddPrompt(&sdk.Prompt{Name: task.Name, Title: task.Title},
			func(context.Context, *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
				return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{
					{Role: "user", Content: &sdk.TextContent{Text: task.Text}},
				}}, nil
			})
	}

	return server
}

func (s *Service) readToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.token
}

// ServiceShutdown stops the server.
func (s *Service) ServiceShutdown() error {
	if s.server == nil {
		return nil
	}

	return s.server.Close()
}

// ResolveAccess returns the address and token an agent needs.
func (s *Service) ResolveAccess() Access {
	return Access{URL: s.readURL(), Token: s.readToken()}
}

// ListTasks returns the starter tasks.
func (s *Service) ListTasks() []Task { return Tasks }

// ListTools returns PMT's tools with their titles. It returns an error if the
// tools cannot be listed.
func (s *Service) ListTools() ([]Tool, error) {
	ctx := context.Background()
	serverEnd, clientEnd := sdk.NewInMemoryTransports()

	serverSession, err := s.newServer().Connect(ctx, serverEnd, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = serverSession.Close() }()

	clientSession, err := sdk.NewClient(&sdk.Implementation{Name: ServerName}, nil).Connect(ctx, clientEnd, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = clientSession.Close() }()

	var tools []Tool
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}

		tools = append(tools, Tool{Name: tool.Name, Title: tool.Title,
			ReadOnly: tool.Annotations != nil && tool.Annotations.ReadOnlyHint})
	}

	return tools, nil
}
