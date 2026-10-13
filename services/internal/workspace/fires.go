// fires.go is what fires what: the events and on_actions a definition fires, through the scripted entries it runs,
// and the definitions that fire one, each placed where it is written.

package workspace

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
)

// Fire is the definition at the far end of a fire, its database, and the scripted entries between; where it starts
// in the definition (the fire, or the scripted call that leads to it) and the definition's block it sits in there
// (option a, immediate); and the statement that fires, as written, with its other fields (trigger_event; days = 30).
type Fire struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	Kind     string `json:"kind"` // what one of its database is called (game.KindName)
	Via      string `json:"via"`
	Through  int    `json:"through,omitempty"` // how many scripted entries Via names
	Path     string `json:"path"`
	Rel      string `json:"rel"`
	Origin   string `json:"origin"`
	Line     int32  `json:"line"` // 0-based
	Block    string `json:"block,omitempty"`
	Command  string `json:"command,omitempty"`
	Args     string `json:"args,omitempty"`
}

// fireRef is a reference inside a definition: the definition and the reference, as the index recorded them.
type fireRef struct {
	def, ref index.At
}

// fireWalk is one walk over what fires what, out of or into one definition: the entries entered once, the files
// parsed once on the way, and whether each fire is placed where it is written.
type fireWalk struct {
	ws     *Session
	from   index.At
	place  bool
	seen   map[index.At]bool
	parsed Parses
	out    []Fire
}

// Fires is what a definition fires, in name order: events and on_actions only (index.Fires). A scripted entry it
// runs (index.Runs) is followed to what it fires, and an entry already met on the way is not entered again.
func (ws *Session) Fires(from index.At) []Fire {
	walk := ws.newFireWalk(from)
	walk.follow(from, "", index.At{})
	return byName(walk.out)
}

// FiredBy is Fires turned around, in name order: each definition holding a reference that fires or runs this one,
// a scripted entry followed to what runs it, with the chain as Fires writes it; with place, each placed where it
// starts in its definition, with the statement that fires this one (which parses its file).
func (ws *Session) FiredBy(target index.At, place bool) []Fire {
	walk := ws.newFireWalk(target)
	walk.place = place
	return byName(walk.callers(target, nil))
}

// newFireWalk is a walk out of or into from, with nothing entered and nothing parsed yet.
func (ws *Session) newFireWalk(from index.At) *fireWalk {
	return &fireWalk{ws: ws, from: from, seen: map[index.At]bool{from: true}, parsed: Parses{}}
}

// follow reads the references written in one definition; via is the entries entered to reach it, start the
// reference in the walk's origin that leads there (the zero At: the reference itself). A scripted entry a reference
// runs is followed once.
func (walk *fireWalk) follow(def index.At, via string, start index.At) {
	s, indexes := walk.ws.Install.Schema, walk.ws.Indexes()
	for _, ref := range index.RefsInside(def) {
		first, occurrence := cmp.Or(start, ref), ref.Occurrence()
		switch {
		case index.Fires(s, occurrence):
			fire := newFire(occurrence.ID, occurrence.Database, via)
			walk.out = append(walk.out, walk.locateFire(fire, fireRef{walk.from, first}, fireRef{def, ref}))
		case index.Runs(s, occurrence):
			called, _ := index.Resolve(s, indexes, occurrence, ref.File().Path)
			for _, target := range called {
				if !walk.seen[target] {
					walk.seen[target] = true
					walk.follow(target, strings.TrimPrefix(via+" → "+target.Occurrence().ID, " → "), first)
				}
			}
		}
	}
}

// callers reads the references to one definition; a local one answers only its own file's; firing is the
// statement that fires the target, met on the first step.
func (walk *fireWalk) callers(def index.At, firing *fireRef) (out []Fire) {
	s := walk.ws.Install.Schema
	target := def.Occurrence()
	for _, ref := range index.RefsTo(s, walk.ws.Indexes(), target.Database, target.ID) {
		caller, _ := ref.Entry()
		calling, occurrence := caller.Occurrence(), ref.Occurrence()
		source := cmp.Or(firing, &fireRef{caller, ref})
		neitherFiresNorRuns := !index.Fires(s, occurrence) && !index.Runs(s, occurrence)
		unnamed := calling.ID == ""
		localToAnotherFile := target.Local && ref.File().Path != def.File().Path
		switch {
		case neitherFiresNorRuns || unnamed || localToAnotherFile:
		case index.Scripted(s, calling):
			out = append(out, walk.through(caller, calling.ID, source)...)
		case walk.place:
			out = append(out, walk.locateFire(newFire(calling.ID, calling.Database, ""), fireRef{caller, ref}, *source))
		default:
			out = append(out, newFire(calling.ID, calling.Database, ""))
		}
	}
	return out
}

// through is what fires a scripted entry, each fire's chain ending with the entry's name; an entry already met
// is not entered again.
func (walk *fireWalk) through(caller index.At, name string, firing *fireRef) (out []Fire) {
	if walk.seen[caller] {
		return nil
	}
	walk.seen[caller] = true
	for _, up := range walk.callers(caller, firing) {
		up.Via = strings.TrimPrefix(up.Via+" → "+name, " → ")
		up.Through = strings.Count(up.Via, " → ") + 1
		out = append(out, up)
	}
	return out
}

// locateFire is a fire placed: where origin's reference sits in its definition (its site and blocks), and what
// firing, a reference in a definition, fires with.
func (walk *fireWalk) locateFire(fire Fire, origin, firing fireRef) Fire {
	file := origin.ref.File()
	fire.Path, fire.Rel, fire.Origin, fire.Line = file.Path, file.Rel, file.Origin, origin.ref.Occurrence().Line
	fire.Block, _, _ = walk.fireSite(origin)
	_, fire.Command, fire.Args = walk.fireSite(firing)
	return fire
}

// fireSite is where a reference sits in its definition: the definition's block it is in (with its name's last
// part where it has one: option a), the statement that holds it, and that statement's other fields as written.
// A list's weight (random_events' 100 = x) is an argument, and a block's id is its statement's.
func (walk *fireWalk) fireSite(firing fireRef) (block, command, args string) {
	parsed, _, _ := walk.ws.Parsed(walk.parsed, firing.ref.File().Path)
	if parsed == nil {
		return "", "", ""
	}
	top := parsed.AssignmentAt(int(firing.def.Occurrence().Line), int(firing.def.Occurrence().Col))
	if top == nil {
		return "", "", ""
	}
	chain := statementChain(top, firing.ref.Occurrence().Range.Start)
	if len(chain) == 0 {
		return "", "", ""
	}
	leaf, last := chain[len(chain)-1], len(chain)-1
	if name := keyOf(leaf); name == "" || name == "id" || jomini.IsDigits(name) {
		if jomini.IsDigits(name) {
			args = name
		}
		last--
	}
	if last > 0 {
		block = keyOf(chain[0])
		if value := jomini.ScalarOf(jomini.ChildBlock(chain[0]), "name"); value != nil {
			block += " " + value.Text[strings.LastIndex(value.Text, ".")+1:]
		}
	}
	if last < 0 {
		return block, "", args
	}
	command = keyOf(chain[last])
	for _, field := range jomini.Fields(jomini.ChildBlock(chain[last])) {
		if jomini.Statement(field) != leaf && field.Value != nil {
			args = strings.TrimPrefix(args+", "+field.Key.Text+" = "+parsed.Flat(field.Value.NodeRange()), ", ")
		}
	}
	return block, command, args
}

// statementChain is the statements from a definition's block down to the one holding an offset, outer first;
// empty where none holds it.
func statementChain(top jomini.Statement, offset int) []jomini.Statement {
	var chain []jomini.Statement
	for block := jomini.ChildBlock(top); block != nil; {
		i := slices.IndexFunc(block.Statements, func(statement jomini.Statement) bool {
			return statement.NodeRange().Start <= offset && offset < statement.NodeRange().End
		})
		if i < 0 {
			break
		}
		chain = append(chain, block.Statements[i])
		block = jomini.ChildBlock(block.Statements[i])
	}
	return chain
}

// keyOf is an assignment's key text; "" for any other statement.
func keyOf(statement jomini.Statement) string {
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		return assign.Key.Text
	}
	return ""
}

// newFire is a fire at name through the chain via, written as Fires writes it.
func newFire(name, database, via string) Fire {
	fire := Fire{Name: name, Database: database, Kind: game.KindName(database), Via: via}
	if via != "" {
		fire.Through = strings.Count(via, " → ") + 1
	}
	return fire
}

// byName is fires in name order, each name once.
func byName(fires []Fire) []Fire {
	slices.SortStableFunc(fires, func(first, second Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(fires, func(first, second Fire) bool { return first.Name == second.Name })
}
