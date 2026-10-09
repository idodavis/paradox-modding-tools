// mentions.go decides which entry each scalar of a placed statement stands for, and whether the
// statement creates it.

package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Localization and File are the databases a loc key and a file path name; no definition carries
// them, so the index resolves them from the loc entries and the inventories. Parameter holds a
// scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined and where passed.
const (
	Localization = "localization"
	File         = "file"
	Parameter    = "parameter"
)

// scalarFacts is what every name rule asks of a word: the statement it is in, whether that statement
// is an entry's own, the entry's database, and whether the word is read as a name at all.
type scalarFacts struct {
	key      *jomini.Scalar // the statement's key; nil for a value statement
	isEntry  bool           // the statement is an entry's own
	isKey    bool           // the word is the key, not the value
	named    bool           // the word is a name: not "", not a constant, no $X$ in it
	reads    bool           // it may refer: a key in an entry, not the entry's own; a value in or at one, a name
	database *Database      // the entry's database; nil outside an entry
	claim    Expected       // the position's expected value; zero without a position
	literal  jomini.LiteralKind
}

// mentionsOf fills a reading's StoredIn and Mentions: the key's names, then the value's.
func (w *fileWalk) mentionsOf(placement *Placement) {
	assign, isAssign := placement.Statement.(*jomini.Assignment)
	command := w.nameCommand(placement, assign)
	placement.StoredIn = command.Database
	if isAssign {
		w.scalarMentions(placement, &assign.Key, w.facts(placement, assign, &assign.Key, true), command)
	}
	if scalar, isScalar := jomini.ValueOf(placement.Statement).(*jomini.Scalar); isScalar {
		w.scalarMentions(placement, scalar, w.facts(placement, assign, scalar, false), command)
	}
}

// nameCommand returns the name command a statement's value belongs to: the key's own (set_variable
// = x), else the key inside the command whose block it is in (save_scope_value_as = { name = x });
// zero for none.
func (w *fileWalk) nameCommand(placement *Placement, assign *jomini.Assignment) game.NameCommand {
	if assign == nil {
		return game.NameCommand{}
	}
	if own, found := game.NameCommands[strings.ToLower(assign.Key.Text)]; found {
		return own
	}
	return game.NameCommands[placement.BlockContext.Command+"/"+assign.Key.Text]
}

// facts gathers what the rules ask of one word of a reading.
func (w *fileWalk) facts(placement *Placement, assign *jomini.Assignment, scalar *jomini.Scalar, isKey bool) scalarFacts {
	f := scalarFacts{isKey: isKey, literal: jomini.Literal(scalar.Text)}
	if assign != nil {
		f.key = &assign.Key
	}
	if placement.Entry >= 0 {
		f.isEntry = w.entries[placement.Entry].Statement == placement.Statement
		f.database = w.s.Databases[w.entries[placement.Entry].Database]
	}
	_, constant := jomini.Constant(scalar.Text)
	f.named = scalar.Text != "" && !constant && len(jomini.Interpolations(scalar.Text)) == 0
	f.reads = isKey && !f.isEntry && placement.InEntry || !isKey && (f.isEntry || placement.InEntry) && f.named
	if placement.Position != nil {
		f.claim = placement.Position.Value.Value
	}
	return f
}

// scalarMentions adds what one word names: its link parts, then the key rules or the value rule, then
// its parameters.
func (w *fileWalk) scalarMentions(placement *Placement, scalar *jomini.Scalar, f scalarFacts, command game.NameCommand) {
	linked := w.prefixReferences(placement, scalar, f)
	switch {
	case f.isKey:
		w.keyMentions(placement, scalar, f)
	case f.reads && !linked && (!f.isEntry || f.key != nil):
		// a listed entry's word is its name, not a value
		w.valueMention(placement, scalar, f, command)
	}
	w.parameterDefinitions(placement, scalar, f)
}

// prefixReferences refers, for each prefix:name part of a word that a link reads, to the name in
// the prefix's database, and reports whether the word has any such part: a linked word takes no
// value rule.
func (w *fileWalk) prefixReferences(placement *Placement, scalar *jomini.Scalar, f scalarFacts) (linked bool) {
	if !f.reads {
		return false
	}
	for part := range strings.SplitSeq(scalar.Text, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !isPrefixed || w.s.Link(strings.ToLower(part)) == nil {
			continue
		}
		linked = true
		if database := w.s.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
			placement.Mentions = append(placement.Mentions, Mention{Database: database, ID: prefixed.Name, Range: scalar.Range})
		}
	}
	return linked
}

// keyMentions adds what a key names by every key rule that fits, in this order: a history entry's top
// key refers to the object it sets up; a call every_x iterates the scripted list x; a key read as a
// call names a scripted entry by its calling word; a key in a call's block names the called entry's
// parameter.
func (w *fileWalk) keyMentions(placement *Placement, scalar *jomini.Scalar, f scalarFacts) {
	if f.isEntry && f.database != nil && f.database.Row.Of != "" {
		placement.Mentions = append(placement.Mentions, Mention{Database: f.database.Row.Of, ID: w.entries[placement.Entry].ID, Range: scalar.Range})
	}
	if f.reads && placement.Listed != "" {
		placement.Mentions = append(placement.Mentions, Mention{
			Database: w.s.Lists, ID: strings.TrimPrefix(scalar.Text, placement.Listed), Range: scalar.Range,
		})
	}
	if f.reads && placement.Calls != "" && f.named {
		placement.Mentions = append(placement.Mentions, Mention{Called: placement.Calls, ID: scalar.Text, Range: scalar.Range})
	}
	if f.reads && placement.Call != "" {
		placement.Mentions = append(placement.Mentions, Mention{Database: Parameter, ID: placement.Call + ":" + scalar.Text, Range: scalar.Range})
	}
}

// valueMention adds what a value names by the first value rule that fits, in this order: history, file
// reference, made, group, read, claim, value, event. A history key names its object before anything
// else; a loc key is referred to only where the position's claim says so; vanilla types a position
// by its words, never its numbers; a word where a number is expected names a script value.
func (w *fileWalk) valueMention(placement *Placement, scalar *jomini.Scalar, f scalarFacts, command game.NameCommand) {
	n := Mention{ID: scalar.Text, Range: scalar.Range}
	fileRef := ""
	if f.key != nil {
		fileRef = game.FileTypes[w.ext].Refs[f.key.Text]
	}
	switch {
	case f.isEntry && f.database != nil && f.database.Row.Of != "":
		n.Database = f.database.Row.Of
	case fileRef != "":
		n.Database = w.ext[1:] + "." + fileRef
	case command.Makes && placement.StoredIn != "":
		n.Defines, n.Database = true, placement.StoredIn
	case f.key != nil && f.database != nil && placement.BlockContext.Position == f.database.Entry &&
		slices.Contains(f.database.Row.Names, f.key.Text):
		n.Defines, n.Named, n.Database = true, true, f.database.Name
	case placement.StoredIn != "":
		n.Database = placement.StoredIn
	case f.claim.Kind == ExpectsReference && (placement.Position.Value.Source != SourceVanilla || f.literal != jomini.LitNumber):
		n.Database = f.claim.Database
	case f.claim.Kind == ExpectsLoc:
		n.Database = Localization
	case f.claim.Kind == ExpectsFile:
		n.Database = File
	case f.literal != jomini.LitNumber && f.literal != jomini.LitDate && placement.Position != nil &&
		(f.claim.Kind == ExpectsNumber || placement.Position.Vanilla.Shapes&ShapeNumber != 0):
		n.Called = game.Blocks[game.BlockValue].Calls
	case jomini.IsEventID(scalar.Text):
		n.Database = w.events
	default:
		return
	}
	if n.Database != "" || n.Called != "" {
		placement.Mentions = append(placement.Mentions, n)
	}
}

// parameterDefinitions defines each $NAME$ an entry of a folder with Parameters writes, at its
// first use in the file.
func (w *fileWalk) parameterDefinitions(placement *Placement, scalar *jomini.Scalar, f scalarFacts) {
	if f.database == nil || !f.database.Row.Parameters {
		return
	}
	start := scalar.Range.Start
	if scalar.Quoted {
		start++
	}
	for _, span := range jomini.Interpolations(scalar.Text) {
		name := w.entries[placement.Entry].ID + ":" + span.Name
		if !w.defined[name] {
			w.defined[name] = true
			placement.Mentions = append(placement.Mentions, Mention{
				Defines: true, Database: Parameter, ID: name,
				Range: jomini.Range{Start: start + span.Start, End: start + span.End},
			})
		}
	}
}
