// Package lsp answers the editor's requests for one open file: the word under the cursor, where it
// is defined and used, what may be written there, what is wrong, and how the file is folded,
// formatted and annotated. Every answer starts from one walk of the file over the schema
// (index.Walk), stopped at the cursor or run whole; payloads carry display data and the frontend
// templates the text.
//
// hover.go is the card for the word under the cursor, read from one walk that stops past it: what
// the word is, the scope it runs in and leads to, an object's localized name, and the blurbs the
// dumps and docs give, marked when their source may be outdated. Signature help is a command's
// arguments, read from the same walk. It also holds the open file every request reads.
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
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// file is an open file's parse and place, held for one request.
type file struct {
	sess   *session.Session
	path   string
	text   string
	parsed *jomini.Result
	place  session.Place
}

// open parses a file (once per text for an open buffer) and places it.
func open(sess *session.Session, path string) (*file, error) {
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
	Word    string            `json:"word"`              // the name it names: an entry, command, key, loc key
	Key     string            `json:"key,omitempty"`     // the key the word is written at, or is
	Head    string            `json:"head,omitempty"`    // the card's first line: what the word is
	Name    string            `json:"name,omitempty"`    // an object's localized name, a loc key's text
	About   string            `json:"about,omitempty"`   // what the dumps or docs say of its database
	Prose   string            `json:"prose,omitempty"`   // what the dumps or docs say of the word
	Takes   []string          `json:"takes,omitempty"`   // a key or command: what a word there may be
	Scope   string            `json:"scope,omitempty"`   // the scope it runs in; "" unknown
	Leads   string            `json:"leads,omitempty"`   // the scope it leads to; "" none
	Sites   []Location        `json:"sites,omitempty"`   // where it is defined, in load order
	Winner  int               `json:"winner"`            // the site the game keeps; -1 for none
	Adds    []int             `json:"adds,omitempty"`    // the sites that add to the kept one (INJECT)
	Stale   bool              `json:"stale,omitempty"`   // a fact shown may be outdated: its source is
	Date    string            `json:"date,omitempty"`    // a date written out, and in a mod's calendar
	Weight  int               `json:"weight,omitempty"`  // a numbered branch: its share of its siblings, in %
	Color   string            `json:"color,omitempty"`   // a colour, as #rrggbb
	Image   *source.ImageInfo `json:"image,omitempty"`   // a file that is an image: its header
	Graphed bool              `json:"graphed,omitempty"` // the event graph draws it: it is or fires an event
	Range   Range             `json:"range"`
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
	ref     *index.Ref
	key     *schema.Key
	command *schema.Command
	scope   string
}

// spot is the cursor's place in a script file, from one walk that stops past it: the statements
// holding it, outer to inner, and the records so far.
type spot struct {
	chain   []index.Context
	records *index.Records
	offset  int
}

// walk reads the file up to an offset (spec §7.2). A statement holds the offset when its span
// does, or when it ends at an operator with only blanks up to the offset: a value still to come.
func (file *file) walk(offset int) spot {
	cursor := spot{offset: offset}
	located, _, _ := file.sess.Located(file.path)
	cursor.records = index.Walk(file.sess.Schema(), located, file.parsed, func(at index.Context) bool {
		span := at.Statement.NodeRange()
		if span.Start > offset {
			return false
		}
		assign, isAssign := at.Statement.(*jomini.Assignment)
		if offset <= span.End || isAssign && assign.Value == nil && strings.TrimSpace(file.text[span.End:offset]) == "" {
			cursor.chain = append(cursor.chain, at)
		}
		return true
	})
	return cursor
}

// block is the innermost statement whose block holds the cursor, -1 at the file's top; its
// context says how that block reads (index.Context's Inner fields).
func (cursor spot) block() int {
	holder := -1
	for i, at := range cursor.chain {
		block := jomini.ChildBlock(at.Statement)
		if block != nil && cursor.offset > block.OpenBrace && (block.CloseBrace < 0 || cursor.offset <= block.CloseBrace) {
			holder = i
		}
	}
	return holder
}

// word is the scalar under the cursor (the innermost statement's key or value; nil for none), if it
// is the key, and the definition or reference the walk read inside it ($NAME$ included).
func (cursor spot) word() (word *jomini.Scalar, isKey bool, ref *index.Ref) {
	if len(cursor.chain) == 0 {
		return nil, false, nil
	}
	at := cursor.chain[len(cursor.chain)-1]
	if list, isList := at.Statement.(*jomini.ValueStmt); isList {
		word, _ = list.Value.(*jomini.Scalar)
	} else if assign, isAssign := at.Statement.(*jomini.Assignment); isAssign {
		word, _ = assign.Value.(*jomini.Scalar)
		if within(assign.Key.Range, cursor.offset) {
			word, isKey = &assign.Key, true
		}
	}
	if word == nil || !within(word.Range, cursor.offset) {
		return nil, false, nil
	}
	inWord := func(span jomini.Range) bool {
		return word.Range.Start <= span.Start && span.End <= word.Range.End && within(span, cursor.offset)
	}
	if found := slices.IndexFunc(cursor.records.Refs, func(ref index.Ref) bool { return inWord(ref.Range) }); found >= 0 {
		return word, isKey, &cursor.records.Refs[found]
	}
	if found := slices.IndexFunc(cursor.records.Defs, func(def index.Def) bool {
		return def.Name != "" && inWord(def.Range)
	}); found >= 0 {
		def := cursor.records.Defs[found]
		return word, isKey, &index.Ref{Database: def.Database, Name: def.Name}
	}
	return word, isKey, nil
}

// Hover is the card for the word at a position, or nil where nothing is known of it.
func Hover(sess *session.Session, path string, line, col int) (*Inspect, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	offset := file.parsed.Lines().OffsetAt(line, col)
	if file.place.Format == source.Loc {
		return file.locCard(offset), nil
	}
	if file.place.Format != source.Script || file.place.Rel == "" {
		return nil, nil
	}

	// the word under the cursor, and the definition or reference the walk read in it
	cursor := file.walk(offset)
	word, isKey, named := cursor.word()
	if word == nil {
		return nil, nil
	}
	at := cursor.chain[len(cursor.chain)-1]
	assign, isAssign := at.Statement.(*jomini.Assignment)
	card := &Inspect{Word: word.Text, Range: file.span(word.Range), Winner: -1}
	if isAssign {
		card.Key = assign.Key.Text
	}

	// a data call written in the word
	start := word.Range.Start
	if word.Quoted {
		start++
	}
	if steps := dataCall(word.Text, start, offset); steps != nil {
		return file.dataCard(steps), nil
	}

	// what the word names (a $NAME$ in it included), else what its key reads as
	sch, read := sess.Schema(), reading{ref: named, scope: at.Scope}
	if isKey {
		read.key, read.command = at.Key, at.Command
	}

	// a link chain's part under the cursor reads through its own link, run where the one before leads
	if read.ref == nil && read.command != nil && read.command.Kind&schema.KindLink != 0 {
		from := word.Range.Start
		for _, part := range strings.Split(word.Text, ".") {
			if read.command = sch.Link(strings.ToLower(part)); read.command == nil || offset <= from+len(part) {
				break
			}
			read.scope, from = "", from+len(part)+1
			if _, leads := sch.Scopes[read.command.Out]; leads {
				read.scope = read.command.Out
			}
		}
	}
	known := file.describe(card, read)

	// a key nothing else reads that names a modifier the dumps list, a $TYPE$ family's included
	if modifier, found := sch.Modifiers[word.Text]; isKey && !known {
		for _, name := range slices.Sorted(maps.Keys(sch.Modifiers)) {
			if found || !sch.Modifiers[name].Families {
				continue
			}
			pattern, last := "", 0
			for _, part := range jomini.Interpolations(name) {
				pattern, last = pattern+name[last:part.Start]+"*", part.End
			}
			if matched, _ := filepath.Match(pattern+name[last:], word.Text); matched {
				modifier, found = sch.Modifiers[name], true
			}
		}
		if found {
			card.Head, card.Prose, card.Stale = "modifier", modifier.Blurb, sch.Sources[schema.SourceDumps].Stale
			known = true
		}
	}

	// what the written form says: a date, a script constant's value in this file
	if jomini.Literal(word.Text) == jomini.LitDate {
		card.Date, card.Head, known = date(word.Text, file.place.Calendar), cmp.Or(card.Head, "date"), true
	}
	if _, constant := jomini.Constant(word.Text); constant {
		for _, statement := range file.parsed.Root.Statements {
			top, isTop := statement.(*jomini.Assignment)
			if !isTop || top.Key.Text != word.Text {
				continue
			}
			if value, isWord := top.Value.(*jomini.Scalar); isWord {
				card.Head, card.Name, card.Winner, known = "script constant", value.Text, 0, true
				card.Sites = []Location{locate(sess, file.path, file.parsed.Lines().PositionAt(top.Key.Range.Start))}
			}
		}
	}

	// a numbered key in a command's block: its share of its siblings' numbers (random_list)
	if parent := len(cursor.chain) - 2; isKey && parent >= 0 && jomini.Literal(word.Text) == jomini.LitNumber {
		holder, block, sum, mine := cursor.chain[parent], jomini.ChildBlock(cursor.chain[parent].Statement), 0.0, 0.0
		if block != nil && holder.Command != nil && holder.Command.Kind&schema.KindLink == 0 {
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
			card.Weight, known = int(mine*100/sum+0.5), true
		}
	}
	if isKey {
		card.Color = colour(assign)
		known = known || card.Color != ""
	}
	if !known {
		return nil, nil
	}
	return card, nil
}

// describe fills a card from what a word names, saying whether anything is known of it: a
// reference's definitions, else a command's or a key's claims. A blurb is the dumps' or the docs'.
func (file *file) describe(card *Inspect, read reading) bool {
	sch := file.sess.Schema()
	shown := func(from schema.Source) {
		card.Stale = card.Stale || sch.Sources[from].Stale
	}
	if read.ref != nil && file.defined(card, *read.ref, shown) {
		return true
	}
	switch {
	case read.command != nil:
		command := read.command
		card.Head, card.Prose, card.Takes, card.Scope = kinds(command), command.Blurb, takes(command.Args), read.scope
		if card.Scope == "" && len(command.Scopes) == 1 {
			card.Scope = command.Scopes[0]
		}
		if _, leads := sch.Scopes[command.Out]; leads {
			card.Leads = command.Out
		}
		shown(schema.SourceDumps)

		// its game text (D-32): each word of the entry named after it in a database describing its
		// kind that is a loc key, as key: text, in the order written
		var lines []string
		describing := map[string]schema.Kinds{"trigger": schema.KindTrigger, "effect": schema.KindEffect}
		for _, database := range sch.Flagged(func(row game.Folder) bool { return describing[row.Describes]&command.Kind != 0 }) {
			winner, _, _, _ := file.sess.Winner(file.sess.Defs(database, strings.ToLower(card.Word)))
			_, entry := file.sess.Entry(winner)
			block := jomini.ChildBlock(entry)
			for i := 0; block != nil && i < len(block.Statements); i++ {
				field, isField := block.Statements[i].(*jomini.Assignment)
				if !isField {
					continue
				}
				if value, isWord := field.Value.(*jomini.Scalar); isWord {
					if text, found := file.sess.Loc(value.Text, ""); found {
						lines = append(lines, field.Key.Text+": "+loc.Plain(text.Text))
					}
				}
			}
		}
		card.Name = strings.Join(lines, "\n")
		return true
	case read.key != nil:
		key := read.key
		card.Head, card.Takes, card.Scope, card.Leads = need(key), takes(key), read.scope, key.Scope.Value
		if from := key.Blurb.From; from == schema.SourceDumps || from == schema.SourceDocs {
			card.Prose = key.Blurb.Value
			shown(from)
		}
		for _, from := range []schema.Source{
			key.Value.From, key.Block.From, key.Scope.From, key.Required.From,
			key.Default.From,
		} {
			shown(from)
		}
		return card.Head != "" || card.Prose != "" || len(card.Takes) > 0 || card.Leads != ""
	}
	return false
}

// defined fills a reference's card: its database, its sites (the kept one and its adds), localized
// name and what the dumps and docs say of its database; a call nothing defines is unknown.
func (file *file) defined(card *Inspect, ref index.Ref, shown func(schema.Source)) bool {
	sess, sch := file.sess, file.sess.Schema()
	defs, known := index.Resolve(sch, sess.Indexes(), ref)
	database := ref.Database
	if len(defs) > 0 {
		database = defs[0].Database
	}
	row := sch.Databases[database]
	if !known && (ref.Called != "" || row == nil && database != index.Localization && database != index.File) {
		return false
	}
	card.Word, card.Head = ref.Name, heading(database)
	if entry, name, cut := strings.Cut(ref.Name, ":"); database == index.Parameter && cut {
		card.Word, card.Head = name, "parameter of "+entry
	}

	// a loc key's text, a file's place and image header
	switch database {
	case index.Localization:
		if entry, found := sess.Loc(ref.Name, ""); found {
			card.Name, card.Winner = loc.Plain(entry.Text), 0
			card.Sites = []Location{locate(sess, entry.Path, Position{Line: entry.Line})}
		}
		return true
	case index.File:
		found, listed := index.FileAt(sess.Indexes(), ref.Name)
		if listed {
			card.Sites, card.Winner = []Location{locate(sess, found.Path, Position{})}, 0
		}
		if found.Format == source.Image {
			if info, err := source.ReadImageInfo(found.Path); err == nil {
				card.Image = &info
			}
		}
		return true
	}

	defs = file.inFile(defs)
	for _, def := range defs {
		card.Sites = append(card.Sites, locate(sess, def.Path, Position{Line: int(def.Line), Character: int(def.Col)}))
	}
	winner, adds, _, _ := sess.Winner(defs)
	if len(defs) > 0 && row != nil {
		card.Winner = slices.Index(defs, winner)
		for _, add := range adds {
			card.Adds = append(card.Adds, slices.Index(defs, add))
		}
	}

	// its localized name, and the comment a mod's author leads their definition with (spec §9)
	listed, _, _ := sess.Located(winner.Path)
	authored, comment := len(defs) > 0 && listed.Origin != game.OriginVanilla && listed.Origin != "", ""
	if row != nil {
		winner.Name = ref.Name // a name nothing defines still shows its own loc key's text
		card.Name = loc.Plain(sess.Title(winner))
	}
	if row != nil && authored {
		if parsed, statement := sess.Entry(winner); statement != nil {
			attached, _ := parsed.Attach()
			var lead []string
			for _, above := range attached[statement] {
				if text := parsed.Src[above.Range.Start:above.Range.End]; above.Range.End <= statement.NodeRange().Start {
					lead = append(lead, strings.TrimSpace(strings.TrimLeft(text, "#")))
				}
			}
			comment = strings.Join(lead, "\n")
		}
	}

	// what its database's entries are and run in; an on_action's own blurb and scope from the dumps;
	// the author's comment where nothing declared says more
	if row != nil {
		if from := row.Entry.Blurb.From; from == schema.SourceDumps || from == schema.SourceDocs {
			card.About = row.Entry.Blurb.Value
			shown(from)
		}
		card.Scope = row.Entry.Scope.Value
		shown(row.Entry.Scope.From)
		if action, found := sch.OnActions[ref.Name]; found && row.Naming.OnActions {
			card.Prose, card.Scope = action.Blurb, cmp.Or(action.Scope, card.Scope)
			shown(schema.SourceDumps)
		}
		card.Graphed = row.Naming.Events || row.Naming.OnActions
	}
	card.Prose = cmp.Or(card.Prose, comment)
	card.Graphed = card.Graphed || len(defs) > 0 && len(sess.Fires(winner)) > 0
	return true
}

// locRef is what an offset of a localization file names: a reference the scan read ($key$, a
// concept), else the line's key; with neither, the data call steps it is in, if any.
func (file *file) locRef(offset int) (ref *index.Ref, steps []jomini.Step) {
	lines := file.parsed.Lines()
	start := lines.OffsetAt(lines.PositionAt(offset).Line, 0)
	line := file.text[start:]
	line, _, _ = strings.Cut(line, "\n")
	line = strings.TrimRight(line, "\r")
	for _, idx := range file.sess.Indexes() {
		listed, found := idx.At(file.path)
		records := idx.Records[listed.Path]
		for i := 0; found && records != nil && i < len(records.Refs); i++ {
			if within(records.Refs[i].Range, offset) {
				ref = &records.Refs[i]
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
		ref = &index.Ref{Database: index.Localization, Name: key, Range: span}
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
	if _, known := index.Resolve(file.sess.Schema(), file.sess.Indexes(), *ref); !known {
		return nil
	}
	card := &Inspect{Word: ref.Name, Range: file.span(ref.Range), Winner: -1}
	file.defined(card, *ref, func(schema.Source) {})
	return card
}

// SignatureHelp is the arguments of the command whose block holds the cursor: the keys its block
// holds, those a declared source states first, with the command's blurb; nil elsewhere.
func SignatureHelp(sess *session.Session, path string, line, col int) (*Signature, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil || file.place.Format != source.Script {
		return nil, err
	}

	// only the block the cursor is directly in: a key written in a command's block is no argument
	cursor := file.walk(file.parsed.Lines().OffsetAt(line, col))
	holder := cursor.block()
	if holder < 0 {
		return nil, nil
	}
	at := cursor.chain[holder]
	assign, isAssign := at.Statement.(*jomini.Assignment)
	if !isAssign || at.Command == nil || at.Command.Kind&schema.KindLink != 0 {
		return nil, nil
	}
	names, _ := keys(at.Command.Args)
	if len(names) == 0 {
		return nil, nil
	}
	label := assign.Key.Text + " = { " + strings.Join(names, " ") + " }"
	return &Signature{Label: label, Parameters: names, Docs: at.Command.Blurb}, nil
}

// dataCard is a data call step's card: a custom localization a quoted argument names, a data
// function or concept entry (D-31), a function of the type before it, else a data type.
func (file *file) dataCard(steps []jomini.Step) *Inspect {
	sch, step := file.sess.Schema(), steps[len(steps)-1]
	card, defined := &Inspect{Word: step.Name, Range: file.span(step.Range), Winner: -1}, false
	on, stale := leads(sch.DataTypes, steps[:len(steps)-1]), sch.Sources[schema.SourceDumps].Stale
	for _, arg := range step.Args {
		if custom, found := sch.CustomLoc[arg.Text]; arg.Quoted && found {
			card.Word, card.Head, card.Scope, card.Stale = arg.Text, "custom localization", custom.Scope, stale
			card.Takes = takes(&schema.Key{Value: schema.Said[schema.Value]{Value: schema.Value{Words: custom.Entries}}})
			if custom.Parent != "" {
				card.Takes = append(card.Takes, "parent "+custom.Parent, "suffix "+custom.Suffix)
			}
			return card
		}
	}
	for _, name := range sch.Flagged(func(row game.Folder) bool { return row.Functions || row.Concepts }) {
		if !defined && len(file.sess.Defs(name, step.Name)) > 0 {
			defined = file.defined(card, index.Ref{Database: name, Name: step.Name}, func(schema.Source) {})
		}
	}
	_, isType := sch.DataTypes[step.Name]
	if function, found := sch.DataTypes[on].Functions[step.Name]; found {
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
	on := "Global"
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

// locate is the editor's location of a position in a file of the workspace or the install.
func locate(sess *session.Session, path string, at Position) Location {
	listed, _, _ := sess.Located(path)
	return Location{Path: path, Origin: listed.Origin, Rel: listed.Rel, Range: Range{Start: at, End: at}}
}

// kinds is what a command is, in the dumps' words: effect, trigger, event target, joined.
func kinds(command *schema.Command) string {
	var names []string
	for _, kind := range []struct {
		bit  schema.Kinds
		name string
	}{{schema.KindEffect, "effect"}, {schema.KindTrigger, "trigger"}, {schema.KindLink, "event target"}} {
		if command.Kind&kind.bit != 0 {
			names = append(names, kind.name)
		}
	}
	return strings.Join(names, " and ")
}

// need is what a declared source says of a key being required: optional, required, required when.
func need(key *schema.Key) string {
	if key == nil {
		return ""
	}
	switch required := key.Required.Value; required.Need {
	case schema.NeedOptional:
		return "optional"
	case schema.NeedAlways:
		return "required"
	case schema.NeedWhen:
		return "required when " + required.When
	}
	return ""
}

// takes is what a key takes, as declared: its block's triggers or effects, its word's type or list
// (a long one ends with how many more), and its default.
func takes(key *schema.Key) []string {
	if key == nil {
		return nil
	}
	claim, out := key.Value.Value, []string(nil)
	said := []string{
		map[schema.Block]string{schema.BlockTriggers: "triggers", schema.BlockEffects: "effects"}[key.Block.Value],
		map[schema.WordType]string{
			schema.WordNumber: "a number", schema.WordBool: "yes or no", schema.WordDate: "a date", schema.WordText: "text",
			schema.WordReference: heading(claim.Database), schema.WordLoc: "a localization key",
			schema.WordFile: "a file path", schema.WordScope: "a scope",
		}[claim.Type],
	}
	if words := claim.Words; len(words) > 0 {
		if len(words) > 6 {
			words = append(words[:6:6], fmt.Sprintf("… and %d more", len(claim.Words)-6))
		}
		said = append(said, "one of "+strings.Join(words, ", "))
	}
	if key.Default.From != schema.SourceNone {
		said = append(said, "defaults to "+key.Default.Value)
	}
	for _, text := range said {
		if text != "" {
			out = append(out, text)
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
func keys(position *schema.Key) (names []string, stated int) {
	if position == nil {
		return nil, 0
	}
	rank := map[string]int{}
	for name, key := range position.Keys {
		if strings.HasPrefix(name, "<") {
			continue
		}
		names, rank[name] = append(names, name), 1
		if key.Stated() {
			rank[name], stated = 0, stated+1
		}
	}
	slices.SortFunc(names, func(first, second string) int {
		return cmp.Or(rank[first]-rank[second], position.Keys[second].Seen.In-position.Keys[first].Seen.In,
			strings.Compare(first, second))
	})
	return names, stated
}

// date writes a date out (January 1st, 8082 AD; year -221 is 221 BC), and in the mod's calendar
// where one of its eras applies (82.1.1 A.C.).
// TODO: What about custom months in addition to years?
func date(text string, calendar session.Calendar) string {
	year, month, day, _, ok := jomini.ParseDate(strings.Trim(text, `"`))
	if !ok || month < 1 || month > 12 {
		return ""
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	out := fmt.Sprintf("%s %s, %d AD", months[month], ordinal(day), year)
	if year < 0 {
		out = fmt.Sprintf("%s %s, %d BC", months[month], ordinal(day), -year)
	}
	if inEra, era, ok := calendar.Read(year); ok {
		out += fmt.Sprintf("; %d.%d.%d %s in this mod's calendar", inEra, month, day, era)
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

// colour is the #rrggbb of the rgb, hsv or hex block a statement writes, for a swatch; "" for none.
func colour(assign *jomini.Assignment) string {
	tagged, isTagged := assign.Value.(*jomini.TaggedBlock)
	if !isTagged {
		return ""
	}
	var parts []float64
	for _, statement := range tagged.Block.Statements {
		if list, isList := statement.(*jomini.ValueStmt); isList {
			if scalar, isScalar := list.Value.(*jomini.Scalar); isScalar {
				part, _ := strconv.ParseFloat(scalar.Text, 64)
				parts = append(parts, part)
			}
		}
	}
	switch tag := strings.ToLower(tagged.Tag.Text); {
	case tag == "rgb" && len(parts) >= 3:
		return hex(parts[0], parts[1], parts[2])
	case (tag == "hsv" || tag == "hsv360") && len(parts) >= 3:
		hue, saturation, value := parts[0], parts[1], parts[2]
		if tag == "hsv360" {
			hue, saturation, value = hue/360, saturation/100, value/100
		}
		red, green, blue := hsv(hue, saturation, value)
		return hex(red*255, green*255, blue*255)
	case tag == "hex" && len(tagged.Block.Statements) == 1:
		if list, isList := tagged.Block.Statements[0].(*jomini.ValueStmt); isList {
			if scalar, isScalar := list.Value.(*jomini.Scalar); isScalar {
				if digits := strings.TrimPrefix(strings.ToLower(scalar.Text), "0x"); len(digits) >= 6 {
					return "#" + digits[:6]
				}
			}
		}
	}
	return ""
}

// hex spells a colour whose parts are 0 to 255, or 0 to 1 where every part is at most 1.
func hex(red, green, blue float64) string {
	if red <= 1 && green <= 1 && blue <= 1 {
		red, green, blue = red*255, green*255, blue*255
	}
	return fmt.Sprintf("#%02x%02x%02x", int(red+0.5), int(green+0.5), int(blue+0.5))
}

// hsv turns hue, saturation and value, each 0 to 1, into red, green and blue, each 0 to 1.
func hsv(hue, saturation, value float64) (float64, float64, float64) {
	// the hue's sixth of the colour wheel, how far into it, and the three levels a channel takes
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
