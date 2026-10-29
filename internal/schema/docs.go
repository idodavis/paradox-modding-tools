package schema

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A docReader reads one doc file line by line into the fields it describes. It keeps what a line tells the lines
// after it.
type docReader struct {
	schema  *Schema
	fields  map[string]*Field
	current *Field   // the field the text attaches to; nil before the first
	headed  bool     // current came from a header and no statement has followed
	header  string   // the key the last header wrote, while no statement of it has followed; "" none
	above   []string // the comment lines directly above the next statement
	list    string   // what a label opened for the current field: choices, scopes, or ""
	keys    []string // the key of each block the line is in, "" for the example's entry and a key that stands for names
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
	statementLine   = regexp.MustCompile(`^(<[^>]+>|\$\w+\$|\w+:[\w@.]+|\w+(?:/\w+)+|[A-Za-z_@][A-Za-z0-9_.'-]*|\d[0-9.]*)\s*(?:\?=|==|<=|>=|=|<|>)\s*(.*)$`)
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

// buildDocs reads the docs in the given listing into the databases they describe. A doc is an .info, readme or .md
// file, or a .txt file named like one, that belongs to one of the given database folders. A later doc only fills in
// what an earlier one left out. It returns an error if a doc cannot be read.
func (schema *Schema) buildDocs(listing []files.File, folders []string) error {
	for _, file := range listing {
		if err := schema.buildDoc(file, folders); err != nil {
			return err
		}
	}
	return nil
}

// buildDoc reads one file into the database it describes, if it is a doc of one. It returns an error if the file
// cannot be read.
func (schema *Schema) buildDoc(file files.File, folders []string) error {
	label, ok := findDocDatabase(file.Rel, folders)
	database := schema.Databases[label]
	if !ok || database == nil {
		return nil
	}
	text, err := files.ReadText(file.Path)
	if err != nil {
		return err
	}
	if reader, ok := schema.parseDoc(text.Text, file.Rel, database.Row); ok {
		reader.build(database)
	}
	return nil
}

// findDocDatabase returns the database that a doc file describes. It is the database of the folder the file sits in,
// else the one that the file's own name gives. It returns false if the file is no doc or describes no database.
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

// parseDoc returns a reader that has read one doc file of the given database. It returns false for a .txt file that is
// data, not prose. Such a file has more script lines than prose lines.
func (schema *Schema) parseDoc(text, rel string, row game.Database) (*docReader, bool) {
	reader := &docReader{schema: schema, fields: map[string]*Field{}}
	// a definition named by its file is the whole file, so the doc is read as already inside it
	if row.NamedBy == "file" {
		reader.keys = []string{""}
	}
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

// build gives a database what one doc file states: its blurb, its root scope and the claims of each field. It does
// not replace a claim that is already stated. A key written at the top of a definition counts as documented. A key
// inside a block that the docs give a shape is a sample, such as `debug_only = yes` in a trigger, and makes no field.
func (reader *docReader) build(database *Database) {
	definition := database.Definition
	if blurb := strings.Join(reader.blurb, " "); blurb != "" {
		definition.Blurb.resolve(blurb, game.SourceDocs)
	}
	if reader.scope != "" {
		definition.Scope.resolve(reader.scope, game.SourceDocs)
	}
	for key, docField := range reader.fields {
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
		field.take(docField.Claims)
	}
}

// isSample reports whether the given key sits inside a block that the docs give a shape. Such a block holds commands,
// not fields.
func (reader *docReader) isSample(key string) bool {
	steps := strings.Split(key, "/")
	for end := 1; end < len(steps); end++ {
		if holder := reader.fields[strings.Join(steps[:end], "/")]; holder != nil && holder.Shape.IsStated() {
			return true
		}
	}
	return false
}

// readLine reads one line of a doc by the pattern it matches. A bullet that writes a statement counts as that
// statement. The flag says whether markdown makes the line a heading.
func (reader *docReader) readLine(raw string, heading bool) {
	line := strings.TrimSpace(raw)
	text := strings.TrimSpace(hashRun.ReplaceAllString(line, ""))
	code, comment := splitComment(text)
	// Prose is not valid script, so a statement is matched by pattern, not parsed.
	statement := statementLine.FindStringSubmatch(code)
	if bullet := bulletLine.FindStringSubmatch(code); statement == nil && bullet != nil {
		statement = statementLine.FindStringSubmatch(bullet[1])
	}
	isStatement := statement != nil && !isFact(statement[1]) && !isNote(statement[1], statement[2])
	opens := strings.Count(code, "{") - strings.Count(code, "}")
	reader.countLine(line, isStatement || braceLine.MatchString(code))
	switch {
	case line == "":
		reader.above, reader.list = nil, ""
		reader.ended = reader.ended || len(reader.blurb) > 0
	case text == "":
	case strings.HasPrefix(line, "#") && briefLine.MatchString(text):
		brief := briefLine.FindStringSubmatch(text)
		reader.readHeader(brief[1], brief[2])
	case isStatement:
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

// splitValue splits what a doc writes after a key into the value and the text that describes it. A block, a quoted
// text or a single term is all value. Otherwise the value is a placeholder in angle brackets or the first term, and
// the rest describes it.
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

// isFact reports whether the given key states a fact, the root scope or a default, rather than naming a field.
func isFact(key string) bool {
	lower := strings.ToLower(key)
	return lower == game.ScopeLinks.Root || lower == "default"
}

// isNote reports whether a statement is a note in prose rather than script, as `scope:actor = the actor` and
// `days/weeks/months/years = X` are: a key with a prefix, alternatives or a parameter, whose value is no block. Such a
// key that opens a block is script.
func isNote(key, value string) bool {
	return strings.ContainsAny(key, ":/$") && !strings.HasPrefix(value, "{")
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

// countLine counts a line toward telling a data file from a doc. The flag says whether the line writes a statement or a
// brace. Such a line that is not a comment is script. Any other line with text is prose.
func (reader *docReader) countLine(line string, isCode bool) {
	switch {
	case line == "":
	case isCode && !strings.HasPrefix(line, "#"):
		reader.script++
	default:
		reader.prose++
	}
}

// readHeader reads a `### Brief:` header. It gives the key the header opens and the type in parentheses, which the
// statement of that key then takes.
func (reader *docReader) readHeader(key, text string) {
	reader.closeHeader()
	field := &Field{}
	resolveType(field, text)
	reader.current, reader.headed, reader.header, reader.above, reader.list = field, true, key, nil, ""
}

// readField reads a `key: text` line or a keyed bullet. The key is a field at the top of the definition, and the text
// describes it.
func (reader *docReader) readField(key, text string) {
	field := reader.field(key)
	reader.resolveText(field, text)
	reader.current, reader.headed = field, false
}

// field returns the field at the given path and makes it if it does not exist. It marks the doc as begun.
func (reader *docReader) field(key string) *Field {
	reader.started = true
	if reader.fields[key] == nil {
		reader.fields[key] = &Field{}
	}
	return reader.fields[key]
}

// readStatement reads a line of script. The block at the top of the example is the entry and has no key, and neither
// does a key that stands for names. Any other key is a field under the entry. The comment after it and the comments
// above it are its text, its value is its type, and a header just before it gives its claims.
func (reader *docReader) readStatement(key, value, text string, opens int) {
	reader.started = true
	entry := len(reader.keys) == 0 && (opens > 0 || strings.HasPrefix(value, "{"))
	keyed := key != "" && !entry && !isStandIn(key)
	step := key
	if game.IsNumberKey(step) || step == game.DocDateKey {
		step = game.NumberKey
	}
	if keyed {
		if reader.header != key {
			reader.closeHeader()
		}
		field := reader.field(path.Join(append(slices.DeleteFunc(slices.Clone(reader.keys), isBlank), step)...))
		if reader.header == key {
			field.take(reader.current.Claims)
			reader.header = ""
		}
		reader.resolveText(field, strings.Join(append(reader.above, text), " "))
		reader.resolveValue(field, value)
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

// closeHeader gives the last header's facts to its key at the top of the definition, if no statement of that key
// followed it. It is called when a statement of another key or the end of the doc comes first.
func (reader *docReader) closeHeader() {
	if reader.header != "" {
		reader.field(reader.header).take(reader.current.Claims)
		reader.header = ""
	}
}

// isStandIn reports whether a key stands for names rather than naming a field: a placeholder such as <key>, a key with
// a prefix such as scope:actor, alternatives such as trigger/effect, or a parameter such as $target$.
func isStandIn(key string) bool {
	return strings.HasPrefix(key, "<") || strings.ContainsAny(key, ":/$")
}

// isBlank reports whether a key is empty. The example's entry and a key that stands for names have none, so they are
// on no path.
func isBlank(key string) bool { return key == "" }

// readLabel reads a `Label: text` line. It gives the field above a default or a scope, opens a list of its values or
// scopes for the lines after it, or is prose.
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

// readBullet reads a bullet. It is a value or a scope of the field above if a label opened a list, a field if it has a
// snake-case key, and prose otherwise.
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

// readProse reads a line of prose. Before anything else it is the database's first paragraph, and it may name the
// root scope. After a header it is the text of that field. Otherwise it joins the comments above the next statement.
func (reader *docReader) readProse(text string) {
	if !reader.started && !reader.ended {
		reader.blurb = append(reader.blurb, text)
	}
	if rootMatch := rootPhrase.FindStringSubmatch(text); rootMatch != nil && reader.current == nil {
		reader.scope = cmp.Or(reader.scope, reader.schema.resolveScopePhrase(rootMatch[1]))
	}
	if reader.headed && reader.current != nil {
		reader.resolveText(reader.current, text)
		return
	}
	reader.above = append(reader.above, text)
}

// resolveText reads the text written about a field. A placeholder at its start is the field's value. The rest is the
// blurb, and any type, requirement, default and scope it states. A start such as "Effect run when…" or "The
// localization key, …" gives the type it names. A type in parentheses or brackets counts if the docs know the term.
func (reader *docReader) resolveText(field *Field, text string) {
	text = strings.TrimSpace(text)
	if value, rest := splitValue(text); strings.HasPrefix(value, "<") {
		reader.resolveValue(field, value)
		text = rest
	}
	if text == "" {
		return
	}
	field.Blurb = Claim[string]{Value: strings.TrimSpace(field.Blurb.Value + " " + text), Source: game.SourceDocs}
	if opening, ok := findDocType(text); ok {
		resolveDocType(field, opening, game.SourceDocs)
	}
	if parens := typeParens.FindStringSubmatch(text); parens != nil {
		resolveType(field, parens[1]+","+parens[2])
	}
	if brackets := typeBrackets.FindStringSubmatch(text); brackets != nil {
		resolveDocType(field, brackets[1], game.SourceDocs)
	}
	resolveRequired(field, text)
	if defaultMatch := defaultPhrase.FindStringSubmatch(text); defaultMatch != nil &&
		!slices.Contains(defaultStops, strings.ToLower(defaultMatch[1])) {
		resolveDefault(field, defaultMatch[1])
	}
	if rootMatch := rootPhrase.FindStringSubmatch(text); rootMatch != nil {
		reader.resolveScope(field, rootMatch[1])
	}
}

// findDocType returns the longest type term that opens the text, after an article. It returns false if no term opens
// the text, or if the term names no shape, no yes-or-no type and no database.
func findDocType(text string) (string, bool) {
	words := strings.Fields(text)
	for position, word := range words {
		words[position] = strings.Trim(word, ".,:;()")
	}
	if len(words) > 1 && slices.Contains([]string{"a", "an", "the"}, strings.ToLower(words[0])) {
		words = words[1:]
	}
	for length := len(words); length > 0; length-- {
		phrase := strings.Join(words[:length], " ")
		if row, ok := game.DocTypes[ToPhrase(phrase)]; ok {
			return phrase, row.Shape != "" || row.Value == string(ValueBool) || row.Database != ""
		}
	}
	return "", false
}

// resolveType reads a type written in parentheses. The terms optional, required and mandatory, inside it or after a
// comma, say whether the field is required. The rest is the type.
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
	resolveDocType(field, strings.Join(typed, " "), game.SourceDocs)
	for _, extra := range parts[1:] {
		resolveRequired(field, extra)
	}
}

// resolveDocType gives a field the claims that a type term means: the shape of its block, and its value type with the
// database a reference names. The given source states them. An unknown term makes no claim.
func resolveDocType(field *Field, term string, source game.Source) {
	row := game.DocTypes[ToPhrase(term)]
	if row.Shape != "" {
		field.Shape.resolve(row.Shape, source)
	}
	if row.Value != "" {
		field.Value.resolve(Expected{Type: ValueType(row.Value), Database: row.Database, Values: row.Values}, source)
	}
}

// resolveRequired reads from the text whether a field is required. A condition makes it conditional, and the terms
// optional, required and mandatory make it optional or always required. The first one stated is kept.
func resolveRequired(field *Field, text string) {
	term := strings.ToLower(requiredLevel.FindString(text))
	switch {
	case conditionPhrase.MatchString(text):
		field.Required.resolve("conditional", game.SourceDocs)
	case term == "optional":
		field.Required.resolve("optional", game.SourceDocs)
	case term != "":
		field.Required.resolve("always", game.SourceDocs)
	}
}

// resolveDefault gives a field the default the docs write. It does nothing for an empty default or one already given.
func resolveDefault(field *Field, value string) {
	if value != "" {
		field.Default.resolve(value, game.SourceDocs)
	}
}

// resolveScope gives a field the scope type that the given phrase names, if it names one.
func (reader *docReader) resolveScope(field *Field, phrase string) {
	if scope := reader.schema.resolveScopePhrase(phrase); scope != "" {
		field.Scope.resolve(scope, game.SourceDocs)
	}
}

// resolveValue reads a field's type from the value its example writes. The value may be a placeholder, alternatives,
// a type term, a sample key such as innovation_key that names the database of its words, or a sample value.
func (reader *docReader) resolveValue(field *Field, value string) {
	inner, placeholder := strings.CutPrefix(value, "<")
	inner = strings.ToLower(strings.TrimSuffix(inner, ">"))
	_, known := game.DocTypes[ToPhrase(inner)]
	_, date := jomini.ParseDate(value)
	words, keyed := strings.CutSuffix(inner, "_key")
	database, named := reader.schema.ResolveTypeDatabase(words)
	switch {
	case value == "":
	case strings.HasPrefix(value, "{"):
		resolveDocType(field, "block", game.SourceDocs)
	case alternatives.MatchString(inner):
		// Alternatives such as number/scripted name types, not values, so they state no choices.
		parts := strings.FieldsFunc(inner, func(r rune) bool { return r == '/' || r == '|' })
		if !slices.ContainsFunc(parts, func(part string) bool { _, ok := game.DocTypes[ToPhrase(part)]; return ok }) {
			resolveChoices(field, parts)
		}
	case keyed && named && !known:
		field.Value.resolve(Expected{Type: ValueReference, Database: database}, game.SourceDocs)
	case placeholder || known:
		resolveDocType(field, inner, game.SourceDocs)
	case jomini.IsNumber(value):
		resolveDocType(field, "number", game.SourceDocs)
	case slices.Contains(game.Bools, value):
		resolveDocType(field, "bool", game.SourceDocs)
	case date:
		resolveDocType(field, "date", game.SourceDocs)
	case strings.HasPrefix(value, `"`):
		resolveDocType(field, "string", game.SourceDocs)
	}
}

// resolveChoices adds the given values to the choices of a field. They outrank a type the same doc gave. The one
// marked as the default is the default. If the choices are only the two yes-or-no terms, the field is a bool.
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

// findTerm returns the first term of the text without quotes and punctuation, or "" if it has none.
func findTerm(text string) string {
	terms := strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'(),;`, r) })
	if len(terms) == 0 {
		return ""
	}
	return terms[0]
}

// resolveScopePhrase returns the scope type that a phrase names, such as "interest group" or "owning country or no
// scope". It is the longest run of the phrase's terms that is a scope type the dumps declare. It returns "" if there
// is none.
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

// buildDocBlurbs gives each field that has no blurb the blurb the docs write for the same key elsewhere in its
// database. Only the blurb is shared, nothing else.
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
