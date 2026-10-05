// lookup.go answers the Guide and Patch Notes queries from a game's sidecar, and holds the
// sidecar's shape on disk and the payloads the RPCs hand back.

package wiki

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
)

const guideTabCap = 6

// synonyms are the page stems a file no script database holds also matches, by its extension.
var synonyms = map[string][]string{
	"gui": {"interface", "gui script"}, "yml": {"localization", "customizable localization"},
	"mod": {"mod structure"}, "json": {"mod structure"},
}

// languageStems are the scripting-language pages a script file's Guide shows beside its own.
var languageStems = map[string]bool{"effect": true, "trigger": true, "scope": true, "on action": true}

// Attribution is the Guide / Patch Notes footer line.
func Attribution(gameID string) string {
	info := game.Get(gameID)
	name := "Paradox"
	if info != nil && info.ShortName != "" {
		name = info.ShortName
	}
	return "From " + name + " Wiki (" + License + ")"
}

// NamedPages returns the guide pages named for a database or its file's folder, best first.
func NamedPages(gameID, database, rel string) []Page {
	sidecar, err := load(gameID)
	if err != nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range append([]string{database, path.Base(path.Dir(strings.ReplaceAll(rel, "\\", "/")))}, synonyms[database]...) {
		wanted[Stem(name)] = true
	}
	delete(wanted, "")
	var matches []Page
	for _, page := range sidecar.Guides {
		if wanted[Stem(page.Title)] {
			matches = append(matches, page)
		}
	}
	notModding := func(page Page) int {
		if strings.Contains(strings.ToLower(page.Title), "modding") {
			return 0
		}
		return 1
	}
	slices.SortFunc(matches, func(first, second Page) int {
		return cmp.Or(cmp.Compare(notModding(first), notModding(second)),
			cmp.Compare(len(first.Title), len(second.Title)), strings.Compare(first.Title, second.Title))
	})
	return matches
}

// PagesFor returns a file's Guide tabs: the best page named for its database or folder, the
// scripting-language pages for a script file, then the other named pages; none without a sidecar.
func PagesFor(gameID, database, rel string) []Page {
	matches := NamedPages(gameID, database, rel)
	var language []Page
	if sidecar, err := load(gameID); err == nil && synonyms[database] == nil {
		for _, page := range sidecar.Guides {
			named := slices.ContainsFunc(matches, func(match Page) bool { return match.Title == page.Title })
			if languageStems[Stem(page.Title)] && !named {
				language = append(language, page)
			}
		}
	}
	slices.SortFunc(language, func(first, second Page) int { return strings.Compare(first.Title, second.Title) })
	best := min(1, len(matches))
	pages := slices.Concat(matches[:best], language, matches[best:])
	return pages[:min(len(pages), guideTabCap)]
}

func patchEntry(page Page) PatchEntry {
	entry := PatchEntry{Title: page.Title}
	if pv, ok := parsePatchTitle(page.Title); ok {
		entry.Version = pv.Label
		if pv.Patch > 0 || pv.Wildcard {
			entry.Parent = fmt.Sprintf("%d.%d", pv.Major, pv.Minor)
		}
	}
	return entry
}

// Patches returns patch notes entries, newest first.
func Patches(gameID string) PatchList {
	out := PatchList{GameID: gameID}
	sidecar, err := load(gameID)
	if err != nil {
		return out
	}
	type row struct {
		entry PatchEntry
		pv    patchVer
		ok    bool
	}
	rows := make([]row, 0, len(sidecar.Patches))
	for _, page := range sidecar.Patches {
		pv, ok := parsePatchTitle(page.Title)
		rows = append(rows, row{entry: patchEntry(page), pv: pv, ok: ok})
	}
	slices.SortFunc(rows, func(first, second row) int {
		if first.ok && second.ok {
			return -cmpPatch(first.pv, second.pv)
		}
		if first.ok != second.ok {
			if first.ok {
				return -1
			}
			return 1
		}
		return strings.Compare(second.entry.Title, first.entry.Title)
	})
	for _, row := range rows {
		out.Pages = append(out.Pages, row.entry)
	}
	return out
}

// PatchByVersion finds a patch page by title or version label (e.g. "1.16").
func PatchByVersion(gameID, want string) *Page {
	sidecar, err := load(gameID)
	if err != nil {
		return nil
	}
	wantN := NormTitle(want)
	wantN = strings.TrimPrefix(wantN, "patch ")
	var fallback *Page
	for i := range sidecar.Patches {
		page := &sidecar.Patches[i]
		title := NormTitle(page.Title)
		if title == wantN || title == "patch "+wantN {
			return page
		}
		if pv, ok := parsePatchTitle(page.Title); ok && pv.Label == want {
			return page
		}
		if fallback == nil && strings.Contains(title, wantN) {
			fallback = page
		}
	}
	return fallback
}

const (
	// License is the Paradox wiki editorial-text license shown in the UI.
	License   = "CC-BY-SA 3.0"
	htmlCap   = 1_500_000
	memberCap = 500
)

// Guide is the WikiService.Guide payload: the open file's database and its Guide tabs.
type Guide struct {
	Database    string `json:"database"`
	Pages       []Page `json:"pages"`
	Attribution string `json:"attribution"`
}

// PatchEntry is one row in the Patch Notes version tree.
type PatchEntry struct {
	Title   string `json:"title"`
	Version string `json:"version"`
	Parent  string `json:"parent"`
}

// PatchList is the WikiService.Patches payload.
type PatchList struct {
	GameID string       `json:"gameId"`
	Pages  []PatchEntry `json:"pages"`
}

// Section is one heading of a page, for the TOC.
type Section struct {
	TocLevel int    `json:"tocLevel"`
	Line     string `json:"line"`
	Anchor   string `json:"anchor"`
}

// Page is one fetched page as the sidecar keeps it and the panes show it: its sanitized HTML and
// headings, and for a patch note its Modding section apart.
type Page struct {
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	Revid           int       `json:"revid"`
	HTML            string    `json:"html"`
	Sections        []Section `json:"sections,omitempty"`
	ModdingHTML     string    `json:"moddingHtml,omitempty"`
	ModdingSections []Section `json:"moddingSections,omitempty"`
}

// Sidecar is one game's wiki file: its guide pages and its patch notes.
type Sidecar struct {
	Guides  []Page `json:"guides"`
	Patches []Page `json:"patches"`
}

// NormTitle is a page title as titles are compared: lower case, underscores as spaces, single
// spaces.
func NormTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(title, "_", " ")), " "))
}

// Stem is what a page title or a database name is matched by: NormTitle without a trailing
// "modding" and without a plural s.
func Stem(name string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(" "+NormTitle(name), " modding"))
	return strings.TrimSuffix(stem, "s")
}
