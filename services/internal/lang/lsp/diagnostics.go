// diagnostics.go publishes parse/loc/descriptor diagnostics and pmt:ignore.
package lsp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

func diag(rg Range, sev int, msg, code string) Diagnostic {
	return Diagnostic{Range: rg, Severity: sev, Message: msg, Code: code}
}

// diagFile is one file gathered once for the whole diagnostic run. Each pass
// used to fetch what it needed itself, so a single Diagnose read the text four
// times and re-parsed the file five times — and Parsed re-parses whenever the
// file is not an open buffer.
type diagFile struct {
	s    *session.Session
	path string
	kind string
	// fileKind is the extract kind the learned schema files this file's cells under, which is not the same as kind ("script", "loc", "gui").
	fileKind string
	src      string
	res      jomini.Result
	li       *jomini.LineIndex
	refs     []derived.Ref
}

// eachCell walks the file handing every assignment the cell it sits at: the file's extract kind, the enclosing block keys below the definition, and the key itself.
func (f diagFile) eachCell(fn func(a *jomini.Assignment, path string)) {
	if f.res.Root == nil {
		return
	}
	folded := []string{""}
	jomini.Walk(f.res.Root, func(st jomini.Statement, depth int, _ *jomini.Block) bool {
		for len(folded) <= depth+1 {
			folded = append(folded, "")
		}
		a, ok := st.(*jomini.Assignment)
		key := ""
		if ok {
			key = a.Key.Text
		}
		switch {
		case depth == 0:
			folded[1] = ""
		case folded[depth] == "":
			folded[depth+1] = f.s.CellPath([]string{key})
		default:
			folded[depth+1] = folded[depth] + "." + f.s.CellPath([]string{key})
		}
		if ok {
			fn(a, folded[depth])
		}
		return true
	})
}

// Diagnose runs every check. Suppressions (# pmt:ignore) are honored.
// Install (vanilla) files are silent — harvest stays in the scan cache, not IDE lint.
func Diagnose(s *session.Session, path string) []Diagnostic {
	return DiagnoseEnabled(s, path, nil)
}

// DiagnoseEnabled is Diagnose with the checks the user has switched off skipped
// rather than run and discarded.
//
// A nil or empty set means run everything, so the vanilla gate and the sweep get
// the full surface whatever a user has configured — a gate that only checks what
// happens to be enabled is not a gate.
func DiagnoseEnabled(
	s *session.Session, path string, enabled map[string]bool,
) []Diagnostic {
	if len(enabled) == 0 {
		enabled = nil
	}
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
		out = append(out, f.scriptDiags()...)
	}
	out = append(out, f.missingLocDiags()...)
	out = append(out, f.requiredLocDiags()...)
	out = append(out, f.unknownEventDiags()...)
	out = append(out, f.wrongScopeDiags()...)
	out = append(out, f.duplicateDefDiags()...)
	out = append(out, f.unresolvedRefDiags()...)
	out = append(out, f.wrongValueTypeDiags()...)
	out = append(out, f.unknownKeyDiags()...)
	if enabled == nil || enabled["wrong-declared-value"] {
		out = append(out, f.declaredValueDiags()...)
	}
	if enabled == nil || enabled["unknown-token"] {
		out = append(out, f.unknownTokenDiags()...)
	}
	out = append(out, descriptorDiags(s, path)...)

	sup := scanSuppressions(f.src)
	filtered := out[:0]
	for _, d := range out {
		if sup.Covers(d.Range.Start.Line, d.Code) {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

// wrongScopeDiags flags an effect or trigger used in a scope the game does not
// allow, for example a character-only effect inside a landed_title block. It
// reports only where the game declares both the token and the scope, so a
// missing or partial script_docs dump produces silence rather than noise.
func (f diagFile) wrongScopeDiags() []Diagnostic {
	misuses := ScopeMisuses(f.s, f.path)
	if len(misuses) == 0 {
		return nil
	}
	out := make([]Diagnostic, 0, len(misuses))
	for _, m := range misuses {
		msg := m.Key + " runs on " + strings.Join(m.Allowed, " or ") +
			", but this block is " + m.Scope + " scope."
		out = append(out, diag(byteRange(f.li, m.Start, m.End), sevWarning, msg, "wrong-scope"))
	}
	return out
}

func (f diagFile) scriptDiags() []Diagnostic {
	out := make([]Diagnostic, 0, len(f.res.Errors))
	for _, e := range f.res.Errors {
		out = append(out, diag(byteRange(f.li, e.Range.Start, e.Range.End),
			sevError, e.Message, string(e.Code)))
	}
	return out
}

func (f diagFile) locDiags() []Diagnostic {
	path, src, li := f.path, f.src, f.li
	r := loc.Parse(src)
	out := make([]Diagnostic, 0, len(r.Errors)+2)
	for _, e := range r.Errors {
		out = append(out, diag(byteRange(li, e.Range.Start, e.Range.End),
			locSeverity(e.Code), e.Message, "loc-"+string(e.Code)))
	}
	if !locHasBOM(path, src) {
		out = append(out, diag(Range{}, sevError,
			"This localization file has no UTF-8 BOM; the game ignores it. Save as UTF-8 with BOM.",
			"missing-bom"))
	}
	fileLang := loc.LanguageFromFilename(path)
	if r.Language != "" && fileLang != "" && !strings.EqualFold(r.Language, fileLang) {
		rg := Range{}
		if r.HeaderRange != nil {
			rg = byteRange(li, r.HeaderRange.Start, r.HeaderRange.End)
		}
		out = append(out, diag(rg, sevError,
			"Header l_"+r.Language+": does not match filename _l_"+fileLang+".yml.",
			"loc-header-mismatch"))
	}
	if strings.Contains(strings.ReplaceAll(path, "\\", "/"), "/localisation/") {
		out = append(out, diag(Range{}, sevError,
			"Folder is localisation/ — the game reads localization/.",
			"wrong-localization-folder"))
	}
	return out
}

// locHasBOM reports a UTF-8 BOM in the buffer or on disk. FileText/Monaco
// strip the BOM, so vanilla loc (all BOM on disk) would false-positive otherwise.
func locHasBOM(path, src string) bool {
	if strings.HasPrefix(src, "\uFEFF") {
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

// missingLocDiags reports a script field citing a loc key nothing defines.
func (f diagFile) missingLocDiags() []Diagnostic {
	var out []Diagnostic
	for _, r := range f.refs {
		if r.Kind != "loc" || f.s.LocDefined(r.Key) {
			continue
		}
		out = append(out, diag(byteRange(f.li, r.Start, r.End), sevWarning,
			"Missing localization key \""+r.Key+"\".", "missing-required-loc"))
	}
	return out
}

// requiredLocDiags demands the keys a kind's universal conventions name; a coverage short of 1 is a habit.
func (f diagFile) requiredLocDiags() []Diagnostic {
	if f.src == "" {
		return nil
	}
	var out []Diagnostic
	for _, d := range f.s.DefsInFile(f.path) {
		var required []loc.Convention
		for _, c := range f.s.LocConventionsOf(d.Kind) {
			if c.Coverage >= 1 {
				required = append(required, c)
			}
		}
		for _, key := range loc.Expand(d.Key, required) {
			if f.s.LocDefined(key) {
				continue
			}
			out = append(out, diag(byteRange(f.li, d.Start, d.End), sevWarning,
				"Missing localization key \""+key+"\".", "required-loc"))
		}
	}
	return out
}

func (f diagFile) unknownEventDiags() []Diagnostic {
	if f.src == "" {
		return nil
	}
	var out []Diagnostic
	for _, r := range f.refs {
		if r.Derived || r.Kind != "event" || f.s.Resolve(r.Key) != nil {
			continue
		}
		ns, rest, ok := strings.Cut(r.Key, ".")
		if !ok || rest == "" || !modDeclaresNamespace(f.s, ns) {
			continue
		}
		out = append(out, diag(byteRange(f.li, r.Start, r.End), sevWarning,
			"Unknown event \""+r.Key+"\".", "unknown-event"))
	}
	return out
}

func modDeclaresNamespace(s *session.Session, ns string) bool {
	if ns == "" {
		return false
	}
	return s.ResolveMatching(ns, func(d derived.Def) bool {
		return jomini.CanonicalKind(d.Kind) == "namespace"
	}) != nil
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
	msg := "Mod is missing " + game.DescriptorFile(s.GameID) + "."
	return []Diagnostic{diag(Range{}, sevWarning, msg, "missing-descriptor")}
}

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
		sup.scanLine(lineText, line)
	}
	return sup
}

func (s Suppressions) scanLine(lineText string, line int) {
	hash := strings.IndexByte(lineText, '#')
	if hash < 0 {
		return
	}
	m := ignoreRe.FindStringSubmatch(lineText[hash:])
	if m == nil {
		return
	}
	target := line
	if m[1] != "" {
		target = line + 1
	}
	s.merge(target, parseCodes(m[2]))
}

func (s Suppressions) Covers(line int, code string) bool {
	ls, ok := s[line]
	if !ok {
		return false
	}
	return ls.all || (code != "" && ls.codes[code])
}

func (s Suppressions) merge(line int, codes []string) {
	ls := s[line]
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
	s[line] = ls
}

func parseCodes(rest string) []string {
	var out []string
	for tok := range strings.FieldsSeq(rest) {
		if strings.HasPrefix(tok, "-") {
			break
		}
		out = append(out, tok)
	}
	return out
}
