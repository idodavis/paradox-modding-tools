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

// A FileChange is a file of the open workspace that changed or was deleted on disk. The fs:changed event carries it.
type FileChange struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// Service is the Wails service behind installs, workspaces and their files. It holds the one open workspace and
// launches the game.
type Service struct {
	Config  *app.ConfigFile // config.json, for the records and the folders
	Build   string          // the app's version, which names the caches
	mu      sync.Mutex      // held for the whole of an open
	session *Session        // nil while no workspace is open
}

// A State is a summary of the open workspace. It is empty while no workspace is open.
type State struct {
	Open         bool               `json:"open"`
	ID           string             `json:"id"`
	Game         *game.Game         `json:"game"`
	Install      app.Install        `json:"install"`
	InstallFound bool               `json:"installFound"` // the install's folder exists
	UpdatedFrom  string             `json:"updatedFrom"`  // the recorded version, if opening found a different one
	Mods         []Mod              `json:"mods"`
	Language     string             `json:"language"`
	Dumps        schema.SourceState `json:"dumps"`
	Effects      int                `json:"effects"`  // how many effects the schema declares
	Triggers     int                `json:"triggers"` // how many triggers the schema declares
	Scopes       int                `json:"scopes"`   // how many scope types the schema declares
}

// A Scanning is how far an install's scan has come. The install:scanning event carries it.
type Scanning struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
}

// A FileText is a file's text with its place in the workspace.
type FileText struct {
	Place Place      `json:"place"`
	Text  files.Text `json:"text"`
	// Draft is the editor's unsaved text of the file. It is "" if there is none or it matches the disk.
	Draft  string `json:"draft"`
	Binary bool   `json:"binary"` // the file is not text, so it has no text
}

// A Child is one entry in a folder listing.
type Child struct {
	Name  string `json:"name"`
	Place Place  `json:"place"`
}

// The events the workspace service sends.
const (
	EventOpened          = "workspace:opened" // a workspace is open with its index built. The data is its id
	EventFailed          = "workspace:failed" // a workspace could not be opened. The data is the error's text
	EventClosed          = "workspace:closed" // the open workspace closed
	EventInstallScanning = "install:scanning" // an install's scan reports a stage. The data is a Scanning
	EventInstallScanned  = "install:scanned"  // an install's scan finished. The data is the install's id
	EventFileChanged     = "fs:changed"       // a file of the open workspace changed on disk. The data is a FileChange
	EventGameExited      = "game:exited"      // the launched game exited. The data is true if the dumps are fresher
)

// slotCount is how many color slots there are for mods.
const slotCount = 16

// init registers the workspace events.
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

// ListReadyInstalls returns the ids of the installs whose caches are up to date, so opening them scans nothing.
func (wsp *Service) ListReadyInstalls() []string {
	return lo.FilterMap(wsp.Config.Config().Installs, func(install app.Install, _ int) (string, bool) {
		row := game.LookupGame(install.Game)
		if row == nil {
			return "", false
		}
		folder, err := resolveCaches(wsp.Config.Dirs.Cache, row, install, wsp.Build)
		if err != nil {
			return "", false
		}
		_, schemaErr := os.Stat(filepath.Join(folder, schemaFile))
		_, indexErr := os.Stat(filepath.Join(folder, indexFile))
		return install.ID, schemaErr == nil && indexErr == nil
	})
}

// DetectInstalls finds a game's installs on this machine.
func (wsp *Service) DetectInstalls(gameID string) []game.DetectedInstall {
	return game.FindInstalls(gameID)
}

// AddInstall records an install of a game and returns it. With no name it is named after the game. With no version
// it gets the version its files say. It returns an error if the game is not supported.
func (wsp *Service) AddInstall(gameID, path, name, version string) (app.Install, error) {
	row := game.LookupGame(gameID)
	if row == nil {
		return app.Install{}, fmt.Errorf("the game %s is not supported", gameID)
	}
	install := app.Install{
		ID: rand.Text(), Game: gameID, Name: cmp.Or(name, row.Name), Path: path,
		Version: cmp.Or(version, game.LoadGameVersion(gameID, path)),
	}
	err := wsp.Config.Update("installs", func(records *app.Config) error {
		records.Installs = append(records.Installs, install)
		return nil
	})
	return install, err
}

// UpdateInstall moves an install to another folder and reads its version again. A workspace open on the install
// opens again. It returns an error if the install is not recorded or the folder does not exist.
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
		install.Path = path
		install.Version = resolveVersion(*install)
		return nil
	})
	if err == nil {
		wsp.reopen(func(record app.Workspace) bool { return record.Install == id })
	}
	return err
}

// ListLanguages returns an install's localization languages. It returns an error if the install is not recorded.
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

// DeleteInstall removes an install's record. It returns an error while a workspace uses the install.
func (wsp *Service) DeleteInstall(id string) error {
	return wsp.Config.Update("installs", func(records *app.Config) error {
		if lo.ContainsBy(records.Workspaces, func(record app.Workspace) bool { return record.Install == id }) {
			return fmt.Errorf("a workspace uses the install %s", id)
		}
		records.Installs = slices.DeleteFunc(records.Installs, func(install app.Install) bool { return install.ID == id })
		return nil
	})
}

// RescanInstall deletes an install's caches, so its next open scans it again. A workspace open on the install opens
// again. It returns an error if the install is not recorded or its caches cannot be deleted.
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

// reopen opens the open workspace again if its record passes a test.
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

// ResetData closes the open workspace and removes every workspace, install record and cache. Mods, game files and
// settings stay. It returns an error if the records cannot be written or a cache cannot be deleted.
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

// CreateWorkspace records a workspace under an id of its own and returns it.
func (wsp *Service) CreateWorkspace(ws app.Workspace) (app.Workspace, error) {
	ws.ID = rand.Text()
	err := wsp.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = append(records.Workspaces, ws)
		return nil
	})
	return ws, err
}

// UpdateWorkspace replaces a workspace's record. It returns an error if the workspace is not recorded.
func (wsp *Service) UpdateWorkspace(ws app.Workspace) error {
	return wsp.updateWorkspace(ws.ID, func(record *app.Workspace) error {
		*record = ws
		return nil
	})
}

// updateWorkspace applies a change to a workspace's record. An open workspace opens again if the change alters what
// it opens from. It returns an error if the workspace is not recorded or the change fails.
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

// makeOpenKey returns, as one string, the parts of a workspace's record that opening it reads.
func makeOpenKey(record app.Workspace) string {
	parts := []string{record.Install, record.Language}
	for _, mod := range record.Mods {
		parts = append(parts, mod.ID, mod.Dir)
	}
	return strings.Join(parts, "\x00")
}

// DeleteWorkspace removes a workspace's record, closing the workspace first if it is open. Its mod folders stay.
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

// AddMod adds a mod folder to a workspace and gives the mod a color slot. It returns an error if the workspace is not
// recorded, there is no such folder, or the workspace has the mod already.
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

// ListMods returns the mods of every workspace in load order, by the workspace's id. A workspace whose install is
// not recorded is left out.
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

// resolveSlot returns the first color slot that no mod has. If all are taken, it returns the slot of the next place
// in the load order.
func resolveSlot(mods []app.Mod) string {
	for position := range slotCount {
		slot := makeSlot(position)
		if !lo.ContainsBy(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}
	return makeSlot(len(mods))
}

// makeSlot returns the color slot of a place in the load order, wrapping around after the last slot.
func makeSlot(position int) string {
	return fmt.Sprintf("o%d", position%slotCount+1)
}

// CreateMod makes a mod with a localization file and adds it to a workspace. It returns an error if the workspace is
// not recorded or the folder exists.
func (wsp *Service) CreateMod(workspaceID, dir, name, language string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	folder, err := createModFolder(row, resolveModDir(row, install, dir), name, resolveVersion(install))
	if err != nil {
		return Mod{}, err
	}
	if file, ok := FindLocalizationFile(row, folder, name, language); ok {
		if err := createLocalization(file); err != nil {
			return Mod{}, err
		}
	}
	return wsp.AddMod(workspaceID, folder)
}

// ResolveModFolder returns the folder CreateMod would make. With no name, it returns the folder the mod goes in.
func (wsp *Service) ResolveModFolder(workspaceID, dir, name string) (string, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return "", err
	}
	return resolveModFolder(resolveModDir(row, install, dir), name), nil
}

// resolveModDir returns the folder a mod is made in. With no folder, it is the game's mod folder.
func resolveModDir(row *game.Game, install app.Install, dir string) string {
	return cmp.Or(dir, filepath.Join(game.FindUserDataDir(row.ID, install.Path), "mod"))
}

// RemoveMod takes a mod out of a workspace. Its folder stays.
func (wsp *Service) RemoveMod(workspaceID, modID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.Mods = slices.DeleteFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })
		return nil
	})
}

// ReorderMods sets a workspace's load order. It returns an error unless the order names each of its mods once.
func (wsp *Service) ReorderMods(workspaceID string, modIDs []string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		ordered, err := reorderMods(workspace.Mods, modIDs, func(mod app.Mod) string { return mod.ID })
		if err == nil {
			workspace.Mods = ordered
		}
		return err
	})
}

// reorderMods returns mods in the order of a list of ids. It returns an error unless the list names each mod once.
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

// ResetColors gives a workspace's mods the color slots of their places in the load order and clears the game's color.
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

// Open opens a workspace in the background, closing the one before. It sends workspace:opened or workspace:failed at
// the end.
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

// updateInstall records the version and the scan time of a session's install. It sends install:scanned when the scan
// time changes.
func (wsp *Service) updateInstall(session *Session) error {
	_, install, _, err := findRecords(wsp.Config.Config(), session.ID)
	if err != nil {
		return err
	}
	stat, err := os.Stat(filepath.Join(session.caches, indexFile))
	if err != nil {
		return err
	}
	version, scannedAt := resolveVersion(install), stat.ModTime()
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

// Session returns the open workspace. It returns an error if none is open.
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

// State returns the state of the open workspace. It is empty if none is open.
func (wsp *Service) State() State {
	wsp.mu.Lock()
	defer wsp.mu.Unlock()
	session := wsp.session
	if session == nil {
		return State{}
	}
	// a workspace that is not recorded leaves the install empty
	record, install, _, _ := findRecords(wsp.Config.Config(), session.ID)
	// each mod takes its record from the config, which may have changed since the open
	mods := lo.Map(session.Order, func(mod Mod, _ int) Mod {
		if position := slices.IndexFunc(record.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			mod.Mod = record.Mods[position]
		}
		return mod
	})
	return State{
		Open: true, ID: session.ID, Game: session.Game, Install: install, InstallFound: files.IsFolder(install.Path),
		UpdatedFrom: session.updatedFrom, Mods: mods, Language: session.Language, Dumps: session.Schema.Dumps,
		Effects: session.Effects, Triggers: session.Triggers, Scopes: len(session.Schema.Scopes),
	}
}

// countDeclared returns how many of a schema's commands are declared as one kind, such as effects.
func countDeclared(language *schema.Schema, declaration string) int {
	count := 0
	for _, command := range language.Commands {
		if slices.Contains(command.Declared, declaration) {
			count++
		}
	}
	return count
}

// LookupMod returns a mod of the open workspace. It returns false if none is open or it has no such mod.
//
//wails:ignore
func (wsp *Service) LookupMod(modID string) (Mod, bool) {
	return lo.Find(wsp.State().Mods, func(mod Mod) bool { return mod.ID == modID })
}

// ResolveFolder returns the folder a shell or an agent starts in, and the id of the mod it is the folder of. It is
// the folder of the mod that holds the file at the given path, else of the workspace's first mod, a mod whose folder
// is missing passed over. With neither it is the home folder, and the id is "".
//
//wails:ignore
func (wsp *Service) ResolveFolder(path string) (string, string) {
	mods := wsp.State().Mods
	if path != "" {
		origin := wsp.Locate(path).Origin
		mods = append(lo.Filter(mods, func(mod Mod, _ int) bool { return mod.ID == origin }), mods...)
	}
	if mod, ok := lo.Find(mods, func(mod Mod) bool { return !mod.Broken }); ok {
		return mod.Dir, mod.ID
	}
	home, _ := os.UserHomeDir()
	return home, ""
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

// Read returns a file's text, its place in the workspace and the editor's draft of it. A file that is not text comes
// back as binary, with no text. It returns an error if no workspace is open or the file cannot be read.
func (wsp *Service) Read(path string) (FileText, error) {
	session, err := wsp.Session()
	if err != nil {
		return FileText{}, err
	}
	place := session.Locate(session.Mods.Load(), path)
	text, err := files.ReadText(path)
	if errors.Is(err, files.ErrBinary) {
		return FileText{Place: place, Binary: true}, nil
	}
	draft, open := session.LookupBuffer(path)
	if !open || draft == text.Text {
		draft = ""
	}
	return FileText{Place: place, Text: text, Draft: draft}, err
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

// Write writes a file's text in its encoding, making its folder if it is missing. The watcher sends fs:changed once
// the index has the text. It returns an error for a file outside the workspace's mods.
func (wsp *Service) Write(path string, text files.Text) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	return files.WriteText(path, text)
}

// WriteBytes writes a file's bytes, making its folder if it is missing. The watcher sends fs:changed once the index
// has the file. It returns an error for a file outside the workspace's mods.
//
//wails:ignore
func (wsp *Service) WriteBytes(path string, bytes []byte) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	return files.WriteAtomic(path, bytes)
}

// CreateCopy copies a file of the game or of another mod to the same place in a mod, and returns the copy's path. It
// returns an error if no workspace is open, the mod is not in it or has no folder, the file is under none of its
// roots, or the mod has a file there already.
func (wsp *Service) CreateCopy(path, modID string) (string, error) {
	session, err := wsp.Session()
	if err != nil {
		return "", err
	}
	mod, ok := wsp.LookupMod(modID)
	if !ok || mod.Broken {
		return "", fmt.Errorf("the mod %s is not in the workspace or has no folder", modID)
	}
	place := session.Locate(session.Mods.Load(), path)
	if place.Root == "" {
		return "", fmt.Errorf("%s is outside the workspace", path)
	}
	target := files.Join(mod.Dir, place.Rel)
	if files.IsFile(target) {
		return "", fmt.Errorf("%s has %s already", session.DescribeMod(modID), place.Rel)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return target, wsp.WriteBytes(target, bytes)
}

// findWritable returns the open session if every file or folder is in one of its mods. It returns an error if no
// workspace is open or one is not.
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

// ListFolder lists a folder's entries, folders first. Binary files are left out unless the workspace shows them. It
// returns an error if no workspace is open or the folder cannot be read.
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
		place.IsDir = entry.IsDir()
		return Child{Name: entry.Name(), Place: place}, place.IsDir || record.ShowBinaries || place.Format.IsText()
	})
	slices.SortStableFunc(children, func(a, b Child) int {
		switch {
		case a.Place.IsDir == b.Place.IsDir:
			return 0
		case a.Place.IsDir:
			return -1
		}
		return 1
	})
	return children, nil
}

// ListRoots returns the workspace's root folders in load order, the game's last. It returns none if none is open.
func (wsp *Service) ListRoots() []files.Root {
	session, err := wsp.Session()
	if err != nil {
		return nil
	}
	return session.ListRoots()
}

// Create makes an empty file or a folder. The watcher sends fs:changed for it. It returns an error if it exists
// already or is outside the workspace's mods.
func (wsp *Service) Create(path string, dir bool) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	if dir {
		return os.Mkdir(path, 0o755)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

// Rename moves a file or a folder to a new path. The watcher sends fs:changed for both paths. It returns an error if
// the new path exists already or either path is outside the workspace's mods.
func (wsp *Service) Rename(from, to string) error {
	session, err := wsp.findWritable(from, to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s exists already", to)
	}
	return session.watcher.Move(from, func() error { return os.Rename(from, to) })
}

// Delete moves a file or a folder to PMT's trash and returns where it went. Deleting a mod's folder takes the mod
// out of the workspace too. It returns an error if it is outside the workspace's mods.
func (wsp *Service) Delete(path string) (string, error) {
	session, err := wsp.findWritable(path)
	if err != nil {
		return "", err
	}
	trashed := filepath.Join(wsp.Config.Dirs.Trash, strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(path))
	if err := session.watcher.Move(path, func() error { return move(path, trashed) }); err != nil {
		return "", err
	}
	if mod, ok := lo.Find(session.Order, func(mod Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(path) }); ok {
		return trashed, wsp.RemoveMod(session.ID, mod.ID)
	}
	return trashed, nil
}

// Restore moves a file or a folder back from PMT's trash. It returns an error if it is not in the trash, or the
// target is outside the mods or exists already.
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

// move moves a file or a folder, making the folder it goes into. Across drives it copies, then deletes the original.
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

// Locate returns the place of a file or folder in the open workspace, which says whether it is a folder. It returns
// an empty place if none is open.
func (wsp *Service) Locate(path string) Place {
	session, err := wsp.Session()
	if err != nil {
		return Place{}
	}
	place := session.Locate(session.Mods.Load(), path)
	info, err := os.Stat(path)
	place.IsDir = err == nil && info.IsDir()
	return place
}

// ServeHTTP serves the file a request names, DDS and TGA as PNG. A file outside the recorded installs and mods is
// refused.
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

// cutFrame returns the frame of a sprite sheet that a query names, counted from 1 across and then down. It returns
// the whole image if the query gives no frame size or one larger than the image. A frame past the end is the last.
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

// isRecorded reports whether a file is inside a recorded install or a mod of a recorded workspace.
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
