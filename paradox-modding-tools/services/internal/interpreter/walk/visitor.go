package walk

import (
	parser "paradox-modding-tools/services/internal/interpreter"
)

// Context is passed to every visitor method. Depth is 0 at file top-level (direct entries),
// and incremented when traversing into an Object (so inline expressions have Depth > 0).
type Context struct {
	Depth int
}

// Visitor is called for each node during a depth-first traversal of the Paradox AST.
// Use ctx.Depth to distinguish top-level (0) from inside objects (>0).
type Visitor interface {
	VisitParadoxFile(*parser.ParadoxFile, *Context)
	VisitEntry(*parser.Entry, *Context)
	VisitNamespace(*parser.Namespace, *Context)
	VisitExpression(*parser.Expression, *Context)
	VisitObject(*parser.Object, *Context)
	VisitObjectEntry(*parser.ObjectEntry, *Context)
	VisitLiteral(*parser.Literal, *Context)
	VisitColor(*parser.Color, *Context)
}

// NoopVisitor implements Visitor with no-op methods. Embed it and override only what you need.
type NoopVisitor struct{}

func (NoopVisitor) VisitParadoxFile(*parser.ParadoxFile, *Context) {}
func (NoopVisitor) VisitEntry(*parser.Entry, *Context)             {}
func (NoopVisitor) VisitNamespace(*parser.Namespace, *Context)     {}
func (NoopVisitor) VisitExpression(*parser.Expression, *Context)   {}
func (NoopVisitor) VisitObject(*parser.Object, *Context)           {}
func (NoopVisitor) VisitObjectEntry(*parser.ObjectEntry, *Context) {}
func (NoopVisitor) VisitLiteral(*parser.Literal, *Context)         {}
func (NoopVisitor) VisitColor(*parser.Color, *Context)             {}
