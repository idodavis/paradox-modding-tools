package steam

import (
	"bufio"
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// A bbcodeRenderer renders goldmark's syntax tree as BBCode, the way Steam writes it.
type bbcodeRenderer struct{}

// converter reads Markdown with tables and strikethrough and writes it as BBCode.
var converter = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithRenderer(renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(bbcodeRenderer{}, 1)))),
)

// bbcodeTags holds, for each kind of node written as a pair of tags, its rows of opening and closing tag. A node takes
// the row that selectTags picks. A link's opening tag is followed by its destination and a closing bracket. A kind with
// no row writes nothing, so raw HTML is dropped.
var bbcodeTags = map[ast.NodeKind][][2]string{
	ast.KindHeading:          {{"[h1]", "[/h1]\n\n"}, {"[h2]", "[/h2]\n\n"}, {"[h3]", "[/h3]\n\n"}},
	ast.KindBlockquote:       {{"[quote]", "[/quote]\n\n"}},
	ast.KindList:             {{"[list]\n", "[/list]\n\n"}, {"[olist]\n", "[/olist]\n\n"}, {"[list]\n", "[/list]\n"}, {"[olist]\n", "[/olist]\n"}},
	ast.KindListItem:         {{"[*] ", ""}},
	ast.KindParagraph:        {{"", "\n\n"}},
	ast.KindTextBlock:        {{"", "\n"}},
	ast.KindCodeSpan:         {{"[code]", "[/code]"}},
	ast.KindEmphasis:         {{"[i]", "[/i]"}, {"[b]", "[/b]"}},
	ast.KindLink:             {{"[url=", "[/url]"}},
	extast.KindStrikethrough: {{"[strike]", "[/strike]"}},
	extast.KindTable:         {{"[table]\n", "[/table]\n\n"}},
	extast.KindTableHeader:   {{"[tr]", "[/tr]\n"}},
	extast.KindTableRow:      {{"[tr]", "[/tr]\n"}},
	extast.KindTableCell:     {{"[td]", "[/td]"}, {"[th]", "[/th]"}},
	ast.KindThematicBreak:    {{"[hr][/hr]\n\n", ""}},
	ast.KindImage:            {{"[img]", "[/img]"}},
	ast.KindAutoLink:         {{"[url]", "[/url]"}},
	ast.KindCodeBlock:        {{"[code]\n", "\n[/code]\n\n"}},
	ast.KindFencedCodeBlock:  {{"[code]\n", "\n[/code]\n\n"}},
}

// steamElements are the BBCode tags Steam draws, by name, each with the HTML element it is drawn as and whether that
// element is a block, which stands on lines of its own.
var steamElements = map[string]struct {
	element string
	block   bool
}{
	"h1": {"h1", true}, "h2": {"h2", true}, "h3": {"h3", true}, "quote": {"blockquote", true}, "hr": {"hr", true},
	"list": {"ul", true}, "olist": {"ol", true}, "*": {"li", true}, "table": {"table", true}, "tr": {"tr", true},
	"th": {"th", false}, "td": {"td", false}, "b": {"b", false}, "i": {"i", false}, "u": {"u", false},
	"strike": {"s", false}, "spoiler": {"mark", false}, "url": {"a", false},
}

// steamLiterals are the BBCode tags whose content Steam shows as written, with no tag read in it, each with the HTML
// written before and after the content: text kept as it is, a block of code, and a picture, whose content is its
// address. A block of code drops the line breaks beside its tags.
var steamLiterals = []struct {
	pattern *regexp.Regexp
	html    [2]string
}{
	{regexp.MustCompile(`(?is)\[noparse\](.*?)\[/noparse\]`), [2]string{"", ""}},
	{regexp.MustCompile(`(?is)(?:\r?\n)?\[code\](?:\r?\n)?(.*?)(?:\r?\n)?\[/code\](?:\r?\n)?`), [2]string{"<pre>", "</pre>"}},
	{regexp.MustCompile(`(?is)\[img\](.*?)\[/img\]`), [2]string{`<img alt="" src="`, `">`}},
}

// bbcodeTag matches one BBCode tag with a line break on either side of it: the slash of a closing tag, the tag's name
// and its argument after an equals sign or a space.
var bbcodeTag = regexp.MustCompile(`(?i)(\r?\n)?\[(/?)([a-z0-9*]+)(?:[= ]([^\]]*))?\](\r?\n)?`)

// ToBBCode converts a Markdown text to Steam's BBCode.
func (stm *Service) ToBBCode(markdown string) string {
	var bbcode strings.Builder
	// writing to a Builder never fails, and the renderers fail only when their writer does
	_ = converter.Convert([]byte(markdown), &bbcode)
	return strings.TrimSpace(bbcode.String())
}

// RegisterFuncs registers one render function per node kind goldmark produces.
func (bbcodeRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	for kind := range bbcodeTags {
		registerer.Register(kind, writeNode)
	}
	registerer.Register(ast.KindText, writeText)
	registerer.Register(ast.KindString, writeText)
}

// writeNode writes the opening tag of a node as the walk enters it and the closing tag as it leaves. A node that is
// written whole has its content between the tags, and its children are skipped.
func writeNode(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	tags := bbcodeTags[node.Kind()][selectTags(node)]
	if content, whole := resolveContent(node, source); whole {
		if !entering {
			return ast.WalkContinue, nil
		}
		_, err := writer.WriteString(tags[0] + content + tags[1])
		return ast.WalkSkipChildren, err
	}
	tag := tags[1]
	if entering {
		tag = tags[0]
		if link, ok := node.(*ast.Link); ok {
			tag += string(link.Destination) + "]"
		}
	}
	_, err := writer.WriteString(tag)
	return ast.WalkContinue, err
}

// selectTags returns which row of its kind's tags a node is written with: a heading's level, whether a list is
// ordered and whether it sits in another, an emphasis's strength and whether a cell is in the table's header.
func selectTags(node ast.Node) int {
	switch node := node.(type) {
	case *ast.Heading:
		return min(node.Level, 3) - 1
	case *ast.List:
		row := 0
		if node.IsOrdered() {
			row = 1
		}
		if node.Parent().Kind() == ast.KindListItem {
			row += 2
		}
		return row
	case *ast.Emphasis:
		return min(node.Level, 2) - 1
	case *extast.TableCell:
		if node.Parent().Kind() == extast.KindTableHeader {
			return 1
		}
	}
	return 0
}

// resolveContent returns what a node that is written whole holds, and false for a node that is not: a picture's
// and an address's links, and the lines of a block of code.
func resolveContent(node ast.Node, source []byte) (content string, whole bool) {
	switch node := node.(type) {
	case *ast.ThematicBreak:
		return "", true
	case *ast.Image:
		return string(node.Destination), true
	case *ast.AutoLink:
		return string(node.URL(source)), true
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		return strings.TrimSuffix(string(node.Lines().Value(source)), "\n"), true
	}
	return "", false
}

// writeText writes the text of a node. Outside code, Markdown's backslash escapes and entity references are
// resolved, as goldmark's HTML writer does. A line break inside a paragraph is written as a new line.
func writeText(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var err error
	switch node := node.(type) {
	case *ast.Text:
		text := node.Segment.Value(source)
		if node.Parent().Kind() != ast.KindCodeSpan {
			text = resolveEscapes(text)
		}
		_, err = writer.Write(text)
		if err == nil && (node.SoftLineBreak() || node.HardLineBreak()) {
			err = writer.WriteByte('\n')
		}
	case *ast.String:
		_, err = writer.Write(node.Value)
	}
	return ast.WalkContinue, err
}

// resolveEscapes returns Markdown text with its backslash escapes and entity references resolved to the characters
// they stand for.
func resolveEscapes(text []byte) []byte {
	var resolved bytes.Buffer
	writer := bufio.NewWriter(&resolved)
	goldhtml.DefaultWriter.Write(writer, text)
	_ = writer.Flush()
	return []byte(html.UnescapeString(resolved.String()))
}

// ToHTML returns a BBCode text as the HTML that Steam draws it as. The text is escaped, so the HTML holds no element
// but those of Steam's tags. A tag Steam does not draw stays as text, and each line break is a break. A tag left open
// is closed where the browser closes it.
func (stm *Service) ToHTML(bbcode string) string {
	text := html.EscapeString(bbcode)
	for _, literal := range steamLiterals {
		text = literal.pattern.ReplaceAllStringFunc(text, func(match string) string {
			content := literal.pattern.FindStringSubmatch(match)[1]
			// a bracket in the content becomes its entity, so no tag is read in it below
			return literal.html[0] + strings.ReplaceAll(content, "[", "&#91;") + literal.html[1]
		})
	}
	text = bbcodeTag.ReplaceAllStringFunc(text, drawTag)
	return strings.NewReplacer("\r\n", "<br>", "\n", "<br>").Replace(text)
}

// drawTag returns the HTML that one BBCode tag is drawn as, with the line breaks beside it. A tag's argument, such as
// a link's address, becomes its element's title. A block drops the line breaks. A tag Steam does not draw is returned
// as it is.
func drawTag(tag string) string {
	parts := bbcodeTag.FindStringSubmatch(tag)
	row, ok := steamElements[strings.ToLower(parts[3])]
	if !ok {
		return tag
	}
	element := "<" + parts[2] + row.element
	if parts[4] != "" && parts[2] == "" {
		element += ` title="` + parts[4] + `"`
	}
	element += ">"
	if row.block {
		return element
	}
	return parts[1] + element + parts[5]
}
