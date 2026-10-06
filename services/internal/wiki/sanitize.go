// sanitize.go is what a fetched wiki page keeps: one copy of the page with only the allowed tags
// and attributes and none of the wiki's chrome, the section cuts on that copy's top level, and the
// title filter that decides which pages are worth fetching at all.

package wiki

import (
	"cmp"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const openTableRows = 2 // a table of more rows is kept folded

var (
	dropHead    = map[string]bool{"see also": true, "references": true, "external links": true, "further reading": true}
	moveHead    = map[string]bool{"scripting tools": true}
	keepModding = map[string]bool{"modding": true, "user modding": true}
	dropClass   = map[string]bool{"navbox": true, "infobox": true, "eu4box": true, "thumb": true, "noprint": true, "metadata": true, "reference": true, "references": true}
	dropTag     = map[atom.Atom]bool{atom.Img: true, atom.Figure: true, atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Iframe: true}
	allowed     = map[atom.Atom]bool{}
)

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
	wiki, _ := url.Parse(base)
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for node := range doc.Descendants() {
		if slices.Contains(strings.Fields(attr(node, "class")), "mw-parser-output") {
			copyAllowed(root, node, wiki)
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
	modding = slices.ContainsFunc(nodes, func(node *html.Node) bool {
		return rank(node) > 0 && keepModding[strings.ToLower(strings.TrimSpace(text(node)))]
	})
	return render(nodes), sections, modding
}

// copyAllowed appends a copy of in's children to out that keeps the allowed tags and attributes,
// unwraps every other tag, and leaves out the wiki's chrome and empty superscripts. A table of more
// than two rows is kept folded, closed, under a line that says how long it is.
func copyAllowed(out, in *html.Node, wiki *url.URL) {
	for child := range in.ChildNodes() {
		switch {
		case child.Type == html.TextNode:
			out.AppendChild(&html.Node{Type: html.TextNode, Data: child.Data})
		case child.Type != html.ElementNode || chrome(child):
		case !allowed[child.DataAtom]:
			copyAllowed(out, child, wiki)
		default:
			kept := &html.Node{Type: html.ElementNode, Data: child.Data, DataAtom: child.DataAtom, Attr: attrs(child, wiki)}
			if kept.DataAtom == atom.A && kept.Attr == nil {
				copyAllowed(out, child, wiki)
				continue
			}
			copyAllowed(kept, child, wiki)
			rows := 0
			if kept.DataAtom == atom.Table {
				for descendant := range kept.Descendants() {
					if descendant.DataAtom == atom.Tr {
						rows++
					}
				}
			}
			switch {
			case rows > openTableRows:
				fold := &html.Node{Type: html.ElementNode, Data: "details", DataAtom: atom.Details}
				summary := &html.Node{Type: html.ElementNode, Data: "summary", DataAtom: atom.Summary}
				summary.AppendChild(&html.Node{Type: html.TextNode, Data: "Table, " + strconv.Itoa(rows) + " rows"})
				fold.AppendChild(summary)
				fold.AppendChild(kept)
				out.AppendChild(fold)
			case kept.DataAtom != atom.Sup || strings.TrimSpace(text(kept)) != "":
				out.AppendChild(kept)
			}
		}
	}
}

// chrome reports what a page keeps nowhere: the wiki's navigation, images, citations and the links
// back to the top.
func chrome(node *html.Node) bool {
	id := attr(node, "id")
	return dropTag[node.DataAtom] ||
		slices.ContainsFunc(strings.Fields(attr(node, "class")), func(class string) bool { return dropClass[class] }) ||
		strings.HasPrefix(id, "cite_note") || strings.HasPrefix(id, "cite_ref") ||
		node.DataAtom == atom.A && strings.HasSuffix(attr(node, "href"), "#top")
}

// attrs keeps a link's http or https target made absolute (an in-page #anchor as it is), a
// heading's id (the wiki writes it on a span inside), and a cell's span.
func attrs(node *html.Node, wiki *url.URL) []html.Attribute {
	switch {
	case node.DataAtom == atom.A:
		href := attr(node, "href")
		if strings.HasPrefix(href, "#") {
			return []html.Attribute{{Key: "href", Val: href}}
		}
		if resolved, err := wiki.Parse(href); href != "" && err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
			return []html.Attribute{{Key: "href", Val: resolved.String()}}
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

// blockEnd is where the block that starts at nodes[i] ends: a heading runs to the next heading of
// its rank or higher, anything else is one node.
func blockEnd(nodes []*html.Node, i int) int {
	end := i + 1
	for level := rank(nodes[i]); level > 0 && end < len(nodes) && (rank(nodes[end]) == 0 || rank(nodes[end]) > level); {
		end++
	}
	return end
}

func rank(node *html.Node) int {
	if node.Type != html.ElementNode || len(node.Data) != 2 || node.Data[0] != 'h' || node.Data[1] < '1' || node.Data[1] > '6' {
		return 0
	}
	return int(node.Data[1] - '0')
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func text(node *html.Node) string {
	var out strings.Builder
	for descendant := range node.Descendants() {
		if descendant.Type == html.TextNode {
			out.WriteString(descendant.Data)
		}
	}
	return out.String()
}

func render(nodes []*html.Node) string {
	var out strings.Builder
	for _, node := range nodes {
		html.Render(&out, node)
	}
	return strings.TrimSpace(out.String())
}

var (
	listOfRe  = regexp.MustCompile(`(?i)^List of\b`)
	listEndRe = regexp.MustCompile(`(?i)\blist$`)
	userRe    = regexp.MustCompile(`(?i)^User:`)
)

// denyExact is the titles no pattern below denies.
var denyExact = map[string]bool{"pdx deepl": true, "modding tools": true, "exporters": true}

// denied reports whether an article title is skipped (tools, dumps, user pages).
func denied(title string) bool {
	return denyExact[NormTitle(title)] || userRe.MatchString(title) || listOfRe.MatchString(title) ||
		listEndRe.MatchString(strings.TrimSpace(title))
}

func keepMember(title string, mods map[string]bool) bool {
	page, _, _ := strings.Cut(title, "/") // a mod's subpage (Way of Kings/Religions) goes with it
	return !denied(title) && !mods[NormTitle(page)]
}
