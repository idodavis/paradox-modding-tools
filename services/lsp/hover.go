// hover.go builds the hover card for a key or a value at a position in a file: what it is, what it takes or says and
// where the game defines it.

package lsp

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

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

// A CardDate is a date read in the mod's calendar: its era and the month's name as the mod names it.
type CardDate struct {
	Date  jomini.Date `json:"date"`
	Era   string      `json:"era,omitempty"`
	Month string      `json:"month"`
}

// A Card is a hover card: the facts about a key or a value, for the page to lay out.
type Card struct {
	Title       string          `json:"title"`
	What        string          `json:"what"`            // what it is: the database's singular, "effect", "keyword"
	Class       string          `json:"class,omitempty"` // an operator's class
	Blurb       string          `json:"blurb,omitempty"`
	Quote       string          `json:"quote,omitempty"`       // the game's own text for the word, in the workspace's language: a localization key's text, or the title of a definition
	Takes       string          `json:"takes,omitempty"`       // what the key's value may be, in words
	Need        string          `json:"need,omitempty"`        // whether a field is required: optional, always or conditional
	Description string          `json:"description,omitempty"` // the text the game shows as an entry's description
	Comment     string          `json:"comment,omitempty"`     // the comment lines just above an entry, without their hashes
	Parameters  []string        `json:"parameters,omitempty"`  // the parameters a scripted effect or trigger takes, without dollars
	Callee      string          `json:"callee,omitempty"`      // the scripted effect or trigger a parameter is given to; "" for any other key
	Writes      []string        `json:"writes,omitempty"`      // the game's eight most written values for an undeclared field
	Returns     string          `json:"returns,omitempty"`     // what a call of a scripted definition gives
	Command     string          `json:"command,omitempty"`     // the kind of command a workspace's scripted definition acts as
	Lexicon     string          `json:"lexicon,omitempty"`     // the id of the lexicon class that lists it; "" for none
	Holds       string          `json:"holds,omitempty"`       // what its block holds, in words
	Leads       string          `json:"leads,omitempty"`       // the scope its block runs in, where a command or link moves it
	Target      string          `json:"target,omitempty"`      // the scope its value must be for a command or link with no block
	Stale       bool            `json:"stale,omitempty"`       // a fact comes from dumps older than the install
	Weight      int             `json:"weight,omitempty"`      // a branch of a random list: its percent of the branches' numbers
	Node        string          `json:"node,omitempty"`        // the Event Graph's id of the name; "" if it is not drawn
	Within      []string        `json:"within,omitempty"`      // the definition the position is in, then the blocks inside it
	Runs        string          `json:"runs,omitempty"`        // the scopes the dumps give a command, else "The game does not say"
	Date        *CardDate       `json:"date,omitempty"`
	Color       string          `json:"color,omitempty"`  // a CSS color
	Number      *float64        `json:"number,omitempty"` // the number a constant stands for
	Image       *texture.Header `json:"image,omitempty"`
	Sites       []Site          `json:"sites,omitempty"`
	Inferred    bool            `json:"inferred"` // a fact was learned from the game's files
	Range       Range           `json:"range"`    // what the card is about
}

// A Class is one class of the wiki's lexicon, by which a card and a completion name the commands the game declares.
type Class struct {
	ID       string           `json:"id"`    // such as effect
	Label    string           `json:"label"` // the lexicon tab's, such as Effects
	Declared game.Declaration `json:"-"`     // the way the game declares the class's commands; "" for a class of no commands
}

// Classes holds the lexicon's classes, in the order its tabs show them.
var Classes = []Class{
	{ID: "effect", Label: "Effects", Declared: game.DeclarationEffect},
	{ID: "trigger", Label: "Triggers", Declared: game.DeclarationTrigger},
	{ID: "link", Label: "Event targets", Declared: game.DeclarationLink},
	{ID: "scope", Label: "Scopes"},
	{ID: "function", Label: "Functions"},
}

// cardParameter is what a card says a parameter is: a $NAME$ in a scripted definition, or a key in a call of one.
const cardParameter = "parameter"

// returnsLabels holds what a call of a scripted definition gives, for the shape of its block.
var returnsLabels = map[game.Shape]string{game.ShapeTriggers: "yes or no", game.ShapeValue: "a number"}

// blockLabels holds what a card says a block holds, for the shape of the block.
var blockLabels = map[game.Shape]string{
	game.ShapeFields: "fields", game.ShapeTriggers: "triggers", game.ShapeEffects: "effects",
	game.ShapeArguments: "what the command takes", game.ShapeEffectArguments: "what the effect takes",
	game.ShapeTriggerArguments: "what the trigger takes", game.ShapeValue: "the steps that work out a number",
	game.ShapeWidget: "a widget's properties and children", game.ShapeModifiers: "modifiers",
	game.ShapeDescription: "texts, each with the trigger that picks it", game.ShapeSetup: "history fields and effects",
}

// Hover returns the hover card for the key or value at a position in a file. It returns nil if the position is on
// nothing.
func (lsp *Service) Hover(path string, line, column int) (*Card, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	if place.Format == files.FormatLocalization {
		card, reference := describeMessage(ws, tree, place, offset)
		if card != nil && reference.Name != "" {
			describeKey(ws, mods, place, card, reference)
		}
		return card, nil
	}
	card := lsp.describeHit(ws, mods, place, findCursor(document, offset))
	if card != nil {
		card.Lexicon = findLexicon(card)
	}
	return card, nil
}

// describeHit returns the hover card for the key or value the visitor stopped on in a script file. It returns nil if
// the cursor is on nothing a card can describe.
func (lsp *Service) describeHit(ws *workspace.Session, mods *index.Index, place workspace.Place, cursor cursor) *Card {
	span, reference := findHit(cursor)
	if span.IsEmpty() {
		return nil
	}
	tree, context := cursor.tree, cursor.context
	reference = resolveHit(mods, place, reference, context.References)
	card := &Card{
		Title: tree.Unquoted(span), Range: ToRange(tree.Src, tree.Lines, span), Within: listAncestors(cursor),
	}
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, mods, place, card, reference)
	case reference.Name != "":
		describeDefinitions(card, ws, mods, place, reference)
		if reference.Database == game.Constant {
			describeNumber(card, tree, reference.Span)
		}
	case tree.Statements[cursor.statement].Value.Contains(cursor.offset):
		describeValue(card, ws, cursor)
		if date, ok := findDate(tree, cursor.statement, context); ok {
			mod, _ := lsp.Workspace.LookupMod(place.Origin)
			card.Date = resolveDate(mod.Calendar, date)
		}
	case context.Category == schema.CategoryKeyword:
		keyword := context.Keyword
		card.What, card.Class, card.Blurb = "keyword", string(keyword.Class), keyword.Blurb
		if keyword.Weighted {
			card.Weight = resolveWeight(tree, cursor.statement)
		}
	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, cursor)
		card.Stale = ws.Schema.Dumps.Stale
	case context.Category == schema.CategoryField:
		describeField(card, context)
		card.Stale = ws.Schema.Dumps.Stale
		if context.Parent.Callee != nil {
			call := tree.Statements[cursor.statement].Parent
			card.What, card.Callee = cardParameter, tree.Unquoted(tree.Statements[call].Key)
		}
	case context.Category == schema.CategoryParameter:
		card.What = cardParameter
	}
	describeBlock(card, cursor)
	if card.What == "" && card.Date == nil && card.Color == "" {
		return nil
	}
	return card
}

// describeDefinitions fills a card for a name that definitions define: what they are, where they are, the picture and
// color they stand for, and what the first of them says of itself.
func describeDefinitions(card *Card, ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) {
	sites, winner := listSites(ws, mods, place, reference)
	entries := listEntries(ws, mods, sites)
	card.What, card.Sites, card.Image, card.Color = game.DescribeDefinition(reference.Database, false),
		MakeSites(ws, mods, entries, winner), describeImage(mods, reference), findDefinedColor(ws, mods, winner)
	if slices.Contains(ws.Schema.FireDatabases, reference.Database) {
		card.Node = reference.ToID()
	}
	if database := ws.Schema.Databases[reference.Database]; database != nil {
		card.Returns, card.Command = returnsLabels[database.Callable.Shape], lo.Ternary(len(sites) > 0, string(database.Callable.Command), "")
	}
	if len(entries) > 0 {
		describeEntry(card, ws, mods, entries[0])
	}
}

// describeEntry fills a card with what a definition says of itself: its localized name and description, the comment
// just above it and, for a scripted effect or trigger, the parameters it takes.
func describeEntry(card *Card, ws *workspace.Session, mods *index.Index, site index.Site) {
	card.Quote = ws.LookupDefinitionText(mods, site, game.TitleKey)
	card.Description = ws.LookupDefinitionText(mods, site, game.DescriptionKey)
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return
	}
	lines := strings.Split(tree.FindComment(statement), "\n")
	card.Comment = strings.Join(lo.Map(lines, func(line string, _ int) string {
		return strings.TrimSpace(strings.TrimLeft(line, "#"))
	}), "\n")
	if database := ws.Schema.Databases[mods.ToReference(site).Database]; database != nil && database.Callable.Parameters {
		card.Parameters = slices.Sorted(maps.Keys(schema.ListParameters(tree, statement).Children))
	}
}

// listEntries returns the given definitions that really define their name. It leaves out a second name of a definition,
// such as a trait's group, a name a command makes, such as a saved scope, and a name a definition only switches on.
func listEntries(ws *workspace.Session, mods *index.Index, sites []index.Site) []index.Site {
	return lo.Reject(sites, func(site index.Site, _ int) bool {
		occurrence := mods.Occurrence(site)
		database := mods.Databases.Texts[occurrence.Database]
		return occurrence.Alias || slices.Contains(game.Links, database) || occurrence.Nested && ws.Game.IsSet(database)
	})
}

// A cursor is the innermost statement of a document at an offset, with what
// the walk decided there.
type cursor struct {
	tree      *jomini.Tree
	offset    int
	statement int32 // 0, the file, if there is none
	context   *schema.Context
}

// findCursor returns the cursor at an offset of a document. A document the
// walk does not read, such as a localization file's, gives the file itself
// with an empty context.
func findCursor(document workspace.Document, offset int) cursor {
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

// describeMessage returns the card for a position in a localization file, and the key's reference. The card is nil if
// the position is on nothing.
func describeMessage(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) (*Card, schema.Reference) {
	document, err := ws.LoadDocument(place.Path)
	if err != nil || document.Messages == nil {
		return nil, schema.Reference{}
	}
	messages := document.Messages.Messages
	position := slices.IndexFunc(messages, func(message loc.Message) bool { return message.Span.Contains(offset) })
	if position < 0 {
		return nil, schema.Reference{}
	}
	message := messages[position]
	key, span := message.Key, message.KeySpan
	if !span.Contains(offset) {
		segment := loc.Segment{}
		for _, candidate := range message.Segments {
			if candidate.Span.Contains(offset) {
				segment = candidate
			}
		}
		switch segment.Markup {
		case loc.MarkupKey:
			key, span = segment.Text, segment.TextSpan
		case loc.MarkupCall:
			card := &Card{}
			if segment.Call == nil || !describeFunction(card, ws, tree, *segment.Call, "", offset) {
				return nil, schema.Reference{}
			}
			return card, schema.Reference{}
		default:
			return nil, schema.Reference{}
		}
	}
	card := &Card{Title: key, Range: ToRange(tree.Src, tree.Lines, span)}
	return card, schema.Reference{Database: game.Localization, Name: key, Span: span}
}

// describeKey fills a card with a localization key's text and the messages that define it.
func describeKey(ws *workspace.Session, mods *index.Index, place workspace.Place, card *Card, reference schema.Reference) {
	text, _, _ := ws.LookupText(mods, reference.Name)
	sites, winner := listSites(ws, mods, place, reference)
	card.What, card.Quote, card.Sites = "localization key", text, MakeSites(ws, mods, sites, winner)
}

// describeFunction fills a card for the step of a data call at an offset, and returns false if there is none. A step of
// a call nested in an argument gets its card too, found with no data context.
func describeFunction(card *Card, ws *workspace.Session, tree *jomini.Tree, call jomini.Call, datacontext string, offset int) bool {
	position := slices.IndexFunc(call.Steps, func(candidate jomini.Step) bool { return candidate.Span.Contains(offset) })
	if position < 0 {
		nested, ok := findNestedCall(call, offset)
		return ok && describeFunction(card, ws, tree, nested, "", offset)
	}
	step := call.Steps[position]
	function, _ := ws.Schema.FindFunction(call, datacontext, position)
	card.What, card.Blurb, card.Returns = "data function", function.Blurb, schema.ReturnType(function)
	card.Title, card.Range = step.Text, ToRange(tree.Src, tree.Lines, step.Span)
	return true
}

// findNestedCall returns the call in an argument of the given call that covers an offset, or false if none does.
func findNestedCall(call jomini.Call, offset int) (jomini.Call, bool) {
	arguments := lo.FlatMap(call.Steps, func(step jomini.Step, _ int) []jomini.Argument { return step.Arguments })
	argument, ok := lo.Find(arguments, func(argument jomini.Argument) bool {
		return argument.Call != nil && argument.Call.Span.Contains(offset)
	})
	if !ok {
		return jomini.Call{}, false
	}
	return *argument.Call, true
}

// listSites returns the definitions a reference names as seen from the given place, with the kept ones first. It also
// returns the winner the game keeps, which is nil if it keeps none.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) ([]index.Site, *index.Winner) {
	sites := dropTranslations(ws, mods, place, mods.ResolveLocal(reference, place.Position))
	winner, ok := findKept(ws, mods, reference, place.Position)
	if !ok {
		return sites, nil
	}
	keeping, others := lo.FilterReject(sites, func(site index.Site, _ int) bool {
		return mods.ResolveSite(winner, site).Kept
	})
	return slices.Concat(keeping, others), &winner
}

// dropTranslations returns the sites without the messages in languages other than the key's: the file's own language,
// else the workspace's, else the base language, whichever first has a message among the sites.
func dropTranslations(ws *workspace.Session, mods *index.Index, place workspace.Place, sites []index.Site) []index.Site {
	for _, language := range []string{loc.FindSuffixLanguage(place.Path), ws.Language, game.BaseLanguage} {
		hasMessage := slices.ContainsFunc(sites, func(site index.Site) bool {
			return mods.Occurrence(site).Role == index.RoleLocalizes && mods.Language(site) == language
		})
		if language == "" || !hasMessage {
			continue
		}
		return lo.Reject(sites, func(site index.Site, _ int) bool {
			return mods.File(site).Format == files.FormatLocalization && mods.Language(site) != language
		})
	}
	return sites
}

// findKept returns the winner the game keeps for a reference, as the file at the given position in the index sees it.
// A file the index does not list, -1, sees no local definition. It returns false if the game keeps none.
func findKept(ws *workspace.Session, mods *index.Index, reference schema.Reference, position int32) (index.Winner, bool) {
	if reference.Database == game.Localization {
		_, winner, ok := ws.LookupText(mods, reference.Name)
		return winner, ok
	}
	return mods.FindKeptLocal(ws.Schema, reference, position)
}

// findHit returns the span the cursor is on in the visited statement, and its reference if it names one. The span is
// empty if the cursor is on neither the key nor the value.
func findHit(cursor cursor) (jomini.Span, schema.Reference) {
	if cursor.statement == 0 {
		return jomini.Span{}, schema.Reference{}
	}
	context, offset, current := cursor.context, cursor.offset, cursor.tree.Statements[cursor.statement]
	// a definition named by a value in its block, as a texticon by its icon, has its name in a statement of its own
	if definition := context.Definition; definition != nil && definition.Span.Contains(offset) {
		return definition.Span, schema.Reference{Database: definition.Database, Name: definition.Name, Span: definition.Span}
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

// describeImage returns the header of the image file a reference names. It returns nil if the reference is not to a
// file or no loaded image has the path.
func describeImage(mods *index.Index, reference schema.Reference) *texture.Header {
	if reference.Database != game.File {
		return nil
	}
	path, ok := mods.ResolveTexture(reference.Name)
	if !ok {
		return nil
	}
	header, err := texture.ReadHeader(path)
	if err != nil {
		return nil
	}
	return &header
}

// findDefinedColor returns the CSS color that a winner writes as its block, as a named color does. It returns "" if
// the winner is nil or its block is no color.
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

// describeNumber gives a card the number that the text at the given span stands for. The card has none if the text is
// no number.
func describeNumber(card *Card, tree *jomini.Tree, span jomini.Span) {
	if number, ok := tree.ResolveNumber(span); ok {
		card.Number = &number
	}
}

// describeValue fills a card for a value that is a parameter, a constant, a data call or one of its key's choices.
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
		describeChoice(card, cursor)
	}
}

// describeChoice fills a card for a value that is one of the choices its key takes, such as an event's type: it names
// the key, and gives the choices and what the key's field says of itself.
func describeChoice(card *Card, cursor cursor) {
	field := cursor.context.Field
	card.What, card.Blurb = cursor.tree.Unquoted(cursor.tree.Statements[cursor.statement].Key), field.Blurb.Value
	describeTakes(card, field)
}

// findDate returns the date a statement writes as its value. It returns false unless the statement's field expects a
// date and the value is one that can be.
func findDate(tree *jomini.Tree, statement int32, context *schema.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != schema.ValueTypeDate {
		return jomini.Date{}, false
	}
	date, ok := jomini.ParseDate(tree.Unquoted(tree.Statements[statement].Value))
	return date, ok && date.IsValid()
}

// resolveDate returns a date in the given calendar of a mod: the era it falls in and its month's name. The month has
// the usual name if the calendar gives none.
func resolveDate(calendar app.Calendar, date jomini.Date) *CardDate {
	card := &CardDate{Date: date}
	if date.Month >= 1 && date.Month <= 12 {
		card.Month = time.Month(date.Month).String()
		if len(calendar.Months) == 12 {
			card.Month = calendar.Months[date.Month-1]
		}
	}
	eras := lo.Filter(calendar.Eras, func(era app.Era, _ int) bool { return era.Year <= date.Year })
	card.Era = lo.MaxBy(eras, func(era, latest app.Era) bool { return era.Year > latest.Year }).Name
	return card
}

// describeCommand fills a card for an effect, a trigger or a link. A command the schema does not know gets only what
// it is.
func describeCommand(card *Card, cursor cursor) {
	card.What = findClass(cursor.context.Declaration).ID
	command := cursor.context.Command
	if command == nil {
		return
	}
	card.Blurb, card.Runs = command.Blurb, cmp.Or(strings.Join(command.Scopes, ", "), "The game does not say")
	if cursor.tree.Statements[cursor.statement].IsBlock() {
		card.Leads = command.Target
	} else {
		card.Target = command.Target
		describeTakes(card, cursor.context.Parent.FindArguments(command, cursor.context.Declaration))
	}
}

// findClass returns the lexicon class of the commands the game declares in the given way. It returns the zero Class
// for a way no class has.
func findClass(declaration game.Declaration) Class {
	class, _ := lo.Find(Classes, func(class Class) bool { return class.Declared != "" && class.Declared == declaration })
	return class
}

// describeField fills a card for a field of a definition: what it does, whether it is required and what it takes.
func describeField(card *Card, context *schema.Context) {
	field := context.Field
	card.What, card.Blurb, card.Need = "field", field.Blurb.Value, string(field.Required.Value)
	describeTakes(card, field)
	if !field.Value.IsStated() {
		values := slices.SortedFunc(maps.Keys(field.Vanilla.Values), func(first, second string) int {
			return cmp.Or(field.Vanilla.Values[second]-field.Vanilla.Values[first], strings.Compare(first, second))
		})
		card.Writes = values[:min(len(values), 8)]
	}
	card.Inferred = card.Inferred || field.Shape.Source == game.SourceVanilla
}

// describeTakes gives a card the words for what a field's value takes, where a source states it, and marks the card
// inferred where the game's own files state it. A nil field takes nothing.
func describeTakes(card *Card, field *schema.Field) {
	if field != nil && field.Value.IsStated() {
		expected := field.Value.Value
		card.Takes = describeExpected(expected) + lo.Ternary(expected.Values, ", or a script value", "")
		card.Inferred = field.Value.Source == game.SourceVanilla
	}
}

// resolveWeight returns a random-list branch's number as a percent of the numbers of all its siblings, rounded. It
// returns 0 if the numbers add up to 0.
func resolveWeight(tree *jomini.Tree, statement int32) int {
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
	if sum == 0 {
		return 0
	}
	return int(math.Round(own * 100 / sum))
}

// describeBlock fills a card for a statement that holds a block: what the block holds and, for a color, the color.
func describeBlock(card *Card, cursor cursor) {
	if cursor.tree.Statements[cursor.statement].IsBlock() && cursor.context.Shape != game.ShapeUnknown {
		card.Holds = cmp.Or(blockLabels[cursor.context.Shape], string(cursor.context.Shape))
	}
	if color, ok := jomini.ToColor(cursor.tree, cursor.statement); ok {
		card.Color = color.ToCSS()
	}
}

// findLexicon returns the id of the lexicon class that lists what a card is about, or "" if none does.
func findLexicon(card *Card) string {
	if slices.ContainsFunc(Classes, func(class Class) bool { return class.ID == card.What }) {
		return card.What
	}
	return card.Command
}
