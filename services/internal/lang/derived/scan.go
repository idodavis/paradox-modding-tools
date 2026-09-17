// scan.go walks a game install or a workspace's mods into one Index: gather,
// localization, Phase A over every file in parallel, then -- with the
// definition index complete -- Phase B, the shipped prose, the declared type
// system and the data split. A workspace applies the install's schema instead
// of learning one of its own.

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

const scanConcurrency = 8

// ScanRequest is the install harvest input. script_docs come from UserDataDir.
type ScanRequest struct {
	InstallID, GameID, InstallPath, Version, LocLang string
	OnProgress                                       func(pct int, msg string)
}

// Scan harvests installPath into an Index, with the data-row tier split into a second one.
func Scan(ctx context.Context, req ScanRequest) (*Index, *Index, error) {
	info := game.Get(req.GameID)
	if info == nil {
		return nil, nil, os.ErrInvalid
	}
	locLang := req.LocLang
	if locLang == "" {
		locLang = "english"
	}
	step := req.OnProgress
	if step == nil {
		step = func(int, string) {}
	}
	step(5, "listing files")
	inv := gather(scriptRoots(info, req.InstallPath))
	// A game whose script root holds nothing is not an empty game, it is the wrong folder -- and without this guard Scan writes a 0-def cache with a fresh ScannedAt over the good one.
	if len(inv.script) == 0 {
		return nil, nil, fmt.Errorf(
			"no script files under %s: check the install path", req.InstallPath)
	}
	x := NewIndex(req.GameID)
	x.InstallID, x.InstallPath = req.InstallID, req.InstallPath
	x.GameVersion = CacheVersion(req.Version)
	x.ScannedAt = time.Now().UTC().Format(time.RFC3339)

	// The declared type system is read first: binding prefixes to databases needs it during the walk, not after.
	docDirs := scriptDocsDirs(req.GameID, req.InstallPath)
	x.Declared = declared.ReadSchema(docDirs)

	// Localization is read before the script walk because a cell's type is the union of what its values resolve to, and a localization key is one of the answers.
	step(15, "reading localization")
	locRefs, err := harvestLoc(ctx, x, inv.loc, locLang)
	if err != nil {
		return nil, nil, err
	}
	x.Schema.LocEngineSlots = locEngineSlots(x.Loc[locLang], locRefs)
	for _, r := range locRefs {
		if !isLocKind(r.Kind) {
			continue
		}
		// Through the interners like every other record: harvestLoc hands these back before it adds the file, with a path string the file does not persist.
		r.PathID, r.KindID = x.intern(r.Path, "", "", "loc"), x.internKind(r.Kind)
		r.Path, r.Kind = "", ""
		if r.End == r.Start+len(r.Key) {
			r.End = 0
		}
		x.Refs = append(x.Refs, r)
	}

	step(30, "parsing script")
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	dataFns, err := harvest(ctx, x, files, nil, locLang)
	if err != nil {
		return nil, nil, err
	}

	step(80, "reading game info")
	docs, docStructs := harvestGameInfo(req.GameID, inv.info)
	x.Docs = docs
	for kind, keys := range docStructs {
		for k := range keys {
			x.Schema.at(kind, "", k).N++
		}
	}
	step(90, "reading script_docs")
	x.projections(dataFunctions(docDirs, dataFns), locLang)

	// The references an install cache does not keep go before the file is written, and the data rows leave for a tier of their own.
	x.compact()
	data := splitData(x)
	x.SortLocKeys()
	step(100, "done")
	return x, data, nil
}

// projections turns the settled cells and the declared API into the lists the editor reads. Every one of them is a view of something already stored; none is a second opinion about it.
func (x *Index) projections(dataFns []string, locLang string) {
	s := &x.Schema
	vocab := map[string]bool{}
	guiProps := map[string]bool{}
	for kind, byPath := range s.Cells {
		for _, byKey := range byPath {
			for key := range byKey {
				if !jomini.IsLowerName(key) || jomini.IsStop(key) {
					continue
				}
				vocab[key] = true
				if kind == jomini.KindGUIType {
					guiProps[key] = true
				}
			}
		}
	}
	s.GUIProps = sortedKeys(guiProps)
	s.GUITypes = sortedKeys(x.keysOfKind(jomini.KindGUIType))
	s.DataFunctions = dataFns
	// Effects and triggers come only from the declared API: inferring them by position elected every $PARAM$ substitution site as an effect, turning CK3's 1,905 real effects into 10,337 completion entries.
	s.Effects, s.Triggers = nil, nil
	if x.Declared != nil {
		for name := range x.Declared.Effects {
			s.Effects = append(s.Effects, name)
		}
		for name := range x.Declared.Triggers {
			s.Triggers = append(s.Triggers, name)
		}
		slices.Sort(s.Effects)
		slices.Sort(s.Triggers)
		x.Declared.EachName(func(name string) { vocab[name] = true })
	}
	s.EffectSet, s.TriggerSet = sliceSet(s.Effects), sliceSet(s.Triggers)
	s.VocabNames = sortedKeys(vocab)
	s.KindPrefix, s.ScopePrefix, s.ScopeKind = x.citeForms()
	keys := loc.NewKeys(keySet(x.Loc[locLang]))
	s.LocKeyPrefixes = loc.LearnKeyPrefixes(keys)
	_, keysByKind := x.citable()
	s.Kinds = map[string]KindInfo{}
	for kind, ks := range keysByKind {
		// IsObjectKind, not IsCitableDef: a namespace is an event-id prefix with no loc of its own.
		if !jomini.IsObjectKind(kind) {
			continue
		}
		cs := loc.Learn(slices.Collect(maps.Keys(ks)), s.Members(kind, ""), keys)
		if len(cs) == 0 {
			continue
		}
		info := s.Kinds[kind]
		info.Loc = cs
		s.Kinds[kind] = info
	}
}

func sortedKeys(set map[string]bool) []string {
	out := slices.Collect(maps.Keys(set))
	slices.Sort(out)
	return out
}

func sliceSet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	m := make(map[string]bool, len(keys)*2)
	for _, k := range keys {
		m[k] = true
		m[strings.ToLower(k)] = true
	}
	return m
}

func keySet(m map[string]LocEntry) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// BuildWorkspace harvests every mod in load order into the session's own Index, applying the install's schema rather than learning one: mods add members of kinds, not kinds or fields.
func BuildWorkspace(
	ctx context.Context, gameID string, mods []ModInput, vanilla *Index,
) (*Index, error) {
	ordered := slices.Clone(mods)
	slices.SortStableFunc(ordered, func(a, b ModInput) int { return cmp.Compare(a.Order, b.Order) })
	var files []fileRef
	x := NewIndex(gameID)
	if vanilla != nil {
		x.Schema = vanilla.Schema
		// Its own cells, not the install's: a workspace types only the positions the install never wrote at, and must not edit the table it was handed.
		x.Schema.Cells = nil
		x.Schema.MacroDefs = maps.Clone(vanilla.Schema.MacroDefs)
		x.Declared, x.Docs = vanilla.Declared, vanilla.Docs
	}
	if x.Schema.MacroDefs == nil {
		x.Schema.MacroDefs = map[string]string{}
	}
	for _, m := range ordered {
		x.Order = append(x.Order, m.Origin)
		for _, f := range modFiles(m.Root) {
			f.origin = m.Origin
			files = append(files, f)
		}
	}
	_, err := harvest(ctx, x, files, vanilla, "")
	// A reference to an interpolation the engine fills in is not a missing key, and no mod can define one.
	x.dropEngineSlotRefs()
	x.SortLocKeys()
	return x, err
}

// dropEngineSlotRefs removes loc references to interpolations the engine fills in (rule 8). This is the harvest site, not the view: filtering the row later would leave a wrong reference for hover and find-references to trip over too.
func (x *Index) dropEngineSlotRefs() {
	slots := x.Schema.LocEngineSlots
	if len(slots) == 0 {
		return
	}
	dropped := false
	for i := range x.Refs {
		if r := &x.Refs[i]; x.KindOf(r.KindID) == "loc" && slots[r.Key] && r.PathID >= 0 {
			r.PathID = -1
			dropped = true
		}
	}
	if dropped {
		x.reindexRefs()
	}
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
	script, gui, info, loc []fileRef
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
		// Loose .txt at the script root is engine bookkeeping, not script: parsing checksum_manifest.txt as Jomini produced fire edges for "common", "events" and "history".
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

// harvest is the corpus pass: Phase A over every file in parallel, then Phase B once the definition index is complete. locLang empty means a workspace, which applies the install's schema rather than learning one.
func harvest(ctx context.Context, x *Index, files []fileRef, base *Index, locLang string) (map[string]bool, error) {
	names := &interner{}
	hs := make([]*FileHarvest, len(files))
	var mu sync.Mutex
	dataFns := map[string]bool{}
	if err := walkN(ctx, len(files), func(i int) error {
		f := files[i]
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			return nil
		}
		text, _ := jomini.Decode(raw)
		rule := game.MatchExtract(x.GameID, f.rel)
		id := fileID{GameID: x.GameID, Path: filepath.Clean(f.abs), Origin: f.origin,
			Rule: rule, Order: i}
		if rule.Mode == game.ModeLocKey {
			hs[i] = ApplyLoc(id, jomini.Normalize(text))
			return nil
		}
		ff := FactsOf(x.GameID, f.abs, f.rel, f.origin, jomini.Parse(jomini.Normalize(text)))
		ff.Order = i
		names.facts(&ff)
		h := Apply(ff, x.Declared)
		fns := harvestGetSet(ff.Src)
		hs[i] = h
		mu.Lock()
		maps.Copy(dataFns, fns)
		mu.Unlock()
		return nil
	}); err != nil {
		return dataFns, err
	}
	// Records land in file order, so two scans of one install produce the same slices.
	recs := make([]*fileRecs, 0, len(hs))
	for i, h := range hs {
		if h == nil {
			continue
		}
		x.Add(h.Path, files[i].rel, h.Origin,
			ClassifyRel(files[i].rel, filepath.Base(files[i].abs)), h)
		h.Defs, h.Refs, h.Edges = nil, nil, nil
		if h.recs != nil {
			recs = append(recs, h.recs)
		}
		hs[i] = nil
	}
	x.learn(ctx, recs, locLang, base)
	return dataFns, nil
}

// citeForms reverse the bind: kind -> the shortest prefix that cites one of its rows, and scope type -> the prefix and the database a user writes to name one.
func (x *Index) citeForms() (kindPrefix, scopePrefix, scopeKind map[string]string) {
	kindPrefix, scopePrefix, scopeKind = map[string]string{}, map[string]string{}, map[string]string{}
	for prefix, kind := range x.Schema.Prefix {
		k := jomini.CanonicalKind(kind)
		if cur, ok := kindPrefix[k]; !ok || len(prefix) < len(cur) {
			kindPrefix[k] = prefix
		}
	}
	for kind, scope := range x.Schema.KindScope {
		if cur, ok := scopeKind[scope]; !ok || len(kind) < len(cur) {
			scopeKind[scope] = jomini.CanonicalKind(kind)
		}
	}
	for prefix, scope := range x.Declared.Prefixes() {
		if cur, ok := scopePrefix[scope]; !ok || len(prefix) < len(cur) {
			scopePrefix[scope] = prefix
		}
	}
	return kindPrefix, scopePrefix, scopeKind
}

// dataFnRe finds the `GetX` / `SetX` names the data-type dumps declare.
var dataFnRe = regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)

// harvestGetSet collects `GetX` / `SetX` data-function names from one file's source. It runs over 180 MB on CK3, so it scans for the two three-byte prefixes directly instead of running a regex over all of it, which cost 12% of the entire scan.
func harvestGetSet(src string) map[string]bool {
	var out map[string]bool
	for i := 0; i+3 < len(src); i++ {
		if c := src[i]; c != 'G' && c != 'S' {
			continue
		}
		if src[i+1] != 'e' || src[i+2] != 't' {
			continue
		}
		if i > 0 && isDataFnByte(src[i-1]) {
			continue // mid-word: the regex's leading \b
		}
		j := i + 3
		for j < len(src) && isDataFnByte(src[j]) {
			j++
		}
		if j == i+3 {
			continue // bare "Get"/"Set": the pattern needs at least one more byte
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
	return c == '_' || (c >= '0' && c <= '9') ||
		(c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// dataFunctions reads the data-type dumps the game writes beside script_docs, falling back to the corpus harvest when an install ships none.
func dataFunctions(dirs []string, harvested map[string]bool) []string {
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			name := strings.ToLower(e.Name())
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
	if len(seen) == 0 {
		return sortedKeys(harvested)
	}
	return sortedKeys(seen)
}

func locEngineSlots(defined map[string]LocEntry, refs []Ref) map[string]bool {
	out := map[string]bool{}
	for _, r := range refs {
		if r.Kind != "loc" || r.Key == "" {
			continue
		}
		if _, isKey := defined[r.Key]; isKey {
			continue
		}
		out[r.Key] = true
	}
	return out
}

// harvestLoc reads localization into the index, keeping only the workspace language and english: a CK3 install ships eleven, and writing them all cost most of the loc pass.
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
		h := ApplyLoc(fileID{GameID: x.GameID, Path: filepath.Clean(f.abs),
			Rule:  game.ExtractRule{Kind: jomini.KindLocKey, Mode: game.ModeLocKey},
			Order: i}, jomini.Normalize(text))
		mu.Lock()
		if h.Loc.Lang == locLang {
			refs = append(refs, h.Refs...)
		}
		h.Refs = nil
		x.Add(h.Path, f.rel, h.Origin, "loc", h)
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
	if locLang == "" {
		locLang = "english"
	}
	inv := gather(scriptRoots(info, installPath))
	var one []fileRef
	for _, f := range inv.loc {
		if loc.LanguageFromRel(f.abs) == locLang {
			one = append(one, f)
		}
	}
	x := NewIndex(gameID)
	_, err := harvestLoc(ctx, x, one, locLang)
	x.SortLocKeys()
	return x, err
}

// firstWins files one documented name: the first non-empty value seen wins, globally and per kind. A nil global files per kind only; an empty kind files globally only.
func firstWins(global map[string]string, byKind map[string]map[string]string, kind, key, v string) {
	if v == "" {
		return
	}
	if global != nil && global[key] == "" {
		global[key] = v
	}
	if kind == "" {
		return
	}
	m := byKind[kind]
	if m == nil {
		m = map[string]string{}
		byKind[kind] = m
	}
	if m[key] == "" {
		m[key] = v
	}
}

// harvestGameInfo reads shipped `_*.info` / `*.md` / readme files into field prose and the declared types written beside it. Both are filed globally for the names that mean one thing everywhere and per kind because most do not -- `type` is a value set inside a scripted relation and something else entirely inside an event.
func harvestGameInfo(gameID string, docs []fileRef) (Docs, map[string]map[string]bool) {
	d := Docs{
		FieldInfo: map[string]string{}, FieldInfoByKind: map[string]map[string]string{},
		KindInfo:                 map[string]string{},
		DeclaredFieldTypes:       map[string]declared.DeclaredType{},
		DeclaredFieldTypesByKind: map[string]map[string]declared.DeclaredType{},
	}
	rawTypes, rawByKind := map[string]string{}, map[string]map[string]string{}
	structs := map[string]map[string]bool{}
	for _, f := range docs {
		raw, err := os.ReadFile(f.abs)
		if err != nil {
			continue
		}
		kind := game.MatchExtract(gameID, f.rel).Kind
		if kind != "" && d.KindInfo[kind] == "" {
			if prose := game.LeadingKindProse(string(raw)); prose != "" {
				d.KindInfo[kind] = prose
			}
		}
		harvested, typed, nested := game.ParseDocFile(string(raw))
		for key, t := range typed {
			firstWins(rawTypes, rawByKind, kind, key, t)
		}
		for key, doc := range harvested {
			// A documented name is a member of the kind whether or not it came with prose.
			if kind != "" {
				if structs[kind] == nil {
					structs[kind] = map[string]bool{}
				}
				structs[kind][key] = true
			}
			firstWins(d.FieldInfo, d.FieldInfoByKind, kind, key, doc)
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
			firstWins(nil, d.FieldInfoByKind, kind, path, doc)
		}
	}
	// The declared type is classified once, here, rather than on every hover.
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
	d.FieldInfo = normalizeDocMap(d.FieldInfo)
	for kind, m := range d.FieldInfoByKind {
		d.FieldInfoByKind[kind] = normalizeDocMap(m)
	}
	return d, structs
}

func normalizeDocMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if out[lk] == "" {
			out[lk] = v
		}
	}
	return out
}
