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
	Path        []string    // the keys that lead to the block holding the statement, from its definition or the top
	Label       string      // what the statement is written under: its key, or the tag of a block with no key
	Shape       string      // what kind of block the statement holds, a game.Shapes key; "" if unknown
	Conditional bool        // the statement is inside a trigger block, so a command here must be a trigger
	Preview     bool        // the statement is inside a block that is shown in the tooltip and never run
	Category    Category    // the key's color category
	Command     *Command    // the command the key names; nil where the key matched none
	// Callee is the scripted definition the key calls, where the walk knows it; nil for any other key.
	Callee      *ScriptedDefinition
	Declaration string   // how the dumps declare that command where the key is in this block: a game.Declared word
	ValueGroup  Category // the value's color category
	// Caller is the word a call in this statement's block resolves by, where a keyword gives one; "" means the block's
	// shape's.
	Caller      string
	Datacontext string   // a GUI block's data context type; "" if none
	Parent      *Context // the context of the block that holds the statement, the file's own at the top
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
	namespaces []int32                       // the statements that declare an event namespace
	declaring  map[string]string             // the database a word before a statement declares a definition of, inline
	inline     map[string]ScriptedDefinition // each name the file declared for itself so far
	mods       map[string]ScriptedDefinition // the mods' scripted definitions when the walk began
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
		mods: schema.loadMods(),
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
		walker.buildPaths(level, label, defines && walker.database != nil && definition.Database == walker.database.Name)
	}
	if walker.file.Format == files.FormatGUI && walker.tree.Statements[statement].IsBlock() {
		walker.resolveDatacontext(context, statement, parent.Datacontext)
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

// findDefinitionShape returns the shape of a definition's block in a database, or fields if nothing says.
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

// findRootScope returns the scope a definition's script starts in, or "" if nothing says.
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

// findScriptDefinition returns the definition a statement of a script file makes. It returns false if it makes none.
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
		name, ok := walker.findNameSpan(statement, database.Row.NameKey)
		if !ok {
			return Definition{}, false
		}
		span, label = name, walker.tree.Text(name)
	case "call":
		name, ok := walker.findNameSpan(statement, database.Row.NameKey)
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

// findNameSpan returns the span of the value of the given key in a statement's block, without quotes. It returns false
// if the block writes no such key.
func (walker *walker) findNameSpan(statement int32, nameKey string) (jomini.Span, bool) {
	child, ok := walker.tree.FindChild(statement, nameKey)
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

// findGUIDefinition returns the definition a statement of a GUI file makes. It returns false if it makes none.
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

// findWindowLabel returns the name of a window at the top of a GUI file, else its file's name if it is the file's
// only window. It returns "" otherwise.
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

// makeGUIDefinition returns a definition in a GUI database. If that database's definitions are local to their file,
// the rest of the file can call the definition. It returns false if the label is empty.
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

// findNested returns the definition a statement makes in a nested database. It returns false if it makes none.
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
		definition := Definition{Database: database, Name: label, Span: span, Statement: statement}
		if nesting.NameKey != "" {
			name, ok := walker.findNameSpan(statement, nesting.NameKey)
			if !ok {
				continue
			}
			definition.Name, definition.Span = walker.tree.Text(name), name
		}
		if holder := walker.frames[level-1].context.Definition; holder != nil {
			definition.Name = walker.schema.JoinNested(database, holder.Name, definition.Name)
		}
		return definition, true
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

// isNested reports whether a key in a definition's block defines a nested definition rather than a field.
func (walker *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if walker.database != nil && walker.database.Documented[key] {
		return false
	}
	fits, _ := path.Match(nesting.Keys, key)
	return fits || !same && nesting.Keys == ""
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
		return match{category: CategoryLink}, walker.schema.isLink(key)
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

// findCallee returns the scripted definition a key calls, and the word it was found through. It returns false if
// there is none.
func (walker *walker) findCallee(caller, key string) (ScriptedDefinition, string, bool) {
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

// matchCommand returns the command a key names. It returns false if the block does not take that command.
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

// findLinkCommand returns the command of the first link of a chain, or the command of its prefix if it has one. It
// returns nil if the dumps declare none.
func (walker *walker) findLinkCommand(key string) *Command {
	first := strings.ToLower(jomini.SplitChain(key)[0])
	if prefix, _, ok := jomini.SplitPrefixed(first); ok {
		first = prefix
	}
	return walker.schema.Commands[first]
}

// resolveMatch sets the context of a keyed statement from the rule its key fits.
func (walker *walker) resolveMatch(context *Context, parent *Context, fit match, keyword *game.Keyword) {
	rule := game.KeyRules[fit.rule]
	context.Field = walker.resolveField(rule.Fields, parent, fit)
	context.Shape = cmp.Or(walker.resolveShape(rule.Shape, parent, context.Field, fit), game.Shapes[findShape(parent)].Blocks)
	context.Category = fit.category
	context.Command, context.Declaration, context.Callee = fit.command, fit.declaration, fit.callee
	if keyword != nil {
		if keyword.Shape == "enclosing" {
			context.Shape = findShape(parent)
		} else {
			context.Shape = cmp.Or(keyword.Shape, context.Shape)
		}
		context.Caller = keyword.Caller
		context.Category = CategoryKeyword
		context.Preview = context.Preview || keyword.Preview
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

// resolveShape returns the shape of a block, as the decider of a key rule says. A command with arguments takes the
// shape stated for them, else the arguments shape. Any other command takes the kind it is declared as.
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

// resolveScope sets the scope inside a block, as its key's rule says.
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

// resolveCallReferences resolves the references a key makes by the rule it fits.
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

// resolveChain returns the scope a chain of links leads to. It is unknown if a link cannot be followed.
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
	for _, saved := range slices.Backward(walker.saved) {
		if saved.Name == label {
			return saved.Type
		}
	}
	return ""
}

// resolveSaved resolves a statement that saves a name of one of game.Links, or reads one back. A save makes the name a
// definition: a command that saves, or an argument of a scripted call whose body saves it. A read makes it a
// reference. A saved scope is also added to the scopes the definition saved. A name with a parameter is none yet.
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
	if inBlock || isArgument(parent, key) {
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

// findLinked returns the link of a key in a table of game.LinkMakers' shape: the key is a command the table holds, or a
// key in the block of one, the holder given. It reports whether the key is in the holder's block, and returns false if
// the table holds neither.
func findLinked(table map[string]string, holder, key string) (link string, inBlock, ok bool) {
	if link, ok := table[key]; ok {
		return link, false, true
	}
	link, ok = table[strings.ToLower(holder)+"/"+key]
	return link, true, ok
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

// resolveValueReferences resolves the references a plain value makes. A value with a parameter or a bracket makes
// none, and so does a name the statement saves or reads back.
func (walker *walker) resolveValueReferences(context *Context, statement int32, level int, span jomini.Span) {
	text := walker.tree.Text(span)
	if text == "" || strings.ContainsAny(text, "[$") || isSavedName(context, span) {
		return
	}
	var databases []string
	claim := ""
	if field := context.Field; field != nil && field.Value.Value.Type == ValueReference && !walker.schema.isLink(text) {
		claim = field.Value.Value.Database
	}
	if claim != "" {
		databases = append(databases, claim)
	}
	// Where the field says so, the value may also name a database nested in the claimed one, as a dialect is nested in
	// its language.
	if database := walker.schema.Databases[claim]; database != nil && context.Field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}
	placed, folder := "", ""
	if database, where, ok := walker.findValueDatabase(context, level); ok {
		databases, placed, folder = append(databases, database), database, where
	}
	// A value with a space is literal text, since a message key has none.
	if database := game.Shapes[findShape(context)].Values; database != "" && placed == "" && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}
	if !walker.tree.Statements[statement].Tag.IsEmpty() && walker.database != nil && walker.database.Name == game.Coat.Database {
		databases = append(databases, game.Coat.ListsDatabase)
	}
	// A value shaped like an event id may name an event, unless its field claims another database.
	if walker.schema.Events != "" && walker.file.Format == files.FormatScript && jomini.IsEventID(text) &&
		(claim == "" || claim == walker.schema.Events) {
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

// findScriptValue returns the reference a value makes to a script value. It returns false if it makes none.
func (walker *walker) findScriptValue(context *Context, level int, span jomini.Span) (Reference, bool) {
	text := walker.tree.Text(span)
	links := jomini.SplitChain(text)
	last := links[len(links)-1]
	enclosing, key := walker.frames[level-1], walker.frames[level].label
	chained := len(links) > 1 && !jomini.IsEventID(text) &&
		!slices.ContainsFunc(links[:len(links)-1], func(link string) bool { return !walker.schema.isLink(link) })
	operated := game.ClassifyKey(findShape(enclosing.context), strings.ToLower(enclosing.label), strings.ToLower(key)) == "math"
	valued := context.Field != nil && context.Field.Value.Value.Values
	_, declared := walker.schema.FindCommand(last)
	if walker.schema.Values == "" || !chained && !operated && !valued || !jomini.IsIdentifier(last) || declared ||
		walker.schema.isLink(last) || slices.Contains(game.Bools, last) {
		return Reference{}, false
	}
	start := span.End - len(last)
	return Reference{Database: walker.schema.Values, Name: last, Span: jomini.Span{Start: start, End: span.End}}, true
}

// findValueDatabase returns the database a statement's value names by where it is written, and the folder of a file
// it names, or "". It returns false if it names none.
func (walker *walker) findValueDatabase(context *Context, level int) (string, string, bool) {
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

// listPrefixed returns the references a chain of links makes after prefixes that have a database.
func (walker *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	offset := 0
	for _, link := range jomini.SplitChain(chain) {
		if prefix, label, ok := jomini.SplitPrefixed(link); ok {
			if database, exists := walker.schema.Prefixes[prefix]; exists && database != "" {
				references = append(references, walker.listLabelReferences(database, label, base+offset+len(prefix)+1)...)
			}
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

// isLead reports whether a value written alone makes the statement after it a definition or a call.
func (walker *walker) isLead(text string) bool {
	if walker.declaring[text] != "" {
		return true
	}
	return walker.file.Format == files.FormatGUI &&
		(slices.Contains(game.GUI.Declares, text) || text != game.AnyCall && len(walker.schema.Callees[text]) > 0)
}

// FindDeclaration returns how the dumps declare a command in this context's block. It returns false if the block does
// not take it.
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
