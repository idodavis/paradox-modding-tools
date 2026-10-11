// bbcode.go is the Workshop's description format: the BBCode subset Steam shows, written from
// Markdown and read back into it. What the Workshop cannot show (tables, task lists, HTML, local
// images) is dropped and named in the notes.

package steam

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Converted is converted text plus a note for each construct that was dropped.
type Converted struct {
	Text  string   `json:"text"`
	Notes []string `json:"notes,omitempty"`
}

var markdown = goldmark.New(goldmark.WithExtensions(
	extension.Strikethrough,
	extension.Table,
	extension.TaskList,
))

// MarkdownToBBCode renders Markdown as Workshop BBCode.
func MarkdownToBBCode(src string) Converted {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	doc := markdown.Parser().Parse(text.NewReader([]byte(src)))
	var out strings.Builder
	var notes []string
	renderMarkdown(doc, []byte(src), &out, &notes, 0)
	return Converted{Text: strings.TrimRight(out.String(), "\n") + "\n", Notes: distinct(notes)}
}

// renderMarkdown writes node and what it holds as BBCode into out, a note for each construct it drops.
// A list nested in a list is flattened to one level.
//
//nolint:funlen // a switch over the node kinds
func renderMarkdown(node ast.Node, src []byte, out *strings.Builder, notes *[]string, listDepth int) {
	switch node.Kind() {
	case ast.KindDocument:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			renderMarkdown(child, src, out, notes, listDepth)
		}
	case ast.KindHeading:
		// Steam has three heading sizes; a smaller heading is bold
		tag := "b"
		if level := node.(*ast.Heading).Level; level <= 3 {
			tag = "h" + strconv.Itoa(level)
		}
		out.WriteString("[" + tag + "]")
		writeInlines(node, src, out, notes)
		out.WriteString("[/" + tag + "]\n\n")
	case ast.KindParagraph:
		writeInlines(node, src, out, notes)
		out.WriteString("\n\n")
	case ast.KindBlockquote:
		out.WriteString("[quote]")
		var inner strings.Builder
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			renderMarkdown(child, src, &inner, notes, listDepth)
		}
		out.WriteString(strings.TrimSpace(inner.String()))
		out.WriteString("[/quote]\n\n")
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		out.WriteString("[code]")
		for i := 0; i < node.Lines().Len(); i++ {
			segment := node.Lines().At(i)
			out.Write(src[segment.Start:segment.Stop])
		}
		out.WriteString("[/code]\n\n")
	case ast.KindThematicBreak:
		out.WriteString("[hr][/hr]\n\n")
	case ast.KindList:
		tag := "list"
		if node.(*ast.List).IsOrdered() {
			tag = "olist"
		}
		if listDepth > 0 {
			*notes = append(*notes, "nested lists flattened to one level")
		}
		out.WriteString("[" + tag + "]")
		for listItem := node.FirstChild(); listItem != nil; listItem = listItem.NextSibling() {
			out.WriteString("[*]")
			var item strings.Builder
			for part := listItem.FirstChild(); part != nil; part = part.NextSibling() {
				if part.Kind() == ast.KindParagraph || part.Kind() == ast.KindTextBlock {
					writeInlines(part, src, &item, notes)
					continue
				}
				renderMarkdown(part, src, &item, notes, listDepth+1)
			}
			out.WriteString(strings.TrimSpace(item.String()))
		}
		out.WriteString("[/" + tag + "]\n\n")
	case east.KindTable:
		*notes = append(*notes, "tables dropped")
	case east.KindTaskCheckBox:
		*notes = append(*notes, "task lists dropped")
	case ast.KindHTMLBlock:
		*notes = append(*notes, "HTML dropped")
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			renderMarkdown(child, src, out, notes, listDepth)
		}
	}
}

// writeInlines writes the inline nodes of block as BBCode into out, a note for each construct it drops.
//
//nolint:funlen // a switch over the node kinds
func writeInlines(block ast.Node, src []byte, out *strings.Builder, notes *[]string) {
	_ = ast.Walk(block, func(inline ast.Node, entering bool) (ast.WalkStatus, error) {
		if inline == block {
			return ast.WalkContinue, nil
		}
		switch inline.Kind() {
		case ast.KindText:
			if entering {
				run := inline.(*ast.Text)
				out.Write(run.Segment.Value(src))
				if run.SoftLineBreak() {
					out.WriteByte('\n')
				}
			}
			return ast.WalkContinue, nil
		case ast.KindString:
			if entering {
				out.Write(inline.(*ast.String).Value)
			}
		case ast.KindEmphasis:
			tag := "i"
			if inline.(*ast.Emphasis).Level >= 2 {
				tag = "b"
			}
			if entering {
				out.WriteString("[" + tag + "]")
			} else {
				out.WriteString("[/" + tag + "]")
			}
		case east.KindStrikethrough:
			if entering {
				out.WriteString("[strike]")
			} else {
				out.WriteString("[/strike]")
			}
		case ast.KindCodeSpan:
			if entering {
				out.WriteString("[b]")
				for child := inline.FirstChild(); child != nil; child = child.NextSibling() {
					if run, ok := child.(*ast.Text); ok {
						out.Write(run.Segment.Value(src))
					}
				}
				out.WriteString("[/b]")
			}
			return ast.WalkSkipChildren, nil
		case ast.KindLink:
			if entering {
				out.WriteString("[url=" + string(inline.(*ast.Link).Destination) + "]")
			} else {
				out.WriteString("[/url]")
			}
		case ast.KindAutoLink:
			if entering {
				out.Write(inline.(*ast.AutoLink).URL(src))
			}
			return ast.WalkSkipChildren, nil
		case ast.KindImage:
			if entering {
				dest := string(inline.(*ast.Image).Destination)
				lower := strings.ToLower(dest)
				if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
					*notes = append(*notes, "images need a public http(s) URL for Steam; local paths dropped")
					return ast.WalkSkipChildren, nil
				}
				out.WriteString("[img]" + dest + "[/img]")
			}
			return ast.WalkSkipChildren, nil
		case ast.KindRawHTML:
			if entering {
				*notes = append(*notes, "HTML dropped")
			}
			return ast.WalkSkipChildren, nil
		case east.KindTaskCheckBox:
			if entering {
				*notes = append(*notes, "task lists dropped")
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
}

// distinct is notes without blanks or repeats, in first-seen order.
func distinct(notes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, note := range notes {
		if note == "" || seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	return out
}

// BBCodeToMarkdown renders Workshop BBCode as Markdown. A tag outside the Workshop's set
// stays literal.
//
//nolint:funlen // a switch over the tag names
func BBCodeToMarkdown(src string) Converted {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out strings.Builder
	pos := 0
	for pos < len(src) {
		if src[pos] != '[' {
			out.WriteByte(src[pos])
			pos++
			continue
		}
		name, arg, closing, end, ok := parseTag(src, pos)
		if !ok {
			out.WriteByte('[')
			pos++
			continue
		}
		if closing {
			out.WriteString(src[pos:end])
			pos = end
			continue
		}
		body, closeEnd, closed := closeTag(src, end, name)
		if !closed {
			out.WriteString(src[pos:end])
			pos = end
			continue
		}
		inner := func() string { return strings.TrimSpace(BBCodeToMarkdown(body).Text) }
		switch name {
		case "h1":
			out.WriteString("# " + inner())
		case "h2":
			out.WriteString("## " + inner())
		case "h3":
			out.WriteString("### " + inner())
		case "b":
			out.WriteString("**" + inner() + "**")
		case "i":
			out.WriteString("*" + inner() + "*")
		case "strike":
			out.WriteString("~~" + inner() + "~~")
		case "url":
			label := inner()
			fmt.Fprintf(&out, "[%s](%s)", label, cmp.Or(arg, label))
		case "img":
			out.WriteString("![](" + strings.TrimSpace(body) + ")")
		case "quote":
			for line := range strings.SplitSeq(inner(), "\n") {
				out.WriteString("> " + line + "\n")
			}
		case "code":
			out.WriteString("```\n" + strings.Trim(body, "\n") + "\n```")
		case "hr":
			out.WriteString("---")
		case "list":
			out.WriteString(listToMarkdown(body, false))
		case "olist":
			out.WriteString(listToMarkdown(body, true))
		case "noparse":
			if strings.Contains(body, "\n") {
				out.WriteString("```\n" + body + "\n```")
			} else {
				out.WriteString("`" + body + "`")
			}
		default:
			out.WriteString(src[pos:closeEnd])
		}
		pos = closeEnd
	}
	return Converted{Text: strings.TrimRight(out.String(), "\n") + "\n"}
}

// parseTag reads the tag starting at src[offset]: its lower-cased name, its =argument, whether it
// closes, and where it ends.
func parseTag(src string, offset int) (name, arg string, closing bool, end int, ok bool) {
	if offset >= len(src) || src[offset] != '[' {
		return "", "", false, offset, false
	}
	pos := offset + 1
	if pos < len(src) && src[pos] == '/' {
		closing = true
		pos++
	}
	start := pos
	for pos < len(src) && src[pos] != ']' && src[pos] != '=' {
		pos++
	}
	if pos >= len(src) {
		return "", "", false, offset, false
	}
	name = strings.ToLower(src[start:pos])
	if src[pos] == '=' {
		pos++
		argStart := pos
		for pos < len(src) && src[pos] != ']' {
			pos++
		}
		if pos >= len(src) {
			return "", "", false, offset, false
		}
		arg = src[argStart:pos]
	}
	return name, arg, closing, pos + 1, name != ""
}

// closeTag finds name's closing tag from start on, in any case: the body before it and where it ends.
// It compares in place, as lower-casing can change the text's length.
func closeTag(src string, start int, name string) (body string, closeEnd int, ok bool) {
	needle := "[/" + name + "]"
	for pos := start; ; pos += 2 {
		next := strings.Index(src[pos:], "[/")
		if next < 0 {
			return "", start, false
		}
		pos += next
		if end := pos + len(needle); end <= len(src) && strings.EqualFold(src[pos:end], needle) {
			return src[start:pos], end, true
		}
	}
}

// listToMarkdown renders the body of a [list] or [olist], its items split at [*], as Markdown list lines;
// ordered numbers them.
func listToMarkdown(body string, ordered bool) string {
	var out strings.Builder
	number := 1
	for _, part := range strings.Split(body, "[*]") {
		item := strings.TrimSpace(BBCodeToMarkdown(part).Text)
		if item == "" {
			continue
		}
		if ordered {
			fmt.Fprintf(&out, "%d. %s\n", number, item)
			number++
		} else {
			out.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimRight(out.String(), "\n")
}
