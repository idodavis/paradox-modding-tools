// info.go reads the documentation a game ships beside its script (CK3's _traits.info, EU5's
// readme.txt, Vic3's buildings.md) and the comment headers of its script files, as notes on the
// positions of one definition: the prose written for a key, the type its value is written as, and
// what the prose says of whether it may be left out, its default and the scope its block runs in.
// The code lines make a skeleton that jomini parses for the nesting; each statement takes the
// comment on its line, else the notes above it. A form the reader does not know is drift.
package schema

import (
	"cmp"
	"maps"
	"os"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Info is an install's documentation, by folder from a root, lower-cased: common/traits.
type Info map[string]*FolderInfo

// FolderInfo is what the documentation of one folder says.
type FolderInfo struct {
	Prose       string            // about the whole kind: what a doc says before its first key, four lines at most
	Entry       *Field            // one definition as the docs write it; a kind in it is named by its folder
	Definitions map[string]string // a definition's blurb from its own file's header list (# name - text), by name
}

// infoMaxBytes keeps data out: EU5 ships 5 MB .info terrain caches; real prose stays under 25 KB.
// scriptMaxBytes is the largest script file read for its header and names.
const (
	infoMaxBytes   = 1 << 20
	scriptMaxBytes = 8 << 20
)

// script is what one vanilla script file tells the documentation reader: its header's template
// lines, its header's blurbs for names it defines, the names at its top and the keys inside them.
type script struct {
	templates string
	blurbs    map[string]string
	tops      []string
	inner     []string
}

// known is what vanilla's script writes, for reading a doc: each doc folder's top names and inner
// keys (its subfolders' included), each folder by its last segment, and the install's top folders.
type known struct {
	tops      map[string]map[string]bool
	inner     map[string]map[string]bool
	bySegment map[string][]string
	roots     map[string]bool
}

// ReadInfo reads the documentation of an inventory: every script file once, for its header and what
// vanilla writes, then every doc and header template. The lines no form reads come back as drift.
func ReadInfo(files []source.File, vocab *Vocab) (Info, []Tally) {
	info := Info{}
	folderOf := func(file source.File) *FolderInfo {
		folder := strings.ToLower(path.Dir(file.Rel))
		if info[folder] == nil {
			info[folder] = &FolderInfo{Entry: &Field{Origin: Declared}, Definitions: map[string]string{}}
		}
		return info[folder]
	}

	// the folders a doc documents, whose script says which of a doc's openers are entries
	docFolders := map[string]bool{}
	knew := &known{tops: map[string]map[string]bool{}, inner: map[string]map[string]bool{}, bySegment: map[string][]string{}, roots: map[string]bool{}}
	for _, file := range files {
		if file.Format == source.Info {
			docFolders[strings.ToLower(path.Dir(file.Rel))] = true
		}
		first, _, _ := strings.Cut(strings.ToLower(file.Rel), "/")
		knew.roots[first] = true
	}

	// every script file, a reader per CPU, merged in file order so the first file to speak wins
	scripts, gate := make([]script, len(files)), make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, file := range files {
		if file.Format == source.Script && strings.HasSuffix(strings.ToLower(file.Rel), ".txt") {
			gate <- struct{}{}
			wg.Go(func() {
				scripts[i] = readScript(file.Path)
				<-gate
			})
		}
	}
	wg.Wait()
	for i, got := range scripts {
		folder := strings.ToLower(path.Dir(files[i].Rel))
		if got.tops != nil && !slices.Contains(knew.bySegment[path.Base(folder)], folder) {
			knew.bySegment[path.Base(folder)] = append(knew.bySegment[path.Base(folder)], folder)
		}
		for at := folder; at != "." && at != "/"; at = path.Dir(at) {
			if !docFolders[at] {
				continue
			}
			if knew.tops[at] == nil {
				knew.tops[at], knew.inner[at] = map[string]bool{}, map[string]bool{}
			}
			for _, name := range got.tops {
				knew.tops[at][name] = true
			}
			for _, key := range got.inner {
				knew.inner[at][key] = true
			}
		}
	}

	// the docs, then the headers' templates where no doc file covers the folder; the blurbs always
	unread := map[string]int{}
	reader := func(file source.File) *docReader {
		return &docReader{info: folderOf(file), folder: strings.ToLower(path.Dir(file.Rel)), file: file.Rel, known: knew, vocab: vocab, unread: unread,
			every: map[*Field]Need{}}
	}
	for _, file := range files {
		if stat, err := os.Stat(file.Path); file.Format != source.Info || err != nil || stat.Size() > infoMaxBytes {
			continue
		}
		if text, err := file.Text(); err == nil {
			reader(file).read(text, strings.HasSuffix(strings.ToLower(file.Rel), ".md"))
		}
	}
	for i, got := range scripts {
		if got.templates == "" && len(got.blurbs) == 0 {
			continue
		}
		doc := reader(files[i])
		if !docFolders[doc.folder] {
			doc.read(got.templates, false)
		}
		for name, blurb := range got.blurbs {
			doc.info.Definitions[name] = cmp.Or(doc.info.Definitions[name], blurb)
		}
	}

	var drift []Tally
	for line, count := range unread {
		drift = append(drift, Tally{Name: line, Count: count})
	}
	slices.SortFunc(drift, func(first, second Tally) int { return strings.Compare(first.Name, second.Name) })
	return info, drift
}

// readScript reads a script file once: the comments at its top, before its first line of script,
// where a key line whose value is a template (<path>, yes/no, a | b) documents the folder and a
// line name - text is a blurb for a name the file defines; then the names it defines at its top
// and the keys written inside them.
func readScript(abs string) script {
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) > scriptMaxBytes {
		return script{}
	}
	text, _ := jomini.Decode(raw)
	var got script
	// the other header lines are kept blank, so a template line keeps its number for the drift report
	var templates []string
	found := false
	listed := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		body, hashed := jomini.PeelComment(line)
		if !hashed && strings.TrimSpace(line) != "" {
			break
		}
		kept, item := "", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "- "))
		name, blurb, dashed := strings.Cut(item, " - ")
		if !dashed {
			name, blurb, dashed = strings.Cut(item, " – ")
		}
		if dashed && jomini.IsDefName(name) && len(strings.Fields(blurb)) >= 2 {
			listed[strings.ToLower(name)] = strings.TrimSpace(blurb)
		} else if read := readLine(line, false); read.kind == lineCode && (read.typ != "" || typeWords(read.rest).stated) {
			kept, found = line, true
		}
		templates = append(templates, kept)
	}
	if found {
		got.templates = strings.Join(templates, "\n")
	}

	inner := map[string]bool{}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			if assignment, ok := st.(*jomini.Assignment); ok {
				inner[strings.ToLower(assignment.Key.Text)] = true
				if block := jomini.BlockOf(assignment.Value); block != nil {
					walk(block.Statements)
				}
			}
		}
	}
	for _, st := range jomini.Parse(text).Root.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok {
			_, name := jomini.Mode(assignment.Key.Text)
			name = strings.Clone(strings.ToLower(name))
			got.tops = append(got.tops, name)
			if blurb, ok := listed[name]; ok {
				if got.blurbs == nil {
					got.blurbs = map[string]string{}
				}
				got.blurbs[name] = blurb
			}
			if block := jomini.BlockOf(assignment.Value); block != nil {
				walk(block.Statements)
			}
		}
	}
	for key := range inner {
		got.inner = append(got.inner, strings.Clone(key))
	}
	return got
}

// Kind is what the game says about the kind whose definitions are in folders.
func (info Info) Kind(folders []string) string {
	for _, folder := range folders {
		if fi := info[strings.ToLower(folder)]; fi != nil && fi.Prose != "" {
			return fi.Prose
		}
	}
	return ""
}

// Field is what the documentation writes at key inside a block named parent, its shallowest
// mention, else, where anywhere, at key under any parent; parent is "" for a definition's own
// member. The zero Field where none does.
func (info Info) Field(folders []string, parent, key string, anywhere bool) Field {
	for _, folder := range folders {
		fi := info[strings.ToLower(folder)]
		if fi == nil {
			continue
		}
		for _, anyParent := range []bool{false, anywhere} {
			for level := []*Field{fi.Entry}; len(level) > 0; {
				var next []*Field
				for _, field := range level {
					if member := field.Members[key]; member != nil && (anyParent || field.Name == parent) {
						return *member
					}
					for _, name := range slices.Sorted(maps.Keys(field.Members)) {
						next = append(next, field.Members[name])
					}
				}
				level = next
			}
		}
	}
	return Field{}
}

// aliases are the misspellings the games' docs write for the words the reader reads, each found in
// a shipped doc (CK3 _bookmark_groups.info: Breif); a line is read with them corrected.
var aliases = strings.NewReplacer("Breif", "Brief", "breif", "brief", "Brienf", "Brief", "brienf", "brief", "Defualt", "Default", "defualt", "default",
	"Defult", "Default", "defult", "default", "Defaut", "Default", "defaut", "default", "Sctipted", "Scripted", "sctipted", "scripted",
	"Scipted", "Scripted", "scipted", "scripted", "effets", "effects", "descriotion", "description", "descriptopn", "description",
	"Exmaple", "Example", "exmaple", "example", "casus belly", "casus belli", "procuction", "production")

// docLabels is the words a doc writes before a colon to say what a line is, not naming a key.
var docLabels = []string{"default", "defaults", "scope", "scopes", "root", "note", "notes", "optional", "example", "examples", "warning",
	"important", "syntax", "format", "values", "options", "usage", "localization", "errors", "targets", "enums", "fallback", "ex", "eg"}

// lineKind is what one doc line is, whatever style wrote it.
type lineKind uint8

const (
	lineNote    lineKind = iota // a sentence, a label (Default: 0), a bullet of prose
	lineBreak                   // a lone #: parts the paragraphs of the notes around it
	lineBlank                   // nothing: gives the notes above to what is open
	lineRule                    // ======, #####, a fence
	lineTitle                   // === Structure ===, TITLE, Valid values:, a markdown heading
	lineSection                 // a markdown heading naming a key: ## key (default: x)
	lineBrief                   // ### Brief: key ( type ), ### key (type), ### a/b effects
	lineCode                    // key = value, - key: <type> prose, <key> = {, 1.1.1 = {
	lineClose                   // } and nothing else
	lineItem                    // a placeholder alone: <effects>
	lineSkip                    // a word alone, an example comparison: nothing to read
	lineUnread                  // a table row, a form no rule reads: drift
)

// docLine is one line of a doc as a record, the same whatever style wrote it.
type docLine struct {
	kind      lineKind
	number    int      // its line in the file
	col       int      // its indentation with comment marks left out: tab 4, space 1
	commented bool     // written behind #
	keys      []string // a code line's key; a brief's keys, several where the doc joins them with /
	op        string   // = or :
	rest      string   // what a code line writes after its operator, its comment cut off; an item
	typ       string   // a type the line names: <type>, ( type ), a type word before its prose
	note      string   // its prose: a sentence, a label, a code line's comment or the words after its type
	opens     int      // blocks a code line opens less those it closes; a close line's count, negative
}

var (
	keyPattern = `[A-Za-z_$@][\w.@$\-|]*(?::[\w.@$\-|]+)*`
	keyLineRe  = regexp.MustCompile("^(?P<bullet>(?:[-*+•]|\\d+[.)])[ \\t]+)?(?P<tick>`?)(?P<key>" + keyPattern + ")`?[ \\t]*(?P<op>\\?=|<=|>=|==|!=|=|:|<|>)(?P<rest>.*)$")
	briefRe    = regexp.MustCompile(`(?i)^(?:[-*+][ \t]+)?brief[ \t]*:[ \t]*(` + keyPattern + `|[\w/]+)?\s*(.*)$`)
	bareBrief  = regexp.MustCompile(`^(` + keyPattern + `)\s*\(([^()]*)\)\s*$`)
	ruleRe     = regexp.MustCompile(`^[=\-*_~+#. ]{3,}$`)
	titleRule  = regexp.MustCompile(`^[=\-*~#]{2,}[ \t]*(.+?)[ \t]*[=\-*~#]{2,}$`)
	typeFirst  = regexp.MustCompile(`(?i)^(\[?[\w.\-]+(?:\s*[/|]\s*[\w.\-]+)+\]?|bool(?:ean)?|int(?:eger|32|64)?|float|number|fixed[ _]?point|script ?value|scripted value|string|text|loc(?:alization)?[ _]?key|date|triggers?|effects?|modifiers?)\s*[:;,\-–(]?\s+\S`)
	needLead   = regexp.MustCompile(`(?i)^(?:[(\[<]\s*(?:optional|required)\s*[)\]>]|optional\b)\s*`)
)

// readLine reads one doc line into its record, its typos read as their words. markdown makes a # at
// column 0 a heading.
func readLine(raw string, markdown bool) docLine {
	raw = aliases.Replace(strings.TrimRight(raw, "\r"))
	body, marks, after := strings.TrimLeft(raw, " \t"), 0, ""
	indent := raw[:len(raw)-len(body)]
	for strings.HasPrefix(body, "#") {
		trimmed := strings.TrimLeft(body[1:], " \t")
		after, body, marks = body[1:len(body)-len(trimmed)], trimmed, marks+1
	}
	width := func(space string) int { return strings.Count(space, "\t")*4 + strings.Count(space, " ") }
	line := docLine{commented: marks > 0, col: width(indent) + width(after)}
	body = strings.TrimSpace(body)
	fields, title, brief := strings.Fields(body), titleRule.FindStringSubmatch(body), bareBrief.FindStringSubmatch(body)

	// what only parts or ends the notes, the headings and briefs that name keys, a table row, a close
	switch labeled := briefRe.FindStringSubmatch(body); {
	case body == "" && marks > 0 && marks < 4:
		line.kind = lineBreak
	case body == "" && marks == 0:
		line.kind = lineBlank
	case body == "" || ruleRe.MatchString(body) || strings.HasPrefix(body, "```"):
		line.kind = lineRule
	case markdown && indent == "" && marks > 0 && after != "":
		line.kind, line.note = lineTitle, body
		if name := strings.Trim(body, "`"); jomini.IsLowerName(name) {
			line.kind, line.keys = lineSection, []string{name}
		} else if brief != nil && jomini.IsLowerName(brief[1]) {
			line.kind, line.keys, line.typ = lineSection, []string{brief[1]}, strings.TrimSpace(brief[2])
		}
	case title != nil && strings.ContainsFunc(title[1], unicode.IsLetter):
		line.kind, line.note = lineTitle, title[1]
	case labeled != nil:
		line.kind, line.keys = lineBrief, strings.Split(labeled[1], "/")
		line.typ = strings.TrimSpace(strings.Trim(strings.TrimSpace(labeled[2]), "()-– "))
	case brief != nil && marks >= 2:
		line.kind, line.keys, line.typ = lineBrief, []string{brief[1]}, strings.TrimSpace(brief[2])
	case marks >= 2 && len(fields) >= 2 && strings.Contains(fields[0], "/") && typeWords(strings.Join(fields[1:], " ")).stated:
		line.kind, line.keys, line.typ = lineBrief, strings.Split(fields[0], "/"), strings.Join(fields[1:], " ")
	case strings.HasPrefix(body, "|"):
		line.kind = lineUnread
	case strings.HasPrefix(body, "}") && strings.Trim(strings.Split(body, "#")[0], "} \t") == "":
		line.kind, line.opens = lineClose, -strings.Count(strings.Split(body, "#")[0], "}")
	}
	if line.kind != lineNote {
		return line
	}

	// a key line: key = value # comment, - key: <type> prose, <key> = {, 1.1.1 = {; a comparison is
	// an example statement
	key, op, rest, bullet, tick := "", "", "", "", ""
	if closed := strings.IndexByte(body, '>'); strings.HasPrefix(body, "<") && closed > 0 {
		if value, found := strings.CutPrefix(strings.TrimSpace(body[closed+1:]), "="); found {
			key, op, rest = body[:closed+1], "=", value
		}
	}
	if left, value, ok := strings.Cut(body, "="); key == "" && ok && slices.Contains([]jomini.LiteralKind{jomini.LitNumber, jomini.LitDate}, jomini.Literal(strings.TrimSpace(left))) {
		key, op, rest = strings.TrimSpace(left), "=", value
	}
	if match := keyLineRe.FindStringSubmatch(body); key == "" && match != nil {
		bullet, tick, key, op, rest = match[1], match[2], strings.TrimRight(match[3], ".-"), match[4], match[5]
	}
	if key != "" {
		line.kind, line.keys, line.op = lineCode, []string{key}, op
		text := strings.TrimSpace(rest)
		switch op {
		case "=":
			line.rest, line.note = cutComment(text)
			if tick != "" {
				line.rest = strings.TrimSuffix(line.rest, "`")
			}
			line.opens = strings.Count(line.rest, "{") - strings.Count(line.rest, "}")
			if typ, tail := typeOf(line.rest); typ != "" {
				line.typ, line.note = typ, joinProse([]string{tail, line.note})
			}
		case ":":
			line.rest, line.note = text, text
			if typ, tail := typeOf(text); typ != "" {
				line.typ, line.note = typ, tail
			}
			// a label, a capitalised word, or in live script or with few words, a colon is prose (-
			// Travel: the character is travelling, scope: character)
			label := slices.Contains(docLabels, strings.ToLower(key)) && bullet == ""
			switch {
			case label && text == "":
				line.kind, line.keys, line.op, line.note = lineTitle, nil, "", key
			case label || line.typ == "" && (!jomini.IsLowerName(key) || bullet == "" && tick == "" && (!line.commented && !markdown || len(strings.Fields(text)) < 3)):
				line.kind, line.keys, line.op, line.note = lineNote, nil, "", strings.TrimPrefix(body, bullet)
			}
		default:
			line.kind = lineSkip
		}
		return line
	}

	// a placeholder alone, bare example values ("text", { a b }), titles, sentences, and a word
	// alone
	letters := strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) {
			return char
		}
		return -1
	}, body)
	trimmed := strings.TrimRight(body, ".")
	value, comment := cutComment(body)
	switch {
	case strings.HasPrefix(value, "<") && strings.Count(value, "<") == 1 && strings.Count(value, ">") == 1:
		line.kind, line.rest, line.note = lineItem, value, comment
	case strings.ContainsRune(`"{`, rune(body[0])) && strings.ContainsRune(`"}`, rune(trimmed[len(trimmed)-1])):
		line.kind = lineSkip
	case strings.HasSuffix(body, ":") && len(fields) <= 6, len(letters) >= 2 && strings.ToUpper(letters) == letters,
		marks >= 2 && len(fields) <= 6 && !strings.HasSuffix(body, "."):
		line.kind, line.note = lineTitle, strings.TrimSuffix(body, ":")
	case len(fields) >= 2 && len(letters) >= 2:
		line.note = body
		if slices.Contains([]string{"-", "*", "+", "•"}, fields[0]) {
			line.note = strings.TrimSpace(body[len(fields[0]):])
		}
	case len(fields) == 1:
		line.kind = lineSkip
	default:
		line.kind = lineUnread
	}
	return line
}

// cutComment splits a value from the # comment behind it.
func cutComment(text string) (value, comment string) {
	value, comment, _ = strings.Cut(text, "#")
	comment = strings.TrimSpace(strings.TrimLeft(comment, "# "))
	if !strings.ContainsFunc(comment, unicode.IsLetter) {
		comment = ""
	}
	return strings.TrimSpace(value), comment
}

// typeOf is the type a value names and the prose after it: <type> prose, { <type> } prose, (type)
// prose, a type word, a/b/c, or a type word leading its prose (yes/no by default yes).
func typeOf(value string) (typ, prose string) {
	value = strings.TrimSpace(value)

	// a need written before the type, (optional) <trigger>, stays in the prose
	if lead := needLead.FindString(value); lead != "" {
		if typ, _ = typeOf(value[len(lead):]); typ != "" {
			return typ, value
		}
		return "", ""
	}
	for _, open := range []string{"<", "{"} {
		inner, ok := strings.CutPrefix(value, open)
		if !ok {
			continue
		}
		inner = strings.TrimSpace(inner)
		if placeholder, rest, closed := strings.Cut(strings.TrimPrefix(inner, "<"), ">"); closed && strings.HasPrefix(inner, "<") || open == "<" && closed {
			return strings.TrimSpace(placeholder), strings.TrimSpace(strings.TrimLeft(rest, "} "))
		}
	}
	if inner, ok := strings.CutPrefix(value, "("); ok {
		if typ, rest, closed := strings.Cut(inner, ")"); closed && typeWords(typ).stated {
			return strings.TrimSpace(typ), strings.TrimSpace(rest)
		}
	}
	if typeWords(value).stated {
		return value, ""
	}
	if first := typeFirst.FindStringSubmatch(value); first != nil && typeWords(first[1]).stated {
		return first[1], strings.TrimSpace(strings.TrimLeft(value[len(first[1]):], " :;,-–"))
	}
	return "", ""
}

// docReader reads one documentation file into its folder's template.
type docReader struct {
	info      *FolderInfo
	folder    string
	file      string // its path from a root, for the drift report
	known     *known
	vocab     *Vocab
	unread    map[string]int
	every     map[*Field]Need // the blocks whose prose gives every member a need: "All tiers are required"
	lines     []docLine
	at        *jomini.LineIndex
	pending   []string // notes waiting for the statement below them
	open      []opened // positions whose notes may still follow them
	section   bool     // open is a markdown heading, whose paragraphs are its notes
	kindProse []string // what the doc says of the whole kind, before its first key
	kindDone  bool
	wrap      bool // the doc writes one definition's template
}

// opened is a position whose notes may still follow: a block's opener, a brief, a markdown heading.
type opened struct {
	field *Field
	prose string
}

// read adds what one doc says. markdown makes a # at column 0 a heading.
func (doc *docReader) read(text string, markdown bool) {
	for raw := range strings.SplitSeq(text, "\n") {
		line := readLine(raw, markdown)
		line.number = len(doc.lines) + 1
		doc.lines = append(doc.lines, line)
	}
	parsed := jomini.Parse(skeleton(doc.lines, markdown))
	doc.at, doc.kindDone = parsed.Lines(), doc.info.Prose != ""

	// a doc is one definition's template where an outermost opener is a placeholder, or the only
	// opener there with no field beside it (_traits.info writes fields, then tracks = {)
	openers, fields := 0, 0
	for _, st := range parsed.Root.Statements {
		assignment, ok := st.(*jomini.Assignment)
		line := doc.lines[doc.at.PositionAt(st.NodeRange().Start).Line]
		switch {
		case !ok:
		case jomini.BlockOf(assignment.Value) != nil:
			openers++
			doc.wrap = doc.wrap || doc.marker(line.keys[0])
		case line.rest != "":
			fields++
		}
	}
	doc.wrap = doc.wrap || openers == 1 && fields == 0
	doc.walk(parsed.Root.Statements, doc.info.Entry, 0, len(doc.lines), true)
	doc.settle(false)
	if doc.info.Prose == "" {
		doc.info.Prose = joinProse(doc.kindProse)
	}

	// "All fields are optional" about the kind, "All tiers are required" about a block, once the
	// doc has written the members
	doc.need(doc.info.Entry, joinProse(doc.kindProse))
	for field, need := range doc.every {
		for _, member := range field.Members {
			setNeed(member, need, "", Declared)
		}
	}

	// the events doc says where its definitions run on its scope field's default
	if scope := doc.info.Entry.Members["scope"]; scope != nil && scope.Default != "" {
		doc.root(doc.info.Entry, "root = "+scope.Default)
	}
}

// skeleton writes a doc's code lines as bare script, line for line, for jomini to nest: k = v,
// k = {, }, v. Where the braces do not balance, a missing } is written where a line returns to its
// opener's column or a title starts at column 0 (owner), and an extra one is dropped.
func skeleton(lines []docLine, markdown bool) string {
	depth, balanced := 0, true
	for _, line := range lines {
		if line.kind == lineCode || line.kind == lineClose {
			depth += line.opens
			balanced = balanced && depth >= 0
		}
	}
	balanced = balanced && depth == 0

	var out strings.Builder
	var open []int // the columns of the blocks open, where the braces do not balance
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		heading := line.kind == lineTitle && line.col == 0 && (markdown || !line.commented)
		for !balanced && len(open) > 0 && (heading || line.kind == lineCode && line.col <= open[len(open)-1]) {
			out.WriteString("} ")
			open = open[:len(open)-1]
		}
		switch {
		case line.kind == lineCode && line.opens > 0:
			out.WriteString("k =")
			for range line.opens {
				out.WriteString(" {")
				open = append(open, line.col)
			}
		case line.kind == lineCode && balanced:
			out.WriteString("k = v")
			out.WriteString(strings.Repeat(" }", -line.opens))
		case line.kind == lineCode:
			out.WriteString("k = v")
		case line.kind == lineClose && balanced:
			out.WriteString(strings.Repeat("} ", -line.opens))
		case line.kind == lineClose:
			closes := min(-line.opens, len(open))
			out.WriteString(strings.Repeat("} ", closes))
			open = open[:len(open)-closes]
		case line.kind == lineItem:
			out.WriteString("v")
		}
	}
	out.WriteString(strings.Repeat(" }", len(open)*map[bool]int{false: 1}[balanced]))
	return out.String()
}

// walk reads the statements of one block, lines from up to to, into the positions below parent;
// top is the doc's outermost level, where an opener may be the definition itself.
func (doc *docReader) walk(stmts []jomini.Statement, parent *Field, from, to int, top bool) {
	for _, st := range stmts {
		first, last := doc.at.PositionAt(st.NodeRange().Start).Line, doc.at.PositionAt(st.NodeRange().End).Line
		doc.gap(from, first, parent)
		from = last + 1
		line, bare := doc.lines[first], false
		var value jomini.Value
		switch st := st.(type) {
		case *jomini.Assignment:
			value = st.Value
		case *jomini.ValueStmt:
			value, bare = st.Value, true
		}
		block := jomini.BlockOf(value)
		if bare {
			// a line opening two blocks nests the second in the first; <effects> alone inside a block
			// says what the block holds, its comment is a note on the block, and a placeholder no type
			// reads is drift
			if block != nil {
				doc.walk(block.Statements, parent, first+1, last, false)
				continue
			}
			if typ, _ := typeOf(line.rest); top || !doc.typed(parent, typ) {
				doc.unread[doc.file+":"+strconv.Itoa(line.number)]++
			}
			if line.note != "" {
				doc.pending = append(doc.pending, line.note)
			}
			continue
		}

		// root = character and scope:actor = the actor are notes about the position below
		key := line.keys[0]
		lower := strings.ToLower(key)
		if (lower == "root" || strings.HasPrefix(lower, "scope:") || lower == "scope" && !doc.known.inner[doc.folder][lower]) && block == nil {
			doc.pending = append(doc.pending, joinProse([]string{key + " " + line.op + " " + line.rest, line.note}))
			continue
		}
		doc.kindDone = true
		doc.settle(!doc.section)
		above := joinProse(doc.pending)
		doc.pending = nil

		// a top-level opener is the definition itself where the doc wraps one, or where vanilla
		// writes the word as a name or not at all; otherwise it is a field
		if block != nil && top && (doc.wrap || doc.marker(key) || !doc.known.inner[doc.folder][lower] || doc.known.tops[doc.folder][lower]) {
			doc.open = []opened{{field: doc.info.Entry, prose: joinProse([]string{above, line.note})}}
			doc.walk(block.Statements, doc.info.Entry, first+1, last, false)
			continue
		}

		// a placeholder inside a block is a position of names; a number or a date, a position of
		// numbers or dates
		switch {
		case doc.marker(key):
			key = "<name>"
		case jomini.Literal(key) == jomini.LitNumber:
			key = "<number>"
		case jomini.Literal(key) == jomini.LitDate:
			key = "<date>"
		}
		field := doc.member(parent, key)
		prose := joinProse([]string{above, doc.value(field, line)})
		if block == nil {
			doc.note(field, prose)
			continue
		}
		doc.open = []opened{{field: field, prose: prose}}
		doc.walk(block.Statements, field, first+1, last, false)
	}
	doc.gap(from, to, parent)
	if !top && to < len(doc.lines) && doc.lines[to].kind == lineClose && !doc.section {
		doc.settle(false)
	}
}

// gap reads the lines between two statements: notes wait for the statement below them; a blank line
// or a title gives them to what is open; a brief opens the positions it names below parent, and a
// markdown heading naming a key its section.
func (doc *docReader) gap(from, to int, parent *Field) {
	for _, line := range doc.lines[from:max(from, to)] {
		switch line.kind {
		case lineNote:
			if !doc.kindDone && len(doc.kindProse) < 4 && line.op == "" {
				doc.kindProse = append(doc.kindProse, line.note)
			}
			if !doc.section || line.col == 0 {
				doc.pending = append(doc.pending, line.note)
			}
		case lineBreak, lineBlank:
			doc.kindDone = doc.kindDone || len(doc.kindProse) > 0
			if doc.section || line.kind == lineBreak {
				doc.pending = append(doc.pending, "")
			} else {
				doc.settle(false)
			}
		case lineRule:
			if !doc.section {
				doc.settle(false)
			}
		case lineTitle, lineSection:
			doc.settle(false)
			doc.kindDone = doc.kindDone || len(doc.kindProse) > 0
			if line.kind == lineSection {
				field := doc.member(doc.info.Entry, line.keys[0])
				doc.typed(field, line.typ)
				doc.need(field, line.typ)
				doc.open, doc.section = []opened{{field: field}}, true
			}
		case lineBrief:
			// a brief's notes are the ones above it, unless something before it is still open
			doc.kindDone = true
			doc.settle(!doc.section && len(doc.open) == 0)
			above := joinProse(doc.pending)
			doc.pending = nil
			for _, name := range line.keys {
				if jomini.IsDefName(name) {
					field := doc.member(parent, name)
					doc.typed(field, line.typ)
					doc.need(field, line.typ)
					doc.open = append(doc.open, opened{field: field, prose: above})
				}
			}
		case lineUnread:
			doc.unread[doc.file+":"+strconv.Itoa(line.number)]++
		}
	}
}

// settle gives the positions still open the notes waiting, unless a statement below claims them.
func (doc *docReader) settle(claimed bool) {
	for _, got := range doc.open {
		if claimed {
			doc.note(got.field, got.prose)
		} else {
			doc.note(got.field, joinProse([]string{got.prose, joinProse(doc.pending)}))
		}
	}
	if !claimed {
		doc.pending = nil
	}
	doc.open, doc.section = nil, false
}

// placeholder is a word a doc writes for a name of its own choosing: <key>, trait_key, my_event,
// option_1. marker also asks that vanilla never writes it as a key in the folder.
var placeholder = regexp.MustCompile(`(?i)^(<[^<>]+>|[a-z0-9_]*_(key|id|name|tag|type)|(example|my|some|another|name_of|your|unique|new)_[a-z0-9_]*|[a-z_]+_\d)$`)

func (doc *docReader) marker(key string) bool {
	return placeholder.MatchString(key) && (strings.HasPrefix(key, "<") || !doc.known.inner[doc.folder][strings.ToLower(key)])
}

// member is the position key below parent, made where the doc first writes it.
func (doc *docReader) member(parent *Field, key string) *Field {
	if parent.Members == nil {
		parent.Members = map[string]*Field{}
	}
	if parent.Members[key] == nil {
		parent.Members[key] = &Field{Name: key, Origin: Declared}
	}
	return parent.Members[key]
}

// note gives a position the prose written for it, the first file to write any keeping it, and the
// need, default and root scope the prose states.
func (doc *docReader) note(field *Field, prose string) {
	if prose == "" {
		return
	}
	field.Doc = cmp.Or(field.Doc, prose)
	doc.need(field, prose)
	doc.root(field, prose)
}

// needWord, needIf, needAll and needOmitted find whether prose lets its position be left out:
// "Optional; will cause", "This field is required.", "must be set", "required for letter events",
// "If the type is lifestyle this is required", "All tiers are required", "if not set".
var (
	needWord    = regexp.MustCompile(`^(?:(?:this|the) (?:field|parameter|key|value) is |it is |it's |this is )?(optional|required|mandatory|must be (?:set|specified|defined|present))\b\s*(\S*)\s*(.*)$`)
	needIf      = regexp.MustCompile(`^(if .+?),? (?:this|it) is (?:required|mandatory)$`)
	needAll     = regexp.MustCompile(`^all [a-z_ ]{1,30}? are (optional|required|mandatory)\b`)
	needOmitted = regexp.MustCompile(`\bif (?:omitted|not (?:set|specified|defined|present)|left (?:empty|out|blank))\b|\b(?:can|may) be (?:omitted|left out)\b`)
)

// need reads whether prose lets its position be left out, and the default it takes then, clause by
// clause. Optional is any clause opening with the word, the doc's own adjective (optional list of,
// [Optional]); required only where it cannot be one (Required technologies to), with the condition
// that follows it. A default, or what happens when it is omitted, makes it optional by inference.
func (doc *docReader) need(field *Field, prose string) {
	if said := defaultSaid.FindStringSubmatch(prose); said != nil && field.Default == "" {
		field.Default = strings.Trim(said[1], `".`)
	}
	low := strings.ToLower(prose)
	for clause := range strings.FieldsFuncSeq(low, func(char rune) bool { return strings.ContainsRune(".;!?,()[]<>", char) }) {
		clause = strings.TrimSpace(clause)
		if all := needAll.FindStringSubmatch(clause); all != nil {
			doc.every[field] = map[bool]Need{true: NeedOptional, false: NeedRequired}[all[1] == "optional"]
			continue
		}
		if cond := needIf.FindStringSubmatch(clause); cond != nil {
			setNeed(field, NeedWhen, cond[1], Declared)
			continue
		}
		word := needWord.FindStringSubmatch(clause)
		switch {
		case word == nil:
		case word[1] == "optional":
			setNeed(field, NeedOptional, "", Declared)
		case word[2] == "" || slices.Contains([]string{"and", "but", "here", "always"}, word[2]) || word[2] == "to" && strings.HasPrefix(word[3], "be "):
			setNeed(field, NeedRequired, "", Declared)
		case slices.Contains([]string{"for", "if", "when", "in", "on", "only", "unless", "with"}, word[2]):
			setNeed(field, NeedWhen, word[2]+" "+word[3], Declared)
		}
	}
	if field.Default != "" || needOmitted.MatchString(low) {
		setNeed(field, NeedOptional, "", Inferred)
	}
}

// setNeed gives a position a need: a declared one replaces an inferred one, and the stronger of two
// declared ones stays; an inferred optional only fills a silence.
func setNeed(field *Field, need Need, when string, from Origin) {
	if from == Declared && (field.NeedFrom != Declared || need > field.Need) || field.Need == NeedUnstated {
		field.Need, field.NeedWhen, field.NeedFrom = need, when, from
	}
}

// defaultSaid and rootSaid find a default and a root scope in prose: "Defaults to all", "(default
// 0)", "Default: 0", "root = the character", "Scopes: root ( Province )".
var (
	defaultSaid = regexp.MustCompile(`(?i)\bdefaults?\b(?:\s*(?:to|is|=|:)\s*|\s+)("?[a-z0-9_.\-]+"?)`)
	rootSaid    = regexp.MustCompile(`(?i)\b(?:root|scope)(?:\s+scope)?\s*(?:is|[:=(])\s*(?:the |a |an )?([a-z_]+(?: [a-z_]+){0,2})`)
)

// root sets where a block's statements run from what a line says, when it names a scope type the
// dumps declare.
func (doc *docReader) root(field *Field, said string) {
	match := rootSaid.FindStringSubmatch(said)
	if match == nil || field.BodyScope != "" {
		return
	}
	words := strings.Fields(strings.ToLower(match[1]))
	for n := len(words); n > 0; n-- {
		if scope := strings.Join(words[:n], "_"); doc.vocab.IsScopeType(scope) {
			field.BodyScope = scope
			return
		}
	}
}

// value reads the type a key line writes after its key into the position, and gives back its prose:
// its comment, or the sentence written as its value.
func (doc *docReader) value(field *Field, line docLine) string {
	switch {
	case line.typ != "":
		doc.typed(field, line.typ)
	case placeholder.MatchString(line.rest):
		doc.typed(field, line.rest)
	case strings.HasPrefix(line.rest, "{") && strings.Contains(line.rest, "}"):
		return joinProse([]string{line.rest[strings.LastIndexByte(line.rest, '}')+1:], line.note})
	case line.op == "=" && strings.ContainsAny(line.rest, " \t") && !strings.HasPrefix(line.rest, "{"):
		return joinProse([]string{line.rest, line.note})
	}
	return line.note
}

// typed reads a type the doc names into the position, and reports whether it named one: a type
// word, or a kind named by its folder (<trait>, trait_key, geographical region). A path is none.
func (doc *docReader) typed(field *Field, text string) bool {
	if first, _, found := strings.Cut(strings.TrimPrefix(strings.ToLower(text), "/"), "/"); found && doc.known.roots[first] {
		return false
	}
	stated := typeWords(text)
	if !stated.stated {
		if kind := doc.kindOf(text); kind != "" {
			stated.accept, stated.stated = Accept{Type: TypeRef, Kind: kind, Origin: Declared}, true
		}
	}
	if stated.body != 0 {
		field.Class, field.Body = ClassBody, field.Body|stated.body
	}
	if stated.accept.Type != TypeNone && !slices.ContainsFunc(field.Accepts, func(accept Accept) bool {
		return accept.Type == stated.accept.Type && accept.Kind == stated.accept.Kind
	}) {
		field.Accepts = append(field.Accepts, stated.accept)
	}
	return stated.stated
}

// kindOf is the folder a doc's word for a kind names: trait, trait_key, <traits>, geographical
// region, law key; one folder of the install by its last name, singular or plural, or none.
func (doc *docReader) kindOf(text string) string {
	word := strings.ReplaceAll(strings.ToLower(strings.Trim(strings.TrimSpace(text), "<>")), " ", "_")
	for _, suffix := range []string{"_key", "_id", "_name", "_tag"} {
		word = strings.TrimSuffix(word, suffix)
	}
	if !jomini.IsLowerName(word) {
		return ""
	}
	candidates := []string{word, word + "s", word + "es", strings.TrimSuffix(word, "_type") + "_types"}
	if stem, ok := strings.CutSuffix(word, "y"); ok {
		candidates = append(candidates, stem+"ies")
	}
	for _, candidate := range candidates {
		if folders := doc.known.bySegment[candidate]; len(folders) == 1 {
			return folders[0]
		}
	}
	return ""
}

// stated is what a doc's words for a type say: what a body holds, or what a value is.
type stated struct {
	body   BodyClass
	accept Accept
	stated bool
}

// typeSpellings are the words the docs write for a type, each with what it says.
var (
	statedTrigger, statedEffect, statedModifier = stated{body: BodyTrigger, stated: true}, stated{body: BodyEffect, stated: true}, stated{body: BodyModifier, stated: true}
	statedMath                                  = stated{body: BodyMath, accept: Accept{Type: TypeNumber, Origin: Declared}, stated: true}
	statedBool, statedNumber, statedString      = stated{accept: Accept{Type: TypeBool, Origin: Declared}, stated: true},
		stated{accept: Accept{Type: TypeNumber, Origin: Declared}, stated: true}, stated{accept: Accept{Type: TypeString, Origin: Declared}, stated: true}
	statedLoc, statedDate, statedFile = stated{accept: Accept{Type: TypeLoc, Origin: Declared}, stated: true},
		stated{accept: Accept{Type: TypeDate, Origin: Declared}, stated: true}, stated{accept: Accept{Type: TypeFile, Origin: Declared}, stated: true}
	statedEnum    = stated{accept: Accept{Type: TypeEnum, Origin: Declared}, stated: true}
	typeSpellings = map[string]stated{
		"trigger": statedTrigger, "triggers": statedTrigger, "jomini trigger": statedTrigger, "trigger definition": statedTrigger,
		"condition": statedTrigger, "conditions": statedTrigger,
		"effect": statedEffect, "effects": statedEffect, "jomini effect": statedEffect, "effect definition": statedEffect,
		"modifier": statedModifier, "modifiers": statedModifier, "modifier definition": statedModifier,
		"script value": statedMath, "scripted value": statedMath, "script_value": statedMath, "scriptvalue": statedMath, "scripted_value": statedMath,
		"scripted math": statedMath, "scripted maths": statedMath, "script math": statedMath, "mtth": statedMath, "math": statedMath,
		"scripted value int32": statedMath, "script value int": statedMath, "int/script value": statedMath, "int/scripted value": statedMath,
		"float/scripted value": statedMath, "float script value": statedMath, "script value fixed point": statedMath, "days/scripted value": statedMath,
		"bool": statedBool, "boolean": statedBool, "yes/no": statedBool, "yes|no": statedBool,
		"int": statedNumber, "int32": statedNumber, "int64": statedNumber, "integer": statedNumber, "float": statedNumber, "double": statedNumber,
		"num": statedNumber, "number": statedNumber, "fixed_point": statedNumber, "fixed point": statedNumber, "fixedpoint": statedNumber,
		"cfixedpoint": statedNumber, "decimal": statedNumber, "percentage": statedNumber, "value": statedNumber,
		"string": statedString, "text": statedString,
		"loc key": statedLoc, "localization key": statedLoc, "localisation key": statedLoc, "loc_key": statedLoc, "localization_key": statedLoc,
		"localisation_key": statedLoc, "dynamic description": statedLoc, "dynamic_description": statedLoc, "dynamic desc": statedLoc,
		"date": statedDate, "path": statedFile, "file": statedFile, "file path": statedFile, "texture path": statedFile, "enum": statedEnum,
	}
)

// typeWords reads the words a doc writes for a type: a spelling of the table, a/b/c or a|b|c (open
// where it ends in etc), list of X. "optional" and the like are no part of it.
func typeWords(text string) stated {
	var parts []string
	for part := range strings.SplitSeq(strings.ToLower(strings.Trim(strings.TrimSpace(text), "<>{}[] ")), ",") {
		part = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(part), "optional "), "required ")
		if !slices.Contains([]string{"optional", "situational", "required", "opt", "mandatory"}, part) {
			parts = append(parts, part)
		}
	}
	word := strings.Join(parts, ", ")
	for _, plural := range []string{"array of ", "list of "} {
		if rest, ok := strings.CutPrefix(word, plural); ok {
			word = strings.TrimSuffix(rest, "s")
		}
	}
	if spelled, ok := typeSpellings[word]; ok {
		return spelled
	}

	// a set of words, a/b/c or a|b|c: lower-case words, two or more, or one where the last is etc
	words := strings.FieldsFunc(word, func(char rune) bool { return char == '/' || char == '|' })
	for i := range words {
		words[i] = strings.TrimSpace(words[i])
	}
	open := len(words) > 0 && slices.Contains([]string{"etc", "etc.", "..."}, words[len(words)-1])
	if open {
		words = words[:len(words)-1]
	}
	if len(words) < 2 && !(open && len(words) == 1) || slices.ContainsFunc(words, func(word string) bool { return !jomini.IsLowerName(word) }) {
		return stated{}
	}
	enum := stated{accept: Accept{Type: TypeEnum, Enum: words, Origin: Declared}, stated: true}
	if open {
		enum.accept.Origin = Inferred
	}
	return enum
}

// joinProse joins lines with a space and paragraphs, marked by an empty line, with a blank line.
func joinProse(lines []string) string {
	var out strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "" && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n"):
			out.WriteString("\n\n")
		case line == "":
		case out.Len() > 0 && !strings.HasSuffix(out.String(), "\n"):
			out.WriteString(" " + line)
		default:
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}
