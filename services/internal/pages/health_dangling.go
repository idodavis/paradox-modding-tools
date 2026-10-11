// health_dangling.go is each name a mod's loaded file writes that the editor's checks find nothing for, one row per
// name.

package pages

import (
	"cmp"
	"slices"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// dangling is each name a mod's loaded file writes that the editor's checks find nothing for, from their findings,
// so a name dangles by the editor's rules.
func dangling(ws *workspace.Session, findings map[string][]lsp.Diagnostic, sources *sourceIndex) []Dangling {
	rows := map[[2]string]*Dangling{} // by database and name
	for _, file := range loadedFiles(ws) {
		occurrences := ws.Workspace.InFile(file.Path)
		for _, diagnostic := range findings[file.Path] {
			start := diagnostic.Range.Start
			i := slices.IndexFunc(occurrences, func(at index.At) bool {
				occurrence := at.Occurrence()
				return occurrence.Role == schema.Refers && int(occurrence.Line) == start.Line && int(occurrence.Col) == start.Character
			})
			if shownBy[diagnostic.Code] != cardDangling || i < 0 {
				continue
			}
			ref := occurrences[i].Occurrence()
			key := [2]string{ref.Database, ref.ID}
			row := rows[key]
			if row == nil {
				row = &Dangling{Database: ref.Database, Kind: game.KindName(ref.Database), Name: ref.ID}
				rows[key] = row
			}
			// the last check to find the name is the one the row names
			row.Code = diagnostic.Code
			row.Sites = append(row.Sites, Site{File: sources.index(file), Line: start.Line, Col: start.Character})
		}
	}
	var out []Dangling
	for _, row := range rows {
		out = append(out, *row)
	}
	slices.SortFunc(out, func(first, second Dangling) int {
		return cmp.Compare(first.Database+first.Name, second.Database+second.Name)
	})
	return out
}
