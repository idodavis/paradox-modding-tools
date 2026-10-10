// hover.go is the card for what is under the cursor: what the scalar stands for, then each fact its
// written form adds; a data call's step and a loc file's key have cards of their own.

package lsp

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/texture"
	"paradox-modding-tools/services/internal/workspace"
)

// Inspect is the hover card: what the word is and what the workspace knows of it.
type Inspect struct {
	Word    string         `json:"word"`              // the name it names: an entry, command, key, loc key
	Key     string         `json:"key,omitempty"`     // its statement's key (the word, when it is the key)
	Head    string         `json:"head,omitempty"`    // the card's first line: what the word is
	Need    string         `json:"need,omitempty"`    // a key: optional, required, required when ...
	Name    string         `json:"name,omitempty"`    // an object's localized name, a loc key's text
	About   string         `json:"about,omitempty"`   // what the dumps or docs say of its database
	Prose   string         `json:"prose,omitempty"`   // what the dumps or docs say of the word
	Takes   []string       `json:"takes,omitempty"`   // a key or command: what a word there may be
	Writes  []string       `json:"writes,omitempty"`  // a key nothing declares: what vanilla writes there
	Scope   *schema.Scope  `json:"scope,omitempty"`   // where it runs: the scope, its root, the ones before, the saved scopes, and why; nil unknown
	Leads   string         `json:"leads,omitempty"`   // the scope it leads to; "" none
	Returns string         `json:"returns,omitempty"` // what a defined name's entries give: yes or no, a number
	Sites   []Location     `json:"sites,omitempty"`   // where it is defined, in load order
	Winner  int            `json:"winner"`            // the site the game keeps; -1 for none
	Adds    []int          `json:"adds,omitempty"`    // the sites that add to the kept one (INJECT)
	Stale   bool           `json:"stale,omitempty"`   // a fact shown comes from a stale source
	Date    string         `json:"date,omitempty"`    // a date written out, and in a mod's calendar
	Weight  int            `json:"weight,omitempty"`  // a numbered branch: its share of its siblings, in %
	Color   string         `json:"color,omitempty"`   // a color, as #rrggbb
	Image   *texture.Facts `json:"image,omitempty"`   // a file that is an image: its header
	Graphed bool           `json:"graphed,omitempty"` // the event graph draws it: it is or fires an event
	Within  []string       `json:"within,omitempty"`  // the entry the cursor is in, then its blocks' keys
	Range   Range          `json:"range"`
}

// subject is what the scalar under the cursor stands for: a reference the walk read in it, else its
// key's position or command; with where the scalar is written and what the walker reads the key's
// block as.
type subject struct {
	ref     *schema.Occurrence
	key     *schema.Position
	command *schema.Command
	scope   schema.Scope
	holds   game.Shape
}

// Hover is the card for the scalar at a position of a script or localization file: what it stands
// for and each fact its written form adds; nil where the position holds nothing to say.
func Hover(ws *workspace.Session, path string, line, col int) (*Inspect, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	offset := file.parsed.Lines().OffsetAt(line, col)
	switch {
	case file.place.Format == inventory.Loc:
		return file.locCard(offset), nil
	case file.place.Format != inventory.Script || file.place.Rel == "":
		return nil, nil
	}
	return file.scriptCard(offset), nil
}

// scriptCard is the card of a script file's offset: what the scalar stands for, then the facts its
// written form adds, each a call; it is nil where the card would say nothing and the cursor is in
// no entry.
func (file *openFile) scriptCard(offset int) *Inspect {
	s := file.ws.Install.Schema
	cursor := file.walkTo(offset)
	within := cursor.within()
	scalar, isKey, ref := cursor.scalarAt()
	if scalar == nil {
		return bareCard(within, file.span(jomini.Range{Start: offset, End: offset}))
	}
	start := scalar.Range.Start
	if scalar.Quoted {
		start++
	}
	if steps := dataCallAt(scalar.Text, start, offset); steps != nil {
		card := file.dataCallCard(steps)
		if card == nil {
			return bareCard(within, file.span(scalar.Range))
		}
		card.Within = within
		return card
	}
	card := &Inspect{Word: scalar.Text, Range: file.span(scalar.Range), Winner: -1, Within: within}
	assign, isAssign := cursor.chain[len(cursor.chain)-1].statement.(*jomini.Assignment)
	if isAssign {
		card.Key = assign.Key.Text
	}
	subject := cursor.subjectAt(scalar, isKey, ref)
	known := file.describe(card, subject)
	if modifier, isModifier := modifierFact(s, scalar.Text); isKey && !known && isModifier {
		card.Head, card.Prose = "modifier", modifier.Blurb
		card.Stale = card.Stale || s.Sources[schema.SourceDumps].Stale
	}
	if date, isDate := dateFact(scalar.Text, file.place.Calendar); isDate {
		card.Date, card.Head = date, cmp.Or(card.Head, "date")
	}
	if value, at, isConstant := constantFact(file.parsed, scalar.Text); isConstant {
		card.Head, card.Name, card.Winner = "script constant", value, 0
		card.Sites = []Location{locate(file.ws, file.path, file.parsed.Lines().PositionAt(at))}
	}
	if isKey && isAssign {
		card.Color = colorFact(assign)
		if len(cursor.chain) > 1 {
			holder := cursor.chain[len(cursor.chain)-2]
			card.Weight = weightFact(assign, jomini.ChildBlock(holder.statement), holder.inner)
		}
	}
	if saysNothing(card) {
		return bareCard(within, card.Range)
	}
	if isKey && subject.command == nil && card.Head == "" {
		cursor.writtenInFact(card)
	}
	return card
}

// subjectAt is what the scalar stands for, from the innermost statement: the reference the walk
// read in it, the scope it is written in and, for a key, its position, its command and what its
// block holds. A link chain (capital_county.holder) is read part by part: the part under the cursor
// is the command, led to from the scope the parts before it end in.
func (cursor cursor) subjectAt(scalar *jomini.Scalar, isKey bool, ref *schema.Occurrence) subject {
	s := cursor.file.ws.Install.Schema
	at := cursor.chain[len(cursor.chain)-1]
	subject := subject{ref: ref, scope: at.context.Scope}
	if !isKey {
		return subject
	}
	subject.key, subject.command = at.inner.Position(), at.command(s)
	if jomini.ChildBlock(at.statement) != nil || at.inner.Shape == game.ShapeValue {
		subject.holds = at.inner.Shape
	}
	if ref != nil || subject.command == nil || subject.command.Kinds&game.KindLink == 0 {
		return subject
	}
	start := scalar.Range.Start
	parts := strings.Split(scalar.Text, ".")
	for i, part := range parts {
		end := start + len(part)
		if covers(jomini.Range{Start: start, End: end}, cursor.offset) {
			subject.command = s.Link(strings.ToLower(part))
			if i > 0 {
				subject.scope = s.Follow(at.context.Scope, strings.Join(parts[:i], "."))
			}
		}
		start = end + 1
	}
	return subject
}

// describe fills a card from what a scalar stands for: a reference's definitions, else a command's
// or a key's claims; a stale source among those shown marks the card. It reports whether anything
// is known.
func (file *openFile) describe(card *Inspect, subject subject) bool {
	s := file.ws.Install.Schema
	mark := func(from schema.Source) {
		card.Stale = card.Stale || s.Sources[from].Stale
	}
	switch {
	case subject.ref != nil && file.cardForReference(card, subject, mark):
		return true
	case subject.command != nil:
		return file.cardForCommand(card, subject, mark)
	case subject.key != nil:
		return cardForPosition(card, subject, mark)
	}
	return false
}

// scopeCard is the record a card carries: nil where neither the scope nor its root is known.
func scopeCard(scope schema.Scope) *schema.Scope {
	if scope.This == "" && scope.Root == "" {
		return nil
	}
	return &scope
}

// cardForReference fills a reference's card: its database, its sites (the kept one and its adds),
// its localized name, the comment its author leads it with, and what the dumps and docs say of its
// database. subject.scope is where the reference is written, which a scripted entry runs in when
// its database declares none.
func (file *openFile) cardForReference(card *Inspect, subject subject, mark func(schema.Source)) bool {
	ws, s, ref := file.ws, file.ws.Install.Schema, subject.ref
	defs, known := index.Resolve(s, ws.Indexes(), *ref, file.place.Path)
	database := ref.Database
	if len(defs) > 0 {
		database = defs[0].Occurrence().Database
	}
	row := s.Databases[database]
	inSchema := row != nil || database == schema.Localization || database == schema.File
	if unknown := !known && (ref.Caller != "" || !inSchema); unknown {
		return false
	}
	card.Word, card.Head = ref.ID, game.KindName(database)
	if entry, name, _ := strings.Cut(ref.ID, ":"); database == schema.Parameter {
		card.Word, card.Head = name, "parameter of "+entry
	}
	if database == schema.Localization || database == schema.File {
		file.placeCard(card, database, ref.ID)
		return true
	}
	defs = file.inFile(defs)
	for _, def := range defs {
		card.Sites = append(card.Sites, siteOf(ws, def))
	}
	winner, adds, _, _ := ws.Winner(defs)
	if len(defs) > 0 && row != nil {
		card.Winner = slices.Index(defs, winner)
		for _, add := range adds {
			card.Adds = append(card.Adds, slices.Index(defs, add))
		}
	}
	if row != nil {
		card.Name, card.Prose = loc.Plain(ws.Title(ref.ID, winner)), authorComment(ws, winner)
		card.About = row.Entry.Blurb.Value
		if source := row.Entry.Blurb.Source; source == schema.SourceDumps || source == schema.SourceDocs {
			mark(source)
		}
		scope := s.EntryScope(row, ref.ID)
		if scope.This == "" && len(row.Descriptor.Callers) > 0 {
			scope = subject.scope
		}
		card.Scope = scopeCard(scope)
		card.Returns = returnsText[row.Entry.Holds.Value]
		mark(row.Entry.Scope.Source)
		if action, isAction := s.OnActions[ref.ID]; isAction && row.Descriptor.Purpose == game.PurposeOnActions {
			card.Prose = cmp.Or(action.Blurb, card.Prose)
			mark(schema.SourceDumps)
		}
		card.Graphed = row.Descriptor.Purpose == game.PurposeEvents || row.Descriptor.Purpose == game.PurposeOnActions
	}
	card.Graphed = card.Graphed || (len(defs) > 0 && len(ws.Fires(winner)) > 0)
	return true
}

// placeCard fills the card of a localization key (its text and entry) or of a file path (its place,
// and for an image its header).
func (file *openFile) placeCard(card *Inspect, database, id string) {
	ws := file.ws
	if database == schema.Localization {
		if entry, known := ws.Loc(id, ""); known {
			card.Name, card.Winner = loc.Plain(entry.Text()), 0
			card.Sites = []Location{locate(ws, entry.File().Path, Position{Line: int(entry.Occurrence().Line)})}
		}
		return
	}
	fileAt, known := index.FileAt(ws.Indexes(), id)
	if !known {
		return
	}
	card.Winner, card.Sites = 0, []Location{locate(ws, fileAt.Path, Position{})}
	if fileAt.Format == inventory.Image {
		if facts, err := texture.Info(fileAt.Path); err == nil {
			card.Image = &facts
		}
	}
}

// authorComment is the comment a mod's author leads a definition with: the comments attached to its
// statement that end before it starts, each stripped of its #, joined by newlines; "" for the
// game's own, a file under no origin or none.
func authorComment(ws *workspace.Session, def index.At) string {
	if origin := def.File().Origin; origin == game.OriginVanilla || origin == "" {
		return ""
	}
	parsed, statement := ws.Entry(def)
	if statement == nil {
		return ""
	}
	attached, _ := parsed.Attach()
	var lines []string
	for _, comment := range attached[statement] {
		if comment.Range.End <= statement.NodeRange().Start {
			lines = append(lines, strings.TrimSpace(strings.TrimLeft(parsed.Src[comment.Range.Start:comment.Range.End], "#")))
		}
	}
	return strings.Join(lines, "\n")
}

// cardForCommand fills a declared command's card: what it is, what it takes, the scope it runs in
// (the one it declares where the block's is unknown) and the scope it leads to. It reports whether
// the card says anything.
func (file *openFile) cardForCommand(card *Inspect, subject subject, mark func(schema.Source)) bool {
	s, command := file.ws.Install.Schema, subject.command
	card.Head, card.Prose, card.Takes = kindText(command), command.Blurb, takesText(command.Args)
	scope := subject.scope
	if scope.This == "" && len(command.Scopes) == 1 {
		scope.This, scope.Reason = command.Scopes[0], "the one scope it runs in"
	}
	card.Scope = scopeCard(scope)
	if _, declared := s.Scopes[command.Target]; declared {
		card.Leads = command.Target
	}
	mark(schema.SourceDumps)
	return takesByEvidence(card, command.Args, subject.holds)
}

// cardForPosition fills a key's card from its position's claims: whether it is required, what it
// takes, where it runs and leads, and the prose a declared source gives it. It reports whether the
// card says anything.
func cardForPosition(card *Inspect, subject subject, mark func(schema.Source)) bool {
	key := subject.key
	card.Need, card.Takes, card.Scope, card.Leads = requirementText(key), takesText(key), scopeCard(subject.scope), key.Scope.Value
	if source := key.Blurb.Source; source == schema.SourceDumps || source == schema.SourceDocs {
		card.Prose = key.Blurb.Value
		mark(source)
	}
	for _, source := range []schema.Source{key.Value.Source, key.Holds.Source, key.Scope.Source, key.Required.Source, key.Default.Source} {
		mark(source)
	}
	return takesByEvidence(card, key, subject.holds)
}

// takesByEvidence says what a key takes where no source declared it: what its block holds, else
// what vanilla writes there, an inference that alone does not make the card known. It reports
// whether the card now says something.
func takesByEvidence(card *Inspect, position *schema.Position, holds game.Shape) bool {
	if len(card.Takes) == 0 && blockHoldsText[holds] != "" {
		card.Takes = []string{blockHoldsText[holds]}
	}
	if len(card.Takes) == 0 {
		card.Writes = vanillaWritesText(position)
	}
	return card.Head != "" || card.Need != "" || card.Prose != "" || len(card.Takes) > 0 || card.Leads != ""
}

// modifierFact is the modifier the dumps list under a name, else the most specific $TYPE$ family
// the name matches: the longest pattern, the first by name among equals.
func modifierFact(s *schema.Schema, name string) (schema.Modifier, bool) {
	modifier, declared := s.Modifiers[name]
	for _, family := range s.Modifiers {
		matched, _ := filepath.Match(family.Pattern, name)
		longer := len(family.Pattern) > len(modifier.Pattern) ||
			len(family.Pattern) == len(modifier.Pattern) && family.Pattern < modifier.Pattern
		if !declared && family.Pattern != "" && matched && longer {
			modifier = family
		}
	}
	return modifier, declared || modifier.Pattern != ""
}

// dateFact writes a date out (January 1st, 8082 AD; year -221 is 221 BC), and in the mod's calendar
// where one of its eras applies (82.1.1 A.C.). isDate says the text is a date at all; a date whose
// month is out of range is one with no text.
func dateFact(text string, calendar workspace.Calendar) (date string, isDate bool) {
	if jomini.Literal(text) != jomini.LitDate {
		return "", false
	}
	year, month, day, _, ok := jomini.ParseDate(text)
	if !ok || month < 1 || month > 12 {
		return "", true
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	date = fmt.Sprintf("%s %s, %d AD", months[month], ordinal(day), year)
	if year < 0 {
		date = fmt.Sprintf("%s %s, %d BC", months[month], ordinal(day), -year)
	}
	if inEra, applies := calendar.Date(year, month, day); applies {
		date += "; " + inEra + " in this mod's calendar"
	}
	return date, true
}

// ordinal writes 1st, 2nd, 3rd, 4th … 11th, 12th, 13th … 21st.
func ordinal(number int) string {
	suffix := "th"
	if number%100 < 11 || number%100 > 13 {
		switch number % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(number) + suffix
}

// constantFact is a script constant's value in this file: the value of the last top statement that
// writes the name with a scalar, and where that statement's key starts; isConstant is false for a
// name that is no constant (@x) or that no top statement writes so.
func constantFact(parsed *jomini.Result, name string) (value string, at int, isConstant bool) {
	if _, constant := jomini.Constant(name); !constant {
		return "", 0, false
	}
	for _, statement := range parsed.Root.Statements {
		top, isTop := statement.(*jomini.Assignment)
		if !isTop || top.Key.Text != name {
			continue
		}
		if scalar, isScalar := top.Value.(*jomini.Scalar); isScalar {
			value, at, isConstant = scalar.Text, top.Key.Range.Start, true
		}
	}
	return value, at, isConstant
}

// weightFact is a numbered branch's share of its siblings' numbers, in percent, where the holder's
// construct row gives numbered keys a weight (random_list's <number> branches); 0 elsewhere.
func weightFact(branch *jomini.Assignment, holder *jomini.Block, context schema.Context) int {
	construct, isConstruct := game.ConstructOf(context.Shape, context.Command, "<number>")
	weighted := isConstruct && construct.Key == "<number>"
	if holder == nil || !weighted || jomini.Literal(branch.Key.Text) != jomini.LitNumber {
		return 0
	}
	sum, mine := 0.0, 0.0
	for _, sibling := range holder.Statements {
		other, isBranch := sibling.(*jomini.Assignment)
		if isBranch && jomini.Literal(other.Key.Text) == jomini.LitNumber {
			share, _ := strconv.ParseFloat(other.Key.Text, 64)
			sum += share
			if other == branch {
				mine = share
			}
		}
	}
	if sum == 0 {
		return 0
	}
	return int(mine*100/sum + 0.5)
}

// colorFact is a color written as a tagged block (rgb { 255 0 0 }) spelled as #rrggbb; "" for any
// other value, an unknown tag or too few parts.
func colorFact(assign *jomini.Assignment) string {
	tagged, isTagged := assign.Value.(*jomini.TaggedBlock)
	if !isTagged {
		return ""
	}
	var words []string
	var parts []float64
	for _, statement := range tagged.Block.Statements {
		value, isValue := statement.(*jomini.ValueStmt)
		if !isValue {
			continue
		}
		if scalar, isScalar := value.Value.(*jomini.Scalar); isScalar {
			words = append(words, scalar.Text)
			number, _ := strconv.ParseFloat(scalar.Text, 64)
			parts = append(parts, number)
		}
	}
	i := slices.IndexFunc(game.Colors, func(color game.Color) bool { return strings.EqualFold(color.Tag, tagged.Tag.Text) })
	if i < 0 {
		return ""
	}
	row := game.Colors[i]
	if row.Hex && len(words) == 1 {
		if digits := strings.TrimPrefix(strings.ToLower(words[0]), "0x"); len(digits) >= 6 {
			return "#" + digits[:6]
		}
		return ""
	}
	switch {
	case len(parts) < 3:
		return ""
	case row.HSV:
		return hex(hsv(parts[0]/cmp.Or(row.Hue, 1), parts[1]/cmp.Or(row.Parts, 1), parts[2]/cmp.Or(row.Parts, 1)))
	}
	return hex(parts[0], parts[1], parts[2])
}

// hex spells a color whose parts are 0 to 255, or 0 to 1 where every part is at most 1, as #rrggbb.
func hex(red, green, blue float64) string {
	scale := 1.0
	if max(red, green, blue) <= 1 {
		scale = 255
	}
	return fmt.Sprintf("#%02x%02x%02x", int(red*scale+0.5), int(green*scale+0.5), int(blue*scale+0.5))
}

// hsv turns a hue, a saturation and a value, each 0 to 1, into red, green and blue, each 0 to 1.
func hsv(hue, saturation, value float64) (red, green, blue float64) {
	sector, fraction := int(hue*6)%6, hue*6-float64(int(hue*6))
	bottom, falling, rising := value*(1-saturation), value*(1-fraction*saturation), value*(1-(1-fraction)*saturation)
	switch sector {
	case 0:
		return value, rising, bottom
	case 1:
		return falling, value, bottom
	case 2:
		return bottom, value, rising
	case 3:
		return bottom, falling, value
	case 4:
		return rising, bottom, value
	}
	return value, bottom, falling
}

// writtenInFact says where a key is written when nothing else names it: in its entry's database
// where the key sits in the entry's own block, else in the key of the block that holds it.
func (cursor cursor) writtenInFact(card *Inspect) {
	if len(cursor.chain) < 2 {
		return
	}
	holder := cursor.chain[len(cursor.chain)-2]
	var database *schema.Database
	if holder.context.Entry >= 0 {
		database = cursor.file.ws.Install.Schema.Databases[cursor.occurrences[holder.context.Entry].Database]
	}
	inEntry := database != nil && holder.inner.At == database.Entry
	assign, isAssign := holder.statement.(*jomini.Assignment)
	switch {
	case inEntry:
		card.Head = "in " + heading(database.Name)
	case isAssign:
		card.Head = "in `" + assign.Key.Text + "`"
	}
}

// bareCard is a card with only the entry and blocks the cursor is in, which draws nothing; nil
// outside an entry.
func bareCard(within []string, span Range) *Inspect {
	if len(within) == 0 {
		return nil
	}
	return &Inspect{Within: within, Range: span, Winner: -1}
}

// saysNothing reports whether a card holds no fact a reader would see: no head, need, prose, date,
// color, weight, scope led to, takes, writes or sites.
func saysNothing(card *Inspect) bool {
	texts := card.Head != "" || card.Need != "" || card.Prose != "" || card.Date != "" || card.Color != "" || card.Leads != ""
	lists := len(card.Takes) > 0 || len(card.Writes) > 0 || len(card.Sites) > 0
	return !texts && !lists && card.Weight == 0
}

// dataCallCard is the card of a data call's last step: a custom localization named by a quoted
// argument, an entry of the functions or concepts databases, a function of the type the steps
// before lead to, or a data type; nil where the step is none of them.
func (file *openFile) dataCallCard(steps []jomini.Step) *Inspect {
	ws, s := file.ws, file.ws.Install.Schema
	step := steps[len(steps)-1]
	card := &Inspect{Word: step.Name, Range: file.span(step.Range), Winner: -1}
	typeName := stepsLeadTo(s.DataTypes, steps[:len(steps)-1])
	stale := s.Sources[schema.SourceDumps].Stale
	for _, arg := range step.Args {
		if custom, isCustom := s.CustomLoc[arg.Text]; arg.Quoted && isCustom {
			card.Word, card.Head, card.Stale = arg.Text, "custom localization", stale
			card.Scope = scopeCard(schema.Scope{This: custom.Scope, Reason: "declared for " + arg.Text})
			card.Takes = []string{oneOf(custom.Entries)}
			if custom.Parent != "" {
				card.Takes = append(card.Takes, "parent "+custom.Parent, "suffix "+custom.Suffix)
			}
			return card
		}
	}
	known := false
	for _, name := range s.DatabasesWhere(readInDataCalls) {
		if len(index.Lookup(ws.Indexes(), schema.Defines, name, step.Name)) == 0 {
			continue
		}
		reference := subject{ref: &schema.Occurrence{Database: name, ID: step.Name}}
		if known = file.cardForReference(card, reference, func(schema.Source) {}); known {
			break
		}
	}
	if function, isFunction := s.DataTypes[typeName].Functions[step.Name]; isFunction {
		card.Head, card.Leads = cmp.Or(card.Head, "data function"), function.Returns
		card.Scope = scopeCard(schema.Scope{This: typeName, Reason: "what " + step.Name + " is called on"})
		card.Stale = card.Stale || stale
	}
	if _, isType := s.DataTypes[step.Name]; len(steps) == 1 && isType && !known {
		card.Head, card.Stale = "data type", card.Stale || stale
	}
	if card.Head == "" {
		return nil
	}
	return card
}

// readInDataCalls reports whether a database holds the entries a data call names: functions and
// concepts.
func readInDataCalls(row game.Descriptor) bool {
	return row.Purpose == game.PurposeFunctions || row.Purpose == game.PurposeConcepts
}

// locCard is the card in a localization file: a data call's step, a reference the line makes
// ($key$, a concept) or the line's own key; nil where the offset names nothing that exists.
func (file *openFile) locCard(offset int) *Inspect {
	ws := file.ws
	ref, steps := file.locRef(offset)
	if steps != nil {
		return file.dataCallCard(steps)
	}
	if ref == nil {
		return nil
	}
	if _, known := index.Resolve(ws.Install.Schema, ws.Indexes(), *ref, file.place.Path); !known {
		return nil
	}
	card := &Inspect{Word: ref.ID, Range: file.span(ref.Range), Winner: -1}
	file.cardForReference(card, subject{ref: ref}, func(schema.Source) {})
	return card
}
