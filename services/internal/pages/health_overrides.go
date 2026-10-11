// health_overrides.go is each file path two origins ship, which the game loads one of: what each copy defines and
// what the kept copy writes otherwise.

package pages

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// shippedFiles is each path, lower-cased, that files of the preview sit at in a folder the install ships files in,
// with the files there in load order, and each file's entries: those a schema database holds, neither local nor a
// name script makes.
type shippedFiles struct {
	byRel   map[string][]inventory.File
	defines map[string][]index.At // by file path
}

// fileOverrides is each path a mod ships a file at that another origin ships too, by a load order: the game loads
// one file of a path, so the loaded one replaces the others whole, a contest where two mods ship it, else an
// override, and each site lists the entries its file defines, as contests counts them, and those the kept file
// writes otherwise. Only a file in a folder the install ships files in is the game's: a mod's descriptor, readme,
// thumbnail or editor settings are not.
func fileOverrides(ws *workspace.Session, preview []*index.Index, sources *sourceIndex) (contested, overrides []Contest) {
	shipped := shippedAt(ws, preview)
	for _, rel := range slices.Sorted(maps.Keys(shipped.byRel)) {
		files := shipped.byRel[rel]
		kept := slices.IndexFunc(files, func(file inventory.File) bool {
			_, loaded := index.Loaded(preview, file.Path)
			return loaded
		})
		origins := map[string]bool{}
		for _, file := range files {
			origins[file.Origin] = true
		}
		if len(origins) < 2 || kept < 0 {
			continue
		}
		row := shipped.row(ws, files, kept, sources)
		delete(origins, game.OriginVanilla)
		if len(origins) >= 2 {
			contested = append(contested, row)
		} else {
			overrides = append(overrides, row)
		}
	}
	return contested, overrides
}

// shippedAt is the preview's files at each path in a folder the install ships files in, with what each defines.
func shippedAt(ws *workspace.Session, preview []*index.Index) shippedFiles {
	folders := map[string]bool{}
	for _, file := range ws.Install.Index.Files {
		if top, _, nested := strings.Cut(strings.ToLower(file.Rel), "/"); nested {
			folders[top] = true
		}
	}
	shipped := shippedFiles{byRel: map[string][]inventory.File{}, defines: map[string][]index.At{}}
	for _, layer := range preview {
		shipped.add(ws, layer, folders)
	}
	return shipped
}

// add takes in the files of one index that sit in a folder the install ships files in, and their entries.
func (shipped shippedFiles) add(ws *workspace.Session, index *index.Index, folders map[string]bool) {
	for _, file := range index.Files {
		rel := strings.ToLower(file.Rel)
		if top, _, nested := strings.Cut(rel, "/"); !nested || !folders[top] {
			continue
		}
		shipped.byRel[rel] = append(shipped.byRel[rel], file)
		for _, def := range index.InFile(file.Path) {
			occurrence := def.Occurrence()
			entry := occurrence.Role == schema.Defines && occurrence.ID != ""
			if entry && ws.Install.Schema.Databases[occurrence.Database] != nil && !occurrence.Local && !occurrence.IsName {
				shipped.defines[file.Path] = append(shipped.defines[file.Path], def)
			}
		}
	}
}

// parse is the parse of a file that defines entries; nil for a file that defines none (an image is never parsed).
func (shipped shippedFiles) parse(ws *workspace.Session, file inventory.File) *jomini.Result {
	if len(shipped.defines[file.Path]) == 0 {
		return nil
	}
	parsed, _, err := ws.Parsed(file.Path)
	if err != nil {
		return nil
	}
	return parsed
}

// row is one path's contest: a site per file with the names it defines, and on each file but the kept one the
// names it writes otherwise than the kept file does; the kept file's site lists the names any other changes.
func (shipped shippedFiles) row(ws *workspace.Session, files []inventory.File, kept int, sources *sourceIndex) Contest {
	row := Contest{Database: schema.File, Kind: game.KindName(schema.File), Name: files[kept].Rel, Winner: kept, Rule: "LIOS"}
	keptParse := shipped.parse(ws, files[kept])
	keptDefs := map[string]index.At{}
	for _, def := range shipped.defines[files[kept].Path] {
		keptDefs[def.Occurrence().ID] = def
	}
	changed := map[string]bool{}
	for i, file := range files {
		site := Site{File: sources.index(file)}
		parsed := shipped.parse(ws, file)
		for _, def := range shipped.defines[file.Path] {
			occurrence := def.Occurrence()
			site.Defines = append(site.Defines, occurrence.ID)
			keptDef, inKept := keptDefs[occurrence.ID]
			if !inKept || i == kept || parsed == nil || keptParse == nil {
				continue
			}
			here := parsed.AssignmentAt(int(occurrence.Line), int(occurrence.Col))
			there := keptParse.AssignmentAt(int(keptDef.Occurrence().Line), int(keptDef.Occurrence().Col))
			if !sameStatement(here, there) {
				site.Changed = append(site.Changed, occurrence.ID)
				changed[occurrence.ID] = true
			}
		}
		row.Sites = append(row.Sites, site)
	}
	for _, name := range row.Sites[kept].Defines {
		if changed[name] {
			row.Sites[kept].Changed = append(row.Sites[kept].Changed, name)
		}
	}
	return row
}

// sameStatement is whether two statements are written alike: the same keys, operators and words, quoted or not,
// in the same blocks. Comments are outside the tree and never count; a pair of other kinds is not alike.
func sameStatement(first, second jomini.Node) bool {
	switch first := first.(type) {
	case *jomini.Assignment:
		second, ok := second.(*jomini.Assignment)
		return ok && first.Key.Text == second.Key.Text && first.Op == second.Op && sameValue(first.Value, second.Value)
	case *jomini.ValueStmt:
		second, ok := second.(*jomini.ValueStmt)
		return ok && sameValue(first.Value, second.Value)
	case *jomini.Scalar:
		second, ok := second.(*jomini.Scalar)
		return ok && first.Text == second.Text
	case *jomini.TaggedBlock:
		second, ok := second.(*jomini.TaggedBlock)
		return ok && first.Tag.Text == second.Tag.Text && sameStatement(&first.Block, &second.Block)
	case *jomini.Block:
		second, ok := second.(*jomini.Block)
		return ok && slices.EqualFunc(first.Statements, second.Statements, func(left, right jomini.Statement) bool {
			return sameStatement(left, right)
		})
	}
	return false
}

// sameValue is whether two values are written alike; a missing value is alike only to another missing one.
func sameValue(first, second jomini.Value) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return sameStatement(first, second)
}
