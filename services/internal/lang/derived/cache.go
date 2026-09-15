// Package derived builds and persists VanillaCache (install scan) and harvests
// workspace mods into session maps. It answers "what objects/refs/edges exist?"
// by scanning the game install and indexing mods. All game knowledge is DERIVED
// here from the install — nothing is bundled. It imports jomini, loc, declared
// and game; it must not import session, and Wails DTOs live in views.
//
// cache.go declares the persisted shapes, the format versions, and the
// projections PrepareCache builds over them. A version bump invalidates
// on-disk files; there is no migration.
package derived

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// CacheFormatVersion is the on-disk schema of VanillaCache, bumped when the derived CONTENT changes too and not only the field layout, since nothing else invalidates a cache harvested by a since-corrected pass -- 42 keeps the corpus-harvested Get*/Set* data functions when an install ships no data-type dump, where 41 let enrichDataTypes overwrite them with an empty set.
const CacheFormatVersion = 42

// LocFormatVersion is the on-disk schema of a vanilla loc sidecar.
const LocFormatVersion = 3

// NestedShape is a derived parent→child harvest pattern (faiths under religion, nested titles). GroupKey is the wrapper when every hit sat under one assignment.
type NestedShape struct {
	ParentKind string `json:"parentKind"`
	ChildKind  string `json:"childKind"`
	GroupKey   string `json:"groupKey,omitempty"`
	// KeyPrefix is the shared `X_` first letters of discovered child keys (CK3 titles: "ekdcb"); empty harvests every matching block.
	KeyPrefix string `json:"keyPrefix,omitempty"`
}

// Def is one named object: Kind/Key, absolute Path, 0-based Line, Origin ("" = vanilla). OwnerKey is the enclosing scripted macro for script_param defs. Value is the harvested RHS literal at a ScriptName def site (hover body).
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
	// Nested marks a definition harvested from inside another one rather than at a file's top level; it is real, but it is not the file's subject.
	Nested bool `json:"nested,omitempty"`
	// Local marks a definition the file DECLARES inline (`scripted_trigger foo = {…}`, a GUI local_template), so a repeated key is required rather than a defect and a call must never navigate to another file's body.
	Local bool `json:"local,omitempty"`
	// Doc is the comment block written directly above the definition -- for most objects the only description that exists, since script_docs covers the engine API and not script.
	Doc string `json:"doc,omitempty"`
}

// IsCitableDef reports a definition whose key names a real object, so a citation, a field value or a nested-shape join may bind to it. One question, previously asked at seven sites with four answers; jomini.IsObjectKind is the stricter sibling that also rejects `namespace`.
func IsCitableDef(d Def) bool {
	k := jomini.CanonicalKind(d.Kind)
	return k != "" && k != jomini.KindLocKey && d.Key != "" && !jomini.IsEphemeral(k)
}

// Ref is a use-site. Kind is "loc", "loc-broad", "event", "on_action", or an ephemeral/script kind. OwnerKey scopes script_param refs to the enclosing scripted_* call.
type Ref struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	OwnerKey string `json:"ownerKey,omitempty"`
	// Derived marks a reference whose KIND came from an install-derived pass rather than from something the game declares; accusing surfaces skip these.
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
	// Rescoped marks a fire site under a block that changed scope, so the event does NOT run in the scope its source declares.
	Rescoped bool `json:"rescoped,omitempty"`
}

// ModInput is one workspace mod: Origin id, Root folder, load Order, display Name.
type ModInput struct {
	Origin string
	Root   string
	Order  int
	Name   string
	// Calendar is the era this mod's dates are written in, when it declares one.
	Calendar ModCalendar
}

// ModCalendar is how a mod's date literals map to the era its players see: A Game of Thrones writes 8082.1.1 for what the game shows as 300 AC. Not derivable and deliberately not guessed -- it is metadata a person states once.
type ModCalendar struct {
	// Era is the suffix shown after a converted year, e.g. "AC".
	Era string `json:"era,omitempty"`
	// EpochOffset is subtracted from the script year to get the era year.
	EpochOffset int `json:"epochOffset,omitempty"`
}

// Declared reports whether this calendar says anything; an undeclared one must leave every date exactly as written.
func (c ModCalendar) Declared() bool {
	return c.Era != "" || c.EpochOffset != 0
}

// EraDate renders a Clausewitz date in the mod's era. Empty for an undeclared calendar, a non-date, or a year the offset would push to zero or below.
func EraDate(date string, cal ModCalendar) string {
	if !cal.Declared() {
		return ""
	}
	parts := strings.Split(date, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return ""
	}
	era := year - cal.EpochOffset
	if era <= 0 {
		return ""
	}
	out := strconv.Itoa(era) + "." + strings.Join(parts[1:], ".")
	if cal.Era != "" {
		out += " " + cal.Era
	}
	return out
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

	Defs      []Def             `json:"defs"`
	FieldInfo map[string]string `json:"fieldInfo"`
	// FieldInfoByKind is keyed "block.field" for a field documented inside an example block, bare otherwise.
	FieldInfoByKind map[string]map[string]string `json:"fieldInfoByKind"`
	Structures      map[string][]string          `json:"structures"`
	// NestedMembers is block key -> the members that block holds, most-used first: what answers "what belongs inside triggered_desc?" rather than only "inside an event?".
	NestedMembers map[string][]string `json:"nestedMembers,omitempty"`
	// UnambiguousBlocks are the block names used by exactly one kind; only those can have an absence judged.
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
	// LocConventions is the key templates each kind's definitions are localized under, learned from the install's own loc.
	LocConventions map[string][]loc.Convention `json:"locConventions,omitempty"`
	// LocKeyPrefixes build one loc key out of another (EU5's NOT_<key>): the stem is a key, not a def.
	LocKeyPrefixes []string          `json:"locKeyPrefixes,omitempty"`
	LocFields      []string          `json:"locFields,omitempty"`
	KindInfo       map[string]string `json:"kindInfo,omitempty"`
	DataFunctions  []string          `json:"dataFunctions,omitempty"`
	// LocEngineSlots are `$NAME$` interpolations the game's loc uses but never defines (`$ACTION$`, `$AGE$`); demanding one from a mod is a false "missing localization". 354 of CK3's 1,138 ALL-CAPS interpolations, and a game update changes the set.
	LocEngineSlots []string `json:"locEngineSlots,omitempty"`
	// Schema is the type system the game declares in script_docs. Nil when the user has not run it; callers degrade rather than infer.
	Schema *declared.Schema `json:"schema,omitempty"`
	// KindScope maps a harvested database kind to the scope type it holds (kind "cultures" -> scope "culture"), resolved by bindKinds.
	KindScope map[string]string `json:"kindScope,omitempty"`
	// Paths interns Def/Ref/Edge paths on disk only: saveCacheFile writes it and loadCacheFile expands it away.
	Paths []string `json:"paths,omitempty"`

	// data is the tier a scan produced, held until SaveCache writes it; nil on a loaded cache, which reads it on demand.
	data []Def `json:"-"`

	// The token views are projections of Schema, rebuilt on every load so the declared API is stored exactly once.
	effectNames        []string                   `json:"-"`
	triggerNames       []string                   `json:"-"`
	vocabNames         []string                   `json:"-"`
	effectSet          map[string]bool            `json:"-"`
	triggerSet         map[string]bool            `json:"-"`
	structureSets      map[string]map[string]bool `json:"-"`
	structureBlockSets map[string]map[string]bool `json:"-"`
	// prepared marks the projections built. A plain bool rather than sync.Once because internPaths copies the struct by value and every guard type carries noCopy; the copy inherits true correctly since it shares the maps.
	prepared bool `json:"-"`
}

// VanillaLoc is the default-language loc sidecar for one install + version + lang.
type VanillaLoc struct {
	FormatVersion int                 `json:"formatVersion"`
	Sites         map[string]LocEntry `json:"sites"`
}

// prepareMu serializes cache preparation process-wide; it is held only while the projections are built, once per cache.
var prepareMu sync.Mutex

// PrepareCache normalizes field docs and builds the O(1) membership maps LSP and views read. Idempotent, and locked because rebuilding in place while another session reads the same cache is a data race.
func PrepareCache(c *VanillaCache) {
	if c == nil {
		return
	}
	prepareMu.Lock()
	defer prepareMu.Unlock()
	if c.prepared {
		return
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
	c.prepared = true
}

// projectSchema builds the completion and ranking views over the declared API. Nothing here is persisted, so a cache written without script_docs simply offers no engine tokens.
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
