// workspace.go is the workspace service: the games, installs and workspaces the app records, the one open session,
// and the reading, writing and moving of the files in the workspace's mods.

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
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
)

// A FileChange is a file of the open workspace that changed or was deleted on disk. The fs:changed event carries it.
type FileChange struct {
	Path       string `json:"path"`
	Deleted    bool   `json:"deleted"`
	Descriptor bool   `json:"descriptor"` // the file is a mod's descriptor
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

// A Workspace is one recorded workspace as the pages list it: its record, its install, its game and its mods in load
// order. Each mod's descriptor is read from disk when the list is made.
type Workspace struct {
	Record  app.Workspace `json:"record"`
	Install app.Install   `json:"install"` // empty if the install is not recorded or its game not supported
	Game    *game.Game    `json:"game"`    // nil if the install is not recorded or its game not supported
	Mods    []Mod         `json:"mods"`    // none if the install is not recorded or its game not supported
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
func (wsp *Service) ListGames() []game.Game { return game.ListGames() }

// DescribePrecedence returns, in plain words, which copy the game with the given id keeps when several definitions of a
// database share a name. The folder is the database's folder in the game's folder, with its stage folder in front where
// the game has stages. It returns "" for a game this build does not support.
func (wsp *Service) DescribePrecedence(gameID, database, folder string) string {
	row := game.LookupGame(gameID)
	if row == nil {
		return ""
	}
	_, folder = row.SplitStage(folder)
	return row.LookupPrecedence(database, folder).Describe()
}

// ListInstalls returns every recorded install of the given game.
func (wsp *Service) ListInstalls(gameID string) []app.Install {
	return lo.Filter(wsp.Config.Config().Installs, func(install app.Install, _ int) bool { return install.Game == gameID })
}

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

// DetectInstalls finds a game's installs on this machine. It finds none for a game this build does not support.
func (wsp *Service) DetectInstalls(gameID string) []game.DetectedInstall {
	row := game.LookupGame(gameID)
	if row == nil {
		return nil
	}
	return row.FindInstalls()
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
		Version: cmp.Or(version, row.LoadVersion(path)),
	}
	err := wsp.Config.Update("installs", func(records *app.Config) error {
		records.Installs = append(records.Installs, install)
		return nil
	})
	return install, err
}

// UpdateInstall moves an install to another folder and reads its version again. A workspace open on the install
// opens again. It returns an error if the install is not recorded, its game is not supported or the folder does not
// exist.
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
		row := game.LookupGame(install.Game)
		if row == nil {
			return fmt.Errorf("the game %s is not supported", install.Game)
		}
		install.Path = path
		install.Version = resolveVersion(row, *install)
		return nil
	})
	if err == nil {
		wsp.reopen(func(record app.Workspace) bool { return record.Install == id })
	}
	return err
}

// ListLanguages returns an install's localization languages, each with its name in English. It returns an error if the
// install is not recorded.
func (wsp *Service) ListLanguages(installID string) ([]loc.Language, error) {
	install, row, err := findInstall(wsp.Config.Config(), installID)
	if err != nil {
		return nil, err
	}
	rel, _ := findLocalizationFolder(row)
	entries, _ := os.ReadDir(filepath.Join(install.Path, game.ScriptRoot, filepath.FromSlash(rel)))
	languages := []loc.Language{}
	for _, entry := range entries {
		if entry.IsDir() {
			languages = append(languages, loc.Language{Folder: entry.Name(), Label: loc.DescribeLanguage(entry.Name())})
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
// settings stay. It returns an error if the records cannot be written, and one for each cache it cannot delete.
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
	return removeEntries(wsp.Config.Dirs.Cache, "")
}

// ListWorkspaces returns every recorded workspace with its install, its game and its mods, each mod's descriptor read
// from disk.
func (wsp *Service) ListWorkspaces() []Workspace {
	records := wsp.Config.Config()
	return lo.Map(records.Workspaces, func(record app.Workspace, _ int) Workspace {
		install, row, _ := findInstall(records, record.Install)
		workspace := Workspace{Record: record, Install: install, Game: row}
		if row != nil {
			workspace.Mods = lo.Map(record.Mods, func(mod app.Mod, _ int) Mod { return readMod(row, mod) })
		}
		return workspace
	})
}

// CreateWorkspace records a workspace of the given install, with the given name and localization language, under an
// id of its own, and returns it. It has no mods yet.
func (wsp *Service) CreateWorkspace(name, install, language string) (app.Workspace, error) {
	record := app.Workspace{
		ID: rand.Text(), Name: name, Install: install, Language: language, Mods: []app.Mod{}, IgnoredPairs: [][2]string{},
	}
	err := wsp.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = append(records.Workspaces, record)
		return nil
	})
	return record, err
}

// UpdateWorkspace replaces a workspace's record, all but its mods, its settings and its ignored pairs. It returns an
// error if the workspace is not recorded.
func (wsp *Service) UpdateWorkspace(ws app.Workspace) error {
	return wsp.updateWorkspace(ws.ID, func(record *app.Workspace) error {
		ws.Mods, ws.Settings, ws.IgnoredPairs = record.Mods, record.Settings, record.IgnoredPairs
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
	_, install, row, err := findRecords(wsp.Config.Config(), session.ID)
	if err != nil {
		return err
	}
	stat, err := os.Stat(filepath.Join(session.caches, indexFile))
	if err != nil {
		return err
	}
	version, scannedAt := resolveVersion(row, install), stat.ModTime()
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
	_, install, _, _ := findRecords(wsp.Config.Config(), session.ID)
	return State{
		Open: true, ID: session.ID, Game: session.Game, Install: install, InstallFound: files.IsFolder(install.Path),
		UpdatedFrom: session.updatedFrom, Dumps: session.Schema.Dumps,
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
	return lo.Find(wsp.ListMods(), func(mod Mod) bool { return mod.ID == modID })
}

// ResolveFolder returns the folder a shell or an agent starts in, and the id of the mod it is the folder of. It is
// the folder of the mod that holds the file at the given path, else of the workspace's first mod, a mod whose folder
// is missing passed over. With neither it is the home folder, and the id is "".
//
//wails:ignore
func (wsp *Service) ResolveFolder(path string) (string, string) {
	mods := wsp.ListMods()
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
	program, args, ok := session.Game.ResolveProgram(session.InstallPath)
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
		dumpsAt, err := schema.FindDumpsAt(session.Game.FindDumpDirs(session.InstallPath))
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
	if _, err := wsp.FindWritable(path); err != nil {
		return err
	}
	return files.WriteText(path, text)
}

// WriteBytes writes a file's bytes, making its folder if it is missing. The watcher sends fs:changed once the index
// has the file. It returns an error for a file outside the workspace's mods.
//
//wails:ignore
func (wsp *Service) WriteBytes(path string, bytes []byte) error {
	if _, err := wsp.FindWritable(path); err != nil {
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

// FindWritable returns the open session if every given file or folder is in one of its mods. It returns an error if no
// workspace is open, or if one is outside the mods, among the game's own files or in a Workshop mod.
//
//wails:ignore
func (wsp *Service) FindWritable(paths ...string) (*Session, error) {
	session, err := wsp.Session()
	if err != nil {
		return nil, err
	}
	mods := session.Mods.Load()
	for _, path := range paths {
		switch place := session.Locate(mods, path); {
		case place.Root == "":
			return nil, fmt.Errorf("%s is outside the workspace", path)
		case place.Origin == game.OriginVanilla:
			return nil, errors.New("the game's own files are not changed")
		case place.ReadOnly:
			return nil, fmt.Errorf("%s is Steam's copy of a Workshop mod; Steam replaces it when its author updates it",
				session.DescribeMod(place.Origin))
		}
	}
	return session, nil
}

// ListFolder returns the places of a folder's entries, folders first. Binary files are left out unless the workspace
// shows them. It returns an error if no workspace is open or the folder cannot be read.
func (wsp *Service) ListFolder(dir string) ([]Place, error) {
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
	places := lo.FilterMap(entries, func(entry os.DirEntry, _ int) (Place, bool) {
		place := session.Locate(mods, filepath.Join(dir, entry.Name()))
		place.IsDir = entry.IsDir()
		return place, place.IsDir || record.ShowBinaries || place.Format.IsText()
	})
	slices.SortStableFunc(places, func(a, b Place) int {
		switch {
		case a.IsDir == b.IsDir:
			return 0
		case a.IsDir:
			return -1
		}
		return 1
	})
	return places, nil
}

// Create makes an empty file or a folder, and the folders it goes in that are not there yet. The watcher sends
// fs:changed for it. It returns an error if it exists already or is outside the workspace's mods.
func (wsp *Service) Create(path string, dir bool) error {
	if _, err := wsp.FindWritable(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
func (wsp *Service) Rename(source, target string) error {
	session, err := wsp.FindWritable(source, target)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s exists already", target)
	}
	return session.watcher.Move(source, func() error { return os.Rename(source, target) })
}

// Delete moves a file or a folder to PMT's trash and returns where it went. Deleting a mod's folder takes the mod
// out of the workspace too. It returns an error if it is outside the workspace's mods.
func (wsp *Service) Delete(path string) (string, error) {
	session, err := wsp.FindWritable(path)
	if err != nil {
		return "", err
	}
	trashed := filepath.Join(wsp.Config.Dirs.Trash, strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(path))
	if err := session.watcher.Move(path, func() error { return move(path, trashed) }); err != nil {
		return "", err
	}
	if mod, ok := lo.Find(*session.Order.Load(), func(mod Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(path) }); ok {
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
	if _, err := wsp.FindWritable(path); err != nil {
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
	place.IsDir = files.IsFolder(path)
	return place
}

// ServeHTTP serves the file a request names, DDS and TGA as PNG. A file outside the recorded installs and mods is
// refused.
func (wsp *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	path := query.Get("path")
	records := wsp.Config.Config()
	dirs := lo.Map(records.Installs, func(install app.Install, _ int) string { return install.Path })
	for _, record := range records.Workspaces {
		dirs = append(dirs, lo.Map(record.Mods, func(mod app.Mod, _ int) string { return mod.Dir })...)
	}
	isRecorded := lo.ContainsBy(dirs, func(dir string) bool {
		_, ok := files.ToRel(dir, path)
		return ok
	})
	if !isRecorded {
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
	rowFrames := bounds.Dx() / size.X
	position := min(max(frame, 1), rowFrames*(bounds.Dy()/size.Y)) - 1
	corner := bounds.Min.Add(image.Pt(position%rowFrames*size.X, position/rowFrames*size.Y))
	return sheet.SubImage(image.Rectangle{Min: corner, Max: corner.Add(size)})
}
