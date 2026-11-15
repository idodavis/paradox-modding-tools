// pages.go finds and searches pages and reads a patch note's facts.

package wiki

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/app"
)

// Note is one patch's notes page.
type Note struct {
	Version string `json:"version"`

	// Released is the date the note gives for its release, or "" for none.
	Released string `json:"released"`
	Title    string `json:"title"`
	Modding  bool   `json:"modding"`
}

// ResolveNote returns the version of the wiki's note that holds the notes of a
// game version. The wiki writes one note for a release, as 1.15, and one for
// its hotfixes, as 1.15.X. A version whose numbers after the first two are
// all 0, as 1.15.0, is in the release's note. Any other version of the
// release, as 1.15.1, is in the hotfixes'. An open beta is read by its number.
// It returns "" for a version that names no release.
func ResolveNote(version string) string {
	release := game.ResolveRelease(version)
	number, _, _ := strings.Cut(version, "-")
	rest := strings.Split(strings.TrimPrefix(number, release), ".")
	isHotfix := slices.ContainsFunc(rest, func(part string) bool { return part != "" && part != "0" })
	if release == "" || !isHotfix {
		return release
	}

	return release + ".X"
}

// ErrNoListing is the error of ListNotes before a refresh has fetched the
// wiki's listing.
var ErrNoListing = errors.New("the wiki's listing has not been fetched yet")

// pageTopics are the words the games' wikis put in the titles of the pages
// about a format's files or a folder's, beside the format's own name. A row
// with no format or no folder holds for any.
var pageTopics = []struct {
	Format files.Format
	Folder string
	Terms  []string
}{
	{Format: files.FormatGUI, Terms: []string{"interface", "gui script", "scripted gui"}},
	{Folder: "gfx/interface", Terms: []string{"interface"}},
	{Format: files.FormatAsset, Terms: []string{"model"}},
	{Folder: "gfx/models", Terms: []string{"model"}},
	{Format: files.FormatImage, Terms: []string{"graphical asset"}},
	{Folder: "gfx/map", Terms: []string{"map modding"}},
}

// FindPages returns the wiki pages that belong to a file, best match first. It
// returns nil, with no error, until a refresh has fetched the listing, and an
// error if no workspace is open. A file with no page gets an empty slice.
func (s *Service) FindPages(path string) ([]string, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	listing, ok := s.resolveClient(ws).LoadListing()
	if !ok {
		return nil, nil
	}

	place := ws.Locate(ws.Mods.Load(), path)
	singular := strings.ToLower(lang.DescribeDefinition(place.Database, false))
	plural := strings.ToLower(lang.DescribeDefinition(place.Database, true))
	format := strings.ToLower(string(place.Format))
	var topics []string
	for _, topic := range pageTopics {
		inFolder := topic.Folder == "" || strings.HasPrefix(strings.ToLower(place.Rel), topic.Folder+"/")
		if (topic.Format == "" || topic.Format == place.Format) && inFolder {
			topics = append(topics, topic.Terms...)
		}
	}

	// rank orders the titles that belong to the file, and is -1 for the rest.
	// The format's name ranks a page only as its whole title: "script" is in
	// the title of every page about scripting, and so of none about this file.
	holds := func(title, term string) bool { return term != "" && strings.Contains(title, term) }
	rank := func(title string) int {
		title = strings.ToLower(title)
		switch {
		case title == singular:
			return 0
		case title == plural:
			return 1
		case holds(title, singular) || holds(title, plural):
			return 2
		case title == format || slices.Contains(topics, title):
			return 3
		case slices.ContainsFunc(topics, func(term string) bool { return holds(title, term) }):
			return 4
		}

		return -1
	}

	pages := make([]string, 0, len(listing.Pages))
	for _, title := range listing.Pages {
		if rank(title) >= 0 {
			pages = append(pages, title)
		}
	}

	slices.SortStableFunc(pages, func(first, second string) int { return cmp.Compare(rank(first), rank(second)) })

	return pages, nil
}

// SearchPages returns the modding pages that hold a text, title matches first.
// A page that is not cached is searched by its title alone. It returns nil,
// with no error, until a refresh has fetched the listing, and an error if no
// workspace is open.
func (s *Service) SearchPages(term string) ([]string, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	client := s.resolveClient(ws)
	listing, ok := client.LoadListing()
	if !ok {
		return nil, nil
	}

	term = strings.ToLower(term)
	pages := make([]string, 0, len(listing.Pages))
	var rest []string
	for _, title := range listing.Pages {
		if strings.Contains(strings.ToLower(title), term) {
			pages = append(pages, title)
		} else {
			rest = append(rest, title)
		}
	}

	for _, title := range rest {
		if text, ok := client.loadText(title); ok && strings.Contains(text, term) {
			pages = append(pages, title)
		}
	}

	return pages, nil
}

var releasePattern = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)

const releaseSpan = 2000

// resolveReleased returns the date a patch note says it was released on, as
// in "released on 2026-09-30" near the start of its text, or "" for none.
func resolveReleased(root *html.Node) string {
	text := app.TextOf(root)
	match := releasePattern.FindStringSubmatch(text[:min(len(text), releaseSpan)])
	if len(match) < 2 {
		return ""
	}

	return match[1]
}

// hasModding reports whether a heading of the page holds the word Modding.
func hasModding(root *html.Node) bool {
	for node := range root.Descendants() {
		if app.LevelOf(node) > 0 && strings.Contains(app.TextOf(node), "Modding") {
			return true
		}
	}

	return false
}
