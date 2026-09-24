// diagnostics.go is what is wrong with a file. A diagnostic accuses only on a fact the game
// declares (a token's scopes, a declared prefix bound to a kind, a declared field type); a learned
// convention (required-loc) is held to the gate over the games' own files. The install's own files
// are never judged. A code is turned off in the settings, or on one line by `# pmt:ignore code`.
package lsp

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// Diagnostic is one finding.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1 error, 2 warning
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Code is one check, for the settings: what it finds, and whether it runs by default.
type Code struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Default bool   `json:"default"`
}

// Codes is every check a user may turn off. The parsers' errors are not among them: a file that
// does not parse is wrong.
var Codes = []Code{
	{"unresolved-ref", "a declared cite names a definition nothing has", true},
	{"wrong-value-type", "a value of another type than the game declares for the field", true},
	{"wrong-scope", "a token written in a scope it does not run in", true},
	{"duplicate-definition", "a name the workspace defines twice", true},
	{"required-loc", "a definition without the localization key every vanilla one has", true},
	{"missing-bom", "a localization file without a UTF-8 byte order mark", true},
	{"loc-header-mismatch", "a localization file whose header names another language than its name", true},
	{"wrong-localization-folder", "a localization folder spelled localisation, which the game never reads", true},
	{"missing-file", "a path that exists nowhere in the install or the mods", false},
}

// Diagnose is a file's findings under the codes the user has on; nil enabled means the defaults.
func Diagnose(sess *session.Session, path string, enabled map[string]bool) ([]Diagnostic, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	if file.place.Origin == game.OriginVanilla || file.place.Origin == "" {
		return nil, nil
	}
	if enabled == nil {
		enabled = map[string]bool{}
		for _, code := range Codes {
			enabled[code.Code] = code.Default
		}
	}
	judge := &judge{file: file, on: enabled, ignored: ignores(file.text)}
	switch file.place.Format {
	case source.Loc:
		judge.locFile()
	case source.Script:
		judge.script()
	}
	return judge.out, nil
}

// judge gathers the findings of one file.
type judge struct {
	file    *file
	on      map[string]bool
	ignored map[int][]string // by 0-based line: the codes a pmt:ignore comment turns off there; "" for all
	out     []Diagnostic
}

// report keeps a finding whose code is on and not ignored on its line.
func (judge *judge) report(span jomini.Range, severity int, code, message string) {
	line := judge.file.parsed.Lines().PositionAt(span.Start).Line
	if !judge.on[code] && slices.ContainsFunc(Codes, func(known Code) bool { return known.Code == code }) {
		return
	}
	if codes, ok := judge.ignored[line]; ok && (len(codes) == 0 || slices.Contains(codes, code)) {
		return
	}
	judge.out = append(judge.out, Diagnostic{Range: judge.file.span(span), Severity: severity, Code: code, Message: message})
}

// ignores reads every `# pmt:ignore code, code` comment: it turns those codes off on its own line
// and the line below; with no code, every code.
func ignores(text string) map[int][]string {
	out := map[int][]string{}
	for i, line := range strings.Split(text, "\n") {
		_, after, ok := strings.Cut(line, "pmt:ignore")
		if !ok || !strings.Contains(line[:strings.Index(line, "pmt:ignore")], "#") {
			continue
		}
		codes := strings.FieldsFunc(after, func(char rune) bool { return char == ',' || char == ' ' || char == '\t' || char == '\r' })
		out[i] = codes
		if _, own := out[i+1]; !own {
			out[i+1] = codes
		}
	}
	return out
}

// script judges a script file: the parser's errors, then every statement of the one descent.
func (judge *judge) script() {
	sess := judge.file.sess
	for _, issue := range judge.file.parsed.Errors {
		judge.report(issue.Range, 1, string(issue.Code), issue.Message)
	}
	found, _, _ := sess.Located(judge.file.path)
	reader := sess.Reader()
	records := index.Visit(found, judge.file.parsed, reader, func(level index.Level) {
		assignment, ok := level.Statement.(*jomini.Assignment)
		if !ok {
			if bare, ok := level.Statement.(*jomini.ValueStmt); ok {
				if scalar, ok := bare.Value.(*jomini.Scalar); ok {
					judge.value(reader.Value(level.Field, scalar.Text, scalar.Quoted), level.Field, scalar)
				}
			}
			return
		}
		judge.key(level, assignment)
		if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
			judge.value(reader.Value(level.Below, scalar.Text, scalar.Quoted), level.Below, scalar)
		}
	})
	judge.definitions(records.Defs)
}

// key judges a key: a token in a scope it does not run in. No key is judged against a structure or
// a macro's parameters: the schema keeps a structure's members, not every key the install writes,
// and a macro's parameters are not every argument the game passes (`$SUCCESS_%$`, effects among an
// EU5 call's arguments).
func (judge *judge) key(level index.Level, assignment *jomini.Assignment) {
	sess := judge.file.sess
	key, keyValue := assignment.Key.Text, level.Key
	if keyValue.Type == schema.TypeToken {
		_, _, in, _ := tokenDocs(sess.Install.Vocab, keyValue.Name)
		if level.Scope != "" && len(in) > 0 && !schema.UsableIn(in, level.Scope) {
			judge.report(assignment.Key.Range, 2, "wrong-scope", fmt.Sprintf("%s runs on %s, but this block is %s scope", key, strings.Join(in, " or "), level.Scope))
		}
	}
}

// value judges a word: a declared cite nothing defines, a literal of another type than the field's
// declared one, a path that exists nowhere.
func (judge *judge) value(value schema.Value, field *schema.Field, scalar *jomini.Scalar) {
	switch {
	case value.Type == schema.TypeRef && value.Declared && value.Kind != "" && !judge.file.sess.All().Def(value.Kind, value.Name):
		// a member cite (define:NGroup|KEY) whose group exists may name a member the learner did not
		// read, and a prefix bound to a subject kind names what is defined elsewhere: neither is
		// accused
		kind := judge.file.sess.Schema.Kinds[value.Kind]
		if _, scriptMade := schema.ScriptKinds[value.Kind]; !scriptMade && (value.Parent == "" || len(judge.file.sess.Defs(value.Parent)) == 0) && (kind == nil || kind.Subject == "") {
			judge.report(scalar.Range, 2, "unresolved-ref", fmt.Sprintf("no %s named %s", value.Kind, value.Name))
		}
	case field != nil && (value.Type == schema.TypeString || value.Type == schema.TypeUnresolved || value.Type == schema.TypeNumber || value.Type == schema.TypeBool || value.Type == schema.TypeDate):
		var declared []string
		for _, accept := range field.Accepts {
			if accept.Declared {
				declared = append(declared, accept.Type.String())
			}
		}
		fits := len(declared) == 0 || slices.ContainsFunc(field.Accepts, func(accept schema.Accept) bool {
			return accept.Declared && (accept.Type == value.Type || accept.Type == schema.TypeRef || accept.Type == schema.TypeLoc || accept.Type == schema.TypeEnum || accept.Type == schema.TypeString)
		})
		if !fits {
			judge.report(scalar.Range, 2, "wrong-value-type", fmt.Sprintf("%s is written as %s; the game declares %s", scalar.Text, value.Type, strings.Join(declared, " or ")))
		}
	}
	if (value.Type == schema.TypeUnresolved || value.Type == schema.TypeString) && strings.Contains(scalar.Text, "/") && strings.Contains(scalar.Text, ".") {
		judge.report(scalar.Range, 2, "missing-file", fmt.Sprintf("%s exists nowhere in the install or the mods", scalar.Text))
	}
}

// definitions judges what a file defines: a name another workspace file defines too, and a
// definition without the loc key every vanilla one has.
func (judge *judge) definitions(defs []index.Def) {
	sess := judge.file.sess
	lines := judge.file.parsed.Lines()
	found, _, _ := sess.Located(judge.file.path)
	for _, def := range defs {
		kind := sess.Schema.Kinds[def.Kind]
		if kind == nil || (kind.Naming.Form != "key" && kind.Naming.Form != "event_id" && kind.Naming.Form != "number" && kind.Naming.Form != "declaration") {
			continue
		}
		at := jomini.Range{Start: lines.OffsetAt(int(def.Line), int(def.Col)), End: lines.OffsetAt(int(def.Line), int(def.Col)) + len(def.Name)}
		self := index.Found{Def: def, At: found}
		for _, other := range sess.Defs(def.Name) {
			if other.Kind == def.Kind && other.At.Origin != game.OriginVanilla && other != self && index.Before(other, self, sess.Order()) < 0 {
				judge.report(at, 2, "duplicate-definition", fmt.Sprintf("%s is also defined in %s", def.Name, other.At.Rel))
				break
			}
		}
		for _, locTemplate := range kind.LocTemplates {
			if key := loc.Expand(def.Name, []loc.Convention{locTemplate})[0]; locTemplate.Coverage == 1 && !sess.All().Loc(key) {
				judge.report(at, 2, "required-loc", fmt.Sprintf("%s has no localization key %s (every vanilla %s has one)", def.Name, key, def.Kind))
			}
		}
	}
}

// locFile judges a localization file: the parser's errors, its header against its name, its
// folder's spelling, and its byte order mark.
func (judge *judge) locFile() {
	parsed := loc.Parse(judge.file.text)
	for _, issue := range parsed.Errors {
		judge.report(issue.Range, 1, "loc-"+string(issue.Code), issue.Message)
	}
	whole := jomini.Range{}
	if named := loc.LanguageFromFilename(judge.file.path); named != "" && parsed.Language != "" && named != parsed.Language {
		judge.report(whole, 2, "loc-header-mismatch", fmt.Sprintf("the file is named for %s but its header says l_%s", named, parsed.Language))
	}
	if strings.Contains(strings.ToLower(judge.file.place.Rel), "localisation/") {
		judge.report(whole, 2, "wrong-localization-folder", "the game reads localization, not localisation")
	}
	if !parsed.HadBOM {
		raw, err := os.ReadFile(judge.file.path)
		if err != nil || !jomini.HasUTF8BOM(raw) {
			judge.report(whole, 2, "missing-bom", "the game reads a localization file only with a UTF-8 byte order mark")
		}
	}
}
