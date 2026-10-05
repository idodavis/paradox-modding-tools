// workspaceservice.go manages the game installs, the workspaces and their mods, and the IDE roots.

package services

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"uuid"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/session"
	"paradox-modding-tools/services/internal/steam"
)

// WorkspaceService manages workspaces, game installs, and mods.
type WorkspaceService struct {
	App     *AppService
	Session *SessionService
}

// Game installs.

// ListGames returns the supported-game table and vanilla origin token.
func (s *WorkspaceService) ListGames() game.GameList {
	return game.GameList{OriginVanilla: game.OriginVanilla, Games: game.All()}
}

// ListGameInstalls returns a game's installs, each with its scan time.
func (s *WorkspaceService) ListGameInstalls(gameID string) ([]GameInstall, error) {
	var out []GameInstall
	languages := map[string]string{} // by install id: the language of the first workspace on it
	s.App.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			if inst.GameID == gameID {
				out = append(out, inst)
			}
		}
		for _, ws := range cfg.Workspaces {
			if _, found := languages[ws.InstallID]; !found {
				languages[ws.InstallID] = ws.DefaultLocLang
			}
		}
	})

	// a cache file's time per install, so the config lock is back before any of it
	for i := range out {
		out[i].ScannedAt = s.Session.scannedAt(&out[i], languages[out[i].ID])
	}
	return out, nil
}

// FindGameInstalls returns Steam-detected installs for a game.
func (s *WorkspaceService) FindGameInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// DetectGameVersion reads an install's version (launcher-settings.json, else the Steam manifest).
func (s *WorkspaceService) DetectGameVersion(gameID, path string) string {
	info := game.Get(gameID)
	if info == nil {
		return ""
	}
	return game.ReadGameVersion(path, info.SteamAppID)
}

// AddGameInstall adds a new game installation, with the version it holds now.
func (s *WorkspaceService) AddGameInstall(gameID, name, path string) (*GameInstall, error) {
	info := game.Get(gameID)
	if info == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	inst := GameInstall{
		ID: uuid.New().String(), GameID: gameID, Name: name, Path: path,
		VersionDetected: game.ReadGameVersion(path, info.SteamAppID),
	}
	err := s.App.mutate(func(cfg *Config) error {
		cfg.Installs = append(cfg.Installs, inst)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert install: %w", err)
	}
	return &inst, nil
}

// UpdateGameInstall updates path, then refreshes version.
func (s *WorkspaceService) UpdateGameInstall(id, path string) (*GameInstall, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("invalid path: %w", err)
		}
	}
	var out *GameInstall
	err := s.App.mutate(func(cfg *Config) error {
		inst := findInstall(cfg, id)
		if inst == nil {
			return fmt.Errorf("install not found")
		}
		info := game.Get(inst.GameID)
		if info == nil {
			return fmt.Errorf("unknown game %s", inst.GameID)
		}
		if path != "" {
			inst.Path = path
		}
		inst.VersionDetected = game.ReadGameVersion(inst.Path, info.SteamAppID)
		cp := *inst
		out = &cp
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update install: %w", err)
	}
	return out, nil
}

// DeleteGameInstall removes an install if no workspace uses it.
func (s *WorkspaceService) DeleteGameInstall(id string) (inUse []string, err error) {
	var listed GameInstall
	s.App.read(func(cfg *Config) {
		inUse = workspacesUsingInstall(cfg, id)
		if found := findInstall(cfg, id); found != nil {
			listed = *found
		}
	})
	if len(inUse) > 0 {
		return inUse, fmt.Errorf("install in use by: %s", strings.Join(inUse, ", "))
	}
	if listed.ID != "" {
		if err := index.DropInstall(index.Request{Game: listed.GameID, Dir: listed.Path}); err != nil {
			return nil, err
		}
	}
	return nil, s.App.mutate(func(cfg *Config) error {
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
func (s *WorkspaceService) ListWorkspaces(gameID string) ([]Workspace, error) {
	var out []Workspace
	s.App.read(func(cfg *Config) {
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
func (s *WorkspaceService) GetWorkspace(id string) (*Workspace, error) {
	if id == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	var ws *Workspace
	s.App.read(func(cfg *Config) {
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
func (s *WorkspaceService) CreateWorkspace(
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
	err := s.App.mutate(func(cfg *Config) error {
		cfg.Workspaces = append(cfg.Workspaces, ws)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return &ws, nil
}

// UpdateWorkspace updates workspace fields. A different install retires the session.
func (s *WorkspaceService) UpdateWorkspace(id, name, installID string) error {
	var moved bool
	err := s.mutateWorkspace(id, func(ws *Workspace) error {
		moved = ws.InstallID != installID
		ws.Name, ws.InstallID = name, installID
		return nil
	})
	if err == nil && moved {
		s.Session.drop(id)
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
func (s *WorkspaceService) UpdateWorkspacePrefs(
	workspaceID string, resetIdeOnOpen bool, defaultTool, gameColor string,
	hideExplorerBinaries bool,
) error {
	if !legalDefaultTools[defaultTool] {
		return fmt.Errorf("invalid default tool")
	}
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.ResetIdeOnOpen = resetIdeOnOpen
		ws.DefaultTool = defaultTool
		ws.GameColor = gameColor
		ws.HideExplorerBinaries = hideExplorerBinaries
		return nil
	})
}

// SetWorkspaceLocLang sets the default loc language; a live session is built again with it.
func (s *WorkspaceService) SetWorkspaceLocLang(id, lang string) error {
	err := s.mutateWorkspace(id, func(ws *Workspace) error {
		ws.DefaultLocLang = lang
		return nil
	})
	if err != nil {
		return err
	}
	s.Session.drop(id)
	return nil
}

// CountWorkspacesUsingInstall returns names of workspaces on this install.
func (s *WorkspaceService) CountWorkspacesUsingInstall(installID string) []string {
	var names []string
	s.App.read(func(cfg *Config) {
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
func (s *WorkspaceService) DeleteWorkspace(id string) error {
	if id == "" {
		return fmt.Errorf("workspace id is required")
	}
	err := s.App.mutate(func(cfg *Config) error {
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
	s.Session.drop(id)
	return nil
}

func (s *WorkspaceService) mutateWorkspace(workspaceID string, fn func(*Workspace) error) error {
	return s.App.mutate(func(cfg *Config) error {
		ws := findWorkspace(cfg, workspaceID)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		return fn(ws)
	})
}

// Mods in a workspace.

// DefaultModParent is UserDataDir/<game>/mod, or "".
func (s *WorkspaceService) DefaultModParent(gameID string) string {
	return local.DefaultModParent(gameID)
}

// CreateMod writes a new mod skeleton under parentDir and returns its path.
func (s *WorkspaceService) CreateMod(
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
func (s *WorkspaceService) AddWorkspaceMod(
	workspaceID, name, path, thumbnail string,
) (*WorkspaceMod, error) {
	var gameID string
	s.App.read(func(cfg *Config) {
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
		Thumbnail: thumbnail,
	}
	err := s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		mod.SortOrder, mod.Slot = len(ws.Mods), freeSlot(ws.Mods)
		ws.Mods = append(ws.Mods, mod)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert mod: %w", err)
	}
	s.Session.drop(workspaceID)
	return &mod, nil
}

// UpdateWorkspaceMod updates listing paths, ignore text, name, color, and thumbnail.
func (s *WorkspaceService) UpdateWorkspaceMod(
	workspaceID, modID, name, color, thumbnail, descMdRel, descBbRel, workshopIgnore string,
) error {
	return s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Name = cmp.Or(name, mod.Name)
		mod.Color, mod.Thumbnail = color, thumbnail
		markdown, err := steam.DescriptionRel(mod.Path, descMdRel)
		if err != nil {
			return err
		}
		bbcode, err := steam.DescriptionRel(mod.Path, descBbRel)
		if err != nil {
			return err
		}
		mod.DescMdRel, mod.DescBbRel, mod.WorkshopIgnore = markdown, bbcode, workshopIgnore
		return nil
	})
}

// SetWorkspaceModSlot gives a mod one of the color slots and clears its hand-picked color.
func (s *WorkspaceService) SetWorkspaceModSlot(workspaceID, modID string, slot int) error {
	if slot < 1 || slot > modSlots {
		return fmt.Errorf("slot %d is not between 1 and %d", slot, modSlots)
	}
	return s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Slot, mod.Color = slot, ""
		return nil
	})
}

// modSlots is how many color slots a workspace's mods share: eight hues at two lightnesses.
const modSlots = 16

// freeSlot is the lowest slot no mod in mods holds; with every slot held, slot 1 again.
func freeSlot(mods []WorkspaceMod) int {
	held := make([]bool, modSlots+1)
	for _, mod := range mods {
		if mod.Slot >= 1 && mod.Slot <= modSlots {
			held[mod.Slot] = true
		}
	}
	if free := slices.Index(held[1:], false); free >= 0 {
		return free + 1
	}
	return 1
}

// slotUnslotted gives each mod saved without a slot the lowest free one, in load order, and says
// whether it gave any.
func slotUnslotted(cfg *Config) bool {
	changed := false
	for w := range cfg.Workspaces {
		mods := cfg.Workspaces[w].Mods
		byOrder := make([]*WorkspaceMod, len(mods))
		for i := range mods {
			byOrder[i] = &mods[i]
		}
		slices.SortFunc(byOrder, func(first, second *WorkspaceMod) int { return loadOrder(*first, *second) })
		for _, mod := range byOrder {
			if mod.Slot == 0 {
				mod.Slot, changed = freeSlot(mods), true
			}
		}
	}
	return changed
}

// SetWorkspaceModCalendar declares the eras a mod's dates are written in: names trimmed, unnamed
// eras dropped; an empty list clears it, leaving every date as written.
func (s *WorkspaceService) SetWorkspaceModCalendar(workspaceID, modID string, eras session.Calendar) error {
	err := s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Eras = nil
		for _, era := range eras {
			if era.Name = strings.TrimSpace(era.Name); era.Name != "" {
				mod.Eras = append(mod.Eras, era)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// the session holds the calendar, so it is built again on the next request
	s.Session.drop(workspaceID)
	// VS Code re-requests a document's inlay hints only when told they changed
	emit(EventLangCalendarChanged, workspaceID)
	return nil
}

// ReorderWorkspaceMods writes SortOrder from the given id list.
func (s *WorkspaceService) ReorderWorkspaceMods(workspaceID string, modIDs []string) error {
	err := s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
	s.Session.drop(workspaceID)
	return nil
}

// RemoveWorkspaceMod detaches a mod from a workspace.
func (s *WorkspaceService) RemoveWorkspaceMod(workspaceID, modID string) error {
	err := s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
	s.Session.drop(workspaceID)
	return nil
}

// ExportWorkshopIgnore writes the saved ignore text to .workshop-ignore in the mod root.
func (s *WorkspaceService) ExportWorkshopIgnore(workspaceID, modID string) error {
	_, mod, err := findMod(s.App, workspaceID, modID)
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
func (s *WorkspaceService) ImportWorkshopIgnore(workspaceID, modID string) (string, error) {
	_, mod, err := findMod(s.App, workspaceID, modID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(mod.Path, steam.IgnoreFile))
	if err != nil {
		return "", err
	}
	text := string(raw)
	err = s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.WorkshopIgnore = text
		return nil
	})
	if err != nil {
		return "", err
	}
	return text, nil
}

func findMod(app *AppService, workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	ws, _ := app.workspace(workspaceID)
	if ws == nil {
		return nil, nil, fmt.Errorf("workspace not found")
	}
	for i := range ws.Mods {
		if ws.Mods[i].ID == modID {
			return ws, &ws.Mods[i], nil
		}
	}
	return nil, nil, fmt.Errorf("mod not found")
}

func (s *WorkspaceService) mutateMod(workspaceID, modID string, fn func(*WorkspaceMod) error) error {
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
	Slot      int    `json:"slot,omitempty"`
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
func (s *WorkspaceService) GetIdeRoots(workspaceID string) (*IdeRoots, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}

	// one read, so the workspace and the install it names cannot disagree
	var ws *Workspace
	var scriptRoot string
	s.App.read(func(cfg *Config) {
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
			Origin: mod.ID, Color: mod.Color, Slot: mod.Slot, Thumbnail: mod.Thumbnail,
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
func (s *WorkspaceService) SaveIdeSession(workspaceID string, files []string, active string) error {
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.IdeOpenFiles = append([]string(nil), files...)
		ws.IdeActiveFile = active
		return nil
	})
}
