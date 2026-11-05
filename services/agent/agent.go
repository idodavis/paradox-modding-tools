// Package agent starts the coding agents PMT chats with. It lists the agents, installs them from the ACP registry
// with npm on Node, and pipes each running agent to the window that chats with it.
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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/mcp"
	"paradox-modding-tools/services/workspace"
)

// EventChanged is sent when an agent's install starts and when it ends. The data is the agent's id.
const EventChanged = "agent:changed"

// init registers the agent event with the application.
func init() { application.RegisterEvent[string](EventChanged) }

// Service is the Wails service that lists, installs and starts the coding agents.
type Service struct {
	Workspace *workspace.Service // the open workspace, for an agent's folder
	MCP       *mcp.Service       // PMT's tools, whose address and token the register texts carry
	installs  sync.Map           // when each running install started, by the agent's id
}

// An Agent is one agent as Settings and the agent pane list it: its record, with what the registry and PMT's folder
// say of it.
type Agent struct {
	app.Agent
	Version     string    `json:"version"`     // the registry's version of one PMT offers; "" for the user's
	Installed   string    `json:"installed"`   // the version installed in PMT's folder; "" if none is
	Description string    `json:"description"` // the registry's words for it
	Installing  time.Time `json:"installing"`  // when its running install started; zero if none runs
}

// A Node is the Node that PMT's agents run on: the system's if it is new enough, else PMT's own copy.
type Node struct {
	Path   string `json:"path"`   // the program; "" if no Node is new enough
	Own    bool   `json:"own"`    // it is the copy PMT downloaded into its folder
	System string `json:"system"` // the system's own version, such as 20.11.1; "" if the system has none
}

// A registry is the ACP registry's list of agents, as far as PMT reads it.
type registry struct {
	Agents []registryAgent `json:"agents"`
}

// A registryAgent is one agent of the registry.
type registryAgent struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Distribution struct {
		Npx *npxPackage `json:"npx"`
	} `json:"distribution"`
}

// An npxPackage is how the registry runs an agent on Node: its npm package at its version, and its arguments.
type npxPackage struct {
	Package string   `json:"package"`
	Args    []string `json:"args"`
}

// A release is one Node release as nodejs.org's index lists it.
type release struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false, or the name of its long-term line
}

// agents are the agents PMT offers. A row has the registry id, the Register text that adds PMT's tools to the agent
// outside PMT ({url} and {token} are filled in), and the instruction files and folders the agent reads in a mod.
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

// The places PMT reads the agents and Node from.
const (
	registryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"
	nodeURL     = "https://nodejs.org/dist/"
)

// windowsOS is runtime.GOOS on Windows, where Node's archive and program differ from Linux's.
const windowsOS = "windows"

// minNode is the oldest Node version the agents run on, the floor Claude's adapter sets.
const minNode = "22"

// registryAge is how long the registry's copy in PMT's folder is used before it is fetched again.
const registryAge = 24 * time.Hour

// fetchTimeout is how long a download from nodejs.org or the ACP registry may take.
const fetchTimeout = 5 * time.Minute

// ListFiles returns the instruction files and folders that the agents PMT offers read in a mod, each name once.
func ListFiles() []string {
	return lo.Uniq(lo.FlatMap(agents, func(agent app.Agent, _ int) []string { return agent.Files }))
}

// ListAgents returns the agents PMT offers, then the user's own. An offered agent gets the registry's version and
// description, its Register text with PMT's address and token filled in, and, once installed with a Node new enough,
// the Program and Args that start it. With no registry copy, the agents have no version and cannot be installed.
func (agt *Service) ListAgents() []Agent {
	listing, _ := agt.loadRegistry()
	node := agt.DescribeNode()
	access := agt.MCP.ResolveAccess()
	replacer := strings.NewReplacer("{url}", access.URL, "{token}", access.Token)
	offers := lo.Map(agents, func(row app.Agent, _ int) Agent {
		row.Register = replacer.Replace(row.Register)
		entry, _ := lo.Find(listing.Agents, func(entry registryAgent) bool { return entry.ID == row.ID })
		versions := agt.listInstalled(row.ID)
		slices.SortFunc(versions, func(first, second string) int { return semver.Compare("v"+first, "v"+second) })
		agent := Agent{Agent: row, Version: entry.Version, Installed: lo.LastOr(versions, ""), Description: entry.Description}
		started, _ := agt.installs.Load(row.ID)
		agent.Installing, _ = started.(time.Time)
		script, err := findScript(filepath.Join(agt.resolveDir(), row.ID+"@"+agent.Installed))
		canStart := agent.Installed != "" && err == nil && node.Path != ""
		if canStart {
			agent.Program, agent.Args = node.Path, append([]string{script}, lo.FromPtr(entry.Distribution.Npx).Args...)
		}
		return agent
	})
	own := lo.Map(agt.Workspace.Config.Config().Agents, func(record app.Agent, _ int) Agent { return Agent{Agent: record} })
	return slices.Concat(offers, own)
}

// AddAgent adds an agent of the user's own, a name with a program and its arguments, and returns it with its new id.
// It returns an error if the name or the program is empty, or the config cannot be saved.
func (agt *Service) AddAgent(agent app.Agent) (app.Agent, error) {
	if agent.Name == "" || agent.Program == "" {
		return app.Agent{}, errors.New("an agent needs a name and a program")
	}
	agent.ID, agent.Register, agent.Files = rand.Text(), "", nil
	return agent, agt.Workspace.Config.Update("agents", func(config *app.Config) error {
		config.Agents = append(config.Agents, agent)
		return nil
	})
}

// RemoveAgent removes an agent of the user's own. An id that names none changes nothing. It returns an error if the
// config cannot be saved.
func (agt *Service) RemoveAgent(id string) error {
	return agt.Workspace.Config.Update("agents", func(config *app.Config) error {
		config.Agents = slices.DeleteFunc(config.Agents, func(agent app.Agent) bool { return agent.ID == id })
		return nil
	})
}

// DescribeNode returns the Node PMT's agents run on, and the system's own version. The system's version is "" if its
// Node cannot be run.
func (agt *Service) DescribeNode() Node {
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
	dir := filepath.Join(agt.resolveDir(), "node")
	own := lo.Ternary(runtime.GOOS == windowsOS, filepath.Join(dir, "node.exe"), filepath.Join(dir, "bin", "node"))
	if files.IsFile(own) {
		node.Path, node.Own = own, true
	}
	return node
}

// InstallNode downloads the newest long-term release of Node from nodejs.org into PMT's folder. It returns an error if
// a download fails, the sum nodejs.org publishes does not match, or the archive cannot be unpacked.
func (agt *Service) InstallNode() error {
	listing, err := app.FetchURL(nodeURL+"index.json", fetchTimeout)
	if err != nil {
		return err
	}
	var releases []release
	if err := json.Unmarshal(listing, &releases); err != nil {
		return err
	}
	newest, ok := lo.Find(releases, func(candidate release) bool { return candidate.LTS != false })
	if !ok {
		return errors.New("nodejs.org lists no long-term release")
	}
	system := lo.Ternary(runtime.GOOS == windowsOS, "win", runtime.GOOS)
	arch := lo.Ternary(runtime.GOARCH == "amd64", "x64", runtime.GOARCH)
	asset := fmt.Sprintf("node-%s-%s-%s%s", newest.Version, system, arch, lo.Ternary(system == "win", ".zip", ".tar.gz"))
	archive, err := app.FetchURL(nodeURL+newest.Version+"/"+asset, fetchTimeout)
	if err != nil {
		return err
	}
	if err := checkSum(newest.Version, asset, archive); err != nil {
		return err
	}
	dir := filepath.Join(agt.resolveDir(), "node")
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

// InstallAgent installs an agent PMT offers into PMT's folder at the registry's version, with npm on the Node that
// DescribeNode gives, and removes its other versions. It sends EventChanged when npm starts and when it ends. It
// returns an error if the registry lacks the agent, no Node is new enough, npm is installing it already, or npm
// fails.
func (agt *Service) InstallAgent(id string) error {
	listing, err := agt.loadRegistry()
	if err != nil {
		return err
	}
	entry, ok := lo.Find(listing.Agents, func(entry registryAgent) bool { return entry.ID == id })
	if !ok || entry.Distribution.Npx == nil {
		return fmt.Errorf("the registry has no agent %s that runs on Node", id)
	}
	node := agt.DescribeNode()
	if node.Path == "" {
		return fmt.Errorf("the agents need Node %s or newer", minNode)
	}
	// two npm runs in one folder delete each other's files
	if _, isRunning := agt.installs.LoadOrStore(id, time.Now()); isRunning {
		return fmt.Errorf("npm is installing %s already", entry.Distribution.Npx.Package)
	}
	application.Get().Event.Emit(EventChanged, id)
	defer func() {
		agt.installs.Delete(id)
		application.Get().Event.Emit(EventChanged, id)
	}()
	dir := filepath.Join(agt.resolveDir(), id+"@"+entry.Version)
	modules := filepath.Join(filepath.Dir(node.Path), "node_modules")
	if runtime.GOOS != windowsOS {
		modules = filepath.Join(filepath.Dir(node.Path), "..", "lib", "node_modules")
	}
	command := exec.Command(node.Path, filepath.Join(modules, "npm", "bin", "npm-cli.js"), "install", "--prefix", dir,
		"--no-audit", "--no-fund", entry.Distribution.Npx.Package)
	// npm's install scripts run on the same Node
	command.Env = addToPath(os.Environ(), filepath.Dir(node.Path))
	prepareCommand(command)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("npm could not install %s\n%w\n%s", entry.Distribution.Npx.Package, err, output)
	}
	for _, version := range agt.listInstalled(id) {
		if version != entry.Version {
			_ = os.RemoveAll(filepath.Join(agt.resolveDir(), id+"@"+version))
		}
	}
	return nil
}

// UninstallAgent removes an agent PMT offers from PMT's folder, every version of it, and sends EventChanged. It returns
// an error if PMT offers no such agent, npm is installing it, or a folder cannot be removed, as while the agent runs.
func (agt *Service) UninstallAgent(id string) error {
	if !lo.ContainsBy(agents, func(row app.Agent) bool { return row.ID == id }) {
		return fmt.Errorf("PMT offers no agent %s", id)
	}
	if _, isRunning := agt.installs.Load(id); isRunning {
		return fmt.Errorf("npm is installing %s", id)
	}
	defer application.Get().Event.Emit(EventChanged, id)
	// a version npm left half installed goes too
	dirs, _ := filepath.Glob(filepath.Join(agt.resolveDir(), id+"@*"))
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}

// findAgent returns the agent with the given id, if it can start. It returns an error if there is no such agent, it is
// not installed, or no Node is new enough.
func (agt *Service) findAgent(id string) (Agent, error) {
	agent, ok := lo.Find(agt.ListAgents(), func(other Agent) bool { return other.ID == id })
	switch {
	case !ok:
		return Agent{}, fmt.Errorf("PMT has no agent %s", id)
	case agent.Program != "":
		return agent, nil
	case agent.Installed == "":
		return Agent{}, fmt.Errorf("%s is not installed", agent.Name)
	}
	return Agent{}, fmt.Errorf("%s needs Node %s or newer", agent.Name, minNode)
}

// loadRegistry returns the registry: the copy in PMT's folder while it is less than a day old, else a new one, which
// replaces the copy. The old copy is used if the fetch fails. It returns an error if there is neither.
func (agt *Service) loadRegistry() (registry, error) {
	path := filepath.Join(agt.resolveDir(), "registry.json")
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

// listInstalled returns each version of an agent that PMT's folder holds whole.
func (agt *Service) listInstalled(id string) []string {
	matches, _ := filepath.Glob(filepath.Join(agt.resolveDir(), id+"@*"))
	return lo.FilterMap(matches, func(match string, _ int) (string, bool) {
		_, version, _ := strings.Cut(filepath.Base(match), "@")
		_, err := findScript(match)
		return version, err == nil
	})
}

// resolveDir returns the folder that holds the agents, Node and the registry's copy, under PMT's cache folder.
func (agt *Service) resolveDir() string {
	return filepath.Join(agt.Workspace.Config.Dirs.Cache, "agents")
}

// findScript returns the script of the agent installed in a folder: the one program its one package declares. It
// returns an error if the folder holds no such package.
func findScript(dir string) (string, error) {
	installed, err := app.LoadJSON[struct {
		Dependencies map[string]string `json:"dependencies"`
	}](filepath.Join(dir, "package.json"))
	if err != nil || len(installed.Dependencies) != 1 {
		return "", errors.Join(err, fmt.Errorf("%s holds no one agent package", dir))
	}
	packageDir := filepath.Join(dir, "node_modules", lo.Keys(installed.Dependencies)[0])
	declared, err := app.LoadJSON[struct {
		Bin json.RawMessage `json:"bin"`
	}](filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", err
	}
	// a package declares its program as a path, or as a map of one name to a path
	var script string
	var scripts map[string]string
	if json.Unmarshal(declared.Bin, &script) != nil && json.Unmarshal(declared.Bin, &scripts) == nil && len(scripts) == 1 {
		script = lo.Values(scripts)[0]
	}
	if script == "" {
		return "", fmt.Errorf("the package in %s declares no one program", packageDir)
	}
	return filepath.Join(packageDir, filepath.FromSlash(script)), nil
}

// checkSum checks the archive of a Node release's asset against the SHA-256 sum nodejs.org publishes for it. It returns
// an error if the sums cannot be fetched, have no sum for the asset, or list a different one.
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

// untar unpacks a gzipped tar archive into a folder, dropping the archive's one top folder from each path. It skips
// everything but regular files, and a file that would land outside the folder.
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
		// the top folder's own entry leaves an empty path, which is not local either
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
