package wiki

import (
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
// workspace's game on first use.
type Service struct {
	Config    *app.ConfigFile // the cache folder
	Workspace *workspace.Service
	client    *Client // made on first use for the open workspace's game
}

// ReadPage returns the wiki page with the given title, fetching it if it is not cached.
func (wik *Service) ReadPage(title string) (Page, error) { panic("stub") }

// ListNotes returns the patch notes, newest first.
func (wik *Service) ListNotes() ([]Note, error) { panic("stub") }

// ReadNote returns the patch notes page of a version.
func (wik *Service) ReadNote(version string) (Page, error) { panic("stub") }

// Refresh fetches the page listing and every page that is missing or stale. The wiki:progress event reports each
// page and wiki:updated the end.
func (wik *Service) Refresh() { panic("stub") }
