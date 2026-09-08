// schema.go reads the type system a game ships in its script_docs output: the
// scope-type universe, the scope links (which double as the typed-prefix table),
// and every engine token with its input scope and argument type. Nothing here is
// inferred — each field is declared by the game. Three syntaxes, one model:
// classic "----" blocks (CK3), markdown headings (EU5, Vic3), and bare "name:"
// blocks (event_scopes.log, Vic3 modifiers.log).

package catalog

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

// ScopeType is one entry of the game's scope-type universe (event_scopes.log).
// A scope type is the "class" an effect or trigger runs against.
type ScopeType struct {
	EvaluateTriggers bool   `json:"evaluateTriggers,omitempty"`
	ExecuteEffects   bool   `json:"executeEffects,omitempty"`
	ChangeScopes     bool   `json:"changeScopes,omitempty"`
	StoresVariables  bool   `json:"storesVariables,omitempty"`
	SaveToken        string `json:"saveToken,omitempty"`
}

// ScopeLink is one scope transition (event_targets.log). In is the scope types
// the link may be used from; Out is the type it yields. Global+Data together
// mean the link is cited as `name:key` — this is the typed-prefix table.
type ScopeLink struct {
	In     []string `json:"in,omitempty"`
	Out    string   `json:"out,omitempty"`
	Global bool     `json:"global,omitempty"`
	Data   bool     `json:"data,omitempty"`
	Wild   bool     `json:"wild,omitempty"`
	Doc    string   `json:"doc,omitempty"`
}

// EngineToken is one effect or trigger. In is Supported Scopes ("none" means
// usable anywhere); Target is Supported Targets, the argument's scope type.
type EngineToken struct {
	In     []string `json:"in,omitempty"`
	Target string   `json:"target,omitempty"`
	Doc    string   `json:"doc,omitempty"`
	Usage  string   `json:"usage,omitempty"`
}

// Modifier is one modifier token. Area is CK3 "Use areas" / Vic3 "Mask".
type Modifier struct {
	Name string `json:"name,omitempty"`
	Doc  string `json:"doc,omitempty"`
	Area string `json:"area,omitempty"`
}

// Schema is the declared type system for one install, read from script_docs.
// Nil means the user has not run script_docs; callers degrade rather than guess.
type Schema struct {
	Scopes    map[string]ScopeType   `json:"scopes,omitempty"`
	Links     map[string]ScopeLink   `json:"links,omitempty"`
	Effects   map[string]EngineToken `json:"effects,omitempty"`
	Triggers  map[string]EngineToken `json:"triggers,omitempty"`
	OnActions map[string]string      `json:"onActions,omitempty"`
	Modifiers map[string]Modifier    `json:"modifiers,omitempty"`

	// SavedScopes are the scope names the engine saves itself (actor, recipient,
	// …), listed under "Event Targets Saved from Code" in event_targets.log.
	SavedScopes []string `json:"savedScopes,omitempty"`

	// ScopesDerived says Scopes was recovered from the link table rather than
	// declared: only CK3 ships event_scopes.log. Provenance is kept because a
	// derived entry carries only ChangeScopes / EvaluateTriggers, never the
	// ExecuteEffects / StoresVariables / SaveToken facts no other file states.
	ScopesDerived bool `json:"scopesDerived,omitempty"`

	// Source says whether the dumps came from the game's user-data folder or
	// from PMT's archived copy, so the UI can tell the user.
	Source SchemaSource `json:"source,omitempty"`

	// ReadAt is the newest dump mtime (RFC3339), for staleness against the install.
	ReadAt string `json:"readAt,omitempty"`
}

// Empty reports a schema with no usable type information.
func (s *Schema) Empty() bool {
	return s == nil || (len(s.Scopes) == 0 && len(s.Links) == 0 &&
		len(s.Effects) == 0 && len(s.Triggers) == 0)
}

// EffectCount is the number of declared effects, for the health strip.
func (s *Schema) EffectCount() int {
	if s == nil {
		return 0
	}
	return len(s.Effects)
}

// Prefixes returns the link names citable as `name:key`, i.e. the typed-prefix
// table, mapped to the scope type each yields.
func (s *Schema) Prefixes() map[string]string {
	if s == nil {
		return nil
	}
	out := make(map[string]string, len(s.Links))
	for name, l := range s.Links {
		if l.Global && l.Data && l.Out != "" {
			out[name] = l.Out
		}
	}
	return out
}

// Token returns the effect or trigger named key, and which bag it came from.
func (s *Schema) Token(key string) (EngineToken, string, bool) {
	if s == nil {
		return EngineToken{}, "", false
	}
	k := strings.ToLower(key)
	if t, ok := s.Effects[k]; ok {
		return t, "effect", true
	}
	if t, ok := s.Triggers[k]; ok {
		return t, "trigger", true
	}
	return EngineToken{}, "", false
}

// kindFromDocFilename routes a dump file to the bag it fills. The games name
// these consistently across versions; only the block syntax inside differs.
func kindFromDocFilename(name string) string {
	switch {
	case strings.Contains(name, "event_target"):
		return "event_target"
	case strings.Contains(name, "event_scope"):
		return "scope_type"
	case strings.Contains(name, "effect"):
		return "effect"
	case strings.Contains(name, "trigger"):
		return "trigger"
	case strings.Contains(name, "modif"):
		return "modifier"
	case strings.Contains(name, "on_action"):
		return "on_action"
	default:
		return ""
	}
}

func stripDocMarkup(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(s)
}

// Doc returns the game's own prose for any declared name — effect, trigger,
// scope link or modifier. Empty when the name is not declared.
func (s *Schema) Doc(key string) string {
	if s == nil {
		return ""
	}
	k := strings.ToLower(key)
	if t, ok := s.Effects[k]; ok && t.Doc != "" {
		return t.Doc
	}
	if t, ok := s.Triggers[k]; ok && t.Doc != "" {
		return t.Doc
	}
	if l, ok := s.Links[k]; ok && l.Doc != "" {
		return l.Doc
	}
	return s.Modifiers[k].Doc
}

// Usage returns the declared signature for an effect or trigger.
func (s *Schema) Usage(key string) string {
	t, _, ok := s.Token(key)
	if !ok {
		return ""
	}
	return t.Usage
}

// ScopeText renders what a declared name runs against, for the hover card:
// its input scopes and, for a token, the type of its argument. Display only —
// filter on EngineToken.In, never by parsing this back apart.
func (s *Schema) ScopeText(key string) string {
	if s == nil {
		return ""
	}
	k := strings.ToLower(key)
	if t, _, ok := s.Token(k); ok {
		in := strings.Join(t.In, ", ")
		switch {
		case in != "" && t.Target != "":
			return in + "; targets: " + t.Target
		case in != "":
			return in
		default:
			return t.Target
		}
	}
	if l, ok := s.Links[k]; ok && len(l.In) > 0 {
		in := strings.Join(l.In, ", ")
		if l.Out != "" {
			return in + "; targets: " + l.Out
		}
		return in
	}
	return s.OnActions[k]
}

// Declares reports that the game names this as part of its type system — a
// scope type, a scope link, or a citable prefix. It is the test for "is this a
// real kind of object", as opposed to a word that merely appeared in a block.
func (s *Schema) Declares(name string) bool {
	if s == nil || name == "" {
		return false
	}
	k := strings.ToLower(name)
	if _, ok := s.Scopes[k]; ok {
		return true
	}
	_, ok := s.Links[k]
	return ok
}

// HasName reports that the game declares this name anywhere in its engine API —
// effect, trigger, scope link, modifier or on_action. A reference to one of
// these is not a reference to a missing object.
func (s *Schema) HasName(name string) bool {
	if s == nil || name == "" {
		return false
	}
	k := strings.ToLower(name)
	if _, ok := s.Effects[k]; ok {
		return true
	}
	if _, ok := s.Triggers[k]; ok {
		return true
	}
	if _, ok := s.Links[k]; ok {
		return true
	}
	if _, ok := s.Modifiers[k]; ok {
		return true
	}
	_, ok := s.OnActions[k]
	return ok
}

// EachName calls fn once per declared name, in no particular order. It is how
// the completion vocabulary picks up the engine API without a second copy of it.
func (s *Schema) EachName(fn func(string)) {
	if s == nil {
		return
	}
	for name := range s.Effects {
		fn(name)
	}
	for name := range s.Triggers {
		fn(name)
	}
	for name := range s.Links {
		fn(name)
	}
	for name := range s.Modifiers {
		fn(name)
	}
	for name := range s.OnActions {
		fn(name)
	}
}

// UsableIn reports whether a token with these Supported Scopes may run in scope.
// An empty list or "none" means unrestricted.
func UsableIn(in []string, scope string) bool {
	if len(in) == 0 || scope == "" {
		return true
	}
	for _, s := range in {
		if s == "none" || strings.EqualFold(s, scope) {
			return true
		}
	}
	return false
}

// ReadSchema parses every script_docs file under dirs into one Schema.
// Returns nil when no dump file yields anything.
func ReadSchema(dirs []string) *Schema {
	s := &Schema{
		Scopes:    map[string]ScopeType{},
		Links:     map[string]ScopeLink{},
		Effects:   map[string]EngineToken{},
		Triggers:  map[string]EngineToken{},
		OnActions: map[string]string{},
		Modifiers: map[string]Modifier{},
	}
	var newest time.Time
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".md") {
				return nil
			}
			kind := kindFromDocFilename(name)
			if kind == "" {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
			s.ingest(kind, string(raw))
			return nil
		})
	}
	if s.Empty() && len(s.OnActions) == 0 && len(s.Modifiers) == 0 {
		return nil
	}
	s.recoverScopesFromLinks()
	if !newest.IsZero() {
		s.ReadAt = newest.UTC().Format(time.RFC3339)
	}
	return s
}

// scopePrimitives are the scope types that hold a bare value rather than an
// object, so script can never be "inside" one.
var scopePrimitives = map[string]bool{
	"value": true, "boolean": true, "flag": true, "color": true, "date": true,
}

// recoverScopesFromLinks fills the scope-type universe from event_targets.log
// when event_scopes.log is absent.
//
// Only CK3 ships event_scopes.log. Victoria 3 and EU5 have no such file at all,
// so Schema.Scopes was empty for two of the three games — and because
// scope-walking asks `Scopes[target].ChangeScopes` before stepping through an
// iterator, scope-aware completion and typed hover silently did nothing there.
//
// event_targets.log declares the same universe indirectly: its "Input Scopes"
// and "Output Scopes" are scope-type names by definition. Checked against CK3,
// where both files exist, the link-derived set is a strict subset of the
// declared one — 67 of 71 types, inventing none — and the only link-mentioned
// types the game marks as not enterable are exactly the primitives above.
// ScopesDerived records that this happened so nothing mistakes it for declared.
func (s *Schema) recoverScopesFromLinks() {
	if len(s.Scopes) > 0 || len(s.Links) == 0 {
		return
	}
	add := func(name string) {
		// A multi-output link lists its types comma-separated.
		for _, part := range strings.Split(name, ",") {
			n := strings.ToLower(strings.TrimSpace(part))
			if n == "" || n == "none" || scopePrimitives[n] {
				continue
			}
			s.Scopes[n] = ScopeType{ChangeScopes: true, EvaluateTriggers: true}
		}
	}
	for _, l := range s.Links {
		add(l.Out)
		for _, in := range l.In {
			add(in)
		}
	}
	s.ScopesDerived = len(s.Scopes) > 0
}

// savedFromCode marks the trailing bare name list in event_targets.log.
const savedFromCode = "Saved from Code"

// ingest routes one dump file's blocks into the matching bag.
func (s *Schema) ingest(kind, text string) {
	if kind == "event_target" {
		if i := strings.Index(text, savedFromCode); i >= 0 {
			s.addSavedScopes(text[i+len(savedFromCode):])
			text = text[:i]
		}
	}
	for _, b := range game.SplitDumpBlocks(text) {
		if b.Name == "" {
			continue
		}
		f := blockFields(b)
		key := strings.ToLower(b.Name)
		switch kind {
		case "scope_type":
			s.Scopes[key] = ScopeType{
				EvaluateTriggers: f.yes("evaluate triggers"),
				ExecuteEffects:   f.yes("execute effects"),
				ChangeScopes:     f.yes("change scopes"),
				StoresVariables:  f.yes("stores variables"),
				SaveToken:        f.str("save token"),
			}
		case "event_target":
			if len(f.vals) == 0 {
				continue
			}
			// A link name may be declared twice: once as a global `name:key`
			// citation and once as a contextual hop. Both are real, so merge
			// rather than overwrite. Output types are verified never to disagree.
			s.Links[key] = mergeLink(s.Links[key], ScopeLink{
				In:     f.list("input scopes"),
				Out:    f.str("output scopes"),
				Global: f.yes("global link"),
				Data:   f.yes("requires data"),
				Wild:   f.yes("wild card"),
				Doc:    f.prose,
			})
		case "effect":
			s.Effects[key] = f.token()
		case "trigger":
			s.Triggers[key] = f.token()
		case "on_action":
			s.OnActions[key] = f.str("expected scope")
		case "modifier":
			s.Modifiers[key] = Modifier{
				Name: strings.TrimPrefix(f.str("name"), "aut!"),
				Doc:  cmpFirst(f.str("description"), f.prose),
				// EU5 and CK3 write the token's categories on the header line; Vic3
				// uses a Mask label. The list form drops the empty entries both
				// games leave behind in "Location, , All,".
				Area: cmpFirst(cmpFirst(f.str("mask"), f.str("use areas")),
					strings.Join(f.list("categories"), ", ")),
			}
		}
	}
}

// --- Binding declared types to harvested databases ---------------------------

// minBindCites is the evidence floor: below this a coincidental key match is
// plausible, so the type stays unbound and callers fall back.
const minBindCites = 3

// bindFloor is the share of a prefix's citations its folders must explain for
// the type to bind at all. Measured on live installs the real bindings sit near
// 1.00, so a simple majority is a wide margin.
const bindFloor = 0.5

// bindSaturated stops the search once the database is essentially accounted
// for, leaving the tail of folders that each explain a citation or two.
const bindSaturated = 0.98

// bindShare is the fraction of a type's citations a folder must explain to be
// treated as one of its homes rather than an incidental co-keyed folder.
const bindShare = 0.1

// KindShare is one folder's contribution to a binding.
type KindShare struct {
	Kind string `json:"kind"`
	Hits int    `json:"hits"`
}

// KindBinding records one resolved type-to-database link, with the evidence.
// Kinds may hold more than one entry: a scope type is sometimes split across
// folders, as CK3 decisions are across their DLC directories. Kind is the
// largest contributor.
type KindBinding struct {
	Prefix   string      `json:"prefix"`
	Scope    string      `json:"scope"`
	Kind     string      `json:"kind"`
	Kinds    []KindShare `json:"kinds,omitempty"`
	Cites    int         `json:"cites"`
	Coverage float64     `json:"coverage"`
	ByName   bool        `json:"byName,omitempty"`
}

// Owners returns the folders that hold enough of the type to be treated as its
// home. Folders below the share threshold are incidental: CK3 coats of arms are
// keyed by title id, so they match title citations without being titles.
func (b KindBinding) Owners() []string {
	var out []string
	for _, k := range b.Kinds {
		if b.Cites == 0 || k.Hits*int(1/bindShare) >= b.Cites {
			out = append(out, k.Kind)
		}
	}
	return out
}

// bindKinds joins the type system the game declares to the databases the
// install walk harvested. For each citable `prefix:key` link, the folder whose
// top-level keys account for those citations owns that scope type.
//
// This replaces inference: the prefix list and its output types are read from
// event_targets.log, and the only thing computed here is which folder holds the
// instances — a set intersection over thousands of citations, not a vote.
func bindKinds(s *Schema, defs []Def, cites []typedCite) (prefixKind, kindScope map[string]string, bound []KindBinding) {
	prefixKind, kindScope = map[string]string{}, map[string]string{}
	if s == nil {
		return prefixKind, kindScope, nil
	}

	keysByKind := map[string]map[string]bool{}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if k == "" || d.Key == "" || jomini.IsEphemeral(k) {
			continue
		}
		set := keysByKind[k]
		if set == nil {
			set = map[string]bool{}
			keysByKind[k] = set
		}
		set[d.Key] = true
	}

	idsByPrefix := map[string]map[string]bool{}
	for _, c := range cites {
		if c.prefix == "" || c.id == "" {
			continue
		}
		set := idsByPrefix[c.prefix]
		if set == nil {
			set = map[string]bool{}
			idsByPrefix[c.prefix] = set
		}
		set[c.id] = true
	}

	// Resolve the strongest evidence first so a contested kind goes to the type
	// that explains it best.
	for prefix, scope := range s.Prefixes() {
		b := bindOne(prefix, scope, idsByPrefix[prefix], keysByKind)
		if b.Kind != "" {
			bound = append(bound, b)
		}
	}
	slices.SortFunc(bound, func(a, b KindBinding) int {
		if a.Coverage != b.Coverage {
			if a.Coverage > b.Coverage {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Prefix, b.Prefix)
	})
	for _, b := range bound {
		prefixKind[b.Prefix] = b.Kind
		// Every folder that genuinely holds this type carries the scope, so a
		// split database still types all of its rows.
		for _, k := range b.Owners() {
			if _, taken := kindScope[k]; !taken {
				kindScope[k] = b.Scope
			}
		}
	}
	return prefixKind, kindScope, bound
}

// bindOne finds the folders that hold one prefix's citations. Kinds are taken
// in descending order of unexplained citations until the floor is cleared, so a
// database split across folders binds to all of them. Falls back to a
// singular/plural name match when vanilla never cites the prefix.
func bindOne(prefix, scope string, ids map[string]bool, keysByKind map[string]map[string]bool) KindBinding {
	b := KindBinding{Prefix: prefix, Scope: scope, Cites: len(ids)}
	if len(ids) >= minBindCites {
		remaining := make(map[string]bool, len(ids))
		maps.Copy(remaining, ids)
		explained := 0
		// Take folders in descending order of what they still explain, and keep
		// going while they contribute. Stopping at the floor would bind only the
		// largest folder of a split database.
		for len(remaining) > 0 {
			bestKind, bestHits := "", 0
			for kind, keys := range keysByKind {
				hits := 0
				for id := range remaining {
					if keys[id] {
						hits++
					}
				}
				// Ties go to the shorter kind name so a broad folder beats one
				// that incidentally reuses the same keys.
				if hits > bestHits || (hits == bestHits && hits > 0 && kind < bestKind) {
					bestKind, bestHits = kind, hits
				}
			}
			if bestHits == 0 {
				break
			}
			b.Kinds = append(b.Kinds, KindShare{Kind: bestKind, Hits: bestHits})
			explained += bestHits
			for id := range remaining {
				if keysByKind[bestKind][id] {
					delete(remaining, id)
				}
			}
			// Leave the long tail of one-hit folders alone once the database is
			// essentially accounted for.
			if float64(explained)/float64(len(ids)) >= bindSaturated {
				break
			}
		}
		if cov := float64(explained) / float64(len(ids)); cov >= bindFloor {
			b.Kind, b.Coverage = b.Kinds[0].Kind, cov
			return b
		}
		b.Kinds = nil
	}
	if kind := nameMatchKind(scope, keysByKind); kind != "" {
		b.Kind, b.ByName = kind, true
		b.Kinds = []KindShare{{Kind: kind}}
	}
	return b
}

// nameMatchKind matches a scope type to a folder kind by name, tolerating the
// singular/plural split (scope "culture" vs folder "cultures").
func nameMatchKind(scope string, keysByKind map[string]map[string]bool) string {
	if scope == "" {
		return ""
	}
	for _, cand := range []string{scope, scope + "s", strings.TrimSuffix(scope, "s")} {
		if cand != "" && len(keysByKind[cand]) > 0 {
			return cand
		}
	}
	return ""
}

// typedCite is one `prefix:id` citation found in script.
type typedCite struct {
	prefix, id string
}

// collectTypedCites gathers every `prefix:id` citation in one parsed file.
// Language prefixes (scope:, var:, …) are skipped — they name locals, not
// database rows.
func collectTypedCites(root *jomini.Root) []typedCite {
	if root == nil {
		return nil
	}
	var out []typedCite
	add := func(text string) {
		for _, sp := range jomini.TypedSpans(text) {
			if jomini.PrefixKind(sp.Prefix) != "" {
				continue
			}
			out = append(out, typedCite{prefix: sp.Prefix, id: sp.ID})
		}
	}
	jomini.Walk(root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		switch n := st.(type) {
		case *jomini.Assignment:
			if !n.Key.Quoted {
				add(n.Key.Text)
			}
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted {
				add(sc.Text)
			}
		case *jomini.ValueStmt:
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted {
				add(sc.Text)
			}
		}
		return true
	})
	return out
}

// mergeLink folds a second declaration of the same link name into the first.
func mergeLink(old, add ScopeLink) ScopeLink {
	for _, in := range add.In {
		if !slices.Contains(old.In, in) {
			old.In = append(old.In, in)
		}
	}
	old.Out = cmpFirst(old.Out, add.Out)
	old.Doc = cmpFirst(old.Doc, add.Doc)
	old.Global = old.Global || add.Global
	old.Data = old.Data || add.Data
	old.Wild = old.Wild || add.Wild
	return old
}

// addSavedScopes reads the trailing bare name list of engine-saved scopes.
func (s *Schema) addSavedScopes(tail string) {
	seen := make(map[string]bool, len(s.SavedScopes))
	for _, n := range s.SavedScopes {
		seen[n] = true
	}
	for _, raw := range strings.Split(tail, "\n") {
		t := strings.TrimSpace(raw)
		if t == "" || t == ":" || strings.HasPrefix(t, "-") {
			continue
		}
		t = strings.TrimPrefix(t, ":")
		t = strings.TrimSpace(t)
		if t == "" || t != tokenRe.FindString(t) || seen[t] {
			continue
		}
		seen[t] = true
		s.SavedScopes = append(s.SavedScopes, t)
	}
	slices.Sort(s.SavedScopes)
}

// docFields is one block's labelled values plus whatever prose was left over.
type docFields struct {
	vals  map[string]string
	prose string
}

// fields splits body lines into "Label: value" pairs, a usage signature, and
// prose. Markdown bold around the label is stripped, so "**Supported Scopes**:
// country" and "Supported Scopes: country" parse the same.
//
// The signature is the one piece these files do NOT label. All three games
// write it as a line restating the token's own name as an assignment, directly
// under the header:
//
//	if - Executes enclosed effects if limit criteria are met
//	if = { limit = { <triggers> } <effects> }
//	Supported Scopes: none
//
// Reading it as prose left EngineToken.Usage empty for every token in every
// game — so signature help had nothing to show and every hover ran the
// signature together with the description in one blob.
// blockFields reads the "Label: value" body of a dump block.
//
// A function rather than a method: game.DocBlock is the LAYOUT -- the carving
// of a dump into named blocks, which differs per game -- while what the labels
// mean is the same everywhere and belongs here.
func blockFields(b game.DocBlock) docFields {
	f := docFields{vals: map[string]string{}}
	var body, usage []string
	depth := 0
	for _, raw := range b.Lines {
		t := stripDocMarkup(strings.TrimSpace(raw))
		if t == "" || docPlaceholder(t) {
			continue
		}
		if label, value, ok := splitLabel(t); ok {
			// A label always ends a signature. CK3 dumps `switch = {` with no
			// closing brace, so the label is the only reliable terminator.
			depth = 0
			prev, seen := f.vals[label]
			switch {
			case !seen:
				f.vals[label] = value
			case scopeListLabels[label]:
				// A block may state its scope list twice, once as prose and
				// once as the real labelled field, and the two disagree.
				// Victoria 3's has_modifier writes "Supported scopes: Country,
				// …, InterestGroup" above the signature and "**Supported
				// Scopes**: country, …, interest_group" below it. Keeping only
				// the first left `interest_group` out of the list, and the
				// wrong-scope check then flagged correct vanilla script.
				//
				// Neither line is marked as authoritative in any syntax, so
				// both are kept. A display name that is not a real scope type
				// ("interestgroup") matches nothing and is inert.
				f.vals[label] = prev + ", " + value
			}
			continue
		}
		if depth > 0 || usageStart(b.Name, t) {
			usage = append(usage, t)
			depth += strings.Count(t, "{") - strings.Count(t, "}")
			if depth < 0 {
				depth = 0
			}
			continue
		}
		body = append(body, t)
	}
	if len(usage) > 0 {
		if _, seen := f.vals["usage"]; !seen {
			f.vals["usage"] = strings.Join(usage, "\n")
		}
	}
	f.prose = prose(body)
	return f
}

// usageStart reports a body line that restates the token's own name as an
// assignment. A description mentioning the token mid-sentence does not match,
// because the name must be the very first thing on the line.
func usageStart(name, t string) bool {
	if name == "" || len(t) < len(name) || !strings.EqualFold(t[:len(name)], name) {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(t[len(name):], " \t"), "=")
}

// splitLabel accepts a line as "Label: value" only when the label is a short
// run of words, so prose containing a colon stays prose.
func splitLabel(t string) (label, value string, ok bool) {
	i := strings.IndexByte(t, ':')
	if i <= 0 {
		return "", "", false
	}
	label = strings.ToLower(strings.TrimSpace(t[:i]))
	if label == "" || len(label) > 24 || strings.ContainsAny(label, "=<>{}[]$#\"") {
		return "", "", false
	}
	if len(strings.Fields(label)) > 3 {
		return "", "", false
	}
	if !docLabels[label] {
		return "", "", false
	}
	return label, strings.TrimSpace(t[i+1:]), true
}

// docPlaceholders are strings a game prints in place of documentation nobody
// wrote. EU5 stamps "Unknown, add something in code registration" on 253 of the
// 288 links in its event_targets.log; CK3 emits no placeholder at all. Showing
// one on a hover card claims the engine documented something it did not, and
// crowds out the parts of the card that are real.
//
// Matched exactly, not by keyword: these are literal constants in the games'
// output, and prose that merely reads as vague is still the game's own prose.
var docPlaceholders = map[string]bool{
	"unknown, add something in code registration": true,
}

func docPlaceholder(line string) bool {
	return docPlaceholders[strings.ToLower(strings.TrimSpace(line))]
}

// scopeListLabels are the multi-valued labels safe to concatenate when a block
// states them more than once. Single-valued labels (a target type, an output
// scope) keep the first, since joining two of those would corrupt the value.
var scopeListLabels = map[string]bool{
	"supported scopes": true, "input scopes": true,
}

// docLabels is the closed set of labels the games emit. Anything else is prose.
var docLabels = map[string]bool{
	"supported scopes": true, "supported targets": true, "usage": true,
	"input scopes": true, "output scopes": true,
	"global link": true, "requires data": true, "wild card": true,
	"expected scope": true, "from code": true,
	"evaluate triggers": true, "execute effects": true, "change scopes": true,
	"save token": true, "stores variables": true,
	"mask": true, "use areas": true, "categories": true,
	"name": true, "description": true,
	"scope": true, "random valid": true,
}

func (f docFields) str(label string) string { return f.vals[label] }

func (f docFields) yes(label string) bool {
	return strings.EqualFold(f.vals[label], "yes")
}

// list splits a comma-separated scope list ("country, state").
func (f docFields) list(label string) []string {
	v := f.vals[label]
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (f docFields) token() EngineToken {
	return EngineToken{
		In:     f.list("supported scopes"),
		Target: strings.ToLower(f.str("supported targets")),
		Doc:    f.prose,
		Usage:  f.str("usage"),
	}
}

func cmpFirst(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// iterPrefixes are the four spellings of one iterator. They document each other
// unevenly -- CK3 declares `category` on any_trait_in_category and omits it on
// random_trait_in_category -- so the family is read as one.
var iterPrefixes = []string{"any_", "every_", "random_", "ordered_"}

// TokenParams is the set of argument names the game declares inside a token's
// block, read from the usage lines script_docs prints beside it:
//
//	any_trait_in_category = { category = fame/health/etc }
//
// `category` there is an argument consumed by the iterator, not a trigger
// evaluated in the trait scope it produces -- and `category` IS a declared
// trigger, on artifact, so judging it reported correct vanilla script.
//
// Declared data, not a heuristic: an iterator whose arguments the game does not
// document contributes none, and a game update is a rescan.
func (s *Schema) TokenParams(key string) map[string]bool {
	if s == nil {
		return nil
	}
	k := strings.ToLower(key)
	out := map[string]bool{}
	add := func(name string) {
		if t, _, ok := s.Token(name); ok {
			for _, p := range usageParams(t.Usage) {
				out[p] = true
			}
		}
	}
	add(k)
	for _, p := range iterPrefixes {
		if stem, cut := strings.CutPrefix(k, p); cut {
			for _, q := range iterPrefixes {
				add(q + stem)
			}
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// usageParams reads the keys assigned at the top level of a usage block.
//
// The whole usage is one stream, not a line at a time: the game prints
// multi-line signatures, and looking for `{` per line saw only the first.
// add_activity_log_entry declares eight arguments across nine lines, `tags`
// among them, and `tags` is otherwise unknown -- 308 findings on vanilla CK3.
//
// Whitespace does NOT end a name: the game writes `limit = { … }` with spaces
// around the operator and `<count=num/all>` without them, and only the second
// form survived clearing the word on a space.
func usageParams(usage string) []string {
	var out []string
	ident := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')
	}
	depth, word, started := 0, "", false
	for i := 0; i < len(usage); i++ {
		c := usage[i]
		switch {
		case c == '{':
			depth++
			started = true
			word = ""
		case c == '}':
			if depth > 0 {
				depth--
			}
			word = ""
		case !started:
			// Before the first brace is the token's own name and operator.
		case c == '=':
			if depth == 1 && word != "" && jomini.IsLowerName(word) {
				out = append(out, word)
			}
			word = ""
		case c == ' ' || c == '\t':
			// Keep the word: the operator may be spaced away from it.
		case ident(c):
			word += string(c)
		default:
			// A placeholder's brackets, a slash, a comment hash: whatever was
			// accumulating is not a key being assigned.
			word = ""
		}
	}
	return out
}
