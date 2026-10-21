package schema

import (
	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Category is the color category of a key or a value in the editor. Each theme maps a category to a color.
type Category string

const (
	CategoryNone       Category = ""
	CategoryKeyword    Category = "keyword"    // the language's own keys: if, limit, AND, random_list
	CategoryCommand    Category = "command"    // an effect or a trigger
	CategoryLink       Category = "link"       // a scope link: root, scope:x, liege
	CategoryField      Category = "field"      // a definition's own key
	CategoryDefinition Category = "definition" // a name being defined
	CategoryReference  Category = "reference"  // a name referring to a definition
	CategoryParameter  Category = "parameter"  // $NAME$
	CategoryConstant   Category = "constant"   // @name
	CategoryCall       Category = "call"       // a data call's step
)

// A Definition is a name that a statement defines in a database, with the facts that decide which definition of
// that name the game keeps.
type Definition struct {
	Database  string      `json:"database"`
	Name      string      `json:"name"`
	Span      jomini.Span `json:"span"`      // the name as written
	Statement int32       `json:"statement"` // the index of the statement in the file's tree
	Mode      string      `json:"mode"`      // an entry mode's prefix such as INJECT or REPLACE; "" for a plain definition
	Priority  int         `json:"priority"`  // CK3's override priority; 0 when none is written
	Local     bool        `json:"local"`     // visible only in its own file
}

// A Reference is a name that a key or a value refers to, in a database, together with the caller it was resolved
// through.
type Reference struct {
	Database string      `json:"database"`
	Name     string      `json:"name"`
	Span     jomini.Span `json:"span"`
	Caller   string      `json:"caller,omitempty"` // the scripted call it is resolved through; "" if none
}

// A Saved is one scope that the definition saved before the current statement, with the type it holds.
type Saved struct {
	Name string `json:"name"`
	Type string `json:"type"` // "" if unknown
}

// A Context is everything the walk has decided at the statement it is on. The walk keeps one per depth and reuses
// it, so a visitor copies whatever it wants to keep.
type Context struct {
	Field       *Field      // the field the key matched; nil when no source knows the key
	Database    string      // the database of the definition the statement is in; "" outside any
	Definition  *Definition // the definition the statement is in, or the one it makes; nil outside any
	Defines     bool        // the statement itself defines Definition
	References  []Reference // the names the key and the value refer to
	Scope       string      // the scope script runs in here; "" if unknown
	Root        string      // the definition's root scope; "" if unknown
	Previous    []string    // the scopes entered before this one, innermost first
	Saved       []Saved     // the scopes the definition saved before this statement
	Shape       string      // what kind of block the statement holds, a game.Shapes key; "" if unknown
	Conditional bool        // the statement is inside a trigger block, so a command here must be a trigger
	Category    Category    // the key's color category
	ValueGroup  Category    // the value's color category
	Caller      string      // the scripted definition that a call here is resolved through; "" if none
	Datacontext string      // a GUI block's data context type; "" if none
}

// A Visitor is called by the walk at each statement it enters and leaves.
type Visitor interface {
	// Enter is called when the walk reaches a statement, with its context. Returning false skips the statement's
	// children.
	Enter(tree *jomini.Tree, statement int32, context *Context) bool
	// Leave is called after the walk has finished the statement's children.
	Leave(tree *jomini.Tree, statement int32, context *Context)
}

// Walk goes through a file's tree once and calls the visitor at every statement. At each one it decides what the
// schema and the game's rules say about it: the field, the scope, the shape, the definition and the references.
// Nothing else in the app walks a file.
func (schema *Schema) Walk(tree *jomini.Tree, file files.File, visitor Visitor) { panic("stub") }

// WalkTo is Walk restricted to one position: it only enters statements that cover the given byte offset, so the
// visitor sees the path to that place and nothing else.
func (schema *Schema) WalkTo(tree *jomini.Tree, file files.File, offset int, visitor Visitor) {
	panic("stub")
}

// DeclaresNamespace returns the event namespace a file declares, or "" if it declares none.
func (schema *Schema) DeclaresNamespace(tree *jomini.Tree, file files.File) string { panic("stub") }
