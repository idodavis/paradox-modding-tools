// lexicon.go builds the rows of the script command lexicon and the card of one
// of its keys.

package lsp

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// LexiconRow is one key of the dumps: an effect, a trigger, an event target, a
// scope, a modifier, an on_action or a data function. It is also one scripted
// effect or trigger.
type LexiconRow struct {
	Class  string   `json:"class"`
	Name   string   `json:"name"`
	Blurb  string   `json:"blurb"`
	Usage  string   `json:"usage"`
	Scopes []string `json:"scopes,omitempty"`
	Target string   `json:"target,omitempty"`

	// Status is what the latest patch of the range did to the key, and
	// Version is that patch's version. Both are "" in a lexicon built with no
	// range.
	Status  schema.Status `json:"status"`
	Version string        `json:"version"`

	// Defined is where the game's copy of a scripted effect or trigger is. It
	// is nil for what the dumps declare.
	Defined *Site `json:"defined,omitempty"`
}

// beforeAll is a version older than any the game has, so a range that starts
// there holds every patch.
const beforeAll = "0"

// ListClasses returns the lexicon's classes in the order its tabs show them,
// each with the database that holds its keys in the open workspace's game.
func (s *Service) ListClasses() []Class {
	classes := slices.Clone(Classes)
	if ws, err := s.Workspace.Session(); err == nil {
		for position, class := range classes {
			classes[position] = class.resolve(ws.Schema)
		}
	}

	return classes
}

// BuildLexicon returns the rows of a class, sorted by name. With a start
// version, it returns only the keys the patches changed after it, up to the
// end version, each with its latest change. It returns an error if no
// workspace is open or the class is unknown.
func (s *Service) BuildLexicon(id, start, end string) ([]LexiconRow, error) {
	ws, class, err := s.loadClass(id)
	if err != nil {
		return nil, err
	}

	rows := listRows(ws, ws.Mods.Load(), class)
	if start != "" {
		rows = listChanged(rows, class, ws.Patches.Load().ListRange(start, end))
	}

	slices.SortFunc(rows, func(first, second LexiconRow) int {
		return cmp.Or(strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name)), strings.Compare(first.Name, second.Name))
	})

	return rows, nil
}

// DescribeKey returns the card of one key of a lexicon class: what hover says
// of it, what each patch did to it, and where the mods use it. It returns an
// error if no workspace is open, the class is unknown, or the key is in
// neither the game nor a patch.
func (s *Service) DescribeKey(id, key string) (Card, error) {
	ws, class, err := s.loadClass(id)
	if err != nil {
		return Card{}, err
	}

	card := Card{Title: key, Stale: ws.Schema.Dumps.Stale}
	if class.Database == lang.DataFunctions {
		typeName, functionName, _ := strings.Cut(key, ".")
		if function, ok := ws.Schema.DataTypes[typeName].Functions[functionName]; ok {
			describeFunctionFacts(&card, function)
		}
	} else if definition := ws.Schema.LookupDefinition(class.Database, key); definition != nil {
		describeDefinition(&card, class, definition)
		if isCommand(class.Database) && definition.Target != "" {
			card.add(rowTarget, describeScope(definition.Target))
		}
	}

	// A key the game no longer has takes the entry of its last change.
	lastBlurb := ""
	for version, change := range ws.Patches.Load().ListRange(beforeAll, ws.Schema.Version).Changes(class.Database) {
		if change.Key == key {
			card.History = append(card.History, CardRow{Label: version, Text: describeChange(change)})
			lastBlurb = change.Blurb
		}
	}

	switch {
	case card.What == "" && len(card.History) == 0:
		return Card{}, fmt.Errorf("%q is not a key of the %s lexicon", key, class.ID)
	case card.What == "":
		card.What, card.Blurb = class.ID, lastBlurb
	}

	if mods := ws.Mods.Load(); class.Database != "" {
		card.Sites = MakeSites(ws, mods, listOwnUses(mods, class.Database, key), nil)
	}

	return card, nil
}

// describeChange says what a patch did to a key of the dumps.
func describeChange(change schema.KeyChange) string {
	switch change.Status {
	case schema.StatusAdded:
		return "Added."
	case schema.StatusRemoved:
		return "Removed."
	}

	return "Changed: " + strings.Join(change.Differences, ", ") + "."
}

// loadClass returns the open session and the lexicon class with the given id,
// with its database in the session's game.
func (s *Service) loadClass(id string) (*workspace.Session, Class, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, Class{}, err
	}

	position := slices.IndexFunc(Classes, func(class Class) bool { return class.ID == id })
	if position < 0 {
		return nil, Class{}, fmt.Errorf("%q is not a lexicon class", id)
	}

	return ws, Classes[position].resolve(ws.Schema), nil
}

// resolve returns the class with the database that holds its keys in a
// language. The on_actions' is the language's own, "" for a game with none.
func (c Class) resolve(language *lang.Schema) Class {
	if c.OnActions {
		c.Database = language.OnActions
	}

	return c
}

// listRows returns a row for each key a class lists, then a row for each
// scripted definition that acts as one of its commands.
func listRows(ws *workspace.Session, mods *index.Index, class Class) []LexiconRow {
	var rows []LexiconRow
	if class.Database == lang.DataFunctions {
		for _, dataType := range ws.Schema.DataTypes {
			for _, function := range dataType.Functions {
				rows = append(rows, LexiconRow{
					Class: class.ID, Name: dataType.Name + "." + function.Name, Blurb: function.Blurb,
					Target: lang.ReturnType(function),
				})
			}
		}

		return rows
	}

	for _, key := range ws.Schema.ListKeys(class.Database) {
		definition := ws.Schema.LookupDefinition(class.Database, key)
		rows = append(rows, LexiconRow{
			Class: class.ID, Name: key, Blurb: definition.Blurb, Usage: definition.Usage, Scopes: definition.Scopes,
			Target: definition.Target,
		})
	}

	if class.Database == "" {
		return rows
	}

	return slices.Concat(rows, listScripted(ws, mods, class))
}

// listScripted returns a row for each scripted definition that acts as a
// command of a class's database.
func listScripted(ws *workspace.Session, mods *index.Index, class Class) []LexiconRow {
	var rows []LexiconRow
	for _, called := range lang.Called {
		owner := ws.Schema.Databases[called]
		if owner == nil || owner.Declaration.Command() != class.Database {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, called) {
			rows = append(rows, makeScripted(ws, mods, class.ID, mods.ToReference(winner.Site), winner.Site))
		}
	}

	return rows
}

// makeScripted returns the row of a scripted definition. Its usage is "" when
// the definition reads no parameter.
func makeScripted(ws *workspace.Session, mods *index.Index, class string, reference lang.Reference, kept index.Site) LexiconRow {
	usage := ""
	if tree, statement, err := ws.FindStatement(mods, kept); err == nil {
		var parameters []string
		for _, key := range slices.Sorted(maps.Keys(lang.ListParameters(tree, statement).Children)) {
			if !strings.HasPrefix(key, "<") {
				parameters = append(parameters, key+" = …")
			}
		}

		if len(parameters) > 0 {
			usage = reference.Name + " = { " + strings.Join(parameters, " ") + " }"
		}
	}

	defined := MakeSite(ws, mods, kept)

	return LexiconRow{Class: class, Name: reference.Name, Usage: usage, Defined: &defined}
}

// listChanged returns the rows of the keys the patches changed, each with its
// latest change. A key the game no longer has gets a row with that change's
// blurb.
func listChanged(rows []LexiconRow, class Class, patches patch.Patches) []LexiconRow {
	latest := map[string]LexiconRow{}
	for version, change := range patches.Changes(class.Database) {
		latest[change.Key] = LexiconRow{
			Class: class.ID, Name: change.Key, Blurb: change.Blurb, Status: change.Status, Version: version,
		}
	}

	var changed []LexiconRow
	for _, row := range rows {
		if change, ok := latest[row.Name]; ok {
			row.Status, row.Version = change.Status, change.Version
			changed = append(changed, row)
			delete(latest, row.Name)
		}
	}

	return slices.AppendSeq(changed, maps.Values(latest))
}

// listOwnUses returns the references to a key in the mods' own files, not the
// game's.
func listOwnUses(mods *index.Index, database, key string) []index.Site {
	return slices.DeleteFunc(mods.ListUses(database, key), func(site index.Site) bool {
		return mods.File(site).Origin == files.OriginVanilla
	})
}
