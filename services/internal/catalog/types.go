// types.go declares persisted semantic-model shapes and format-version constants.
// A version bump invalidates on-disk files (no migration).

package catalog

import (
	"slices"
	"strings"
	"sync"
)

// CacheFormatVersion is the on-disk schema of VanillaCache. Bump it when the
// derived *content* changes too, not only the field layout: a cache written
// before deriveNestedOptions was made order-independent has 352 CK3 defs under
// whichever kind that scan's threads happened to pick, and nothing else would
// invalidate it.
const CacheFormatVersion = 28

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
	// SkipKeys marks an option database and lists the inner keys that are
	// ordinary fields rather than rows — a game rule's `categories` and
	// `default`. Everything else under the parent is a row, so a mod that adds
	// its own option is picked up without re-deriving anything.
	SkipKeys []string `json:"skipKeys,omitempty"`
	// IsOption distinguishes an option database with nothing to skip (every key
	// under the group is a row) from an ordinary cite-driven nested shape.
	IsOption bool `json:"isOption,omitempty"`
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

	// Doc is the comment block written immediately above the definition. For
	// most objects it is the only description that exists: script_docs covers
	// the engine API, not script, so no dump describes an event, a decision, a
	// trait or a scripted macro. Authors write a comment instead.
	Doc string `json:"doc,omitempty"`
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
	// Snippets is the derived skeleton for each kind whose shape is
	// well-evidenced: the members that clear a coverage floor, most-used first.
	Snippets         map[string][]SnippetMember     `json:"snippets,omitempty"`
	StructureBlocks  map[string][]string            `json:"structureBlocks,omitempty"`
	Vocabulary       []string                       `json:"vocabulary"`
	GUITypes         []string                       `json:"guiTypes"`
	GUIProps         []string                       `json:"guiProps"`
	MetaKeys         []string                       `json:"metaKeys"`
	Edges            []Edge                         `json:"edges,omitempty"`
	LocRefs          []Ref                          `json:"locRefs,omitempty"`
	CallRefs         []Ref                          `json:"callRefs,omitempty"` // scripted_* call sites
	FieldValueKinds  map[string]string              `json:"fieldValueKinds,omitempty"`
	FieldEnumsByKind map[string]map[string][]string `json:"fieldEnumsByKind,omitempty"`
	PrefixKinds      map[string]string              `json:"prefixKinds,omitempty"`
	FireKeys         map[string]string              `json:"fireKeys,omitempty"`
	NestedShapes     []NestedShape                  `json:"nestedShapes,omitempty"`
	Wrappers         []string                       `json:"wrappers,omitempty"`
	LocAffixes       map[string][]LocAffix          `json:"locAffixes,omitempty"`
	LocFields        []string                       `json:"locFields,omitempty"`
	LocListFields    []string                       `json:"locListFields,omitempty"`
	LocMemberAffixes map[string][]LocAffix          `json:"locMemberAffixes,omitempty"`
	// LocKeyAffixes are shapes that derive one loc key from another, so a key
	// nothing cites is still explained. Not per kind: the stem is a key, not a def.
	LocKeyAffixes []LocKeyAffix     `json:"locKeyAffixes,omitempty"`
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

	// Schema is the type system the game declares in script_docs: scope types,
	// scope links, and typed engine tokens. Nil when the user has not run
	// script_docs; callers degrade rather than infer.
	Schema *Schema `json:"schema,omitempty"`

	// KindScope maps a harvested database kind to the scope type it holds
	// (kind "cultures" → scope "culture"), resolved by bindKinds. This is what
	// lets completion filter effects by the scope the cursor sits in.
	KindScope map[string]string `json:"kindScope,omitempty"`

	// Paths is the interning table for Def/Ref/Edge paths. It exists only on
	// disk: saveCacheFile writes it and loadCacheFile expands it away, so code
	// reading a cache always sees real paths.
	Paths []string `json:"paths,omitempty"`

	// Built by PrepareCache on load/scan; not persisted. The token views are
	// projections of Schema, rebuilt each load so the declared API is stored
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
	if c.FieldEnumsByKind == nil {
		c.FieldEnumsByKind = map[string]map[string][]string{}
	}
	if c.PrefixKinds == nil {
		c.PrefixKinds = map[string]string{}
	}
	if c.FireKeys == nil {
		c.FireKeys = map[string]string{}
	}
	if c.LocAffixes == nil {
		c.LocAffixes = map[string][]LocAffix{}
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
// Nothing here is persisted: Schema is the only stored copy, so a cache written
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
