// Package workspace holds the one open workspace: a game install and the mods
// being edited, in load order. It also keeps the records of installs,
// workspaces and mods, and reads, writes and moves the files in the mods.
//
// Opening a workspace loads the install's schema and index from a cache
// folder, or builds and saves them, then scans the mods on top and watches
// their folders. The cache folder is named for the install's version, its
// dumps and the app's build, so a change to any of them scans again.
//
// It applies these rules:
//   - The game's own files and Steam's copy of a Workshop mod are read only,
//     and a Workshop mod is always an external mod.
//   - A mod is a folder with the game's descriptor file in it. A mod whose
//     folder is missing is broken.
//   - A mod's color is the first of 16 slots that no mod has.
//   - A localization key's text comes in the workspace's language, else in
//     the base language.
//   - The explorer never lists .git, and hides other dot folders and binary
//     files if the settings say so.
//   - The watcher skips dot folders but those of coding agents and the
//     Workshop listing.
//
// # Robustness
//
// It refuses to write outside the mods, to open a workspace or an install
// that is not recorded, and to set a load order that does not name each mod
// once. It reads past a descriptor it cannot read, which leaves the mod with
// an empty one, and past a changed file it cannot scan, which keeps its old
// entries until the next change.
package workspace

import (
	"cmp"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// FileChange is a file of the open workspace that changed or was deleted on
// disk.
type FileChange struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`

	// Descriptor is true for a mod's descriptor file.
	Descriptor bool `json:"descriptor"`
}

// Service is the Wails service behind installs, workspaces and their files.
type Service struct {
	Config *app.ConfigFile

	// Build is the app's version, which names the caches.
	Build string

	// mu is held for the whole of an open. session is nil while none is open.
	mu      sync.Mutex
	session *Session
}

// State is a summary of the open workspace, empty while none is open.
type State struct {
	Open    bool        `json:"open"`
	ID      string      `json:"id"`
	Game    *game.Game  `json:"game"`
	Install app.Install `json:"install"`

	// InstallFound is false if the install's folder is missing.
	InstallFound bool `json:"installFound"`

	// UpdatedFrom is the recorded version, when opening found another.
	UpdatedFrom string           `json:"updatedFrom"`
	Dumps       lang.SourceState `json:"dumps"`

	// Effects, Triggers and Scopes count what the schema declares.
	Effects  int `json:"effects"`
	Triggers int `json:"triggers"`
	Scopes   int `json:"scopes"`
}

// Scanning is how far an install's scan has come.
type Scanning struct {
	Stage string `json:"stage"`

	// Percent is -1 for a stage that reports none.
	Percent int `json:"percent"`
}

// Workspace is one recorded workspace as the pages list it.
type Workspace struct {
	Record app.Workspace `json:"record"`

	// Install, Game and Mods are empty if the install is not recorded or its
	// game is not supported.
	Install app.Install `json:"install"`
	Game    *game.Game  `json:"game"`
	Mods    []Mod       `json:"mods"`
}

// The events the workspace service sends. EventOpened carries the workspace's
// id, EventFailed the error's text, EventInstallScanned the install's id and
// EventGameExited true if the game wrote fresher dumps.
const (
	EventOpened          = "workspace:opened"
	EventFailed          = "workspace:failed"
	EventClosed          = "workspace:closed"
	EventInstallScanning = "install:scanning"
	EventInstallScanned  = "install:scanned"
	EventFileChanged     = "fs:changed"
	EventGameExited      = "game:exited"
)

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
func (s *Service) ServiceShutdown() error {
	s.Close()

	return nil
}

// ListGames returns the supported games, each with whether its example's
// database takes entry modes such as INJECT:.
func (s *Service) ListGames() []game.Game {
	rows := game.ListGames()
	for position := range rows {
		row := &rows[position]
		dialect := lang.LookupDialect(row.ID)
		_, folder := files.SplitStage(row.Example.Folder, row.Stages)
		declaration, _ := dialect.FindDeclaration(folder)
		declaration.Folder = folder
		row.Example.Modes = dialect.LookupPrecedence(declaration, row.Example.Database).Modes != ""
	}

	return rows
}

// ListInstalls returns every recorded install of a game.
func (s *Service) ListInstalls(gameID string) []app.Install {
	installs := []app.Install{}
	for _, install := range s.Config.Config().Installs {
		if install.Game == gameID {
			installs = append(installs, install)
		}
	}

	return installs
}

// ListReadyInstalls returns the ids of the installs whose caches are built, so
// they open without a scan.
func (s *Service) ListReadyInstalls() []string {
	ready := []string{}
	for _, install := range s.Config.Config().Installs {
		row := game.LookupGame(install.Game)
		if row == nil {
			continue
		}

		folder, err := resolveCaches(s.Config.Dirs.Cache, row, install.Path, resolveVersion(row, install), s.Build)
		if err != nil {
			continue
		}

		_, schemaErr := os.Stat(filepath.Join(folder, schemaFile))
		_, indexErr := os.Stat(filepath.Join(folder, indexFile))
		if schemaErr == nil && indexErr == nil {
			ready = append(ready, install.ID)
		}
	}

	return ready
}

// DetectInstalls finds a game's installs on this machine.
func (s *Service) DetectInstalls(gameID string) []game.DetectedInstall {
	row := game.LookupGame(gameID)
	if row == nil {
		return nil
	}

	return row.FindInstalls()
}

// AddInstall records an install of a game. A missing name or version is filled
// from the game and the install's files.
func (s *Service) AddInstall(gameID, path, name, version string) (app.Install, error) {
	row := game.LookupGame(gameID)
	if row == nil {
		return app.Install{}, fmt.Errorf("the game %s is not supported", gameID)
	}

	install := app.Install{
		ID: rand.Text(), Game: gameID, Name: cmp.Or(name, row.Name), Path: path,
		Version: cmp.Or(version, row.LoadVersion(path)),
	}
	err := s.Config.Update("installs", func(records *app.Config) error {
		records.Installs = append(records.Installs, install)

		return nil
	})

	return install, err
}

// UpdateInstall moves an install to another folder and reads its version
// again. An open workspace on the install opens again.
func (s *Service) UpdateInstall(id, path string) error {
	if !files.IsFolder(path) {
		return fmt.Errorf("%s is not a folder", path)
	}

	err := s.Config.Update("installs", func(records *app.Config) error {
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
		s.reopen(func(record app.Workspace) bool { return record.Install == id })
	}

	return err
}

// ListLanguages returns an install's localization languages.
func (s *Service) ListLanguages(installID string) ([]loc.Language, error) {
	install, row, err := findInstall(s.Config.Config(), installID)
	if err != nil {
		return nil, err
	}

	rel, _ := findLocalizationFolder(row)
	entries, _ := os.ReadDir(filepath.Join(install.Path, game.ScriptRoot, filepath.FromSlash(rel)))
	languages := []loc.Language{}
	for _, entry := range entries {
		if entry.IsDir() && loc.IsLanguage(entry.Name()) {
			languages = append(languages, loc.Language{
				Folder: entry.Name(), Label: loc.DescribeLanguage(entry.Name()), Base: entry.Name() == loc.BaseLanguage,
			})
		}
	}

	return languages, nil
}

// DeleteInstall removes an install's record. It fails while a workspace uses
// the install.
func (s *Service) DeleteInstall(id string) error {
	return s.Config.Update("installs", func(records *app.Config) error {
		if slices.ContainsFunc(records.Workspaces, func(record app.Workspace) bool { return record.Install == id }) {
			return fmt.Errorf("a workspace uses the install %s", id)
		}

		records.Installs = slices.DeleteFunc(records.Installs, func(install app.Install) bool { return install.ID == id })

		return nil
	})
}

// RescanInstall deletes an install's caches, so its next open scans it again.
func (s *Service) RescanInstall(id string) error {
	records := s.Config.Config()
	install, row, err := findInstall(records, id)
	if err != nil {
		return err
	}

	folder, err := resolveCaches(s.Config.Dirs.Cache, row, install.Path, resolveVersion(row, install), s.Build)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(folder); err != nil {
		return err
	}

	s.reopen(func(record app.Workspace) bool { return record.Install == id })

	return nil
}

// reopen opens the open workspace again if its record matches.
func (s *Service) reopen(matches func(record app.Workspace) bool) {
	s.mu.Lock()
	workspaceID := ""
	if s.session != nil {
		if record, _, _, err := findRecords(s.Config.Config(), s.session.ID); err == nil && matches(record) {
			workspaceID = record.ID
		}
	}
	s.mu.Unlock()

	if workspaceID != "" {
		s.Close()
		s.Open(workspaceID)
	}
}

// ResetData closes the open workspace and removes every workspace, install
// record and cache. Mods, game files and settings stay.
func (s *Service) ResetData() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closeSession()

	// The records go first, so no page shows a workspace that is gone.
	err := s.Config.Update("", func(records *app.Config) error {
		records.Installs, records.Workspaces = nil, nil

		return nil
	})
	if err != nil {
		return err
	}

	return removeEntries(s.Config.Dirs.Cache, "")
}

// ListWorkspaces returns every recorded workspace with its install and mods.
func (s *Service) ListWorkspaces() []Workspace {
	records := s.Config.Config()
	workspaces := make([]Workspace, len(records.Workspaces))
	for position, record := range records.Workspaces {
		install, row, _ := findInstall(records, record.Install)
		workspaces[position] = Workspace{Record: record, Install: install, Game: row}
		if row == nil {
			continue
		}

		workspaces[position].Mods = make([]Mod, len(record.Mods))
		roots := row.ListWorkshopRoots()
		for modPosition, mod := range record.Mods {
			workspaces[position].Mods[modPosition] = readMod(row, roots, mod)
		}
	}

	return workspaces
}

// CreateWorkspace records a workspace with no mods and returns it.
func (s *Service) CreateWorkspace(name, install, language string) (app.Workspace, error) {
	record := app.Workspace{
		ID: rand.Text(), Name: name, Install: install, Language: language, Mods: []app.Mod{}, IgnoredPairs: [][2]string{},
	}
	err := s.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = append(records.Workspaces, record)

		return nil
	})

	return record, err
}

// UpdateWorkspace replaces a workspace's record but keeps its mods, settings
// and ignored pairs.
func (s *Service) UpdateWorkspace(workspace app.Workspace) error {
	return s.UpdateRecord(workspace.ID, func(record *app.Workspace) error {
		workspace.Mods, workspace.Settings, workspace.IgnoredPairs = record.Mods, record.Settings, record.IgnoredPairs
		*record = workspace

		return nil
	})
}

// UpdateRecord applies a change to a workspace's record, and opens the
// workspace again if the change alters what it opens from.
//
//wails:ignore
func (s *Service) UpdateRecord(workspaceID string, change func(record *app.Workspace) error) error {
	reopens := false
	err := s.Config.Update("workspaces", func(records *app.Config) error {
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
		s.reopen(func(record app.Workspace) bool { return record.ID == workspaceID })
	}

	return err
}

// makeOpenKey joins the parts of a workspace's record that opening it reads,
// so a change to any of them opens it again.
func makeOpenKey(record app.Workspace) string {
	parts := []string{record.Install, record.Language}
	for _, mod := range record.Mods {
		parts = append(parts, mod.ID, mod.Dir)
	}

	return strings.Join(parts, "\x00")
}

// DeleteWorkspace removes a workspace's record. Its mod folders stay.
func (s *Service) DeleteWorkspace(id string) error {
	s.mu.Lock()
	if s.session != nil && s.session.ID == id {
		s.closeSession()
	}
	s.mu.Unlock()

	return s.Config.Update("workspaces", func(records *app.Config) error {
		records.Workspaces = slices.DeleteFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == id })

		return nil
	})
}

// Open opens a workspace in the background, closing the open one first, and
// sends workspace:opened or workspace:failed. The lock is held until the open
// ends, so every call that asks for the session waits for it.
func (s *Service) Open(workspaceID string) {
	s.mu.Lock()

	// A page that loads while the first open runs asks again.
	if s.session != nil && s.session.ID == workspaceID {
		s.mu.Unlock()
		application.Get().Event.Emit(EventOpened, workspaceID)

		return
	}

	s.closeSession()
	go func() {
		defer s.mu.Unlock()

		pmt := application.Get()
		ws, err := s.openSession(workspaceID,
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

		s.session = ws

		// The workspace opens even if the install record cannot be written.
		_ = s.updateInstall(ws)
		pmt.Event.Emit(EventOpened, workspaceID)
	}()
}

// updateInstall records the version and the scan time of a session's install,
// and sends install:scanned when the scan time changes.
func (s *Service) updateInstall(ws *Session) error {
	_, install, _, err := findRecords(s.Config.Config(), ws.ID)
	if err != nil {
		return err
	}

	stat, err := os.Stat(filepath.Join(ws.caches, indexFile))
	if err != nil {
		return err
	}

	// the schema keeps the version the open read
	version, scannedAt := ws.Schema.Version, stat.ModTime()
	if install.Version != version {
		ws.updatedFrom = install.Version
	}

	scanned := !install.ScannedAt.Equal(scannedAt)
	if install.Version == version && !scanned {
		return nil
	}

	err = s.Config.Update("installs", func(records *app.Config) error {
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

// closeSession closes the open session, if any. The caller holds the lock.
func (s *Service) closeSession() {
	if s.session == nil {
		return
	}

	s.session.Close()
	s.session = nil
	application.Get().Event.Emit(EventClosed, struct{}{})
}

// Session returns the open workspace, or an error if none is open.
//
//wails:ignore
func (s *Service) Session() (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return nil, errors.New("no workspace is open")
	}

	return s.session, nil
}

// Close closes the open workspace, if any.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closeSession()
}

// State returns the state of the open workspace, empty if none is open.
func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.session
	if ws == nil {
		return State{}
	}

	_, install, _, _ := findRecords(s.Config.Config(), ws.ID)
	count := func(dump string) int { return len(ws.Schema.ListKeys(dump)) }

	return State{
		Open: true, ID: ws.ID, Game: ws.Game, Install: install, InstallFound: files.IsFolder(install.Path),
		UpdatedFrom: ws.updatedFrom, Dumps: ws.Schema.Dumps,
		Effects: count(lang.DumpEffects), Triggers: count(lang.DumpTriggers), Scopes: count(lang.DumpTypes),
	}
}

// LookupMod returns a mod of the open workspace.
//
//wails:ignore
func (s *Service) LookupMod(modID string) (Mod, bool) {
	return findMod(s.ListMods(), modID)
}

// ResolveFolder returns the folder a shell or an agent starts in, and its mod's
// id: the mod that holds the file, else the first mod that has a folder, else
// the home folder with the id "".
//
//wails:ignore
func (s *Service) ResolveFolder(path string) (string, string) {
	mods := s.ListMods()
	if path != "" {
		if mod, ok := findMod(mods, s.Locate(path).Origin); ok && !mod.Broken {
			return mod.Dir, mod.ID
		}
	}

	for _, mod := range mods {
		if !mod.Broken {
			return mod.Dir, mod.ID
		}
	}

	home, _ := os.UserHomeDir()

	return home, ""
}

// Launch starts the game with its debug flag. When it exits, game:exited says
// whether it wrote fresher dumps.
func (s *Service) Launch() error {
	ws, err := s.Session()
	if err != nil {
		return err
	}

	program, args, ok := ws.Game.ResolveProgram(ws.InstallPath)
	if !ok {
		return fmt.Errorf("%s cannot be started on this system", ws.Game.Name)
	}

	command := exec.Command(program, args...)
	command.Dir = filepath.Dir(program)
	if err := command.Start(); err != nil {
		return err
	}

	knownDumps := ws.Schema.Dumps.WrittenAt
	go func() {
		_ = command.Wait()
		dumpsAt, err := schema.FindDumpsAt(ws.Game.FindDumpDirs(ws.InstallPath))
		application.Get().Event.Emit(EventGameExited, err == nil && dumpsAt.After(knownDumps))
	}()

	return nil
}
