package lsp

import (
	"cmp"
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

// A CardScope is the scope section of a hover card.
type CardScope struct {
	This     string         `json:"this"`
	Root     string         `json:"root"`
	Previous []string       `json:"previous"`
	Saved    []schema.Saved `json:"saved"`
}

// A CardSite is one place a name is defined, with whether the game keeps that definition and why.
type CardSite struct {
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"` // counted from one
	Kept   bool            `json:"kept"`
	Reason string          `json:"reason,omitempty"` // order, mode, priority, merge, replace
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
	Takes    schema.Expected `json:"takes"`           // what the key's value may be
	Holds    string          `json:"holds,omitempty"` // what its block holds: the shape's word
	Leads    string          `json:"leads,omitempty"` // the scope a link or command leads to
	Scope    *CardScope      `json:"scope,omitempty"`
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
	reference = resolveCall(mods, place, reference)
	card := &Card{Title: tree.Unquoted(span), Range: ToRange(tree.Src, tree.Lines, span), Scope: describeScope(context)}
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, mods, card, reference.Name)
	case reference.Name != "":
		card.What, card.Leads = game.DescribeDefinition(reference.Database, false), findSaved(context, reference)
		card.Sites, card.Image = listSites(ws, mods, place, reference), describeImage(mods, reference)
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
	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, context)
	case context.Category == schema.CategoryField:
		describeField(card, context)
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
}

// Enter keeps the context when the statement covers the offset.
func (visitor *hoverVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if tree.Statements[statement].Span.Contains(visitor.offset) {
		visitor.statement, visitor.context = statement, *context
		visitor.context.References = slices.Clone(context.References)
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
		describeKey(ws, ws.Mods.Load(), card, reference.Name)
	}
	return card
}

// describeMessage returns the card for what a position in a localization file is on: a message's key, a key between
// dollar signs in a text, or a step of a data call. It also returns the key's reference if the card is for a key. The
// card is nil for anything else.
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

// describeKey fills a card for a localization key: the text the game shows for it and the messages that define it. The
// text is empty and no message is kept if nothing defines the key.
func describeKey(ws *workspace.Session, mods *index.Index, card *Card, key string) {
	text, winner, ok := ws.LookupText(mods, key)
	card.What, card.Blurb = "localization key", text
	var kept *index.Winner
	if ok {
		kept = &winner
	}
	card.Sites = describeSites(ws, mods, mods.ListMessages(key), kept)
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
	card.What, card.Blurb, card.Leads = "data function", function.Blurb, function.Returns
	card.Title, card.Range = step.Text, ToRange(tree.Src, tree.Lines, step.Span)
}

// listSites returns every definition a reference names as seen from the given file, with whether the game keeps each
// and why. The kept ones come first. It returns none if nothing defines the name.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) []CardSite {
	var kept *index.Winner
	if winner, ok := findKept(ws, mods, reference, place.Path); ok {
		kept = &winner
	}
	return describeSites(ws, mods, mods.ResolveLocal(reference, place.Path), kept)
}

// describeSites returns the given definitions as the sites of a card, with whether the given winner keeps each and why.
// The kept ones come first. None is kept if the winner is nil.
func describeSites(ws *workspace.Session, mods *index.Index, sites []index.Site, winner *index.Winner) []CardSite {
	cards := lo.Map(sites, func(site index.Site, _ int) CardSite {
		card := CardSite{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
		if card.Kept = winner != nil && winner.IsKept(site); card.Kept {
			card.Reason = winner.Reason
		}
		return card
	})
	keeping, others := lo.FilterReject(cards, func(card CardSite, _ int) bool { return card.Kept })
	return slices.Concat(keeping, others)
}

// findKept returns the definition of a reference that the game keeps, and why, as seen from the given file. It returns
// false if the game keeps none.
func findKept(ws *workspace.Session, mods *index.Index, reference schema.Reference, path string) (index.Winner, bool) {
	if reference.Database == game.Localization {
		_, winner, ok := ws.LookupText(mods, reference.Name)
		return winner, ok
	}
	database, ok := ws.Schema.Databases[reference.Database]
	if !ok {
		return index.Winner{}, false
	}
	return mods.FindWinner(mods.ResolveLocal(reference, path), database.Precedence)
}

// findHit returns the span the cursor is on in the visited statement: a name it defines or refers to, a link of a key's
// chain, a key or a value. It also returns the name's reference if the span is one. The span is empty if the cursor is
// on neither the key nor the value.
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

// describeScope returns the scope section of a card for a context. It returns nil if the context has no scope.
func describeScope(context *schema.Context) *CardScope {
	if context.Scope == "" && context.Root == "" {
		return nil
	}
	return &CardScope{
		This: context.Scope, Root: context.Root, Previous: slices.Clone(context.Previous), Saved: slices.Clone(context.Saved),
	}
}

// findSaved returns the type of the scope a reference names, if the definition saved that scope earlier. It returns ""
// otherwise.
func findSaved(context *schema.Context, reference schema.Reference) string {
	if reference.Database != game.SavedScopes {
		return ""
	}
	for _, saved := range slices.Backward(context.Saved) {
		if saved.Name == reference.Name {
			return saved.Type
		}
	}
	return ""
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

// describeCommand fills a card for an effect, a trigger or a link: what it is, what it does and the scope it leads to.
// The card has no blurb and leads nowhere if the game declares no command for it.
func describeCommand(card *Card, context *schema.Context) {
	card.What = declaredLabels[context.Declaration]
	if command := context.Command; command != nil {
		card.Blurb, card.Leads = command.Blurb, command.Target
	}
}

// describeField fills a card for a field of a definition: what it does and what it takes. The card is marked inferred
// if the game's files are the only source.
func describeField(card *Card, context *schema.Context) {
	field := context.Field
	card.What, card.Blurb = "field", field.Blurb.Value
	if field.Value.IsStated() {
		card.Takes = field.Value.Value
	}
	card.Inferred = field.Value.Source == game.SourceVanilla || field.Shape.Source == game.SourceVanilla
}

// describeBlock fills a card for a statement that holds a block: what the block holds and, for a color, the color.
func describeBlock(card *Card, tree *jomini.Tree, visitor *hoverVisitor) {
	if tree.Statements[visitor.statement].IsBlock() && visitor.context.Shape != game.ShapeUnknown {
		card.Holds = visitor.context.Shape
	}
	if color, ok := game.ToColor(tree, visitor.statement); ok {
		card.Color = color.ToCSS()
	}
}
