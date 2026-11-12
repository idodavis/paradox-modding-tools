// hover.go builds the hover card for a key or a value at a position in a
// file: what it is, what it takes or says and where the game defines it.

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
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Card is a hover card: what a key or a value is, and the facts about it
// in the words and the order the page shows them.
type Card struct {
	Title string `json:"title"`

	// What is what the word is: its database's singular, "effect" or
	// "keyword". Class is an operator's class, "" for any other word.
	What  string `json:"what"`
	Class string `json:"class,omitempty"`

	Blurb string `json:"blurb,omitempty"`

	// Quote is the game's own text for the word in the workspace's language:
	// a localization key's text, or the title of a definition.
	Quote string `json:"quote,omitempty"`

	// Description is the text the game shows as a definition's description.
	// Comment is the comment lines just above the definition, without their
	// hashes.
	Description string `json:"description,omitempty"`
	Comment     string `json:"comment,omitempty"`

	// Rows are the card's facts, in the order rowLabels lists their labels.
	Rows []CardRow `json:"rows,omitempty"`

	// Lexicon is the id of the lexicon class that lists the word. It is ""
	// for a word the lexicon does not list.
	Lexicon string `json:"lexicon,omitempty"`

	// Stale is whether a fact comes from dumps older than the install.
	// Inferred is whether one was learned from the game's own files.
	Stale    bool `json:"stale,omitempty"`
	Inferred bool `json:"inferred"`

	// Node is the Event Graph's id of the name. It is "" for a name the
	// graph does not draw.
	Node string `json:"node,omitempty"`

	// Within is the key of the definition the position is in, then the keys
	// of the blocks inside it that hold the position.
	Within []string `json:"within,omitempty"`

	// Color is the CSS color the word stands for, "" for none.
	Color string `json:"color,omitempty"`

	Sites []Site `json:"sites,omitempty"`

	// Range is the word the card is about.
	Range jomini.Range `json:"range"`

	// Token is the token type the editor colors the card's word by, as
	// ListLegend names it. It is "" for a word with no color of its own.
	Token string `json:"token,omitempty"`
}

// A CardRow is one fact of a card: a label, and the fact in words.
type CardRow struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// A Class is one class of the wiki's lexicon, by which a card and a
// completion name the commands the game declares.
type Class struct {
	// ID is the class's own name, such as effect. Label is the name of its
	// tab in the lexicon, such as Effects.
	ID    string `json:"id"`
	Label string `json:"label"`

	// Database is the database of the dumps that declares the class's
	// commands. It is "" for a class of no commands.
	Database string `json:"-"`
}

// A cursor is the innermost statement of a document at an offset, with what
// the walk decided there.
type cursor struct {
	tree   *jomini.Tree
	offset int

	// statement is 0, the file, if no statement holds the offset.
	statement int32

	context *schema.Context
}

// Classes holds the lexicon's classes, in the order its tabs show them.
var Classes = []Class{
	{ID: "effect", Label: "Effects", Database: game.DumpEffects},
	{ID: "trigger", Label: "Triggers", Database: game.DumpTriggers},
	{ID: "link", Label: "Event targets", Database: game.DumpLinks},
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

// rowLabels holds the labels of a card's rows, in the order a card lists
// them.
var rowLabels = []string{
	rowCallee, rowRequirement, rowScopes, rowTakes, rowWrites, rowReturns, rowTarget, rowBlock, rowDate, rowNumber,
	rowWeight, rowImage,
}

// cardParameter is what a card says a parameter is: a $NAME$ in a scripted
// definition, or a key in a call of one.
const cardParameter = "parameter"

// Hover returns the hover card for the key or value at a position in a file.
// It returns nil if the position is on nothing.
func (lsp *Service) Hover(path string, line, column int) (*Card, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	offset := jomini.ToOffset(tree.Src, tree.Lines, line, column)
	if place.Format != files.FormatLocalization {
		return colorCard(document, lsp.describeHit(ws, mods, place, findCursor(document, offset))), nil
	}

	card, reference := describeMessage(ws, document, offset)
	switch {
	case card == nil || reference.Name == "":
	case reference.Database == game.Localization:
		describeKey(ws, mods, place, card, reference)
	default:
		describeDefinitions(card, ws, mods, place, reference)
	}

	return colorCard(document, card), nil
}

// describeHit returns the hover card for the key or value a cursor is on in
// a script file. It returns nil if the cursor is on nothing a card can
// describe.
func (lsp *Service) describeHit(ws *workspace.Session, mods *index.Index, place workspace.Place, cursor cursor) *Card {
	span, reference := findHit(cursor)
	if span.IsEmpty() {
		return nil
	}

	tree, context := cursor.tree, cursor.context
	reference = resolveHit(mods, place, reference, context.References)
	card := &Card{
		Title: tree.Unquoted(span), Range: jomini.ToRange(tree.Src, tree.Lines, span), Within: listAncestors(cursor),
	}

	hitsKey := tree.Statements[cursor.statement].Key.Contains(cursor.offset)
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, mods, place, card, reference)

	case context.Category == schema.CategoryKeyword && hitsKey:
		// a keyword the dumps also declare, as NOT, is the language's word
		// before it is the dump's trigger
		describeKeyword(card, cursor)

	case findClass(reference.Database).ID != "":
		// a use of a name the dumps declare; a key a pattern makes has the
		// pattern's own
		declared := cmp.Or(ws.Schema.LookupDefinition(reference.Database, reference.Name), context.Command)
		describeCommand(card, cursor, span, declared)
		card.Stale = ws.Schema.Dumps.Stale

	case reference.Name != "":
		describeDefinitions(card, ws, mods, place, reference)
		if reference.Database == game.Constant {
			describeNumber(card, tree, reference.Span)
		}

	case tree.Statements[cursor.statement].Value.Contains(cursor.offset):
		describeValue(card, ws, cursor)
		if date, ok := findDate(tree, cursor.statement, context); ok {
			mod, _ := lsp.Workspace.LookupMod(place.Origin)
			card.add(rowDate, describeDate(mod.Calendar, date))
		}

	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, cursor, span, context.Command)
		card.Stale = ws.Schema.Dumps.Stale

	case context.Category == schema.CategoryField:
		describeField(card, cursor)
		card.Stale = ws.Schema.Dumps.Stale

	case context.Category == schema.CategoryParameter:
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

// describeKeyword fills a card for the keyword that the cursor's statement
// is written under: its class, what it does and, for a branch of a random
// list, its weight among its siblings.
func describeKeyword(card *Card, cursor cursor) {
	keyword := cursor.context.Keyword
	card.What, card.Class, card.Blurb = "keyword", string(keyword.Class), keyword.Blurb
	if keyword.Weighted {
		card.add(rowWeight, describeWeight(cursor.tree, cursor.statement))
	}
}

// findHit returns the span a cursor is on in its statement, and its
// reference if it names one. The span is empty if the cursor is on neither
// the key nor the value.
func findHit(cursor cursor) (jomini.Span, schema.Reference) {
	if cursor.statement == 0 {
		return jomini.Span{}, schema.Reference{}
	}

	context, offset, current := cursor.context, cursor.offset, cursor.tree.Statements[cursor.statement]
	// a definition named by a value in its block, as a texticon by its icon,
	// has its name in a statement of its own
	if definition := context.Definition; definition != nil && definition.Span.Contains(offset) {
		named := schema.Reference{Database: definition.Database, Name: definition.Name, Span: definition.Span}

		return definition.Span, named
	}

	for _, reference := range context.References {
		if reference.Span.Contains(offset) {
			return reference.Span, reference
		}
	}

	switch {
	case current.Value.Contains(offset):
		return current.Value, schema.Reference{}

	case current.Key.Contains(offset) && context.Category == schema.CategoryLink:
		for _, link := range jomini.Links(cursor.tree.Text(current.Key), current.Key.Start) {
			if link.Span.Contains(offset) {
				return link.Span, schema.Reference{}
			}
		}

	case current.Key.Contains(offset):
		return current.Key, schema.Reference{}

	case current.Key.IsEmpty() && current.Tag.Contains(offset):
		return current.Tag, schema.Reference{}
	}

	return jomini.Span{}, schema.Reference{}
}

// findCursor returns the cursor at an offset of a document. A document the
// walk does not read, such as a localization file's, gives the file itself
// with an empty context.
func findCursor(document schema.Document, offset int) cursor {
	cursor := cursor{tree: document.Tree, offset: offset, context: &schema.Context{}}
	if document.Contexts != nil {
		cursor.statement = document.Tree.FindStatement(offset)
		cursor.context = &document.Contexts[cursor.statement]
	}

	return cursor
}

// listAncestors returns the key of the definition a cursor is in, then the
// keys of the blocks inside it that hold the cursor. A name script saves,
// such as a scope, starts none. It returns nil outside any definition.
func listAncestors(cursor cursor) []string {
	var keys []string
	tree, statement := cursor.tree, cursor.statement
	for context := cursor.context; context != nil; context = context.Parent {
		current := tree.Statements[statement]
		if context.Defines && context.Category == schema.CategoryDefinition {
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

// describeMessage returns the card for a position in a localization file,
// and the reference of the name it is on: the message's own key, or a key,
// an icon, a format, a game concept or a quoted name its text writes. On a
// step of a data call that names nothing, the card is the function's and the
// reference is empty. The card is nil if the position is on nothing.
func describeMessage(ws *workspace.Session, document schema.Document, offset int) (*Card, schema.Reference) {
	if document.Messages == nil {
		return nil, schema.Reference{}
	}

	tree, messages := document.Tree, document.Messages.Messages
	position := slices.IndexFunc(messages, func(message loc.Message) bool { return message.Span.Contains(offset) })
	if position < 0 || position >= len(document.MessageContexts) {
		return nil, schema.Reference{}
	}

	context := &document.MessageContexts[position]
	references := slices.Concat([]schema.Reference{{
		Database: game.Localization, Name: context.Definition.Name, Span: context.Definition.Span,
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
			return card, schema.Reference{}
		}
	}

	return nil, schema.Reference{}
}

// describeKey fills a card with a localization key's text and the messages
// that define it.
func describeKey(ws *workspace.Session, mods *index.Index, place workspace.Place, card *Card, reference schema.Reference) {
	text, _, _ := ws.LookupText(mods, reference.Name)
	sites, winner := listSites(ws, mods, place, reference)
	card.What, card.Quote, card.Sites = "localization key", text, MakeSites(ws, mods, sites, winner)
}

// describeFunction fills a card for the step of a data call at an offset,
// and returns false if there is none. A step of a call nested in an argument
// gets its card too, found with no data context.
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
	card.add(rowReturns, schema.ReturnType(function))

	return true
}

// findNestedCall returns the call in an argument of the given call that
// covers an offset, or false if none does.
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

// returnsLabels holds what a call of a scripted definition gives, for the
// shape of its block.
var returnsLabels = map[game.Shape]string{game.ShapeTriggers: "yes or no", game.ShapeValue: "a number"}

// describeDefinitions fills a card for a name that definitions define: what
// they are, where they are, the picture and color they stand for, and what
// the first of them says of itself.
func describeDefinitions(card *Card, ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) {
	sites, winner := listSites(ws, mods, place, reference)
	entries := listEntries(mods, sites)
	card.What = game.DescribeDefinition(reference.Database, false)
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

// listSites returns the definitions a reference names as seen from the given
// place, with the kept ones first. It also returns the winner the game
// keeps, which is nil if it keeps none.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) ([]index.Site, *index.Winner) {
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

// dropTranslations returns the sites without the messages in languages other
// than the key's: the file's own language, else the first of the session's
// languages, whichever first has a message among the sites.
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

// findKept returns the winner the game keeps for a reference, as the file
// that the position names in the index sees it. A file the index does not
// list, -1, sees no local definition. It returns false if the game keeps
// none.
func findKept(ws *workspace.Session, mods *index.Index, reference schema.Reference, position int32) (index.Winner, bool) {
	if reference.Database == game.Localization {
		_, winner, ok := ws.LookupText(mods, reference.Name)

		return winner, ok
	}

	return mods.FindKeptLocal(ws.Schema, reference, position)
}

// listEntries returns the given definitions that really define their name.
// It leaves out a second name of a definition, such as a trait's group, a
// name a command makes, such as a saved scope, and a name a definition only
// switches on.
func listEntries(mods *index.Index, sites []index.Site) []index.Site {
	return slices.DeleteFunc(slices.Clone(sites), func(site index.Site) bool { return !mods.Occurrence(site).Own })
}

// findDefinedColor returns the CSS color that a winner writes as its block,
// as a named color does. It returns "" if the winner is nil or its block is
// no color.
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

// describeImage returns the size, the format and the mip levels of the image
// file a reference names, such as "256×256 dds, BC7_UNORM, 9 mip levels". It
// returns "" if the reference is not to a file or no loaded image has the
// path.
func describeImage(mods *index.Index, reference schema.Reference) string {
	if reference.Database != game.File {
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

// describeEntry fills a card with what a definition says of itself: its
// localized name and description, the comment just above it and, for a
// scripted effect or trigger, the parameters it takes.
func describeEntry(card *Card, ws *workspace.Session, mods *index.Index, site index.Site) {
	card.Quote = ws.LookupDefinitionText(mods, site, game.TitleKey)
	card.Description = ws.LookupDefinitionText(mods, site, game.DescriptionKey)
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
		parameters := slices.Sorted(maps.Keys(schema.ListParameters(tree, statement).Children))
		card.add(rowTakes, strings.Join(parameters, ", "))
	}
}

// describeNumber lists on a card the number that the text of a span stands
// for. It lists nothing if the text is no number.
func describeNumber(card *Card, tree *jomini.Tree, span jomini.Span) {
	if number, ok := tree.ResolveNumber(span); ok {
		card.add(rowNumber, strconv.FormatFloat(number, 'f', -1, 64))
	}
}

// describeValue fills a card for a value that is a parameter, a constant, a
// data call or one of its key's choices.
func describeValue(card *Card, ws *workspace.Session, cursor cursor) {
	switch cursor.context.ValueGroup {
	case schema.CategoryParameter:
		card.What = cardParameter

	case schema.CategoryConstant:
		card.What = "constant"
		describeNumber(card, cursor.tree, cursor.tree.Statements[cursor.statement].Value)

	case schema.CategoryCall:
		describeFunction(card, ws, cursor.tree, *cursor.context.Call, cursor.context.Datacontext, cursor.offset)

	case schema.CategoryChoice:
		// a choice, such as an event's type, is named by its key
		field := cursor.context.Field
		card.What, card.Blurb = cursor.tree.Unquoted(cursor.tree.Statements[cursor.statement].Key), field.Blurb.Value
		describeTakes(card, field)
	}
}

// findDate returns the date a statement writes as its value. It returns
// false unless the statement's field expects a date and the value is one
// that can be.
func findDate(tree *jomini.Tree, statement int32, context *schema.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != schema.ValueTypeDate {
		return jomini.Date{}, false
	}

	date, ok := jomini.ParseDate(tree.Unquoted(tree.Statements[statement].Value))

	return date, ok && date.IsValid()
}

// describeDate returns a date in words, read in a mod's calendar: its day,
// its month's name, its year and the era it falls in, such as
// "15 September 1066". The month has the usual name if the calendar names
// none. A date with no day or no month leaves it out, and so does a year
// before the calendar's first era.
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

// describeWeight returns a random-list branch's number as a percent of the
// numbers of all its siblings, rounded, such as "25% of its siblings". It
// returns "" if the percent rounds to 0.
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

// describeCommand fills a card for an effect, a trigger or a link of the
// dumps, written as the given span of the cursor's statement. The whole key
// of a statement with no block takes a value. A link of a block's key, a
// link before the last of a chain and a link written as a value lead to
// their scope. A nil command fills nothing.
func describeCommand(card *Card, cursor cursor, span jomini.Span, command *schema.Definition) {
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
		card.add(rowTakes, describeExpected(schema.Expected{Type: schema.ValueTypeTarget, Scope: command.Target}))
	}
}

// findClass returns the lexicon class of the commands that the given
// database of the dumps declares. It returns the zero Class for any other
// database.
func findClass(database string) Class {
	position := slices.IndexFunc(Classes, func(class Class) bool {
		return class.Database != "" && class.Database == database
	})
	if position < 0 {
		return Class{}
	}

	return Classes[position]
}

// describeScopes returns the given types of scope in words, such as
// "character, landed title". It returns "" for none.
func describeScopes(scopes []string) string {
	phrases := make([]string, len(scopes))
	for position, scope := range scopes {
		phrases[position] = schema.ToPhrase(scope)
	}

	return strings.Join(phrases, ", ")
}

// describeScope returns the words for one scope of the given type, such as
// "a landed title" or "an army".
func describeScope(scope string) string {
	phrase := schema.ToPhrase(scope)
	if strings.ContainsAny(phrase[:min(len(phrase), 1)], "aeio") {
		return "an " + phrase
	}

	return "a " + phrase
}

// describeField fills a card for the field of a definition that the cursor's
// statement writes: what it does, whether it is required, what it takes, its
// weight among its siblings and, for a parameter of a call, the scripted
// definition it is given to. A field no source states the value of lists the
// eight values the game's own files write most.
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

	card.Inferred = card.Inferred || field.Shape.Source == game.SourceVanilla
	if field.Weighted {
		card.add(rowWeight, describeWeight(tree, cursor.statement))
	}

	if context.Parent.Callee != nil {
		call := tree.Statements[cursor.statement].Parent
		card.What = cardParameter
		card.add(rowCallee, tree.Unquoted(tree.Statements[call].Key))
	}
}

// describeTakes lists on a card what a field's value takes, and marks the
// card inferred where the game's own files are what state it. It reports
// whether a source states it: a nil field and a field no source states list
// nothing.
func describeTakes(card *Card, field *schema.Field) bool {
	if field == nil || !field.Value.IsStated() {
		return false
	}

	expected := field.Value.Value
	takes := describeExpected(expected)
	if expected.Values {
		takes += ", or a script value"
	}

	card.add(rowTakes, takes)
	card.Inferred = field.Value.Source == game.SourceVanilla

	return true
}

// blockLabels holds what a card says a block holds, for the shape of the
// block.
var blockLabels = map[game.Shape]string{
	game.ShapeFields: "fields", game.ShapeTriggers: "triggers", game.ShapeEffects: "effects",
	game.ShapeParameters: "its arguments",
	game.ShapeArguments:  "what the command takes", game.ShapeEffectArguments: "what the effect takes",
	game.ShapeTriggerArguments: "what the trigger takes", game.ShapeValue: "the steps that work out a number",
	game.ShapeWidget: "a widget's properties and children", game.ShapeModifiers: "modifiers",
	game.ShapeDescription: "texts, each with the trigger that picks it", game.ShapeSetup: "history fields and effects",
}

// describeBlock lists on a card what the block of the cursor's statement
// holds. The card of a link before the last of a chain, which the given span
// is, lists nothing: the block is the whole chain's.
func describeBlock(card *Card, cursor cursor, span jomini.Span) {
	current, shape := cursor.tree.Statements[cursor.statement], cursor.context.Shape
	if !current.IsBlock() || shape == game.ShapeUnknown {
		return
	}

	if current.Key.Contains(span.Start) && span.End < current.Key.End {
		return
	}

	card.add(rowBlock, cmp.Or(blockLabels[shape], string(shape)))
}

// add lists a fact on the card under a label, in the place rowLabels gives
// the label. An empty text lists nothing.
func (c *Card) add(label, text string) {
	if text == "" {
		return
	}

	position, _ := slices.BinarySearchFunc(c.Rows, label, func(row CardRow, label string) int {
		return cmp.Compare(slices.Index(rowLabels, row.Label), slices.Index(rowLabels, label))
	})
	c.Rows = slices.Insert(c.Rows, position, CardRow{Label: label, Text: text})
}

// colorCard sets the token type of the word a card is about, so a page can
// draw the card in the color the editor gives the word, and returns the
// card. A nil card stays nil.
func colorCard(document schema.Document, card *Card) *Card {
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
