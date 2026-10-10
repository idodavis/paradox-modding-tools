// hover.go is the card for the word under the cursor, read from one walk that stops past it: what
// the word is, the scope it runs in and leads to, an object's localized name, and the blurbs the
// dumps and docs give, marked when their source may be outdated. Signature help is a command's
// arguments, read from the same walk. It also holds the open file every request reads.

// Package lsp answers the editor's requests for one open file: the word under the cursor, where it
// is defined and used, what may be written there, what is wrong, and how the file is folded,
// formatted and annotated. Every answer starts from one walk of the file over the schema
// (index.Collect), stopped at the cursor or run whole; payloads carry display data and the frontend
// templates the text.
package lsp

import (
	"cmp"
	"fmt"
	"maps"
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

// file is an open file's parse and place, held for one request.
type file struct {
	sess   *workspace.Session
	path   string
	text   string
	parsed *jomini.Result
	place  workspace.Place
}

// open parses a file (once per text for an open buffer) and places it.
func open(sess *workspace.Session, path string) (*file, error) {
	parsed, text, err := sess.Parsed(path)
	if err != nil {
		return nil, err
	}
	return &file{sess: sess, path: path, text: text, parsed: parsed, place: sess.Where(path)}, nil
}

// span is the editor's range of a span of the file.
func (file *file) span(span jomini.Range) Range {
	return Range{Start: file.parsed.Lines().PositionAt(span.Start), End: file.parsed.Lines().PositionAt(span.End)}
}

// Position and Range are the editor's: 0-based line and byte column.
type Position = jomini.Position

// Range is a span of the file.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a place in a file of the workspace or the install.
type Location struct {
	Path   string `json:"path"`
	Origin string `json:"origin"` // game.OriginVanilla or a mod's id
	Rel    string `json:"rel"`
	Range  Range  `json:"range"`
}

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
	Scope   string         `json:"scope,omitempty"`   // the scope it runs in; "" unknown
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

// Signature is a command's arguments: the keys its block holds, and its blurb.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Docs       string   `json:"docs,omitempty"`
}

// reading is what a word names: a reference, else its key's position or command, in the scope the
// word is written in.
type reading struct {
	ref     *schema.Occurrence
	key     *schema.Position
	command *schema.Command
	scope   string
	holds   game.Shape // what the walker reads the key's block as
}

// step is one statement of the chain: the statement, the context it is written in and the one its block
// reads in.
type step struct {
	statement jomini.Statement
	context   schema.Context
	inner     schema.Context
}

// command is the declared effect, trigger or link the step's key is, by the rule that read it; nil for none.
func (at step) command(s *schema.Schema) *schema.Command {
	assign, isAssign := at.statement.(*jomini.Assignment)
	if !isAssign {
		return nil
	}
	switch at.inner.Rule {
	case game.KeyRuleCommand:
		return s.Commands[strings.ToLower(assign.Key.Text)]
	case game.KeyRuleLink:
		return s.Link(strings.ToLower(assign.Key.Text))
	}
	return nil
}

// spot is the cursor's place in a script file, from one walk that stops past it: the statements
// holding it, outer to inner, and the occurrences so far.
type spot struct {
	chain  []step
	found  []schema.Occurrence
	path   string
	offset int
}

// walk reads the file up to an offset. A statement holds the offset when its span does, or when it
// ends at an operator with only blanks up to the offset: a value still to come.
func (file *file) walk(offset int) spot {
	cursor := spot{offset: offset, path: file.place.Path}
	cursor.found, _ = index.Collect(file.sess.Install.Schema, file.place.File, file.parsed, func(statement jomini.Statement, context schema.Context, inner *schema.Context, _ []schema.Occurrence) bool {
		span := statement.NodeRange()
		if span.Start > offset {
			return false
		}
		assign, isAssign := statement.(*jomini.Assignment)
		if offset <= span.End || isAssign && assign.Value == nil && strings.TrimSpace(file.text[span.End:offset]) == "" {
			cursor.chain = append(cursor.chain, step{statement, context, *inner})
		}
		return true
	})
	return cursor
}

// block is the innermost statement whose block holds the cursor, -1 at the file's top; its inner
// context says how that block reads.
func (cursor spot) block() int {
	holder := -1
	for i, at := range cursor.chain {
		block := jomini.ChildBlock(at.statement)
		if block != nil && cursor.offset > block.OpenBrace && (block.CloseBrace < 0 || cursor.offset <= block.CloseBrace) {
			holder = i
		}
	}
	return holder
}

// within is the entry the cursor is in, then the keys of the blocks between it and the cursor; nil
// outside an entry. The entry's own statement is its name, not a key.
func (cursor spot) within() []string {
	holder := cursor.block()
	if holder < 0 || cursor.chain[holder].context.Entry < 0 {
		return nil
	}
	entry := cursor.chain[holder].context.Entry
	def := cursor.found[entry]
	out := []string{def.ID}
	for _, at := range cursor.chain[:holder+1] {
		if assign, isAssign := at.statement.(*jomini.Assignment); isAssign && at.context.Entry == entry && assign.Key.Range != def.Range {
			out = append(out, assign.Key.Text)
		}
	}
	if def.ID == "" {
		out = out[1:]
	}
	return out
}

// word is the scalar under the cursor (the innermost statement's key or value; nil for none), if it
// is the key, and the definition or reference the walk read inside it ($NAME$ included): of several
// references (a call's parameter and the call), the first that resolves, else the first.
func (cursor spot) word(sess *workspace.Session) (word *jomini.Scalar, isKey bool, ref *schema.Occurrence) {
	if len(cursor.chain) == 0 {
		return nil, false, nil
	}
	at := cursor.chain[len(cursor.chain)-1]
	word, _ = jomini.ValueOf(at.statement).(*jomini.Scalar)
	if assign, isAssign := at.statement.(*jomini.Assignment); isAssign && within(assign.Key.Range, cursor.offset) {
		word, isKey = &assign.Key, true
	}
	if word == nil || !within(word.Range, cursor.offset) {
		return nil, false, nil
	}
	inWord := func(span jomini.Range) bool {
		return word.Range.Start <= span.Start && span.End <= word.Range.End && within(span, cursor.offset)
	}
	found := slices.IndexFunc(cursor.found, func(ref schema.Occurrence) bool {
		if ref.Role != schema.Refers || !inWord(ref.Range) {
			return false
		}
		_, known := index.Resolve(sess.Install.Schema, sess.Indexes(), ref, cursor.path)
		return known
	})
	if found < 0 {
		found = slices.IndexFunc(cursor.found, func(ref schema.Occurrence) bool { return ref.Role == schema.Refers && inWord(ref.Range) })
	}
	if found >= 0 {
		return word, isKey, &cursor.found[found]
	}
	if found := slices.IndexFunc(cursor.found, func(def schema.Occurrence) bool {
		return def.Role == schema.Defines && def.ID != "" && inWord(def.Range)
	}); found >= 0 {
		def := cursor.found[found]
		return word, isKey, &schema.Occurrence{Database: def.Database, ID: def.ID}
	}
	return word, isKey, nil
}

// Hover is the card for the word at a position, or nil where nothing is known of it.
func Hover(sess *workspace.Session, path string, line, col int) (*Inspect, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	offset := file.parsed.Lines().OffsetAt(line, col)
	if file.place.Format == inventory.Loc {
		return file.locCard(offset), nil
	}
	if file.place.Format != inventory.Script || file.place.Rel == "" {
		return nil, nil
	}

	// the word under the cursor, and the definition or reference the walk read in it; where nothing is
	// known of it, the card is only the entry and blocks the cursor is in
	cursor := file.walk(offset)
	within := cursor.within()
	bare := func(span jomini.Range) *Inspect {
		if len(within) == 0 {
			return nil
		}
		return &Inspect{Within: within, Range: file.span(span), Winner: -1}
	}
	word, isKey, named := cursor.word(sess)
	if word == nil {
		return bare(jomini.Range{Start: offset, End: offset}), nil
	}
	at := cursor.chain[len(cursor.chain)-1]
	assign, isAssign := at.statement.(*jomini.Assignment)
	card := &Inspect{Word: word.Text, Range: file.span(word.Range), Winner: -1, Within: within}
	if isAssign {
		card.Key = assign.Key.Text
	}

	// a data call written in the word
	start := word.Range.Start
	if word.Quoted {
		start++
	}
	if steps := dataCall(word.Text, start, offset); steps != nil {
		data := file.dataCard(steps)
		if data == nil {
			return bare(word.Range), nil
		}
		data.Within = within
		return data, nil
	}

	// what the word names (a $NAME$ in it included), else what its key reads as
	s, read := sess.Install.Schema, reading{ref: named, scope: at.context.Scope}
	if isKey {
		read.key, read.command = at.inner.Position(), at.command(s)
	}
	if isKey && (jomini.ChildBlock(at.statement) != nil || at.inner.Shape == game.ShapeValue) {
		read.holds = at.inner.Shape // a value block is also written as a number
	}

	// a link chain's part under the cursor reads through its own link, run where the one before leads
	if read.ref == nil && read.command != nil && read.command.Kinds&game.KindLink != 0 {
		from := word.Range.Start
		for _, part := range strings.Split(word.Text, ".") {
			if read.command = s.Link(strings.ToLower(part)); read.command == nil || offset <= from+len(part) {
				break
			}
			read.scope, from = "", from+len(part)+1
			if _, leads := s.Scopes[read.command.Target]; leads {
				read.scope = read.command.Target
			}
		}
	}
	described := file.describe(card, read)

	// a key nothing else reads that names a modifier the dumps list, else the most specific $TYPE$
	// family it matches
	if modifier, found := s.Modifiers[word.Text]; isKey && !described {
		for _, family := range s.Modifiers {
			matched, _ := filepath.Match(family.Pattern, word.Text)
			longer := len(family.Pattern) > len(modifier.Pattern) ||
				len(family.Pattern) == len(modifier.Pattern) && family.Pattern < modifier.Pattern
			if !found && family.Pattern != "" && matched && longer {
				modifier = family
			}
		}
		if found || modifier.Pattern != "" {
			card.Head, card.Prose, card.Stale = "modifier", modifier.Blurb, s.Sources[schema.SourceDumps].Stale
		}
	}

	// what the written form says: a date, a script constant's value in this file
	if jomini.Literal(word.Text) == jomini.LitDate {
		card.Date, card.Head = date(word.Text, file.place.Calendar), cmp.Or(card.Head, "date")
	}
	if _, constant := jomini.Constant(word.Text); constant {
		for _, statement := range file.parsed.Root.Statements {
			top, isTop := statement.(*jomini.Assignment)
			if !isTop || top.Key.Text != word.Text {
				continue
			}
			if value, isWord := top.Value.(*jomini.Scalar); isWord {
				card.Head, card.Name, card.Winner = "script constant", value.Text, 0
				card.Sites = []Location{locate(sess, file.path, file.parsed.Lines().PositionAt(top.Key.Range.Start))}
			}
		}
	}

	// a weighted branch (Structurals' numbered keys, random_list): its share of its siblings' numbers
	if parent := len(cursor.chain) - 2; isKey && parent >= 0 && jomini.Literal(word.Text) == jomini.LitNumber {
		block, sum, mine := jomini.ChildBlock(cursor.chain[parent].statement), 0.0, 0.0
		construct, found := game.ConstructOf(at.context.Shape, at.context.Command, "<number>")
		weighted := found && construct.Key == "<number>"
		if block != nil && weighted {
			for _, sibling := range block.Statements {
				branch, isBranch := sibling.(*jomini.Assignment)
				if isBranch && jomini.Literal(branch.Key.Text) == jomini.LitNumber {
					share, _ := strconv.ParseFloat(branch.Key.Text, 64)
					sum += share
					if branch == assign {
						mine = share
					}
				}
			}
		}
		if sum > 0 {
			card.Weight = int(mine*100/sum + 0.5)
		}
	}
	if isKey {
		card.Color = color(assign)
	}

	// a card says something, else there is none; a key nothing else names then says where it is
	// written: in its entry's database when the statement holding it is the entry, else in its key
	if card.Head == "" && card.Need == "" && card.Prose == "" && card.Date == "" && card.Color == "" &&
		card.Weight == 0 && card.Leads == "" && len(card.Takes)+len(card.Writes)+len(card.Sites) == 0 {
		return bare(word.Range), nil
	}
	if parent := len(cursor.chain) - 2; isKey && read.command == nil && parent >= 0 && card.Head == "" {
		holder, database := cursor.chain[parent], (*schema.Database)(nil)
		if holder.context.Entry >= 0 {
			database = s.Databases[cursor.found[holder.context.Entry].Database]
		}
		holding, isAssign := holder.statement.(*jomini.Assignment)
		switch {
		case database != nil && holder.inner.At == database.Entry:
			card.Head = "in " + heading(database.Name)
		case isAssign:
			card.Head = "in `" + holding.Key.Text + "`"
		}
	}
	return card, nil
}

// describe fills a card from what a word names, saying whether anything is known of it: a
// reference's definitions, else a command's or a key's claims. A blurb is the dumps' or the docs'.
func (file *file) describe(card *Inspect, read reading) bool {
	s := file.sess.Install.Schema
	shown := func(from schema.Source) {
		card.Stale = card.Stale || s.Sources[from].Stale
	}
	if read.ref != nil && file.defined(card, *read.ref, read.scope, shown) {
		return true
	}
	key := read.key
	switch {
	case read.command != nil:
		command := read.command
		card.Head, card.Prose, card.Takes, card.Scope = kinds(command), command.Blurb, takes(command.Args), read.scope
		if card.Scope == "" && len(command.Scopes) == 1 {
			card.Scope = command.Scopes[0]
		}
		if _, leads := s.Scopes[command.Target]; leads {
			card.Leads = command.Target
		}
		shown(schema.SourceDumps)
		key = command.Args
	case key != nil:
		card.Need, card.Takes, card.Scope, card.Leads = need(key), takes(key), read.scope, key.Scope.Value
		if from := key.Blurb.Source; from == schema.SourceDumps || from == schema.SourceDocs {
			card.Prose = key.Blurb.Value
			shown(from)
		}
		for _, from := range []schema.Source{
			key.Value.Source, key.Holds.Source, key.Scope.Source, key.Required.Source,
			key.Default.Source,
		} {
			shown(from)
		}
	default:
		return false
	}

	// where no source declares what it takes, what the walker reads its block as (AND: triggers),
	// else what vanilla writes there: an inference, which alone does not make the card known
	if len(card.Takes) == 0 && held[read.holds] != "" {
		card.Takes = []string{held[read.holds]}
	}
	if len(card.Takes) == 0 {
		card.Writes = writes(key)
	}
	return card.Head != "" || card.Need != "" || card.Prose != "" || len(card.Takes) > 0 || card.Leads != ""
}

// defined fills a reference's card: its database, its sites (the kept one and its adds), localized
// name and what the dumps and docs say of its database; a call nothing defines is unknown. scope is
// where the reference is written, which a scripted entry runs in when its database declares none.
func (file *file) defined(card *Inspect, ref schema.Occurrence, scope string, shown func(schema.Source)) bool {
	sess, s := file.sess, file.sess.Install.Schema
	defs, known := index.Resolve(s, sess.Indexes(), ref, file.place.Path)
	database := ref.Database
	if len(defs) > 0 {
		database = defs[0].Occurrence().Database
	}
	row := s.Databases[database]
	if !known && (ref.Caller != "" || row == nil && database != schema.Localization && database != schema.File) {
		return false
	}
	card.Word, card.Head = ref.ID, game.KindName(database)
	if entry, name, cut := strings.Cut(ref.ID, ":"); database == schema.Parameter && cut {
		card.Word, card.Head = name, "parameter of "+entry
	}
	if database == schema.Localization || database == schema.File {
		file.placeCard(card, database, ref.ID)
		return true
	}

	defs = file.inFile(defs)
	for _, def := range defs {
		card.Sites = append(card.Sites, defined(sess, def))
	}
	winner, adds, _, _ := sess.Winner(defs)
	if len(defs) > 0 && row != nil {
		card.Winner = slices.Index(defs, winner)
		for _, add := range adds {
			card.Adds = append(card.Adds, slices.Index(defs, add))
		}
	}

	// its localized name, and the comment a mod's author leads their definition with
	comment := ""
	if row != nil {
		card.Name = loc.Plain(sess.Title(ref.ID, winner)) // a name nothing defines still shows its own loc key's text
		comment = leadComment(sess, winner)
	}

	// what its database's entries are and run in; an on_action's own blurb and scope from the dumps;
	// the author's comment where nothing declared says more
	if row != nil {
		if from := row.Entry.Blurb.Source; from == schema.SourceDumps || from == schema.SourceDocs {
			card.About = row.Entry.Blurb.Value
			shown(from)
		}
		card.Scope, card.Returns = row.Entry.Scope.Value, returns[row.Entry.Holds.Value]
		if card.Scope == "" && len(row.Row.CalledIn) > 0 {
			card.Scope = scope
		}
		shown(row.Entry.Scope.Source)
		if action, found := s.OnActions[ref.ID]; found && row.Row.OnActions {
			card.Prose, card.Scope = action.Blurb, cmp.Or(action.Scope, card.Scope)
			shown(schema.SourceDumps)
		}
		card.Graphed = row.Row.Events || row.Row.OnActions
	}
	card.Prose, card.Graphed = cmp.Or(card.Prose, comment), card.Graphed || len(defs) > 0 && len(sess.Fires(winner)) > 0
	return true
}

// placeCard fills a loc key's or a file path's card: the key's text and its entry, or the file's place and, for
// an image, its header.
func (file *file) placeCard(card *Inspect, database, id string) {
	sess := file.sess
	if database == schema.Localization {
		if entry, found := sess.Loc(id, ""); found {
			card.Name, card.Winner = loc.Plain(entry.Text()), 0
			card.Sites = []Location{locate(sess, entry.File().Path, Position{Line: int(entry.Occurrence().Line)})}
		}
		return
	}
	found, listed := index.FileAt(sess.Indexes(), id)
	if listed {
		card.Sites, card.Winner = []Location{locate(sess, found.Path, Position{})}, 0
	}
	if found.Format == inventory.Image {
		if info, err := texture.Info(found.Path); err == nil {
			card.Image = &info
		}
	}
}

// leadComment is the comment a mod's author leads a definition with; "" for the game's own or none.
func leadComment(sess *workspace.Session, def index.At) string {
	if origin := def.File().Origin; origin == game.OriginVanilla || origin == "" {
		return ""
	}
	parsed, statement := sess.Entry(def)
	if statement == nil {
		return ""
	}
	attached, _ := parsed.Attach()
	var lead []string
	for _, above := range attached[statement] {
		if text := parsed.Src[above.Range.Start:above.Range.End]; above.Range.End <= statement.NodeRange().Start {
			lead = append(lead, strings.TrimSpace(strings.TrimLeft(text, "#")))
		}
	}
	return strings.Join(lead, "\n")
}

// locRef is what an offset of a localization file names: a reference the scan read ($key$, a
// concept), else the line's key; with neither, the data call steps it is in, if any.
func (file *file) locRef(offset int) (ref *schema.Occurrence, steps []jomini.Step) {
	lines := file.parsed.Lines()
	start := lines.OffsetAt(lines.PositionAt(offset).Line, 0)
	line := file.text[start:]
	line, _, _ = strings.Cut(line, "\n")
	line = strings.TrimRight(line, "\r")
	for _, index := range file.sess.Indexes() {
		for _, at := range index.InFile(file.path) {
			if found := at.Occurrence(); found.Role == schema.Refers && within(found.Range, offset) {
				ref = &found
			}
		}
	}
	if steps = dataCall(line, start, offset); ref == nil && steps != nil {
		return nil, steps
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	key, _, _ := strings.Cut(line[indent:], ":")
	span := jomini.Range{Start: start + indent, End: start + indent + len(key)}
	if ref == nil && within(span, offset) {
		ref = &schema.Occurrence{Role: schema.Refers, Database: schema.Localization, ID: key, Range: span, Entry: -1}
	}
	return ref, nil
}

// locCard is the card in a localization file: what locRef reads at the offset; nil where it is
// unknown.
func (file *file) locCard(offset int) *Inspect {
	ref, steps := file.locRef(offset)
	if steps != nil {
		return file.dataCard(steps)
	}
	if ref == nil {
		return nil
	}
	if _, known := index.Resolve(file.sess.Install.Schema, file.sess.Indexes(), *ref, file.path); !known {
		return nil
	}
	card := &Inspect{Word: ref.ID, Range: file.span(ref.Range), Winner: -1}
	file.defined(card, *ref, "", func(schema.Source) {})
	return card
}

// SignatureHelp is the arguments of the command whose block holds the cursor: the keys a declared
// source states its block holds, with the command's blurb; nil elsewhere.
func SignatureHelp(sess *workspace.Session, path string, line, col int) (*Signature, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil || file.place.Format != inventory.Script {
		return nil, err
	}

	// only the block the cursor is directly in: a key written in a command's block is no argument
	cursor := file.walk(file.parsed.Lines().OffsetAt(line, col))
	holder := cursor.block()
	if holder < 0 {
		return nil, nil
	}
	at := cursor.chain[holder]
	assign, isAssign := at.statement.(*jomini.Assignment)
	command := at.command(sess.Install.Schema)
	if !isAssign || command == nil || command.Kinds&game.KindLink != 0 {
		return nil, nil
	}
	names, stated := keys(command.Args)
	if names = names[:stated]; len(names) == 0 {
		return nil, nil
	}
	label := assign.Key.Text + " = { " + strings.Join(names, " ") + " }"
	return &Signature{Label: label, Parameters: names, Docs: command.Blurb}, nil
}

// dataCard is a data call step's card: a custom localization a quoted argument names, a data
// function or concept entry, a function of the type before it, else a data type.
func (file *file) dataCard(steps []jomini.Step) *Inspect {
	s, step := file.sess.Install.Schema, steps[len(steps)-1]
	card, defined := &Inspect{Word: step.Name, Range: file.span(step.Range), Winner: -1}, false
	on, stale := leads(s.DataTypes, steps[:len(steps)-1]), s.Sources[schema.SourceDumps].Stale
	for _, arg := range step.Args {
		if custom, found := s.CustomLoc[arg.Text]; arg.Quoted && found {
			card.Word, card.Head, card.Scope, card.Stale = arg.Text, "custom localization", custom.Scope, stale
			card.Takes = []string{oneOf(custom.Entries)}
			if custom.Parent != "" {
				card.Takes = append(card.Takes, "parent "+custom.Parent, "suffix "+custom.Suffix)
			}
			return card
		}
	}
	for _, name := range s.DatabasesWhere(func(row game.Folder) bool { return row.Functions || row.Concepts }) {
		if !defined && len(index.Lookup(file.sess.Indexes(), schema.Defines, name, step.Name)) > 0 {
			defined = file.defined(card, schema.Occurrence{Database: name, ID: step.Name}, "", func(schema.Source) {})
		}
	}
	_, isType := s.DataTypes[step.Name]
	if function, found := s.DataTypes[on].Functions[step.Name]; found {
		card.Head, card.Scope, card.Leads = cmp.Or(card.Head, "data function"), on, function.Returns
		card.Stale = card.Stale || stale
	} else if len(steps) == 1 && isType && !defined {
		card.Head, card.Stale = "data type", stale
	} else if !defined {
		return nil
	}
	return card
}

// leads is the data type a data call's steps lead to: from the global functions, or a data type
// written first, through each function's return type; "" where a step is unknown.
func leads(types map[string]schema.DataType, steps []jomini.Step) string {
	on := game.GlobalType
	for i, step := range steps {
		function, found := types[on].Functions[step.Name]
		_, isType := types[step.Name]
		switch {
		case found:
			on = function.Returns
		case i == 0 && isType:
			on = step.Name
		default:
			return ""
		}
	}
	return on
}

// dataCall is the data call step at an offset of text written from start: the steps up to it; nil
// where the offset is in no [ … ] step.
func dataCall(text string, start, offset int) []jomini.Step {
	for _, segment := range jomini.Calls(text, start) {
		steps := jomini.ParseCall(text[segment.Start-start:segment.End-start], segment.Start)
		for i, step := range steps {
			if within(step.Range, offset) {
				return steps[:i+1]
			}
		}
	}
	return nil
}

// within reports an offset inside a span, end included, since the cursor often sits after it.
func within(span jomini.Range, offset int) bool {
	return span.Start <= offset && offset <= span.End
}

// defined is where a definition is written.
func defined(sess *workspace.Session, def index.At) Location {
	return locate(sess, def.File().Path, Position{Line: int(def.Occurrence().Line), Character: int(def.Occurrence().Col)})
}

// locate is the editor's location of a position in a file of the workspace or the install.
func locate(sess *workspace.Session, path string, at Position) Location {
	listed, _, _ := sess.Located(path)
	return Location{Path: path, Origin: listed.Origin, Rel: listed.Rel, Range: Range{Start: at, End: at}}
}

// kinds is what a command is, in the dumps' words: effect, trigger, event target, joined.
func kinds(command *schema.Command) string {
	var names []string
	for _, kind := range game.KindWords {
		if command.Kinds&kind.Kind != 0 {
			names = append(names, kind.Word)
		}
	}
	return strings.Join(names, " and ")
}

// need is what a declared source says of a key being required: optional, required, required when.
func need(key *schema.Position) string {
	if key == nil {
		return ""
	}
	switch required := key.Required.Value; required.Level {
	case schema.LevelOptional:
		return "optional"
	case schema.LevelAlways:
		return "required"
	case schema.LevelConditional:
		return "required when " + required.Condition
	}
	return ""
}

// takes is what a key takes, as declared, never vanilla's inference: its block's triggers or
// effects, its word's type or list (a long one ends with how many more), and its default.
func takes(key *schema.Position) []string {
	if key == nil {
		return nil
	}
	claim, out := key.Value.Value, []string(nil)
	if key.Value.Source == schema.SourceVanilla {
		claim = schema.Expected{}
	}
	said := []string{held[key.Holds.Value], typed(claim)}
	if names := slices.Sorted(maps.Keys(key.Children)); key.Holds.Value == game.ShapeArguments && len(names) > 0 {
		said = append(said, "a block with "+strings.Join(names, ", "))
	}
	if len(claim.Choices) > 0 {
		said = append(said, oneOf(claim.Choices))
	}
	if key.Default.Source != schema.SourceNone {
		said = append(said, "defaults to "+key.Default.Value)
	}
	for _, text := range said {
		if text != "" && !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	return out
}

// oneOf is a word list as a card says it: the first six, then how many more.
func oneOf(words []string) string {
	if len(words) > 6 {
		words = append(words[:6:6], fmt.Sprintf("… and %d more", len(words)-6))
	}
	return "one of " + strings.Join(words, ", ")
}

// The card's words for a number and a yes-or-no value.
const (
	aNumber = "a number"
	yesOrNo = "yes or no"
)

// held is what a block of each kind holds, as a card says it.
var held = map[game.Shape]string{
	game.ShapeTriggers: "triggers", game.ShapeEffects: "effects", game.ShapeValue: aNumber,
	game.ShapeDescription: "a description",
}

// returns is what a database's entries give where they are run, by what their block is.
var returns = map[game.Shape]string{game.ShapeTriggers: yesOrNo, game.ShapeValue: aNumber}

// typed is a word type as a card says it; "" unknown.
func typed(claim schema.Expected) string {
	return map[schema.ValueType]string{
		schema.ValueNumber: aNumber, schema.ValueBool: yesOrNo, schema.ValueDate: "a date", schema.ValueString: "text",
		schema.ValueReference: game.KindName(claim.Database), schema.ValueLoc: "a localization key",
		schema.ValueFile: "a file path", schema.ValueScope: "a scope", schema.ValueBlock: "a block",
	}[claim.Type]
}

// writes is what vanilla writes at a key, as a card says it: each form it writes there, its words
// and texts typed by vanilla's names, else its words listed where few.
func writes(key *schema.Position) (out []string) {
	words := slices.Sorted(maps.Keys(key.Vanilla.Values))
	for _, row := range []struct {
		form schema.Writes
		said string
	}{
		{schema.WritesNumber, aNumber},
		{schema.WritesBool, yesOrNo},
		{schema.WritesDate, "a date"},
		{schema.WritesIdent, "a word"},
		{schema.WritesString, "text"},
		{schema.WritesBlock, "a block"},
	} {
		form, said := row.form, row.said
		switch {
		case key.Vanilla.Writes&form == 0:
			continue
		case form&(schema.WritesIdent|schema.WritesString) == 0:
		case key.Value.Source == schema.SourceVanilla:
			said = typed(key.Value.Value)
		case form == schema.WritesIdent && len(words) <= 6:
			said = oneOf(words)
		}
		if !slices.Contains(out, said) {
			out = append(out, said)
		}
	}
	return out
}

// heading is a database's name as a modder reads it: its last folder, underscores and dots spaced.
func heading(database string) string {
	return strings.NewReplacer("_", " ", ".", " ").Replace(database[strings.LastIndexByte(database, '/')+1:])
}

// keys is a position's written keys, placeholders aside: those a declared source states first, then
// those vanilla writes, the most written first; stated is how many lead.
func keys(position *schema.Position) (names []string, stated int) {
	rank := map[string]int{}
	for name, key := range position.Children {
		if strings.HasPrefix(name, "<") {
			continue
		}
		names, rank[name] = append(names, name), 1
		if key.DeclaredBy != schema.SourceNone {
			rank[name], stated = 0, stated+1
		}
	}
	slices.SortFunc(names, func(first, second string) int {
		return cmp.Or(rank[first]-rank[second], position.Children[second].Vanilla.Count-position.Children[first].Vanilla.Count,
			strings.Compare(first, second))
	})
	return names, stated
}

// date writes a date out (January 1st, 8082 AD; year -221 is 221 BC), and in the mod's calendar
// where one of its eras applies (82.1.1 A.C.).
// TODO: What about custom months in addition to years?
func date(text string, calendar workspace.Calendar) string {
	year, month, day, _, ok := jomini.ParseDate(text)
	if !ok || month < 1 || month > 12 {
		return ""
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	out := fmt.Sprintf("%s %s, %d AD", months[month], ordinal(day), year)
	if year < 0 {
		out = fmt.Sprintf("%s %s, %d BC", months[month], ordinal(day), -year)
	}
	if inEra, ok := calendar.Date(year, month, day); ok {
		out += "; " + inEra + " in this mod's calendar"
	}
	return out
}

// ordinal is 1st, 2nd, 3rd, 4th … 11th, 12th, 13th … 21st.
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

// color is the #rrggbb of a color block a statement writes (game.Colors), for a swatch; "" for
// none.
func color(assign *jomini.Assignment) string {
	tagged, isTagged := assign.Value.(*jomini.TaggedBlock)
	if !isTagged {
		return ""
	}
	var words []string
	var parts []float64
	for _, statement := range tagged.Block.Statements {
		if list, isList := statement.(*jomini.ValueStmt); isList {
			if scalar, isScalar := list.Value.(*jomini.Scalar); isScalar {
				part, _ := strconv.ParseFloat(scalar.Text, 64)
				words, parts = append(words, scalar.Text), append(parts, part)
			}
		}
	}
	found := slices.IndexFunc(game.Colors, func(row game.Color) bool { return strings.EqualFold(row.Tag, tagged.Tag.Text) })
	switch {
	case found < 0:
	case game.Colors[found].Hex && len(words) == 1:
		if digits := strings.TrimPrefix(strings.ToLower(words[0]), "0x"); len(digits) >= 6 {
			return "#" + digits[:6]
		}
	case len(parts) < 3:
	case game.Colors[found].HSV:
		row := game.Colors[found]
		return hex(hsv(parts[0]/cmp.Or(row.Hue, 1), parts[1]/cmp.Or(row.Parts, 1), parts[2]/cmp.Or(row.Parts, 1)))
	default:
		return hex(parts[0], parts[1], parts[2])
	}
	return ""
}

// hex spells a color whose parts are 0 to 255, or 0 to 1 where every part is at most 1.
func hex(red, green, blue float64) string {
	if red <= 1 && green <= 1 && blue <= 1 {
		red, green, blue = red*255, green*255, blue*255
	}
	return fmt.Sprintf("#%02x%02x%02x", int(red+0.5), int(green+0.5), int(blue+0.5))
}

// hsv turns hue, saturation and value, each 0 to 1, into red, green and blue, each 0 to 1.
func hsv(hue, saturation, value float64) (float64, float64, float64) {
	// the hue's sixth of the color wheel, how far into it, and the three levels a channel takes
	sector := int(hue*6) % 6
	fraction := hue*6 - float64(int(hue*6))
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
