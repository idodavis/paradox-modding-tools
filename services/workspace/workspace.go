package workspace

import (
	"cmp"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	Open         bool               `json:"open"`
	ID           string             `json:"id"`
	Game         *game.Game         `json:"game"`
	Install      app.Install        `json:"install"`
	InstallFound bool               `json:"installFound"` // the install's folder exists
	UpdatedFrom  string             `json:"updatedFrom"`  // the install's version before the open found a new one
	Mods         []Mod              `json:"mods"`
	Language     string             `json:"language"`
	Dumps        schema.SourceState `json:"dumps"`
}

// A Scanning is the data of the install:scanning event: a stage of the scan and the percent of its files done.
type Scanning struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
}

// A FileText is a file's text in its encoding, together with where the file sits in the workspace.
type FileText struct {
	Place Place      `json:"place"`
	Text  files.Text `json:"text"`
	// Draft is the text the editor last sent for the file where it differs from the disk's, such as edits that a
	// reloaded page has not saved; "" where it holds none.
	Draft string `json:"draft"`
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
	EventInstallScanning = "install:scanning" // an install's scan reports a stage; the data is a Scanning
	EventInstallScanned  = "install:scanned"  // an install's scan finished; the data is the install's id
	EventFileChanged     = "fs:changed"       // a file of the open workspace changed on disk; the data is a FileChange
	EventGameExited      = "game:exited"      // the launched game exited; the data is true when the dumps are now fresher
)

// slotCount is how many color slots a mod can have.
const slotCount = 16

// init registers the workspace events with the application.
func init() {
	application.RegisterEvent[string](EventOpened)
	application.RegisterEvent[string](EventFailed)
	application.RegisterEvent[struct{}](EventClosed)
	application.RegisterEvent[Scanning](EventInstallScanning)
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

// UpdateInstall moves an install's record to another folder and reads its version again. If the open workspace uses
// the install, that workspace is opened again at once. It returns an error if the install is not recorded or the
// folder does not exist.
func (wsp *Service) UpdateInstall(id, path string) error {
	if !files.IsFolder(path) {
		return fmt.Errorf("%s is not a folder", path)
	}
	err := wsp.Config.Update("installs", func(records *app.Config) error {
		position := slices.IndexFunc(records.Installs, func(install app.Install) bool { return install.ID == id })
		if position < 0 {
			return fmt.Errorf("the install %s is not recorded", id)
		}
		install := &records.Installs[position]
		install.Path, install.Version = path, game.LoadGameVersion(install.Game, path)
		return nil
	})
	if err == nil {
		wsp.reopen(func(record app.Workspace) bool { return record.Install == id })
	}
	return err
}

// ListLanguages returns the localization languages of an install: the folders its localization folder holds. It
// returns an error if the install is not recorded.
func (wsp *Service) ListLanguages(installID string) ([]string, error) {
	install, row, err := findInstall(wsp.Config.Config(), installID)
	if err != nil {
		return nil, err
	}
	rel, _ := findLocalizationFolder(row)
	entries, _ := os.ReadDir(filepath.Join(install.Path, game.ScriptRoot, filepath.FromSlash(rel)))
	languages := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			languages = append(languages, entry.Name())
		}
	}
	return languages, nil
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
	if err := os.RemoveAll(folder); err != nil {
		return err
	}
	wsp.reopen(func(record app.Workspace) bool { return record.Install == id })
	return nil
}

// reopen opens the open workspace again if its record passes the given test.
func (wsp *Service) reopen(matches func(record app.Workspace) bool) {
	wsp.mu.Lock()
	workspaceID := ""
	if wsp.session != nil {
		if record, _, _, err := findRecords(wsp.Config.Config(), wsp.session.ID); err == nil && matches(record) {
			workspaceID = record.ID
		}
	}
	wsp.mu.Unlock()
	if workspaceID != "" {
		wsp.Close()
		wsp.Open(workspaceID)
	}
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
// When the change alters what the open workspace was opened from, that workspace is opened again. It returns an error
// if the workspace is not recorded or the change fails.
func (wsp *Service) updateWorkspace(workspaceID string, change func(record *app.Workspace) error) error {
	reopens := false
	err := wsp.Config.Update("workspaces", func(records *app.Config) error {
		position := slices.IndexFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
		if position < 0 {
			return fmt.Errorf("the workspace %s is not recorded", workspaceID)
		}
		key := makeOpenKey(records.Workspaces[position])
		if err := change(&records.Workspaces[position]); err != nil {
			return err
		}
		reopens = makeOpenKey(records.Workspaces[position]) != key
		return nil
	})
	if err == nil && reopens {
		wsp.reopen(func(record app.Workspace) bool { return record.ID == workspaceID })
	}
	return err
}

// makeOpenKey returns, as one string, what opening a workspace reads from its record: its install, its language, and
// its mods' ids and folders in load order.
func makeOpenKey(record app.Workspace) string {
	parts := []string{record.Install, record.Language}
	for _, mod := range record.Mods {
		parts = append(parts, mod.ID, mod.Dir)
	}
	return strings.Join(parts, "\x00")
}

// DeleteWorkspace removes the record of the workspace with the given id, closing it first if it is open. The mod
// folders on disk stay.
func (wsp *Service) DeleteWorkspace(id string) error {
	wsp.mu.Lock()
	if wsp.session != nil && wsp.session.ID == id {
		wsp.closeSession()
	}
	wsp.mu.Unlock()
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
	return readMod(row, record), nil
}

// ListMods returns the mods of every recorded workspace by the workspace's id, each in load order with its
// descriptor, whether or not the workspace is open. A workspace whose install is not recorded is left out.
func (wsp *Service) ListMods() map[string][]Mod {
	records := wsp.Config.Config()
	mods := map[string][]Mod{}
	for _, record := range records.Workspaces {
		if _, row, err := findInstall(records, record.Install); err == nil {
			mods[record.ID] = lo.Map(record.Mods, func(mod app.Mod, _ int) Mod { return readMod(row, mod) })
		}
	}
	return mods
}

// resolveSlot returns the color slot for a new mod: the first that no mod has. If every slot is in use, it is the slot
// at the mods' count, wrapping around.
func resolveSlot(mods []app.Mod) string {
	for position := range slotCount {
		slot := makeSlot(position)
		if !lo.ContainsBy(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}
	return makeSlot(len(mods))
}

// makeSlot returns the color slot of the given position in a load order, wrapping around after the last slot.
func makeSlot(position int) string {
	return fmt.Sprintf("o%d", position%slotCount+1)
}

// CreateMod makes a new mod in a folder, the game's mod folder when it is "", with a localization file in a language,
// the base language when it is "", and adds it to a workspace. It returns an error if the workspace is not recorded
// or the mod's folder exists already.
func (wsp *Service) CreateMod(workspaceID, dir, name, language string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	folder, err := createModFolder(row, resolveModDir(row, install, dir), name, game.LoadGameVersion(row.ID, install.Path))
	if err != nil {
		return Mod{}, err
	}
	if err := createLocalization(row, folder, name, language); err != nil {
		return Mod{}, err
	}
	return wsp.AddMod(workspaceID, folder)
}

// ResolveModFolder returns the folder CreateMod would make, or with no name the folder it would make it in.
func (wsp *Service) ResolveModFolder(workspaceID, dir, name string) (string, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return "", err
	}
	return resolveModFolder(resolveModDir(row, install, dir), name), nil
}

// resolveModDir returns the folder a new mod is made in: the given one, else the game's mod folder.
func resolveModDir(row *game.Game, install app.Install, dir string) string {
	return cmp.Or(dir, filepath.Join(game.FindUserDataDir(row.ID, install.Path), "mod"))
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

// ResetColors gives each mod of a workspace the color slot of its place in the load order and clears the color of the
// game.
func (wsp *Service) ResetColors(workspaceID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.GameColor = ""
		for position := range workspace.Mods {
			workspace.Mods[position].Color = makeSlot(position)
		}
		return nil
	})
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
			func(stage string, percent int) {
				pmt.Event.Emit(EventInstallScanning, Scanning{Stage: stage, Percent: percent})
			},
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
	if install.Version != version {
		session.updatedFrom = install.Version
	}
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
		Open: true, ID: session.ID, Game: session.Game, Install: install, InstallFound: files.IsFolder(install.Path),
		UpdatedFrom: session.updatedFrom, Mods: mods, Language: session.Language, Dumps: session.Schema.Dumps,
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

// Read returns the text of the file with the given path, where the file sits in the workspace, and the editor's draft
// of it where one differs from the disk. It returns an error if no workspace is open or the file cannot be read.
func (wsp *Service) Read(path string) (FileText, error) {
	session, err := wsp.Session()
	if err != nil {
		return FileText{}, err
	}
	text, err := files.ReadText(path)
	draft, open := session.LookupBuffer(path)
	if !open || draft == text.Text {
		draft = ""
	}
	return FileText{Place: session.Locate(session.Mods.Load(), path), Text: text, Draft: draft}, err
}

// ReadHeader returns an image file's header. It returns an error if the file is not an image in the open workspace.
func (wsp *Service) ReadHeader(path string) (texture.Header, error) {
	session, err := wsp.Session()
	if err != nil {
		return texture.Header{}, err
	}
	if _, ok := session.Mods.Load().FindRoot(path); !ok {
		return texture.Header{}, fmt.Errorf("%s is outside the workspace", path)
	}
	return texture.ReadHeader(path)
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

// ListFolder lists the entries of a folder with the place of each, folders first. Binary files are left out unless the
// open workspace shows them. It returns an error if no workspace is open or the folder cannot be read.
func (wsp *Service) ListFolder(dir string) ([]Child, error) {
	session, err := wsp.Session()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	record, _, _, _ := findRecords(wsp.Config.Config(), session.ID)
	mods := session.Mods.Load()
	children := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (Child, bool) {
		place := session.Locate(mods, filepath.Join(dir, entry.Name()))
		return Child{Name: entry.Name(), Dir: entry.IsDir(), Place: place}, entry.IsDir() || record.ShowBinaries || place.Format.IsText()
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

// Delete moves a file or a folder into PMT's trash and returns where it went, for Restore. Deleting a mod's root
// folder removes the mod from the workspace too.
func (wsp *Service) Delete(path string) (string, error) {
	session, err := wsp.findWritable(path)
	if err != nil {
		return "", err
	}
	trashed := filepath.Join(wsp.Config.Dirs.Trash, strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(path))
	if err := move(path, trashed); err != nil {
		return "", err
	}
	if mod, ok := lo.Find(session.Order, func(mod Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(path) }); ok {
		return trashed, wsp.RemoveMod(session.ID, mod.ID)
	}
	return trashed, nil
}

// Restore moves a file or a folder back from PMT's trash. It returns an error if the trashed path is not in the trash,
// or the path it goes back to is outside the mods or exists already.
func (wsp *Service) Restore(trashed, path string) error {
	if !files.IsUnder(wsp.Config.Dirs.Trash, trashed) {
		return fmt.Errorf("%s is not in the trash", trashed)
	}
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s exists already", path)
	}
	return move(trashed, path)
}

// move moves a file or a folder, making the folder it goes into. Across drives, where a rename fails, it copies and
// then deletes the original.
func move(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	err := os.Rename(source, target)
	if err == nil || !errors.Is(err, syscall.EXDEV) && filepath.VolumeName(source) == filepath.VolumeName(target) {
		return err
	}
	if files.IsFolder(source) {
		err = os.CopyFS(target, os.DirFS(source))
	} else {
		var content []byte
		if content, err = os.ReadFile(source); err == nil {
			err = os.WriteFile(target, content, 0o644)
		}
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(source)
}

// Locate returns the place of a file in the open workspace. It returns an empty place if none is open.
func (wsp *Service) Locate(path string) Place {
	session, err := wsp.Session()
	if err != nil {
		return Place{}
	}
	return session.Locate(session.Mods.Load(), path)
}

// ServeHTTP serves the file that the path of a request names, whether or not a workspace is open. A DDS or TGA image
// is decoded to PNG first, since the browser cannot show it, and so is an image cut to one frame of a sprite sheet or
// one of a DDS's smaller mip levels, which the level in the request names. A path outside every recorded install and
// mod folder is refused.
func (wsp *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	path := query.Get("path")
	if !isRecorded(wsp.Config.Config(), path) {
		http.Error(w, "the path is outside every recorded install and mod", http.StatusForbidden)
		return
	}
	isCut := query.Has("framesize") || query.Has("framegrid") || query.Has("level")
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".dds" && ext != ".tga" && !isCut {
		http.ServeFile(w, r, path)
		return
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	level, _ := strconv.Atoi(query.Get("level"))
	picture, err := texture.Decode(bytes, level)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, cutFrame(picture, query))
}

// cutFrame returns the one frame of a sprite sheet that a query names, counted from 1 across and then down. A frame's
// size is the framesize times the density, in the image's pixels, or the image cut into the framegrid's columns and
// rows. It returns the whole image if the query names no frame size or one larger than the image. A frame past the end
// of the sheet is its last.
func cutFrame(picture image.Image, query url.Values) image.Image {
	bounds := picture.Bounds()
	var width, height, columns, rows float64
	if _, err := fmt.Sscan(query.Get("framesize"), &width, &height); err == nil {
		density, err := strconv.ParseFloat(query.Get("density"), 64)
		if err != nil || density <= 0 {
			density = 1
		}
		width, height = width*density, height*density
	} else if _, err := fmt.Sscan(query.Get("framegrid"), &columns, &rows); err == nil && columns >= 1 && rows >= 1 {
		width, height = float64(bounds.Dx())/columns, float64(bounds.Dy())/rows
	}
	size := image.Pt(int(width), int(height))
	sheet, ok := picture.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok || size.X < 1 || size.Y < 1 || size.X > bounds.Dx() || size.Y > bounds.Dy() {
		return picture
	}
	frame, _ := strconv.Atoi(query.Get("frame"))
	across := bounds.Dx() / size.X
	position := min(max(frame, 1), across*(bounds.Dy()/size.Y)) - 1
	corner := bounds.Min.Add(image.Pt(position%across*size.X, position/across*size.Y))
	return sheet.SubImage(image.Rectangle{Min: corner, Max: corner.Add(size)})
}

// isRecorded reports whether a path is inside the folder of a recorded install or of a mod that a workspace records.
func isRecorded(records app.Config, path string) bool {
	dirs := lo.Map(records.Installs, func(install app.Install, _ int) string { return install.Path })
	for _, record := range records.Workspaces {
		dirs = append(dirs, lo.Map(record.Mods, func(mod app.Mod, _ int) string { return mod.Dir })...)
	}
	return lo.ContainsBy(dirs, func(dir string) bool {
		_, inside := files.ToRel(dir, path)
		return inside
	})
}
