// scan.go scans an install and a workspace's mods into indexes, keeps an install's schema and index
// in one cache file, and walks one mod file again when it is saved.

package index

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lang/shipped"
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

	// 2. the localization, read once in english and the asked language (a file whose path names
	// another is never read): its keys go to the schema, its records to the index
	req.Progress("localization", 0, 1)
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
		return read(req.Game, nil, file, text, languages)
	}, func(file inventory.File, kept *Records) {
		if kept != nil {
			localized[file.Path] = kept
			for _, entry := range kept.Loc {
				input.LocKeys[entry.Key] = true
			}
		}
	})
	if err != nil {
		return nil, err
	}

	// 3. the schema, from the dump files when every one is there, else with the shipped dump part; a
	// shipped file that does not decode leaves Build the dump files that exist
	req.Progress("schema", 0, 1)
	if len(schema.MissingDumps(input.Dumps)) > 0 {
		if backup, exact, _ := shipped.Load(req.Game, req.Version); backup != nil {
			origin := backup.Sources[schema.SourceDumps]
			origin.From, origin.Stale = "shipped", !exact
			backup.Sources[schema.SourceDumps] = origin
			input.Shipped = backup
		}
	}
	sch, err := schema.Build(ctx, input)
	if err != nil {
		return nil, err
	}

	// 4. the index: each other file parsed and walked once; Ready adds the localization's records in
	// file order, so no loc file is read again
	idx := &Index{Files: input.Install, Roots: roots, Records: localized}
	err = index(ctx, sch, idx, false, func(done, total int) { req.Progress("index", done, total) })
	if err != nil {
		return nil, err
	}

	install := &Install{Schema: sch, Index: idx, Scanned: time.Now()}
	if !cached {
		return install, nil
	}
	req.Progress("saving", 0, 1)
	return install, SaveInstall(req, install)
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
// dump folders' files, the wiki sidecar and when the install last changed.
func schemaInput(req Request, roots []string) schema.Input {
	sidecar, _ := wiki.SidecarPath(req.Game)
	return schema.Input{
		Game:    req.Game,
		Version: req.Version,
		Install: inventory.Files(game.OriginVanilla, roots),
		Dumps:   inventory.Files("dumps", game.DumpDirs(req.Game, req.Dir)),
		Wiki:    sidecar,
		Updated: game.InstallUpdatedAt(req.Dir),
	}
}

// index reads the files of idx.Files that hold records, localization in every language only when
// withLoc is set: a worker per CPU parses and walks ahead while Add runs in file order here.
func index(ctx context.Context, sch *schema.Schema, idx *Index, withLoc bool, progress func(done, total int)) error {
	idx.Ready()
	var files []inventory.File
	for _, file := range idx.Files {
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) ||
			withLoc && file.Format == inventory.Loc {
			files = append(files, file)
		}
	}

	done := 0
	return inventory.Ordered(ctx, files, func(file inventory.File) *Records {
		text, _ := file.Text()
		return read(sch.Game, sch, file, text, nil)
	}, func(file inventory.File, records *Records) {
		if records != nil {
			idx.Add(records, file.Path)
		}
		done++
		if progress != nil {
			progress(done, len(files))
		}
	})
}

// read is one file's records by its format: script walked, a table or sound list walked without a
// tree, localization parsed with no schema (nil before the schema stage); nil when the format holds
// none or the language is not kept (languages none: every one).
func read(gameID string, sch *schema.Schema, file inventory.File, text string, languages []string) *Records {
	switch file.Format {
	case inventory.Script:
		parsed := jomini.Parse(text)
		return Walk(sch, file, &parsed, nil)
	case inventory.Table, inventory.List:
		return Walk(sch, file, nil, nil)
	case inventory.Loc:
		parsed := loc.Parse(text)
		language := strings.ToLower(cmp.Or(parsed.Language, loc.LanguageFromRel(file.Rel)))
		if len(languages) > 0 && !slices.Contains(languages, language) {
			return nil
		}

		// a loc text is cloned so no record pins the file; its $key$ names a loc key, and [x|E] and
		// Concept('x', …) a game concept: each a reference in no definition
		concepts := ""
		for at, row := range game.Get(gameID).Folders {
			if row.Concepts {
				concepts = schema.NameOf(at)
			}
		}
		records, lines := &Records{}, jomini.NewLineIndex(text)
		refer := func(database, name string, span jomini.Range) {
			at := lines.PositionAt(span.Start)
			records.Refs = append(records.Refs, Ref{
				Database: database, Name: name, Path: file.Path, Range: span,
				Line: int32(at.Line), Col: int32(at.Character), From: -1,
			})
		}
		for _, entry := range parsed.Entries {
			records.Loc = append(records.Loc, Loc{
				Key: entry.Key, Language: language, Text: strings.Clone(entry.Value), Path: file.Path, Line: entry.Line,
			})
			for _, interp := range loc.Interps(entry.Value, entry.ValueRange.Start) {
				refer(Localization, interp.Key, interp.KeyRange)
			}
			for _, call := range jomini.Calls(entry.Value, entry.ValueRange.Start) {
				steps := jomini.ParseCall(text[call.Start:call.End], call.Start)
				if concepts == "" || len(steps) == 0 {
					continue
				}
				format := strings.TrimLeft(text[steps[0].Range.End:call.End], " \t")
				if len(steps) == 1 && len(steps[0].Args) == 0 && (format == "|E]" || format == "|e]") {
					refer(concepts, steps[0].Name, steps[0].Range)
				}
				for _, step := range steps {
					if step.Name == "Concept" && len(step.Args) > 0 && step.Args[0].Quoted {
						arg := step.Args[0]
						refer(concepts, arg.Text, jomini.Range{Start: arg.Range.Start + 1, End: arg.Range.End - 1})
					}
				}
			}
		}
		return records
	}
	return nil
}

// ScanMods indexes a workspace's mods, in playset order, over an install: one index of their files,
// every localization language kept, with the folders their descriptors unload in Replaced.
func ScanMods(ctx context.Context, install *Install, mods []Mod) (*Index, error) {
	info := game.Get(install.Schema.Game)
	idx := &Index{}
	for _, mod := range mods {
		roots := inventory.Roots(mod.Dir)
		idx.Roots = append(idx.Roots, roots...)
		idx.Files = append(idx.Files, inventory.Files(mod.ID, roots)...)

		// each folder its descriptor unloads, held lower-case, slash separated and without the stage an
		// inventory root drops
		descriptor, _ := modfiles.ReadDescriptor(info.ID, mod.Dir)
		for _, folder := range descriptor.ReplacePaths {
			folder = strings.Trim(strings.ReplaceAll(strings.ToLower(folder), `\`, "/"), "/")
			if stage, rest, staged := strings.Cut(folder, "/"); staged && slices.Contains(info.StageRoots, stage) {
				folder = rest
			}
			idx.Replaced = append(idx.Replaced, folder)
		}
	}
	return idx, index(ctx, install.Schema, idx, true, nil)
}

// Reindex walks one file again: its records dropped and added back. A file not yet in Files goes
// after its origin's last file, so it keeps that origin's place in load order. Every localization
// language is kept.
func (idx *Index) Reindex(sch *schema.Schema, file inventory.File, text string) {
	idx.Drop(file.Path)
	if idx.defs == nil {
		idx.Ready()
	}
	if _, listed := idx.At(file.Path); !listed {
		at := len(idx.Files)
		for i, other := range idx.Files {
			if other.Origin == file.Origin {
				at = i + 1
			}
		}
		idx.Files = slices.Insert(idx.Files, at, file)
		idx.Place()
	}
	if records := read(sch.Game, sch, file, text, nil); records != nil {
		idx.Add(records, file.Path)
	}
}

// devKey is a dev build's app part of a cache name: dev- and a hash of the engine's code, the files
// of the game, lang and wiki packages in the repo the build sits in (tests and READMEs aside), so
// only an engine change rescans. Outside a repo it hashes the executable's path, size and time.
var devKey = sync.OnceValues(func() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	sum, engine := sha256.New(), false
	for repo := filepath.Dir(executable); repo != filepath.Dir(repo) && !engine; repo = filepath.Dir(repo) {
		for _, dir := range []string{"game", "lang", "wiki"} {
			_ = inventory.Walk(filepath.Join(repo, "services", "internal", dir), func(path string) error {
				if text, err := os.ReadFile(path); err == nil && !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".md") {
					engine = true
					fmt.Fprintf(sum, "%s|%d|", filepath.Base(path), len(text))
					sum.Write(text)
				}
				return nil
			})
		}
	}
	if !engine {
		fmt.Fprintf(sum, "%s|%d|%d", executable, info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("dev-%x", sum.Sum(nil)[:6]), nil
})

// cachePath is a request's cache file, install-<game>-<folder>-<version>-<app>-<state>.gob.gz, and
// its prefix; the state hashes the language, install update time, dump names and dates.
func cachePath(req Request) (path, ofFolder string, err error) {
	dir, err := appdata.CacheDir()
	if err != nil {
		return "", "", err
	}
	version, app := shipped.Safe(req.Version), shipped.Safe(req.App)
	if req.App == "dev" {
		if app, err = devKey(); err != nil {
			return "", "", err
		}
	}
	state := sha256.New()
	fmt.Fprintf(state, "%s|%d", req.Language, game.InstallUpdatedAt(req.Dir).UnixNano())
	for _, file := range inventory.Files("dumps", game.DumpDirs(req.Game, req.Dir)) {
		name := filepath.Base(file.Path)
		if info, err := os.Stat(file.Path); err == nil && slices.Contains(schema.DumpFiles, name) {
			fmt.Fprintf(state, "|%s|%d", name, info.ModTime().UnixNano())
		}
	}
	folder := sha256.Sum256([]byte(filepath.Clean(req.Dir)))
	ofFolder = filepath.Join(dir, fmt.Sprintf("install-%s-%x-", shipped.Safe(req.Game), folder[:6]))
	return fmt.Sprintf("%s%s-%s-%x.gob.gz", ofFolder, version, app, state.Sum(nil)[:6]), ofFolder, nil
}

// forget deletes the cache files whose path begins with prefix, except keep, and gives the version
// in the name of the first (a version has no dash).
func forget(prefix, keep string) (version string, err error) {
	dir := filepath.Dir(prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if path == keep || !strings.HasPrefix(path, prefix) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return version, err
		}
		if version == "" {
			version, _, _ = strings.Cut(strings.TrimPrefix(path, prefix), "-")
		}
	}
	return version, nil
}

// SaveInstall writes the install to its cache file whole, then deletes its install folder's older
// cache files (Previous takes the first one's version) and every cache file no reader names
// (neither install- nor wiki-), and the temporary files a run that ended mid-write left (appdata.Leftover); a write in
// flight keeps its own.
func SaveInstall(req Request, install *Install) error {
	path, ofFolder, err := cachePath(req)
	if err != nil {
		return err
	}
	err = appdata.Write(path, func(out io.Writer) error { return appdata.Encode(out, install) })
	if err != nil {
		return err
	}
	install.Previous, err = forget(ofFolder, path)
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		name, full := entry.Name(), filepath.Join(filepath.Dir(path), entry.Name())
		if !strings.HasPrefix(name, "install-") && !strings.HasPrefix(name, "wiki-") && (!strings.HasPrefix(name, ".") || appdata.Leftover(full)) {
			_ = os.Remove(full)
		}
	}
	return err
}

// CachedAt is when a request's cache file was written, by its file time; zero where there is none.
func CachedAt(req Request) time.Time {
	if path, _, err := cachePath(req); err == nil {
		if info, err := os.Stat(path); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}

// LoadInstall reads a request's cache file, with the index ready to query.
func LoadInstall(req Request) (*Install, error) {
	path, _, err := cachePath(req)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	install := &Install{}
	if err := appdata.Decode(file, install); err != nil {
		return nil, err
	}
	install.Index.Ready()
	return install, nil
}

// DropInstall deletes every cache file of a request's game and install folder.
func DropInstall(req Request) error {
	_, ofFolder, err := cachePath(req)
	if err != nil {
		return err
	}
	_, err = forget(ofFolder, "")
	return err
}
