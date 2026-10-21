package steam

import "github.com/yuin/goldmark/renderer"

// ToBBCode converts a Markdown description to Steam's BBCode.
func ToBBCode(markdown string) string { panic("stub") }

// A bbcodeRenderer renders goldmark's syntax tree as BBCode: headings, emphasis, lists, links, images, code,
// quotes and rules, each as Steam writes it.
type bbcodeRenderer struct{}

// RegisterFuncs registers one render function per node kind goldmark produces.
func (bbcodeRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) { panic("stub") }
