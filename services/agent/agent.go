// Package agent starts the coding agents PMT chats with. It lists the agents, installs them from the ACP registry
// with npm on Node, and pipes each running agent to the window that chats with it.
package agent

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/mcp"
	"paradox-modding-tools/services/workspace"
)

// Service is the agent service. It holds a running agent's program only while a window's stream to it is open.
type Service struct {
	Workspace *workspace.Service // the open workspace, for an agent's folder
	MCP       *mcp.Service       // PMT's tools, whose address and token the register texts carry
}

// An Agent is one agent as the Agents page lists it: its record, with what the registry and PMT's folder say of it.
type Agent struct {
	app.Agent
	Version     string `json:"version"`     // the registry's version of one PMT offers; "" for the user's
	Installed   string `json:"installed"`   // the version installed in PMT's folder; "" if none is
	Description string `json:"description"` // the registry's words for it
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

// agents holds the agents PMT offers: the coding agents whose makers stand behind an ACP agent for them. Each row has
// its registry id, how PMT's tools are registered with it for use outside PMT, with {url} and {token} to fill in, and
// the instruction files and folders it reads in a mod.
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

// windowsOS is Windows as runtime.GOOS names it, where Node's archive and its program are laid out apart from Linux.
const windowsOS = "windows"

// minNode is the oldest Node version the agents run on, the floor Claude's adapter sets.
const minNode = "22"

// registryAge is how long the registry's copy in PMT's folder is used before it is fetched again.
const registryAge = 24 * time.Hour

// ListFiles returns the instruction files and folders that the agents PMT offers read in a mod, each name once.
func ListFiles() []string {
	return lo.Uniq(lo.FlatMap(agents, func(agent app.Agent, _ int) []string { return agent.Files }))
}

// ListAgents returns the agents PMT offers, then the user's own. Each one PMT offers has its registry version and
// words, and its register text with PMT's address and token filled in. One installed, with a Node new enough, has the
// program and arguments that start it: its package's script on Node, with the registry's arguments. The registry is
// fetched at most once a day; without a network its last copy is used, and with no copy the agents have no version
// and cannot be installed.
func (agt *Service) ListAgents() []Agent {
	listing, _ := agt.loadRegistry()
	node := agt.DescribeNode()
	replacer := strings.NewReplacer("{url}", agt.MCP.ReadURL(), "{token}", agt.MCP.ReadToken())
	offers := lo.Map(agents, func(row app.Agent, _ int) Agent {
		row.Register = replacer.Replace(row.Register)
		entry, _ := lo.Find(listing.Agents, func(entry registryAgent) bool { return entry.ID == row.ID })
		agent := Agent{Agent: row, Version: entry.Version, Installed: agt.findInstalled(row.ID), Description: entry.Description}
		script, err := findScript(filepath.Join(agt.findDir(), row.ID+"@"+agent.Installed))
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

// DescribeNode returns the Node PMT's agents run on, and the system's own version.
func (agt *Service) DescribeNode() Node {
	node := Node{}
	if path, err := exec.LookPath("node"); err == nil {
		node.System = readVersion(path)
		if node.System != "" && compareVersions(node.System, minNode) >= 0 {
			node.Path = path
			return node
		}
	}
	if own := agt.findOwnNode(); files.IsFile(own) {
		node.Path, node.Own = own, true
	}
	return node
}

// InstallNode downloads the newest long-term release of Node from nodejs.org into PMT's folder, checked against the
// sums nodejs.org publishes. It returns an error if a download fails, the sum does not match, or the archive cannot
// be unpacked.
func (agt *Service) InstallNode() error {
	index, err := fetch(nodeURL + "index.json")
	if err != nil {
		return err
	}
	var releases []release
	if err := json.Unmarshal(index, &releases); err != nil {
		return err
	}
	newest, ok := lo.Find(releases, func(release release) bool { return release.LTS != false })
	if !ok {
		return errors.New("nodejs.org lists no long-term release")
	}
	system := lo.Ternary(runtime.GOOS == windowsOS, "win", runtime.GOOS)
	arch := lo.Ternary(runtime.GOARCH == "amd64", "x64", runtime.GOARCH)
	asset := fmt.Sprintf("node-%s-%s-%s%s", newest.Version, system, arch, lo.Ternary(system == "win", ".zip", ".tar.gz"))
	archive, err := fetch(nodeURL + newest.Version + "/" + asset)
	if err != nil {
		return err
	}
	if err := checkSum(newest.Version, asset, archive); err != nil {
		return err
	}
	dir := filepath.Join(agt.findDir(), "node")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if system == "win" {
		return unzip(archive, dir)
	}
	return untar(archive, dir)
}

// InstallAgent installs an agent PMT offers into PMT's folder at the registry's version, with npm on the Node that
// DescribeNode gives, and removes its other versions. It returns an error if the registry lacks the agent, no Node is
// new enough, or npm fails, with what npm wrote.
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
	dir := filepath.Join(agt.findDir(), id+"@"+entry.Version)
	command := exec.Command(node.Path, findNpm(node.Path), "install", "--prefix", dir, "--no-audit", "--no-fund",
		entry.Distribution.Npx.Package)
	// npm's install scripts run on the same Node
	command.Env = addToPath(os.Environ(), filepath.Dir(node.Path))
	prepareCommand(command)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("npm could not install %s: %w\n%s", entry.Distribution.Npx.Package, err, output)
	}
	for _, version := range agt.listInstalled(id) {
		if version != entry.Version {
			_ = os.RemoveAll(filepath.Join(agt.findDir(), id+"@"+version))
		}
	}
	return nil
}

// findAgent returns the agent with the given id, which can start. It returns an error if PMT has no such agent, or it
// is not installed, or no Node is new enough to run it.
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
// replaces the copy. It falls back to the copy when the registry cannot be fetched. It returns an error if there is
// neither.
func (agt *Service) loadRegistry() (registry, error) {
	path := filepath.Join(agt.findDir(), "registry.json")
	content, err := os.ReadFile(path)
	if info, statErr := os.Stat(path); statErr != nil || time.Since(info.ModTime()) > registryAge {
		if fetched, fetchErr := fetch(registryURL); fetchErr == nil {
			content, err = fetched, files.WriteAtomic(path, fetched)
		}
	}
	var listing registry
	if len(content) == 0 {
		return listing, errors.Join(err, errors.New("the ACP registry could not be fetched"))
	}
	return listing, json.Unmarshal(content, &listing)
}

// findInstalled returns the version of an agent that PMT's folder holds, the newest if it holds several, or "".
func (agt *Service) findInstalled(id string) string {
	versions := agt.listInstalled(id)
	slices.SortFunc(versions, compareVersions)
	return lo.LastOr(versions, "")
}

// listInstalled returns each version of an agent that PMT's folder holds whole.
func (agt *Service) listInstalled(id string) []string {
	matches, _ := filepath.Glob(filepath.Join(agt.findDir(), id+"@*"))
	return lo.FilterMap(matches, func(match string, _ int) (string, bool) {
		_, version, _ := strings.Cut(filepath.Base(match), "@")
		_, err := findScript(match)
		return version, err == nil
	})
}

// findDir returns the folder that holds the agents, Node and the registry's copy. It is under PMT's cache folder, so
// that resetting PMT's data removes them.
func (agt *Service) findDir() string {
	return filepath.Join(agt.Workspace.Config.Dirs.Cache, "agents")
}

// findOwnNode returns where PMT's own copy of Node keeps its program.
func (agt *Service) findOwnNode() string {
	dir := filepath.Join(agt.findDir(), "node")
	if runtime.GOOS == windowsOS {
		return filepath.Join(dir, "node.exe")
	}
	return filepath.Join(dir, "bin", "node")
}

// findNpm returns npm's script for a Node program, where Node's own archive puts it beside the program.
func findNpm(node string) string {
	dir := filepath.Dir(node)
	if runtime.GOOS != windowsOS {
		dir = filepath.Join(dir, "..", "lib")
	}
	return filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")
}

// findScript returns the script of the agent installed in a folder: the one program its one package declares. It
// returns an error if the folder holds no such package.
func findScript(dir string) (string, error) {
	var installed struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := readJSON(filepath.Join(dir, "package.json"), &installed); err != nil || len(installed.Dependencies) != 1 {
		return "", errors.Join(err, fmt.Errorf("%s holds no one agent package", dir))
	}
	packageDir := filepath.Join(dir, "node_modules", lo.Keys(installed.Dependencies)[0])
	var declared struct {
		Bin json.RawMessage `json:"bin"`
	}
	if err := readJSON(filepath.Join(packageDir, "package.json"), &declared); err != nil {
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

// readJSON reads a JSON file into a value. It returns an error if the file cannot be read or is not that JSON.
func readJSON(path string, value any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, value)
}

// readVersion returns the version a Node program reports, without its v, or "" if it cannot be run.
func readVersion(node string) string {
	command := exec.Command(node, "--version")
	prepareCommand(command)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
}

// compareVersions orders two dotted versions by their numbers, so 22 comes before 22.1.
func compareVersions(a, b string) int {
	parse := func(version string) []int {
		return lo.Map(strings.Split(version, "."), func(part string, _ int) int {
			number, _ := strconv.Atoi(part)
			return number
		})
	}
	return slices.Compare(parse(a), parse(b))
}

// fetch returns the body of a web address. It returns an error if the request fails or does not answer 200.
func fetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, response.Status)
	}
	return io.ReadAll(response.Body)
}

// checkSum checks a Node archive, a release's file of the given version, against the sum nodejs.org publishes for it.
// It returns an error if the sums cannot be fetched, list no sum for the file, or list another.
func checkSum(version, asset string, archive []byte) error {
	sums, err := fetch(nodeURL + version + "/SHASUMS256.txt")
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

// unzip unpacks the files of a zip archive into a folder, without the archive's one top folder.
func unzip(archive []byte, dir string) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		content, err := entry.Open()
		if err != nil {
			return err
		}
		err = errors.Join(writeEntry(dir, entry.Name, entry.Mode(), content), content.Close())
		if err != nil {
			return err
		}
	}
	return nil
}

// untar unpacks the files of a gzipped tar archive into a folder, without the archive's one top folder. Its links are
// left out, since PMT runs Node's program and npm's script by their own paths.
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
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if err := writeEntry(dir, header.Name, header.FileInfo().Mode(), reader); err != nil {
			return err
		}
	}
}

// writeEntry writes one file of an archive, by its path in the archive, into a folder, without the archive's top
// folder. An entry that would land outside the folder is left out.
func writeEntry(dir, entry string, mode os.FileMode, content io.Reader) error {
	_, rest, ok := strings.Cut(filepath.ToSlash(entry), "/")
	target := filepath.Join(dir, filepath.FromSlash(rest))
	rel, err := filepath.Rel(dir, target)
	isTop := !ok || rest == ""
	isOutside := err != nil || strings.HasPrefix(rel, "..")
	if isTop || isOutside {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, content)
	return errors.Join(err, file.Close())
}
