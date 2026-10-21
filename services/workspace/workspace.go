package workspace

import (
	"net/http"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// The events the workspace service sends.
const (
	EventOpened          = "workspace:opened" // a workspace is open with its index built; the data is its id
	EventFailed          = "workspace:failed" // a workspace could not be opened; the data is the error's text
	EventClosed          = "workspace:closed" // the open workspace closed
	EventInstallScanning = "install:scanning" // an install's scan reports a stage; the data is the stage
	EventInstallScanned  = "install:scanned"  // an install's scan finished; the data is the install's id
	EventFileChanged     = "fs:changed"       // a file of the open workspace changed on disk; the data is a FileChange
	EventGameExited      = "game:exited"      // the launched game exited; the data is a GameExit
)

func init() {
	application.RegisterEvent[string](EventOpened)
	application.RegisterEvent[string](EventFailed)
	application.RegisterEvent[struct{}](EventClosed)
	application.RegisterEvent[string](EventInstallScanning)
	application.RegisterEvent[string](EventInstallScanned)
	application.RegisterEvent[FileChange](EventFileChanged)
	application.RegisterEvent[GameExit](EventGameExited)
}

// A FileChange is the data of the fs:changed event: the file's path and whether it was deleted.
type FileChange struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
}

// A GameExit is the data of the game:exited event. It says whether the game wrote fresher dumps while it ran.
type GameExit struct {
	DumpsFresher bool `json:"dumpsFresher"` // the dumps are now fresher than the schema, so a rescan is offered
}

// Service is the workspace service. It keeps the records of games, installs, workspaces and mods, holds the one
// open workspace that every other service reads through Session, serves files to the explorer, and launches the
// game.
type Service struct {
	Config  *app.ConfigFile // config.json, for the records and the folders
	mu      sync.Mutex
	session *Session // nil while no workspace is open
}

// ServiceShutdown closes the open workspace.
func (wsp *Service) ServiceShutdown() error { panic("stub") }

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

// ListGames returns the supported games.
func (wsp *Service) ListGames() []*game.Game { panic("stub") }

// ListInstalls returns every recorded install.
func (wsp *Service) ListInstalls() []app.Install { panic("stub") }

// DetectInstalls finds a game's installs on this machine.
func (wsp *Service) DetectInstalls(gameID string) []game.DetectedInstall { panic("stub") }

// AddInstall records an install of a game at a folder, reads its version, and emits config:changed for the
// installs.
func (wsp *Service) AddInstall(gameID, path string) (app.Install, error) { panic("stub") }

// DeleteInstall removes an install's record. It returns an error while a workspace uses the install.
func (wsp *Service) DeleteInstall(id string) error { panic("stub") }

// RescanInstall drops an install's cache, so the next open scans it again.
func (wsp *Service) RescanInstall(id string) error { panic("stub") }

// ListWorkspaces returns every recorded workspace.
func (wsp *Service) ListWorkspaces() []app.Workspace { panic("stub") }

// CreateWorkspace records a new workspace and emits config:changed for the workspaces.
func (wsp *Service) CreateWorkspace(ws app.Workspace) (app.Workspace, error) { panic("stub") }

// UpdateWorkspace replaces a workspace's record.
func (wsp *Service) UpdateWorkspace(ws app.Workspace) error { panic("stub") }

// DeleteWorkspace removes a workspace's record. The mod folders on disk stay.
func (wsp *Service) DeleteWorkspace(id string) error { panic("stub") }

// AddMod adds an existing mod folder to a workspace.
func (wsp *Service) AddMod(workspaceID, dir string) (Mod, error) { panic("stub") }

// CreateMod creates a new mod folder with its skeleton and adds it to a workspace.
func (wsp *Service) CreateMod(workspaceID, name string) (Mod, error) { panic("stub") }

// RemoveMod takes a mod out of a workspace. Its folder stays.
func (wsp *Service) RemoveMod(workspaceID, modID string) error { panic("stub") }

// ReorderMods sets a workspace's load order.
func (wsp *Service) ReorderMods(workspaceID string, modIDs []string) error { panic("stub") }

// UpdateMod replaces a mod's record: its color, calendar, Workshop item and exclusions.
func (wsp *Service) UpdateMod(workspaceID string, mod app.Mod) error { panic("stub") }

// Open opens a workspace in the background, closing the one that was open before. The install:scanning event
// reports each stage, and workspace:opened or workspace:failed ends it. A call to Session while a workspace is
// opening waits for it.
func (wsp *Service) Open(workspaceID string) { panic("stub") }

// Session returns the open workspace, or an error saying that none is open. Every other service calls it at each
// request and keeps no session of its own. It is not bound to the frontend.
//
//wails:ignore
func (wsp *Service) Session() (*Session, error) { panic("stub") }

// Close closes the open workspace, if any.
func (wsp *Service) Close() { panic("stub") }

// State returns the state of the open workspace, with Open false when there is none.
func (wsp *Service) State() State { panic("stub") }

// Launch starts the game with its debug flag. When the game exits, the game:exited event says whether it wrote
// fresher dumps.
func (wsp *Service) Launch() error { panic("stub") }

// Read returns a file's text and where the file sits in the workspace.
func (wsp *Service) Read(path string) (FileText, error) { panic("stub") }

// Write writes a file's text in its encoding and emits fs:changed. The write is atomic.
func (wsp *Service) Write(path string, text files.Text) error { panic("stub") }

// ListFolder lists a folder's entries, folders first. Binary files are left out when the setting hides them.
func (wsp *Service) ListFolder(dir string) ([]Child, error) { panic("stub") }

// ListRoots returns the explorer's top-level folders: the mods in load order, then the game's.
func (wsp *Service) ListRoots() []files.Root { panic("stub") }

// Create makes a new file, or a folder when the flag is set, and emits fs:changed.
func (wsp *Service) Create(path string, dir bool) error { panic("stub") }

// Rename moves a file or a folder and emits fs:changed.
func (wsp *Service) Rename(from, to string) error { panic("stub") }

// Delete removes a file or a folder and emits fs:changed. Deleting a mod's root folder removes the mod from the
// workspace as well.
func (wsp *Service) Delete(path string) error { panic("stub") }

// Locate returns where a file sits in the workspace.
func (wsp *Service) Locate(path string) Place { panic("stub") }

// ServeHTTP serves a file's bytes by its path under the service's route. An image the browser cannot show is
// decoded to PNG first. A path outside the workspace's roots is refused.
func (wsp *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) { panic("stub") }
