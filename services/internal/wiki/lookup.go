// lookup.go answers the Guide and Patch Notes queries from a game's sidecar, and holds the sidecar's shape on disk
// and the payloads the RPCs hand back.

package wiki

import (
	"cmp"
	stdhtml "html"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"paradox-modding-tools/services/internal/game"
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

// hotfix is one hotfix of a .X page: the part of the page under a heading that names a version.
type hotfix struct {
	version  string
	html     string
	sections []Section
}

// patchVer is a parsed "Patch 1.16.2" / "Patch 1.16.X" title: the version as written, its minor version
// (1.16), and whether it is a hotfix or, written X, a page of them.
type patchVer struct {
	Label, Minor     string
	Hotfix, Wildcard bool
}

const (
	// License is the Paradox wiki editorial-text license shown in the UI.
	License     = "CC-BY-SA 3.0"
	guideTabCap = 6 // the most tabs a file's Guide shows
)

// synonyms are the page stems a file no script database holds also matches, by its extension.
var synonyms = map[string][]string{
	"gui": {"interface", "gui script"}, "yml": {"localization", "customizable localization"},
	"mod": {"mod structure"}, "json": {"mod structure"},
}

// languageStems are the scripting-language pages a script file's Guide shows beside its own.
var languageStems = map[string]bool{"effect": true, "trigger": true, "scope": true, "on action": true}

// a patch note's first sentence names it and dates it (`Patch 1.20, aka "Crozier", was released on 2026-09-30`),
// read with its tags taken out
var (
	tagRe      = regexp.MustCompile(`<[^>]+>`)
	akaRe      = regexp.MustCompile(`aka\s+"([^"]+)"`)
	releasedRe = regexp.MustCompile(`released on (?:the )?(\d{4}-\d{2}-\d{2})`)
)

// versionRe is the heading anchor of a hotfix (1.16.2); patchTitleRe is the title of a patch page (Patch 1.16.X).
var (
	versionRe    = regexp.MustCompile(`^\d+\.\d+(\.\d+)+$`)
	patchTitleRe = regexp.MustCompile(`(?i)^Patch\s+(\d+\.\d+(?:\.(?:\d+|[Xx]))?)`)
)

// PagesFor returns a file's Guide tabs: the best page named for its database or folder, the
// scripting-language pages for a script file, then the other named pages; none without a sidecar.
func (client *Client) PagesFor(gameID, database, rel string) []Page {
	matches := client.NamedPages(gameID, database, rel)
	var language []Page
	if sidecar, err := client.load(gameID); err == nil && synonyms[database] == nil {
		for _, page := range sidecar.Guides {
			matched := slices.ContainsFunc(matches, func(match Page) bool { return match.Title == page.Title })
			if languageStems[Stem(page.Title)] && !matched {
				language = append(language, page)
			}
		}
	}
	slices.SortFunc(language, func(first, second Page) int { return strings.Compare(first.Title, second.Title) })
	best := min(1, len(matches))
	pages := slices.Concat(matches[:best], language, matches[best:])
	return pages[:min(len(pages), guideTabCap)]
}

// NamedPages returns the guide pages named for a database or its file's folder, best first.
func (client *Client) NamedPages(gameID, database, rel string) []Page {
	sidecar, err := client.load(gameID)
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

// GuidePage is a guide page by its title; nil where the game's wiki has none so named.
func (client *Client) GuidePage(gameID, title string) *Page {
	sidecar, err := client.load(gameID)
	if err != nil {
		return nil
	}
	for _, page := range sidecar.Guides {
		if page.Title == title {
			return &page
		}
	}
	return nil
}

// Attribution is the Guide / Patch Notes footer line.
func Attribution(gameID string) string {
	info := game.Get(gameID)
	name := "Paradox"
	if info != nil && info.ShortName != "" {
		name = info.ShortName
	}
	return "From " + name + " Wiki (" + License + ")"
}

// Patches returns patch notes entries by minor version, newest first: each version's own note, then
// its hotfixes, oldest first, a .X page's hotfixes each its own entry. The newest is Latest.
func (client *Client) Patches(gameID string) PatchList {
	out := PatchList{GameID: gameID}
	sidecar, err := client.load(gameID)
	if err != nil {
		return out
	}
	// a refresh saves the sidecar whole, so its time is the last fetch
	if info, err := os.Stat(client.SidecarPath(gameID)); err == nil {
		out.Fetched = info.ModTime().UTC().Format(time.RFC3339)
	}
	for _, page := range sidecar.Patches {
		version, ok := parsePatchTitle(page.Title)
		fixes := hotfixes(page)
		if !ok || !version.Wildcard || len(fixes) == 0 {
			out.Pages = append(out.Pages, patchEntry(page))
			continue
		}
		for _, fix := range fixes {
			_, released := releaseLine(fix.html)
			out.Pages = append(out.Pages, PatchEntry{
				Title: "Patch " + fix.version, Version: fix.version, Parent: version.Minor, Modding: hasModding(fix.sections), Released: released,
			})
		}
	}
	// by minor version newest first, a version before its hotfixes; a page no version names goes last
	slices.SortStableFunc(out.Pages, func(first, second PatchEntry) int {
		firstParts, secondParts := versionParts(first.Version), versionParts(second.Version)
		if len(firstParts) < 2 || len(secondParts) < 2 {
			return cmp.Compare(len(secondParts), len(firstParts))
		}
		return cmp.Or(-slices.Compare(firstParts[:2], secondParts[:2]), slices.Compare(firstParts[2:], secondParts[2:]))
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

// patchEntry is one patch page's row of the version tree: its title, code name and release date, its version, the
// version it is a hotfix of, and whether it has a Modding section.
func patchEntry(page Page) PatchEntry {
	// judged from the headings kept, so a page fetched before the rule changed is judged by today's
	entry := PatchEntry{Title: page.Title, Modding: hasModding(page.Sections)}
	entry.Name, entry.Released = releaseLine(page.HTML)
	if version, ok := parsePatchTitle(page.Title); ok {
		entry.Version = version.Label
		if version.Hotfix {
			entry.Parent = version.Minor
		}
	}
	return entry
}

// releaseLine is a note's name and release date, read from its opening text; empty where it gives none.
func releaseLine(pageHTML string) (name, released string) {
	text := stdhtml.UnescapeString(tagRe.ReplaceAllString(pageHTML[:min(len(pageHTML), 2000)], " "))
	if match := akaRe.FindStringSubmatch(text); match != nil {
		name = match[1]
	}
	if match := releasedRe.FindStringSubmatch(text); match != nil {
		released = match[1]
	}
	return name, released
}

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

// parsePatchTitle parses a page title that names a patch; false for any other title.
func parsePatchTitle(title string) (patchVer, bool) {
	match := patchTitleRe.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return patchVer{}, false
	}
	fields, numbers := strings.Split(match[1], "."), versionParts(match[1])
	wildcard := len(fields) > 2 && strings.EqualFold(fields[2], "x")
	return patchVer{
		Label: match[1], Minor: fields[0] + "." + fields[1],
		Hotfix: wildcard || len(numbers) > 2 && numbers[2] > 0, Wildcard: wildcard,
	}, true
}

// versionParts is a version's numbers; a .X page's are its major and minor.
func versionParts(label string) []int {
	var parts []int
	for part := range strings.SplitSeq(label, ".") {
		if number, err := strconv.Atoi(part); err == nil {
			parts = append(parts, number)
		}
	}
	return parts
}

// PatchByVersion finds a patch page by title or version label (e.g. "1.16"), or a hotfix of a .X
// page as a page of its own.
func (client *Client) PatchByVersion(gameID, want string) *Page {
	sidecar, err := client.load(gameID)
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
		if version, ok := parsePatchTitle(page.Title); ok && version.Label == want {
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

// ScriptExamples returns the script examples the wiki's pages hold for each of the named databases: the text
// of every <pre> block on the pages NamedPages picks for the database. The time is the sidecar's, zero when
// the game has no sidecar. The engine reads these as a source in trust order; it never sees the HTML.
func (client *Client) ScriptExamples(gameID string, databases []string) (map[string][]string, time.Time, error) {
	info, err := os.Stat(client.SidecarPath(gameID))
	if err != nil {
		return nil, time.Time{}, nil // no sidecar: no examples, and the wiki source is "none"
	}
	examples := map[string][]string{}
	for _, database := range databases {
		for _, page := range client.NamedPages(gameID, database, "") {
			examples[database] = append(examples[database], preBlocks(page.HTML)...)
		}
	}
	return examples, info.ModTime(), nil
}

// preBlocks returns the text of every <pre> element of a page, in document order.
func preBlocks(pageHTML string) []string {
	document, err := html.Parse(strings.NewReader(pageHTML))
	if err != nil {
		return nil
	}
	var blocks []string
	for node := range document.Descendants() {
		if node.DataAtom != atom.Pre {
			continue
		}
		var text strings.Builder
		for inner := range node.Descendants() {
			if inner.Type == html.TextNode {
				text.WriteString(inner.Data)
			}
		}
		blocks = append(blocks, text.String())
	}
	return blocks
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
