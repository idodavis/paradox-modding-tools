// scan.go is the three ways records come to be: a scan of a game install, which also builds its
// schema; the build of a workspace's mods over an install; and one file read again after an edit.
// Each reads files with a reader per CPU and adds their records in inventory order, because load
// order decides winners.

package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Install is everything one scan of a game install produced. It is what the cache holds.
type Install struct {
	Index  *Index
	Schema *schema.Schema
}

// Request is what a scan is asked for.
type Request struct {
	Game     string   // the registry's id
	Dir      string   // the game's root folder
	Version  string   // the install's version
	Dumps    []string // the folders the game's script_docs dumps may be in
	Wiki     string   // the wiki sidecar's path; the shipped wiki where it does not exist
	Language string   // the workspace's language, read beside english; "" for english alone

	// Progress hears each file read or step taken, by stage: reading localization, reading the
	// install, reading definitions, reading references; nil for none
	Progress func(stage string, done, total int)
}

// Mod is one mod of a workspace, in the playset's order.
type Mod struct{ ID, Dir string }

// replaced is the folders a mod replaces, lower-cased and slash separated, a leading stage folder
// dropped (EU5's main_menu/setup/start): replace_path lines in a CK3 descriptor,
// game_custom_data.replace_paths in a Victoria 3 or EU5 metadata.json, whose byte-order mark goes.
func replaced(dir string, stages []string) []string {
	var out []string
	if raw, err := os.ReadFile(filepath.Join(dir, "descriptor.mod")); err == nil {
		text, _ := jomini.Decode(raw)
		for _, st := range jomini.Parse(text).Root.Statements {
			if assignment, ok := st.(*jomini.Assignment); ok && assignment.Key.Text == "replace_path" {
				if folder, ok := assignment.Value.(*jomini.Scalar); ok {
					out = append(out, folder.Text)
				}
			}
		}
	}
	var metadata struct {
		Custom struct {
			ReplacePaths []string `json:"replace_paths"`
		} `json:"game_custom_data"`
	}
	metadataPath := filepath.Join(dir, ".metadata", "metadata.json")
	if raw, err := os.ReadFile(metadataPath); err == nil && json.Unmarshal(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}), &metadata) == nil {
		out = append(out, metadata.Custom.ReplacePaths...)
	}
	for i, folder := range out {
		folder = strings.Trim(strings.ToLower(filepath.ToSlash(folder)), "/")
		for _, stage := range stages {
			if rest, ok := strings.CutPrefix(folder, stage+"/"); ok {
				folder = rest
				break
			}
		}
		out[i] = folder
	}
	return out
}

// each harvests the files with a reader per CPU and hands the results to keep in file order,
// telling progress, where there is one, of each under stage.
func each(
	ctx context.Context,
	files []source.File,
	reader schema.Reader,
	keep func(*Records),
	progress func(stage string, done, total int),
	stage string,
) error {
	type read struct {
		i       int
		records *Records
	}
	workers := runtime.NumCPU()
	window, jobs, out := make(chan struct{}, workers*4), make(chan int), make(chan read, workers*4)
	go func() {
		defer close(jobs)
		for i := range files {
			select {
			case window <- struct{}{}:
				jobs <- i
			case <-ctx.Done():
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				text := ""
				if Readable(files[i], reader.Schema) {
					text, _ = files[i].Text()
				}
				out <- read{i, Harvest(files[i], text, reader)}
			}
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	// keep each file when its turn comes; only this loop touches what is waiting
	waiting, turn := map[int]*Records{}, 0
	for got := range out {
		waiting[got.i] = got.records
		for records := waiting[turn]; records != nil; records = waiting[turn] {
			keep(records)
			delete(waiting, turn)
			turn++
			<-window
			if progress != nil {
				progress(stage, turn, len(files))
			}
		}
	}
	return ctx.Err()
}

// wanted is the files of a language the scan reads, and every file that is no localization.
func wanted(files []source.File, language string, locs bool) []source.File {
	var out []source.File
	for _, file := range files {
		spoken := loc.LanguageFromRel(file.Rel)
		isLoc := file.Format == source.Loc
		inLanguage := !isLoc || spoken == "english" || (language != "" && spoken == language)
		if isLoc == locs && inLanguage {
			out = append(out, file)
		}
	}
	return out
}

// harvested fills an index from files in the two walks the harvest needs: definitions and
// localization for all, then settle with the names each database defines, then, with those behind
// Found, references and edges, in script and in localization (a $key$ a value interpolates).
func harvested(
	ctx context.Context,
	idx *Index,
	files []source.File,
	reader schema.Reader,
	found schema.Defined,
	progress func(stage string, done, total int),
	settle func(names map[string][]string),
) error {
	reader.Found = nil
	if err := each(ctx, files, reader, idx.Add, progress, "reading definitions"); err != nil {
		return err
	}

	idx.Ready()
	names := map[string][]string{}
	for _, def := range idx.Defs {
		names[def.Database] = append(names[def.Database], def.Name)
	}
	settle(names)
	reader.Found = found
	scripts := slices.DeleteFunc(slices.Clone(files), func(file source.File) bool {
		return file.Format != source.Script && file.Format != source.Loc
	})
	return each(ctx, scripts, reader, func(records *Records) {
		records.Defs, records.Loc = nil, nil
		idx.Add(records)
	}, progress, "reading references")
}

// Scan reads a game install: its files, its localization, what the game says of itself, the schema
// built from all of it, and then every definition, reference and edge.
func Scan(ctx context.Context, req Request) (*Install, error) {
	gameInfo := game.Get(req.Game)
	if gameInfo == nil {
		return nil, fmt.Errorf("unknown game %q", req.Game)
	}
	idx := &Index{Roots: source.Roots(req.Dir)}
	files := source.Inventory(game.OriginVanilla, idx.Roots)
	if !slices.ContainsFunc(files, func(file source.File) bool { return file.Format == source.Script }) {
		return nil, fmt.Errorf("no script files under %s: check the install path", req.Dir)
	}

	// localization first: the build asks which words are loc keys
	keys := map[string]bool{}
	locs := wanted(files, req.Language, true)
	if err := each(ctx, locs, schema.Reader{Schema: &schema.Schema{Dumps: &schema.Dumps{}}}, func(records *Records) {
		for _, entry := range records.Loc {
			keys[entry.Key] = true
		}
	}, req.Progress, "reading localization"); err != nil {
		return nil, err
	}

	// what the game says of itself, each dump set the user's where it is not older than the install,
	// else the shipped one; the wiki the sidecar where there is one, else the shipped one
	if req.Progress != nil {
		req.Progress("reading the install", 0, 1)
	}
	shipped, hasShipped := schema.ShippedLayers(req.Game)
	yours, installed := schema.ReadDumps(req.Dumps), game.InstallUpdatedAt(req.Dir)
	pick := func(written time.Time) (*schema.Dumps, string) {
		switch {
		case !written.IsZero() && !written.Before(installed):
			return yours, "yours"
		case hasShipped && shipped.Dumps != nil:
			return shipped.Dumps, "shipped"
		}
		return &schema.Dumps{}, "none"
	}
	dumps, scriptDocsFrom := pick(yours.ScriptDocsWritten)
	dataTypes, dataTypesFrom := pick(yours.DataTypesWritten)
	if dataTypes != dumps {
		dumps = &schema.Dumps{
			Effects: dumps.Effects, Triggers: dumps.Triggers, Modifiers: dumps.Modifiers, EventTargets: dumps.EventTargets,
			DataTargets: dumps.DataTargets, Scopes: dumps.Scopes, OnActions: dumps.OnActions, CustomLocalization: dumps.CustomLocalization,
			SavedScopes: dumps.SavedScopes, Unknown: dumps.Unknown, ScriptDocsWritten: dumps.ScriptDocsWritten,
			DataTypes: dataTypes.DataTypes, Globals: dataTypes.Globals, DataTypesWritten: dataTypes.DataTypesWritten,
		}
	}
	scripts := schema.ReadScripts(files, gameInfo)
	docs, unread := schema.ReadDocs(files, scripts, dumps)
	wiki, wikiUnread := schema.ReadWiki(req.Wiki, scripts, dumps)
	if _, err := os.Stat(req.Wiki); err != nil && hasShipped {
		wiki, wikiUnread = shipped.Wiki, nil
	}
	built := schema.Build(schema.Input{Game: gameInfo, Version: req.Version, Files: files, Scripts: scripts, Dumps: dumps, Docs: docs, Wiki: wiki})
	built.DumpsFrom = schema.DumpsFrom{ScriptDocs: scriptDocsFrom, DataTypes: dataTypesFrom}
	if scriptDocsFrom == "shipped" || dataTypesFrom == "shipped" {
		built.DumpsVersion = shipped.GameVersion
	}
	built.Unread.DocLines = append(unread, wikiUnread...)
	if req.Progress != nil {
		req.Progress("reading the install", 1, 1)
	}

	// every file has its place in the inventory, in load order, whether or not it is read
	idx.Files = files
	err := harvested(ctx, idx, append(locs, wanted(files, "", false)...), schema.Reader{Schema: built}, All{idx}, req.Progress, func(names map[string][]string) {
		schema.Settle(built, names, loc.NewKeys(keys), files, scripts)
	})
	idx.shared = nil
	return &Install{Index: idx, Schema: built}, err
}

// BuildWorkspace reads a workspace's mods over an install: the databases their files add to the
// install's schema, read with the install's dumps, docs and wiki, and their own records, whose
// references may name anything of the install. Every loc file of a mod is read, whatever its
// language: health reports each language's gaps.
func BuildWorkspace(ctx context.Context, mods []Mod, install *Install) (*Index, *schema.Schema, error) {
	base, gameInfo := install.Schema, game.Get(install.Schema.Game)
	idx := &Index{owned: map[int32]*owns{}}
	for _, mod := range mods {
		roots := source.Roots(mod.Dir)
		idx.Roots = append(idx.Roots, roots...)
		idx.Files = append(idx.Files, source.Inventory(mod.ID, roots)...)
		idx.Replaced = append(idx.Replaced, replaced(mod.Dir, gameInfo.StageRoots)...)
	}
	files := idx.Files
	scripts := schema.ReadScripts(files, gameInfo)
	built := schema.Build(schema.Input{
		Game: gameInfo, Version: base.Version, Files: files, Scripts: scripts, Dumps: base.Dumps, Docs: base.Docs, Wiki: base.Wiki, Base: base,
	})

	// the harvest reads a copy: adding a file's records writes back into idx.Files
	return idx, built, harvested(ctx, idx, slices.Clone(files), schema.Reader{Schema: built}, All{idx, install.Index}, nil, func(names map[string][]string) {
		keys := map[string]bool{}
		for _, entries := range [][]Loc{install.Index.Loc, idx.Loc} {
			for _, entry := range entries {
				keys[entry.Key] = true
			}
		}
		schema.Settle(built, names, loc.NewKeys(keys), files, scripts)
	})
}

// IndexFile reads one file of a live index again: what it added is taken back, and it is harvested
// as a scan would, its own definitions first.
func (idx *Index) IndexFile(file source.File, text string, reader schema.Reader) {
	idx.Drop(file.Path, false)
	found := reader.Found
	reader.Found = nil
	idx.Add(Harvest(file, text, reader))
	reader.Found = found
	refs := Harvest(file, text, reader)
	refs.Defs, refs.Loc = nil, nil
	idx.Add(refs)
}
