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
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
)

// A FileChange is the data of the fs:changed event: the file's path and whether it was deleted.
type FileChange struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// Service is the workspace service. It keeps the records of games, installs, workspaces and mods, holds the one
// open workspace that every other service reads through Session, serves files to the explorer, and launches the
// game.
type Service struct {
	Config  *app.ConfigFile // config.json, for the records and the folders
	Build   string          // the app's version, which names the caches
	mu      sync.Mutex      // held for the whole of an open
	session *Session        // nil while no workspace is open
}

// State describes the open workspace: whether one is open, its game and install, its mods, its language and how
// fresh its dumps are.
type State struct {
	Open      bool               `json:"open"`
	ID        string             `json:"id"`
	Game      *game.Game         `json:"game"`
	Install   app.Install        `json:"install"`
	Mods      []Mod              `json:"mods"`
	Language  string             `json:"language"`
	Dumps     schema.SourceState `json:"dumps"`
	ScannedAt time.Time          `json:"scannedAt"`
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
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	if wsp.session != nil {
		wsp.session.Close()
		wsp.session = nil
	}
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

// AddInstall records an install of a game at a folder, reads its version, and emits config:changed for the
// installs.
func (wsp *Service) AddInstall(gameID, path string) (app.Install, error) {
	install := app.Install{ID: rand.Text(), Game: gameID, Path: path, Version: game.LoadGameVersion(gameID, path)}
	err := wsp.updateConfig("installs", func(records *app.Config) error {
		records.Installs = append(records.Installs, install)
		return nil
	})
	return install, err
}

// updateConfig changes the records under the config's lock and emits config:changed for the given section. A change
// that returns an error must have changed nothing, and nothing is emitted.
func (wsp *Service) updateConfig(section string, change func(records *app.Config) error) error {
	var failure error
	err := wsp.Config.Update(func(records *app.Config) { failure = change(records) })
	if err = errors.Join(err, failure); err != nil {
		return err
	}
	application.Get().Event.Emit(app.EventConfigChanged, section)
	return nil
}

// DeleteInstall removes an install's record. It returns an error while a workspace uses the install.
func (wsp *Service) DeleteInstall(id string) error {
	return wsp.updateConfig("installs", func(records *app.Config) error {
		if lo.ContainsBy(records.Workspaces, func(record app.Workspace) bool { return record.Install == id }) {
			return fmt.Errorf("a workspace uses the install %s", id)
		}
		records.Installs = slices.DeleteFunc(records.Installs, func(install app.Install) bool { return install.ID == id })
		return nil
	})
}

// RescanInstall drops an install's caches, so its next open scans it again. When the open workspace uses the
// install, it is opened again at once.
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

// ListWorkspaces returns every recorded workspace.
func (wsp *Service) ListWorkspaces() []app.Workspace { return wsp.Config.Config().Workspaces }

// CreateWorkspace records a new workspace and emits config:changed for the workspaces.
func (wsp *Service) CreateWorkspace(ws app.Workspace) (app.Workspace, error) {
	ws.ID = rand.Text()
	err := wsp.updateConfig("workspaces", func(records *app.Config) error {
		records.Workspaces = append(records.Workspaces, ws)
		return nil
	})
	return ws, err
}

// UpdateWorkspace replaces a workspace's record.
func (wsp *Service) UpdateWorkspace(ws app.Workspace) error {
	return wsp.updateWorkspace(ws.ID, func(record *app.Workspace) error {
		*record = ws
		return nil
	})
}

// updateWorkspace changes one workspace's record under the config's lock and emits config:changed for the
// workspaces. It returns an error if the workspace is not recorded or the change fails.
func (wsp *Service) updateWorkspace(workspaceID string, change func(record *app.Workspace) error) error {
	return wsp.updateConfig("workspaces", func(records *app.Config) error {
		position := slices.IndexFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
		if position < 0 {
			return fmt.Errorf("the workspace %s is not recorded", workspaceID)
		}
		return change(&records.Workspaces[position])
	})
}

// DeleteWorkspace removes a workspace's record. The mod folders on disk stay.
func (wsp *Service) DeleteWorkspace(id string) error {
	return wsp.updateConfig("workspaces", func(records *app.Config) error {
		records.Workspaces = slices.DeleteFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == id })
		return nil
	})
}

// AddMod adds an existing mod folder to a workspace.
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

// resolveSlot returns the color slot, o1 to o16, for a new mod: the first that no mod has. When all sixteen are
// taken, it is the one that follows the mods' count around the sixteen.
func resolveSlot(mods []app.Mod) string {
	for number := 1; number <= 16; number++ {
		slot := fmt.Sprintf("o%d", number)
		if !lo.ContainsBy(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}
	return fmt.Sprintf("o%d", len(mods)%16+1)
}

// CreateMod creates a new mod folder with its skeleton and adds it to a workspace.
func (wsp *Service) CreateMod(workspaceID, name string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	holder := filepath.Join(game.FindUserDataDir(row.ID, install.Path), "mod")
	if _, err := createModFolder(row, holder, name, game.LoadGameVersion(row.ID, install.Path)); err != nil {
		return Mod{}, err
	}
	return wsp.AddMod(workspaceID, resolveModFolder(holder, name))
}

// RemoveMod takes a mod out of a workspace. Its folder stays.
func (wsp *Service) RemoveMod(workspaceID, modID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.Mods = slices.DeleteFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })
		return nil
	})
}

// ReorderMods sets a workspace's load order.
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

// UpdateMod replaces a mod's record: its color, calendar, Workshop item and exclusions.
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
		session, err := Open(wsp.Config, wsp.Build, workspaceID,
			func(stage string) { pmt.Event.Emit(EventInstallScanning, stage) },
			func(path string, deleted bool) {
				pmt.Event.Emit(EventFileChanged, FileChange{Path: path, Deleted: deleted})
			})
		if err != nil {
			pmt.Event.Emit(EventFailed, err.Error())
			return
		}
		wsp.session = session
		// an install record that cannot be written is left as it was, and the workspace is open all the same
		_ = wsp.updateInstall(session)
		pmt.Event.Emit(EventOpened, workspaceID)
	}()
}

// updateInstall gives the install record of the open session the session's version and the time of its index
// cache. It emits config:changed for the installs when the record changed, and install:scanned when its scan time
// did.
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
	err = wsp.updateConfig("installs", func(records *app.Config) error {
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

// Session returns the open workspace, or an error saying that none is open. Every other service calls it at each
// request and keeps no session of its own. It is not bound to the frontend.
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
		Language: session.Language, Dumps: session.Schema.Sources[game.SourceDumps], ScannedAt: install.ScannedAt,
	}
}

// Launch starts the game with its debug flag. When the game exits, the game:exited event says whether it wrote
// fresher dumps.
func (wsp *Service) Launch() error {
	session, err := wsp.Session()
	if err != nil {
		return err
	}
	_, install, row, err := findRecords(wsp.Config.Config(), session.ID)
	if err != nil {
		return err
	}
	program, args, ok := game.ResolveProgram(row.ID, install.Path)
	if !ok {
		return fmt.Errorf("%s cannot be started on this system", row.Name)
	}
	command := exec.Command(program, args...)
	command.Dir = filepath.Dir(program)
	if err := command.Start(); err != nil {
		return err
	}
	before := session.Schema.Sources[game.SourceDumps].WrittenAt
	go func() {
		_ = command.Wait()
		dumps, err := schema.ReadDumps(game.FindDumpDirs(row.ID, install.Path))
		application.Get().Event.Emit(EventGameExited, err == nil && dumps.WrittenAt.After(before))
	}()
	return nil
}

// Read returns a file's text and where the file sits in the workspace.
func (wsp *Service) Read(path string) (FileText, error) {
	session, err := wsp.Session()
	if err != nil {
		return FileText{}, err
	}
	text, err := files.ReadText(path)
	return FileText{Place: session.Locate(path), Text: text}, err
}

// Write writes a file's text in its encoding, after making the folder the file sits in. The write is atomic. The
// watcher sends fs:changed once the index has the new text.
func (wsp *Service) Write(path string, text files.Text) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return files.WriteText(path, text)
}

// findWritable returns the open session if each of the given paths lies in one of the workspace's mods, whose
// folders the watcher reports. It returns an error for a path outside the workspace, for one of the game's own
// files, or when no workspace is open.
func (wsp *Service) findWritable(paths ...string) (*Session, error) {
	session, err := wsp.Session()
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		switch place := session.Locate(path); {
		case place.Root == "":
			return nil, fmt.Errorf("%s is outside the workspace", path)
		case place.ReadOnly:
			return nil, errors.New("the game's own files are not changed")
		}
	}
	return session, nil
}

// ListFolder lists a folder's entries, folders first. Binary files are left out when the setting hides them.
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
	children := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (Child, bool) {
		place := session.Locate(filepath.Join(dir, entry.Name()))
		binary := place.Format == files.FormatBinary || place.Format == files.FormatImage
		return Child{Name: entry.Name(), Dir: entry.IsDir(), Place: place}, entry.IsDir() || hide != "true" || !binary
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

// ListRoots returns the explorer's top-level folders: the mods in load order, then the game's.
func (wsp *Service) ListRoots() []files.Root {
	session, err := wsp.Session()
	if err != nil {
		return nil
	}
	return session.ListRoots()
}

// Create makes a new file, or a folder when the flag is set. The watcher sends fs:changed for it.
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

// Rename moves a file or a folder. The watcher sends fs:changed for both paths.
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

// Locate returns where a file sits in the workspace.
func (wsp *Service) Locate(path string) Place {
	session, err := wsp.Session()
	if err != nil {
		return Place{}
	}
	return session.Locate(path)
}

// ServeHTTP serves a file's bytes by its path under the service's route. An image the browser cannot show is
// decoded to PNG first. A path outside the workspace's roots is refused.
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
