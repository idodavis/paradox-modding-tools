// derive.go works out what the install implies but never states: fire keys,
// nested databases, setup wrappers, field value kinds, block shapes and loc
// conventions. Everything the game declares outright is read from script_docs
// by the declared package instead, and outranks anything here.
//
// These derivations resolve conflicts by dropping, so one odd usage anywhere in
// the corpus silently deletes a fact. Tolerable for conventions, and not
// tolerable for a type system -- which is why the type system is read, not
// inferred. Scan persists the results; BuildIndex merges mod overlays on top.

package derived

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Derived is the in-memory overlay ExtractParsed and the second corpus pass consume. PrefixKinds and KindScope come from bindKinds joining the game's declared types to the harvested databases; the rest is inferred from the corpus.
type Derived struct {
	PrefixKinds     map[string]string
	KindScope       map[string]string
	FireKeys        map[string]string
	NestedShapes    []NestedShape
	Wrappers        map[string]bool
	FieldValueKinds map[string]string
	LocFields       map[string]bool
	// CitedByKind is the whole corpus's citations by nested child kind. The corpus pass must set it before it shares one *Derived across workers: nil means "this caller holds one file", which makes ExtractFacts derive that file's own facts and WRITE them back here.
	CitedByKind map[string]map[string]bool
}

// derivedFromCache copies the persisted derived maps so a live reindex can overlay one file onto them. Nil cache yields empty derived.
func derivedFromCache(c *VanillaCache) *Derived {
	v := &Derived{
		PrefixKinds:     map[string]string{},
		KindScope:       map[string]string{},
		FireKeys:        map[string]string{},
		Wrappers:        map[string]bool{},
		FieldValueKinds: map[string]string{},
		LocFields:       map[string]bool{},
	}
	if c == nil {
		return v
	}
	maps.Copy(v.KindScope, c.KindScope)
	maps.Copy(v.PrefixKinds, c.PrefixKinds)
	maps.Copy(v.FireKeys, c.FireKeys)
	maps.Copy(v.FieldValueKinds, c.FieldValueKinds)
	for _, f := range c.LocFields {
		v.LocFields[f] = true
	}
	for _, w := range c.Wrappers {
		v.Wrappers[w] = true
	}
	v.NestedShapes = slices.Clone(c.NestedShapes)
	return v
}

func (v *Derived) fireKind(key string) string {
	if v == nil || loc.Classify(key) != loc.PropNone {
		return ""
	}
	if k := v.FireKeys[key]; k != "" {
		return k
	}
	return v.FireKeys[strings.ToLower(key)]
}

func (v *Derived) fieldKind(key string) string {
	if v == nil {
		return ""
	}
	if k := v.FieldValueKinds[key]; k != "" {
		return k
	}
	return v.FieldValueKinds[strings.ToLower(key)]
}

// locField reports a script property the corpus shows holds localization keys, beyond the engine property names loc lists outright. Its LIST members are keys too.
func (v *Derived) locField(key string) bool {
	if v == nil {
		return false
	}
	return v.LocFields[key] || v.LocFields[strings.ToLower(key)]
}

func (v *Derived) mergeLocal(local *Derived) {
	if local == nil {
		return
	}
	v.PrefixKinds = mergeStringMap(v.PrefixKinds, local.PrefixKinds)
	// KindScope travels with PrefixKinds: one bindKinds call produces both, and dropping it here left a cache with PrefixKinds populated and KindScope empty, which session/scope.go reads as a silent fallback.
	v.KindScope = mergeStringMap(v.KindScope, local.KindScope)
	v.FireKeys = mergeStringMap(v.FireKeys, local.FireKeys)
	v.FieldValueKinds = mergeStringMap(v.FieldValueKinds, local.FieldValueKinds)
	for f := range local.LocFields {
		v.LocFields[f] = true
	}
	for k, ok := range local.Wrappers {
		if ok {
			v.Wrappers[k] = true
		}
	}
	v.NestedShapes = mergeShapes(v.NestedShapes, local.NestedShapes)
}

// mergeStringMap overlays mods on vanilla; a value conflict drops the key.
func mergeStringMap(vanilla, mods map[string]string) map[string]string {
	if len(vanilla) == 0 && len(mods) == 0 {
		return nil
	}
	out := maps.Clone(vanilla)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range mods {
		if prev, ok := out[k]; ok && prev != v {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeShapes(a, b []NestedShape) []NestedShape {
	if len(a) == 0 {
		return slices.Clone(b)
	}
	seen := map[string]bool{}
	var out []NestedShape
	add := func(s NestedShape) {
		k := s.ParentKind + "\x00" + s.ChildKind + "\x00" + s.GroupKey
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, s := range a {
		add(s)
	}
	for _, s := range b {
		add(s)
	}
	return out
}

// defIndex groups a def slice the ways the derivations ask for it, under IsCitableDef because that is what most of them want. IsObjectKind -- deriveLocConventions' rule -- is this filter minus `namespace`, so it narrows from here rather than needing its own walk. One index per slice, never shared across two: rebind runs after nested defs exist and the mod path prepends the cache's defs, so those hold slices this one does not describe and build their own.
type defIndex struct {
	// kindsByKey is key -> the distinct citable kinds defined under it.
	kindsByKey map[string][]string
	// keysByKind is citable kind -> its distinct keys.
	keysByKind map[string]map[string]bool
}

func newDefIndex(defs []Def) *defIndex {
	x := &defIndex{
		kindsByKey: make(map[string][]string, len(defs)/2),
		keysByKind: make(map[string]map[string]bool, 64),
	}
	for _, d := range defs {
		if !IsCitableDef(d) {
			continue
		}
		k := jomini.CanonicalKind(d.Kind)
		set := x.keysByKind[k]
		if set == nil {
			set = map[string]bool{}
			x.keysByKind[k] = set
		}
		// One membership test dedupes both maps: the key is new for this kind exactly when the kind is new for this key.
		if !set[d.Key] {
			set[d.Key] = true
			x.kindsByKey[d.Key] = append(x.kindsByKey[d.Key], k)
		}
	}
	return x
}

// citableKeys is every name already spoken for by a real definition; passes that invent a database from unexplained citations subtract it first.
func (x *defIndex) citableKeys() map[string]bool {
	out := make(map[string]bool, len(x.kindsByKey))
	for key := range x.kindsByKey {
		out[key] = true
	}
	return out
}

// keysOfKind is the distinct keys defined under one canonical kind; nil when the install has none, and callers only read it.
func (x *defIndex) keysOfKind(kind string) map[string]bool { return x.keysByKind[kind] }

const (
	// fieldKindCoverage is the share of a field's resolving values that must agree, and minFieldHits how many must resolve at all.
	fieldKindCoverage = 50
	minFieldHits      = 2
)

// deriveFieldValueKinds types a field from the definitions its values name: `field = value` where enough values resolve and they agree on one kind. No dump states which field holds which type, so this is install-derived.
func deriveFieldValueKinds(rhs map[string]map[string]bool, idx *defIndex) map[string]string {
	kindsByKey := idx.kindsByKey
	out := map[string]string{}
	for field, vals := range rhs {
		// A bare number is a weight, not a field name: typing `20` as `event` made `20 = empty` inside a Vic3 gene block read as a missing event.
		if isDigitKey(field) {
			continue
		}
		var kind string
		ok, conflict, hits := false, false, 0
		for v := range vals {
			kinds := kindsByKey[v]
			// A value naming two kinds is no evidence; as a veto it left `holder` and 116 more fields typed by nothing, since every CK3 id is a character AND a province.
			if len(kinds) != 1 {
				continue
			}
			if !ok {
				kind, ok = kinds[0], true
			} else if kinds[0] != kind {
				conflict = true
				break
			}
			hits++
		}
		if ok && !conflict && hits >= minFieldHits &&
			hits*100 >= len(vals)*fieldKindCoverage {
			out[field] = kind
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const (
	// locFieldCoverage is higher than fieldKindCoverage because the target namespace is enormous: an install defines hundreds of thousands of loc keys, so a value coinciding with one proves much less than one coinciding with a definition of some specific kind.
	minLocFieldHits  = 8
	locFieldCoverage = 80
)

// deriveLocFields decides which script properties hold localization keys.
func deriveLocFields(
	rhs map[string]map[string]bool, locKeys, defKeys map[string]bool, schema *declared.Schema,
) map[string]bool {
	if len(locKeys) == 0 || len(rhs) == 0 {
		return nil
	}
	out := map[string]bool{}
	for field, vals := range rhs {
		if isDigitKey(field) || loc.Classify(field) != loc.PropNone {
			continue // already declared by the engine property list
		}
		// Declared beats derived. `has_realm_law` and `unlock_law` are triggers and effects the game states outright; that their values are law ids with keys named after them is a fact about laws, not about the token.
		if schema.HasName(field) {
			continue
		}
		// A macro-parameterised key (`has_$TYPE$_law`) is a spelling of a token, not a property in its own right.
		if strings.ContainsRune(field, '$') {
			continue
		}
		hits, of := 0, 0
		for v := range vals {
			if defKeys[v] {
				continue
			}
			of++
			if locKeys[v] {
				hits++
			}
		}
		if hits >= minLocFieldHits && hits*100 >= of*locFieldCoverage {
			out[field] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func locKeySet(sites map[string]LocEntry) map[string]bool {
	out := map[string]bool{}
	for k := range sites {
		out[k] = true
	}
	return out
}

// locConventionFloor is the share of a kind's definitions a template must hold for.
const locConventionFloor = 0.9

// deriveLocConventions reads the install's own localization to work out which key templates each kind uses.
func deriveLocConventions(
	idx *defIndex, structures map[string][]string, keys *loc.Keys,
) map[string][]loc.Convention {
	if keys == nil || len(idx.keysByKind) == 0 {
		return nil
	}
	out := map[string][]loc.Convention{}
	for kind, ks := range idx.keysByKind {
		// IsObjectKind, not IsCitableDef: a namespace is an event-id prefix with no loc of its own.
		if !jomini.IsObjectKind(kind) {
			continue
		}
		cs := loc.Learn(slices.Collect(maps.Keys(ks)),
			structures[kind], keys, locConventionFloor)
		if len(cs) > 0 {
			out[kind] = cs
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fireCount is the evidence for one assignment key: how often its RHS named an event or on_action, against how often it named anything at all.
type fireCount struct {
	kinds map[string]int
	named int
}

const (
	// fireKeyCoverage is the share of a key's named RHS values that must be events or on_actions before the key is treated as firing them. Unanimity alone made one coincidence a fire site, which produced 4,033 false dangling rows on one Vic3 mod.
	fireKeyCoverage = 50
	// minFireNames is how many names a key must carry before coverage may veto it: a mod whose trigger_event targets mostly do not exist has broken references, and silencing the key would hide exactly the defect Workspace Health is for.
	minFireNames = 8
)

// deriveFireKeys counts, per assignment key, how many of the names on its RHS are harvested event or on_action ids (or the event-id form `ns.1`).
func deriveFireKeys(ff FileFacts, eventIDs, onActionIDs map[string]bool) map[string]*fireCount {
	out := map[string]*fireCount{}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key == "" || f.Quoted || jomini.IsStop(f.Key) ||
			loc.Classify(f.Key) != loc.PropNone || strings.EqualFold(f.Key, "id") {
			continue
		}
		for _, to := range ff.fireNames(int32(i)) {
			m := out[f.Key]
			if m == nil {
				m = &fireCount{kinds: map[string]int{}}
				out[f.Key] = m
			}
			m.named++
			if kind := classifyFireTarget(to, eventIDs, onActionIDs); kind != "" {
				m.kinds[kind]++
			}
		}
	}
	return out
}

// fireNames is the assignment's own RHS: a scalar, list values, or inner id=/digit keys. Nested fire sites stay on their own keys.
func (ff *FileFacts) fireNames(i int32) []string {
	f := ff.Facts[i]
	var out []string
	if !f.Block {
		if !f.VQuoted && fireTargetOK(f.Value) {
			out = append(out, f.Value)
		}
		return out
	}
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i || c.Block || c.VQuoted || !fireTargetOK(c.Value) {
			continue
		}
		key := strings.ToLower(c.Key)
		if c.Key == "" || key == "id" || isDigitKey(key) {
			out = append(out, c.Value)
		}
	}
	return out
}

func classifyFireTarget(s string, eventIDs, onActionIDs map[string]bool) string {
	if eventIDs[s] || jomini.IsEventID(s) {
		return "event"
	}
	if onActionIDs[s] {
		return "on_action"
	}
	return ""
}

func resolveFireKinds(counts map[string]*fireCount) map[string]string {
	out := map[string]string{}
	for key, c := range counts {
		if c == nil || len(c.kinds) != 1 {
			continue
		}
		hits := 0
		for _, n := range c.kinds {
			hits += n
		}
		if c.named >= minFireNames && hits*100 < c.named*fireKeyCoverage {
			continue
		}
		for k := range c.kinds {
			out[key] = k
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeFireCounts(dst, src map[string]*fireCount) map[string]*fireCount {
	if dst == nil {
		dst = map[string]*fireCount{}
	}
	for key, c := range src {
		m := dst[key]
		if m == nil {
			m = &fireCount{kinds: map[string]int{}}
			dst[key] = m
		}
		m.named += c.named
		for k, n := range c.kinds {
			m.kinds[k] += n
		}
	}
	return dst
}

func nestedChildKind(gameID, prefix, id string, prefixKinds map[string]string) string {
	if prefixKinds != nil {
		if k := prefixKinds[prefix]; k != "" {
			return k
		}
	}
	if k, _, ok := game.ParseTyped(gameID, prefix+":"+id); ok {
		return k
	}
	return jomini.CanonicalKind(prefix)
}

func addNestedCite(
	dst map[string][]string, defKeys map[string]bool, gameID string,
	c typedCite, prefixKinds map[string]string,
) {
	if c.id == "" || defKeys[c.id] || jomini.IsStop(c.id) {
		return
	}
	childKind := nestedChildKind(gameID, c.prefix, c.id, prefixKinds)
	if childKind == "" || childKind == "type" {
		return
	}
	if slices.Contains(dst[c.id], childKind) {
		return
	}
	dst[c.id] = append(dst[c.id], childKind)
}

// nestedWanted is where one cited-but-undefined child kind was found: the parent it sits under, the group key inside that parent, and the first-letter set of its keys.
type nestedWanted struct {
	parent, group string
	ok, clash     bool
	letters       map[byte]bool
}

// nestedIDChild is the citation half of the join -- which child kinds are cited and have no definition -- and it needs no walk of its own.
func nestedIDChild(
	gameID string, defKeys map[string]bool, prefixKinds map[string]string,
	cites, fieldCites []typedCite,
) map[string][]string {
	idChild := map[string][]string{}
	for _, c := range cites {
		addNestedCite(idChild, defKeys, gameID, c, prefixKinds)
	}
	for _, c := range fieldCites {
		addNestedCite(idChild, defKeys, gameID, c, prefixKinds)
	}
	return idChild
}

// nestedShapesFrom turns the accumulator into shapes, keeping only child kinds the game declares.
func nestedShapesFrom(byChild map[string]*nestedWanted, schema *declared.Schema) []NestedShape {
	var out []NestedShape
	for child, a := range byChild {
		if !a.ok || a.clash || a.parent == "" {
			continue
		}
		s := NestedShape{ParentKind: a.parent, ChildKind: child, GroupKey: a.group}
		if len(a.letters) > 0 {
			var b strings.Builder
			keys := slices.Collect(maps.Keys(a.letters))
			slices.Sort(keys)
			for _, c := range keys {
				b.WriteByte(c)
			}
			s.KeyPrefix = b.String()
		}
		if !schema.Declares(s.ChildKind) {
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b NestedShape) int {
		if a.ParentKind != b.ParentKind {
			return strings.Compare(a.ParentKind, b.ParentKind)
		}
		return strings.Compare(a.ChildKind, b.ChildKind)
	})
	return out
}

func keyPrefixOK(key, prefixSet string) bool {
	if prefixSet == "" || len(key) < 3 || key[1] != '_' {
		return prefixSet == ""
	}
	return strings.IndexByte(prefixSet, key[0]) >= 0
}
func citedIDsForKind(cites []typedCite, prefixKinds map[string]string, childKind string) map[string]bool {
	out := map[string]bool{}
	ck := jomini.CanonicalKind(childKind)
	for _, c := range cites {
		k := ""
		if prefixKinds != nil {
			k = prefixKinds[c.prefix]
		}
		if k == "" {
			k = jomini.CanonicalKind(c.prefix)
		}
		if jomini.CanonicalKind(k) == ck {
			out[c.id] = true
		}
	}
	return out
}

func dropWrapperDefs(defs []Def, wrappers map[string]bool) []Def {
	if len(wrappers) == 0 {
		return defs
	}
	out := defs[:0]
	for _, d := range defs {
		if wrappers[d.Key] || wrappers[strings.ToLower(d.Key)] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// harvestGetSet collects `GetX` / `SetX` data-function names. It runs over the whole source of every file in the install -- 180 MB on CK3 -- so it scans for the two three-byte prefixes directly instead of running a regex over all of it, which cost 12% of the entire scan.
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

// keysByCount orders each kind's harvested member names by how often they were seen.
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

// blockKeys names the members of each kind that are usually written as a block.
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
	// minNestedHits is how often a member must be seen inside a block before it is part of that block's shape. A block appears far more often than a definition does, so a single sighting is even weaker evidence here.
	minNestedHits = 3
	// maxNestedMembers bounds one block's member list.
	maxNestedMembers = 40
)

// deriveNestedMembers turns raw block-member counts into each block's member list, most-used first -- what answers "what belongs inside triggered_desc?" rather than only "inside an event?". Keyed by the block's own name, because a block called `limit` holds the same members wherever it appears.
func deriveNestedMembers(nested map[string]map[string]int) map[string][]string {
	if len(nested) == 0 {
		return nil
	}
	kept := make(map[string]map[string]int, len(nested))
	for parent, counts := range nested {
		hits := map[string]int{}
		for k, n := range counts {
			if n >= minNestedHits {
				hits[k] = n
			}
		}
		if len(hits) > 0 {
			kept[parent] = hits
		}
	}
	out := keysByCount(kept)
	for parent, members := range out {
		if len(members) > maxNestedMembers {
			out[parent] = members[:maxNestedMembers]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// unambiguousBlocks names the blocks that appear under exactly one kind. A block name is not a position: CK3 has an `option` inside an event and another inside common/accolade_names, holding nothing in common. Merging them by name is right for OFFERING keys and fatal for judging an absence, which reported 18,341 findings on 400 vanilla files.
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

// deriveFacts derives one file's facts for a live reindex. The prefix table stays empty: prefixes are bound against the whole install by bindKinds, and one edited file is not evidence enough to rebind anything.
func deriveFacts(ff FileFacts, idx *defIndex, schema *declared.Schema) *Derived {
	fire := resolveFireKinds(deriveFireKeys(
		ff, idx.keysOfKind("event"), idx.keysOfKind("on_action")))
	if fire == nil {
		fire = map[string]string{}
	}
	v := &Derived{
		PrefixKinds: map[string]string{},
		FireKeys:    fire,
		Wrappers:    map[string]bool{},
	}
	defKeys := idx.citableKeys()
	for _, w := range deriveWrappers(ff, defKeys) {
		v.Wrappers[w] = true
	}
	byChild := map[string]*nestedWanted{}
	cites, fieldCites := foldCites(ff)
	observeNestedFacts(ff, nestedIDChild(
		ff.GameID, defKeys, v.PrefixKinds, cites, fieldCites), byChild)
	v.NestedShapes = nestedShapesFrom(byChild, schema)
	return v
}

// observeNestedFacts is the per-file half of the nested join: every nested object key (depth > 0) some citation wants, filed under the parent kind and the depth-1 block it sits in. Accumulation is commutative -- a clash flag, a group that nulls on disagreement, a letter set -- so it does not care in what order files arrive.
func observeNestedFacts(ff FileFacts, idChild map[string][]string, byChild map[string]*nestedWanted) {
	if len(idChild) == 0 {
		return
	}
	// wrap[i] is the fact naming the depth-1 block fact i sits under, which is the group key a shape records; -1 directly under a definition.
	wrap := make([]int32, len(ff.Facts))
	for i := range ff.Facts {
		f := ff.Facts[i]
		at := int32(-1)
		if f.Parent >= 0 {
			at = wrap[f.Parent]
		}
		wrap[i] = at
		if f.Key == "" || !f.Block {
			continue
		}
		if f.Depth == 0 {
			wrap[i] = -1
			continue
		}
		name := game.KeyIdentity(ff.GameID, f.Key)
		if at < 0 {
			wrap[i] = int32(i)
		}
		if len(idChild[name]) == 0 {
			continue
		}
		w := ""
		if at >= 0 {
			w = game.KeyIdentity(ff.GameID, ff.Facts[at].Key)
		}
		for _, ck := range idChild[name] {
			a := byChild[ck]
			if a == nil {
				a = &nestedWanted{
					parent: ff.Rule.Kind, group: w, ok: true, letters: map[byte]bool{},
				}
				byChild[ck] = a
			}
			if a.parent != ff.Rule.Kind {
				a.clash = true
			}
			if a.group != w {
				a.group = ""
			}
			if a.letters != nil && len(name) > 2 && name[1] == '_' {
				a.letters[name[0]] = true
			} else if a.letters != nil {
				a.letters = nil
			}
		}
	}
}

// deriveWrappers marks top-level blocks whose inners are typed cites to existing defs (Vic3 COUNTRIES = { c:USA ?= { } }). Callers drop defs with these keys, so a false positive deletes a real object from the model.
func deriveWrappers(ff FileFacts, defKeys map[string]bool) []string {
	// A folder that declares named script bodies has already said what its top-level keys are, and those bodies are routinely a single typed cite -- the wrapper shape exactly.
	if jomini.DeclaresNamedBodies(ff.Rule.Kind) {
		return nil
	}
	var out []string
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Depth != 0 || f.Key == "" || f.Quoted || !f.Block {
			continue
		}
		// A `keyword name = { }` pair is never a wrapper however its body is shaped: CK3 declares inline macros that way, and matching them on shape alone deleted 28 vanilla macros from the model. The keyword is a childless bare word, so the fact before this one is the statement before it.
		if i > 0 {
			if p := ff.Facts[i-1]; p.Parent == f.Parent && p.Key == "" && !p.Quoted &&
				jomini.DeclKeywordKind(p.Value) != "" {
				continue
			}
		}
		any, all := false, true
		for j := int32(i) + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= int32(i) && all; j++ {
			c := ff.Facts[j]
			if c.Parent != int32(i) || c.Key == "" || c.Quoted {
				continue
			}
			any = true
			kind, id, ok := game.ParseTyped(ff.GameID, c.Key)
			if !ok {
				if sp, sok := jomini.TypedSpanAt(c.Key, 0); sok {
					kind, id, ok = game.ParseTyped(ff.GameID, sp.Prefix+":"+sp.ID)
				}
			}
			if !ok || kind == "" || !defKeys[id] {
				all = false
			}
		}
		if any && all {
			out = append(out, f.Key)
		}
	}
	return out
}
