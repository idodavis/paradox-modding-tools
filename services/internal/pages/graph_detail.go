// graph_detail.go is one definition opened: where it is, each key at the top of its block, its options, what it
// fires and what fires it, and what its block names.

package pages

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Roots is a root search's hits, grouped by database, and how many of each database the query holds.
type Roots struct {
	Hits      []Node          `json:"hits"`
	Databases []DatabaseCount `json:"databases"`
}

// DatabaseCount is one database and how many roots of it a search holds.
type DatabaseCount struct {
	Database string `json:"database"`
	Count    int    `json:"count"`
}

// Detail is one definition, opened.
type Detail struct {
	Kind     string           `json:"kind"`
	Name     string           `json:"name"`
	Line     int              `json:"line"`
	At       inventory.File   `json:"at"`
	Lines    []string         `json:"lines"` // the definition as its file writes it
	Sections []Section        `json:"sections"`
	Options  []Option         `json:"options"`
	Fires    []workspace.Fire `json:"fires"`   // as the graph draws them: events and on_actions, through scripted effects
	FiredBy  []workspace.Fire `json:"firedBy"` // the same, into it
	Refs     []RefGroup       `json:"refs"`    // what the block names, by database in the order first written
	Reason   string           `json:"reason,omitempty"`
}

// Section is one key at the top of the definition's block: its value as written (a block as its statements on one
// line, each inner block as { … }), and the text a one-word value has as a loc key.
type Section struct {
	Key      string     `json:"key"`
	Value    string     `json:"value,omitempty"`
	Line     int        `json:"line"`
	When     string     `json:"when,omitempty"`     // a block's own trigger, on one line: it applies only then
	Parts    []TextPart `json:"parts,omitempty"`    // a text's parts in order: a loc key, or a dynamic description's
	Portrait string     `json:"portrait,omitempty"` // a portrait's character
}

// TextPart is one part of a text, read in order: one of its versions shows, the first whose trigger holds, or one
// of them at random.
type TextPart struct {
	Random   bool      `json:"random,omitempty"`
	Versions []Version `json:"versions"`
}

// Version is one text a part can show: its trigger on one line (none: it always can), its loc key and that key's
// text, at its line.
type Version struct {
	When string `json:"when,omitempty"`
	Key  string `json:"key"`
	Text string `json:"text,omitempty"`
	Line int    `json:"line"`
}

// Option is one option block of an event: its trigger on one line, every name it is written with (the first whose
// trigger holds shows) and its flags.
type Option struct {
	Line  int       `json:"line"`
	When  string    `json:"when,omitempty"`
	Names []Version `json:"names,omitempty"`
	Flags []string  `json:"flags,omitempty"`
}

// RefGroup is the references of one database written in a block; loc keys are database localization, files file.
type RefGroup struct {
	Database string  `json:"database"`
	Kind     string  `json:"kind"` // what one of its database is called (game.KindName)
	Refs     []Cited `json:"refs"`
}

// Cited is one name a block writes, and where.
type Cited struct {
	Name string         `json:"name"`
	Line int            `json:"line"`
	Col  int            `json:"col"`
	At   inventory.File `json:"at"`
}

// maxRootsPerDatabase is how many roots of one database a root search draws.
const maxRootsPerDatabase = 8

// EventDetail opens one definition by name: the one the game keeps.
func EventDetail(ws *workspace.Session, id string) *Detail {
	def := newGrapher(ws, Params{}).def(id)
	if def == (index.At{}) {
		return &Detail{Reason: "Nothing defines " + id + "."}
	}
	file, occurrence := def.File(), def.Occurrence()
	detail := &Detail{
		Kind: game.KindName(occurrence.Database), Name: occurrence.ID, Line: int(occurrence.Line), At: file,
		Fires: ws.Fires(def), FiredBy: ws.FiredBy(def, true),
	}
	parsed, statement := ws.Statement(def)
	block := jomini.ChildBlock(statement)
	if block == nil {
		return detail
	}
	detail.Lines = spanLines(parsed, statement.NodeRange(), 0)
	detail.Sections, detail.Options = sections(ws, parsed, block)
	detail.Refs = references(ws, def, file)
	return detail
}

// sections is each key at the top of the block, read as the page shows it, and the option blocks among them.
func sections(ws *workspace.Session, parsed *jomini.Result, block *jomini.Block) ([]Section, []Option) {
	text := eventText{ws: ws, parsed: parsed}
	var shown []Section
	var options []Option
	for _, field := range blockFieldsOf(block) {
		section, option := text.section(field)
		shown = append(shown, section)
		if option != nil {
			options = append(options, *option)
		}
	}
	return shown, options
}

// section is one key of the block as the page shows it: a one-word value with its loc text and portrait, a block
// with its trigger, its parts and its statements on one line; an option block also as an Option.
func (text eventText) section(field *jomini.Assignment) (Section, *Option) {
	section := Section{Key: field.Key.Text, Line: text.parsed.Lines().PositionAt(field.Key.Range.Start).Line}
	portrait := slices.ContainsFunc(game.EventPortraits.Suffixes, func(suffix string) bool {
		return strings.HasSuffix(section.Key, suffix)
	})
	if scalar, isScalar := field.Value.(*jomini.Scalar); isScalar {
		section.Value = scalar.Text
		if _, known := text.ws.Loc(scalar.Text, ""); known {
			section.Parts = []TextPart{{Versions: []Version{text.version(scalar, "")}}}
		}
		if portrait {
			section.Portrait = scalar.Text
		}
		return section, nil
	}
	inner := jomini.BlockOf(field.Value)
	if inner == nil {
		return section, nil
	}
	when, trigger := text.trigger(inner)
	section.When = when
	statements := slices.DeleteFunc(slices.Clone(inner.Statements), func(statement jomini.Statement) bool {
		return statement == trigger
	})
	section.Value = oneLine(text.parsed, statements)
	section.Parts = text.parts(inner)
	if character := text.scalar(inner, game.EventPortraits.Character); portrait && character != nil {
		section.Portrait = character.Text
	}
	if section.Key == "option" {
		option := text.option(inner, section.Line)
		return section, &option
	}
	return section, nil
}

// references is what the block names, grouped by database in the order first written: a call by the database that
// defines what it calls, and one nothing defines is left out.
func references(ws *workspace.Session, def index.At, file inventory.File) []RefGroup {
	var groups []RefGroup
	for _, ref := range index.RefsInside(def) {
		occurrence := ref.Occurrence()
		database := occurrence.Database
		if database == schema.Parameter {
			continue
		}
		if occurrence.Caller != "" {
			called, known := index.Resolve(ws.Install.Schema, ws.Indexes(), occurrence, file.Path)
			if !known {
				continue
			}
			database = called[0].Occurrence().Database
		}
		i := slices.IndexFunc(groups, func(group RefGroup) bool { return group.Database == database })
		if i < 0 {
			groups = append(groups, RefGroup{Database: database, Kind: game.KindName(database)})
			i = len(groups) - 1
		}
		groups[i].Refs = append(groups[i].Refs, Cited{Name: occurrence.ID, Line: int(occurrence.Line), Col: int(occurrence.Col), At: file})
	}
	return groups
}

// SearchRoots is the fired events and on_actions, and what fires one but a scripted entry, whose id holds the
// query: ids that start with it first, 8 of each database.
func SearchRoots(ws *workspace.Session, query string) *Roots {
	query = strings.ToLower(query)
	fired, firing := firedNames(ws)
	databaseOf := map[string]string{} // by name
	for name, database := range fired {
		if strings.Contains(strings.ToLower(name), query) && len(index.Lookup(ws.Indexes(), schema.Defines, database, name)) > 0 {
			databaseOf[name] = database
		}
	}
	// a name of several databases takes the first in sorted order, so no map order decides
	for _, roots := range firing {
		for _, root := range roots {
			if strings.Contains(strings.ToLower(root.Name), query) && (databaseOf[root.Name] == "" || root.Database < databaseOf[root.Name]) {
				databaseOf[root.Name] = root.Database
			}
		}
	}
	byDatabase := map[string][]string{}
	for name, database := range databaseOf {
		byDatabase[database] = append(byDatabase[database], name)
	}
	out := &Roots{}
	for database, names := range byDatabase {
		out.Databases = append(out.Databases, DatabaseCount{Database: database, Count: len(names)})
	}
	slices.SortFunc(out.Databases, func(first, second DatabaseCount) int {
		return cmp.Or(cmp.Compare(second.Count, first.Count), strings.Compare(first.Database, second.Database))
	})
	drawn := newGrapher(ws, Params{})
	for _, count := range out.Databases {
		names := byDatabase[count.Database]
		slices.SortFunc(names, func(first, second string) int {
			return cmp.Or(
				cmp.Compare(min(strings.Index(strings.ToLower(first), query), 1), min(strings.Index(strings.ToLower(second), query), 1)),
				strings.Compare(first, second),
			)
		})
		for _, id := range names[:min(maxRootsPerDatabase, len(names))] {
			drawn.node(id, false)
		}
	}
	position := func(node Node) int {
		return slices.IndexFunc(out.Databases, func(count DatabaseCount) bool { return count.Database == node.Database })
	}
	slices.SortStableFunc(drawn.out.Nodes, func(first, second Node) int { return cmp.Compare(position(first), position(second)) })
	out.Hits = drawn.out.Nodes
	return out
}

// firedRoots is what the loaded files fire: each name fired with its database, and for each definition holding a
// reference that fires or runs one the fires it stands for.
type firedRoots struct {
	ws     *workspace.Session
	fired  map[string]string
	firing map[index.At][]workspace.Fire
}

// firedNames is, in the files the game loads, what is fired (by database) and what fires it: a scripted entry by
// what fires through it (FiredBy), another definition by itself.
func firedNames(ws *workspace.Session) (fired map[string]string, firing map[index.At][]workspace.Fire) {
	roots := &firedRoots{ws: ws, fired: map[string]string{}, firing: map[index.At][]workspace.Fire{}}
	indexes := ws.Indexes()
	for i := range indexes {
		for _, file := range indexes[i].Files {
			if _, loaded := index.Loaded(indexes, file.Path); loaded {
				roots.read(indexes[i].InFile(file.Path))
			}
		}
	}
	return roots.fired, roots.firing
}

// read takes in the references of one loaded file that fire something.
func (roots *firedRoots) read(occurrences []index.At) {
	s := roots.ws.Install.Schema
	for _, ref := range occurrences {
		occurrence := ref.Occurrence()
		if occurrence.Role != schema.Refers || !index.Fires(s, occurrence) {
			continue
		}
		roots.fired[occurrence.ID] = occurrence.Database
		caller, inEntry := ref.Entry()
		if _, known := roots.firing[caller]; !inEntry || known {
			continue
		}
		calling := caller.Occurrence()
		if index.Scripted(s, calling) {
			roots.firing[caller] = roots.ws.FiredBy(caller, false)
		} else {
			roots.firing[caller] = []workspace.Fire{{Name: calling.ID, Database: calling.Database}}
		}
	}
}
