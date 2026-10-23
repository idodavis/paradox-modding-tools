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

// A CardScope is the scope section of a hover card: this, root, the previous scopes and the saved ones.
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

// A Card is a hover card. It holds facts for the frontend to lay out, never a sentence that combines them.
type Card struct {
	Title    string          `json:"title"`
	What     string          `json:"what"`            // what it is: the database's singular, "effect", "trigger", "link", "keyword"
	Class    string          `json:"class,omitempty"` // an operator's class
	Blurb    string          `json:"blurb,omitempty"`
	Takes    schema.Expected `json:"takes"`           // what the key's value may be
	Holds    string          `json:"holds,omitempty"` // what its block holds: the shape's word
	Leads    string          `json:"leads,omitempty"` // the scope a link or command leads to
	Scope    *CardScope      `json:"scope,omitempty"`
	Date     *CardDate       `json:"date,omitempty"`
	Color    string          `json:"color,omitempty"` // a CSS color
	Image    *texture.Header `json:"image,omitempty"`
	Sites    []CardSite      `json:"sites,omitempty"`
	Inferred bool            `json:"inferred"` // a fact was learned from the game's files
	Range    Range           `json:"range"`    // what the card is about
}

// unknownShape is the shape of a block whose content no source describes.
const unknownShape = "unknown"

// declaredLabels holds what a card says a command is, for each way the dumps declare one.
var declaredLabels = map[string]string{
	game.DeclaredEffect: "effect", game.DeclaredTrigger: "trigger", game.DeclaredLink: "link",
}

// Hover returns the hover card for the key or value at a position in a file, or nil if the position is on
// nothing.
func (lsp *Service) Hover(path string, line, column int) (*Card, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	place := ws.Locate(path)
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	if place.Format == files.FormatLocalization {
		return describeLocalization(ws, tree, place, offset), nil
	}
	mods := ws.Mods.Load()
	visitor := &hoverVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	span, reference := findHit(tree, visitor)
	if span.IsEmpty() {
		return nil, nil
	}
	context := &visitor.context
	card := &Card{Title: tree.Unquoted(span), Range: ToRange(tree.Src, tree.Lines, span), Scope: describeScope(context)}
	switch {
	case reference.Database == game.Localization:
		describeKey(ws, mods, card, reference.Name)
		card.Sites = listSites(ws, mods, place, reference)
	case reference.Name != "":
		card.What, card.Leads = game.DescribeDefinition(reference.Database, false), findSaved(context, reference)
		card.Sites, card.Image = listSites(ws, mods, place, reference), describeImage(mods, reference)
	case tree.Statements[visitor.statement].Value.Contains(offset):
		describeValue(card, ws, tree, visitor)
		if date, ok := findDate(tree, visitor.statement, context); ok {
			mod, _ := lsp.Workspace.Config.LookupMod(ws.ID, place.Origin)
			card.Date = resolveDate(mod.Calendar, date)
		}
	case context.Category == schema.CategoryKeyword:
		keyword := findKeyword(tree, visitor)
		card.What, card.Class, card.Blurb = "keyword", keyword.Class, keyword.Blurb
	case context.Category == schema.CategoryCommand || context.Category == schema.CategoryLink:
		describeCommand(card, ws, context)
	case context.Category == schema.CategoryField:
		describeField(card, context)
	case context.Category == schema.CategoryParameter:
		card.What = "parameter"
	}
	describeBlock(card, tree, visitor)
	if card.What == "" && card.Date == nil && card.Color == "" {
		return nil, nil
	}
	return card, nil
}

// A hoverVisitor is the visitor behind Hover. It keeps the context of the innermost statement at the offset.
type hoverVisitor struct {
	offset    int
	statement int32 // the innermost statement at the offset; 0, the file, if there is none
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

// Leave does nothing: the card needs only the innermost statement's context.
func (visitor *hoverVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// describeLocalization returns the hover card for a position in a localization file. Where the card is for a key,
// it has the key's text and the places that define the key. The card is nil where the position is on nothing.
func describeLocalization(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) *Card {
	card, reference := describeMessage(ws, tree, place, offset)
	if card != nil && reference.Name != "" {
		mods := ws.Mods.Load()
		describeKey(ws, mods, card, reference.Name)
		card.Sites = listSites(ws, mods, place, reference)
	}
	return card
}

// describeMessage describes what a position in a localization file is on: a message's key, a key written between
// dollar signs in a text, or a step of a data call in a text. The card is nil for anything else. The name of the key
// is returned too where the card is for one.
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
			key, span = segment.Text, jomini.Span{Start: segment.Span.Start + 1, End: segment.Span.Start + 1 + len(segment.Text)}
		case loc.MarkupCall:
			return describeCall(ws, tree, segment, offset), schema.Reference{}
		default:
			return nil, schema.Reference{}
		}
	}
	card := &Card{Title: key, Range: ToRange(tree.Src, tree.Lines, span)}
	return card, schema.Reference{Database: game.Localization, Name: key, Span: span}
}

// describeKey fills a card for a localization key: what it is and the text the game shows for it, found in the given
// index of the mods. The text is empty where no message has the key.
func describeKey(ws *workspace.Session, mods *index.Index, card *Card, key string) {
	card.What = "localization key"
	card.Blurb, _, _ = ws.LookupText(mods, key)
}

// describeCall describes the step of a data call that a localization text writes at an offset, or returns nil if the
// offset is on no step.
func describeCall(ws *workspace.Session, tree *jomini.Tree, segment loc.Segment, offset int) *Card {
	call, ok := jomini.ParseCall(segment.Text, segment.Span.Start+1)
	position := slices.IndexFunc(call.Steps, func(step jomini.Step) bool { return step.Span.Contains(offset) })
	if !ok || position < 0 {
		return nil
	}
	card := &Card{}
	describeFunction(card, tree, call.Steps[position], findFunction(ws.Schema, call, "", position))
	return card
}

// findFunction returns the function that one step of a data call calls. The given type is the one the call starts on,
// and the first step falls back to the global type. It returns an empty function if the dumps declare none.
func findFunction(schema *schema.Schema, call jomini.Call, datacontext string, position int) schema.DataFunction {
	types := schema.ResolveCallTypes(call, datacontext)
	if position > 0 && position <= len(types) {
		datacontext = types[position-1]
	}
	function, known := schema.DataTypes[datacontext].Functions[call.Steps[position].Text]
	if !known && position == 0 {
		function = schema.DataTypes[game.GlobalType].Functions[call.Steps[position].Text]
	}
	return function
}

// describeFunction fills a card for one step of a data call: what the function returns and says.
func describeFunction(card *Card, tree *jomini.Tree, step jomini.Step, function schema.DataFunction) {
	card.What, card.Blurb, card.Leads = "data function", function.Blurb, function.Returns
	card.Title, card.Range = step.Text, ToRange(tree.Src, tree.Lines, step.Span)
}

// listSites returns every definition a reference names, with whether the game keeps each and why, the kept ones
// first. The given place is the file the reference is written in. It returns no site where nothing defines the name,
// and none of them is kept where the schema has no database for the name.
func listSites(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) []CardSite {
	sites := resolve(ws, mods, reference, place.Path)
	winner, kept := findKept(ws, mods, reference, place.Path)
	cards := lo.Map(sites, func(site index.Site, _ int) CardSite {
		card := CardSite{Place: ws.Locate(mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
		if card.Kept = kept && winner.IsKept(site); card.Kept {
			card.Reason = winner.Reason
		}
		return card
	})
	keeping, others := lo.FilterReject(cards, func(card CardSite, _ int) bool { return card.Kept })
	return slices.Concat(keeping, others)
}

// resolve returns the definitions a reference names, as the file at the given path sees them: the file's local
// definitions count if the index lists the file.
func resolve(ws *workspace.Session, mods *index.Index, reference schema.Reference, path string) []index.Site {
	if file, ok := mods.FindFile(path); ok {
		return mods.ResolveLocal(ws.Schema, reference, file)
	}
	return mods.Resolve(ws.Schema, reference)
}

// findKept returns the definition of a name that the game keeps, and why, as the file at the given path sees it. The
// message of a localization key that the game shows is the one the session's language has, else the base language. It
// returns false if the game keeps none.
func findKept(ws *workspace.Session, mods *index.Index, reference schema.Reference, path string) (index.Winner, bool) {
	if reference.Database != game.Localization {
		if file, ok := mods.FindFile(path); ok {
			return mods.FindKeptLocal(ws.Schema, reference, file)
		}
		return mods.FindKept(ws.Schema, reference)
	}
	_, site, ok := ws.LookupText(mods, reference.Name)
	if !ok {
		return index.Winner{}, false
	}
	reason := "order"
	if language := mods.Language(site); schema.IsLocReplacement(ws.Game, mods.File(site).Rel, language) {
		reason = "replace"
	}
	return index.Winner{Site: site, Reason: reason}, true
}

// findHit returns the span the offset is on in the visited statement: a name the statement defines or refers to, a
// link of a key's chain, a key or a value. The name is returned too where the span is one. The span is empty where
// the offset is on neither the key nor the value.
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

// describeScope returns the scope section of a card for a context, or nil where the walk knows no scope.
func describeScope(context *schema.Context) *CardScope {
	if context.Scope == "" && context.Root == "" {
		return nil
	}
	return &CardScope{
		This: context.Scope, Root: context.Root, Previous: slices.Clone(context.Previous), Saved: slices.Clone(context.Saved),
	}
}

// findSaved returns the type of the scope that a reference names, when the reference is to a scope the definition
// saved before the statement. It returns "" otherwise.
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

// describeImage returns the header of the image file that a written path names, or nil if the reference is not to a
// file or no loaded image file has the path.
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

// describeValue fills a card for a value that is a parameter, a constant or a data call.
func describeValue(card *Card, ws *workspace.Session, tree *jomini.Tree, visitor *hoverVisitor) {
	switch visitor.context.ValueGroup {
	case schema.CategoryParameter:
		card.What = "parameter"
	case schema.CategoryConstant:
		card.What = "constant"
	case schema.CategoryCall:
		call, ok := schema.ParseCall(tree, tree.Statements[visitor.statement].Value)
		position := slices.IndexFunc(call.Steps, func(step jomini.Step) bool { return step.Span.Contains(visitor.offset) })
		if ok && position >= 0 {
			function := findFunction(ws.Schema, call, visitor.context.Datacontext, position)
			describeFunction(card, tree, call.Steps[position], function)
		}
	}
}

// findDate returns the date that a statement writes as its value, where its field expects a date. It returns false
// for any other value, so that a number such as 10.12 is no date.
func findDate(tree *jomini.Tree, statement int32, context *schema.Context) (jomini.Date, bool) {
	if context.Field == nil || context.Field.Value.Value.Type != schema.ValueDate {
		return jomini.Date{}, false
	}
	return jomini.ParseDate(tree.Unquoted(tree.Statements[statement].Value))
}

// resolveDate reads a date in the calendar of a mod: the era whose year is the last one at or before the date's, and
// the month's name as the mod gives it, else as the usual calendar names it.
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

// findKeyword returns the row of the language's keywords that the visited statement's key is, as it stands in the
// block that holds it. It returns an empty row if the language has none for the key.
func findKeyword(tree *jomini.Tree, visitor *hoverVisitor) game.Keyword {
	shape := unknownShape
	if holder := visitor.context.Parent; holder != nil && holder.Shape != "" {
		shape = holder.Shape
	}
	current := tree.Statements[visitor.statement]
	holder := tree.Statements[current.Parent]
	label := cmp.Or(tree.Unquoted(holder.Key), tree.Unquoted(holder.Tag))
	keyword, _ := game.LookupKeyword(shape, strings.ToLower(label), strings.ToLower(tree.Unquoted(current.Key)))
	return keyword
}

// describeCommand fills a card for an effect, a trigger or a link: what it is, what it does and the scope it leads
// to. The command is the one the walk matched. A link written with a prefix is described by the prefix's command. A
// key that no command answers to is a link.
func describeCommand(card *Card, ws *workspace.Session, context *schema.Context) {
	command, declaration := context.Command, context.Declaration
	if command == nil {
		key := card.Title
		if prefix, _, ok := jomini.SplitPrefixed(key); ok {
			key = prefix
		}
		command, declaration = findCommand(ws.Schema, key), game.DeclaredLink
	}
	card.What = declaredLabels[declaration]
	if command != nil {
		card.Blurb, card.Leads = command.Blurb, command.Target
	}
}

// findCommand returns the command a key names, written in any case, or nil if the dumps declare none.
func findCommand(schema *schema.Schema, key string) *schema.Command {
	if command := schema.Commands[key]; command != nil {
		return command
	}
	return schema.Commands[strings.ToLower(key)]
}

// describeField fills a card for a field of a definition: what it is, what it does and what it takes. The card says
// what the field takes only where a source states it. It is marked inferred where the game's files are the source.
func describeField(card *Card, context *schema.Context) {
	field := context.Field
	card.What, card.Blurb = "field", field.Blurb.Value
	if field.Value.IsStated() {
		card.Takes = field.Value.Value
	}
	card.Inferred = field.Value.Source == game.SourceVanilla || field.Shape.Source == game.SourceVanilla
}

// describeBlock fills a card for a statement that holds a block: the shape of what the block holds and, for a color,
// the color. A block of unknown shape holds nothing the card can name.
func describeBlock(card *Card, tree *jomini.Tree, visitor *hoverVisitor) {
	if tree.Statements[visitor.statement].IsBlock() && visitor.context.Shape != unknownShape {
		card.Holds = visitor.context.Shape
	}
	if color, ok := game.ToColor(tree, visitor.statement); ok {
		card.Color = color.ToCSS()
	}
}
