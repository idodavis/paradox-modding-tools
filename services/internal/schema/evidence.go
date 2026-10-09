// evidence.go records what vanilla writes at each position, and gives a position the type its words share
// where no source declares one. Vanilla's types never accuse (MayAccuse).

package schema

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// recordEvidence walks one vanilla file with its entries and records at each position how often and in what
// forms vanilla writes it. It records evidence only, never a claim.
func (s *Schema) recordEvidence(file inventory.File, parsed *jomini.Result, entries []Entry) {
	counted := map[*jomini.Block]map[*Position]bool{} // In counts a key once per block
	s.WalkFile(file, parsed, entries, Visitor{
		Create: true,
		Visit:  func(r *Reading) bool { return recordReading(r, entries, counted) },
	})
}

// recordReading records one reading of the walk and returns whether to read its block.
func recordReading(r *Reading, entries []Entry, counted map[*jomini.Block]map[*Position]bool) bool {
	// an entry's own statement records its value at the entry's position and counts nothing; a word listed as an
	// entry is written at its holder's position, so it is recorded there
	if r.Entry >= 0 && entries[r.Entry].Statement == r.Statement {
		switch statement := r.Statement.(type) {
		case *jomini.Assignment:
			recordValue(statement.Value, r.Position)
		case *jomini.ValueStmt:
			if block := jomini.ChildBlock(statement); block != nil {
				recordValue(block, r.Position)
			} else {
				recordValue(statement.Value, r.BlockContext.Position)
			}
		}
		return true
	}
	if r.Entry < 0 {
		return true // outside every entry there is no position: read on for the entries inside
	}
	if list, isList := r.Statement.(*jomini.ValueStmt); isList {
		recordValue(list.Value, r.Position)
		return true
	}
	if command := r.Command; command != nil && command.Kind&game.KindLink == 0 && r.BlockContext.Position != nil {
		forms := &r.BlockContext.Position.Evidence.Forms
		*forms |= FormCommand
		if command.Kind == game.KindTrigger {
			*forms |= FormTrigger
		}
		if command.Kind == game.KindEffect {
			*forms |= FormEffect
		}
	}
	if r.Position == nil {
		// a key whose block reads in place, a link or a date, records nothing itself
		return r.Command != nil || r.Inner.Position != nil && r.Inner.Position == r.BlockContext.Position
	}
	if !r.Leaf {
		if counted[r.Holder] == nil {
			counted[r.Holder] = map[*Position]bool{}
		}
		if !counted[r.Holder][r.Position] {
			counted[r.Holder][r.Position] = true
			r.Position.Evidence.In++
		}
	}
	recordValue(r.Statement.(*jomini.Assignment).Value, r.Position)
	return true
}

// recordValue records a written value at a position: a word's form and, unless it is a number or a date,
// the word itself, cloned so the schema never pins vanilla's text; a block is counted.
func recordValue(value jomini.Value, at *Position) {
	if at == nil {
		return
	}
	switch written := value.(type) {
	case *jomini.Scalar:
		form := formOf(written)
		at.Evidence.Forms |= form
		if form&(FormNumber|FormDate) == 0 {
			if at.Evidence.Values == nil {
				at.Evidence.Values = map[string]int{}
			}
			at.Evidence.Values[strings.Clone(written.Text)]++
		}
	case *jomini.Block, *jomini.TaggedBlock:
		at.Evidence.Blocks++
		at.Evidence.Forms |= FormBlock
	}
}

// typing holds what giving positions a type from vanilla's words needs.
type typing struct {
	s        *Schema
	names    map[string]map[string]bool // database → the names its entries have
	holding  map[string][]string        // name → the databases holding it
	locKeys  map[string]bool
	prefixes map[string]map[string]bool // link prefix → the names vanilla writes after it
}

// inferValueTypes gives each untyped position the type vanilla's words there share, then each link prefix
// the database vanilla writes names of after it. names are the entry names vanilla defines, by database.
func (s *Schema) inferValueTypes(names map[string]map[string]bool, locKeys map[string]bool) {
	t := &typing{s: s, names: names, holding: map[string][]string{}, locKeys: locKeys, prefixes: map[string]map[string]bool{}}

	// the words a database's entries write at its row's Names keys name entries too (trait groups)
	for name, database := range s.Databases {
		for _, key := range database.Row.Names {
			if child := database.Entry.Child(key); child != nil {
				if names[name] == nil {
					names[name] = map[string]bool{}
				}
				for word := range child.Evidence.Values {
					names[name][word] = true
				}
			}
		}
	}
	for database, set := range names {
		for name := range set {
			t.holding[name] = append(t.holding[name], database)
		}
	}

	visited := map[*Position]bool{}
	for name, database := range s.Databases {
		t.visit(database.Entry, isFileTypeDatabase(name))
	}
	for _, command := range s.Commands {
		t.visit(command.Args, false)
	}
	for _, entry := range s.ScriptedEntries {
		if entry.Parameters != nil && !visited[entry.Parameters] { // an entry is under each word that calls it
			visited[entry.Parameters] = true
			t.visit(entry.Parameters, false)
		}
	}
	for prefix, written := range t.prefixes {
		if database := t.one(slices.Collect(maps.Keys(written)), false); database != "" && s.Prefixes[prefix] == "" {
			s.Prefixes[prefix] = database
		}
	}
}

// isFileTypeDatabase reports whether a database name is a file type's own (gui.type).
func isFileTypeDatabase(name string) bool {
	extension, _, cut := strings.Cut(name, ".")
	_, typed := game.FileTypes["."+extension]
	return cut && typed
}

// one returns the only database that holds every one of the words, among the databases of the same kind
// (a file type's own, or not) and not a history database; "" for none or several.
func (t *typing) one(words []string, inGUI bool) (found string) {
	count := 0
	for _, database := range t.holding[words[0]] {
		if isFileTypeDatabase(database) != inGUI || t.s.Databases[database].Row.History {
			continue
		}
		if slices.ContainsFunc(words, func(word string) bool { return !t.names[database][word] }) {
			continue
		}
		found = database
		count++
	}
	if count != 1 {
		return ""
	}
	return found
}

// visit types a position and the ones below it from the words vanilla writes at each: a database the words
// all name, else a loc key when every word is one. The words after a link prefix are kept for the prefix.
func (t *typing) visit(position *Position, inGUI bool) {
	var words []string
	for word := range position.Evidence.Values {
		switch {
		case len(jomini.Interpolations(word)) > 0:
		case t.s.Link(strings.ToLower(word)) == nil:
			words = append(words, word)
		default:
			t.keepAfterPrefixes(word)
		}
	}
	if len(words) > 0 && position.Value.Source == SourceNone {
		if database := t.one(words, inGUI); database != "" {
			SetFirst(&position.Value, Expected{Type: WordReference, Database: database}, SourceVanilla)
		} else if !slices.ContainsFunc(words, func(word string) bool { return !t.locKeys[word] }) {
			SetFirst(&position.Value, Expected{Type: WordLoc}, SourceVanilla)
		}
	}
	for _, child := range position.Children {
		t.visit(child, inGUI)
	}
}

// keepAfterPrefixes keeps, for each link prefix in a chain of links (c:FRA.title), the name written after
// it, unless that name is a link itself (ai_state_value:root).
func (t *typing) keepAfterPrefixes(chain string) {
	for part := range strings.SplitSeq(chain, ".") {
		prefixed, found := jomini.SplitPrefixed(part)
		if !found || t.s.Link(strings.ToLower(part)) == nil || t.s.Link(strings.ToLower(prefixed.Name)) != nil {
			continue
		}
		prefix := strings.ToLower(prefixed.Prefix)
		if t.prefixes[prefix] == nil {
			t.prefixes[prefix] = map[string]bool{}
		}
		t.prefixes[prefix][prefixed.Name] = true
	}
}
