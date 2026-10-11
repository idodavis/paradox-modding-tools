// health_contests.go is each name two origins define, by a load order: a contest between mods, else one mod's
// override of another origin's.

package pages

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// contests is each name two origins define, by a load order: a contest where two mods contend for it, else one
// mod's override of another origin's; not in a database where the game repeats names, nor one no schema database
// names (a parameter, a name script makes).
func contests(ws *workspace.Session, preview []*index.Index, sources *sourceIndex) (contested, overrides []Contest) {
	for _, name := range contestedNames(ws) {
		database, id := name[0], name[1]
		defs := index.Lookup(preview, schema.Defines, database, id)
		defs = slices.DeleteFunc(defs, func(def index.At) bool {
			occurrence := def.Occurrence()
			return occurrence.Local || occurrence.IsName
		})
		origins := map[string]bool{}
		for _, def := range defs {
			origins[def.File().Origin] = true
		}
		if len(origins) < 2 {
			continue
		}
		row, betweenMods := contestRow(ws, name, defs, sources)
		if betweenMods {
			contested = append(contested, row)
		} else {
			overrides = append(overrides, row)
		}
	}
	return contested, overrides
}

// contestedNames is each name, as its database and id, that a mod's file defines in a database a schema row names
// and the install does not repeat names in, in order.
func contestedNames(ws *workspace.Session) [][2]string {
	names := map[[2]string]bool{}
	for _, file := range ws.Workspace.Files {
		for _, at := range ws.Workspace.InFile(file.Path) {
			occurrence := at.Occurrence()
			named := occurrence.Role == schema.Defines && occurrence.ID != ""
			if named && ws.Install.Schema.Databases[occurrence.Database] != nil && !ws.Install.Index.Repeated(occurrence.Database) {
				names[[2]string{occurrence.Database, occurrence.ID}] = true
			}
		}
	}
	return slices.SortedFunc(maps.Keys(names), func(first, second [2]string) int {
		return cmp.Or(strings.Compare(first[0], second[0]), strings.Compare(first[1], second[1]))
	})
}

// contestRow is one contested name: its definitions' sites in the game's order, the winner among them, and the
// rule that decided it: the load order's, or the entry mode of a definition added to the kept one or of the kept
// one. It says whether two mods contend for the name.
func contestRow(ws *workspace.Session, name [2]string, defs []index.At, sources *sourceIndex) (row Contest, betweenMods bool) {
	winner, adds, _, betweenMods := ws.Winner(defs)
	database := name[0]
	row = Contest{
		Database: database, Kind: game.KindName(database), Name: name[1], Winner: slices.Index(defs, winner),
		FirstWins: slices.Contains(ws.Game.Layout.FirstWins, database), Rule: "LIOS",
	}
	if row.FirstWins {
		row.Rule = "FIOS"
	}
	for _, def := range defs {
		occurrence := def.Occurrence()
		site := Site{File: sources.index(def.File()), Line: int(occurrence.Line), Col: int(occurrence.Col)}
		if site.Injected = slices.Contains(adds, def); site.Injected {
			row.Rule = cmp.Or(occurrence.Mode, row.Rule)
		}
		row.Sites = append(row.Sites, site)
	}
	if row.Winner >= 0 {
		row.Rule = cmp.Or(defs[row.Winner].Occurrence().Mode, row.Rule)
	}
	return row, betweenMods
}
