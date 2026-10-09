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

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
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

	// a cache file's time and the version per install, so the config lock is back before any of it
	for i := range out {
		out[i].ScannedAt = s.Session.scannedAt(&out[i], languages[out[i].ID])
		out[i].VersionDetected = game.ReadGameVersion(out[i].GameID, out[i].Path)
	}
	return out, nil
}

// FindGameInstalls returns Steam-detected installs for a game.
func (s *WorkspaceService) FindGameInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// AddGameInstall adds a new game installation, with the version it holds now.
func (s *WorkspaceService) AddGameInstall(gameID, name, path string) (*GameInstall, error) {
	if game.Get(gameID) == nil {
		return nil, fmt.Errorf("unknown game %s", gameID)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	inst := GameInstall{ID: uuid.New().String(), GameID: gameID, Name: name, Path: path}
	err := s.App.mutate("installs", func(cfg *Config) error {
		cfg.Installs = append(cfg.Installs, inst)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert install: %w", err)
	}
	inst.VersionDetected = game.ReadGameVersion(gameID, path)
	return &inst, nil
}

// UpdateGameInstall changes an install's path.
func (s *WorkspaceService) UpdateGameInstall(id, path string) error {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	}
	err := s.App.mutate("installs", func(cfg *Config) error {
		inst := findInstall(cfg, id)
		if inst == nil {
			return fmt.Errorf("install not found")
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
func (s *WorkspaceService) DeleteGameInstall(id string) error {
	var inUse []string
	var listed GameInstall
	s.App.read(func(cfg *Config) {
		for _, ws := range cfg.Workspaces {
			if ws.InstallID == id {
				inUse = append(inUse, ws.Name)
			}
		}
		if found := findInstall(cfg, id); found != nil {
			listed = *found
		}
	})
	if len(inUse) > 0 {
		return fmt.Errorf("install in use by: %s", strings.Join(inUse, ", "))
	}
	if listed.ID != "" {
		if err := index.DropInstall(index.Request{Game: listed.GameID, Dir: listed.Path}); err != nil {
			return err
		}
	}
	return s.App.mutate("installs", func(cfg *Config) error {
		cfg.Installs = slices.DeleteFunc(cfg.Installs, func(inst GameInstall) bool { return inst.ID == id })
		return nil
	})
}

// Workspaces.

// ListWorkspaces returns every workspace, its mods in load order.
func (s *WorkspaceService) ListWorkspaces() []Workspace {
	var out []Workspace
	s.App.read(func(cfg *Config) {
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
	err := s.App.mutate("workspaces", func(cfg *Config) error {
		cfg.Workspaces = append(cfg.Workspaces, ws)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	return &ws, nil
}

// UpdateWorkspace updates workspace fields; the session follows a different install by itself (SessionService.get).
func (s *WorkspaceService) UpdateWorkspace(id, name, installID string) error {
	return s.mutateWorkspace(id, func(ws *Workspace) error {
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
	return s.mutateWorkspace(id, func(ws *Workspace) error {
		ws.DefaultLocLang = lang
		return nil
	})
}

// DeleteWorkspace removes a workspace from PMT. Mod folders on disk stay.
func (s *WorkspaceService) DeleteWorkspace(id string) error {
	err := s.App.mutate("workspaces", func(cfg *Config) error {
		listed := len(cfg.Workspaces)
		cfg.Workspaces = slices.DeleteFunc(cfg.Workspaces, func(ws Workspace) bool { return ws.ID == id })
		if len(cfg.Workspaces) == listed {
			return errNoWorkspace
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.Session.drop(id)
	return nil
}

func (s *WorkspaceService) mutateWorkspace(workspaceID string, fn func(*Workspace) error) error {
	return s.App.mutate("workspaces", func(cfg *Config) error {
		ws := findWorkspace(cfg, workspaceID)
		if ws == nil {
			return errNoWorkspace
		}
		return fn(ws)
	})
}

// Mods in a workspace.

// DefaultModParent is UserDataDir/<game>/mod, or "".
func (s *WorkspaceService) DefaultModParent(gameID string) string {
	return appdata.DefaultModParent(gameID)
}

// ModFolder is the folder CreateMod makes for a mod named name under parentDir.
func (s *WorkspaceService) ModFolder(parentDir, name string) string {
	return filepath.Join(parentDir, appdata.ModSlug(name))
}

// CreateMod writes a new mod skeleton under parentDir and returns its path.
func (s *WorkspaceService) CreateMod(
	gameID, parentDir, name, locLang, supportedVersion, description, thumbnailSrc string,
) (string, error) {
	// WriteNewMod refuses an unknown game or an empty name
	name = strings.TrimSpace(name)
	if parentDir == "" {
		parentDir = appdata.DefaultModParent(gameID)
	}
	if parentDir == "" {
		return "", fmt.Errorf("parent folder is required")
	}
	root := s.ModFolder(parentDir, name)
	if err := appdata.WriteNewMod(appdata.NewModOpts{
		GameID: gameID, Root: root, Name: name, SupportedVersion: supportedVersion,
		LocLang: locLang, ThumbnailSrc: thumbnailSrc,
	}); err != nil {
		return "", err
	}
	return root, steam.NewDescription(root, steam.DescMdName, steam.FromMarkdown, name, description)
}

// AddWorkspaceMod adds a mod to a workspace: a folder holding its game's descriptor, named by it (else
// by the folder), with the thumbnail where its game keeps one, last in load order.
func (s *WorkspaceService) AddWorkspaceMod(workspaceID, path string) (*WorkspaceMod, error) {
	gameID, err := s.modFolderOf(workspaceID, path)
	if err != nil {
		return nil, err
	}
	fields, err := appdata.ReadListingFields(gameID, path)
	if err != nil {
		return nil, err
	}
	mod := WorkspaceMod{ID: uuid.New().String(), Name: cmp.Or(fields.Name, filepath.Base(path)), Path: path}
	if rel, found := appdata.FindThumbnail(gameID, path, fields.Picture); found {
		mod.Thumbnail = filepath.Join(path, filepath.FromSlash(rel))
	}
	err = s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
func (s *WorkspaceService) modFolderOf(workspaceID, path string) (string, error) {
	ws, _, err := s.App.workspace(workspaceID)
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
// session is built again from it.
func (s *WorkspaceService) SetWorkspaceModPath(workspaceID, modID, path string) error {
	if _, err := s.modFolderOf(workspaceID, path); err != nil {
		return err
	}
	return s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Path = path
		return nil
	})
}

// OpenFolder shows a folder in the system's file manager.
func (s *WorkspaceService) OpenFolder(path string) error {
	return application.Get().Env.OpenFileManager(path, false)
}

// UpdateWorkspaceMod updates a mod's name, color, thumbnail and upload exclusions.
func (s *WorkspaceService) UpdateWorkspaceMod(workspaceID, modID, name, color, thumbnail, workshopIgnore string) error {
	return s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Name = cmp.Or(name, mod.Name)
		mod.Color, mod.Thumbnail, mod.WorkshopIgnore = color, thumbnail, workshopIgnore
		return nil
	})
}

// SetModListingImage makes src the mod's listing image: copied in where its game keeps one, named in
// its descriptor (its Workshop id as it is) and made the workspace's picture of the mod. It returns the
// image's path.
func (s *WorkspaceService) SetModListingImage(workspaceID, modID, src string) (string, error) {
	ws, mod, err := findMod(s.App, workspaceID, modID)
	if err != nil {
		return "", err
	}
	rel, err := appdata.CopyThumbnail(ws.GameID, mod.Path, src)
	if err != nil {
		return "", err
	}
	fields, err := appdata.ReadListingFields(ws.GameID, mod.Path)
	if err != nil {
		return "", err
	}
	fields.Picture = picture(rel)
	if err := appdata.WriteListingFields(ws.GameID, mod.Path, fields); err != nil {
		return "", err
	}
	image := filepath.Join(mod.Path, filepath.FromSlash(rel))
	return image, s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		mod.Thumbnail = image
		return nil
	})
}

// SetWorkspaceModDescription sets where a mod's Workshop description comes from: a Markdown or a
// BBCode file at rel ("" for the default name), or Steam's own text.
func (s *WorkspaceService) SetWorkspaceModDescription(workspaceID, modID, source, rel string) error {
	return s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
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
	return err
}

// ReorderWorkspaceMods writes SortOrder from the given id list.
func (s *WorkspaceService) ReorderWorkspaceMods(workspaceID string, modIDs []string) error {
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
				return errNoMod
			}
			mod.SortOrder = i
			delete(byID, id)
		}
		if len(byID) != 0 {
			return fmt.Errorf("mod id set incomplete")
		}
		return nil
	})
}

// RemoveWorkspaceMod detaches a mod from a workspace.
func (s *WorkspaceService) RemoveWorkspaceMod(workspaceID, modID string) error {
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
		listed := len(ws.Mods)
		ws.Mods = slices.DeleteFunc(ws.Mods, func(mod WorkspaceMod) bool { return mod.ID == modID })
		if len(ws.Mods) == listed {
			return errNoMod
		}
		return nil
	})
}

// ExportWorkshopIgnore writes the saved ignore text to .workshop-ignore in the mod root.
func (s *WorkspaceService) ExportWorkshopIgnore(workspaceID, modID string) error {
	_, mod, err := findMod(s.App, workspaceID, modID)
	if err != nil {
		return err
	}
	return appdata.WriteBytes(filepath.Join(mod.Path, steam.IgnoreFile), []byte(mod.WorkshopIgnore))
}

// ImportWorkshopIgnore reads .workshop-ignore into workspace config and returns the text.
func (s *WorkspaceService) ImportWorkshopIgnore(workspaceID, modID string) (string, error) {
	var text string
	err := s.mutateMod(workspaceID, modID, func(mod *WorkspaceMod) error {
		raw, err := os.ReadFile(filepath.Join(mod.Path, steam.IgnoreFile))
		if err != nil {
			return err
		}
		text, mod.WorkshopIgnore = string(raw), string(raw)
		return nil
	})
	return text, err
}

func findMod(app *AppService, workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	ws, _, err := app.workspace(workspaceID)
	if err != nil {
		return nil, nil, err
	}
	mod, err := ws.mod(modID)
	return ws, mod, err
}

func (s *WorkspaceService) mutateMod(workspaceID, modID string, fn func(*WorkspaceMod) error) error {
	return s.mutateWorkspace(workspaceID, func(ws *Workspace) error {
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
func (s *WorkspaceService) GetIdeRoots(workspaceID string) (*IdeRoots, error) {
	// one read, so the workspace and the install it names cannot disagree
	ws, inst, err := s.App.workspace(workspaceID)
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
