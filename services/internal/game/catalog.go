// catalog.go — game-specific logic for internal/catalog.
//
// Scope: reading the documentation the games ship beside their script. When a
// game update changes how it documents a field, this file breaks and no other.
//
// Everything here is DATA a function loops over. There is no table that merely
// describes: editing a row changes what PMT does.
//
//	DocShapes        the documentation formats  → ParseDocFile loops it
//	DeclKeywords     the declaration keywords   → DeclKeywordKind, DeclIsLocal
//	LocalDeclPrefix  the file-local keyword family → DeclIsLocal
//	IsDocFile        which files are documentation at all
//	LeadingKindProse a file's opening comment, describing its kind
//
// Every row names the games that write it, on a Games field or a GAME: line.
// catalog_test.go checks those names against the installs, so they cannot drift.
// Evidence and measurements live in GAME-SYNTAX.md, not here.

package game

import (
	"path/filepath"
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/parser/jomini"
)

// ---------------------------------------------------------------------------
// Documentation files
// ---------------------------------------------------------------------------

// IsDocFile reports a file that documents script rather than being script.
//
// GAME: all three, three naming conventions.
//
//	ck3   _<kind>.info      162 of 166 underscored
//	eu5   readme.txt, <kind>.info   23 of 32 .info NOT underscored
//	vic3  <kind>.md
//
// Underscored DATA (`_default.txt`, `_hardcoded.txt`) is not documentation and
// stays unclassified.
func IsDocFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".md") {
		return true
	}
	if strings.HasSuffix(lower, ".info") {
		return true
	}
	stem := strings.Trim(strings.TrimSuffix(lower, filepath.Ext(lower)), "_")
	return stem == "readme" || stem == "info"
}

// ---------------------------------------------------------------------------
// Documentation shapes
// ---------------------------------------------------------------------------

// DocShape is one way a game documents a field, and how to read it.
//
// ParseDocFile is a loop over this table: to support a new shape, add a row.
// Exactly one of Line or Value is set, or neither:
//
//	Line   the shape owns a whole line and names its own key
//	Value  the shape reads the right-hand side of `key = value`
//	neither  POSITIONAL — the prose is not on the key's line at all, so the
//	         parser's frame handles it and the row is here to be found
//
// Order matters. Line shapes are tried before `key = value` is considered;
// Value shapes are tried in order and the first to answer wins.
type DocShape struct {
	Summary string   // names the shape
	Games   []string // games that write it, verified by TestDocShapeAttribution
	Where   string   // which files, with file counts as measured

	// Line reads a whole line: `# - build_time: <integer> days`.
	Line func(body string) (key, doc, typ string, ok bool)
	// Value reads the right of `key = `: `<yes/no> ends the relationship`.
	Value func(rhs string) (doc, typ string, ok bool)
}

// DocShapes is every field-documentation format the three games ship, in the
// order ParseDocFile tries them.
var DocShapes = []DocShape{
	{
		Summary: "`### brief: name ( type )` header, prose, then a lone `#`",
		Games:   []string{"ck3"},
		Where:   "_*.info — 206 headers",
		Line: func(body string) (key, doc, typ string, ok bool) {
			m := briefHeadRe.FindStringSubmatch(body)
			if m == nil {
				return "", "", "", false
			}
			// Names the key that FOLLOWS and its type; the prose is the lines
			// after it, which the frame is already accumulating.
			return m[1], "", m[2], true
		},
	},
	{
		Summary: "`# - key: <type> desc` attribute bullet",
		Games:   []string{"ck3", "eu5"},
		Where:   "EU5 readme.txt (13 files); CK3 rarely",
		Line: func(body string) (key, doc, typ string, ok bool) {
			m := attrBulletRe.FindStringSubmatch(body)
			if m == nil {
				return "", "", "", false
			}
			typ, _ = splitAngle(m[2])
			return m[1], attrProse(m[2]), typ, true
		},
	},
	{
		Summary: "comment TRAILING the key, same line",
		Games:   []string{"ck3", "vic3", "eu5"},
		Where:   "every doc file kind — ~61 CK3 files, 30 Vic3, 42 EU5",
		Value: func(rhs string) (doc, typ string, ok bool) {
			h := strings.IndexByte(rhs, '#')
			if h < 0 {
				return "", "", false
			}
			// A type may precede the hash: `x = <yes/no> # why`.
			typ, _ = splitAngle(rhs)
			return strings.TrimSpace(strings.TrimLeft(rhs[h+1:], "# ")), typ, true
		},
	},
	{
		Summary: "`key = <type> desc`, no hash between type and prose",
		Games:   []string{"ck3", "vic3", "eu5"},
		Where:   "EU5 readme.txt (42 files); CK3 15, Vic3 12",
		Value: func(rhs string) (doc, typ string, ok bool) {
			typ, rest := splitAngle(rhs)
			if typ == "" {
				return "", "", false
			}
			return rest, typ, true
		},
	},
	{
		Summary: "comment ABOVE the key",
		Games:   []string{"ck3"},
		Where:   "_*.info",
		// POSITIONAL: the prose is on the preceding lines, which the frame
		// accumulates and attaches when no Value shape answers.
	},
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

var (
	// `key = value`, the spine every shape hangs off.
	docKeyRe = regexp.MustCompile(`^(\s*)([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$`)
	// `- key: rest`, the attribute-bullet shape.
	attrBulletRe = regexp.MustCompile(`^-\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::|=)\s*(.*)$`)
	// `<type> rest`, the angle-bracket shape.
	angleTypeRe = regexp.MustCompile(`^<([^<>]{1,120})>\s*(.*)$`)
	// `brief: name ( type )`, after PeelComment has taken the hashes off.
	briefHeadRe = regexp.MustCompile(`^brief:\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(([^)]*)\))?\s*$`)
)

// ParseDocFile reads one documentation file.
//
// GAME: all three. It handles every shape in DocShapes and asks which game it is
// reading nowhere — that is what lets a game add a sixth shape without a branch.
//
// Returns two maps keyed by field name:
//
//	docs   the prose describing the field
//	types  what the game says it HOLDS, verbatim: "yes/no", "script value",
//	       "bool", "database key, optional". Raw on purpose;
//	       catalog.ClassifyDeclaredType decides what the words mean, in one place.
func ParseDocFile(text string) (docs, types map[string]string) {
	out := map[string]string{}
	types = map[string]string{}
	putType := func(key, raw string) {
		key = strings.ToLower(key)
		raw = strings.TrimSpace(raw)
		if raw == "" || !jomini.IsLowerName(key) || jomini.IsStop(key) {
			return
		}
		if _, seen := types[key]; !seen {
			types[key] = raw
		}
	}
	var pending []string // comment lines waiting for the key they describe
	lastKey := ""        // the key a closing brace should attach prose to

	for _, raw := range strings.Split(text, "\n") {
		body, hashed := jomini.PeelComment(raw)

		if body == "" {
			// A lone `#` separates paragraphs INSIDE a comment block; only a
			// genuinely blank line ends one. CK3's brief shape depends on this.
			if !hashed {
				pending = pending[:0]
			}
			continue
		}

		// A Line shape owns the whole line and names its own key.
		if key, doc, typ, ok := lineShape(body); ok {
			putType(key, typ)
			if doc != "" {
				putFieldDoc(out, key, doc)
			}
			pending, lastKey = pending[:0], ""
			continue
		}

		if m := docKeyRe.FindStringSubmatch(body); m != nil {
			key := strings.ToLower(m[2])
			// A Value shape reads the right-hand side. When none answers, the
			// prose is the comment lines above — the positional shape.
			doc, typ := valueShape(m[3])
			putType(key, typ)
			putFieldDoc(out, key, docProse(append(pending, doc)))
			if jomini.IsLowerName(key) && !jomini.IsStop(key) {
				lastKey = key
			} else {
				lastKey = ""
			}
			pending = pending[:0]
			continue
		}

		if strings.HasPrefix(body, "}") {
			// `key = { # desc }` puts the description below the brace.
			if lastKey != "" {
				appendFieldDoc(out, lastKey, docProse(pending))
			}
			pending = pending[:0]
			continue
		}

		if hashed {
			pending = append(pending, body)
			continue
		}
		pending = pending[:0]
	}
	return out, types
}

// lineShape asks each DocShapes row that owns a whole line, in table order.
func lineShape(body string) (key, doc, typ string, ok bool) {
	for _, s := range DocShapes {
		if s.Line == nil {
			continue
		}
		if key, doc, typ, ok = s.Line(body); ok {
			return key, doc, typ, true
		}
	}
	return "", "", "", false
}

// valueShape asks each DocShapes row that reads a right-hand side, in table
// order; the first to answer wins. Empty doc means no shape matched and the
// prose, if any, is on the lines above.
func valueShape(rhs string) (doc, typ string) {
	for _, s := range DocShapes {
		if s.Value == nil {
			continue
		}
		if doc, typ, ok := s.Value(rhs); ok {
			return doc, typ
		}
	}
	return "", ""
}

// splitAngle splits `<type> prose` into its two halves, or returns empty when
// the value does not open with a type.
func splitAngle(rhs string) (typ, rest string) {
	if a := angleTypeRe.FindStringSubmatch(strings.TrimSpace(rhs)); a != nil {
		return strings.TrimSpace(a[1]), strings.TrimSpace(a[2])
	}
	return "", ""
}

// LeadingKindProse is a documentation file's opening comment block, which
// describes the kind the file documents rather than any one field.
//
// GAME: all three. Stops at the first key, bullet, or blank line, and at four
// lines.
func LeadingKindProse(text string) string {
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		body, hashed := jomini.PeelComment(raw)
		if body == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		if !hashed {
			break
		}
		if docKeyRe.MatchString(body) || attrBulletRe.MatchString(body) {
			break
		}
		lines = append(lines, body)
		if len(lines) >= 4 {
			break
		}
	}
	return docAuthorProse(lines)
}

// attrProse is the description after `- key: <type> …` or `= { … }: …`.
func attrProse(rhs string) string {
	rhs = strings.TrimSpace(rhs)
	if i := strings.Index(rhs, "}:"); i >= 0 {
		return strings.TrimSpace(rhs[i+2:])
	}
	if h := strings.IndexByte(rhs, '#'); h >= 0 {
		return strings.TrimSpace(rhs[h+1:])
	}
	if strings.HasPrefix(rhs, "{") {
		return ""
	}
	return rhs
}

// putFieldDoc records a field's prose, first writer winning, skipping keys that
// are script grammar rather than field names.
func putFieldDoc(out map[string]string, key, doc string) {
	key = strings.ToLower(key)
	if !jomini.IsLowerName(key) || jomini.IsStop(key) {
		return
	}
	if _, seen := out[key]; !seen || (out[key] == "" && doc != "") {
		out[key] = doc
	}
}

// appendFieldDoc adds below-the-brace prose to what a field already has.
func appendFieldDoc(out map[string]string, key, extra string) {
	if extra == "" || key == "" {
		return
	}
	if out[key] == "" {
		out[key] = extra
		return
	}
	out[key] = docProse([]string{out[key], extra})
}

// docProse joins comment fragments into one line, reflowing them.
func docProse(parts []string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.Join(parts, " ")), " "))
}

// docAuthorProse joins comment lines KEEPING the author's line breaks. The games
// console-wrap their own dump prose, which docProse reflows; a comment a modder
// typed is not wrapped and must not be.
func docAuthorProse(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.Join(strings.Fields(p), " "); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------------------
// Declaration keywords — BEHAVIOUR
// ---------------------------------------------------------------------------

// DeclKeyword is one `<keyword> <name> = { … }` form.
//
// ---------------------------------------------------------------------------
// script_docs dump layouts
// ---------------------------------------------------------------------------

// dumpTokenRe is the leading identifier of a block header. A local copy rather
// than a shared one: the dump grammar is this file's business.
var dumpTokenRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)`)

// DocBlock is one entry of a script_docs dump: a declared name and the lines
// describing it.
type DocBlock struct {
	Name  string
	Lines []string
}

// DumpFormat is one layout a game writes its script_docs dumps in.
//
// SplitDumpBlocks is a loop over this table: to support a new layout, add a row.
// Detect decides by LOOKING at the file rather than by asking which game wrote
// it, so a game changing layout is followed automatically and a game shipping
// two layouts is fine.
type DumpFormat struct {
	Name  string   // names the layout
	Games []string // games that write it
	// Detect reports that text uses this layout. Rows are tried in order and
	// the first to answer wins, so Detect may be cheap and approximate.
	Detect func(text string) bool
	// Split carves text into entries.
	Split func(text string) []DocBlock
}

// DumpFormats is every script_docs layout the three games write, in the order
// SplitDumpBlocks tries them.
var DumpFormats = []DumpFormat{
	{
		Name:  "markdown headings",
		Games: []string{"vic3", "eu5"},
		Detect: func(text string) bool {
			for _, line := range strings.Split(text, "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") {
					return true
				}
				if strings.HasPrefix(t, "----") {
					return false // a classic rule came first
				}
			}
			return false
		},
		Split: splitMarkdownDump,
	},
	{
		Name:  "---- separated",
		Games: []string{"ck3"},
		// Before the bare form, and that ORDER IS LOAD-BEARING: a classic file's
		// body lines carry `Label: value`, which the bare header matcher also
		// accepts, so testing bare first carves CK3's effect dumps into one block
		// per body line. It cost five unknown-key findings on vanilla.
		Detect: func(text string) bool { return strings.Contains(text, "\n----") },
		Split:  splitClassicDump,
	},
	{
		Name:  `bare "name:" headers`,
		Games: []string{"ck3", "vic3"},
		// event_scopes.log (CK3 only) and modifiers.log. Vic3 changed from an
		// indented `key: name` to an unindented `name:` between versions; CK3
		// writes `Tag: name`. All three shapes are one format.
		//
		// Last row: the fallback when no other layout claims the file.
		Detect: func(string) bool { return true },
		Split:  splitBareDump,
	},
}

// SplitDumpBlocks carves a script_docs dump into entries, using the first layout
// in DumpFormats that recognises it.
func SplitDumpBlocks(text string) []DocBlock {
	// Normalise first: the dumps are written on Windows and every Detect and
	// Split below reasons about lines.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, f := range DumpFormats {
		if f.Detect(text) {
			return f.Split(text)
		}
	}
	return nil
}

// splitMarkdownDump reads `## name` / `### name` headings.
func splitMarkdownDump(text string) []DocBlock {
	var out []DocBlock
	var cur *DocBlock
	for _, raw := range strings.Split(text, "\n") {
		h := strings.TrimSpace(raw)
		if strings.HasPrefix(h, "## ") || strings.HasPrefix(h, "### ") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &DocBlock{Name: dumpTokenRe.FindString(strings.TrimLeft(h, "# "))}
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, raw)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// splitClassicDump reads `----`-separated blocks. A header is either
// `name - description` (effects, triggers, targets) or `name:` (on_actions).
func splitClassicDump(text string) []DocBlock {
	var out []DocBlock
	for _, chunk := range strings.Split(text, "----") {
		var b DocBlock
		for _, raw := range strings.Split(chunk, "\n") {
			t := strings.TrimSpace(raw)
			if t == "" {
				continue
			}
			if b.Name == "" {
				if strings.Contains(strings.ToLower(t), "documentation") {
					continue // the file's own banner, not an entry
				}
				b.Name = dumpTokenRe.FindString(t)
				if b.Name == "" {
					continue
				}
				if i := strings.Index(t, " - "); i >= 0 {
					b.Lines = append(b.Lines, t[i+3:])
				}
				continue
			}
			b.Lines = append(b.Lines, t)
		}
		if b.Name != "" {
			out = append(out, b)
		}
	}
	return out
}

// ModifierAreaLabels are the headers a game uses for the list of areas a
// modifier applies in. The same field, two spellings.
//
//	ck3   "Use areas"
//	vic3  "Mask"
//
// EU5 states no areas at all, so it contributes no label.
var ModifierAreaLabels = []string{"use areas", "mask"}

// IsModifierAreaLabel reports a header naming a modifier's areas, in any game's
// spelling. Case-insensitive; the dumps are not consistent about it.
func IsModifierAreaLabel(header string) bool {
	h := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(header), ":")))
	for _, l := range ModifierAreaLabels {
		if h == l {
			return true
		}
	}
	return false
}

// splitBareDump reads files whose headers are a bare "name:" at column 0
// (event_scopes.log, Vic3 modifiers.log) or "Tag: name" (CK3 modifiers.log).
// Body lines carry "Label: value".
func splitBareDump(text string) []DocBlock {
	var out []DocBlock
	var cur *DocBlock
	flush := func() {
		if cur != nil && cur.Name != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "---") {
			flush()
			continue
		}
		if name, rest, ok := bareHeader(raw); ok {
			flush()
			cur = &DocBlock{Name: name}
			if rest != "" {
				cur.Lines = append(cur.Lines, rest)
			}
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, raw)
		}
	}
	flush()
	return out
}

// bareHeader matches the three header shapes these files use:
//
//	name:            unindented, nothing after the colon (event_scopes, Vic3 modifiers)
//	Tag: name        CK3 modifiers, optionally followed by ", Categories: ..."
//	key: name        older Vic3 modifiers, indented
//
// Vic3 changed from the third form to the first between game versions, so both
// are accepted. Names carrying a $PLACEHOLDER$ are templates, not tokens.
// bareHeader returns the block's name and whatever the header line carried
// after it. That tail is the token's category list, which CK3 and EU5 write on
// the header rather than on a line of its own:
//
//	Tag: local_trades_per_burgher, Categories: Location, , All,
//
// Discarding the tail threw away the only thing either game says about a
// modifier — EU5 documents 2,436 of them and gives prose for none.
func bareHeader(raw string) (name, rest string, ok bool) {
	t := strings.TrimRight(raw, " \t\r")
	if t == "" {
		return "", "", false
	}
	trimmed := strings.TrimLeft(t, " \t")
	for _, p := range []string{"Tag:", "key:"} {
		after, cut := strings.CutPrefix(trimmed, p)
		if !cut {
			continue
		}
		after = strings.TrimSpace(after)
		head := after
		if i := strings.IndexByte(after, ','); i > 0 {
			head, rest = strings.TrimSpace(after[:i]), strings.TrimSpace(after[i+1:])
		}
		name = dumpTokenRe.FindString(head)
		if name == "" || name != head {
			return "", "", false
		}
		return name, rest, true
	}
	// The bare "name:" form must be unindented, or every "  Mask: x" body line
	// would start a new block.
	if t != trimmed || !strings.HasSuffix(t, ":") {
		return "", "", false
	}
	name = strings.TrimSuffix(t, ":")
	if name == "" || name != dumpTokenRe.FindString(name) {
		return "", "", false
	}
	return name, "", true
}
