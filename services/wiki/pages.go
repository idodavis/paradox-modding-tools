// pages.go finds and searches pages and reads a patch note's facts.

package wiki

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"paradox-modding-tools/internal/game"
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
	singular := strings.ToLower(game.DescribeDefinition(place.Database, false))
	plural := strings.ToLower(game.DescribeDefinition(place.Database, true))
	topics := []string{strings.ToLower(string(place.Format))}
	for _, topic := range game.WikiTopics {
		inFolder := topic.Folder == "" || strings.HasPrefix(strings.ToLower(place.Rel), topic.Folder+"/")
		if (topic.Format == "" || topic.Format == place.Format) && inFolder {
			topics = append(topics, topic.Terms...)
		}
	}

	holds := func(title, term string) bool { return term != "" && strings.Contains(title, term) }
	holdsTopic := func(title string) bool {
		return slices.ContainsFunc(topics, func(term string) bool { return holds(title, term) })
	}
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
		}

		return 4
	}

	pages := make([]Summary, 0, len(listing.Pages))
	for _, summary := range listing.Pages {
		title := strings.ToLower(summary.Title)
		if holds(title, singular) || holds(title, plural) || holdsTopic(title) {
			pages = append(pages, summary)
		}
	}

	slices.SortStableFunc(pages, func(first, second Summary) int {
		return cmp.Compare(rank(first.Title), rank(second.Title))
	})

	return pages, nil
}

// SearchPages returns the modding pages that hold a text, title matches first.
// A page that is not cached is searched by title alone. It returns the errors
// FindPages does.
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
		page, ok := client.LoadPage(summary.Title)
		if !ok {
			continue
		}

		root, err := parseFragment(page.HTML)
		if err == nil && strings.Contains(strings.ToLower(textOf(root)), term) {
			pages = append(pages, summary)
		}
	}

	return pages, nil
}

var releasePattern = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)

const releaseSpan = 2000

// resolveReleased returns the release date a note gives, or "" for none.
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
