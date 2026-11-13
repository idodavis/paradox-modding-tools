// sanitize.go cuts a wiki page's HTML down to what the pane shows.

package wiki

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// element says what a sanitized page does with one kind of HTML element. One
// with no row gives way to its children.
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
	atom.A: {attributes: []string{"href"}}, atom.Span: {attributes: anchor},
	atom.Img: {dropped: true}, atom.Figure: {dropped: true}, atom.Iframe: {dropped: true},
	atom.Script: {dropped: true}, atom.Style: {dropped: true}, atom.Noscript: {dropped: true},
	atom.Nav: {dropped: true}, atom.Head: {dropped: true},
}

// sanitizePage cuts a parsed wiki page down to what the pane shows and
// returns its HTML: no chrome, citations, pictures or reference sections, long
// tables folded, and only web links, made absolute.
func sanitizePage(root *html.Node, origin *url.URL) string {
	sanitize(root, origin)
	arrangeSections(root)
	var sanitized strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		_ = html.Render(&sanitized, node)
	}

	return strings.TrimSpace(sanitized.String())
}

var fragmentParent = &html.Node{Type: html.ElementNode, Data: atom.Div.String(), DataAtom: atom.Div}

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

// openRows is the most rows a table may have and stay unfolded.
const openRows = 2

func sanitize(parent *html.Node, origin *url.URL) {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		switch _, kept := elements[child.DataAtom]; {
		case child.Type == html.TextNode:
			// Text stays as it is.

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

var droppedClasses = []string{
	"navbox", "infobox", "eu4box", "thumb", "noprint", "metadata", "reference", "references", "mw-editsection",
}

// isDropped reports whether a page loses an element with everything inside it.
func isDropped(node *html.Node) bool {
	id := LookupAttribute(node, "id")
	switch {
	case elements[node.DataAtom].dropped, node.DataAtom == atom.Div && id == "toc":
		return true
	case strings.HasPrefix(id, "cite_note"), strings.HasPrefix(id, "cite_ref"):
		return true
	case node.DataAtom == atom.A && strings.HasSuffix(LookupAttribute(node, "href"), "#top"):
		return true
	case node.DataAtom == atom.Div && strings.TrimSpace(textOf(node)) == "[top]":
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

var (
	droppedSections = []string{"see also", "references", "external links", "further reading"}
	lastSections    = []string{"scripting tools"}
)

// arrangeSections drops the sections a page loses and moves those it shows last
// to its end.
func arrangeSections(root *html.Node) {
	nodes := slices.Collect(root.ChildNodes())
	var moved []*html.Node
	for start := 0; start < len(nodes); start++ {
		title := strings.ToLower(strings.TrimSpace(textOf(nodes[start])))
		level := levelOf(nodes[start])
		if level == 0 || (!slices.Contains(droppedSections, title) && !slices.Contains(lastSections, title)) {
			continue
		}

		end := start + 1
		for end < len(nodes) && (levelOf(nodes[end]) == 0 || levelOf(nodes[end]) > level) {
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

func levelOf(node *html.Node) int {
	if !elements[node.DataAtom].heading {
		return 0
	}

	return int(node.Data[1] - '0')
}

// resolveAttributes returns the attributes of an element that a page keeps. A
// link keeps only a web address, made absolute, and a link to another wiki page
// also gets its title as data-page.
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

// resolveTitle returns the title of the wiki page an address names, or "" for
// another site or no page.
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

// resolveLink makes a link absolute, or returns false if it is not a web
// address. A link inside the page stays as it is.
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
