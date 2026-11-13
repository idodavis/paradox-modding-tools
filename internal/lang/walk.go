// walk.go is the walk over a parsed file: what each statement's key is and
// the scope it runs in. values.go reads the statement's value.

package lang

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// Category is what a key or a value is. The editor colors by it.
type Category string

// The categories of a key or a value.
const (
	CategoryNone Category = ""

	// CategoryKeyword is a fixed word of the language, such as if or limit.
	CategoryKeyword Category = "keyword"

	// CategoryCommand is an effect or a trigger.
	CategoryCommand Category = "command"

	// CategoryLink is a link to a scope, such as root or scope:x.
	CategoryLink Category = "link"

	// CategoryField is a key of the definition it is written in.
	CategoryField Category = "field"

	// CategoryDefinition is a name being defined, and CategoryReference a
	// name that refers to a definition.
	CategoryDefinition Category = "definition"
	CategoryReference  Category = "reference"

	// CategoryParameter is a parameter, such as $NAME$.
	CategoryParameter Category = "parameter"

	// CategoryConstant is a constant, such as @name.
	CategoryConstant Category = "constant"

	// CategoryCall is a key that calls a scripted definition, or a value that
	// is a data call.
	CategoryCall Category = "call"

	// CategoryChoice is one of a fixed list of values, such as an event's
	// type.
	CategoryChoice Category = "choice"
)

// Copy is one name that one statement defines in a database. Several files
// may each hold a copy of the same name.
type Copy struct {
	Database string `json:"database"`
	Name     string `json:"name"`

	// Span is the name alone, without quotes or an entry mode.
	Span jomini.Span `json:"span"`

	// Statement defines the name. It is 0, the file itself, where the file's
	// name is the name.
	Statement int32 `json:"statement"`

	// Mode is the entry mode before the name, such as INJECT, "" for none.
	Mode string `json:"mode"`

	// Priority is the override priority the definition writes, as CK3's
	// id_override_priority, 0 for none.
	Priority int `json:"priority"`

	// Local is whether the name answers only in its own file, as a constant
	// @name does.
	Local bool `json:"local"`

	// Alias is whether the name is a second name for a definition, as a
	// trait's group is.
	Alias bool `json:"alias"`

	// Sets is whether the statement only switches a name on, as a key in
	// parameters = { … } does.
	Sets bool `json:"sets"`
}

// IsOwn reports whether the copy is a definition in its own right: not a
// second name, a name only switched on, or a name script saves.
func (c *Copy) IsOwn() bool {
	return !c.Alias && !c.Sets && !slices.Contains(Links, c.Database)
}

// Reference is a name that a key or a value refers to in a database.
type Reference struct {
	Database string `json:"database"`
	Name     string `json:"name"`

	// Span is the name alone, without quotes or a link's prefix.
	Span jomini.Span `json:"span"`

	// Caller is the kind of place a call is made from: a trigger, effect or
	// value block, or a GUI key. It is "" for a name that is not a call.
	Caller string `json:"caller,omitempty"`

	// Inferred is whether the name is no mistake when nothing defines it:
	// only the game's own files suggest it, or its database needs no
	// definition.
	Inferred bool `json:"inferred,omitempty"`
}

// ToID returns the reference's database, a colon and its name.
func (r Reference) ToID() string { return r.Database + ":" + r.Name }

// DescribeReference returns the facts of a problem about a name: the name,
// and in words what it is a name of.
func DescribeReference(reference Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": DescribeDefinition(reference.Database, false)}
}

// Problem is one thing the walk found wrong in a file. Its severity and its
// sentence are given where it is shown.
type Problem struct {
	Code string
	Span jomini.Span

	// Facts holds what the problem is about by key, such as the name and, in
	// words, its database.
	Facts map[string]string

	// Expected is what a value of the wrong kind should have been. Only a
	// problem with a value has it.
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

// Document is one file after it is parsed and walked. A script or GUI file
// has its tree and a context per statement. A localization file has its
// messages and a context per message.
type Document struct {
	File            files.File
	Tree            *jomini.Tree
	Messages        *loc.File
	Contexts        []Context
	MessageContexts []Context
	Problems        []Problem
}

// Saved is one scope a definition has saved under a name, as
// save_scope_as = target does.
type Saved struct {
	Name string `json:"name"`

	// Type is the saved scope's type, "" where it is not known.
	Type string `json:"type"`
}

// Context is everything the walk decided about one statement.
type Context struct {
	// Field is the field the key is, nil for a key no source knows as one.
	Field *Field

	// Database and Definition are the definition the statement makes or is
	// written inside. They are "" and nil outside any definition.
	Database   string
	Definition *Copy

	// Defines is whether the statement makes the definition, rather than
	// being written inside it.
	Defines bool

	// References are the names the key and the value refer to.
	References []Reference

	// Scope is the scope type the statement runs in, and Root the one its
	// definition starts in. Each is "" where it is not known.
	Scope string
	Root  string

	// Previous are the scope types entered before this one, the nearest
	// first. They are what prev leads back to.
	Previous []string

	// Saved are the scopes the definition saved before the statement.
	Saved []Saved

	// Path is the keys from the definition, or from the top of the file, down
	// to the statement's block.
	Path []string

	// Label is the statement's key, or the tag of a block written with no
	// key.
	Label string

	// Shape is what the statement's own block holds, "" where it is not
	// known.
	Shape Shape

	// Conditional is whether the statement sits inside a block of triggers,
	// so that a command here must be a trigger.
	Conditional bool

	// Preview is whether the statement is in a block the game only shows in
	// a tooltip and never runs, as show_as_tooltip's.
	Preview bool

	// Category is what the key is.
	Category Category

	// Command is the effect, trigger or link the key names, and Callee the
	// scripted definition it calls. Each is nil for none.
	Command *Definition
	Callee  *Definition

	// ValueGroup is what the value is.
	ValueGroup Category

	// Call is the data call the value writes, and Keyword the keyword the
	// key is. Each is nil for none.
	Call    *jomini.Call
	Keyword *Keyword

	// Iterates is whether the key runs its block once per item of a list, as
	// every_vassal does.
	Iterates bool

	// Datacontext is the data type the data calls in a GUI block start from,
	// "" for none.
	Datacontext string

	// Namespaces are the statements that declare an event namespace. Only the
	// file's own context has them.
	Namespaces []int32

	// Parent is the context of the statement whose block holds this one. At
	// the top of a file it is the file's own context.
	Parent *Context
}

// FindCaller returns the kind of place a call in this block is made from,
// which decides what it may call: the keyword's, as a random_list branch
// sets, else the block shape's.
func (c *Context) FindCaller() string {
	if c.Keyword != nil && c.Keyword.Caller != "" {
		return c.Keyword.Caller
	}

	return ShapeRows[findShape(c)].Caller
}

// FindArguments returns the keys a command takes in this block. Where the
// block may hold an effect or a trigger and both have the command's name,
// they are the keys of both together.
func (c *Context) FindArguments(command *Definition) *Field {
	switch {
	case command == nil:
		return nil
	case command.Union != nil && c.Takes(DumpEffects) && c.Takes(DumpTriggers):
		return command.Union
	}

	return command.Field
}

// TakesLinks reports whether a key in this block may be a link.
func (c *Context) TakesLinks() bool {
	return c.Takes(DumpLinks) || c.Takes(takeBareLinks)
}

// Takes reports whether a key in this block may be read as the given thing:
// an effect, a trigger, a link, or another entry of the block's shape. An
// effect never fits inside a block of triggers.
func (c *Context) Takes(entry string) bool {
	return slices.Contains(ShapeRows[findShape(c)].Takes, entry) &&
		(entry != DumpEffects || !c.holdsConditions())
}

// holdsConditions reports whether the block is checked rather than run: it
// holds triggers, or sits inside a block of triggers and computes no number.
func (c *Context) holdsConditions() bool {
	return c.Shape == ShapeTriggers || c.Shape == ShapeTriggerArguments ||
		c.Conditional && c.Shape != ShapeValue
}

// HoldsArguments reports whether the block holds a command's arguments, as
// add_gold = { value = 10 } does. A command that leads to a scope, or runs
// its block on a list, holds script instead.
func (c *Context) HoldsArguments() bool {
	command := c.Command

	return c.Category == CategoryCommand && (command == nil || command.Target == "" && !command.Iterates)
}

// findShape returns what the block holds, unknown where the walk did not
// decide it.
func findShape(context *Context) Shape {
	return cmp.Or(context.Shape, ShapeUnknown)
}

// findEnclosing returns what a block inside this one holds when its key is a
// link or a keyword such as if.
func findEnclosing(context *Context) Shape {
	return cmp.Or(ShapeRows[findShape(context)].Encloses, findShape(context))
}

// walker is one walk over one file.
type walker struct {
	schema  *Schema
	dialect *Dialect
	tree    *jomini.Tree
	file    files.File

	// contexts is the result, one per statement.
	contexts []Context

	// rel is the file's path from its stage folder, and stem its name
	// without the extension.
	rel  string
	stem string

	// database is the file's database, nil for none.
	database *Database

	fileType FileType

	// declaring maps each key that declares a definition inside an events
	// file, as scripted_effect, to that definition's database. It is nil for
	// any other file.
	declaring map[string]string

	// inline holds the definitions only this file can call: all of an events
	// file's, and the local ones of a GUI file read so far.
	inline map[string]*Definition

	// winners holds the mods' definitions as they were when the walk started.
	winners map[string]*Definition

	// frames holds what is kept for each depth of nesting, frames[0] for the
	// file.
	frames []frame

	// saved are the scopes the current definition has saved so far.
	saved []Saved

	problems []Problem
}

// frame is what the walk keeps about the statement it last read at one
// depth of nesting.
type frame struct {
	// context, label, span and class are that statement's context, its key,
	// where the key is written and what kind of word it is.
	context *Context
	label   string
	span    jomini.Span
	class   jomini.Class

	// path is the keys from the definition, or from the top of the file, down
	// to this statement's key: the path of the statements inside it.
	path []string

	// wraps is whether the statements inside this one are definitions, as at
	// the top of a file and inside a block that only wraps them.
	wraps bool
}

// match is what a key that is no keyword turned out to be in its block.
type match struct {
	category Category

	// field holds the keys the key's own block may write.
	field *Field

	// definition is the effect, trigger or link that has the key, or the star
	// key it fits, and callee the scripted definition it calls. Each is nil
	// for none.
	definition *Definition
	callee     *Definition

	// shape is what the key's block holds where the kind of key decides it,
	// "" where the field says.
	shape Shape

	// observed is whether only the game's own files explain the key, so the
	// scope its block runs in is not known.
	observed bool

	// iterator is the list keyword the key starts with, as every_* for
	// every_vassal, and nil for any other key.
	iterator *Keyword

	// target is the scope type the command or link leads to, and chain a
	// link's chain with the scope after each link.
	target string
	chain  []ChainLink

	// references are the names the key refers to, and uses the effects,
	// triggers and links the key is a use of.
	references []Reference
	uses       []Reference
}

// isLink reports whether the key is a link the dumps declare.
func (m match) isLink() bool {
	return m.definition != nil && m.definition.Database == DumpLinks
}

// moves reports whether the key changes the scope its block runs in: a list
// iterator, a link, or a command that leads to a scope.
func (m match) moves() bool {
	return m.iterator != nil || m.definition != nil && (m.isLink() || m.target != "" || m.definition.Iterates)
}

// ShapeRow is how a block of one shape reads the keys and values in it.
type ShapeRow struct {
	// Takes lists what a key that is no keyword is tried as, in order. The
	// first that has the key wins.
	Takes []string

	// Caller is the kind of place a call in the block is made from. AnyCall
	// lets it call anything that can be called.
	Caller string

	// Values is the database every value in the block names, "" for none.
	Values string

	// Blocks is what a block in it holds where nothing explains the block's
	// key, "" for none.
	Blocks Shape

	// Encloses is what a block in it holds when the block's key is a link or
	// a keyword such as if, "" for the shape itself.
	Encloses Shape
}

// What a key may be tried as, beside an effect, a trigger and a link.
const (
	// takeCalls is a scripted definition the key calls, the file's own first.
	takeCalls = "calls"

	// takeDeclared is a field that a row, the dumps or the docs declare.
	// takeObserved is a field only the game's own files write.
	takeDeclared = "declared"
	takeObserved = "observed"

	// takeDocumented is a field the docs write for the block's database.
	takeDocumented = "documented"

	// takeOwn is any field the block has.
	takeOwn = "own"

	// takeBareLinks is a link, except a plain word with a plain value, which
	// is more likely a field.
	takeBareLinks = "bare links"

	// takeModifiers is a modifier the dumps list. takeModifierTypes is any
	// plain word, read as the name of a modifier type.
	takeModifiers     = "modifiers"
	takeModifierTypes = "modifier types"

	// takeUnknown is any plain word that is no event id, read as a call of
	// something nothing defines.
	takeUnknown = "unknown"
)

// effectTakes and scriptTakes are what a key is tried as in a block of
// effects, and in a block that may hold any script.
var (
	effectTakes = []string{takeCalls, takeDeclared, DumpEffects, takeObserved, takeBareLinks, takeUnknown}
	scriptTakes = []string{takeCalls, takeDeclared, DumpEffects, DumpTriggers, takeObserved, DumpLinks, takeUnknown}
)

// ShapeRows holds how a block of each shape is read. In a block of a
// command's arguments, a key nothing explains is a call of anything and is
// never reported: it may be an argument the dumps do not list.
var ShapeRows = map[Shape]ShapeRow{
	ShapeFields:     {Takes: []string{takeOwn}},
	ShapeParameters: {Takes: []string{takeOwn}},
	ShapeTriggers: {
		Takes:  []string{takeCalls, takeDeclared, DumpTriggers, takeObserved, DumpLinks, takeUnknown},
		Caller: CallTrigger,
	},
	ShapeEffects:         {Takes: effectTakes, Caller: CallEffect},
	ShapeArguments:       {Takes: scriptTakes, Caller: AnyCall},
	ShapeEffectArguments: {Takes: effectTakes, Caller: AnyCall, Encloses: ShapeEffects},
	ShapeTriggerArguments: {
		Takes:  []string{takeCalls, takeDeclared, DumpTriggers, takeObserved, takeBareLinks, takeUnknown},
		Caller: AnyCall, Encloses: ShapeTriggers,
	},
	ShapeValue: {Takes: scriptTakes, Caller: CallValue},
	ShapeUnknown: {
		Takes: []string{
			takeCalls, takeDeclared, DumpEffects, DumpTriggers, DumpLinks, takeOwn, takeUnknown,
		},
		Caller: AnyCall,
	},
	ShapeSetup: {
		Takes: []string{
			takeDeclared, takeDocumented, takeCalls, DumpEffects, takeObserved, DumpLinks, takeUnknown,
		},
		Caller: CallEffect,
	},
	ShapeWidget: {
		Takes: []string{takeCalls, takeOwn, takeUnknown}, Caller: GUITagCalls, Blocks: ShapeWidget,
	},
	ShapeModifiers: {Takes: []string{takeModifiers, takeDeclared, takeObserved, takeModifierTypes}},
	// _events.info:313-318 writes a description's texts as localization keys
	ShapeDescription: {Values: Localization},
}

// linkMakers holds the commands that save something under a name, each with
// the link that reads the name back: save_scope_as saves a name scope:x
// reads. A row written command/key is the key inside the command's block
// that holds the name.
var linkMakers = map[string]string{
	"save_scope_as": SavedScopes, "save_temporary_scope_as": SavedScopes,
	"save_scope_value_as/name": SavedScopes, "save_temporary_scope_value_as/name": SavedScopes,
	"save_opinion_value_as/name": SavedScopes, "save_temporary_opinion_value_as/name": SavedScopes,

	// CK3's common/script_values, EU5's generic_actions/western_schism.txt:361
	"save_temporary_value_as": SavedScopes, "save_temporary_value_as/name": SavedScopes,

	// the option picked sets scope:<name> (CK3
	// _character_interactions.info:349, _decisions.info:315, 395)
	"send_option/flag": SavedScopes, "item/value": SavedScopes,

	// EU5's cabinet_actions/assimilate_area.txt:16
	"select_trigger/target_flag": SavedScopes,

	"set_variable": Variables, "set_variable/name": Variables,
	"set_dead_character_variable/name": Variables, "add_to_variable_list/name": Variables,
	"add_to_variable_map/name": Variables,

	"set_local_variable": LocalVariables, "set_local_variable/name": LocalVariables,
	"add_to_local_variable_list/name": LocalVariables, "add_to_local_variable_map/name": LocalVariables,

	"set_global_variable": GlobalVariables, "set_global_variable/name": GlobalVariables,
	"add_to_global_variable_list/name": GlobalVariables, "add_to_global_variable_map/name": GlobalVariables,

	// CK3 effects.log:68
	"add_to_list": Lists, "add_to_temporary_list": Lists,
	"add_to_list/name": Lists, "add_to_temporary_list/name": Lists,

	// CK3 effects.log:2984, 3014
	"add_character_flag": Flags, "add_character_flag/flag": Flags, "add_dead_character_flag/flag": Flags,
}

// Walk reads one parsed file. It returns a context per statement, the file's
// own first, and the problems it found. The contexts reuse the given slice
// where it is long enough, so a caller that walks many files can keep one.
func (s *Schema) Walk(tree *jomini.Tree, file files.File, contexts []Context) ([]Context, []Problem) {
	walker := s.newWalker(tree, file, contexts)
	walker.walkChildren(0, 1)
	walker.judgeCalls()

	return walker.contexts, walker.problems
}

// WalkDeclarations reads only what a file declares, which is all the reading
// of scripted definitions needs. Statements it skips get an empty context.
// The contexts reuse the given slice as Walk's do.
func (s *Schema) WalkDeclarations(tree *jomini.Tree, file files.File, contexts []Context) []Context {
	walker := s.newWalker(tree, file, contexts)

	// a reused context keeps only the memory its references had
	for position := 1; position < len(walker.contexts); position++ {
		walker.contexts[position] = Context{References: walker.contexts[position].References[:0]}
	}

	if walker.startsBody(&walker.contexts[0]) {
		walker.walkChildren(0, 1)
	} else {
		walker.walkDeclared(0, 1)
	}

	return walker.contexts
}

// newWalker returns the walker for one file. Where the file's database names
// its one definition after the file, the file's own context holds it.
func (s *Schema) newWalker(tree *jomini.Tree, file files.File, contexts []Context) *walker {
	dialect := LookupDialect(s.Game)
	if dialect == nil {
		dialect = &Dialect{}
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
		schema: s, dialect: dialect, tree: tree, file: file, contexts: contexts, inline: map[string]*Definition{},
		winners: mods.winners,
	}
	walker.rel = file.StageRel()
	walker.stem = strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
	walker.fileType = FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = s.FindDatabase(file)
	}

	root := &contexts[0]
	*root = Context{Shape: ShapeUnknown, References: root.References[:0]}
	walker.frames = []frame{{context: root, wraps: true}}

	if database := walker.database; database != nil {
		if database.Declaration.Contents == ContentsEvents {
			walker.declaring = s.InlineDatabases
			walker.declareInline()
		}

		if database.Declaration.DefinedBy == DefinedByFile && len(tree.Statements) > 1 {
			walker.resolveDefinition(root, Copy{Database: database.Name, Name: walker.stem, Local: database.Declaration.Local})
		}
	}

	return walker
}

// declareInline reads the namespaces and inline definitions an events file
// declares at its top. The games call an inline definition above where it is
// declared, so these are read before the walk.
func (w *walker) declareInline() {
	var bodies []int32
	for statement := range w.tree.Children(0) {
		if w.tree.Text(w.tree.Statements[statement].Key) == EventNamespaceKey {
			w.contexts[0].Namespaces = append(w.contexts[0].Namespaces, statement)
		}

		label, _, _ := w.findLabel(w.tree.Statements[statement])
		if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
			w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
			bodies = append(bodies, statement)
		}
	}

	for _, statement := range bodies {
		w.walkStatement(statement, 1)
		scripted := w.inline[w.contexts[statement].Label]
		for position := statement + 1; position < w.tree.Statements[statement].Last; position++ {
			w.schema.typeDefinition(scripted, listTypings(w.tree, position, &w.contexts[position]))
		}
	}

	// the walk of the whole file records these again
	w.saved, w.problems = nil, nil
}

// walkChildren walks the statements inside one. A definition's are walked
// in the order the game runs its blocks, where its database gives one, so
// each block sees the scopes the earlier ones saved.
func (w *walker) walkChildren(parent int32, level int) {
	var order []string
	holder := w.frames[level-1].context
	database := w.schema.Databases[holder.Database]
	if database != nil && holder.Defines && holder.Category == CategoryDefinition {
		order = database.Declaration.RunOrder
	}

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

// walkStatement decides what one statement is, then walks its block.
func (w *walker) walkStatement(statement int32, level int) {
	w.resolve(statement, level)
	if w.tree.Statements[statement].IsBlock() {
		w.walkChildren(statement, level+1)
	}
}

// walkDeclared walks the statements inside one as WalkDeclarations reads
// them: into a top block that defines nothing, and through the whole body of
// a scripted definition that takes parameters.
func (w *walker) walkDeclared(parent int32, level int) {
	for child := range w.tree.Children(parent) {
		context := w.resolve(child, level)
		switch {
		case !w.tree.Statements[child].IsBlock():
		case w.startsBody(context):
			w.walkChildren(child, level+1)
		case !context.Defines && level == 1:
			w.walkDeclared(child, level+1)
		}
	}
}

// startsBody reports whether a statement makes a scripted definition that
// takes parameters and that other files can call.
func (w *walker) startsBody(context *Context) bool {
	if !context.Defines || context.Category != CategoryDefinition || context.Definition.Local {
		return false
	}

	database := w.schema.Databases[context.Definition.Database]

	return database != nil && database.Declaration.Parameters
}

// resolve fills in and returns the context of one statement, starting from
// what the block around it passes down, then judges its value.
func (w *walker) resolve(statement int32, level int) *Context {
	if len(w.frames) <= level {
		w.frames = append(w.frames, frame{})
	}

	context := &w.contexts[statement]
	w.frames[level].context = context
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
	w.frames[level].path = context.Path
	if label != "" {
		w.frames[level].path = slices.Concat(context.Path, []string{label})
	}

	if defines && w.database != nil && definition.Database == w.database.Name {
		w.frames[level].path = nil
	}

	// a GUI block's datacontext sets the data type its calls start from, ""
	// where the dumps do not say what the call returns. A widget that writes
	// none takes the one its type writes.
	if w.file.Format == files.FormatGUI && w.tree.Statements[statement].IsBlock() {
		child, writes := w.tree.FindChild(statement, GUIContextKey)
		switch {
		case writes:
			context.Datacontext = ""
			if call, ok := w.tree.ParseBracketed(w.tree.Statements[child].Value); ok {
				context.Datacontext = w.schema.ResolveDatacontext(call, parent.Datacontext)
			}

		case context.Callee != nil && context.Callee.Datacontext != nil:
			context.Datacontext = w.schema.ResolveDatacontext(*context.Callee.Datacontext, parent.Datacontext)
		}
	}

	w.lookupValue(context, statement, level)
	judge(context.References)
	w.judgeValue(context, statement)
	w.judgeDate(context, statement)

	return context
}

// findLabel returns the word a statement is known by, unquoted: its key, or
// the tag of a block written with no key. It returns "" for a value written
// alone.
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

// resolveDefinition fills in the context of a statement that makes a
// definition. A definition starts with no saved scopes.
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

// findDefinitionShape returns what a definition's block holds: what its
// database says, else what the file's extension says, else fields.
func (w *walker) findDefinitionShape(database *Database) Shape {
	if database == nil {
		return cmp.Or(w.fileType.Shape, ShapeFields)
	}

	return cmp.Or(database.Declaration.Shape, w.fileType.Shape, ShapeFields)
}

// findRootScope returns the scope type a definition's script starts in, ""
// where nothing says. An event with no root key takes it from its type, as
// character_event gives character.
func (w *walker) findRootScope(database *Database, definition Copy) string {
	if database == nil {
		return ""
	}

	if value, ok := w.tree.FindChildValue(definition.Statement, database.Declaration.RootKey); ok {
		if w.schema.IsScope(value) {
			return value
		}

		return ""
	}

	switch database.Declaration.Contents {
	case ContentsEvents:
		events := w.dialect.Events
		eventType, _ := w.tree.FindChildValue(definition.Statement, events.TypeKey)

		return cmp.Or(strings.TrimSuffix(eventType, events.TypeSuffix), events.DefaultScope)

	case ContentsOnActions:
		if declared := database.Definitions[definition.Name]; declared != nil {
			return declared.Target
		}
	}

	return ""
}

// resolveStatement fills in the context of a statement that defines nothing.
// In a database whose definitions have no names, a statement at the top of a
// file is read as inside its one definition.
func (w *walker) resolveStatement(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	switch {
	// blockoverride "name" { … } names the block by its tag, and its braces
	// hold what the widget around it holds (CK3
	// gui/interaction_marriage.gui:190)
	case w.file.Format == files.FormatGUI && label == GUIBlockCalls && !current.Tag.IsEmpty():
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

	if database := w.database; level == 1 && database != nil && database.Declaration.DefinedBy == DefinedByNothing {
		context.Database, context.Field, context.Shape = database.Name, database.Field, w.findDefinitionShape(database)
	}

	w.judgeUnexplained(context, statement)
}

// findLead returns the word written alone just before a statement where it
// makes the statement a definition or a call: scripted_effect in an events
// file, or type, template or blockoverride in a GUI file. It returns ""
// otherwise.
func (w *walker) findLead(statement int32) string {
	lead, ok := w.tree.FindLead(statement)
	if !ok {
		return ""
	}

	text := w.tree.Text(w.tree.Statements[lead].Value)
	isGUILead := w.file.Format == files.FormatGUI &&
		(slices.Contains(GUIDeclares, text) || len(w.schema.Callees[text]) > 0)
	if w.declaring[text] == "" && !isGUILead {
		return ""
	}

	return text
}

// resolveLead reads a call written as two words, as blockoverride "header",
// adding a reference to the name in each database the first word calls into.
func (w *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range w.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}

	if lead != "" && len(w.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// resolveKey fills in the context of a statement with a key: what the key
// is, what its block holds and the scope the block runs in. A key with an
// argument in parentheses, as aptitude(court_jester_court_position), is read
// as the command before the parentheses.
func (w *walker) resolveKey(context *Context, statement int32, level int) {
	parent := w.frames[level-1].context
	key, span, class := w.frames[level].label, w.frames[level].span, w.frames[level].class

	// a key with a parameter in it is not known until a call fills it in
	if class == jomini.ClassParameter {
		context.Category, context.Shape, context.Scope = CategoryParameter, ShapeUnknown, ""

		return
	}

	step, command, isCall := w.schema.findStatementCall(w.tree, statement)
	if isCall {
		key, span, class = step.Text, step.Span, jomini.Classify(step.Text)
	}

	// a keyword whose block may hold the same keyword again, as Vic3's else
	// in a progress bar, is found by its own key inside itself
	current := w.tree.Statements[statement]
	block := current.IsBlock()
	keyword := parent.Keyword
	if keyword == nil || !keyword.Recursive || !strings.EqualFold(keyword.Key, key) {
		keyword = nil
		if row, ok := w.dialect.LookupKeyword(findShape(parent), w.frames[level-1].label, key, block); ok {
			keyword = &row
		}
	}

	// a block a key is compared with is a formula, as in
	// list_size(x) < { value = … }. A block with no operator, as a GUI
	// file's Name { }, compares nothing.
	operator := w.tree.Text(current.Op)
	hasComparison := block && operator != "" && operator != "=" && operator != "?="
	fit, fits := w.findFit(parent, keyword, key, span, class, block, isCall)
	w.resolveMatch(context, parent, fit, fits, keyword, key, hasComparison)
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

	// a keyword nothing else explains takes what the block around it says of
	// the key, else what the command opening that block says, as the dumps
	// give an if its limit. A math keyword's value is a number, so it takes
	// no field.
	if keyword != nil && keyword.Class != KeywordClassMath && context.Field == nil {
		context.Field = parent.Field.findPatternChild(key, class)
		if context.Field == nil && parent.Command != nil {
			context.Field = parent.Command.Field.findChild(key)
		}
	}

	w.resolveValueName(context, statement, level)
}

// findFit returns what a key is in its block: the first thing the block's
// shape lists that has the key. A keyword is tried only as the effect or
// trigger of its name, since the dumps list keywords such as if among the
// effects.
func (w *walker) findFit(
	parent *Context, keyword *Keyword, key string, span jomini.Span, class jomini.Class, block, isCall bool,
) (match, bool) {
	if keyword != nil && !strings.EqualFold(keyword.Key, key) {
		return match{}, false
	}

	for _, entry := range ShapeRows[findShape(parent)].Takes {
		if keyword != nil && entry != DumpEffects && entry != DumpTriggers {
			continue
		}

		fit, fits := w.take(entry, parent, key, span, class, block, isCall)
		if !fits {
			continue
		}

		// a command such as every_vassal runs its block in the scope of
		// each item of the list
		if iterator, iterates := MatchListIterator(key); iterates && keyword == nil && fit.definition != nil && !fit.isLink() {
			fit.iterator = new(iterator)
		}

		return fit, true
	}

	return match{}, false
}

// take tries a key as one thing a block's shape lists. It returns false if
// the key is not that thing.
func (w *walker) take(
	entry string, parent *Context, key string, span jomini.Span, class jomini.Class, block, isCall bool,
) (match, bool) {
	switch entry {
	case takeCalls:
		return w.takeCall(parent, key, span)

	case takeDeclared, takeObserved:
		child := parent.Field.findNumberChild(key, class)
		isObserved := child != nil && child.Source == SourceVanilla
		if child == nil || child.Source == SourceNone || isObserved != (entry == takeObserved) {
			return match{}, false
		}

		fit := takeField(parent, child, key, span)
		fit.observed = isObserved

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

	case DumpLinks, takeBareLinks:
		// where the block takes only links that look like one, a plain
		// word with a plain value is left to be a field
		isPlain := entry == takeBareLinks && !block && !strings.ContainsAny(key, ":.")
		link := w.schema.FindLink(key)
		if isPlain || link == nil {
			return match{}, false
		}

		return w.makeLink(link, parent, key, span), true
	}

	return w.takeCommand(entry, parent, key, span)
}

// takeCall reads a key as a call of a scripted definition. The key's block
// holds the definition's parameters, or the fields of its database for a
// definition that takes none.
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
		fit.shape = ShapeParameters
	}

	if callee.Database != "" {
		fit.references = []Reference{{Database: callee.Database, Name: key, Span: span, Caller: caller}}
	}

	return fit, true
}

// findCalledDefinition returns the scripted definition a key calls from the
// given kind of place, and the kind of place that finds it. The file's own
// definitions come first. In a GUI file a key may also name a widget type.
func (w *walker) findCalledDefinition(caller, key string) (*Definition, string, bool) {
	if scripted, ok := w.inline[key]; ok {
		callers := w.schema.Databases[scripted.Database].Declaration.Callers
		if len(callers) > 0 && (caller == AnyCall || slices.Contains(callers, caller)) {
			return scripted, callers[0], true
		}
	}

	if caller == AnyCall {
		return w.schema.findScripted(w.winners, key, w.file.Format)
	}

	callee := w.schema.lookupCall(w.winners, caller, key)
	if callee == nil && caller == GUITagCalls {
		callee = w.schema.Databases[GUIWidgetTypes].Definitions[key]
	}

	return callee, caller, callee != nil && caller != ""
}

// takeField reads a key as a field of its block. Where that block's keys
// name definitions, as a culture's opinions block names cultures, the key is
// also a reference, unless the dumps declare it, as base in stress_impact.
func takeField(parent *Context, field *Field, key string, span jomini.Span) match {
	fit := match{category: CategoryField, field: field}
	if parent.Field == nil || field != nil && field.Source == SourceDumps {
		return fit
	}

	if keys := parent.Field.Keys; keys.Value != "" {
		fit.references = []Reference{{Database: keys.Value, Name: key, Span: span, Inferred: !keys.Source.CanAccuse()}}
	}

	return fit
}

// takeModifier reads a key in a block of modifiers as a modifier the dumps
// list, or else as any plain word. Where the game has modifier types, the
// key refers to the type of its name, which a modifier the dumps list may
// lack without a mistake.
func (w *walker) takeModifier(
	entry string, parent *Context, key string, span jomini.Span, class jomini.Class,
) (match, bool) {
	isListed := entry == takeModifiers
	types := w.schema.ModifierTypes
	if isListed && !w.schema.DeclaresModifier(key) || !isListed && (types == "" || !class.IsIdentifier()) {
		return match{}, false
	}

	field := parent.Field.findChild(key)
	if declared := w.schema.LookupScripted(w.winners, types, key); declared != nil {
		field = declared.Field
	}

	if types == "" {
		return takeField(parent, field, key, span), true
	}

	fit := match{category: CategoryField, field: field}
	if !isListed {
		fit.category = CategoryReference
	}

	fit.references = []Reference{{Database: types, Name: key, Span: span, Inferred: isListed}}

	return fit, true
}

// takeUnknown reads a plain word that nothing else explains as a call of a
// name nothing defines, so it can be reported. With an argument in
// parentheses it may be a link that takes a name, and every_my_list refers
// to a scripted list.
func (w *walker) takeUnknown(
	parent *Context, key string, span jomini.Span, class jomini.Class, isCall bool,
) (match, bool) {
	if !class.IsIdentifier() || class == jomini.ClassEventID {
		return match{}, false
	}

	if isCall {
		if link := w.schema.LookupLink(key, true); link != nil {
			return w.makeLink(link, parent, key, span), true
		}
	}

	fit := match{category: CategoryCall}
	caller := parent.FindCaller()
	iterator, iterates := MatchListIterator(key)
	switch {
	case iterates && w.schema.Lists != "":
		fit.iterator = new(iterator)
		list := strings.TrimPrefix(key, strings.TrimSuffix(iterator.Key, "*"))
		fit.references = []Reference{{Database: w.schema.Lists, Name: list, Span: span}}

	case caller == AnyCall:
		fit.references = []Reference{{Database: AnyCall, Name: key, Span: span, Caller: AnyCall}}

	default:
		for _, database := range w.schema.Callees[caller] {
			fit.references = append(fit.references, Reference{Database: database, Name: key, Span: span, Caller: caller})
		}
	}

	return fit, true
}

// makeLink returns what a key that starts with a link is: its chain followed
// to the scope it leads to. The key's block takes the same keys as the block
// around it.
func (w *walker) makeLink(link *Definition, parent *Context, key string, span jomini.Span) match {
	chain := w.schema.ResolveChain(key, parent, w.saved)

	return match{
		category: CategoryLink, definition: link, field: parent.Field, shape: findEnclosing(parent),
		target: chain[len(chain)-1].Scope, chain: chain,
		references: w.listPrefixed(jomini.Links(key, span.Start)), uses: w.schema.listLinkUses(key, span.Start),
	}
}

// listLinkUses returns a reference to each link of a chain the dumps
// declare. They show where a link is used and are never a mistake.
func (s *Schema) listLinkUses(chain string, start int) []Reference {
	var uses []Reference
	for _, link := range jomini.Links(chain, start) {
		span := link.Span
		if !link.Prefix.IsEmpty() {
			span = link.Prefix
		}

		definition := s.LookupLink(chain[span.Start-start:span.End-start], !link.Prefix.IsEmpty())
		if definition != nil {
			uses = append(uses, Reference{Database: DumpLinks, Name: definition.Key, Span: span, Inferred: true})
		}
	}

	return uses
}

// takeCommand reads a key as an effect or a trigger of the given database:
// one the dumps list, or one that fits a star key, as has_relation_rival
// fits has_relation_*.
func (w *walker) takeCommand(database string, parent *Context, key string, span jomini.Span) (match, bool) {
	if !parent.Takes(database) {
		return match{}, false
	}

	definition, patterns := w.schema.LookupDefinition(database, key), w.schema.findPatterns(database, key)
	if definition == nil && len(patterns) > 0 {
		definition = patterns[0]

		// a list iterator a pattern makes takes the keys of the plain
		// iterator of its scope, as any_character
		if iterator, iterates := MatchListIterator(key); iterates {
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
		fit.shape = ShapeUnknown
	case definition.Database == DumpTriggers:
		fit.shape = ShapeTriggerArguments
	default:
		fit.shape = ShapeEffectArguments
	}

	lower := strings.ToLower(key)
	for _, pattern := range patterns {
		start, end, _ := pattern.findStar(lower)
		for _, database := range pattern.Star {
			fit.references = append(fit.references, Reference{
				Database: database, Name: key[start:end], Span: jomini.Span{Start: span.Start + start, End: span.Start + end},
			})
		}
	}

	return fit, true
}

// findBlockShape returns what a field's block holds: what a source says,
// else a formula where the field takes a script value, since a formula may
// be written wherever a script value works (CK3 _script_values.info). It
// returns "" for neither.
func findBlockShape(field *Field) Shape {
	switch {
	case field == nil:
		return ""
	case field.Shape.IsStated():
		return field.Shape.Value
	case field.Value.Value.Values:
		return ShapeValue
	}

	return ""
}

// resolveMatch writes what a key turned out to be into its context, with
// what its block holds and the scope the block runs in. The block of a
// keyword such as if takes the same keys as the block around it.
func (w *walker) resolveMatch(
	context, parent *Context, fit match, fits bool, keyword *Keyword, key string, hasComparison bool,
) {
	context.Field, context.Category = fit.field, fit.category
	context.Command, context.Callee = fit.definition, fit.callee
	context.Iterates = fit.iterator != nil
	if keyword != nil {
		if keyword.Shape == ShapeEnclosing {
			context.Field = parent.Field
		}

		context.Keyword, context.Category = keyword, CategoryKeyword
		context.Preview = context.Preview || keyword.Preview
	}

	context.Shape = findKeyShape(parent, fit, fits, keyword, hasComparison)
	context.Scope = w.findKeyScope(context, parent, fit, fits, keyword, key)
	if fit.moves() {
		context.Previous = slices.Concat([]string{parent.Scope}, parent.Previous)
	}
}

// findKeyShape returns what the block of a statement with a key holds. The
// first case that fits decides.
func findKeyShape(parent *Context, fit match, fits bool, keyword *Keyword, hasComparison bool) Shape {
	switch {
	// a block a key is compared with is a formula
	case hasComparison:
		return ShapeValue

	// a list inside a formula holds the formula's math, as
	// add_gold = { every_child = { add = 1 } } (CK3 _script_values.info)
	case fit.iterator != nil && findShape(parent) == ShapeValue:
		return ShapeValue

	case fit.iterator != nil:
		return fit.iterator.Shape

	case keyword != nil && keyword.Shape == ShapeEnclosing:
		return findEnclosing(parent)

	case keyword != nil && keyword.Shape != "":
		return keyword.Shape

	case fits:
		return cmp.Or(fit.shape, findBlockShape(fit.field), ShapeUnknown)
	}

	return ShapeRows[findShape(parent)].Blocks
}

// findKeyScope returns the scope type the block of a statement with a key
// runs in, "" where it is not known. The first case that fits decides.
func (w *walker) findKeyScope(
	context, parent *Context, fit match, fits bool, keyword *Keyword, key string,
) string {
	hasShape := context.Shape != "" && context.Shape != ShapeUnknown
	isExplained := fits && !fit.observed
	isKeywordBlock := keyword != nil && !fits && keyword.Shape != ""
	isArgument := parent.HoldsArguments() && parent.Field.findChild(key) != nil
	switch {
	// the arguments of a call are read inside the definition called
	case context.Shape == ShapeParameters:
		return ""

	// an argument of a command is read in a scope the command makes, which
	// is not known. A keyword such as limit is read in the scope the command
	// is written in.
	case isArgument && (keyword == nil || keyword.Class != KeywordClassConditional):
		return ""

	case fit.moves() && w.schema.IsScope(fit.target):
		return fit.target

	case fit.moves():
		return ""

	// the scope stays the same inside a key that a source other than the
	// game's own files explains, and inside a keyword whose row says what its
	// block holds
	case hasShape && (isExplained || isKeywordBlock):
		return parent.Scope
	}

	return ""
}

// judgeScope records a problem for an effect, a trigger or a link written in
// a scope the dumps do not allow it in. It looks only at keys written
// directly in a block of effects or of triggers.
func (w *walker) judgeScope(parent *Context, fit match, level int, isCall bool) {
	eligible := parent.Shape == ShapeEffects || parent.Shape == ShapeTriggers
	if fit.definition == nil || !eligible || parent.HoldsArguments() {
		return
	}

	key, span := w.frames[level].label, w.frames[level].span
	scope, isMoved := parent.Scope, len(parent.Previous) > 0
	switch {
	case !fit.isLink():
		w.judgeCommandScope(span, fit.definition, scope, isMoved)

	case !isCall:
		links := jomini.Links(key, span.Start)
		for position, link := range fit.chain {
			w.judgeCommandScope(links[position].Span, link.Definition, scope, isMoved)
			scope, isMoved = link.Scope, true
		}
	}
}

// judgeCommandScope records a problem for one command or link written in a
// scope type the dumps do not list for it. Nothing is checked until a link
// or a command has moved the scope.
func (w *walker) judgeCommandScope(span jomini.Span, command *Definition, scope string, isMoved bool) {
	if command == nil || len(command.Scopes) == 0 || scope == "" || !isMoved || slices.Contains(command.Scopes, scope) {
		return
	}

	facts := DescribeReference(Reference{Name: w.tree.Text(span)})
	facts["scope"], facts["allowed"] = scope, strings.Join(command.Scopes, ", ")
	w.record(ProblemScope, span, facts)
}

func (w *walker) record(code string, span jomini.Span, facts map[string]string) {
	w.problems = append(w.problems, Problem{Code: code, Span: span, Facts: facts})
}

// judgeFollows records a problem for a keyword that must come right after
// another, as else after if, and does not.
func (w *walker) judgeFollows(keyword *Keyword, statement int32) {
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
	facts["follows"] = JoinList(keyword.Follows, "or")
	w.record(ProblemFollows, current.Key, facts)
}

// linkUsers holds the triggers and list commands that read back a name a
// command saved, each with the link that reads such a name: has_variable
// reads what var:x reads. Its rows are written as linkMakers' are, from the
// games' triggers.log and effects.log.
var linkUsers = map[string]string{
	"has_variable": Variables, "has_variable/name": Variables, "has_all_variables/name": Variables,
	"has_none_of_variables/name": Variables, "has_dead_character_variable": Variables,
	"has_variable_list": Variables, "variable_list_size/name": Variables,
	"is_target_in_variable_list/name": Variables, "has_variable_map": Variables,
	"variable_map_size/name": Variables, "is_key_in_variable_map/name": Variables,
	"is_value_in_variable_map/name": Variables, "any_key_in_variable_map/variable": Variables,
	"every_key_in_variable_map/variable": Variables, "ordered_key_in_variable_map/variable": Variables,
	"random_key_in_variable_map/variable": Variables, "any_in_list/variable": Variables,
	"every_in_list/variable": Variables, "ordered_in_list/variable": Variables,
	"random_in_list/variable": Variables,

	"has_local_variable": LocalVariables, "has_local_variable_list": LocalVariables,
	"local_variable_list_size/name": LocalVariables, "is_target_in_local_variable_list/name": LocalVariables,
	"has_local_variable_map": LocalVariables, "local_variable_map_size/name": LocalVariables,
	"is_key_in_local_variable_map/name": LocalVariables, "is_value_in_local_variable_map/name": LocalVariables,
	"any_key_in_local_variable_map/variable":     LocalVariables,
	"every_key_in_local_variable_map/variable":   LocalVariables,
	"ordered_key_in_local_variable_map/variable": LocalVariables,
	"random_key_in_local_variable_map/variable":  LocalVariables, "any_in_local_list/variable": LocalVariables,
	"every_in_local_list/variable": LocalVariables, "ordered_in_local_list/variable": LocalVariables,
	"random_in_local_list/variable": LocalVariables,

	"has_global_variable": GlobalVariables, "has_global_variable_list": GlobalVariables,
	"global_variable_list_size/name":         GlobalVariables,
	"is_target_in_global_variable_list/name": GlobalVariables,
	"has_global_variable_map":                GlobalVariables, "global_variable_map_size/name": GlobalVariables,
	"is_key_in_global_variable_map/name":          GlobalVariables,
	"is_value_in_global_variable_map/name":        GlobalVariables,
	"any_key_in_global_variable_map/variable":     GlobalVariables,
	"every_key_in_global_variable_map/variable":   GlobalVariables,
	"ordered_key_in_global_variable_map/variable": GlobalVariables,
	"random_key_in_global_variable_map/variable":  GlobalVariables,
	"any_in_global_list/variable":                 GlobalVariables,
	"every_in_global_list/variable":               GlobalVariables,
	"ordered_in_global_list/variable":             GlobalVariables,
	"random_in_global_list/variable":              GlobalVariables,

	"is_in_list": Lists, "any_in_list/list": Lists, "every_in_list/list": Lists,
	"ordered_in_list/list": Lists, "random_in_list/list": Lists,

	"has_character_flag": Flags, "has_dead_character_flag": Flags, "remove_character_flag": Flags,
}

// resolveValueName reads a value that is a name of its own. A name a command
// saves, as target in save_scope_as = target, or a second name for the
// definition around it, as a CK3 trait's group, becomes a definition. A
// saved name read back, as in has_variable = x, becomes a reference. A saved
// scope keeps the type of the scope it was saved in, unknown where a key
// inside a command's block saves it.
func (w *walker) resolveValueName(context *Context, statement int32, level int) {
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

	owner := w.schema.Databases[parent.Database]
	alias, isAlias := "", false
	if owner != nil && parent.Defines && parent.Category == CategoryDefinition {
		alias, isAlias = owner.Declaration.Aliases[key]
	}

	link, isKey, isSaved := findLinked(linkMakers, holder, key)
	scope := parent.Scope
	if isKey || parent.HoldsArguments() {
		scope = ""
	}

	if !isSaved && parent.Callee != nil {
		link, isSaved = parent.Callee.Saves[key]
		scope = ""
	}

	definition := Copy{Name: label, Span: span, Statement: statement}
	switch {
	case isAlias:
		definition.Database, definition.Alias = cmp.Or(alias, owner.Name), true

	case isSaved:
		definition.Database = link
		if link == SavedScopes {
			w.saved = append(w.saved, Saved{Name: label, Type: scope})
		}

	default:
		return
	}

	context.Definition, context.Defines = &definition, true
}

// findLinked looks a key up in linkMakers or linkUsers, as a command and
// then as a key inside the block of the command around it. isKey reports
// that the row is for such a key.
func findLinked(table map[string]string, holder, key string) (link string, isKey, ok bool) {
	if link, ok = table[key]; ok {
		return link, false, true
	}

	link, ok = table[strings.ToLower(holder)+"/"+key]

	return link, true, ok
}

// judgeUnexplained records an event whose name opens with no namespace its
// file declares, or a key nothing explains. A key is reported only if it is
// a plain word, in a block whose keys a firm source lists, where the game's
// own files never write anything else.
func (w *walker) judgeUnexplained(context *Context, statement int32) {
	current := w.tree.Statements[statement]
	if context.Defines || current.Key.IsEmpty() || context.Category != CategoryNone || context.Field != nil {
		return
	}

	label := w.tree.Unquoted(current.Key)
	if current.Parent == 0 && current.KeyClass == jomini.ClassEventID {
		if w.database == nil || w.database.Declaration.Contents != ContentsEvents {
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
		holder.Field == nil || holder.Shape == ShapeUnknown || !holder.Field.Source.CanAccuse() ||
		holder.Field.Vanilla.Count > 0 {
		return
	}

	w.record(ProblemUnknownKey, current.Key, DescribeReference(Reference{Database: context.Database, Name: label}))
}
