package lsp

import (
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Completion is one candidate the completion list shows: the text to insert, how it ranks, and what the item
// shows.
type Completion struct {
	Label    string          `json:"label"`
	Insert   string          `json:"insert"`           // the text inserted; the label where equal
	Category schema.Category `json:"category"`         // what it is, for the icon and the color
	Detail   string          `json:"detail,omitempty"` // the database's singular, the command's class
	Blurb    string          `json:"blurb,omitempty"`  // the one line shown beside it
	Rank     int             `json:"rank"`             // how often vanilla writes it in this holder, for the order
	Snippet  bool            `json:"snippet"`          // Insert is a snippet with placeholders
}

// Complete returns the completion candidates at a position in a file. In a key position they are the operators
// first, then the commands and fields the block takes, ranked by how often the game's own files write each one
// there. In a value position they come from the field's expected type: its choices, the names the game loads, or
// the texture files a path could name. Every candidate is sent; Monaco filters by what was typed.
func (lsp *Service) Complete(path string, line, column int) ([]Completion, error) { panic("stub") }

// A completeVisitor is the visitor behind Complete. It keeps the context of the block that holds the offset.
type completeVisitor struct {
	offset  int
	context schema.Context
}

// Enter keeps the context of the innermost block covering the offset.
func (visitor *completeVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	panic("stub")
}

// Leave does nothing: completion needs only the holding block's context.
func (visitor *completeVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}
