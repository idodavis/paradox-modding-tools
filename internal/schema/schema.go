// Package schema builds the model of one game version's script language, a
// lang.Schema. It reads three things: the files the game writes when asked
// to dump its language, the doc files the install ships, and the game's own
// script files.
//
// # The dumps
//
//   - The dumps come in parts: the effects, the triggers, the links, the
//     scope types, the modifiers, the on_actions, the data types and the
//     customizable localizations. A part the install's dumps lack is taken
//     whole from the backup the app ships.
//   - A dump correction replaces the scopes of a command whose dump entry
//     contradicts its own sentence about the command.
//   - The effects, the triggers, the links and the scope types are four
//     databases, and each dump entry is a definition in the database of its
//     file. A name that two files list is a definition in each. A
//     definition leads to the one scope its entry names, and to none where
//     the entry names several.
//   - The keys of a command's block are the keys of the example written
//     under its name, and the lines of its description written as
//     key = value. A statement in [ ] or < > may be left out, [a|b] lists
//     the values a key takes, and a placeholder such as <triggers> says
//     what a block holds. A trigger's Traits line says what its value is.
//     It comes before the value the dump writes after the command's name,
//     as add_trait = <trait_key> does.
//   - A name that is both an effect and a trigger keeps the keys of each,
//     and gets the keys of both together for a block that may hold either.
//     There a key keeps what its block holds only where the two agree, or
//     where the block may hold effects and triggers alike. Six names whose
//     two entries differ only in what their block holds, such as switch and
//     debug_log, share one set of keys.
//   - The commands that go through one list, such as any_vassal,
//     every_vassal, random_vassal and ordered_vassal, share their keys.
//   - A keyword written inside a command's block, such as the numbered
//     branches of random_list, is a key of that command.
//   - A command that leads to no scope and whose description starts with
//     "Iterate through" runs its block on each item of a list. The scope
//     inside its block is not known.
//   - A link whose entry says "Requires Data: yes" is written with a name
//     after a colon, as scope:target is.
//   - A data function listed more than once keeps its first entry. Self and
//     AccessSelf return the data type they are called on.
//
// # The docs
//
//   - A doc is an .info or .md file, a file whose name starts with readme,
//     or a .txt file named as a doc. Its sentences are not script a parser
//     could read, so a doc is read line by line, by the pattern of each
//     line.
//   - A doc describes the database of the folder it is in, else of a folder
//     above it, else of the folder named as the file is. A doc in a folder
//     below its database's describes only part of the database, and gives
//     it no hover text. Where each file of a database is one definition, a
//     doc is read as already inside that definition.
//   - The script a doc writes gives the fields. The sentences beside it
//     give each field its hover text, its type, whether it must be written,
//     its default and the values it takes. A doc gives no scope. What an
//     earlier doc says is kept over a later one.
//   - A field is needed only in some cases where a sentence says so, as
//     "only used when" does. Otherwise the words optional, required and
//     mandatory say whether it must be written. What is said first is kept.
//   - A list of only yes and no makes a field a yes or no, and the value
//     marked (default) is the default.
//   - A database whose definitions are triggers, effects or a script value
//     takes no fields from a doc, and a field whose block holds script
//     takes none below it.
//   - A field with no hover text takes the one the docs give a field of the
//     same key elsewhere in its database.
//
// # The count of the game's own files
//
//   - The files that declare scripted definitions are read first, so that a
//     key that calls one is counted as a call.
//   - The count says only what no other source says, field by field. What a
//     field's block holds is what the keys counted in it show. A block with
//     math keys computes a number, even with effects or triggers among
//     them. What it holds is unknown where the keys disagree.
//   - Where the game's files write names as a field's value, the field
//     takes the database that defines every one of those names. A field
//     never written as a block takes the first type every one of its values
//     fits. A field always written as a block is a color or a block. A link
//     to a scope also names a database only where that database defines
//     every name the game's files write there.
//   - The game concepts are taken only where no other database defines the
//     values, and the localization only where the game concepts do not
//     either. A field another source says is a block or a text never takes
//     the game concepts. No field takes a GUI database, since the names in
//     those are common words such as value.
//   - Where every key that only the game's files write in a block is a
//     definition of one database, the keys of that block name the database,
//     as the keys of a culture's opinions block name cultures. Keys never
//     name the game concepts, the localization, a GUI database or a
//     database that has others nested in it. The block of a command is left
//     alone, except where a commandKeys row says its keys name a database.
//   - A number may be written as a script value too where the game's files
//     write one there.
//   - A link that takes a name after a colon takes the database that
//     defines every name the game's files write after it. A link that may
//     be written in any scope and leads to a scope tries the database of
//     that scope's type first.
//   - A database makes a star key, as has_relation_*, where the dumps list
//     a command for each of its definitions, two at least, whose name
//     differs only by the definition's own name.
//
// # Robustness
//
// Build returns an error if a dump file's time or a doc cannot be read, or
// if no script file of the listing can be parsed. A dump file that cannot
// be read as text, such as one larger than files.TextLimit or one in
// UTF-16, is treated as missing. A script file that cannot be parsed is
// left out of the count. A file with no sentence and no comment, or a .txt
// file with more lines of script than of sentences, is data and no doc, and
// is skipped. LoadShipped returns false for a game with no backup, and for
// a backup that cannot be decoded.
package schema

import (
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// builder is the schema of one game version while it is being built.
type builder struct {
	*lang.Schema
}

// Build returns the schema of one game version from its dumps, its docs and
// the game's own files. DLC files are left out of the listing, since the
// game's own folders list them too.
func Build(
	dialect *lang.Dialect, dumpDirs []string, installPath string, listing []files.File,
	parse func(path string) (*jomini.Tree, error),
) (*lang.Schema, error) {
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return strings.HasPrefix(file.Rel, game.DLCFolder+"/")
	})

	schema := &builder{Schema: &lang.Schema{
		Game:            dialect.ID,
		Databases:       map[string]*lang.Database{},
		Callees:         map[string][]string{},
		TypeDatabases:   map[string]string{},
		InlineDatabases: map[string]string{},
		UnlistedCalls:   map[string]bool{},
	}}

	schema.Setups = listSetups(dialect, listing, parse)
	folders := listFolders(dialect, listing)

	schema.buildDatabases(dialect, folders)
	schema.buildFields()
	if err := schema.buildDumps(dialect, dumpDirs, installPath); err != nil {
		return nil, err
	}

	if err := schema.buildDocs(installPath, listing, folders); err != nil {
		return nil, err
	}

	// the count reads the keys an effect and a trigger of one name share, and
	// then adds to both, so the two are united before the count and after it
	schema.buildUnions()
	if err := schema.countVanilla(listing, parse); err != nil {
		return nil, err
	}

	schema.buildUnions()
	schema.buildDocBlurbs()

	return schema.Schema, nil
}

// listSetups returns the folders the game's bookmarks name for their start
// setups, sorted and each once.
func listSetups(dialect *lang.Dialect, listing []files.File, parse func(path string) (*jomini.Tree, error)) []string {
	var setups []string
	for _, file := range listing {
		if dialect.BookmarkFolder == "" || !strings.HasPrefix(file.StageRel(), dialect.BookmarkFolder+"/") {
			continue
		}

		tree, err := parse(file.Path)
		if err != nil {
			continue
		}

		for bookmark := range tree.Children(0) {
			if setup, ok := tree.FindChildValue(bookmark, dialect.SetupKey); ok && setup != "" {
				setups = append(setups, setup)
			}
		}
	}

	slices.Sort(setups)

	return slices.Compact(setups)
}

// listFolders returns the folder of each database the game's files make: the
// folder each script file belongs to, and each declared folder the listing
// has.
func listFolders(dialect *lang.Dialect, listing []files.File) []string {
	folders := map[string]bool{}
	present := map[string]bool{}
	for _, file := range listing {
		rel := file.StageRel()
		for dir := rel; dir != "."; dir = path.Dir(dir) {
			present[dir] = true
		}

		if files.Classify(rel) == files.FormatScript {
			folders[findFolder(dialect, rel)] = true
		}
	}

	for _, declaration := range dialect.ListDeclarations() {
		if present[declaration.Folder] {
			folders[declaration.Folder] = true
		}
	}

	delete(folders, ".")

	return slices.Sorted(maps.Keys(folders))
}

// findFolder returns the folder of the database a script file belongs to. A
// declaration may name the file itself or a folder above it; otherwise it is
// the file's own folder.
func findFolder(dialect *lang.Dialect, rel string) string {
	if _, ok := dialect.FindDeclaration(rel); ok {
		return rel
	}

	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		if _, ok := dialect.FindDeclaration(dir); ok {
			return dir
		}
	}

	return path.Dir(rel)
}

// fireContents are the kinds of database whose definitions fire one another.
// A message is fired when an effect sends it.
var fireContents = []lang.Contents{
	lang.ContentsEvents, lang.ContentsOnActions, lang.ContentsDecisions, lang.ContentsMessages,
}

// buildDatabases makes a database for each folder and nesting, for what GUI
// files declare, and for the events' namespaces. It then records which
// database plays each role the schema names, such as the events' or the
// messages', only where exactly one does.
func (b *builder) buildDatabases(dialect *lang.Dialect, folders []string) {
	for _, folder := range folders {
		declaration, _ := dialect.FindDeclaration(folder)
		if slices.Contains(b.Setups, folder) {
			declaration, _ = dialect.FindDeclaration(lang.SetupFolder)
		}

		declaration.Folder = folder
		declaration.Nestings = dialect.ListNestings(declaration)
		b.buildDatabase(dialect, lang.ToDatabase(folder), declaration)
		b.buildNested(dialect, declaration.Nestings)
	}

	names := []string{
		lang.GUIWidgetDatabase, lang.GUIWidgetTypes, lang.GUITextFormats, lang.GUITextIcons, lang.EventNamespaces,
	}
	for _, declaring := range lang.GUIDeclares {
		names = append(names, lang.GUIDatabasePrefix+declaring)
	}

	for _, label := range names {
		b.buildDatabase(dialect, label, dialect.LookupDeclaration(label))
	}

	housed := map[lang.Contents][]string{}
	for label, database := range b.Databases {
		housed[database.Declaration.Contents] = append(housed[database.Declaration.Contents], label)

		// of several called databases of effects, or of script values, the
		// first by name is taken
		called, shape := len(database.Declaration.Callers) > 0, database.Declaration.Shape
		switch {
		case called && shape == lang.ShapeEffects && (b.Effects == "" || label < b.Effects):
			b.Effects = label
		case called && shape == lang.ShapeValue && (b.Values == "" || label < b.Values):
			b.Values = label
		}
	}

	findOnly := func(contents lang.Contents) string {
		if len(housed[contents]) == 1 {
			return housed[contents][0]
		}

		return ""
	}

	b.Events, b.Messages, b.Lists = findOnly(lang.ContentsEvents), findOnly(lang.ContentsMessages),
		findOnly(lang.ContentsLists)
	b.Concepts, b.ModifierTypes = findOnly(lang.ContentsConcepts), findOnly(lang.ContentsModifiers)
	b.OnActions = findOnly(lang.ContentsOnActions)

	for _, house := range fireContents {
		b.FireDatabases = append(b.FireDatabases, housed[house]...)
	}

	slices.Sort(b.FireDatabases)
	b.DeclareWidgets(slices.Concat(slices.Collect(maps.Keys(lang.BuiltinWidgets)), dialect.UndeclaredWidgets))
	b.listCallees()
}

// buildDatabase adds a database from its declaration, keeping only the rows
// that hold for the game, unless the database exists. It records the
// database under the words of its name, so "government type" finds
// governments, unless its definitions stand for scopes.
func (b *builder) buildDatabase(dialect *lang.Dialect, label string, declaration lang.Declaration) {
	if b.Databases[label] != nil {
		return
	}

	declaration.References = slices.DeleteFunc(slices.Clone(declaration.References), func(reference lang.KeyReference) bool {
		return !dialect.Matches(reference.Games)
	})
	declaration.Fields = slices.DeleteFunc(slices.Clone(declaration.Fields), func(field lang.DatabaseField) bool {
		return !dialect.Matches(field.Games)
	})
	b.Databases[label] = &lang.Database{
		Name: label, Field: &lang.Field{}, Declaration: declaration, Precedence: dialect.LookupPrecedence(declaration, label),
	}

	for _, key := range declaration.DeclaringKeys {
		b.InlineDatabases[key] = label
	}

	if declaration.Target != "" {
		return
	}

	// where two databases have the same words, the one whose name is
	// exactly those words keeps them, and neither does where neither is
	phrase := lang.DescribeDefinition(label, false)
	owner := label
	if earlier, ok := b.TypeDatabases[phrase]; ok && !isNamedExactly(label, phrase) {
		owner = ""
		if isNamedExactly(earlier, phrase) {
			owner = earlier
		}
	}

	b.TypeDatabases[phrase] = owner
}

func isNamedExactly(label, phrase string) bool {
	return label == strings.ReplaceAll(phrase, " ", "_")
}

// buildNested adds the database of each nesting that has one of its own and
// lists it in its parent database.
func (b *builder) buildNested(dialect *lang.Dialect, nestings []lang.Nesting) {
	for _, nesting := range nestings {
		if nesting.Database == "" {
			continue
		}

		label := nesting.Label()
		declaration := lang.Declaration{Target: nesting.Target}
		if nesting.Precedence.Keeps != "" {
			declaration.Precedences = []lang.Precedence{nesting.Precedence}
		}

		b.buildDatabase(dialect, label, declaration)
		if parent := b.Databases[lang.ToDatabase(nesting.Parent)]; parent != nil && !slices.Contains(parent.Nested, label) {
			parent.Nested = append(parent.Nested, label)
		}
	}
}

// listCallees records, for each kind of caller such as a trigger block, the
// databases whose definitions it may call.
func (b *builder) listCallees() {
	for _, label := range lang.Called {
		if database := b.Databases[label]; database != nil {
			for _, caller := range database.Declaration.Callers {
				b.Callees[caller] = append(b.Callees[caller], label)
			}
		}
	}
}

// buildFields gives each field a database's declaration lists what its row
// says.
func (b *builder) buildFields() {
	for _, database := range b.Databases {
		for _, entry := range database.Declaration.Fields {
			field := database.Field
			for key := range strings.SplitSeq(entry.KeyPath, "/") {
				field = field.BuildChild(key, lang.SourceRegistry)
			}

			field.Weighted = entry.Weighted
			field.Blurb.Resolve(entry.Blurb, lang.SourceRegistry)
			if entry.Shape != "" {
				field.Shape.Resolve(entry.Shape, lang.SourceRegistry)
			}
		}
	}
}
