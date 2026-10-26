package steam

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// steamElements are the BBCode tags Steam draws, with the HTML element each is drawn as.
var steamElements = map[string]struct {
	element string
	block   bool     // stands on lines of its own
	address bool     // its argument is a link's address
	aliases []string // the other elements the editor writes for it
}{
	"h1": {"h1", true, false, nil}, "h2": {"h2", true, false, nil}, "h3": {"h3", true, false, nil},
	"quote": {"blockquote", true, false, nil}, "hr": {"hr", true, false, nil}, "list": {"ul", true, false, nil},
	"olist": {"ol", true, false, nil}, "*": {"li", true, false, nil}, "table": {"table", true, false, nil},
	"tr": {"tr", true, false, nil}, "th": {"th", true, false, nil}, "td": {"td", true, false, nil},
	"b": {"b", false, false, []string{"strong"}}, "i": {"i", false, false, []string{"em"}}, "u": {"u", false, false, nil},
	"strike": {"s", false, false, []string{"del", "strike"}}, "spoiler": {"mark", false, false, nil},
	"url": {"a", false, true, nil},
}

// steamLiterals are the BBCode tags whose content Steam shows as written, with no tag read inside.
var steamLiterals = []struct {
	tag     string
	element string // "" for noparse, which has none
	block   bool
	pattern *regexp.Regexp
	html    [2]string
}{
	{"noparse", "", false, regexp.MustCompile(`(?is)\[noparse\](.*?)\[/noparse\]`), [2]string{"", ""}},
	{"code", "pre", true, regexp.MustCompile(`(?is)(?:\r?\n)?\[code\](?:\r?\n)?(.*?)(?:\r?\n)?\[/code\](?:\r?\n)?`),
		[2]string{"<pre>", "</pre>"}},
	{"img", "img", false, regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`), [2]string{`<img alt="" src="`, `">`}},
}

// bbcodeTag matches one BBCode tag, opening or closing, with its argument.
var bbcodeTag = regexp.MustCompile(`(?i)\[(/?)([a-z0-9*]+)(?:[= ]([^\]]*))?\]`)

// listEnds are the tags that end a list item, which has no closing tag.
var listEnds = []string{"*", "/list", "/olist"}

// webAddress matches a web page's address, the only kind of link ToHTML draws.
var webAddress = regexp.MustCompile(`(?i)^https?://`)

// ToBBCode returns the visual editor's HTML as Steam's BBCode. A tag typed as text is kept in noparse, so Steam shows
// it as written. It returns "" if the HTML cannot be read.
func (stm *Service) ToBBCode(markup string) string {
	document, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return ""
	}
	var bbcode strings.Builder
	writeChildren(&bbcode, document)
	return strings.TrimRight(bbcode.String(), "\n")
}

// writeChildren writes the children of an HTML node as BBCode.
func writeChildren(bbcode *strings.Builder, parent *html.Node) {
	for node := range parent.ChildNodes() {
		writeNode(bbcode, node)
	}
}

// writeNode writes one HTML node as BBCode. An HTML comment writes nothing, and so does an empty paragraph that is
// alone in its parent, such as an empty table cell's.
func writeNode(bbcode *strings.Builder, node *html.Node) {
	switch {
	case node.Type == html.TextNode:
		writeText(bbcode, node.Data)
	case node.Type != html.ElementNode:
		// a comment or a doctype writes nothing
	case node.DataAtom == atom.Br:
		bbcode.WriteString("\n")
	case node.DataAtom == atom.P:
		if node.PrevSibling != nil {
			writeBreak(bbcode)
		}
		// an empty paragraph between others is a blank line
		if node.FirstChild == nil && (node.PrevSibling != nil || node.NextSibling != nil) {
			bbcode.WriteString("\n")
		}
		writeChildren(bbcode, node)
	default:
		writeElement(bbcode, node)
	}
}

// writeText writes text as BBCode, with each tag Steam would draw in it kept in noparse.
func writeText(bbcode *strings.Builder, text string) {
	bbcode.WriteString(bbcodeTag.ReplaceAllStringFunc(text, func(match string) string {
		if !isDrawnTag(strings.ToLower(bbcodeTag.FindStringSubmatch(match)[2])) {
			return match
		}
		return "[noparse]" + match + "[/noparse]"
	}))
}

// isDrawnTag reports whether Steam draws a BBCode tag, named in lower case.
func isDrawnTag(name string) bool {
	if _, ok := steamElements[name]; ok {
		return true
	}
	for _, row := range steamLiterals {
		if row.tag == name {
			return true
		}
	}
	return false
}

// writeBreak ends the BBCode's current line, unless no line is open.
func writeBreak(bbcode *strings.Builder) {
	if text := bbcode.String(); text != "" && !strings.HasSuffix(text, "\n") {
		bbcode.WriteString("\n")
	}
}

// writeElement writes an HTML element as the BBCode tag it is drawn for, with its contents. An element with no tag
// writes its contents alone.
func writeElement(bbcode *strings.Builder, node *html.Node) {
	tag, block, literal, ok := lookupTag(node.Data)
	if !ok {
		writeChildren(bbcode, node)
		return
	}
	if block {
		writeBreak(bbcode)
	}
	fmt.Fprintf(bbcode, "[%s", tag)
	if argument := cmp.Or(lookupAttribute(node, "href"), lookupAttribute(node, "title")); argument != "" && !literal {
		fmt.Fprintf(bbcode, "=%s", argument)
	}
	bbcode.WriteString("]")
	switch {
	case node.DataAtom == atom.Img:
		bbcode.WriteString(lookupAttribute(node, "src"))
	case literal:
		if block {
			bbcode.WriteString("\n")
		}
		for descendant := range node.Descendants() {
			switch {
			case descendant.Type == html.TextNode:
				bbcode.WriteString(descendant.Data)
			case descendant.DataAtom == atom.Br:
				bbcode.WriteString("\n")
			}
		}
		if block {
			bbcode.WriteString("\n")
		}
	default:
		writeChildren(bbcode, node)
	}
	if tag != "*" {
		fmt.Fprintf(bbcode, "[/%s]", tag)
	}
	if block {
		bbcode.WriteString("\n")
	}
}

// lookupTag returns the BBCode tag drawn as an HTML element, and whether it is a block or a literal. It returns false
// if there is none.
func lookupTag(element string) (tag string, block, literal, ok bool) {
	for name, row := range steamElements {
		if row.element == element || slices.Contains(row.aliases, element) {
			return name, row.block, false, true
		}
	}
	for _, row := range steamLiterals {
		if row.element == element {
			return row.tag, row.block, true, true
		}
	}
	return "", false, false, false
}

// lookupAttribute returns the value of one of an HTML element's attributes, or "" if it has none.
func lookupAttribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

// ToHTML returns BBCode as the HTML Steam draws it as. Only Steam's tags become elements, and a link only to a web
// page. BBCode that ToBBCode wrote comes back as the HTML it came from.
func (stm *Service) ToHTML(bbcode string) string {
	text := html.EscapeString(strings.ReplaceAll(bbcode, "\r\n", "\n"))
	for _, literal := range steamLiterals {
		text = literal.pattern.ReplaceAllStringFunc(text, func(match string) string {
			content := literal.pattern.FindStringSubmatch(match)[1]
			// a bracket in the content becomes its entity, so no tag is read in it below
			return literal.html[0] + strings.ReplaceAll(content, "[", "&#91;") + literal.html[1]
		})
	}
	var drawn strings.Builder
	// where the text not drawn yet starts, and whether the last tag drawn closed a block
	start, closed := 0, false
	for _, match := range bbcodeTag.FindAllStringSubmatchIndex(text, -1) {
		parts := bbcodeTag.FindStringSubmatch(text[match[0]:match[1]])
		name := parts[1] + strings.ToLower(parts[2])
		row, ok := steamElements[strings.ToLower(parts[2])]
		drawn.WriteString(trimBreaks(text[start:match[0]], closed, name, ok && row.block))
		drawn.WriteString(drawTag(parts))
		start, closed = match[1], ok && row.block && parts[1] != ""
	}
	drawn.WriteString(trimBreaks(text[start:], closed, "", false))
	return strings.ReplaceAll(drawn.String(), "\n", "<br>")
}

// trimBreaks returns the text between two tags without the line breaks that ToBBCode writes beside a block.
func trimBreaks(text string, closed bool, next string, block bool) string {
	trimmed := text
	if closed {
		trimmed = strings.TrimPrefix(trimmed, "\n")
	}
	if !block || !strings.HasSuffix(trimmed, "\n") {
		return trimmed
	}
	// the line before the break is blank when the break is the one left after a block's or follows another break
	blank := strings.HasSuffix(trimmed, "\n\n") || (trimmed == "\n" && text != trimmed)
	if slices.Contains(listEnds, next) || (!strings.HasPrefix(next, "/") && !blank) {
		return strings.TrimSuffix(trimmed, "\n")
	}
	return trimmed
}

// drawTag returns the HTML of one BBCode tag. A link to anything but a web page draws nothing, so its text shows
// alone. A tag Steam does not draw is returned as written.
func drawTag(parts []string) string {
	row, ok := steamElements[strings.ToLower(parts[2])]
	if !ok {
		return parts[0]
	}
	switch argument := parts[3]; {
	case parts[1] != "":
		// a closing tag takes no argument; one left over from a link drawn as text alone is ignored by the browser
		return "</" + row.element + ">"
	case row.address && !webAddress.MatchString(argument):
		return ""
	case row.address:
		return "<" + row.element + ` href="` + argument + `">`
	case argument != "":
		return "<" + row.element + ` title="` + argument + `">`
	}
	return "<" + row.element + ">"
}
