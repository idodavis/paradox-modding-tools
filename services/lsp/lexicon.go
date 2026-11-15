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
	// Version is that patch's version. Both are "" for a key the range did
	// not change, and for a lexicon built with no range.
	Status  schema.Status `json:"status"`
	Version string        `json:"version"`

	// Defined is where the game's copy of a scripted effect or trigger is. It
	// is nil for what the dumps declare.
	Defined *Site `json:"defined,omitempty"`
}

// beforeAll is a version older than any the game has, so a range that starts
// there holds every patch.
const beforeAll = "0"

// ListClasses returns the lexicon's classes in the order its tabs show them.
func (s *Service) ListClasses() []Class { return Classes }

// BuildLexicon returns the rows of a class, sorted by name, scripted effects
// and triggers included. With a start and an end version, each row also says
// how the patches after the start, up to the end, last changed its key, and
// there is a row for each key they removed that the game no longer has. It
// returns an error if no workspace is open or the class is unknown.
func (s *Service) BuildLexicon(id, start, end string) ([]LexiconRow, error) {
	ws, class, err := s.loadClass(id)
	if err != nil {
		return nil, err
	}

	database := class.resolveDatabase(ws.Schema)
	rows := listRows(ws, ws.Mods.Load(), class, database)
	rows = markChanges(rows, class, database, ws.Patches.Load().ListRange(start, end))
	slices.SortFunc(rows, func(first, second LexiconRow) int {
		return cmp.Or(strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name)), strings.Compare(first.Name, second.Name))
	})

	return rows, nil
}

// DescribeKey returns the card of one key of a lexicon class: what hover says
// of the key, the patches that changed it, and where the mods use it. A key
// the game no longer has gets a card only if a patch removed it. It returns an
// error if no workspace is open, the class is unknown, or the key is neither
// in the game nor in a patch.
func (s *Service) DescribeKey(id, key string) (Card, error) {
	ws, class, err := s.loadClass(id)
	if err != nil {
		return Card{}, err
	}

	database, mods := class.resolveDatabase(ws.Schema), ws.Mods.Load()
	card := Card{Title: key, History: listHistory(ws.Patches.Load(), ws.Schema.Version, database, key), Stale: ws.Schema.Dumps.Stale}
	switch {
	case class.Functions:
		typeName, functionName, _ := strings.Cut(key, ".")
		if function, ok := ws.Schema.DataTypes[typeName].Functions[functionName]; ok {
			describeFunctionFacts(&card, function)
		}

	default:
		if definition := ws.Schema.LookupDefinition(database, key); definition != nil {
			describeDefinition(&card, class, definition)
			if isCommand(database) && definition.Target != "" {
				card.add(rowTarget, describeScope(definition.Target))
			}
		}
	}

	if card.What == "" && len(card.History) == 0 {
		return Card{}, fmt.Errorf("%q is not a key of the %s lexicon", key, class.ID)
	}

	if card.What == "" {
		// a removed key has the entry its last patch took away
		card.What, card.Blurb = class.ID, card.History[len(card.History)-1].DumpChanges[0].Blurb
	}

	if database != "" {
		card.Sites = MakeSites(ws, mods, listOwnUses(mods, database, key), nil)
	}

	return card, nil
}

// loadClass returns the open session and the lexicon class with the given id.
func (s *Service) loadClass(id string) (*workspace.Session, Class, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, Class{}, err
	}

	position := slices.IndexFunc(Classes, func(class Class) bool { return class.ID == id })
	if position < 0 {
		return nil, Class{}, fmt.Errorf("%q is not a lexicon class", id)
	}

	return ws, Classes[position], nil
}

// resolveDatabase returns the database that holds the class's keys in a
// language. It is "" for the functions, and for a game with no database of
// the class.
func (c Class) resolveDatabase(language *lang.Schema) string {
	if c.OnActions {
		return language.OnActions
	}

	return c.Database
}

// listRows returns a row for each key a class lists, then a row for each
// scripted definition that acts as one of its commands.
func listRows(ws *workspace.Session, mods *index.Index, class Class, database string) []LexiconRow {
	if class.Functions {
		return listFunctions(ws.Schema, class.ID)
	}

	var rows []LexiconRow
	for _, key := range ws.Schema.ListKeys(database) {
		definition := ws.Schema.LookupDefinition(database, key)
		rows = append(rows, LexiconRow{
			Class: class.ID, Name: key, Blurb: definition.Blurb, Usage: definition.Usage, Scopes: definition.Scopes,
			Target: definition.Target,
		})
	}

	if database == "" {
		return rows
	}

	return slices.Concat(rows, listScripted(ws, mods, class.ID, database))
}

// listFunctions returns a row for each function of the data types, named
// Type.Function.
func listFunctions(language *lang.Schema, class string) []LexiconRow {
	var rows []LexiconRow
	for _, dataType := range language.DataTypes {
		for _, function := range dataType.Functions {
			rows = append(rows, LexiconRow{
				Class: class, Name: dataType.Name + "." + function.Name, Blurb: function.Blurb, Target: lang.ReturnType(function),
			})
		}
	}

	return rows
}

// listScripted returns a row for each scripted definition that acts as a
// command of a database.
func listScripted(ws *workspace.Session, mods *index.Index, class, database string) []LexiconRow {
	var rows []LexiconRow
	for _, called := range lang.Called {
		owner := ws.Schema.Databases[called]
		if owner == nil || owner.Declaration.Command() != database {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, called) {
			rows = append(rows, makeScripted(ws, mods, class, mods.ToReference(winner.Site), winner.Site))
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

// markChanges gives each row the status and version of its key's latest
// change in the patches, and adds a row for each key the patches removed that
// no row has. The added rows hold the removed entry's blurb.
func markChanges(rows []LexiconRow, class Class, database string, patches []workspace.Patch) []LexiconRow {
	latest := map[string]LexiconRow{}
	for _, patch := range patches {
		for _, change := range patch.DumpChanges {
			if change.Database == database {
				latest[change.Key] = LexiconRow{
					Class: class.ID, Name: change.Key, Blurb: change.Blurb, Status: change.Status, Version: patch.Version,
				}
			}
		}
	}

	for position, row := range rows {
		if change, ok := latest[row.Name]; ok {
			rows[position].Status, rows[position].Version = change.Status, change.Version
			delete(latest, row.Name)
		}
	}

	for _, change := range latest {
		if change.Status == schema.StatusRemoved {
			rows = append(rows, change)
		}
	}

	return rows
}

// listHistory returns the patches up to a version that changed a key of a
// database, oldest first. Each holds only that key's change.
func listHistory(patches *workspace.Patches, end, database, key string) []workspace.Patch {
	var history []workspace.Patch
	for _, patch := range patches.ListRange(beforeAll, end) {
		position := slices.IndexFunc(patch.DumpChanges, func(change schema.DumpChange) bool {
			return change.Database == database && change.Key == key
		})
		if position < 0 {
			continue
		}

		history = append(history, workspace.Patch{
			Version: patch.Version, Base: patch.Base, Local: patch.Local, DumpChanges: []schema.DumpChange{patch.DumpChanges[position]},
		})
	}

	return history
}

// listOwnUses returns the references to a key in the mods' own files, not the
// game's.
func listOwnUses(mods *index.Index, database, key string) []index.Site {
	return slices.DeleteFunc(mods.ListUses(database, key), func(site index.Site) bool {
		return mods.File(site).Origin == files.OriginVanilla
	})
}
