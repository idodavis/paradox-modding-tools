// scan.go is the three ways records come to be: a scan of a game install, which also learns its
// schema; the build of a workspace's mods over an install; and one file read again after an edit.
// Each reads files with a reader per CPU and adds their records in inventory order, because load
// order decides winners.

package index

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/learn"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Install is everything one scan of a game install produced. It is what the cache holds.
type Install struct {
	Index  *Index
	Schema *schema.Schema
	Vocab  *schema.Vocab
	Info   schema.Info
}

// Request is what a scan is asked for.
type Request struct {
	Game     string   // the registry's id
	Dir      string   // the game's root folder
	Version  string   // the install's version, for the schema's header
	Docs     []string // the folders the game's script_docs dumps may be in
	Language string   // the workspace's language, read beside english; "" for english alone

	// Progress hears each file read or step taken, by stage: reading localization, reading the
	// install, learning the schema, reading definitions, reading references; nil for none
	Progress func(stage string, done, total int)
}

// Mod is one mod of a workspace, in the playset's order.
type Mod struct{ ID, Dir string }

// replaced is the folders a mod replaces, lower-cased and slash separated: replace_path lines in a
// CK3 descriptor, game_custom_data.replace_paths in a Victoria 3 or EU5 metadata.json.
func replaced(dir string) []string {
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
	if raw, err := os.ReadFile(metadataPath); err == nil && json.Unmarshal(raw, &metadata) == nil {
		out = append(out, metadata.Custom.ReplacePaths...)
	}
	for i, folder := range out {
		out[i] = strings.Trim(strings.ToLower(filepath.ToSlash(folder)), "/")
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
				if Readable(files[i].Format) {
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
// localization for all, then, with those behind Found, references and edges, in script and in
// localization (a $key$ a value interpolates).
func harvested(
	ctx context.Context,
	idx *Index,
	files []source.File,
	reader schema.Reader,
	found schema.Defined,
	progress func(stage string, done, total int),
) error {
	reader.Found = nil
	if err := each(ctx, files, reader, idx.Add, progress, "reading definitions"); err != nil {
		return err
	}

	idx.Ready()
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
// learned from all of it, and then every definition, reference and edge.
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

	// localization first: the learner asks which words are loc keys
	keys := map[string]bool{}
	locs := wanted(files, req.Language, true)
	if err := each(ctx, locs, schema.Reader{Schema: &schema.Schema{}, Vocab: &schema.Vocab{}}, func(records *Records) {
		for _, entry := range records.Loc {
			keys[entry.Key] = true
		}
	}, req.Progress, "reading localization"); err != nil {
		return nil, err
	}

	// what the game says of itself, and the schema learned from its files
	vocab := schema.ReadVocab(req.Docs)
	docs, unread := schema.ReadInfo(files, vocab)
	var modes []string
	for _, entryMode := range gameInfo.EntryModes {
		modes = append(modes, entryMode.Word)
	}
	sch, err := learn.Learn(ctx, learn.Input{
		Game: req.Game, Version: req.Version, Files: files, Vocab: vocab, Info: docs, Loc: loc.NewKeys(keys),
		EntryModes: modes, Subjects: gameInfo.SubjectDirs, Progress: req.Progress,
	})
	if err != nil {
		return nil, err
	}
	sch.Drift.DocLines = unread

	// every file has its place in the inventory, in load order, whether or not it is read
	idx.Files = files
	reader := schema.Reader{Schema: sch, Vocab: vocab}
	err = harvested(ctx, idx, append(locs, wanted(files, "", false)...), reader, All{idx}, req.Progress)
	idx.shared = nil
	return &Install{Index: idx, Schema: sch, Vocab: vocab, Info: docs}, err
}

// BuildWorkspace reads a workspace's mods over an install: what their files add to the install's
// schema, and their own records, whose references may name anything of the install. Every loc file
// of a mod is read, whatever its language: health reports each language's gaps.
func BuildWorkspace(ctx context.Context, mods []Mod, install *Install) (*Index, *schema.Schema, error) {
	idx := &Index{owned: map[int32]*owns{}}
	for _, mod := range mods {
		roots := source.Roots(mod.Dir)
		idx.Roots = append(idx.Roots, roots...)
		idx.Files = append(idx.Files, source.Inventory(mod.ID, roots)...)
		idx.Replaced = append(idx.Replaced, replaced(mod.Dir)...)
	}
	files := idx.Files

	keys := map[string]bool{}
	for _, entry := range install.Index.Loc {
		keys[entry.Key] = true
	}
	input := learn.Input{
		Game: install.Schema.Game, Version: install.Schema.Version, Files: files, Vocab: install.Vocab,
		Info: install.Info, Loc: loc.NewKeys(keys),
	}
	sch, err := learn.Relearn(ctx, input, install.Schema)
	if err != nil {
		return nil, nil, err
	}

	// the harvest reads a copy: adding a file's records writes back into idx.Files
	reader := schema.Reader{Schema: sch, Vocab: install.Vocab}
	return idx, sch, harvested(ctx, idx, slices.Clone(files), reader, All{idx, install.Index}, nil)
}

// IndexFile reads one file of a live index again: what it added is taken back, and it is harvested
// as a scan would, its own definitions first.
func (idx *Index) IndexFile(file source.File, text string, reader schema.Reader) {
	idx.Drop(file.Path)
	found := reader.Found
	reader.Found = nil
	idx.Add(Harvest(file, text, reader))
	reader.Found = found
	refs := Harvest(file, text, reader)
	refs.Defs, refs.Loc = nil, nil
	idx.Add(refs)
}
