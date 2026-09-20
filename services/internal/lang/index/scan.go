// scan.go walks a game install or a workspace's mods into an Index: gather,
// localization, the shipped prose, then the three passes over every script
// file -- definitions, the fold into the schema tree, resolution -- with the
// nested join between the first two. A workspace applies the install's tree
// instead of learning one; an edited file runs the same three passes alone.

package index

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

const scanConcurrency = 8

// ScanRequest is the install harvest input. Declared, when set, is the type system to use instead of reading script_docs.
type ScanRequest struct {
	InstallID, GameID, InstallPath, Version, LocLang string
	OnProgress                                       func(pct int, msg string)
	Declared                                         *declared.Schema
}

// Scan harvests installPath into an Index, with the data-row tier split into a second one.
func Scan(ctx context.Context, req ScanRequest) (*Index, *Index, error) {
	info := game.Get(req.GameID)
	if info == nil {
		return nil, nil, os.ErrInvalid
	}
	locLang := cmp.Or(req.LocLang, "english")
	step := req.OnProgress
	if step == nil {
		step = func(int, string) {}
	}
	step(5, "listing files")
	inv := gather(scriptRoots(info, req.InstallPath))

	// A game whose script root holds nothing is the wrong folder, not an empty game.
	if len(inv.script) == 0 {
		return nil, nil, fmt.Errorf("no script files under %s: check the install path", req.InstallPath)
	}
	docDirs := scriptDocsDirs(req.GameID, req.InstallPath)
	decl := req.Declared
	if decl == nil {
		decl = declared.ReadSchema(docDirs)
	}
	x := NewIndex(req.GameID, decl)
	x.InstallID, x.InstallPath = req.InstallID, req.InstallPath
	x.GameVersion = CacheVersion(req.Version)
	x.ScannedAt = time.Now().UTC().Format(time.RFC3339)

	// Localization first: a node's type is the union of what its values resolve to, and a loc key is one of the answers.
	step(15, "reading localization")
	locRefs, err := harvestLoc(ctx, x, inv.loc, locLang)
	if err != nil {
		return nil, nil, err
	}
	x.Tree.LocEngineSlots = locEngineSlots(x.Loc[locLang], locRefs)
	for _, r := range locRefs {
		if isLocKind(r.Kind) {
			x.Add(&Harvest{Path: r.Path, Kind: "loc", Refs: []Ref{r}})
		}
	}
	step(25, "reading game info")
	docs, docFields := harvestGameInfo(req.GameID, inv.info)
	x.Docs = docs

	step(30, "parsing script")
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	dataFns, err := x.learn(ctx, files, nil, locLang, docFields)
	if err != nil {
		return nil, nil, err
	}
	step(90, "reading script_docs")
	x.Tree.DataFunctions = dataFunctions(docDirs, dataFns)
	keys := loc.NewKeys(keySet(x.Loc[locLang]))
	x.Tree.LocKeyPrefixes = loc.LearnKeyPrefixes(keys)
	byKind := map[string]map[string]bool{}
	x.citable(map[string][]string{}, byKind)
	for kind, ks := range byKind {
		// IsObjectKind, not IsCitableDef: a namespace is an event-id prefix with no loc of its own.
		if root := x.Tree.Root(kind, false); root != nil && jomini.IsObjectKind(kind) {
			root.Loc = loc.Learn(slices.Collect(maps.Keys(ks)), root.Members(), keys)
		}
	}
	x.Tree.Nested = x.Tree.Shapes()
	x.compact()
	data := splitData(x)

	// The scanning process reads its own rows without a round trip through the cache.
	x.data = data
	x.SortLocKeys()
	step(100, "done")
	return x, data, nil
}

// BuildWorkspace harvests every mod in load order into a workspace index over the install's, applying the install's schema rather than learning one: mods add members of kinds, not kinds or fields.
func BuildWorkspace(ctx context.Context, gameID string, mods []ModInput, base *Index) (*Index, error) {
	ordered := slices.Clone(mods)
	slices.SortStableFunc(ordered, func(a, b ModInput) int { return cmp.Compare(a.Order, b.Order) })
	x := NewIndex(gameID, base.Declared)
	x.Docs = base.Docs
	x.Tree.base, x.Tree.LocEngineSlots = &base.Tree, base.Tree.LocEngineSlots
	x.Tree.Macros = maps.Clone(base.Tree.Macros)
	var files []fileRef
	for _, m := range ordered {
		x.Order = append(x.Order, m.Origin)
		for _, f := range modFiles(m.Root) {
			f.origin = m.Origin
			files = append(files, f)
		}
	}
	_, err := x.learn(ctx, files, base, "", nil)
	x.dropEngineSlotRefs()
	x.SortLocKeys()
	return x, err
}

// learn is the corpus pass: the three walks over every file, with the join between the first two. base is the install for a workspace; locLang is set only for an install scan.
func (x *Index) learn(ctx context.Context, files []fileRef, base *Index, locLang string, docFields map[string]map[string]bool) (map[string]bool, error) {
	install := base == nil
	decl := x.Declared
	fs := make([]*File, len(files))
	hs := make([]*Harvest, len(files))
	sks := make([]*skel, len(files))
	dataFns := map[string]bool{}
	var mu sync.Mutex

	// Pass 1: definitions and the rows that need no global knowledge.
	err := walkN(ctx, len(files), func(i int) error {
		f, text, ok := x.open(files[i], i)
		if !ok {
			return nil
		}
		if f.Rule.Mode == game.ModeLocKey {
			hs[i] = ApplyLoc(f, text)
			return nil
		}
		f.Parse(text, jomini.Result{})
		hs[i], sks[i] = f.defs(decl)
		if install {
			fns := harvestGetSet(f.Src)
			mu.Lock()
			maps.Copy(dataFns, fns)
			mu.Unlock()
		}
		f.Facts, f.Src, f.names, f.empty, f.litList = nil, "", nil, nil, nil
		fs[i] = f
		return nil
	})
	if err != nil {
		return dataFns, err
	}
	for _, h := range hs {
		if h != nil {
			x.Add(h)
		}
	}
	kindsByKey, keysByKind := x.citableWith(base)
	if install {
		x.Tree.bind(decl, keysByKind)
	}
	if x.Tree.Macros == nil {
		x.Tree.Macros = map[string]string{}
	}
	x.macros(x.Tree.Macros)

	// The guide the fold reads database marks from: a bootstrap tree the join marks for an install, the install's own tree for a workspace.
	var guide *Tree
	shapes := map[[2]string]bool{}
	for _, sk := range sks {
		if sk != nil {
			maps.Copy(x.cites, sk.cites)
		}
	}
	cited := citedKinds(x.cites, x.Tree.prefixMap(), decl)
	if install {
		guide = x.boot(fs, sks, kindsByKey, cited)
	} else {
		guide = &base.Tree
		for _, sh := range base.Tree.Shapes() {
			shapes[[2]string{sh.ParentKind, sh.ChildKind}] = true
		}
	}
	for kind, root := range guide.Kinds {
		root.eachDef(func(def string) { shapes[[2]string{kind, def}] = true })
	}
	for i, sk := range sks {
		if sk == nil {
			continue
		}
		if defs := fs[i].nested(sk, guide, cited, shapes); len(defs) > 0 {
			x.Add(&Harvest{Path: fs[i].Path, Rel: fs[i].Rel, Origin: fs[i].Origin, Kind: fs[i].Kind, Defs: defs})
		}
	}

	// Row 7: a wrapper key is not a definition, and a site under one belongs to no definition.
	wrappers := map[string]bool{}
	for _, sk := range sks {
		if sk == nil {
			continue
		}
		for key, ids := range sk.wraps {
			if !slices.ContainsFunc(ids, func(id string) bool { return len(kindsByKey[id]) == 0 }) {
				wrappers[key] = true
			}
		}
	}
	if len(wrappers) > 0 {
		x.retract(func(d Def) bool { return wrappers[d.Key] })
	}

	// The nested definitions joined; the fold and the resolution see them.
	kindsByKey, keysByKind = x.citableWith(base)
	if install {
		x.Tree.bind(decl, keysByKind)
	}
	r := &resolver{tree: &x.Tree, kindsByKey: kindsByKey, locKeys: locKeys(x, base, locLang)}

	// Pass 2: each file into a tree of its own, merged in file order so two scans of one install agree.
	trees := make([]*Tree, len(files))
	err = walkN(ctx, len(files), func(i int) error {
		if fs[i] == nil {
			return nil
		}
		if f, text, ok := x.open(files[i], i); ok {
			trees[i] = f.Parse(text, jomini.Result{}).fold(guide, r, install)
		}
		return nil
	})
	if err != nil {
		return dataFns, err
	}
	for i, tree := range trees {
		if tree == nil {
			continue
		}
		kids := x.Tree.merge(tree, nil)
		if !install {
			x.nodesByFile[x.PathID(fs[i].Path)] = kids
		}
	}
	for _, root := range x.Tree.Kinds {
		root.settle()
	}

	// A documented field is a member of the structure it is written for, whether or not vanilla writes it.
	for structure, keys := range docFields {
		if root := x.Tree.Root(structure, false); root != nil {
			for key := range keys {
				x.Tree.Child(root, key, false, false, true).N++
			}
		}
	}
	x.Tree.blocks(docFields, install)
	if install {
		for _, kind := range game.Get(x.GameID).DataKinds {
			if r := x.Tree.Root(kind, false); r != nil {
				r.kind().Data = true
			}
		}
	}

	// Pass 3: references and edges, against the settled tree.
	settled := []*Tree{&x.Tree}
	if !install {
		settled = []*Tree{&base.Tree, &x.Tree}
	}
	out := make([]*Harvest, len(files))
	err = walkN(ctx, len(files), func(i int) error {
		if fs[i] == nil {
			return nil
		}
		f, text, ok := x.open(files[i], i)
		if ok {
			out[i] = f.Parse(text, jomini.Result{}).resolve(settled, r, decl, wrappers)
		}
		return nil
	})
	for _, h := range out {
		if h != nil {
			x.Add(h)
		}
	}
	return dataFns, err
}

// boot builds the bootstrap tree the join marks: the block skeleton of every file, with any script definition's name filed as a row, then the local-database rule and the nested join.
func (x *Index) boot(fs []*File, sks []*skel, kindsByKey map[string][]string, cited map[string]string) *Tree {
	scriptDefs := map[string]bool{}
	for key, kinds := range kindsByKey {
		if slices.ContainsFunc(kinds, func(k string) bool { return !game.IsDataKind(x.GameID, k) }) {
			scriptDefs[key] = true
		}
	}
	boot := &Tree{gameID: x.GameID, decl: x.Declared, byName: func(name string) bool { return scriptDefs[name] }}
	local := map[[2]string][2]int32{}
	for i, sk := range sks {
		if sk == nil {
			continue
		}
		root := boot.Root(fs[i].Kind, true)
		root.Flags |= Names
		hang := make([]*Node, len(sk.blks))
		for b, bl := range sk.blks {
			p := root
			if bl.parent >= 0 {
				p = hang[bl.parent]
			}
			name := boot.name(bl.key, bl.quoted, true, p.Flags&Rows != 0)
			n := p.Kids[name]
			if n == nil {
				if p.Kids == nil {
					p.Kids = map[string]*Node{}
				}
				n = &Node{}
				p.Kids[strings.Clone(name)] = n
			}
			if bl.names {
				n.Flags |= Names
			}
			hang[b] = n
		}
		for key, c := range sk.local {
			at := [2]string{fs[i].Kind, key}
			local[at] = [2]int32{local[at][0] + c[0], local[at][1] + c[1]}
		}
	}

	// A definition-body field whose every value names one of the definition's own block keys says those keys are rows of a local database: a game rule's `default` names one of its options.
	for at, c := range local {
		if c[0] > 0 && c[0] == c[1] {
			boot.Root(at[0], true).Flags |= Rows
		}
	}
	boot.join(cited, func(name string) bool { return scriptDefs[name] })
	return boot
}

// eachDef visits every nested kind marked below a node.
func (n *Node) eachDef(fn func(def string)) {
	if n.Def != "" {
		fn(n.Def)
	}
	for _, c := range n.Kids {
		c.eachDef(fn)
	}
}

// IndexFile runs the three passes over one edited workspace file. An install file open in the editor keeps only its live call and `$NAME$` sites.
func (x *Index) IndexFile(f *File, base *Index) {
	decl := base.Declared
	vanilla := f.Origin == ""
	if f.Rule.Mode == game.ModeLocKey {
		if !vanilla {
			x.Add(ApplyLoc(f, f.Src))
		}
		return
	}
	h, sk := f.defs(decl)
	if vanilla {
		h.Defs, h.Loc = nil, LocDelta{}
		h.Refs = slices.DeleteFunc(h.Refs, func(r Ref) bool {
			return r.Kind != jomini.KindScriptParam && !jomini.IsMacroKind(r.Kind)
		})
		x.Add(h)
		return
	}
	x.Add(h)
	for _, d := range h.Defs {
		if k := jomini.CanonicalKind(d.Kind); jomini.IsMacroKind(k) {
			x.Tree.Macros[d.Key] = k
		}
	}
	shapes := map[[2]string]bool{}
	for _, sh := range base.Tree.Shapes() {
		shapes[[2]string{sh.ParentKind, sh.ChildKind}] = true
	}
	maps.Copy(x.cites, sk.cites)
	cited := citedKinds(x.cites, base.Tree.prefixMap(), decl)
	if defs := f.nested(sk, &base.Tree, cited, shapes); len(defs) > 0 {
		x.Add(&Harvest{Path: f.Path, Rel: f.Rel, Origin: f.Origin, Kind: f.Kind, Defs: defs})
	}
	kindsByKey, _ := x.citableWith(base)
	r := &resolver{tree: &x.Tree, kindsByKey: kindsByKey, locKeys: locKeys(x, base, "")}
	tree := f.fold(&base.Tree, r, false)
	for _, root := range tree.Kinds {
		root.settle()
	}
	x.nodesByFile[x.PathID(f.Path)] = x.Tree.merge(tree, nil)
	x.Tree.blocks(nil, false)
	if h := f.resolve([]*Tree{&base.Tree, &x.Tree}, r, decl, nil); h != nil {
		x.Add(h)
	}
}

// open reads and decodes one file of the walk.
func (x *Index) open(fr fileRef, order int) (*File, string, bool) {
	raw, err := os.ReadFile(fr.abs)
	if err != nil {
		return nil, "", false
	}
	text, _ := jomini.Decode(raw)
	return NewFile(x.GameID, fr.abs, fr.rel, fr.origin, order), jomini.Normalize(text), true
}

// citableWith groups the definitions a fold and a resolution resolve against: this index's, and the install's underneath a workspace.
func (x *Index) citableWith(base *Index) (kindsByKey map[string][]string, keysByKind map[string]map[string]bool) {
	kindsByKey, keysByKind = make(map[string][]string, len(x.Defs)/2), map[string]map[string]bool{}
	x.citable(kindsByKey, keysByKind)
	if base != nil {
		base.citable(kindsByKey, keysByKind)
	}
	return kindsByKey, keysByKind
}

// locKeys is the localization a value is classified against: the install's for a scan, the install plus the workspace's own for a mod build.
func locKeys(x, base *Index, locLang string) map[string]bool {
	lang := locLang
	if lang == "" {
		lang = x.defaultLocLang()
		if base != nil && len(base.Loc) > 0 {
			lang = base.defaultLocLang()
		}
	}
	out := keySet(x.Loc[lang])
	if base != nil {
		for k := range base.Loc[lang] {
			out[k] = true
		}
	}
	return out
}

// defaultLocLang is the language the harvest read refs for; english is the fallback every game ships.
func (x *Index) defaultLocLang() string {
	for lang := range x.Loc {
		if lang != "english" {
			return lang
		}
	}
	return "english"
}

func keySet(m map[string]LocEntry) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// dropEngineSlotRefs removes loc references to interpolations the engine fills in: not a missing key, and no mod can define one.
func (x *Index) dropEngineSlotRefs() {
	slots := x.Tree.LocEngineSlots
	if len(slots) == 0 {
		return
	}
	dropped := false
	for i := range x.Refs {
		if r := &x.Refs[i]; x.KindOf(r.KindID) == "loc" && slots[r.Key] && r.PathID >= 0 {
			r.PathID, dropped = -1, true
		}
	}
	if dropped {
		x.reindex()
	}
}

// ------------------------------------------------------------------ the walk

func scriptDocsDirs(gameID, installPath string) []string {
	info := game.Get(gameID)
	if info == nil {
		return nil
	}
	ud := game.UserDataDir(gameID, installPath)
	if ud == "" {
		return nil
	}
	var out []string
	for _, sub := range []string{info.ScriptDocsSubdir, "docs", "logs"} {
		if p := filepath.Join(ud, sub); sub != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// SchemaStatus is what the Workspace Health strip needs to know about the declared type system, read without loading the model.
type SchemaStatus struct {
	Source     declared.SchemaSource `json:"source"`
	Effects    int                   `json:"effects"`
	Triggers   int                   `json:"triggers"`
	ScopeTypes int                   `json:"scopeTypes"`
	Prefixes   int                   `json:"prefixes"`
	OnActions  int                   `json:"onActions"`
	ReadAt     string                `json:"readAt,omitempty"`
	Archived   bool                  `json:"archived"`
	Folder     string                `json:"folder,omitempty"`
}

// ReadSchemaStatus reports the type system's state for one install, read live.
func ReadSchemaStatus(gameID, installPath string) SchemaStatus {
	dirs := scriptDocsDirs(gameID, installPath)
	st := SchemaStatus{Source: declared.SchemaMissing}
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			st.Folder = d
			break
		}
	}
	if st.Folder == "" && len(dirs) > 0 {
		st.Folder = dirs[0]
	}
	sc := declared.ReadSchema(dirs)
	if sc == nil {
		return st
	}
	st.Source = declared.SchemaLive
	st.Effects, st.Triggers = len(sc.Effects), len(sc.Triggers)
	st.ScopeTypes, st.OnActions = len(sc.Scopes), len(sc.OnActions)
	st.Prefixes, st.ReadAt = len(sc.Prefixes()), sc.ReadAt
	return st
}

func scriptRoots(info *game.GameInfo, installPath string) []string {
	if info.ScriptRoot != "" {
		return []string{filepath.Join(installPath, info.ScriptRoot)}
	}
	roots := make([]string, 0, len(info.StageRoots))
	for _, s := range info.StageRoots {
		roots = append(roots, filepath.Join(installPath, s))
	}
	return roots
}

// fileRef is a file plus its path relative to its script root.
type fileRef struct{ abs, rel, origin string }

type inventory struct{ script, gui, info, loc []fileRef }

// ClassifyRel buckets a file by name and relative path for the install walk and the mod indexer.
func ClassifyRel(rel, name string) string {
	lower := strings.ToLower(name)
	slash := strings.ReplaceAll(rel, "\\", "/")
	switch {
	case game.IsDocFile(name):
		return "info"
	case strings.HasSuffix(lower, ".gui"):
		return "gui"
	case strings.HasSuffix(lower, ".mod"):
		return "mod"
	case strings.HasSuffix(lower, ".yml"), strings.HasSuffix(lower, ".yaml"):
		if strings.Contains(slash, "localization") {
			return "loc"
		}
	case lower == game.MetadataFile:
		return "meta"
	case strings.HasSuffix(lower, ".txt") && !strings.HasPrefix(name, "_"):
		// Loose .txt at the script root is engine bookkeeping, not script.
		if strings.Contains(slash, "/") {
			return "script"
		}
	}
	return ""
}

func walkClassified(root string, fn func(kind string, f fileRef)) {
	WalkTree(root, func(p string, isDir bool) {
		if isDir {
			return
		}
		rel, _ := filepath.Rel(root, p)
		if kind := ClassifyRel(rel, filepath.Base(p)); kind != "" {
			fn(kind, fileRef{abs: filepath.Clean(p), rel: rel})
		}
	})
}

// WalkTree is WalkDir that also enters symlinked (or junctioned) directories, keeps paths spelled under root, and stops on a link back to an ancestor.
func WalkTree(root string, fn func(path string, isDir bool)) {
	walkTree(root, nil, fn)
}

func walkTree(dir string, ancestors []os.FileInfo, fn func(string, bool)) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return
	}
	for _, a := range ancestors {
		if os.SameFile(a, fi) {
			return
		}
	}
	ancestors = append(ancestors, fi)
	fn(dir, true)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.Type().IsRegular() {
			fn(p, false)
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			fn(p, false)
			continue
		}
		walkTree(p, ancestors, fn)
	}
}

func gather(roots []string) inventory {
	var inv inventory
	for _, root := range roots {
		walkClassified(root, func(kind string, f fileRef) {
			switch kind {
			case "info":
				inv.info = append(inv.info, f)
			case "gui":
				inv.gui = append(inv.gui, f)
			case "loc":
				inv.loc = append(inv.loc, f)
			case "script":
				inv.script = append(inv.script, f)
			}
		})
	}
	return inv
}

func modFiles(root string) []fileRef {
	var out []fileRef
	walkClassified(root, func(kind string, f fileRef) {
		switch kind {
		case "mod", "gui", "loc", "script":
			out = append(out, f)
		}
	})
	return out
}

// walkN runs fn(0..n-1) with scanConcurrency, cancelling siblings on error.
func walkN(ctx context.Context, n int, fn func(i int) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(scanConcurrency)
	for i := range n {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fn(i)
		})
	}
	return g.Wait()
}

// ------------------------------------------------------------- localization

// harvestLoc reads localization into the index, keeping only the workspace language and english.
func harvestLoc(ctx context.Context, x *Index, files []fileRef, locLang string) ([]Ref, error) {
	want := map[string]bool{locLang: true, "english": true}
	var mu sync.Mutex
	var refs []Ref
	err := walkN(ctx, len(files), func(i int) error {
		f := files[i]
		lang := loc.LanguageFromRel(f.abs)
		if lang == "" || !want[lang] {
			return nil
		}
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return nil
		}
		text, _ := jomini.Decode(raw)
		h := ApplyLoc(&File{GameID: x.GameID, Path: filepath.Clean(f.abs), Rel: f.rel, Order: i}, jomini.Normalize(text))
		mu.Lock()
		if h.Loc.Lang == locLang {
			refs = append(refs, h.Refs...)
		}
		h.Refs = nil
		x.Add(h)
		mu.Unlock()
		return nil
	})
	return refs, err
}

// HarvestLoc gathers install loc files for locLang without opening other languages.
func HarvestLoc(ctx context.Context, gameID, installPath, locLang string) (*Index, error) {
	info := game.Get(gameID)
	if info == nil {
		return nil, os.ErrInvalid
	}
	locLang = cmp.Or(locLang, "english")
	inv := gather(scriptRoots(info, installPath))
	var one []fileRef
	for _, f := range inv.loc {
		if loc.LanguageFromRel(f.abs) == locLang {
			one = append(one, f)
		}
	}
	x := NewIndex(gameID, nil)
	_, err := harvestLoc(ctx, x, one, locLang)
	x.SortLocKeys()
	return x, err
}

func locEngineSlots(defined map[string]LocEntry, refs []Ref) map[string]bool {
	out := map[string]bool{}
	for _, r := range refs {
		if _, isKey := defined[r.Key]; r.Kind == "loc" && r.Key != "" && !isKey {
			out[r.Key] = true
		}
	}
	return out
}

// ------------------------------------------------------------- game info

var dataFnRe = regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)

// harvestGetSet collects `GetX` / `SetX` data-function names from one file's source, scanning for the two prefixes directly: a regex over 180 MB cost 12% of the scan.
func harvestGetSet(src string) map[string]bool {
	var out map[string]bool
	for i := 0; i+3 < len(src); i++ {
		if c := src[i]; c != 'G' && c != 'S' || src[i+1] != 'e' || src[i+2] != 't' || i > 0 && isDataFnByte(src[i-1]) {
			continue
		}
		j := i + 3
		for j < len(src) && isDataFnByte(src[j]) {
			j++
		}
		if j == i+3 {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[src[i:j]] = true
		i = j - 1
	}
	return out
}

func isDataFnByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// dataFunctions reads the data-type dumps the game writes beside script_docs, falling back to the corpus harvest when an install ships none.
func dataFunctions(dirs []string, harvested map[string]bool) []string {
	seen := map[string]bool{}
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			name := strings.ToLower(e.Name())
			if !strings.Contains(name, "data_type") && !strings.Contains(name, "datatypes") {
				return nil
			}
			if raw, err := os.ReadFile(p); err == nil {
				for _, m := range dataFnRe.FindAllSubmatch(raw, -1) {
					seen[string(m[1])] = true
				}
			}
			return nil
		})
	}
	if len(seen) == 0 {
		seen = harvested
	}
	return slices.Sorted(maps.Keys(seen))
}

// firstWins files one documented name: the first non-empty value seen wins, globally and under its structure.
func firstWins(global map[string]string, by map[string]map[string]string, structure, key, v string) {
	if v == "" {
		return
	}
	if global != nil && global[key] == "" {
		global[key] = v
	}
	if structure == "" {
		return
	}
	if by[structure] == nil {
		by[structure] = map[string]string{}
	}
	if by[structure][key] == "" {
		by[structure][key] = v
	}
}

// harvestGameInfo reads shipped `_*.info` / `*.md` / readme files into prose addressed by (structure, field) and the declared types written beside it, and returns the fields each structure is documented to hold.
func harvestGameInfo(gameID string, docs []fileRef) (Docs, map[string]map[string]bool) {
	d := Docs{FieldInfo: map[string]string{}, Fields: map[string]map[string]string{}, KindInfo: map[string]string{},
		DeclaredFieldTypes: map[string]declared.DeclaredType{}, DeclaredFieldTypesByKind: map[string]map[string]declared.DeclaredType{}}
	rawTypes, rawByKind := map[string]string{}, map[string]map[string]string{}
	structs := map[string]map[string]bool{}
	member := func(structure, key string) {
		if structs[structure] == nil {
			structs[structure] = map[string]bool{}
		}
		structs[structure][key] = true
	}
	for _, f := range docs {
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			continue
		}
		text, _ := jomini.Decode(raw)
		kind := game.MatchExtract(gameID, f.rel).Kind
		if kind != "" && d.KindInfo[kind] == "" {
			d.KindInfo[kind] = game.LeadingKindProse(text)
		}
		harvested, typed, nested := game.ParseDocFile(text)
		for key, t := range typed {
			firstWins(rawTypes, rawByKind, kind, key, t)
		}
		for key, doc := range harvested {
			if kind != "" {
				member(kind, key)
			}
			firstWins(d.FieldInfo, d.Fields, kind, key, doc)
		}

		// A key documented inside an example block is addressed by that block, one structure wherever it is written.
		for path, doc := range nested {
			if block, bare, ok := strings.Cut(path, "."); ok {
				member(block, bare)
				firstWins(nil, d.Fields, block, bare, doc)
			}
		}
	}
	for key, t := range rawTypes {
		d.DeclaredFieldTypes[key] = declared.ClassifyDeclaredType(t)
	}
	for kind, m := range rawByKind {
		out := make(map[string]declared.DeclaredType, len(m))
		for key, t := range m {
			out[key] = declared.ClassifyDeclaredType(t)
		}
		d.DeclaredFieldTypesByKind[kind] = out
	}
	d.FieldInfo = lowerKeys(d.FieldInfo)
	for structure, m := range d.Fields {
		d.Fields[structure] = lowerKeys(m)
	}
	return d, structs
}

func lowerKeys(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if lk := strings.ToLower(k); out[lk] == "" {
			out[lk] = v
		}
	}
	return out
}
