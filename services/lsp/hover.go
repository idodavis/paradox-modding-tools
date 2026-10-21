package lsp

import (
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// A CardScope is the scope section of a hover card: this, root, the previous scopes and the saved ones.
type CardScope struct {
	This     string         `json:"this"`
	Root     string         `json:"root"`
	Previous []string       `json:"previous"`
	Saved    []schema.Saved `json:"saved"`
}

// A CardSite is one place a name is defined, with whether the game keeps that definition and why.
type CardSite struct {
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"`
	Kept   bool            `json:"kept"`
	Reason string          `json:"reason,omitempty"` // order, mode, priority, merge, replace
}

// A CardDate is a date read in the mod's calendar: its era and the month's name as the mod names it.
type CardDate struct {
	Date  jomini.Date `json:"date"`
	Era   string      `json:"era,omitempty"`
	Month string      `json:"month"`
}

// A Card is a hover card. It holds facts for the frontend to lay out, never a sentence that combines them.
type Card struct {
	Title    string          `json:"title"`
	What     string          `json:"what"`            // what it is: the database's singular, "effect", "trigger", "link", "keyword"
	Class    string          `json:"class,omitempty"` // an operator's class
	Blurb    string          `json:"blurb,omitempty"`
	Takes    schema.Expected `json:"takes"`           // what the key's value may be
	Holds    string          `json:"holds,omitempty"` // what its block holds: the shape's word
	Leads    string          `json:"leads,omitempty"` // the scope a link or command leads to
	Scope    *CardScope      `json:"scope,omitempty"`
	Date     *CardDate       `json:"date,omitempty"`
	Color    string          `json:"color,omitempty"` // a CSS color
	Image    *texture.Header `json:"image,omitempty"`
	Sites    []CardSite      `json:"sites,omitempty"`
	Inferred bool            `json:"inferred"` // a fact was learned from the game's files
	Range    Range           `json:"range"`    // what the card is about
}

// Hover returns the hover card for the key or value at a position in a file, or nil if the position is on
// nothing.
func (lsp *Service) Hover(path string, line, column int) (*Card, error) { panic("stub") }

// A hoverVisitor is the visitor behind Hover. It keeps the context of the innermost statement at the offset.
type hoverVisitor struct {
	offset    int
	statement int32
	context   schema.Context
}

// Enter keeps the context when the statement covers the offset.
func (visitor *hoverVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	panic("stub")
}

// Leave does nothing: the card needs only the innermost statement's context.
func (visitor *hoverVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}
