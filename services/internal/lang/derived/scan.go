// scan.go harvests a game install into VanillaCache: script defs, structure keys,
// vocabulary, loc, shipped game-info prose, GUI types/props, metadata, script_docs.

package derived

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
	onProgress := req.OnProgress
	progress(onProgress, 5, "listing files")
	inv := gather(scriptRoots(info, req.InstallPath))

	// A game whose script root holds nothing is not an empty game — it is the
	// wrong folder. walkClassified discards its walk error, so a missing,
	// moved or mis-pointed install arrives here as an empty inventory rather
	// than a failure, and without this guard Scan would return a 0-def cache
	// with a fresh ScannedAt, SaveCache would write it over the good one, and
	// CacheStale would then read false forever. The symptom is not silence:
	// unresolved-ref is gated on KindHasDefs, which is satisfied by the mod's
	// own defs, so a mod extending `traits` gets a false finding on every
	// vanilla trait it cites.
	if len(inv.script) == 0 {
		return nil, nil, fmt.Errorf(
			"no script files under %s: check the install path", req.InstallPath)
	}

	// The declared type system is read first: binding prefixes to databases
	// needs it during the walk, not after. Live dumps win and refresh PMT's
	// archived copy; the archive stands in when the user's have been removed.
	docDirs := scriptDocsDirs(req.GameID, req.InstallPath)
	schema := declared.ReadSchema(docDirs)

	// Localization is read before the script walk, not after, because
	// deriveLocFields needs the key set to decide which script properties hold
	// a key — and that decision has to be made between the walk's two passes,
	// while the second one is still emitting references.
	progress(onProgress, 15, "reading localization")
	vloc, locFileRefs, err := harvestLoc(ctx, inv.loc, locLang, req.InstallID, version)
	if err != nil {
		return nil, nil, err
	}
	engineSlots := locEngineSlots(vloc.Sites, locFileRefs)
	locKeys := locKeySet(vloc.Sites)

	progress(onProgress, 30, "parsing script")
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	acc, derived, dataFns, err := collectExtracts(
		ctx, req.GameID, files, true, nil, schema, locKeys)
	if err != nil {
		return nil, nil, err
	}

	progress(onProgress, 80, "reading game info")
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
	nestedMembers := deriveNestedMembers(acc.nested)
	// Indexed here, not at harvest: the index is ~80 MB and the parallel walk is the heap peak.
	locIndex := loc.NewKeys(locKeys)
	kinds := deriveFieldValueKinds(acc.fieldRHS, accIdx)
	if derived != nil {
		derived.FieldValueKinds = kinds
	}
	// The data rows leave here for their own file; see data.go.
	script, data := splitData(req.GameID, dropEphemeralDefs(acc.defs))
	// Effects and triggers are read from the game's own script_docs and live on
	// c.Schema alone; the corpus never contributes one. Inferring them by
	// position elected every $PARAM$ substitution site as an effect: on CK3 that
	// turned 1,905 real effects into 10,337 completion entries, 82% of them
	// noise like `$CHARACTER$.culture`. Data functions are harvested in the
	// first corpus pass, where the source text is already in hand.
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
		NestedMembers:            nestedMembers,
		UnambiguousBlocks:        unambiguousBlocks(acc.blockKinds),
		StructureBlocks:          blockKeys(acc.structures, acc.structBlocks),
		Vocabulary:               sortedKeys(acc.vocab),
		GUITypes:                 sortedKeys(acc.guiTypes),
		GUIProps:                 sortedKeys(acc.guiProps),
		DataFunctions:            sortedKeys(dataFns),
		LocEngineSlots:           engineSlots,
		data:                     data,
	}

	progress(onProgress, 90, "reading script_docs")
	c.Schema = schema
	for _, dir := range docDirs {
		enrichDataTypes(dir, c)
	}
	PrepareCache(c)

	progress(onProgress, 100, "done")
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

// SchemaStatus is what the Workspace Health strip needs to know about the
// declared type system, read without loading the model. script_docs is a couple
// of megabytes; the model is ninety, and health is polled.
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
		if !strings.Contains(slash, "/") {
			// Loose .txt at the script root is engine bookkeeping, not script:
			// checksum_manifest.txt, credits.txt, compound_settings.txt. Parsing
			// the manifest as Jomini produced fire edges for "common", "events"
			// and "history". Real script always sits in a content folder.
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

// Evidence collects FileExtract results from all workers behind one mutex. It is
// the single merge sink for both Scan and BuildIndex.
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
	// nested is block key -> member key -> occurrences, across the whole walk.
	// Not keyed by kind: a block named triggered_desc holds the same members
	// wherever it appears, and merging the evidence makes the floor meaningful.
	nested map[string]map[string]int
	// blockKinds is block name -> the kinds it was seen under. A name used by
	// one kind can be judged; a name used by several cannot.
	blockKinds map[string]map[string]bool
	vocab      map[string]bool
	guiTypes   map[string]bool
	guiProps   map[string]bool
	fieldRHS   map[string]map[string]bool
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
		fieldRHS:     map[string]map[string]bool{},
	}
}

func (a *Evidence) merge(ex FileExtract) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.defs = append(a.defs, ex.Defs...)
	a.refs = append(a.refs, ex.Refs...)
	a.edges = append(a.edges, ex.Edges...)
	a.mergeLocOrdered(ex.LocOrder, ex.Loc)
	if ex.MemberKind != "" {
		addCounts(&a.structures, ex.MemberKind, ex.StructCounts)
	}
	if ex.StructKind != "" {
		addCounts(&a.structBlocks, ex.StructKind, ex.StructBlocks)
	}
	for parent, members := range ex.StructNested {
		addCounts(&a.nested, parent, members)
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
	for field, vals := range ex.FieldRHS {
		m := a.fieldRHS[field]
		if m == nil {
			m = map[string]bool{}
			a.fieldRHS[field] = m
		}
		maps.Copy(m, vals)
	}
	a.cands = append(a.cands, ex.Cands...)
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

// collectExtracts parses files and derives the install-only facts. schema is the
// game's declared type system, used to bind prefixes to databases; it comes from
// the cache for mods and is read up front for a vanilla scan.
// collectExtracts harvests a whole corpus in two streaming passes.
//
// The first pass needs no global knowledge and produces the defs; the second
// needs the derived facts, which need every def, so it cannot be folded into the
// first. Neither retains a parse tree: holding all 4,551 CK3 trees at once cost
// 4.30 GB and drove a scan to an 8.06 GB peak, while re-reading and re-parsing
// the whole install costs about half a second. Memory here is bounded by what is
// kept — defs, refs, edges — not by the size of the install.
// locKeys is the install's localization key set, used between the two passes to
// derive which script properties hold a key. Nil for a mod walk, which inherits
// the install's answer from the cache.
func collectExtracts(
	ctx context.Context, gameID string, files []fileRef, bodies bool,
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
		ex := extractForms(gameID, f.abs, f.rel, f.origin, res, bodies)
		fns := harvestGetSet(res.Src)
		c := collectTypedCites(res.Root)
		fc := collectFieldCites(res.Root)
		mu.Lock()
		defer mu.Unlock()
		baseDefs = append(baseDefs, ex.Defs...)
		cites = append(cites, c...)
		fieldCites = append(fieldCites, fc...)
		maps.Copy(dataFns, fns)
		for field, vals := range ex.FieldRHS {
			m := fieldRHS[field]
			if m == nil {
				m = map[string]bool{}
				fieldRHS[field] = m
			}
			maps.Copy(m, vals)
		}
		// Only pass 1 wants list members: nothing downstream reads them, so
		// pass 2 computing them again was 42% of its extractForms time.
		for field, vals := range extractFieldListValues(res.Root) {
			m := fieldListRHS[field]
			if m == nil {
				m = map[string]bool{}
				fieldListRHS[field] = m
			}
			maps.Copy(m, vals)
		}
		return nil
	}); err != nil {
		return nil, nil, dataFns, err
	}

	derived := derivedFromCache(cache)
	baseIdx := newDefIndex(baseDefs)
	local := deriveAll(gameID, corp, baseIdx, schema, cites, fieldCites)
	derived.mergeLocal(local)
	if derived.FieldValueKinds == nil {
		derived.FieldValueKinds = map[string]string{}
	}
	for k, v := range deriveFieldValueKinds(fieldRHS, baseIdx) {
		if derived.FieldValueKinds[k] == "" {
			derived.FieldValueKinds[k] = v
		}
	}
	// The install derives its own; a mod walk keeps the cache's. Scalar and LIST sides both hold keys.
	if len(locKeys) > 0 {
		defKeys := baseIdx.citableKeys()
		derived.LocFields = map[string]bool{}
		maps.Copy(derived.LocFields, deriveLocFields(fieldRHS, locKeys, defKeys, schema))
		maps.Copy(derived.LocFields, deriveLocFields(fieldListRHS, locKeys, defKeys, schema))
	}
	baseDefs = nil

	// Second pass builds the model. The derived facts are complete now, so
	// wrapper defs drop, nested defs appear, and refs and edges resolve. The
	// citations are indexed once, corpus-wide: a nested database is routinely
	// declared in one file and cited from another.
	citedByKind := citedIDsByKind(derived.NestedShapes, cites, derived.PrefixKinds)
	acc := newEvidence()
	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		ex := extractForms(gameID, f.abs, f.rel, f.origin, res, bodies)
		ex.Defs = applyDerivedDefs(gameID, f.abs, f.origin, f.rel, res, ex.Defs, derived, citedByKind)
		bodyRefs, edges, cands := extractRefsAndEdges(
			gameID, res.Root, res.Lines(), f.abs, ex.Defs, derived, schema,
		)
		ex.Refs = append(ex.Refs, bodyRefs...)
		ex.Edges, ex.Cands = edges, cands
		acc.merge(ex)
		return nil
	}); err != nil {
		return acc, derived, dataFns, err
	}

	// Nested databases (CK3 faiths under religions, sub-tier landed titles) only
	// exist as defs after applyDerivedDefs, so the first bind could not see them.
	// Re-bind against the complete set now that it does.
	rebind(derived, schema, acc.defs)

	if err := harvestLocFiles(ctx, gameID, files, bodies, acc); err != nil {
		return acc, derived, dataFns, err
	}
	dropEngineSlotRefs(acc, cache)
	kinds := macroDefKinds(acc.defs, cache)
	acc.refs = append(acc.refs, ApplyCallRefs(acc.cands, kinds)...)
	acc.edges = append(acc.edges, ApplyCallEdges(acc.cands, macroDefKeys(acc.defs, cache))...)
	return acc, derived, dataFns, nil
}

// harvestLocFiles merges loc YAML that loadParsed skipped (not Jomini).
func harvestLocFiles(
	ctx context.Context, gameID string, files []fileRef, bodies bool, acc *Evidence,
) error {
	// walkN rather than walkFiles: the index is the file's load order, and the
	// merge needs it to decide which value wins when two files define one key.
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
		ex := ExtractFile(gameID, f.abs, f.rel, f.origin, text, bodies)
		ex.LocOrder = i
		acc.merge(ex)
		return nil
	})
}

type corpus struct {
	ctx    context.Context
	gameID string
	files  []fileRef
	inline []parsedScript
}

// oneFile is a corpus of a single already-parsed file.
func oneFile(gameID, rel string, res jomini.Result) corpus {
	return corpus{
		gameID: gameID,
		inline: []parsedScript{{f: fileRef{rel: rel}, res: res}},
	}
}

func (c corpus) walk(fn func(fileRef, jomini.Result) error) error {
	if c.inline != nil {
		for _, p := range c.inline {
			if err := fn(p.f, p.res); err != nil {
				return err
			}
		}
		return nil
	}
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

// deriveAll derives the four things no dump states: fire keys, nested databases,
// setup wrappers and loc conventions. The prefix table is not among them — it
// comes from bindKinds, a join against declared types, so without a schema there
// is no prefix table at all rather than a guessed one.
func deriveAll(
	gameID string, corp corpus, idx *defIndex, s *declared.Schema,
	cites, fieldCites []typedCite,
) *Derived {
	ev, oa := idx.keysOfKind("event"), idx.keysOfKind("on_action")
	fireCounts := map[string]*fireCount{}
	wrappers := map[string]bool{}
	defKeys := idx.citableKeys()
	prefix, kindScope := bindPrefixes(s, idx)
	idChild := nestedIDChild(gameID, defKeys, prefix, cites, fieldCites)
	byChild := map[string]*nestedWanted{}
	var mu sync.Mutex
	// One walk for every fact that depends only on a single file. Each corpus
	// walk re-parses the whole install, so folding these together is worth more
	// than any micro-optimisation inside them.
	_ = corp.walk(func(f fileRef, res jomini.Result) error {
		fire := deriveFireKeys(res.Root, ev, oa)
		wraps := deriveWrappers(gameID, f.rel, res.Root, defKeys)
		mu.Lock()
		defer mu.Unlock()
		fireCounts = mergeFireCounts(fireCounts, fire)
		for _, w := range wraps {
			wrappers[w] = true
		}
		if len(idChild) > 0 {
			observeNestedWanted(gameID, f.rel, res.Root, idChild, byChild)
		}
		return nil
	})
	shapes := nestedShapesFrom(byChild, s)
	return &Derived{
		PrefixKinds:  prefix,
		KindScope:    kindScope,
		FireKeys:     resolveFireKinds(fireCounts),
		Wrappers:     wrappers,
		NestedShapes: shapes,
	}
}

func bindPrefixes(s *declared.Schema, idx *defIndex) (prefixKind, kindScope map[string]string) {
	if s.Empty() {
		return nil, nil
	}
	return bindKinds(s, idx)
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
	// Harvest only the workspace language and english. A CK3 install ships
	// eleven, and writing them all cost ~790 MB of sidecars and most of the loc
	// pass. English is kept because coverage compares against it. Any other
	// language is harvested on first use by loadVanillaLoc.
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

// harvestGameInfo reads shipped `_*.info` / `*.md` / readme files into field
// prose and the declared types written beside it.
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
		// Types are filed the same way prose is: globally for the fields whose
		// name means one thing everywhere, and per kind because it does not --
		// `type` is a value set inside a scripted relation and something else
		// entirely inside an event. Per-kind wins at lookup.
		for key, t := range typed {
			if declTypes[key] == "" {
				declTypes[key] = t
			}
			if kind == "" {
				continue
			}
			if declByKind[kind] == nil {
				declByKind[kind] = map[string]string{}
			}
			if declByKind[kind][key] == "" {
				declByKind[kind][key] = t
			}
		}
		for key, doc := range harvested {
			if doc != "" && fieldDocs[key] == "" {
				fieldDocs[key] = doc
			}
			if kind == "" {
				continue
			}
			if structs[kind] == nil {
				structs[kind] = map[string]bool{}
			}
			structs[kind][key] = true
			if doc == "" {
				continue
			}
			if byKind[kind] == nil {
				byKind[kind] = map[string]string{}
			}
			if byKind[kind][key] == "" {
				byKind[kind][key] = doc
			}
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
			if doc == "" {
				continue
			}
			if byKind[kind] == nil {
				byKind[kind] = map[string]string{}
			}
			if byKind[kind][path] == "" {
				byKind[kind][path] = doc
			}
		}
	}
	return fieldDocs, byKind, kindInfo, structs, declTypes, declByKind
}

var dataFnRe = regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)

func enrichDataTypes(dir string, c *VanillaCache) {
	if dir == "" || c == nil {
		return
	}
	seen := map[string]bool{}
	walk := dir
	for range 2 {
		_ = filepath.WalkDir(walk, func(p string, d os.DirEntry, err error) error {
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
		parent := filepath.Dir(walk)
		if parent == walk {
			break
		}
		walk = parent
	}
	c.DataFunctions = sortedKeys(seen)
}

func sortedKeys(set map[string]bool) []string {
	out := slices.Collect(maps.Keys(set))
	slices.Sort(out)
	return out
}

func addCounts(dst *map[string]map[string]int, kind string, src map[string]int) {
	if kind == "" || len(src) == 0 {
		return
	}
	set := (*dst)[kind]
	if set == nil {
		set = map[string]int{}
		(*dst)[kind] = set
	}
	for k, n := range src {
		set[k] += n
	}
}

func keysByCount(m map[string]map[string]int) map[string][]string {
	out := make(map[string][]string, len(m))
	for kind, counts := range m {
		keys := slices.Collect(maps.Keys(counts))
		slices.SortFunc(keys, func(a, b string) int {
			if c := counts[b] - counts[a]; c != 0 {
				return c
			}
			return strings.Compare(a, b)
		})
		out[kind] = keys
	}
	return out
}

func blockKeys(counts, blocks map[string]map[string]int) map[string][]string {
	out := map[string][]string{}
	for kind, c := range counts {
		b := blocks[kind]
		var keys []string
		for k, n := range c {
			if b[k]*2 > n {
				keys = append(keys, k)
			}
		}
		if len(keys) == 0 {
			continue
		}
		slices.Sort(keys)
		out[kind] = keys
	}
	return out
}

const (
	// minNestedHits is how often a member must be seen inside a block before it
	// is part of that block's shape. A block appears far more often than a
	// definition does, so a single sighting is even weaker evidence here than
	// it is for a kind.
	minNestedHits = 3
	// maxNestedMembers bounds one block's member list.
	maxNestedMembers = 40
)

// deriveNestedMembers turns raw block-member counts into the member list for
// each block, most-used first.
//
// This is what lets a query answer "what belongs inside triggered_desc?" rather
// than only "what belongs inside an event?". Keyed by the block's own name: a
// block called limit holds the same members wherever it appears, and merging the
// evidence is what makes the floor mean anything.
func deriveNestedMembers(nested map[string]map[string]int) map[string][]string {
	if len(nested) == 0 {
		return nil
	}
	out := make(map[string][]string, len(nested))
	for parent, counts := range nested {
		members := make([]string, 0, len(counts))
		for k, n := range counts {
			if n >= minNestedHits {
				members = append(members, k)
			}
		}
		if len(members) == 0 {
			continue
		}
		slices.SortFunc(members, func(a, b string) int {
			if c := cmp.Compare(counts[b], counts[a]); c != 0 {
				return c
			}
			return strings.Compare(a, b)
		})
		if len(members) > maxNestedMembers {
			members = members[:maxNestedMembers]
		}
		out[parent] = members
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// unambiguousBlocks names the blocks that appear under exactly one kind.
//
// A block name is not a position: CK3 has an `option` inside an event and
// another inside common/accolade_names, holding nothing in common. Merging them
// by name is right for OFFERING keys — the worst case is a suggestion that does
// not belong — and fatal for judging an absence, which reported 18,341 findings
// on 400 vanilla files. Only a name one kind uses can be judged.
func unambiguousBlocks(blockKinds map[string]map[string]bool) map[string]bool {
	if len(blockKinds) == 0 {
		return nil
	}
	out := map[string]bool{}
	for block, kinds := range blockKinds {
		if len(kinds) == 1 {
			out[block] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func progress(fn func(pct int, msg string), pct int, msg string) {
	if fn != nil {
		fn(pct, msg)
	}
}

// dropEngineSlotRefs removes loc references to interpolations the engine fills
// in — `$EFFECT_LIST_BULLET$`, `$ACTION$` — which no mod can define.
//
// This is the harvest site, not the view: the decision needs the vanilla slot
// set, which only exists once the install has been scanned, and the earliest
// point that has it is here. Filtering the row later would leave a wrong
// reference in the model for hover and find-references to trip over too.
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

// mergeLocOrdered merges one file's localization under LOAD order rather than
// completion order.
//
// The corpus walk is parallel, so when two files of one language define the same
// key, whichever worker happened to finish last decided which value survived.
// That is wrong twice over: load order decides every other override, and the
// answer changed between runs of the same workspace — Workspace Health's
// untranslated count moved by one or two across identical sweeps, which makes it
// useless as a threshold. order is the file's index in the walk list, which is
// mod load order and then walk order within a mod.
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
