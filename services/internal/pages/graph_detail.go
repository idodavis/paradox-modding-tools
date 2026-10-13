// graph_detail.go is one definition opened: where it is, each key at the top of its block as the page shows it, its
// options, what it fires and what fires it, and what its block names.

package pages

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

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

// eventText is the parts of an event's block the page shows as text: a trigger, a description's versions,
// an option's names; each key's text is its localization key's entry in the workspace's language.
type eventText struct {
	ws     *workspace.Session
	parsed *jomini.Result
}

// EventDetail opens one definition by name: the one the game keeps. It takes the session's read lock.
func EventDetail(ws *workspace.Session, id string) *Detail {
	ws.RLock()
	defer ws.RUnlock()
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
	detail.Sections, detail.Options = eventText{ws: ws, parsed: parsed}.sections(block)
	detail.Refs = references(ws, def, file)
	return detail
}

// spanLines is the source lines a span covers, each without its line end; with a limit, the first limit lines and a
// "…" where the span runs on.
func spanLines(parsed *jomini.Result, span jomini.Range, limit int) []string {
	lines := parsed.Lines()
	first := lines.PositionAt(span.Start).Line
	last := lines.PositionAt(max(span.End-1, span.Start)).Line
	var out []string
	for line := first; line <= last; line++ {
		if limit > 0 && len(out) == limit {
			return append(out, "…")
		}
		start, end := lines.OffsetAt(line, 0), len(parsed.Src)
		if line+1 < lines.LineCount() {
			end = lines.OffsetAt(line+1, 0)
		}
		out = append(out, strings.TrimRight(parsed.Src[start:end], "\r\n"))
	}
	return out
}

// sections is each key at the top of the block as the page shows it (a one-word value with its loc text and
// portrait; a block with its trigger, its parts and its statements on one line), and the option blocks among them.
func (text eventText) sections(block *jomini.Block) ([]Section, []Option) {
	var shown []Section
	var options []Option
	for _, field := range jomini.Fields(block) {
		section := Section{Key: field.Key.Text, Line: text.parsed.Lines().PositionAt(field.Key.Range.Start).Line}
		portrait := slices.ContainsFunc(game.EventPortraits.Suffixes, func(suffix string) bool {
			return strings.HasSuffix(section.Key, suffix)
		})
		scalar, isScalar := field.Value.(*jomini.Scalar)
		inner := jomini.BlockOf(field.Value)
		switch {
		case isScalar:
			section.Value = scalar.Text
			if _, known := text.ws.Loc(scalar.Text, ""); known {
				section.Parts = []TextPart{{Versions: []Version{text.version(scalar, "")}}}
			}
			if portrait {
				section.Portrait = scalar.Text
			}
		case inner != nil:
			when, trigger := text.trigger(inner)
			section.When = when
			statements := slices.DeleteFunc(slices.Clone(inner.Statements), func(statement jomini.Statement) bool {
				return statement == trigger
			})
			section.Value = oneLine(text.parsed, statements)
			section.Parts = text.parts(inner)
			if character := jomini.ScalarOf(inner, game.EventPortraits.Character); portrait && character != nil {
				section.Portrait = character.Text
			}
			if section.Key == "option" {
				options = append(options, text.option(inner, section.Line))
			}
		}
		shown = append(shown, section)
	}
	return shown, options
}

// version is one text a key shows: the key at its line, and its text in the workspace's language where it has one.
func (text eventText) version(scalar *jomini.Scalar, when string) Version {
	version := Version{When: when, Key: scalar.Text, Line: text.parsed.Lines().PositionAt(scalar.Range.Start).Line}
	if entry, known := text.ws.Loc(scalar.Text, ""); known {
		version.Text = entry.Text()
	}
	return version
}

// trigger is the block's own trigger on one line, and its statement: the first trigger key whose value is a block.
func (text eventText) trigger(block *jomini.Block) (string, jomini.Statement) {
	for _, statement := range block.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign || assign.Key.Text != game.DynamicDescription.Trigger {
			continue
		}
		if inner := jomini.BlockOf(assign.Value); inner != nil {
			return text.parsed.Flat(jomini.Range{Start: inner.OpenBrace + 1, End: inner.CloseBrace}), assign
		}
	}
	return "", nil
}

// oneLine is a block's statements on one line: each as its source folded to single spaces, an inner block as its
// head and { … }.
func oneLine(parsed *jomini.Result, statements []jomini.Statement) string {
	parts := []string{"{"}
	for _, statement := range statements {
		span := statement.NodeRange()
		if inner := jomini.ChildBlock(statement); inner != nil {
			parts = append(parts, parsed.Flat(jomini.Range{Start: span.Start, End: inner.OpenBrace})+" { … }")
			continue
		}
		parts = append(parts, parsed.Flat(span))
	}
	return strings.Join(append(parts, "}"), " ")
}

// parts is a dynamic description as the game reads it, each statement a part in order: a leaf's block is read in
// its place, a leaf or a triggered leaf is one part, and a first-valid or random-valid block is one part of the
// versions of each statement in it.
func (text eventText) parts(block *jomini.Block) []TextPart {
	shape := game.DynamicDescription
	var parts []TextPart
	for _, statement := range block.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			continue
		}
		key, inner := assign.Key.Text, jomini.BlockOf(assign.Value)
		switch {
		case key == shape.Leaf && inner != nil:
			parts = append(parts, text.parts(inner)...)
		case key == shape.Leaf || key == shape.Triggered:
			parts = append(parts, TextPart{Versions: text.versions(statement)})
		case slices.Contains(shape.Markers, key) && inner != nil:
			part := TextPart{Random: key == shape.Random}
			for _, entry := range inner.Statements {
				part.Versions = append(part.Versions, text.versions(entry)...)
			}
			parts = append(parts, part)
		}
	}
	return parts
}

// versions is the texts one statement of a description can show: a leaf's one, or a triggered leaf's with its
// trigger; none for any other statement.
func (text eventText) versions(statement jomini.Statement) []Version {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return nil
	}
	shape := game.DynamicDescription
	switch assign.Key.Text {
	case shape.Leaf:
		if scalar, isScalar := assign.Value.(*jomini.Scalar); isScalar {
			return []Version{text.version(scalar, "")}
		}
	case shape.Triggered:
		if block := jomini.BlockOf(assign.Value); block != nil {
			if scalar := jomini.ScalarOf(block, shape.Leaf); scalar != nil {
				when, _ := text.trigger(block)
				return []Version{text.version(scalar, when)}
			}
		}
	}
	return nil
}

// option is one option block: its trigger, every name it is written with (a plain one, or a block holding the text
// and its trigger) and the flags it is marked with.
func (text eventText) option(block *jomini.Block, line int) Option {
	option := Option{Line: line}
	option.When, _ = text.trigger(block)
	for _, field := range jomini.Fields(block) {
		switch scalar, isScalar := field.Value.(*jomini.Scalar); {
		case field.Key.Text == "name" && isScalar:
			option.Names = append(option.Names, text.version(scalar, ""))
		case field.Key.Text == "name":
			if name, hasText := text.blockName(field); hasText {
				option.Names = append(option.Names, name)
			}
		case isScalar && scalar.Text == game.Bools[0] && slices.Contains(game.OptionFlags, field.Key.Text):
			option.Flags = append(option.Flags, field.Key.Text)
		}
	}
	return option
}

// blockName is the name an option writes as a block: the text key's word with the block's own trigger.
func (text eventText) blockName(field *jomini.Assignment) (Version, bool) {
	inner := jomini.BlockOf(field.Value)
	if inner == nil {
		return Version{}, false
	}
	label := jomini.ScalarOf(inner, "text")
	if label == nil {
		return Version{}, false
	}
	when, _ := text.trigger(inner)
	return text.version(label, when), true
}

// references is what the block names, grouped by database in the order first written: a call by the database that
// defines what it calls, and one nothing defines is left out, as are a parameter and a data function.
func references(ws *workspace.Session, def index.At, file inventory.File) []RefGroup {
	var groups []RefGroup
	for _, ref := range index.RefsInside(def) {
		occurrence := ref.Occurrence()
		database := occurrence.Database
		if database == schema.Parameter || database == game.DataFunction {
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
