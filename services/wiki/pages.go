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
	"paradox-modding-tools/internal/lang"
)

// Note is one patch's notes page.
type Note struct {
	Version string `json:"version"`

	// Parent is the version a hotfix belongs to, such as 1.14 for 1.14.2. It is
	// "" for a release.
	Parent string `json:"parent"`

	// Released is the date the note gives for its release, or "" for none.
	Released string `json:"released"`
	Title    string `json:"title"`
	Modding  bool   `json:"modding"`
}

var errNoListing = errors.New("the wiki's listing has not been fetched yet")

// pageTopics are the words the games' wikis put in the titles of the pages
// about a format's files or a folder's, beside the format's own name. A row
// with no format or no folder holds for any.
var pageTopics = []struct {
	Format files.Format
	Folder string
	Terms  []string
}{
	{Format: files.FormatGUI, Terms: []string{"interface"}},
	{Folder: "gfx/interface", Terms: []string{"interface"}},
	{Format: files.FormatAsset, Terms: []string{"model"}},
	{Folder: "gfx/models", Terms: []string{"model"}},
	{Format: files.FormatImage, Terms: []string{"graphical asset"}},
	{Folder: "gfx/map", Terms: []string{"map modding"}},
}

// FindPages returns the wiki pages that belong to a file, best match first. It
// returns an error if no workspace is open or the listing has not been fetched.
func (s *Service) FindPages(path string) ([]Summary, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	listing, ok := s.resolveClient(ws).LoadListing()
	if !ok {
		return nil, errNoListing
	}

	place := ws.Locate(ws.Mods.Load(), path)
	singular := strings.ToLower(lang.DescribeDefinition(place.Database, false))
	plural := strings.ToLower(lang.DescribeDefinition(place.Database, true))
	topics := []string{strings.ToLower(string(place.Format))}
	for _, topic := range pageTopics {
		inFolder := topic.Folder == "" || strings.HasPrefix(strings.ToLower(place.Rel), topic.Folder+"/")
		if (topic.Format == "" || topic.Format == place.Format) && inFolder {
			topics = append(topics, topic.Terms...)
		}
	}

	// rank orders the titles that belong to the file, and is -1 for the rest.
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
		case slices.Contains(topics, title):
			return 3
		case slices.ContainsFunc(topics, func(term string) bool { return holds(title, term) }):
			return 4
		}

		return -1
	}

	pages := make([]Summary, 0, len(listing.Pages))
	for _, summary := range listing.Pages {
		if rank(summary.Title) >= 0 {
			pages = append(pages, summary)
		}
	}

	slices.SortStableFunc(pages, func(first, second Summary) int {
		return cmp.Compare(rank(first.Title), rank(second.Title))
	})

	return pages, nil
}

// SearchPages returns the modding pages that hold a text, title matches first.
// A page that is not cached is searched by its title alone.
func (s *Service) SearchPages(term string) ([]Summary, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	client := s.resolveClient(ws)
	listing, ok := client.LoadListing()
	if !ok {
		return nil, errNoListing
	}

	term = strings.ToLower(term)
	pages := make([]Summary, 0, len(listing.Pages))
	var rest []Summary
	for _, summary := range listing.Pages {
		if strings.Contains(strings.ToLower(summary.Title), term) {
			pages = append(pages, summary)
		} else {
			rest = append(rest, summary)
		}
	}

	for _, summary := range rest {
		if text, ok := client.loadText(summary.Title); ok && strings.Contains(text, term) {
			pages = append(pages, summary)
		}
	}

	return pages, nil
}

var releasePattern = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)

const releaseSpan = 2000

// resolveReleased returns the date a patch note says it was released on, as
// in "released on 2026-09-30" near the start of its text, or "" for none.
func resolveReleased(root *html.Node) string {
	text := textOf(root)
	match := releasePattern.FindStringSubmatch(text[:min(len(text), releaseSpan)])
	if len(match) < 2 {
		return ""
	}

	return match[1]
}

// hasModding reports whether a heading of the page holds the word Modding.
func hasModding(root *html.Node) bool {
	for node := range root.Descendants() {
		if elements[node.DataAtom].heading && strings.Contains(textOf(node), "Modding") {
			return true
		}
	}

	return false
}
