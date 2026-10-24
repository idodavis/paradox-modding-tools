package workspace

import (
	"crypto/rand"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
)

// A FileChange is one file of the open workspace that changed or was deleted on disk. It is the data of the fs:changed
// event.
type FileChange struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// Service is the workspace service. It keeps the records of installs, workspaces and mods, holds the one open
// workspace that every other service reads through Session, changes and serves the workspace's files, and launches
// the game.
type Service struct {
	Config  *app.ConfigFile // config.json, for the records and the folders
	Build   string          // the app's version, which names the caches
	mu      sync.Mutex      // held for the whole of an open
	session *Session        // nil while no workspace is open
}

// A State is the open workspace as the frontend sees it. Open is false and the rest is empty while none is open.
type State struct {
	Open     bool               `json:"open"`
	ID       string             `json:"id"`
	Game     *game.Game         `json:"game"`
	Install  app.Install        `json:"install"`
	Mods     []Mod              `json:"mods"`
	Language string             `json:"language"`
	Dumps    schema.SourceState `json:"dumps"`
}

// A FileText is a file's text in its encoding, together with where the file sits in the workspace.
type FileText struct {
	Place Place      `json:"place"`
	Text  files.Text `json:"text"`
}

// A Child is one entry in a folder listing.
type Child struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Place Place  `json:"place"`
}

// The events the workspace service sends.
const (
	EventOpened          = "workspace:opened" // a workspace is open with its index built; the data is its id
	EventFailed          = "workspace:failed" // a workspace could not be opened; the data is the error's text
	EventClosed          = "workspace:closed" // the open workspace closed
	EventInstallScanning = "install:scanning" // an install's scan reports a stage; the data is the stage
	EventInstallScanned  = "install:scanned"  // an install's scan finished; the data is the install's id
	EventFileChanged     = "fs:changed"       // a file of the open workspace changed on disk; the data is a FileChange
	EventGameExited      = "game:exited"      // the launched game exited; the data is true when the dumps are now fresher
)

// init registers the workspace events with the application.
func init() {
	application.RegisterEvent[string](EventOpened)
	application.RegisterEvent[string](EventFailed)
	application.RegisterEvent[struct{}](EventClosed)
	application.RegisterEvent[string](EventInstallScanning)
	application.RegisterEvent[string](EventInstallScanned)
	application.RegisterEvent[FileChange](EventFileChanged)
	application.RegisterEvent[bool](EventGameExited)
}

// ServiceShutdown closes the open workspace.
func (wsp *Service) ServiceShutdown() error {
	wsp.Close()
	return nil
}

// ListGames returns the supported games.
func (wsp *Service) ListGames() []*game.Game { return game.ListGames() }

// ListInstalls returns every recorded install.
func (wsp *Service) ListInstalls() []app.Install { return wsp.Config.Config().Installs }

// DetectInstalls finds a game's installs on this machine.
func (wsp *Service) DetectInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// AddInstall records an install of the game with the given id in the given folder, reads its version and emits
// config:changed for the installs.
func (wsp *Service) AddInstall(gameID, path string) (app.Install, error) {
	install := app.Install{ID: rand.Text(), Game: gameID, Path: path, Version: game.LoadGameVersion(gameID, path)}
	err := wsp.Config.Update("installs", func(records *app.Config) error {
		records.Installs = append(records.Installs, install)
		return nil
	})
	return install, err
}

// DeleteInstall removes the record of the install with the given id. It returns an error while a workspace uses the
// install.
func (wsp *Service) DeleteInstall(id string) error {
	return wsp.Config.Update("installs", func(records *app.Config) error {
		if lo.ContainsBy(records.Workspaces, func(record app.Workspace) bool { return record.Install == id }) {
			return fmt.Errorf("a workspace uses the install %s", id)
		}
		records.Installs = slices.DeleteFunc(records.Installs, func(install app.Install) bool { return install.ID == id })
		return nil
	})
}

// RescanInstall drops the caches of the install with the given id, so its next open scans it again. If the open
// workspace uses the install, that workspace is opened again at once. It returns an error if the install is not
// recorded or its caches cannot be deleted.
func (wsp *Service) RescanInstall(id string) error {
	records := wsp.Config.Config()
	install, row, err := findInstall(records, id)
	if err != nil {
		return err
	}
	folder, err := resolveCaches(wsp.Config.Dirs.Cache, row, install, wsp.Build)
	if err != nil {
		return err
	}
	wsp.mu.Lock()
	reopen := ""
	if wsp.session != nil {
		if record, _, _, err := findRecords(records, wsp.session.ID); err == nil && record.Install == id {
			reopen = record.ID
		}
	}
	wsp.mu.Unlock()
	if err := os.RemoveAll(folder); err != nil {
		return err
	}
	if reopen != "" {
		wsp.Close()
		wsp.Open(reopen)
	}
	return nil
}

// ResetData closes the open workspace, then removes every workspace, install record and cache. Mods, game files and
// the settings stay. It returns an error if the records cannot be written or a cache cannot be deleted.
func (wsp *Service) ResetData() error {
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	wsp.closeSession()
	// the records go first, so the frontend never shows a workspace that is gone
	err := wsp.Config.Update("", func(records *app.Config) error {
		records.Installs, records.Workspaces = nil, nil
		return nil
	})
	if err != nil {
		return err
	}
	cache := wsp.Config.Dirs.Cache
	entries, err := os.ReadDir(cache)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cache, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ListWorkspaces returns every recorded workspace.
func (wsp *Service) ListWorkspaces() []app.Workspace { return wsp.Config.Config().Workspaces }

// CreateWorkspace records a new workspace under an id of its own and emits config:changed for the workspaces. It
// returns the workspace with that id.
func (wsp *Service) CreateWorkspace(ws app.Workspace) (app.Workspace, error) {
	ws.ID = rand.Text()
	err := wsp.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = append(records.Workspaces, ws)
		return nil
	})
	return ws, err
}

// UpdateWorkspace replaces the record of the workspace that has the given workspace's id. It returns an error if the
// workspace is not recorded.
func (wsp *Service) UpdateWorkspace(ws app.Workspace) error {
	return wsp.updateWorkspace(ws.ID, func(record *app.Workspace) error {
		*record = ws
		return nil
	})
}

// updateWorkspace changes the record of the workspace with the given id and emits config:changed for the workspaces.
// It returns an error if the workspace is not recorded or the change fails.
func (wsp *Service) updateWorkspace(workspaceID string, change func(record *app.Workspace) error) error {
	return wsp.Config.Update("workspaces", func(records *app.Config) error {
		position := slices.IndexFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
		if position < 0 {
			return fmt.Errorf("the workspace %s is not recorded", workspaceID)
		}
		return change(&records.Workspaces[position])
	})
}

// DeleteWorkspace removes the record of the workspace with the given id. The mod folders on disk stay.
func (wsp *Service) DeleteWorkspace(id string) error {
	return wsp.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = slices.DeleteFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == id })
		return nil
	})
}

// AddMod adds an existing mod folder to a workspace and gives the mod a color slot. It returns an error if the
// workspace is not recorded, the path is not a folder, or the workspace has the mod already.
func (wsp *Service) AddMod(workspaceID, dir string) (Mod, error) {
	_, _, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	if !files.IsFolder(dir) {
		return Mod{}, fmt.Errorf("%s is not a folder", dir)
	}
	record := app.Mod{ID: rand.Text(), Dir: dir}
	err = wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		if lo.ContainsBy(workspace.Mods, func(mod app.Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(dir) }) {
			return fmt.Errorf("%s is a mod of the workspace already", dir)
		}
		record.Color = resolveSlot(workspace.Mods)
		workspace.Mods = append(workspace.Mods, record)
		return nil
	})
	if err != nil {
		return Mod{}, err
	}
	descriptor, _ := ReadDescriptor(row, dir)
	return Mod{Mod: record, Descriptor: descriptor}, nil
}

// resolveSlot returns the color slot for a new mod: the first that no mod has. If every slot is in use, it is the slot
// at the mods' count, wrapping around.
func resolveSlot(mods []app.Mod) string {
	for number := 1; number <= 16; number++ {
		slot := fmt.Sprintf("o%d", number)
		if !lo.ContainsBy(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}
	return fmt.Sprintf("o%d", len(mods)%16+1)
}

// CreateMod makes a new mod with the given name in the game's mod folder of the user's data, and adds it to a
// workspace. It returns an error if the workspace is not recorded or the mod's folder exists already.
func (wsp *Service) CreateMod(workspaceID, name string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	holder := filepath.Join(game.FindUserDataDir(row.ID, install.Path), "mod")
	folder, err := createModFolder(row, holder, name, game.LoadGameVersion(row.ID, install.Path))
	if err != nil {
		return Mod{}, err
	}
	return wsp.AddMod(workspaceID, folder)
}

// RemoveMod takes the mod with the given id out of a workspace. Its folder stays.
func (wsp *Service) RemoveMod(workspaceID, modID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.Mods = slices.DeleteFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })
		return nil
	})
}

// ReorderMods sets a workspace's load order to the given mod ids. It returns an error unless the ids name each of the
// workspace's mods once.
func (wsp *Service) ReorderMods(workspaceID string, modIDs []string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		ordered, err := reorderMods(workspace.Mods, modIDs, func(mod app.Mod) string { return mod.ID })
		if err == nil {
			workspace.Mods = ordered
		}
		return err
	})
}

// reorderMods returns a workspace's mods in the order the given ids name them, each mod's id read by the given
// function. It returns an error if an id names no mod or names one twice, or if the ids leave a mod out.
func reorderMods[T any](mods []T, modIDs []string, id func(mod T) string) ([]T, error) {
	remaining := slices.Clone(mods)
	var ordered []T
	for _, modID := range modIDs {
		position := slices.IndexFunc(remaining, func(mod T) bool { return id(mod) == modID })
		if position < 0 {
			return nil, fmt.Errorf("the mod %s is not in the workspace or is named twice", modID)
		}
		ordered = append(ordered, remaining[position])
		remaining = slices.Delete(remaining, position, position+1)
	}
	if len(remaining) > 0 {
		return nil, fmt.Errorf("the order leaves out %d of the workspace's mods", len(remaining))
	}
	return ordered, nil
}

// UpdateMod replaces a mod's record in a workspace. A mod the workspace does not have is ignored.
func (wsp *Service) UpdateMod(workspaceID string, mod app.Mod) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		if position := slices.IndexFunc(workspace.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			workspace.Mods[position] = mod
		}
		return nil
	})
}

// Open opens a workspace in the background, closing the one that was open before. The install:scanning event
// reports each stage, and workspace:opened or workspace:failed ends it. A call to Session, State or Close while a
// workspace is opening waits for it. Opening the workspace that is already open does nothing.
func (wsp *Service) Open(workspaceID string) {
	wsp.mu.Lock()
	if wsp.session != nil && wsp.session.ID == workspaceID {
		wsp.mu.Unlock()
		return
	}
	wsp.closeSession()
	go func() {
		defer wsp.mu.Unlock()
		pmt := application.Get()
		session, err := wsp.openSession(workspaceID,
			func(stage string) { pmt.Event.Emit(EventInstallScanning, stage) },
			func(changes []FileChange) {
				for _, change := range changes {
					pmt.Event.Emit(EventFileChanged, change)
				}
			})
		if err != nil {
			pmt.Event.Emit(EventFailed, err.Error())
			return
		}
		wsp.session = session
		// the workspace opens even if the install record cannot be written
		_ = wsp.updateInstall(session)
		pmt.Event.Emit(EventOpened, workspaceID)
	}()
}

// updateInstall records the open session's version and index cache time on its install. It emits config:changed for
// the installs when the record changes, and install:scanned when the scan time changes.
func (wsp *Service) updateInstall(session *Session) error {
	_, install, _, err := findRecords(wsp.Config.Config(), session.ID)
	if err != nil {
		return err
	}
	stat, err := os.Stat(filepath.Join(session.caches, indexFile))
	if err != nil {
		return err
	}
	version, scannedAt := session.Schema.Version, stat.ModTime()
	scanned := !install.ScannedAt.Equal(scannedAt)
	if install.Version == version && !scanned {
		return nil
	}
	err = wsp.Config.Update("installs", func(records *app.Config) error {
		for position := range records.Installs {
			if records.Installs[position].ID == install.ID {
				records.Installs[position].Version, records.Installs[position].ScannedAt = version, scannedAt
			}
		}
		return nil
	})
	if err == nil && scanned {
		application.Get().Event.Emit(EventInstallScanned, install.ID)
	}
	return err
}

// closeSession closes the open session, if any, and emits workspace:closed. The caller holds the service's lock.
func (wsp *Service) closeSession() {
	if wsp.session == nil {
		return
	}
	wsp.session.Close()
	wsp.session = nil
	application.Get().Event.Emit(EventClosed, struct{}{})
}

// Session returns the open workspace, or an error saying that none is open. It is not bound to the frontend.
//
//wails:ignore
func (wsp *Service) Session() (*Session, error) {
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	if wsp.session == nil {
		return nil, errors.New("no workspace is open")
	}
	return wsp.session, nil
}

// Close closes the open workspace, if any.
func (wsp *Service) Close() {
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	wsp.closeSession()
}

// State returns the state of the open workspace, with Open false when there is none.
func (wsp *Service) State() State {
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	session := wsp.session
	if session == nil {
		return State{}
	}
	// a workspace recorded no more leaves the install empty
	record, install, _, _ := findRecords(wsp.Config.Config(), session.ID)
	// the user may have edited a mod's record since the open, so each mod carries the record as the config holds it
	mods := lo.Map(session.Order, func(mod Mod, _ int) Mod {
		if position := slices.IndexFunc(record.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			mod.Mod = record.Mods[position]
		}
		return mod
	})
	return State{
		Open: true, ID: session.ID, Game: session.Game, Install: install, Mods: mods,
		Language: session.Language, Dumps: session.Schema.Dumps,
	}
}

// LookupMod returns the mod of the open workspace with the given id, as State gives it. It returns false if no
// workspace is open or it has no such mod. It is not bound to the frontend.
//
//wails:ignore
func (wsp *Service) LookupMod(modID string) (Mod, bool) {
	return lo.Find(wsp.State().Mods, func(mod Mod) bool { return mod.ID == modID })
}

// Launch starts the game with its debug flag. When the game exits, the game:exited event says whether it wrote
// fresher dumps. It returns an error if no workspace is open or the game cannot be started.
func (wsp *Service) Launch() error {
	session, err := wsp.Session()
	if err != nil {
		return err
	}
	program, args, ok := game.ResolveProgram(session.Game.ID, session.InstallPath)
	if !ok {
		return fmt.Errorf("%s cannot be started on this system", session.Game.Name)
	}
	command := exec.Command(program, args...)
	command.Dir = filepath.Dir(program)
	if err := command.Start(); err != nil {
		return err
	}
	before := session.Schema.Dumps.WrittenAt
	go func() {
		_ = command.Wait()
		dumpsAt, err := schema.FindDumpsAt(game.FindDumpDirs(session.Game.ID, session.InstallPath))
		application.Get().Event.Emit(EventGameExited, err == nil && dumpsAt.After(before))
	}()
	return nil
}

// Read returns the text of the file with the given path and where the file sits in the workspace. It returns an error
// if no workspace is open or the file cannot be read.
func (wsp *Service) Read(path string) (FileText, error) {
	session, err := wsp.Session()
	if err != nil {
		return FileText{}, err
	}
	text, err := files.ReadText(path)
	return FileText{Place: session.Locate(session.Mods.Load(), path), Text: text}, err
}

// Write writes a file's text in its encoding and makes the file's folder if it is missing. The old file is replaced
// whole. The watcher sends fs:changed once the index has the new text. It returns an error for a path outside the
// workspace's mods or in the game's own files.
func (wsp *Service) Write(path string, text files.Text) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	return files.WriteText(path, text)
}

// findWritable returns the open session if each of the given paths is in one of the workspace's mods. It returns an
// error for a path outside the workspace, for one of the game's own files, or when no workspace is open.
func (wsp *Service) findWritable(paths ...string) (*Session, error) {
	session, err := wsp.Session()
	if err != nil {
		return nil, err
	}
	mods := session.Mods.Load()
	for _, path := range paths {
		switch place := session.Locate(mods, path); {
		case place.Root == "":
			return nil, fmt.Errorf("%s is outside the workspace", path)
		case place.ReadOnly:
			return nil, errors.New("the game's own files are not changed")
		}
	}
	return session, nil
}

// ListFolder lists the entries of a folder with the place of each, folders first. Binary files are left out when the
// setting hides them. It returns an error if no workspace is open or the folder cannot be read.
func (wsp *Service) ListFolder(dir string) ([]Child, error) {
	session, err := wsp.Session()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	hide, _ := wsp.Config.LookupSetting("ide.hideBinaries")
	mods := session.Mods.Load()
	children := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (Child, bool) {
		place := session.Locate(mods, filepath.Join(dir, entry.Name()))
		return Child{Name: entry.Name(), Dir: entry.IsDir(), Place: place}, entry.IsDir() || hide != "true" || place.Format.IsText()
	})
	slices.SortStableFunc(children, func(a, b Child) int {
		switch {
		case a.Dir == b.Dir:
			return 0
		case a.Dir:
			return -1
		}
		return 1
	})
	return children, nil
}

// ListRoots returns the folders the open workspace's files are listed from: the mods' in load order, then the game's.
// It returns none if no workspace is open.
func (wsp *Service) ListRoots() []files.Root {
	session, err := wsp.Session()
	if err != nil {
		return nil
	}
	return session.ListRoots()
}

// Create makes a new empty file, or a folder when asked for one. The watcher sends fs:changed for it. It returns an
// error if the path exists already, is outside the workspace's mods or is in the game's own files.
func (wsp *Service) Create(path string, dir bool) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	var err error
	switch {
	case dir:
		err = os.Mkdir(path, 0o755)
	default:
		var file *os.File
		if file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
			err = file.Close()
		}
	}
	return err
}

// Rename moves a file or a folder to a new path. The watcher sends fs:changed for both paths. It returns an error if
// the new path exists already or either path is outside the workspace's mods.
func (wsp *Service) Rename(from, to string) error {
	if _, err := wsp.findWritable(from, to); err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s exists already", to)
	}
	return os.Rename(from, to)
}

// Delete removes a file or a folder, and the watcher sends fs:changed for it. Deleting a mod's root folder removes
// the mod from the workspace as well.
func (wsp *Service) Delete(path string) error {
	session, err := wsp.findWritable(path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	if mod, ok := lo.Find(session.Order, func(mod Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(path) }); ok {
		return wsp.RemoveMod(session.ID, mod.ID)
	}
	return nil
}

// Locate returns the place of a file in the open workspace. It returns an empty place if none is open.
func (wsp *Service) Locate(path string) Place {
	session, err := wsp.Session()
	if err != nil {
		return Place{}
	}
	return session.Locate(session.Mods.Load(), path)
}

// ServeHTTP serves the file that the path of a request names. A DDS or TGA image is decoded to PNG first, since the
// browser cannot show it. A path outside the workspace's roots is refused, and so is any request while no workspace is
// open.
func (wsp *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	session, err := wsp.Session()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if _, ok := session.Mods.Load().FindRoot(path); !ok {
		http.Error(w, "the path is outside the workspace", http.StatusForbidden)
		return
	}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".dds" && ext != ".tga" {
		http.ServeFile(w, r, path)
		return
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	picture, err := texture.Decode(bytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, picture)
}
