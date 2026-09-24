// workspaceservice.go manages the game installs, the workspaces and their mods, and the IDE roots.

package services

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/steam"
)

// WorkspaceService manages workspaces, game installs, and mods.
type WorkspaceService struct {
	Config  *ConfigService
	Session *SessionService
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// Game installs.

// ListGames returns the supported-game table and vanilla origin token.
func (w *WorkspaceService) ListGames() game.GameList {
	return game.GameList{OriginVanilla: game.OriginVanilla, Games: game.All()}
}

// ListGameInstalls returns a game's installs, each with its cache time and whether it exists.
func (w *WorkspaceService) ListGameInstalls(gameID string) ([]GameInstall, error) {
	var out []GameInstall
	w.Config.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			if inst.GameID == gameID {
				out = append(out, inst)
			}
		}
	})

	// a stat and a cache header read per install, so the config lock is back before any of it
	for i := range out {
		_, err := os.Stat(out[i].Path)
		out[i].IsBroken = err != nil
		out[i].ScannedAt = local.ScannedAt(out[i].ID, out[i].Version)
	}
	return out, nil
}

// FindGameInstalls returns Steam-detected installs for a game.
func (w *WorkspaceService) FindGameInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// DetectGameVersion reads version from launcher-settings.json.
func (w *WorkspaceService) DetectGameVersion(path string) string {
	return game.ReadGameVersion(path)
}

// AddGameInstall adds a new game installation. version empty → detected or latest.
func (w *WorkspaceService) AddGameInstall(gameID, name, path, version string) (*GameInstall, error) {
	if game.Get(gameID) == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	detected := game.ReadGameVersion(path)
	version = cmp.Or(version, detected)
	inst := GameInstall{
		ID: uuid.New().String(), GameID: gameID, Name: name, Path: path,
		Version: version, VersionDetected: detected, CreatedAt: nowUTC(),
	}
	err := w.Config.mutate(func(cfg *Config) error {
		cfg.Installs = append(cfg.Installs, inst)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert install: %w", err)
	}
	return &inst, nil
}

// UpdateGameInstall updates path, then refreshes version.
func (w *WorkspaceService) UpdateGameInstall(id, path string) (*GameInstall, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("invalid path: %w", err)
		}
	}
	var out *GameInstall
	err := w.Config.mutate(func(cfg *Config) error {
		inst := findInstall(cfg, id)
		if inst == nil {
			return fmt.Errorf("install not found")
		}
		if path != "" {
			inst.Path = path
		}
		inst.VersionDetected = game.ReadGameVersion(inst.Path)
		cp := *inst
		out = &cp
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update install: %w", err)
	}
	return out, nil
}

// SetInstallVersion pins the cache-key version (user override; empty → latest).
func (w *WorkspaceService) SetInstallVersion(id, version string) error {
	return w.Config.mutate(func(cfg *Config) error {
		inst := findInstall(cfg, id)
		if inst == nil {
			return fmt.Errorf("install not found")
		}
		inst.Version = version
		return nil
	})
}

// DeleteGameInstall removes an install if no workspace uses it.
func (w *WorkspaceService) DeleteGameInstall(id string) (inUse []string, err error) {
	w.Config.read(func(cfg *Config) {
		inUse = workspacesUsingInstall(cfg, id)
	})
	if len(inUse) > 0 {
		return inUse, fmt.Errorf("install in use by: %s", strings.Join(inUse, ", "))
	}
	if err := local.DropInstall(id); err != nil {
		return nil, err
	}
	return nil, w.Config.mutate(func(cfg *Config) error {
		kept := cfg.Installs[:0]
		for _, inst := range cfg.Installs {
			if inst.ID != id {
				kept = append(kept, inst)
			}
		}
		cfg.Installs = kept
		return nil
	})
}

// Workspaces.

// ListWorkspaces returns workspaces, optionally filtered by game. Pass "" for all.
func (w *WorkspaceService) ListWorkspaces(gameID string) ([]Workspace, error) {
	var out []Workspace
	w.Config.read(func(cfg *Config) {
		for _, ws := range cfg.Workspaces {
			if gameID == "" || ws.GameID == gameID {
				out = append(out, cloneWorkspace(ws))
			}
		}
	})
	for i := range out {
		markBroken(&out[i])
	}
	return out, nil
}

// markBroken re-derives IsBroken from disk, a stat per mod, outside the config lock; stored, it
// would go stale the moment a folder moves.
func markBroken(ws *Workspace) {
	for i := range ws.Mods {
		_, err := os.Stat(ws.Mods[i].Path)
		ws.Mods[i].IsBroken = err != nil
	}
}

// GetWorkspace returns a workspace by ID.
func (w *WorkspaceService) GetWorkspace(id string) (*Workspace, error) {
	if id == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	var ws *Workspace
	w.Config.read(func(cfg *Config) {
		if found := findWorkspace(cfg, id); found != nil {
			cp := cloneWorkspace(*found)
			ws = &cp
		}
	})
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	markBroken(ws)
	return ws, nil
}

// CreateWorkspace creates a new workspace.
func (w *WorkspaceService) CreateWorkspace(
	gameID, name, installID string,
) (*Workspace, error) {
	if game.Get(gameID) == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	id := uuid.New().String()
	ws := Workspace{
		ID: id, GameID: gameID, Name: name, InstallID: installID,
		HideExplorerBinaries: true, CreatedAt: nowUTC(),
	}
	err := w.Config.mutate(func(cfg *Config) error {
		cfg.Workspaces = append(cfg.Workspaces, ws)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return &ws, nil
}

// UpdateWorkspace updates workspace fields. A different install retires the session.
func (w *WorkspaceService) UpdateWorkspace(id, name, installID string) error {
	var moved bool
	err := w.mutateWorkspace(id, func(ws *Workspace) error {
		moved = ws.InstallID != installID
		ws.Name, ws.InstallID = name, installID
		return nil
	})
	if err == nil && moved {
		w.Session.drop(id)
	}
	return err
}

var legalDefaultTools = map[string]bool{
	"":              true,
	"workspace-ide": true,
	"event-graph":   true,
	"health":        true,
	"patcher":       true,
	"release":       true,
}

// UpdateWorkspacePrefs sets the IDE reset, default landing page, explorer hiding, and game color.
func (w *WorkspaceService) UpdateWorkspacePrefs(
	workspaceID string, resetIdeOnOpen bool, defaultTool, gameColor string,
	hideExplorerBinaries bool,
) error {
	if !legalDefaultTools[defaultTool] {
		return fmt.Errorf("invalid default tool")
	}
	return w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.ResetIdeOnOpen = resetIdeOnOpen
		ws.DefaultTool = defaultTool
		ws.GameColor = gameColor
		ws.HideExplorerBinaries = hideExplorerBinaries
		return nil
	})
}

// SetWorkspaceLocLang sets the default loc language; a live session is built again with it.
func (w *WorkspaceService) SetWorkspaceLocLang(id, lang string) error {
	err := w.mutateWorkspace(id, func(ws *Workspace) error {
		ws.DefaultLocLang = lang
		return nil
	})
	if err != nil {
		return err
	}
	// the session holds the language, so it is built again on the next request
	w.Session.drop(id)
	return nil
}

// CountWorkspacesUsingInstall returns names of workspaces on this install.
func (w *WorkspaceService) CountWorkspacesUsingInstall(installID string) []string {
	var names []string
	w.Config.read(func(cfg *Config) {
		names = workspacesUsingInstall(cfg, installID)
	})
	return names
}

func workspacesUsingInstall(cfg *Config, installID string) []string {
	var names []string
	for _, ws := range cfg.Workspaces {
		if ws.InstallID == installID {
			names = append(names, ws.Name)
		}
	}
	return names
}

// DeleteWorkspace removes a workspace from PMT. Mod folders on disk stay.
func (w *WorkspaceService) DeleteWorkspace(id string) error {
	if id == "" {
		return fmt.Errorf("workspace id is required")
	}
	err := w.Config.mutate(func(cfg *Config) error {
		for i, ws := range cfg.Workspaces {
			if ws.ID == id {
				cfg.Workspaces = append(cfg.Workspaces[:i], cfg.Workspaces[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("workspace not found")
	})
	if err != nil {
		return err
	}
	w.Session.drop(id)
	return nil
}

func (w *WorkspaceService) mutateWorkspace(workspaceID string, fn func(*Workspace) error) error {
	return w.Config.mutate(func(cfg *Config) error {
		ws := findWorkspace(cfg, workspaceID)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		return fn(ws)
	})
}

// Mods in a workspace.

// DefaultModParent is UserDataDir/<game>/mod, or "".
func (w *WorkspaceService) DefaultModParent(gameID string) string {
	return local.DefaultModParent(gameID)
}

// CreateMod writes a new mod skeleton under parentDir and returns its path.
func (w *WorkspaceService) CreateMod(
	gameID, parentDir, name, locLang, supportedVersion, description, thumbnailSrc string,
) (string, error) {
	if game.Get(gameID) == nil {
		return "", fmt.Errorf("unknown game %s", gameID)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if parentDir == "" {
		parentDir = local.DefaultModParent(gameID)
	}
	if parentDir == "" {
		return "", fmt.Errorf("parent folder is required")
	}
	root := filepath.Join(parentDir, local.ModSlug(name))
	if err := local.WriteNewMod(local.NewModOpts{
		GameID: gameID, Root: root, Name: name, SupportedVersion: supportedVersion,
		LocLang: locLang, ThumbnailSrc: thumbnailSrc,
	}); err != nil {
		return "", err
	}
	return root, steam.EnsureDescriptions(root, name, description)
}

// AddWorkspaceMod adds a mod to a workspace: a folder holding its game's descriptor.
func (w *WorkspaceService) AddWorkspaceMod(
	workspaceID, name, path, thumbnail string,
) (*WorkspaceMod, error) {
	var gameID string
	w.Config.read(func(cfg *Config) {
		if ws := findWorkspace(cfg, workspaceID); ws != nil {
			gameID = ws.GameID
		}
	})
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", path)
	}
	if _, err := os.Stat(game.DescriptorPath(gameID, path)); err != nil {
		return nil, fmt.Errorf("%s has no %s, so it is not a mod", path, game.DescriptorFile(gameID))
	}

	// an empty thumbnail is found here, as for the Release page: only Go knows each game's folder
	if thumbnail == "" {
		if rel, found := local.FindThumbnail(gameID, path, ""); found {
			thumbnail = filepath.Join(path, filepath.FromSlash(rel))
		}
	}
	mod := WorkspaceMod{
		ID: uuid.New().String(), Name: name, Path: path,
		Thumbnail: thumbnail, CreatedAt: nowUTC(),
	}
	err := w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		mod.SortOrder = len(ws.Mods)
		ws.Mods = append(ws.Mods, mod)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert mod: %w", err)
	}
	w.Session.drop(workspaceID)
	return &mod, nil
}

// UpdateWorkspaceMod updates listing paths, ignore text, name, color, and thumbnail.
func (w *WorkspaceService) UpdateWorkspaceMod(
	workspaceID, modID, name, color, thumbnail, descMdRel, descBbRel, workshopIgnore string,
) error {
	return w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		for i := range ws.Mods {
			if ws.Mods[i].ID != modID {
				continue
			}
			if name != "" {
				ws.Mods[i].Name = name
			}
			ws.Mods[i].Color = color
			ws.Mods[i].Thumbnail = thumbnail
			markdown, err := steam.DescriptionRel(ws.Mods[i].Path, descMdRel)
			if err != nil {
				return err
			}
			bbcode, err := steam.DescriptionRel(ws.Mods[i].Path, descBbRel)
			if err != nil {
				return err
			}
			ws.Mods[i].DescMdRel = markdown
			ws.Mods[i].DescBbRel = bbcode
			ws.Mods[i].WorkshopIgnore = workshopIgnore
			return nil
		}
		return fmt.Errorf("mod not found")
	})
}

// SetWorkspaceModCalendar declares the era a mod's date literals are written in, apart from
// UpdateWorkspaceMod because a calendar is set once and rarely. era "" with offset 0 clears it, and
// a cleared calendar leaves every date exactly as written.
func (w *WorkspaceService) SetWorkspaceModCalendar(
	workspaceID, modID, era string, epochOffset int,
) error {
	err := w.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Era = strings.TrimSpace(era)
		mod.EpochOffset = epochOffset
		return nil
	})
	if err != nil {
		return err
	}
	// the session holds the calendar, so it is built again on the next request
	w.Session.drop(workspaceID)
	// VS Code re-requests a document's inlay hints only when told they changed
	emit(EventLangCalendarChanged, workspaceID)
	return nil
}

// ReorderWorkspaceMods writes SortOrder from the given id list.
func (w *WorkspaceService) ReorderWorkspaceMods(workspaceID string, modIDs []string) error {
	err := w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		if len(modIDs) != len(ws.Mods) {
			return fmt.Errorf("mod id set incomplete")
		}
		byID := make(map[string]*WorkspaceMod, len(ws.Mods))
		for i := range ws.Mods {
			byID[ws.Mods[i].ID] = &ws.Mods[i]
		}
		for i, id := range modIDs {
			mod, ok := byID[id]
			if !ok {
				return fmt.Errorf("mod not found")
			}
			mod.SortOrder = i
			delete(byID, id)
		}
		if len(byID) != 0 {
			return fmt.Errorf("mod id set incomplete")
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.Session.drop(workspaceID)
	return nil
}

// RemoveWorkspaceMod detaches a mod from a workspace.
func (w *WorkspaceService) RemoveWorkspaceMod(workspaceID, modID string) error {
	err := w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		kept := ws.Mods[:0]
		found := false
		for _, mod := range ws.Mods {
			if mod.ID == modID {
				found = true
				continue
			}
			kept = append(kept, mod)
		}
		if !found {
			return fmt.Errorf("mod not found")
		}
		ws.Mods = kept
		return nil
	})
	if err != nil {
		return err
	}
	w.Session.drop(workspaceID)
	return nil
}

// ExportWorkshopIgnore writes the saved ignore text to .workshop-ignore in the mod root.
func (w *WorkspaceService) ExportWorkshopIgnore(workspaceID, modID string) error {
	_, mod, err := findMod(w.Config, workspaceID, modID)
	if err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(mod.Path, steam.IgnoreFile),
		[]byte(mod.WorkshopIgnore),
		0o644,
	)
}

// ImportWorkshopIgnore reads .workshop-ignore into workspace config and returns the text.
func (w *WorkspaceService) ImportWorkshopIgnore(workspaceID, modID string) (string, error) {
	_, mod, err := findMod(w.Config, workspaceID, modID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(mod.Path, steam.IgnoreFile))
	if err != nil {
		return "", err
	}
	text := string(raw)
	err = w.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.WorkshopIgnore = text
		return nil
	})
	if err != nil {
		return "", err
	}
	return text, nil
}

func findMod(config *ConfigService, workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	var ws Workspace
	var mod WorkspaceMod
	var foundWS, foundMod bool
	config.read(func(cfg *Config) {
		word := findWorkspace(cfg, workspaceID)
		if word == nil {
			return
		}
		ws = *word
		foundWS = true
		for i := range word.Mods {
			if word.Mods[i].ID == modID {
				mod = word.Mods[i]
				foundMod = true
				return
			}
		}
	})
	if !foundWS {
		return nil, nil, fmt.Errorf("workspace not found")
	}
	if !foundMod {
		return nil, nil, fmt.Errorf("mod not found")
	}
	return &ws, &mod, nil
}

func (w *WorkspaceService) mutateMod(workspaceID, modID string, fn func(*WorkspaceMod) error) error {
	return w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		for i := range ws.Mods {
			if ws.Mods[i].ID == modID {
				return fn(&ws.Mods[i])
			}
		}
		return fmt.Errorf("mod not found")
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
	Color     string `json:"color,omitempty"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// IdeRoots is the IDE boot DTO: folders plus tab restore.
type IdeRoots struct {
	Roots                []IdeRoot `json:"roots"`
	ResetIdeOnOpen       bool      `json:"resetIdeOnOpen"`
	HideExplorerBinaries bool      `json:"hideExplorerBinaries"`
	IdeOpenFiles         []string  `json:"ideOpenFiles,omitempty"`
	IdeActiveFile        string    `json:"ideActiveFile,omitempty"`
}

// GetIdeRoots returns game / mod folders and tab restore for the IDE.
func (w *WorkspaceService) GetIdeRoots(workspaceID string) (*IdeRoots, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}

	// one read, so the workspace and the install it names cannot disagree
	var ws *Workspace
	var scriptRoot string
	w.Config.read(func(cfg *Config) {
		found := findWorkspace(cfg, workspaceID)
		if found == nil {
			return
		}
		cp := cloneWorkspace(*found)
		ws = &cp
		scriptRoot, _ = installScriptRoot(cfg, cp.InstallID)
	})
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	markBroken(ws)

	out := &IdeRoots{
		ResetIdeOnOpen:       ws.ResetIdeOnOpen,
		HideExplorerBinaries: ws.HideExplorerBinaries,
		IdeOpenFiles:         append([]string(nil), ws.IdeOpenFiles...),
		IdeActiveFile:        ws.IdeActiveFile,
	}
	for _, mod := range ws.Mods {
		if mod.IsBroken || mod.Path == "" {
			continue
		}
		label := mod.Name
		label = cmp.Or(label, "Mod")
		out.Roots = append(out.Roots, IdeRoot{
			Label: label, Path: mod.Path, Kind: "mod",
			Origin: mod.ID, Color: mod.Color, Thumbnail: mod.Thumbnail,
		})
	}
	if scriptRoot != "" {
		label := "Game"
		if info := game.Get(ws.GameID); info != nil && info.Name != "" {
			label = info.Name
		}
		out.Roots = append(out.Roots, IdeRoot{
			Label: label, Path: scriptRoot,
			ReadOnly: true, Kind: "game", Origin: game.OriginVanilla,
			Color: ws.GameColor,
		})
	}
	return out, nil
}

// SaveIdeSession stores open editor paths for a workspace.
func (w *WorkspaceService) SaveIdeSession(workspaceID string, files []string, active string) error {
	return w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.IdeOpenFiles = append([]string(nil), files...)
		ws.IdeActiveFile = active
		return nil
	})
}
