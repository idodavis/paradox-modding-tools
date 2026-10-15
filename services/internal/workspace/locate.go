// locate.go is where a path and a definition sit: the file, the mod and the database at a path, a definition's
// statement in its parse, a key's localization and a definition's title, and a mod's calendar.

package workspace

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
)

// Calendar is the eras a mod writes its dates in. A mod never states them, so the user declares
// them; with none declared, dates read as written.
type Calendar []Era

// Era is one era of a mod's calendar: its name and the year before its first (AGOT's A.C., 8000).
type Era struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
}

// FilePlace is where a path sits: its file (Origin "" for a path under no root), the database it defines and its
// mod's calendar.
type FilePlace struct {
	inventory.File
	Database *schema.Database // nil when the folder holds none
	Calendar Calendar
}

// Date is a date in the named era with the greatest offset below its year ("82.1.1 A.C." for 8082.1.1); false
// where none applies.
func (calendar Calendar) Date(year, month, day int) (string, bool) {
	best := -1
	for i, era := range calendar {
		if era.Name != "" && year-era.Offset > 0 && (best < 0 || era.Offset > calendar[best].Offset) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return fmt.Sprintf("%d.%d.%d %s", year-calendar[best].Offset, month, day, calendar[best].Name), true
}

// Locate is the indexed file at a path, else the file the path would be under a mod's or the install's root, and
// the mod it belongs to; under no root, the path alone and false.
func (ws *Session) Locate(path string) (inventory.File, *Mod, bool) {
	if path == "" {
		return inventory.File{}, nil, false
	}
	path = filepath.Clean(path)
	mod := func(origin string) *Mod {
		if i := slices.IndexFunc(ws.Mods, func(mod Mod) bool { return mod.ID == origin }); i >= 0 {
			return &ws.Mods[i]
		}
		return nil
	}
	for _, index := range slices.Backward(ws.Indexes()) {
		if file, known := index.At(path); known {
			return file, mod(file.Origin), true
		}
	}
	under := func(roots []string, origin string) (inventory.File, bool) {
		for _, root := range roots {
			if rel, err := filepath.Rel(root, path); err == nil && filepath.IsLocal(rel) {
				rel = filepath.ToSlash(rel)
				return inventory.File{Path: path, Rel: rel, Origin: origin, Format: inventory.Classify(path, rel)}, true
			}
		}
		return inventory.File{}, false
	}
	for i := range ws.Mods {
		if file, known := under(inventory.Roots(ws.Mods[i].Dir), ws.Mods[i].ID); known {
			return file, &ws.Mods[i], true
		}
	}
	if file, known := under(ws.Install.Index.Roots, game.OriginVanilla); known {
		return file, nil, true
	}
	return inventory.File{Path: path, Format: inventory.Classify(path, filepath.ToSlash(path))}, nil, false
}

// PlaceOf places a path in the workspace: its file, and the database a file there defines. A file not yet walked
// is placed by its root.
func (ws *Session) PlaceOf(path string) FilePlace {
	file, mod, known := ws.Locate(path)
	if !known {
		return FilePlace{File: file}
	}
	place := FilePlace{File: file, Database: ws.Install.Schema.DatabaseAt(file.Rel)}
	if mod != nil {
		place.Calendar = mod.Calendar
	}
	return place
}

// Statement is a definition's statement in its file's parse, from the parses a caller that asks for many keeps: the
// assignment whose key starts at the definition's line and column; nil where its file does not parse or none starts
// there.
func (ws *Session) Statement(parses Parses, def index.At) (*jomini.Result, jomini.Statement) {
	if def == (index.At{}) {
		return nil, nil
	}
	parsed, _, err := ws.Parsed(parses, def.File().Path)
	if err != nil {
		return nil, nil
	}
	return parsed, parsed.AssignmentAt(int(def.Occurrence().Line), int(def.Occurrence().Col))
}

// Loc is a key's entry in a language, the last loaded: a mod's over the install's. With none in the language it
// is the english one, so a title shows; whether the key exists is Resolve's to say.
func (ws *Session) Loc(key, language string) (index.At, bool) {
	entries := index.Lookup(ws.Indexes(), schema.Localizes, game.Localization, key)
	for _, wanted := range []string{cmp.Or(language, ws.Language), game.BaseLanguage} {
		for _, entry := range slices.Backward(entries) {
			if entry.Language() == wanted {
				return entry, true
			}
		}
	}
	return index.At{}, false
}

// Title is the display name of a definition of id in a language ("" the workspace's): the text of the id as a loc key
// (after its database's prefix where it has one), else of the definition's first field typed as a loc key that has
// text (the zero At: the loc key alone).
func (ws *Session) Title(id string, def index.At, language string) string {
	row := ws.Install.Schema.Databases[def.Occurrence().Database]
	keys := []string{id}
	if row != nil && row.Descriptor.LocPrefix != "" {
		keys = []string{row.Descriptor.LocPrefix + id, id}
	}
	for _, key := range keys {
		if entry, known := ws.Loc(key, language); known {
			return entry.Text()
		}
	}
	_, statement := ws.Statement(Parses{}, def)
	for i, block := 0, jomini.ChildBlock(statement); row != nil && block != nil && i < len(block.Statements); i++ {
		field, isField := block.Statements[i].(*jomini.Assignment)
		if !isField {
			continue
		}
		position := ws.Install.Schema.Enter(ws.Install.Schema.EntryContext(row, ""), field, nil, false).Position()
		value, isScalar := field.Value.(*jomini.Scalar)
		if !isScalar || position == nil || position.Value.Value.Type != schema.ValueLoc {
			continue
		}
		if entry, known := ws.Loc(value.Text, language); known {
			return entry.Text()
		}
	}
	return ""
}

// LocKeys is up to limit of the keys that start with prefix, lower-cased, in order.
func (ws *Session) LocKeys(prefix string, limit int) []string {
	keys := map[string]bool{}
	for _, index := range ws.Indexes() {
		for key := range index.LocKeys() {
			if strings.HasPrefix(strings.ToLower(key), prefix) {
				keys[key] = true
			}
		}
	}
	sorted := slices.Sorted(maps.Keys(keys))
	return sorted[:min(limit, len(sorted))]
}
