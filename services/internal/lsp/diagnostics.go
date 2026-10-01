// diagnostics.go is what is wrong with a file. A diagnostic accuses only on a fact the game
// declares (a command's scopes, a declared prefix bound to a database, a declared key type); a
// learned convention (required-loc) is held to the gate over the games' own files. The install's
// own files are never judged. A code is turned off in the settings, or on one line by `# pmt:ignore code`.
package lsp

import (
	"fmt"
	"maps"
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
	{"unresolved-ref", "a declared reference names an entry nothing defines", true},
	{"wrong-value-type", "a value of another type than the game declares for the key", true},
	{"wrong-scope", "a command written in a scope it does not run in", true},
	{"duplicate-definition", "a name the workspace defines twice", true},
	{"required-loc", "a definition without the localization key every vanilla one has", true},
	{"missing-bom", "a localization file without a UTF-8 byte order mark", true},
	{"loc-header-mismatch", "a localization file whose header names another language than its name", true},
	{"wrong-localization-folder", "a localization folder spelled localisation, which the game never reads", true},
	{"missing-file", "a path that exists nowhere in the install or the mods", false},
	{"unknown-event", "an event id no event defines", true},
	{"wrong-declared-value", "a word outside the words the game declares for the key", true},
	{"missing-key", "a key the game's docs say every entry writes, left out", false},
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
					judge.value(reader.Value(level.At, scalar.Text, scalar.Quoted), level.At, scalar, level.Scopes)
				}
			}
			return
		}
		judge.key(level, assignment)
		if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
			judge.value(reader.Value(level.Below, scalar.Text, scalar.Quoted), level.Below, scalar, level.Scopes)
		}
	})
	judge.definitions(records.Defs)
}

// key judges a key: a command in a scope it does not run in, a variable read where the scope stores
// none, and a key the docs require that its block leaves out. No key is judged against a structure
// or a scripted effect's parameters: the schema keeps a structure's keys, not every key the install
// writes, and a scripted effect's parameters are not every argument the game passes
// (`$SUCCESS_%$`, effects among an EU5 call's arguments).
func (judge *judge) key(level index.Level, assignment *jomini.Assignment) {
	sess := judge.file.sess
	key, keyWord, scopes := assignment.Key.Text, level.Key, level.Scopes
	if _, _, in, _ := commandDocs(sess.Schema.Dumps, keyWord.Name); keyWord.Type == schema.WordCommand && scopes.This != "" && !scopes.Guessed && len(in) > 0 && !schema.UsableIn(in, scopes.This) {
		var runs []string
		for _, scope := range in {
			runs = append(runs, plain(scope))
		}
		message := fmt.Sprintf("%s runs on %s, but this block is %s %s", key, strings.Join(runs, " or "), plain(scopes.This), chain(scopes))
		judge.report(assignment.Key.Range, 2, "wrong-scope", message)
	}
	judge.variable(keyWord, key, assignment.Key.Range, scopes)

	// a key the docs say every entry of this block writes
	block := jomini.BlockOf(assignment.Value)
	if block == nil || level.Below == nil {
		return
	}
	written := map[string]bool{}
	for _, st := range block.Statements {
		if inner, ok := st.(*jomini.Assignment); ok {
			written[strings.ToLower(inner.Key.Text)] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(level.Below.Keys)) {
		if child := level.Below.Keys[name]; child.Required == schema.RequiredAlways && child.Page == "" && !written[strings.ToLower(name)] {
			judge.report(assignment.Key.Range, 2, "missing-key", fmt.Sprintf("%s leaves out %s, which the game's docs say it must write", key, name))
		}
	}
}

// value judges a word: an event id no event defines, a declared reference nothing defines, a word
// outside a declared closed list, a literal of another type than the key's declared one, a variable
// read where the scope stores none, a file path that exists nowhere.
func (judge *judge) value(word schema.Word, key *schema.Key, scalar *jomini.Scalar, scopes index.Scopes) {
	sess := judge.file.sess
	var declared, closed []string
	open := false
	if key != nil {
		for _, take := range key.Takes {
			if !take.Source.Declared() || take.Guessed {
				continue
			}
			declared = append(declared, take.Type.String())
			switch take.Type {
			case schema.WordEnum:
				closed = append(closed, take.Enum...)
			case schema.WordBool:
				closed = append(closed, "yes", "no")
			default:
				open = true
			}
		}
	}
	database := sess.Schema.Databases[word.Database]
	literal := word.Type == schema.WordString || word.Type == schema.WordUnknown || word.Type == schema.WordNumber || word.Type == schema.WordBool || word.Type == schema.WordDate
	switch {
	case word.Type == schema.WordReference && word.Declared && database != nil && database.Naming.Form == "event_id" && jomini.IsEventID(word.Name) && !sess.All().Def(word.Database, word.Name):
		judge.report(scalar.Range, 2, "unknown-event", fmt.Sprintf("no event is named %s", word.Name))
	case word.Type == schema.WordReference && word.Declared && word.Database != "" && !sess.All().Def(word.Database, word.Name):
		// a keyed prefixed word (define:NGroup|KEY) whose group exists may name a key no scan read, a
		// prefix bound to a history database names what is defined elsewhere, and a saved scope the game
		// saves itself is saved nowhere in script: none is accused
		_, scriptMade := schema.ScriptMade[word.Database]
		saved := word.Database == "saved_scopes" && !slices.Contains(sess.Schema.Dumps.SavedScopes, word.Name)
		if (!scriptMade || saved) && (word.Parent == "" || len(sess.Defs(word.Parent)) == 0) && (database == nil || len(database.HistoryOf) == 0) {
			judge.report(scalar.Range, 2, "unresolved-ref", fmt.Sprintf("nothing defines %s %s", strings.ReplaceAll(word.Database, "_", " "), word.Name))
		}
	case len(closed) > 0 && !open && !slices.Contains(closed, scalar.Text) && (literal || word.Type == schema.WordReference || word.Type == schema.WordEnum):
		judge.report(scalar.Range, 2, "wrong-declared-value", fmt.Sprintf("%s is not one of the words the game declares here: %s", scalar.Text, strings.Join(closed, ", ")))
	case len(declared) > 0 && open && literal && !slices.ContainsFunc(key.Takes, func(take schema.Take) bool {
		return take.Source.Declared() && !take.Guessed && (take.Type == word.Type || take.Type == schema.WordReference || take.Type == schema.WordLoc || take.Type == schema.WordEnum || take.Type == schema.WordString)
	}):
		judge.report(scalar.Range, 2, "wrong-value-type", fmt.Sprintf("%s is written as %s; the game declares %s", scalar.Text, word.Type, strings.Join(declared, " or ")))
	}
	judge.variable(word, scalar.Text, scalar.Range, scopes)
	if (word.Type == schema.WordUnknown || word.Type == schema.WordString) && source.Classify("", scalar.Text) != source.Unknown {
		judge.report(scalar.Range, 2, "missing-file", fmt.Sprintf("%s exists nowhere in the install or the mods", scalar.Text))
	}
}

// variable judges a var: word read in a scope the dumps say stores no variables.
func (judge *judge) variable(word schema.Word, text string, span jomini.Range, scopes index.Scopes) {
	scope, known := judge.file.sess.Schema.Dumps.Scopes[scopes.This]
	if word.Database == "variables" && known && !scopes.Guessed && !scope.Variables {
		judge.report(span, 2, "wrong-scope", fmt.Sprintf("%s reads a variable, but %s stores none %s", text, plain(scopes.This), chain(scopes)))
	}
}

// plain is a scope as a sentence says it: a character, an army, a landed title.
func plain(scope string) string {
	words := strings.ReplaceAll(scope, "_", " ")
	if strings.ContainsRune("aeiou", rune(words[0])) {
		return "an " + words
	}
	return "a " + words
}

// chain is the scopes from an entry's root to the block: (root province → this province).
func chain(scopes index.Scopes) string {
	if scopes.Root == "" {
		return "(this " + strings.ReplaceAll(scopes.This, "_", " ") + ")"
	}
	return "(root " + strings.ReplaceAll(scopes.Root, "_", " ") + " → this " + strings.ReplaceAll(scopes.This, "_", " ") + ")"
}

// definitions judges what a file defines: a name another workspace file defines too, and a
// definition without the loc key every vanilla one has. An entry written inline outside its
// database's folders (scripted_trigger x = { } in an event file), a nested database's, and a GUI
// local type or template are never duplicates.
func (judge *judge) definitions(defs []index.Def) {
	sess := judge.file.sess
	lines := judge.file.parsed.Lines()
	found, _, _ := sess.Located(judge.file.path)
	for _, def := range defs {
		database := sess.Schema.Databases[def.Database]
		if database == nil || (database.Naming.Form != "key" && database.Naming.Form != "event_id" && database.Naming.Form != "number" && database.Naming.Form != "declaration") {
			continue
		}
		at := jomini.Range{Start: lines.OffsetAt(int(def.Line), int(def.Col)), End: lines.OffsetAt(int(def.Line), int(def.Col)) + len(def.Name)}
		self := index.Found{Def: def, At: found}
		inline := len(database.Folders) > 0 && !slices.Contains(sess.Schema.DatabasesAt(found.Rel), database)
		single := inline || database.Parent != "" || strings.HasPrefix(database.Naming.Keyword, "local_")
		for _, other := range sess.Defs(def.Name) {
			if !single && other.Database == def.Database && other.At.Origin != game.OriginVanilla && other != self && index.Before(other, self, sess.Order()) < 0 {
				judge.report(at, 2, "duplicate-definition", fmt.Sprintf("%s is also defined in %s", def.Name, other.At.Rel))
				break
			}
		}
		for _, convention := range database.LocKeys {
			if key := loc.Expand(def.Name, []loc.Convention{convention})[0]; convention.Coverage == 1 && !sess.All().Loc(key) {
				judge.report(at, 2, "required-loc", fmt.Sprintf("%s has no localization key %s (every vanilla %s has one)", def.Name, key, def.Database))
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
