// document.go makes the HTML of a document the pages show: a wiki page, a
// digest's article or a release's notes, cut down to text, lists, tables,
// code and links.

package app

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// LinkRule says what a document keeps of a link, given the link's target as
// its source wrote it: the link's attributes, which are its href and the data
// attributes that tell the page where it leads. A link the page cannot follow
// gets none.
type LinkRule func(target string) []html.Attribute

// linkTarget is the attribute that holds where a link leads.
const linkTarget = "href"

// keepWebLink is the link rule of a source with no place of its own in the
// app, as a release's notes: a link inside the document and a web address
// stay, and any other link is dropped.
func keepWebLink(target string) []html.Attribute {
	isWeb := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	if !isWeb && !strings.HasPrefix(target, "#") {
		return nil
	}

	return []html.Attribute{{Key: linkTarget, Val: target}}
}

// element says what a document does with one kind of HTML element. One with
// no row gives way to its children.
type element struct {
	// dropped is true when the element goes with everything inside it.
	dropped    bool
	heading    bool
	attributes []string
}

var (
	anchor = []string{"id"}
	span   = []string{"colspan", "rowspan"}
)

var elements = map[atom.Atom]element{
	atom.H1: {heading: true, attributes: anchor}, atom.H2: {heading: true, attributes: anchor},
	atom.H3: {heading: true, attributes: anchor}, atom.H4: {heading: true, attributes: anchor},
	atom.H5: {heading: true, attributes: anchor}, atom.H6: {heading: true, attributes: anchor},
	atom.P: {}, atom.Ul: {}, atom.Ol: {}, atom.Li: {}, atom.Dl: {}, atom.Dt: {}, atom.Dd: {}, atom.Blockquote: {},
	atom.Table: {}, atom.Caption: {}, atom.Thead: {}, atom.Tbody: {}, atom.Tfoot: {}, atom.Tr: {},
	atom.Th: {attributes: span}, atom.Td: {attributes: span},
	atom.Pre: {}, atom.Code: {}, atom.B: {}, atom.I: {}, atom.Strong: {}, atom.Em: {}, atom.Sup: {}, atom.Sub: {},
	atom.Small: {}, atom.Details: {}, atom.Summary: {}, atom.Br: {}, atom.Hr: {},
	atom.A: {attributes: []string{linkTarget}}, atom.Span: {attributes: anchor},
	atom.Img: {dropped: true}, atom.Figure: {dropped: true}, atom.Iframe: {dropped: true},
	atom.Script: {dropped: true}, atom.Style: {dropped: true}, atom.Noscript: {dropped: true},
	atom.Nav: {dropped: true}, atom.Head: {dropped: true},
}

// markdown reads markdown as GitHub writes it. Each heading gets an id made
// from its text, so that a link inside the document finds it.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// RenderMarkdown returns the document of a markdown text, sanitized as
// Sanitize says. It returns "" for a text that cannot be read.
func RenderMarkdown(text string, rule LinkRule) string {
	var rendered bytes.Buffer
	if err := markdown.Convert([]byte(text), &rendered); err != nil {
		return ""
	}

	return Sanitize(rendered.String(), rule)
}

// Sanitize cuts a source's HTML down to the document the pages show, and
// returns its HTML: no chrome, citations, pictures or reference sections, long
// tables folded, and of each link only what the given rule keeps. It returns
// "" for HTML that cannot be parsed.
func Sanitize(text string, rule LinkRule) string {
	root, err := ParseFragment(text)
	if err != nil {
		return ""
	}

	sanitize(root, rule)
	arrangeSections(root)

	var sanitized strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		_ = html.Render(&sanitized, node)
	}

	return strings.TrimSpace(sanitized.String())
}

var fragmentParent = &html.Node{Type: html.ElementNode, Data: atom.Div.String(), DataAtom: atom.Div}

// ParseFragment parses a piece of HTML, and returns one node that holds it.
func ParseFragment(text string) (*html.Node, error) {
	nodes, err := html.ParseFragment(strings.NewReader(text), fragmentParent)
	if err != nil {
		return nil, err
	}

	root := &html.Node{Type: html.ElementNode}
	for _, node := range nodes {
		root.AppendChild(node)
	}

	return root, nil
}

// openRows is the most rows a table may have and stay unfolded.
const openRows = 2

func sanitize(parent *html.Node, rule LinkRule) {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		switch _, ok := elements[child.DataAtom]; {
		case child.Type == html.TextNode:
			// Text stays as it is.

		case child.Type != html.ElementNode || isDropped(child):
			parent.RemoveChild(child)

		case ok:
			child.Attr = resolveAttributes(child, rule)
			sanitize(child, rule)
			if child.DataAtom == atom.Sup && strings.TrimSpace(TextOf(child)) == "" {
				parent.RemoveChild(child)
			} else if rows := countRows(child); rows > openRows {
				foldTable(child, rows)
			}

		default:
			sanitize(child, rule)
			for inner := child.FirstChild; inner != nil; inner = child.FirstChild {
				child.RemoveChild(inner)
				parent.InsertBefore(inner, child)
			}

			parent.RemoveChild(child)
		}

		child = next
	}
}

var droppedClasses = []string{
	"navbox", "infobox", "eu4box", "thumb", "noprint", "metadata", "reference", "references", "mw-editsection",
}

// isDropped reports whether a document loses an element with everything
// inside it.
func isDropped(node *html.Node) bool {
	id := LookupAttribute(node, "id")
	switch {
	case elements[node.DataAtom].dropped, node.DataAtom == atom.Div && id == "toc":
		return true
	case strings.HasPrefix(id, "cite_note"), strings.HasPrefix(id, "cite_ref"):
		return true
	case node.DataAtom == atom.A && strings.HasSuffix(LookupAttribute(node, linkTarget), "#top"):
		return true
	case node.DataAtom == atom.Div && strings.TrimSpace(TextOf(node)) == "[top]":
		return true
	}

	return slices.ContainsFunc(strings.Fields(LookupAttribute(node, "class")), func(class string) bool {
		return slices.Contains(droppedClasses, class)
	})
}

// countRows returns how many rows a table holds, or 0 for any other element.
func countRows(node *html.Node) int {
	rows := 0
	if node.DataAtom == atom.Table {
		for descendant := range node.Descendants() {
			if descendant.DataAtom == atom.Tr {
				rows++
			}
		}
	}

	return rows
}

// foldTable wraps a table in a closed fold that says how many rows it has.
func foldTable(table *html.Node, count int) {
	fold := &html.Node{Type: html.ElementNode, Data: atom.Details.String(), DataAtom: atom.Details}
	summary := &html.Node{Type: html.ElementNode, Data: atom.Summary.String(), DataAtom: atom.Summary}
	summary.AppendChild(&html.Node{Type: html.TextNode, Data: "Table, " + strconv.Itoa(count) + " rows"})

	table.Parent.InsertBefore(fold, table)
	table.Parent.RemoveChild(table)

	fold.AppendChild(summary)
	fold.AppendChild(table)
}

// The sections a document loses, since the page draws its own contents and
// follows no references, and those it shows last.
var (
	droppedSections = []string{
		"see also", "references", "external links", "further reading", "contents", "table of contents",
	}
	lastSections = []string{"scripting tools"}
)

// arrangeSections drops the sections a document loses and moves those it
// shows last to its end.
func arrangeSections(root *html.Node) {
	nodes := slices.Collect(root.ChildNodes())
	var moved []*html.Node
	for start := 0; start < len(nodes); start++ {
		title := strings.ToLower(strings.TrimSpace(TextOf(nodes[start])))
		level := LevelOf(nodes[start])
		if level == 0 || (!slices.Contains(droppedSections, title) && !slices.Contains(lastSections, title)) {
			continue
		}

		end := start + 1
		for end < len(nodes) && (LevelOf(nodes[end]) == 0 || LevelOf(nodes[end]) > level) {
			end++
		}

		for _, node := range nodes[start:end] {
			root.RemoveChild(node)
		}

		if slices.Contains(lastSections, title) {
			moved = append(moved, nodes[start:end]...)
		}

		start = end - 1
	}

	for _, node := range moved {
		root.AppendChild(node)
	}
}

// LevelOf returns the level of a heading, as 2 for an h2, or 0 for any other
// element.
func LevelOf(node *html.Node) int {
	if !elements[node.DataAtom].heading {
		return 0
	}

	return int(node.Data[1] - '0')
}

// resolveAttributes returns the attributes of an element that a document
// keeps. A link keeps what the given rule gives it.
func resolveAttributes(node *html.Node, rule LinkRule) []html.Attribute {
	row := elements[node.DataAtom]
	var attributes []html.Attribute
	for _, attribute := range node.Attr {
		if !slices.Contains(row.attributes, attribute.Key) {
			continue
		}

		switch attribute.Key {
		case "id":
			if !row.heading && !elements[node.Parent.DataAtom].heading {
				continue
			}

		case "colspan", "rowspan":
			if _, err := strconv.Atoi(attribute.Val); err != nil {
				continue
			}

		case linkTarget:
			attributes = append(attributes, rule(attribute.Val)...)

			continue
		}

		attributes = append(attributes, attribute)
	}

	return attributes
}

// LookupAttribute returns the value of an element's attribute, or "" if it has
// none.
func LookupAttribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}

	return ""
}

// TextOf returns the text an element holds, without its markup.
func TextOf(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}

	var text strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			text.WriteString(descendant.Data)
		}
	}

	return text.String()
}
