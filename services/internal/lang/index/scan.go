// scan.go scans an install and a workspace's mods into indexes, keeps an install's schema and index
// in one cache file, and walks one mod file again when it is saved (spec §3, §7.3).

package index

import (
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/shipped"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/wiki"
)

// Request is one install's scan: Version is the game's (D-11), Language the localization kept
// besides english. Progress hears inventory, schema, index (per file read) and saving.
type Request struct {
	Game, Dir, Version, Language string
	App                          string // the app's version; "dev" or "" never reads or writes the cache
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

// Scan gives an install's schema and index from the cache, else builds and caches them (D-5); a
// cache that cannot be written returns the install with its error.
func Scan(ctx context.Context, req Request) (*Install, error) {
	if req.Progress == nil {
		req.Progress = func(string, int, int) {}
	}
	cached := req.App != "" && req.App != "dev"
	if cached {
		if install, err := LoadInstall(req); err == nil {
			return install, nil
		}
	}

	// 1. the install's files and the dump folders'
	req.Progress("inventory", 0, 1)
	roots := source.Roots(req.Dir)
	input := inventory(req, roots)

	// 2. the schema, from the dump files when every one is there, else with the shipped dump part; a
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

	// 3. the index: each file parsed and walked once, localization in english and the asked language
	idx := &Index{Files: input.Install, Roots: roots}
	languages := []string{"english", strings.ToLower(req.Language)}
	err = index(ctx, sch, idx, languages, func(done, total int) { req.Progress("index", done, total) })
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

// BuildSchema is what the generator ships (D-27): the install's databases, which dump claims name,
// and the dump part; refused unless every dump file is there, so a partial backup never ships.
func BuildSchema(ctx context.Context, req Request) (*schema.Schema, error) {
	input := inventory(req, source.Roots(req.Dir))
	if missing := schema.MissingDumps(input.Dumps); len(missing) > 0 {
		return nil, fmt.Errorf("dump files missing for %s: %s", req.Game, strings.Join(missing, ", "))
	}
	input.DumpsOnly = true
	return schema.Build(ctx, input)
}

// inventory is the schema stage's input: the files under the install's roots in load order, the
// dump folders' files, the wiki sidecar and when the install last changed.
func inventory(req Request, roots []string) schema.Input {
	sidecar, _ := wiki.SidecarPath(req.Game)
	return schema.Input{
		Game:    req.Game,
		Version: req.Version,
		Install: source.Inventory(game.OriginVanilla, roots),
		Dumps:   source.Inventory("dumps", game.DumpDirs(req.Game, req.Dir)),
		Wiki:    sidecar,
		Updated: game.InstallUpdatedAt(req.Dir),
	}
}

// index reads the files of idx.Files that hold records: a worker per CPU parses and walks ahead
// while Add runs in file order here. languages limits the localization kept (none: every language).
func index(ctx context.Context, sch *schema.Schema, idx *Index, languages []string,
	progress func(done, total int)) error {
	idx.Ready()
	var files []source.File
	for _, file := range idx.Files {
		if slices.Contains([]source.Format{source.Script, source.Table, source.List, source.Loc}, file.Format) {
			files = append(files, file)
		}
	}

	done := 0
	return source.Ordered(ctx, files, func(file source.File) *Records {
		text, _ := file.Text()
		return read(sch, file, text, languages)
	}, func(file source.File, records *Records) {
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
// tree, localization parsed; nil when the format holds none or the language is not kept.
func read(sch *schema.Schema, file source.File, text string, languages []string) *Records {
	switch file.Format {
	case source.Script:
		parsed := jomini.Parse(text)
		return Walk(sch, file, &parsed, nil)
	case source.Table, source.List:
		return Walk(sch, file, nil, nil)
	case source.Loc:
		parsed := loc.Parse(text)
		language := strings.ToLower(cmp.Or(parsed.Language, loc.LanguageFromRel(file.Rel)))
		if len(languages) > 0 && !slices.Contains(languages, language) {
			return nil
		}

		// strings are cloned, so a record never pins the file's text. Each $key$ a text interpolates
		// names a loc key; a [x|E] or [x|e] link and Concept('x', …) name a concept of the database
		// whose registry row has Concepts; each is a reference in no definition (D-31)
		concepts := cmp.Or(sch.Flagged(func(row game.Folder) bool { return row.Concepts })...)
		records, language, lines := &Records{}, strings.Clone(language), jomini.NewLineIndex(text)
		refer := func(database, name string, span jomini.Range) {
			at := lines.PositionAt(span.Start)
			records.Refs = append(records.Refs, Ref{
				Database: database, Name: strings.Clone(name), Path: file.Path, Range: span,
				Line: int32(at.Line), Col: int32(at.Character), From: -1,
			})
		}
		for _, entry := range parsed.Entries {
			records.Loc = append(records.Loc, Loc{
				Key: strings.Clone(entry.Key), Language: language, Text: strings.Clone(entry.Value), Path: file.Path,
				Line: entry.Line,
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
		roots := source.Roots(mod.Dir)
		idx.Roots = append(idx.Roots, roots...)
		idx.Files = append(idx.Files, source.Inventory(mod.ID, roots)...)

		// CK3's descriptor.mod has a replace_path line per folder; Vic3 and EU5 list them in the
		// metadata's game_custom_data.replace_paths
		descriptor, _ := source.File{Path: game.DescriptorPath(info.ID, mod.Dir)}.Text()
		var folders []string
		if info.Descriptor == "metadata" {
			var metadata struct {
				Custom struct {
					Paths []string `json:"replace_paths"`
				} `json:"game_custom_data"`
			}
			_ = json.Unmarshal([]byte(descriptor), &metadata)
			folders = metadata.Custom.Paths
		} else {
			parsed := jomini.Parse(descriptor)
			for _, statement := range parsed.Root.Statements {
				if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Text == "replace_path" {
					if scalar, isScalar := assign.Value.(*jomini.Scalar); isScalar {
						folders = append(folders, scalar.Text)
					}
				}
			}
		}

		// a folder is held lower-case, slash separated and without the stage an inventory root drops
		for _, folder := range folders {
			folder = strings.Trim(strings.ReplaceAll(strings.ToLower(folder), `\`, "/"), "/")
			if stage, rest, staged := strings.Cut(folder, "/"); staged && slices.Contains(info.StageRoots, stage) {
				folder = rest
			}
			idx.Replaced = append(idx.Replaced, folder)
		}
	}
	return idx, index(ctx, install.Schema, idx, nil, nil)
}

// Reindex walks one file again: its records dropped and added back. A file not yet in Files is
// appended to it, its Rel listed. Every localization language is kept.
func (idx *Index) Reindex(sch *schema.Schema, file source.File, text string) {
	idx.Drop(file.Path)
	if idx.defs == nil {
		idx.Ready()
	}
	if _, listed := idx.At(file.Path); !listed {
		idx.order[strings.ToLower(file.Path)] = len(idx.Files)
		idx.rels[strings.ToLower(file.Rel)] = len(idx.Files)
		idx.Files = append(idx.Files, file)
	}
	if records := read(sch, file, text, nil); records != nil {
		idx.Add(records, file.Path)
	}
}

// cachePath is a request's cache file, install-<game>-<folder>-<version>-<app>-<state>.gob.gz, and
// its prefix (D-28); the state hashes the language, install update time, dump names and dates.
func cachePath(req Request) (path, ofFolder string, err error) {
	dir, err := local.CacheDir()
	if err != nil {
		return "", "", err
	}
	state := sha256.New()
	fmt.Fprintf(state, "%s|%d", req.Language, game.InstallUpdatedAt(req.Dir).UnixNano())
	for _, file := range source.Inventory("dumps", game.DumpDirs(req.Game, req.Dir)) {
		name := filepath.Base(file.Path)
		if info, err := os.Stat(file.Path); err == nil && slices.Contains(schema.DumpFiles, name) {
			fmt.Fprintf(state, "|%s|%d", name, info.ModTime().UnixNano())
		}
	}
	folder := sha256.Sum256([]byte(filepath.Clean(req.Dir)))
	ofFolder = filepath.Join(dir, fmt.Sprintf("install-%s-%x-", shipped.Safe(req.Game), folder[:6]))
	version, app := shipped.Safe(req.Version), shipped.Safe(req.App)
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

// SaveInstall writes an install's schema and index to its request's cache file, whole or not at
// all, then deletes the older cache files of its game and install folder and sets the install's
// Previous to the version the first held; the file itself holds none.
func SaveInstall(req Request, install *Install) error {
	path, ofFolder, err := cachePath(req)
	if err != nil {
		return err
	}
	err = local.Write(path, func(out io.Writer) error {
		zip := gzip.NewWriter(out)
		if err := gob.NewEncoder(zip).Encode(install); err != nil {
			return err
		}
		return zip.Close()
	})
	if err != nil {
		return err
	}
	install.Previous, err = forget(ofFolder, path)
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
	unzip, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer unzip.Close()
	install := &Install{}
	if err := gob.NewDecoder(unzip).Decode(install); err != nil {
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
