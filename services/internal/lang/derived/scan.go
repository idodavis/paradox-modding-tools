// scan.go harvests a game install into VanillaCache: script defs, structure keys,
// vocabulary, loc, shipped game-info prose, GUI types/props, metadata, script_docs.

package derived

import (
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

const locValueLimit = 200
const scanConcurrency = 8

// ScanRequest is the install harvest input. script_docs come from UserDataDir.
type ScanRequest struct {
	InstallID, GameID, InstallPath, Version, LocLang string
	OnProgress                                       func(pct int, msg string)
}

// Scan harvests installPath into a VanillaCache.
func Scan(ctx context.Context, req ScanRequest) (*VanillaCache, *VanillaLoc, error) {
	info := game.Get(req.GameID)
	if info == nil {
		return nil, nil, os.ErrInvalid
	}
	locLang := req.LocLang
	if locLang == "" {
		locLang = "english"
	}
	version := CacheVersion(req.Version)
	step := req.OnProgress
	if step == nil {
		step = func(int, string) {}
	}
	step(5, "listing files")
	inv := gather(scriptRoots(info, req.InstallPath))

	// A game whose script root holds nothing is not an empty game, it is the wrong folder -- and without this guard Scan writes a 0-def cache with a fresh ScannedAt over the good one, after which CacheStale reads false forever and every mod extending `traits` gets a false unresolved-ref finding on every vanilla trait it cites.
	if len(inv.script) == 0 {
		return nil, nil, fmt.Errorf(
			"no script files under %s: check the install path", req.InstallPath)
	}

	// The declared type system is read first: binding prefixes to databases needs it during the walk, not after.
	docDirs := scriptDocsDirs(req.GameID, req.InstallPath)
	schema := declared.ReadSchema(docDirs)

	// Localization is read before the script walk because deriveLocFields needs the key set to decide which script properties hold a key, and that decision has to be made between the walk's two passes.
	step(15, "reading localization")
	vloc, locFileRefs, err := harvestLoc(ctx, inv.loc, locLang, req.InstallID, version)
	if err != nil {
		return nil, nil, err
	}
	engineSlots := locEngineSlots(vloc.Sites, locFileRefs)
	locKeys := locKeySet(vloc.Sites)

	step(30, "parsing script")
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	acc, derived, dataFns, err := collectExtracts(
		ctx, req.GameID, files, nil, schema, locKeys)
	if err != nil {
		return nil, nil, err
	}

	step(80, "reading game info")
	fieldInfo, fieldByKind, kindInfo, docStructs, declTypes, declByKind := harvestGameInfo(req.GameID, inv.info)
	for kind, keys := range docStructs {
		set := acc.structures[kind]
		if set == nil {
			set = map[string]int{}
			acc.structures[kind] = set
		}
		for k := range keys {
			if set[k] == 0 {
				set[k] = 1
			}
		}
	}

	// One grouping of acc.defs for the three derivations below.
	accIdx := newDefIndex(acc.defs)
	structures := keysByCount(acc.structures)
	// Indexed here, not at harvest: the index is ~80 MB and the parallel walk is the heap peak.
	locIndex := loc.NewKeys(locKeys)
	kinds := deriveFieldValueKinds(acc.fieldRHS, accIdx)
	derived.FieldValueKinds = kinds
	// The data rows leave here for their own file; see data.go.
	script, data := splitData(req.GameID, dropEphemeralDefs(acc.defs))
	// Effects and triggers live on c.Schema alone; the corpus never contributes one, because inferring them by position elected every $PARAM$ substitution site as an effect -- on CK3, 1,905 real effects became 10,337 completion entries, 82% of them noise.
	c := &VanillaCache{
		FormatVersion:            CacheFormatVersion,
		InstallID:                req.InstallID,
		GameID:                   req.GameID,
		InstallPath:              req.InstallPath,
		GameVersion:              version,
		ScannedAt:                time.Now().UTC().Format(time.RFC3339),
		Defs:                     script,
		Edges:                    acc.edges,
		LocRefs:                  append(locKindRefs(acc.refs), locKindRefs(locFileRefs)...),
		CallRefs:                 macroCallRefs(acc.refs),
		FieldValueKinds:          kinds,
		PrefixKinds:              derived.PrefixKinds,
		KindScope:                derived.KindScope,
		FireKeys:                 derived.FireKeys,
		NestedShapes:             derived.NestedShapes,
		Wrappers:                 sortedKeys(derived.Wrappers),
		LocConventions:           deriveLocConventions(accIdx, structures, locIndex),
		LocKeyPrefixes:           loc.LearnKeyPrefixes(locIndex, locConventionFloor),
		LocFields:                sortedKeys(derived.LocFields),
		KindInfo:                 kindInfo,
		FieldInfo:                fieldInfo,
		FieldInfoByKind:          fieldByKind,
		DeclaredFieldTypes:       declTypes,
		DeclaredFieldTypesByKind: declByKind,
		Structures:               structures,
		NestedMembers:            deriveNestedMembers(acc.nested),
		UnambiguousBlocks:        unambiguousBlocks(acc.blockKinds),
		StructureBlocks:          blockKeys(acc.structures, acc.structBlocks),
		Vocabulary:               sortedKeys(acc.vocab),
		GUITypes:                 sortedKeys(acc.guiTypes),
		GUIProps:                 sortedKeys(acc.guiProps),
		DataFunctions:            sortedKeys(dataFns),
		LocEngineSlots:           engineSlots,
		Schema:                   schema,
		data:                     data,
	}

	step(90, "reading script_docs")
	enrichDataTypes(docDirs, c)
	PrepareCache(c)

	step(100, "done")
	return c, vloc, nil
}

func scriptDocsDirs(gameID, installPath string) []string {
	info := game.Get(gameID)
	if info == nil {
		return nil
	}
	ud := game.UserDataDir(gameID, installPath)
	if ud == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, sub := range []string{info.ScriptDocsSubdir, "docs", "logs"} {
		if sub == "" {
			continue
		}
		p := filepath.Join(ud, sub)
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// SchemaStatus is what the Workspace Health strip needs to know about the declared type system, read without loading the model: script_docs is a couple of megabytes, the model is ninety, and health is polled.
type SchemaStatus struct {
	Source     declared.SchemaSource `json:"source"`
	Effects    int                   `json:"effects"`
	Triggers   int                   `json:"triggers"`
	ScopeTypes int                   `json:"scopeTypes"`
	Prefixes   int                   `json:"prefixes"`
	OnActions  int                   `json:"onActions"`
	// ReadAt is the newest dump mtime (RFC3339), for staleness against the install.
	ReadAt string `json:"readAt,omitempty"`
	// Archived always reports false: PMT no longer archives script_docs.
	Archived bool `json:"archived"`
	// Folder is where this game writes its dumps, for the walkthrough.
	Folder string `json:"folder,omitempty"`
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
	st.Prefixes = len(sc.Prefixes())
	st.ReadAt = sc.ReadAt
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

// fileRef is a file plus its path relative to its script root (for MatchExtract).
type fileRef struct{ abs, rel, origin string }

// inventory buckets the walked files by role.
type inventory struct {
	script, gui, info []fileRef
	loc               []string
}

// ClassifyRel buckets a file by name/rel for the install walk and mod indexer.
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
		// Loose .txt at the script root is engine bookkeeping, not script: parsing checksum_manifest.txt as Jomini produced fire edges for "common", "events" and "history". Real script always sits in a content folder.
		if !strings.Contains(slash, "/") {
			return ""
		}
		return "script"
	}
	return ""
}

// walkClassified walks root and hands each ClassifyRel-recognized file to fn.
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
				inv.loc = append(inv.loc, f.abs)
			case "script":
				inv.script = append(inv.script, f)
			}
		})
	}
	return inv
}

// Evidence collects FileExtract results from all workers behind one mutex. It is the single merge sink for both Scan and BuildIndex.
type Evidence struct {
	mu           sync.Mutex
	defs         []Def
	refs         []Ref
	edges        []Edge
	cands        []CallCandidate
	loc          map[string]map[string]LocEntry
	locOrder     map[string]map[string]int
	structures   map[string]map[string]int
	structBlocks map[string]map[string]int
	// nested is block key -> member key -> occurrences, across the whole walk. Not keyed by kind: a block named triggered_desc holds the same members wherever it appears, and merging the evidence makes the floor meaningful.
	nested map[string]map[string]int
	// blockKinds is block name -> the kinds it was seen under. A name used by one kind can be judged; a name used by several cannot.
	blockKinds map[string]map[string]bool
	vocab      map[string]bool
	guiTypes   map[string]bool
	guiProps   map[string]bool
	// fieldRHS is the first pass's whole-corpus field -> values map, stored rather than merged: it is a pure function of the trees, so having the second pass build it again was one extra walk of the entire install.
	fieldRHS map[string]map[string]bool
}

func newEvidence() *Evidence {
	return &Evidence{
		loc:          map[string]map[string]LocEntry{},
		locOrder:     map[string]map[string]int{},
		structures:   map[string]map[string]int{},
		structBlocks: map[string]map[string]int{},
		nested:       map[string]map[string]int{},
		blockKinds:   map[string]map[string]bool{},
		vocab:        map[string]bool{},
		guiTypes:     map[string]bool{},
		guiProps:     map[string]bool{},
	}
}

func (a *Evidence) merge(ex FileExtract) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.defs = append(a.defs, ex.Defs...)
	a.refs = append(a.refs, ex.Refs...)
	a.edges = append(a.edges, ex.Edges...)
	a.mergeLocOrdered(ex.LocOrder, ex.Loc)
	addCounts(a.structures, ex.MemberKind, ex.StructCounts)
	addCounts(a.structBlocks, ex.MemberKind, ex.StructBlocks)
	for parent, members := range ex.StructNested {
		addCounts(a.nested, parent, members)
		if ex.StructNestedKind != "" {
			if a.blockKinds[parent] == nil {
				a.blockKinds[parent] = map[string]bool{}
			}
			a.blockKinds[parent][ex.StructNestedKind] = true
		}
	}
	maps.Copy(a.vocab, ex.Vocab)
	maps.Copy(a.guiTypes, ex.GUITypes)
	maps.Copy(a.guiProps, ex.GUIProps)
	a.cands = append(a.cands, ex.Cands...)
}

// mergeLocOrdered merges one file's localization under LOAD order rather than completion order. The corpus walk is parallel, so when two files of one language define the same key, whichever worker finished last decided which value survived -- and the answer changed between runs of the same workspace. order is the file's index in the walk list.
func (a *Evidence) mergeLocOrdered(order int, d LocDelta) {
	if d.Lang == "" || len(d.Vals) == 0 {
		return
	}
	m := a.loc[d.Lang]
	if m == nil {
		m = map[string]LocEntry{}
		a.loc[d.Lang] = m
	}
	ord := a.locOrder[d.Lang]
	if ord == nil {
		ord = map[string]int{}
		a.locOrder[d.Lang] = ord
	}
	for k, v := range d.Vals {
		if prev, seen := ord[k]; seen && prev > order {
			continue
		}
		m[k] = v
		ord[k] = order
	}
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

func walkFiles(ctx context.Context, files []fileRef, fn func(fileRef) error) error {
	return walkN(ctx, len(files), func(i int) error {
		return fn(files[i])
	})
}

// collectExtracts harvests a whole corpus in two streaming passes: the first needs no global knowledge and produces the defs, the second needs the derived facts, which need every def. Neither retains a parse tree -- holding all 4,551 CK3 trees at once cost 4.30 GB and drove a scan to an 8.06 GB peak, while re-reading and re-parsing the install costs about half a second. schema binds prefixes to databases; it comes from the cache for mods and is read up front for a vanilla scan. locKeys is the install's localization key set, nil for a mod walk, which inherits the install's answer from the cache.
func collectExtracts(
	ctx context.Context, gameID string, files []fileRef,
	cache *VanillaCache, schema *declared.Schema, locKeys map[string]bool,
) (*Evidence, *Derived, map[string]bool, error) {
	corp := corpus{ctx: ctx, gameID: gameID, files: files}
	dataFns := map[string]bool{}
	fieldRHS := map[string]map[string]bool{}
	fieldListRHS := map[string]map[string]bool{}
	var baseDefs []Def
	var cites, fieldCites []typedCite
	var mu sync.Mutex

	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		ff := FactsOf(gameID, f.abs, f.rel, f.origin, res)
		ex := foldForms(ff)
		fns := harvestGetSet(res.Src)
		c, fc := foldCites(ff)
		mu.Lock()
		defer mu.Unlock()
		baseDefs = append(baseDefs, ex.Defs...)
		cites = append(cites, c...)
		fieldCites = append(fieldCites, fc...)
		maps.Copy(dataFns, fns)
		mergeValueSets(fieldRHS, ex.FieldRHS)
		mergeValueSets(fieldListRHS, ex.FieldListRHS)
		return nil
	}); err != nil {
		return nil, nil, dataFns, err
	}

	derived := derivedFromCache(cache)
	baseIdx := newDefIndex(baseDefs)
	derived.mergeLocal(deriveAll(gameID, corp, baseIdx, schema, cites, fieldCites))
	// mergeStringMap returns nil for an empty result, so mergeLocal can leave these maps nil.
	if derived.FieldValueKinds == nil {
		derived.FieldValueKinds = map[string]string{}
	}
	for k, v := range deriveFieldValueKinds(fieldRHS, baseIdx) {
		if derived.FieldValueKinds[k] == "" {
			derived.FieldValueKinds[k] = v
		}
	}
	// The install derives its own loc fields; a mod walk keeps the cache's. Scalar and LIST sides both hold keys.
	if len(locKeys) > 0 {
		defKeys := baseIdx.citableKeys()
		derived.LocFields = map[string]bool{}
		maps.Copy(derived.LocFields, deriveLocFields(fieldRHS, locKeys, defKeys, schema))
		maps.Copy(derived.LocFields, deriveLocFields(fieldListRHS, locKeys, defKeys, schema))
	}
	baseDefs = nil

	// Second pass builds the model: the derived facts are complete now, so wrapper defs drop, nested defs appear, and refs and edges resolve. The citations are indexed once, corpus-wide, because a nested database is routinely declared in one file and cited from another.
	derived.CitedByKind = citedIDsByKind(derived.NestedShapes, cites, derived.PrefixKinds)
	acc := newEvidence()
	acc.fieldRHS = fieldRHS
	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		acc.merge(ExtractFacts(
			FactsOf(gameID, f.abs, f.rel, f.origin, res), schema, derived))
		return nil
	}); err != nil {
		return acc, derived, dataFns, err
	}

	// Nested databases (CK3 faiths under religions) only exist as defs after applyDerivedDefs, so the first bind could not see them.
	rebind(derived, schema, acc.defs)

	if err := harvestLocFiles(ctx, gameID, files, acc); err != nil {
		return acc, derived, dataFns, err
	}
	dropEngineSlotRefs(acc, cache)
	kinds := macroDefKinds(acc.defs, cache)
	acc.refs = append(acc.refs, ApplyCallRefs(acc.cands, kinds)...)
	acc.edges = append(acc.edges, ApplyCallEdges(acc.cands, macroDefKeys(acc.defs, cache))...)
	return acc, derived, dataFns, nil
}

func mergeValueSets(dst, src map[string]map[string]bool) {
	for field, vals := range src {
		m := dst[field]
		if m == nil {
			m = map[string]bool{}
			dst[field] = m
		}
		maps.Copy(m, vals)
	}
}

// harvestLocFiles merges loc YAML that corpus.walk skipped (not Jomini).
func harvestLocFiles(
	ctx context.Context, gameID string, files []fileRef, acc *Evidence,
) error {
	// walkN rather than walkFiles: the index is the file's load order, and the merge needs it to decide which value wins when two files define one key.
	return walkN(ctx, len(files), func(i int) error {
		f := files[i]
		if game.MatchExtract(gameID, f.rel).Mode != game.ModeLocKey {
			return nil
		}
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return nil
		}
		text, _ := jomini.Decode(raw)
		ex := ExtractFile(gameID, f.abs, f.rel, f.origin, text)
		ex.LocOrder = i
		acc.merge(ex)
		return nil
	})
}

type corpus struct {
	ctx    context.Context
	gameID string
	files  []fileRef
}

func (c corpus) walk(fn func(fileRef, jomini.Result) error) error {
	return walkFiles(c.ctx, c.files, func(f fileRef) error {
		if game.MatchExtract(c.gameID, f.rel).Mode == game.ModeLocKey {
			return nil
		}
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return nil
		}
		text, _ := jomini.Decode(raw)
		return fn(f, jomini.Parse(jomini.Normalize(text)))
	})
}

// deriveAll derives the four things no dump states: fire keys, nested databases, setup wrappers and loc conventions. The prefix table is not among them -- it comes from bindKinds, a join against declared types, so without a schema there is no prefix table at all rather than a guessed one. One walk for every fact that depends on a single file: each corpus walk re-parses the whole install, so folding these together is worth more than any micro-optimisation inside them.
func deriveAll(
	gameID string, corp corpus, idx *defIndex, s *declared.Schema,
	cites, fieldCites []typedCite,
) *Derived {
	ev, oa := idx.keysOfKind("event"), idx.keysOfKind("on_action")
	fireCounts := map[string]*fireCount{}
	wrappers := map[string]bool{}
	defKeys := idx.citableKeys()
	var prefix, kindScope map[string]string
	if !s.Empty() {
		prefix, kindScope = bindKinds(s, idx)
	}
	idChild := nestedIDChild(gameID, defKeys, prefix, cites, fieldCites)
	byChild := map[string]*nestedWanted{}
	var mu sync.Mutex
	_ = corp.walk(func(f fileRef, res jomini.Result) error {
		ff := FactsOf(gameID, f.abs, f.rel, f.origin, res)
		fire := deriveFireKeys(ff, ev, oa)
		wraps := deriveWrappers(ff, defKeys)
		mu.Lock()
		defer mu.Unlock()
		fireCounts = mergeFireCounts(fireCounts, fire)
		for _, w := range wraps {
			wrappers[w] = true
		}
		observeNestedFacts(ff, idChild, byChild)
		return nil
	})
	return &Derived{
		PrefixKinds:  prefix,
		KindScope:    kindScope,
		FireKeys:     resolveFireKinds(fireCounts),
		Wrappers:     wrappers,
		NestedShapes: nestedShapesFrom(byChild, s),
	}
}

// rebind re-runs the bind once nested defs (CK3 faiths under religions) exist.
func rebind(v *Derived, s *declared.Schema, defs []Def) {
	if v == nil || s.Empty() || len(defs) == 0 {
		return
	}
	prefixKind, kindScope := bindKinds(s, newDefIndex(defs))
	if len(prefixKind) == 0 {
		return
	}
	if v.PrefixKinds == nil {
		v.PrefixKinds = map[string]string{}
	}
	if v.KindScope == nil {
		v.KindScope = map[string]string{}
	}
	maps.Copy(v.PrefixKinds, prefixKind)
	maps.Copy(v.KindScope, kindScope)
}

func harvestLoc(
	ctx context.Context, files []string, locLang, installID, version string,
) (*VanillaLoc, []Ref, error) {
	byLang := map[string]*VanillaLoc{}
	refs := make([]fileRef, 0, len(files))
	for _, f := range files {
		if loc.LanguageFromRel(f) == "" {
			continue
		}
		refs = append(refs, fileRef{abs: f})
	}
	// Harvest only the workspace language and english. A CK3 install ships eleven, and writing them all cost ~790 MB of sidecars and most of the loc pass; english is kept because coverage compares against it.
	want := map[string]bool{locLang: true, "english": true}

	var mu sync.Mutex
	var locRefs []Ref
	err := walkFiles(ctx, refs, func(f fileRef) error {
		lang := loc.LanguageFromRel(f.abs)
		if lang == "" || !want[lang] {
			return nil
		}
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return nil
		}
		text, _ := jomini.Decode(raw)
		_, locd, fileRefs := extractLoc(f.abs, text, "")
		mu.Lock()
		out := byLang[lang]
		if out == nil {
			out = &VanillaLoc{FormatVersion: LocFormatVersion, Sites: map[string]LocEntry{}}
			byLang[lang] = out
		}
		for k, v := range locd.Vals {
			out.Sites[k] = LocEntry{Path: v.Path, Line: v.Line, Value: v.Value}
		}
		if lang == locLang {
			locRefs = append(locRefs, fileRefs...)
		}
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if installID != "" {
		for lang, vl := range byLang {
			if err := SaveVanillaLoc(installID, version, lang, vl); err != nil {
				return nil, nil, err
			}
		}
	}
	if out := byLang[locLang]; out != nil {
		return out, locRefs, nil
	}
	return &VanillaLoc{FormatVersion: LocFormatVersion, Sites: map[string]LocEntry{}}, locRefs, nil
}

func locEngineSlots(defined map[string]LocEntry, refs []Ref) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if r.Kind != "loc" || r.Key == "" || seen[r.Key] {
			continue
		}
		if _, isKey := defined[r.Key]; isKey {
			continue
		}
		seen[r.Key] = true
		out = append(out, r.Key)
	}
	slices.Sort(out)
	return out
}

// HarvestLoc gathers install loc files for locLang without opening other languages.
func HarvestLoc(ctx context.Context, gameID, installPath, locLang string) (*VanillaLoc, error) {
	info := game.Get(gameID)
	if info == nil {
		return nil, os.ErrInvalid
	}
	if locLang == "" {
		locLang = "english"
	}
	inv := gather(scriptRoots(info, installPath))
	var one []string
	for _, f := range inv.loc {
		if loc.LanguageFromRel(f) == locLang {
			one = append(one, f)
		}
	}
	v, _, err := harvestLoc(ctx, one, locLang, "", "")
	return v, err
}

// firstWins files one documented name: the first non-empty value seen wins, globally and per kind. A nil global files per kind only; an empty kind files globally only.
func firstWins[V comparable](global map[string]V, byKind map[string]map[string]V, kind, key string, v V) {
	var zero V
	if v == zero {
		return
	}
	if global != nil && global[key] == zero {
		global[key] = v
	}
	if kind == "" {
		return
	}
	m := byKind[kind]
	if m == nil {
		m = map[string]V{}
		byKind[kind] = m
	}
	if m[key] == zero {
		m[key] = v
	}
}

// harvestGameInfo reads shipped `_*.info` / `*.md` / readme files into field prose and the declared types written beside it. Both are filed globally for the names that mean one thing everywhere and per kind because most do not -- `type` is a value set inside a scripted relation and something else entirely inside an event. Per-kind wins at lookup.
func harvestGameInfo(gameID string, docs []fileRef) (
	fieldDocs map[string]string,
	byKind map[string]map[string]string,
	kindInfo map[string]string,
	structs map[string]map[string]bool,
	declTypes map[string]string,
	declByKind map[string]map[string]string,
) {
	fieldDocs, byKind, structs = map[string]string{}, map[string]map[string]string{}, map[string]map[string]bool{}
	kindInfo = map[string]string{}
	declTypes, declByKind = map[string]string{}, map[string]map[string]string{}
	for _, f := range docs {
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			continue
		}
		kind := game.MatchExtract(gameID, f.rel).Kind
		if kind != "" && kindInfo[kind] == "" {
			if prose := game.LeadingKindProse(string(raw)); prose != "" {
				kindInfo[kind] = prose
			}
		}
		harvested, typed, nested := game.ParseDocFile(string(raw))
		for key, t := range typed {
			firstWins(declTypes, declByKind, kind, key, t)
		}
		for key, doc := range harvested {
			// A documented name is a member of the kind whether or not it came with prose.
			if kind != "" {
				if structs[kind] == nil {
					structs[kind] = map[string]bool{}
				}
				structs[kind][key] = true
			}
			firstWins(fieldDocs, byKind, kind, key, doc)
		}
		// A key documented inside an example block stays path-qualified and per-kind only, never bare or global.
		if kind == "" {
			continue
		}
		for path, doc := range nested {
			if structs[kind] == nil {
				structs[kind] = map[string]bool{}
			}
			if _, bare, ok := strings.Cut(path, "."); ok {
				structs[kind][bare] = true
			}
			firstWins(nil, byKind, kind, path, doc)
		}
	}
	return fieldDocs, byKind, kindInfo, structs, declTypes, declByKind
}

var dataFnRe = regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)

// enrichDataTypes reads the data-type dumps the game writes beside script_docs.
func enrichDataTypes(dirs []string, c *VanillaCache) {
	if c == nil {
		return
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !strings.Contains(name, "data_type") && !strings.Contains(name, "datatypes") {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, m := range dataFnRe.FindAllSubmatch(raw, -1) {
				seen[string(m[1])] = true
			}
			return nil
		})
	}
	// Only when the dumps named some: an install with no data-type dump keeps the corpus harvest rather than being emptied by it.
	if len(seen) > 0 {
		c.DataFunctions = sortedKeys(seen)
	}
}

func sortedKeys(set map[string]bool) []string {
	out := slices.Collect(maps.Keys(set))
	slices.Sort(out)
	return out
}

func addCounts(dst map[string]map[string]int, kind string, src map[string]int) {
	if kind == "" || len(src) == 0 {
		return
	}
	set := dst[kind]
	if set == nil {
		set = map[string]int{}
		dst[kind] = set
	}
	for k, n := range src {
		set[k] += n
	}
}

// dropEngineSlotRefs removes loc references to interpolations the engine fills in, which no mod can define. This is the harvest site, not the view: the decision needs the vanilla slot set, and filtering the row later would leave a wrong reference for hover and find-references to trip over too.
func dropEngineSlotRefs(acc *Evidence, cache *VanillaCache) {
	if acc == nil || cache == nil || len(cache.LocEngineSlots) == 0 {
		return
	}
	slots := make(map[string]bool, len(cache.LocEngineSlots))
	for _, k := range cache.LocEngineSlots {
		slots[k] = true
	}
	kept := acc.refs[:0]
	for _, r := range acc.refs {
		if r.Kind == "loc" && slots[r.Key] {
			continue
		}
		kept = append(kept, r)
	}
	acc.refs = kept
}
