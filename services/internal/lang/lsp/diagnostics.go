// diagnostics.go is what is wrong with a file, judged by the plan's §6a: a diagnostic accuses only on a fact the game declares (a token's scopes, a declared prefix bound to a kind, a declared field type) or where the install's own structure never holds the key; a learned convention (required-loc) is held to zero findings on vanilla. The install's own files are never judged. A code is turned off in the settings, or on one line by `# pmt:ignore code`.
package lsp

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// Diagnostic is one finding.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1 error, 2 warning
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Code is one check, for the settings: what it finds, and whether it runs when the user has not chosen.
type Code struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Default bool   `json:"default"`
}

// Codes is every check a user may turn off. The parsers' errors are not among them: a file that does not parse is wrong.
var Codes = []Code{
	{"unresolved-ref", "a declared cite names a definition nothing has", true},
	{"wrong-value-type", "a value of another type than the game declares for the field", true},
	{"unknown-key", "a key the install's structure never holds", true},
	{"wrong-scope", "a token written in a scope it does not run in", true},
	{"duplicate-definition", "a name the workspace defines twice", true},
	{"required-loc", "a definition without the localization key every vanilla one has", true},
	{"missing-bom", "a localization file without a UTF-8 byte order mark", true},
	{"loc-header-mismatch", "a localization file whose header names another language than its name", true},
	{"wrong-localization-folder", "a localization folder spelled localisation, which the game never reads", true},
	{"missing-file", "a path that exists nowhere in the install or the mods", false},
}

// Diagnose is the findings for a file, under the codes the user has on; nil enabled means the defaults.
func Diagnose(s *session.Session, path string, enabled map[string]bool) ([]Diagnostic, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	if f.place.Origin == game.OriginVanilla || f.place.Origin == "" {
		return nil, nil
	}
	if enabled == nil {
		enabled = map[string]bool{}
		for _, c := range Codes {
			enabled[c.Code] = c.Default
		}
	}
	d := &judge{f: f, on: enabled, ignored: ignores(f.text)}
	switch f.place.Format {
	case source.Loc:
		d.locFile()
	case source.Script:
		d.script()
	}
	return d.out, nil
}

// judge gathers the findings of one file.
type judge struct {
	f       *file
	on      map[string]bool
	ignored map[int][]string // by 0-based line: the codes a pmt:ignore comment turns off there; "" for all
	out     []Diagnostic
}

// report keeps a finding whose code is on and not ignored on its line.
func (d *judge) report(r jomini.Range, severity int, code, message string) {
	line := d.f.parsed.Lines().PositionAt(r.Start).Line
	if !d.on[code] && slices.ContainsFunc(Codes, func(c Code) bool { return c.Code == code }) {
		return
	}
	if codes, ok := d.ignored[line]; ok && (len(codes) == 0 || slices.Contains(codes, code)) {
		return
	}
	d.out = append(d.out, Diagnostic{Range: d.f.span(r), Severity: severity, Code: code, Message: message})
}

// ignores reads every `# pmt:ignore code, code` comment: it turns those codes off on its own line and the line below; with no code, every code.
func ignores(text string) map[int][]string {
	out := map[int][]string{}
	for i, line := range strings.Split(text, "\n") {
		_, after, ok := strings.Cut(line, "pmt:ignore")
		if !ok || !strings.Contains(line[:strings.Index(line, "pmt:ignore")], "#") {
			continue
		}
		codes := strings.FieldsFunc(after, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\r' })
		out[i] = codes
		if _, own := out[i+1]; !own {
			out[i+1] = codes
		}
	}
	return out
}

// script judges a script file: the parser's errors, then every statement of the one descent.
func (d *judge) script() {
	s := d.f.s
	for _, e := range d.f.parsed.Errors {
		d.report(e.Range, 1, string(e.Code), e.Message)
	}
	found, _, _ := s.Located(d.f.path)
	r := s.Reader()
	written := writtenKeys(s.Install.Schema)
	records := index.Visit(found, d.f.text, r, func(level index.Level) {
		a, ok := level.Statement.(*jomini.Assignment)
		if !ok {
			if v, ok := level.Statement.(*jomini.ValueStmt); ok {
				if scalar, ok := v.Value.(*jomini.Scalar); ok {
					d.value(r.Value(level.Field, scalar.Text, scalar.Quoted), level.Field, scalar)
				}
			}
			return
		}
		d.key(level, a, written)
		if scalar, ok := a.Value.(*jomini.Scalar); ok {
			d.value(r.Value(level.Below, scalar.Text, scalar.Quoted), level.Below, scalar)
		}
	})
	d.definitions(records.Defs)
}

// key judges a key: a token in a scope it does not run in; a key a learned structure never holds; a macro's argument the macro never spells.
func (d *judge) key(level index.Level, a *jomini.Assignment, written map[string]bool) {
	s := d.f.s
	key, v := a.Key.Text, level.Key
	switch v.Type {
	case schema.TypeToken:
		_, _, in, _ := tokenDocs(s.Install.Vocab, v.Name)
		if level.Scope != "" && len(in) > 0 && !schema.UsableIn(in, level.Scope) {
			d.report(a.Key.Range, 2, "wrong-scope", fmt.Sprintf("%s runs on %s, but this block is %s scope", key, strings.Join(in, " or "), level.Scope))
		}
	case schema.TypeRef:
		if v.Kind == "params" && !s.All().Def("params", v.Name) {
			d.report(a.Key.Range, 2, "unknown-key", fmt.Sprintf("%s spells no parameter %s", v.Parent, key))
		}
	case schema.TypeUnresolved:
		field := level.Field
		if field == nil || field.Class != schema.ClassStructure || level.Kind == nil || s.Install.Schema.Kinds[level.Kind.Name] == nil || written[key] || a.Key.Quoted || strings.Contains(key, "$") {
			return
		}
		if !jomini.IsDefName(key) {
			return
		}
		d.report(a.Key.Range, 2, "unknown-key", fmt.Sprintf("%s is not a key of %s in any file of the game", key, fieldName(field)))
	}
}

// fieldName is a field's name for a message, the kind's own shape being "the definition".
func fieldName(f *schema.Field) string {
	if f.Name == "" {
		return "the definition"
	}
	return f.Name
}

// value judges a word: a declared cite nothing defines; a literal of another type than the field's declared one; a path that exists nowhere.
func (d *judge) value(v schema.Value, field *schema.Field, scalar *jomini.Scalar) {
	switch {
	case v.Type == schema.TypeRef && v.Declared && v.Kind != "" && !d.f.s.All().Def(v.Kind, v.Name):
		if _, scriptMade := schema.ScriptKinds[v.Kind]; !scriptMade {
			d.report(scalar.Range, 2, "unresolved-ref", fmt.Sprintf("no %s named %s", v.Kind, v.Name))
		}
	case field != nil && (v.Type == schema.TypeString || v.Type == schema.TypeUnresolved || v.Type == schema.TypeNumber || v.Type == schema.TypeBool || v.Type == schema.TypeDate):
		var declared []string
		for _, a := range field.Accepts {
			if a.Declared {
				declared = append(declared, a.Type.String())
			}
		}
		fits := len(declared) == 0 || slices.ContainsFunc(field.Accepts, func(a schema.Accept) bool {
			return a.Declared && (a.Type == v.Type || a.Type == schema.TypeRef || a.Type == schema.TypeLoc || a.Type == schema.TypeEnum || a.Type == schema.TypeString)
		})
		if !fits {
			d.report(scalar.Range, 2, "wrong-value-type", fmt.Sprintf("%s is written as %s; the game declares %s", scalar.Text, v.Type, strings.Join(declared, " or ")))
		}
	}
	if (v.Type == schema.TypeUnresolved || v.Type == schema.TypeString) && strings.Contains(scalar.Text, "/") && strings.Contains(scalar.Text, ".") {
		d.report(scalar.Range, 2, "missing-file", fmt.Sprintf("%s exists nowhere in the install or the mods", scalar.Text))
	}
}

// definitions judges what a file defines: a name another file of the workspace defines too, and a definition without the loc key every vanilla one has.
func (d *judge) definitions(defs []index.Def) {
	s := d.f.s
	lines := d.f.parsed.Lines()
	found, _, _ := s.Located(d.f.path)
	for _, def := range defs {
		k := s.Schema.Kinds[def.Kind]
		if k == nil || (k.Naming.Form != "key" && k.Naming.Form != "event_id" && k.Naming.Form != "number" && k.Naming.Form != "declaration") {
			continue
		}
		at := jomini.Range{Start: lines.OffsetAt(int(def.Line), int(def.Col)), End: lines.OffsetAt(int(def.Line), int(def.Col)) + len(def.Name)}
		self := index.Found{Def: def, At: found}
		for _, other := range s.Defs(def.Name) {
			if other.Kind == def.Kind && other.At.Origin != game.OriginVanilla && other != self && index.Before(other, self, s.Order()) < 0 {
				d.report(at, 2, "duplicate-definition", fmt.Sprintf("%s is also defined in %s", def.Name, other.At.Rel))
				break
			}
		}
		for _, c := range k.LocTemplates {
			if key := loc.Expand(def.Name, []loc.Convention{c})[0]; c.Coverage == 1 && !s.All().Loc(key) {
				d.report(at, 2, "required-loc", fmt.Sprintf("%s has no localization key %s (every vanilla %s has one)", def.Name, key, def.Kind))
			}
		}
	}
}

// locFile judges a localization file: the parser's errors, its header against its name, its folder's spelling, and its byte order mark.
func (d *judge) locFile() {
	parsed := loc.Parse(d.f.text)
	for _, e := range parsed.Errors {
		d.report(e.Range, 1, "loc-"+string(e.Code), e.Message)
	}
	whole := jomini.Range{}
	if named := loc.LanguageFromFilename(d.f.path); named != "" && parsed.Language != "" && named != parsed.Language {
		d.report(whole, 2, "loc-header-mismatch", fmt.Sprintf("the file is named for %s but its header says l_%s", named, parsed.Language))
	}
	if strings.Contains(strings.ToLower(d.f.place.Rel), "localisation/") {
		d.report(whole, 2, "wrong-localization-folder", "the game reads localization, not localisation")
	}
	if !parsed.HadBOM {
		raw, err := os.ReadFile(d.f.path)
		if err != nil || !jomini.HasUTF8BOM(raw) {
			d.report(whole, 2, "missing-bom", "the game reads a localization file only with a UTF-8 byte order mark")
		}
	}
}

var known sync.Map

// writtenKeys is every key the install writes anywhere, as a member of any kind's shape or of any token's arguments, computed once per schema.
func writtenKeys(s *schema.Schema) map[string]bool {
	if keys, ok := known.Load(s); ok {
		return keys.(map[string]bool)
	}
	keys := map[string]bool{}
	var walk func(f *schema.Field)
	walk = func(f *schema.Field) {
		for name, m := range f.Members {
			keys[name] = true
			walk(m)
		}
	}
	for _, k := range s.Kinds {
		if k.Shape != nil {
			walk(k.Shape)
		}
	}
	for _, f := range s.Arguments {
		walk(f)
	}
	known.Store(s, keys)
	return keys
}
