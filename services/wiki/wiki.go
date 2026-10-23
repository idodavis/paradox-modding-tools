package wiki

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The events the wiki service sends.
const (
	EventUpdated  = "wiki:updated"  // a wiki refresh finished
	EventProgress = "wiki:progress" // a wiki refresh fetched a page; the data is a Progress
)

func init() {
	application.RegisterEvent[struct{}](EventUpdated)
	application.RegisterEvent[Progress](EventProgress)
}

// A Progress is the data of the wiki:progress event: how many pages a refresh has fetched out of the total.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Service is the wiki service behind the Wiki pane and the Patch Notes page. It makes one client for the open
// workspace's game on first use, and a new one when a workspace of another game opens.
type Service struct {
	Config    *app.ConfigFile // the cache folder
	Workspace *workspace.Service
	mu        sync.Mutex // held while client is read or replaced
	client    *Client    // made on first use for the open workspace's game
}

// ReadPage returns the wiki page with the given title, fetching it if it is not cached.
func (wik *Service) ReadPage(title string) (Page, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return Page{}, err
	}
	return wik.resolveClient(ws).resolvePage(title)
}

// ListNotes returns the patch notes, newest first.
func (wik *Service) ListNotes() ([]Note, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}
	return wik.resolveClient(ws).ListNotes()
}

// ReadNote returns the patch notes page of a version.
func (wik *Service) ReadNote(version string) (Page, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return Page{}, err
	}
	return wik.resolveClient(ws).ReadNote(version)
}

// Refresh fetches the page listing and every page that is missing or stale. The wiki:progress event reports each
// page fetched and wiki:updated the end. It returns when the refresh ends, with an error if no workspace is open or
// the listing or a page could not be fetched.
func (wik *Service) Refresh() error {
	defer application.Get().Event.Emit(EventUpdated, struct{}{})
	ws, err := wik.Workspace.Session()
	if err != nil {
		return err
	}
	return wik.resolveClient(ws).Refresh(func(done, total int) {
		application.Get().Event.Emit(EventProgress, Progress{Done: done, Total: total})
	})
}

// resolveClient returns the client for the open workspace's game. It makes one on first use and again when the
// workspace's game is not the client's.
func (wik *Service) resolveClient(ws *workspace.Session) *Client {
	wik.mu.Lock()
	defer wik.mu.Unlock()
	if wik.client == nil || wik.client.game.ID != ws.Game.ID {
		wik.client = NewClient(wik.Config.Dirs.Cache, ws.Game)
	}
	return wik.client
}
