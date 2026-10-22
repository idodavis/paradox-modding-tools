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

// A walker is one descent over one file. It keeps what the descent needs from one statement to the next: the
// context of each level, the keys that lead to the statement, the names the file declared inline, and the scopes
// the definition saved.
type walker struct {
	schema     *Schema
	row        *game.Game
	tree       *jomini.Tree
	file       files.File
	visitor    Visitor
	offset     int // the byte offset the walker is restricted to; -1 for the whole file
	rel        string
	database   *Database // the database of the file's folder; nil for a file of no folder
	fileType   game.FileType
	nestings   []game.Nesting
	namespaces []int32                       // the statements that declare an event namespace
	declaring  map[string]*Database          // the database a word before a statement declares a definition of, inline
	inline     map[string]ScriptedDefinition // each name the file declared for itself so far
	frames     []frame                       // frames[0] is the file's; frames[n] is that of the statements n levels down
	saved      []Saved
}

// A frame is what the walk keeps for the statements one level down: their context, and of the statement last read
// there its label and where it is written, the keys that lead to its children, and whether its children are written
// as definitions.
type frame struct {
	context *Context
	label   string
	span    jomini.Span
	path    []string // from the enclosing definition; from the top where the file names none
	wraps   bool
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
}

// The shapes the walker itself names. The other shapes are the keys of game.Shapes.
const (
	shapeUnknown   = "unknown"
	shapeArguments = "arguments"
	shapeTriggers  = "triggers"
	shapeEffects   = "effects"
	shapeValue     = "value"
)

// Walk goes through a file's tree once and calls the visitor at every statement. At each one it decides what the
// schema and the game's rules say about it: the field, the scope, the shape, the definition and the references.
// Nothing else in the app walks a file.
func (schema *Schema) Walk(tree *jomini.Tree, file files.File, visitor Visitor) {
	schema.newWalker(tree, file, visitor, -1).walkChildren(-1, 1, false)
}

// WalkTo is Walk restricted to one position: it only enters statements that cover the given byte offset, so the
// visitor sees the path to that place and nothing else.
func (schema *Schema) WalkTo(tree *jomini.Tree, file files.File, offset int, visitor Visitor) {
	schema.newWalker(tree, file, visitor, offset).walkChildren(-1, 1, false)
}

// ListNamespaces returns the event namespaces a file declares, in the order it declares them. A file may declare
// several; a file that declares none gives an empty list.
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
	for statement := range tree.Children(-1) {
		if tree.Text(tree.Statements[statement].Key) == game.EventNamespaceKey {
			statements = append(statements, statement)
		}
	}
	return statements
}

// newWalker returns the walker of one file. The file's own context holds the one definition of a file that its
// database names by its own name. Only a file of the events database declares definitions inline.
func (schema *Schema) newWalker(tree *jomini.Tree, file files.File, visitor Visitor, offset int) *walker {
	row := game.LookupGame(schema.Game)
	if row == nil {
		row = &game.Game{}
	}
	walker := &walker{
		schema: schema, row: row, tree: tree, file: file, visitor: visitor, offset: offset,
		declaring: map[string]*Database{}, inline: map[string]ScriptedDefinition{},
	}
	walker.rel = trimStage(row, file.Rel)
	walker.fileType = game.FileTypes[strings.ToLower(path.Ext(walker.rel))]
	if file.Format == files.FormatScript || file.Format == files.FormatTable {
		walker.database, _ = schema.FindDatabase(file.Rel)
	}
	walker.nestings = row.FindNestings(walker.rel)
	root := &Context{Shape: shapeUnknown}
	if database := walker.database; database != nil {
		if database.Row.Houses == "events" {
			walker.namespaces = listNamespaces(tree)
			for _, declared := range schema.Databases {
				for _, key := range declared.Row.DeclaringKeys {
					walker.declaring[key] = declared
				}
			}
		}
		if database.Row.NamedBy == "file" && len(tree.Statements) > 0 {
			stem := strings.TrimSuffix(path.Base(walker.rel), path.Ext(walker.rel))
			walker.resolveDefinition(root, Definition{Database: database.Name, Name: stem, Local: database.Callable.Local})
		}
	}
	walker.frames = []frame{{context: root, wraps: true}}
	return walker
}

// walkChildren walks the children of a statement one level down, or the top statements for -1. A word that
// declares a definition, or calls one, applies to the statement after it. A walk to an offset still resolves the
// statements before it, so that it knows what they saved, but does not offer them to the visitor; the walk of a
// statement already silent is silent.
func (walker *walker) walkChildren(parent int32, level int, silent bool) {
	for len(walker.frames) <= level {
		walker.frames = append(walker.frames, frame{context: &Context{}})
	}
	pending := ""
	for child := range walker.tree.Children(parent) {
		statement := walker.tree.Statements[child]
		offered := !silent && (walker.offset < 0 || statement.Span.Contains(walker.offset))
		if offered || walker.offset >= 0 && statement.Span.End <= walker.offset {
			walker.walkStatement(child, level, pending, offered)
		}
		pending = ""
		if text := walker.tree.Text(statement.Value); statement.IsListed() && !statement.IsBlock() && walker.isLead(text) {
			pending = text
		}
	}
}

// walkStatement resolves one statement, offers it to the visitor if it is offered, and walks its block if the
// visitor asks.
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

// resolve fills the context of a statement from the one of the block that holds it: the definition it makes, else
// the rule its key fits. It then resolves what the value refers to. A statement is inside a trigger block below a
// triggers-shaped block, until a value block, which computes a number, begins.
func (walker *walker) resolve(context *Context, statement int32, level int, pending string) {
	parent := walker.frames[level-1].context
	*context = Context{
		Database: parent.Database, Definition: parent.Definition, Scope: parent.Scope, Root: parent.Root,
		Previous: parent.Previous, Saved: walker.saved,
		Conditional: parent.Shape == shapeTriggers || parent.Conditional && parent.Shape != shapeValue,
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

// resolveStatement fills the context of a statement that defines nothing. A keyed statement is resolved by the rule
// its key fits. A block with no key is resolved as the block that holds it. At the top of a file whose database
// names nothing, a block is a definition's block with no name, and in a file that is one definition the first
// statement is where it begins.
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
	}
	if database := walker.database; level == 1 && database != nil {
		switch database.Row.NamedBy {
		case "nothing":
			context.Database, context.Field, context.Shape = database.Name, database.Definition, walker.findDefinitionShape(database)
		case "file":
			context.Defines = statement == 0
		}
	}
}

// resolveLead resolves the references of a call written as a word and a label, such as a GUI file's `blockoverride
// "label"`: the label in each database the word calls.
func (walker *walker) resolveLead(context *Context, lead, label string, span jomini.Span) {
	for _, database := range walker.schema.Callees[lead] {
		context.References = append(context.References, Reference{Database: database, Name: label, Span: span, Caller: lead})
	}
	if lead != "" && len(walker.schema.Callees[lead]) > 0 {
		context.Category = CategoryCall
	}
}

// findLabel returns what a statement is written under: its key, or the tag of a block that has no key. It returns
// an empty label and span for a value written alone.
func (walker *walker) findLabel(current jomini.Statement) (string, jomini.Span) {
	switch {
	case !current.Key.IsEmpty():
		return walker.tree.Unquoted(current.Key), current.Key
	case !current.Tag.IsEmpty() && current.IsBlock():
		return walker.tree.Unquoted(current.Tag), current.Tag
	}
	return "", jomini.Span{}
}

// resolveDefinition makes the context of a definition's own statement. Its block holds the fields of the
// definition's database, in the shape the database gives it, and its scope begins at the definition's root.
func (walker *walker) resolveDefinition(context *Context, definition Definition) {
	database := walker.schema.Databases[definition.Database]
	*context = Context{
		Database: definition.Database, Definition: &definition, Defines: true, Category: CategoryDefinition,
		Shape: walker.findDefinitionShape(database), References: context.References[:0],
	}
	if database != nil {
		context.Field = database.Definition
	}
	context.Root = walker.findRootScope(database, definition)
	context.Scope = context.Root
	walker.saved = nil
}

// findDefinitionShape returns the shape a definition's block has in a database: the callable's, else the row's,
// else the file type's, else the shape named by the first word that calls it, else fields.
func (walker *walker) findDefinitionShape(database *Database) string {
	if database == nil {
		return cmp.Or(walker.fileType.Shape, "fields")
	}
	shape := cmp.Or(database.Callable.Shape, database.Row.Shape, walker.fileType.Shape)
	if callers := database.Callable.Callers; shape == "" && len(callers) > 0 {
		if _, ok := game.Shapes[callers[0]]; ok {
			shape = callers[0]
		}
	}
	return cmp.Or(shape, "fields")
}

// findRootScope returns the scope a definition's script starts in. An event writes it, else the game's default
// holds; an on_action has the one the dumps give; any other has the one its database's docs give. It returns ""
// where nothing says.
func (walker *walker) findRootScope(database *Database, definition Definition) string {
	if database == nil {
		return ""
	}
	switch database.Row.Houses {
	case "events":
		events := walker.row.Events
		scope := walker.findChildValue(definition.Statement, events.ScopeKey)
		if scope == "" && events.TypeKey != "" {
			scope = strings.TrimSuffix(walker.findChildValue(definition.Statement, events.TypeKey), events.TypeSuffix)
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

// findChildValue returns the unquoted value of the child of a statement with the given key, or "" if there is none
// or the key is empty.
func (walker *walker) findChildValue(statement int32, key string) string {
	if key == "" {
		return ""
	}
	if child := walker.tree.FindChild(statement, key); child >= 0 {
		return walker.tree.Unquoted(walker.tree.Statements[child].Value)
	}
	return ""
}

// findDefinition returns the definition that a statement makes, if it makes one: a constant, a name a nesting
// defines, or what the file's format or database says is defined here.
func (walker *walker) findDefinition(statement int32, level int, pending string) (Definition, bool) {
	current := walker.tree.Statements[statement]
	if label := walker.frames[level].label; level == 1 && strings.HasPrefix(label, "@") {
		return Definition{Database: game.Constant, Name: label, Span: current.Key, Statement: statement, Local: true}, true
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
// names its definitions. Only a statement written where definitions are written makes one. A table file's rows are
// the index's, so the walk makes none there. A name declared inline is local to its file.
func (walker *walker) findScriptDefinition(statement int32, level int, pending string) (Definition, bool) {
	label, span := walker.frames[level].label, walker.frames[level].span
	if owner := walker.declaring[pending]; pending != "" && owner != nil && label != "" {
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
		child := walker.tree.FindChild(statement, database.Row.NameKey)
		if child < 0 {
			return Definition{}, false
		}
		label, span = walker.tree.Unquoted(walker.tree.Statements[child].Value), walker.tree.Statements[child].Value
	}
	if label == "" || slices.Contains(database.Row.WrapperKeys, label) {
		return Definition{}, false
	}
	mode, label := walker.row.SplitMode(label)
	if database.Row.Houses == "events" && !walker.isEvent(label, statement) {
		return Definition{}, false
	}
	return Definition{
		Database: database.Name, Name: label, Span: span, Statement: statement, Mode: mode,
		Priority: walker.findPriority(database, statement), Local: database.Callable.Local,
	}, true
}

// isEvent reports whether a label is the name of an event of the file: a namespace the file declares above the
// statement, a dot and an id.
func (walker *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(walker.namespaces, func(declaring int32) bool {
		namespace := walker.tree.Unquoted(walker.tree.Statements[declaring].Value)
		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}

// findPriority returns the override priority a definition writes under the key its database's precedence names, or
// 0.
func (walker *walker) findPriority(database *Database, statement int32) int {
	priority, _ := strconv.Atoi(walker.findChildValue(statement, database.Precedence.PriorityKey))
	return priority
}

// findGUIDefinition returns the definition that a statement of a GUI file makes: the one the word before it
// declares, a block named by one of its keys, or a window at the top of the file named by its name.
func (walker *walker) findGUIDefinition(statement int32, level int, pending string) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	switch nameKey := game.GUI.NameKeyOf[label]; {
	case slices.Contains(game.GUI.Declares, pending) && label != "":
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+pending, label, span, statement)
	case nameKey != "":
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+label, walker.findChildValue(statement, nameKey), span, statement)
	case level == 1 && !current.Key.IsEmpty() && current.IsBlock():
		return walker.makeGUIDefinition(game.GUI.WidgetDatabase, walker.findChildValue(statement, game.GUI.WidgetKey), span, statement)
	}
	return Definition{}, false
}

// makeGUIDefinition returns a definition in a GUI database. One of a database that answers only in its own file is
// callable in the rest of the file. It returns false for a label that is empty.
func (walker *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Definition, bool) {
	owner := walker.schema.Databases[database]
	local := owner != nil && owner.Callable.Local
	if local && label != "" {
		walker.inline[label] = makeScripted(owner, walker.tree, statement)
	}
	return Definition{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
}

// findAssetDefinition returns the definition that a top block of an asset or font file makes, in the database
// named for the file's extension and the block's key, named by the key the file type names.
func (walker *walker) findAssetDefinition(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	key, span := walker.frames[level].label, walker.frames[level].span
	if level != 1 || key == "" || !current.IsBlock() {
		return Definition{}, false
	}
	label := key
	if walker.fileType.NameKey != "" {
		label = walker.findChildValue(statement, walker.fileType.NameKey)
	}
	database := strings.TrimPrefix(strings.ToLower(path.Ext(walker.rel)), ".") + "." + key
	return Definition{Database: database, Name: label, Span: span, Statement: statement}, label != ""
}

// findNested returns the definition that a statement makes in a database nested in another's. It returns false for
// a statement that is not written under the key path of a nesting, or whose key the docs describe as a field. The
// label is the statement's key, the tag of a block with none, or a value written alone.
func (walker *walker) findNested(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	if label == "" && current.IsListed() && !current.IsBlock() {
		label, span = walker.tree.Unquoted(current.Value), current.Value
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

// findNestedDatabase returns the name of the database a nesting defines in, and whether that is the holder's own.
func (walker *walker) findNestedDatabase(nesting game.Nesting) (string, bool) {
	if nesting.Database != "" {
		return game.ToDatabase(nesting.Parent) + "." + nesting.Database, false
	}
	if walker.database == nil {
		return "", true
	}
	return walker.database.Name, true
}

// isNested reports whether a statement written in a definition's own block under a label defines a nested
// definition of a nesting: not where the docs describe the label as a field, and in the holder's own database only
// where it fits the nesting's keys.
func (walker *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if walker.database != nil && walker.database.Documented[key] {
		return false
	}
	fits, _ := path.Match(nesting.Keys, key)
	return !same || fits
}

// resolveKey fills the context of a keyed statement by the first rule its key fits in the block that holds it. A
// keyword of the language adjusts what the rule gave, and is never a call.
func (walker *walker) resolveKey(context *Context, statement int32, level int) {
	parent := walker.frames[level-1].context
	key, span := walker.frames[level].label, walker.frames[level].span
	if jomini.IsParameter(key) {
		context.Category, context.Shape, context.Scope = CategoryParameter, findShape(parent), parent.Scope
		return
	}
	var keyword *game.Keyword
	if row, ok := game.LookupKeyword(findShape(parent), strings.ToLower(walker.frames[level-1].label), strings.ToLower(key)); ok {
		keyword = &row
	}
	fit := walker.findMatch(parent, key)
	// a key that starts an iterator and is no word of its own runs its block in the list's item scope
	if iterator, ok := game.MatchListIterator(key); ok && keyword == nil &&
		(fit.command != nil || fit.category == CategoryCall && fit.callee == nil && walker.schema.Lists != "") {
		fit.iterator = &iterator
	}
	switch {
	case fit.category == CategoryLink:
		fit.target = walker.resolveChain(key, parent)
	case fit.command != nil:
		fit.target = fit.command.Target
	}
	walker.resolveMatch(context, parent, fit, keyword)
	if keyword == nil {
		walker.resolveCallReferences(context, fit, key, span)
	}
	if keyword != nil && (fit.rule == "" || context.Field == nil) {
		context.Field = parent.Field.findPatternChild(key)
	}
	walker.resolveSaved(context, statement, level)
}

// findShape returns the shape of the block a context holds, which is unknown where the walk decided none.
func findShape(context *Context) string {
	return cmp.Or(context.Shape, shapeUnknown)
}

// findMatch returns the first of the rules of a block's shape that the key fits. It returns the empty match if none
// does.
func (walker *walker) findMatch(parent *Context, key string) match {
	for _, rule := range game.Shapes[findShape(parent)].Rules {
		if fit, ok := walker.matchRule(rule, parent, key); ok {
			fit.rule = rule
			return fit
		}
	}
	return match{}
}

// matchRule reports whether a key fits one rule of its block's shape, and returns what it fit.
func (walker *walker) matchRule(rule string, parent *Context, key string) (match, bool) {
	holder := parent.Field
	child := holder.findChild(key)
	switch rule {
	case "known-call":
		callee, caller, ok := walker.findCallee(game.Shapes[findShape(parent)].Caller, key)
		return match{category: CategoryCall, callee: &callee, caller: caller}, ok
	case "declared":
		return match{category: CategoryField, child: child}, child != nil &&
			child.Source != game.SourceNone && child.Source != game.SourceVanilla
	case "documented":
		database := walker.schema.Databases[parent.Database]
		return match{category: CategoryField, child: child}, database != nil && database.Documented[key]
	case "command":
		return walker.matchCommand(parent, key)
	case "operator":
		return match{}, game.IsOperator(key)
	case "link":
		return match{category: CategoryLink}, walker.isLink(key)
	case "observed":
		return match{category: CategoryField, child: child}, child != nil && child.Source == game.SourceVanilla
	case "field":
		child = holder.findPatternChild(key)
		return match{category: CategoryField, child: child}, child != nil
	case "field-or-call":
		if child = holder.findPatternChild(key); child != nil {
			return match{category: CategoryField, child: child}, true
		}
		return walker.matchCall(parent, key, walker.hasCommands(holder))
	case "call":
		return walker.matchCall(parent, key, true)
	}
	return match{}, false
}

// matchCall returns a call by the shape's caller, if the call is allowed and the key is an identifier.
func (walker *walker) matchCall(parent *Context, key string, allowed bool) (match, bool) {
	return match{category: CategoryCall, caller: game.Shapes[findShape(parent)].Caller}, allowed && jomini.IsIdentifier(key)
}

// findCallee returns the scripted definition that a key calls through the given word, and the word it was found
// through: one the file declared for itself, else one the game defines. The word game.AnyCall tries every one. It
// returns false if no definition has the name.
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

// lookupCallee returns the scripted definition of a name called through a word. A name written in another case
// finds the definition of a database that matches names without regard to case, and no other.
func (walker *walker) lookupCallee(caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := walker.schema.ScriptedDefinitions[caller+":"+key]; ok {
		return callee, true
	}
	callee, ok := walker.schema.ScriptedDefinitions[caller+":"+strings.ToLower(key)]
	database := walker.schema.Databases[callee.Database]
	return callee, ok && database != nil && database.Callable.FoldsCase
}

// matchCommand returns the command that a key names, if the block's shape holds commands of the kind it is declared
// as. A block inside triggers holds no effect.
func (walker *walker) matchCommand(parent *Context, key string) (match, bool) {
	shape := findShape(parent)
	command, ok := walker.schema.Commands[key]
	if lower := strings.ToLower(key); !ok && lower != key && game.ClassifyKey(shape, "", key) != "" {
		command, ok = walker.schema.Commands[lower]
	}
	if !ok {
		return match{}, false
	}
	for _, declaration := range command.Declared {
		if slices.Contains(game.Shapes[shape].Allows, declaration) && (declaration != game.DeclaredEffect || !parent.Conditional) {
			return match{category: CategoryCommand, command: command, declaration: declaration}, true
		}
	}
	return match{}, false
}

// isLink reports whether a key starts with a scope link: one the dumps declare as a link, or a prefix that takes a
// name after a colon. A link is matched without regard to case, as the games write ROOT and root alike.
func (walker *walker) isLink(key string) bool {
	first := strings.ToLower(jomini.SplitChain(key)[0])
	if command := walker.schema.Commands[first]; command != nil && slices.Contains(command.Declared, game.DeclaredLink) {
		return true
	}
	prefix, _, ok := jomini.SplitPrefixed(first)
	if _, exists := walker.schema.Prefixes[prefix]; ok && exists {
		return true
	}
	wild := walker.schema.Commands[prefix]
	return ok && wild != nil && wild.Wild
}

// hasCommands reports whether the vanilla files wrote any command as a child of a field.
func (walker *walker) hasCommands(field *Field) bool {
	if field == nil {
		return false
	}
	for key := range field.Children {
		if command := walker.schema.Commands[key]; command != nil &&
			(slices.Contains(command.Declared, game.DeclaredEffect) || slices.Contains(command.Declared, game.DeclaredTrigger)) {
			return true
		}
	}
	return false
}

// resolveMatch sets the context of a keyed statement from the rule its key fit: the field its block's keys come
// from, the shape of the block, the scope inside it, and the key's category. A keyword of the language is a keyword
// by category and adjusts the shape and the caller the rule gave. An iterator gives the shape of its block.
func (walker *walker) resolveMatch(context *Context, parent *Context, fit match, keyword *game.Keyword) {
	rule := game.KeyRules[fit.rule]
	context.Field = walker.resolveField(rule.Fields, parent, fit)
	context.Shape = walker.resolveShape(rule.Shape, parent, context.Field, fit)
	context.Category, context.Caller = fit.category, fit.caller
	if keyword != nil {
		if keyword.Shape == "enclosing" {
			context.Shape = findShape(parent)
		} else {
			context.Shape = cmp.Or(keyword.Shape, context.Shape)
		}
		context.Caller = cmp.Or(keyword.Caller, context.Caller)
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

// resolveField returns the field that the keys inside a block come from, by what a key rule says.
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
	case shapeArguments:
		return fit.command.Arguments
	case "holder":
		return parent.Field
	}
	return nil
}

// resolveShape returns the shape of a block, by what a key rule says. A command whose dump gave arguments is read
// as those, in the shape its usage names; any other is read as the kind it is declared as.
func (walker *walker) resolveShape(decider string, parent *Context, field *Field, fit match) string {
	switch decider {
	case "claim":
		if field != nil && field.Shape.IsStated() {
			return field.Shape.Value
		}
		return shapeUnknown
	case "command":
		switch {
		case field != nil && field.Shape.IsStated():
			return field.Shape.Value
		case field != nil && field.Source == game.SourceDumps:
			return shapeArguments
		case fit.declaration == game.DeclaredTrigger:
			return shapeTriggers
		}
		return shapeEffects
	case "same":
		return findShape(parent)
	case shapeArguments:
		return shapeArguments
	}
	return ""
}

// resolveScope sets the scope inside a block, by what a key rule says. A command or a link that leads to a scope
// where script runs moves the scope there and pushes the one left. A command that iterates a list with no target
// moves it to an unknown scope. Any other block keeps the scope, where its shape is known.
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
	case (rule.Scope == "stays" || rule.Scope == "follows") && context.Shape != "" && context.Shape != shapeUnknown:
		context.Scope = parent.Scope
	default:
		context.Scope = ""
	}
}

// resolveCallReferences resolves the references a key makes by the rule it fit: the prefixed names of a link, the
// scripted definition a call names, else the name in each database its caller calls. A key that starts the
// iterator of a scripted list names the list.
func (walker *walker) resolveCallReferences(context *Context, fit match, key string, span jomini.Span) {
	switch {
	case fit.category == CategoryLink:
		context.References = append(context.References, walker.listPrefixed(key, walker.findTextStart(span))...)
	case fit.callee != nil && fit.callee.Database != "":
		context.References = append(context.References, Reference{Database: fit.callee.Database, Name: key, Span: span, Caller: fit.caller})
	case fit.category != CategoryCall || fit.callee != nil || !jomini.IsIdentifier(key):
	case fit.iterator != nil:
		prefix := strings.TrimSuffix(fit.iterator.Key, "*")
		context.References = append(context.References,
			Reference{Database: walker.schema.Lists, Name: strings.TrimPrefix(key, prefix), Span: span})
	default:
		for _, database := range walker.schema.Callees[fit.caller] {
			context.References = append(context.References, Reference{Database: database, Name: key, Span: span, Caller: fit.caller})
		}
	}
}

// resolveChain returns the scope that a chain of links leads to, starting in the scope of the block that holds it.
// The language's own words lead to the definition's root, the scope before the chain and the current one. A link is
// matched without regard to case. It stops at the first link that no dump declares or that cannot be followed from
// the scope it is in: the rest of the chain is unknown.
func (walker *walker) resolveChain(chain string, parent *Context) string {
	scope := parent.Scope
	for _, written := range jomini.SplitChain(chain) {
		link := strings.ToLower(written)
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
			_, label, _ := jomini.SplitPrefixed(written)
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

// findSavedType returns the type of the scope the definition saved last under a name, or "" if it saved none.
func (walker *walker) findSavedType(label string) string {
	for i := len(walker.saved) - 1; i >= 0; i-- {
		if walker.saved[i].Name == label {
			return walker.saved[i].Type
		}
	}
	return ""
}

// resolveSaved resolves a command that makes a name a link reads back: a saved scope or a variable. The name is a
// definition, and a saved scope is also added to the scopes the definition saved.
func (walker *walker) resolveSaved(context *Context, statement int32, level int) {
	current := walker.tree.Statements[statement]
	if current.IsBlock() || current.Value.IsEmpty() {
		return
	}
	key := walker.frames[level].label
	link, ok := game.LookupLinkMaker(key)
	scope := walker.frames[level-1].context.Scope
	if !ok {
		link, ok = game.LookupLinkMaker(strings.ToLower(walker.frames[level-1].label) + "/" + key)
		scope = ""
	}
	label := walker.tree.Unquoted(current.Value)
	if !ok || jomini.IsParameter(label) || strings.HasPrefix(label, "[") {
		return
	}
	context.Definition = &Definition{Database: link, Name: label, Span: current.Value, Statement: statement}
	context.Defines = true
	if link == game.SavedScopes {
		walker.saved = append(walker.saved, Saved{Name: label, Type: scope})
	}
}

// resolveDatacontext sets the data context type of a GUI block from the data call its datacontext key writes, given
// the type of the block that holds it. A call the dumps do not resolve leaves the type unknown.
func (walker *walker) resolveDatacontext(context *Context, statement int32, outer string) {
	child := walker.tree.FindChild(statement, game.GUI.ContextKey)
	if child < 0 {
		return
	}
	context.Datacontext = ""
	if call, ok := walker.parseCall(walker.tree.Statements[child].Value); ok {
		if types := walker.schema.ResolveCallTypes(walker.resolveMacro(call, outer), outer); len(types) > 0 {
			context.Datacontext = types[len(types)-1]
		}
	}
}

// parseCall parses the data call that a value writes between brackets, with its spans in the file's own offsets.
// It returns false for a value that is no call.
func (walker *walker) parseCall(span jomini.Span) (jomini.Call, bool) {
	text := walker.tree.Text(span)
	start := span.Start
	if strings.HasPrefix(text, `"`) {
		text, start = strings.Trim(text, `"`), start+1
	}
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return jomini.Call{}, false
	}
	return jomini.ParseCall(text[1:len(text)-1], start+1)
}

// resolveValue resolves a scalar statement's value. A parameter or a constant is that, a constant naming the file's
// own constant; a data call is read for the names its quoted arguments refer to; any other value refers to the names
// that the field it is written at, the game's rows and its prefixes say.
func (walker *walker) resolveValue(context *Context, statement int32, level int) {
	current := walker.tree.Statements[statement]
	if current.IsBlock() || current.Value.IsEmpty() {
		return
	}
	text := walker.tree.Unquoted(current.Value)
	before := len(context.References)
	switch {
	case jomini.IsParameter(text):
		context.ValueGroup = CategoryParameter
		return
	case jomini.IsConstant(text):
		context.ValueGroup = CategoryConstant
		// an expression, @[…], is computed where it is written and names no constant
		if !strings.HasPrefix(text, "@[") {
			context.References = append(context.References, Reference{Database: game.Constant, Name: text, Span: current.Value})
		}
		return
	default:
		if call, ok := walker.parseCall(current.Value); ok {
			context.ValueGroup = CategoryCall
			context.References = append(context.References, walker.listCallReferences(call, context.Datacontext)...)
			return
		}
		walker.resolveValueReferences(context, statement, level, text)
	}
	if len(context.References) > before {
		context.ValueGroup = CategoryReference
	}
}

// listCallReferences returns the names that the quoted arguments of a data call refer to. A step the game answers
// from a database names it; any other names the database that holds the type the step returns, else none.
func (walker *walker) listCallReferences(call jomini.Call, datacontext string) []Reference {
	var references []Reference
	call = walker.resolveMacro(call, datacontext)
	types := walker.schema.ResolveCallTypes(call, datacontext)
	for i, step := range call.Steps {
		database := game.GUI.Answered[step.Text]
		if database == "" && i < len(types) && types[i] != "" {
			database, _ = walker.schema.ResolveTypeDatabase(types[i])
		}
		for _, argument := range step.Arguments {
			if argument.Call != nil {
				references = append(references, walker.listCallReferences(*argument.Call, "")...)
			}
			if argument.Quoted && !argument.Span.IsEmpty() {
				references = append(references, Reference{Database: database, Name: argument.Text, Span: argument.Span})
			}
		}
	}
	return references
}

// resolveMacro returns the call that a data call stands for. A call that opens with one of the game's data binding
// macros is the macro's text with each parameter replaced by the argument written for it, followed by the rest of
// the call. Any other call is itself. What the macro writes itself has no place in the file, so its spans are empty.
func (walker *walker) resolveMacro(call jomini.Call, datacontext string) jomini.Call {
	first := call.Steps[0]
	function := walker.schema.DataTypes[game.GlobalType].Functions[first.Text]
	_, own := walker.schema.DataTypes[datacontext].Functions[first.Text]
	expansion, ok := jomini.ParseCall(function.Expansion, 0)
	if own || !ok {
		return call
	}
	given := map[string]jomini.Argument{}
	for i, parameter := range function.Arguments {
		if i < len(first.Arguments) {
			given[parameter] = first.Arguments[i]
		}
	}
	expanded := jomini.SubstituteCall(expansion, given)
	expanded.Steps = append(expanded.Steps, call.Steps[1:]...)
	expanded.Span, expanded.Suffix = call.Span, call.Suffix
	return expanded
}

// resolveValueReferences resolves the references that a plain value makes: the database its field says it names,
// the one the game's rows give for its key path, a coat's template list, an event, and the names after the prefixes
// it is written with. Which event an id names, and whether its namespace is one a file declares, is the index's to
// resolve.
func (walker *walker) resolveValueReferences(context *Context, statement int32, level int, text string) {
	current := walker.tree.Statements[statement]
	if text == "" || strings.ContainsAny(text, "[$") {
		return
	}
	if field := context.Field; field != nil && field.Value.Value.Type == ValueReference && field.Value.Value.Database != "" {
		context.References = append(context.References, Reference{Database: field.Value.Value.Database, Name: text, Span: current.Value})
	}
	if database, ok := walker.findValueDatabase(context, statement, level); ok {
		context.References = append(context.References, Reference{Database: database, Name: text, Span: current.Value})
	}
	if !current.Tag.IsEmpty() && walker.database != nil && walker.database.Name == game.Coat.Database {
		context.References = append(context.References, Reference{Database: game.Coat.ListsDatabase, Name: text, Span: current.Value})
	}
	if walker.schema.Events != "" && jomini.IsEventID(text) {
		context.References = append(context.References, Reference{Database: walker.schema.Events, Name: text, Span: current.Value})
	}
	context.References = append(context.References, walker.listPrefixed(text, walker.findTextStart(current.Value))...)
}

// findValueDatabase returns the database that the value of a statement names by where it is written: a key path the
// game's rows give for the definition's database, a key its database says also names its definitions, or a key a
// GUI file says names a template, a texture, a font or a text.
func (walker *walker) findValueDatabase(context *Context, statement int32, level int) (string, bool) {
	key := walker.frames[level].label
	keys := walker.frames[level-1].path
	if key != "" {
		keys = append(keys[:len(keys):len(keys)], key)
	}
	for _, row := range slices.Concat(game.GUI.References, walker.row.References) {
		if row.Database == "" && walker.file.Format == files.FormatGUI && key == row.KeyPath ||
			row.Database != "" && row.Database == context.Database && game.MatchKeyPath(row.KeyPath, keys) {
			return cmp.Or(row.Target, row.Database), true
		}
	}
	database := walker.schema.Databases[context.Database]
	if database != nil && slices.Contains(database.Row.AliasKeys, key) {
		return database.Name, true
	}
	return "", false
}

// listPrefixed returns a reference for each name that a chain of links writes after a prefix with a database. The
// given offset is where the chain starts in the file.
func (walker *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	offset := 0
	for _, link := range jomini.SplitChain(chain) {
		if prefix, label, ok := jomini.SplitPrefixed(link); ok {
			if database, exists := walker.schema.Prefixes[prefix]; exists && database != "" {
				start := base + offset + len(prefix) + 1
				references = append(references, Reference{Database: database, Name: label, Span: jomini.Span{Start: start, End: start + len(label)}})
			}
		}
		offset += len(link) + 1
	}
	return references
}

// findTextStart returns the offset of the text a scalar writes, which is past the quote it opens with, if it has
// one.
func (walker *walker) findTextStart(span jomini.Span) int {
	if strings.HasPrefix(walker.tree.Text(span), `"`) {
		return span.Start + 1
	}
	return span.Start
}

// buildPaths builds the keys that lead to the children of a statement. A definition of the file's own database
// begins the keys counted from the enclosing definition anew.
func (walker *walker) buildPaths(level int, label string, begins bool) {
	walker.frames[level].path = walker.frames[level-1].path
	if label != "" {
		walker.frames[level].path = slices.Concat(walker.frames[level-1].path, []string{label})
	}
	if begins {
		walker.frames[level].path = nil
	}
}

// isLead reports whether a value written alone is a word that makes the statement after it a definition or a call:
// one a database declares its definitions with, or in a GUI file one it declares them with or calls them by.
func (walker *walker) isLead(text string) bool {
	if walker.declaring[text] != nil {
		return true
	}
	return walker.file.Format == files.FormatGUI &&
		(slices.Contains(game.GUI.Declares, text) || text != game.AnyCall && len(walker.schema.Callees[text]) > 0)
}
