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
	Title    string          `json:"title"`
	What     string          `json:"what"`            // what it is: the database's singular, "effect", "keyword"
	Class    string          `json:"class,omitempty"` // an operator's class
	Blurb    string          `json:"blurb,omitempty"`
	Quote    string          `json:"quote,omitempty"`   // the game's own text for the word: a localization key's
	Takes    schema.Expected `json:"takes"`             // what the key's value may be
	Need     string          `json:"need,omitempty"`    // whether a field is required: optional, always or conditional
	About    string          `json:"about,omitempty"`   // what the database says of its definitions
	Writes   []string        `json:"writes,omitempty"`  // the game's eight most written values for an undeclared field
	Returns  string          `json:"returns,omitempty"` // what a call of a scripted definition gives
	Command  string          `json:"command,omitempty"` // the kind of command a workspace's scripted definition acts as
	Lexicon  string          `json:"lexicon,omitempty"` // the id of the lexicon class that lists it; "" for none
	Holds    string          `json:"holds,omitempty"`   // what its block holds, in words
	Leads    string          `json:"leads,omitempty"`   // the scope its block runs in, where a command or link moves it
	Target   string          `json:"target,omitempty"`  // the scope its value must be for a command or link with no block
	Stale    bool            `json:"stale,omitempty"`   // a fact comes from dumps older than the install
	Weight   int             `json:"weight,omitempty"`  // a branch of a random list: its percent of the branches' numbers
	Node     string          `json:"node,omitempty"`    // the Event Graph's id of the name; "" if it is not drawn
	Within   []string        `json:"within,omitempty"`  // the definition the position is in, then the blocks inside it
	Runs     string          `json:"runs,omitempty"`    // the scopes the dumps give a command, else "The game does not say"
	Date     *CardDate       `json:"date,omitempty"`
	Color    string          `json:"color,omitempty"`  // a CSS color
	Number   *float64        `json:"number,omitempty"` // the number a constant stands for
	Image    *texture.Header `json:"image,omitempty"`
	Sites    []Site          `json:"sites,omitempty"`
	Inferred bool            `json:"inferred"` // a fact was learned from the game's files
	Range    Range           `json:"range"`    // what the card is about
}

// A Class is one class of the wiki's lexicon, by which a card and a completion name the commands the game declares.
type Class struct {
	ID       string `json:"id"`    // such as effect
	Label    string `json:"label"` // the lexicon tab's, such as Effects
	Declared string `json:"-"`     // the way the game declares the class's commands; "" for a class of no commands
}

// Classes holds the lexicon's classes, in the order its tabs show them.
var Classes = []Class{
	{ID: "effect", Label: "Effects", Declared: game.DeclaredEffect},
	{ID: "trigger", Label: "Triggers", Declared: game.DeclaredTrigger},
	{ID: "link", Label: "Event targets", Declared: game.DeclaredLink},
	{ID: "scope", Label: "Scopes"},
	{ID: "function", Label: "Functions"},
}

// returnsLabels holds what a call of a scripted definition gives, for the shape of its block.
var returnsLabels = map[string]string{game.ShapeTriggers: "yes or no", game.ShapeValue: "a number"}

// blockLabels holds what a card says a block holds, for the shape of the block.
var blockLabels = map[string]string{
	game.ShapeFields: "fields", game.ShapeTriggers: "triggers", game.ShapeEffects: "effects",
	game.ShapeArguments: "what the command takes", game.ShapeEffectArguments: "what the effect takes",
	game.ShapeTriggerArguments: "what the trigger takes", game.ShapeValue: "the steps that work out a number",
	game.ShapeWidget: "a widget's properties and children", game.ShapeModifiers: "modifiers",
	game.ShapeDescription: "texts, each with the trigger that picks it", "setup": "history fields and effects",
}

// Hover returns the hover card for the key or value at a position in a file. It returns nil if the position is on
// nothing.
func (lsp *Service) Hover(path string, line, column int) (*Card, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	if place.Format == files.FormatLocalization {
		card, reference := describeMessage(ws, tree, place, offset)
		if card != nil && reference.Name != "" {
			describeKey(ws, mods, place, card, reference)
		}
		return card, nil
	}
	visitor := &hoverVisitor{tree: tree, offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	card := lsp.describeHit(ws, mods, place, visitor)
	if card != nil {
		card.Lexicon = findLexicon(card)
	}
	return card, nil
}

// describeHit returns the hover card for the key or value the visitor stopped on in a script file. It returns nil if
// the cursor is on nothing a card can describe.
func (lsp *Service) describeHit(ws *workspace.Session, mods *index.Index, place workspace.Place, visitor *hoverVisitor) *Card {
	span, reference := findHit(visitor)
	if span.IsEmpty() {
		return nil
	}
	tree, context := visitor.tree, &visitor.context
	reference = resolveHit(mods, place, reference, context.References)
	card := &Card{
		Title: tree.Unquoted(span), Range: ToRange(tree.Src, tree.Lines, span), Within: visitor.ancestors,
	}
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, mods, place, card, reference)
	case reference.Name != "":
		sites, winner := listSites(ws, mods, place, reference)
		card.What, card.Sites, card.Image, card.Color = game.DescribeDefinition(reference.Database, false),
			MakeSites(ws, mods, listEntries(ws, mods, sites), winner), describeImage(mods, reference), findDefinedColor(ws, mods, winner)
		if slices.Contains(ws.Schema.FireDatabases, reference.Database) {
			card.Node = reference.ToID()
		}
		if database := ws.Schema.Databases[reference.Database]; database != nil {
			card.About, card.Returns, card.Command = database.Definition.Blurb.Value, returnsLabels[database.Callable.Shape],
				lo.Ternary(len(sites) > 0, database.Callable.Command, "")
		}
		if reference.Database == game.Constant {
			describeNumber(card, tree, reference.Span)
		}
	case tree.Statements[visitor.statement].Value.Contains(visitor.offset):
		describeValue(card, ws, visitor)
		if date, ok := findDate(tree, visitor.statement, context); ok {
			mod, _ := lsp.Workspace.LookupMod(place.Origin)
			card.Date = resolveDate(mod.Calendar, date)
		}
	case context.Category == schema.CategoryKeyword:
		keyword := context.Keyword
		card.What, card.Class, card.Blurb = "keyword", keyword.Class, keyword.Blurb
		if keyword.Weighted {
			card.Weight = resolveWeight(tree, visitor.statement)
		}
	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, visitor)
		card.Stale = ws.Schema.Dumps.Stale
	case context.Category == schema.CategoryField:
		describeField(card, context)
		card.Stale = ws.Schema.Dumps.Stale
	case context.Category == schema.CategoryParameter:
		card.What = "parameter"
	}
	describeBlock(card, visitor)
	if card.What == "" && card.Date == nil && card.Color == "" {
		return nil
	}
	return card
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

// A hoverVisitor finds the innermost statement of a tree at an offset, and its context.
type hoverVisitor struct {
	tree      *jomini.Tree
	offset    int
	statement int32 // 0, the file, if there is none
	context   schema.Context
	ancestors []string // the definition's name, then the keys of the blocks that hold the offset
}

// Enter keeps the context when the statement covers the offset, with the name of the definition the offset is in and
// the keys of the blocks inside it.
func (visitor *hoverVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	current := tree.Statements[statement]
	if !current.Span.Contains(visitor.offset) {
		return true
	}
	visitor.statement, visitor.context = statement, *context
	visitor.context.References = slices.Clone(context.References)
	switch {
	case context.Defines:
		visitor.ancestors = []string{context.Definition.Name}
	case context.Definition != nil && !current.Key.IsEmpty() && current.Block.Contains(visitor.offset):
		visitor.ancestors = append(visitor.ancestors, tree.Unquoted(current.Key))
	}
	return true
}

// Leave does nothing.
func (visitor *hoverVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

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
		for _, candidate := range loc.Segments(message.Text, message.TextSpan.Start) {
			if candidate.Span.Contains(offset) {
				segment = candidate
			}
		}
		switch segment.Markup {
		case loc.MarkupKey:
			key, span = segment.Text, segment.TextSpan
		case loc.MarkupCall:
			call, ok := jomini.ParseCall(segment.Text, segment.TextSpan.Start)
			card := &Card{}
			if !ok || !describeFunction(card, ws, tree, call, "", offset) {
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
		kept, _, _ := mods.ResolveSite(winner, site)
		return kept
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
func findHit(visitor *hoverVisitor) (jomini.Span, schema.Reference) {
	if visitor.statement == 0 {
		return jomini.Span{}, schema.Reference{}
	}
	context, offset, current := &visitor.context, visitor.offset, visitor.tree.Statements[visitor.statement]
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
		start := current.Key.Start
		for _, link := range jomini.SplitChain(visitor.tree.Text(current.Key)) {
			span := jomini.Span{Start: start, End: start + len(link)}
			if span.Contains(offset) {
				return span, schema.Reference{}
			}
			start = span.End + 1
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

// describeValue fills a card for a value that is a parameter, a constant or a data call.
func describeValue(card *Card, ws *workspace.Session, visitor *hoverVisitor) {
	switch visitor.context.ValueGroup {
	case schema.CategoryParameter:
		card.What = "parameter"
	case schema.CategoryConstant:
		card.What = "constant"
		describeNumber(card, visitor.tree, visitor.tree.Statements[visitor.statement].Value)
	case schema.CategoryCall:
		describeFunction(card, ws, visitor.tree, *visitor.context.Call, visitor.context.Datacontext, visitor.offset)
	}
}

// findDate returns the date a statement writes as its value. It returns false unless the statement's field expects a
// date and the value is one that can be.
func findDate(tree *jomini.Tree, statement int32, context *schema.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != schema.ValueDate {
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
func describeCommand(card *Card, visitor *hoverVisitor) {
	card.What = findClass(visitor.context.Declaration).ID
	command := visitor.context.Command
	if command == nil {
		return
	}
	card.Blurb, card.Runs = command.Blurb, cmp.Or(strings.Join(command.Scopes, ", "), "The game does not say")
	if visitor.tree.Statements[visitor.statement].IsBlock() {
		card.Leads = command.Target
	} else {
		card.Target = command.Target
		describeTakes(card, command.ArgumentsOf(visitor.context.Declaration))
	}
}

// findClass returns the lexicon class of the commands the game declares in the given way. It returns the zero Class
// for a way no class has.
func findClass(declaration string) Class {
	class, _ := lo.Find(Classes, func(class Class) bool { return class.Declared != "" && class.Declared == declaration })
	return class
}

// describeField fills a card for a field of a definition: what it does, whether it is required and what it takes.
func describeField(card *Card, context *schema.Context) {
	field := context.Field
	card.What, card.Blurb, card.Need = "field", field.Blurb.Value, field.Required.Value
	describeTakes(card, field)
	if !field.Value.IsStated() {
		values := slices.SortedFunc(maps.Keys(field.Vanilla.Values), func(first, second string) int {
			return cmp.Or(field.Vanilla.Values[second]-field.Vanilla.Values[first], strings.Compare(first, second))
		})
		card.Writes = values[:min(len(values), 8)]
	}
	card.Inferred = card.Inferred || field.Shape.Source == game.SourceVanilla
}

// describeTakes gives a card the type a field's value takes, where a source states one, and marks the card inferred
// where the game's own files state it. A nil field takes nothing.
func describeTakes(card *Card, field *schema.Field) {
	if field != nil && field.Value.IsStated() {
		card.Takes, card.Inferred = field.Value.Value, field.Value.Source == game.SourceVanilla
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
func describeBlock(card *Card, visitor *hoverVisitor) {
	if visitor.tree.Statements[visitor.statement].IsBlock() && visitor.context.Shape != game.ShapeUnknown {
		card.Holds = cmp.Or(blockLabels[visitor.context.Shape], visitor.context.Shape)
	}
	if color, ok := jomini.ToColor(visitor.tree, visitor.statement); ok {
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
