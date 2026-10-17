// workspaceservice.go manages the game installs, the workspaces and their mods, the IDE's roots, and the opening of
// a workspace: the config's records turned into a request for the one open workspace, what the frontend hears of
// it, and the state a page shows. The events about the open workspace and the installs are declared here.

package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/modfiles"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/steam"
	"paradox-modding-tools/services/internal/wiki"
	"paradox-modding-tools/services/internal/workspace"
)

// WorkspaceService manages workspaces, game installs and mods, and opens a workspace.
type WorkspaceService struct {
	config  *ConfigFile
	version string
	wikis   *wiki.Client    // the wiki's script examples are a scan's last source
	slot    *workspace.Slot // the one open workspace
}

// NewWorkspaceService is the service over the config and the wiki client, scanning with index.Scan.
func NewWorkspaceService(config *ConfigFile, version string, wikis *wiki.Client) *WorkspaceService {
	slot := &workspace.Slot{Scan: index.Scan, Closed: func(id string) { emit(EventWorkspaceClosed, id) }}
	return &WorkspaceService{config: config, version: version, wikis: wikis, slot: slot}
}

// WorkspaceState is what the pages show of a workspace: whether it is open, its install, and whose dumps the
// schema read.
type WorkspaceState struct {
	InstallID   string `json:"installId"`
	InstallOk   bool   `json:"installOk"`
	GameVersion string `json:"gameVersion"`
	ScannedAt   string `json:"scannedAt,omitempty"`   // when the install was built, else its cache file written
	UpdatedFrom string `json:"updatedFrom,omitempty"` // the version this run's scan replaced, where it differs
	Open        bool   `json:"open"`

	DumpsFrom   string   `json:"dumpsFrom"`   // whose dumps the schema read: yours, shipped or none
	DumpsStale  bool     `json:"dumpsStale"`  // they predate the install, or are another game version's
	DumpFolders []string `json:"dumpFolders"` // where the game's script_docs writes them
}

// The events the frontend listens for about the open workspace and the installs.
const (
	EventWorkspaceOpened = "workspace:opened" // a workspace is open, its index built: data is its id
	EventWorkspaceFailed = "workspace:failed" // a workspace could not be opened: data is its id
	EventWorkspaceClosed = "workspace:closed" // the open workspace closed: data is its id
	EventInstallScanning = "install:scanning" // an install's scan moved: data is ScanProgress
	EventInstallScanned  = "install:scanned"  // an install was scanned or loaded from its cache: data is its id
	EventFSChanged       = "fs:changed"       // a file under a mod changed on disk: data is FSChanged
)

// registers every event so the bindings generator types them
func init() {
	application.RegisterEvent[string](EventWorkspaceOpened)
	application.RegisterEvent[string](EventWorkspaceFailed)
	application.RegisterEvent[string](EventWorkspaceClosed)
	application.RegisterEvent[ScanProgress](EventInstallScanning)
	application.RegisterEvent[string](EventInstallScanned)
	application.RegisterEvent[FSChanged](EventFSChanged)
}

// ScanProgress is one step of an install scan.
type ScanProgress struct {
	InstallID string `json:"installId"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
}

// FSChanged is one path under a mod that changed on disk; Deleted when it is gone.
type FSChanged struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// Game installs.

// ListGames returns the supported-game table and vanilla origin token.
func (wsp *WorkspaceService) ListGames() game.GameList {
	return game.GameList{OriginVanilla: game.OriginVanilla, Games: game.All()}
}

// ListGameInstalls returns a game's installs, each with its scan time.
func (wsp *WorkspaceService) ListGameInstalls(gameID string) ([]GameInstall, error) {
	var out []GameInstall
	languages := map[string]string{} // by install id: the language of the first workspace on it
	wsp.config.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			if inst.GameID == gameID {
				out = append(out, inst)
			}
		}
		for _, ws := range cfg.Workspaces {
			if _, known := languages[ws.InstallID]; !known {
				languages[ws.InstallID] = ws.DefaultLocLang
			}
		}
	})

	// a cache file's time and the version per install, so the config lock is back before any of it
	for i := range out {
		out[i].ScannedAt = wsp.scannedAt(&out[i], languages[out[i].ID])
		out[i].VersionDetected = game.LoadGameVersion(out[i].GameID, out[i].Path)
	}
	return out, nil
}

// FindGameInstalls returns Steam-detected installs for a game.
func (wsp *WorkspaceService) FindGameInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// AddGameInstall adds a new game installation, with the version it holds now.
func (wsp *WorkspaceService) AddGameInstall(gameID, name, path string) (*GameInstall, error) {
	if game.Get(gameID) == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	inst := GameInstall{ID: uuid.New().String(), GameID: gameID, Name: name, Path: path}
	err := wsp.config.mutate("installs", func(cfg *Config) error {
		cfg.Installs = append(cfg.Installs, inst)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert install: %w", err)
	}
	inst.VersionDetected = game.LoadGameVersion(gameID, path)
	return &inst, nil
}

// UpdateGameInstall changes an install's path.
func (wsp *WorkspaceService) UpdateGameInstall(id, path string) error {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	}
	err := wsp.config.mutate("installs", func(cfg *Config) error {
		inst := findInstall(cfg, id)
		if inst == nil {
			return errors.New("install not found")
		}
		inst.Path = cmp.Or(path, inst.Path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("update install: %w", err)
	}
	return nil
}

// DeleteGameInstall removes an install if no workspace uses it.
func (wsp *WorkspaceService) DeleteGameInstall(id string) error {
	var inUse []string
	var install GameInstall
	wsp.config.read(func(cfg *Config) {
		for _, ws := range cfg.Workspaces {
			if ws.InstallID == id {
				inUse = append(inUse, ws.Name)
			}
		}
		if row := findInstall(cfg, id); row != nil {
			install = *row
		}
	})
	if len(inUse) > 0 {
		return fmt.Errorf("install in use by: %s", strings.Join(inUse, ", "))
	}
	if install.ID != "" {
		if err := index.DropInstall(index.Request{Game: install.GameID, Dir: install.Path}); err != nil {
			return err
		}
	}
	return wsp.config.mutate("installs", func(cfg *Config) error {
		cfg.Installs = slices.DeleteFunc(cfg.Installs, func(inst GameInstall) bool { return inst.ID == id })
		return nil
	})
}

// Workspaces.

// ListWorkspaces returns every workspace, its mods in load order.
func (wsp *WorkspaceService) ListWorkspaces() []Workspace {
	var out []Workspace
	wsp.config.read(func(cfg *Config) {
		for _, ws := range cfg.Workspaces {
			out = append(out, cloneWorkspace(ws))
		}
	})
	for i := range out {
		markDisk(&out[i])
	}
	return out
}

// markDisk re-derives each mod's IsBroken and Folder from disk, a stat per mod, outside the config
// lock; stored, they would go stale the moment a folder moves.
func markDisk(ws *Workspace) {
	userData := game.UserDataDir(ws.GameID, "")
	for i := range ws.Mods {
		mod := &ws.Mods[i]
		_, err := os.Stat(mod.Path)
		mod.IsBroken = err != nil
		mod.Folder = mod.Path
		if rel, err := filepath.Rel(userData, mod.Path); userData != "" && err == nil && filepath.IsLocal(rel) {
			mod.Folder = rel
		}
	}
}

// CreateWorkspace creates a new workspace.
func (wsp *WorkspaceService) CreateWorkspace(
	gameID, name, installID string,
) (*Workspace, error) {
	if game.Get(gameID) == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	id := uuid.New().String()
	ws := Workspace{
		ID: id, GameID: gameID, Name: name, InstallID: installID,
		HideExplorerBinaries: true,
	}
	err := wsp.config.mutate("workspaces", func(cfg *Config) error {
		cfg.Workspaces = append(cfg.Workspaces, ws)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return &ws, nil
}

// UpdateWorkspace updates workspace fields; the open workspace follows a different install by itself (the next
// request finds its fingerprint stale).
func (wsp *WorkspaceService) UpdateWorkspace(id, name, installID string) error {
	return wsp.mutateWorkspace(id, func(ws *Workspace) error {
		ws.Name, ws.InstallID = name, installID
		return nil
	})
}

var legalDefaultTools = map[string]bool{
	"":              true,
	"workspace-ide": true,
	"event-graph":   true,
	"health":        true,
	"patch-notes":   true,
	"release":       true,
}

// UpdateWorkspacePrefs sets the IDE reset, default landing page, explorer hiding, and game color.
func (wsp *WorkspaceService) UpdateWorkspacePrefs(
	workspaceID string, resetIdeOnOpen bool, defaultTool, gameColor string,
	hideExplorerBinaries bool,
) error {
	if !legalDefaultTools[defaultTool] {
		return errors.New("invalid default tool")
	}
	return wsp.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.ResetIdeOnOpen = resetIdeOnOpen
		ws.DefaultTool = defaultTool
		ws.GameColor = gameColor
		ws.HideExplorerBinaries = hideExplorerBinaries
		return nil
	})
}

// SetWorkspaceLocLang sets the default loc language; the open workspace is built again with it.
func (wsp *WorkspaceService) SetWorkspaceLocLang(id, lang string) error {
	return wsp.mutateWorkspace(id, func(ws *Workspace) error {
		ws.DefaultLocLang = lang
		return nil
	})
}

// DeleteWorkspace removes a workspace from PMT. Mod folders on disk stay.
func (wsp *WorkspaceService) DeleteWorkspace(id string) error {
	err := wsp.config.mutate("workspaces", func(cfg *Config) error {
		count := len(cfg.Workspaces)
		cfg.Workspaces = slices.DeleteFunc(cfg.Workspaces, func(ws Workspace) bool { return ws.ID == id })
		if len(cfg.Workspaces) == count {
			return errNoWorkspace
		}
		return nil
	})
	if err != nil {
		return err
	}
	wsp.slot.Close(id)
	return nil
}

// mutateWorkspace changes one workspace of the config under its lock and saves; errNoWorkspace where none has the
// id.
func (wsp *WorkspaceService) mutateWorkspace(workspaceID string, fn func(*Workspace) error) error {
	return wsp.config.mutate("workspaces", func(cfg *Config) error {
		ws := findWorkspace(cfg, workspaceID)
		if ws == nil {
			return errNoWorkspace
		}
		return fn(ws)
	})
}

// Mods in a workspace.

// DefaultModParent is UserDataDir/<game>/mod, or "".
func (wsp *WorkspaceService) DefaultModParent(gameID string) string {
	return modfiles.DefaultModParent(gameID)
}

// ModFolder is the folder CreateMod makes for a mod named name under parentDir.
func (wsp *WorkspaceService) ModFolder(parentDir, name string) string {
	return filepath.Join(parentDir, modfiles.Slug(name))
}

// CreateMod writes a new mod skeleton under parentDir and returns its path.
func (wsp *WorkspaceService) CreateMod(
	gameID, parentDir, name, locLang, supportedVersion, description, thumbnailSrc string,
) (string, error) {
	// Create refuses an unknown game or an empty name
	name = strings.TrimSpace(name)
	if parentDir == "" {
		parentDir = modfiles.DefaultModParent(gameID)
	}
	if parentDir == "" {
		return "", errors.New("parent folder is required")
	}
	root := wsp.ModFolder(parentDir, name)
	if err := modfiles.Create(modfiles.NewModOpts{
		GameID: gameID, Root: root, Name: name, SupportedVersion: supportedVersion,
		LocLang: locLang, ThumbnailSrc: thumbnailSrc,
	}); err != nil {
		return "", err
	}
	return root, steam.NewDescription(root, steam.DescMdName, steam.FromMarkdown, name, description)
}

// AddWorkspaceMod adds a mod to a workspace: a folder holding its game's descriptor, named by it (else
// by the folder), with the thumbnail where its game keeps one, last in load order.
func (wsp *WorkspaceService) AddWorkspaceMod(workspaceID, path string) (*WorkspaceMod, error) {
	gameID, err := wsp.modFolderOf(workspaceID, path)
	if err != nil {
		return nil, err
	}
	fields, err := modfiles.LoadDescriptor(gameID, path)
	if err != nil {
		return nil, err
	}
	mod := WorkspaceMod{ID: uuid.New().String(), Name: cmp.Or(fields.Name, filepath.Base(path)), Path: path}
	if rel, known := modfiles.Thumbnail(gameID, path, fields.Picture); known {
		mod.Thumbnail = filepath.Join(path, filepath.FromSlash(rel))
	}
	err = wsp.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		// after the last one, whatever gaps removals left
		for _, other := range ws.Mods {
			mod.SortOrder = max(mod.SortOrder, other.SortOrder+1)
		}
		mod.Slot = freeSlot(ws.Mods)
		ws.Mods = append(ws.Mods, mod)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert mod: %w", err)
	}
	return &mod, nil
}

// modFolderOf is the game of a workspace whose mod may live at path: a folder holding the game's
// descriptor.
func (wsp *WorkspaceService) modFolderOf(workspaceID, path string) (string, error) {
	ws, _, err := wsp.config.workspace(workspaceID)
	if err != nil {
		return "", err
	}
	gameID := ws.GameID
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", path)
	}
	if _, err := os.Stat(game.DescriptorPath(gameID, path)); err != nil {
		return "", fmt.Errorf("%s has no %s, so it is not a mod", path, game.DescriptorFile(gameID))
	}
	return gameID, nil
}

// SetWorkspaceModPath points a mod at another folder, one holding its game's descriptor; the
// open workspace is built again from it.
func (wsp *WorkspaceService) SetWorkspaceModPath(workspaceID, modID, path string) error {
	if _, err := wsp.modFolderOf(workspaceID, path); err != nil {
		return err
	}
	return wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Path = path
		return nil
	})
}

// OpenFolder shows a folder in the system's file manager.
func (wsp *WorkspaceService) OpenFolder(path string) error {
	return application.Get().Env.OpenFileManager(path, false)
}

// Opening a workspace.

// Open is the workspace opened, its index built once however many callers ask at once, and its state; opening a
// workspace closes the one open before it.
func (wsp *WorkspaceService) Open(workspaceID string) (*WorkspaceState, error) {
	if _, err := wsp.openWorkspace(workspaceID); err != nil {
		return nil, err
	}
	return wsp.State(workspaceID), nil
}

// openWorkspace is the workspace's index, opened when it is not: what Open and every page RPC call. The caller
// whose call built it tells the frontend and logs its name, so the log says whether a page read reaches Go before
// Open.
func (wsp *WorkspaceService) openWorkspace(workspaceID string) (*workspace.Session, error) {
	request, err := wsp.request(workspaceID)
	if err != nil {
		return nil, err
	}
	caller, _, _, _ := runtime.Caller(1)
	ws, built, err := wsp.slot.Open(context.Background(), request)
	switch {
	case built && err != nil:
		emit(EventWorkspaceFailed, workspaceID)
	case built:
		slog.Info("workspace opened", "workspace", workspaceID, "by", runtime.FuncForPC(caller).Name())
		emit(EventWorkspaceOpened, workspaceID)
	}
	return ws, err
}

// request is what a workspace is opened from, as the config says now. Its fingerprint is the inputs an edit could
// change: the install and its folder, the language, each mod's id, folder and calendar in load order; a change needs
// no close of its own, the next request finds the open workspace stale. Its callbacks tell the frontend.
func (wsp *WorkspaceService) request(workspaceID string) (workspace.Request, error) {
	entry, inst, err := wsp.config.workspace(workspaceID)
	if err != nil {
		return workspace.Request{}, err
	}
	if inst == nil {
		return workspace.Request{}, fmt.Errorf("workspace %s has no install", workspaceID)
	}
	fingerprint := fmt.Sprintf("%s|%s|%s", inst.ID, inst.Path, entry.DefaultLocLang)
	var mods []workspace.Mod
	for _, mod := range entry.Mods {
		fingerprint += fmt.Sprintf("|%s=%s %v", mod.ID, mod.Path, mod.Eras)
		mods = append(mods, workspace.Mod{ID: mod.ID, Dir: mod.Path, Calendar: mod.Eras})
	}
	return workspace.Request{
		ID: workspaceID, Game: inst.GameID, Dir: inst.Path, Language: entry.DefaultLocLang, Mods: mods,
		App: wsp.version, Fingerprint: fingerprint,
		Wiki: func(databases []string) (map[string][]string, time.Time, error) {
			return wsp.wikis.ScriptExamples(inst.GameID, databases)
		},
		Progress: func(percent int, stage string) {
			emit(EventInstallScanning, ScanProgress{InstallID: inst.ID, Percent: percent, Stage: stage})
		},
		Scanned: func() { emit(EventInstallScanned, inst.ID) },
		Changed: func(path string) {
			_, err := os.Stat(path)
			emit(EventFSChanged, FSChanged{Path: path, Deleted: err != nil})
		},
	}, nil
}

// scannedAt is when an install was built, in RFC 3339, read in a language; "" when it never was.
func (wsp *WorkspaceService) scannedAt(inst *GameInstall, language string) string {
	scanTime := wsp.slot.ScannedAt(workspace.Request{Game: inst.GameID, Dir: inst.Path, Language: language, App: wsp.version})
	if scanTime.IsZero() {
		return ""
	}
	return scanTime.Format(time.RFC3339)
}

// State is what the pages show of a workspace, without opening it: its install and when it was scanned, and when
// it is open, whose dumps its schema read and the version the scan replaced.
func (wsp *WorkspaceService) State(workspaceID string) *WorkspaceState {
	state := &WorkspaceState{}
	entry, inst, _ := wsp.config.workspace(workspaceID)
	if entry != nil {
		state.InstallID = entry.InstallID
	}
	request, err := wsp.request(workspaceID)
	if inst == nil || err != nil {
		return state
	}
	state.GameVersion = game.LoadGameVersion(inst.GameID, inst.Path)
	_, statErr := os.Stat(inst.Path)
	state.InstallOk = statErr == nil
	state.ScannedAt = wsp.scannedAt(inst, entry.DefaultLocLang)
	state.DumpFolders = game.DumpDirs(inst.GameID, inst.Path)
	if ws := wsp.slot.Current(workspaceID, request.Fingerprint); ws != nil {
		ws.RLock()
		dumps := ws.Install.Schema.Sources[schema.SourceDumps]
		state.Open, state.DumpsFrom, state.DumpsStale = true, dumps.Origin, dumps.Stale
		// the version the scan read, and the one it replaced where the game was updated since
		state.GameVersion = ws.Install.Schema.Version
		if previous := ws.Install.Previous; previous != "" && previous != appdata.SafeName(state.GameVersion) {
			state.UpdatedFrom = previous
		}
		ws.RUnlock()
	}
	return state
}

// UpdateWorkspaceMod updates a mod's name, color, thumbnail and upload exclusions.
func (wsp *WorkspaceService) UpdateWorkspaceMod(workspaceID, modID, name, color, thumbnail, workshopIgnore string) error {
	return wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Name = cmp.Or(name, mod.Name)
		mod.Color, mod.Thumbnail, mod.WorkshopIgnore = color, thumbnail, workshopIgnore
		return nil
	})
}

// SetModListingImage makes src the mod's listing image: copied in where its game keeps one, named in
// its descriptor (its Workshop id as it is) and made the workspace's picture of the mod. It returns the
// image's path.
func (wsp *WorkspaceService) SetModListingImage(workspaceID, modID, src string) (string, error) {
	ws, mod, err := findMod(wsp.config, workspaceID, modID)
	if err != nil {
		return "", err
	}
	rel, err := modfiles.CopyThumbnail(ws.GameID, mod.Path, src)
	if err != nil {
		return "", err
	}
	fields, err := modfiles.LoadDescriptor(ws.GameID, mod.Path)
	if err != nil {
		return "", err
	}
	fields.Picture = picture(rel)
	if err := modfiles.WriteDescriptor(ws.GameID, mod.Path, fields); err != nil {
		return "", err
	}
	image := filepath.Join(mod.Path, filepath.FromSlash(rel))
	return image, wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Thumbnail = image
		return nil
	})
}

// SetWorkspaceModDescription sets where a mod's Workshop description comes from: a Markdown or a
// BBCode file at rel ("" for the default name), or Steam's own text.
func (wsp *WorkspaceService) SetWorkspaceModDescription(workspaceID, modID, source, rel string) error {
	return wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		rel, err := steam.DescriptionRel(mod.Path, rel)
		if err != nil {
			return err
		}
		switch source {
		case steam.FromMarkdown:
			mod.DescMdRel = rel
		case steam.FromBBCode:
			mod.DescBbRel = rel
		case steam.FromSteam:
		default:
			return fmt.Errorf("no description source %q", source)
		}
		mod.DescSource = source
		return nil
	})
}

// SetWorkspaceModSlot gives a mod one of the color slots and clears its hand-picked color.
func (wsp *WorkspaceService) SetWorkspaceModSlot(workspaceID, modID string, slot int) error {
	if slot < 1 || slot > modSlots {
		return fmt.Errorf("slot %d is not between 1 and %d", slot, modSlots)
	}
	return wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Slot, mod.Color = slot, ""
		return nil
	})
}

// modSlots is how many color slots a workspace's mods share: eight hues at two lightnesses.
const modSlots = 16

// freeSlot is the lowest slot no mod in mods holds; with every slot held, slot 1 again.
func freeSlot(mods []WorkspaceMod) int {
	used := make([]bool, modSlots+1)
	for _, mod := range mods {
		if mod.Slot >= 1 && mod.Slot <= modSlots {
			used[mod.Slot] = true
		}
	}
	if free := slices.Index(used[1:], false); free >= 0 {
		return free + 1
	}
	return 1
}

// SetWorkspaceModCalendar declares the eras a mod's dates are written in: names trimmed, unnamed
// eras dropped; an empty list clears it, leaving every date as written.
func (wsp *WorkspaceService) SetWorkspaceModCalendar(workspaceID, modID string, eras workspace.Calendar) error {
	err := wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Eras = nil
		for _, era := range eras {
			if era.Name = strings.TrimSpace(era.Name); era.Name != "" {
				mod.Eras = append(mod.Eras, era)
			}
		}
		return nil
	})
	return err
}

// ReorderWorkspaceMods writes SortOrder from the given id list.
func (wsp *WorkspaceService) ReorderWorkspaceMods(workspaceID string, modIDs []string) error {
	return wsp.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		if len(modIDs) != len(ws.Mods) {
			return errors.New("mod id set incomplete")
		}
		byID := make(map[string]*WorkspaceMod, len(ws.Mods))
		for i := range ws.Mods {
			byID[ws.Mods[i].ID] = &ws.Mods[i]
		}
		for i, id := range modIDs {
			mod, ok := byID[id]
			if !ok {
				return errNoMod
			}
			mod.SortOrder = i
			delete(byID, id)
		}
		if len(byID) != 0 {
			return errors.New("mod id set incomplete")
		}
		return nil
	})
}

// RemoveWorkspaceMod detaches a mod from a workspace.
func (wsp *WorkspaceService) RemoveWorkspaceMod(workspaceID, modID string) error {
	return wsp.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		count := len(ws.Mods)
		ws.Mods = slices.DeleteFunc(ws.Mods, func(mod WorkspaceMod) bool { return mod.ID == modID })
		if len(ws.Mods) == count {
			return errNoMod
		}
		return nil
	})
}

// ExportWorkshopIgnore writes the saved ignore text to .workshop-ignore in the mod root.
func (wsp *WorkspaceService) ExportWorkshopIgnore(workspaceID, modID string) error {
	_, mod, err := findMod(wsp.config, workspaceID, modID)
	if err != nil {
		return err
	}
	return appdata.WriteBytes(filepath.Join(mod.Path, steam.IgnoreFile), []byte(mod.WorkshopIgnore))
}

// ImportWorkshopIgnore reads .workshop-ignore into workspace config and returns the text.
func (wsp *WorkspaceService) ImportWorkshopIgnore(workspaceID, modID string) (string, error) {
	var text string
	err := wsp.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		raw, err := os.ReadFile(filepath.Join(mod.Path, steam.IgnoreFile))
		if err != nil {
			return err
		}
		text, mod.WorkshopIgnore = string(raw), string(raw)
		return nil
	})
	return text, err
}

// mutateMod changes one mod of a workspace the same way; errNoMod where the workspace has none of the id.
func (wsp *WorkspaceService) mutateMod(workspaceID, modID string, fn func(*WorkspaceMod) error) error {
	return wsp.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		mod, err := ws.mod(modID)
		if err != nil {
			return err
		}
		return fn(mod)
	})
}

// What the IDE opens with.

// IdeRoot is one folder in the workspace IDE multi-root set.
type IdeRoot struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	ReadOnly  bool   `json:"readOnly"`
	Kind      string `json:"kind"`
	Origin    string `json:"origin,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// IdeRoots is the IDE boot DTO: its folders, and whether it starts with no tabs (the tabs themselves are kept on
// this computer, in the frontend).
type IdeRoots struct {
	Roots                []IdeRoot `json:"roots"`
	ResetIdeOnOpen       bool      `json:"resetIdeOnOpen"`
	HideExplorerBinaries bool      `json:"hideExplorerBinaries"`
}

// GetIdeRoots returns the game and mod folders for the IDE.
func (wsp *WorkspaceService) GetIdeRoots(workspaceID string) (*IdeRoots, error) {
	// one read, so the workspace and the install it names cannot disagree
	ws, inst, err := wsp.config.workspace(workspaceID)
	if err != nil {
		return nil, err
	}
	markDisk(ws)

	out := &IdeRoots{ResetIdeOnOpen: ws.ResetIdeOnOpen, HideExplorerBinaries: ws.HideExplorerBinaries}
	for _, mod := range ws.Mods {
		if !mod.IsBroken {
			out.Roots = append(out.Roots, IdeRoot{
				Label: mod.Name, Path: mod.Path, Kind: "mod", Origin: mod.ID, Thumbnail: mod.Thumbnail,
			})
		}
	}
	if info := game.Get(ws.GameID); inst != nil && info != nil {
		out.Roots = append(out.Roots, IdeRoot{
			Label: info.Name, Path: filepath.Join(inst.Path, info.ScriptRoot),
			ReadOnly: true, Kind: "game", Origin: game.OriginVanilla,
		})
	}
	return out, nil
}
