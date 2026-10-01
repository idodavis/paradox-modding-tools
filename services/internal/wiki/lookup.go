// lookup.go answers the Guide and Patch Notes queries from a game's sidecar, and holds the
// sidecar's shape on disk and the payloads the RPCs hand back.
package wiki

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/schema"
)

// PagesFor returns the Guide tabs for a file of an extract database at a relative path: the best
// page named for the database or the folder, the scripting-language pages for a script file, then
// the other named pages. A missing sidecar or an unmatched file gives none.
func PagesFor(gameID, database, rel string) []GuidePage {
	sidecar, err := load(gameID)
	if err != nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, name := range append([]string{database, path.Base(path.Dir(strings.ReplaceAll(rel, "\\", "/")))}, synonyms[database]...) {
		wanted[schema.Stem(name)] = true
	}
	delete(wanted, "")
	var matches, language []schema.WikiPage
	for _, page := range sidecar.Guides {
		switch title := schema.Stem(page.Title); {
		case wanted[title]:
			matches = append(matches, page)
		case synonyms[database] == nil && languageStems[title]:
			language = append(language, page)
		}
	}
	notModding := func(page schema.WikiPage) int {
		if strings.Contains(strings.ToLower(page.Title), "modding") {
			return 0
		}
		return 1
	}
	slices.SortFunc(matches, func(first, second schema.WikiPage) int {
		return cmp.Or(cmp.Compare(notModding(first), notModding(second)),
			cmp.Compare(len(first.Title), len(second.Title)), strings.Compare(first.Title, second.Title))
	})
	slices.SortFunc(language, func(first, second schema.WikiPage) int { return strings.Compare(first.Title, second.Title) })
	best := min(1, len(matches))
	var out []GuidePage
	for _, page := range slices.Concat(matches[:best], language, matches[best:]) {
		if len(out) < guideTabCap {
			out = append(out, GuidePage{Title: page.Title, HTML: page.HTML, URL: page.URL, Sections: page.Sections})
		}
	}
	return out
}

func patchEntry(page schema.WikiPage) PatchEntry {
	entry := PatchEntry{Title: page.Title, URL: page.URL, HasModding: page.ModdingHTML != ""}
	if pv, ok := parsePatchTitle(page.Title); ok {
		entry.Version = pv.Label
		if pv.Patch > 0 || pv.Wildcard {
			entry.Parent = itoa2(pv.Major, pv.Minor)
		}
	}
	return entry
}

func itoa2(major, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
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
func PatchByVersion(gameID, want string) *PatchPage {
	sidecar, err := load(gameID)
	if err != nil {
		return nil
	}
	wantN := schema.NormTitle(want)
	wantN = strings.TrimPrefix(wantN, "patch ")
	var fallback *schema.WikiPage
	for i := range sidecar.Patches {
		page := &sidecar.Patches[i]
		n := schema.NormTitle(page.Title)
		if n == wantN || n == "patch "+wantN {
			return patchPageOf(page)
		}
		if pv, ok := parsePatchTitle(page.Title); ok && pv.Label == want {
			return patchPageOf(page)
		}
		if fallback == nil && strings.Contains(n, wantN) {
			fallback = page
		}
	}
	if fallback != nil {
		return patchPageOf(fallback)
	}
	return nil
}

func patchPageOf(page *schema.WikiPage) *PatchPage {
	pv, _ := parsePatchTitle(page.Title)
	return &PatchPage{
		Title:           page.Title,
		Version:         pv.Label,
		HTML:            page.HTML,
		ModdingHTML:     page.ModdingHTML,
		URL:             page.URL,
		HasModding:      page.ModdingHTML != "",
		Sections:        page.Sections,
		ModdingSections: page.ModdingSections,
	}
}

const (
	// License is the Paradox wiki editorial-text license shown in the UI.
	License   = "CC-BY-SA 3.0"
	htmlCap   = 1_500_000
	memberCap = 500
)

// GuidePage is one tab in the Guide pane.
type GuidePage struct {
	Title    string               `json:"title"`
	HTML     string               `json:"html"`
	URL      string               `json:"url"`
	Sections []schema.WikiSection `json:"sections"`
}

// Guide is the WikiService.Guide payload. Rel/origin/database come from Locate.
type Guide struct {
	Rel         string      `json:"rel"`
	Origin      string      `json:"origin"`
	OriginName  string      `json:"originName"`
	Database    string      `json:"database"`
	Pages       []GuidePage `json:"pages"`
	Attribution string      `json:"attribution"`
}

// PatchEntry is one row in the Patch Notes version tree.
type PatchEntry struct {
	Title      string `json:"title"`
	Version    string `json:"version"`
	Parent     string `json:"parent"`
	URL        string `json:"url"`
	HasModding bool   `json:"hasModding"`
}

// PatchList is the WikiService.Patches payload.
type PatchList struct {
	GameID string       `json:"gameId"`
	Pages  []PatchEntry `json:"pages"`
}

// PatchPage is one patch notes body (All + optional Modding HTML).
type PatchPage struct {
	Title           string               `json:"title"`
	Version         string               `json:"version"`
	HTML            string               `json:"html"`
	ModdingHTML     string               `json:"moddingHtml"`
	URL             string               `json:"url"`
	HasModding      bool                 `json:"hasModding"`
	Sections        []schema.WikiSection `json:"sections"`
	ModdingSections []schema.WikiSection `json:"moddingSections"`
}
