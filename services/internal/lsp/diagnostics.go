// diagnostics.go is what is wrong with a file: one method per code. A code accuses only on a claim a declared
// source states, fresh, that no vanilla evidence contradicts (schema.MayAccuse); the install's own files are
// never judged. A code is turned off in the settings, or for a line and the next by `# pmt:ignore code`.

package lsp

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
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

// Codes is every check a user may turn off. The parsers' errors are not among them: a file that does not parse
// is wrong.
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

// judge gathers the findings of one file.
type judge struct {
	file      *openFile
	enabled   map[string]bool
	ignored   map[int][]string                  // by 0-based line: the codes a pmt:ignore comment turns off there; nil for all
	positions map[jomini.Range]*schema.Position // the position each value sits at, by its span
	out       []Diagnostic
}

// Diagnose is the findings of a mod's script or localization file, with the codes that are on; nil enabled
// means each code's default. The install's files and other formats have none.
func Diagnose(ws *workspace.Session, path string, enabled map[string]bool) ([]Diagnostic, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	isMod := file.place.Origin != "" && file.place.Origin != game.OriginVanilla
	if !isMod || file.place.Format != inventory.Script && file.place.Format != inventory.Loc {
		return nil, nil
	}
	if enabled == nil {
		enabled = map[string]bool{}
		for _, code := range Codes {
			enabled[code.Code] = code.Default
		}
	}
	judge := &judge{file: file, enabled: enabled, ignored: ignores(file.text), positions: map[jomini.Range]*schema.Position{}}
	if file.place.Format == inventory.Loc {
		judge.locFile()
	} else {
		judge.script()
	}
	return judge.out, nil
}

// report keeps a finding whose code is on (a code not in Codes, a parser's, is always on) and not turned off on
// its line by a pmt:ignore comment.
func (judge *judge) report(span jomini.Range, severity int, code, message string) {
	isCheck := slices.ContainsFunc(Codes, func(known Code) bool { return known.Code == code })
	if isCheck && !judge.enabled[code] {
		return
	}
	codes, isIgnored := judge.ignored[judge.file.parsed.Lines().PositionAt(span.Start).Line]
	if isIgnored && (len(codes) == 0 || slices.Contains(codes, code)) {
		return
	}
	judge.out = append(judge.out, Diagnostic{Range: judge.file.span(span), Severity: severity, Code: code, Message: message})
}

// ignores reads every `# pmt:ignore code, code` comment of a text: its codes, split on commas and blanks, apply
// to its own line and the line below, unless that line has a comment of its own. No codes turn every check off.
func ignores(text string) map[int][]string {
	ignored := map[int][]string{}
	for number, line := range strings.Split(text, "\n") {
		at := strings.Index(line, "pmt:ignore")
		if at < 0 || !strings.Contains(line[:at], "#") {
			continue
		}
		codes := strings.FieldsFunc(line[at+len("pmt:ignore"):], func(char rune) bool { return strings.ContainsRune(", \t\r", char) })
		ignored[number], ignored[number+1] = codes, codes
	}
	return ignored
}

// script judges a script file: the parser's errors, each statement the walk reads, then the names it refers to
// and defines.
func (judge *judge) script() {
	file := judge.file
	for _, parseError := range file.parsed.Errors {
		judge.report(parseError.Range, 1, string(parseError.Code), parseError.Message)
	}
	occurrences, _ := index.Collect(file.ws.Install.Schema, file.place.File, file.parsed,
		func(statement jomini.Statement, context schema.Context, inner *schema.Context, _ []schema.Occurrence) bool {
			judge.statement(step{statement, context, *inner})
			return true
		})
	judge.unresolved(occurrences)
	judge.definitions(occurrences)
}

// statement judges what one statement writes: its key by the scope it runs in and the variable it reads, its
// block by the keys it leaves out, its value by the scope, the variable and the type it reads.
func (judge *judge) statement(at step) {
	if assign, isAssign := at.statement.(*jomini.Assignment); isAssign {
		judge.variableRead(at, &assign.Key)
		judge.wrongScope(at, assign)
		judge.missingKey(at, assign)
	}
	if scalar, isScalar := jomini.ValueOf(at.statement).(*jomini.Scalar); isScalar {
		judge.variableRead(at, scalar)
		judge.wrongValue(at, scalar)
	}
}

// variableRead finds a variable read in a scope that stores none (wrong-scope: a command written in a scope it
// does not run in, or a variable read in one that stores none).
func (judge *judge) variableRead(at step, scalar *jomini.Scalar) {
	ws := judge.file.ws
	s, scope := ws.Install.Schema, at.context.Scope
	scopeType, declared := s.Scopes[scope.This]
	prefixed, isPrefixed := jomini.SplitPrefixed(scalar.Text)
	link := s.Link(strings.ToLower(scalar.Text))
	database := s.Prefixes[strings.ToLower(prefixed.Prefix)]
	readsVariable := isPrefixed && link != nil && !link.Global && database != "" && s.Databases[database] == nil
	storesNone := declared && !scopeType.Variables
	if !storesNone || !readsVariable || ws.Install.Index.WritesInScope(scalar.Text, scope.This) ||
		!s.MayAccuse(schema.CheckCommand, at.inner.Position(), nil) {
		return
	}
	message := fmt.Sprintf("%s reads a variable, but %s stores none (%s)", scalar.Text, scopeText(scope.This), scope.Reason)
	judge.report(scalar.Range, 2, "wrong-scope", message)
}

// wrongScope finds a command written in a scope it does not run in (wrong-scope: a command written in a scope it
// does not run in, or a variable read in one that stores none).
func (judge *judge) wrongScope(at step, assign *jomini.Assignment) {
	ws := judge.file.ws
	s, scope := ws.Install.Schema, at.context.Scope
	command := at.command(s)
	if command == nil || len(command.Scopes) == 0 || scope.This == "" || slices.Contains(command.Scopes, scope.This) ||
		ws.Install.Index.WritesInScope(assign.Key.Text, scope.This) || !s.MayAccuse(schema.CheckCommand, at.inner.Position(), nil) {
		return
	}
	var runsOn []string
	for _, name := range command.Scopes {
		runsOn = append(runsOn, scopeText(name))
	}
	message := fmt.Sprintf("%s runs on %s, but this block is %s (%s)",
		assign.Key.Text, strings.Join(runsOn, " or "), scopeText(scope.This), scope.Reason)
	judge.report(assign.Key.Range, 2, "wrong-scope", message)
}

// missingKey finds the keys of an entry's block that the game's docs say every such block writes and this one
// leaves out (missing-key: a key the game's docs say every entry writes, left out). A key with a mode word adds
// to another entry and may leave keys out.
func (judge *judge) missingKey(at step, assign *jomini.Assignment) {
	ws := judge.file.ws
	s, position, block := ws.Install.Schema, at.inner.Position(), jomini.BlockOf(assign.Value)
	if mode, _ := ws.Game.Mode(assign.Key.Text); block == nil || position == nil || mode != "" {
		return
	}
	writes := map[string]bool{}
	for _, statement := range block.Statements {
		if child, isAssign := statement.(*jomini.Assignment); isAssign {
			writes[child.Key.Text] = true
		}
	}
	for name, child := range position.Children {
		if !writes[name] && s.MayAccuse(schema.CheckRequired, child, position) {
			message := fmt.Sprintf("%s leaves out %s, which the game's docs say it must write", assign.Key.Text, name)
			judge.report(assign.Key.Range, 2, "missing-key", message)
		}
	}
}

// wrongValue finds a value of another type than its key takes, or a word outside the words it takes
// (wrong-value-type: a value of another type than the key takes; wrong-declared-value: a word outside the words
// the key takes). It notes the position each value sits at, which unresolved reads.
func (judge *judge) wrongValue(at step, scalar *jomini.Scalar) {
	ws := judge.file.ws
	s, text := ws.Install.Schema, scalar.Text
	position := schema.ValuePosition(at.statement, at.context, at.inner)
	if position == nil {
		return
	}
	judge.positions[scalar.Range] = position
	_, isConstant := jomini.Constant(text)
	claim := position.Value.Value
	if _, isChoice := slices.BinarySearch(claim.Choices, text); isConstant || len(jomini.Interpolations(text)) > 0 || isChoice {
		return
	}
	code, wrong := "wrong-value-type", false
	switch {
	case claim.Type == schema.ValueNumber:
		last := text[strings.LastIndex(text, ".")+1:]
		_, isValue := index.Resolve(s, ws.Indexes(), schema.Occurrence{Caller: game.Shapes[game.ShapeValue].Caller, ID: last}, judge.file.place.Path)
		namesNumber := isValue || s.Commands[strings.ToLower(last)] != nil || strings.Contains(text, ":")
		wrong = (scalar.Quoted || jomini.Literal(text) != jomini.LitNumber) && !namesNumber
	case claim.Type == schema.ValueBool:
		wrong = scalar.Quoted || !slices.Contains(game.Bools, text)
	case claim.Type == schema.ValueDate:
		wrong = scalar.Quoted || jomini.Literal(text) != jomini.LitDate
	case len(claim.Choices) > 0:
		code, wrong = "wrong-declared-value", true
	}
	if wrong && s.MayAccuse(schema.CheckValue, position, nil) {
		message := fmt.Sprintf("%s is not allowed here; this key takes %s", text, strings.Join(takesText(position), ", "))
		judge.report(scalar.Range, 2, code, message)
	}
}

// unresolved finds the names the file refers to that nothing defines: an event id no event defines
// (unknown-event), a word naming an entry nothing defines (unresolved-ref) and a path that exists nowhere
// (missing-file). Only a name written as the value of a key a declared source types is judged.
func (judge *judge) unresolved(occurrences []schema.Occurrence) {
	ws := judge.file.ws
	s := ws.Install.Schema
	for _, ref := range occurrences {
		if ref.Role != schema.Refers {
			continue
		}
		position, row := judge.positions[ref.Range], s.Databases[ref.Database]
		text := strings.Trim(judge.file.text[ref.Range.Start:ref.Range.End], `"`)
		linked := row != nil && strings.HasSuffix(text, ":"+ref.ID)
		typed := position != nil && s.MayAccuse(schema.CheckValue, position, nil)
		isEvent := s.Events != "" && ref.Database == s.Events
		var claim schema.Expected
		if position != nil {
			claim = position.Value.Value
		}
		unresolvedRef := fmt.Sprintf("nothing defines %s %s", game.KindName(ref.Database), ref.ID)
		code, message := "", ""
		switch {
		case isEvent && typed:
			code, message = "unknown-event", "no event is named "+ref.ID
		case isEvent:
		case linked:
			code, message = "unresolved-ref", unresolvedRef
		case typed && text == ref.ID && claim.Type == schema.ValueFile && ref.Database == schema.File:
			code, message = "missing-file", ref.ID+" exists nowhere in the install or the mods"
		case typed && text == ref.ID && claim.Type == schema.ValueReference && claim.Database == ref.Database:
			code, message = "unresolved-ref", unresolvedRef
		}
		if _, known := index.Resolve(s, ws.Indexes(), ref, judge.file.place.Path); code != "" && !known && (linked || !judge.contradicted(position, ref)) {
			judge.report(ref.Range, 2, code, message)
		}
	}
}

// contradicted reports whether vanilla itself writes, at the position, a word of the same kind that the
// workspace does not know (an event-shaped word, for an events database): a miss then proves nothing.
func (judge *judge) contradicted(position *schema.Position, ref schema.Occurrence) bool {
	if position == nil {
		return false
	}
	ws := judge.file.ws
	for word := range position.Vanilla.Values {
		sameKind := ref.Database != ws.Install.Schema.Events || jomini.IsEventID(word)
		_, known := index.Resolve(ws.Install.Schema, ws.Indexes(), schema.Occurrence{Database: ref.Database, ID: word}, judge.file.place.Path)
		if sameKind && !known {
			return true
		}
	}
	return false
}

// definitions finds the names this file defines that the game does not load as written: an INJECT or REPLACE with
// no target, else a name defined twice.
func (judge *judge) definitions(occurrences []schema.Occurrence) {
	ws := judge.file.ws
	for _, def := range occurrences {
		row := ws.Install.Schema.Databases[def.Database]
		if def.Role != schema.Defines || def.ID == "" || row == nil {
			continue
		}
		loaded := index.Lookup(ws.Indexes(), schema.Defines, def.Database, def.ID)
		mine := slices.IndexFunc(loaded, func(at index.At) bool {
			return at.File().Path == judge.file.place.Path && at.Occurrence().Range == def.Range
		})
		if mine >= 0 && !judge.orphanMode(def, loaded, mine) {
			judge.duplicate(def, row, loaded)
		}
	}
}

// orphanMode finds an INJECT or REPLACE with nothing loaded before it (inject-without-target: an INJECT or
// REPLACE with nothing loaded before it), and reports whether it fired.
func (judge *judge) orphanMode(def schema.Occurrence, loaded []index.At, mine int) bool {
	_, _, orphans, _ := judge.file.ws.Winner(loaded)
	if !slices.Contains(orphans, loaded[mine]) {
		return false
	}
	message := fmt.Sprintf("%s:%s does nothing: no %s is loaded before it", def.Mode, def.ID, def.ID)
	judge.report(def.Range, 2, "inject-without-target", message)
	return true
}

// duplicate finds a name another mod's file defines too (duplicate-definition: a name the workspace defines
// twice). The game's own files, on_actions, history and the names vanilla itself repeats are not judged.
func (judge *judge) duplicate(def schema.Occurrence, row *schema.Database, loaded []index.At) {
	ws := judge.file.ws
	contends := func(occurrence schema.Occurrence) bool {
		return occurrence.Mode == "" && !occurrence.Local && !occurrence.IsName
	}
	purpose := row.Descriptor.Purpose
	if !contends(def) || purpose == game.PurposeOnActions || purpose == game.PurposeHistory || ws.Install.Index.Repeated(def.Database) {
		return
	}
	for _, other := range loaded {
		if contends(other.Occurrence()) && other.File().Path != judge.file.place.Path && other.File().Origin != game.OriginVanilla {
			judge.report(def.Range, 2, "duplicate-definition", fmt.Sprintf("%s is also defined in %s", def.ID, other.File().Rel))
			return
		}
	}
}

// locFile judges a localization file: the parser's errors, a header naming another language than the file, a
// folder spelled as the game never reads it, and a missing UTF-8 byte order mark (missing-bom, loc-header-mismatch,
// wrong-localization-folder).
func (judge *judge) locFile() {
	file := judge.file
	parsed := loc.Parse(file.text)
	for _, parseError := range parsed.Errors {
		judge.report(parseError.Range, 1, "loc-"+string(parseError.Code), parseError.Message)
	}
	language := loc.LanguageFromFilename(file.place.Rel)
	if language != "" && parsed.Language != "" && !strings.EqualFold(language, parsed.Language) {
		message := fmt.Sprintf("the file is named for %s but its header says l_%s", language, parsed.Language)
		judge.report(jomini.Range{}, 2, "loc-header-mismatch", message)
	}
	rel := strings.ToLower(file.place.Rel)
	for wrong, right := range game.MisspelledFolders {
		if strings.HasPrefix(rel, wrong+"/") || strings.Contains(rel, "/"+wrong+"/") {
			judge.report(jomini.Range{}, 2, "wrong-localization-folder", fmt.Sprintf("the game reads %s, not %s", right, wrong))
		}
	}
	raw, err := os.ReadFile(file.path)
	if !strings.HasPrefix(file.text, jomini.BOM) && (err != nil || !bytes.HasPrefix(raw, []byte(jomini.BOM))) {
		judge.report(jomini.Range{}, 2, "missing-bom", "the game reads a localization file only with a UTF-8 byte order mark")
	}
}
