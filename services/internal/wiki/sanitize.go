// sanitize.go is what a fetched wiki page keeps: one copy of the page with only the allowed tags
// and attributes and none of the wiki's chrome, and the section cuts on that copy's top level.

package wiki

import (
	"cmp"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const openTableRows = 2 // a table of more rows is kept folded

var (
	dropHead  = map[string]bool{"see also": true, "references": true, "external links": true, "further reading": true}
	moveHead  = map[string]bool{"scripting tools": true}
	dropClass = map[string]bool{"navbox": true, "infobox": true, "eu4box": true, "thumb": true, "noprint": true, "metadata": true, "reference": true, "references": true}
	dropTag   = map[atom.Atom]bool{atom.Img: true, atom.Figure: true, atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Iframe: true}
	allowed   = map[atom.Atom]bool{}
)

// init fills allowed with the tags a page keeps.
func init() {
	for _, tag := range []atom.Atom{atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.P, atom.Br, atom.Hr,
		atom.Blockquote, atom.Ul, atom.Ol, atom.Li, atom.Dl, atom.Dt, atom.Dd, atom.Table, atom.Thead, atom.Tbody,
		atom.Tfoot, atom.Tr, atom.Th, atom.Td, atom.Caption, atom.Pre, atom.Code, atom.A, atom.Strong, atom.Em,
		atom.B, atom.I, atom.Sup, atom.Sub, atom.Small, atom.Details, atom.Summary} {
		allowed[tag] = true
	}
}

// clean returns a page's allowlisted HTML and its headings, and whether it has a Modding section.
func clean(raw, base string) (page string, sections []Section, modding bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", nil, false
	}
	baseURL, _ := url.Parse(base)
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for node := range doc.Descendants() {
		if slices.Contains(strings.Fields(attr(node, "class")), "mw-parser-output") {
			copyAllowed(root, node, baseURL)
			break
		}
	}
	nodes := slices.Collect(root.ChildNodes())
	var kept, moved []*html.Node
	for i := 0; i < len(nodes); {
		end := blockEnd(nodes, i)
		switch line := strings.ToLower(strings.TrimSpace(text(nodes[i]))); {
		case rank(nodes[i]) > 0 && dropHead[line]:
		case rank(nodes[i]) > 0 && moveHead[line]:
			moved = append(moved, nodes[i:end]...)
		default:
			kept = append(kept, nodes[i:end]...)
		}
		i = end
	}
	for _, node := range nodes {
		root.RemoveChild(node)
	}
	nodes = slices.Concat(kept, moved)
	for _, node := range nodes {
		root.AppendChild(node)
	}
	for node := range root.Descendants() {
		line := strings.TrimSpace(text(node))
		if rank(node) == 0 || line == "" {
			continue
		}
		id := cmp.Or(attr(node, "id"), strings.ReplaceAll(line, " ", "_"))
		node.Attr = []html.Attribute{{Key: "id", Val: id}}
		sections = append(sections, Section{TocLevel: rank(node), Line: line, Anchor: id})
	}
	return render(nodes), sections, hasModding(sections)
}

// hasModding is whether a heading among sections, at any depth, names a patch note's modding section: Modding or User
// modding, read by its words alone, since the wiki also writes "User-modding" and leaves zero-width spaces in headings.
func hasModding(sections []Section) bool {
	return slices.ContainsFunc(sections, func(section Section) bool {
		words := strings.Join(strings.FieldsFunc(strings.ToLower(section.Line), func(char rune) bool { return !unicode.IsLetter(char) }), " ")
		return words == "modding" || words == "user modding"
	})
}

// copyAllowed appends a copy of source's children to target that keeps the allowed tags and attributes,
// unwraps every other tag, and leaves out the wiki's chrome and empty superscripts. A table of more
// than two rows is kept folded, closed, under a line that says how long it is.
func copyAllowed(target, source *html.Node, base *url.URL) {
	for child := range source.ChildNodes() {
		switch {
		case child.Type == html.TextNode:
			target.AppendChild(&html.Node{Type: html.TextNode, Data: child.Data})
		case child.Type != html.ElementNode || chrome(child):
		case !allowed[child.DataAtom]:
			copyAllowed(target, child, base)
		default:
			kept := &html.Node{Type: html.ElementNode, Data: child.Data, DataAtom: child.DataAtom, Attr: attrs(child, base)}
			if kept.DataAtom == atom.A && kept.Attr == nil {
				copyAllowed(target, child, base)
				continue
			}
			copyAllowed(kept, child, base)
			switch rows := tableRows(kept); {
			case rows > openTableRows:
				fold := &html.Node{Type: html.ElementNode, Data: "details", DataAtom: atom.Details}
				summary := &html.Node{Type: html.ElementNode, Data: "summary", DataAtom: atom.Summary}
				summary.AppendChild(&html.Node{Type: html.TextNode, Data: "Table, " + strconv.Itoa(rows) + " rows"})
				fold.AppendChild(summary)
				fold.AppendChild(kept)
				target.AppendChild(fold)
			case kept.DataAtom != atom.Sup || strings.TrimSpace(text(kept)) != "":
				target.AppendChild(kept)
			}
		}
	}
}

// tableRows is how many rows a table holds; 0 for any other node.
func tableRows(node *html.Node) int {
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

// chrome reports what a page keeps nowhere: the wiki's navigation, images, citations and the links
// back to the top, with the float that holds each (its spaces would be a blank line).
func chrome(node *html.Node) bool {
	id := attr(node, "id")
	return dropTag[node.DataAtom] ||
		slices.ContainsFunc(strings.Fields(attr(node, "class")), func(class string) bool { return dropClass[class] }) ||
		strings.HasPrefix(id, "cite_note") || strings.HasPrefix(id, "cite_ref") ||
		node.DataAtom == atom.A && strings.HasSuffix(attr(node, "href"), "#top") ||
		node.DataAtom == atom.Div && strings.TrimSpace(text(node)) == "[top]"
}

// attrs keeps a link's http or https target made absolute (an in-page #anchor as it is) and, for a
// page of this wiki, its title (data-title), a heading's id (the wiki writes it on a span inside),
// and a cell's span.
func attrs(node *html.Node, base *url.URL) []html.Attribute {
	switch {
	case node.DataAtom == atom.A:
		href := attr(node, "href")
		if strings.HasPrefix(href, "#") {
			return []html.Attribute{{Key: "href", Val: href}}
		}
		if resolved, err := base.Parse(href); href != "" && err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			kept := []html.Attribute{{Key: "href", Val: resolved.String()}}
			if title := path.Base(resolved.Path); resolved.Host == base.Host && !slices.Contains([]string{"/", ".", "wiki", "index.php"}, title) {
				kept = append(kept, html.Attribute{Key: "data-title", Val: strings.ReplaceAll(title, "_", " ")})
			}
			return kept
		}
	case rank(node) > 0:
		id := attr(node, "id")
		for descendant := range node.Descendants() {
			id = cmp.Or(id, attr(descendant, "id"))
		}
		if id != "" {
			return []html.Attribute{{Key: "id", Val: id}}
		}
	case node.DataAtom == atom.Td || node.DataAtom == atom.Th:
		var kept []html.Attribute
		for _, attribute := range node.Attr {
			if _, err := strconv.Atoi(attribute.Val); err == nil && (attribute.Key == "colspan" || attribute.Key == "rowspan") {
				kept = append(kept, attribute)
			}
		}
		return kept
	}
	return nil
}

// blockEnd is where the block that starts at nodes[start] ends: a heading runs to the next heading of
// its rank or higher, anything else is one node.
func blockEnd(nodes []*html.Node, start int) int {
	end := start + 1
	for level := rank(nodes[start]); level > 0 && end < len(nodes) && (rank(nodes[end]) == 0 || rank(nodes[end]) > level); {
		end++
	}
	return end
}

// rank is a heading element's level, 1 to 6; 0 for any other node.
func rank(node *html.Node) int {
	if node.Type != html.ElementNode || len(node.Data) != 2 || node.Data[0] != 'h' || node.Data[1] < '1' || node.Data[1] > '6' {
		return 0
	}
	return int(node.Data[1] - '0')
}

// attr is the value of node's attribute key; "" where it has none.
func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

// text is the text of node and everything under it.
func text(node *html.Node) string {
	var out strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			out.WriteString(descendant.Data)
		}
	}
	return out.String()
}

// render is the HTML text of nodes, trimmed.
func render(nodes []*html.Node) string {
	var out strings.Builder
	for _, node := range nodes {
		_ = html.Render(&out, node) // a strings.Builder never fails
	}
	return strings.TrimSpace(out.String())
}
