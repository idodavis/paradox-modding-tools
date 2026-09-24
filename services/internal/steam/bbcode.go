// bbcode.go maps Markdown to the Steam Workshop's BBCode subset and back. What the Workshop
// cannot show (tables, task lists, HTML, local images) is dropped and named in the notes.

package steam

import (
	"cmp"
	"fmt"
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

func renderMarkdown(node ast.Node, src []byte, out *strings.Builder, notes *[]string, listDepth int) {
	switch node.Kind() {
	case ast.KindDocument:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			renderMarkdown(child, src, out, notes, listDepth)
		}
	case ast.KindHeading:
		opening, closing := headingTags(node.(*ast.Heading).Level)
		out.WriteString(opening)
		writeInlines(node, src, out, notes)
		out.WriteString(closing + "\n\n")
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

func headingTags(level int) (string, string) {
	switch level {
	case 1:
		return "[h1]", "[/h1]"
	case 2:
		return "[h2]", "[/h2]"
	case 3:
		return "[h3]", "[/h3]"
	default:
		return "[b]", "[/b]"
	}
}

func writeInlines(block ast.Node, src []byte, out *strings.Builder, notes *[]string) {
	ast.Walk(block, func(inline ast.Node, entering bool) (ast.WalkStatus, error) {
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
func BBCodeToMarkdown(src string) Converted {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out strings.Builder
	i := 0
	for i < len(src) {
		if src[i] != '[' {
			out.WriteByte(src[i])
			i++
			continue
		}
		name, arg, closing, end, ok := parseTag(src, i)
		if !ok {
			out.WriteByte('[')
			i++
			continue
		}
		if closing {
			out.WriteString(src[i:end])
			i = end
			continue
		}
		body, after, found := closeTag(src, end, name)
		if !found {
			out.WriteString(src[i:end])
			i = end
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
			out.WriteString(src[i:after])
		}
		i = after
	}
	return Converted{Text: strings.TrimRight(out.String(), "\n") + "\n"}
}

// parseTag reads the tag starting at src[i]: its lower-cased name, its =argument, whether it
// closes, and where it ends.
func parseTag(src string, i int) (name, arg string, closing bool, end int, ok bool) {
	if i >= len(src) || src[i] != '[' {
		return "", "", false, i, false
	}
	j := i + 1
	if j < len(src) && src[j] == '/' {
		closing = true
		j++
	}
	start := j
	for j < len(src) && src[j] != ']' && src[j] != '=' {
		j++
	}
	if j >= len(src) {
		return "", "", false, i, false
	}
	name = strings.ToLower(src[start:j])
	if src[j] == '=' {
		j++
		argStart := j
		for j < len(src) && src[j] != ']' {
			j++
		}
		if j >= len(src) {
			return "", "", false, i, false
		}
		arg = src[argStart:j]
	}
	return name, arg, closing, j + 1, name != ""
}

// closeTag finds name's closing tag from from on: the body before it and where it ends.
func closeTag(src string, from int, name string) (body string, after int, ok bool) {
	needle := "[/" + name + "]"
	at := strings.Index(strings.ToLower(src[from:]), needle)
	if at < 0 {
		return "", from, false
	}
	return src[from : from+at], from + at + len(needle), true
}

func listToMarkdown(body string, ordered bool) string {
	var out strings.Builder
	n := 1
	for _, part := range strings.Split(body, "[*]") {
		item := strings.TrimSpace(BBCodeToMarkdown(part).Text)
		if item == "" {
			continue
		}
		if ordered {
			fmt.Fprintf(&out, "%d. %s\n", n, item)
			n++
		} else {
			out.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimRight(out.String(), "\n")
}
