// Package agent starts the coding agents PMT chats with, and the one that
// completes code in the editor. It lists the agents PMT offers and the user's
// own, installs the offered ones with npm on Node, and pipes each running
// agent to the window that talks to it.
//
// Go holds none of the Agent Client Protocol. The window speaks it, so this
// package only starts the program, carries its lines both ways and ends it.
//
// The rules it applies:
//
//   - An agent PMT offers is a row of the agents table: its registry id, the
//     text that registers PMT's tools with it outside PMT, and the
//     instruction files and folders it reads in a mod.
//   - Node is the system's if it is version 22 or newer, else the copy PMT
//     downloaded into its cache folder, else none. Npm and every agent run on
//     that Node.
//   - The registry is fetched at most once a day and the last copy is used
//     while the fetch fails.
//   - An agent is installed at the registry's version in a folder of its own,
//     and its other versions are removed. Two installs of one agent never run
//     at once, since two npm runs in one folder delete each other's files.
//   - A row that names its own npm package is installed at npm's latest
//     version, without the package's optional parts, which are builds and
//     runtimes PMT does not run. npm's answer is saved and fetched like the
//     registry.
//   - A row that answers tab completion starts only for completion, with its
//     own environment, never for a chat. Its Chat names the row that chats
//     for it, and Settings draws the two as one agent.
//   - Uninstalling an agent ends its completion programs in every window
//     first, so that its folder can go, and refuses while a chat with it runs.
//   - An agent runs in a group of its own, a job object on Windows and a
//     process group on Linux, so ending the stream ends everything it
//     started.
//
// # Robustness
//
// InstallNode refuses an archive whose sum is not the one nodejs.org
// publishes. Unpacking a Node archive skips everything but regular files, and
// any file that would land outside the folder. An installed agent whose
// package does not declare exactly one program cannot start. With no registry
// copy and no network, the offered agents have no version and cannot be
// installed. A stream whose first message is not an opening is closed with no
// answer. An agent's blank output lines are dropped, and its error output is
// read only up to one megabyte a line.
package agent

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/mcp"
	"paradox-modding-tools/services/workspace"
)

// EventChanged is sent when an agent's install starts and ends. The data is
// the agent's id.
const EventChanged = "agent:changed"

func init() { application.RegisterEvent[string](EventChanged) }

// Service lists, installs and starts the coding agents.
type Service struct {
	Workspace *workspace.Service
	MCP       *mcp.Service

	// installs maps an agent's id to the time its install started.
	installs sync.Map

	// programs holds the running programs of every window, as keys.
	programs sync.Map
}

// Agent is one agent as Settings lists it, with what the registry and PMT's
// folder say of it.
type Agent struct {
	app.Agent

	// Version is "" for the user's own agents.
	Version string `json:"version"`

	// Installed is "" if no version is installed.
	Installed string `json:"installed"`

	Description string `json:"description"`

	// Installing is the zero time if no install runs.
	Installing time.Time `json:"installing"`

	// Env holds the environment variables the program starts with beyond
	// PMT's own and Node on the PATH. It is empty for a chat.
	Env []string `json:"-"`
}

// Node is the Node that PMT's agents run on.
type Node struct {
	// Path is "" if no Node is new enough.
	Path string `json:"path"`

	// Own is true when Path is the copy PMT downloaded.
	Own bool `json:"own"`

	// System is "" if the system has no Node that runs.
	System string `json:"system"`
}

type registry struct {
	Agents []registryAgent `json:"agents"`
}

type registryAgent struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Distribution struct {
		Npx *npxPackage `json:"npx"`
	} `json:"distribution"`
}

type npxPackage struct {
	Package string   `json:"package"`
	Args    []string `json:"args"`
}

type release struct {
	Version string `json:"version"`

	// LTS is false, or the name of the long-term line.
	LTS any `json:"lts"`
}

// agents is the table of agents PMT offers. A Register text has {url} and
// {token} to fill in. GitHub Copilot is two rows: its CLI chats, and its
// language server answers tab completion, with the CLI kept out of it.
var agents = []app.Agent{
	{ID: "claude-acp", Name: "Claude",
		Register: `claude mcp add --transport http pmt {url} --header "Authorization: Bearer {token}"`,
		Files:    []string{"CLAUDE.md", "CLAUDE.local.md", ".claude"}},
	{ID: "github-copilot-cli", Name: "GitHub Copilot",
		Register: `copilot mcp add --transport http pmt {url} --header "Authorization: Bearer {token}"`,
		Files:    []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".github"}},
	{ID: "copilot-language-server", Name: "GitHub Copilot", Chat: "github-copilot-cli", Completion: true,
		Package: "@github/copilot-language-server", Script: "dist/language-server.js", Args: []string{"--acp"},
		CompletionEnv: "GITHUB_COPILOT_ACP_USE_CLI=0"},
	{ID: "codex-acp", Name: "Codex",
		Register: "[mcp_servers.pmt]\nurl = \"{url}\"\nhttp_headers = { Authorization = \"Bearer {token}\" }",
		Files:    []string{"AGENTS.md", "AGENTS.override.md", ".codex"}},
}

const nodeURL = "https://nodejs.org/dist/"

const windowsOS = "windows"

// minNode is the oldest Node that Claude's adapter runs on.
const minNode = "22"

const fetchTimeout = 5 * time.Minute

// ListFiles returns the instruction files and folders the offered agents read
// in a mod, each once.
func ListFiles() []string {
	entries := []string{}

	for _, agent := range agents {
		for _, entry := range agent.Files {
			if !slices.Contains(entries, entry) {
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

// ListAgents returns the agents PMT offers, then the user's own.
func (s *Service) ListAgents() []Agent {
	listing, _ := s.loadRegistry()
	node := s.DescribeNode()
	access := s.MCP.ResolveAccess()
	replacer := strings.NewReplacer("{url}", access.URL, "{token}", access.Token)
	records := s.Workspace.Config.Config().Agents

	listed := make([]Agent, 0, len(agents)+len(records))
	for _, row := range agents {
		row.Register = replacer.Replace(row.Register)
		entry, _ := s.loadEntry(row, listing)
		listed = append(listed, s.makeOffer(row, entry, node))
	}

	for _, record := range records {
		listed = append(listed, Agent{Agent: record})
	}

	return listed
}

// makeOffer returns an offered agent with its registry entry and installed
// version. An installed agent that the Node can run gets the program and
// arguments that start it for a chat, unless it answers tab completion only.
func (s *Service) makeOffer(row app.Agent, entry registryAgent, node Node) Agent {
	agent := Agent{Agent: row, Version: entry.Version, Description: entry.Description}
	var script string
	agent.Installed, script = s.findInstalled(row)

	started, _ := s.installs.Load(row.ID)
	agent.Installing, _ = started.(time.Time)

	if script != "" && node.Path != "" && !row.Completion {
		agent.Program, agent.Args = node.Path, []string{script}
		if npx := entry.Distribution.Npx; npx != nil {
			agent.Args = append(agent.Args, npx.Args...)
		}
	}

	return agent
}

// AddAgent adds an agent of the user's own and returns it with its new id. It
// returns an error if the name or the program is empty.
func (s *Service) AddAgent(agent app.Agent) (app.Agent, error) {
	if agent.Name == "" || agent.Program == "" {
		return app.Agent{}, errors.New("an agent needs a name and a program")
	}

	agent.ID, agent.Register, agent.Files = rand.Text(), "", nil

	return agent, s.Workspace.Config.Update("agents", func(config *app.Config) error {
		config.Agents = append(config.Agents, agent)

		return nil
	})
}

// RemoveAgent removes an agent of the user's own. An id that names none
// changes nothing.
func (s *Service) RemoveAgent(id string) error {
	return s.Workspace.Config.Update("agents", func(config *app.Config) error {
		config.Agents = slices.DeleteFunc(config.Agents, func(agent app.Agent) bool { return agent.ID == id })

		return nil
	})
}

// DescribeNode returns the Node the agents run on, and the system's own
// version, which is "" if its Node cannot run.
func (s *Service) DescribeNode() Node {
	var node Node
	if path, err := exec.LookPath("node"); err == nil {
		command := exec.Command(path, "--version")
		prepareCommand(command)

		if output, err := command.Output(); err == nil {
			node.System = strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
		}

		if node.System != "" && semver.Compare("v"+node.System, "v"+minNode) >= 0 {
			node.Path = path

			return node
		}
	}

	dir := filepath.Join(s.resolveDir(), "node")
	own := filepath.Join(dir, "bin", "node")
	if runtime.GOOS == windowsOS {
		own = filepath.Join(dir, "node.exe")
	}

	if files.IsFile(own) {
		node.Path, node.Own = own, true
	}

	return node
}

// InstallNode downloads the newest long-term Node into PMT's folder, after
// checking its sum against nodejs.org's.
func (s *Service) InstallNode() error {
	listing, err := app.FetchURL(nodeURL+"index.json", fetchTimeout)
	if err != nil {
		return err
	}

	var releases []release
	if err := json.Unmarshal(listing, &releases); err != nil {
		return err
	}

	position := slices.IndexFunc(releases, func(candidate release) bool { return candidate.LTS != false })
	if position < 0 {
		return errors.New("nodejs.org lists no long-term release")
	}

	newest := releases[position]

	system, extension, arch := runtime.GOOS, ".tar.gz", runtime.GOARCH
	if system == windowsOS {
		system, extension = "win", ".zip"
	}
	if arch == "amd64" {
		arch = "x64"
	}

	asset := fmt.Sprintf("node-%s-%s-%s%s", newest.Version, system, arch, extension)
	archive, err := app.FetchURL(nodeURL+newest.Version+"/"+asset, fetchTimeout)
	if err != nil {
		return err
	}

	if err := checkSum(newest.Version, asset, archive); err != nil {
		return err
	}

	dir := filepath.Join(s.resolveDir(), "node")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	if system == "win" {
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return err
		}

		return files.Unzip(reader, dir)
	}

	return untar(archive, dir)
}

// InstallAgent installs an offered agent with npm and removes its other
// versions. A row that names its own package is installed without the
// package's optional parts.
func (s *Service) InstallAgent(id string) error {
	offer := slices.IndexFunc(agents, func(row app.Agent) bool { return row.ID == id })
	if offer < 0 {
		return fmt.Errorf("PMT offers no agent %s", id)
	}

	row := agents[offer]
	listing, listErr := s.loadRegistry()
	entry, err := s.loadEntry(row, listing)
	if err != nil {
		return errors.Join(listErr, err)
	}

	node := s.DescribeNode()
	if node.Path == "" {
		return fmt.Errorf("the agents need Node %s or newer", minNode)
	}

	// Two npm runs in one folder delete each other's files.
	if _, isRunning := s.installs.LoadOrStore(id, time.Now()); isRunning {
		return fmt.Errorf("npm is installing %s already", entry.Distribution.Npx.Package)
	}
	application.Get().Event.Emit(EventChanged, id)
	defer func() {
		s.installs.Delete(id)
		application.Get().Event.Emit(EventChanged, id)
	}()

	dir := filepath.Join(s.resolveDir(), id+"@"+entry.Version)
	modules := filepath.Join(filepath.Dir(node.Path), "node_modules")
	if runtime.GOOS != windowsOS {
		modules = filepath.Join(filepath.Dir(node.Path), "..", "lib", "node_modules")
	}

	npm := []string{filepath.Join(modules, "npm", "bin", "npm-cli.js"), "install", "--prefix", dir, "--no-audit", "--no-fund"}
	if row.Package != "" {
		npm = append(npm, "--omit=optional")
	}

	command := exec.Command(node.Path, append(npm, entry.Distribution.Npx.Package)...)
	// Npm's install scripts run on the same Node.
	command.Env = addToPath(os.Environ(), filepath.Dir(node.Path))
	prepareCommand(command)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("npm could not install %s\n%w\n%s", entry.Distribution.Npx.Package, err, output)
	}

	// A version that cannot be removed, as while it runs, waits for the next
	// install.
	_ = s.removeVersions(id, dir)

	return nil
}

// UninstallAgent removes every installed version of an offered agent. It first
// ends the agent's completion programs in every window, and waits for them,
// since a running program's folder cannot be removed. It fails while a chat
// with the agent runs.
func (s *Service) UninstallAgent(id string) error {
	if !slices.ContainsFunc(agents, func(row app.Agent) bool { return row.ID == id }) {
		return fmt.Errorf("PMT offers no agent %s", id)
	}

	if _, isRunning := s.installs.Load(id); isRunning {
		return fmt.Errorf("npm is installing %s", id)
	}

	if chats := s.listPrograms(id, false); len(chats) > 0 {
		return fmt.Errorf("PMT cannot uninstall %s until you close the sessions with it", chats[0].agent.Name)
	}

	for _, running := range s.listPrograms(id, true) {
		running.end()
	}
	defer application.Get().Event.Emit(EventChanged, id)

	return s.removeVersions(id, "")
}

// removeVersions deletes the installed versions of an agent but the one in the
// kept folder, "" to keep none. A version npm left half installed goes too.
func (s *Service) removeVersions(id, kept string) error {
	dirs, _ := filepath.Glob(filepath.Join(s.resolveDir(), id+"@*"))
	var failure error
	for _, dir := range dirs {
		if dir != kept {
			failure = errors.Join(failure, os.RemoveAll(dir))
		}
	}

	return failure
}

// findAgent returns the agent with the id if it can start on the given Node,
// else an error saying why not. For an offered agent it reads the registry's
// saved copy, however old, and fetches nothing.
func (s *Service) findAgent(id string, node Node) (Agent, error) {
	var agent Agent
	own := s.Workspace.Config.Config().Agents
	offer := slices.IndexFunc(agents, func(row app.Agent) bool { return row.ID == id })
	record := slices.IndexFunc(own, func(other app.Agent) bool { return other.ID == id })
	switch {
	case offer >= 0:
		saved, _ := app.LoadJSON[registry](s.resolveRegistry())
		entry, _ := s.loadEntry(agents[offer], saved)
		agent = s.makeOffer(agents[offer], entry, node)
	case record >= 0:
		agent = Agent{Agent: own[record]}
	default:
		return Agent{}, fmt.Errorf("PMT has no agent %s", id)
	}

	switch {
	case agent.Program != "":
		return agent, nil
	case agent.Installed == "":
		return Agent{}, fmt.Errorf("%s is not installed", agent.Name)
	}

	return Agent{}, fmt.Errorf("%s needs Node %s or newer", agent.Name, minNode)
}

// findCompletion returns the agent with the id as it starts for tab
// completion on the given Node, with its own environment. It needs only the
// agent itself installed. It returns an error if the agent answers no tab
// completion, is not installed, or the Node is "".
func (s *Service) findCompletion(id string, node Node) (Agent, error) {
	offer := slices.IndexFunc(agents, func(row app.Agent) bool { return row.ID == id && row.Completion })
	if offer < 0 {
		return Agent{}, fmt.Errorf("PMT has no agent %s that completes code", id)
	}

	row := agents[offer]
	agent := Agent{Agent: row, Env: []string{row.CompletionEnv}}
	var script string
	agent.Installed, script = s.findInstalled(row)
	switch {
	case agent.Installed == "":
		return Agent{}, fmt.Errorf("%s is not installed", row.Name)
	case node.Path == "":
		return Agent{}, fmt.Errorf("%s needs Node %s or newer", row.Name, minNode)
	}

	agent.Program, agent.Args = node.Path, append([]string{script}, row.Args...)

	return agent, nil
}

// The registry and npm's answer for a package's latest version. npmURL is
// followed by the package's name and "/latest".
const (
	registryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"
	npmURL      = "https://registry.npmjs.org/"
)

const registryAge = 24 * time.Hour

// loadRegistry returns the ACP registry as loadCopy gives it.
func (s *Service) loadRegistry() (registry, error) {
	var listing registry
	content, err := loadCopy(s.resolveRegistry(), registryURL, "the ACP registry")
	if err != nil {
		return listing, err
	}

	return listing, json.Unmarshal(content, &listing)
}

// loadEntry returns what the registry says of an offered agent. For a row that
// names its own package it is npm's latest version of the package and its
// description, as loadCopy gives them from a copy beside the registry's, with
// the package pinned to that version. It returns an error if the registry has
// no such agent that runs on Node, or npm's answer cannot be had.
func (s *Service) loadEntry(row app.Agent, listing registry) (registryAgent, error) {
	if row.Package == "" {
		position := slices.IndexFunc(listing.Agents, func(candidate registryAgent) bool { return candidate.ID == row.ID })
		if position < 0 || listing.Agents[position].Distribution.Npx == nil {
			return registryAgent{}, fmt.Errorf("the registry has no agent %s that runs on Node", row.ID)
		}

		return listing.Agents[position], nil
	}

	content, err := loadCopy(filepath.Join(s.resolveDir(), row.ID+".json"), npmURL+row.Package+"/latest", "npm")
	if err != nil {
		return registryAgent{}, err
	}

	var entry registryAgent
	if err := json.Unmarshal(content, &entry); err != nil {
		return registryAgent{}, err
	}

	entry.Distribution.Npx = &npxPackage{Package: row.Package + "@" + entry.Version}

	return entry, nil
}

// loadCopy returns a URL's content from its copy in a file while the copy is
// under a day old, else from a fresh fetch, which replaces the copy. It keeps
// the old copy if the fetch fails, and errors, naming the source, if there is
// none.
func loadCopy(path, url, source string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if info, statErr := os.Stat(path); statErr != nil || time.Since(info.ModTime()) > registryAge {
		if fetched, fetchErr := app.FetchURL(url, fetchTimeout); fetchErr == nil {
			content, err = fetched, files.WriteAtomic(path, fetched)
		}
	}

	if len(content) == 0 {
		return nil, errors.Join(err, fmt.Errorf("%s could not be fetched", source))
	}

	return content, nil
}

// resolveRegistry returns where the registry's saved copy is kept.
func (s *Service) resolveRegistry() string {
	return filepath.Join(s.resolveDir(), "registry.json")
}

// findInstalled returns the newest installed version of an offered agent and
// the script that starts it. Both are "" if none is installed. A folder with
// no script is no install.
func (s *Service) findInstalled(row app.Agent) (version, script string) {
	matches, _ := filepath.Glob(filepath.Join(s.resolveDir(), row.ID+"@*"))
	for _, match := range matches {
		program, err := findScript(match, row)
		if err != nil {
			continue
		}

		_, candidate, _ := strings.Cut(filepath.Base(match), "@")
		if version == "" || semver.Compare("v"+candidate, "v"+version) > 0 {
			version, script = candidate, program
		}
	}

	return version, script
}

func (s *Service) resolveDir() string {
	return filepath.Join(s.Workspace.Config.Dirs.Cache, "agents")
}

// findScript returns the script of the agent installed in a folder: the
// script its row names in its own package, else the one program its one
// package declares. It returns an error if there is none.
func findScript(dir string, row app.Agent) (string, error) {
	if row.Script != "" {
		script := filepath.Join(dir, "node_modules", filepath.FromSlash(row.Package), filepath.FromSlash(row.Script))
		if !files.IsFile(script) {
			return "", fmt.Errorf("%s holds no %s", dir, row.Script)
		}

		return script, nil
	}

	installed, err := app.LoadJSON[struct {
		Dependencies map[string]string `json:"dependencies"`
	}](filepath.Join(dir, "package.json"))
	if err != nil || len(installed.Dependencies) != 1 {
		return "", errors.Join(err, fmt.Errorf("%s holds no one agent package", dir))
	}

	packageDir := filepath.Join(dir, "node_modules", slices.Collect(maps.Keys(installed.Dependencies))[0])
	declared, err := app.LoadJSON[struct {
		Bin json.RawMessage `json:"bin"`
	}](filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", err
	}

	// A package declares its program as a path, or as a map of one name to a
	// path.
	var script string
	var scripts map[string]string
	if json.Unmarshal(declared.Bin, &script) != nil && json.Unmarshal(declared.Bin, &scripts) == nil && len(scripts) == 1 {
		script = slices.Collect(maps.Values(scripts))[0]
	}
	if script == "" {
		return "", fmt.Errorf("the package in %s declares no one program", packageDir)
	}

	return filepath.Join(packageDir, filepath.FromSlash(script)), nil
}

// checkSum checks a Node archive against the sum nodejs.org publishes for it.
func checkSum(version, asset string, archive []byte) error {
	sums, err := app.FetchURL(nodeURL+version+"/SHASUMS256.txt", fetchTimeout)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(archive)
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) == 2 && fields[1] == asset {
			if fields[0] != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("%s does not match the sum nodejs.org publishes for it", asset)
			}

			return nil
		}
	}

	return fmt.Errorf("nodejs.org publishes no sum for %s", asset)
}

// untar unpacks a gzipped tar into a folder without its top folder. It skips
// anything but regular files, and files that would land outside the folder.
func untar(archive []byte, dir string) error {
	unpacked, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}

	reader := tar.NewReader(unpacked)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		// The top folder's own entry leaves an empty path, which is not local.
		_, rest, _ := strings.Cut(filepath.ToSlash(header.Name), "/")
		rel := filepath.FromSlash(rest)
		if header.Typeflag != tar.TypeReg || !filepath.IsLocal(rel) {
			continue
		}

		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, header.FileInfo().Mode().Perm()|0o600)
		if err != nil {
			return err
		}

		_, err = io.Copy(file, reader)
		if err = errors.Join(err, file.Close()); err != nil {
			return err
		}
	}
}
