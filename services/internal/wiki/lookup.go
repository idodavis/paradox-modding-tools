// lookup.go answers the Guide and Patch Notes queries from a game's sidecar, and holds the
// sidecar's shape on disk and the payloads the RPCs hand back.

package wiki

import (
	"cmp"
	stdhtml "html"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

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

// a patch note's first sentence names it and dates it (`Patch 1.20, aka "Crozier", was released on 2026-09-30`),
// read with its tags taken out
var (
	tagRe      = regexp.MustCompile(`<[^>]+>`)
	akaRe      = regexp.MustCompile(`aka\s+"([^"]+)"`)
	releasedRe = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)
)

// said is a note's name and release date, read from its opening text; empty where it gives none.
func said(html string) (name, released string) {
	text := stdhtml.UnescapeString(tagRe.ReplaceAllString(html[:min(len(html), 2000)], " "))
	if match := akaRe.FindStringSubmatch(text); match != nil {
		name = match[1]
	}
	if match := releasedRe.FindStringSubmatch(text); match != nil {
		released = match[1]
	}
	return name, released
}

func patchEntry(page Page) PatchEntry {
	// judged from the headings kept, so a page fetched before the rule changed is judged by today's
	entry := PatchEntry{Title: page.Title, Modding: hasModding(page.Sections)}
	entry.Name, entry.Released = said(page.HTML)
	if pv, ok := parsePatchTitle(page.Title); ok {
		entry.Version = pv.Label
		if pv.Hotfix {
			entry.Parent = pv.Minor
		}
	}
	return entry
}

// hotfix is one hotfix of a .X page: the part of the page under a heading that names a version.
type hotfix struct {
	version  string
	html     string
	sections []Section
}

var versionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)+$`)

// hotfixes are a .X page's hotfixes, in the page's order; none when no heading names a version.
func hotfixes(page Page) (fixes []hotfix) {
	for i, section := range page.Sections {
		if section.TocLevel != 2 || !versionRe.MatchString(section.Anchor) {
			continue
		}
		open := strings.Index(page.HTML, `<h2 id="`+section.Anchor+`"`)
		start := strings.Index(page.HTML[max(open, 0):], "</h2>")
		if open < 0 || start < 0 {
			continue
		}
		body := page.HTML[open+start+len("</h2>"):]
		if next := strings.Index(body, "<h2"); next >= 0 {
			body = body[:next]
		}
		fix := hotfix{version: section.Anchor, html: strings.TrimSpace(body)}
		for _, sub := range page.Sections[i+1:] {
			if sub.TocLevel <= 2 {
				break
			}
			fix.sections = append(fix.sections, sub)
		}
		fixes = append(fixes, fix)
	}
	return fixes
}

// Patches returns patch notes entries by minor version, newest first: each version's own note, then
// its hotfixes, oldest first, a .X page's hotfixes each its own entry. The newest is Latest.
func Patches(gameID string) PatchList {
	out := PatchList{GameID: gameID}
	sidecar, err := load(gameID)
	if err != nil {
		return out
	}
	// a refresh saves the sidecar whole, so its time is the last fetch
	if path, err := SidecarPath(gameID); err == nil {
		if info, err := os.Stat(path); err == nil {
			out.Fetched = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	for _, page := range sidecar.Patches {
		pv, ok := parsePatchTitle(page.Title)
		fixes := hotfixes(page)
		if !ok || !pv.Wildcard || len(fixes) == 0 {
			out.Pages = append(out.Pages, patchEntry(page))
			continue
		}
		for _, fix := range fixes {
			_, released := said(fix.html)
			out.Pages = append(out.Pages, PatchEntry{
				Title: "Patch " + fix.version, Version: fix.version, Parent: pv.Minor, Modding: hasModding(fix.sections), Released: released,
			})
		}
	}
	// by minor version newest first, a version before its hotfixes; a page no version names goes last
	slices.SortStableFunc(out.Pages, func(first, second PatchEntry) int {
		a, b := versionParts(first.Version), versionParts(second.Version)
		if len(a) < 2 || len(b) < 2 {
			return cmp.Compare(len(b), len(a))
		}
		return cmp.Or(-slices.Compare(a[:2], b[:2]), slices.Compare(a[2:], b[2:]))
	})
	latest := -1
	for i, entry := range out.Pages {
		if parts := versionParts(entry.Version); len(parts) >= 2 && (latest < 0 || slices.Compare(parts, versionParts(out.Pages[latest].Version)) > 0) {
			latest = i
		}
	}
	if latest >= 0 {
		out.Pages[latest].Latest = true
	}
	return out
}

// PatchByVersion finds a patch page by title or version label (e.g. "1.16"), or a hotfix of a .X
// page as a page of its own.
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
		for _, fix := range hotfixes(*page) {
			if fix.version == wantN {
				return &Page{Title: "Patch " + fix.version, URL: page.URL + "#" + fix.version, Revid: page.Revid, HTML: fix.html, Sections: fix.sections}
			}
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
	Kind        string `json:"kind"` // what one of its database is called (game.KindName)
	Pages       []Page `json:"pages"`
	Attribution string `json:"attribution"`
}

// PatchEntry is one row in the Patch Notes version tree.
type PatchEntry struct {
	Title    string `json:"title"`
	Name     string `json:"name,omitempty"`     // its code name: Crozier
	Released string `json:"released,omitempty"` // its release date, 2026-09-30, as its note says
	Version  string `json:"version"`
	Parent   string `json:"parent"`           // the version a hotfix belongs to, as 1.14
	Modding  bool   `json:"modding"`          // the page has a Modding section
	Latest   bool   `json:"latest,omitempty"` // the newest patch
}

// PatchList is the WikiService.Patches payload.
type PatchList struct {
	GameID  string       `json:"gameId"`
	Pages   []PatchEntry `json:"pages"`
	Fetched string       `json:"fetched,omitempty"` // when the game's pages were last fetched (RFC 3339)
}

// Section is one heading of a page, for the TOC.
type Section struct {
	TocLevel int    `json:"tocLevel"`
	Line     string `json:"line"`
	Anchor   string `json:"anchor"`
}

// Page is one fetched page as the sidecar keeps it and the panes show it: its sanitized HTML and
// headings, and for a patch note whether it has a Modding section.
type Page struct {
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Revid    int       `json:"revid"`
	HTML     string    `json:"html"`
	Sections []Section `json:"sections,omitempty"`
	Modding  bool      `json:"modding,omitempty"`
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
