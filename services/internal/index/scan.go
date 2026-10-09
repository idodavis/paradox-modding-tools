// scan.go scans an install and a workspace's mods into indexes, and walks one mod file again when
// it is saved.

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
	"paradox-modding-tools/services/internal/wiki"
)

// Request is one install's scan: Version is the game's, Language the localization kept
// besides english. Progress hears inventory, localization, schema, index (per file read), saving.
type Request struct {
	Game, Dir, Version, Language string
	App                          string // the app's version; "dev" keys the cache by the engine's code, "" never caches
	Progress                     func(stage string, done, total int)
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
	cached := req.App != ""
	if cached {
		if install, err := LoadInstall(req); err == nil {
			return install, nil
		}
	}

	// 1. the install's files and the dump folders'
	req.Progress("inventory", 0, 1)
	roots := inventory.Roots(req.Dir)
	input := schemaInput(req, roots)
	examples, written, err := wiki.ScriptExamples(req.Game, schema.DatabaseNames(input))
	if err != nil {
		return nil, err
	}
	input.WikiExamples, input.WikiWritten = examples, written

	// 2. the localization
	req.Progress("localization", 0, 1)
	localized, err := readLocalization(ctx, req, &input)
	if err != nil {
		return nil, err
	}

	// 3. the schema, from the dump files when every one is there, else with the shipped dump part; a
	// shipped file that does not decode leaves Build the dump files that exist
	req.Progress("schema", 0, 1)
	if len(schema.MissingDumps(input.Dumps)) > 0 {
		if backup, exact, _ := schema.LoadShipped(req.Game, req.Version); backup != nil {
			origin := backup.Sources[schema.SourceDumps]
			origin.From, origin.Stale = "shipped", !exact
			backup.Sources[schema.SourceDumps] = origin
			input.Shipped = backup
		}
	}
	s, err := schema.Build(ctx, input)
	if err != nil {
		return nil, err
	}

	// 4. the index: each other file parsed and walked once; Ready adds the localization's records in
	// file order, so no loc file is read again
	index := &Index{Files: input.Install, Roots: roots, Records: localized}
	err = index.walkFiles(ctx, s, false, func(done, total int) { req.Progress("index", done, total) })
	if err != nil {
		return nil, err
	}

	install := &Install{Schema: s, Index: index, Scanned: time.Now()}
	if !cached {
		return install, nil
	}
	req.Progress("saving", 0, 1)
	return install, SaveInstall(req, install)
}

// readLocalization reads the install's localization once, in english and the asked language: a file
// whose path names another language is never read. Its keys go to the schema's input, and its
// records come back for the index.
func readLocalization(ctx context.Context, req Request, input *schema.Input) (map[string]*Records, error) {
	languages, localized := []string{game.BaseLanguage, strings.ToLower(req.Language)}, map[string]*Records{}
	var locs []inventory.File
	for _, file := range input.Install {
		if file.Format != inventory.Loc {
			continue
		}
		if named := loc.LanguageFromRel(file.Rel); named == "" || slices.Contains(languages, named) {
			locs = append(locs, file)
		}
	}
	input.LocKeys = map[string]bool{}
	err := inventory.Ordered(ctx, locs, func(file inventory.File) *Records {
		text, _ := file.Text()
		return locRecords(req.Game, file, text, languages)
	}, func(file inventory.File, kept *Records) {
		if kept != nil {
			localized[file.Path] = kept
			for _, entry := range kept.Loc {
				input.LocKeys[entry.Key] = true
			}
		}
	})
	return localized, err
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

// walkFiles reads the files of Files that hold records, localization in every language only when
// withLoc is set: a worker per CPU parses and walks ahead while Add runs in file order here.
func (i *Index) walkFiles(ctx context.Context, s *schema.Schema, withLoc bool, progress func(done, total int)) error {
	i.Rebuild()
	var files []inventory.File
	for _, file := range i.Files {
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) ||
			withLoc && file.Format == inventory.Loc {
			files = append(files, file)
		}
	}

	done := 0
	return inventory.Ordered(ctx, files, func(file inventory.File) *Records {
		text, _ := file.Text()
		return recordsOf(s, file, text)
	}, func(file inventory.File, records *Records) {
		if records != nil {
			i.Add(records, file.Path)
		}
		done++
		if progress != nil {
			progress(done, len(files))
		}
	})
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
		descriptor, _ := modfiles.ReadDescriptor(info.ID, mod.Dir)
		for _, folder := range descriptor.ReplacePaths {
			folder = strings.Trim(strings.ReplaceAll(strings.ToLower(folder), `\`, "/"), "/")
			if stage, rest, staged := strings.Cut(folder, "/"); staged && slices.Contains(info.StageRoots, stage) {
				folder = rest
			}
			index.Replaced = append(index.Replaced, folder)
		}
	}
	return index, index.walkFiles(ctx, install.Schema, true, nil)
}

// Reindex walks one file again: its records dropped and added back. A file not yet in Files goes
// after its origin's last file, so it keeps that origin's place in load order. Every localization
// language is kept.
func (i *Index) Reindex(s *schema.Schema, file inventory.File, text string) {
	i.Drop(file.Path)
	if i.defs == nil {
		i.Rebuild()
	}
	if _, listed := i.At(file.Path); !listed {
		at := len(i.Files)
		for pos, other := range i.Files {
			if other.Origin == file.Origin {
				at = pos + 1
			}
		}
		i.Files = slices.Insert(i.Files, at, file)
		i.PlaceFiles()
	}
	if records := recordsOf(s, file, text); records != nil {
		i.Add(records, file.Path)
	}
}
