// Package agent starts the coding agents PMT chats with. It lists the agents
// PMT offers and the user's own, installs the offered ones from the ACP
// registry with npm on Node, and pipes each running agent to the window that
// chats with it.
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
// {token} to fill in.
var agents = []app.Agent{
	{ID: "claude-acp", Name: "Claude",
		Register: `claude mcp add --transport http pmt {url} --header "Authorization: Bearer {token}"`,
		Files:    []string{"CLAUDE.md", "CLAUDE.local.md", ".claude"}},
	{ID: "github-copilot-cli", Name: "GitHub Copilot",
		Register: `copilot mcp add --transport http pmt {url} --header "Authorization: Bearer {token}"`,
		Files:    []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".github"}},
	{ID: "codex-acp", Name: "Codex",
		Register: "[mcp_servers.pmt]\nurl = \"{url}\"\nhttp_headers = { Authorization = \"Bearer {token}\" }",
		Files:    []string{"AGENTS.md", "AGENTS.override.md", ".codex"}},
}

const nodeURL = "https://nodejs.org/dist/"

const windowsOS = "windows"

// minNode is the floor Claude's adapter sets.
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

// ListAgents returns the agents PMT offers, then the user's own. An installed
// agent that a Node can run gets the Program and Args that start it.
func (s *Service) ListAgents() []Agent {
	listing, _ := s.loadRegistry()
	node := s.DescribeNode()
	access := s.MCP.ResolveAccess()
	replacer := strings.NewReplacer("{url}", access.URL, "{token}", access.Token)

	offers := make([]Agent, 0, len(agents))
	for _, row := range agents {
		row.Register = replacer.Replace(row.Register)

		var entry registryAgent
		if index := slices.IndexFunc(listing.Agents, func(candidate registryAgent) bool { return candidate.ID == row.ID }); index >= 0 {
			entry = listing.Agents[index]
		}

		versions := s.listInstalled(row.ID)
		slices.SortFunc(versions, func(first, second string) int { return semver.Compare("v"+first, "v"+second) })

		agent := Agent{Agent: row, Version: entry.Version, Description: entry.Description}
		if len(versions) > 0 {
			agent.Installed = versions[len(versions)-1]
		}

		started, _ := s.installs.Load(row.ID)
		agent.Installing, _ = started.(time.Time)

		script, err := findScript(filepath.Join(s.resolveDir(), row.ID+"@"+agent.Installed))
		canStart := agent.Installed != "" && err == nil && node.Path != ""
		if canStart {
			agent.Program, agent.Args = node.Path, []string{script}
			if npx := entry.Distribution.Npx; npx != nil {
				agent.Args = append(agent.Args, npx.Args...)
			}
		}

		offers = append(offers, agent)
	}

	own := make([]Agent, 0, len(s.Workspace.Config.Config().Agents))
	for _, record := range s.Workspace.Config.Config().Agents {
		own = append(own, Agent{Agent: record})
	}

	return slices.Concat(offers, own)
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
	node := Node{}

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

	index := slices.IndexFunc(releases, func(candidate release) bool { return candidate.LTS != false })
	if index < 0 {
		return errors.New("nodejs.org lists no long-term release")
	}
	newest := releases[index]

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
// versions. It returns an error if the registry lacks the agent, no Node is
// new enough, an install of it already runs, or npm fails.
func (s *Service) InstallAgent(id string) error {
	listing, err := s.loadRegistry()
	if err != nil {
		return err
	}

	index := slices.IndexFunc(listing.Agents, func(candidate registryAgent) bool { return candidate.ID == id })
	if index < 0 || listing.Agents[index].Distribution.Npx == nil {
		return fmt.Errorf("the registry has no agent %s that runs on Node", id)
	}
	entry := listing.Agents[index]

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

	command := exec.Command(node.Path, filepath.Join(modules, "npm", "bin", "npm-cli.js"), "install", "--prefix", dir,
		"--no-audit", "--no-fund", entry.Distribution.Npx.Package)
	// Npm's install scripts run on the same Node.
	command.Env = addToPath(os.Environ(), filepath.Dir(node.Path))
	prepareCommand(command)

	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("npm could not install %s\n%w\n%s", entry.Distribution.Npx.Package, err, output)
	}

	for _, version := range s.listInstalled(id) {
		if version != entry.Version {
			_ = os.RemoveAll(filepath.Join(s.resolveDir(), id+"@"+version))
		}
	}

	return nil
}

// UninstallAgent removes every installed version of an offered agent. It
// returns an error if npm is installing it or a folder cannot be removed, as
// while the agent runs.
func (s *Service) UninstallAgent(id string) error {
	if !slices.ContainsFunc(agents, func(row app.Agent) bool { return row.ID == id }) {
		return fmt.Errorf("PMT offers no agent %s", id)
	}

	if _, isRunning := s.installs.Load(id); isRunning {
		return fmt.Errorf("npm is installing %s", id)
	}
	defer application.Get().Event.Emit(EventChanged, id)

	// A version npm left half installed goes too.
	dirs, _ := filepath.Glob(filepath.Join(s.resolveDir(), id+"@*"))
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}

	return nil
}

// findAgent returns the agent with the id if it can start, else an error
// saying why not.
func (s *Service) findAgent(id string) (Agent, error) {
	offered := s.ListAgents()

	index := slices.IndexFunc(offered, func(other Agent) bool { return other.ID == id })
	if index < 0 {
		return Agent{}, fmt.Errorf("PMT has no agent %s", id)
	}
	agent := offered[index]

	switch {
	case agent.Program != "":
		return agent, nil
	case agent.Installed == "":
		return Agent{}, fmt.Errorf("%s is not installed", agent.Name)
	}

	return Agent{}, fmt.Errorf("%s needs Node %s or newer", agent.Name, minNode)
}

const registryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"

const registryAge = 24 * time.Hour

// loadRegistry returns the saved registry while it is under a day old, else a
// fresh one. It keeps the old copy if the fetch fails, and errors if there is
// none.
func (s *Service) loadRegistry() (registry, error) {
	path := filepath.Join(s.resolveDir(), "registry.json")
	content, err := os.ReadFile(path)
	if info, statErr := os.Stat(path); statErr != nil || time.Since(info.ModTime()) > registryAge {
		if fetched, fetchErr := app.FetchURL(registryURL, fetchTimeout); fetchErr == nil {
			content, err = fetched, files.WriteAtomic(path, fetched)
		}
	}

	var listing registry
	if len(content) == 0 {
		return listing, errors.Join(err, errors.New("the ACP registry could not be fetched"))
	}

	return listing, json.Unmarshal(content, &listing)
}

func (s *Service) listInstalled(id string) []string {
	matches, _ := filepath.Glob(filepath.Join(s.resolveDir(), id+"@*"))

	versions := []string{}
	for _, match := range matches {
		if _, err := findScript(match); err != nil {
			continue
		}

		_, version, _ := strings.Cut(filepath.Base(match), "@")
		versions = append(versions, version)
	}

	return versions
}

func (s *Service) resolveDir() string {
	return filepath.Join(s.Workspace.Config.Dirs.Cache, "agents")
}

// findScript returns the script of the agent installed in a folder: the one
// program its one package declares. It returns an error if there is none.
func findScript(dir string) (string, error) {
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
