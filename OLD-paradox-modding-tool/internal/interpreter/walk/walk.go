package walk

import (
	parser "paradox-modding-tool/internal/interpreter"
)

// Walk performs a depth-first traversal of the Paradox AST, calling the visitor at each node.
// Context.Depth is 0 for file-level entries and increases inside each Object.
func Walk(file *parser.ParadoxFile, v Visitor) {
	if file == nil || v == nil {
		return
	}
	ctx := &Context{Depth: 0}
	v.VisitParadoxFile(file, ctx)
	for _, e := range file.Entries {
		walkEntry(e, v, ctx)
	}
}

// walkEntry visits an entry and recurses into its content (ctx unchanged; entries are top-level).
func walkEntry(entry *parser.Entry, v Visitor, ctx *Context) {
	if entry == nil || v == nil {
		return
	}
	v.VisitEntry(entry, ctx)
	if entry.Namespace != nil {
		v.VisitNamespace(entry.Namespace, ctx)
	}
	if entry.Expression != nil {
		walkExpression(entry.Expression, v, ctx)
	}
}

// walkExpression visits an expression and recurses into literal or object (object increases depth).
func walkExpression(expr *parser.Expression, v Visitor, ctx *Context) {
	if expr == nil || v == nil {
		return
	}
	v.VisitExpression(expr, ctx)
	if expr.Literal != nil {
		walkLiteral(expr.Literal, v, ctx)
	}
	if expr.Object != nil {
		inner := *ctx
		inner.Depth++
		walkObject(expr.Object, v, &inner)
	}
}

// walkObject visits an object and recurses into each entry (already inside object, depth already set).
func walkObject(obj *parser.Object, v Visitor, ctx *Context) {
	if obj == nil || v == nil {
		return
	}
	v.VisitObject(obj, ctx)
	for _, oe := range obj.Entries {
		walkObjectEntry(oe, v, ctx)
	}
}

// walkObjectEntry visits an object entry and recurses into expression, literal, or nested object.
func walkObjectEntry(oe *parser.ObjectEntry, v Visitor, ctx *Context) {
	if oe == nil || v == nil {
		return
	}
	v.VisitObjectEntry(oe, ctx)
	if oe.Expression != nil {
		walkExpression(oe.Expression, v, ctx)
	}
	if oe.Literal != nil {
		walkLiteral(oe.Literal, v, ctx)
	}
	if oe.Object != nil {
		inner := *ctx
		inner.Depth++
		walkObject(oe.Object, v, &inner)
	}
}

// walkLiteral visits a literal and recurses into color or array elements.
func walkLiteral(lit *parser.Literal, v Visitor, ctx *Context) {
	if lit == nil || v == nil {
		return
	}
	v.VisitLiteral(lit, ctx)
	if lit.Color != nil {
		v.VisitColor(lit.Color, ctx)
	}
	if lit.Array != nil {
		for _, elem := range lit.Array {
			walkLiteral(elem, v, ctx)
		}
	}
}
