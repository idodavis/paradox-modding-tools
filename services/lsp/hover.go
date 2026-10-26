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

// A CardScope is one scope that script can name where a statement is written: the link that names it, such as root or
// scope:target, the type the game gives it, and what it is.
type CardScope struct {
	Link  string `json:"link"`
	Type  string `json:"type"`
	About string `json:"about"`
}

// A CardSite is one place a name is defined, with whether the game keeps that definition and why.
type CardSite struct {
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"` // counted from one
	Kept   bool            `json:"kept"`
	Reason string          `json:"reason,omitempty"` // as index.ResolveSite gives it; unsettled where no source says
}

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
	Takes    schema.Expected `json:"takes"`             // what the key's value may be
	Need     string          `json:"need,omitempty"`    // whether a field is required: optional, always or conditional
	About    string          `json:"about,omitempty"`   // what the database says of its definitions
	Writes   []string        `json:"writes,omitempty"`  // the values the game writes most for a field nothing declares, up to eight
	Returns  string          `json:"returns,omitempty"` // what a call of a scripted definition gives
	Holds    string          `json:"holds,omitempty"`   // what its block holds, in words
	Leads    string          `json:"leads,omitempty"`   // the scope its block runs in, for a command or link that switches
	Target   string          `json:"target,omitempty"`  // the scope its value must be, for a command or link with no block
	Stale    bool            `json:"stale,omitempty"`   // a fact comes from dumps older than the install
	Weight   int             `json:"weight,omitempty"`  // a branch of a random list: its percent of the branches' numbers
	Node     string          `json:"node,omitempty"`    // the event graph's id of the name; "" if the graph does not draw it
	Within   []string        `json:"within,omitempty"`  // the definition the position is in, then the blocks inside it
	Scopes   []CardScope     `json:"scopes,omitempty"`
	Date     *CardDate       `json:"date,omitempty"`
	Color    string          `json:"color,omitempty"`  // a CSS color
	Number   *float64        `json:"number,omitempty"` // the number a constant stands for
	Image    *texture.Header `json:"image,omitempty"`
	Sites    []CardSite      `json:"sites,omitempty"`
	Inferred bool            `json:"inferred"` // a fact was learned from the game's files
	Range    Range           `json:"range"`    // what the card is about
}

// declaredLabels holds what a card calls a command, for each way the game declares one.
var declaredLabels = map[string]string{
	game.DeclaredEffect: "effect", game.DeclaredTrigger: "trigger", game.DeclaredLink: "link",
}

// returnsLabels holds what a call of a scripted definition gives, for the shape of its block.
var returnsLabels = map[string]string{game.ShapeTriggers: "yes or no", game.ShapeValue: "a number"}

// blockLabels holds what a card says a block holds, for the shape of the block.
var blockLabels = map[string]string{
	game.ShapeFields: "fields", game.ShapeTriggers: "triggers", game.ShapeEffects: "effects",
	game.ShapeArguments: "what the command takes", game.ShapeValue: "the steps that work out a number",
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
		return describeLocalization(ws, tree, place, offset), nil
	}
	visitor := &hoverVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	return lsp.describeHit(ws, tree, place, visitor), nil
}

// describeHit returns the hover card for what the given visitor found at the cursor in a script file. It returns nil if
// the cursor is on nothing a card can describe.
func (lsp *Service) describeHit(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, visitor *hoverVisitor) *Card {
	span, reference := findHit(tree, visitor)
	if span.IsEmpty() {
		return nil
	}
	mods, context := ws.Mods.Load(), &visitor.context
	reference = resolveHit(mods, place, reference, context.References)
	card := &Card{
		Title: tree.Unquoted(span), Range: ToRange(tree.Src, tree.Lines, span), Scopes: listScopes(context),
		Within: visitor.within,
	}
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, place, card, reference)
	case reference.Name != "":
		card.What = game.DescribeDefinition(reference.Database, false)
		card.Sites, card.Image, card.Color = listSites(ws, mods, place, reference), describeImage(mods, reference),
			findDefinedColor(ws, mods, place, reference)
		if slices.Contains(ws.Schema.FireDatabases, reference.Database) {
			card.Node = reference.ToID()
		}
		if database := ws.Schema.Databases[reference.Database]; database != nil {
			card.About, card.Returns = database.Definition.Blurb.Value, returnsLabels[database.Callable.Shape]
		}
		if reference.Database == game.Constant {
			describeNumber(card, tree, reference.Span)
		}
	case tree.Statements[visitor.statement].Value.Contains(visitor.offset):
		describeValue(card, ws, tree, visitor)
		if date, ok := findDate(tree, visitor.statement, context); ok {
			mod, _ := lsp.Workspace.LookupMod(place.Origin)
			card.Date = resolveDate(mod.Calendar, date)
		}
	case context.Category == schema.CategoryKeyword:
		keyword := findKeyword(tree, visitor)
		card.What, card.Class, card.Blurb = "keyword", keyword.Class, keyword.Blurb
		if keyword.Weighted {
			card.Weight = resolveWeight(tree, visitor.statement)
		}
	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, tree, visitor)
		card.Stale = ws.Schema.Dumps.Stale
	case context.Category == schema.CategoryField:
		describeField(card, context)
		card.Stale = ws.Schema.Dumps.Stale
	case context.Category == schema.CategoryParameter:
		card.What = "parameter"
	}
	describeBlock(card, tree, visitor)
	if card.What == "" && card.Date == nil && card.Color == "" {
		return nil
	}
	return card
}

// A hoverVisitor finds the innermost statement at an offset and its context.
type hoverVisitor struct {
	offset    int
	statement int32 // 0, the file, if there is none
	context   schema.Context
	within    []string // the definition's name, then the keys of the blocks that hold the offset
}

// Enter keeps the context when the statement covers the offset. It also keeps the name of the definition the offset is
// in and the keys of the blocks inside it.
func (visitor *hoverVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	current := tree.Statements[statement]
	if !current.Span.Contains(visitor.offset) {
		return true
	}
	visitor.statement, visitor.context = statement, *context
	visitor.context.References = slices.Clone(context.References)
	switch {
	case context.Defines:
		visitor.within = []string{context.Definition.Name}
	case context.Definition != nil && !current.Key.IsEmpty() && current.Block.Contains(visitor.offset):
		visitor.within = append(visitor.within, tree.Unquoted(current.Key))
	}
	return true
}

// Leave does nothing.
func (visitor *hoverVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// describeLocalization returns the hover card for a position in a localization file. A card for a key has the key's
// text and the places that define it. It returns nil if the position is on nothing.
func describeLocalization(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) *Card {
	card, reference := describeMessage(ws, tree, place, offset)
	if card != nil && reference.Name != "" {
		describeKey(ws, place, card, reference)
	}
	return card
}

// describeMessage returns the card for a position in a localization file, and the key's reference. The card is nil if
// the position is on nothing.
func describeMessage(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) (*Card, schema.Reference) {
	file := loc.Parse(tree.Src, place.Path)
	position := slices.IndexFunc(file.Messages, func(message loc.Message) bool { return message.Span.Contains(offset) })
	if position < 0 {
		return nil, schema.Reference{}
	}
	message := file.Messages[position]
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
			return describeCall(ws, tree, segment, offset), schema.Reference{}
		default:
			return nil, schema.Reference{}
		}
	}
	card := &Card{Title: key, Range: ToRange(tree.Src, tree.Lines, span)}
	return card, schema.Reference{Database: game.Localization, Name: key, Span: span}
}

// describeKey fills a card with a localization key's text and the messages that define it.
func describeKey(ws *workspace.Session, place workspace.Place, card *Card, reference schema.Reference) {
	mods := ws.Mods.Load()
	text, _, _ := ws.LookupText(mods, reference.Name)
	card.What, card.Blurb, card.Sites = "localization key", text, listSites(ws, mods, place, reference)
	card.Scopes = nil
}

// describeCall returns the card for the step of a data call at the given offset. It returns nil if the offset is on no
// step.
func describeCall(ws *workspace.Session, tree *jomini.Tree, segment loc.Segment, offset int) *Card {
	call, ok := jomini.ParseCall(segment.Text, segment.TextSpan.Start)
	position := slices.IndexFunc(call.Steps, func(step jomini.Step) bool { return step.Span.Contains(offset) })
	if !ok || position < 0 {
		return nil
	}
	card := &Card{}
	function, _ := ws.Schema.FindFunction(call, "", position)
	describeFunction(card, tree, call.Steps[position], function)
	return card
}

// describeFunction fills a card for one step of a data call: what the function returns and says.
func describeFunction(card *Card, tree *jomini.Tree, step jomini.Step, function schema.DataFunction) {
	card.What, card.Blurb, card.Returns = "data function", function.Blurb, function.Returns
	card.Title, card.Range = step.Text, ToRange(tree.Src, tree.Lines, step.Span)
}

// listSites returns the definitions a reference names, kept ones first, with whether the game keeps each and why.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) []CardSite {
	var kept *index.Winner
	if winner, ok := findKept(ws, mods, reference, place.Path); ok {
		kept = &winner
	}
	return describeSites(ws, mods, dropTranslations(ws, mods, place, mods.ResolveLocal(reference, place.Path)), kept)
}

// dropTranslations returns the sites without the messages in other languages than the one the key is shown in.
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

// describeSites returns definitions as the sites of a card, with whether a winner keeps each and why, as
// index.ResolveSite says. The kept ones come first. None is kept if the winner is nil.
func describeSites(ws *workspace.Session, mods *index.Index, sites []index.Site, winner *index.Winner) []CardSite {
	cards := lo.Map(sites, func(site index.Site, _ int) CardSite {
		card := CardSite{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
		if winner != nil {
			card.Kept, card.Reason = mods.ResolveSite(*winner, site)
		}
		return card
	})
	keeping, others := lo.FilterReject(cards, func(card CardSite, _ int) bool { return card.Kept })
	return slices.Concat(keeping, others)
}

// findKept returns the definition of a reference the game keeps, and why. It returns false if it keeps none.
func findKept(ws *workspace.Session, mods *index.Index, reference schema.Reference, path string) (index.Winner, bool) {
	if reference.Database == game.Localization {
		_, winner, ok := ws.LookupText(mods, reference.Name)
		return winner, ok
	}
	file, listed := mods.FindFile(path)
	if !listed {
		return mods.FindKept(ws.Schema, reference)
	}
	return mods.FindKeptLocal(ws.Schema, reference, file)
}

// findHit returns the span the cursor is on in the visited statement, and its reference if it names one. The span is
// empty if the cursor is on neither the key nor the value.
func findHit(tree *jomini.Tree, visitor *hoverVisitor) (jomini.Span, schema.Reference) {
	if visitor.statement == 0 {
		return jomini.Span{}, schema.Reference{}
	}
	context, offset, current := &visitor.context, visitor.offset, tree.Statements[visitor.statement]
	if definition := context.Definition; context.Defines && definition.Span.Contains(offset) {
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
		for _, link := range jomini.SplitChain(tree.Text(current.Key)) {
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

// listScopes returns the scopes script can name where a statement is written, the latest saved first.
func listScopes(context *schema.Context) []CardScope {
	holder := context.Parent
	if context.Defines || holder == nil {
		holder = context
	}
	scopes := []CardScope{
		{Link: game.ScopeLinks.This, Type: holder.Scope, About: "the scope this line runs in"},
		{Link: game.ScopeLinks.Root, Type: holder.Root, About: "the scope the definition starts in"},
		{Link: game.ScopeLinks.Previous, Type: lo.FirstOrEmpty(holder.Previous), About: "the scope before this one"},
	}
	latest := slices.Clone(context.Saved)
	slices.Reverse(latest)
	for _, saved := range lo.UniqBy(latest, func(saved schema.Saved) string { return saved.Name }) {
		link := game.SavedScopes + ":" + saved.Name
		scopes = append(scopes, CardScope{Link: link, Type: saved.Type, About: "saved earlier in the definition"})
	}
	return lo.Filter(scopes, func(scope CardScope, _ int) bool { return scope.Type != "" })
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

// findDefinedColor returns the CSS color that the definition the game keeps of a reference writes as its block, as a
// named color does, seen from the given file. It returns "" if the game keeps none or its block is no color.
func findDefinedColor(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) string {
	winner, ok := findKept(ws, mods, reference, place.Path)
	if !ok {
		return ""
	}
	tree, statement, err := ws.FindStatement(mods, winner.Site)
	if err != nil {
		return ""
	}
	if color, ok := game.ToColor(tree, statement); ok {
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
func describeValue(card *Card, ws *workspace.Session, tree *jomini.Tree, visitor *hoverVisitor) {
	switch visitor.context.ValueGroup {
	case schema.CategoryParameter:
		card.What = "parameter"
	case schema.CategoryConstant:
		card.What = "constant"
		describeNumber(card, tree, tree.Statements[visitor.statement].Value)
	case schema.CategoryCall:
		call, ok := schema.ParseCall(tree, tree.Statements[visitor.statement].Value)
		position := slices.IndexFunc(call.Steps, func(step jomini.Step) bool { return step.Span.Contains(visitor.offset) })
		if ok && position >= 0 {
			function, _ := ws.Schema.FindFunction(call, visitor.context.Datacontext, position)
			describeFunction(card, tree, call.Steps[position], function)
		}
	}
}

// findDate returns the date a statement writes as its value. It returns false unless the statement's field expects a
// date.
func findDate(tree *jomini.Tree, statement int32, context *schema.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != schema.ValueDate {
		return jomini.Date{}, false
	}
	return jomini.ParseDate(tree.Unquoted(tree.Statements[statement].Value))
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

// findKeyword returns the keyword of the language that the visited statement's key is. It returns an empty keyword if
// the key is none.
func findKeyword(tree *jomini.Tree, visitor *hoverVisitor) game.Keyword {
	shape := cmp.Or(visitor.context.Parent.Shape, game.ShapeUnknown)
	current := tree.Statements[visitor.statement]
	holder := tree.Statements[current.Parent]
	label := cmp.Or(tree.Unquoted(holder.Key), tree.Unquoted(holder.Tag))
	keyword, _ := game.LookupKeyword(shape, strings.ToLower(label), strings.ToLower(tree.Unquoted(current.Key)))
	return keyword
}

// describeCommand fills a card for an effect, a trigger or a link: what it does and the scope it takes or switches to.
func describeCommand(card *Card, tree *jomini.Tree, visitor *hoverVisitor) {
	card.What = declaredLabels[visitor.context.Declaration]
	command := visitor.context.Command
	if command == nil {
		return
	}
	card.Blurb = command.Blurb
	if tree.Statements[visitor.statement].IsBlock() {
		card.Leads = command.Target
	} else {
		card.Target = command.Target
	}
}

// describeField fills a card for a field of a definition: what it does, whether it is required and what it takes.
func describeField(card *Card, context *schema.Context) {
	field := context.Field
	card.What, card.Blurb, card.Need = "field", field.Blurb.Value, field.Required.Value
	if field.Value.IsStated() {
		card.Takes = field.Value.Value
	} else {
		values := slices.SortedFunc(maps.Keys(field.Vanilla.Values), func(first, second string) int {
			return cmp.Or(field.Vanilla.Values[second]-field.Vanilla.Values[first], strings.Compare(first, second))
		})
		card.Writes = values[:min(len(values), 8)]
	}
	card.Inferred = field.Value.Source == game.SourceVanilla || field.Shape.Source == game.SourceVanilla
}

// resolveWeight returns the number a branch of a random list is written with, as a percent of the numbers of all its
// siblings. It returns 0 if the numbers add up to nothing.
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
func describeBlock(card *Card, tree *jomini.Tree, visitor *hoverVisitor) {
	if tree.Statements[visitor.statement].IsBlock() && visitor.context.Shape != game.ShapeUnknown {
		card.Holds = cmp.Or(blockLabels[visitor.context.Shape], visitor.context.Shape)
	}
	if color, ok := game.ToColor(tree, visitor.statement); ok {
		card.Color = color.ToCSS()
	}
}
