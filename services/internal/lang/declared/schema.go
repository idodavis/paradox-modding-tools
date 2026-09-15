// schema.go reads the type system a game ships in its script_docs output: the
// scope-type universe, the scope links (which double as the typed-prefix table),
// and every engine token with its input scope and argument type. Nothing here is
// inferred — each field is declared by the game. Three syntaxes, one model:
// classic "----" blocks (CK3), markdown headings (EU5, Vic3), and bare "name:"
// blocks (event_scopes.log, Vic3 modifiers.log).

package declared

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
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
			// The one point dump text leaves the file: scrub invalid UTF-8 here.
			s.ingest(kind, strings.ToValidUTF8(string(raw), "�"))
			return nil
		})
	}
	if s.Empty() && len(s.OnActions) == 0 && len(s.Modifiers) == 0 {
		return nil
	}
	if !newest.IsZero() {
		s.ReadAt = newest.UTC().Format(time.RFC3339)
	}
	return s
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
	for raw := range strings.SplitSeq(tail, "\n") {
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

// blockFields splits a dump block's body into "Label: value" pairs, the unlabelled usage signature line, and leftover prose.
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
				// A block may state its scope list twice and disagree, so both are kept.
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
	f.prose = jomini.Prose(body)
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

// docPlaceholders are literal strings a game prints in place of documentation nobody wrote, matched exactly, not by keyword.
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

// TokenParams is the set of argument names the game declares inside a token's block, read from the usage line.
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

// usageParams reads the keys assigned at the top level of a usage block, as one stream rather than a line at a time.
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

var tokenRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)`)

func (s *Schema) SourceKind() SchemaSource {
	if s.Empty() {
		return SchemaMissing
	}
	if s.Source == "" {
		return SchemaLive
	}
	return s.Source
}

// SchemaSource says where a Schema was read from.
type SchemaSource string

const (
	// SchemaLive means the game's own user-data folder supplied the dumps.
	SchemaLive SchemaSource = "live"
	// SchemaArchive means the live dumps were gone and PMT's copy was used.
	SchemaArchive SchemaSource = "archive"
	// SchemaMissing means neither exists; the engine runs without a type system.
	SchemaMissing SchemaSource = "missing"
)

// IsDumpFile reports a filename that is one of the game's script_docs dumps.
// Exported for the archive, which copies exactly these and nothing else.
func IsDumpFile(name string) bool { return kindFromDocFilename(strings.ToLower(name)) != "" }

// Modifier returns the declared modifier named key. EU5 documents 2,436 of them
// with a category and no prose at all, so Area is usually the only thing the
// game says about one.
func (s *Schema) Modifier(key string) (Modifier, bool) {
	if s == nil {
		return Modifier{}, false
	}
	m, ok := s.Modifiers[strings.ToLower(key)]
	return m, ok
}

// Link returns the declared scope link named key.
func (s *Schema) Link(key string) (ScopeLink, bool) {
	if s == nil {
		return ScopeLink{}, false
	}
	l, ok := s.Links[strings.ToLower(key)]
	return l, ok
}

// TokensInScope returns the declared effect or trigger names usable in scope,
// filtered by prefix. slot is "effect" or "trigger". An empty scope returns
// every token, since an unknown scope must not hide valid completions.
func (s *Schema) TokensInScope(scope, slot, prefix string, limit int) []string {
	if s == nil {
		return nil
	}
	bag := s.Effects
	if slot == "trigger" {
		bag = s.Triggers
	}
	lower := strings.ToLower(prefix)
	out := make([]string, 0, min(limit, len(bag)))
	for name, t := range bag {
		if prefix != "" && !strings.HasPrefix(name, lower) {
			continue
		}
		if !UsableIn(t.In, scope) {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Schema) ScopeTokens(scope string) map[string]bool {
	if scope == "" || s == nil {
		return nil
	}
	out := make(map[string]bool, len(s.Effects)+len(s.Triggers))
	for _, bag := range []map[string]EngineToken{s.Effects, s.Triggers} {
		for name, t := range bag {
			if UsableIn(t.In, scope) {
				out[strings.ToLower(name)] = true
			}
		}
	}
	return out
}
