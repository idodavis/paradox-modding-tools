// scan.go is the three ways records come to be: a scan of a game install, which also learns its schema; the build of a workspace's mods over an install; and one file read again after an edit. Every one reads files with a reader per CPU and takes the results back in the inventory's order, because a first read of a file costs milliseconds of waiting whatever its size and load order decides winners, so scheduling may decide nothing.
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
	"paradox-modding-tools/services/internal/source"
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
}

// Mod is one mod of a workspace, in the playset's order.
type Mod struct{ ID, Dir string }

// replaced is the folders a mod replaces: replace_path lines in a CK3 descriptor, game_custom_data.replace_paths in a Victoria 3 or EU5 metadata.json. Lower-cased, slash separated.
func replaced(dir string) []string {
	var out []string
	if raw, err := os.ReadFile(filepath.Join(dir, "descriptor.mod")); err == nil {
		text, _ := jomini.Decode(raw)
		for _, st := range jomini.Parse(text).Root.Statements {
			if a, ok := st.(*jomini.Assignment); ok && a.Key.Text == "replace_path" {
				if folder, ok := a.Value.(*jomini.Scalar); ok {
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
	if raw, err := os.ReadFile(filepath.Join(dir, ".metadata", "metadata.json")); err == nil && json.Unmarshal(raw, &metadata) == nil {
		out = append(out, metadata.Custom.ReplacePaths...)
	}
	for i, folder := range out {
		out[i] = strings.Trim(strings.ToLower(filepath.ToSlash(folder)), "/")
	}
	return out
}

// each harvests the files with a reader per CPU and hands the results to keep in the order of files.
func each(ctx context.Context, files []source.File, r schema.Reader, keep func(*Records)) error {
	type read struct {
		i int
		h *Records
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				text := ""
				if Readable(files[i].Format) {
					text, _ = files[i].Text()
				}
				out <- read{i, Harvest(files[i], text, r)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	// keep each file when its turn comes; only this loop touches what is waiting
	waiting, turn := map[int]*Records{}, 0
	for got := range out {
		waiting[got.i] = got.h
		for h := waiting[turn]; h != nil; h = waiting[turn] {
			keep(h)
			delete(waiting, turn)
			turn++
			<-window
		}
	}
	return ctx.Err()
}

// wanted is the files of a language the scan reads, and every file that is no localization.
func wanted(files []source.File, language string, locs bool) []source.File {
	var out []source.File
	for _, f := range files {
		spoken := loc.LanguageFromRel(f.Rel)
		if isLoc := f.Format == source.Loc; isLoc == locs && (!isLoc || spoken == "english" || (language != "" && spoken == language)) {
			out = append(out, f)
		}
	}
	return out
}

// harvested fills an index from files in the two walks the harvest needs: definitions and localization for all, then, with those behind Found, references and edges, in script and in localization (a $key$ a value interpolates).
func harvested(ctx context.Context, x *Index, files []source.File, r schema.Reader, found schema.Defined) error {
	r.Found = nil
	if err := each(ctx, files, r, x.Add); err != nil {
		return err
	}

	x.Ready()
	r.Found = found
	scripts := slices.DeleteFunc(slices.Clone(files), func(f source.File) bool { return f.Format != source.Script && f.Format != source.Loc })
	return each(ctx, scripts, r, func(h *Records) {
		h.Defs, h.Loc = nil, nil
		x.Add(h)
	})
}

// Scan reads a game install: its files, its localization, what the game says of itself, the schema learned from all of it, and then every definition, reference and edge.
func Scan(ctx context.Context, req Request) (*Install, error) {
	g := game.Get(req.Game)
	if g == nil {
		return nil, fmt.Errorf("unknown game %q", req.Game)
	}
	x := &Index{Roots: source.Roots(req.Dir)}
	files := source.Inventory(game.OriginVanilla, x.Roots)
	if !slices.ContainsFunc(files, func(f source.File) bool { return f.Format == source.Script }) {
		return nil, fmt.Errorf("no script files under %s: check the install path", req.Dir)
	}

	// localization first: the learner asks which words are loc keys
	keys := map[string]bool{}
	locs := wanted(files, req.Language, true)
	if err := each(ctx, locs, schema.Reader{Schema: &schema.Schema{}, Vocab: &schema.Vocab{}}, func(h *Records) {
		for _, entry := range h.Loc {
			keys[entry.Key] = true
		}
	}); err != nil {
		return nil, err
	}

	// what the game says of itself, and the schema learned from its files
	in := &Install{Index: x, Vocab: schema.ReadVocab(req.Docs), Info: schema.ReadInfo(files)}
	var modes []string
	for _, m := range g.EntryModes {
		modes = append(modes, m.Word)
	}
	s, err := learn.Learn(ctx, learn.Input{Game: req.Game, Version: req.Version, Files: files, Vocab: in.Vocab, Info: in.Info, Loc: loc.NewKeys(keys), EntryModes: modes, Subjects: g.SubjectDirs})
	if err != nil {
		return nil, err
	}
	in.Schema = s

	// every file has its place in the inventory, in load order, whether or not anything is read from it
	x.Files = files
	err = harvested(ctx, x, append(locs, wanted(files, "", false)...), schema.Reader{Schema: s, Vocab: in.Vocab}, All{x})
	x.shared = nil
	return in, err
}

// BuildWorkspace reads a workspace's mods over an install: what the mods' files add to the install's schema, and the mods' own records, whose references may name anything of the install.
func BuildWorkspace(ctx context.Context, mods []Mod, install *Install, language string) (*Index, *schema.Schema, error) {
	x := &Index{owned: map[int32]*owns{}}
	for _, m := range mods {
		roots := source.Roots(m.Dir)
		x.Roots, x.Files, x.Replaced = append(x.Roots, roots...), append(x.Files, source.Inventory(m.ID, roots)...), append(x.Replaced, replaced(m.Dir)...)
	}
	files := x.Files

	keys := map[string]bool{}
	for _, entry := range install.Index.Loc {
		keys[entry.Key] = true
	}
	s, err := learn.Relearn(ctx, learn.Input{Game: install.Schema.Game, Version: install.Schema.Version, Files: files, Vocab: install.Vocab, Info: install.Info, Loc: loc.NewKeys(keys)}, install.Schema)
	if err != nil {
		return nil, nil, err
	}
	read := append(wanted(files, language, true), wanted(files, "", false)...)
	return x, s, harvested(ctx, x, read, schema.Reader{Schema: s, Vocab: install.Vocab}, All{x, install.Index})
}

// IndexFile reads one file of a live index again: what it added is taken back, and it is harvested as a scan would, its own definitions first.
func (x *Index) IndexFile(f source.File, text string, r schema.Reader) {
	x.Drop(f.Path)
	found := r.Found
	r.Found = nil
	x.Add(Harvest(f, text, r))
	r.Found = found
	refs := Harvest(f, text, r)
	refs.Defs, refs.Loc = nil, nil
	x.Add(refs)
}
