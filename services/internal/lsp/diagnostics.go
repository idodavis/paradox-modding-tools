// diagnostics.go is what is wrong with a file. A diagnostic accuses only on a claim a declared
// source states, fresh, that no vanilla evidence contradicts (schema.Speaks); the install's own
// files are never judged. A code is turned off in the settings, or for a line and the next by
// `# pmt:ignore code`.

package lsp

import (
	"bytes"
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
	{"unresolved-ref", "a word naming an entry that nothing defines", true},
	{"wrong-value-type", "a value of another type than the key takes", true},
	{"wrong-scope", "a command written in a scope it does not run in, or a variable read in one that stores none", true},
	{"duplicate-definition", "a name the workspace defines twice", true},
	{"inject-without-target", "an INJECT or REPLACE with nothing loaded before it", true},
	{"missing-bom", "a localization file without a UTF-8 byte order mark", true},
	{"loc-header-mismatch", "a localization file whose header names another language than its name", true},
	{"wrong-localization-folder", "a localization folder spelled localisation, which the game never reads", true},
	{"missing-file", "a path that exists nowhere in the install or the mods", false},
	{"unknown-event", "an event id no event defines", true},
	{"wrong-declared-value", "a word outside the words the key takes", true},
	{"missing-key", "a key the game's docs say every entry writes, left out", false},
}

// Diagnose is a file's findings under the codes the user has on; nil enabled means the defaults.
func Diagnose(sess *session.Session, path string, enabled map[string]bool) ([]Diagnostic, error) {
	sess.RLock()
	defer sess.RUnlock()
	// only a mod's script and loc files have findings; any other file (images, sound) is never read
	place := sess.Where(path)
	isModFile := place.Origin != game.OriginVanilla && place.Origin != ""
	if isText := place.Format == source.Script || place.Format == source.Loc; !isModFile || !isText {
		return nil, nil
	}
	file, err := open(sess, path)
	if err != nil {
		return nil, err
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
	ignored map[int][]string             // by 0-based line: the codes a pmt:ignore comment turns off there; "" for all
	written map[jomini.Range]*schema.Key // the position each word written as a value sits at, by its span
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
		before, after, ok := strings.Cut(line, "pmt:ignore")
		if !ok || !strings.Contains(before, "#") {
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

// script judges a script file: the parser's errors, then each statement as the walk reaches it, and
// the references and definitions the walk recorded.
func (judge *judge) script() {
	sess := judge.file.sess
	for _, issue := range judge.file.parsed.Errors {
		judge.report(issue.Range, 1, string(issue.Code), issue.Message)
	}
	judge.written = map[jomini.Range]*schema.Key{}
	records := index.Walk(sess.Install.Schema, judge.file.place.File, judge.file.parsed, func(at index.Context) bool {
		switch statement := at.Statement.(type) {
		case *jomini.Assignment:
			judge.key(at, statement)
			if scalar, isScalar := statement.Value.(*jomini.Scalar); isScalar {
				judge.value(at, scalar)
			}
		case *jomini.ValueStmt:
			if scalar, isScalar := statement.Value.(*jomini.Scalar); isScalar {
				judge.value(at, scalar)
			}
		}
		return true
	})
	judge.references(records.Refs)
	judge.definitions(records.Defs)
}

// variable judges a non-global variable read (var:x, not local_var:x) written directly in a scope
// the dumps say stores none; an undeclared scope, a read through a link, and a word vanilla writes
// in that scope are quiet.
func (judge *judge) variable(at index.Context, word *jomini.Scalar) {
	sess, sch := judge.file.sess, judge.file.sess.Install.Schema
	scope, declared := sch.Scopes[at.Scope]
	if !declared || scope.Variables {
		return
	}
	prefixed, isPrefixed := jomini.SplitPrefixed(word.Text)
	link, database := sch.Link(strings.ToLower(word.Text)), sch.Prefixes[prefixed.Prefix]
	if isPrefixed && link != nil && !link.Global && database != "" && sch.Databases[database] == nil &&
		!sess.Install.Index.Scoped[index.Pair(word.Text, at.Scope)] && sch.Speaks(schema.ClaimCommand, at.Key, nil) {
		judge.report(word.Range, 2, "wrong-scope", fmt.Sprintf("%s reads a variable, but %s stores none",
			word.Text, plain(at.Scope)))
	}
}

// key judges a key: a variable read, a command written in a scope it does not run in where vanilla
// never writes it there, and a key the position's sources call required that its block leaves out.
func (judge *judge) key(at index.Context, assign *jomini.Assignment) {
	sess, sch := judge.file.sess, judge.file.sess.Install.Schema
	judge.variable(at, &assign.Key)
	if command := at.Command; command != nil && at.Scope != "" && len(command.Scopes) > 0 &&
		!slices.Contains(command.Scopes, at.Scope) && !sess.Install.Index.Scoped[index.Pair(assign.Key.Text, at.Scope)] &&
		sch.Speaks(schema.ClaimCommand, at.Key, nil) {
		runs := make([]string, len(command.Scopes))
		for i, scope := range command.Scopes {
			runs[i] = plain(scope)
		}
		judge.report(assign.Key.Range, 2, "wrong-scope", fmt.Sprintf("%s runs on %s, but this block is %s",
			assign.Key.Text, strings.Join(runs, " or "), plain(at.Scope)))
	}

	// an entry written with a mode word the registry lists (INJECT:) adds to another and may leave
	// keys out
	block := jomini.BlockOf(assign.Value)
	if mode, _ := sess.Game.Mode(assign.Key.Text); block == nil || at.Key == nil || mode != "" {
		return
	}
	written := map[*schema.Key]bool{}
	for _, statement := range block.Statements {
		if inner, isInner := statement.(*jomini.Assignment); isInner {
			written[at.Key.At(inner.Key.Text)] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(at.Key.Keys)) {
		if child := at.Key.Keys[name]; !written[child] && sch.Speaks(schema.ClaimRequired, child, at.Key) {
			judge.report(assign.Key.Range, 2, "missing-key",
				fmt.Sprintf("%s leaves out %s, which the game's docs say it must write", assign.Key.Text, name))
		}
	}
}

// value judges a word's variable read, and the word against its position's declared list or form;
// a constant or parameter is not yet its word, and a word that reads a value stands for a number.
func (judge *judge) value(at index.Context, scalar *jomini.Scalar) {
	judge.variable(at, scalar)
	if at.Key == nil {
		return
	}
	judge.written[scalar.Range] = at.Key
	sess := judge.file.sess
	claim, text := at.Key.Value.Value, scalar.Text
	if _, constant := jomini.Constant(text); constant || len(jomini.Interpolations(text)) > 0 {
		return
	}
	code, wrong := "wrong-value-type", false
	_, listed := slices.BinarySearch(claim.Words, text)
	switch {
	case listed:
	case claim.Type == schema.WordNumber:
		// a script value, a declared command (earnings) or a link (var:x, root.gold) reads a value
		if last := text[strings.LastIndex(text, ".")+1:]; scalar.Quoted || jomini.Literal(text) != jomini.LitNumber {
			_, valued := index.Resolve(sess.Install.Schema, sess.Indexes(), index.Ref{Called: game.Blocks[game.BlockValue].Calls, Name: text})
			_, command := sess.Install.Schema.Commands[last]
			wrong = !valued && !command && !strings.Contains(last, ":")
		}
	case claim.Type == schema.WordBool:
		wrong = scalar.Quoted || !slices.Contains(game.Bools, text)
	case claim.Type == schema.WordDate:
		wrong = scalar.Quoted || jomini.Literal(text) != jomini.LitDate
	case len(claim.Words) > 0:
		code, wrong = "wrong-declared-value", true
	}
	if wrong && sess.Install.Schema.Speaks(schema.ClaimValue, at.Key, nil) {
		judge.report(scalar.Range, 2, code, fmt.Sprintf("%s is not allowed here; this key takes %s",
			text, strings.Join(takes(at.Key), ", ")))
	}
}

// references judges the value words the walk read at a position that nothing defines: an event, or
// a word a position declares names a database or a file; vanilla's contradiction keeps it quiet.
func (judge *judge) references(refs []index.Ref) {
	sess := judge.file.sess
	sch := sess.Install.Schema
	for _, ref := range refs {
		position, row := judge.written[ref.Range], sch.Databases[ref.Database]
		events := row != nil && row.Naming.Events
		written := strings.Trim(judge.file.text[ref.Range.Start:ref.Range.End], `"`)
		linked := row != nil && strings.HasSuffix(written, ":"+ref.Name) // a prefix names its game database (title:k_x)
		var code, message string
		switch {
		case events && (position == nil || position.Value.From != schema.SourceNone &&
			!sch.Speaks(schema.ClaimValue, position, nil)):
			continue
		case events:
			code, message = "unknown-event", "no event is named "+ref.Name
		case linked:
			code, message = "unresolved-ref", fmt.Sprintf("nothing defines %s %s", game.KindName(ref.Database), ref.Name)
		case position == nil || written != ref.Name:
			continue
		case !sch.Speaks(schema.ClaimValue, position, nil):
			continue
		case position.Value.Value.Type == schema.WordFile && ref.Database == index.File:
			code, message = "missing-file", ref.Name+" exists nowhere in the install or the mods"
		case position.Value.Value.Type == schema.WordReference && position.Value.Value.Database == ref.Database:
			code, message = "unresolved-ref", fmt.Sprintf("nothing defines %s %s", game.KindName(ref.Database), ref.Name)
		default:
			continue
		}
		_, known := index.Resolve(sch, sess.Indexes(), ref)
		if known || !linked && judge.contradicted(position, ref) {
			continue
		}
		judge.report(ref.Range, 2, code, message)
	}
}

// contradicted is whether vanilla itself writes, at the reference's position, a word of the same
// kind (for an event, an event-shaped word) that nothing defines: then a miss proves nothing.
func (judge *judge) contradicted(position *schema.Key, ref index.Ref) bool {
	sess := judge.file.sess
	row := sess.Install.Schema.Databases[ref.Database]
	for word := range position.Seen.Values {
		if row != nil && row.Naming.Events && !jomini.IsEventID(word) {
			continue
		}
		if _, found := index.Resolve(sess.Install.Schema, sess.Indexes(), index.Ref{Database: ref.Database, Name: word}); !found {
			return true
		}
	}
	return false
}

// plain is a scope as a sentence says it: a character, an army, a landed title.
func plain(scope string) string {
	words := strings.ReplaceAll(scope, "_", " ")
	if strings.ContainsRune("aeiou", rune(words[0])) {
		return "an " + words
	}
	return "a " + words
}

// definitions judges a file's loaded named definitions: an INJECT or REPLACE with nothing before
// it, and a plain name another mod file defines plainly where vanilla never repeats names and
// entries do not merge (on_actions, history); a file-local name or one an entry gives never counts.
func (judge *judge) definitions(defs []index.Def) {
	sess := judge.file.sess
	contends := func(def index.Def) bool { return def.Mode == "" && !def.Local && !def.Named }
	for _, def := range defs {
		row := sess.Install.Schema.Databases[def.Database]
		if def.Name == "" || row == nil {
			continue
		}
		loaded := index.Lookup(sess.Indexes(), def.Database, def.Name)
		if !slices.Contains(loaded, def) {
			continue
		}
		_, _, orphans, _ := sess.Winner(loaded)
		if slices.Contains(orphans, def) {
			judge.report(def.Range, 2, "inject-without-target",
				fmt.Sprintf("%s:%s does nothing: no %s is loaded before it", def.Mode, def.Name, def.Name))
			continue
		}
		if !contends(def) || row.Naming.OnActions || row.Naming.History || sess.Install.Index.Repeated[def.Database] {
			continue
		}
		if other := slices.IndexFunc(loaded, func(other index.Def) bool {
			listed, _, _ := sess.Located(other.Path)
			return contends(other) && other.Path != def.Path && listed.Origin != game.OriginVanilla
		}); other >= 0 {
			listed, _, _ := sess.Located(loaded[other].Path)
			judge.report(def.Range, 2, "duplicate-definition", fmt.Sprintf("%s is also defined in %s", def.Name, listed.Rel))
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
	for wrong, read := range game.MisspelledFolders {
		if strings.Contains(strings.ToLower(judge.file.place.Rel), wrong+"/") {
			judge.report(whole, 2, "wrong-localization-folder", "the game reads "+read+", not "+wrong)
		}
	}
	if !parsed.HadBOM {
		raw, err := os.ReadFile(judge.file.path)
		if err != nil || !bytes.HasPrefix(raw, []byte(jomini.BOM)) {
			judge.report(whole, 2, "missing-bom", "the game reads a localization file only with a UTF-8 byte order mark")
		}
	}
}
