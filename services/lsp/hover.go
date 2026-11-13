// hover.go builds the hover card for a word of a file.

package lsp

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Card is a hover card: what a key or a value is, and its facts.
type Card struct {
	Title string `json:"title"`

	// What is what the word is, such as "effect". Class is an operator's
	// class, "" for any other word.
	What  string `json:"what"`
	Class string `json:"class,omitempty"`

	Blurb string `json:"blurb,omitempty"`

	// Quote is the game's text for the word: a key's text or a definition's
	// title.
	Quote string `json:"quote,omitempty"`

	// Description is the game's description of a definition. Comment is the
	// comment lines above it, without hashes.
	Description string `json:"description,omitempty"`
	Comment     string `json:"comment,omitempty"`

	// Rows are the card's facts, in the order rowLabels lists their labels.
	Rows []CardRow `json:"rows,omitempty"`

	// Lexicon is the id of the lexicon class that lists the word, "" if none.
	Lexicon string `json:"lexicon,omitempty"`

	// Stale is whether a fact comes from dumps older than the install, and
	// Inferred whether one comes from the game's files only.
	Stale    bool `json:"stale,omitempty"`
	Inferred bool `json:"inferred"`

	// Node is the Event Graph's id of the name, "" if the graph does not draw
	// it.
	Node string `json:"node,omitempty"`

	// Color is the CSS color the word stands for, "" for none.
	Color string `json:"color,omitempty"`

	Sites []Site `json:"sites,omitempty"`

	// Range is the word the card is about.
	Range jomini.Range `json:"range"`

	// Token is the token type the editor colors the word by, as ListLegend
	// names it, "" for none.
	Token string `json:"token,omitempty"`
}

// A CardRow is one fact of a card: a label and the fact in words.
type CardRow struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// A Class is one class of the wiki's lexicon of commands.
type Class struct {
	// ID is the class's name, such as effect. Label is its tab's name, such as
	// Effects.
	ID    string `json:"id"`
	Label string `json:"label"`

	// Database is the dumps' database that declares the class's commands, ""
	// for none.
	Database string `json:"-"`
}

// A cursor is the innermost statement at an offset, with what the walk decided
// there.
type cursor struct {
	tree   *jomini.Tree
	offset int

	// statement is 0, the file, if no statement holds the offset.
	statement int32

	context *lang.Context
}

// Classes holds the lexicon's classes in the order of its tabs.
var Classes = []Class{
	{ID: "effect", Label: "Effects", Database: lang.DumpEffects},
	{ID: "trigger", Label: "Triggers", Database: lang.DumpTriggers},
	{ID: "link", Label: "Event targets", Database: lang.DumpLinks},
	{ID: "scope", Label: "Scopes"},
	{ID: "function", Label: "Functions"},
}

// The labels of a card's rows.
const (
	rowCallee      = "Given to"
	rowRequirement = "Needed"
	rowScopes      = "Supported scopes"
	rowTakes       = "Takes"
	rowWrites      = "Usually"
	rowReturns     = "Returns"
	rowTarget      = "Leads to"
	rowBlock       = "Inside its block"
	rowDate        = "Date"
	rowNumber      = "Number"
	rowWeight      = "Weight"
	rowImage       = "Image"
)

// rowLabels lists the row labels in the order a card shows them.
var rowLabels = []string{
	rowCallee, rowRequirement, rowScopes, rowTakes, rowWrites, rowReturns, rowTarget, rowBlock, rowDate, rowNumber,
	rowWeight, rowImage,
}

// cardParameter is what a card calls a $NAME$ or a key in a call.
const cardParameter = "parameter"

// Hover returns the hover card for the word at a position, or nil if it is on
// nothing.
func (s *Service) Hover(path string, line, column int) (*Card, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	offset := jomini.ToOffset(tree.Src, tree.Lines, line, column)
	if place.Format != files.FormatLocalization {
		return colorCard(document, s.describeHit(ws, mods, place, findCursor(document, offset))), nil
	}

	card, reference := describeMessage(ws, document, offset)
	switch {
	case card == nil || reference.Name == "":
	case reference.Database == lang.Localization:
		describeKey(ws, mods, place, card, reference)
	default:
		describeDefinitions(card, ws, mods, place, reference)
	}

	return colorCard(document, card), nil
}

// findCursor returns the cursor at an offset. A document the walk does not read
// gives the file itself with an empty context.
func findCursor(document lang.Document, offset int) cursor {
	cursor := cursor{tree: document.Tree, offset: offset, context: &lang.Context{}}
	if document.Contexts != nil {
		cursor.statement = document.Tree.FindStatement(offset)
		cursor.context = &document.Contexts[cursor.statement]
	}

	return cursor
}

// describeHit returns the card for what a cursor is on in a script file, or
// nil.
func (s *Service) describeHit(ws *workspace.Session, mods *index.Index, place workspace.Place, cursor cursor) *Card {
	span, reference := findHit(cursor)
	if span.IsEmpty() {
		return nil
	}

	tree, context := cursor.tree, cursor.context
	reference = resolveHit(mods, place, reference, context.References)
	card := &Card{
		Title: tree.Unquoted(span), Range: jomini.ToRange(tree.Src, tree.Lines, span),
	}

	hitsKey := tree.Statements[cursor.statement].Key.Contains(cursor.offset)
	switch {
	case reference.Database == lang.Localization:
		describeKey(ws, mods, place, card, reference)

	case context.Category == lang.CategoryKeyword && hitsKey:
		// a keyword the dumps also declare, as NOT, is the language's word
		// first
		describeKeyword(card, cursor)

	case findClass(reference.Database).ID != "":
		// a key a pattern makes has the pattern's command
		declared := cmp.Or(ws.Schema.LookupDefinition(reference.Database, reference.Name), context.Command)
		describeCommand(card, cursor, span, declared)
		card.Stale = ws.Schema.Dumps.Stale

	case reference.Name != "":
		describeDefinitions(card, ws, mods, place, reference)
		if reference.Database == lang.Constant {
			describeNumber(card, tree, reference.Span)
		}

	case tree.Statements[cursor.statement].Value.Contains(cursor.offset):
		describeValue(card, ws, cursor)
		if date, ok := findDate(tree, cursor.statement, context); ok {
			mod, _ := s.Workspace.LookupMod(place.Origin)
			card.add(rowDate, describeDate(mod.Calendar, date))
		}

	case context.Category == lang.CategoryCommand || context.Category == lang.CategoryLink:
		describeCommand(card, cursor, span, context.Command)
		card.Stale = ws.Schema.Dumps.Stale

	case context.Category == lang.CategoryField:
		describeField(card, cursor)
		card.Stale = ws.Schema.Dumps.Stale

	case context.Category == lang.CategoryParameter:
		card.What = cardParameter
	}

	if color, ok := jomini.ToColor(tree, cursor.statement); ok {
		card.Color = color.ToCSS()
	}

	if card.What == "" && len(card.Rows) == 0 && card.Color == "" {
		return nil
	}

	describeBlock(card, cursor, span)

	return card
}

// findHit returns the span a cursor is on and its reference, if it names one.
// The span is empty if the cursor is on nothing.
func findHit(cursor cursor) (jomini.Span, lang.Reference) {
	if cursor.statement == 0 {
		return jomini.Span{}, lang.Reference{}
	}

	context, offset, current := cursor.context, cursor.offset, cursor.tree.Statements[cursor.statement]

	// a texticon names its definition by its icon, in a statement of its own
	if definition := context.Definition; definition != nil && definition.Span.Contains(offset) {
		named := lang.Reference{Database: definition.Database, Name: definition.Name, Span: definition.Span}

		return definition.Span, named
	}

	for _, reference := range context.References {
		if reference.Span.Contains(offset) {
			return reference.Span, reference
		}
	}

	switch {
	case current.Value.Contains(offset):
		return current.Value, lang.Reference{}

	case current.Key.Contains(offset) && context.Category == lang.CategoryLink:
		for _, link := range jomini.Links(cursor.tree.Text(current.Key), current.Key.Start) {
			if link.Span.Contains(offset) {
				return link.Span, lang.Reference{}
			}
		}

	case current.Key.Contains(offset):
		return current.Key, lang.Reference{}

	case current.Key.IsEmpty() && current.Tag.Contains(offset):
		return current.Tag, lang.Reference{}
	}

	return jomini.Span{}, lang.Reference{}
}

// ListAncestors returns the key of the definition a position is in, then the
// keys of the blocks around it, whether or not a word is there. It returns
// nil outside a definition and in a file the walk does not read.
func (s *Service) ListAncestors(path string, line, column int) ([]string, error) {
	_, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree := document.Tree

	return listAncestors(findCursor(document, jomini.ToOffset(tree.Src, tree.Lines, line, column))), nil
}

// listAncestors returns the key of the definition a cursor is in, then the keys
// of its blocks around the cursor. It returns nil outside a definition.
func listAncestors(cursor cursor) []string {
	var keys []string
	tree, statement := cursor.tree, cursor.statement
	for context := cursor.context; context != nil; context = context.Parent {
		current := tree.Statements[statement]
		if context.Defines && context.Category == lang.CategoryDefinition {
			keys = append(keys, context.Definition.Name)
			slices.Reverse(keys)

			return keys
		}

		if !current.Key.IsEmpty() && current.Block.Contains(cursor.offset) {
			keys = append(keys, tree.Unquoted(current.Key))
		}

		statement = current.Parent
	}

	return nil
}

// describeKey fills a card with a localization key's text and the messages that
// define it.
func describeKey(ws *workspace.Session, mods *index.Index, place workspace.Place, card *Card, reference lang.Reference) {
	text, _, _ := ws.LookupText(mods, reference.Name)
	sites, winner := listSites(ws, mods, place, reference)
	card.What, card.Quote, card.Sites = "localization key", text, MakeSites(ws, mods, sites, winner)
}

// listSites returns the definitions a reference names, the kept ones first, and
// the winner the game keeps, nil if none.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference lang.Reference) ([]index.Site, *index.Winner) {
	sites := dropTranslations(ws, mods, place, mods.ResolveLocal(reference, place.Position))
	winner, ok := findKept(ws, mods, reference, place.Position)
	if !ok {
		return sites, nil
	}

	var kept, others []index.Site
	for _, site := range sites {
		if mods.ResolveSite(winner, site).Kept {
			kept = append(kept, site)
		} else {
			others = append(others, site)
		}
	}

	return slices.Concat(kept, others), &winner
}

// dropTranslations returns the sites without the messages of other languages
// than the file's own, else the session's first.
func dropTranslations(ws *workspace.Session, mods *index.Index, place workspace.Place, sites []index.Site) []index.Site {
	for _, language := range slices.Concat([]string{loc.FindSuffixLanguage(place.Path)}, ws.Languages()) {
		hasMessage := slices.ContainsFunc(sites, func(site index.Site) bool {
			return mods.Occurrence(site).Role == index.RoleLocalizes && mods.Language(site) == language
		})
		if language == "" || !hasMessage {
			continue
		}

		return slices.DeleteFunc(slices.Clone(sites), func(site index.Site) bool {
			return mods.File(site).Format == files.FormatLocalization && mods.Language(site) != language
		})
	}

	return sites
}

// findKept returns the winner the game keeps for a reference, or false if none.
// A position of -1 sees no local definition.
func findKept(ws *workspace.Session, mods *index.Index, reference lang.Reference, position int32) (index.Winner, bool) {
	if reference.Database == lang.Localization {
		_, winner, ok := ws.LookupText(mods, reference.Name)

		return winner, ok
	}

	return mods.FindKeptLocal(ws.Schema, reference, position)
}

// describeKeyword fills a card for a keyword, with a random-list branch's
// weight.
func describeKeyword(card *Card, cursor cursor) {
	keyword := cursor.context.Keyword
	card.What, card.Class, card.Blurb = "keyword", string(keyword.Class), keyword.Blurb
	if keyword.Weighted {
		card.add(rowWeight, describeWeight(cursor.tree, cursor.statement))
	}
}

// describeWeight returns a random-list branch's share of its siblings, such as
// "25% of its siblings", or "" if it rounds to 0.
func describeWeight(tree *jomini.Tree, statement int32) string {
	own, sum := 0.0, 0.0
	for sibling := range tree.Children(tree.Statements[statement].Parent) {
		number, ok := tree.ResolveNumber(tree.Statements[sibling].Key)
		if !ok {
			continue
		}

		sum += number
		if sibling == statement {
			own = number
		}
	}

	if sum == 0 || math.Round(own*100/sum) == 0 {
		return ""
	}

	return fmt.Sprintf("%.0f%% of its siblings", math.Round(own*100/sum))
}

// findClass returns the lexicon class of a dumps database, or the zero Class.
func findClass(database string) Class {
	position := slices.IndexFunc(Classes, func(class Class) bool {
		return class.Database != "" && class.Database == database
	})
	if position < 0 {
		return Class{}
	}

	return Classes[position]
}

// describeCommand fills a card for an effect, trigger or link of the dumps. A
// key with no block takes a value, and any other part leads to its scope.
func describeCommand(card *Card, cursor cursor, span jomini.Span, command *lang.Definition) {
	if command == nil {
		return
	}

	class := findClass(command.Database)
	card.What, card.Lexicon, card.Blurb = class.ID, class.ID, command.Blurb
	card.add(rowScopes, cmp.Or(describeScopes(command.Scopes), "The game does not say"))
	current := cursor.tree.Statements[cursor.statement]
	if current.IsBlock() || span.End != current.Key.End {
		if command.Target != "" {
			card.add(rowTarget, describeScope(command.Target))
		}

		return
	}

	if !describeTakes(card, cursor.context.Parent.FindArguments(command)) && command.Target != "" {
		card.add(rowTakes, describeExpected(lang.Expected{Type: lang.ValueTypeTarget, Scope: command.Target}))
	}
}

func describeScopes(scopes []string) string {
	phrases := make([]string, len(scopes))
	for position, scope := range scopes {
		phrases[position] = lang.ToPhrase(scope)
	}

	return strings.Join(phrases, ", ")
}

// describeScope returns "a landed title" or "an army" for a scope.
func describeScope(scope string) string {
	phrase := lang.ToPhrase(scope)
	if strings.ContainsAny(phrase[:min(len(phrase), 1)], "aeio") {
		return "an " + phrase
	}

	return "a " + phrase
}

// describeTakes lists what a field's value takes and reports whether a source
// states it. It marks the card inferred where only the game's files do.
func describeTakes(card *Card, field *lang.Field) bool {
	if field == nil || !field.Value.IsStated() {
		return false
	}

	expected := field.Value.Value
	takes := describeExpected(expected)
	if expected.Values {
		takes += ", or a script value"
	}

	card.add(rowTakes, takes)
	card.Inferred = field.Value.Source == lang.SourceVanilla

	return true
}

// returnsLabels says what a call of a scripted definition gives, by block
// shape.
var returnsLabels = map[lang.Shape]string{lang.ShapeTriggers: "yes or no", lang.ShapeValue: "a number"}

// describeDefinitions fills a card for a name that definitions define, with
// what the first of them says of itself.
func describeDefinitions(card *Card, ws *workspace.Session, mods *index.Index, place workspace.Place, reference lang.Reference) {
	sites, winner := listSites(ws, mods, place, reference)
	entries := listEntries(mods, sites)
	card.What = lang.DescribeDefinition(reference.Database, false)
	card.Sites, card.Color = MakeSites(ws, mods, entries, winner), findDefinedColor(ws, mods, winner)
	card.add(rowImage, describeImage(mods, reference))
	if slices.Contains(ws.Schema.FireDatabases, reference.Database) {
		card.Node = reference.ToID()
	}

	if database := ws.Schema.Databases[reference.Database]; database != nil {
		card.add(rowReturns, returnsLabels[database.Declaration.Shape])
		if len(sites) > 0 {
			card.Lexicon = findClass(database.Declaration.Command()).ID
		}
	}

	if len(entries) > 0 {
		describeEntry(card, ws, mods, entries[0])
	}
}

// listEntries returns the sites that really define their name, not a second
// name or one a command makes.
func listEntries(mods *index.Index, sites []index.Site) []index.Site {
	return slices.DeleteFunc(slices.Clone(sites), func(site index.Site) bool { return !mods.Occurrence(site).Own })
}

// findDefinedColor returns the CSS color a winner's block writes, or "".
func findDefinedColor(ws *workspace.Session, mods *index.Index, winner *index.Winner) string {
	if winner == nil {
		return ""
	}

	tree, statement, err := ws.FindStatement(mods, winner.Site)
	if err != nil {
		return ""
	}

	if color, ok := jomini.ToColor(tree, statement); ok {
		return color.ToCSS()
	}

	return ""
}

// describeImage returns the size, format and mip levels of the image a
// reference names, such as "256×256 dds, BC7_UNORM, 9 mip levels", or "".
func describeImage(mods *index.Index, reference lang.Reference) string {
	if reference.Database != lang.File {
		return ""
	}

	path, ok := mods.ResolveTexture(reference.Name)
	if !ok {
		return ""
	}

	header, err := texture.ReadHeader(path)
	if err != nil {
		return ""
	}

	facts := []string{fmt.Sprintf("%d×%d %s", header.Width, header.Height, header.Format)}
	if header.PixelFormat != "" {
		facts = append(facts, header.PixelFormat)
	}

	if header.Mipmaps > 1 {
		facts = append(facts, fmt.Sprintf("%d mip levels", header.Mipmaps))
	}

	return strings.Join(facts, ", ")
}

// describeEntry fills a card with a definition's text, the comment above it
// and, for a scripted effect or trigger, its parameters.
func describeEntry(card *Card, ws *workspace.Session, mods *index.Index, site index.Site) {
	card.Quote = ws.LookupDefinitionText(mods, site, lang.TitleKey)
	card.Description = ws.LookupDefinitionText(mods, site, lang.DescriptionKey)
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return
	}

	lines := strings.Split(tree.FindComment(statement), "\n")
	for position, line := range lines {
		lines[position] = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}

	card.Comment = strings.Join(lines, "\n")
	if database := ws.Schema.Databases[mods.ToReference(site).Database]; database != nil && database.Declaration.Parameters {
		parameters := slices.Sorted(maps.Keys(lang.ListParameters(tree, statement).Children))
		card.add(rowTakes, strings.Join(parameters, ", "))
	}
}

func describeNumber(card *Card, tree *jomini.Tree, span jomini.Span) {
	if number, ok := tree.ResolveNumber(span); ok {
		card.add(rowNumber, strconv.FormatFloat(number, 'f', -1, 64))
	}
}

// describeValue fills a card for a parameter, a constant, a data call or a
// choice of the key.
func describeValue(card *Card, ws *workspace.Session, cursor cursor) {
	switch cursor.context.ValueGroup {
	case lang.CategoryParameter:
		card.What = cardParameter

	case lang.CategoryConstant:
		card.What = "constant"
		describeNumber(card, cursor.tree, cursor.tree.Statements[cursor.statement].Value)

	case lang.CategoryCall:
		describeFunction(card, ws, cursor.tree, *cursor.context.Call, cursor.context.Datacontext, cursor.offset)

	case lang.CategoryChoice:
		// a choice, such as an event's type, is named by its key
		field := cursor.context.Field
		card.What, card.Blurb = cursor.tree.Unquoted(cursor.tree.Statements[cursor.statement].Key), field.Blurb.Value
		describeTakes(card, field)
	}
}

// describeFunction fills a card for the step of a data call at an offset. It
// returns false if there is none.
func describeFunction(card *Card, ws *workspace.Session, tree *jomini.Tree, call jomini.Call, datacontext string, offset int) bool {
	position := slices.IndexFunc(call.Steps, func(step jomini.Step) bool { return step.Span.Contains(offset) })
	if position < 0 {
		nested, ok := findNestedCall(call, offset)

		return ok && describeFunction(card, ws, tree, nested, "", offset)
	}

	step := call.Steps[position]
	function, _ := ws.Schema.FindFunction(call, datacontext, position)
	card.What, card.Blurb = "data function", function.Blurb
	card.Title, card.Range = step.Text, jomini.ToRange(tree.Src, tree.Lines, step.Span)
	card.add(rowReturns, lang.ReturnType(function))

	return true
}

func findNestedCall(call jomini.Call, offset int) (jomini.Call, bool) {
	for _, step := range call.Steps {
		for _, argument := range step.Arguments {
			if argument.Call != nil && argument.Call.Span.Contains(offset) {
				return *argument.Call, true
			}
		}
	}

	return jomini.Call{}, false
}

// findDate returns the date a statement's value writes, if its field expects
// one.
func findDate(tree *jomini.Tree, statement int32, context *lang.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != lang.ValueTypeDate {
		return jomini.Date{}, false
	}

	date, ok := jomini.ParseDate(tree.Unquoted(tree.Statements[statement].Value))

	return date, ok && date.IsValid()
}

// describeDate returns a date in words, read in a mod's calendar, such as "15
// September 1066". It leaves out a missing day, month or era.
func describeDate(calendar app.Calendar, date jomini.Date) string {
	var parts []string
	if date.Day > 0 {
		parts = append(parts, strconv.Itoa(date.Day))
	}

	if date.Month >= 1 && date.Month <= 12 {
		month := time.Month(date.Month).String()
		if len(calendar.Months) == 12 {
			month = calendar.Months[date.Month-1]
		}

		parts = append(parts, month)
	}

	parts = append(parts, strconv.Itoa(date.Year))
	latest, start := "", math.MinInt
	for _, era := range calendar.Eras {
		if era.Year <= date.Year && era.Year > start {
			latest, start = era.Name, era.Year
		}
	}

	parts = append(parts, latest)

	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), " ")
}

// describeField fills a card for the field a statement writes. A field with no
// stated value lists the values the game's files write most. A parameter of a
// call names its definition.
func describeField(card *Card, cursor cursor) {
	tree, context := cursor.tree, cursor.context
	field := context.Field
	card.What, card.Blurb = "field", field.Blurb.Value
	card.add(rowRequirement, string(field.Required.Value))
	if !describeTakes(card, field) {
		values := slices.SortedFunc(maps.Keys(field.Vanilla.Values), func(first, second string) int {
			return cmp.Or(field.Vanilla.Values[second]-field.Vanilla.Values[first], strings.Compare(first, second))
		})
		card.add(rowWrites, strings.Join(values[:min(len(values), 8)], ", "))
	}

	card.Inferred = card.Inferred || field.Shape.Source == lang.SourceVanilla
	if field.Weighted {
		card.add(rowWeight, describeWeight(tree, cursor.statement))
	}

	if context.Parent.Callee != nil {
		call := tree.Statements[cursor.statement].Parent
		card.What = cardParameter
		card.add(rowCallee, tree.Unquoted(tree.Statements[call].Key))
	}
}

// blockLabels says what a card says a block holds, by block shape.
var blockLabels = map[lang.Shape]string{
	lang.ShapeFields: "fields", lang.ShapeTriggers: "triggers", lang.ShapeEffects: "effects",
	lang.ShapeParameters: "its arguments",
	lang.ShapeArguments:  "what the command takes", lang.ShapeEffectArguments: "what the effect takes",
	lang.ShapeTriggerArguments: "what the trigger takes", lang.ShapeValue: "the steps that work out a number",
	lang.ShapeWidget: "a widget's properties and children", lang.ShapeModifiers: "modifiers",
	lang.ShapeDescription: "texts, each with the trigger that picks it", lang.ShapeSetup: "history fields and effects",
}

// describeBlock lists what the cursor's block holds. It lists nothing for a
// link before the last of a chain.
func describeBlock(card *Card, cursor cursor, span jomini.Span) {
	current, shape := cursor.tree.Statements[cursor.statement], cursor.context.Shape
	if !current.IsBlock() || shape == lang.ShapeUnknown {
		return
	}

	if current.Key.Contains(span.Start) && span.End < current.Key.End {
		return
	}

	card.add(rowBlock, cmp.Or(blockLabels[shape], string(shape)))
}

// describeMessage returns the card for a position in a localization file and
// the reference of the name it is on. The reference is empty on a step of a
// data call. The card is nil if the position is on nothing.
func describeMessage(ws *workspace.Session, document lang.Document, offset int) (*Card, lang.Reference) {
	if document.Messages == nil {
		return nil, lang.Reference{}
	}

	tree, messages := document.Tree, document.Messages.Messages
	position := slices.IndexFunc(messages, func(message loc.Message) bool { return message.Span.Contains(offset) })
	if position < 0 || position >= len(document.MessageContexts) {
		return nil, lang.Reference{}
	}

	context := &document.MessageContexts[position]
	references := slices.Concat([]lang.Reference{{
		Database: lang.Localization, Name: context.Definition.Name, Span: context.Definition.Span,
	}}, context.References)
	for _, reference := range references {
		if reference.Database != "" && reference.Span.Contains(offset) {
			card := &Card{Title: reference.Name, Range: jomini.ToRange(tree.Src, tree.Lines, reference.Span)}

			return card, reference
		}
	}

	for _, segment := range messages[position].Segments {
		card := &Card{}
		if segment.Call != nil && segment.Span.Contains(offset) && describeFunction(card, ws, tree, *segment.Call, "", offset) {
			return card, lang.Reference{}
		}
	}

	return nil, lang.Reference{}
}

// colorCard sets the token type of the card's word and returns the card. A nil
// card stays nil.
func colorCard(document lang.Document, card *Card) *Card {
	if card == nil {
		return nil
	}

	tree := document.Tree
	tokens := listTokens(document, jomini.ToSpan(tree.Src, tree.Lines, card.Range), true)
	first := slices.IndexFunc(tokens, func(current token) bool { return slices.Contains(legend, current.category) })
	if first >= 0 {
		card.Token = string(tokens[first].category)
	}

	return card
}

// add lists a fact under a label, in the place rowLabels gives it. An empty
// text lists nothing.
func (c *Card) add(label, text string) {
	if text == "" {
		return
	}

	position, _ := slices.BinarySearchFunc(c.Rows, label, func(row CardRow, label string) int {
		return cmp.Compare(slices.Index(rowLabels, row.Label), slices.Index(rowLabels, label))
	})
	c.Rows = slices.Insert(c.Rows, position, CardRow{Label: label, Text: text})
}
