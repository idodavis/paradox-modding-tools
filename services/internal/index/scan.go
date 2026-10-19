// scan.go is the pipeline: Scan runs an install through six steps, ScanMods a mod set over it,
// Reindex one saved file again.

package index

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/modfiles"
	"paradox-modding-tools/services/internal/schema"
)

// Request is one install's scan: Version is the game's, Language the localization kept
// besides english. Progress hears inventory, localization, schema, index (per file read), saving.
type Request struct {
	Game, Dir, Version, Language string
	App                          string // the app's version; "dev" keys the cache by the engine's code, "" never caches
	Progress                     func(stage string, done, total int)

	// Wiki gives the wiki's script examples by database and its sidecar's time; nil for none.
	Wiki func(databases []string) (map[string][]string, time.Time, error)
}

// Install is an install's schema and the index of its files.
type Install struct {
	Schema   *schema.Schema
	Index    *Index
	Scanned  time.Time // when the install was built; a cache load keeps the original
	Previous string    // the version of the older cache file this scan replaced; "" when none or loaded
}

// Mod is one mod of a workspace: its id and folder.
type Mod struct{ ID, Dir string }

// Scan gives an install's schema and index from the cache, else builds and caches them; a
// cache that cannot be written returns the install with its error.
func Scan(ctx context.Context, req Request) (*Install, error) {
	if req.Progress == nil {
		req.Progress = func(string, int, int) {}
	}
	if install, found := fromCache(req); found { // 1: the cache, while nothing changed
		return install, nil
	}
	roots, input, err := inventoryOf(req) // 2: the install's files, the dump folders, the wiki's examples
	if err != nil {
		return nil, err
	}
	localized, err := localizationOf(ctx, req, &input) // 3: english and the asked language
	if err != nil {
		return nil, err
	}
	s, err := schemaOf(ctx, req, input) // 4: the dumps or the shipped backup, then Build
	if err != nil {
		return nil, err
	}

	// 5: each other file parsed and walked once; the localization's occurrences came with the localization, so
	// no loc file is read again
	index := &Index{Files: input.Install, Roots: roots}
	index.Rebuild()
	for path, records := range localized {
		index.add(path, records)
	}
	err = index.collectFiles(ctx, s, false, func(done, total int) { req.Progress("index", done, total) })
	if err != nil {
		return nil, err
	}
	install := &Install{Schema: s, Index: index, Scanned: time.Now()}
	if req.App == "" {
		return install, nil
	}
	req.Progress("saving", 0, 1)
	return install, SaveInstall(req, install) // 6: the cache
}

// inventoryOf lists the install's files and the dump folders', with the wiki to ask for the examples of the
// databases the schema declares.
func inventoryOf(req Request) (roots []string, input schema.Input, err error) {
	req.Progress("inventory", 0, 1)
	roots = inventory.Roots(req.Dir)
	input = schemaInput(req, roots)
	input.Wiki = req.Wiki
	return roots, input, nil
}

// schemaInput is the schema stage's input: the files under the install's roots in load order, the
// dump folders' files and when the install last changed.
func schemaInput(req Request, roots []string) schema.Input {
	return schema.Input{
		Game:    req.Game,
		Version: req.Version,
		Install: inventory.Files(game.OriginVanilla, roots),
		Dumps:   inventory.Files("dumps", game.DumpDirs(req.Game, req.Dir)),
		Updated: game.InstallUpdatedAt(req.Dir),
	}
}

// localizationOf reads the install's localization once, in english and the asked language: a file
// whose path names another language is never read. Its keys go to the schema's input, and its
// occurrences come back for the index.
func localizationOf(ctx context.Context, req Request, input *schema.Input) (map[string]collected, error) {
	req.Progress("localization", 0, 1)
	languages, localized := []string{game.BaseLanguage, strings.ToLower(req.Language)}, map[string]collected{}
	var locs []inventory.File
	for _, file := range input.Install {
		if file.Format != inventory.Loc {
			continue
		}
		if named := loc.Language("", file.Rel); named == "" || slices.Contains(languages, named) {
			locs = append(locs, file)
		}
	}
	input.LocKeys = map[string]bool{}
	err := inventory.Ordered(ctx, locs, func(file inventory.File) collected {
		text, _ := file.Text()
		return collectLoc(req.Game, file, text.Content, languages)
	}, func(file inventory.File, kept collected) {
		if len(kept.occurrences) > 0 {
			localized[file.Path] = kept
			for _, occurrence := range kept.occurrences {
				if occurrence.Role == schema.Localizes {
					input.LocKeys[occurrence.ID] = true
				}
			}
		}
	})
	return localized, err
}

// schemaOf builds the schema from the dump files when every one is there, else with the shipped dump
// part; a shipped file that does not decode leaves Build the dump files that exist.
func schemaOf(ctx context.Context, req Request, input schema.Input) (*schema.Schema, error) {
	req.Progress("schema", 0, 1)
	if len(schema.MissingDumps(input.Dumps)) > 0 {
		if backup, exact, _ := schema.LoadShipped(req.Game, req.Version); backup != nil {
			srcState := backup.Sources[schema.SourceDumps]
			srcState.Origin, srcState.Stale = "shipped", !exact
			backup.Sources[schema.SourceDumps] = srcState
			input.Shipped = backup
		}
	}
	return schema.Build(ctx, input)
}

// BuildSchema is the dump part the generator ships, with the install's databases so dump claims
// can name them; it refuses unless every dump file is there, so a partial backup never ships.
func BuildSchema(ctx context.Context, req Request) (*schema.Schema, error) {
	input := schemaInput(req, inventory.Roots(req.Dir))
	if missing := schema.MissingDumps(input.Dumps); len(missing) > 0 {
		return nil, fmt.Errorf("dump files missing for %s: %s", req.Game, strings.Join(missing, ", "))
	}
	input.DumpsOnly = true
	return schema.Build(ctx, input)
}

// ScanMods indexes a workspace's mods, in playset order, over an install: one index of their files,
// every localization language kept, with the folders their descriptors unload in Replaced.
func ScanMods(ctx context.Context, install *Install, mods []Mod) (*Index, error) {
	info := game.Get(install.Schema.Game)
	index := &Index{}
	for _, mod := range mods {
		roots := inventory.Roots(mod.Dir)
		index.Roots = append(index.Roots, roots...)
		index.Files = append(index.Files, inventory.Files(mod.ID, roots)...)

		// each folder its descriptor unloads, held lower-case, slash separated and without the stage an
		// inventory root drops
		descriptor, _ := modfiles.LoadDescriptor(info.ID, mod.Dir)
		for _, folder := range descriptor.ReplacePaths {
			folder = strings.Trim(strings.ReplaceAll(strings.ToLower(folder), `\`, "/"), "/")
			if stage, rest, staged := strings.Cut(folder, "/"); staged && slices.Contains(info.StageRoots, stage) {
				folder = rest
			}
			index.Replaced = append(index.Replaced, folder)
		}
	}
	return index, index.collectFiles(ctx, install.Schema, true, nil)
}

// Reindex walks one file again: its occurrences dropped and added back. A file not yet in Files goes
// after its origin's last file, so it keeps that origin's place in load order. Every localization
// language is kept.
func (index *Index) Reindex(s *schema.Schema, file inventory.File, text string) {
	if index.postings == nil {
		index.Rebuild()
	}
	index.Drop(file.Path)
	if _, listed := index.At(file.Path); !listed {
		at := len(index.Files)
		for pos, other := range index.Files {
			if other.Origin == file.Origin {
				at = pos + 1
			}
		}
		index.Files = slices.Insert(index.Files, at, file)
		index.Occurrences = slices.Insert(index.Occurrences, at, nil)
		index.Texts = slices.Insert(index.Texts, at, nil)
		index.Rebuild()
	}
	if records := collectFile(s, file, text); len(records.occurrences) > 0 {
		index.add(file.Path, records)
	}
}
