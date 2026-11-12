// walk.go decides what each statement's key is, with what the schema knows
// about it. values.go reads its value.

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

// Category is the color category of a key or a value in the editor.
type Category string

// The categories a key or a value can have.
const (
	CategoryNone Category = ""

	// CategoryKeyword is a key of the language, such as if, limit or AND.
	CategoryKeyword Category = "keyword"

	CategoryCommand Category = "command"

	// CategoryLink is a scope link, such as root, scope:x or liege.
	CategoryLink Category = "link"

	// CategoryField is a definition's own key.
	CategoryField Category = "field"

	CategoryDefinition Category = "definition"
	CategoryReference  Category = "reference"

	// CategoryParameter is a parameter, such as $NAME$.
	CategoryParameter Category = "parameter"

	// CategoryConstant is a constant, such as @name.
	CategoryConstant Category = "constant"

	// CategoryCall is a key that calls a definition, or a data call.
	CategoryCall Category = "call"

	// CategoryChoice is one of the values a key takes, such as an event's type.
	CategoryChoice Category = "choice"
)

// Copy is one name that a statement defines in a database, with what decides
// which copy the game keeps.
type Copy struct {
	Database string `json:"database"`
	Name     string `json:"name"`

	// Span is where the name alone is written, without quotes or an entry
	// mode.
	Span jomini.Span `json:"span"`

	// Statement is the statement that writes the name. It is 0, the file
	// itself, where the file's name is the name.
	Statement int32 `json:"statement"`

	// Mode is the entry mode written before the name, such as INJECT. It is
	// "" if none.
	Mode string `json:"mode"`

	// Priority is the override priority written at the key the database's
	// precedence names. It is 0 if none.
	Priority int `json:"priority"`

	Local bool `json:"local"`

	// Alias is whether the name is the value of a key the database also names
	// definitions by.
	Alias bool `json:"alias"`

	// Sets is whether a nesting only switches the name on, as for a key in
	// parameters = { … }.
	Sets bool `json:"sets"`
}

// IsOwn reports whether the copy is a definition of its own, not a second name,
// a name a nesting only switches on, or one a command saves while the game
// runs, such as a saved scope.
func (c *Copy) IsOwn() bool {
	return !c.Alias && !c.Sets && !slices.Contains(game.Links, c.Database)
}

// Reference is a name that a key or a value refers to in a database.
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

// ToID returns the reference's database, a colon and its name.
func (r Reference) ToID() string { return r.Database + ":" + r.Name }

// Problem is one thing wrong in a file, before it has a level or words.
type Problem struct {
	Code string
	Span jomini.Span

	// Facts holds what the problem is about, such as the name and its
	// database in words.
	Facts map[string]string

	// Expected is what the value of a ProblemValue must be.
	Expected Expected
}

// The codes of the problems the walk records.
const (
	ProblemUnknownKey = "unknown-key"
	ProblemScope      = "wrong-scope"
	ProblemValue      = "wrong-value"
	ProblemDate       = "invalid-date"
	ProblemFollows    = "else-without-if"
	ProblemNamespace  = "wrong-namespace"
	ProblemMode       = "misplaced-mode"
	ProblemArgument   = "unused-argument"
)

// DescribeReference returns the facts that name a reference.
func DescribeReference(reference Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": game.DescribeDefinition(reference.Database, false)}
}

// Document is one file parsed and walked. A script file has a context for each
// statement and its problems. A localization file has one for each message.
type Document struct {
	File            files.File
	Tree            *jomini.Tree
	Messages        *loc.File
	Contexts        []Context
	MessageContexts []Context
	Problems        []Problem
}

// Saved is one scope a definition saved, with its type.
type Saved struct {
	Name string `json:"name"`

	// Type is the scope's type. It is "" if not known.
	Type string `json:"type"`
}

// Context is what the walk decided at one statement.
type Context struct {
	// Field is the field the key matched. It is nil if no source knows it.
	Field *Field

	// Database and Definition are the definition the statement is in or
	// makes. They are "" and nil outside any definition.
	Database   string
	Definition *Copy

	// Defines is whether the statement itself defines Definition.
	Defines bool

	// References are the names the key and the value refer to.
	References []Reference

	// Scope is where script runs here, and Root the definition's root scope.
	// Each is "" if not known.
	Scope string
	Root  string

	// Previous are the scopes entered before this one, innermost first.
	Previous []string

	// Saved are the scopes the definition saved before the statement.
	Saved []Saved

	// Path is the keys from the definition, or the top of the file, to the
	// statement's block.
	Path []string

	// Label is the statement's key, or the tag of a block with no key.
	Label string

	// Shape is the shape of the block the statement holds, "" if not known.
	Shape game.Shape

	// Conditional is whether the statement is inside a trigger block, so a
	// command here must be a trigger.
	Conditional bool

	// Preview is whether the statement is in a block that the tooltip shows
	// and the game never runs.
	Preview bool

	// Category is the key's color category, and ValueGroup the value's.
	Category Category

	// Command is the effect, trigger or link of the dumps that the key names,
	// and Callee the scripted definition it calls. Each is nil if none.
	Command *Definition
	Callee  *Definition

	ValueGroup Category

	// Call is the data call the value writes, and Keyword the keyword the key
	// is. Each is nil if none.
	Call    *jomini.Call
	Keyword *game.Keyword

	// Iterates is whether the key runs its block on a list's items, as
	// every_vassal does.
	Iterates bool

	// Datacontext is the data context type of a GUI block. It is "" if none.
	Datacontext string

	// Namespaces are the statements that declare an event namespace, held by
	// the file's own context only.
	Namespaces []int32

	// Parent is the context of the holding block, the file's own at the top.
	Parent *Context
}

// walker is one descent over one file.
type walker struct {
	schema *Schema
	row    *game.Game
	tree   *jomini.Tree
	file   files.File

	contexts []Context

	// declarations is whether the walk reads only what the file declares,
	// and body whether it is inside a definition whose calls take parameters.
	declarations bool
	body         bool

	rel  string
	stem string

	// database is the database of the file's folder. It is nil for a file of
	// no folder.
	database *Database

	fileType game.FileType

	// declaring holds, by key, the database whose definition the key
	// declares inline. It is nil outside an events file.
	declaring map[string]string

	// inline holds the names the file declares for itself: all of an events
	// file's, and a GUI file's so far.
	inline map[string]*Definition

	// winners holds what the open workspace's mods declare, as at the start.
	winners map[string]*Definition

	// frames holds one frame for each level: frames[0] is the file's.
	frames []frame

	saved []Saved

	problems []Problem
}

// frame is what the walk keeps for the statements at one level of depth.
type frame struct {
	// context, label, span and class are those of the statement last read at
	// this level: what it is written under, where, and as.
	context *Context
	label   string
	span    jomini.Span
	class   jomini.Class

	// path is the keys from the definition, or the top of the file, to the
	// statement's children.
	path []string

	// wraps is whether the statement's children are written as definitions.
	wraps bool
}

// match is what a key that is no keyword is in its block.
type match struct {
	category Category

	// field holds the keys inside the key's block.
	field *Field

	// definition is the effect, trigger, link or pattern of the dumps that
	// has the key, and callee the scripted definition it calls. Each is nil
	// for none.
	definition *Definition
	callee     *Definition

	// shape is the shape of the key's own block where its kind decides it.
	// It is "" where the field's shape does.
	shape game.Shape

	// observed is whether only the game's own files explain the key, so its
	// block's scope is not known.
	observed bool

	iterator *game.Keyword

	// target is the scope the command or link leads to, and chain a link's
	// chain as resolved.
	target string
	chain  []ChainLink

	// references are the names the key refers to, and uses its uses of the
	// dumps' own names.
	references []Reference
	uses       []Reference
}

func (m match) isLink() bool {
	return m.definition != nil && m.definition.Database == game.DumpLinks
}

// moves reports whether the key moves the scope inside its block, as an
// iterator, a link, or a command that leads to a scope does.
func (m match) moves() bool {
	return m.iterator != nil || m.definition != nil && (m.isLink() || m.target != "" || m.definition.Iterates)
}

// shapeRow is how a block of one shape reads its keys and values.
type shapeRow struct {
	// takes are what a key that is no keyword is tried as, in order: a dumps'
	// database or a take mark. The first that has the key wins.
	takes []string

	// caller is the caller a call here is looked up by, game.AnyCall for
	// every callable.
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

// The take marks: what a block takes a key as, beside the dumps' databases.
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

	// takeBareLinks is a link, but not a plain name with a plain value.
	takeBareLinks = "bare links"

	// takeModifiers is a modifier the dumps declare, and takeModifierTypes
	// any identifier, as a modifier type's name.
	takeModifiers     = "modifiers"
	takeModifierTypes = "modifier types"

	// takeUnknown is any identifier that is no event id.
	takeUnknown = "unknown"
)

// shapeRows holds the row of each shape a block can have. A key nothing
// explains in a command's block is a call of any callable, never judged: it
// may be an argument no dump writes.
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

// linkMakers holds the commands that save a name, as command or command/key
// for a key in its block, each with the game.Links link that reads it back.
var linkMakers = map[string]string{
	"save_scope_as": game.SavedScopes, "save_temporary_scope_as": game.SavedScopes,
	"save_scope_value_as/name": game.SavedScopes, "save_temporary_scope_value_as/name": game.SavedScopes,
	"save_opinion_value_as/name": game.SavedScopes, "save_temporary_opinion_value_as/name": game.SavedScopes,

	// CK3's common/script_values, and EU5's
	// generic_actions/western_schism.txt:361
	"save_temporary_value_as": game.SavedScopes, "save_temporary_value_as/name": game.SavedScopes,

	// CK3's _character_interactions.info:349 and _decisions.info:315, 395:
	// the option picked sets scope:<name>
	"send_option/flag": game.SavedScopes, "item/value": game.SavedScopes,

	// EU5's cabinet_actions/assimilate_area.txt:16
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

// Walk returns the context of each statement in the tree's order, the first
// the file's own, and the file's own problems. It reuses the given contexts if
// they are long enough.
func (s *Schema) Walk(tree *jomini.Tree, file files.File, contexts []Context) ([]Context, []Problem) {
	walker := s.newWalker(tree, file, contexts)
	walker.walkChildren(0, 1)
	walker.judgeCalls()

	return walker.contexts, walker.problems
}

func (w *walker) record(code string, span jomini.Span, facts map[string]string) {
	w.problems = append(w.problems, Problem{Code: code, Span: span, Facts: facts})
}

// WalkDeclarations is Walk for the pass that reads declarations. It decides
// only the top statements, a top block that defines nothing, and the body of a
// definition whose calls take parameters. Other statements get an empty
// context.
func (s *Schema) WalkDeclarations(tree *jomini.Tree, file files.File, contexts []Context) []Context {
	walker := s.newWalker(tree, file, contexts)

	// a kept list holds the last file's contexts; keep only each one's
	// references' room
	for position := 1; position < len(walker.contexts); position++ {
		walker.contexts[position] = Context{References: walker.contexts[position].References[:0]}
	}

	walker.declarations = true
	walker.body = walker.startsBody(&walker.contexts[0])
	walker.walkChildren(0, 1)

	return walker.contexts
}

// newWalker returns the walker for one file. Where the file's database names
// its one definition by the file's name, the file's own context holds it.
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

// declareInline records each definition an events file declares inline at its
// top. The games call one above its declaration, as CK3's events/dlc/ep3 do,
// so these are read first.
func (w *walker) declareInline() {
	var bodies []int32
	for statement := range w.tree.Children(0) {
		label, _, _ := w.findLabel(w.tree.Statements[statement])
		if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
			w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
			bodies = append(bodies, statement)
		}
	}

	for _, statement := range bodies {
		label, _, _ := w.findLabel(w.tree.Statements[statement])
		w.walkStatement(statement, 1)
		for position := statement + 1; position < w.tree.Statements[statement].Last; position++ {
			w.schema.typeDefinition(w.inline[label], listTypings(w.tree, position, &w.contexts[position]))
		}
	}

	w.saved, w.problems = nil, nil
}

func listNamespaces(tree *jomini.Tree) []int32 {
	var statements []int32
	for statement := range tree.Children(0) {
		if tree.Text(tree.Statements[statement].Key) == game.EventNamespaceKey {
			statements = append(statements, statement)
		}
	}

	return statements
}

// walkChildren walks the children of a statement, a definition's in the order
// its database says they run, so each sees the scopes the earlier ones saved.
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

// findRunOrder returns the keys of a definition's blocks in run order, or nil.
func (w *walker) findRunOrder(level int) []string {
	context := w.frames[level-1].context
	database := w.schema.Databases[context.Database]
	if !context.Defines || context.Category != CategoryDefinition || database == nil {
		return nil
	}

	return database.Declaration.RunOrder
}

// walkStatement resolves one statement and walks its block. A walk of the
// declarations walks only the blocks WalkDeclarations names.
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

func (w *walker) startsBody(context *Context) bool {
	if !context.Defines || context.Category != CategoryDefinition || context.Definition.Local {
		return false
	}

	database := w.schema.Databases[context.Definition.Database]

	return database != nil && database.Declaration.Parameters
}

// resolve fills the context of a statement from the context of the block
// that holds it, and records what is wrong with its value.
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
		w.judgeMode(statement, definition)
	} else {
		w.resolveStatement(context, statement, level)
	}

	context.Path, context.Label = w.frames[level-1].path, label
	if w.tree.Statements[statement].IsBlock() {
		keys := w.frames[level-1].path
		if label != "" {
			keys = slices.Concat(keys, []string{label})
		}

		if defines && w.database != nil && definition.Database == w.database.Name {
			keys = nil
		}

		w.frames[level].path = keys
	}

	// a GUI block's datacontext key sets its data context, "" if the dumps do
	// not resolve the call
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
	w.judgeValue(context, statement)
	w.judgeDate(context, statement)
}

// findLabel returns a statement's key or block tag, where it is written and
// its class. It returns "", an empty span and ClassNone for a lone value.
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

func (w *walker) findDefinitionShape(database *Database) game.Shape {
	if database == nil {
		return cmp.Or(w.fileType.Shape, game.ShapeFields)
	}

	return cmp.Or(database.Declaration.Shape, w.fileType.Shape, game.ShapeFields)
}

// findRootScope returns the scope a definition starts in: its root key's, else
// its event type's, or its on_action's in the dumps. It returns "" if none.
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
// top statement of a database whose definitions have no name is in its one
// definition.
func (w *walker) resolveStatement(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	switch {
	// a GUI block is also overridden as blockoverride = "name" { … }, as CK3
	// gui/interaction_marriage.gui:190 writes. The name is the block's tag,
	// and the braces hold what the widget around it holds.
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

	w.judgeUnexplained(context, statement)
}

// findLead returns the value written alone just before a statement, where
// it makes the statement a definition or a call. It returns "" otherwise.
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

func (w *walker) isLead(text string) bool {
	if w.declaring[text] != "" {
		return true
	}

	return w.file.Format == files.FormatGUI && (slices.Contains(game.GUI.Declares, text) || len(w.schema.Callees[text]) > 0)
}

// resolveLead resolves a call written as a lead and a label, such as
// blockoverride "label" in a GUI file.
func (w *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range w.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}

	if lead != "" && len(w.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// resolveKey fills the context of a keyed statement. A command written with an
// argument in parentheses is read as the command, and the argument makes its
// own references.
func (w *walker) resolveKey(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	key, span, class := w.frames[level].label, w.frames[level].span, w.frames[level].class

	// a key with a parameter is unknown until the call is made, and so are its
	// block's shape and scope
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
	fit, fits := w.findFit(parent, keyword, key, span, class, block, isCall)
	w.resolveMatch(context, parent, fit, fits, keyword, key, isCompared(w.tree, statement))
	if keyword == nil {
		context.References = append(context.References, fit.references...)
		w.judgeScope(parent, fit, level, isCall)
	} else {
		w.judgeFollows(keyword, statement)
	}

	context.References = append(context.References, fit.uses...)

	if isCall {
		command = cmp.Or(context.Command, command)
		context.References = append(context.References,
			w.listArgumentReferences(command, parent.FindArguments(command), step.Arguments[0])...)
	}

	// a math keyword's value is a number or a script value, so it takes no
	// field
	if keyword != nil && keyword.Class != game.KeywordClassMath && context.Field == nil {
		context.Field = findKeywordField(parent, key, class)
	}

	w.resolveSaved(context, statement, level)
	w.resolveAlias(context, statement, level)
}

// findFit returns what a key is in the block of a context, and whether it is
// anything. A keyword fits only the dumps' command of its own key, as they
// declare if.
func (w *walker) findFit(
	parent *Context, keyword *game.Keyword, key string, span jomini.Span, class jomini.Class, block, isCall bool,
) (match, bool) {
	var fit match
	fits := false
	switch {
	case keyword == nil:
		fit, fits = w.lookupKey(parent, key, span, class, block, isCall)
	case strings.EqualFold(keyword.Key, key):
		fit, fits = w.findCommand(parent, key, span)
	}

	// a command that starts an iterator runs its block in the scope of the
	// list's item
	if keyword == nil && fit.definition != nil && !fit.isLink() {
		if iterator, iterates := game.MatchListIterator(key); iterates {
			fit.iterator = new(iterator)
		}
	}

	return fit, fits
}

// findCommand returns the effect or the trigger a key names, in the order
// the block's shape tries the two. It returns false if the block takes
// neither.
func (w *walker) findCommand(parent *Context, key string, span jomini.Span) (match, bool) {
	for _, entry := range shapeRows[findShape(parent)].takes {
		if entry != game.DumpEffects && entry != game.DumpTriggers {
			continue
		}

		if fit, ok := w.takeCommand(entry, parent, key, span); ok {
			return fit, true
		}
	}

	return match{}, false
}

// findKeywordField returns the field of a keyword no rule gave one: its key's
// child in the block around it, or under an enclosing keyword the command's,
// as the dumps give an if its limit. It returns nil if none.
func findKeywordField(parent *Context, key string, class jomini.Class) *Field {
	if field := parent.Field.findPatternChild(key, class); field != nil || parent.Command == nil {
		return field
	}

	return parent.Command.Field.findChild(key)
}

func findShape(context *Context) game.Shape {
	return cmp.Or(context.Shape, game.ShapeUnknown)
}

// findKeyword returns the keyword a key is in the block of a context, or nil.
// A keyword whose block may hold itself, as Vic3's else in a progress bar,
// matches its own key again.
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

func findEnclosing(context *Context) game.Shape {
	return cmp.Or(shapeRows[findShape(context)].encloses, findShape(context))
}

func (w *walker) lookupKey(
	parent *Context, key string, span jomini.Span, class jomini.Class, block, isCall bool,
) (match, bool) {
	for _, entry := range shapeRows[findShape(parent)].takes {
		if fit, ok := w.take(entry, parent, key, span, class, block, isCall); ok {
			return fit, true
		}
	}

	return match{}, false
}

// take returns what a key is under one entry of a shape's takes, if it has
// it. A number or a date fits a field's <number> child, and under takeOwn its
// <any> child too.
func (w *walker) take(
	entry string, parent *Context, key string, span jomini.Span, class jomini.Class, block, isCall bool,
) (match, bool) {
	switch entry {
	case takeCalls:
		return w.takeCall(parent, key, span)

	case takeDeclared:
		child := parent.Field.findNumberChild(key, class)
		if child == nil || child.Source == game.SourceNone || child.Source == game.SourceVanilla {
			return match{}, false
		}

		return takeField(parent, child, key, span), true

	case takeObserved:
		child := parent.Field.findNumberChild(key, class)
		if child == nil || child.Source != game.SourceVanilla {
			return match{}, false
		}

		fit := takeField(parent, child, key, span)
		fit.observed = true

		return fit, true

	case takeDocumented:
		if database := w.schema.Databases[parent.Database]; database == nil || !database.declares(key) {
			return match{}, false
		}

		return takeField(parent, parent.Field.findChild(key), key, span), true

	case takeOwn:
		child := parent.Field.findPatternChild(key, class)
		if child == nil {
			return match{}, false
		}

		return takeField(parent, child, key, span), true

	case takeModifiers, takeModifierTypes:
		return w.takeModifier(entry, parent, key, span, class)

	case takeUnknown:
		return w.takeUnknown(parent, key, span, class, isCall)

	case game.DumpLinks, takeBareLinks:
		return w.takeLink(entry, parent, key, span, block)
	}

	return w.takeCommand(entry, parent, key, span)
}

// takeCall returns the match of a key that calls a scripted definition. Its
// block holds the definition's parameters, else its database's fields.
func (w *walker) takeCall(parent *Context, key string, span jomini.Span) (match, bool) {
	callee, caller, ok := w.findCalledDefinition(parent.FindCaller(), key)
	if !ok {
		return match{}, false
	}

	fit := match{category: CategoryCall, field: callee.Field, callee: callee}
	if database := w.schema.Databases[callee.Database]; fit.field == nil && database != nil {
		fit.field = database.Field
	}

	if callee.Field != nil {
		fit.shape = game.ShapeParameters
	}

	if callee.Database != "" {
		fit.references = []Reference{{Database: callee.Database, Name: key, Span: span, Caller: caller}}
	}

	return fit, true
}

// takeField returns the match of a key that is a field of the block around
// it. If that block's keys name a database the key refers to a name there,
// unless the dumps declare it, as for stress_impact's base.
func takeField(parent *Context, field *Field, key string, span jomini.Span) match {
	fit := match{category: CategoryField, field: field}
	if parent.Field == nil || field != nil && field.Source == game.SourceDumps {
		return fit
	}

	if keys := parent.Field.Keys; keys.Value != "" {
		fit.references = []Reference{{Database: keys.Value, Name: key, Span: span, Inferred: !keys.Source.CanAccuse()}}
	}

	return fit
}

// takeModifier returns the match of a key in a block of modifiers. Where the
// game has modifier types the key refers to one, never as an error: not every
// declared modifier is known to have a type.
func (w *walker) takeModifier(
	entry string, parent *Context, key string, span jomini.Span, class jomini.Class,
) (match, bool) {
	declared := entry == takeModifiers
	types := w.schema.ModifierTypes
	if declared && !w.schema.declaresModifier(key) || !declared && (types == "" || !class.IsIdentifier()) {
		return match{}, false
	}

	field := w.findModifierField(parent.Field, key)
	if types == "" {
		return takeField(parent, field, key, span), true
	}

	fit := match{category: CategoryField, field: field}
	if !declared {
		fit.category = CategoryReference
	}

	fit.references = []Reference{{Database: types, Name: key, Span: span, Inferred: declared}}

	return fit, true
}

// takeUnknown returns the match of an identifier nothing else explains, a call
// to a name nothing defines. A name with an argument in parentheses is a link
// where the dumps declare one that takes data, as relations(scope:overlord).
// An iterator's name refers to a scripted list.
func (w *walker) takeUnknown(
	parent *Context, key string, span jomini.Span, class jomini.Class, isCall bool,
) (match, bool) {
	if !class.IsIdentifier() || class == jomini.ClassEventID {
		return match{}, false
	}

	if isCall {
		if link := w.schema.lookupLink(key, true); link != nil {
			return w.makeLink(link, parent, key, span), true
		}
	}

	fit := match{category: CategoryCall}
	caller := parent.FindCaller()
	iterator, iterates := game.MatchListIterator(key)
	switch {
	case iterates && w.schema.Lists != "":
		fit.iterator = new(iterator)
		list := strings.TrimPrefix(key, strings.TrimSuffix(iterator.Key, "*"))
		fit.references = []Reference{{Database: w.schema.Lists, Name: list, Span: span}}

	case caller == game.AnyCall:
		fit.references = []Reference{{Database: game.AnyCall, Name: key, Span: span, Caller: game.AnyCall}}

	default:
		for _, database := range w.schema.Callees[caller] {
			fit.references = append(fit.references, Reference{Database: database, Name: key, Span: span, Caller: caller})
		}
	}

	return fit, true
}

// takeLink returns the match of a key that starts with a link, as scope:target
// does.
func (w *walker) takeLink(entry string, parent *Context, key string, span jomini.Span, block bool) (match, bool) {
	if entry == takeBareLinks && !block && !strings.ContainsAny(key, ":.") {
		return match{}, false
	}

	link := w.schema.FindLink(key)
	if link == nil {
		return match{}, false
	}

	return w.makeLink(link, parent, key, span), true
}

// makeLink returns the match of a key that starts with the given link. Its
// block keeps the field of the block around it, and the key refers to the
// names after its links' prefixes.
func (w *walker) makeLink(link *Definition, parent *Context, key string, span jomini.Span) match {
	chain := w.schema.ResolveChain(key, parent, w.saved)

	return match{
		category: CategoryLink, definition: link, field: parent.Field, shape: findEnclosing(parent),
		target: chain[len(chain)-1].Scope, chain: chain,
		references: w.listPrefixed(key, span.Start), uses: w.schema.listLinkUses(key, span.Start),
	}
}

// takeCommand returns the match of a key that names an effect or trigger of the
// given database, declared or made by a pattern. It returns false for an
// effect in a block of triggers.
func (w *walker) takeCommand(database string, parent *Context, key string, span jomini.Span) (match, bool) {
	if !parent.Takes(database) {
		return match{}, false
	}

	definition, patterns := w.schema.LookupDefinition(database, key), w.schema.findPatterns(database, key)
	if definition == nil && len(patterns) > 0 {
		definition = patterns[0]

		// a list iterator a pattern makes takes the keys of the plain
		// iterator of its scope, as any_character
		if iterator, iterates := game.MatchListIterator(key); iterates {
			plain := strings.TrimSuffix(iterator.Key, "*") + definition.Target
			definition = cmp.Or(w.schema.LookupDefinition(database, plain), definition)
		}
	}

	if definition == nil {
		return match{}, false
	}

	fit := match{
		category: CategoryCommand, definition: definition, field: parent.FindArguments(definition),
		target: definition.Target,
		uses:   []Reference{{Database: definition.Database, Name: definition.Key, Span: span, Inferred: true}},
	}
	switch {
	case findBlockShape(fit.field) != "":
	case fit.field != nil && fit.field == definition.Union:
		fit.shape = game.ShapeUnknown
	case definition.Database == game.DumpTriggers:
		fit.shape = game.ShapeTriggerArguments
	default:
		fit.shape = game.ShapeEffectArguments
	}

	lower := strings.ToLower(key)
	for _, pattern := range patterns {
		start, end, _ := pattern.findStar(lower)
		for _, starred := range pattern.Star {
			fit.references = append(fit.references, Reference{
				Database: starred, Name: key[start:end], Span: jomini.Span{Start: span.Start + start, End: span.Start + end},
			})
		}
	}

	return fit, true
}

// findBlockShape returns the shape of a field's own block: the one a source
// states, else a value block where the field takes a script value, as a
// formula is written "inline wherever scripted values work" (CK3
// _script_values.info, "Inlining"). It returns "" if neither.
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

// findCalledDefinition returns the scripted definition a key calls and its
// caller, the file's inline definitions first.
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
// field. It returns nil if neither has one.
func (w *walker) findModifierField(holder *Field, key string) *Field {
	if declared := w.schema.lookupScripted(w.winners, w.schema.ModifierTypes, key); declared != nil {
		return declared.Field
	}

	return holder.findChild(key)
}

// FindCaller returns the caller a call in this block resolves through: the
// keyword's, else the block shape's.
func (c *Context) FindCaller() string {
	if c.Keyword != nil && c.Keyword.Caller != "" {
		return c.Keyword.Caller
	}

	return shapeRows[findShape(c)].caller
}

// FindArguments returns the keys a command takes in this block: its own, or
// its effect's and trigger's together in a block that takes either. It returns
// nil for no command.
func (c *Context) FindArguments(command *Definition) *Field {
	switch {
	case command == nil:
		return nil
	case command.Union != nil && c.Takes(game.DumpEffects) && c.Takes(game.DumpTriggers):
		return command.Union
	}

	return command.Field
}

// TakesLinks reports whether a key in this block may be a link.
func (c *Context) TakesLinks() bool {
	return c.Takes(game.DumpLinks) || c.Takes(takeBareLinks)
}

// Takes reports whether this block takes the keys of a dumps' database or of a
// take mark. An effect never fits in a block of triggers.
func (c *Context) Takes(entry string) bool {
	return slices.Contains(shapeRows[findShape(c)].takes, entry) &&
		(entry != game.DumpEffects || !c.holdsConditions())
}

// holdsConditions reports whether the statements in the block are inside a
// trigger block: it is triggers, or sits below triggers and is no value block.
func (c *Context) holdsConditions() bool {
	return c.Shape == game.ShapeTriggers || c.Shape == game.ShapeTriggerArguments ||
		c.Conditional && c.Shape != game.ShapeValue
}

// resolveMatch sets the context of a keyed statement from what its key fits.
// A keyword that encloses keeps the field of the block around it.
func (w *walker) resolveMatch(
	context, parent *Context, fit match, fits bool, keyword *game.Keyword, key string, compared bool,
) {
	context.Field, context.Category = fit.field, fit.category
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

// findKeyShape returns the shape of the block a keyed statement holds. The
// first rule that holds decides.
func findKeyShape(parent *Context, fit match, fits bool, keyword *game.Keyword, compared bool) game.Shape {
	switch {
	// a block a key is compared with is a formula
	case compared:
		return game.ShapeValue

	// CK3 _script_values.info, "Lists": a list in a formula holds the
	// formula's math, as add_gold = { every_child = { add = 1 } } does
	case fit.iterator != nil && findShape(parent) == game.ShapeValue:
		return game.ShapeValue

	case fit.iterator != nil:
		return fit.iterator.Shape

	case keyword != nil && keyword.Shape == game.ShapeEnclosing:
		return findEnclosing(parent)

	case keyword != nil && keyword.Shape != "":
		return keyword.Shape

	case fits:
		return cmp.Or(fit.shape, findBlockShape(fit.field), game.ShapeUnknown)
	}

	return shapeRows[findShape(parent)].blocks
}

// findKeyScope returns the scope script runs in inside a keyed statement's
// block, "" if not known. The first rule that holds decides.
func (w *walker) findKeyScope(
	context, parent *Context, fit match, fits bool, keyword *game.Keyword, key string,
) string {
	known := context.Shape != "" && context.Shape != game.ShapeUnknown
	explained := fits && !fit.observed
	shaped := keyword != nil && !fits && keyword.Shape != ""
	switch {
	// a call's argument is read inside the called definition
	case context.Shape == game.ShapeParameters:
		return ""

	// an argument is read in a scope the command makes, but a conditional
	// keyword such as limit in the scope of its command
	case isArgument(parent, key) && (keyword == nil || keyword.Class != game.KeywordClassConditional):
		return ""

	case fit.moves() && w.schema.isScope(fit.target):
		return fit.target

	case fit.moves():
		return ""

	// a key that a source other than the game's own files explains, and a
	// keyword whose row says what its block holds, keep the scope
	case known && (explained || shaped):
		return parent.Scope
	}

	return ""
}

func isArgument(parent *Context, key string) bool {
	return parent.HoldsArguments() && parent.Field.findChild(key) != nil
}

// HoldsArguments reports whether the block of a context holds a command's
// arguments: the command leads to no scope and iterates no list.
func (c *Context) HoldsArguments() bool {
	command := c.Command

	return c.Category == CategoryCommand && (command == nil || command.Target == "" && !command.Iterates)
}

// judgeScope records a command, and each link of a chain, written in a scope
// the dumps do not list for it. It skips blocks of arguments and links written
// with data in parentheses.
func (w *walker) judgeScope(parent *Context, fit match, level int, isCall bool) {
	eligible := parent.Shape == game.ShapeEffects || parent.Shape == game.ShapeTriggers
	if fit.definition == nil || !eligible || parent.HoldsArguments() {
		return
	}

	key, span := w.frames[level].label, w.frames[level].span
	scope, followed := parent.Scope, len(parent.Previous) > 0
	switch {
	case !fit.isLink():
		w.judgeCommandScope(span, fit.definition, scope, followed)

	case !isCall:
		links := jomini.Links(key, span.Start)
		for position, link := range fit.chain {
			w.judgeCommandScope(links[position].Span, link.Definition, scope, followed)
			scope, followed = link.Scope, true
		}
	}
}

// judgeCommandScope records a command or link that the dumps do not list for
// its scope. It records none if they list none, or the scope is not known.
func (w *walker) judgeCommandScope(span jomini.Span, command *Definition, scope string, followed bool) {
	if command == nil || len(command.Scopes) == 0 || scope == "" || !followed || slices.Contains(command.Scopes, scope) {
		return
	}

	facts := DescribeReference(Reference{Name: w.tree.Text(span)})
	facts["scope"], facts["allowed"] = scope, strings.Join(command.Scopes, ", ")
	w.record(ProblemScope, span, facts)
}

// judgeFollows records a keyword such as else that is not written right
// after one of the keys it must follow.
func (w *walker) judgeFollows(keyword *game.Keyword, statement int32) {
	if len(keyword.Follows) == 0 {
		return
	}

	current := w.tree.Statements[statement]
	previous := statement - 1
	for previous > current.Parent && w.tree.Statements[previous].Parent != current.Parent {
		previous = w.tree.Statements[previous].Parent
	}

	if previous > current.Parent &&
		slices.Contains(keyword.Follows, strings.ToLower(w.tree.Unquoted(w.tree.Statements[previous].Key))) {
		return
	}

	facts := DescribeReference(Reference{Name: w.tree.Unquoted(current.Key)})
	facts["follows"] = game.JoinList(keyword.Follows, "or")
	w.record(ProblemFollows, current.Key, facts)
}

// judgeUnexplained records an event outside its file's namespaces, else a key
// nothing explains. Only a plain name is judged, in a block whose keys a firm
// source lists and the game's files never write.
func (w *walker) judgeUnexplained(context *Context, statement int32) {
	current := w.tree.Statements[statement]
	if context.Defines || current.Key.IsEmpty() || context.Category != CategoryNone || context.Field != nil {
		return
	}

	label := w.tree.Unquoted(current.Key)
	if current.Parent == 0 && current.KeyClass == jomini.ClassEventID {
		if w.database == nil || w.database.Declaration.Contents != game.ContentsEvents {
			return
		}

		var namespaces []string
		for _, declaration := range context.Parent.Namespaces {
			namespaces = append(namespaces, w.tree.Unquoted(w.tree.Statements[declaration].Value))
		}

		facts := DescribeReference(Reference{Database: w.database.Name, Name: label})
		facts["namespace"] = cmp.Or(strings.Join(namespaces, ", "), "none")
		w.record(ProblemNamespace, current.Key, facts)

		return
	}

	holder := context.Parent
	if context.Definition == nil || w.schema.Databases[context.Database] == nil || !current.KeyClass.IsIdentifier() ||
		holder.Field == nil || holder.Shape == game.ShapeUnknown || !holder.Field.Source.CanAccuse() ||
		holder.Field.Vanilla.Count > 0 {
		return
	}

	w.record(ProblemUnknownKey, current.Key, DescribeReference(Reference{Database: context.Database, Name: label}))
}

// listLinkUses returns a use of each link of a chain that the dumps declare.
// A use is never an error.
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

// linkUsers holds the triggers and lists that read a saved name back, keyed as
// linkMakers is, each with its link, from the games' triggers.log and
// effects.log. Variable maps are Vic3's and EU5's, flags CK3's.
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

// resolveSaved makes a name that a statement saves a definition, and one it
// reads back a reference. A saved scope joins the definition's, its type
// unknown inside a command's block that makes no scope. A name with a
// parameter is skipped.
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

// findLinked looks up a key in a table shaped like linkMakers, as a command or
// as command/key under the given holder. It returns the row's link, whether it
// is a key in a block, and false if there is none.
func findLinked(table map[string]string, holder, key string) (link string, nested, ok bool) {
	if link, ok := table[key]; ok {
		return link, false, true
	}

	link, ok = table[strings.ToLower(holder)+"/"+key]

	return link, true, ok
}

// resolveAlias makes the value written at an alias key of the database a
// second name of the definition the statement sits in, unless it is a
// parameter.
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
