package wiki

import (
	"net/url"
	"slices"
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
// one keeps them.
var anchor = []string{"id"}

// elements are the HTML elements a page may hold, by what a sanitized page does with each.
var elements = map[atom.Atom]element{
	atom.H1: {heading: true, attributes: anchor}, atom.H2: {heading: true, attributes: anchor},
	atom.H3: {heading: true, attributes: anchor}, atom.H4: {heading: true, attributes: anchor},
	atom.P: {}, atom.Ul: {}, atom.Ol: {}, atom.Li: {}, atom.Table: {}, atom.Thead: {}, atom.Tbody: {}, atom.Tr: {},
	atom.Th: {}, atom.Td: {}, atom.Pre: {}, atom.Code: {}, atom.B: {}, atom.I: {}, atom.Strong: {}, atom.Em: {},
	atom.A: {attributes: []string{"href"}}, atom.Img: {attributes: []string{"src", "alt"}},
	atom.Br: {}, atom.Hr: {}, atom.Div: {}, atom.Span: {attributes: anchor},
	atom.Script: {dropped: true}, atom.Style: {dropped: true}, atom.Noscript: {dropped: true},
	atom.Nav: {dropped: true}, atom.Head: {dropped: true},
}

// droppedClasses are the classes of the navigation boxes and the edit links, which a sanitized page loses with
// everything inside them.
var droppedClasses = []string{"navbox", "mw-editsection"}

// fragmentParent is the element a page's HTML is parsed as the content of.
var fragmentParent = &html.Node{Type: html.ElementNode, Data: atom.Div.String(), DataAtom: atom.Div}

// Sanitize strips a wiki page's HTML down to what the article pane shows. Links and images are made absolute against
// the given origin, the wiki's address. Scripts, styles, navigation and edit links are dropped. It returns "" if the
// HTML cannot be parsed.
func Sanitize(text string, origin *url.URL) string {
	root, err := parseFragment(text)
	if err != nil {
		return ""
	}
	sanitize(root, origin)
	var sanitized strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		_ = html.Render(&sanitized, node)
	}
	return sanitized.String()
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
// contents, an element with no row gives way to its children, and a kept element has only the attributes it keeps.
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

// isDropped reports whether an element is one that a sanitized page loses with everything inside it: a script, a
// style, a navigation box, an edit link or the table of contents.
func isDropped(node *html.Node) bool {
	if elements[node.DataAtom].dropped || (node.DataAtom == atom.Div && lookupAttribute(node, "id") == "toc") {
		return true
	}
	return slices.ContainsFunc(strings.Fields(lookupAttribute(node, "class")), func(class string) bool {
		return slices.Contains(droppedClasses, class)
	})
}

// resolveAttributes returns the attributes of an element that a sanitized page keeps. A link or an image is kept
// with an absolute address, and only if that address is on the web.
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
		case "href", "src":
			target, ok := resolveLink(attribute.Val, origin)
			if !ok {
				continue
			}
			attribute.Val = target
		}
		kept = append(kept, attribute)
	}
	return kept
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

// writeText writes the text of a node and of everything under it to the given builder.
func writeText(plain *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		plain.WriteString(node.Data)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeText(plain, child)
	}
}
