// walk.go goes through a file's tree once and decides what each statement's
// key is, with what the schema knows about it. values.go reads its value.

package schema

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// Category is the color category of a key or a value in the editor. Each
// theme maps a category to a color.
type Category string

// The categories a key or a value can have.
const (
	CategoryNone Category = ""

	// CategoryKeyword is a key of the language itself, such as if, limit,
	// AND or random_list.
	CategoryKeyword Category = "keyword"

	// CategoryCommand is an effect or a trigger.
	CategoryCommand Category = "command"

	// CategoryLink is a scope link, such as root, scope:x or liege.
	CategoryLink Category = "link"

	// CategoryField is a definition's own key.
	CategoryField Category = "field"

	// CategoryDefinition is a name being defined.
	CategoryDefinition Category = "definition"

	// CategoryReference is a name that refers to a definition.
	CategoryReference Category = "reference"

	// CategoryParameter is a parameter, such as $NAME$.
	CategoryParameter Category = "parameter"

	// CategoryConstant is a constant, such as @name.
	CategoryConstant Category = "constant"

	// CategoryCall is a call: a key that calls a definition, or a data call.
	CategoryCall Category = "call"

	// CategoryChoice is one of the values a key takes, such as an event's
	// type.
	CategoryChoice Category = "choice"
)

// Copy is one copy of a key: a name that a statement defines in a database,
// with the facts that decide which copy of that name the game keeps.
type Copy struct {
	Database string `json:"database"`
	Name     string `json:"name"`

	// Span is where the name alone is written, without quotes or an entry
	// mode.
	Span jomini.Span `json:"span"`

	// Statement is the statement that writes the name. It is 0, the file
	// itself, where the file's name is the name.
	Statement int32 `json:"statement"`

	// Mode is the prefix of the entry mode written before the name, such as
	// INJECT or REPLACE. It is "" if none is written.
	Mode string `json:"mode"`

	// Priority is the override priority the definition writes at the key its
	// database's precedence names. It is 0 if none is written.
	Priority int `json:"priority"`

	// Local is whether only the definition's own file sees it.
	Local bool `json:"local"`

	// Alias is whether the name is the value of a key that its database also
	// names definitions by.
	Alias bool `json:"alias"`

	// Sets is whether the name is one a nesting only switches on, as a key
	// in parameters = { … } is.
	Sets bool `json:"sets"`
}

// IsOwn reports whether the copy is a definition of its own. A second name
// of another definition is none, nor a name a nesting only switches on, nor
// a name a command saves while the game runs, such as a saved scope.
func (c *Copy) IsOwn() bool {
	return !c.Alias && !c.Sets && !slices.Contains(game.Links, c.Database)
}

// Reference is a name that a key or a value refers to, in a database, with
// the caller it was resolved through.
type Reference struct {
	Database string `json:"database"`
	Name     string `json:"name"`

	// Span is where the name alone is written, without quotes or a prefix.
	Span jomini.Span `json:"span"`

	// Caller is the caller the name is resolved through. It is "" if none.
	Caller string `json:"caller,omitempty"`

	// Inferred is whether the reference is never an error: it rests only on
	// the game's own files, or its database needs no entry.
	Inferred bool `json:"inferred,omitempty"`
}

// ToID returns the id of a referenced name: its database, a colon and its
// name.
func (r Reference) ToID() string { return r.Database + ":" + r.Name }

// Document is one file parsed in the dialect of its format and walked. A
// localization file's tree holds its text and lines only; its messages are
// what it says, each with the walk's context of it in MessageContexts. A
// script file has the walk's context of each statement of its tree in
// Contexts. Any other file has neither.
type Document struct {
	File            files.File
	Tree            *jomini.Tree
	Messages        *loc.File
	Contexts        []Context
	MessageContexts []Context
}

// Saved is one scope that the definition saved before the current
// statement, with the type it holds.
type Saved struct {
	Name string `json:"name"`

	// Type is the scope's type. It is "" if it is not known.
	Type string `json:"type"`
}

// Context is what the walk decided at one statement.
type Context struct {
	// Field is the field the key matched. It is nil if no source knows the
	// key.
	Field *Field

	// Database is the database of the definition the statement is in, and
	// Definition that definition, or the one the statement makes. They are ""
	// and nil outside any definition.
	Database   string
	Definition *Copy

	// Defines is whether the statement itself defines Definition.
	Defines bool

	// References are the names the key and the value refer to.
	References []Reference

	// Scope is the scope script runs in here, and Root the definition's root
	// scope. Each is "" if it is not known.
	Scope string
	Root  string

	// Previous are the scopes entered before this one, innermost first.
	Previous []string

	// Saved are the scopes the definition saved before the statement.
	Saved []Saved

	// Path is the keys that lead to the statement's block, from its
	// definition or the top of the file.
	Path []string

	// Label is what the statement is written under: its key, or the tag of a
	// block with no key.
	Label string

	// Shape is the shape of the block the statement holds, a key of
	// game.Shapes. It is "" if it is not known.
	Shape game.Shape

	// Conditional is whether the statement is inside a trigger block, so a
	// command here must be a trigger.
	Conditional bool

	// Preview is whether the statement is inside a block that the tooltip
	// shows and the game never runs.
	Preview bool

	// Category is the key's color category.
	Category Category

	// Command is the effect, the trigger or the link of the dumps that the
	// key names, as its block takes it. It is nil if the key names none.
	Command *Definition

	// Callee is the scripted definition the key calls. It is nil for any
	// other key.
	Callee *Definition

	// ValueGroup is the value's color category.
	ValueGroup Category

	// Call is the data call the value writes. It is nil if none.
	Call *jomini.Call

	// Keyword is the keyword the key is. It is nil for any other key.
	Keyword *game.Keyword

	// Iterates is whether the key runs its block on the items of a list, as
	// every_vassal does.
	Iterates bool

	// Datacontext is the data context type of a GUI block. It is "" if none.
	Datacontext string

	// Namespaces are the statements that declare an event namespace, in
	// order. Only the file's own context holds them.
	Namespaces []int32

	// Parent is the context of the block that holds the statement, the
	// file's own at the top.
	Parent *Context
}

// walker is one descent over one file. It keeps what the descent carries
// from one statement to the next.
type walker struct {
	schema *Schema
	row    *game.Game
	tree   *jomini.Tree
	file   files.File

	// contexts holds the context of each statement, as the tree lists them.
	contexts []Context

	// declarations is whether the walk reads only what the file declares,
	// and body whether it is inside a definition whose calls take parameters.
	declarations bool
	body         bool

	rel string

	// stem is the file's name without its folder and extension.
	stem string

	// database is the database of the file's folder. It is nil for a file of
	// no folder.
	database *Database

	fileType game.FileType

	// declaring holds, by key, the database whose definition the key
	// declares inline. It is nil outside an events file.
	declaring map[string]string

	// inline holds the names the file declares for itself: every one of an
	// events file, and those a GUI file has declared so far.
	inline map[string]*Definition

	// winners holds what the files of the open workspace's mods declare, as
	// lookupScripted reads it and as it was when the walk began.
	winners map[string]*Definition

	// frames holds the frame of each level: frames[0] is the file's, and
	// frames[n] that of the statements n levels down.
	frames []frame

	// saved are the scopes the current definition has saved so far.
	saved []Saved
}

// frame is what the walk keeps for the statements at one level of depth.
type frame struct {
	// context is the context of the statement last read at this level.
	context *Context

	// label is what the statement last read at this level is written under,
	// span where it is written, and class what it is written as.
	label string
	span  jomini.Span
	class jomini.Class

	// path is the keys that lead to the statement's children, from the
	// definition that holds it or the top of the file.
	path []string

	// wraps is whether the statement's children are written as definitions.
	wraps bool
}

// match is what a key that is no keyword is in its block.
type match struct {
	// field is the field the keys inside the key's block come from.
	field *Field

	// definition is the effect, the trigger, the link or the pattern of the
	// dumps that has the key. It is nil where none does.
	definition *Definition

	// mark is the take mark of the rule that has the key where no dump's
	// database does. It is "" otherwise.
	mark string

	// callee is the scripted definition that a known call names, and caller
	// the caller it is called through.
	callee *Definition
	caller string

	// patterns are the patterns that make the key.
	patterns []*Definition

	// iterator is the list iterator that the key starts, nil for none.
	iterator *game.Keyword

	// target is the scope that the command or the link leads to.
	target string
}

// isLink reports whether the key is a link.
func (m match) isLink() bool {
	return m.definition != nil && m.definition.Database == game.DumpLinks
}

// moves reports whether the key moves the scope inside its block: an
// iterator, a link, or a command that leads to a scope or walks a list.
func (m match) moves() bool {
	return m.iterator != nil || m.definition != nil && (m.isLink() || m.target != "" || m.definition.Iterates)
}

// category returns the color category of the key: a link or a command for a
// definition of the dumps, else what its mark says.
func (m match) category() Category {
	switch {
	case m.isLink():
		return CategoryLink
	case m.definition != nil:
		return CategoryCommand
	case m.mark == takeCalls, m.mark == takeUnknown:
		return CategoryCall
	case m.mark == takeModifierTypes:
		return CategoryReference
	case m.mark != "":
		return CategoryField
	}

	return CategoryNone
}

// shapeRow is how a block of one shape reads its keys and values.
type shapeRow struct {
	// takes are what a key that is no keyword is tried as, in order: a
	// database of the dumps by its name, or a take mark. The first that has
	// the key wins.
	takes []string

	// caller is the caller a call here is looked up by, among
	// game.Declaration.Callers. game.AnyCall means every callable.
	caller string

	// values is the database every value in the block names. It is "" for
	// none.
	values string

	// blocks is the shape of a block written in it where nothing takes its
	// key. It is "" for none.
	blocks game.Shape

	// encloses is the shape of the block that a link or an enclosing keyword
	// in it holds. It is "" for the shape itself.
	encloses game.Shape
}

// The take marks: what a block takes a key as, beside the names of the
// dumps' databases.
const (
	// takeCalls is a scripted definition the block calls, the file's own
	// first.
	takeCalls = "calls"

	// takeDeclared is a key the block's field holds from any source but the
	// game's own files, and takeObserved one only those files write.
	takeDeclared = "declared"
	takeObserved = "observed"

	// takeDocumented is a key the docs write for the block's database.
	takeDocumented = "documented"

	// takeOwn is any key the block's field has a child for.
	takeOwn = "own"

	// takeBareLinks is a link, but no plain name with a plain value, which is
	// an argument or a call.
	takeBareLinks = "bare links"

	// takeModifiers is a modifier the dumps declare, and takeModifierTypes
	// any identifier, as a modifier type's name.
	takeModifiers     = "modifiers"
	takeModifierTypes = "modifier types"

	// takeUnknown is any identifier that is no event id, as a call of the
	// block's caller to a name nothing defines.
	takeUnknown = "unknown"
)

// shapeRows holds the row of each shape a block can have. A description
// block's keys are all keywords. A key that nothing explains in a command's
// block is a call of any callable, never judged: it may be an argument no
// dump or file writes.
var shapeRows = map[game.Shape]shapeRow{
	game.ShapeFields:     {takes: []string{takeOwn}},
	game.ShapeParameters: {takes: []string{takeOwn}},
	game.ShapeTriggers: {
		takes:  []string{takeCalls, takeDeclared, game.DumpTriggers, takeObserved, game.DumpLinks, takeUnknown},
		caller: game.CallTrigger,
	},
	game.ShapeEffects: {
		takes:  []string{takeCalls, takeDeclared, game.DumpEffects, takeObserved, takeBareLinks, takeUnknown},
		caller: game.CallEffect,
	},
	game.ShapeArguments: {
		takes: []string{
			takeCalls, takeDeclared, game.DumpEffects, game.DumpTriggers, takeObserved, game.DumpLinks, takeUnknown,
		},
		caller: game.AnyCall,
	},
	game.ShapeEffectArguments: {
		takes:  []string{takeCalls, takeDeclared, game.DumpEffects, takeObserved, takeBareLinks, takeUnknown},
		caller: game.AnyCall, encloses: game.ShapeEffects,
	},
	game.ShapeTriggerArguments: {
		takes:  []string{takeCalls, takeDeclared, game.DumpTriggers, takeObserved, takeBareLinks, takeUnknown},
		caller: game.AnyCall, encloses: game.ShapeTriggers,
	},
	game.ShapeValue: {
		takes: []string{
			takeCalls, takeDeclared, game.DumpEffects, game.DumpTriggers, takeObserved, game.DumpLinks, takeUnknown,
		},
		caller: game.CallValue,
	},
	game.ShapeUnknown: {
		takes: []string{
			takeCalls, takeDeclared, game.DumpEffects, game.DumpTriggers, game.DumpLinks, takeOwn, takeUnknown,
		},
		caller: game.AnyCall,
	},
	game.ShapeSetup: {
		takes: []string{
			takeDeclared, takeDocumented, takeCalls, game.DumpEffects, takeObserved, game.DumpLinks, takeUnknown,
		},
		caller: game.CallEffect,
	},
	game.ShapeWidget: {
		takes: []string{takeCalls, takeOwn, takeUnknown}, caller: game.GUI.TagCalls, blocks: game.ShapeWidget,
	},
	game.ShapeModifiers: {takes: []string{takeModifiers, takeDeclared, takeObserved, takeModifierTypes}},
	// _events.info:313-318 writes a description's texts as localization keys
	game.ShapeDescription: {values: game.Localization},
}

// linkMakers holds the commands that save a name for one of game.Links. A
// row is a command, or command/key for a key inside the command's block, and
// its value is the link that reads the name back.
var linkMakers = map[string]string{
	"save_scope_as": game.SavedScopes, "save_temporary_scope_as": game.SavedScopes,
	"save_scope_value_as/name": game.SavedScopes, "save_temporary_scope_value_as/name": game.SavedScopes,
	"save_opinion_value_as/name": game.SavedScopes, "save_temporary_opinion_value_as/name": game.SavedScopes,

	// a script value's: CK3's common/script_values, and the block EU5's
	// generic_actions/western_schism.txt:361 writes
	"save_temporary_value_as": game.SavedScopes, "save_temporary_value_as/name": game.SavedScopes,

	// CK3's _character_interactions.info:349 and _decisions.info:315, 395:
	// the option picked sets scope:<name>
	"send_option/flag": game.SavedScopes, "item/value": game.SavedScopes,

	// EU5's cabinet_actions/assimilate_area.txt:16: the target picked is
	// scope:<name>
	"select_trigger/target_flag": game.SavedScopes,

	"set_variable": game.Variables, "set_variable/name": game.Variables,
	"set_dead_character_variable/name": game.Variables, "add_to_variable_list/name": game.Variables,
	"add_to_variable_map/name": game.Variables,

	"set_local_variable": game.LocalVariables, "set_local_variable/name": game.LocalVariables,
	"add_to_local_variable_list/name": game.LocalVariables, "add_to_local_variable_map/name": game.LocalVariables,

	"set_global_variable": game.GlobalVariables, "set_global_variable/name": game.GlobalVariables,
	"add_to_global_variable_list/name": game.GlobalVariables, "add_to_global_variable_map/name": game.GlobalVariables,

	// CK3 effects.log:68, "add_to_list = { name = <name_of_list> value =
	// <script_value> }"
	"add_to_list": game.Lists, "add_to_temporary_list": game.Lists,
	"add_to_list/name": game.Lists, "add_to_temporary_list/name": game.Lists,

	// CK3's effects.log:2984 and 3014: "where X is the name of the flag",
	// "flag = <name>"
	"add_character_flag": game.Flags, "add_character_flag/flag": game.Flags, "add_dead_character_flag/flag": game.Flags,
}

// Walk decides what each statement of a file is. It returns one context for
// each statement of the tree, in the tree's order; the first is the file's
// own. It fills the given list where that is long enough, so a caller that
// walks many files keeps one list, and makes a new one for nil.
func (s *Schema) Walk(tree *jomini.Tree, file files.File, contexts []Context) []Context {
	walker := s.newWalker(tree, file, contexts)
	walker.walkChildren(0, 1)

	return walker.contexts
}

// WalkDeclarations is Walk for the pass that reads what a file declares. It
// decides the top statements, the statements of a top block that defines
// nothing, and the body of a definition whose calls take parameters. Every
// other statement gets an empty context.
func (s *Schema) WalkDeclarations(tree *jomini.Tree, file files.File, contexts []Context) []Context {
	walker := s.newWalker(tree, file, contexts)

	// a kept list still holds the last file's contexts, and this walk leaves
	// most statements alone; each keeps only the room of its references
	for position := 1; position < len(walker.contexts); position++ {
		walker.contexts[position] = Context{References: walker.contexts[position].References[:0]}
	}

	walker.declarations = true
	walker.body = walker.startsBody(&walker.contexts[0])
	walker.walkChildren(0, 1)

	return walker.contexts
}

// newWalker returns the walker for one file, with an empty context for each
// statement. Where the file's database names its one definition by the
// file's name, the file's own context holds that definition. Only an events
// file declares definitions inline.
func (s *Schema) newWalker(tree *jomini.Tree, file files.File, contexts []Context) *walker {
	row := game.LookupGame(s.Game)
	if row == nil {
		row = &game.Game{}
	}

	mods := s.mods.Load()
	if mods == nil {
		mods = &modScripted{}
	}

	if cap(contexts) < len(tree.Statements) {
		contexts = make([]Context, len(tree.Statements))
	}

	contexts = contexts[:len(tree.Statements)]
	walker := &walker{
		schema: s, row: row, tree: tree, file: file, contexts: contexts, inline: map[string]*Definition{},
		winners: mods.winners,
	}
	walker.rel = file.StageRel()
	walker.stem = strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
	walker.fileType = game.FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = s.FindDatabase(file)
	}

	// the walk writes every other context of the list as it reaches it
	root := &contexts[0]
	*root = Context{Shape: game.ShapeUnknown, References: root.References[:0]}
	walker.frames = []frame{{context: root, wraps: true}, {}}
	if database := walker.database; database != nil {
		if database.Declaration.Contents == game.ContentsEvents {
			root.Namespaces = listNamespaces(tree)
			walker.declaring = s.InlineDatabases
			walker.declareInline()
		}

		if database.Declaration.DefinedBy == game.DefinedByFile && len(tree.Statements) > 1 {
			walker.resolveDefinition(root, Copy{Database: database.Name, Name: walker.stem, Local: database.Declaration.Local})
		}
	}

	return walker
}

// declareInline records each definition that an events file declares inline
// at its top, with what its parameters take. The games call such a
// definition above the line that declares it, as CK3's events under
// events/dlc/ep3 do, so the names and the parameters are read before any
// script.
func (w *walker) declareInline() {
	var bodies []int32
	for statement := range w.tree.Children(0) {
		label, _, _ := w.findLabel(w.tree.Statements[statement])
		if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
			w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
			bodies = append(bodies, statement)
		}
	}

	// each body is walked once before the file is, so that a call above its
	// definition already finds what the parameters take
	for _, statement := range bodies {
		label, _, _ := w.findLabel(w.tree.Statements[statement])
		w.walkStatement(statement, 1)
		for position := statement + 1; position < w.tree.Statements[statement].Last; position++ {
			w.schema.typeDefinition(w.inline[label], listTypings(w.tree, position, &w.contexts[position]))
		}
	}

	w.saved = nil
}

// listNamespaces returns the top statements of a tree that declare an event
// namespace, in the order they are written.
func listNamespaces(tree *jomini.Tree) []int32 {
	var statements []int32
	for statement := range tree.Children(0) {
		if tree.Text(tree.Statements[statement].Key) == game.EventNamespaceKey {
			statements = append(statements, statement)
		}
	}

	return statements
}

// walkChildren walks the children of a statement, one level down. A
// definition's children are walked in the order its database says they run,
// so each starts with the scopes the earlier ones saved.
func (w *walker) walkChildren(parent int32, level int) {
	for len(w.frames) <= level {
		w.frames = append(w.frames, frame{})
	}

	order := w.findRunOrder(level)
	if len(order) == 0 {
		for child := range w.tree.Children(parent) {
			w.walkStatement(child, level)
		}

		return
	}

	rank := func(child int32) int {
		label, _, _ := w.findLabel(w.tree.Statements[child])
		if position := slices.Index(order, label); position >= 0 {
			return position
		}

		return len(order)
	}

	children := slices.Collect(w.tree.Children(parent))
	slices.SortStableFunc(children, func(first, second int32) int { return cmp.Compare(rank(first), rank(second)) })

	for _, child := range children {
		w.walkStatement(child, level)
	}
}

// findRunOrder returns the keys of a definition's blocks in the order they
// run, for the definition that holds the statements of the given level. It
// returns nil for any other block and for a database with no run order.
func (w *walker) findRunOrder(level int) []string {
	context := w.frames[level-1].context
	database := w.schema.Databases[context.Database]
	if !context.Defines || context.Category != CategoryDefinition || database == nil {
		return nil
	}

	return database.Declaration.RunOrder
}

// walkStatement resolves one statement and walks its block. A walk of the
// declarations walks only a top block that defines nothing and the body of a
// definition whose calls take parameters.
func (w *walker) walkStatement(statement int32, level int) {
	context := &w.contexts[statement]
	w.frames[level].context = context
	w.resolve(context, statement, level)
	if !w.tree.Statements[statement].IsBlock() {
		return
	}

	if w.declarations && !w.body {
		starts := w.startsBody(context)
		if !starts && (context.Defines || level > 1) {
			return
		}

		w.body = starts
		defer func() { w.body = false }()
	}

	w.walkChildren(statement, level+1)
}

// startsBody reports whether a context makes a definition whose calls take
// parameters, so that the pass over the declarations reads its body.
func (w *walker) startsBody(context *Context) bool {
	if !context.Defines || context.Category != CategoryDefinition || context.Definition.Local {
		return false
	}

	database := w.schema.Databases[context.Definition.Database]

	return database != nil && database.Declaration.Parameters
}

// resolve fills the context of a statement from the context of the block
// that holds it.
func (w *walker) resolve(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	*context = Context{
		Parent: parent, Database: parent.Database, Definition: parent.Definition, Scope: parent.Scope, Root: parent.Root,
		Previous: parent.Previous, Saved: w.saved,
		Conditional: parent.holdsConditions(), Preview: parent.Preview,
		Datacontext: parent.Datacontext, References: context.References[:0],
	}

	label, span, class := w.findLabel(w.tree.Statements[statement])
	w.frames[level].label, w.frames[level].span, w.frames[level].class, w.frames[level].wraps = label, span, class, false
	definition, defines := w.findDefinition(statement, level)
	if defines {
		w.resolveDefinition(context, definition)
	} else {
		w.resolveStatement(context, statement, level)
	}

	context.Path, context.Label = w.frames[level-1].path, label
	if w.tree.Statements[statement].IsBlock() {
		keys := w.frames[level-1].path
		if label != "" {
			keys = slices.Concat(keys, []string{label})
		}

		// a definition of the file's own database starts the keys anew
		if defines && w.database != nil && definition.Database == w.database.Name {
			keys = nil
		}

		w.frames[level].path = keys
	}

	// a GUI block's datacontext key sets its data context, unknown if the
	// dumps do not resolve the call
	if w.file.Format == files.FormatGUI && w.tree.Statements[statement].IsBlock() {
		if child, ok := w.tree.FindChild(statement, game.GUI.ContextKey); ok {
			context.Datacontext = ""
			if call, ok := w.tree.ParseBracketed(w.tree.Statements[child].Value); ok {
				context.Datacontext = w.schema.ResolveDatacontext(call, parent.Datacontext)
			}
		}
	}

	w.lookupValue(context, statement, level)
	judge(context.References)
}

// findLabel returns what a statement is written under, its key or else the
// tag of a block that has no key, where that is written, and its class. It
// returns "", an empty span and ClassNone for a value written alone.
func (w *walker) findLabel(current jomini.Statement) (string, jomini.Span, jomini.Class) {
	var span jomini.Span
	var class jomini.Class
	switch {
	case !current.Key.IsEmpty():
		span, class = current.Key, current.KeyClass
	case !current.Tag.IsEmpty() && current.IsBlock():
		span, class = current.Tag, current.TagClass
	default:
		return "", jomini.Span{}, jomini.ClassNone
	}

	span = w.tree.UnquotedSpan(span)

	return w.tree.Text(span), span, class
}

// resolveDefinition fills the context of the statement that makes a
// definition. The definition starts with no saved scopes.
func (w *walker) resolveDefinition(context *Context, definition Copy) {
	database := w.schema.Databases[definition.Database]
	*context = Context{
		Parent: context.Parent, Database: definition.Database, Definition: &definition, Defines: true,
		Category: CategoryDefinition, Shape: w.findDefinitionShape(database), References: context.References[:0],
	}
	if database != nil {
		context.Field = database.Field
	}

	context.Root = w.findRootScope(database, definition)
	context.Scope = context.Root
	w.saved = nil
}

// findDefinitionShape returns the shape of a definition's block in a
// database: its callable's, else its row's, else its file type's, else
// fields. A nil database gives its file type's, else fields.
func (w *walker) findDefinitionShape(database *Database) game.Shape {
	if database == nil {
		return cmp.Or(w.fileType.Shape, game.ShapeFields)
	}

	return cmp.Or(database.Declaration.Shape, w.fileType.Shape, game.ShapeFields)
}

// findRootScope returns the scope a definition's script starts in: the scope
// its root key names, else its event type's scope, or its on_action's scope
// in the dumps. It returns "" if nothing says, or if the root key names no
// scope.
func (w *walker) findRootScope(database *Database, definition Copy) string {
	if database == nil {
		return ""
	}

	if value, ok := w.tree.FindChildValue(definition.Statement, database.Declaration.RootKey); ok {
		if w.schema.isScope(value) {
			return value
		}

		return ""
	}

	switch database.Declaration.Contents {
	case game.ContentsEvents:
		events := w.row.Events
		eventType, _ := w.tree.FindChildValue(definition.Statement, events.TypeKey)

		return cmp.Or(strings.TrimSuffix(eventType, events.TypeSuffix), events.DefaultScope)

	case game.ContentsOnActions:
		if declared := database.Definitions[definition.Name]; declared != nil {
			return declared.Target
		}
	}

	return ""
}

// resolveStatement fills the context of a statement that defines nothing. A
// keyed statement takes what its key fits, a block with no key its holder's
// shape, field and scope, and a value written alone its holder's field. A
// top statement of a database whose definitions have no name is in its one
// definition.
func (w *walker) resolveStatement(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	switch {
	// a GUI block is also overridden as blockoverride = "name" { … }, the
	// name read as the block's tag, as CK3 gui/interaction_marriage.gui:190
	// writes. Its braces hold what the widget around it holds.
	case w.file.Format == files.FormatGUI && label == game.GUI.BlockCalls && !current.Tag.IsEmpty():
		name := w.tree.UnquotedSpan(current.Tag)
		w.resolveLead(context, label, w.tree.Text(name), name)
		context.Shape, context.Field, context.Scope = parent.Shape, parent.Field, parent.Scope

	case label != "":
		w.frames[level].wraps = w.frames[level-1].wraps && w.database != nil &&
			slices.Contains(w.database.Declaration.WrapperKeys, label)
		w.resolveKey(context, statement, level)
		w.resolveLead(context, w.findLead(statement), label, span)

	case current.IsBlock():
		context.Shape, context.Field, context.Scope = parent.Shape, parent.Field, parent.Scope

	case current.IsListed():
		context.Field = parent.Field
	}

	if database := w.database; level == 1 && database != nil && database.Declaration.DefinedBy == game.DefinedByNothing {
		context.Database, context.Field, context.Shape = database.Name, database.Field, w.findDefinitionShape(database)
	}
}

// findLead returns the value written alone just before a statement, where it
// makes the statement a definition or a call. It returns "" otherwise.
func (w *walker) findLead(statement int32) string {
	lead, ok := w.tree.FindLead(statement)
	if !ok {
		return ""
	}

	text := w.tree.Text(w.tree.Statements[lead].Value)
	if !w.isLead(text) {
		return ""
	}

	return text
}

// isLead reports whether a value written alone makes the statement after it
// a definition or a call: a key that declares a definition inline, or in a
// GUI file a declaration or a key that calls definitions.
func (w *walker) isLead(text string) bool {
	if w.declaring[text] != "" {
		return true
	}

	return w.file.Format == files.FormatGUI && (slices.Contains(game.GUI.Declares, text) || len(w.schema.Callees[text]) > 0)
}

// resolveLead resolves the references of a call written as a lead and a
// label, such as blockoverride "label" in a GUI file. The label refers to a
// name in each database that the lead calls.
func (w *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range w.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}

	if lead != "" && len(w.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// resolveKey fills the context of a keyed statement. A key that is a keyword
// where it is written is that and nothing else: a rule gives it only the
// field and the command of its own key. Any other key takes what the first
// rule of its block's shape gives. A key written as a command's name with an
// argument in parentheses is read as the name, and its argument makes its
// own references.
func (w *walker) resolveKey(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	key, span, class := w.frames[level].label, w.frames[level].span, w.frames[level].class

	// a key with a parameter is unknown until the call is made, and so are
	// its block's shape and scope
	if class == jomini.ClassParameter {
		context.Category, context.Shape, context.Scope = CategoryParameter, game.ShapeUnknown, ""

		return
	}

	step, command, isCall := w.schema.findStatementCall(w.tree, statement)
	if isCall {
		key, span, class = step.Text, step.Span, jomini.Classify(step.Text)
	}

	block := w.tree.Statements[statement].IsBlock()
	keyword := w.findKeyword(parent, w.frames[level-1].label, key, block)
	fit, fits := w.findFit(parent, keyword, key, class, block, isCall)
	w.resolveMatch(context, parent, fit, fits, keyword, key, isCompared(w.tree, statement))
	if keyword == nil {
		w.resolveCallReferences(context, parent, fit, key, span)
	}

	// a use covers the whole key, so it comes after the names inside the key
	context.References = append(context.References, listUses(w.schema, fit, key, span)...)

	if isCall {
		command = cmp.Or(context.Command, command)
		context.References = append(context.References,
			w.listArgumentReferences(command, parent.FindArguments(command), step.Arguments[0])...)
	}

	// a math keyword's value is a number or a script value, so it takes no
	// field the game's files taught
	if keyword != nil && keyword.Class != game.KeywordClassMath && context.Field == nil {
		context.Field = findKeywordField(parent, key, class)
	}

	w.resolveSaved(context, statement, level)
	w.resolveAlias(context, statement, level)
}

// findFit returns what a key is in the block of a context, given the
// keyword the key is there, nil for none, and whether it is anything. A
// keyword takes only the command of its own key: where the dumps declare
// one, as they declare if, their entry gives its keys, and a keyword that
// stands for any key, as a switch's case does, has none. Any other key is
// the first thing the block's shape takes that has it. The flags say whether
// the key's statement is a block and whether the key is written with an
// argument in parentheses. The match names the scope a link or a command
// leads to, and the iterator a key that is no keyword starts.
func (w *walker) findFit(parent *Context, keyword *game.Keyword, key string, class jomini.Class, block, isCall bool) (match, bool) {
	var fit match
	fits := false
	switch {
	case keyword == nil:
		fit, fits = w.lookupKey(parent, key, class, block)
	case strings.EqualFold(keyword.Key, key):
		fit, fits = w.findCommand(parent, key)
	}

	// a link that requires data takes it in parentheses too, as
	// relations(scope:overlord) does, though its name alone is no link
	if link := w.schema.lookupLink(key, true); isCall && fit.mark == takeUnknown && link != nil {
		fit, fits = match{definition: link, field: parent.Field}, true
	}

	// a key that starts an iterator and is no keyword runs its block in the
	// scope of the list's item
	iterator, isIterator := game.MatchListIterator(key)
	command := fit.definition != nil && !fit.isLink()
	if isIterator && keyword == nil && (command || fit.mark == takeUnknown && w.schema.Lists != "") {
		fit.iterator = new(iterator)
	}

	switch {
	case fit.isLink():
		chain := w.schema.ResolveChain(key, parent, w.saved)
		fit.target = chain[len(chain)-1].Scope

	case fit.definition != nil:
		fit.target = fit.definition.Target
	}

	return fit, fits
}

// findCommand returns the effect or the trigger that a key names and the
// block of a context takes, in the order the block's shape tries the two.
// It returns false if the dumps declare none, or the block takes neither.
func (w *walker) findCommand(parent *Context, key string) (match, bool) {
	for _, entry := range shapeRows[findShape(parent)].takes {
		if entry != game.DumpEffects && entry != game.DumpTriggers {
			continue
		}

		if fit, ok := w.takeCommand(entry, parent, key); ok {
			return fit, true
		}
	}

	return match{}, false
}

// findKeywordField returns the field of a keyword that no rule gave one: the
// field of its key in the block that holds it. Under an enclosing keyword
// that block's field is another block's, so the field is the one the dumps
// give the key above its key, as they give an if its limit. It returns nil
// if neither has one.
func findKeywordField(parent *Context, key string, class jomini.Class) *Field {
	if field := parent.Field.findPatternChild(key, class); field != nil || parent.Command == nil {
		return field
	}

	return parent.Command.Field.findChild(key)
}

// findShape returns the shape of the block that a context holds, or
// game.ShapeUnknown if it holds none.
func findShape(context *Context) game.Shape {
	return cmp.Or(context.Shape, game.ShapeUnknown)
}

// findKeyword returns the keyword a key is in the block of a context, which
// is written under the given key. Inside a keyword whose block may hold the
// same keyword again, as Vic3's else in a progress bar, the same key is that
// keyword. It returns nil if the key is no keyword there.
func (w *walker) findKeyword(parent *Context, holder, key string, block bool) *game.Keyword {
	if outer := parent.Keyword; outer != nil && outer.Recursive && strings.EqualFold(outer.Key, key) {
		return outer
	}

	row, ok := w.row.LookupKeyword(findShape(parent), holder, key, block)
	if !ok {
		return nil
	}

	return &row
}

// findEnclosing returns the shape of the block that a link or an enclosing
// keyword holds, written in the block of a context: the Encloses of the
// context's shape, else the context's own shape.
func findEnclosing(context *Context) game.Shape {
	return cmp.Or(shapeRows[findShape(context)].encloses, findShape(context))
}

// lookupKey returns what a key that is no keyword is in the block of a
// context: the first thing the block's shape takes, in order, that has the
// key. It returns false if nothing takes the key. The flag says whether the
// key's statement is a block.
func (w *walker) lookupKey(parent *Context, key string, class jomini.Class, block bool) (match, bool) {
	for _, entry := range shapeRows[findShape(parent)].takes {
		if fit, ok := w.take(entry, parent, key, class, block); ok {
			return fit, true
		}
	}

	return match{}, false
}

// take returns what a key is under one entry of a shape's takes, a database
// of the dumps or a take mark, and whether that entry has the key. A number
// or a date fits a field's <number> child, and under takeOwn its <any>
// child too. A key of takeUnknown calls through the block's caller.
func (w *walker) take(entry string, parent *Context, key string, class jomini.Class, block bool) (match, bool) {
	switch entry {
	case takeCalls:
		// the keys in a call's block are the definition's parameters, else
		// the fields of its database
		callee, caller, ok := w.findCalledDefinition(parent.FindCaller(), key)
		if !ok {
			return match{}, false
		}

		field := callee.Field
		if database := w.schema.Databases[callee.Database]; field == nil && database != nil {
			field = database.Field
		}

		return match{mark: entry, field: field, callee: callee, caller: caller}, true

	case takeDeclared:
		child := parent.Field.findNumberChild(key, class)

		return match{mark: entry, field: child}, child != nil && child.Source != game.SourceNone &&
			child.Source != game.SourceVanilla

	case takeObserved:
		child := parent.Field.findNumberChild(key, class)

		return match{mark: entry, field: child}, child != nil && child.Source == game.SourceVanilla

	case takeDocumented:
		database := w.schema.Databases[parent.Database]

		return match{mark: entry, field: parent.Field.findChild(key)}, database != nil && database.declares(key)

	case takeOwn:
		child := parent.Field.findPatternChild(key, class)

		return match{mark: entry, field: child}, child != nil

	case takeModifiers:
		return match{mark: entry, field: w.findModifierField(parent.Field, key)}, w.schema.declaresModifier(key)

	case takeModifierTypes:
		return match{mark: entry, field: w.findModifierField(parent.Field, key)},
			w.schema.ModifierTypes != "" && class.IsIdentifier()

	case takeUnknown:
		return match{mark: entry, caller: parent.FindCaller()}, class.IsIdentifier() && class != jomini.ClassEventID

	case game.DumpLinks, takeBareLinks:
		return w.takeLink(entry, parent, key, block)
	}

	return w.takeCommand(entry, parent, key)
}

// takeLink returns the link that a key starts with, as scope:target does.
// Under takeBareLinks a plain name with a plain value is none: it is an
// argument or a call. A link's block keeps the field of the block around
// it.
func (w *walker) takeLink(entry string, parent *Context, key string, block bool) (match, bool) {
	if entry == takeBareLinks && !block && !strings.ContainsAny(key, ":.") {
		return match{}, false
	}

	link := w.schema.FindLink(key)

	return match{definition: link, field: parent.Field}, link != nil
}

// takeCommand returns the effect or the trigger of the given database that a
// key names: the one the dumps declare, else the one a pattern makes. It
// returns false if there is none, and for an effect in a block of triggers.
// The match holds the keys the command takes in the block.
func (w *walker) takeCommand(database string, parent *Context, key string) (match, bool) {
	if !parent.Takes(database) {
		return match{}, false
	}

	fit := match{definition: w.schema.LookupDefinition(database, key), patterns: w.schema.findPatterns(database, key)}
	if fit.definition == nil && len(fit.patterns) > 0 {
		fit.definition = fit.patterns[0]

		// a list iterator a pattern makes takes the plain iterator of its
		// scope, as any_character, for its keys
		if iterator, listed := game.MatchListIterator(key); listed {
			plain := strings.TrimSuffix(iterator.Key, "*") + fit.definition.Target
			fit.definition = cmp.Or(w.schema.LookupDefinition(database, plain), fit.definition)
		}
	}

	if fit.definition == nil {
		return match{}, false
	}

	fit.field = parent.FindArguments(fit.definition)

	return fit, true
}

// findMatchShape returns the shape of the block of a key that fits. A link's
// block has the shape a link encloses in the block around it. A call to a
// scripted definition with parameters holds its arguments. A command's block
// has the shape its field states. Without one it is unknown where the field
// holds an effect's and a trigger's keys together, and else the arguments of
// its own kind. Any other block has the shape its field states, or unknown.
func findMatchShape(parent *Context, fit match) game.Shape {
	shape := findBlockShape(fit.field)
	switch {
	case fit.isLink():
		return findEnclosing(parent)
	case fit.callee != nil && fit.callee.Field != nil:
		return game.ShapeParameters
	case shape != "" || fit.definition == nil:
		return cmp.Or(shape, game.ShapeUnknown)
	case fit.field != nil && fit.field == fit.definition.Union:
		return game.ShapeUnknown
	case fit.definition.Database == game.DumpTriggers:
		return game.ShapeTriggerArguments
	}

	return game.ShapeEffectArguments
}

// findBlockShape returns the shape of a field's own block where no rule
// decides it: the shape a source states, else a value block where the field
// takes a script value, since a formula is written "inline wherever scripted
// values work" (CK3 _script_values.info, "Inlining"). It returns "" if
// neither says, and for no field.
func findBlockShape(field *Field) game.Shape {
	switch {
	case field == nil:
		return ""
	case field.Shape.IsStated():
		return field.Shape.Value
	case field.Value.Value.Values:
		return game.ShapeValue
	}

	return ""
}

// findCalledDefinition returns the scripted definition a key calls and the
// caller it is called through, trying the file's inline definitions first.
// A GUI widget type calls an empty one. It returns false if there is none.
func (w *walker) findCalledDefinition(caller, key string) (*Definition, string, bool) {
	if scripted, ok := w.inline[key]; ok {
		callers := w.schema.Databases[scripted.Database].Declaration.Callers
		if len(callers) > 0 && (caller == game.AnyCall || slices.Contains(callers, caller)) {
			return scripted, callers[0], true
		}
	}

	if caller == game.AnyCall {
		return w.schema.findScripted(w.winners, key, w.file.Format)
	}

	callee := w.schema.lookupCall(w.winners, caller, key)
	if callee == nil && caller == game.GUI.TagCalls {
		callee = w.schema.Databases[game.GUI.WidgetTypes].Definitions[key]
	}

	return callee, caller, callee != nil && caller != ""
}

// findModifierField returns the field of a key in a modifiers block: what
// the modifier type of that name takes, else the key's child of the given
// field. It returns nil if none has one.
func (w *walker) findModifierField(holder *Field, key string) *Field {
	if declared := w.schema.lookupScripted(w.winners, w.schema.ModifierTypes, key); declared != nil {
		return declared.Field
	}

	return holder.findChild(key)
}

// FindCaller returns the caller that a call in the block of a context
// resolves through: a keyword's, else the one the block's shape gives.
func (c *Context) FindCaller() string {
	if c.Keyword != nil && c.Keyword.Caller != "" {
		return c.Keyword.Caller
	}

	return shapeRows[findShape(c)].caller
}

// FindArguments returns the keys a command takes in this context's block: its
// own, or the keys of its effect and its trigger together in a block that
// takes either. It returns nil for no command, and for one that takes none.
func (c *Context) FindArguments(command *Definition) *Field {
	switch {
	case command == nil:
		return nil
	case command.Union != nil && c.Takes(game.DumpEffects) && c.Takes(game.DumpTriggers):
		return command.Union
	}

	return command.Field
}

// TakesLinks reports whether a key in this context's block may be a link.
func (c *Context) TakesLinks() bool {
	return c.Takes(game.DumpLinks) || c.Takes(takeBareLinks)
}

// Takes reports whether this context's block takes the keys of the given
// database of the dumps, or the keys a take mark stands for. An effect never
// fits in a block of triggers.
func (c *Context) Takes(entry string) bool {
	return slices.Contains(shapeRows[findShape(c)].takes, entry) &&
		(entry != game.DumpEffects || !c.holdsConditions())
}

// holdsConditions reports whether the statements in the block of a context
// are inside a trigger block. They are when the block is triggers, or sits
// below triggers and is no value block.
func (c *Context) holdsConditions() bool {
	return c.Shape == game.ShapeTriggers || c.Shape == game.ShapeTriggerArguments ||
		c.Conditional && c.Shape != game.ShapeValue
}

// resolveMatch sets the context of a keyed statement from what its key fits
// in the block around it, the keyword it is there, nil for none, and whether
// its block is compared with the key. A keyword that encloses keeps the
// field of the block around it.
func (w *walker) resolveMatch(
	context, parent *Context, fit match, fits bool, keyword *game.Keyword, key string, compared bool,
) {
	context.Field, context.Category = fit.field, fit.category()
	context.Command, context.Callee = fit.definition, fit.callee
	context.Iterates = fit.iterator != nil
	if keyword != nil {
		if keyword.Shape == game.ShapeEnclosing {
			context.Field = parent.Field
		}

		context.Keyword, context.Category = keyword, CategoryKeyword
		context.Preview = context.Preview || keyword.Preview
	}

	context.Shape = findKeyShape(parent, fit, fits, keyword, compared)
	context.Scope = w.findKeyScope(context, parent, fit, fits, keyword, key)
	if fit.moves() {
		context.Previous = slices.Concat([]string{parent.Scope}, parent.Previous)
	}
}

// findKeyShape returns the shape of the block that a keyed statement holds.
// The first rule that holds decides.
func findKeyShape(parent *Context, fit match, fits bool, keyword *game.Keyword, compared bool) game.Shape {
	switch {
	// a block a key is compared with is a formula
	case compared:
		return game.ShapeValue

	// CK3 _script_values.info, "Lists": a list written in a formula holds the
	// formula's math, as add_gold = { every_child = { add = 1 } } does
	case fit.iterator != nil && findShape(parent) == game.ShapeValue:
		return game.ShapeValue

	case fit.iterator != nil:
		return fit.iterator.Shape

	// an enclosing keyword's block is the block around it again
	case keyword != nil && keyword.Shape == game.ShapeEnclosing:
		return findEnclosing(parent)

	// any other keyword's block holds what its row says, where it says
	case keyword != nil && keyword.Shape != "":
		return keyword.Shape

	case fits:
		return findMatchShape(parent, fit)
	}

	return shapeRows[findShape(parent)].blocks
}

// findKeyScope returns the scope script runs in inside the block of a keyed
// statement, whose shape the context already holds. It is "" where it is not
// known. The first rule that holds decides.
func (w *walker) findKeyScope(
	context, parent *Context, fit match, fits bool, keyword *game.Keyword, key string,
) string {
	known := context.Shape != "" && context.Shape != game.ShapeUnknown
	explained := fits && fit.mark != takeObserved
	shaped := keyword != nil && !fits && keyword.Shape != ""
	switch {
	// a call's argument is read inside the called definition, so its scope
	// is not known here
	case context.Shape == game.ShapeParameters:
		return ""

	// an argument is read in a scope the command makes, but a conditional
	// keyword such as limit in the scope of the command that holds it
	case isArgument(parent, key) && (keyword == nil || keyword.Class != game.KeywordClassConditional):
		return ""

	// a link, an iterator or a command that leads to a scope moves it there
	case fit.moves() && w.schema.isScope(fit.target):
		return fit.target

	case fit.moves():
		return ""

	// a key that a source other than the game's own files explains keeps the
	// scope it is written in, and so does a keyword whose row says what its
	// block holds
	case known && (explained || shaped):
		return parent.Scope
	}

	return ""
}

// isArgument reports whether a key in the block of a command is one of the
// command's arguments. The block must hold arguments, and the command's
// arguments must hold the key.
func isArgument(parent *Context, key string) bool {
	return parent.HoldsArguments() && parent.Field.findChild(key) != nil
}

// HoldsArguments reports whether the block of a context holds a command's
// arguments. It does when the command leads to no scope and iterates no
// list, so what is written in the block is read in the scope the command is
// written in.
func (c *Context) HoldsArguments() bool {
	command := c.Command

	return c.Category == CategoryCommand && (command == nil || command.Target == "" && !command.Iterates)
}

// resolveCallReferences adds the references a key makes, by what it is: the
// command or each link of the dumps it uses, the star of each pattern that
// makes it, the names after a link's prefixes, the name a call calls, a
// modifier's type, or a name of the database that the keys of the block
// around it name. A call to an unknown name refers to it in each database
// of its caller, or in the lists for an iterator.
func (w *walker) resolveCallReferences(context, parent *Context, fit match, key string, span jomini.Span) {
	// a key the dumps declare is an argument, as stress_impact's base is, and
	// names nothing
	keys := Claim[string]{}
	if declared := fit.field != nil && fit.field.Source == game.SourceDumps; parent.Field != nil && !declared {
		keys = parent.Field.Keys
	}

	modifies := fit.mark == takeModifiers || fit.mark == takeModifierTypes
	switch {
	case modifies && w.schema.ModifierTypes != "":
		// nothing says that every modifier the dumps declare has a definition
		context.References = append(context.References,
			Reference{Database: w.schema.ModifierTypes, Name: key, Span: span, Inferred: fit.mark == takeModifiers})

	// a modifier in a game whose modifier types have no database of their
	// own names what the keys of its block name, as any field does
	case fit.category() == CategoryField && keys.Value != "":
		context.References = append(context.References,
			Reference{Database: keys.Value, Name: key, Span: span, Inferred: !keys.Source.CanAccuse()})

	case len(fit.patterns) > 0:
		lower := strings.ToLower(key)
		for _, pattern := range fit.patterns {
			start, end, _ := pattern.findStar(lower)
			for _, database := range pattern.Star {
				context.References = append(context.References, Reference{
					Database: database, Name: key[start:end],
					Span: jomini.Span{Start: span.Start + start, End: span.Start + end},
				})
			}
		}

	case fit.isLink():
		context.References = append(context.References, w.listPrefixed(key, span.Start)...)

	case fit.callee != nil && fit.callee.Database != "":
		context.References = append(context.References, Reference{Database: fit.callee.Database, Name: key, Span: span, Caller: fit.caller})

	case fit.mark != takeUnknown: // not a call to an unknown name

	case fit.iterator != nil:
		prefix := strings.TrimSuffix(fit.iterator.Key, "*")
		context.References = append(context.References,
			Reference{Database: w.schema.Lists, Name: strings.TrimPrefix(key, prefix), Span: span})

	case fit.caller == game.AnyCall:
		context.References = append(context.References, Reference{Database: game.AnyCall, Name: key, Span: span, Caller: game.AnyCall})

	default:
		for _, database := range w.schema.Callees[fit.caller] {
			context.References = append(context.References, Reference{Database: database, Name: key, Span: span, Caller: fit.caller})
		}
	}
}

// listUses returns the uses of the dumps' names that a key makes: one of its
// command, or one of each link of its chain.
func listUses(language *Schema, fit match, key string, span jomini.Span) []Reference {
	switch {
	case fit.isLink():
		return language.listLinkUses(key, span.Start)
	case fit.definition != nil:
		return []Reference{{Database: fit.definition.Database, Name: fit.definition.Key, Span: span, Inferred: true}}
	}

	return nil
}

// listLinkUses returns a use of each link of a chain that the dumps declare,
// with where the link is written: its name, or its prefix where it has one.
// The chain starts at the given offset of the file. A use is never an error.
func (s *Schema) listLinkUses(chain string, start int) []Reference {
	var uses []Reference
	for _, link := range jomini.Links(chain, start) {
		span := link.Span
		if !link.Prefix.IsEmpty() {
			span = link.Prefix
		}

		definition := s.lookupLink(chain[span.Start-start:span.End-start], !link.Prefix.IsEmpty())
		if definition != nil {
			uses = append(uses, Reference{Database: game.DumpLinks, Name: definition.Key, Span: span, Inferred: true})
		}
	}

	return uses
}

// linkUsers holds the triggers and lists that read a name saved for one of
// game.Links, keyed as linkMakers is, each with that link. Each is in its
// game's triggers.log or effects.log. Variable maps are Vic3's and EU5's,
// flags CK3's.
var linkUsers = map[string]string{
	"has_variable": game.Variables, "has_variable/name": game.Variables, "has_all_variables/name": game.Variables,
	"has_none_of_variables/name": game.Variables, "has_dead_character_variable": game.Variables,
	"has_variable_list": game.Variables, "variable_list_size/name": game.Variables,
	"is_target_in_variable_list/name": game.Variables, "has_variable_map": game.Variables,
	"variable_map_size/name": game.Variables, "is_key_in_variable_map/name": game.Variables,
	"is_value_in_variable_map/name": game.Variables, "any_key_in_variable_map/variable": game.Variables,
	"every_key_in_variable_map/variable": game.Variables, "ordered_key_in_variable_map/variable": game.Variables,
	"random_key_in_variable_map/variable": game.Variables, "any_in_list/variable": game.Variables,
	"every_in_list/variable": game.Variables, "ordered_in_list/variable": game.Variables,
	"random_in_list/variable": game.Variables,

	"has_local_variable": game.LocalVariables, "has_local_variable_list": game.LocalVariables,
	"local_variable_list_size/name": game.LocalVariables, "is_target_in_local_variable_list/name": game.LocalVariables,
	"has_local_variable_map": game.LocalVariables, "local_variable_map_size/name": game.LocalVariables,
	"is_key_in_local_variable_map/name": game.LocalVariables, "is_value_in_local_variable_map/name": game.LocalVariables,
	"any_key_in_local_variable_map/variable":     game.LocalVariables,
	"every_key_in_local_variable_map/variable":   game.LocalVariables,
	"ordered_key_in_local_variable_map/variable": game.LocalVariables,
	"random_key_in_local_variable_map/variable":  game.LocalVariables, "any_in_local_list/variable": game.LocalVariables,
	"every_in_local_list/variable": game.LocalVariables, "ordered_in_local_list/variable": game.LocalVariables,
	"random_in_local_list/variable": game.LocalVariables,

	"has_global_variable": game.GlobalVariables, "has_global_variable_list": game.GlobalVariables,
	"global_variable_list_size/name":         game.GlobalVariables,
	"is_target_in_global_variable_list/name": game.GlobalVariables,
	"has_global_variable_map":                game.GlobalVariables, "global_variable_map_size/name": game.GlobalVariables,
	"is_key_in_global_variable_map/name":          game.GlobalVariables,
	"is_value_in_global_variable_map/name":        game.GlobalVariables,
	"any_key_in_global_variable_map/variable":     game.GlobalVariables,
	"every_key_in_global_variable_map/variable":   game.GlobalVariables,
	"ordered_key_in_global_variable_map/variable": game.GlobalVariables,
	"random_key_in_global_variable_map/variable":  game.GlobalVariables,
	"any_in_global_list/variable":                 game.GlobalVariables,
	"every_in_global_list/variable":               game.GlobalVariables,
	"ordered_in_global_list/variable":             game.GlobalVariables,
	"random_in_global_list/variable":              game.GlobalVariables,

	"is_in_list": game.Lists, "any_in_list/list": game.Lists, "every_in_list/list": game.Lists,
	"ordered_in_list/list": game.Lists, "random_in_list/list": game.Lists,

	"has_character_flag": game.Flags, "has_dead_character_flag": game.Flags, "remove_character_flag": game.Flags,
}

// resolveSaved makes a name that a statement saves for one of game.Links a
// definition, and one it reads back a reference. A saved scope also joins
// the definition's saved scopes. A name with a parameter is skipped, and a
// save in the block of a command that makes no scope has an unknown type.
func (w *walker) resolveSaved(context *Context, statement int32, level int) {
	current := w.tree.Statements[statement]
	span := w.tree.UnquotedSpan(current.Value)
	label := w.tree.Text(span)
	if current.IsBlock() || label == "" || current.ValueClass == jomini.ClassParameter || strings.HasPrefix(label, "[") {
		return
	}

	key, holder, parent := w.frames[level].label, w.frames[level-1].label, w.frames[level-1].context
	if link, _, ok := findLinked(linkUsers, holder, key); ok {
		context.References = append(context.References, Reference{Database: link, Name: label, Span: span})

		return
	}

	link, nested, ok := findLinked(linkMakers, holder, key)
	scope := parent.Scope
	if nested || parent.HoldsArguments() {
		scope = ""
	}

	if !ok && parent.Callee != nil {
		link, ok = parent.Callee.Saves[key]
		scope = ""
	}

	if !ok {
		return
	}

	context.Definition = &Copy{Database: link, Name: label, Span: span, Statement: statement}
	context.Defines = true
	if link == game.SavedScopes {
		w.saved = append(w.saved, Saved{Name: label, Type: scope})
	}
}

// findLinked looks up a key, written in the block of the given command, in a
// table shaped like linkMakers, whose rows are a command, such as
// save_scope_as, or a key inside a command's block, written command/key. It
// returns the row's link, whether the row is a key inside the command's
// block, and false if the table has neither row.
func findLinked(table map[string]string, holder, key string) (link string, nested, ok bool) {
	if link, ok := table[key]; ok {
		return link, false, true
	}

	link, ok = table[strings.ToLower(holder)+"/"+key]

	return link, true, ok
}

// resolveAlias makes the value that a statement writes at an alias key of
// its database a definition: a second name of the definition the statement
// sits directly in. A value with a parameter names nothing yet.
func (w *walker) resolveAlias(context *Context, statement int32, level int) {
	current, parent := w.tree.Statements[statement], w.frames[level-1].context
	database := w.schema.Databases[parent.Database]
	if database == nil || !parent.Defines || parent.Category != CategoryDefinition || current.IsBlock() ||
		current.Value.IsEmpty() {
		return
	}

	target, isAlias := database.Declaration.Aliases[w.frames[level].label]
	span := w.tree.UnquotedSpan(current.Value)
	if label := w.tree.Text(span); isAlias && label != "" && current.ValueClass != jomini.ClassParameter {
		context.Definition = &Copy{
			Database: cmp.Or(target, database.Name), Name: label, Span: span, Statement: statement, Alias: true,
		}
		context.Defines = true
	}
}
