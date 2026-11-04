// pages.go finds and searches the wiki's pages and reads the facts of a patch note.

package wiki

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/mod/semver"
	"golang.org/x/net/html"

	"paradox-modding-tools/internal/game"
)

// A Note is one patch's notes page.
type Note struct {
	Version  string `json:"version"`
	Parent   string `json:"parent"`   // the version a hotfix belongs to, such as 1.14 for 1.14.2; "" for a release
	Released string `json:"released"` // the release date as the note writes it; "" if it gives none
	Title    string `json:"title"`
	Modding  bool   `json:"modding"` // the page has a Modding section
}

// releasePattern matches the release date in a patch note, such as "released on 2026-09-30".
var releasePattern = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)

// releaseSpan is how many bytes of a note are searched for its release date.
const releaseSpan = 2000

// errNoListing is the error of a call that needs the listing before a refresh has fetched it.
var errNoListing = errors.New("the wiki's listing has not been fetched yet")

// FindPages returns the wiki pages that belong to the file at the given path, best match first. It returns an error if
// no workspace is open or no refresh has fetched the listing yet.
func (wik *Service) FindPages(path string) ([]Summary, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}
	listing, ok := wik.resolveClient(ws).LoadListing()
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
	pages := lo.Filter(listing.Pages, func(summary Summary, _ int) bool {
		title := strings.ToLower(summary.Title)
		return holds(title, singular) || holds(title, plural) || holdsTopic(title)
	})
	slices.SortStableFunc(pages, func(first, second Summary) int {
		return cmp.Compare(rank(first.Title), rank(second.Title))
	})
	return pages, nil
}

// SearchPages returns the wiki's modding pages that hold the given text, letter case aside, those with it in the title
// first. A page that is not cached is searched by its title alone. It returns an error if no workspace is open or no
// refresh has fetched the listing yet.
func (wik *Service) SearchPages(term string) ([]Summary, error) {
	ws, err := wik.Workspace.Session()
	if err != nil {
		return nil, err
	}
	client := wik.resolveClient(ws)
	listing, ok := client.LoadListing()
	if !ok {
		return nil, errNoListing
	}
	term = strings.ToLower(term)
	pages, rest := lo.FilterReject(listing.Pages, func(summary Summary, _ int) bool {
		return strings.Contains(strings.ToLower(summary.Title), term)
	})
	return append(pages, lo.Filter(rest, func(summary Summary, _ int) bool {
		page, ok := client.LoadPage(summary.Title)
		if !ok {
			return false
		}
		root, err := parseFragment(page.HTML)
		return err == nil && strings.Contains(strings.ToLower(textOf(root)), term)
	})...), nil
}

// resolveReleased returns the release date a patch note gives, or "" if it gives none.
func resolveReleased(root *html.Node) string {
	text := textOf(root)
	return lo.NthOr(releasePattern.FindStringSubmatch(text[:min(len(text), releaseSpan)]), 1, "")
}

// compareVersions orders two patch versions as semver does, except that a minor version's hotfix notes, "1.20.X",
// sort below every other version of that minor version.
func compareVersions(first, second string) int {
	hotfixFirst, hotfixSecond := strings.HasSuffix(first, ".X"), strings.HasSuffix(second, ".X")
	first, second = strings.TrimSuffix(first, ".X"), strings.TrimSuffix(second, ".X")
	if minors := semver.Compare(semver.MajorMinor("v"+first), semver.MajorMinor("v"+second)); minors != 0 {
		return minors
	}
	if hotfixFirst != hotfixSecond {
		return lo.Ternary(hotfixFirst, -1, 1)
	}
	return semver.Compare("v"+first, "v"+second)
}

// hasModding reports whether a page has a heading that holds the word "Modding".
func hasModding(root *html.Node) bool {
	for node := range root.Descendants() {
		if elements[node.DataAtom].heading && strings.Contains(textOf(node), "Modding") {
			return true
		}
	}
	return false
}
