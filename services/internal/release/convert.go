// convert.go maps a Steam Workshop BBCode subset to and from Markdown.
package release

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Result is converted text plus notes for dropped constructs.
type Result struct {
	Text  string   `json:"text"`
	Notes []string `json:"notes,omitempty"`
}

var md = goldmark.New(goldmark.WithExtensions(
	extension.Strikethrough,
	extension.Table,
	extension.TaskList,
))

// MarkdownToBBCode renders the locked Workshop mapping. Lossy tags become notes.
func MarkdownToBBCode(src string) Result {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	var b strings.Builder
	var notes []string
	renderMD(doc, []byte(src), &b, &notes, 0)
	return Result{Text: strings.TrimRight(b.String(), "\n") + "\n", Notes: uniq(notes)}
}

func renderMD(n ast.Node, src []byte, b *strings.Builder, notes *[]string, listDepth int) {
	switch n.Kind() {
	case ast.KindDocument:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderMD(c, src, b, notes, listDepth)
		}
	case ast.KindHeading:
		h := n.(*ast.Heading)
		open, close := headingTags(h.Level)
		b.WriteString(open)
		writeInlines(n, src, b, notes)
		b.WriteString(close + "\n\n")
	case ast.KindParagraph:
		writeInlines(n, src, b, notes)
		b.WriteString("\n\n")
	case ast.KindBlockquote:
		b.WriteString("[quote]")
		var inner strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderMD(c, src, &inner, notes, listDepth)
		}
		b.WriteString(strings.TrimSpace(inner.String()))
		b.WriteString("[/quote]\n\n")
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		b.WriteString("[code]")
		writeLines(n, src, b)
		b.WriteString("[/code]\n\n")
	case ast.KindThematicBreak:
		b.WriteString("[hr][/hr]\n\n")
	case ast.KindList:
		list := n.(*ast.List)
		tag := "list"
		if list.IsOrdered() {
			tag = "olist"
		}
		if listDepth > 0 {
			*notes = append(*notes, "nested lists flattened to one level")
		}
		b.WriteString("[" + tag + "]")
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			b.WriteString("[*]")
			var item strings.Builder
			for gc := c.FirstChild(); gc != nil; gc = gc.NextSibling() {
				if gc.Kind() == ast.KindList {
					renderMD(gc, src, &item, notes, listDepth+1)
					continue
				}
				if gc.Kind() == ast.KindParagraph || gc.Kind() == ast.KindTextBlock {
					writeInlines(gc, src, &item, notes)
					continue
				}
				renderMD(gc, src, &item, notes, listDepth+1)
			}
			b.WriteString(strings.TrimSpace(item.String()))
		}
		b.WriteString("[/" + tag + "]\n\n")
	case east.KindTable:
		*notes = append(*notes, "tables dropped")
	case east.KindTaskCheckBox:
		*notes = append(*notes, "task lists dropped")
	case ast.KindHTMLBlock:
		*notes = append(*notes, "HTML dropped")
	default:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderMD(c, src, b, notes, listDepth)
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

func writeInlines(n ast.Node, src []byte, b *strings.Builder, notes *[]string) {
	ast.Walk(n, func(cur ast.Node, entering bool) (ast.WalkStatus, error) {
		if cur == n {
			return ast.WalkContinue, nil
		}
		switch cur.Kind() {
		case ast.KindText:
			if entering {
				t := cur.(*ast.Text)
				b.Write(t.Segment.Value(src))
				if t.SoftLineBreak() {
					b.WriteByte('\n')
				}
			}
			return ast.WalkContinue, nil
		case ast.KindString:
			if entering {
				b.Write(cur.(*ast.String).Value)
			}
		case ast.KindEmphasis:
			if entering {
				if cur.(*ast.Emphasis).Level >= 2 {
					b.WriteString("[b]")
				} else {
					b.WriteString("[i]")
				}
			} else {
				if cur.(*ast.Emphasis).Level >= 2 {
					b.WriteString("[/b]")
				} else {
					b.WriteString("[/i]")
				}
			}
		case east.KindStrikethrough:
			if entering {
				b.WriteString("[strike]")
			} else {
				b.WriteString("[/strike]")
			}
		case ast.KindCodeSpan:
			if entering {
				b.WriteString("[b]")
				for c := cur.FirstChild(); c != nil; c = c.NextSibling() {
					if t, ok := c.(*ast.Text); ok {
						b.Write(t.Segment.Value(src))
					}
				}
				b.WriteString("[/b]")
			}
			return ast.WalkSkipChildren, nil
		case ast.KindLink:
			if entering {
				dest := string(cur.(*ast.Link).Destination)
				b.WriteString("[url=" + dest + "]")
			} else {
				b.WriteString("[/url]")
			}
		case ast.KindAutoLink:
			if entering {
				b.Write(cur.(*ast.AutoLink).URL(src))
			}
			return ast.WalkSkipChildren, nil
		case ast.KindImage:
			if entering {
				dest := string(cur.(*ast.Image).Destination)
				if !publicHTTP(dest) {
					*notes = append(*notes,
						"images need a public http(s) URL for Steam; local paths dropped")
					return ast.WalkSkipChildren, nil
				}
				b.WriteString("[img]" + dest + "[/img]")
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

func writeLines(n ast.Node, src []byte, b *strings.Builder) {
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(src[seg.Start:seg.Stop])
	}
}

func publicHTTP(dest string) bool {
	d := strings.ToLower(dest)
	return strings.HasPrefix(d, "https://") || strings.HasPrefix(d, "http://")
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// BBCodeToMarkdown inverts the locked Workshop tag set. Unknown tags stay literal.
func BBCodeToMarkdown(src string) Result {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var b strings.Builder
	i := 0
	for i < len(src) {
		if src[i] != '[' {
			b.WriteByte(src[i])
			i++
			continue
		}
		name, arg, close, end, ok := parseTag(src, i)
		if !ok {
			b.WriteByte('[')
			i++
			continue
		}
		if close {
			b.WriteString(src[i:end])
			i = end
			continue
		}
		body, after, found := closeTag(src, end, name)
		if !found {
			b.WriteString(src[i:end])
			i = end
			continue
		}
		switch name {
		case "h1":
			b.WriteString("# " + strings.TrimSpace(BBCodeToMarkdown(body).Text))
		case "h2":
			b.WriteString("## " + strings.TrimSpace(BBCodeToMarkdown(body).Text))
		case "h3":
			b.WriteString("### " + strings.TrimSpace(BBCodeToMarkdown(body).Text))
		case "b":
			b.WriteString("**" + strings.TrimSpace(BBCodeToMarkdown(body).Text) + "**")
		case "i":
			b.WriteString("*" + strings.TrimSpace(BBCodeToMarkdown(body).Text) + "*")
		case "strike":
			b.WriteString("~~" + strings.TrimSpace(BBCodeToMarkdown(body).Text) + "~~")
		case "url":
			inner := strings.TrimSpace(BBCodeToMarkdown(body).Text)
			href := arg
			if href == "" {
				href = inner
			}
			b.WriteString(fmt.Sprintf("[%s](%s)", inner, href))
		case "img":
			b.WriteString("![](" + strings.TrimSpace(body) + ")")
		case "quote":
			for line := range strings.SplitSeq(strings.TrimSpace(BBCodeToMarkdown(body).Text), "\n") {
				b.WriteString("> " + line + "\n")
			}
		case "code":
			b.WriteString("```\n" + strings.Trim(body, "\n") + "\n```")
		case "hr":
			b.WriteString("---")
		case "list":
			b.WriteString(listToMD(body, false))
		case "olist":
			b.WriteString(listToMD(body, true))
		case "noparse":
			if strings.Contains(body, "\n") {
				b.WriteString("```\n" + body + "\n```")
			} else {
				b.WriteString("`" + body + "`")
			}
		default:
			b.WriteString(src[i:after])
			i = after
			continue
		}
		i = after
	}
	return Result{Text: strings.TrimRight(b.String(), "\n") + "\n"}
}

func parseTag(s string, i int) (name, arg string, close bool, end int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return "", "", false, i, false
	}
	j := i + 1
	if j < len(s) && s[j] == '/' {
		close = true
		j++
	}
	start := j
	for j < len(s) && s[j] != ']' && s[j] != '=' {
		j++
	}
	if j >= len(s) {
		return "", "", false, i, false
	}
	name = strings.ToLower(s[start:j])
	if s[j] == '=' {
		j++
		argStart := j
		for j < len(s) && s[j] != ']' {
			j++
		}
		if j >= len(s) {
			return "", "", false, i, false
		}
		arg = s[argStart:j]
	}
	return name, arg, close, j + 1, name != ""
}

func closeTag(s string, from int, name string) (body string, after int, ok bool) {
	needle := "[/" + name + "]"
	idx := strings.Index(strings.ToLower(s[from:]), needle)
	if idx < 0 {
		return "", from, false
	}
	return s[from : from+idx], from + idx + len(needle), true
}

func listToMD(body string, ordered bool) string {
	parts := strings.Split(body, "[*]")
	var b strings.Builder
	n := 1
	for _, p := range parts {
		item := strings.TrimSpace(BBCodeToMarkdown(p).Text)
		if item == "" {
			continue
		}
		if ordered {
			fmt.Fprintf(&b, "%d. %s\n", n, item)
			n++
		} else {
			b.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
