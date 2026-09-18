// diagnostics.go publishes every diagnostic for a file: parse and loc errors,
// the passes over its harvested definitions and references, the rules judged
// at every cell of one walk down the learned tree, the descriptor check, and
// the `# pmt:ignore` suppressions. The code registry at the end is what the
// settings surface toggles and the sweep counts by.

package lsp

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

func diag(rg Range, sev int, msg, code string) Diagnostic {
	return Diagnostic{Range: rg, Severity: sev, Message: msg, Code: code}
}

// diagFile is one file gathered once for the whole diagnostic run.
type diagFile struct {
	s    *session.Session
	path string
	kind string
	// fileKind is the extract kind the learned tree files this file's cells under, which is not the same as kind ("script", "loc", "gui").
	fileKind string
	src      string
	res      jomini.Result
	li       *jomini.LineIndex
	refs     []index.Ref
}

// Diagnose runs every check. Install (vanilla) files are silent: harvest stays in the scan cache, not IDE lint. The caller applies the user's per-code choices afterward, so the vanilla gate always sees the full surface.
func Diagnose(s *session.Session, path string) []Diagnostic {
	if origin, _, ok := s.Locate(path); ok && origin == game.OriginVanilla {
		return nil
	}
	f := diagFile{s: s, path: path, kind: s.KindFor(path), src: s.FileText(path)}
	if _, rel, ok := s.Locate(path); ok {
		f.fileKind = game.MatchExtract(s.GameID, rel).Kind
	}
	f.refs = s.RefsInFile(path)
	if f.kind == "loc" {
		f.li = jomini.NewLineIndex(f.src)
	} else {
		f.res = s.Parsed(path)
		f.li = f.res.Lines()
	}
	var out []Diagnostic
	if f.kind == "loc" {
		out = append(out, f.locDiags()...)
	} else if !s.IsMetaFile(path) {
		for _, e := range f.res.Errors {
			out = append(out, diag(byteRange(f.li, e.Range.Start, e.Range.End), sevError, e.Message, string(e.Code)))
		}
	}
	out = append(out, f.refDiags()...)
	out = append(out, f.defDiags()...)
	out = append(out, f.cellDiags()...)
	out = append(out, descriptorDiags(s, path)...)
	sup := scanSuppressions(f.src)
	filtered := out[:0]
	for _, d := range out {
		if !sup.Covers(d.Range.Start.Line, d.Code) {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func (f diagFile) locDiags() []Diagnostic {
	path, src, li := f.path, f.src, f.li
	r := loc.Parse(src)
	out := make([]Diagnostic, 0, len(r.Errors)+2)
	for _, e := range r.Errors {
		out = append(out, diag(byteRange(li, e.Range.Start, e.Range.End), locSeverity(e.Code), e.Message, "loc-"+string(e.Code)))
	}
	if !locHasBOM(path, src) {
		out = append(out, diag(Range{}, sevError, "This localization file has no UTF-8 BOM; the game ignores it. Save as UTF-8 with BOM.", "missing-bom"))
	}
	fileLang := loc.LanguageFromFilename(path)
	if r.Language != "" && fileLang != "" && !strings.EqualFold(r.Language, fileLang) {
		rg := Range{}
		if r.HeaderRange != nil {
			rg = byteRange(li, r.HeaderRange.Start, r.HeaderRange.End)
		}
		out = append(out, diag(rg, sevError, "Header l_"+r.Language+": does not match filename _l_"+fileLang+".yml.", "loc-header-mismatch"))
	}
	if strings.Contains(strings.ReplaceAll(path, "\\", "/"), "/localisation/") {
		out = append(out, diag(Range{}, sevError, "Folder is localisation/ — the game reads localization/.", "wrong-localization-folder"))
	}
	return out
}

// locHasBOM reports a UTF-8 BOM in the buffer or on disk; the editor strips the BOM, so vanilla loc would false-positive otherwise.
func locHasBOM(path, src string) bool {
	if strings.HasPrefix(src, bom) {
		return true
	}
	raw, err := os.ReadFile(path)
	return err == nil && jomini.HasUTF8BOM(raw)
}

func locSeverity(code loc.ErrorCode) int {
	switch code {
	case loc.ErrNoHeader, loc.ErrContentBeforeHeader, loc.ErrUnterminatedValue:
		return sevError
	default:
		return sevWarning
	}
}

// refDiags is the one pass over the file's harvested references: a loc key nothing defines, and an event fired that no file defines.
func (f diagFile) refDiags() []Diagnostic {
	var out []Diagnostic
	for _, r := range f.refs {
		switch {
		case r.Kind == "loc":
			if !f.s.LocDefined(r.Key) {
				out = append(out, diag(byteRange(f.li, r.Start, r.End), sevWarning, "Missing localization key \""+r.Key+"\".", "missing-required-loc"))
			}
		case r.Kind == "event" && !r.Derived && f.src != "":
			if f.s.Resolve(r.Key) != nil {
				continue
			}
			ns, rest, ok := strings.Cut(r.Key, ".")
			if !ok || rest == "" || !modDeclaresNamespace(f.s, ns) {
				continue
			}
			out = append(out, diag(byteRange(f.li, r.Start, r.End), sevWarning, "Unknown event \""+r.Key+"\".", "unknown-event"))
		}
	}
	return out
}

func modDeclaresNamespace(s *session.Session, ns string) bool {
	if ns == "" {
		return false
	}
	return s.ResolveMatching(ns, func(d index.Def) bool { return jomini.CanonicalKind(d.Kind) == "namespace" }) != nil
}

// defDiags is the one pass over the file's own definitions: a key defined twice inside one origin, and the localization a kind's universal conventions name. One origin, not the workspace: a mod redefining a vanilla object is an override, which Workspace Health already reports; two definitions of one key inside a single mod is a defect load order decides.
func (f diagFile) defDiags() []Diagnostic {
	origin, _, ok := f.s.Locate(f.path)
	if !ok || f.src == "" {
		return nil
	}
	var out []Diagnostic
	for _, d := range f.s.DefsInFile(f.path) {
		out = append(out, f.requiredLoc(d)...)
		// Only a file's own subject: a definition harvested from inside another one is real but not the file's subject, and an inline declaration is resolved per file by the engine, so a repeat is required, not a collision.
		if !jomini.IsObjectKind(d.Kind) || d.Nested || d.Local {
			continue
		}
		for _, other := range f.s.ModDefs(d.Key) {
			if other.Origin != origin || other.Kind != d.Kind {
				continue
			}
			// Different files only: a key repeated inside ONE file is an authoring idiom. Report once, at the later site by path then offset, so the first definition is left clean.
			op, dp := index.CanonPath(other.Path), index.CanonPath(d.Path)
			if op == dp || op > dp || (op == dp && other.Start > d.Start) {
				continue
			}
			out = append(out, diag(byteRange(f.li, d.Start, d.End), sevWarning,
				d.Key+" is already defined in "+f.s.DisplayRel(other.Path)+"; load order decides which one the game uses.", "duplicate-definition"))
			break
		}
	}
	return out
}

// requiredLoc demands the keys a kind's universal conventions name; a coverage short of 1 is a habit.
func (f diagFile) requiredLoc(d index.Def) []Diagnostic {
	var required []loc.Convention
	for _, c := range f.s.LocConventions(d.Kind) {
		if c.Coverage >= 1 {
			required = append(required, c)
		}
	}
	var out []Diagnostic
	for _, key := range loc.Expand(d.Key, required) {
		if !f.s.LocDefined(key) {
			out = append(out, diag(byteRange(f.li, d.Start, d.End), sevWarning, "Missing localization key \""+key+"\".", "required-loc"))
		}
	}
	return out
}

func descriptorDiags(s *session.Session, path string) []Diagnostic {
	root := modRootOf(s, path)
	if root == "" {
		return nil
	}
	want := game.DescriptorPath(s.GameID, root)
	if fileExists(want) {
		return nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil
	}
	if strings.Contains(rel, string(filepath.Separator)) && filepath.Base(path) != filepath.Base(want) {
		return nil
	}
	return []Diagnostic{diag(Range{}, sevWarning, "Mod is missing "+game.DescriptorFile(s.GameID)+".", "missing-descriptor")}
}

// ------------------------------------------------------- the rules per cell

// rule is one diagnostic that reads the tree: judge sees every assignment with the walk in force at it and returns the span and message it reports.
type rule struct {
	code  string
	judge func(f diagFile, a *jomini.Assignment, w walk, block string, top bool) (jomini.Range, string, bool)
}

// cellRules is every kept code that reads the tree, in one table; cellDiags is the single walk that feeds them.
var cellRules = []rule{
	{"unresolved-ref", unresolvedRefAt},
	{"wrong-value-type", wrongValueTypeAt},
	{"unknown-key", unknownKeyAt},
	{"wrong-scope", wrongScopeAt},
}

func (f diagFile) cellDiags() []Diagnostic {
	var out []Diagnostic
	f.eachCell(func(a *jomini.Assignment, w walk, block string, top bool) {
		for _, r := range cellRules {
			if rng, msg, ok := r.judge(f, a, w, block, top); ok {
				out = append(out, diag(byteRange(f.li, rng.Start, rng.End), sevWarning, msg, r.code))
			}
		}
	})
	return out
}

// eachCell walks the file handing every assignment the walk in force at it: the position its key hangs from, the scope, the body class and the macro parameters. The definition's own row is transparent, so its members hang from the kind's root; block is the enclosing key as the author wrote it, which is what a message names, and top marks the definition's own key.
func (f diagFile) eachCell(fn func(a *jomini.Assignment, w walk, block string, top bool)) {
	if f.res.Root == nil {
		return
	}
	for _, st := range f.res.Root.Statements {
		w := rootWalk(f.s, f.fileKind, st)
		if a, ok := st.(*jomini.Assignment); ok {
			fn(a, w, "", true)
		}
		if b := jomini.ChildBlock(st); b != nil {
			f.walkBlock(b, w, "", fn)
		}
	}
}

func (f diagFile) walkBlock(b *jomini.Block, w walk, block string, fn func(a *jomini.Assignment, w walk, block string, top bool)) {
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if ok {
			fn(a, w, block, false)
		}
		child := jomini.ChildBlock(st)
		if child == nil {
			continue
		}
		key, quoted := "", false
		if ok {
			key, quoted = a.Key.Text, a.Key.Quoted
		}
		f.walkBlock(child, w.into(key, quoted), key, fn)
	}
}

// wrongScopeAt flags an effect or trigger used in a scope the game does not allow. It reports only where the game declares both the token and the scope, so a missing dump or an unknown transition produces silence rather than noise. An argument the enclosing token declares is not a statement in the scope that token produces; a mod macro sharing a name with an engine token wins.
func wrongScopeAt(f diagFile, a *jomini.Assignment, w walk, _ string, top bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	key := a.Key.Text
	lk := strings.ToLower(game.KeyIdentity("", key))
	if top || a.Key.Quoted || w.dead || w.body == 0 || w.cur() == "" || w.params[lk] || f.s.Work.Tree.Macros[key] != "" {
		return none, "", false
	}
	t, role, ok := f.s.Install.Declared.Token(lk)
	if !ok || w.body&roleBit(role) == 0 || declared.UsableIn(t.In, w.cur()) {
		return none, "", false
	}
	return a.Key.Range, key + " runs on " + strings.Join(t.In, " or ") + ", but this block is " + w.cur() + " scope.", true
}

// unresolvedRefAt flags `culture:nonexistent`: a citation whose database the game names in the text and whose key nothing defines. Narrower than Workspace Health's dangling check: a citation states its own database, and a bare name is judged only where the install's cell names one.
func unresolvedRefAt(f diagFile, a *jomini.Assignment, w walk, _ string, _ bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	text, rng, ok := scalarValue(a)
	if !ok {
		return none, "", false
	}
	// A dotted value is a scope chain, not a name. A value at a script-name key (`save_scope_as = x`) is a name the script invents, never a reference to judge.
	if _, named := jomini.ScriptName(a.Key.Text); named || strings.ContainsRune(text, '.') {
		return none, "", false
	}
	id, kind := text, ""
	if k, rest, typed := game.ParseTyped(f.s.GameID, text); typed {
		// A citation needs no cell: it is the one reference form where the game itself states both the database and the key.
		id, kind = rest, k
		if pk := f.s.Install.Tree.PrefixKind(strings.SplitN(text, ":", 2)[0]); pk != "" {
			kind = pk
		}
		// Only a prefix bound to a database PMT holds, and never an ephemeral kind: `flag:private_chamber` names a value the script invents as it runs.
		if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return none, "", false
		}
	} else {
		// The reporting policy, at the one value it is about: a bare name is judged only where the install's cell names a database for it and never wrote a literal or an unresolved name there itself.
		cell := f.s.Install.Tree.Child(w.hang, a.Key.Text, a.Key.Quoted, false, false)
		if cell == nil || len(cell.Kinds) == 0 || cell.Has(index.LabelUnresolved) || cell.Has(index.LabelLiteral) ||
			jomini.SkipObjectRHS(text) || isScopeWord(text) {
			return none, "", false
		}
		kind = cell.Kinds[0]
		if jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return none, "", false
		}
	}
	if f.defined(id, kind) {
		return none, "", false
	}
	return rng, "nothing named " + id + " is defined.", true
}

// defined reports a name the model holds under a kind. The miss has to be total: a nested definition is filed under a kind the binding does not name, and CK3's k_austria sits inside another title. A data tier that would not load is a model missing its rows, which is no evidence that this one is absent.
func (f diagFile) defined(id, kind string) bool {
	if len(f.s.FindDefsOf(id, kind, 1, false, true)) > 0 || len(f.s.FindDefsOf(id, "", 1, false, true)) > 0 {
		return true
	}
	return f.s.Install.DataError() != "" && game.IsDataKind(f.s.GameID, kind)
}

// wrongValueTypeAt flags a field value that is not of the type the game declares for that field. Declared only: a value contradicting `Supported Targets` is wrong however little else PMT knows, and the learned field types exist to offer completions and to name a hover, where being wrong costs a suggestion, not a false accusation.
func wrongValueTypeAt(f diagFile, a *jomini.Assignment, w walk, _ string, _ bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	if a.Key.Quoted || f.s.Install.Declared.Empty() {
		return none, "", false
	}
	text, rng, ok := scalarValue(a)
	if !ok || jomini.SkipObjectRHS(text) || isScopeWord(text) {
		return none, "", false
	}
	target := tokenTarget(f.s, a.Key.Text)
	if target == "" {
		return none, "", false
	}
	// The same reporting policy unresolved-ref follows: a cell the install itself wrote a literal or an unresolved name at accuses nothing.
	if cell := f.s.Install.Tree.Child(w.hang, a.Key.Text, a.Key.Quoted, false, false); cell != nil && (cell.Has(index.LabelUnresolved) || cell.Has(index.LabelLiteral)) {
		return none, "", false
	}
	cite, kind := f.s.Install.Tree.CiteForm(target)
	if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
		return none, "", false
	}
	// Any citation at all is left alone: stripping only the bound prefix read `has_title = title:k_austria` whole and reported a correct line.
	bare := text
	if i := strings.IndexByte(text, ':'); i > 0 {
		if cite == "" || text[:i] != cite {
			return none, "", false
		}
		bare = text[i+1:]
	}
	if f.defined(bare, kind) {
		return none, "", false
	}
	return rng, a.Key.Text + " takes a " + PrettyKind(kind) + "; " + text + " is not one.", true
}

// scalarValue returns an assignment's right-hand side when it is a bare scalar.
func scalarValue(a *jomini.Assignment) (string, jomini.Range, bool) {
	tok, ok := a.Value.(*jomini.Scalar)
	if !ok || tok.Quoted || tok.Text == "" {
		return "", jomini.Range{}, false
	}
	return tok.Text, tok.Range, true
}

// isScopeWord reports a value that names the current scope rather than a row in a database: the built-in keywords, and anything carrying a language prefix. `has_same_court_language = root` takes a character, and root IS a character; it just is not the id of one.
func isScopeWord(text string) bool {
	if _, ok := jomini.ParsePrefixed(text); ok {
		return true
	}
	switch strings.ToLower(text) {
	case "root", "prev", "this", "from", "fromfrom", "prevprev":
		return true
	}
	return false
}

// unknownKeyAt flags a key the block it sits in does not hold, the one check that reports an absence, so it judges only where the tree learned a shape and never a name the game or a macro declares. The one classifier decides what the key IS: only a plain field name may be named; a weight, a date or a citation is a position, a stop word is control flow, a declared token or a macro call is the engine's API, a row of a database is a name the author may invent, and a dot or an unbound colon is a scope path. Known asks whether the key exists AT ALL: a real key in the wrong block is not this check's business.
func unknownKeyAt(f diagFile, a *jomini.Assignment, w walk, block string, top bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	// The definition's own key sits in no block; its fields are judged one level down, against the kind's own structure.
	if top || a.Key.Quoted {
		return none, "", false
	}
	key := strings.ToLower(a.Key.Text)
	// The arguments of a call are the macro's parameters, not a structure's members; a macro whose parameters PMT did not record answers nothing at all.
	if block != "" && f.s.Work.Tree.Macros[block] != "" {
		params := f.s.Params(block)
		if len(params) == 0 || slices.ContainsFunc(params, func(p string) bool { return strings.EqualFold(p, key) }) {
			return none, "", false
		}
		return a.Key.Range, key + " is not a parameter of " + block + ".", true
	}
	if block == "" {
		block = f.fileKind
	}
	hang := w.hang
	if hang == nil || (hang.Class != index.Structure && hang.Class != index.Body) {
		return none, "", false
	}
	cls, _ := f.s.Install.Tree.KeyClass(a.Key.Text, false, hang.Flags&index.Rows != 0)
	if !cls.Nameable() || strings.ContainsAny(key, ".:") || isScopeWord(key) ||
		f.s.Install.Tree.Child(hang, a.Key.Text, false, false, false) != nil ||
		isMember(f.s, hang, a.Key.Text) || f.s.Install.Tree.KnownKey(key) || jomini.IsMacroKind(key) {
		return none, "", false
	}
	// A definition the engine invokes by name is a call wherever it is written: `num_unaligned_vassals > 5` compares a script value the mod wrote, and no block holds it as a field.
	if f.s.ResolveMatching(a.Key.Text, func(d index.Def) bool { return jomini.DeclaresNamedBodies(d.Kind) }) != nil {
		return none, "", false
	}
	return a.Key.Range, key + " is not a key of " + block + ".", true
}

// ------------------------------------------------------------ suppressions

var ignoreRe = regexp.MustCompile(`(?i)#\s*pmt:ignore(-next-line)?\b(.*)`)

type lineSuppression struct {
	all   bool
	codes map[string]bool
}

// Suppressions maps a 0-based line to what diagnostics are suppressed on it.
type Suppressions map[int]lineSuppression

func scanSuppressions(text string) Suppressions {
	sup := Suppressions{}
	if !strings.Contains(text, "pmt:ignore") {
		return sup
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for line, lineText := range strings.Split(text, "\n") {
		hash := strings.IndexByte(lineText, '#')
		if hash < 0 {
			continue
		}
		m := ignoreRe.FindStringSubmatch(lineText[hash:])
		if m == nil {
			continue
		}
		target := line
		if m[1] != "" {
			target = line + 1
		}
		var codes []string
		for tok := range strings.FieldsSeq(m[2]) {
			if strings.HasPrefix(tok, "-") {
				break
			}
			codes = append(codes, tok)
		}
		ls := sup[target]
		if len(codes) == 0 {
			ls.all = true
		} else if !ls.all {
			if ls.codes == nil {
				ls.codes = map[string]bool{}
			}
			for _, c := range codes {
				ls.codes[c] = true
			}
		}
		sup[target] = ls
	}
	return sup
}

func (s Suppressions) Covers(line int, code string) bool {
	ls, ok := s[line]
	return ok && (ls.all || (code != "" && ls.codes[code]))
}

// ------------------------------------------------------------ the registry

// Groups a code belongs to, for the settings surface: GroupParse is syntax the game itself cannot read, shown apart because silencing one hides a whole file.
const (
	GroupCheck = "check"
	GroupParse = "parse"
)

// SettingOn and SettingOff are the two stored values a code's setting may hold.
const (
	SettingOn  = "on"
	SettingOff = "off"
)

// DiagnosticCode describes one check for the settings surface; Default is whether it runs when the user has not chosen.
type DiagnosticCode struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Group   string `json:"group"`
	Default bool   `json:"default"`
}

// diagnosticCodes is the full list, ordered as the settings surface shows them.
var diagnosticCodes = []DiagnosticCode{
	{Code: "duplicate-definition", Default: true, Group: GroupCheck, Summary: "One key defined twice inside a single mod; load order decides which survives."},
	{Code: "loc-header-mismatch", Default: true, Group: GroupCheck, Summary: "A localization file's l_<lang>: header disagrees with its folder."},
	{Code: "missing-bom", Default: true, Group: GroupCheck, Summary: "A localization file without the UTF-8 BOM the games require."},
	{Code: "missing-descriptor", Default: true, Group: GroupCheck, Summary: "A mod with no descriptor.mod."},
	{Code: "missing-required-loc", Default: true, Group: GroupCheck, Summary: "A key the game will look for that nothing defines."},
	{Code: "required-loc", Default: true, Group: GroupCheck, Summary: "A definition missing localization its kind conventionally has."},
	{Code: "unknown-event", Default: true, Group: GroupCheck, Summary: "An event fired or called that no file defines."},
	{Code: "unknown-key", Default: true, Group: GroupCheck, Summary: "A key that is not a member of the block it sits in."},
	{Code: "unresolved-ref", Default: true, Group: GroupCheck, Summary: "A citation naming a database PMT holds, and a key nothing defines."},
	{Code: "wrong-localization-folder", Default: true, Group: GroupCheck, Summary: "A localization file under the wrong language folder."},
	{Code: "wrong-scope", Default: true, Group: GroupCheck, Summary: "An effect or trigger used in a scope the game does not allow."},
	{Code: "wrong-value-type", Default: true, Group: GroupCheck, Summary: "A value contradicting the argument type the game declares for that key."},
	{Code: "missing-value", Default: true, Group: GroupParse, Summary: "A key with `=` and nothing after it."},
	{Code: "stray-close", Default: true, Group: GroupParse, Summary: "A closing brace with no block open."},
	{Code: "unclosed-brace", Default: true, Group: GroupParse, Summary: "A block opened and never closed."},
	{Code: "unterminated-string", Default: true, Group: GroupParse, Summary: "A quoted value with no closing quote, recovered at end of line."},
	{Code: "loc-bad-entry", Default: true, Group: GroupParse, Summary: "A localization line that is not `key:0 \"value\"`."},
	{Code: "loc-content-before-header", Default: true, Group: GroupParse, Summary: "Localization entries above the l_<lang>: header, which the game ignores."},
	{Code: "loc-no-header", Default: true, Group: GroupParse, Summary: "A localization file with no l_<lang>: header."},
	{Code: "loc-tab-indent", Default: true, Group: GroupParse, Summary: "A localization line indented with a tab; the games want spaces."},
	{Code: "loc-unterminated-value", Default: true, Group: GroupParse, Summary: "A localization value with no closing quote."},
}

// DiagnosticCodes returns the registry, for the settings surface.
func DiagnosticCodes() []DiagnosticCode {
	return slices.Clone(diagnosticCodes)
}

// EnabledCodes resolves each code's on/off state: the stored setting, or the code's own default when the user has not chosen one.
func EnabledCodes(settings map[string]string) map[string]bool {
	out := make(map[string]bool, len(diagnosticCodes))
	for _, c := range diagnosticCodes {
		on := c.Default
		if v, ok := settings[c.Code]; ok && v != "" {
			on = v == SettingOn
		}
		out[c.Code] = on
	}
	return out
}

// ApplyEnabled drops diagnostics whose code the user has switched off.
func ApplyEnabled(ds []Diagnostic, enabled map[string]bool) []Diagnostic {
	out := ds[:0]
	for _, d := range ds {
		if on, ok := enabled[d.Code]; ok && !on {
			continue
		}
		out = append(out, d)
	}
	return out
}
