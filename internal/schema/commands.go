// commands.go builds the schema's effects, triggers, links, scope types and
// data types from the dumps, with the keys each command's block takes.

package schema

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// The origins of the dumps' data: none, the install, or the shipped backup.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// buildDumps builds the schema's commands, scope types, modifiers,
// on_actions and data types from the install's dumps, with the shipped
// backup filling what they lack. It records where the data came from and
// whether it is older than the install.
func (b *builder) buildDumps(dialect *lang.Dialect, dumpDirs []string, installPath string) error {
	dumps, err := ReadDumps(dumpDirs)
	if err != nil {
		return err
	}

	backup, hasBackup := LoadShipped(dialect.ID)
	isFilled := hasBackup && dumps.resolveMissing(backup)
	dumps.applyCorrections(dialect)

	state := lang.SourceState{Origin: originNone, WrittenAt: dumps.WrittenAt, Folders: dumps.Folders}
	switch {
	case !dumps.WrittenAt.IsZero():
		state.Origin = originInstall
		state.Stale = dumps.OldestAt.Before(game.ReadInstallUpdatedAt(installPath))

	case isFilled:
		state.Origin = originShipped
	}

	b.Dumps = state
	b.Customs, b.DataTypes = dumps.Customs, dumps.DataTypes
	b.buildTypes(dumps)
	b.buildCommands(dumps.Commands)
	b.buildPrefixes()
	b.buildCommandKeys(dialect)
	b.buildKeywordShapes(dialect)
	b.buildFamilies()

	for _, database := range b.Databases {
		database.Keys = slices.Sorted(maps.Keys(database.Definitions))
	}

	return nil
}

// buildTypes makes the databases the dumps fill, and in them a definition
// for each scope type, modifier and on_action. Script runs in a scope type
// if triggers or effects do.
func (b *builder) buildTypes(dumps *Dumps) {
	for _, database := range lang.Dumps {
		b.Databases[database] = &lang.Database{Name: database, Field: &lang.Field{}, Definitions: map[string]*lang.Definition{}}
	}

	types := b.Databases[lang.DumpTypes]
	for key, scope := range dumps.Scopes {
		types.Definitions[key] = &lang.Definition{Database: types.Name, Key: key, Runs: scope.Triggers || scope.Effects}
	}

	for key := range dumps.Modifiers {
		b.Databases[lang.DumpModifiers].Definitions[key] = &lang.Definition{Database: lang.DumpModifiers, Key: key}
	}

	// a game may have no on_actions database
	actions := b.Databases[b.OnActions]
	if actions == nil {
		return
	}

	actions.Definitions = map[string]*lang.Definition{}
	for key, action := range dumps.OnActions {
		actions.Definitions[key] = &lang.Definition{
			Database: actions.Name, Key: key, Target: action.Scope, Hardcoded: action.Hardcoded,
		}
	}
}

// sharedWords are the names the dumps list as both an effect and a trigger
// whose two entries differ only in what their block holds. Each pair shares
// one set of keys.
var sharedWords = []string{
	"switch", "assert_if", "assert_read", "debug_log", "save_temporary_scope_value_as",
	"save_temporary_opinion_value_as",
}

// buildCommands makes a definition for each effect, trigger and link the
// dumps list, with the keys its example writes. A name listed twice in one
// database is one definition.
func (b *builder) buildCommands(dumps []DumpCommand) {
	for _, dump := range dumps {
		position := slices.IndexFunc(dumpDatabases, func(section dumpSection) bool { return section.declared == dump.Declared })
		if position < 0 {
			continue
		}

		// a link written with a name after a colon is kept apart from the
		// same word written alone, under its name and a colon
		key := dump.Name
		if dump.Declared == declaredLink && dump.Prefixed {
			key += ":"
		}

		owner := b.Databases[dumpDatabases[position].database]
		definition := owner.Definitions[key]
		isListed := definition != nil
		if !isListed {
			definition = &lang.Definition{Database: owner.Name, Key: key, Path: dump.Path, Line: dump.Line}
			owner.Definitions[key] = definition
		}

		b.buildDefinition(definition, dump, isListed)
		if expected, ok := b.resolveTraits(dump.Traits); ok {
			makeField(definition, lang.SourceDumps).Value.Resolve(expected, lang.SourceDumps)
		}

		b.resolveTitleValue(definition, dump)
	}

	for _, key := range sharedWords {
		effect, trigger := b.LookupDefinition(lang.DumpEffects, key), b.LookupDefinition(lang.DumpTriggers, key)
		if effect == nil || trigger == nil {
			continue
		}

		shared := uniteCommands(effect, trigger)
		effect.Field, trigger.Field = shared, shared
	}
}

// usageToken is one piece of the example a dump writes for a command: a
// brace, an operator, a word, or a group in <> or [].
var usageToken = regexp.MustCompile(`[{}]|[<>!=?]=|<[^<>\n]*>|\[[^\[\]\n]*\]|[=<>]|[^\s{}=<>\[\]]+`)

// usageComment is a comment inside such an example.
var usageComment = regexp.MustCompile(`#[^\n]*`)

// usageOperators are the operators a key in such an example may be written
// with.
var usageOperators = []string{"=", "==", "!=", "<", ">", "<=", ">=", "?="}

// usageLine is a line that opens a block, as key = { does. describedLine is
// such a block closed and described on the same line after a dash, which is
// prose and not an example.
var (
	usageLine     = regexp.MustCompile(`^\s*\S+\s*=\s*\{`)
	describedLine = regexp.MustCompile(`\}\s+-\s+\S`)
)

// splitUsage splits a command's description into its sentences and its
// example, which starts at the first line that opens a block.
func splitUsage(description string) (prose, usage string) {
	lines := strings.Split(description, "\n")
	cut := slices.IndexFunc(lines, func(line string) bool {
		return usageLine.MatchString(line) && !describedLine.MatchString(line)
	})
	if cut < 0 {
		return description, ""
	}

	return strings.Join(lines[:cut], "\n"), strings.Join(lines[cut:], "\n")
}

// parseArguments reads the keys a command's block takes from its example
// block and from the key = value lines of its sentences. It returns them as
// a field, nil if there are none, and the example's first key, which an
// argument in parentheses stands for.
func (b *builder) parseArguments(title, prose, usage string) (*lang.Field, string) {
	arguments, first := &lang.Field{Source: lang.SourceDumps}, ""
	prose, usage = usageComment.ReplaceAllString(prose, ""), usageComment.ReplaceAllString(usage, "")
	lines := strings.Split(prose, "\n")

	for position, line := range lines {
		tokens := usageToken.FindAllString(line, -1)

		// a key alone whose block opens on the next line is a scope, as
		// Vic3's "power_bloc =" is
		opens := len(tokens) == 2 && position+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[position+1]), "{")
		if len(tokens) > 1 && tokens[0] != title && jomini.Classify(tokens[0]).IsIdentifier() && tokens[1] == "=" && !opens {
			b.parseUsageStatement(tokens, 0, arguments, false)
		}
	}

	tokens := usageToken.FindAllString(prose+"\n"+usage, -1)
	for position := 0; position+2 < len(tokens); position++ {
		if tokens[position] == title && tokens[position+1] == "=" && tokens[position+2] == "{" {
			start := position + 3
			if first == "" && start+1 < len(tokens) && jomini.Classify(tokens[start]).IsIdentifier() &&
				slices.Contains(usageOperators, tokens[start+1]) {
				first = tokens[start]
			}

			position = b.parseUsageBlock(tokens, start, arguments) - 1
		}
	}

	if len(arguments.Children) == 0 && !arguments.Shape.IsStated() {
		return nil, ""
	}

	return arguments, first
}

// parseUsageStatement reads one key = value of an example and adds the key
// to the field. optional marks the key as one that may be left out. Keys
// joined by slashes each get the value. It returns the place after the
// statement.
func (b *builder) parseUsageStatement(tokens []string, cursor int, field *lang.Field, optional bool) int {
	if cursor+1 >= len(tokens) || !slices.Contains(usageOperators, tokens[cursor+1]) {
		return cursor + 1
	}

	// a key with no value, as create_character's "age =", is still declared
	hasValue := cursor+2 < len(tokens) && tokens[cursor+2] != "}"
	next := cursor + 2
	for part := range strings.SplitSeq(tokens[cursor], "/") {
		// a part that is no key still has its value read, and thrown away
		argument := &lang.Field{}
		if jomini.Classify(part).IsIdentifier() {
			argument = field.BuildChild(part, lang.SourceDumps)
		}

		if optional {
			argument.Required.Resolve(lang.RequirementOptional, lang.SourceDumps)
		}

		if hasValue {
			next = b.parseUsageValue(tokens, cursor+2, argument)
		}
	}

	return next
}

// parseUsageValue reads the value an example writes for a key and gives the
// key what it says: a block, a list written a|b, a type such as <bool>, or
// what a placeholder such as <character> names. A plain sample value, as
// 10, says nothing. It returns the place after the value.
func (b *builder) parseUsageValue(tokens []string, cursor int, argument *lang.Field) int {
	value := tokens[cursor]
	term := strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
	_, isType := docTypes[lang.ToPhrase(term)]
	switch {
	case value == "{":
		return b.parseUsageBlock(tokens, cursor+1, argument)
	case strings.ContainsAny(value[:1], "<[") && strings.Contains(value, "|"):
		resolveChoices(argument, strings.Split(strings.Trim(value, "<>[]"), "|"), lang.SourceDumps)
	case isType && len(term) > 1:
		resolveDocType(argument, term, lang.SourceDumps)
	case value[0] == '<':
		if expected, ok := b.resolvePlaceholder(term); ok {
			argument.Value.Resolve(expected, lang.SourceDumps)
		}
	}

	return cursor + 1
}

// parseUsageBlock reads one block of an example and adds its keys to the
// field. A placeholder alone, as <triggers>, says what the block holds, and
// a statement in [ ] or < > may be left out. It returns the place after the
// block's closing brace.
func (b *builder) parseUsageBlock(tokens []string, cursor int, field *lang.Field) int {
	for cursor < len(tokens) && tokens[cursor] != "}" {
		token := tokens[cursor]
		switch {
		case token == "{":
			cursor = b.parseUsageBlock(tokens, cursor+1, &lang.Field{})

		case (token[0] == '<' || token[0] == '[') && strings.Contains(token, "="):
			b.parseUsageStatement(usageToken.FindAllString(token[1:len(token)-1], -1), 0, field, true)
			cursor++

		case token[0] == '<':
			if shape, ok := findPlaceholderShape(token[1:]); ok {
				field.Shape.Resolve(shape, lang.SourceDumps)
			}

			cursor++

		default:
			cursor = b.parseUsageStatement(tokens, cursor, field, false)
		}
	}

	return cursor + 1
}

// findPlaceholderShape returns what a block holds when an example writes a
// placeholder alone in it: <triggers> and <character_trigger> both say
// triggers. It returns false for a placeholder that names no kind of block.
func findPlaceholderShape(placeholder string) (lang.Shape, bool) {
	shape := lang.Shape(strings.TrimSuffix(placeholder, ">"))
	shape = shape[strings.LastIndex(string(shape), "_")+1:]
	if !strings.HasSuffix(string(shape), "s") {
		shape += "s"
	}

	_, ok := lang.ShapeRows[shape]

	return shape, ok
}

// resolvePlaceholder returns what a placeholder written as a value stands
// for. One ending in key or name, as <effect_localization_key>, stands for a
// name of the database its other words lead to; any other stands for a link
// to a scope of the type it names, as <character> does.
func (b *builder) resolvePlaceholder(term string) (lang.Expected, bool) {
	words := strings.Fields(lang.ToPhrase(term))
	if len(words) == 0 {
		return lang.Expected{}, false
	}

	last, stem := words[len(words)-1], strings.Join(words[:len(words)-1], " ")
	if last == "key" || last == "name" {
		database, ok := b.ResolveTypeDatabase(stem)

		return lang.Expected{Type: lang.ValueTypeReference, Database: database}, ok
	}

	if last != "scope" && last != "target" {
		stem = strings.Join(words, " ")
	}

	scope := strings.ReplaceAll(stem, " ", "_")
	if !b.IsScope(scope) {
		return lang.Expected{}, false
	}

	database, _ := b.ResolveTypeDatabase(scope)

	return lang.Expected{Type: lang.ValueTypeTarget, Scope: scope, Database: database}, true
}

// dumpIterates is how the dumps begin the sentence about a command that
// runs its block on each item of a list.
const dumpIterates = "Iterate through"

// buildDefinition gives a definition what one dump entry says about it; the
// keys its block takes come from the first entry only. A name listed twice
// gets the scopes of both entries, or none if either lists none. A command
// that goes through a list and leads to no scope runs its block on each
// item.
func (b *builder) buildDefinition(definition *lang.Definition, dump DumpCommand, isListed bool) {
	prose, usage := splitUsage(dump.Description)
	switch {
	case !isListed:
		definition.Scopes = slices.Clone(dump.Scopes)
		definition.Field, definition.ArgumentKey = b.parseArguments(dump.Name, prose, usage)
	case len(dump.Scopes) == 0 || len(definition.Scopes) == 0:
		definition.Scopes = nil
	default:
		definition.Scopes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(definition.Scopes, dump.Scopes))))
	}

	definition.Target = cmp.Or(definition.Target, dump.Target)
	blurb, _, _ := strings.Cut(strings.TrimSpace(prose), "\n")
	blurb = strings.TrimSpace(blurb)
	definition.Blurb = cmp.Or(definition.Blurb, blurb)
	definition.Usage = cmp.Or(definition.Usage, usage)
	if dump.Declared == declaredLink {
		definition.Wild, definition.Prefixed = dump.Wild, dump.Prefixed
	}

	definition.Iterates = definition.Iterates || (strings.HasPrefix(blurb, dumpIterates) && definition.Target == "")
}

// The forms of a trigger's Traits line: a comparison (with "valid date" for
// a date), a class of keys, a scope, a tag or id with a scope, and a list of
// values.
var (
	comparisonTraits = regexp.MustCompile(`^<, <=, =, (?:!=, )?>, >=( valid date)?$`)
	classTraits      = regexp.MustCompile(`^class (C\w+) key$`)
	targetTraits     = regexp.MustCompile(`^(\w+) (?:scope|target)(?: \w+ scope)?$`)
	tagTraits        = regexp.MustCompile(`^\w+ (?:tag|id)/(\w+) scope$`)
	choiceTraits     = regexp.MustCompile(`^(?:Valid [\w ]+: )?(.+, .+)$`)
)

// resolveTraits returns what a trigger's value must be, as its Traits line
// says. It returns false for a line it does not know, and for a class of
// keys no database of the game fits.
func (b *builder) resolveTraits(traits string) (lang.Expected, bool) {
	comparison, class := comparisonTraits.FindStringSubmatch(traits), classTraits.FindStringSubmatch(traits)
	target, tag := targetTraits.FindStringSubmatch(traits), tagTraits.FindStringSubmatch(traits)
	list := choiceTraits.FindStringSubmatch(traits)
	switch {
	case comparison != nil && comparison[1] != "":
		return lang.Expected{Type: lang.ValueTypeDate}, true

	case comparison != nil:
		return lang.Expected{Type: lang.ValueTypeNumber, Values: true}, true

	case traits == "yes/no":
		return lang.Expected{Type: lang.ValueTypeBool}, true

	case class != nil:
		database, ok := b.FindDatabaseByClass(class[1])
		if !ok {
			return lang.Expected{}, false
		}

		return lang.Expected{Type: lang.ValueTypeReference, Database: database.Name}, true

	case target != nil:
		return lang.Expected{Type: lang.ValueTypeTarget, Scope: target[1]}, true

	case tag != nil:
		database, _ := b.ResolveTypeDatabase(tag[1])

		return lang.Expected{Type: lang.ValueTypeTarget, Scope: tag[1], Database: database}, true

	case list != nil:
		choices := splitList(list[1])
		choices[len(choices)-1] = strings.TrimPrefix(choices[len(choices)-1], "and ")

		return lang.Expected{Type: lang.ValueTypeChoice, Choices: choices}, true
	}

	return lang.Expected{}, false
}

// makeField returns a definition's field, made with the given source if it
// has none.
func makeField(definition *lang.Definition, source lang.Source) *lang.Field {
	if definition.Field == nil {
		definition.Field = &lang.Field{Source: source}
	}

	return definition.Field
}

// resolveTitleValue gives a command what the value written right after its
// name in the dump says it takes, as add_trait = <trait_key> does. What a
// Traits line says comes first.
func (b *builder) resolveTitleValue(command *lang.Definition, dump DumpCommand) {
	hasKeys := command.Field != nil && len(command.Field.Children) > 0
	for line := range strings.SplitSeq(usageComment.ReplaceAllString(dump.Description, ""), "\n") {
		tokens := usageToken.FindAllString(line, -1)
		if len(tokens) < 3 || tokens[0] != dump.Name || !slices.Contains(usageOperators, tokens[1]) || tokens[2] == "{" {
			continue
		}

		value := &lang.Field{}
		b.parseUsageValue(tokens, 2, value)

		// a command with keys of its own holds its arguments in its block, so
		// a placeholder's block kind gives only the database a plain value
		// names
		if hasKeys && value.Shape.IsStated() {
			database := lang.ShapeRows[value.Shape.Value].Values
			value.Shape, value.Value = lang.Claim[lang.Shape]{}, lang.Claim[lang.Expected]{}
			if database != "" {
				value.Value.Resolve(lang.Expected{Type: lang.ValueTypeReference, Database: database}, lang.SourceDumps)
			}
		}

		if !value.Value.IsStated() {
			continue
		}

		makeField(command, lang.SourceDumps).Take(value.Claims)

		return
	}
}

// buildPrefixes gives each link that takes a name after a colon the database
// of those names, as scope: takes a saved scope. A database's declaration
// lists the links that take its names. A link the dumps do not list is
// added.
func (b *builder) buildPrefixes() {
	links := b.Databases[lang.DumpLinks]
	claim := func(prefix, database string) {
		link := b.LookupLink(prefix, true)
		if link == nil {
			link = &lang.Definition{Database: links.Name, Key: prefix + ":", Prefixed: true}
			links.Definitions[link.Key] = link
		}

		expected := lang.Expected{Type: lang.ValueTypeReference, Database: database}
		makeField(link, lang.SourceRegistry).Value.Resolve(expected, lang.SourceRegistry)
	}

	for _, saved := range []string{lang.SavedScopes, lang.Variables, lang.LocalVariables, lang.GlobalVariables} {
		claim(saved, saved)
	}

	for _, label := range slices.Sorted(maps.Keys(b.Databases)) {
		for _, prefix := range b.Databases[label].Declaration.Prefixes {
			claim(prefix, label)
		}
	}
}

// commandKey is a command, or a key of a command's block, that takes a name
// of a database though the dump's example of it does not say so.
type commandKey struct {
	// Games lists the games the row holds for, all if none.
	Games []string

	// Command is the command, or command/key for a key inside its block.
	Command string

	// Database is the database the keys of its block name, and Value the one
	// its value names. Each is "" for none.
	Database, Value string

	// Stem is the name the value stands for, with %s for the value, as
	// %s_title. It is "" where the value is the name itself.
	Stem string
}

// commandKeys are the commands and keys whose database the dumps' examples
// do not give, though their sentences do.
var commandKeys = []commandKey{
	{Command: "stress_impact", Database: "traits"}, {Command: "stress_and_fulfillment_impact", Database: "traits"},
	{Games: []string{game.CK3}, Command: "create_character/random_traits_list", Database: "traits"},
	{Command: "custom_tooltip", Value: lang.Localization}, {Command: "custom_tooltip/text", Value: lang.Localization},
	{Command: "custom_label", Value: lang.Localization}, {Command: "custom_label/text", Value: lang.Localization},
	// the game shows <key>_title for an activity log entry
	{Command: "add_activity_log_entry/key", Value: lang.Localization, Stem: "%s_title"},
	// the dump gives no example line for has_trait
	{Games: []string{game.CK3}, Command: "has_trait", Value: "traits"},
	{Command: lang.GameRuleTrigger, Value: lang.GameRuleSettings},
}

// buildCommandKeys applies the rows of commandKeys that hold for the game. A
// row for a command the dumps do not list does nothing.
func (b *builder) buildCommandKeys(dialect *lang.Dialect) {
	for _, entry := range commandKeys {
		if !dialect.Matches(entry.Games) {
			continue
		}

		title, key, hasKey := strings.Cut(entry.Command, "/")
		for _, database := range lang.CommandDatabases {
			command := b.Databases[database].Definitions[title]
			if command == nil {
				continue
			}

			field := makeField(command, lang.SourceRegistry)
			if hasKey {
				field = field.BuildChild(key, lang.SourceRegistry)
			}

			if entry.Database != "" {
				field.Keys.Resolve(entry.Database, lang.SourceRegistry)
			}

			if entry.Value != "" {
				expected := lang.Expected{Type: lang.ValueTypeReference, Database: entry.Value, Stem: entry.Stem}
				field.Value = lang.Claim[lang.Expected]{Value: expected, Source: lang.SourceDumps}
			}
		}
	}
}

// buildKeywordShapes gives a command the keys that keywords written inside
// its block add to it, as a random_list's numbered branches.
func (b *builder) buildKeywordShapes(dialect *lang.Dialect) {
	for _, keyword := range lang.Keywords {
		command := b.FindCommand(keyword.Parent)
		if command == nil || keyword.Shape == "" || keyword.Shape == lang.ShapeEnclosing || !dialect.Matches(keyword.Games) {
			continue
		}

		argument := makeField(command, lang.SourceDumps).BuildChild(keyword.Key, lang.SourceNone)
		argument.Shape.Resolve(keyword.Shape, lang.SourceRegistry)
	}
}

// buildFamilies gives the commands that go through one list, as any_vassal
// and every_vassal, the keys of all of them.
func (b *builder) buildFamilies() {
	families := map[string][]*lang.Field{}
	for _, database := range lang.CommandDatabases {
		for _, name := range slices.Sorted(maps.Keys(b.Databases[database].Definitions)) {
			iterator, ok := lang.MatchListIterator(name)
			if arguments := b.Databases[database].Definitions[name].Field; ok && arguments != nil {
				base := strings.TrimPrefix(name, strings.TrimSuffix(iterator.Key, "*"))
				families[base] = append(families[base], arguments)
			}
		}
	}

	for _, members := range families {
		if len(members) < 2 {
			continue
		}

		united := unite(members)
		for _, member := range members {
			member.Children = united.Children
		}
	}
}

// buildUnions gives each name that is both an effect and a trigger one more
// set of keys, those of both together, for a block that may hold either.
// The names that already share one set of keys are left out.
func (b *builder) buildUnions() {
	for key, effect := range b.Databases[lang.DumpEffects].Definitions {
		trigger := b.Databases[lang.DumpTriggers].Definitions[key]
		if trigger == nil || slices.Contains(sharedWords, key) {
			continue
		}

		union := uniteCommands(effect, trigger)
		effect.Union, trigger.Union = union, union
	}
}

// uniteCommands returns the keys of an effect and a trigger of one name as
// one field. A key keeps what its block holds only where the two agree, or
// where the block may hold effects and triggers alike.
func uniteCommands(effect, trigger *lang.Definition) *lang.Field {
	fields := slices.DeleteFunc([]*lang.Field{effect.Field, trigger.Field}, func(field *lang.Field) bool {
		return field == nil
	})
	united := unite(fields)
	keepSharedShapes(united, fields)

	return united
}

// unite returns one field that holds everything the given fields hold, the
// first to state a claim winning, with keys of one name united in turn.
func unite(fields []*lang.Field) *lang.Field {
	united := &lang.Field{}
	members := map[string][]*lang.Field{}
	for _, field := range fields {
		if field.Source != lang.SourceNone && (united.Source == lang.SourceNone || field.Source < united.Source) {
			united.Source = field.Source
		}

		united.Take(field.Claims)
		united.Keys.Take(field.Keys)
		addVanilla(&united.Vanilla, field.Vanilla)

		for key, child := range field.Children {
			members[key] = append(members[key], child)
		}
	}

	for key, children := range members {
		if united.Children == nil {
			united.Children = map[string]*lang.Field{}
		}

		united.Children[key] = unite(children)
	}

	return united
}

// keepSharedShapes drops what a united field's block holds unless every
// field it came from says the same, or the block may hold effects and
// triggers alike. It does the same for each key inside.
func keepSharedShapes(united *lang.Field, fields []*lang.Field) {
	takes := lang.ShapeRows[united.Shape.Value].Takes
	takesBoth := slices.Contains(takes, lang.DumpEffects) && slices.Contains(takes, lang.DumpTriggers)
	if slices.ContainsFunc(fields, func(field *lang.Field) bool {
		return field.Shape.Value != united.Shape.Value && (field.Shape.IsStated() || !takesBoth)
	}) {
		united.Shape = lang.Claim[lang.Shape]{}
	}

	for key, child := range united.Children {
		var children []*lang.Field
		for _, field := range fields {
			if field.Children[key] != nil {
				children = append(children, field.Children[key])
			}
		}

		keepSharedShapes(child, children)
	}
}
