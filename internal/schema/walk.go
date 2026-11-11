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

// Definition is a name that a statement defines in a database, with the
// facts that decide which definition of that name the game keeps.
type Definition struct {
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
	Definition *Definition

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

	// Command is the command the key names. It is nil if it names none.
	Command *Command

	// Callee is the scripted definition the key calls. It is nil for any
	// other key.
	Callee *ScriptedDefinition

	// Declaration is how the dumps declare Command in this block.
	Declaration game.Declaration

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
	nestings []game.Nesting

	// declaring holds, by key, the database whose definition the key
	// declares inline. It is nil outside an events file.
	declaring map[string]string

	// inline holds the names the file declares for itself: every one of an
	// events file, and those a GUI file has declared so far.
	inline map[string]ScriptedDefinition

	// mods, modifiers and namespaces hold the mods' scripted definitions,
	// what their modifier types take and the event namespaces they declare,
	// as they were when the walk began.
	mods       map[string]ScriptedDefinition
	modifiers  map[string]*Field
	namespaces map[string]bool

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

// match is what a key fits under one rule of its block's shape.
type match struct {
	category Category

	// field is the field the keys inside the key's block come from.
	field *Field

	// shape is the block's shape where the rule decides it. It is "" for a
	// claim, which findMatch fills.
	shape game.Shape

	// observed is whether the key fits only because the game's own files
	// write it, so the scope inside its block is not known.
	observed bool

	// command is the command that fits, and declaration what declares it.
	command     *Command
	declaration game.Declaration

	// callee is the scripted definition that a known call names, and caller
	// the caller it is called through.
	callee *ScriptedDefinition
	caller string

	// iterator is the list iterator that the key starts.
	iterator *game.Keyword

	// target is the scope that the command or the link leads to.
	target string

	// database is the database the key itself names, and inferred whether
	// only the game's own files say so, so that it never accuses.
	database string
	inferred bool

	// patterns are the command patterns that make the key.
	patterns []CommandPattern
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
		schema: s, row: row, tree: tree, file: file, contexts: contexts, inline: map[string]ScriptedDefinition{},
		mods: mods.all, modifiers: mods.modifiers, namespaces: mods.namespaces,
	}
	walker.rel = file.StageRel()
	walker.stem = strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
	walker.fileType = game.FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = s.FindDatabase(file)
	}

	walker.nestings = row.FindNestings(walker.rel)
	if database := walker.database; database != nil && slices.Contains(s.Setups, database.Row.Folder) {
		walker.nestings = row.ListSetupNestings(database.Row.Folder)
	}

	// the walk writes every other context of the list as it reaches it
	root := &contexts[0]
	*root = Context{Shape: game.ShapeUnknown, References: root.References[:0]}
	if database := walker.database; database != nil {
		if database.Row.Contents == game.ContentsEvents {
			root.Namespaces = listNamespaces(tree)
			walker.declaring = s.InlineDatabases
			walker.declareInline()
		}

		if database.Row.DefinedBy == game.DefinedByFile && len(tree.Statements) > 1 {
			walker.resolveDefinition(root, Definition{Database: database.Name, Name: walker.stem, Local: database.Callable.Local})
		}
	}

	walker.frames = []frame{{context: root, wraps: true}}

	return walker
}

// declareInline records each definition that an events file declares inline
// at its top. The games call such a definition above the line that declares
// it, as CK3's events under events/dlc/ep3 do, so the names are read before
// any script.
func (w *walker) declareInline() {
	for statement := range w.tree.Children(0) {
		label, _, _ := w.findLabel(w.tree.Statements[statement])
		if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
			w.inline[label] = makeScripted(owner, w.tree, statement)
		}
	}
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

	return database.Row.RunOrder
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

	return database != nil && database.Callable.Parameters
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
func (w *walker) resolveDefinition(context *Context, definition Definition) {
	database := w.schema.Databases[definition.Database]
	*context = Context{
		Parent: context.Parent, Database: definition.Database, Definition: &definition, Defines: true,
		Category: CategoryDefinition, Shape: w.findDefinitionShape(database), References: context.References[:0],
	}
	if database != nil {
		context.Field = database.Definition
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

	return cmp.Or(database.Callable.Shape, database.Row.Shape, w.fileType.Shape, game.ShapeFields)
}

// findRootScope returns the scope a definition's script starts in: the scope
// its root key names, else its event type's scope, or its on_action's scope
// in the dumps. It returns "" if nothing says, or if the root key names no
// scope.
func (w *walker) findRootScope(database *Database, definition Definition) string {
	if database == nil {
		return ""
	}

	if value, ok := w.tree.FindChildValue(definition.Statement, database.Row.RootKey); ok {
		if w.schema.isScope(value) {
			return value
		}

		return ""
	}

	switch database.Row.Contents {
	case game.ContentsEvents:
		events := w.row.Events
		eventType, _ := w.tree.FindChildValue(definition.Statement, events.TypeKey)

		return cmp.Or(strings.TrimSuffix(eventType, events.TypeSuffix), events.DefaultScope)

	case game.ContentsOnActions:
		return w.schema.OnActions[definition.Name].Scope
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
	case label != "":
		w.frames[level].wraps = w.frames[level-1].wraps && w.database != nil &&
			slices.Contains(w.database.Row.WrapperKeys, label)
		w.resolveKey(context, statement, level)
		w.resolveLead(context, w.findLead(statement), label, span)

	case current.IsBlock():
		context.Shape, context.Field, context.Scope = parent.Shape, parent.Field, parent.Scope

	case current.IsListed():
		context.Field = parent.Field
	}

	if database := w.database; level == 1 && database != nil && database.Row.DefinedBy == game.DefinedByNothing {
		context.Database, context.Field, context.Shape = database.Name, database.Definition, w.findDefinitionShape(database)
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
	w.resolveMatch(context, parent, fit, fits, keyword, isCompared(w.tree, statement))

	// a conditional keyword such as limit is read in the scope of the
	// command that holds it, any other argument in a scope the command makes
	if isArgument(parent, key) && (keyword == nil || keyword.Class != game.KeywordClassConditional) {
		context.Scope = ""
	}

	if keyword == nil {
		w.resolveCallReferences(context, fit, key, span)
	}

	if isCall {
		context.References = append(context.References,
			w.listArgumentReferences(command, parent.FindArguments(command, context.Declaration), step.Arguments[0])...)
	}

	// a math keyword's value is a number or a script value, so it takes no
	// field the game's files taught
	if keyword != nil && keyword.Class != game.KeywordClassMath && context.Field == nil {
		context.Field = findKeywordField(parent, key, class)
	}

	w.resolveSaved(context, statement, level)
	w.resolveAlias(context, statement, level)
}

// findFit returns what a key fits in the block of a context, given the
// keyword the key is there, nil for none, and whether it fits anything. No
// rule of the block runs for a keyword: where the dumps declare its own key
// a command, as they declare if, their entry gives its arguments, and a
// keyword that stands for any key, as a switch's case does, has none. Any
// other key fits the first rule of the block's shape. The flags say whether
// the key's statement is a block and whether the key is written with an
// argument in parentheses. The match names the scope a link or a command
// leads to, and the iterator a key that is no keyword starts.
func (w *walker) findFit(parent *Context, keyword *game.Keyword, key string, class jomini.Class, block, isCall bool) (match, bool) {
	var fit match
	fits := false
	switch {
	case keyword == nil:
		fit, fits = w.findMatch(parent, key, class, block)
	case strings.EqualFold(keyword.Key, key):
		if entry, ok := w.matchCommand(parent, key, class, block); ok {
			fit, fits = entry, true
		}
	}

	// a link that requires data takes it in parentheses too, as
	// relations(scope:overlord) does, though its name alone is no link
	if isCall && fit.category == CategoryCall && fit.callee == nil && w.schema.IsLink(key) {
		fit, fits = match{category: CategoryLink, field: parent.Field, shape: findEnclosing(parent)}, true
	}

	// a key that starts an iterator and is no keyword runs its block in the
	// scope of the list's item
	iterator, isIterator := game.MatchListIterator(key)
	unknownCall := fit.category == CategoryCall && fit.callee == nil
	if isIterator && keyword == nil && (fit.command != nil || unknownCall && w.schema.Lists != "") {
		fit.iterator = new(iterator)
	}

	switch {
	case fit.category == CategoryLink:
		head, _ := splitLinkHead(key)
		fit.command, fit.declaration = w.schema.Commands[head], game.DeclarationLink
		fit.target = w.resolveChain(key, parent)

	case fit.command != nil:
		fit.target = fit.command.Target
	}

	return fit, fits
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

	return parent.Command.ArgumentsOf(parent.Declaration).findChild(key)
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
	return cmp.Or(game.Shapes[findShape(context)].Encloses, findShape(context))
}

// ruleMatchers holds, by key rule, the function that tells what a key fits
// under that rule and whether it fits. Each takes the context of the block
// that holds the key, the key and its class, and whether the key's statement
// is a block.
var ruleMatchers = map[game.KeyRule]func(*walker, *Context, string, jomini.Class, bool) (match, bool){
	game.KeyRuleKnownCall:    (*walker).matchKnownCall,
	game.KeyRuleDeclared:     (*walker).matchDeclared,
	game.KeyRuleDocumented:   (*walker).matchDocumented,
	game.KeyRuleCommand:      (*walker).matchCommand,
	game.KeyRuleLink:         (*walker).matchLink,
	game.KeyRuleScopeLink:    (*walker).matchScopeLink,
	game.KeyRuleModifier:     (*walker).matchModifier,
	game.KeyRuleModifierType: (*walker).matchModifierType,
	game.KeyRuleObserved:     (*walker).matchObserved,
	game.KeyRuleField:        (*walker).matchField,
	game.KeyRuleFieldOrCall:  (*walker).matchFieldOrCall,
	game.KeyRuleCall:         (*walker).matchCall,
}

// findMatch returns what a key of the given class fits under the first rule
// of the parent block's shape that it fits, and whether one does. A rule
// ruleMatchers does not hold fits nothing. Where the rule gives no shape, the
// match takes what findBlockShape gives its field, else unknown. A field's
// key in a block whose keys name definitions also refers to one. The flag
// says whether the key's statement is a block.
func (w *walker) findMatch(parent *Context, key string, class jomini.Class, block bool) (match, bool) {
	for _, rule := range game.Shapes[findShape(parent)].KeyRules {
		matcher, ok := ruleMatchers[rule]
		if !ok {
			continue
		}

		fit, fits := matcher(w, parent, key, class, block)
		if !fits {
			continue
		}

		fit.shape = cmp.Or(fit.shape, findBlockShape(fit.field), game.ShapeUnknown)

		// a key the dumps declare is an argument, as stress_impact's base is,
		// and none of the names
		declared := fit.field != nil && fit.field.Source == game.SourceDumps
		if fit.category == CategoryField && fit.database == "" && parent.Field != nil && !declared {
			fit.database, fit.inferred = parent.Field.Keys.Value, !parent.Field.Keys.Source.CanAccuse()
		}

		return fit, true
	}

	return match{}, false
}

// matchKnownCall fits a key that names a scripted definition the block
// calls, the file's own first. The keys in its block are the definition's
// parameters, else the fields of the definition's database.
func (w *walker) matchKnownCall(parent *Context, key string, _ jomini.Class, _ bool) (match, bool) {
	callee, caller, ok := w.findCalledDefinition(parent.FindCaller(), key)
	field := callee.Parameters
	if database := w.schema.Databases[callee.Database]; field == nil && database != nil {
		field = database.Definition
	}

	return match{category: CategoryCall, field: field, callee: &callee, caller: caller}, ok
}

// matchDeclared fits a key that the block's field holds from a source other
// than the game's own files. A number or a date fits the field's <number>
// child.
func (w *walker) matchDeclared(parent *Context, key string, class jomini.Class, _ bool) (match, bool) {
	child := parent.Field.findNumberChild(key, class)

	return match{category: CategoryField, field: child}, child != nil && child.Source != game.SourceNone &&
		child.Source != game.SourceVanilla
}

// matchDocumented fits a key that the docs write for the definitions of the
// block's database.
func (w *walker) matchDocumented(parent *Context, key string, _ jomini.Class, _ bool) (match, bool) {
	database := w.schema.Databases[parent.Database]

	return match{category: CategoryField, field: parent.Field.findChild(key)}, database != nil && database.Documented[key]
}

// matchCommand fits a command that the dumps declare, or a pattern makes,
// and that the block takes. The match holds the command's arguments and the
// shape of its block. It returns false if no command matches or the block
// does not take the command.
func (w *walker) matchCommand(parent *Context, key string, _ jomini.Class, _ bool) (match, bool) {
	command, ok := w.schema.FindCommand(key)
	fit := match{category: CategoryCommand, command: command, patterns: w.schema.findPatterns(key)}
	if !ok && len(fit.patterns) > 0 {
		fit.command = w.schema.Commands[fit.patterns[0].Commands[0]]

		// a list iterator a pattern makes takes the plain iterator of its
		// scope, as any_character, for its arguments
		if iterator, listed := game.MatchListIterator(key); listed {
			fit.command = cmp.Or(w.schema.Commands[strings.TrimSuffix(iterator.Key, "*")+fit.command.Target], fit.command)
		}
	}

	if fit.command == nil {
		return match{}, false
	}

	fit.declaration, ok = parent.FindDeclaration(fit.command)
	fit.field = parent.FindArguments(fit.command, fit.declaration)
	switch shape := findBlockShape(fit.field); {
	case shape != "":
		fit.shape = shape
	case fit.field != nil && fit.field == fit.command.Union:
		fit.shape = game.ShapeUnknown
	case fit.declaration == game.DeclarationTrigger:
		fit.shape = game.ShapeTriggerArguments
	default:
		fit.shape = game.ShapeEffectArguments
	}

	return fit, ok
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

// matchLink fits a key that starts with a scope link, as scope:target does.
// Its block keeps the field of the block around it, in the shape a link
// encloses there.
func (w *walker) matchLink(parent *Context, key string, _ jomini.Class, _ bool) (match, bool) {
	return match{category: CategoryLink, field: parent.Field, shape: findEnclosing(parent)}, w.schema.isKeyLink(key)
}

// matchScopeLink does what matchLink does, but refuses a plain name with a
// plain value, which is an argument or a call.
func (w *walker) matchScopeLink(parent *Context, key string, class jomini.Class, block bool) (match, bool) {
	if !block && !strings.ContainsAny(key, ":.") {
		return match{}, false
	}

	return w.matchLink(parent, key, class, block)
}

// matchModifier fits a modifier the dumps declare. The key names a modifier
// type, inferred, since nothing says that every modifier has a definition.
func (w *walker) matchModifier(parent *Context, key string, _ jomini.Class, _ bool) (match, bool) {
	_, declared := w.schema.Modifiers[key]

	return match{
		category: CategoryField, field: w.findModifierField(parent.Field, key), database: w.schema.ModifierTypes,
		inferred: true,
	}, declared
}

// matchModifierType fits any identifier as the name of a modifier type, in a
// game whose modifier types have a database.
func (w *walker) matchModifierType(parent *Context, key string, class jomini.Class, _ bool) (match, bool) {
	return match{
		category: CategoryReference, field: w.findModifierField(parent.Field, key), database: w.schema.ModifierTypes,
	}, w.schema.ModifierTypes != "" && class.IsIdentifier()
}

// matchObserved fits a key that only the game's own files write in the
// block's field. A number or a date fits the field's <number> child. The
// scope inside its block is not known.
func (w *walker) matchObserved(parent *Context, key string, class jomini.Class, _ bool) (match, bool) {
	child := parent.Field.findNumberChild(key, class)

	return match{category: CategoryField, field: child, observed: true}, child != nil && child.Source == game.SourceVanilla
}

// matchField fits a key that the block's field has a child for: the key's
// own, else the <number> child for a number or a date, else the <any> child.
func (w *walker) matchField(parent *Context, key string, class jomini.Class, _ bool) (match, bool) {
	child := parent.Field.findPatternChild(key, class)

	return match{category: CategoryField, field: child}, child != nil
}

// matchFieldOrCall does what matchField does, else what matchCall does.
func (w *walker) matchFieldOrCall(parent *Context, key string, class jomini.Class, block bool) (match, bool) {
	if fit, ok := w.matchField(parent, key, class, block); ok {
		return fit, true
	}

	return w.matchCall(parent, key, class, block)
}

// matchCall fits an identifier or a chain, but no event id, as a call through
// the caller of the block that holds the key.
func (w *walker) matchCall(parent *Context, _ string, class jomini.Class, _ bool) (match, bool) {
	return match{category: CategoryCall, caller: parent.FindCaller()}, class.IsIdentifier() && class != jomini.ClassEventID
}

// findCalledDefinition returns the scripted definition a key calls and the
// caller it is called through, trying the file's inline definitions first.
// A GUI widget type calls an empty one. It returns false if there is none.
func (w *walker) findCalledDefinition(caller, key string) (ScriptedDefinition, string, bool) {
	if scripted, ok := w.inline[key]; ok {
		callers := w.schema.Databases[scripted.Database].Callable.Callers
		if len(callers) > 0 && (caller == game.AnyCall || slices.Contains(callers, caller)) {
			return scripted, callers[0], true
		}
	}

	if caller != game.AnyCall {
		callee, ok := w.schema.lookupScripted(w.mods, caller, key)
		if !ok && caller == game.GUI.TagCalls && w.schema.WidgetTypes[key] {
			return ScriptedDefinition{}, caller, true
		}

		return callee, caller, ok && caller != ""
	}

	return w.schema.findScripted(w.mods, key)
}

// findModifierField returns the field of a key in a modifiers block: what
// the modifier type of that name takes, the game's before a mod's, else the
// key's child of the given field. It returns nil if none has one.
func (w *walker) findModifierField(holder *Field, key string) *Field {
	return cmp.Or(w.schema.ModifierFields[key], w.modifiers[key], holder.findChild(key))
}

// FindCaller returns the caller that a call in the block of a context
// resolves through: a keyword's, else the one the block's shape gives.
func (c *Context) FindCaller() string {
	if c.Keyword != nil && c.Keyword.Caller != "" {
		return c.Keyword.Caller
	}

	return game.Shapes[findShape(c)].Caller
}

// FindDeclaration returns how the dumps declare a command, among the
// declarations this context's block allows. It returns false if the block
// does not take the command.
func (c *Context) FindDeclaration(command *Command) (game.Declaration, bool) {
	position := slices.IndexFunc(command.Declared, c.Allows)
	if position < 0 {
		return "", false
	}

	return command.Declared[position], true
}

// FindArguments returns the keys a command takes in this context's block
// under the given declaration. A command declared as both an effect and a
// trigger takes the keys of both in a block that allows either.
func (c *Context) FindArguments(command *Command, declaration game.Declaration) *Field {
	if command.Union != nil && c.Allows(game.DeclarationEffect) && c.Allows(game.DeclarationTrigger) {
		return command.Union
	}

	return command.ArgumentsOf(declaration)
}

// Allows reports whether this context's block takes a command with the
// given declaration. An effect never fits in a block of triggers.
func (c *Context) Allows(declaration game.Declaration) bool {
	return slices.Contains(game.Shapes[findShape(c)].Allows, declaration) &&
		(declaration != game.DeclarationEffect || !c.holdsConditions())
}

// holdsConditions reports whether the statements in the block of a context
// are inside a trigger block. They are when the block is triggers, or sits
// below triggers and is no value block.
func (c *Context) holdsConditions() bool {
	return c.Shape == game.ShapeTriggers || c.Shape == game.ShapeTriggerArguments ||
		c.Conditional && c.Shape != game.ShapeValue
}

// resolveMatch sets the context of a keyed statement from the rule its key
// fits, or from its block's shape if none does. A keyword's block holds what
// its row says. An enclosing keyword's block is the block around it again,
// with the same field and shape. A list's block inside a value block, and a
// block the key is compared with, as the flag says, are value blocks.
func (w *walker) resolveMatch(context *Context, parent *Context, fit match, fits bool, keyword *game.Keyword, compared bool) {
	context.Field, context.Shape, context.Category = fit.field, fit.shape, fit.category
	if !fits {
		context.Shape = game.Shapes[findShape(parent)].Blocks
	}

	context.Command, context.Declaration, context.Callee = fit.command, fit.declaration, fit.callee
	keepsScope := fits && !fit.observed
	if keyword != nil {
		if keyword.Shape == game.ShapeEnclosing {
			context.Field, context.Shape = parent.Field, findEnclosing(parent)
		} else {
			context.Shape = cmp.Or(keyword.Shape, context.Shape)
		}

		context.Keyword = keyword
		context.Category = CategoryKeyword
		context.Preview = context.Preview || keyword.Preview
		keepsScope = keepsScope || !fits && keyword.Shape != ""
	}

	if fit.iterator != nil {
		context.Shape, context.Iterates = fit.iterator.Shape, true
	}

	// CK3 _script_values.info, "Lists": a list written in a formula holds the
	// formula's math, as add_gold = { every_child = { add = 1 } } does. A
	// block a key is compared with is a formula too.
	if fit.iterator != nil && findShape(parent) == game.ShapeValue || compared {
		context.Shape = game.ShapeValue
	}

	w.resolveScope(context, parent, fit, keepsScope)
}

// resolveScope sets the scope inside a block. A link, an iterator or a
// command that leads to a scope moves it there. Any other block keeps the
// scope it is written in where keepsScope is set and its shape is known,
// else has none.
func (w *walker) resolveScope(context *Context, parent *Context, fit match, keepsScope bool) {
	moves := fit.iterator != nil || fit.category == CategoryLink ||
		fit.command != nil && (fit.command.Target != "" || fit.command.Lists)
	switch {
	case moves:
		context.Scope = ""
		if w.schema.isScope(fit.target) {
			context.Scope = fit.target
		}

		context.Previous = slices.Concat([]string{parent.Scope}, parent.Previous)

	case keepsScope && context.Shape != "" && context.Shape != game.ShapeUnknown:
		context.Scope = parent.Scope

	default:
		context.Scope = ""
	}
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

	return c.Category == CategoryCommand && (command == nil || command.Target == "" && !command.Lists)
}

// resolveCallReferences adds the references a key makes, by what it fits:
// a name of the database the match names, the star of each command pattern
// that makes it, the names after a link's prefixes, or the name a call
// calls. A call to an unknown name refers to it in each database of its
// caller, or in the lists for an iterator.
func (w *walker) resolveCallReferences(context *Context, fit match, key string, span jomini.Span) {
	switch {
	case fit.database != "":
		context.References = append(context.References,
			Reference{Database: fit.database, Name: key, Span: span, Inferred: fit.inferred})

	case len(fit.patterns) > 0:
		lower := strings.ToLower(key)
		for _, pattern := range fit.patterns {
			start, end, _ := pattern.findStar(lower)
			context.References = append(context.References, Reference{
				Database: pattern.Database, Name: key[start:end],
				Span: jomini.Span{Start: span.Start + start, End: span.Start + end},
			})
		}

	case fit.category == CategoryLink:
		context.References = append(context.References, w.listPrefixed(key, span.Start)...)

	case fit.callee != nil && fit.callee.Database != "":
		context.References = append(context.References, Reference{Database: fit.callee.Database, Name: key, Span: span, Caller: fit.caller})

	case fit.category != CategoryCall || fit.callee != nil: // not a call to an unknown name

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

// resolveChain returns the scope a chain of links leads to. It returns "" if
// a link cannot be followed.
func (w *walker) resolveChain(chain string, parent *Context) string {
	scope := parent.Scope
	for _, link := range jomini.Links(chain, 0) {
		text := strings.ToLower(chain[link.Span.Start:link.Span.End])
		prefix, prefixed := strings.ToLower(chain[link.Prefix.Start:link.Prefix.End]), !link.Prefix.IsEmpty()
		switch {
		case text == game.ScopeLinks.Root:
			scope = parent.Root

		case text == game.ScopeLinks.This:
			scope = parent.Scope

		case text == game.ScopeLinks.Previous:
			scope = ""
			if len(parent.Previous) > 0 {
				scope = parent.Previous[0]
			}

		case prefixed && prefix == game.SavedScopes:
			label := chain[link.Name.Start:link.Name.End]
			scope = ""
			for _, saved := range slices.Backward(w.saved) {
				if saved.Name == label {
					scope = saved.Type
					break
				}
			}

		case prefixed:
			scope = ""
			if command := w.schema.Commands[prefix]; command != nil {
				scope = command.Target
			}

		default:
			scope, _ = w.schema.ResolveLink(text, scope)
		}

		if scope == "" {
			return ""
		}
	}

	return scope
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

	context.Definition = &Definition{Database: link, Name: label, Span: span, Statement: statement}
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

	target, isAlias := database.Row.Aliases[w.frames[level].label]
	span := w.tree.UnquotedSpan(current.Value)
	if label := w.tree.Text(span); isAlias && label != "" && current.ValueClass != jomini.ClassParameter {
		context.Definition = &Definition{
			Database: cmp.Or(target, database.Name), Name: label, Span: span, Statement: statement, Alias: true,
		}
		context.Defines = true
	}
}
