// sanitize.go allowlists wiki HTML and strips chrome via drop tables.
package wiki

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const dumpTableRows = 20

var citeMark = regexp.MustCompile(`^(\[\d+\])+$`)

// Drop / keep tables. Add wiki chrome here instead of a new helper.
var (
	allowed = set(
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
		atom.P, atom.Br, atom.Hr, atom.Blockquote,
		atom.Ul, atom.Ol, atom.Li, atom.Dl, atom.Dt, atom.Dd,
		atom.Table, atom.Thead, atom.Tbody, atom.Tfoot,
		atom.Tr, atom.Th, atom.Td, atom.Caption,
		atom.Pre, atom.Code, atom.A,
		atom.Strong, atom.Em, atom.B, atom.I,
		atom.Sup, atom.Sub, atom.Small,
	)
	dropTag = set(
		atom.Img, atom.Figure, atom.Style, atom.Script,
		atom.Noscript, atom.Link, atom.Meta,
	)
	rank = map[atom.Atom]int{
		atom.H1: 1, atom.H2: 2, atom.H3: 3,
		atom.H4: 4, atom.H5: 5, atom.H6: 6,
	}
	dropClass = set(
		"navbox", "thumb", "infobox", "mw-editsection",
		"noprint", "metadata", "toc", "mw-portlet-toc",
		"sidebar-toc", "reference", "references",
	)
	dropHead = set(
		"contents", "table of contents", "see also",
		"references", "external links", "further reading",
	)
	moveHead    = set("scripting tools")
	keepModding = set("modding", "user modding")
	dropID      = set("toc")
	dropText    = set("[top]")
	dropHash    = set("top")
	citePrefix  = []string{"cite_note", "cite_ref"}
	keepAttr    = map[atom.Atom][]string{
		atom.Td: {"colspan", "rowspan"},
		atom.Th: {"colspan", "rowspan"},
	}
)

func set[K comparable](ks ...K) map[K]bool {
	m := make(map[K]bool, len(ks))
	for _, k := range ks {
		m[k] = true
	}
	return m
}

func lower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func frag(href string) string {
	if _, after, ok := strings.Cut(href, "#"); ok {
		return after
	}
	return ""
}

func prefixed(s string) bool {
	s = lower(s)
	for _, p := range citePrefix {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func drop(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	if dropTag[n.DataAtom] || dropID[lower(attrVal(n, "id"))] || prefixed(attrVal(n, "id")) {
		return true
	}
	for t := range strings.FieldsSeq(attrVal(n, "class")) {
		if dropClass[strings.ToLower(t)] {
			return true
		}
	}
	if n.DataAtom == atom.A || n.DataAtom == atom.Sup {
		t := lower(nodeText(n))
		if dropText[t] || citeMark.MatchString(t) {
			return true
		}
	}
	return n.DataAtom == atom.A && (dropHash[lower(frag(attrVal(n, "href")))] ||
		prefixed(frag(attrVal(n, "href"))))
}

func prune(root *html.Node) {
	if root == nil {
		return
	}
	for c := root.FirstChild; c != nil; {
		next := c.NextSibling
		if drop(c) {
			root.RemoveChild(c)
			c = next
			continue
		}
		prune(c)
		if c.DataAtom == atom.Sup && strings.TrimSpace(nodeText(c)) == "" {
			root.RemoveChild(c)
		}
		c = next
	}
}

func rewriteHref(href, wikiBase string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "data:") {
		return ""
	}
	if strings.HasPrefix(href, "#") {
		return href
	}
	base := strings.TrimRight(wikiBase, "/")
	switch {
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/wiki/"):
		return base + "/" + strings.TrimPrefix(href, "/wiki/")
	case strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//"):
		return base + href
	case strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://"):
		return href
	}
	return ""
}

func countTr(n *html.Node) int {
	if n == nil {
		return 0
	}
	ntr := 0
	if n.Type == html.ElementNode && n.DataAtom == atom.Tr {
		ntr = 1
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		ntr += countTr(c)
	}
	return ntr
}

func copyAttr(dst, n *html.Node, wikiBase string) {
	if headingRank(n) > 0 {
		if id := firstID(n); id != "" {
			dst.Attr = []html.Attribute{{Key: "id", Val: id}}
		}
		return
	}
	if n.DataAtom == atom.A {
		if h := rewriteHref(attrVal(n, "href"), wikiBase); h != "" {
			dst.Attr = []html.Attribute{{Key: "href", Val: h}}
		}
		return
	}
	want := keepAttr[n.DataAtom]
	for _, a := range n.Attr {
		for _, k := range want {
			if a.Key == k {
				dst.Attr = append(dst.Attr, a)
			}
		}
	}
}

func appendAllowed(dst, n *html.Node, wikiBase string) {
	if n == nil || drop(n) {
		return
	}
	switch n.Type {
	case html.TextNode:
		dst.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})
	case html.ElementNode:
		if n.DataAtom == atom.Table && countTr(n) >= dumpTableRows {
			return
		}
		if n.DataAtom == atom.Span || !allowed[n.DataAtom] {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				appendAllowed(dst, c, wikiBase)
			}
			return
		}
		out := &html.Node{Type: html.ElementNode, Data: n.Data, DataAtom: n.DataAtom}
		copyAttr(out, n, wikiBase)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			appendAllowed(out, c, wikiBase)
		}
		dst.AppendChild(out)
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			appendAllowed(dst, c, wikiBase)
		}
	}
}

func renderChildren(n *html.Node) string {
	if n == nil {
		return ""
	}
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&buf, c)
	}
	return strings.TrimSpace(buf.String())
}

func articleRoot(n *html.Node) *html.Node {
	var body, out *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || out != nil {
			return
		}
		if n.Type == html.ElementNode {
			if strings.Contains(attrVal(n, "class"), "mw-parser-output") {
				out = n
				return
			}
			if n.DataAtom == atom.Body {
				body = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	if out != nil {
		return out
	}
	if body != nil {
		return body
	}
	return n
}

// sanitize returns allowlisted HTML (no images) and <pre> snippet texts.
func sanitize(raw, wikiBase string) (clean string, snippets []string) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", nil
	}
	src := articleRoot(doc)
	dst := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	if src != nil {
		for c := src.FirstChild; c != nil; c = c.NextSibling {
			appendAllowed(dst, c, wikiBase)
		}
	}
	reshapeTree(dst)
	return renderChildren(dst), collectPre(dst)
}

// reshape drops leftover wiki chrome and moves Scripting Tools to the end.
func reshape(cleanHTML string) (string, []Section) {
	root := fragmentRoot(cleanHTML)
	if root == nil {
		return "", nil
	}
	reshapeTree(root)
	return renderChildren(root), sectionsFrom(root)
}

func takeChildren(root *html.Node) []*html.Node {
	var kids []*html.Node
	for c := root.FirstChild; c != nil; {
		n := c
		c = c.NextSibling
		root.RemoveChild(n)
		kids = append(kids, n)
	}
	return kids
}

func reshapeTree(root *html.Node) {
	if root == nil {
		return
	}
	kids := takeChildren(root)
	var keep, moved [][]*html.Node
	for i := 0; i < len(kids); {
		n := kids[i]
		r := headingRank(n)
		if r == 0 {
			keep = append(keep, []*html.Node{n})
			i++
			continue
		}
		end := i + 1
		for end < len(kids) {
			if er := headingRank(kids[end]); er > 0 && er <= r {
				break
			}
			end++
		}
		block := kids[i:end]
		line := lower(headingLine(n))
		switch {
		case dropHead[line]:
		case moveHead[line]:
			moved = append(moved, block)
		default:
			keep = append(keep, block)
		}
		i = end
	}
	for _, blocks := range [][][]*html.Node{keep, moved} {
		for _, b := range blocks {
			for _, n := range b {
				root.AppendChild(n)
			}
		}
	}
	prune(root)
}

func sectionsFrom(root *html.Node) []Section {
	if root == nil {
		return nil
	}
	var out []Section
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if r := headingRank(n); r > 0 {
			line := headingLine(n)
			if line == "" {
				return
			}
			id := firstID(n)
			if id == "" {
				id = strings.ReplaceAll(strings.TrimSpace(line), " ", "_")
				n.Attr = append(n.Attr, html.Attribute{Key: "id", Val: id})
			}
			out = append(out, Section{TocLevel: r, Line: line, Anchor: id})
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func collectPre(n *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Pre {
			text := strings.TrimSpace(nodeText(n))
			if text != "" {
				out = append(out, text)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(nodeText(c))
	}
	return b.String()
}

func headingRank(n *html.Node) int {
	if n == nil || n.Type != html.ElementNode {
		return 0
	}
	return rank[n.DataAtom]
}

func headingLine(n *html.Node) string {
	return strings.TrimSpace(nodeText(n))
}

func fragmentRoot(htmlSrc string) *html.Node {
	nodes, err := html.ParseFragment(strings.NewReader(htmlSrc), &html.Node{
		Type:     html.ElementNode,
		Data:     "div",
		DataAtom: atom.Div,
	})
	if err != nil {
		return nil
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	return root
}

// extractModding keeps the Modding / User modding heading and deeper children.
func extractModding(cleanHTML string) string {
	root := fragmentRoot(cleanHTML)
	if root == nil {
		return ""
	}
	dst := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	collecting := false
	headRank := 0
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if r := headingRank(c); r > 0 {
			if !collecting && keepModding[lower(headingLine(c))] {
				collecting = true
				headRank = r
				dst.AppendChild(cloneTree(c))
				continue
			}
			if collecting && r <= headRank {
				break
			}
		}
		if collecting {
			dst.AppendChild(cloneTree(c))
		}
	}
	return renderChildren(dst)
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func firstID(n *html.Node) string {
	if id := attrVal(n, "id"); id != "" {
		return id
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if id := firstID(c); id != "" {
			return id
		}
	}
	return ""
}

func cloneTree(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	out := &html.Node{
		Type:      n.Type,
		Data:      n.Data,
		DataAtom:  n.DataAtom,
		Namespace: n.Namespace,
	}
	if n.Attr != nil {
		out.Attr = append([]html.Attribute(nil), n.Attr...)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out.AppendChild(cloneTree(c))
	}
	return out
}
