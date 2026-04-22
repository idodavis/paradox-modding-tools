package internal

import (
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/frustra/bbcode"
)

// Steam BBCode per https://steamcommunity.com/comment/Recommendation/formattinghelp
// Uses frustra/bbcode with custom Steam tags.

var steamCompiler bbcode.Compiler

func init() {
	steamCompiler = bbcode.NewCompiler(true, true)
	// Simple passthrough: bbcode tag name -> HTML tag name
	for _, t := range []struct{ bb, html string }{
		{"p", "p"}, {"h1", "h1"}, {"h2", "h2"}, {"h3", "h3"}, {"tr", "tr"}, {"th", "th"}, {"td", "td"},
	} {
		tag, h := t.bb, t.html
		steamCompiler.SetTag(tag, func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
			out := bbcode.NewHTMLTag("")
			out.Name = h
			return out, true
		})
	}
	steamCompiler.SetTag("strike", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "s"
		return out, true
	})
	steamCompiler.SetTag("hr", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "hr"
		return out, false
	})
	steamCompiler.SetTag("url", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "a"
		t := n.GetOpeningTag()
		href := t.Value
		if href == "" {
			href = t.Args["href"]
		}
		if href == "" {
			href = bbcode.CompileText(n)
		}
		if u, err := url.Parse(href); err == nil {
			out.Attrs["href"] = u.String()
			out.Attrs["target"], out.Attrs["rel"] = "_blank", "noopener"
		}
		return out, true
	})
	steamCompiler.SetTag("dynamiclink", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "a"
		if href := n.GetOpeningTag().Args["href"]; href != "" {
			if u, err := url.Parse(href); err == nil {
				out.Attrs["href"] = u.String()
				out.Attrs["target"], out.Attrs["rel"] = "_blank", "noopener"
				out.AppendChild(bbcode.NewHTMLTag("Store"))
			}
		}
		return out, false
	})
	steamCompiler.SetTag("noparse", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Value = html.EscapeString(bbcode.CompileText(n))
		return out, false
	})
	steamCompiler.SetTag("spoiler", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "details"
		sum := bbcode.NewHTMLTag("")
		sum.Name = "summary"
		sum.AppendChild(bbcode.NewHTMLTag("Spoiler"))
		out.AppendChild(sum)
		inner := bbcode.NewHTMLTag("")
		inner.Name = "div"
		for _, c := range n.Children {
			inner.AppendChild(steamCompiler.CompileTree(c))
		}
		out.AppendChild(inner)
		return out, false
	})
	steamCompiler.SetTag("quote", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "blockquote"
		if who := n.GetOpeningTag().Value; who != "" {
			p := bbcode.NewHTMLTag("")
			p.Name = "p"
			p.AppendChild(bbcode.NewHTMLTag("Originally posted by "))
			s := bbcode.NewHTMLTag("")
			s.Name = "strong"
			s.AppendChild(bbcode.NewHTMLTag(who))
			p.AppendChild(s)
			p.AppendChild(bbcode.NewHTMLTag(":"))
			out.AppendChild(p)
		}
		inner := bbcode.NewHTMLTag("")
		inner.Name = "p"
		for _, c := range n.Children {
			inner.AppendChild(steamCompiler.CompileTree(c))
		}
		out.AppendChild(inner)
		return out, false
	})
	steamCompiler.SetTag("code", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		pre := bbcode.NewHTMLTag("")
		pre.Name = "pre"
		code := bbcode.NewHTMLTag("")
		code.Name = "code"
		for _, c := range n.Children {
			code.AppendChild(bbcode.CompileRaw(c))
		}
		pre.AppendChild(code)
		return pre, false
	})
	steamCompiler.SetTag("list", steamListTag("ul"))
	steamCompiler.SetTag("olist", steamListTag("ol"))
	steamCompiler.SetTag("*", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "li"
		return out, true
	})
	steamCompiler.SetTag("table", func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = "table"
		t := n.GetOpeningTag()
		if t.Args["noborder"] == "1" {
			out.Attrs["style"] = "border:none"
		}
		if t.Args["equalcells"] == "1" {
			if s := out.Attrs["style"]; s != "" {
				out.Attrs["style"] = s + ";table-layout:fixed;width:100%"
			} else {
				out.Attrs["style"] = "table-layout:fixed;width:100%"
			}
		}
		for _, c := range n.Children {
			out.AppendChild(steamCompiler.CompileTree(c))
		}
		return out, false
	})
}

func steamListTag(wrapper string) bbcode.TagCompilerFunc {
	return func(n *bbcode.BBCodeNode) (*bbcode.HTMLTag, bool) {
		out := bbcode.NewHTMLTag("")
		out.Name = wrapper
		for _, li := range flattenListItems(n.Children) {
			out.AppendChild(li)
		}
		return out, false
	}
}

func flattenListItems(children []*bbcode.BBCodeNode) []*bbcode.HTMLTag {
	var items []*bbcode.HTMLTag
	for _, c := range children {
		tag, ok := c.Value.(bbcode.BBOpeningTag)
		if c.ID != bbcode.OPENING_TAG || !ok || tag.Name != "*" {
			continue
		}
		var cur *bbcode.HTMLTag
		for _, cc := range c.Children {
			if cc.ID == bbcode.OPENING_TAG {
				if t, ok := cc.Value.(bbcode.BBOpeningTag); ok && t.Name == "*" {
					if cur != nil {
						items = append(items, cur)
					}
					items = append(items, flattenListItems([]*bbcode.BBCodeNode{cc})...)
					cur = nil
					continue
				}
			}
			if cur == nil {
				cur = bbcode.NewHTMLTag("")
				cur.Name = "li"
			}
			cur.AppendChild(steamCompiler.CompileTree(cc))
		}
		if cur != nil {
			items = append(items, cur)
		}
	}
	return items
}

func SteamBBCodeToHTML(bb string) string {
	if bb == "" {
		return ""
	}
	s := strings.ReplaceAll(bb, `\"`, `"`)
	s = strings.ReplaceAll(s, `[\*]`, `[*]`)
	s = strings.ReplaceAll(s, `[\/*]`, `[/*]`)
	s = regexp.MustCompile(`\[/\*\s*\]`).ReplaceAllString(s, `[/*]`)
	result := steamCompiler.Compile(s)
	result = regexp.MustCompile(`<p>\s*</p>`).ReplaceAllString(result, "")
	result = regexp.MustCompile(`\n\s*\n`).ReplaceAllString(result, "\n")
	return result
}
