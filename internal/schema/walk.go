// walk.go goes through a file's tree once and calls a visitor at every statement, with what the schema knows about it.

package schema

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A Category is the color category of a key or a value in the editor. Each theme maps a category to a color.
type Category string

// The categories a key or a value can have.
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
	CategoryChoice     Category = "choice"     // one of the values a key takes, such as an event's type
)

// A Definition is a name that a statement defines in a database, with the facts that decide which definition of
// that name the game keeps.
type Definition struct {
	Database  string      `json:"database"`
	Name      string      `json:"name"`
	Span      jomini.Span `json:"span"`      // the name alone: no quotes, no entry mode
	Statement int32       `json:"statement"` // the statement that writes it; 0, the file, if the file names it
	Mode      string      `json:"mode"`      // an entry mode's prefix such as INJECT or REPLACE; "" if none
	Priority  int         `json:"priority"`  // CK3's override priority; 0 if none is written
	Local     bool        `json:"local"`     // visible only in its own file
	Alias     bool        `json:"alias"`     // the value of a key its database also names definitions by
}

// A Reference is a name that a key or a value refers to, in a database, together with the caller it was resolved
// through.
type Reference struct {
	Database string      `json:"database"`
	Name     string      `json:"name"`
	Span     jomini.Span `json:"span"`               // the name alone: no quotes, no prefix
	Caller   string      `json:"caller,omitempty"`   // the caller it is resolved through; "" if none
	Inferred bool        `json:"inferred,omitempty"` // it is never an error: it rests only on the game's own files, or its database needs no entry
}

// ToID returns the id the pages give a referenced name: its database and its name.
func (reference Reference) ToID() string { return reference.Database + ":" + reference.Name }

// A Saved is one scope that the definition saved before the current statement, with the type it holds.
type Saved struct {
	Name string `json:"name"`
	Type string `json:"type"` // "" if unknown
}

// A Context is everything the walk has decided at the statement it is on. The walk reuses one per depth, so a visitor
// copies whatever it keeps.
type Context struct {
	Field       *Field              // the field the key matched; nil if no source knows the key
	Database    string              // the database of the definition the statement is in; "" outside any
	Definition  *Definition         // the definition the statement is in, or the one it makes; nil outside any
	Defines     bool                // the statement itself defines Definition
	References  []Reference         // the names the key and the value refer to
	Scope       string              // the scope script runs in here; "" if unknown
	Root        string              // the definition's root scope; "" if unknown
	Previous    []string            // the scopes entered before this one, innermost first
	Saved       []Saved             // the scopes the definition saved before this statement
	Path        []string            // the keys that lead to the statement's block, from its definition or the top
	Label       string              // what the statement is written under: its key, or the tag of a block with no key
	Shape       string              // the shape of the block the statement holds, a game.Shapes key; "" if unknown
	Conditional bool                // the statement is inside a trigger block, so a command here must be a trigger
	Preview     bool                // the statement is inside a block that is shown in the tooltip and never run
	Category    Category            // the key's color category
	Command     *Command            // the command the key names; nil if it matched none
	Callee      *ScriptedDefinition // the scripted definition the key calls; nil for any other key
	Declaration string              // how the dumps declare that command in this block: a game.Declared constant
	ValueGroup  Category            // the value's color category
	Call        *jomini.Call        // the data call the value writes; nil if none
	Keyword     *game.Keyword       // the keyword the key is; nil for any other key
	Datacontext string              // a GUI block's data context type; "" if none
	Namespaces  []int32             // on the file's own context only: the event namespace declarations, in order
	Parent      *Context            // the context of the block that holds the statement, the file's own at the top
}

// A Visitor is told of each statement the walk enters and leaves. Statement 0 is the file itself.
type Visitor interface {
	// Enter is called when the walk reaches a statement, with its context. Returning false skips the statement's
	// children.
	Enter(tree *jomini.Tree, statement int32, context *Context) bool
	// Leave is called after the walk has finished the statement's children.
	Leave(tree *jomini.Tree, statement int32, context *Context)
}

// A walker is one descent over one file. It keeps what the descent carries from one statement to the next.
type walker struct {
	schema     *Schema
	row        *game.Game
	tree       *jomini.Tree
	file       files.File
	visitor    Visitor
	restricted bool // the walk enters only the statements that cover the offset
	offset     int  // the byte offset the walk is restricted to
	rel        string
	stem       string    // the file's name without its folder and extension
	database   *Database // the database of the file's folder; nil for a file of no folder
	fileType   game.FileType
	nestings   []game.Nesting
	declaring  map[string]string             // by key: the database whose definition it declares inline
	inline     map[string]ScriptedDefinition // each name the file declared for itself so far
	mods       map[string]ScriptedDefinition // the mods' scripted definitions when the walk began
	modifiers  map[string]*Field             // what the mods' modifier types take when the walk began
	namespaces map[string]bool               // the event namespaces the mods declared when the walk began
	frames     []frame                       // frames[0] is the file's; frames[n] is that of the statements n levels down
	saved      []Saved
}

// A frame is what the walk keeps for the statements at one level of depth.
type frame struct {
	context *Context
	label   string      // what the statement last read at this level is written under
	span    jomini.Span // where that label is written
	path    []string    // the keys that lead to its children, from the enclosing definition or the top
	wraps   bool        // its children are written as definitions
}

// A match is what a key fits under one rule of its block's shape.
type match struct {
	category    Category
	field       *Field // the field the keys inside its block come from
	shape       string // the block's shape where the rule decides it; "" for a claim, which findMatch fills
	observed    bool   // the key fits only because vanilla writes it, so the scope inside is unknown
	command     *Command
	declaration string              // what declares the command that fit
	callee      *ScriptedDefinition // the scripted definition that a known call names
	caller      string
	iterator    *game.Keyword    // the list iterator that the key starts
	target      string           // the scope that the command or the link leads to
	database    string           // the database the key itself names
	inferred    bool             // only the game's own files say the key names that database, so it never accuses
	patterns    []CommandPattern // the command patterns that make the key
}

// Walk goes through a file's tree once and calls the visitor at every statement, with the field, scope, shape,
// definition and references the schema finds there.
func (schema *Schema) Walk(tree *jomini.Tree, file files.File, visitor Visitor) {
	schema.newWalker(tree, file, visitor).walkFile()
}

// WalkTo is Walk restricted to the statements that cover the given byte offset.
func (schema *Schema) WalkTo(tree *jomini.Tree, file files.File, offset int, visitor Visitor) {
	walker := schema.newWalker(tree, file, visitor)
	walker.restricted, walker.offset = true, offset
	walker.walkFile()
}

// newWalker returns the walker for one file. If the file's database names its one definition by the file's name, the
// file's own context holds that definition. Only an events file declares definitions inline.
func (schema *Schema) newWalker(tree *jomini.Tree, file files.File, visitor Visitor) *walker {
	row := game.LookupGame(schema.Game)
	if row == nil {
		row = &game.Game{}
	}
	mods := schema.mods.Load()
	if mods == nil {
		mods = &modScripted{}
	}
	walker := &walker{
		schema: schema, row: row, tree: tree, file: file, visitor: visitor, inline: map[string]ScriptedDefinition{},
		mods: mods.all, modifiers: mods.modifiers, namespaces: mods.namespaces,
	}
	_, walker.rel = row.SplitStage(file.Rel)
	walker.stem = strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
	walker.fileType = game.FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = schema.FindDatabase(file.Rel)
	}
	walker.nestings = row.FindNestings(walker.rel)
	if database := walker.database; database != nil && slices.Contains(schema.Setups, database.Row.Folder) {
		walker.nestings = row.ListSetupNestings(database.Row.Folder)
	}
	root := &Context{Shape: game.ShapeUnknown}
	if database := walker.database; database != nil {
		if database.Row.Contents == game.ContentsEvents {
			root.Namespaces = listNamespaces(tree)
			walker.declaring = schema.InlineDatabases
		}
		if database.Row.NamedBy == "file" && len(tree.Statements) > 1 {
			walker.resolveDefinition(root, Definition{Database: database.Name, Name: walker.stem, Local: database.Callable.Local})
		}
	}
	walker.frames = []frame{{context: root, wraps: true}}
	return walker
}

// listNamespaces returns the top statements of a tree that declare an event namespace, in document order.
func listNamespaces(tree *jomini.Tree) []int32 {
	var statements []int32
	for statement := range tree.Children(0) {
		if tree.Text(tree.Statements[statement].Key) == game.EventNamespaceKey {
			statements = append(statements, statement)
		}
	}
	return statements
}

// walkFile walks the top statements of the file. A file that is one definition is entered as statement 0 first, with
// the definition's context, and left after them. A walk to an offset always enters it.
func (walker *walker) walkFile() {
	root := walker.frames[0].context
	if root.Definition == nil {
		walker.walkChildren(0, 1, false)
	} else if walker.visitor.Enter(walker.tree, 0, root) {
		walker.walkChildren(0, 1, false)
		walker.visitor.Leave(walker.tree, 0, root)
	}
}

// walkChildren walks the children of a statement, one level down. A silent walk tells the visitor nothing.
func (walker *walker) walkChildren(parent int32, level int, silent bool) {
	for len(walker.frames) <= level {
		walker.frames = append(walker.frames, frame{context: &Context{}})
	}
	starts := walker.walkRunOrder(parent, level, silent)
	pending := ""
	for child := range walker.tree.Children(parent) {
		statement := walker.tree.Statements[child]
		offered := !silent && (!walker.restricted || statement.Span.Contains(walker.offset))
		// a restricted walk still resolves the statements before the offset, to learn what they saved
		if offered || !walker.restricted || statement.Span.End <= walker.offset {
			if starts != nil {
				walker.saved = starts[child]
			}
			walker.walkStatement(child, level, pending, offered)
		}
		pending = ""
		if text := walker.tree.Text(statement.Value); statement.IsListed() && walker.isLead(text) {
			pending = text
		}
	}
}

// walkRunOrder walks a definition's children silently in the order they run, and returns the scopes saved before each.
// It returns nil if the definition has no run order or the walk is silent.
func (walker *walker) walkRunOrder(parent int32, level int, silent bool) map[int32][]Saved {
	context := walker.frames[level-1].context
	database := walker.schema.Databases[context.Database]
	if silent || !context.Defines || context.Category != CategoryDefinition || database == nil || len(database.Row.RunOrder) == 0 {
		return nil
	}
	rank := func(child int32) int {
		label, _ := walker.findLabel(walker.tree.Statements[child])
		if position := slices.Index(database.Row.RunOrder, label); position >= 0 {
			return position
		}
		return len(database.Row.RunOrder)
	}
	children := slices.Collect(walker.tree.Children(parent))
	slices.SortStableFunc(children, func(first, second int32) int { return cmp.Compare(rank(first), rank(second)) })
	starts, restricted := map[int32][]Saved{}, walker.restricted
	walker.restricted = false
	for _, child := range children {
		starts[child] = slices.Clip(walker.saved)
		walker.walkStatement(child, level, "", false)
	}
	walker.restricted = restricted
	return starts
}

// walkStatement resolves one statement, offers it to the visitor if the flag says so, and walks its block unless the
// visitor declines.
func (walker *walker) walkStatement(statement int32, level int, pending string, offered bool) {
	context := walker.frames[level].context
	walker.resolve(context, statement, level, pending)
	block := walker.tree.Statements[statement].IsBlock()
	if !offered {
		if block {
			walker.walkChildren(statement, level+1, true)
		}
		return
	}
	if !walker.visitor.Enter(walker.tree, statement, context) {
		return
	}
	if block {
		walker.walkChildren(statement, level+1, false)
	}
	walker.visitor.Leave(walker.tree, statement, context)
}

// resolve fills the context of a statement from the context of the block that holds it.
func (walker *walker) resolve(context *Context, statement int32, level int, pending string) {
	parent := walker.frames[level-1].context
	*context = Context{
		Parent: parent, Database: parent.Database, Definition: parent.Definition, Scope: parent.Scope, Root: parent.Root,
		Previous: parent.Previous, Saved: walker.saved,
		Conditional: parent.holdsConditions(), Preview: parent.Preview,
		Datacontext: parent.Datacontext, References: context.References[:0],
	}
	label, span := walker.findLabel(walker.tree.Statements[statement])
	walker.frames[level].label, walker.frames[level].span, walker.frames[level].wraps = label, span, false
	definition, defines := walker.findDefinition(statement, level, pending)
	if defines {
		walker.resolveDefinition(context, definition)
	} else {
		walker.resolveStatement(context, statement, level, pending)
	}
	context.Path, context.Label = walker.frames[level-1].path, label
	if walker.tree.Statements[statement].IsBlock() {
		keys := walker.frames[level-1].path
		if label != "" {
			keys = slices.Concat(keys, []string{label})
		}
		// a definition of the file's own database starts the keys anew
		if defines && walker.database != nil && definition.Database == walker.database.Name {
			keys = nil
		}
		walker.frames[level].path = keys
	}
	// a GUI block's datacontext key sets its data context, unknown if the dumps do not resolve the call
	if walker.file.Format == files.FormatGUI && walker.tree.Statements[statement].IsBlock() {
		if child, ok := walker.tree.FindChild(statement, game.GUI.ContextKey); ok {
			context.Datacontext = ""
			if call, ok := walker.tree.ParseBracketed(walker.tree.Statements[child].Value); ok {
				context.Datacontext = walker.schema.ResolveDatacontext(call, parent.Datacontext)
			}
		}
	}
	walker.resolveValue(context, statement, level)
}

// resolveStatement fills the context of a statement that defines nothing.
func (walker *walker) resolveStatement(context *Context, statement int32, level int, pending string) {
	parent := walker.frames[level-1].context
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	switch {
	case label != "":
		walker.frames[level].wraps = walker.frames[level-1].wraps && walker.database != nil &&
			slices.Contains(walker.database.Row.WrapperKeys, label)
		walker.resolveKey(context, statement, level)
		walker.resolveLead(context, pending, label, span)
	case current.IsBlock():
		context.Shape, context.Field, context.Scope = parent.Shape, parent.Field, parent.Scope
	case current.IsListed():
		context.Field = parent.Field
	}
	if database := walker.database; level == 1 && database != nil && database.Row.NamedBy == "nothing" {
		context.Database, context.Field, context.Shape = database.Name, database.Definition, walker.findDefinitionShape(database)
	}
}

// resolveLead resolves the references of a call written as a lead and a label, such as `blockoverride "label"` in a GUI
// file. The label refers to a name in each database that the lead calls.
func (walker *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range walker.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}
	if lead != "" && len(walker.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// findLabel returns the label of a statement, its key or else the tag of a block that has no key, and where it is
// written. It returns an empty label and span for a value written alone.
func (walker *walker) findLabel(current jomini.Statement) (string, jomini.Span) {
	var span jomini.Span
	switch {
	case !current.Key.IsEmpty():
		span = current.Key
	case !current.Tag.IsEmpty() && current.IsBlock():
		span = current.Tag
	default:
		return "", jomini.Span{}
	}
	span = walker.tree.UnquotedSpan(span)
	return walker.tree.Text(span), span
}

// resolveDefinition fills the context of the statement that makes a definition.
func (walker *walker) resolveDefinition(context *Context, definition Definition) {
	database := walker.schema.Databases[definition.Database]
	*context = Context{
		Parent: context.Parent, Database: definition.Database, Definition: &definition, Defines: true,
		Category: CategoryDefinition, Shape: walker.findDefinitionShape(database), References: context.References[:0],
	}
	if database != nil {
		context.Field = database.Definition
	}
	context.Root = walker.findRootScope(database, definition)
	context.Scope = context.Root
	walker.saved = nil
}

// findDefinitionShape returns the shape of a definition's block in a database, fields by default.
func (walker *walker) findDefinitionShape(database *Database) string {
	if database == nil {
		return cmp.Or(walker.fileType.Shape, game.ShapeFields)
	}
	return cmp.Or(database.Callable.Shape, database.Row.Shape, walker.fileType.Shape, game.ShapeFields)
}

// findRootScope returns the scope a definition's script starts in. It is the scope named by the definition's root key,
// else its event type's scope, or its on_action's scope in the dumps. It returns "" if nothing says, or if the root key
// names no scope.
func (walker *walker) findRootScope(database *Database, definition Definition) string {
	if database == nil {
		return ""
	}
	if value, ok := walker.tree.FindChildValue(definition.Statement, database.Row.RootKey); ok {
		if walker.schema.isScope(value) {
			return value
		}
		return ""
	}
	switch database.Row.Contents {
	case game.ContentsEvents:
		events := walker.row.Events
		eventType, _ := walker.tree.FindChildValue(definition.Statement, events.TypeKey)
		return cmp.Or(strings.TrimSuffix(eventType, events.TypeSuffix), events.DefaultScope)
	case game.ContentsOnActions:
		return walker.schema.OnActions[definition.Name].Scope
	}
	return ""
}

// resolveKey fills the context of a keyed statement from the first rule that its key fits in the block that holds it.
// A keyword of the language adjusts what the rule gave, and is never a call. A key written as a command's name with an
// argument in parentheses is read as the name, and its argument makes its own references.
func (walker *walker) resolveKey(context *Context, statement int32, level int) {
	parent := walker.frames[level-1].context
	key, span := walker.frames[level].label, walker.frames[level].span
	// a key with a parameter is unknown until the call is made, and so are its block's shape and scope
	if jomini.IsParameter(key) {
		context.Category, context.Shape, context.Scope = CategoryParameter, game.ShapeUnknown, ""
		return
	}
	step, command, isCall := walker.schema.findStatementCall(walker.tree, statement)
	if isCall {
		key, span = step.Text, step.Span
	}
	var keyword *game.Keyword
	if row, ok := walker.row.LookupKeyword(findShape(parent), walker.frames[level-1].label, key, findKeywordKey(parent)); ok {
		keyword = new(row)
	}
	fit, fits := walker.findMatch(parent, key, walker.tree.Statements[statement].IsBlock())
	// a case of a switch is a value of the switch's trigger, so no rule of the block explains its key
	if keyword != nil && strings.HasSuffix(keyword.Key, game.AnyKey) {
		fit, fits = match{}, false
	}
	// a link that requires data takes it in parentheses too, as relations(scope:overlord) does, though its name
	// written alone is no link
	if isCall && fit.category == CategoryCall && fit.callee == nil && walker.schema.IsLink(key) {
		fit, fits = match{category: CategoryLink, field: parent.Field, shape: findEnclosing(parent)}, true
	}
	// a key that starts an iterator and is no keyword runs its block in the scope of the list's item
	iterator, isIterator := game.MatchListIterator(key)
	unknownCall := fit.category == CategoryCall && fit.callee == nil
	if isIterator && keyword == nil && (fit.command != nil || unknownCall && walker.schema.Lists != "") {
		fit.iterator = new(iterator)
	}
	switch {
	case fit.category == CategoryLink:
		head, _ := splitLinkHead(key)
		fit.command, fit.declaration = walker.schema.Commands[head], game.DeclaredLink
		fit.target = walker.resolveChain(key, parent)
	case fit.command != nil:
		fit.target = fit.command.Target
	}
	walker.resolveMatch(context, parent, fit, fits, keyword)
	// a conditional keyword such as limit is read in the scope of the command that holds it, any other argument in a
	// scope the command makes
	if isArgument(parent, key) && (keyword == nil || keyword.Class != "conditional") {
		context.Scope = ""
	}
	if keyword == nil {
		walker.resolveCallReferences(context, fit, key, span)
	}
	if isCall {
		context.References = append(context.References,
			walker.listArgumentReferences(command, parent.FindArguments(command, context.Declaration), step.Arguments[0])...)
	}
	// an operator's value is a number or a script value, so it takes no field the game's files taught
	if keyword != nil && keyword.Class != "math" && context.Field == nil {
		context.Field = parent.Field.findPatternChild(key)
	}
	walker.resolveSaved(context, statement, level)
	walker.resolveAlias(context, statement, level)
}

// findShape returns the shape of the block that a context holds, game.ShapeUnknown by default.
func findShape(context *Context) string {
	return cmp.Or(context.Shape, game.ShapeUnknown)
}

// findKeywordKey returns the Key of the keyword row that a context is, or "" if the context is no keyword.
func findKeywordKey(context *Context) string {
	if context.Keyword == nil {
		return ""
	}
	return context.Keyword.Key
}

// findEnclosing returns the shape of the block that a link or an "enclosing" keyword written in the block of a context
// holds. It is the Encloses of the context's shape, else the context's own shape.
func findEnclosing(context *Context) string {
	return cmp.Or(game.Shapes[findShape(context)].Encloses, findShape(context))
}

// findMatch returns what the key fits under the first rule of the parent block's shape that it fits, and whether one
// does. If the rule gives no shape, the match takes its field's stated shape, else unknown. A field's key in a block
// whose keys name definitions also refers to one. The given flag is whether the statement of the key is a block.
func (walker *walker) findMatch(parent *Context, key string, block bool) (match, bool) {
	for _, rule := range game.Shapes[findShape(parent)].Rules {
		if fit, ok := walker.matchRule(rule, parent, key, block); ok {
			switch {
			case fit.shape != "": // the rule decided it
			case fit.field != nil && fit.field.Shape.IsStated():
				fit.shape = fit.field.Shape.Value
			default:
				fit.shape = game.ShapeUnknown
			}
			// a key the dumps declare is an argument, as stress_impact's base is, and none of the names
			declared := fit.field != nil && fit.field.Source == game.SourceDumps
			if fit.category == CategoryField && fit.database == "" && parent.Field != nil && !declared {
				fit.database, fit.inferred = parent.Field.Keys.Value, !parent.Field.Keys.Source.CanAccuse()
			}
			return fit, true
		}
	}
	return match{}, false
}

// FindCaller returns the caller that a call in the block of a context resolves through: a keyword's, else the one the
// block's shape gives.
func (context *Context) FindCaller() string {
	if context.Keyword != nil && context.Keyword.Caller != "" {
		return context.Keyword.Caller
	}
	return game.Shapes[findShape(context)].Caller
}

// HoldsArguments reports whether the block of a context holds a command's arguments. It does when the command leads
// to no scope and iterates no list, so what is written in the block is read in the scope the command is written in.
func (context *Context) HoldsArguments() bool {
	command := context.Command
	return context.Category == CategoryCommand && (command == nil || command.Target == "" && !command.Lists)
}

// isArgument reports whether a key in the block of a command is one of the command's arguments. The block must hold
// arguments, and the command's arguments must hold the key.
func isArgument(parent *Context, key string) bool {
	return parent.HoldsArguments() && parent.Field.findChild(key) != nil
}

// matchRule returns what a key fits under one rule of its block's shape, and whether it fits. The given flag is
// whether the statement of the key is a block.
func (walker *walker) matchRule(rule string, parent *Context, key string, block bool) (match, bool) {
	holder := parent.Field
	switch rule {
	case "known-call":
		callee, caller, ok := walker.findCalledDefinition(parent.FindCaller(), key)
		field := callee.Parameters
		if database := walker.schema.Databases[callee.Database]; field == nil && database != nil {
			field = database.Definition
		}
		return match{category: CategoryCall, field: field, callee: &callee, caller: caller}, ok
	case "declared":
		child := holder.findNumberChild(key)
		return match{category: CategoryField, field: child}, child != nil &&
			child.Source != game.SourceNone && child.Source != game.SourceVanilla
	case "documented":
		database := walker.schema.Databases[parent.Database]
		return match{category: CategoryField, field: holder.findChild(key)}, database != nil && database.Documented[key]
	case "command":
		return walker.matchCommand(parent, key)
	case "operator":
		return match{shape: game.ShapeArguments}, game.IsOperator(key)
	case "link", "scope-link":
		// scope-link leaves out a plain name with a plain value, which is an argument or a call
		plain := rule == "scope-link" && !block && !strings.ContainsAny(key, ":.")
		return match{category: CategoryLink, field: holder, shape: findEnclosing(parent)}, !plain && walker.schema.isKeyLink(key)
	case "modifier":
		// a modifier the dumps declare is named by its type, but nothing says that every one has a definition
		_, declared := walker.schema.Modifiers[key]
		return match{
			category: CategoryField, field: walker.findModifierField(holder, key), database: walker.schema.ModifierTypes,
			inferred: true,
		}, declared
	case "modifier-type":
		return match{
			category: CategoryReference, field: walker.findModifierField(holder, key), database: walker.schema.ModifierTypes,
		}, walker.schema.ModifierTypes != "" && jomini.IsIdentifier(key)
	case "observed":
		child := holder.findNumberChild(key)
		return match{category: CategoryField, field: child, observed: true}, child != nil &&
			child.Source == game.SourceVanilla
	case "field":
		child := holder.findPatternChild(key)
		return match{category: CategoryField, field: child}, child != nil
	case "field-or-call":
		if child := holder.findPatternChild(key); child != nil {
			return match{category: CategoryField, field: child}, true
		}
		return walker.matchCall(parent, key)
	case "call":
		return walker.matchCall(parent, key)
	}
	return match{}, false
}

// findModifierField returns the field for a key in a modifiers block: what the modifier type of that name takes, the
// game's before a mod's, else the child of the block's field for the key. It returns nil if none has one.
func (walker *walker) findModifierField(holder *Field, key string) *Field {
	return cmp.Or(walker.schema.ModifierFields[key], walker.modifiers[key], holder.findChild(key))
}

// matchCall returns a call, with the caller of the block that holds the key. It returns false unless the key is an
// identifier and not an event id.
func (walker *walker) matchCall(parent *Context, key string) (match, bool) {
	return match{category: CategoryCall, caller: parent.FindCaller()}, jomini.IsIdentifier(key) && !jomini.IsEventID(key)
}

// findCalledDefinition returns the scripted definition a key calls and the caller it is called through, trying the
// file's inline definitions first. A GUI widget type calls an empty one. It returns false if none.
func (walker *walker) findCalledDefinition(caller, key string) (ScriptedDefinition, string, bool) {
	if scripted, ok := walker.inline[key]; ok {
		callers := walker.schema.Databases[scripted.Database].Callable.Callers
		if len(callers) > 0 && (caller == game.AnyCall || slices.Contains(callers, caller)) {
			return scripted, callers[0], true
		}
	}
	if caller != game.AnyCall {
		callee, ok := walker.schema.lookupScripted(walker.mods, caller, key)
		if !ok && caller == game.GUI.TagCalls && walker.schema.WidgetTypes[key] {
			return ScriptedDefinition{}, caller, true
		}
		return callee, caller, ok && caller != ""
	}
	return walker.schema.findScripted(walker.mods, key)
}

// matchCommand returns the command a key names, from the dumps or else from a pattern, with the shape of its block. It
// returns false if no command matches or the block that holds the key does not take it.
func (walker *walker) matchCommand(parent *Context, key string) (match, bool) {
	command, ok := walker.schema.FindCommand(key)
	fit := match{category: CategoryCommand, command: command, patterns: walker.schema.findPatterns(key)}
	if !ok && len(fit.patterns) > 0 {
		fit.command = walker.schema.Commands[fit.patterns[0].Commands[0]]
		// a list iterator a pattern makes takes the plain iterator of its scope, as any_character, for its arguments
		if iterator, listed := game.MatchListIterator(key); listed {
			fit.command = cmp.Or(walker.schema.Commands[strings.TrimSuffix(iterator.Key, "*")+fit.command.Target], fit.command)
		}
	}
	if fit.command == nil {
		return match{}, false
	}
	fit.declaration, ok = parent.FindDeclaration(fit.command)
	fit.field = parent.FindArguments(fit.command, fit.declaration)
	switch {
	case fit.field != nil && fit.field.Shape.IsStated():
		fit.shape = fit.field.Shape.Value
	case fit.field != nil && fit.field == fit.command.Union:
		fit.shape = game.ShapeUnknown
	case fit.declaration == game.DeclaredTrigger:
		fit.shape = game.ShapeTriggerArguments
	default:
		fit.shape = game.ShapeEffectArguments
	}
	return fit, ok
}

// resolveMatch sets the context of a keyed statement from the rule its key fits, or from its block's shape if none
// does. A keyword adjusts what the rule gave.
func (walker *walker) resolveMatch(context *Context, parent *Context, fit match, fits bool, keyword *game.Keyword) {
	context.Field, context.Shape, context.Category = fit.field, fit.shape, fit.category
	if !fits {
		context.Shape = game.Shapes[findShape(parent)].Blocks
	}
	context.Command, context.Declaration, context.Callee = fit.command, fit.declaration, fit.callee
	keepsScope := fits && !fit.observed
	if keyword != nil {
		if keyword.Shape == "enclosing" {
			context.Shape = findEnclosing(parent)
		} else {
			context.Shape = cmp.Or(keyword.Shape, context.Shape)
		}
		context.Keyword = keyword
		context.Category = CategoryKeyword
		context.Preview = context.Preview || keyword.Preview
		keepsScope = keepsScope || !fits && keyword.Shape != ""
	}
	if fit.iterator != nil {
		context.Shape = fit.iterator.Shape
	}
	walker.resolveScope(context, parent, fit, keepsScope)
}

// resolveScope sets the scope inside a block. A link, an iterator or a command that leads to a scope moves it there.
// Any other block keeps the scope it is written in if the given flag is set and its shape is known, else has none.
func (walker *walker) resolveScope(context *Context, parent *Context, fit match, keepsScope bool) {
	moves := fit.iterator != nil || fit.category == CategoryLink ||
		fit.command != nil && (fit.command.Target != "" || fit.command.Lists)
	switch {
	case moves:
		context.Scope = ""
		if walker.schema.isScope(fit.target) {
			context.Scope = fit.target
		}
		context.Previous = slices.Concat([]string{parent.Scope}, parent.Previous)
	case keepsScope && context.Shape != "" && context.Shape != game.ShapeUnknown:
		context.Scope = parent.Scope
	default:
		context.Scope = ""
	}
}

// resolveCallReferences adds the references a key makes.
func (walker *walker) resolveCallReferences(context *Context, fit match, key string, span jomini.Span) {
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
		context.References = append(context.References, walker.listPrefixed(key, span.Start)...)
	case fit.callee != nil && fit.callee.Database != "":
		context.References = append(context.References, Reference{Database: fit.callee.Database, Name: key, Span: span, Caller: fit.caller})
	case fit.category != CategoryCall || fit.callee != nil: // not a call to an unknown name
	case fit.iterator != nil:
		prefix := strings.TrimSuffix(fit.iterator.Key, "*")
		context.References = append(context.References,
			Reference{Database: walker.schema.Lists, Name: strings.TrimPrefix(key, prefix), Span: span})
	case fit.caller == game.AnyCall:
		context.References = append(context.References, Reference{Database: game.AnyCall, Name: key, Span: span, Caller: game.AnyCall})
	default:
		for _, database := range walker.schema.Callees[fit.caller] {
			context.References = append(context.References, Reference{Database: database, Name: key, Span: span, Caller: fit.caller})
		}
	}
}

// resolveChain returns the scope a chain of links leads to, "" if a link cannot be followed.
func (walker *walker) resolveChain(chain string, parent *Context) string {
	scope := parent.Scope
	for _, rawLink := range jomini.SplitChain(chain) {
		link := strings.ToLower(rawLink)
		prefix, _, prefixed := jomini.SplitPrefixed(link)
		switch {
		case link == game.ScopeLinks.Root:
			scope = parent.Root
		case link == game.ScopeLinks.This:
			scope = parent.Scope
		case link == game.ScopeLinks.Previous:
			scope = ""
			if len(parent.Previous) > 0 {
				scope = parent.Previous[0]
			}
		case prefixed && prefix == game.SavedScopes:
			_, label, _ := jomini.SplitPrefixed(rawLink)
			scope = ""
			for _, saved := range slices.Backward(walker.saved) {
				if saved.Name == label {
					scope = saved.Type
					break
				}
			}
		case prefixed:
			scope = ""
			if command := walker.schema.Commands[prefix]; command != nil {
				scope = command.Target
			}
		default:
			scope, _ = walker.schema.ResolveLink(link, scope)
		}
		if scope == "" {
			return ""
		}
	}
	return scope
}

// resolveSaved makes a name that a statement saves for one of game.Links a definition, and one it reads back a
// reference; a saved scope also joins the definition's saved scopes. A name with a parameter is skipped, and a save in
// the block of a command that makes no scope has an unknown type.
func (walker *walker) resolveSaved(context *Context, statement int32, level int) {
	current := walker.tree.Statements[statement]
	span := walker.tree.UnquotedSpan(current.Value)
	label := walker.tree.Text(span)
	if current.IsBlock() || label == "" || jomini.IsParameter(label) || strings.HasPrefix(label, "[") {
		return
	}
	key, holder, parent := walker.frames[level].label, walker.frames[level-1].label, walker.frames[level-1].context
	if link, _, ok := findLinked(game.LinkUsers, holder, key); ok {
		context.References = append(context.References, Reference{Database: link, Name: label, Span: span})
		return
	}
	link, inBlock, ok := findLinked(game.LinkMakers, holder, key)
	scope := parent.Scope
	if inBlock || parent.HoldsArguments() {
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
		walker.saved = append(walker.saved, Saved{Name: label, Type: scope})
	}
}

// findLinked looks a key up in a table shaped like game.LinkMakers, whose rows are a command, such as save_scope_as, or
// a key inside a command's block, written as command/key. The holder is the command whose block holds the key. It
// returns the row's link, whether the key matched inside the holder's block, and false if the table has neither row.
func findLinked(table map[string]string, holder, key string) (link string, inBlock, ok bool) {
	if link, ok := table[key]; ok {
		return link, false, true
	}
	link, ok = table[strings.ToLower(holder)+"/"+key]
	return link, true, ok
}

// resolveAlias makes the value that a statement writes at an alias key of its database a definition, a second name of
// the definition that the statement sits directly in. A value with a parameter names nothing yet.
func (walker *walker) resolveAlias(context *Context, statement int32, level int) {
	current, parent := walker.tree.Statements[statement], walker.frames[level-1].context
	database := walker.schema.Databases[parent.Database]
	if database == nil || !parent.Defines || parent.Category != CategoryDefinition || current.IsBlock() ||
		current.Value.IsEmpty() {
		return
	}
	target, isAlias := database.Row.Aliases[walker.frames[level].label]
	span := walker.tree.UnquotedSpan(current.Value)
	if label := walker.tree.Text(span); isAlias && label != "" && !jomini.IsParameter(label) {
		context.Definition = &Definition{
			Database: cmp.Or(target, database.Name), Name: label, Span: span, Statement: statement, Alias: true,
		}
		context.Defines = true
	}
}

// resolveValue resolves what the value of a scalar statement refers to.
func (walker *walker) resolveValue(context *Context, statement int32, level int) {
	current := walker.tree.Statements[statement]
	if current.IsBlock() || current.Value.IsEmpty() {
		return
	}
	span := walker.tree.UnquotedSpan(current.Value)
	text := walker.tree.Text(span)
	before := len(context.References)
	switch {
	case jomini.IsParameter(text):
		context.ValueGroup = CategoryParameter
		return
	case jomini.IsConstant(text):
		context.ValueGroup = CategoryConstant
		if !strings.HasPrefix(text, "@[") {
			context.References = append(context.References, Reference{Database: game.Constant, Name: text, Span: span})
		}
		// an expression such as @[…] refers to the constants it uses
		for _, token := range jomini.ListConstants(text, span.Start) {
			context.References = append(context.References,
				Reference{Database: game.Constant, Name: "@" + walker.tree.Text(token), Span: token})
		}
		return
	default:
		if call, ok := walker.tree.ParseBracketed(current.Value); ok {
			context.ValueGroup, context.Call = CategoryCall, new(call)
			context.References = append(context.References, walker.schema.ListCallReferences(call, context.Datacontext)...)
			return
		}
		walker.resolveValueReferences(context, statement, level, span)
	}
	switch {
	case len(context.References) > before:
		context.ValueGroup = CategoryReference
	case walker.isChoice(context, text):
		context.ValueGroup = CategoryChoice
	}
}

// isChoice reports whether a value is one of the choices that the field of a context takes.
func (walker *walker) isChoice(context *Context, text string) bool {
	field := context.Field
	return field != nil && field.Value.Value.Type == ValueChoice && walker.schema.MatchValue(field.Value.Value, text)
}

// resolveValueReferences resolves the references a plain value makes. A value with a parameter or a bracket makes
// none, and so does a name the statement saves or reads back. A math keyword's value that is a command's name with an
// argument in parentheses makes the references of its argument.
func (walker *walker) resolveValueReferences(context *Context, statement int32, level int, span jomini.Span) {
	text := walker.tree.Text(span)
	if text == "" || strings.ContainsAny(text, "[$") || isSavedName(context, span) {
		return
	}
	// a math keyword's value such as "opinion(scope:ruler)" stands for the command's number: its argument makes the
	// references
	if step, command, ok := walker.schema.findKeyCall(text, span.Start); ok && context.isMath() {
		context.References = append(context.References, walker.listArgumentReferences(command, command.ArgumentsOf(""), step.Arguments[0])...)
		return
	}
	claim, databases := walker.listClaimed(context, text)
	placed, folder := "", ""
	if database, valueFolder, ok := walker.findValueDatabase(context, level, text); ok {
		databases, placed, folder = append(databases, database), database, valueFolder
	}
	if database, valueFolder, ok := walker.findCoatDatabase(statement, level, text); ok && placed == "" {
		databases, placed, folder = append(databases, database), database, valueFolder
	}
	// a value with a space is literal text, since a message key has none
	database := cmp.Or(findTextDatabase(context), game.Shapes[findShape(context)].Values)
	if database != "" && placed == "" && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}
	// a value shaped like an event id names an event where its field claims events, and where its field claims nothing
	// and an events file declares its namespace
	if walker.schema.Events != "" && walker.file.Format == files.FormatScript && jomini.IsEventID(text) &&
		(claim == walker.schema.Events || claim == "" && walker.hasNamespace(text)) {
		databases = append(databases, walker.schema.Events)
	}
	// the claim is inferred if it rests on the game's own files and no other rule makes it
	inferred := claim != "" && !context.Field.Value.Source.CanAccuse() && !slices.Contains(databases[1:], claim)
	stem := game.LocStems[strings.ToLower(walker.frames[level-1].label)+"/"+walker.frames[level].label]
	for position, database := range databases {
		if !slices.Contains(databases[:position], database) {
			name := text
			switch {
			case database == placed && folder != "" && !strings.Contains(text, "/"):
				name = folder + "/" + text
			case database == game.Localization && stem != "":
				name = fmt.Sprintf(stem, text)
			}
			context.References = append(context.References, Reference{
				Database: database, Name: name, Span: span, Inferred: position == 0 && inferred || walker.allowsUndefined(database),
			})
		}
	}
	if value, ok := walker.findScriptValue(context, span); ok {
		context.References = append(context.References, value)
	}
	context.References = append(context.References, walker.listPrefixed(text, span.Start)...)
}

// isSavedName reports whether the value at a span is a name of one of game.Links that its statement saves or reads
// back, such as a flag. Such a name names nothing else, even where it is shaped like an event id.
func isSavedName(context *Context, span jomini.Span) bool {
	if context.Defines && context.Definition.Span == span && slices.Contains(game.Links, context.Definition.Database) {
		return true
	}
	return slices.ContainsFunc(context.References, func(reference Reference) bool {
		return reference.Span == span && slices.Contains(game.Links, reference.Database)
	})
}

// isMath reports whether the statement's key is a math keyword, such as value or add, whose value is a number.
func (context *Context) isMath() bool {
	return context.Keyword != nil && context.Keyword.Class == "math"
}

// listArgumentReferences returns the references that the argument in parentheses after a command's name makes. It
// fills the command's ArgumentKey, one of the given keys the command takes. If that key takes a name from a database,
// the argument is such a name, unless it is a link. Any other argument is read as a chain of links.
func (walker *walker) listArgumentReferences(command *Command, arguments *Field, argument jomini.Argument) []Reference {
	field := arguments.findChild(command.ArgumentKey)
	if field != nil && field.Value.Value.Type == ValueReference && field.Value.Value.Database != "" &&
		!walker.schema.IsLink(argument.Text) {
		return []Reference{{
			Database: field.Value.Value.Database, Name: argument.Text, Span: argument.Span,
			Inferred: !field.Value.Source.CanAccuse(),
		}}
	}
	return walker.listPrefixed(argument.Text, argument.Span.Start)
}

// listClaimed returns the database that a value's field claims it names, and that database with the ones nested in it
// if the field says so, as a dialect is nested in its language. It returns "" and nil if the field claims none or the
// value is a link, even one written with an argument in parentheses.
func (walker *walker) listClaimed(context *Context, text string) (string, []string) {
	field := context.Field
	step, _, isCall := walker.schema.findKeyCall(text, 0)
	if !field.claimsDatabase() || walker.schema.IsLink(text) || isCall && walker.schema.IsLink(step.Text) {
		return "", nil
	}
	claim := field.Value.Value.Database
	databases := []string{claim}
	if database := walker.schema.Databases[claim]; database != nil && field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}
	return claim, databases
}

// findScriptValue returns the reference a value makes to a script value. It returns false if it makes none. The
// reference is inferred where only the field makes it and the field learned so from the game's own files. A link
// key's field is its holder's, so it gives the link's value none.
func (walker *walker) findScriptValue(context *Context, span jomini.Span) (Reference, bool) {
	text := walker.tree.Text(span)
	links := jomini.SplitChain(text)
	last := links[len(links)-1]
	chained := len(links) > 1 && !jomini.IsEventID(text) &&
		!slices.ContainsFunc(links[:len(links)-1], func(link string) bool { return !walker.schema.IsLink(link) })
	operated := context.isMath()
	valued := context.Category != CategoryLink && context.Field != nil && context.Field.Value.Value.Values
	_, declared := walker.schema.FindCommand(last)
	if walker.schema.Values == "" || !chained && !operated && !valued || !jomini.IsIdentifier(last) || declared ||
		walker.schema.IsLink(last) || slices.Contains(game.Bools, last) {
		return Reference{}, false
	}
	start := span.End - len(last)
	inferred := !chained && !operated && !context.Field.Value.Source.CanAccuse()
	return Reference{
		Database: walker.schema.Values, Name: last, Span: jomini.Span{Start: start, End: span.End}, Inferred: inferred,
	}, true
}

// findValueDatabase returns the database that a statement's value names by the place it is written, and the folder of
// the value's file if any. A value with a slash names no picture step's database. It returns false if the place names
// no database.
func (walker *walker) findValueDatabase(context *Context, level int, text string) (string, string, bool) {
	key := walker.frames[level].label
	keys := walker.frames[level-1].path
	if key != "" {
		keys = slices.Concat(keys, []string{key})
	}
	for _, row := range walker.row.ListReferences() {
		if row.Database == "" && walker.file.Format == files.FormatGUI && key == row.KeyPath ||
			row.Database != "" && row.Database == context.Database && jomini.MatchKeyPath(row.KeyPath, keys) {
			return cmp.Or(row.Target, row.Database), row.Folder, true
		}
	}
	// a value with a slash is a file's path, which Vic3's video step also takes (events/brazil/amazonas.txt:15)
	if database, ok := walker.findPictureDatabase(context.Database, keys); ok && !strings.Contains(text, "/") {
		return database, "", true
	}
	if database := walker.schema.Databases[context.Database]; database != nil && (!context.Defines || !context.Definition.Alias) {
		if target, isAlias := database.Row.Aliases[key]; isAlias {
			return cmp.Or(target, database.Name), "", true
		}
	}
	return "", "", false
}

// findCoatDatabase returns the database that a statement's value names in a coat of arms, and the folder of the image
// it names: a template list after a list tag, a named color at a color slot unless it names a slot, and an image at a
// pattern key or at the texture key of an emblem. It returns false anywhere else.
func (walker *walker) findCoatDatabase(statement int32, level int, text string) (string, string, bool) {
	if walker.database == nil || walker.database.Name != game.Coat.Database {
		return "", "", false
	}
	isSlot := func(key string) bool {
		return slices.ContainsFunc(game.Coat.Slots, func(slot game.CoatSlot) bool { return strings.EqualFold(slot.Key, key) })
	}
	key, holder := walker.frames[level].label, walker.frames[level-1].label
	switch {
	case !walker.tree.Statements[statement].Tag.IsEmpty():
		return game.Coat.ListsDatabase, "", true
	case isSlot(key) && !isSlot(text):
		return game.Coat.ColorsDatabase, "", true
	case key == game.Coat.PatternKey:
		return game.File, game.Coat.TextureFolders[key], true
	case key == game.Coat.TextureKey && game.Coat.TextureFolders[holder] != "":
		return game.File, game.Coat.TextureFolders[holder], true
	}
	return "", "", false
}

// findPictureDatabase returns the database whose definition a value names as one step of the game's event pictures,
// given the database the value is written in and the keys that lead to it. A step that picks its block by a field, or
// matches the value elsewhere than in a name, names none here. It returns false if no step reads the value.
func (walker *walker) findPictureDatabase(database string, keys []string) (string, bool) {
	for _, chain := range walker.row.Events.Picture.Chains {
		start := walker.schema.Events
		for _, step := range chain.Steps {
			if start == database && step.Match == "" && !strings.Contains(step.Key, "[") &&
				jomini.MatchKeyPath(step.Key, keys) {
				return step.Database, true
			}
			start = step.Database
		}
	}
	return "", false
}

// findTextDatabase returns the database whose definitions the text a keyword shows must be, written as a value. The
// keyword is the one the statement's key is, or its block's where the key is the text key. It is "" if no keyword
// shows the text.
func findTextDatabase(context *Context) string {
	if context.Label == game.LabelKey {
		context = context.Parent
	}
	if keyword := context.Keyword; keyword != nil {
		return keyword.Values
	}
	return ""
}

// hasNamespace reports whether an events file of the game or of the workspace's mods declares the namespace of an
// event id.
func (walker *walker) hasNamespace(id string) bool {
	namespace, _, _ := strings.Cut(id, ".")
	return walker.schema.Namespaces[namespace] || walker.namespaces[namespace]
}

// allowsUndefined reports whether a reference to a database needs no entry there, as its row says.
func (walker *walker) allowsUndefined(label string) bool {
	database := walker.schema.Databases[label]
	return database != nil && database.Row.AllowsUndefined
}

// listPrefixed returns the references a chain of links makes after prefixes that have a database. A name that is itself
// a chain of links, as scope:overlord is in relations:scope:overlord, makes the references of its own prefixes. A name
// a script saves is the name itself, whatever a link is called.
func (walker *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	offset := 0
	for _, link := range jomini.SplitChain(chain) {
		prefix, label, ok := jomini.SplitPrefixed(link)
		start := base + offset + len(prefix) + 1
		database := walker.schema.Prefixes[prefix]
		switch {
		case !ok: // the link has no prefix
		case !slices.Contains(game.Links, database) && walker.schema.IsLink(label):
			references = append(references, walker.listPrefixed(label, start)...)
		case database != "":
			references = append(references, walker.listLabelReferences(database, label, start)...)
		}
		offset += len(link) + 1
	}
	return references
}

// listLabelReferences returns the references a prefixed link's label makes, such as the group NAI and the define
// NAI|X for define:NAI|X.
func (walker *walker) listLabelReferences(database, label string, start int) []Reference {
	owner := walker.schema.Databases[database]
	if owner == nil || owner.Row.Separator == "" {
		return []Reference{{Database: database, Name: label, Span: jomini.Span{Start: start, End: start + len(label)}}}
	}
	holder, rest, _ := strings.Cut(label, owner.Row.Separator)
	references := []Reference{{Database: database, Name: holder, Span: jomini.Span{Start: start, End: start + len(holder)}}}
	key, _, _ := strings.Cut(rest, owner.Row.Separator)
	if key == "" {
		return references
	}
	keyStart := start + len(holder) + len(owner.Row.Separator)
	for _, nested := range owner.Nested {
		references = append(references, Reference{
			Database: nested, Name: walker.schema.JoinNested(nested, holder, key),
			Span: jomini.Span{Start: keyStart, End: keyStart + len(key)},
		})
	}
	return references
}

// isLead reports whether a value written alone makes the statement after it a definition or a call.
func (walker *walker) isLead(text string) bool {
	if walker.declaring[text] != "" {
		return true
	}
	return walker.file.Format == files.FormatGUI &&
		(slices.Contains(game.GUI.Declares, text) || len(walker.schema.Callees[text]) > 0)
}

// FindDeclaration returns how the dumps declare a command, among those this context's block allows. An effect never
// fits in a block of triggers. It returns false if the block does not take the command.
func (context *Context) FindDeclaration(command *Command) (string, bool) {
	position := slices.IndexFunc(command.Declared, context.Allows)
	if position < 0 {
		return "", false
	}
	return command.Declared[position], true
}

// FindArguments returns the keys a command takes in this context's block under the given declaration. A command
// declared as both an effect and a trigger takes the keys of both in a block that allows either.
func (context *Context) FindArguments(command *Command, declaration string) *Field {
	if command.Union != nil && context.Allows(game.DeclaredEffect) && context.Allows(game.DeclaredTrigger) {
		return command.Union
	}
	return command.ArgumentsOf(declaration)
}

// Allows reports whether this context's block takes a command with the given declaration. An effect never fits in a
// block of triggers.
func (context *Context) Allows(declaration string) bool {
	return slices.Contains(game.Shapes[findShape(context)].Allows, declaration) &&
		(declaration != game.DeclaredEffect || !context.holdsConditions())
}

// holdsConditions reports whether the statements in the block of a context are inside a trigger block. They are
// when the block is triggers, or sits below triggers and is no value block.
func (context *Context) holdsConditions() bool {
	return context.Shape == game.ShapeTriggers || context.Shape == game.ShapeTriggerArguments ||
		context.Conditional && context.Shape != game.ShapeValue
}
