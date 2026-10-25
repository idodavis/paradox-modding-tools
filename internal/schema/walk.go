package schema

import (
	"cmp"
	"path"
	"slices"
	"strconv"
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
)

// A Definition is a name that a statement defines in a database, with the facts that decide which definition of
// that name the game keeps.
type Definition struct {
	Database  string      `json:"database"`
	Name      string      `json:"name"`
	Span      jomini.Span `json:"span"`      // the name alone: no quotes, no entry mode
	Statement int32       `json:"statement"` // the statement that writes it; 0, the file, if the file names it
	Mode      string      `json:"mode"`      // an entry mode's prefix such as INJECT or REPLACE; "" if none
	Priority  int         `json:"priority"`  // CK3's override priority; 0 when none is written
	Local     bool        `json:"local"`     // visible only in its own file
	Alias     bool        `json:"alias"`     // the value of a key its database also names definitions by
}

// A Reference is a name that a key or a value refers to, in a database, together with the caller it was resolved
// through.
type Reference struct {
	Database string      `json:"database"`
	Name     string      `json:"name"`
	Span     jomini.Span `json:"span"`             // the name alone: no quotes, no prefix
	Caller   string      `json:"caller,omitempty"` // the scripted call it is resolved through; "" if none
	// Inferred is set when the reference rests only on what the game's own files write, and no declared source or
	// rule makes it.
	Inferred bool `json:"inferred,omitempty"`
}

// ToID returns the id the pages give a referenced name: its database and its name.
func (reference Reference) ToID() string { return reference.Database + ":" + reference.Name }

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
	Command     *Command    // the command the key names; nil where the key matched none
	Declaration string      // how the dumps declare that command where the key is in this block: a game.Declared word
	ValueGroup  Category    // the value's color category
	// Caller is the word a call in this statement's block resolves by, where a keyword gives one; "" means the block's
	// shape's.
	Caller      string
	Datacontext string   // a GUI block's data context type; "" if none
	Parent      *Context // the context of the block that holds the statement, the file's own at the top
}

// A Visitor is called by the walk at each statement it enters and leaves. A file that is one definition is entered
// first as statement 0, the file, with the definition's context, and left last. Statement 0 has no key, value or
// block of its own.
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
	restricted bool // whether the walk is restricted to the statements that cover offset
	offset     int  // the byte offset the walk is restricted to
	rel        string
	stem       string    // the file's name without its folder and extension
	database   *Database // the database of the file's folder; nil for a file of no folder
	fileType   game.FileType
	nestings   []game.Nesting
	namespaces []int32                       // the statements that declare an event namespace
	declaring  map[string]string             // the database a word before a statement declares a definition of, inline
	inline     map[string]ScriptedDefinition // each name the file declared for itself so far
	frames     []frame                       // frames[0] is the file's; frames[n] is that of the statements n levels down
	saved      []Saved
}

// A frame is what the walk keeps for the statements at one level of depth.
type frame struct {
	context *Context
	label   string      // what the statement last read at this level is written under
	span    jomini.Span // where that label is written
	path    []string    // the keys that lead to its children, from the enclosing definition or else the top
	wraps   bool        // its children are written as definitions
}

// A match is the rule that a key fits, with what it fit.
type match struct {
	rule        string
	category    Category
	child       *Field
	command     *Command
	declaration string              // what declares the command that fit
	callee      *ScriptedDefinition // the scripted definition that a known call names
	caller      string
	iterator    *game.Keyword // the list iterator that the key starts
	target      string        // the scope that the command or the link leads to
	database    string        // the database the key itself names
	name        string        // the name in that database the key writes at its end; "" means the whole key
}

// Walk goes through a file's tree once and calls the visitor at every statement. At each one it decides what the
// schema and the game's rules say about it: the field, the scope, the shape, the definition and the references.
func (schema *Schema) Walk(tree *jomini.Tree, file files.File, visitor Visitor) {
	schema.newWalker(tree, file, visitor).walkFile()
}

// WalkTo is Walk restricted to one position: it only enters statements that cover the given byte offset, so the
// visitor sees the path to that place and nothing else.
func (schema *Schema) WalkTo(tree *jomini.Tree, file files.File, offset int, visitor Visitor) {
	walker := schema.newWalker(tree, file, visitor)
	walker.restricted, walker.offset = true, offset
	walker.walkFile()
}

// ListNamespaces returns the event namespaces a file declares, in the order it declares them. It returns none for a
// file that declares none, and for a file outside the events database.
func (schema *Schema) ListNamespaces(tree *jomini.Tree, file files.File) []string {
	var namespaces []string
	if database, ok := schema.FindDatabase(file.Rel); ok && database.Row.Houses == "events" {
		for _, statement := range listNamespaces(tree) {
			namespaces = append(namespaces, tree.Unquoted(tree.Statements[statement].Value))
		}
	}
	return namespaces
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

// newWalker returns the walker of one file. The file's own context holds the one definition of a file that its
// database names by its own name. Only a file of the events database declares definitions inline.
func (schema *Schema) newWalker(tree *jomini.Tree, file files.File, visitor Visitor) *walker {
	row := game.LookupGame(schema.Game)
	if row == nil {
		row = &game.Game{}
	}
	walker := &walker{
		schema: schema, row: row, tree: tree, file: file, visitor: visitor, inline: map[string]ScriptedDefinition{},
	}
	_, walker.rel = SplitStage(row, file.Rel)
	walker.stem = strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
	walker.fileType = game.FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = schema.FindDatabase(file.Rel)
	}
	walker.nestings = row.FindNestings(walker.rel)
	root := &Context{Shape: game.ShapeUnknown}
	if database := walker.database; database != nil {
		if database.Row.Houses == "events" {
			walker.namespaces = listNamespaces(tree)
			walker.declaring = schema.InlineDatabases
		}
		if database.Row.NamedBy == "file" && len(tree.Statements) > 1 {
			walker.resolveDefinition(root, Definition{Database: database.Name, Name: walker.stem, Local: database.Callable.Local})
		}
	}
	walker.frames = []frame{{context: root, wraps: true}}
	return walker
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

// walkChildren walks the children of a statement, one level down. The children of statement 0, the file, are the top
// statements. A word that declares or calls a definition applies to the statement after it. A walk to an offset still
// resolves the statements before it, to learn what they saved, but does not offer them to the visitor. A silent walk
// offers nothing. If the definition has a run order, each block starts from the scopes saved by the blocks that run
// before it.
func (walker *walker) walkChildren(parent int32, level int, silent bool) {
	for len(walker.frames) <= level {
		walker.frames = append(walker.frames, frame{context: &Context{}})
	}
	starts := walker.walkRunOrder(parent, level, silent)
	pending := ""
	for child := range walker.tree.Children(parent) {
		statement := walker.tree.Statements[child]
		offered := !silent && (!walker.restricted || statement.Span.Contains(walker.offset))
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

// walkRunOrder walks the children of a definition silently, in the order that its database says they run. The keys
// the database lists come first, and the rest keep their written order. It walks every child, even in a walk to an
// offset. It returns the scopes saved before each child, by statement, so that the walk in written order can start
// each child from them. It returns nil and walks nothing if the statement is not a definition with a run order, or
// if the walk is already silent.
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

// walkStatement resolves one statement and offers it to the visitor if the walk offers it. It walks the statement's
// block if the visitor asks.
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

// resolve fills the context of a statement from the context of the block that holds it. The context gets the
// definition that the statement makes, or else the rule that its key fits. It then resolves what the value refers
// to. A statement is in a trigger block when it is below a block of triggers, until a value block begins.
func (walker *walker) resolve(context *Context, statement int32, level int, pending string) {
	parent := walker.frames[level-1].context
	*context = Context{
		Parent: parent, Database: parent.Database, Definition: parent.Definition, Scope: parent.Scope, Root: parent.Root,
		Previous: parent.Previous, Saved: walker.saved,
		Conditional: parent.holdsConditions(),
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
	if walker.tree.Statements[statement].IsBlock() {
		walker.buildPaths(level, label, defines && walker.database != nil && definition.Database == walker.database.Name)
	}
	if walker.file.Format == files.FormatGUI && walker.tree.Statements[statement].IsBlock() {
		walker.resolveDatacontext(context, statement, parent.Datacontext)
	}
	walker.resolveValue(context, statement, level)
}

// resolveStatement fills the context of a statement that defines nothing. A keyed statement follows the rule its key
// fits. A block with no key takes the shape and field of the block that holds it. A value with no key belongs to the
// field of the block that holds it. At the top of a file whose database names nothing, a block is a definition's
// block with no name.
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

// resolveLead resolves the references of a call written as a word and a label, such as `blockoverride "label"` in a
// GUI file. The label refers to a name in each database that the word calls.
func (walker *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range walker.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}
	if lead != "" && len(walker.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// findLabel returns the label of a statement and where it is written. The label is the key, or the tag of a block
// that has no key. It returns an empty label and span for a value written alone.
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

// resolveDefinition makes the context of the statement that defines the given definition. Its block holds the fields
// of the definition's database, in the shape the database gives, and its scope starts at the definition's root. The
// context keeps its parent.
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

// findDefinitionShape returns the shape of a definition's block in the given database. It is the shape of the
// callable, else of the row, else of the file type, else the shape named like the first word that calls it, else
// fields.
func (walker *walker) findDefinitionShape(database *Database) string {
	if database == nil {
		return cmp.Or(walker.fileType.Shape, game.ShapeFields)
	}
	shape := cmp.Or(database.Callable.Shape, database.Row.Shape, walker.fileType.Shape)
	if callers := database.Callable.Callers; shape == "" && len(callers) > 0 {
		if _, ok := game.Shapes[callers[0]]; ok {
			shape = callers[0]
		}
	}
	return cmp.Or(shape, game.ShapeFields)
}

// findRootScope returns the scope that a definition's script starts in. An event writes its own scope, else the
// game's default holds. An on_action has the scope the dumps give. Any other definition has the scope its database's
// docs give. It returns "" if nothing says.
func (walker *walker) findRootScope(database *Database, definition Definition) string {
	if database == nil {
		return ""
	}
	switch database.Row.Houses {
	case "events":
		events := walker.row.Events
		scope, _ := walker.tree.FindChildValue(definition.Statement, events.ScopeKey)
		if scope == "" {
			eventType, _ := walker.tree.FindChildValue(definition.Statement, events.TypeKey)
			scope = strings.TrimSuffix(eventType, events.TypeSuffix)
		}
		if slices.Contains(game.DumpPhrases.NoScope, scope) {
			return ""
		}
		return cmp.Or(scope, events.DefaultScope)
	case "on_actions":
		return walker.schema.OnActions[definition.Name].Scope
	}
	return database.Definition.Scope.Value
}

// findDefinition returns the definition that a statement makes. It is a constant, a name that a nesting defines, or
// what the file's format or database says is defined here. It returns false if the statement defines nothing.
func (walker *walker) findDefinition(statement int32, level int, pending string) (Definition, bool) {
	if label := walker.frames[level].label; strings.HasPrefix(label, "@") {
		return Definition{Database: game.Constant, Name: label, Span: walker.frames[level].span, Statement: statement, Local: true}, true
	}
	if definition, ok := walker.findNested(statement, level); ok {
		return definition, true
	}
	switch walker.file.Format {
	case files.FormatGUI:
		return walker.findGUIDefinition(statement, level, pending)
	case files.FormatAsset, files.FormatFont:
		return walker.findAssetDefinition(statement, level)
	}
	return walker.findScriptDefinition(statement, level, pending)
}

// findScriptDefinition returns the definition that a statement of a script file makes, by how the file's database
// names its definitions. Only a statement written where definitions are written makes one. A table file's rows make
// none. A name declared inline is local to its file. A definition named by a call takes the first step of the call
// that its name key writes. It returns false if the statement defines nothing.
func (walker *walker) findScriptDefinition(statement int32, level int, pending string) (Definition, bool) {
	label, span := walker.frames[level].label, walker.frames[level].span
	if owner := walker.schema.Databases[walker.declaring[pending]]; owner != nil && label != "" {
		walker.inline[label] = makeScripted(owner, walker.tree, statement)
		return Definition{Database: owner.Name, Name: label, Span: span, Statement: statement, Local: true}, true
	}
	database := walker.database
	if database == nil || !walker.frames[level-1].wraps {
		return Definition{}, false
	}
	switch database.Row.NamedBy {
	case "nothing", "file", "column":
		return Definition{}, false
	case "field":
		name, ok := walker.findNameSpan(statement, database)
		if !ok {
			return Definition{}, false
		}
		span, label = name, walker.tree.Text(name)
	case "call":
		name, ok := walker.findNameSpan(statement, database)
		call, parsed := jomini.ParseCall(walker.tree.Text(name), name.Start)
		if !ok || !parsed {
			return Definition{}, false
		}
		span, label = call.Steps[0].Span, call.Steps[0].Text
	}
	if label == "" || slices.Contains(database.Row.WrapperKeys, label) {
		return Definition{}, false
	}
	mode, label := walker.row.SplitMode(label)
	if database.Row.Houses == "events" && !walker.isEvent(label, statement) {
		return Definition{}, false
	}
	span.Start = span.End - len(label) // the name follows the mode and colon
	return Definition{
		Database: database.Name, Name: label, Span: span, Statement: statement, Mode: mode,
		Priority: walker.findPriority(database, statement), Local: database.Callable.Local,
	}, true
}

// findNameSpan returns the span of the value of the key that names a definition, without quotes. It returns false if
// the definition writes no such key.
func (walker *walker) findNameSpan(statement int32, database *Database) (jomini.Span, bool) {
	child, ok := walker.tree.FindChild(statement, database.Row.NameKey)
	return walker.tree.UnquotedSpan(walker.tree.Statements[child].Value), ok
}

// isEvent reports whether the label is the name of an event in the file. It is a namespace that the file declares above
// the statement, a dot and an id.
func (walker *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(walker.namespaces, func(declaring int32) bool {
		namespace := walker.tree.Unquoted(walker.tree.Statements[declaring].Value)
		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}

// findPriority returns the override priority that a definition writes under the key its database's precedence names. It
// returns 0 if there is none.
func (walker *walker) findPriority(database *Database, statement int32) int {
	value, _ := walker.tree.FindChildValue(statement, database.Precedence.PriorityKey)
	priority, _ := strconv.Atoi(value)
	return priority
}

// findGUIDefinition returns the definition that a statement of a GUI file makes. It is the one that the word before
// the statement declares, one that the statement's key declares with its name as the block's tag (template = name { }),
// a block named by one of its keys, or a window at the top of the file.
func (walker *walker) findGUIDefinition(statement int32, level int, pending string) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	switch nameKey := game.GUI.NameKeyOf[label]; {
	case slices.Contains(game.GUI.Declares, pending) && label != "":
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+pending, label, span, statement)
	case slices.Contains(game.GUI.Declares, label) && !current.Tag.IsEmpty():
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+label, walker.tree.Text(current.Tag), current.Tag, statement)
	case nameKey != "":
		value, _ := walker.tree.FindChildValue(statement, nameKey)
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+label, value, span, statement)
	case level == 1 && !current.Key.IsEmpty() && current.IsBlock():
		return walker.makeGUIDefinition(game.GUI.WidgetDatabase, walker.findWindowLabel(statement), span, statement)
	}
	return Definition{}, false
}

// findWindowLabel returns what names a window at the top of a GUI file: the value of its name key. A window that writes
// none takes its file's name if it is the only block the file writes under a key, as EU5's
// panels/disaster/war_of_the_roses.gui is the panel of the disaster war_of_the_roses. It returns "" for any other
// window with no name.
func (walker *walker) findWindowLabel(statement int32) string {
	if value, _ := walker.tree.FindChildValue(statement, game.GUI.WidgetKey); value != "" {
		return value
	}
	for child := range walker.tree.Children(0) {
		current := walker.tree.Statements[child]
		if child != statement && !current.Key.IsEmpty() && current.IsBlock() {
			return ""
		}
	}
	return walker.stem
}

// makeGUIDefinition returns a definition in a GUI database. If the database answers only in its own file, the
// definition is callable in the rest of that file. It returns false if the label is empty.
func (walker *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Definition, bool) {
	owner := walker.schema.Databases[database]
	local := owner != nil && owner.Callable.Local
	if local && label != "" {
		walker.inline[label] = makeScripted(owner, walker.tree, statement)
	}
	return Definition{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
}

// findAssetDefinition returns the definition that a top block of an asset or font file makes. Its database is named
// for the file's extension and the block's key, and its name is the value of the key that the file type names.
func (walker *walker) findAssetDefinition(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	key, span := walker.frames[level].label, walker.frames[level].span
	if level != 1 || key == "" || !current.IsBlock() {
		return Definition{}, false
	}
	label := key
	if walker.fileType.NameKey != "" {
		label, _ = walker.tree.FindChildValue(statement, walker.fileType.NameKey)
	}
	database := strings.TrimPrefix(strings.ToLower(path.Ext(walker.rel)), ".") + "." + key
	return Definition{Database: database, Name: label, Span: span, Statement: statement}, label != ""
}

// findNested returns the definition that a statement makes in a database nested in another. The name is the
// statement's key, the tag of a block with none, or a value written alone. It returns false if the statement is not
// written under the key path of a nesting, or if the docs describe its key as a field.
func (walker *walker) findNested(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	if label == "" && current.IsListed() {
		span = walker.tree.UnquotedSpan(current.Value)
		label = walker.tree.Text(span)
	}
	if label == "" {
		return Definition{}, false
	}
	for _, nesting := range walker.nestings {
		if !game.MatchKeyPath(nesting.KeyPath, walker.frames[level-1].path) {
			continue
		}
		database, same := walker.findNestedDatabase(nesting)
		holding := walker.frames[level-1].context.Definition != nil || walker.database != nil && walker.database.Row.NamedBy == "nothing"
		if database == "" || !holding && (same || path.Ext(nesting.Parent) == "") {
			continue
		}
		if nesting.KeyPath == "" && !walker.isNested(label, nesting, same) {
			continue
		}
		return Definition{Database: database, Name: label, Span: span, Statement: statement}, true
	}
	return Definition{}, false
}

// findNestedDatabase returns the database that a nesting defines in. It also reports whether that is the holder's own.
func (walker *walker) findNestedDatabase(nesting game.Nesting) (string, bool) {
	if nesting.Database != "" {
		return game.ToDatabase(nesting.Parent) + "." + nesting.Database, false
	}
	if walker.database == nil {
		return "", true
	}
	return walker.database.Name, true
}

// isNested reports whether a key in a definition's own block defines a nested definition of a nesting. It does not if
// the docs describe the key as a field. In the holder's own database it does only if the key fits the nesting's keys.
func (walker *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if walker.database != nil && walker.database.Documented[key] {
		return false
	}
	fits, _ := path.Match(nesting.Keys, key)
	return !same || fits
}

// resolveKey fills the context of a keyed statement from the first rule that its key fits in the block that holds it.
// A keyword of the language adjusts what the rule gave, and is never a call.
func (walker *walker) resolveKey(context *Context, statement int32, level int) {
	parent := walker.frames[level-1].context
	key, span := walker.frames[level].label, walker.frames[level].span
	// A key with a parameter is unknown until the call is made, and so are its block's shape and scope.
	if jomini.IsParameter(key) {
		context.Category, context.Shape, context.Scope = CategoryParameter, game.ShapeUnknown, ""
		return
	}
	var keyword *game.Keyword
	if row, ok := game.LookupKeyword(findShape(parent), strings.ToLower(walker.frames[level-1].label), strings.ToLower(key)); ok {
		keyword = &row
	}
	fit := walker.findMatch(parent, key)
	// A key that starts an iterator and is no word of its own runs its block in the scope of the list's item.
	if iterator, ok := game.MatchListIterator(key); ok && keyword == nil &&
		(fit.command != nil || fit.category == CategoryCall && fit.callee == nil && walker.schema.Lists != "") {
		fit.iterator = &iterator
	}
	switch {
	case fit.category == CategoryLink:
		fit.command, fit.declaration = walker.findLinkCommand(key), game.DeclaredLink
		fit.target = walker.resolveChain(key, parent)
	case fit.command != nil:
		fit.target = fit.command.Target
	}
	walker.resolveMatch(context, parent, fit, keyword)
	// A conditional keyword such as limit is read in the scope of the command that holds it. Any other argument is
	// read in a scope the command makes.
	if isArgument(parent, key) && (keyword == nil || keyword.Class != "conditional") {
		context.Scope = ""
	}
	if keyword == nil {
		walker.resolveCallReferences(context, fit, key, span)
	}
	if keyword != nil && (fit.rule == "" || context.Field == nil) {
		context.Field = parent.Field.findPatternChild(key)
	}
	walker.resolveSaved(context, statement, level)
	walker.resolveAlias(context, statement, level)
}

// findShape returns the shape of the block that a context holds. It is unknown if the walk decided none.
func findShape(context *Context) string {
	return cmp.Or(context.Shape, game.ShapeUnknown)
}

// findMatch returns the first rule of the block's shape that the key fits. A field's key in a block whose keys name
// definitions also refers to one. It returns the empty match if no rule fits.
func (walker *walker) findMatch(parent *Context, key string) match {
	for _, rule := range game.Shapes[findShape(parent)].Rules {
		if fit, ok := walker.matchRule(rule, parent, key); ok {
			fit.rule = rule
			if fit.category == CategoryField && fit.database == "" && parent.Field != nil {
				fit.database = parent.Field.Keys.Value
			}
			return fit
		}
	}
	return match{}
}

// findCaller returns the word that a call in the block of a context resolves by. It is the word a keyword gave, else
// the one the block's shape gives.
func (context *Context) findCaller() string {
	return cmp.Or(context.Caller, game.Shapes[findShape(context)].Caller)
}

// isArgument reports whether a key in the block of a command is one of the command's arguments. The command must lead
// to no scope and iterate no list, and its arguments must hold the key.
func isArgument(parent *Context, key string) bool {
	command := parent.Command
	return parent.Category == CategoryCommand && command != nil && command.Target == "" && !command.Lists &&
		parent.Field.findChild(key) != nil
}

// matchRule reports whether a key fits one rule of its block's shape. It returns what the key fit.
func (walker *walker) matchRule(rule string, parent *Context, key string) (match, bool) {
	holder := parent.Field
	switch rule {
	case "known-call":
		callee, caller, ok := walker.findCallee(parent.findCaller(), key)
		return match{category: CategoryCall, callee: &callee, caller: caller}, ok
	case "declared":
		child := holder.findChild(key)
		return match{category: CategoryField, child: child}, child != nil &&
			child.Source != game.SourceNone && child.Source != game.SourceVanilla
	case "documented":
		database := walker.schema.Databases[parent.Database]
		return match{category: CategoryField, child: holder.findChild(key)}, database != nil && database.Documented[key]
	case "command":
		return walker.matchCommand(parent, key)
	case "operator":
		return match{}, game.IsOperator(key)
	case "link":
		return match{category: CategoryLink}, walker.isLink(key)
	case "modifier":
		_, declared := walker.schema.Modifiers[key]
		return match{category: CategoryField, child: holder.findChild(key)}, declared
	case "modifier-type":
		return match{category: CategoryReference, child: holder.findChild(key), database: walker.schema.ModifierTypes},
			walker.schema.ModifierTypes != "" && jomini.IsIdentifier(key)
	case "observed":
		child := holder.findChild(key)
		return match{category: CategoryField, child: child}, child != nil && child.Source == game.SourceVanilla
	case "field":
		child := holder.findPatternChild(key)
		return match{category: CategoryField, child: child}, child != nil
	case "field-or-call":
		if child := holder.findPatternChild(key); child != nil {
			return match{category: CategoryField, child: child}, true
		}
		return walker.matchCall(parent, key)
	case "call":
		return walker.matchCall(parent, key)
	}
	return match{}, false
}

// matchCall returns a call, with the caller of the block that holds the key. It returns false unless the key is an
// identifier and not an event id.
func (walker *walker) matchCall(parent *Context, key string) (match, bool) {
	return match{category: CategoryCall, caller: parent.findCaller()}, jomini.IsIdentifier(key) && !jomini.IsEventID(key)
}

// findCallee returns the scripted definition that a key calls through the given word, and the word it was found
// through. A definition the file declared itself comes first, then one the game defines. The word game.AnyCall tries
// every word. It returns false if no definition has the name.
func (walker *walker) findCallee(caller, key string) (ScriptedDefinition, string, bool) {
	if scripted, ok := walker.inline[key]; ok {
		callers := walker.schema.Databases[scripted.Database].Callable.Callers
		if len(callers) > 0 && (caller == game.AnyCall || slices.Contains(callers, caller)) {
			return scripted, callers[0], true
		}
	}
	if caller != game.AnyCall {
		callee, ok := walker.lookupCallee(caller, key)
		if !ok && caller == game.GUI.TagCalls && walker.schema.WidgetTypes[key] {
			return ScriptedDefinition{}, caller, true
		}
		return callee, caller, ok && caller != ""
	}
	var scripted ScriptedDefinition
	best := ""
	for via := range walker.schema.Callees {
		if callee, ok := walker.lookupCallee(via, key); ok && via != game.AnyCall && (best == "" || via < best) {
			scripted, best = callee, via
		}
	}
	return scripted, best, best != ""
}

// lookupCallee returns the scripted definition of a name called through the given word. A name in another case finds
// the definition only in a database that ignores case.
func (walker *walker) lookupCallee(caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := walker.schema.ScriptedDefinitions[caller+":"+key]; ok {
		return callee, true
	}
	callee, ok := walker.schema.ScriptedDefinitions[caller+":"+strings.ToLower(key)]
	database := walker.schema.Databases[callee.Database]
	return callee, ok && database != nil && database.Callable.FoldsCase
}

// matchCommand returns the command that a key names, as written or in lower case. For a key that iterates a list over
// a definition named by an alias, it returns the command of that list. It returns false unless the block's shape
// holds commands of the kind the command is declared as. A block inside triggers holds no effect.
func (walker *walker) matchCommand(parent *Context, key string) (match, bool) {
	command, ok := walker.schema.FindCommand(key)
	fit := match{category: CategoryCommand, command: command}
	if !ok {
		fit.command, fit.database, fit.name = walker.schema.findList(key)
	}
	if fit.command == nil {
		return match{}, false
	}
	fit.declaration, ok = parent.FindDeclaration(fit.command)
	return fit, ok
}

// isLink reports whether a key starts with a scope link. That is a link the dumps declare, or a prefix that takes a
// name after a colon. Case does not matter, as the games write ROOT and root alike. A wild link is a pattern, so its
// own name is no link.
func (walker *walker) isLink(key string) bool {
	first := strings.ToLower(jomini.SplitChain(key)[0])
	if command := walker.schema.Commands[first]; command != nil && !command.Wild && slices.Contains(command.Declared, game.DeclaredLink) {
		return true
	}
	prefix, _, ok := jomini.SplitPrefixed(first)
	if _, exists := walker.schema.Prefixes[prefix]; ok && exists {
		return true
	}
	wild := walker.schema.Commands[prefix]
	return ok && wild != nil && wild.Wild
}

// findLinkCommand returns the command of the first link of a chain, or the command of its prefix if it has one. It
// returns nil if the dumps declare none.
func (walker *walker) findLinkCommand(key string) *Command {
	first := strings.ToLower(jomini.SplitChain(key)[0])
	if prefix, _, ok := jomini.SplitPrefixed(first); ok {
		first = prefix
	}
	return walker.schema.Commands[first]
}

// resolveMatch sets the context of a keyed statement from the rule its key fit. It sets the field the block's keys
// come from, the shape of the block, the scope inside it and the key's category. If the rule gives no shape, the
// block takes the shape its parent gives to blocks written in it. A keyword of the language gets the keyword category
// and adjusts the shape and the caller. An iterator gives the shape of its block.
func (walker *walker) resolveMatch(context *Context, parent *Context, fit match, keyword *game.Keyword) {
	rule := game.KeyRules[fit.rule]
	context.Field = walker.resolveField(rule.Fields, parent, fit)
	context.Shape = cmp.Or(walker.resolveShape(rule.Shape, parent, context.Field, fit), game.Shapes[findShape(parent)].Blocks)
	context.Category = fit.category
	context.Command, context.Declaration = fit.command, fit.declaration
	if keyword != nil {
		if keyword.Shape == "enclosing" {
			context.Shape = findShape(parent)
		} else {
			context.Shape = cmp.Or(keyword.Shape, context.Shape)
		}
		context.Caller = keyword.Caller
		context.Category = CategoryKeyword
		if keyword.Shape != "" && rule.Scope == "" {
			rule.Scope = "stays"
		}
	}
	if fit.iterator != nil {
		context.Shape = fit.iterator.Shape
	}
	walker.resolveScope(context, parent, rule, fit)
}

// resolveField returns the field that the keys inside a block come from, as the origin of a key rule says.
func (walker *walker) resolveField(origin string, parent *Context, fit match) *Field {
	switch origin {
	case "child":
		return fit.child
	case "callee":
		if fit.callee.Parameters != nil {
			return fit.callee.Parameters
		}
		if database := walker.schema.Databases[fit.callee.Database]; database != nil {
			return database.Definition
		}
	case "arguments":
		return fit.command.Arguments
	case "holder":
		return parent.Field
	}
	return nil
}

// resolveShape returns the shape of a block, as the decider of a key rule says. A command whose arguments a source
// states takes the shape its usage names. Any other command takes the kind it is declared as.
func (walker *walker) resolveShape(decider string, parent *Context, field *Field, fit match) string {
	switch decider {
	case "claim":
		if field != nil && field.Shape.IsStated() {
			return field.Shape.Value
		}
		return game.ShapeUnknown
	case "command":
		switch {
		case field != nil && field.Shape.IsStated():
			return field.Shape.Value
		case field != nil:
			return game.ShapeArguments
		case fit.declaration == game.DeclaredTrigger:
			return game.ShapeTriggers
		}
		return game.ShapeEffects
	case "same":
		return findShape(parent)
	case game.ShapeArguments:
		return game.ShapeArguments
	}
	return ""
}

// resolveScope sets the scope inside a block, as the key rule says. A command or a link that leads to a scope where
// script runs moves the scope there and pushes the scope it leaves onto the previous ones. A command that iterates a
// list with no target moves to an unknown scope. Any other block keeps the scope if its shape is known.
func (walker *walker) resolveScope(context *Context, parent *Context, rule game.KeyRule, fit match) {
	moves := fit.iterator != nil || rule.Scope == "follows" &&
		(fit.category == CategoryLink || fit.command != nil && (fit.command.Target != "" || fit.command.Lists))
	switch {
	case moves:
		context.Scope = ""
		if scope := walker.schema.Scopes[fit.target]; scope.Triggers || scope.Effects {
			context.Scope = fit.target
		}
		context.Previous = slices.Concat([]string{parent.Scope}, parent.Previous)
	case (rule.Scope == "stays" || rule.Scope == "follows") && context.Shape != "" && context.Shape != game.ShapeUnknown:
		context.Scope = parent.Scope
	default:
		context.Scope = ""
	}
}

// resolveCallReferences resolves the references that a key makes by the rule it fit. The key may name a database
// itself, or write prefixed names after a link, or call a scripted definition. Otherwise the key is a name in each
// database that its caller calls, or in game.AnyCall once if the caller calls every database. A key that starts the
// iterator of a scripted list names that list.
func (walker *walker) resolveCallReferences(context *Context, fit match, key string, span jomini.Span) {
	switch {
	case fit.database != "":
		name := cmp.Or(fit.name, key)
		span.Start = span.End - len(name)
		context.References = append(context.References, Reference{Database: fit.database, Name: name, Span: span})
	case fit.category == CategoryLink:
		context.References = append(context.References, walker.listPrefixed(key, span.Start)...)
	case fit.callee != nil && fit.callee.Database != "":
		context.References = append(context.References, Reference{Database: fit.callee.Database, Name: key, Span: span, Caller: fit.caller})
	case fit.category != CategoryCall || fit.callee != nil:
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

// resolveChain returns the scope that a chain of links leads to, starting in the scope of the block that holds it.
// The language's own words lead to the definition's root, the previous scope and the current one. Case does not
// matter. At the first link that no dump declares, or that cannot be followed from the scope it is in, the chain
// stops and the result is unknown.
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
			scope = walker.findSavedType(label)
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

// findSavedType returns the type of the scope that the definition saved last under the given name. It returns "" if
// it saved none.
func (walker *walker) findSavedType(label string) string {
	for i := len(walker.saved) - 1; i >= 0; i-- {
		if walker.saved[i].Name == label {
			return walker.saved[i].Type
		}
	}
	return ""
}

// resolveSaved resolves a command that makes a name which a link reads back later, such as a saved scope or a
// variable. The name becomes a definition. A saved scope is also added to the scopes the definition saved.
func (walker *walker) resolveSaved(context *Context, statement int32, level int) {
	current := walker.tree.Statements[statement]
	if current.IsBlock() || current.Value.IsEmpty() {
		return
	}
	key, parent := walker.frames[level].label, walker.frames[level-1].context
	link, ok := game.LookupLinkMaker(key)
	scope := parent.Scope
	if isArgument(parent, key) {
		scope = ""
	}
	if !ok {
		link, ok = game.LookupLinkMaker(strings.ToLower(walker.frames[level-1].label) + "/" + key)
		scope = ""
	}
	span := walker.tree.UnquotedSpan(current.Value)
	label := walker.tree.Text(span)
	if !ok || jomini.IsParameter(label) || strings.HasPrefix(label, "[") {
		return
	}
	context.Definition = &Definition{Database: link, Name: label, Span: span, Statement: statement}
	context.Defines = true
	if link == game.SavedScopes {
		walker.saved = append(walker.saved, Saved{Name: label, Type: scope})
	}
}

// resolveAlias makes the value that a statement writes at an alias key of its database a definition. It is a second
// name of the definition that the statement sits directly in. A value with a parameter names nothing yet.
func (walker *walker) resolveAlias(context *Context, statement int32, level int) {
	current, parent := walker.tree.Statements[statement], walker.frames[level-1].context
	database := walker.schema.Databases[parent.Database]
	if database == nil || !parent.Defines || parent.Category != CategoryDefinition || current.IsBlock() ||
		current.Value.IsEmpty() || !slices.Contains(database.Row.AliasKeys, walker.frames[level].label) {
		return
	}
	span := walker.tree.UnquotedSpan(current.Value)
	if label := walker.tree.Text(span); label != "" && !jomini.IsParameter(label) {
		context.Definition = &Definition{Database: database.Name, Name: label, Span: span, Statement: statement, Alias: true}
		context.Defines = true
	}
}

// resolveDatacontext sets the data context type of a GUI block from the data call that its datacontext key writes. The
// given type is that of the block that holds it. A call the dumps do not resolve leaves the type unknown.
func (walker *walker) resolveDatacontext(context *Context, statement int32, outer string) {
	child, ok := walker.tree.FindChild(statement, game.GUI.ContextKey)
	if !ok {
		return
	}
	context.Datacontext = ""
	if call, ok := ParseCall(walker.tree, walker.tree.Statements[child].Value); ok {
		context.Datacontext = walker.schema.ResolveDatacontext(call, outer)
	}
}

// ParseCall parses the data call that a value in a tree writes between brackets. The spans are in the file's own
// offsets. It returns false if the value is no call.
func ParseCall(tree *jomini.Tree, span jomini.Span) (jomini.Call, bool) {
	text, start := tree.Text(span), span.Start
	if strings.HasPrefix(text, `"`) {
		text, start = strings.Trim(text, `"`), start+1
	}
	return jomini.ParseBracketed(text, start)
}

// resolveValue resolves the value of a scalar statement. A parameter is a parameter. A constant refers to the file's
// own constant, and an expression refers to each constant it uses. A data call refers to the names its quoted
// arguments hold. Any other value refers to the names that its field, the game's rows and its prefixes give.
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
		// An expression such as @[…] refers to the constants it uses.
		for _, token := range jomini.ListConstants(text, span.Start) {
			context.References = append(context.References,
				Reference{Database: game.Constant, Name: "@" + walker.tree.Text(token), Span: token})
		}
		return
	default:
		if call, ok := ParseCall(walker.tree, current.Value); ok {
			context.ValueGroup = CategoryCall
			context.References = append(context.References, walker.schema.ListCallReferences(call, context.Datacontext)...)
			return
		}
		walker.resolveValueReferences(context, statement, level, span)
	}
	if len(context.References) > before {
		context.ValueGroup = CategoryReference
	}
}

// resolveValueReferences resolves the references that a plain value makes. The databases it may name are: the one its
// field names, with the databases nested in it where the field says so, unless the value is a link; the one the game's
// rows give for its key path, else the one its shape
// gives; a coat's template list; the events of a script file; a script value; and the databases of the prefixes it is
// written with. If a row puts a file in a folder and the value names none, the folder is added. A database named
// twice is referred to once. The reference is inferred only if the field's claim alone makes it. The index resolves
// which event an id names and whether its namespace is declared.
func (walker *walker) resolveValueReferences(context *Context, statement int32, level int, span jomini.Span) {
	text := walker.tree.Text(span)
	if text == "" || strings.ContainsAny(text, "[$") {
		return
	}
	var databases []string
	claim := ""
	if field := context.Field; field != nil && field.Value.Value.Type == ValueReference && !walker.isLink(text) {
		claim = field.Value.Value.Database
	}
	if claim != "" {
		databases = append(databases, claim)
	}
	// a field whose values are a database's or its nested databases', as a language's or a dialect's, may name either
	if database := walker.schema.Databases[claim]; database != nil && context.Field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}
	placed, folder := "", ""
	if database, where, ok := walker.findValueDatabase(context, statement, level); ok {
		databases, placed, folder = append(databases, database), database, where
	}
	// A value with a space is literal text, since a message key has none.
	if database := game.Shapes[findShape(context)].Values; database != "" && placed == "" && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}
	if !walker.tree.Statements[statement].Tag.IsEmpty() && walker.database != nil && walker.database.Name == game.Coat.Database {
		databases = append(databases, game.Coat.ListsDatabase)
	}
	if walker.schema.Events != "" && walker.file.Format == files.FormatScript && jomini.IsEventID(text) {
		databases = append(databases, walker.schema.Events)
	}
	// The claim is the first database. It is inferred if it rests on the game's own files and no other rule makes it.
	inferred := claim != "" && !context.Field.Value.Source.CanAccuse() && !slices.Contains(databases[1:], claim)
	for position, database := range databases {
		if !slices.Contains(databases[:position], database) {
			name := text
			if database == placed && folder != "" && !strings.Contains(text, "/") {
				name = folder + "/" + text
			}
			context.References = append(context.References,
				Reference{Database: database, Name: name, Span: span, Inferred: position == 0 && inferred})
		}
	}
	if value, ok := walker.findScriptValue(context, level, span); ok {
		context.References = append(context.References, value)
	}
	context.References = append(context.References, walker.listPrefixed(text, span.Start)...)
}

// findScriptValue returns the reference that a value makes to a script value. The script value is the last link of a
// chain of links that is no event id, or a value written at a math operator of a value block, or at a field that a
// source declared takes a script value. It must be an identifier that is no command, no link and no yes-or-no word.
// It returns false for any other value, and for a game without script values.
func (walker *walker) findScriptValue(context *Context, level int, span jomini.Span) (Reference, bool) {
	text := walker.tree.Text(span)
	links := jomini.SplitChain(text)
	last := links[len(links)-1]
	enclosing, key := walker.frames[level-1], walker.frames[level].label
	chained := len(links) > 1 && !jomini.IsEventID(text) &&
		!slices.ContainsFunc(links[:len(links)-1], func(link string) bool { return !walker.isLink(link) })
	operated := game.ClassifyKey(findShape(enclosing.context), strings.ToLower(enclosing.label), strings.ToLower(key)) == "math"
	valued := context.Field != nil && context.Field.Value.Value.Values
	_, declared := walker.schema.FindCommand(last)
	if walker.schema.Values == "" || !chained && !operated && !valued || !jomini.IsIdentifier(last) || declared ||
		walker.isLink(last) || slices.Contains(game.Bools, last) {
		return Reference{}, false
	}
	start := span.End - len(last)
	return Reference{Database: walker.schema.Values, Name: last, Span: jomini.Span{Start: start, End: span.End}}, true
}

// findValueDatabase returns the database that the value of a statement names, by where it is written. That is a key
// path that the game's rows give for the definition's database, a key its database says also names its definitions
// (unless the statement defines that name itself), or a key that a GUI file says names a template, a texture, a font
// or a text. It also returns the folder that the row puts a file in when the value names none, or "".
func (walker *walker) findValueDatabase(context *Context, statement int32, level int) (string, string, bool) {
	key := walker.frames[level].label
	keys := walker.frames[level-1].path
	if key != "" {
		keys = append(keys[:len(keys):len(keys)], key)
	}
	for _, rows := range [][]game.Reference{game.GUI.References, game.References, walker.row.References} {
		for _, row := range rows {
			if row.Database == "" && walker.file.Format == files.FormatGUI && key == row.KeyPath ||
				row.Database != "" && row.Database == context.Database && game.MatchKeyPath(row.KeyPath, keys) {
				return cmp.Or(row.Target, row.Database), row.Folder, true
			}
		}
	}
	database := walker.schema.Databases[context.Database]
	if database != nil && slices.Contains(database.Row.AliasKeys, key) &&
		(!context.Defines || !context.Definition.Alias) {
		return database.Name, "", true
	}
	return "", "", false
}

// listPrefixed returns a reference for each name that a chain of links writes after a prefix that has a database. A
// name ends at the database's separator. The offset is where the chain starts in the file.
func (walker *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	offset := 0
	for _, link := range jomini.SplitChain(chain) {
		if prefix, label, ok := jomini.SplitPrefixed(link); ok {
			if database, exists := walker.schema.Prefixes[prefix]; exists && database != "" {
				if owner := walker.schema.Databases[database]; owner != nil && owner.Row.Separator != "" {
					label, _, _ = strings.Cut(label, owner.Row.Separator)
				}
				start := base + offset + len(prefix) + 1
				references = append(references, Reference{Database: database, Name: label, Span: jomini.Span{Start: start, End: start + len(label)}})
			}
		}
		offset += len(link) + 1
	}
	return references
}

// buildPaths builds the keys that lead to the children of a statement. A definition of the file's own database starts
// the keys anew.
func (walker *walker) buildPaths(level int, label string, begins bool) {
	walker.frames[level].path = walker.frames[level-1].path
	if label != "" {
		walker.frames[level].path = slices.Concat(walker.frames[level-1].path, []string{label})
	}
	if begins {
		walker.frames[level].path = nil
	}
}

// isLead reports whether a value written alone is a word that makes the statement after it a definition or a call.
// Such a word is one that a database declares its definitions with. In a GUI file it is also one that declares or
// calls definitions.
func (walker *walker) isLead(text string) bool {
	if walker.declaring[text] != "" {
		return true
	}
	return walker.file.Format == files.FormatGUI &&
		(slices.Contains(game.GUI.Declares, text) || text != game.AnyCall && len(walker.schema.Callees[text]) > 0)
}

// FindDeclaration returns how the dumps declare a command written in the block that this context holds. It is the
// first declaration that the block's shape allows. An effect never counts inside a trigger block. It returns false
// if the block takes the command under no declaration.
func (context *Context) FindDeclaration(command *Command) (string, bool) {
	allowed := game.Shapes[findShape(context)].Allows
	for _, declaration := range command.Declared {
		if slices.Contains(allowed, declaration) && (declaration != game.DeclaredEffect || !context.holdsConditions()) {
			return declaration, true
		}
	}
	return "", false
}

// holdsConditions reports whether the statements in the block of a context are inside a trigger block. They are
// when the block is triggers, or sits below triggers and is no value block.
func (context *Context) holdsConditions() bool {
	return context.Shape == game.ShapeTriggers || context.Conditional && context.Shape != game.ShapeValue
}
