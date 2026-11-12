// Package wiki reads the community wiki of the open workspace's game. It caches
// the wiki's pages, lists the patch notes, finds the pages that belong to a
// file, and builds the lexicon of the game's script commands.
//
// A client asks the wiki's API at most twice a second. Each page is cached as
// one JSON file with its HTML sanitized. A refresh fetches the pages that are
// not cached or are over seven days old.
//
// The wiki's rules, as the package applies them:
//
//   - The listing is the pages of the Modding category and the versions of the
//     Patches category. A patch page is titled "Patch 1.20", or "Patch 1.20.X"
//     for a minor version's hotfix notes.
//   - A note's release date is the one in "released on 2026-09-30", within the
//     first 2000 bytes of its text.
//   - A sanitized page loses the wiki's chrome, citations, pictures and its See
//     also, References, External links and Further reading sections. It folds
//     a table of more than two rows and keeps only links that are web
//     addresses.
//
// # Robustness
//
// FetchPage refuses a page the wiki answers with an error, and an answer that
// is not JSON. A refresh stops if the listing fails, but not for a page that
// fails. FindPages, SearchPages and ListNotes refuse until a refresh has
// fetched the listing.
//
// A cached file that is missing or does not decode reads as not cached, and a
// note whose page is not cached has no release date. Sanitize reads past
// elements and attributes it does not keep.
package wiki

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/mod/semver"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/services/workspace"
)

// Progress is the data of the wiki:progress event.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Service is the Wails service that reads the wiki.
type Service struct {
	Workspace *workspace.Service

	mu     sync.Mutex
	client *Client

	// lexicon holds each class's rows, built from the index mods.
	lexicon map[string][]LexiconRow
	mods    *index.Index
}

const (
	// EventUpdated is sent when a refresh finishes. Its data is the error's
	// text, or "" if it worked.
	EventUpdated = "wiki:updated"

	// EventProgress is sent after each page a refresh fetches. Its data is a
	// Progress.
	EventProgress = "wiki:progress"
)

func init() {
	application.RegisterEvent[string](EventUpdated)
	application.RegisterEvent[Progress](EventProgress)
}

// ServiceStartup refreshes the wiki in the background each time a workspace
// opens.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	application.Get().Event.On(workspace.EventOpened, func(*application.CustomEvent) {
		go func() { _ = s.Refresh() }()
	})

	return nil
}

// ReadPage returns a wiki page, fetching it if it is not cached. It returns an
// error if no workspace is open or the fetch fails.
func (s *Service) ReadPage(title string) (Page, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Page{}, err
	}

	client := s.resolveClient(ws)
	if page, ok := client.LoadPage(title); ok {
		return page, nil
	}

	return client.FetchPage(title)
}

// ListNotes returns the patch notes, newest first. It returns an error if no
// workspace is open or the listing has not been fetched.
func (s *Service) ListNotes() ([]Note, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	client := s.resolveClient(ws)
	listing, ok := client.LoadListing()
	if !ok {
		return nil, errNoListing
	}

	versions := slices.Clone(listing.Versions)
	slices.SortFunc(versions, func(first, second string) int { return game.CompareVersions(second, first) })
	notes := make([]Note, len(versions))
	for position, version := range versions {
		title := resolvePatch(version)
		page, _ := client.LoadPage(title)
		minor := strings.TrimPrefix(semver.MajorMinor("v"+strings.TrimSuffix(version, ".X")), "v")
		parent := minor
		if minor == version {
			parent = ""
		}

		notes[position] = Note{
			Version: version, Parent: parent, Released: page.Released, Title: title, Modding: page.Modding,
		}
	}

	return notes, nil
}

// Refresh fetches the listing and every missing or stale page, sending
// wiki:progress for each page and wiki:updated at the end. It returns an error
// if no workspace is open or a fetch fails.
func (s *Service) Refresh() error {
	ws, err := s.Workspace.Session()
	if err == nil {
		err = s.resolveClient(ws).Refresh(func(done, total int) {
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

// resolveClient returns the client for the open workspace's game, making a new
// one when the game changes.
func (s *Service) resolveClient(ws *workspace.Session) *Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.client.game.ID != ws.Game.ID {
		s.client = NewClient(s.Workspace.Config.Dirs.Cache, ws.Game)
	}

	return s.client
}
