// scan.go harvests a game install into VanillaCache: script defs, structure keys,
// vocabulary, loc, shipped game-info prose, GUI types/props, metadata, script_docs.

package catalog

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
)

const locValueLimit = 200
const scanConcurrency = 8

var (
	docKeyRe     = regexp.MustCompile(`^(\s*)([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	attrBulletRe = regexp.MustCompile(`^-\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::|=)\s*(.*)$`)
	tokenRe      = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)`)
)

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
	version := req.Version
	if version == "" {
		version = "latest"
	}
	onProgress := req.OnProgress
	progress(onProgress, 5, "listing files")
	inv := gather(scriptRoots(info, req.InstallPath))

	// The declared type system is read first: binding prefixes to databases
	// needs it during the walk, not after. Live dumps win and refresh PMT's
	// archived copy; the archive stands in when the user's have been removed.
	docDirs := scriptDocsDirs(req.GameID, req.InstallPath)
	schema, _ := loadSchema(req.InstallID, version, docDirs)

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
	fieldInfo, fieldByKind, kindInfo, docStructs := harvestGameInfo(req.GameID, inv.info)
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

	structures := keysByCount(acc.structures)
	nestedMembers := deriveNestedMembers(acc.nested)
	kinds := deriveFieldValueKinds(acc.fieldRHS, acc.defs)
	if derived != nil {
		derived.FieldValueKinds = kinds
	}
	// Effects and triggers are read from the game's own script_docs and live on
	// c.Schema alone; the corpus never contributes one. Inferring them by
	// position elected every $PARAM$ substitution site as an effect: on CK3 that
	// turned 1,905 real effects into 10,337 completion entries, 82% of them
	// noise like `$CHARACTER$.culture`. Data functions are harvested in the
	// first corpus pass, where the source text is already in hand.
	c := &VanillaCache{
		FormatVersion:    CacheFormatVersion,
		InstallID:        req.InstallID,
		GameID:           req.GameID,
		InstallPath:      req.InstallPath,
		GameVersion:      version,
		ScannedAt:        time.Now().UTC().Format(time.RFC3339),
		Defs:             dropEphemeralDefs(acc.defs),
		Edges:            acc.edges,
		LocRefs:          append(locKindRefs(acc.refs), locKindRefs(locFileRefs)...),
		CallRefs:         macroCallRefs(acc.refs),
		FieldValueKinds:  kinds,
		FieldEnumsByKind: deriveFieldEnums(acc.fieldRHSByKind, kinds),
		PrefixKinds:      derived.PrefixKinds,
		KindScope:        derived.KindScope,
		FireKeys:         derived.FireKeys,
		NestedShapes:     derived.NestedShapes,
		Wrappers:         sortedKeys(derived.Wrappers),
		LocAffixes:       deriveLocAffixes(acc.defs, locKeys),
		LocFields:        sortedKeys(derived.LocFields),
		LocListFields:    sortedKeys(derived.LocListFields),
		LocMemberAffixes: deriveLocMemberAffixes(structures, locKeys),
		LocKeyAffixes:    deriveLocKeyAffixes(locKeys),
		KindInfo:         kindInfo,
		FieldInfo:        fieldInfo,
		FieldInfoByKind:  fieldByKind,
		Structures:       structures,
		NestedMembers:    nestedMembers,
		AmbiguousFields:  deriveAmbiguousFields(nestedMembers),
		Snippets:         deriveSnippets(acc.structures, acc.structBlocks, acc.defs),
		StructureBlocks:  blockKeys(acc.structures, acc.structBlocks),
		Vocabulary:       sortedKeys(acc.vocab),
		GUITypes:         sortedKeys(acc.guiTypes),
		GUIProps:         sortedKeys(acc.guiProps),
		DataFunctions:    sortedKeys(dataFns),
		LocEngineSlots:   engineSlots,
		MetaKeys:         readMetaKeys(req.InstallPath, inv.meta),
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
	loc, meta         []string
}

// ClassifyRel buckets a file by name/rel for the install walk and mod indexer.
func ClassifyRel(rel, name string) string {
	lower := strings.ToLower(name)
	slash := strings.ReplaceAll(rel, "\\", "/")
	switch {
	case isGameInfoName(name):
		return "info"
	case strings.HasSuffix(lower, ".gui"):
		return "gui"
	case strings.HasSuffix(lower, ".mod"):
		return "mod"
	case strings.HasSuffix(lower, ".yml"), strings.HasSuffix(lower, ".yaml"):
		if strings.Contains(slash, "localization") {
			return "loc"
		}
	case lower == "metadata.json":
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

// isGameInfoName reports CK3 `_*.info` / `*.md` and EU5 readme/info text files.
// Underscored data (`_default.txt`, `_hardcoded.txt`) stays unclassified.
func isGameInfoName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".md") {
		return true
	}
	if strings.HasPrefix(name, "_") && strings.HasSuffix(lower, ".info") {
		return true
	}
	stem := strings.Trim(strings.TrimSuffix(lower, filepath.Ext(lower)), "_")
	return stem == "readme" || stem == "info"
}

// walkClassified walks root and hands each ClassifyRel-recognized file to fn.
func walkClassified(root string, fn func(kind string, f fileRef)) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if kind := ClassifyRel(rel, d.Name()); kind != "" {
			fn(kind, fileRef{abs: filepath.Clean(p), rel: rel})
		}
		return nil
	})
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
			case "meta":
				inv.meta = append(inv.meta, f.abs)
			case "script":
				inv.script = append(inv.script, f)
			}
		})
	}
	return inv
}

// accum collects FileExtract results from all workers behind one mutex. It is
// the single merge sink for both Scan and BuildIndex.
type accum struct {
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
	nested         map[string]map[string]int
	vocab          map[string]bool
	guiTypes       map[string]bool
	guiProps       map[string]bool
	fieldRHS       map[string]map[string]bool
	fieldListRHS   map[string]map[string]bool
	fieldRHSByKind map[string]map[string]map[string]bool
}

func newAccum() *accum {
	return &accum{
		loc:            map[string]map[string]LocEntry{},
		locOrder:       map[string]map[string]int{},
		structures:     map[string]map[string]int{},
		structBlocks:   map[string]map[string]int{},
		nested:         map[string]map[string]int{},
		vocab:          map[string]bool{},
		guiTypes:       map[string]bool{},
		guiProps:       map[string]bool{},
		fieldRHS:       map[string]map[string]bool{},
		fieldListRHS:   map[string]map[string]bool{},
		fieldRHSByKind: map[string]map[string]map[string]bool{},
	}
}

func (a *accum) merge(ex FileExtract) {
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
	}
	maps.Copy(a.vocab, ex.Vocab)
	maps.Copy(a.guiTypes, ex.GUITypes)
	maps.Copy(a.guiProps, ex.GUIProps)
	for field, vals := range ex.FieldListRHS {
		m := a.fieldListRHS[field]
		if m == nil {
			m = map[string]bool{}
			a.fieldListRHS[field] = m
		}
		maps.Copy(m, vals)
	}
	for field, vals := range ex.FieldRHS {
		m := a.fieldRHS[field]
		if m == nil {
			m = map[string]bool{}
			a.fieldRHS[field] = m
		}
		maps.Copy(m, vals)
		if ex.StructKind == "" {
			continue
		}
		kindM := a.fieldRHSByKind[ex.StructKind]
		if kindM == nil {
			kindM = map[string]map[string]bool{}
			a.fieldRHSByKind[ex.StructKind] = kindM
		}
		km := kindM[field]
		if km == nil {
			km = map[string]bool{}
			kindM[field] = km
		}
		maps.Copy(km, vals)
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
	cache *VanillaCache, schema *Schema, locKeys map[string]bool,
) (*accum, *Derived, map[string]bool, error) {
	corp := corpus{ctx: ctx, gameID: gameID, files: files}
	dataFns := map[string]bool{}
	fieldRHS := map[string]map[string]bool{}
	fieldListRHS := map[string]map[string]bool{}
	var baseDefs []Def
	var mu sync.Mutex

	// First pass gathers only what the derivations need: every def key, the
	// values each field takes, and the data functions in the source text.
	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		ex := extractForms(gameID, f.abs, f.rel, f.origin, res, bodies)
		fns := harvestGetSet(res.Src)
		mu.Lock()
		defer mu.Unlock()
		baseDefs = append(baseDefs, ex.Defs...)
		maps.Copy(dataFns, fns)
		for field, vals := range ex.FieldRHS {
			m := fieldRHS[field]
			if m == nil {
				m = map[string]bool{}
				fieldRHS[field] = m
			}
			maps.Copy(m, vals)
		}
		for field, vals := range ex.FieldListRHS {
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
	local, cites := deriveAll(gameID, corp, baseDefs, schema, fieldRHS)
	derived.mergeLocal(local)
	if derived.FieldValueKinds == nil {
		derived.FieldValueKinds = map[string]string{}
	}
	for k, v := range deriveFieldValueKinds(fieldRHS, baseDefs) {
		if derived.FieldValueKinds[k] == "" {
			derived.FieldValueKinds[k] = v
		}
	}
	// The install derives its own; a mod walk keeps what the cache carried, so
	// a mod's `last_name` is read as localization for the same reason vanilla's
	// is.
	if len(locKeys) > 0 {
		defKeys := defKeySet(baseDefs)
		derived.LocFields = deriveLocFields(fieldRHS, locKeys, defKeys, schema)
		derived.LocListFields = deriveLocFields(fieldListRHS, locKeys, defKeys, schema)
	}
	baseDefs = nil

	// Second pass builds the model. The derived facts are complete now, so
	// wrapper defs drop, nested defs appear, and refs and edges resolve. The
	// citations are indexed once, corpus-wide: a nested database is routinely
	// declared in one file and cited from another.
	citedByKind := citedIDsByKind(derived.NestedShapes, cites, derived.PrefixKinds)
	acc := newAccum()
	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		ex := extractForms(gameID, f.abs, f.rel, f.origin, res, bodies)
		ex.Defs = applyDerivedDefs(gameID, f.abs, f.origin, f.rel, res, ex.Defs, derived, citedByKind)
		ex.Refs = append(ex.Refs, conventionLocRefs(res.Lines(), f.abs, ex.Defs, derived)...)
		bodyRefs, edges, cands := extractRefsAndEdges(
			gameID, res.Root, res.Lines(), f.abs, ex.Defs, derived,
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
	rebind(derived, schema, acc.defs, cites)

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
	ctx context.Context, gameID string, files []fileRef, bodies bool, acc *accum,
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

// corpus is a set of script files that can be walked repeatedly. Each walk
// re-reads and re-parses; nothing is kept between them, and fn must not retain
// the tree it is handed. That is the whole point: a derivation that needs the
// corpus twice costs another half-second of parsing rather than gigabytes of
// retained syntax trees.
//
// inline replaces the disk walk with already-parsed files, for the live reindex
// of a single open buffer.
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
	gameID string, corp corpus, defs []Def, s *Schema,
	fieldRHS map[string]map[string]bool,
) (*Derived, []typedCite) {
	ev, oa := eventOnActionIDs(defs)
	var cites, fieldCites []typedCite
	fireCounts := map[string]*fireCount{}
	wrappers := map[string]bool{}
	owners := map[optionOwner]*ownerKeys{}
	defKeys := defKeySet(defs)
	var mu sync.Mutex
	// One walk for every fact that depends only on a single file. Each corpus
	// walk re-parses the whole install, so folding these together is worth more
	// than any micro-optimisation inside them.
	_ = corp.walk(func(f fileRef, res jomini.Result) error {
		c := collectTypedCites(res.Root)
		fc := collectFieldCites(res.Root)
		fire := deriveFireKeys(res.Root, ev, oa)
		wraps := deriveWrappers(gameID, f.rel, res.Root, defKeys)
		mu.Lock()
		defer mu.Unlock()
		cites = append(cites, c...)
		fieldCites = append(fieldCites, fc...)
		fireCounts = mergeFireCounts(fireCounts, fire)
		for _, w := range wraps {
			wrappers[w] = true
		}
		addOptionOwners(gameID, f.rel, res.Root, owners)
		return nil
	})
	prefix, kindScope := bindPrefixes(s, defs, cites)
	shapes := deriveNestedShapes(gameID, corp, defs, prefix, s, cites, fieldCites)
	// Option databases are found through fields rather than typed cites, so they
	// are a separate join, but they harvest through the same NestedShape path.
	shapes = append(shapes, deriveNestedOptions(gameID, defs, fieldRHS, owners)...)
	return &Derived{
		PrefixKinds:  prefix,
		KindScope:    kindScope,
		FireKeys:     resolveFireKinds(fireCounts),
		Wrappers:     wrappers,
		NestedShapes: shapes,
	}, cites
}

// bindPrefixes resolves the typed-prefix table from the game's declared links.
// Without script_docs there is no table: inferring one from usage on CK3 missed
// every prefix that matters (culture, title, trait, character, province) while
// inventing thirteen that are not prefixes at all, which is what made hover
// report the wrong kind.
func bindPrefixes(s *Schema, defs []Def, cites []typedCite) (prefixKind, kindScope map[string]string) {
	if s.Empty() {
		return nil, nil
	}
	prefixKind, kindScope, _ = bindKinds(s, defs, cites)
	return prefixKind, kindScope
}

// rebind re-runs the type join once nested defs exist, keeping whichever
// binding explains more of the citations.
func rebind(v *Derived, s *Schema, defs []Def, cites []typedCite) {
	if v == nil || s.Empty() || len(defs) == 0 {
		return
	}
	prefixKind, kindScope, _ := bindKinds(s, defs, cites)
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

// locEngineSlots are the `$NAME$` interpolations the game's own localization
// uses but never defines. The engine substitutes those; no mod can write them,
// so they must never be demanded as a missing loc key.
//
// This is evidence, not a guess: vanilla's usage is the declaration. On CK3 it
// yields 354 names out of 1,138 distinct ALL-CAPS interpolations, and
// `$EFFECT_LIST_BULLET$` alone accounted for the largest false "missing"
// cluster in the whole workshop corpus.
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

// harvestGameInfo reads shipped `_*.info` / `*.md` / readme files into field prose.
func harvestGameInfo(gameID string, docs []fileRef) (
	fieldDocs map[string]string,
	byKind map[string]map[string]string,
	kindInfo map[string]string,
	structs map[string]map[string]bool,
) {
	fieldDocs, byKind, structs = map[string]string{}, map[string]map[string]string{}, map[string]map[string]bool{}
	kindInfo = map[string]string{}
	for _, f := range docs {
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			continue
		}
		kind := game.MatchExtract(gameID, f.rel).Kind
		if kind != "" && kindInfo[kind] == "" {
			if prose := leadingKindProse(string(raw)); prose != "" {
				kindInfo[kind] = prose
			}
		}
		for key, doc := range harvestDocFile(string(raw)) {
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
	}
	return fieldDocs, byKind, kindInfo, structs
}

// harvestDocFile extracts field prose from `_*.info`, markdown, and commented
// READMEs. Comments above a key, on the same line, or immediately before a
// closing `}` (EU5 `key = { # desc }` / below-the-brace) all attach. Fully
// hashed lines are peeled so templates still yield `key = value` pairs.
// `# - key: desc` attribute lists are harvested.
func harvestDocFile(text string) map[string]string {
	out := map[string]string{}
	var pending []string
	lastKey := ""
	for _, raw := range strings.Split(text, "\n") {
		body, hashed := peelDocLine(raw)
		if body == "" {
			pending = pending[:0]
			continue
		}
		if m := attrBulletRe.FindStringSubmatch(body); m != nil {
			putFieldDoc(out, m[1], attrProse(m[2]))
			pending = pending[:0]
			lastKey = ""
			continue
		}
		if m := docKeyRe.FindStringSubmatch(body); m != nil {
			key := strings.ToLower(m[2])
			inline := inlineHashDoc(m[3])
			putFieldDoc(out, key, prose(append(pending, inline)))
			if nameOKRe.MatchString(key) && !stoplist[key] {
				lastKey = key
			} else {
				lastKey = ""
			}
			pending = pending[:0]
			continue
		}
		if body == "}" || strings.HasPrefix(body, "}") {
			if lastKey != "" {
				appendFieldDoc(out, lastKey, prose(pending))
			}
			pending = pending[:0]
			continue
		}
		if hashed {
			pending = append(pending, body)
			continue
		}
		pending = pending[:0]
	}
	return out
}

// peelDocLine strips leading `#` markers so commented READMEs parse as script.
func peelDocLine(raw string) (body string, hashed bool) {
	t := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if t == "" {
		return "", false
	}
	hashed = strings.HasPrefix(t, "#")
	for strings.HasPrefix(t, "#") {
		t = strings.TrimSpace(t[1:])
	}
	return t, hashed
}

func inlineHashDoc(rhs string) string {
	h := strings.IndexByte(rhs, '#')
	if h < 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimLeft(rhs[h+1:], "# "))
}

// attrProse is the description after `# - key: <type> …` / `= { … }: …`.
func attrProse(rhs string) string {
	rhs = strings.TrimSpace(rhs)
	if i := strings.Index(rhs, "}:"); i >= 0 {
		return strings.TrimSpace(rhs[i+2:])
	}
	if h := strings.IndexByte(rhs, '#'); h >= 0 {
		return strings.TrimSpace(rhs[h+1:])
	}
	if strings.HasPrefix(rhs, "{") {
		return ""
	}
	return rhs
}

func putFieldDoc(out map[string]string, key, doc string) {
	key = strings.ToLower(key)
	if !nameOKRe.MatchString(key) || stoplist[key] {
		return
	}
	if _, seen := out[key]; !seen || (out[key] == "" && doc != "") {
		out[key] = doc
	}
}

func appendFieldDoc(out map[string]string, key, extra string) {
	if extra == "" || key == "" {
		return
	}
	if out[key] == "" {
		out[key] = extra
		return
	}
	out[key] = prose([]string{out[key], extra})
}

// readMetaKeys collects top-level keys from .metadata/metadata.json files.
func readMetaKeys(installPath string, metas []string) []string {
	set := map[string]bool{}
	paths := append([]string{filepath.Join(installPath, ".metadata", "metadata.json")}, metas...)
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var obj map[string]any
		if sonic.Unmarshal(raw, &obj) != nil {
			continue
		}
		for k := range obj {
			set[k] = true
		}
	}
	return sortedKeys(set)
}

var dataFnRe = regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)

func enrichDataTypes(dir string, c *VanillaCache) {
	if dir == "" || c == nil {
		return
	}
	seen := map[string]bool{}
	walk := dir
	for i := 0; i < 2; i++ {
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

func progress(fn func(pct int, msg string), pct int, msg string) {
	if fn != nil {
		fn(pct, msg)
	}
}

func prose(parts []string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.Join(parts, " ")), " "))
}

// dropEngineSlotRefs removes loc references to interpolations the engine fills
// in — `$EFFECT_LIST_BULLET$`, `$ACTION$` — which no mod can define.
//
// This is the harvest site, not the view: the decision needs the vanilla slot
// set, which only exists once the install has been scanned, and the earliest
// point that has it is here. Filtering the row later would leave a wrong
// reference in the model for hover and find-references to trip over too.
func dropEngineSlotRefs(acc *accum, cache *VanillaCache) {
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
func (a *accum) mergeLocOrdered(order int, d LocDelta) {
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

// authorProse joins comment lines while keeping the author's line breaks.
//
// prose reflows into a single paragraph, which is right for the game's own dump
// and info-file text: that is prose the game wrapped for a fixed console width,
// so its line endings carry no meaning. A comment a modder wrote above a
// definition is different — they chose where the lines end, and folding a
// four-line note into one wrapped run loses a list, a worked example, or a
// heading. Each line is still normalised internally so stray alignment spaces
// do not survive.
func authorProse(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.Join(strings.Fields(p), " "); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}
