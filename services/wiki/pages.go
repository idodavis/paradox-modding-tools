package wiki

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/mod/semver"

	"paradox-modding-tools/internal/game"
)

// A Note is one patch's notes page: its version and whether the page has a Modding section.
type Note struct {
	Version string `json:"version"`
	Title   string `json:"title"`
	Modding bool   `json:"modding"`
}

// errNoListing is the error of a call that needs the listing before a refresh has kept one.
var errNoListing = errors.New("the wiki's listing has not been fetched yet")

// FindPages returns the wiki pages that belong to a file: those whose title contains the name of the file's database
// or format. The database's own page comes first, then its plural, then the pages that hold either, then the format's
// page. It returns an error if no workspace is open or a refresh has not fetched the wiki's listing yet.
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
	format := strings.ToLower(string(place.Format))
	holds := func(title, term string) bool { return term != "" && strings.Contains(title, term) }
	// the database's terms rank before the format's: its exact title, its plural, a title holding either
	rank := func(title string) int {
		title = strings.ToLower(title)
		switch {
		case title == singular:
			return 0
		case title == plural:
			return 1
		case holds(title, singular) || holds(title, plural):
			return 2
		case title == format:
			return 3
		}
		return 4
	}
	pages := lo.Filter(listing.Pages, func(summary Summary, _ int) bool {
		title := strings.ToLower(summary.Title)
		return holds(title, singular) || holds(title, plural) || holds(title, format)
	})
	slices.SortStableFunc(pages, func(first, second Summary) int {
		return cmp.Compare(rank(first.Title), rank(second.Title))
	})
	return pages, nil
}

// compareVersions orders two patch versions as semver does, except that a minor version's hotfix notes, "1.20.X",
// come after every numbered patch of that minor version.
func compareVersions(first, second string) int {
	hotfixFirst, hotfixSecond := strings.HasSuffix(first, ".X"), strings.HasSuffix(second, ".X")
	first, second = strings.TrimSuffix(first, ".X"), strings.TrimSuffix(second, ".X")
	if minors := semver.Compare(semver.MajorMinor("v"+first), semver.MajorMinor("v"+second)); minors != 0 {
		return minors
	}
	if hotfixFirst != hotfixSecond {
		return lo.Ternary(hotfixFirst, 1, -1)
	}
	return semver.Compare("v"+first, "v"+second)
}

// hasModding reports whether a page's HTML has a heading that holds the word "Modding".
func hasModding(text string) bool {
	root, err := parseFragment(text)
	if err != nil {
		return false
	}
	for node := range root.Descendants() {
		if !elements[node.DataAtom].heading {
			continue
		}
		var heading strings.Builder
		writeText(&heading, node)
		if strings.Contains(heading.String(), "Modding") {
			return true
		}
	}
	return false
}
