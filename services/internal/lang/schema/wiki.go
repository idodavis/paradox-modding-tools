// wiki.go is the wiki layer: the sidecar's page types, which the wiki package fetches and shows, and
// the read of its guide pages into what the docs miss. A code block is placed by folder and read by
// the doc reader; a table of commands fills the dumps' missing descriptions; a sentence or a table
// saying what decides an event's starting scope gives the folder its root. What cannot be placed is
// unread. The shipped layers are the dumps and the wiki PMT carries for when the user has none.
package schema

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"embed"
	"encoding/gob"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// WikiSection is one heading of a page, for the TOC.
type WikiSection struct {
	TocLevel int    `json:"tocLevel"`
	Line     string `json:"line"`
	Anchor   string `json:"anchor"`
}

// WikiPage is one sanitized wiki page in a sidecar; a patch page also keeps its Modding section.
type WikiPage struct {
	Title           string        `json:"title"`
	URL             string        `json:"url"`
	Revid           int           `json:"revid"`
	HTML            string        `json:"html"`
	Sections        []WikiSection `json:"sections,omitempty"`
	ModdingHTML     string        `json:"moddingHtml,omitempty"`
	ModdingSections []WikiSection `json:"moddingSections,omitempty"`
}

// WikiSidecar is wiki-<gameID>.json: a game's guide pages and patch notes.
type WikiSidecar struct {
	Guides  []WikiPage `json:"guides"`
	Patches []WikiPage `json:"patches"`
}

// Wiki is what the wiki says that the docs miss: its code blocks read as docs, by folder; the
// descriptions of commands and event targets the dumps leave empty; and what decides the starting
// scope of a folder's entries.
type Wiki struct {
	Docs  Docs
	Notes map[string]Note
	Roots map[string]Root
}

// Note is one description the wiki writes, and the page and revision it comes from.
type Note struct {
	Text, Page string
	Revision   int
}

// Root is what decides an entry's starting scope: the value of Key, looked up in Scopes ("" the row
// for an entry that writes no Key).
type Root struct {
	Key    string
	Scopes map[string]string
}

// NormTitle is a title lower-cased and trimmed, _ read as a space.
func NormTitle(text string) string {
	text = strings.ReplaceAll(text, "_", " ")
	return strings.ToLower(strings.TrimSpace(text))
}

// Stem is the database-like core of a title or a name: lower-cased, "_" read as a space, without
// "scripted " before or " guide" / " modding" after, each word's plural s folded; "" for the hub.
func Stem(text string) string {
	text = strings.TrimSuffix(strings.TrimSuffix(NormTitle(text), " guide"), " modding")
	if text == "modding" {
		return ""
	}
	words := strings.Fields(strings.TrimPrefix(text, "scripted "))
	for i, word := range words {
		if len(word) > 2 && !strings.HasSuffix(word, "ss") {
			words[i] = strings.TrimSuffix(word, "s")
		}
	}
	return strings.Join(words, " ")
}

// ReadWiki reads the guide pages of the sidecar at path; a missing or unreadable one is no wiki.
func ReadWiki(path string, scripts *Scripts, dumps *Dumps) (Wiki, []Tally) {
	wiki := Wiki{Docs: Docs{}, Notes: map[string]Note{}, Roots: map[string]Root{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		return wiki, nil
	}
	var sidecar WikiSidecar
	if json.Unmarshal(raw, &sidecar) != nil {
		return wiki, nil
	}

	roots := map[string]bool{}
	for folder := range scripts.Tops {
		first, _, _ := strings.Cut(folder, "/")
		roots[first] = true
	}
	unread := map[string]int{}
	for _, page := range sidecar.Guides {
		reader := &wikiReader{wiki: wiki, page: page, scripts: scripts, dumps: dumps, roots: roots, unread: unread}
		if node, err := html.Parse(strings.NewReader(page.HTML)); err == nil {
			reader.walk(node)
		}
	}

	return wiki, tallies(unread)
}

// wikiReader reads one guide page.
type wikiReader struct {
	wiki    Wiki
	page    WikiPage
	scripts *Scripts
	dumps   *Dumps
	roots   map[string]bool
	unread  map[string]int
	section string // the heading the page is under
}

// walk reads a page's nodes in order: headings, a sentence naming what decides the starting scope,
// code blocks and tables.
func (reader *wikiReader) walk(node *html.Node) {
	if node.Type == html.ElementNode {
		switch node.Data {
		case "h2", "h3":
			reader.section = strings.TrimSpace(text(node))
			return
		case "p":
			reader.sentence(node)
			return
		case "pre":
			reader.code(text(node))
			return
		case "table":
			reader.table(node)
			return
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		reader.walk(child)
	}
}

// place is where the unplaceable is counted: wiki:<page>:<section>.
func (reader *wikiReader) place() string {
	return "wiki:" + reader.page.Title + ":" + reader.section
}

// sentence reads "`<key>` determines the starting scope": the paragraph's first code word is the key
// the page's folder decides its entries' root by.
func (reader *wikiReader) sentence(node *html.Node) {
	said := text(node)
	if !strings.Contains(said, "determines the starting scope") {
		return
	}
	key := ""
	for child := node.FirstChild; child != nil && key == ""; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "code" {
			key = strings.TrimSpace(text(child))
		}
	}
	folder := reader.folderOf(reader.page.Title)
	if key == "" || folder == "" {
		reader.unread[reader.place()]++
		return
	}
	root := reader.wiki.Roots[folder]
	root.Key = key
	reader.wiki.Roots[folder] = root
}

// code places a code block by the folder its section's title names, else the page's title, else the
// one folder whose vanilla keys hold most of its keys, and reads it as a doc there; its keys are the
// wiki's, with the page and revision.
func (reader *wikiReader) code(block string) {
	folder := cmp.Or(reader.folderOf(reader.section), reader.folderOf(reader.page.Title), reader.folderByKeys(block))
	if folder == "" {
		reader.unread[reader.place()]++
		return
	}
	doc := newDocReader(reader.wiki.Docs, folder, reader.place(), reader.roots, reader.scripts, reader.dumps, reader.unread)
	doc.read(block, false)
	stamp(doc.docs.Entry, reader.page)
}

// stamp makes every key the docs reader made from the wiki the wiki's, with its page and revision.
func stamp(key *Key, page WikiPage) {
	for _, child := range key.Keys {
		if child.Page == "" {
			child.Source, child.Page, child.Revision = SourceWiki, page.Title, page.Revid
			for i := range child.Takes {
				child.Takes[i].Source = SourceWiki
			}
		}
		stamp(child, page)
	}
}

// folderOf is the one folder holding script that a title names by its stem, singular or plural.
func (reader *wikiReader) folderOf(title string) string {
	name := strings.ReplaceAll(Stem(title), " ", "_")
	if name == "" {
		return ""
	}
	var found []string
	for folder := range reader.scripts.Tops {
		if last := path.Base(folder); last == name || last == name+"s" || last == strings.TrimSuffix(name, "y")+"ies" {
			found = append(found, folder)
		}
	}
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

// folderByKeys is the one folder whose vanilla keys hold most of a code block's keys; "" where none
// or several do.
func (reader *wikiReader) folderByKeys(block string) string {
	keys := map[string]bool{}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			if assignment, ok := st.(*jomini.Assignment); ok {
				keys[strings.ToLower(assignment.Key.Text)] = true
				if inner := jomini.BlockOf(assignment.Value); inner != nil {
					walk(inner.Statements)
				}
			}
		}
	}
	walk(jomini.Parse(block).Root.Statements)
	best, bestCount, ties := "", 0, 0
	for _, folder := range slices.Sorted(maps.Keys(reader.scripts.Inner)) {
		count := 0
		for key := range keys {
			if reader.scripts.Inner[folder][key] {
				count++
			}
		}
		switch {
		case count > bestCount:
			best, bestCount, ties = folder, count, 0
		case count == bestCount && count > 0:
			ties++
		}
	}
	if ties > 0 {
		return ""
	}
	return best
}

// table reads a table headed Trigger, Effect or Event target with a Description column into the
// descriptions the dumps leave empty, and one headed Event Type then Scope into the starting scope
// each event type gives the page's folder.
func (reader *wikiReader) table(node *html.Node) {
	var rows [][]string
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "tr" {
			var cells []string
			for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
					cells = append(cells, strings.TrimSpace(text(cell)))
				}
			}
			rows = append(rows, cells)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	if len(rows) < 2 || len(rows[0]) < 2 {
		return
	}

	head := rows[0]
	switch {
	case slices.Contains([]string{"Trigger", "Effect", "Event target"}, head[0]) && slices.Contains(head, "Description"):
		described := slices.Index(head, "Description")
		for _, row := range rows[1:] {
			if len(row) <= described {
				continue
			}
			name := strings.ToLower(strings.Fields(row[0] + " ")[0])
			if !reader.declares(name) {
				reader.unread[reader.place()]++
				continue
			}
			if !reader.described(name) {
				reader.wiki.Notes[name] = Note{Text: row[described], Page: reader.page.Title, Revision: reader.page.Revid}
			}
		}
	case head[0] == "Event Type" && head[1] == "Scope":
		folder := reader.folderOf(reader.page.Title)
		if folder == "" {
			reader.unread[reader.place()]++
			return
		}
		root := Root{Key: "type", Scopes: map[string]string{}}
		for _, row := range rows[1:] {
			if len(row) < 2 {
				continue
			}
			scope := strings.ToLower(row[1])
			if !reader.dumps.IsScope(scope) {
				reader.unread[reader.place()]++
				continue
			}
			eventType := strings.ToLower(row[0])
			if eventType == "no type" {
				eventType = ""
			}
			root.Scopes[eventType] = scope
		}
		reader.wiki.Roots[folder] = root
	}
}

// declares reports a command or event target the dumps declare.
func (reader *wikiReader) declares(name string) bool {
	_, target := reader.dumps.EventTargets[name]
	_, data := reader.dumps.DataTargets[name]
	return reader.dumps.declares(name) || target || data
}

// described reports a command or event target the dumps give a description.
func (reader *wikiReader) described(name string) bool {
	return reader.dumps.Effects[name].Doc != "" || reader.dumps.Triggers[name].Doc != "" ||
		reader.dumps.EventTargets[name].Doc != "" || reader.dumps.DataTargets[name].Doc != ""
}

// text is a node's text, its children's joined.
func text(node *html.Node) string {
	var out strings.Builder
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			out.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return out.String()
}

// Shipped is the layers PMT carries for a game: the dumps and the wiki of one game version, used
// where the user has none of their own.
type Shipped struct {
	GameVersion string
	Dumps       *Dumps
	Wiki        Wiki
}

//go:embed shipped
var shipped embed.FS

// ShippedLayers is the game's shipped layers; false where none ship, or the file does not decode.
func ShippedLayers(gameID string) (Shipped, bool) {
	raw, err := shipped.ReadFile("shipped/" + gameID + ".gob.gz")
	if err != nil {
		return Shipped{}, false
	}
	var layers Shipped
	unzipped, err := gzip.NewReader(bytes.NewReader(raw))
	if err == nil {
		err = gob.NewDecoder(unzipped).Decode(&layers)
	}
	if err != nil {
		slog.Warn("the shipped layers do not decode", "game", gameID, "error", err)
		return Shipped{}, false
	}
	return layers, true
}
