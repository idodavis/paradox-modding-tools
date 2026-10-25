package wiki

import (
	"context"
	"slices"
	"sync"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/workspace"
)

// The events the wiki service sends.
const (
	EventUpdated  = "wiki:updated"  // a wiki refresh finished; the data is its error's text, "" when it worked
	EventProgress = "wiki:progress" // a wiki refresh fetched a page; the data is a Progress
)

// init registers the types of the events' data with Wails.
func init() {
	application.RegisterEvent[string](EventUpdated)
	application.RegisterEvent[Progress](EventProgress)
}

// A Progress is the data of the wiki:progress event.
type Progress struct {
	Done  int `json:"done"`  // the pages a refresh has fetched so far
	Total int `json:"total"` // the pages it fetches in all
}

// Service is the Wails service that reads the wiki. It makes one client for the open workspace's game on first use,
// and a new one when a workspace of another game opens.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex // held while client is read or replaced
	client    *Client    // made on first use for the open workspace's game
}

// ServiceStartup refreshes the wiki of the open workspace's game in the background each time a workspace opens.
func (wik *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	application.Get().Event.On(workspace.EventOpened, func(*application.CustomEvent) {
		go func() { _ = wik.Refresh() }()
	})
	return nil
}

// ReadPage returns the wiki page with the given title, fetching it if it is not cached. It returns an error if no
// workspace is open or the fetch fails.
func (wik *Service) ReadPage(title string) (Page, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return Page{}, err
	}
	return wik.resolveClient(ws).resolvePage(title)
}

// ListNotes returns the patch notes, newest first. A note whose page is not cached yet shows no Modding section. It
// returns an error if no workspace is open or a refresh has not fetched the wiki's listing yet.
func (wik *Service) ListNotes() ([]Note, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}
	client := wik.resolveClient(ws)
	listing, ok := client.LoadListing()
	if !ok {
		return nil, errNoListing
	}
	versions := slices.Clone(listing.Versions)
	slices.SortFunc(versions, func(first, second string) int { return compareVersions(second, first) })
	return lo.Map(versions, func(version string, _ int) Note {
		title := resolvePatch(version)
		page, _ := client.LoadPage(title)
		return Note{
			Version: version, Parent: resolveParent(version), Released: resolveReleased(page.HTML), Title: title,
			Modding: page.Modding,
		}
	}), nil
}

// ReadNote returns the patch notes page of the given version, fetching it if it is not cached. It returns an error if
// no workspace is open or the fetch fails.
func (wik *Service) ReadNote(version string) (Page, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return Page{}, err
	}
	return wik.resolveClient(ws).resolvePage(resolvePatch(version))
}

// Refresh fetches the page listing and every page that is missing or stale. The wiki:progress event reports each
// page fetched and wiki:updated the end, with the error's text if there was one. It returns when the refresh ends,
// with an error if no workspace is open or the listing or a page could not be fetched.
func (wik *Service) Refresh() error {
	ws, err := wik.Workspace.Session()
	if err == nil {
		err = wik.resolveClient(ws).Refresh(func(done, total int) {
			application.Get().Event.Emit(EventProgress, Progress{Done: done, Total: total})
		})
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	application.Get().Event.Emit(EventUpdated, message)
	return err
}

// resolveClient returns the client for the open workspace's game. It makes one on first use and again when the
// workspace's game is not the client's.
func (wik *Service) resolveClient(ws *workspace.Session) *Client {
	wik.mu.Lock()
	defer wik.mu.Unlock()
	if wik.client == nil || wik.client.game.ID != ws.Game.ID {
		wik.client = NewClient(wik.Workspace.Config.Dirs.Cache, ws.Game)
	}
	return wik.client
}
