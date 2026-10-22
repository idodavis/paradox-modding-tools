package schema

import (
	"cmp"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A docReader reads the lines of one doc file in order into fields of their own, by their paths under the
// definition. It keeps the field the text attaches to, the comment lines above the next statement, the list a label
// opened and the brace depth.
type docReader struct {
	schema  *Schema
	fields  map[string]*Field
	current *Field   // the field the text attaches to; nil before the first
	headed  bool     // current came from a header and no statement has followed
	header  string   // the key the last header wrote, while no statement of it has followed; "" none
	above   []string // the comment lines directly above the next statement
	list    string   // what a label opened for the current field: choices, scopes, or ""
	keys    []string // the key of each block the line is in, "" for the example's entry and a placeholder
	blurb   []string // the first paragraph of prose before the first statement
	scope   string   // the scope type of the root the prose before the first field gives
	started bool     // a statement, header or field has been read
	ended   bool     // the first paragraph has ended
	script  int      // the live script lines, which tell a data file from a doc
	prose   int      // the other lines with text
}

// docMarkers are the terms in a .txt file's name that mark it as a doc.
var docMarkers = []string{"readme", "info", "example", "examples", "template", "help", "documentation"}

// defaultStops are the terms after "default" that give no default.
var defaultStops = []string{"value", "values", "is", "to", "the", "a", "an", "by", "behavior", "option", "one"}

// The patterns of the lines a doc is read by, and of the facts its text states.
var (
	hashRun         = regexp.MustCompile(`^(?:#+\s*)+`)
	headingLine     = regexp.MustCompile(`^(=+ *[^=]* *=+|[-=#*_|]{3,}|[A-Z][A-Z0-9 _/:-]{2,}|-[a-z_]+(=<[^>]*>)?)$`)
	briefLine       = regexp.MustCompile(`(?i)^brief\s*[:=]?\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?:-[^(]*)?(?:\(\s*([^)]*)\))?`)
	statementLine   = regexp.MustCompile(`^(<[^>]+>|[A-Za-z_@][A-Za-z0-9_.'-]*|\d[0-9.]*)\s*(?:\?=|==|<=|>=|=|<|>)\s*(.*)$`)
	braceLine       = regexp.MustCompile(`^[{}. ]+$`)
	labelLine       = regexp.MustCompile(`^([A-Z][A-Za-z ]{1,24}|format):\s*(.*)$`)
	fieldLine       = regexp.MustCompile(`^([a-z_][a-z0-9_]*):\s+(\S.*)$`)
	bulletLine      = regexp.MustCompile(`^[-*]\s+(.*)$`)
	bulletKey       = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*)\s*(?:[:-]\s+(.*))?$`)
	scopeBullet     = regexp.MustCompile(`^(?:root|scope:\w+)\s*\(\s*([A-Za-z_]+)\s*\)`)
	typeParens      = regexp.MustCompile(`\(\s*([a-z][a-z0-9 _]*?)\s*(?:,\s*([a-z ,]+?)\s*)?\)`)
	typeBrackets    = regexp.MustCompile(`(?i)\[(string|boolean|bool|integer|int|float|number|trigger|effect|date)\]`)
	requiredLevel   = regexp.MustCompile(`(?i)\b(optional|required|mandatory)\b`)
	conditionPhrase = regexp.MustCompile(`(?i)\b(?:only (?:used|needed|valid|applies|relevant) (?:when|if|for)|required (?:if|when)|ignored (?:if|when|unless)|needed (?:if|when))\b`)
	defaultPhrase   = regexp.MustCompile(`(?i)(?:^|[\s(\[,])default(?:s\s+to|\s+is|\s+value(?:\s+is)?|\s*[:=]|\s+)\s*["']?([A-Za-z0-9_.+-]+)`)
	rootPhrase      = regexp.MustCompile(`(?i)\broot(?:\s+scope)?\s*(?:=|:|-|\bis\b)\s*([a-z_][a-z_ ]*)`)
	choiceLabel     = regexp.MustCompile(`(?i)^(?:possible|valid|available|allowed|supported) \w+$`)
	scopeLabel      = regexp.MustCompile(`(?i)^(?:supported |available )?scopes?$`)
	alternatives    = regexp.MustCompile(`^([A-Za-z0-9_.+-]+(?:[/|][A-Za-z0-9_.+-]+)+)$`)
)

// buildDocs reads the game's prose files under an install into the databases they describe: every .info, readme and
// .md file whose folder or file name gives one of the database folders. A file read earlier keeps what it stated,
// and a later one fills in what it left out. Script fragments inside the prose are read with the docs' own patterns
// rather than the parser, because prose is not valid script. It returns an error if a doc cannot be read.
func (schema *Schema) buildDocs(installPath string, folders []string) error {
	state := SourceState{Origin: originNone}
	for _, root := range game.InstallRoots {
		dir := filepath.Join(installPath, root)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		listing, err := files.Walk(files.Root{Dir: dir, Origin: game.OriginVanilla}, nil)
		if err != nil {
			return err
		}
		for _, file := range listing {
			built, err := schema.buildDoc(file, folders)
			if err != nil {
				return err
			}
			if built {
				state.Origin = originInstall
			}
		}
	}
	schema.Sources[game.SourceDocs] = state
	return nil
}

// buildDoc reads one file into the database it describes, and reports whether it was a doc of one. It returns an
// error if the file cannot be read.
func (schema *Schema) buildDoc(file files.File, folders []string) (bool, error) {
	label, ok := findDocDatabase(file.Rel, folders)
	database := schema.Databases[label]
	if !ok || database == nil {
		return false, nil
	}
	text, err := files.ReadText(file.Path)
	if err != nil {
		return false, err
	}
	reader, ok := schema.parseDoc(text.Text, file.Rel)
	if ok {
		reader.build(database)
	}
	return ok, nil
}

// findDocDatabase returns the database that a doc file describes: the one whose folder the file sits in or ends
// with, else the one its own file name gives. It returns false for a file that is no doc or describes none.
func findDocDatabase(rel string, folders []string) (string, bool) {
	base := strings.ToLower(path.Base(rel))
	stem := strings.TrimSuffix(base, path.Ext(base))
	terms := strings.FieldsFunc(stem, func(r rune) bool { return !unicode.IsLetter(r) })
	marked := slices.ContainsFunc(terms, func(term string) bool { return slices.Contains(docMarkers, term) })
	if path.Ext(base) != ".info" && path.Ext(base) != ".md" && !strings.HasPrefix(base, "readme") &&
		(path.Ext(base) != ".txt" || !marked) {
		return "", false
	}
	dir := path.Dir(rel)
	for _, candidate := range []string{dir, dir + "/" + strings.TrimLeft(stem, "_")} {
		matches := slices.DeleteFunc(slices.Clone(folders), func(folder string) bool {
			return candidate != folder && !strings.HasSuffix(candidate, "/"+folder)
		})
		if len(matches) > 0 {
			longest := slices.MaxFunc(matches, func(a, b string) int { return len(a) - len(b) })
			return game.ToDatabase(longest), true
		}
	}
	return "", false
}

// parseDoc reads one doc file into a reader of its own. It returns false for a .txt that is data rather than prose:
// one whose live script lines outnumber its other lines that are not blank. In a markdown file a line opening with a
// hash at the margin is a heading unless it writes a statement.
func (schema *Schema) parseDoc(text, rel string) (*docReader, bool) {
	reader := &docReader{schema: schema, fields: map[string]*Field{}}
	md := strings.EqualFold(path.Ext(rel), ".md")
	for raw := range strings.Lines(text) {
		reader.readLine(raw, md && strings.HasPrefix(raw, "#"))
	}
	reader.closeHeader()
	if strings.EqualFold(path.Ext(rel), ".txt") && (reader.prose == 0 || reader.prose < reader.script) {
		return nil, false
	}
	return reader, true
}

// build gives a database what one doc file states: its blurb and root scope, and each field's claims at its path
// where nothing stated them before. A key the docs write at the top of a definition is documented, and a field they
// write has the docs as its source. A key inside a block the docs give a shape is a sample of what the block holds,
// as `debug_only = yes` inside a trigger, and builds no field.
func (reader *docReader) build(database *Database) {
	definition := database.Definition
	if blurb := strings.Join(reader.blurb, " "); blurb != "" {
		definition.Blurb.resolve(blurb, game.SourceDocs)
	}
	if reader.scope != "" {
		definition.Scope.resolve(reader.scope, game.SourceDocs)
	}
	for key, written := range reader.fields {
		if reader.isSample(key) {
			continue
		}
		field := definition
		for step := range strings.SplitSeq(key, "/") {
			field = field.buildChild(step)
			if field.Source == game.SourceNone {
				field.Source = game.SourceDocs
			}
		}
		if !strings.Contains(key, "/") {
			database.Documented[key] = true
		}
		field.take(written.Claims)
	}
}

// isSample reports whether a key the docs write sits inside a block they give a shape, which holds commands rather
// than fields.
func (reader *docReader) isSample(key string) bool {
	steps := strings.Split(key, "/")
	for end := 1; end < len(steps); end++ {
		if holder := reader.fields[strings.Join(steps[:end], "/")]; holder != nil && holder.Shape.IsStated() {
			return true
		}
	}
	return false
}

// readLine reads one line by the pattern it matches, given the line as written and whether markdown makes it a
// heading. Its leading hashes are cut first, and a bullet that writes a statement is that statement.
func (reader *docReader) readLine(raw string, heading bool) {
	line := strings.TrimSpace(raw)
	text := strings.TrimSpace(hashRun.ReplaceAllString(line, ""))
	code, comment := splitComment(text)
	statement := statementLine.FindStringSubmatch(code)
	if bullet := bulletLine.FindStringSubmatch(code); statement == nil && bullet != nil {
		statement = statementLine.FindStringSubmatch(bullet[1])
	}
	written := statement != nil && !isFact(statement[1])
	opens := strings.Count(code, "{") - strings.Count(code, "}")
	reader.countLine(line, written || braceLine.MatchString(code))
	switch {
	case line == "":
		reader.above, reader.list = nil, ""
		reader.ended = reader.ended || len(reader.blurb) > 0
	case text == "":
	case strings.HasPrefix(line, "#") && briefLine.MatchString(text):
		brief := briefLine.FindStringSubmatch(text)
		reader.readHeader(brief[1], brief[2])
	case written:
		value, rest := splitValue(strings.TrimSpace(statement[2]))
		reader.readStatement(statement[1], value, strings.TrimSpace(rest+" "+comment), opens)
	case braceLine.MatchString(code):
		reader.readStatement("", "", "", opens)
	case heading || headingLine.MatchString(text):
		reader.above, reader.list = nil, ""
	case strings.HasPrefix(text, "scope:"):
		reader.readProse(text)
	case labelLine.MatchString(text):
		label := labelLine.FindStringSubmatch(text)
		reader.readLabel(label[1], label[2])
	case fieldLine.MatchString(text):
		field := fieldLine.FindStringSubmatch(text)
		reader.readField(field[1], field[2])
		reader.above = nil
	case bulletLine.MatchString(text):
		reader.readBullet(bulletLine.FindStringSubmatch(text)[1])
	default:
		reader.readProse(text)
	}
}

// splitValue splits the value a doc writes after a key into the value and the text that describes it: a block, a
// quoted text and a single term are the value whole; a placeholder in angle brackets, or the first term, is the value
// of a longer text, and the rest describes it.
func splitValue(value string) (string, string) {
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, `"`) {
		return value, ""
	}
	if strings.HasPrefix(value, "<") {
		if end := strings.Index(value, ">"); end > 0 {
			return value[:end+1], strings.TrimSpace(value[end+1:])
		}
	}
	first, rest, _ := strings.Cut(value, " ")
	return first, strings.TrimSpace(rest)
}

// isFact reports whether a statement's key is a term the docs state a fact with rather than a field: the root scope
// or a default.
func isFact(key string) bool {
	lower := strings.ToLower(key)
	return lower == game.ScopeLinks.Root || lower == "default"
}

// splitComment splits a line into its code and the comment after a `#` that is not inside quotes.
func splitComment(line string) (code, comment string) {
	quoted := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			quoted = !quoted
		case '#':
			if !quoted {
				return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
			}
		}
	}
	return line, ""
}

// countLine counts a line toward telling a data file from a doc, given whether it writes a statement or a brace: such
// a line not written as a comment is live script, and any other line that is not blank is not.
func (reader *docReader) countLine(line string, written bool) {
	switch {
	case line == "":
	case written && !strings.HasPrefix(line, "#"):
		reader.script++
	default:
		reader.prose++
	}
}

// readHeader reads a field's `### Brief:` header: the key it opens and the type in its parentheses, which the
// statement of that key takes.
func (reader *docReader) readHeader(key, text string) {
	reader.closeHeader()
	field := &Field{}
	resolveType(field, text)
	reader.current, reader.headed, reader.header, reader.above, reader.list = field, true, key, nil, ""
}

// readField reads a `key: text` line or a keyed bullet: the field of the key at the top of the definition, with the
// text about it.
func (reader *docReader) readField(key, text string) {
	field := reader.field(key)
	reader.resolveText(field, text)
	reader.current, reader.headed = field, false
}

// field returns the field at a path, making it the first time. It marks the doc as begun.
func (reader *docReader) field(key string) *Field {
	reader.started = true
	if reader.fields[key] == nil {
		reader.fields[key] = &Field{}
	}
	return reader.fields[key]
}

// readStatement reads a line of script. The block at the top of the example is the entry and gives no key, as a
// placeholder key does not; every other key is a field at its path under the entry, with the comment after it and
// the comment run above it as its text, its value as its type, and the header that wrote its key just before it.
func (reader *docReader) readStatement(key, value, text string, opens int) {
	reader.started = true
	entry := len(reader.keys) == 0 && (opens > 0 || strings.HasPrefix(value, "{"))
	keyed := key != "" && !entry && !strings.HasPrefix(key, "<")
	step := key
	if game.IsNumberKey(step) {
		step = game.NumberKey
	}
	if keyed {
		field := reader.field(path.Join(append(slices.DeleteFunc(slices.Clone(reader.keys), isBlank), step)...))
		if reader.header == key {
			field.take(reader.current.Claims)
			reader.header = ""
		}
		reader.resolveText(field, strings.Join(append(reader.above, text), " "))
		resolveValue(field, value)
		reader.current, reader.list = field, ""
	}
	for range max(opens, 0) {
		if keyed {
			reader.keys, keyed = append(reader.keys, step), false
		} else {
			reader.keys = append(reader.keys, "")
		}
	}
	reader.keys = reader.keys[:max(len(reader.keys)+min(opens, 0), 0)]
	reader.above, reader.headed = nil, false
}

// closeHeader gives the key of the last header the facts it stated, at the top of the definition, where no statement
// of that key followed it.
func (reader *docReader) closeHeader() {
	if reader.header != "" {
		reader.field(reader.header).take(reader.current.Claims)
		reader.header = ""
	}
}

// isBlank reports whether a key is empty: the example's entry or a placeholder, which are on no field's path.
func isBlank(key string) bool { return key == "" }

// readLabel reads a `Label: text` line: a default or a scope of the field above, a list of its values or scopes
// that the lines after it fill, or prose.
func (reader *docReader) readLabel(title, text string) {
	label := strings.ToLower(title)
	switch {
	case reader.current == nil:
		reader.readProse(title + ": " + text)
	case label == "default":
		resolveDefault(reader.current, findTerm(text))
	case scopeLabel.MatchString(label):
		reader.list = "scopes"
		reader.resolveScope(reader.current, findTerm(text))
	case choiceLabel.MatchString(label):
		reader.list = "choices"
		resolveChoices(reader.current, strings.Split(text, ","))
	default:
		reader.readProse(title + ": " + text)
	}
}

// readBullet reads a bullet: a value or a scope of the field above where a label opened its list, a field where it
// writes one with a snake-case key, else prose.
func (reader *docReader) readBullet(bullet string) {
	key, text := "", bullet
	if keyed := bulletKey.FindStringSubmatch(bullet); keyed != nil {
		key, text = keyed[1], keyed[2]
	}
	switch {
	case reader.list == "choices" && reader.current != nil:
		resolveChoices(reader.current, []string{cmp.Or(key, text)})
	case reader.list == "scopes" && reader.current != nil:
		if scope := scopeBullet.FindStringSubmatch(key + text); scope != nil {
			reader.resolveScope(reader.current, scope[1])
		}
	case key != "" && text != "" && strings.ToLower(key) == key && !strings.Contains(key, "."):
		reader.readField(key, text)
	default:
		reader.readProse(strings.TrimSpace(key + " " + text))
	}
}

// readProse reads a line of prose: the database's first paragraph before anything else, and its root before the
// first field; the text of a field a header opened, and the run above the next statement.
func (reader *docReader) readProse(text string) {
	if !reader.started && !reader.ended {
		reader.blurb = append(reader.blurb, text)
	}
	if found := rootPhrase.FindStringSubmatch(text); found != nil && reader.current == nil {
		reader.scope = cmp.Or(reader.scope, reader.schema.resolveScopePhrase(found[1]))
	}
	if reader.headed && reader.current != nil {
		reader.resolveText(reader.current, text)
		return
	}
	reader.above = append(reader.above, text)
}

// resolveText reads the text written about a field: a placeholder opening it is the field's value, and the rest is
// its blurb and the type, requirement, default and scope it states. Text that opens with a shape, as in "Effect run
// when…", or with a yes-or-no type gives that type. A type in parentheses or brackets counts where the docs' type
// terms know it.
func (reader *docReader) resolveText(field *Field, text string) {
	text = strings.TrimSpace(text)
	if value, rest := splitValue(text); strings.HasPrefix(value, "<") {
		resolveValue(field, value)
		text = rest
	}
	if text == "" {
		return
	}
	field.Blurb = Claim[string]{Value: strings.TrimSpace(field.Blurb.Value + " " + text), Source: game.SourceDocs}
	opening := strings.Trim(strings.Fields(text)[0], ".,:;()")
	if row := game.DocTypes[toPhrase(opening)]; row.Shape != "" || row.Value == string(ValueBool) {
		resolveDocType(field, opening)
	}
	if parens := typeParens.FindStringSubmatch(text); parens != nil {
		resolveType(field, parens[1]+","+parens[2])
	}
	if brackets := typeBrackets.FindStringSubmatch(text); brackets != nil {
		resolveDocType(field, brackets[1])
	}
	resolveRequired(field, text)
	if found := defaultPhrase.FindStringSubmatch(text); found != nil && !slices.Contains(defaultStops, strings.ToLower(found[1])) {
		resolveDefault(field, found[1])
	}
	if found := rootPhrase.FindStringSubmatch(text); found != nil {
		reader.resolveScope(field, found[1])
	}
}

// resolveType reads a type as written in parentheses. The terms optional, required or mandatory, among it or after a
// comma, say whether the field is required; the rest is the type.
func resolveType(field *Field, text string) {
	parts := strings.Split(text, ",")
	var typed []string
	for term := range strings.FieldsSeq(parts[0]) {
		if requiredLevel.MatchString(term) {
			resolveRequired(field, term)
		} else {
			typed = append(typed, term)
		}
	}
	resolveDocType(field, strings.Join(typed, " "))
	for _, extra := range parts[1:] {
		resolveRequired(field, extra)
	}
}

// resolveDocType gives a field the claims that a docs' type term means, as its row in the docs' type terms gives
// them: the shape of its block, and the value type with the database a reference names. A term the docs' type terms
// lack makes no claim.
func resolveDocType(field *Field, term string) {
	row := game.DocTypes[toPhrase(term)]
	if row.Shape != "" {
		field.Shape.resolve(row.Shape, game.SourceDocs)
	}
	if row.Value != "" {
		field.Value.resolve(Expected{Type: ValueType(row.Value), Database: row.Database}, game.SourceDocs)
	}
}

// resolveRequired reads whether a field is required from its text: a condition, else the terms optional, required
// or mandatory. The first the docs state is kept.
func resolveRequired(field *Field, text string) {
	term := strings.ToLower(requiredLevel.FindString(text))
	switch {
	case conditionPhrase.MatchString(text):
		field.Required.resolve(Requirement{Level: "conditional"}, game.SourceDocs)
	case term == "optional":
		field.Required.resolve(Requirement{Level: "optional"}, game.SourceDocs)
	case term != "":
		field.Required.resolve(Requirement{Level: "always"}, game.SourceDocs)
	}
}

// resolveDefault gives a field the default the docs write, unless it is empty or the docs gave one before.
func resolveDefault(field *Field, value string) {
	if value != "" {
		field.Default.resolve(value, game.SourceDocs)
	}
}

// resolveScope gives a field the scope type the docs' phrase for it names, if any does.
func (reader *docReader) resolveScope(field *Field, phrase string) {
	if scope := reader.schema.resolveScopePhrase(phrase); scope != "" {
		field.Scope.resolve(scope, game.SourceDocs)
	}
}

// resolveValue reads a field's type from the value its example writes: a placeholder's terms, alternatives, a type
// term, or a sample value.
func resolveValue(field *Field, value string) {
	inner, placeholder := strings.CutPrefix(value, "<")
	inner = strings.ToLower(strings.TrimSuffix(inner, ">"))
	_, known := game.DocTypes[toPhrase(inner)]
	_, date := jomini.ParseDate(value)
	switch {
	case value == "":
	case strings.HasPrefix(value, "{"):
		resolveDocType(field, "block")
	case alternatives.MatchString(inner):
		resolveChoices(field, strings.FieldsFunc(inner, func(r rune) bool { return r == '/' || r == '|' }))
	case placeholder || known:
		resolveDocType(field, inner)
	case jomini.IsNumber(value), value == "x", value == "X", value == "N":
		resolveDocType(field, "number")
	case slices.Contains(game.Bools, value):
		resolveDocType(field, "bool")
	case date:
		resolveDocType(field, "date")
	case strings.HasPrefix(value, `"`):
		resolveDocType(field, "string")
	}
}

// resolveChoices adds the values a field may take, which outrank a type the same doc gave it. The one marked as the
// default is the default, and the two yes-or-no terms alone make the field a bool.
func resolveChoices(field *Field, values []string) {
	var choices []string
	if field.Value.Value.Type == ValueChoice {
		choices = field.Value.Value.Choices
	}
	for _, value := range values {
		term := findTerm(value)
		if term == "" {
			continue
		}
		if strings.Contains(strings.ToLower(value), "(default") {
			resolveDefault(field, term)
		}
		if !slices.Contains(choices, term) {
			choices = append(choices, term)
		}
	}
	if len(choices) == 0 {
		return
	}
	expected := Expected{Type: ValueChoice, Choices: choices}
	if slices.Equal(slices.Sorted(slices.Values(choices)), slices.Sorted(slices.Values(game.Bools))) {
		expected = Expected{Type: ValueBool}
	}
	field.Value = Claim[Expected]{Value: expected, Source: game.SourceDocs}
}

// findTerm returns the first term of a text, less the quotes and the punctuation around it, or "" for none.
func findTerm(text string) string {
	terms := strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'(),;`, r) })
	if len(terms) == 0 {
		return ""
	}
	return terms[0]
}

// resolveScopePhrase returns the scope type that the docs' phrase for a scope names, such as "interest group" or
// "owning country or no scope": the longest run of its terms that is a scope type the dumps declare. It returns ""
// if no run is one.
func (schema *Schema) resolveScopePhrase(phrase string) string {
	terms := strings.Fields(strings.ToLower(phrase))
	for length := len(terms); length > 0; length-- {
		for start := 0; start+length <= len(terms); start++ {
			if scope := strings.Join(terms[start:start+length], "_"); schema.Scopes[scope].Name != "" {
				return scope
			}
		}
	}
	return ""
}

// buildDocBlurbs gives every field of a database that no source explains the blurb the docs write for a key of the
// same name elsewhere in that database, as an example's made-up keys would otherwise keep the text from the real
// ones. Only the blurb is given: it explains a key and never fails a verdict.
func (schema *Schema) buildDocBlurbs() {
	for _, database := range schema.Databases {
		blurbs := map[string]string{}
		walkField(database.Definition, func(key string, field *Field) {
			if field.Blurb.Source == game.SourceDocs && blurbs[key] == "" {
				blurbs[key] = field.Blurb.Value
			}
		})
		walkField(database.Definition, func(key string, field *Field) {
			if blurb := blurbs[key]; blurb != "" && !field.Blurb.IsStated() {
				field.Blurb.resolve(blurb, game.SourceDocs)
			}
		})
	}
}
