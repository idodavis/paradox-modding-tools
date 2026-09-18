// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/modfile"

	"github.com/google/uuid"
)

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// WorkspaceService manages workspaces, game installs, and mods.
type WorkspaceService struct {
	Store   *Store
	Session *SessionService
}

// ListGames returns the supported-game table and vanilla origin token.
func (w *WorkspaceService) ListGames() game.GameList {
	return game.GameList{OriginVanilla: game.OriginVanilla, Games: game.All()}
}

// ListGameInstalls returns all installs for a game, with VanillaCache scannedAt.
func (w *WorkspaceService) ListGameInstalls(gameID string) ([]GameInstall, error) {
	var out []GameInstall
	w.Store.Read(func(c *Config) {
		for _, inst := range c.Installs {
			if inst.GameID == gameID {
				cp := inst
				ver := index.CacheVersion(cp.Version)
				cp.ScannedAt = index.PeekCacheScannedAt(cp.ID, ver)
				out = append(out, cp)
			}
		}
	})
	return out, nil
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
	if version == "" {
		version = detected
	}
	version = index.CacheVersion(version)
	inst := GameInstall{
		ID: uuid.New().String(), GameID: gameID, Name: name, Path: path,
		Version: version, VersionDetected: detected, CreatedAt: nowUTC(),
	}
	err := w.Store.Mutate(func(c *Config) error {
		c.Installs = append(c.Installs, inst)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert install: %w", err)
	}
	return &inst, nil
}

// MarkBrokenPaths checks all installs and mods, marks missing paths as broken.
func (w *WorkspaceService) MarkBrokenPaths() error {
	return w.Store.Mutate(func(c *Config) error {
		for i := range c.Installs {
			_, err := os.Stat(c.Installs[i].Path)
			c.Installs[i].IsBroken = err != nil
		}
		for i := range c.Workspaces {
			for j := range c.Workspaces[i].Mods {
				_, err := os.Stat(c.Workspaces[i].Mods[j].Path)
				c.Workspaces[i].Mods[j].IsBroken = err != nil
			}
		}
		return nil
	})
}

// ListWorkspaces returns workspaces, optionally filtered by game. Pass "" for all.
func (w *WorkspaceService) ListWorkspaces(gameID string) ([]Workspace, error) {
	var out []Workspace
	w.Store.Read(func(c *Config) {
		for _, ws := range c.Workspaces {
			if gameID == "" || ws.GameID == gameID {
				out = append(out, cloneWorkspace(ws))
			}
		}
	})
	return out, nil
}

// GetWorkspace returns a workspace by ID.
func (w *WorkspaceService) GetWorkspace(id string) (*Workspace, error) {
	if id == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	var ws *Workspace
	w.Store.Read(func(c *Config) {
		if found := findWorkspace(c, id); found != nil {
			cp := cloneWorkspace(*found)
			ws = &cp
		}
	})
	if ws == nil {
		return nil, fmt.Errorf("workspace not found")
	}
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
	err := w.Store.Mutate(func(c *Config) error {
		c.Workspaces = append(c.Workspaces, ws)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return &ws, nil
}

// DeleteWorkspace removes a workspace from PMT. Mod folders on disk stay.
func (w *WorkspaceService) DeleteWorkspace(id string) error {
	if id == "" {
		return fmt.Errorf("workspace id is required")
	}
	err := w.Store.Mutate(func(c *Config) error {
		for i, ws := range c.Workspaces {
			if ws.ID == id {
				c.Workspaces = append(c.Workspaces[:i], c.Workspaces[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("workspace not found")
	})
	if err != nil {
		return err
	}
	if w.Session != nil {
		w.Session.pool().Drop(id)
	}
	return nil
}

// UpdateWorkspace updates workspace fields.
func (w *WorkspaceService) UpdateWorkspace(id, name, installID string) error {
	var rebuild bool
	err := w.mutateWorkspace(id, func(ws *Workspace) error {
		rebuild = ws.InstallID != installID
		ws.Name, ws.InstallID = name, installID
		return nil
	})
	if err != nil {
		return err
	}
	if rebuild {
		return w.rebuildSession(id)
	}
	return nil
}

func (w *WorkspaceService) rebuildSession(id string) error {
	if w.Session == nil {
		return nil
	}
	w.Session.pool().Drop(id)
	_, err := w.Session.EnsureSession(id)
	return err
}

// AddWorkspaceMod adds a mod to a workspace.
func (w *WorkspaceService) AddWorkspaceMod(
	workspaceID, name, path, thumbnail string,
) (*WorkspaceMod, error) {
	_, statErr := os.Stat(path)
	// An empty thumbnail is discovered, not assumed. The caller used to compute
	// `<root>/thumbnail<ext>` itself, which was a second copy of a rule that is
	// now per-game — EU5 keeps the file in .metadata — and GameInfo.ThumbnailDir
	// is backend-only, so the frontend cannot compute it even in principle. This
	// asks the same modfile.ThumbnailPath the Release page reads, so a mod's
	// thumbnail is one answer from one place whoever adds it.
	if thumbnail == "" {
		var gameID string
		w.Store.Read(func(c *Config) {
			if ws := findWorkspace(c, workspaceID); ws != nil {
				gameID = ws.GameID
			}
		})
		if rel, found := modfile.ThumbnailPath(gameID, path, ""); found {
			thumbnail = filepath.Join(path, filepath.FromSlash(rel))
		}
	}
	mod := WorkspaceMod{
		ID: uuid.New().String(), Name: name, Path: path,
		Thumbnail: thumbnail, IsBroken: statErr != nil, CreatedAt: nowUTC(),
	}
	err := w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		mod.SortOrder = len(ws.Mods)
		ws.Mods = append(ws.Mods, mod)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert mod: %w", err)
	}
	if err := w.rebuildSession(workspaceID); err != nil {
		return &mod, err
	}
	return &mod, nil
}

// DefaultModParent is UserDataDir/<game>/mod, or "".
func (w *WorkspaceService) DefaultModParent(gameID string) string {
	return modfile.DefaultModParent(gameID)
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
		parentDir = modfile.DefaultModParent(gameID)
	}
	if parentDir == "" {
		return "", fmt.Errorf("parent folder is required")
	}
	root := filepath.Join(parentDir, modfile.ModSlug(name))
	if err := modfile.WriteNewMod(modfile.NewModOpts{
		GameID: gameID, Root: root, Name: name, SupportedVersion: supportedVersion,
		LocLang: locLang, Description: description, ThumbnailSrc: thumbnailSrc,
	}); err != nil {
		return "", err
	}
	return root, nil
}

// RemoveWorkspaceMod detaches a mod from a workspace.
func (w *WorkspaceService) RemoveWorkspaceMod(workspaceID, modID string) error {
	err := w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		kept := ws.Mods[:0]
		found := false
		for _, m := range ws.Mods {
			if m.ID == modID {
				found = true
				continue
			}
			kept = append(kept, m)
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
	return w.rebuildSession(workspaceID)
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
			m, ok := byID[id]
			if !ok {
				return fmt.Errorf("mod not found")
			}
			m.SortOrder = i
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
	return w.rebuildSession(workspaceID)
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
			md, err := listingRel(ws.Mods[i].Path, descMdRel)
			if err != nil {
				return err
			}
			bb, err := listingRel(ws.Mods[i].Path, descBbRel)
			if err != nil {
				return err
			}
			ws.Mods[i].DescMdRel = md
			ws.Mods[i].DescBbRel = bb
			ws.Mods[i].WorkshopIgnore = workshopIgnore
			return nil
		}
		return fmt.Errorf("mod not found")
	})
}

const workshopIgnoreName = ".workshop-ignore"

// ExportWorkshopIgnore writes the saved ignore text to .workshop-ignore in the mod root.
func (w *WorkspaceService) ExportWorkshopIgnore(workspaceID, modID string) error {
	_, mod, err := w.modRef(workspaceID, modID)
	if err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(mod.Path, workshopIgnoreName),
		[]byte(mod.WorkshopIgnore),
		0o644,
	)
}

// ImportWorkshopIgnore reads .workshop-ignore into workspace config and returns the text.
func (w *WorkspaceService) ImportWorkshopIgnore(workspaceID, modID string) (string, error) {
	_, mod, err := w.modRef(workspaceID, modID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(mod.Path, workshopIgnoreName))
	if err != nil {
		return "", err
	}
	text := string(raw)
	err = w.mutateMod(workspaceID, modID, func(m *WorkspaceMod) error {
		m.WorkshopIgnore = text
		return nil
	})
	if err != nil {
		return "", err
	}
	return text, nil
}

func findMod(store *Store, workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	var ws Workspace
	var mod WorkspaceMod
	var foundWS, foundMod bool
	store.Read(func(c *Config) {
		w := findWorkspace(c, workspaceID)
		if w == nil {
			return
		}
		ws = *w
		foundWS = true
		for i := range w.Mods {
			if w.Mods[i].ID == modID {
				mod = w.Mods[i]
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

func (w *WorkspaceService) modRef(workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	return findMod(w.Store, workspaceID, modID)
}

// listingRel stores a description path relative to the mod root. Empty keeps the default.
func listingRel(root, chosen string) (string, error) {
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return "", nil
	}
	abs := chosen
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(chosen))
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("description file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("description path is a directory")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("description file must be inside the mod folder")
	}
	slash := filepath.ToSlash(rel)
	if slash == modfile.DescMdName || slash == modfile.DescBbName {
		return "", nil
	}
	return slash, nil
}

// SaveIdeSession stores open editor paths for a workspace.
func (w *WorkspaceService) SaveIdeSession(workspaceID string, files []string, active string) error {
	return w.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		ws.IdeOpenFiles = append([]string(nil), files...)
		ws.IdeActiveFile = active
		return nil
	})
}

var legalDefaultTools = map[string]bool{
	"":              true,
	"workspace-ide": true,
	"event-graph":   true,
	"health":        true,
	"patcher":       true,
	"release":       true,
}

// UpdateWorkspacePrefs sets IDE persist, default landing page, explorer hide, and game origin color.
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

// CountWorkspacesUsingInstall returns names of workspaces on this install.
func (w *WorkspaceService) CountWorkspacesUsingInstall(installID string) []string {
	var names []string
	w.Store.Read(func(c *Config) {
		names = workspacesUsingInstall(c, installID)
	})
	return names
}

func workspacesUsingInstall(c *Config, installID string) []string {
	var names []string
	for _, ws := range c.Workspaces {
		if ws.InstallID == installID {
			names = append(names, ws.Name)
		}
	}
	return names
}

// DetectGameVersion reads version from launcher-settings.json.
func (w *WorkspaceService) DetectGameVersion(path string) string {
	return game.ReadGameVersion(path)
}

// FindGameInstalls returns Steam-detected installs for a game.
func (w *WorkspaceService) FindGameInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// UpdateGameInstall updates path, then refreshes version.
func (w *WorkspaceService) UpdateGameInstall(id, path string) (*GameInstall, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("invalid path: %w", err)
		}
	}
	var out *GameInstall
	err := w.Store.Mutate(func(c *Config) error {
		inst := findInstall(c, id)
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
	ws, err := w.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
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
		if label == "" {
			label = "Mod"
		}
		out.Roots = append(out.Roots, IdeRoot{
			Label: label, Path: mod.Path, Kind: "mod",
			Origin: mod.ID, Color: mod.Color, Thumbnail: mod.Thumbnail,
		})
	}
	var scriptRoot string
	var rootErr error
	w.Store.Read(func(c *Config) {
		scriptRoot, rootErr = installScriptRoot(c, ws.InstallID)
	})
	if rootErr == nil && scriptRoot != "" {
		label := "Game"
		if g := game.Get(ws.GameID); g != nil && g.Name != "" {
			label = g.Name
		}
		out.Roots = append(out.Roots, IdeRoot{
			Label: label, Path: scriptRoot,
			ReadOnly: true, Kind: "game", Origin: game.OriginVanilla,
			Color: ws.GameColor,
		})
	}
	return out, nil
}

// DeleteGameInstall removes an install if no workspace uses it.
func (w *WorkspaceService) DeleteGameInstall(id string) (inUse []string, err error) {
	w.Store.Read(func(c *Config) {
		inUse = workspacesUsingInstall(c, id)
	})
	if len(inUse) > 0 {
		return inUse, fmt.Errorf("install in use by: %s", strings.Join(inUse, ", "))
	}
	if err := index.DropVanillaFiles(id); err != nil {
		return nil, err
	}
	return nil, w.Store.Mutate(func(c *Config) error {
		kept := c.Installs[:0]
		for _, inst := range c.Installs {
			if inst.ID != id {
				kept = append(kept, inst)
			}
		}
		c.Installs = kept
		return nil
	})
}

// SetInstallVersion pins the cache-key version (user override; empty → latest).
func (w *WorkspaceService) SetInstallVersion(id, version string) error {
	version = index.CacheVersion(version)
	return w.Store.Mutate(func(c *Config) error {
		inst := findInstall(c, id)
		if inst == nil {
			return fmt.Errorf("install not found")
		}
		inst.Version = version
		return nil
	})
}

// SetWorkspaceModCalendar declares the era a mod's date literals are written in.
//
// A setter of its own rather than two more parameters on UpdateWorkspaceMod,
// which already takes eight positional strings: a calendar is set once and
// rarely, by someone who knows their mod offsets the year, and it has nothing to
// do with the name-and-colour edits that method carries.
//
// era "" with offset 0 clears it, and a cleared calendar has to leave every date
// exactly as written -- that guardrail is the whole reason the feature is opt-in.
func (w *WorkspaceService) SetWorkspaceModCalendar(
	workspaceID, modID, era string, epochOffset int,
) error {
	err := w.mutateMod(workspaceID, modID, func(m *WorkspaceMod) error {
		m.Era = strings.TrimSpace(era)
		m.EpochOffset = epochOffset
		return nil
	})
	if err != nil {
		return err
	}
	// The session holds the calendar, so it has to be rebuilt before the next
	// hover reads a date. Dropping it is enough: the pool builds on demand.
	if w.Session != nil {
		w.Session.pool().Drop(workspaceID)
	}
	// And the editor has to be told. VS Code caches inlay hints per document and
	// re-requests them only when a provider says they changed, so without this a
	// modder sets a calendar and sees nothing until they edit the file -- or
	// clears one and keeps seeing eras that no longer apply.
	emitLang("lang:calendar-changed", map[string]any{"workspaceId": workspaceID})
	return nil
}

// SetWorkspaceLocLang sets default loc language and loc-harvests if a session is live.
func (w *WorkspaceService) SetWorkspaceLocLang(id, lang string) error {
	var inst *GameInstall
	// Not mutateWorkspace: this one needs the Config too, for findInstall.
	err := w.Store.Mutate(func(c *Config) error {
		ws := findWorkspace(c, id)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		ws.DefaultLocLang = lang
		if found := findInstall(c, ws.InstallID); found != nil {
			cp := *found
			inst = &cp
		}
		return nil
	})
	if err != nil || w.Session == nil {
		return err
	}
	live := w.Session.pool().Get(id)
	if live == nil {
		return nil
	}
	live.SetDefaultLang(lang)
	if inst == nil {
		return nil
	}
	v, err := index.HarvestLoc(context.Background(), inst.GameID, inst.Path, lang)
	if err != nil {
		return err
	}
	live.MergeVanillaLoc(v)
	return nil
}

func (w *WorkspaceService) mutateWorkspace(workspaceID string, fn func(*Workspace) error) error {
	return w.Store.Mutate(func(c *Config) error {
		ws := findWorkspace(c, workspaceID)
		if ws == nil {
			return fmt.Errorf("workspace not found")
		}
		return fn(ws)
	})
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
