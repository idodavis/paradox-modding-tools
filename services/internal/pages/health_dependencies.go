// health_dependencies.go is what each mod needs from another: the names it writes that another mod's definitions
// decide, and whether that mod's definition differs from the one it hides.

package pages

import (
	"cmp"
	"slices"
	"strconv"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// dependency is one edge of the report: a mod needing another, and whether the other's definition affects it.
type dependency struct {
	from, to string
	affected bool
}

// dependencyWalk is one pass over the workspace's references: what each resolves to in its file (a name resolves
// alike in one file, where a local definition shows), which definition pairs were compared, and the rows so far.
type dependencyWalk struct {
	ws       *workspace.Session
	sources  *sourceIndex
	resolved map[[4]string][]index.At // by database, caller, id and file path
	compared map[[2]index.At]bool     // whether a definition's statement differs from the one it hides
	parses   workspace.Parses         // the files compared so far, each parsed once for the walk
	rows     map[dependency]Dependency
}

// dependencies is what each mod needs from another, from the references its loaded files write.
func dependencies(ws *workspace.Session, sources *sourceIndex) []Dependency {
	walk := &dependencyWalk{
		ws: ws, sources: sources, resolved: map[[4]string][]index.At{}, compared: map[[2]index.At]bool{},
		parses: workspace.Parses{}, rows: map[dependency]Dependency{},
	}
	for _, file := range loadedFiles(ws) {
		walk.file(file)
	}
	var out []Dependency
	for _, row := range walk.rows {
		out = append(out, row)
	}
	slices.SortFunc(out, func(first, second Dependency) int {
		return cmp.Compare(first.From+first.To+strconv.FormatBool(first.Affected), second.From+second.To+strconv.FormatBool(second.Affected))
	})
	return out
}

// file takes in each reference of a file that names an entry of a mod: its edges, one use each. A name no mod's
// file defines any longer is the other mod's removal; else the edges go to the origins of the definitions the game
// keeps.
func (walk *dependencyWalk) file(file inventory.File) {
	for _, entry := range walk.ws.Workspace.InFile(file.Path) {
		ref := entry.Occurrence()
		// a loc key, a file, a parameter or a name script makes is no mod's entry, and a call names none
		if ref.Role != schema.Refers || ref.Database != "" && walk.ws.Install.Schema.Databases[ref.Database] == nil {
			continue
		}
		defs := walk.resolve(ref, file.Path)
		var edges []dependency
		var database string
		if len(defs) == 0 {
			edges, database = walk.removedEdge(file, ref)
		} else {
			edges, database = walk.holderEdges(file, ref, defs)
		}
		for _, edge := range edges {
			row := walk.rows[edge]
			row.From, row.To, row.Affected = edge.from, edge.to, edge.affected
			row.Sites = append(row.Sites, Site{
				File: walk.sources.index(file), Line: int(ref.Line), Col: int(ref.Col), Database: database, Name: ref.ID,
				Removed: len(defs) == 0,
			})
			walk.rows[edge] = row
		}
	}
}

// resolve is a reference's definitions, kept for the next reference of the same name in the same file.
func (walk *dependencyWalk) resolve(ref schema.Occurrence, path string) []index.At {
	key := [4]string{ref.Database, ref.Caller, ref.ID, path}
	if defs, known := walk.resolved[key]; known {
		return defs
	}
	defs, _ := index.Resolve(walk.ws.Install.Schema, walk.ws.Indexes(), ref, path)
	walk.resolved[key] = defs
	return defs
}

// removedEdge is the edge to the mod whose file leaves out a name the install defines, which this file writes.
func (walk *dependencyWalk) removedEdge(file inventory.File, ref schema.Occurrence) ([]dependency, string) {
	installed, _ := index.Resolve(walk.ws.Install.Schema, []*index.Index{walk.ws.Install.Index}, ref, file.Path)
	if len(installed) == 0 {
		return nil, ref.Database
	}
	remover, _ := index.FileAt(walk.ws.Indexes(), installed[0].File().Rel)
	database := cmp.Or(ref.Database, installed[0].Occurrence().Database)
	if remover.Origin == "" || remover.Origin == file.Origin || remover.Origin == game.OriginVanilla {
		return nil, database
	}
	return []dependency{{from: file.Origin, to: remover.Origin, affected: true}}, database
}

// holderEdges is the edges to the mods whose definitions of a name decide it: the winner, which affects this file
// where it differs from the one it hides, and the definitions added to it, which always do.
func (walk *dependencyWalk) holderEdges(file inventory.File, ref schema.Occurrence, defs []index.At) ([]dependency, string) {
	winner, adds, _, _ := walk.ws.Winner(defs)
	database := cmp.Or(ref.Database, winner.Occurrence().Database)
	var edges []dependency
	for n, holder := range append([]index.At{winner}, adds...) {
		origin := holder.File().Origin
		if origin == "" || origin == file.Origin || origin == game.OriginVanilla {
			continue
		}
		edge := dependency{from: file.Origin, to: origin, affected: n > 0}
		if n == 0 {
			base, differs := walk.overridden(ref, file.Path, defs, winner)
			if base != (index.At{}) && !differs {
				continue
			}
			edge.affected = base != (index.At{})
		}
		// a mod's add to its own name, which only it defines, is part of what the mod depends on
		if slices.Contains(edges, edge) || slices.Contains(edges, dependency{from: file.Origin, to: origin}) {
			continue
		}
		edges = append(edges, edge)
	}
	return edges, database
}

// overridden is the definition a winning one hides and whether the two are written otherwise: the one before it
// in the load order, else the install's own winner, the zero At for none.
func (walk *dependencyWalk) overridden(ref schema.Occurrence, path string, defs []index.At, winner index.At) (index.At, bool) {
	var base index.At
	if i := slices.Index(defs, winner); i > 0 {
		base = defs[i-1]
	} else {
		installed, _ := index.Resolve(walk.ws.Install.Schema, []*index.Index{walk.ws.Install.Index}, ref, path)
		base, _, _, _ = walk.ws.Winner(installed)
	}
	pair := [2]index.At{winner, base}
	if _, done := walk.compared[pair]; !done && base != (index.At{}) {
		_, winning := walk.ws.Statement(walk.parses, winner)
		_, hidden := walk.ws.Statement(walk.parses, base)
		walk.compared[pair] = !sameStatement(winning, hidden)
	}
	return base, walk.compared[pair]
}
