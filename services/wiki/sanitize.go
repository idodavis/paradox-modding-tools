package wiki

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// An element says what a sanitized page does with one kind of HTML element. An element with no row gives way to its
// children.
type element struct {
	dropped    bool     // it goes with everything inside it
	heading    bool     // it is a heading
	attributes []string // the attributes it keeps
}

// anchor is the attributes that may carry the target of a link inside the page. A heading or an element directly in
// one keeps them. span is the attributes a table's cell keeps.
var (
	anchor = []string{"id"}
	span   = []string{"colspan", "rowspan"}
)

// elements are the HTML elements a page may hold, by what a sanitized page does with each. Images go with the wiki's
// other chrome, as the article pane shows the text.
var elements = map[atom.Atom]element{
	atom.H1: {heading: true, attributes: anchor}, atom.H2: {heading: true, attributes: anchor},
	atom.H3: {heading: true, attributes: anchor}, atom.H4: {heading: true, attributes: anchor},
	atom.H5: {heading: true, attributes: anchor}, atom.H6: {heading: true, attributes: anchor},
	atom.P: {}, atom.Ul: {}, atom.Ol: {}, atom.Li: {}, atom.Dl: {}, atom.Dt: {}, atom.Dd: {}, atom.Blockquote: {},
	atom.Table: {}, atom.Caption: {}, atom.Thead: {}, atom.Tbody: {}, atom.Tfoot: {}, atom.Tr: {},
	atom.Th: {attributes: span}, atom.Td: {attributes: span},
	atom.Pre: {}, atom.Code: {}, atom.B: {}, atom.I: {}, atom.Strong: {}, atom.Em: {}, atom.Sup: {}, atom.Sub: {},
	atom.Small: {}, atom.Details: {}, atom.Summary: {}, atom.Br: {}, atom.Hr: {},
	atom.A: {attributes: []string{"href"}}, atom.Span: {attributes: anchor},
	atom.Img: {dropped: true}, atom.Figure: {dropped: true}, atom.Iframe: {dropped: true},
	atom.Script: {dropped: true}, atom.Style: {dropped: true}, atom.Noscript: {dropped: true},
	atom.Nav: {dropped: true}, atom.Head: {dropped: true},
}

// droppedClasses are the classes of the wiki's chrome, which a sanitized page loses with everything inside them: its
// navigation and info boxes, pictures, notices, citations and edit links.
var droppedClasses = []string{
	"navbox", "infobox", "eu4box", "thumb", "noprint", "metadata", "reference", "references", "mw-editsection",
}

// The headings of the sections a sanitized page loses, and of those it moves to its end, in lower case.
var (
	droppedSections = []string{"see also", "references", "external links", "further reading"}
	lastSections    = []string{"scripting tools"}
)

// openRows is the most rows a table shows open; a longer one is folded under a line that says how long it is.
const openRows = 2

// fragmentParent is the element a page's HTML is parsed as the content of.
var fragmentParent = &html.Node{Type: html.ElementNode, Data: atom.Div.String(), DataAtom: atom.Div}

// Sanitize strips a wiki page's HTML down to what the article pane shows. Links are made absolute against the given
// origin, the wiki's address. The wiki's chrome, its citations and the sections of links to elsewhere are dropped, the
// scripting tools go last, and a long table is folded. It returns "" if the HTML cannot be parsed.
func Sanitize(text string, origin *url.URL) string {
	root, err := parseFragment(text)
	if err != nil {
		return ""
	}
	sanitize(root, origin)
	arrangeSections(root)
	var sanitized strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		_ = html.Render(&sanitized, node)
	}
	return strings.TrimSpace(sanitized.String())
}

// parseFragment parses a page's HTML into the children of one element. It returns an error if the HTML cannot be
// parsed.
func parseFragment(text string) (*html.Node, error) {
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

// sanitize cuts the children of a node down to what a sanitized page keeps. A dropped element goes with its
// contents, an element with no row gives way to its children, and a kept element has only the attributes it keeps. An
// empty superscript goes, and a long table is folded.
func sanitize(parent *html.Node, origin *url.URL) {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		switch _, kept := elements[child.DataAtom]; {
		case child.Type == html.TextNode:
		case child.Type != html.ElementNode || isDropped(child):
			parent.RemoveChild(child)
		case kept:
			child.Attr = resolveAttributes(child, origin)
			sanitize(child, origin)
			if child.DataAtom == atom.Sup && strings.TrimSpace(textOf(child)) == "" {
				parent.RemoveChild(child)
			} else if rows := countRows(child); rows > openRows {
				foldTable(child, rows)
			}
		default:
			sanitize(child, origin)
			for inner := child.FirstChild; inner != nil; inner = child.FirstChild {
				child.RemoveChild(inner)
				parent.InsertBefore(inner, child)
			}
			parent.RemoveChild(child)
		}
		child = next
	}
}

// isDropped reports whether an element is one that a sanitized page loses with everything inside it: a dropped kind
// of element, the table of contents, a citation, a link back to the top, or an element of the wiki's chrome.
func isDropped(node *html.Node) bool {
	id := lookupAttribute(node, "id")
	switch {
	case elements[node.DataAtom].dropped, node.DataAtom == atom.Div && id == "toc":
		return true
	case strings.HasPrefix(id, "cite_note"), strings.HasPrefix(id, "cite_ref"):
		return true
	case node.DataAtom == atom.A && strings.HasSuffix(lookupAttribute(node, "href"), "#top"):
		return true
	case node.DataAtom == atom.Div && strings.TrimSpace(textOf(node)) == "[top]":
		return true
	}
	return slices.ContainsFunc(strings.Fields(lookupAttribute(node, "class")), func(class string) bool {
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

// foldTable puts a table of the given number of rows in a closed fold, under a line that says how long it is.
func foldTable(table *html.Node, rows int) {
	fold := &html.Node{Type: html.ElementNode, Data: atom.Details.String(), DataAtom: atom.Details}
	summary := &html.Node{Type: html.ElementNode, Data: atom.Summary.String(), DataAtom: atom.Summary}
	summary.AppendChild(&html.Node{Type: html.TextNode, Data: "Table, " + strconv.Itoa(rows) + " rows"})
	table.Parent.InsertBefore(fold, table)
	table.Parent.RemoveChild(table)
	fold.AppendChild(summary)
	fold.AppendChild(table)
}

// arrangeSections drops the sections a sanitized page loses and moves those it shows last to its end. A section is a
// heading of the page's top level and what follows it up to the next heading of its level or higher.
func arrangeSections(root *html.Node) {
	nodes := slices.Collect(root.ChildNodes())
	var moved []*html.Node
	for start := 0; start < len(nodes); start++ {
		title := strings.ToLower(strings.TrimSpace(textOf(nodes[start])))
		if levelOf(nodes[start]) == 0 || !slices.Contains(droppedSections, title) && !slices.Contains(lastSections, title) {
			continue
		}
		end := findSectionEnd(nodes, start)
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

// findSectionEnd returns where the block that starts at the given node ends: a heading runs to the next heading of its
// level or higher, and anything else is one node.
func findSectionEnd(nodes []*html.Node, start int) int {
	level := levelOf(nodes[start])
	end := start + 1
	for level > 0 && end < len(nodes) && (levelOf(nodes[end]) == 0 || levelOf(nodes[end]) > level) {
		end++
	}
	return end
}

// levelOf returns a heading's level, 1 to 6, or 0 for any other node.
func levelOf(node *html.Node) int {
	if !elements[node.DataAtom].heading {
		return 0
	}
	return int(node.Data[1] - '0')
}

// resolveAttributes returns the attributes of an element that a sanitized page keeps. A link is kept with an absolute
// address, and only if that address is on the web. A link to another page of the wiki also gets the page's title as
// data-page. A cell keeps its spans only when they are numbers.
func resolveAttributes(node *html.Node, origin *url.URL) []html.Attribute {
	row := elements[node.DataAtom]
	var kept []html.Attribute
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
		case "href":
			target, ok := resolveLink(attribute.Val, origin)
			if !ok {
				continue
			}
			attribute.Val = target
			if title := resolveTitle(target, origin); title != "" {
				kept = append(kept, html.Attribute{Key: "data-page", Val: title})
			}
		}
		kept = append(kept, attribute)
	}
	return kept
}

// resolveTitle returns the title of the page of the wiki that an absolute address names. It returns "" if the address
// is on another site or names no page.
func resolveTitle(target string, origin *url.URL) string {
	address, err := url.Parse(target)
	if err != nil || address.Host != origin.Host {
		return ""
	}
	title := strings.TrimPrefix(strings.TrimPrefix(address.Path, "/"), "wiki/")
	if title == "index.php" {
		return ""
	}
	return strings.ReplaceAll(title, "_", " ")
}

// resolveLink returns a link's address made absolute against the given origin. A link inside the page stays as it is.
// It returns false if the address is not a web address.
func resolveLink(target string, origin *url.URL) (string, bool) {
	if strings.HasPrefix(target, "#") {
		return target, true
	}
	address, err := url.Parse(target)
	if err != nil {
		return "", false
	}
	address = origin.ResolveReference(address)
	return address.String(), address.Scheme == "http" || address.Scheme == "https"
}

// lookupAttribute returns the value of an element's attribute, or "" if it has none.
func lookupAttribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

// textOf returns the text of a node and of everything under it.
func textOf(node *html.Node) string {
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
