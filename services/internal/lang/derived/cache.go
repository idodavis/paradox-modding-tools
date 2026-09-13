// types.go declares persisted semantic-model shapes and format-version constants.
// A version bump invalidates on-disk files (no migration).

package derived

import (
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// CacheFormatVersion is the on-disk schema of VanillaCache. Bump it when the
// derived *content* changes too, not only the field layout: a cache written
// before deriveNestedOptions was made order-independent has 352 CK3 defs under
// whichever kind that scan's threads happened to pick, and nothing else would
// invalidate it.
//
// 34 covers two content changes, neither of which any other check invalidates:
//
//   - maxOptionKeysPerParent. An older cache holds 57,652 phantom EU5 defs of
//     kind `father` — 40% of that model — harvested from the game-setup `start`
//     blocks. Worse than noise: knownAnywhere ends in FindDefs, so each one is a
//     word unknown-token and unknown-key can never report.
//   - defContainers using jomini.IsObjectKind. An older cache attributes 5,604
//     of CK3's 17,073 non-call edges (32.8%) and 1,501 of Vic3's 3,847 (39.0%)
//     to a saved scope rather than to the event that encloses it. The edge
//     counts are unchanged; the Event Graph's source node was wrong.
//
// 35 adds Edge.Rescoped, which the fix above made necessary. Once an edge names
// the on_action that really encloses it, eventRootScopeLocked starts reading
// that on_action's declared scope as the event's — and a fire site nested under
// a scope change is not in that scope at all. An older cache has the flag unset
// everywhere, which is the pre-fix behaviour, so the bump is what turns it on.
//
// 36 is the smallest content change here yet and still needs the bump: the name
// shapes now accept Unicode letters, so Victoria 3's `mon_general_petar_vukotić`
// is harvested as the character template it is. Three or so tokens across the
// three installs — but the rule is not about magnitude. An older cache is a
// model missing objects the install contains, and nothing else would notice.
//
// 37 stops option databases harvesting declared engine names as rows: 639 CK3
// defs and 2 EU5 ones were triggers and effects (`is_alive`, `save_scope_as`),
// which resolveDefinition ranks above the declared token. An older cache still
// holds them.
//
// 38 drops the cut projections: Snippets, MetaKeys, FieldEnumsByKind and
// AmbiguousFields. Nothing derives them any more; an older cache still carries
// the stale values.
const CacheFormatVersion = 38

// NestedShape is a derived parent→child harvest pattern (faiths under religion,
// nested titles, law policies, …). GroupKey is the wrapper when every hit
// sat under the same assignment.
type NestedShape struct {
	ParentKind string `json:"parentKind"`
	ChildKind  string `json:"childKind"`
	GroupKey   string `json:"groupKey,omitempty"`
	// KeyPrefix is the shared `X_` first letters of discovered child keys
	// (CK3 titles: "ekdcb"). Empty means harvest every matching block.
	KeyPrefix string `json:"keyPrefix,omitempty"`
}

// LocFormatVersion is the on-disk schema of a vanilla loc sidecar.
const LocFormatVersion = 3

// Def is one named object: Kind/Key, absolute Path, 0-based Line, Origin ("" = vanilla).
// OwnerKey is the enclosing scripted macro for script_param defs.
// Value is the harvested RHS literal at a ScriptName def site (hover body).
type Def struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Start    int    `json:"start,omitempty"`
	End      int    `json:"end,omitempty"`
	Origin   string `json:"origin,omitempty"`
	OwnerKey string `json:"ownerKey,omitempty"`
	Value    string `json:"value,omitempty"`
	// Nested marks a definition harvested from inside another one rather than at
	// a file's top level: a game rule's options, a script parameter, the members
	// of a nested shape. They are real definitions -- they are cited, hovered and
	// navigated to -- but they are not the file's subject, and a check that
	// treats them as one reports every activity type's `special_guests` as a
	// duplicate of every other's.
	Nested bool `json:"nested,omitempty"`
	// Local marks a definition the file DECLARES inline -- `scripted_trigger
	// foo = { ... }`, a GUI `local_template` -- rather than one a folder gives a
	// kind to. The engine resolves these per file, so a repeated Local key is
	// required rather than a defect, and a call must never be navigated to a
	// Local body in another file. game.LocalDeclPrefix names the keyword family
	// and jomini.DeclKeywords lists them; GAME-SYNTAX 16 has the evidence.
	Local bool `json:"local,omitempty"`

	// Doc is the comment block written immediately above the definition. For
	// most objects it is the only description that exists: script_docs covers
	// the engine API, not script, so no dump describes an event, a decision, a
	// trait or a scripted macro. Authors write a comment instead.
	Doc string `json:"doc,omitempty"`
}

// IsCitableDef reports a definition whose key names a real object, so a
// citation, a field value or a nested-shape join may bind to it.
//
// One question, previously asked at seven sites with four answers. The three
// that disagreed:
//
//   - deriveFieldValueKinds and bindKinds admitted loc_key. Whether a field
//     holds a localization key is deriveLocFields' question, and that pass has
//     four guards this one does not — chiefly schema.HasName, so it refuses to
//     retype a token the game declares. Without it, `has_policy` and
//     `add_random_innovation` were typed loc_key on a mod that localizes them,
//     which is a derived pass overruling a declared one. Worse, the collision
//     made the key ambiguous and so DROPPED the field: on A Game of Thrones
//     111 correct types (`add_innovation` -> innovations, `add_perk` ->
//     lifestyle_perks) were suppressed by the mod's own loc file.
//   - resolveNonLoc omitted the empty-key check, which is unreachable there
//     (ResolveMatching buckets by a non-empty word) but not worth preserving as
//     a difference.
//
// jomini.IsObjectKind is the stricter sibling for "resolve this name to a
// thing": it also rejects `namespace`. That is right when a hover must not land
// on an event id prefix, and wrong here — a namespace still occupies the name,
// and a derivation that treats the name as free invents a database for it.
func IsCitableDef(d Def) bool {
	k := jomini.CanonicalKind(d.Kind)
	return k != "" && k != jomini.KindLocKey && d.Key != "" && !jomini.IsEphemeral(k)
}

// Ref is a use-site. Kind is "loc", "loc-broad", "loc-convention",
// "event", "on_action", or an ephemeral/script kind. OwnerKey scopes
// script_param refs to the enclosing scripted_* call.
type Ref struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	OwnerKey string `json:"ownerKey,omitempty"`
	// Derived marks a reference whose KIND came from an install-derived pass
	// (FieldValueKinds, FireKeys) rather than from something the game declares.
	// Accusing surfaces skip these: both incidents in this project's history —
	// 859 `body_part` rows and 4,033 fire rows — were a strong derived pass
	// reaching a surface that tells a modder their script is wrong. Not
	// persisted in practice: only loc and call refs reach the cache, and
	// neither is derived-marked.
	Derived bool `json:"derived,omitempty"`
}

// Edge is a directed event-graph link. Kind "call" is a scripted-effect invocation.
type Edge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Via     string `json:"via"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Kind    string `json:"kind,omitempty"`
	NameKey string `json:"nameKey,omitempty"`
	// Rescoped marks a fire site sitting under a block that changed scope, so
	// the event does NOT run in the scope its source declares.
	//
	// CK3's on_faith_monthly is faith-scoped and calls
	// `trigger_event = global_religion.1000` fourteen blocks deep, inside
	// character-scoped iterators. Reading the on_action's scope as the event's
	// root accused 31 lines of vanilla CK3 of wrong-scope. The graph is
	// unaffected — the edge is real and the arrow points the right way; only
	// the scope inference must decline.
	Rescoped bool `json:"rescoped,omitempty"`
}

// ModInput is one workspace mod: Origin id, Root folder, load Order, display Name.
type ModInput struct {
	Origin string
	Root   string
	Order  int
	Name   string
	// Calendar is the era this mod's dates are written in, when it declares one.
	// Empty means the dates mean what they say, which is the case for every mod
	// that is not a total conversion.
	Calendar ModCalendar
}

// ModCalendar is how a mod's date literals map to the era its players see.
//
// A total conversion offsets the year so the engine's date arithmetic still
// works: A Game of Thrones writes start_date = 8082.1.1 for what the game shows
// as 300 AC. Every date in that mod's script is read by a human who must
// subtract 7,782 in their head.
//
// Not derivable, and deliberately not guessed. Nothing in a mod declares the
// offset, and inferring it from bookmark dates would be a guess dressed as a
// fact — so it is metadata a person states once, and with none stated nothing
// anywhere changes.
type ModCalendar struct {
	// Era is the suffix shown after a converted year, e.g. "AC".
	Era string `json:"era,omitempty"`
	// EpochOffset is subtracted from the script year to get the era year.
	EpochOffset int `json:"epochOffset,omitempty"`
}

// Declared reports whether this calendar says anything. An undeclared calendar
// must leave every date exactly as written.
func (c ModCalendar) Declared() bool {
	return c.Era != "" || c.EpochOffset != 0
}

// LocEntry is one localization value with its source file and line.
type LocEntry struct {
	Value string `json:"value"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
}

// LocDelta is the loc payload one file contributes to the harvest.
type LocDelta struct {
	Lang string
	Vals map[string]LocEntry
}

// VanillaCache is the vanilla script model for one install + version.
type VanillaCache struct {
	FormatVersion int    `json:"formatVersion"`
	InstallID     string `json:"installId"`
	GameID        string `json:"gameId"`
	InstallPath   string `json:"installPath"`
	GameVersion   string `json:"gameVersion"`
	ScannedAt     string `json:"scannedAt"`

	Defs            []Def                        `json:"defs"`
	FieldInfo       map[string]string            `json:"fieldInfo"`
	FieldInfoByKind map[string]map[string]string `json:"fieldInfoByKind"`
	Structures      map[string][]string          `json:"structures"`
	// NestedMembers is block key -> the members that block holds, most-used
	// first. What lets a query answer "what belongs inside triggered_desc?"
	// rather than only "what belongs inside an event?".
	NestedMembers map[string][]string `json:"nestedMembers,omitempty"`
	// UnambiguousBlocks are the block names used by exactly one kind. Only those
	// can have an absence judged: `option` is a block in a CK3 event and another
	// in accolade_names, holding nothing in common.
	UnambiguousBlocks map[string]bool     `json:"unambiguousBlocks,omitempty"`
	StructureBlocks   map[string][]string `json:"structureBlocks,omitempty"`
	Vocabulary        []string            `json:"vocabulary"`
	GUITypes          []string            `json:"guiTypes"`
	GUIProps          []string            `json:"guiProps"`
	Edges             []Edge              `json:"edges,omitempty"`
	LocRefs           []Ref               `json:"locRefs,omitempty"`
	CallRefs          []Ref               `json:"callRefs,omitempty"` // scripted_* call sites
	FieldValueKinds   map[string]string   `json:"fieldValueKinds,omitempty"`
	// DeclaredFieldTypes is DECLARED by the game's own docs, so it outranks FieldValueKinds.
	DeclaredFieldTypes       map[string]string            `json:"declaredFieldTypes,omitempty"`
	DeclaredFieldTypesByKind map[string]map[string]string `json:"declaredFieldTypesByKind,omitempty"`
	PrefixKinds              map[string]string            `json:"prefixKinds,omitempty"`
	FireKeys                 map[string]string            `json:"fireKeys,omitempty"`
	NestedShapes             []NestedShape                `json:"nestedShapes,omitempty"`
	Wrappers                 []string                     `json:"wrappers,omitempty"`
	LocAffixes               map[string][]loc.Affix       `json:"locAffixes,omitempty"`
	LocFields                []string                     `json:"locFields,omitempty"`
	LocListFields            []string                     `json:"locListFields,omitempty"`
	LocMemberAffixes         map[string][]loc.Affix       `json:"locMemberAffixes,omitempty"`
	// LocKeyAffixes are shapes that derive one loc key from another, so a key
	// nothing cites is still explained. Not per kind: the stem is a key, not a def.
	LocKeyAffixes []loc.KeyAffix    `json:"locKeyAffixes,omitempty"`
	KindInfo      map[string]string `json:"kindInfo,omitempty"`
	DataFunctions []string          `json:"dataFunctions,omitempty"`

	// LocEngineSlots are `$NAME$` interpolations the game's own localization
	// uses but never defines as a key — `$EFFECT_LIST_BULLET$`, `$ACTION$`,
	// `$AGE$`. The engine fills them in; they are not loc keys anyone can
	// write, so demanding one from a mod is a false "missing localization".
	//
	// Derived from the install, never listed: 354 of the 1,138 distinct
	// ALL-CAPS interpolations in CK3's english loc are of this kind, and a game
	// update changes the set. `isLocEngineToken` guesses at the same thing from
	// shape (a single ALL-CAPS word) and is right about 80% of the time; this
	// is the measured version.
	LocEngineSlots []string `json:"locEngineSlots,omitempty"`

	// declared.Schema is the type system the game declares in script_docs: scope types,
	// scope links, and typed engine tokens. Nil when the user has not run
	// script_docs; callers degrade rather than infer.
	Schema *declared.Schema `json:"schema,omitempty"`

	// KindScope maps a harvested database kind to the scope type it holds
	// (kind "cultures" → scope "culture"), resolved by bindKinds. This is what
	// lets completion filter effects by the scope the cursor sits in.
	KindScope map[string]string `json:"kindScope,omitempty"`

	// Paths is the interning table for Def/Ref/Edge paths. It exists only on
	// disk: saveCacheFile writes it and loadCacheFile expands it away, so code
	// reading a cache always sees real paths.
	Paths []string `json:"paths,omitempty"`

	// Built by PrepareCache on load/scan; not persisted. The token views are
	// projections of declared.Schema, rebuilt each load so the declared API is stored
	// exactly once.
	effectNames        []string                   `json:"-"`
	triggerNames       []string                   `json:"-"`
	vocabNames         []string                   `json:"-"`
	effectSet          map[string]bool            `json:"-"`
	triggerSet         map[string]bool            `json:"-"`
	structureSets      map[string]map[string]bool `json:"-"`
	structureBlockSets map[string]map[string]bool `json:"-"`
	// prepared marks the projections above as built. They are a pure function of
	// the persisted fields, so deriving them once per cache is both correct and
	// the only safe option: PrepareCache rebuilds maps in place, and every
	// NewWithLoc calls it, so two sessions sharing one cache pointer raced.
	//
	// A plain bool rather than a sync.Once because internPaths copies the struct
	// by value, and every guard type in sync and sync/atomic carries noCopy. The
	// copy inherits prepared=true correctly: it shares the projection maps.
	prepared bool `json:"-"`
}

// VanillaLoc is the default-language loc sidecar for one install + version + lang.
type VanillaLoc struct {
	FormatVersion int                 `json:"formatVersion"`
	Sites         map[string]LocEntry `json:"sites"`
}

// PrepareCache normalizes field docs and builds O(1) membership maps for
// LSP/views. It is idempotent: the projections derive purely from the persisted
// fields, and rebuilding them in place while another session reads the same
// cache is a data race.
// prepareMu serializes cache preparation process-wide. It is held only while
// the projections are built — once per cache, milliseconds — so the coarseness
// costs nothing and it keeps VanillaCache copyable.
var prepareMu sync.Mutex

func PrepareCache(c *VanillaCache) {
	if c == nil {
		return
	}
	prepareMu.Lock()
	defer prepareMu.Unlock()
	if c.prepared {
		return
	}
	prepareCacheOnce(c)
	c.prepared = true
}

func prepareCacheOnce(c *VanillaCache) {
	if c.FieldInfo == nil {
		c.FieldInfo = map[string]string{}
	}
	if c.FieldInfoByKind == nil {
		c.FieldInfoByKind = map[string]map[string]string{}
	}
	if c.Structures == nil {
		c.Structures = map[string][]string{}
	}
	if c.StructureBlocks == nil {
		c.StructureBlocks = map[string][]string{}
	}
	if c.FieldValueKinds == nil {
		c.FieldValueKinds = map[string]string{}
	}
	if c.DeclaredFieldTypes == nil {
		c.DeclaredFieldTypes = map[string]string{}
	}
	if c.DeclaredFieldTypesByKind == nil {
		c.DeclaredFieldTypesByKind = map[string]map[string]string{}
	}
	if c.PrefixKinds == nil {
		c.PrefixKinds = map[string]string{}
	}
	if c.FireKeys == nil {
		c.FireKeys = map[string]string{}
	}
	if c.LocAffixes == nil {
		c.LocAffixes = map[string][]loc.Affix{}
	}
	if c.KindInfo == nil {
		c.KindInfo = map[string]string{}
	}
	c.FieldInfo = normalizeDocMap(c.FieldInfo)
	for kind, m := range c.FieldInfoByKind {
		c.FieldInfoByKind[kind] = normalizeDocMap(m)
	}
	c.projectSchema()
	c.structureSets = map[string]map[string]bool{}
	for kind, keys := range c.Structures {
		c.structureSets[kind] = sliceSet(keys)
	}
	c.structureBlockSets = map[string]map[string]bool{}
	for kind, keys := range c.StructureBlocks {
		c.structureBlockSets[kind] = sliceSet(keys)
	}
}

// projectSchema builds the completion and ranking views over the declared API.
// Nothing here is persisted: declared.Schema is the only stored copy, so a cache written
// without script_docs simply offers no engine tokens.
func (c *VanillaCache) projectSchema() {
	c.effectSet, c.triggerSet = map[string]bool{}, map[string]bool{}
	c.effectNames, c.triggerNames = nil, nil
	vocab := make(map[string]bool, len(c.Vocabulary))
	for _, k := range c.Vocabulary {
		vocab[k] = true
	}
	if c.Schema != nil {
		for name := range c.Schema.Effects {
			c.effectSet[name] = true
			c.effectNames = append(c.effectNames, name)
		}
		for name := range c.Schema.Triggers {
			c.triggerSet[name] = true
			c.triggerNames = append(c.triggerNames, name)
		}
		c.Schema.EachName(func(name string) { vocab[name] = true })
		slices.Sort(c.effectNames)
		slices.Sort(c.triggerNames)
	}
	c.vocabNames = sortedKeys(vocab)
}

// TokenNames returns the declared names for a completion slot: "effect",
// "trigger", or "vocabulary" for the walk's keys plus the whole engine API.
func (c *VanillaCache) TokenNames(slot string) []string {
	if c == nil {
		return nil
	}
	switch slot {
	case "effect":
		return c.effectNames
	case "trigger":
		return c.triggerNames
	case "vocabulary":
		return c.vocabNames
	default:
		return nil
	}
}

// StructureBlock reports whether key is usually a block under kind.
func (c *VanillaCache) StructureBlock(kind, key string) bool {
	if c == nil || key == "" {
		return false
	}
	set := c.structureBlockSets[kind]
	return set[key] || set[strings.ToLower(key)]
}

// MemberSets returns pre-built structure/effect/trigger maps for LSP rank.
func (c *VanillaCache) MemberSets(kind string) (structKeys, effects, triggers map[string]bool) {
	if c == nil {
		return nil, nil, nil
	}
	return c.structureSets[kind], c.effectSet, c.triggerSet
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
