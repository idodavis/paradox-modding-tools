// docs.go reads the doc files a game ships beside its script, as CK3's
// _traits.info, into the fields they describe.

package schema

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// docReader is the state of reading one doc file line by line.
type docReader struct {
	schema *lang.Schema

	// root holds the fields the doc writes at its top.
	root *lang.Field

	// blocks holds the field of each block open on the line being read.
	blocks []*lang.Field

	// current is the field the next sentences describe, nil before the first
	// field.
	current *lang.Field

	// isHeader is whether current came from a "### Brief:" header that no
	// statement or field has followed yet.
	isHeader bool

	// header is the key such a header names, until a statement follows it.
	header string

	// comments are the sentences since the last statement, which describe the
	// next one.
	comments []string

	// list is the kind of list the last label opened, which the items under
	// it belong to.
	list labelList

	// blurb holds the sentences before the first field, up to the first blank
	// line. They describe the database as a whole.
	blurb []string

	// hasFields is whether any field has been read. isBlurbOver is whether a
	// blank line has ended the opening sentences.
	hasFields   bool
	isBlurbOver bool

	// script and prose count the lines of script and of sentences, which tell
	// a data file from a doc.
	script int
	prose  int
}

// labelList is the kind of list a label line opens for the field above it,
// as "Supported scopes:" opens a list of scopes.
type labelList string

const (
	labelListNone    labelList = ""
	labelListChoices labelList = "choices"
	labelListScopes  labelList = "scopes"
)

// docType is what one of the types the docs write means, as "bool" or
// "localization key".
type docType struct {
	Value lang.ValueType

	// Database is the database a name of this type belongs to, and Shape what
	// a block of this type holds. Each is "" where the type does not say.
	Database string
	Shape    lang.Shape

	// Values is whether a script value may stand for the number.
	Values bool
}

// docTypes holds what each type the docs write means. A type is looked up as
// lower-case words, so LocalizationKey is "localization key".
var docTypes = map[string]docType{
	"bool":    {Value: lang.ValueTypeBool},
	"boolean": {Value: lang.ValueTypeBool},
	"yes/no":  {Value: lang.ValueTypeBool},

	"int":         {Value: lang.ValueTypeNumber},
	"integer":     {Value: lang.ValueTypeNumber},
	"int32":       {Value: lang.ValueTypeNumber},
	"number":      {Value: lang.ValueTypeNumber},
	"float":       {Value: lang.ValueTypeNumber},
	"decimal":     {Value: lang.ValueTypeNumber},
	"fixed point": {Value: lang.ValueTypeNumber},

	"value":                {Value: lang.ValueTypeNumber, Values: true, Shape: lang.ShapeValue},
	"script value":         {Value: lang.ValueTypeNumber, Values: true, Shape: lang.ShapeValue},
	"scripted value":       {Value: lang.ValueTypeNumber, Values: true, Shape: lang.ShapeValue},
	"scripted value int32": {Value: lang.ValueTypeNumber},

	"date": {Value: lang.ValueTypeDate},

	"string":     {Value: lang.ValueTypeText},
	"text":       {Value: lang.ValueTypeText},
	"name":       {Value: lang.ValueTypeText},
	"identifier": {Value: lang.ValueTypeText},

	"localization key": {Value: lang.ValueTypeReference, Database: lang.Localization},
	"loc key":          {Value: lang.ValueTypeReference, Database: lang.Localization},
	"localisation key": {Value: lang.ValueTypeReference, Database: lang.Localization},

	"database key":           {Value: lang.ValueTypeReference},
	"key":                    {Value: lang.ValueTypeReference},
	"array of database keys": {Value: lang.ValueTypeReference},

	"string path": {Value: lang.ValueTypeReference, Database: lang.File},
	"path":        {Value: lang.ValueTypeReference, Database: lang.File},
	"texture":     {Value: lang.ValueTypeReference, Database: lang.File},
	"file":        {Value: lang.ValueTypeReference, Database: lang.File},

	"color": {Value: lang.ValueTypeColor},

	"trigger":  {Value: lang.ValueTypeBlock, Shape: lang.ShapeTriggers},
	"triggers": {Value: lang.ValueTypeBlock, Shape: lang.ShapeTriggers},
	"effect":   {Value: lang.ValueTypeBlock, Shape: lang.ShapeEffects},
	"effects":  {Value: lang.ValueTypeBlock, Shape: lang.ShapeEffects},

	"dynamic description": {Value: lang.ValueTypeUnknown, Shape: lang.ShapeDescription},

	"modifier":    {Value: lang.ValueTypeBlock, Shape: lang.ShapeModifiers},
	"modifiers":   {Value: lang.ValueTypeBlock, Shape: lang.ShapeModifiers},
	"modifier(s)": {Value: lang.ValueTypeBlock, Shape: lang.ShapeModifiers},

	"block":         {Value: lang.ValueTypeBlock},
	"mtth":          {Value: lang.ValueTypeBlock},
	"scripted cost": {Value: lang.ValueTypeBlock},
	"cost":          {Value: lang.ValueTypeBlock},
	"vector 2":      {Value: lang.ValueTypeBlock},
	"vector2":       {Value: lang.ValueTypeBlock},

	"scaled and triggered modifier": {Value: lang.ValueTypeBlock, Shape: lang.ShapeModifiers},

	// the docs say a path, but the files write an animation's name
	"path to animation": {Value: lang.ValueTypeReference, Database: "gfx/portraits/portrait_animations"},

	// the docs write it above a block of triggers, so it says nothing of what
	// the block holds
	"effect group": {Value: lang.ValueTypeBlock},
}

// requiredLevel is a word of a doc that says whether a field must be
// written.
var requiredLevel = regexp.MustCompile(`(?i)\b(optional|required|mandatory)\b`)

// buildDocs reads each doc of the listing into the database it describes.
// What an earlier doc says is kept over a later one. A doc in a folder below
// its database's describes only part of it, so its opening sentences do not
// become the database's hover text.
func (b *builder) buildDocs(installPath string, listing []files.File, folders []string) error {
	for _, file := range listing {
		label, isPartial, ok := findDocDatabase(file.Rel, folders)
		database := b.Databases[label]
		if !ok || database == nil {
			continue
		}

		text, err := files.ReadText(file.Path)
		if err != nil {
			return err
		}

		if reader, ok := b.parseDoc(text.Text, file.Rel, database.Declaration); ok {
			if isPartial {
				reader.blurb = nil
			}

			readme, _ := files.ToRel(installPath, file.Path)
			reader.build(database, readme)
			if !isPartial && database.Readme == "" {
				database.Readme = readme
			}
		}
	}

	return nil
}

// docMarkers are the words that make a .txt file a doc when its name holds
// one of them.
var docMarkers = []string{"readme", "info", "example", "examples", "template", "help", "documentation"}

// findDocDatabase returns the database a doc file describes: that of its
// folder, else of a folder above, else of the folder named as the file is.
// isPartial is true for a doc below its database's folder, which describes
// only part of it. It returns false if the file is no doc or nothing fits.
func findDocDatabase(rel string, folders []string) (database string, isPartial, ok bool) {
	base := strings.ToLower(path.Base(rel))
	stem := strings.TrimSuffix(base, path.Ext(base))
	terms := strings.FieldsFunc(stem, func(r rune) bool { return !unicode.IsLetter(r) })
	isMarked := slices.ContainsFunc(terms, func(term string) bool { return slices.Contains(docMarkers, term) })
	if path.Ext(base) != ".info" && path.Ext(base) != ".md" && !strings.HasPrefix(base, "readme") &&
		(path.Ext(base) != ".txt" || !isMarked) {
		return "", false, false
	}

	var candidates []string
	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		candidates = append(candidates, dir)
	}

	// the doc's own folder, each folder above it, then the folder named as
	// the file is
	for position, candidate := range append(candidates, path.Dir(rel)+"/"+strings.TrimLeft(stem, "_")) {
		matches := slices.DeleteFunc(slices.Clone(folders), func(folder string) bool {
			return candidate != folder && !strings.HasSuffix(candidate, "/"+folder)
		})
		if len(matches) > 0 {
			longest := slices.MaxFunc(matches, func(first, second string) int { return len(first) - len(second) })

			return lang.ToDatabase(longest), position > 0 && position < len(candidates), true
		}
	}

	return "", false, false
}

// parseDoc reads one doc file. It returns false for a file that is data and
// no doc: one with no sentence and no comment, or a .txt file with more
// lines of script than of sentences.
func (b *builder) parseDoc(text, rel string, declaration lang.Declaration) (*docReader, bool) {
	reader := &docReader{schema: b.Schema, root: &lang.Field{}}

	// where each file is one definition, the doc is read as already inside
	// that definition
	if declaration.DefinedBy == lang.DefinedByFile {
		reader.blocks = []*lang.Field{reader.root}
	}

	isMarkdown := strings.EqualFold(path.Ext(rel), ".md")
	for raw := range strings.Lines(text) {
		reader.readLine(raw, isMarkdown && strings.HasPrefix(raw, "#"))
	}

	reader.closeHeader()
	isScript := strings.EqualFold(path.Ext(rel), ".txt") && reader.prose < reader.script
	if reader.prose == 0 && !strings.Contains(text, "#") || isScript {
		return nil, false
	}

	return reader, true
}

// build gives a database what one doc says: its hover text, the doc's path
// and its fields. What the database already says is kept. A database of
// triggers, effects or a script value takes no fields, since its keys are
// script.
func (d *docReader) build(database *lang.Database, readme string) {
	definition := database.Field
	if blurb := strings.Join(d.blurb, " "); blurb != "" && !definition.Blurb.IsStated() {
		definition.Blurb.Resolve(blurb, lang.SourceDocs)
		database.Readme = readme
	}

	switch database.Declaration.Shape {
	case lang.ShapeTriggers, lang.ShapeEffects, lang.ShapeValue:
		return
	}

	shape := cmp.Or(database.Declaration.Shape, lang.ShapeFields)
	buildDocFields(lang.LookupDialect(d.schema.Game), definition, d.root, "", shape)
}

// buildDocFields gives a field the keys a doc wrote under it, and each key
// the keys under it in turn, stopping at a key whose block holds script.
func buildDocFields(dialect *lang.Dialect, field, doc *lang.Field, holder string, shape lang.Shape) {
	for key, child := range doc.Children {
		target := field.BuildChild(key, lang.SourceDocs)
		target.Take(child.Claims)
		if !holdsScript(dialect, shape, holder, key, child) {
			// what a block below a definition's own holds is not known
			buildDocFields(dialect, target, child, key, lang.ShapeUnknown)
		}
	}
}

// holdsScript reports whether the block of a doc's key holds script rather
// than fields of its own.
func holdsScript(dialect *lang.Dialect, shape lang.Shape, holder, key string, field *lang.Field) bool {
	isBlock := len(field.Children) > 0 || field.Value.Value.Type == lang.ValueTypeBlock
	keyword, ok := dialect.LookupKeyword(shape, holder, key, isBlock)

	return field.Shape.IsStated() || field.Value.Value.Type == lang.ValueTypeNumber || ok && keyword.Shape != ""
}

// The kinds of line a doc is made of. hashRun is the #s that open a line.
// headingLine is a title or a rule, as "== Fields ==" and "-----" are.
// briefLine is a header that names a field, as "### Brief: key (type)".
// statementLine is script, as key = value. braceLine holds only braces.
// labelLine is "Title: text", fieldLine is "key: text", and bulletLine is
// an item of a list.
var (
	hashRun       = regexp.MustCompile(`^(?:#+\s*)+`)
	headingLine   = regexp.MustCompile(`^(=+ *[^=]* *=+|[-=#*_|]{3,}|[A-Z][A-Z0-9 _/:-]{2,}|-[a-z_]+(=<[^>]*>)?)$`)
	briefLine     = regexp.MustCompile(`(?i)^brief\s*[:=]?\s*([A-Za-z_][A-Za-z0-9_.]*)\s*(?:-[^(]*)?(?:\(\s*([^)]*)\))?`)
	statementLine = regexp.MustCompile(`^(<[^>]+>(?:\.<[^>]+>)*|\$\w+\$|\w+:[\w@.]+|\w+(?:/\w+)+|[A-Za-z_@][A-Za-z0-9_.'-]*|\d[0-9.]*)\s*(?:\?=|==|<=|>=|=|<|>)\s*(.*)$`)
	braceLine     = regexp.MustCompile(`^[{}. ]+$`)
	labelLine     = regexp.MustCompile(`^([A-Z][A-Za-z ]{1,24}|format):\s*(.*)$`)
	fieldLine     = regexp.MustCompile(`^([a-z_][a-z0-9_]*):\s+(\S.*)$`)
	bulletLine    = regexp.MustCompile(`^[-*]\s+(.*)$`)
)

// readLine reads one line of a doc as the kind of line it is. A list item
// that writes a statement is read as the statement.
func (d *docReader) readLine(raw string, isHeading bool) {
	line := strings.TrimSpace(raw)
	text := strings.TrimSpace(hashRun.ReplaceAllString(line, ""))
	code, comment := splitComment(text)

	// a doc's sentences are not script a parser could read, so a statement
	// is told by its pattern
	statement := statementLine.FindStringSubmatch(code)
	if bullet := bulletLine.FindStringSubmatch(code); statement == nil && bullet != nil {
		statement = statementLine.FindStringSubmatch(bullet[1])
	}

	isStatement := statement != nil && !isFact(statement[1]) && !isNote(statement[1], statement[2])
	opens := strings.Count(code, "{") - strings.Count(code, "}")
	d.countLine(line, isStatement || braceLine.MatchString(code))

	switch {
	case line == "":
		d.comments, d.list = nil, labelListNone
		d.isBlurbOver = d.isBlurbOver || len(d.blurb) > 0

	case text == "": // only hashes

	case strings.HasPrefix(line, "#") && briefLine.MatchString(text):
		// ### Brief: key (type)
		brief := briefLine.FindStringSubmatch(text)
		d.closeHeader()
		field := &lang.Field{}
		resolveType(field, brief[2])
		d.current, d.isHeader, d.header, d.comments, d.list = field, true, brief[1], nil, labelListNone

	case isStatement:
		value, rest := splitValue(strings.TrimSpace(statement[2]))
		d.readStatement(statement[1], value, strings.TrimSpace(rest+" "+comment), opens)

	case braceLine.MatchString(code):
		d.readStatement("", "", "", opens)

	case isHeading || headingLine.MatchString(text):
		d.comments, d.list = nil, labelListNone

	case strings.HasPrefix(text, "scope:"):
		d.readProse(text)

	case labelLine.MatchString(text):
		label := labelLine.FindStringSubmatch(text)
		d.readLabel(label[1], label[2])

	case fieldLine.MatchString(text):
		field := fieldLine.FindStringSubmatch(text)
		d.readField(field[1], field[2])
		d.comments = nil

	case bulletLine.MatchString(text):
		d.readBullet(bulletLine.FindStringSubmatch(text)[1])

	default:
		d.readProse(text)
	}
}

// splitComment splits a line at its first # outside quotes into the script
// before it and the comment after it.
func splitComment(line string) (code, comment string) {
	isQuoted := false
	for i := range len(line) {
		switch line[i] {
		case '"':
			isQuoted = !isQuoted
		case '#':
			if !isQuoted {
				return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
			}
		}
	}

	return line, ""
}

// isFact reports whether a key = value line says something about the whole
// definition rather than a field, as root = character does.
func isFact(key string) bool {
	lower := strings.ToLower(key)

	return lower == lang.LinkRoot || lower == "default"
}

// isNote reports whether a line written as a statement is a sentence about
// one, as scope:actor = the actor is.
func isNote(key, value string) bool {
	return strings.ContainsAny(key, ":/$") && !strings.HasPrefix(value, "{")
}

// countLine counts a line as script or as a sentence. A line that starts
// with # is a sentence.
func (d *docReader) countLine(line string, isCode bool) {
	switch {
	case line == "":
	case isCode && !strings.HasPrefix(line, "#"):
		d.script++
	default:
		d.prose++
	}
}

// closeHeader gives what the last header said to the field of its key, if
// no statement of that key followed it.
func (d *docReader) closeHeader() {
	if d.header != "" {
		d.buildField(d.header).Take(d.current.Claims)
		d.header = ""
	}
}

// buildField returns the doc's top-level field of a key, adding it if the
// doc has not written it yet, and records that the fields have begun.
func (d *docReader) buildField(key string) *lang.Field {
	d.hasFields = true

	return d.root.BuildChild(key, lang.SourceDocs)
}

// splitValue splits the text after a key into the value and the words after
// it. A block or a quoted text is all value; otherwise the value is a
// <placeholder> or the first word.
func splitValue(value string) (first, rest string) {
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, `"`) {
		return value, ""
	}

	if strings.HasPrefix(value, "<") {
		if end := strings.Index(value, ">"); end > 0 {
			return value[:end+1], strings.TrimSpace(value[end+1:])
		}
	}

	first, rest, _ = strings.Cut(value, " ")

	return first, strings.TrimSpace(rest)
}

// docDateKey is the key the docs write for a block under a date.
const docDateKey = "date"

// readStatement reads a line of script as a field of its block, described by
// the sentences before it. opens is how many more blocks the line opens than
// it closes. A block opened at the doc's top is the definition itself and
// makes no field, nor does a key that stands for names.
func (d *docReader) readStatement(key, value, text string, opens int) {
	d.hasFields = true
	holder := d.root
	if len(d.blocks) > 0 {
		holder = d.blocks[len(d.blocks)-1]
	}

	isEntry := len(d.blocks) == 0 && (opens > 0 || strings.HasPrefix(value, "{"))
	field := holder
	if key != "" && !isEntry && !isStandIn(key) {
		step := key
		if jomini.Classify(step).IsNumberKey() || step == docDateKey {
			step = jomini.NumberKey
		}

		if d.header != key {
			d.closeHeader()
		}

		field = holder.BuildChild(step, lang.SourceDocs)
		if d.header == key {
			field.Take(d.current.Claims)
			d.header = ""
		}

		d.resolveText(field, append(d.comments, text)...)
		d.resolveValue(field, value)
		d.current, d.list = field, labelListNone
	}

	for range max(opens, 0) {
		d.blocks = append(d.blocks, field)
	}

	d.blocks = d.blocks[:max(len(d.blocks)+min(opens, 0), 0)]
	d.comments, d.isHeader = nil, false
}

// isStandIn reports whether a key stands for names, as <key>, scope:actor,
// trigger/effect and $target$ do.
func isStandIn(key string) bool {
	return strings.HasPrefix(key, "<") || strings.ContainsAny(key, ":/$")
}

// readProse reads a sentence. Before the first field it is one of the doc's
// opening sentences, after a header it describes the header's field, and
// otherwise it describes the next statement.
func (d *docReader) readProse(text string) {
	if !d.hasFields && !d.isBlurbOver {
		d.blurb = append(d.blurb, text)
	}

	if d.isHeader && d.current != nil {
		d.resolveText(d.current, text)

		return
	}

	d.comments = append(d.comments, text)
}

// choiceLabel is a label that opens a list of the values a field takes, as
// "Possible values" does. scopeLabel is one that opens a list of scopes.
var (
	choiceLabel = regexp.MustCompile(`(?i)^(?:possible|valid|available|allowed|supported) \w+$`)
	scopeLabel  = regexp.MustCompile(`(?i)^(?:supported |available )?scopes?$`)
)

// readLabel reads a "Title: text" line about the current field: its default,
// or a label that opens a list of scopes or of values. Any other label is
// read as a sentence.
func (d *docReader) readLabel(title, text string) {
	label := strings.ToLower(title)
	switch {
	case d.current == nil:
		d.readProse(title + ": " + text)

	case label == "default":
		resolveDefault(d.current, findTerm(text))

	case scopeLabel.MatchString(label):
		d.list = labelListScopes

	case choiceLabel.MatchString(label):
		d.list = labelListChoices
		resolveChoices(d.current, strings.Split(text, ","), lang.SourceDocs)

	default:
		d.readProse(title + ": " + text)
	}
}

// readField reads a "key: text" line as a field at the top of the doc,
// described by the text.
func (d *docReader) readField(key, text string) {
	field := d.buildField(key)
	d.resolveText(field, text)
	d.current, d.isHeader = field, false
}

// bulletKey is an item of a list that starts with a key, with or without
// text after a colon or a dash.
var bulletKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*)\s*(?:[:-]\s+(.*))?$`)

// readBullet reads one item of a list, by the label it falls under.
func (d *docReader) readBullet(bullet string) {
	key, text := "", bullet
	if match := bulletKey.FindStringSubmatch(bullet); match != nil {
		key, text = match[1], match[2]
	}

	switch {
	case d.list == labelListChoices && d.current != nil:
		resolveChoices(d.current, []string{cmp.Or(key, text)}, lang.SourceDocs)
	case d.list == labelListScopes && d.current != nil:
		// a scope is no field
	case key != "" && text != "" && strings.ToLower(key) == key && !strings.Contains(key, "."):
		d.readField(key, text)
	default:
		d.readProse(strings.TrimSpace(key + " " + text))
	}
}

// typeParens is a type written in parentheses, as "(bool, optional)".
// typeBrackets is one written in brackets, as "[int]".
var (
	typeParens   = regexp.MustCompile(`\(\s*([a-z][a-z0-9 _]*?)\s*(?:,\s*([a-z ,]+?)\s*)?\)`)
	typeBrackets = regexp.MustCompile(`(?i)\[(string|boolean|bool|integer|int|float|number|trigger|effect|date)\]`)
)

// defaultPhrase is a default written in a sentence, as "defaults to 0".
var defaultPhrase = regexp.MustCompile(`(?i)(?:^|[\s(\[,])default(?:s\s+to|\s+is|\s+value(?:\s+is)?|\s*[:=]|\s+)\s*["']?([A-Za-z0-9_.+-]+)`)

// defaultStops are the words that follow "default" in a sentence and are no
// default, as in "the default behavior".
var defaultStops = []string{"value", "values", "is", "to", "the", "a", "an", "by", "behavior", "option", "one"}

// resolveText reads the sentences about a field. A placeholder at their
// start is its value, and the rest is its hover text, which may also give
// its type, whether it is required, and its default.
func (d *docReader) resolveText(field *lang.Field, lines ...string) {
	text := strings.TrimSpace(strings.Join(lines, " "))
	if value, rest := splitValue(text); strings.HasPrefix(value, "<") {
		d.resolveValue(field, value)
		text = rest
	}

	if text == "" {
		return
	}

	field.Blurb = lang.Claim[string]{Value: strings.TrimSpace(field.Blurb.Value + " " + text), Source: lang.SourceDocs}
	if opening, ok := findDocType(text); ok {
		resolveDocType(field, opening, lang.SourceDocs)
	}

	if parens := typeParens.FindStringSubmatch(text); parens != nil {
		resolveType(field, parens[1]+","+parens[2])
	}

	if brackets := typeBrackets.FindStringSubmatch(text); brackets != nil {
		resolveDocType(field, brackets[1], lang.SourceDocs)
	}

	resolveRequired(field, text)
	if defaultMatch := defaultPhrase.FindStringSubmatch(text); defaultMatch != nil &&
		!slices.Contains(defaultStops, strings.ToLower(defaultMatch[1])) {
		resolveDefault(field, defaultMatch[1])
	}
}

// findDocType returns the type a sentence starts with, after any article,
// taking the longest that fits. It returns false if there is none, or if the
// type says neither what a block holds, nor yes or no, nor a database.
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
		if row, ok := docTypes[lang.ToPhrase(phrase)]; ok {
			return phrase, row.Shape != "" || row.Value == lang.ValueTypeBool || row.Database != ""
		}
	}

	return "", false
}

// resolveType reads a type written in parentheses, as "(int, optional)",
// where optional, required and mandatory say whether the field must be
// written.
func resolveType(field *lang.Field, text string) {
	kind, _, _ := strings.Cut(text, ",")
	resolveDocType(field, strings.Join(strings.Fields(requiredLevel.ReplaceAllString(kind, "")), " "), lang.SourceDocs)
	for part := range strings.SplitSeq(text, ",") {
		resolveRequired(field, part)
	}
}

// resolveDocType gives a field what a type of the docs means, from the given
// source. A type docTypes does not hold says nothing.
func resolveDocType(field *lang.Field, term string, source lang.Source) {
	row := docTypes[lang.ToPhrase(term)]
	if row.Shape != "" {
		field.Shape.Resolve(row.Shape, source)
	}

	if row.Value != "" {
		field.Value.Resolve(lang.Expected{Type: row.Value, Database: row.Database, Values: row.Values}, source)
	}
}

// conditionPhrase is how a sentence says a field is needed only in some
// cases, as "only used when" and "required if" do.
var conditionPhrase = regexp.MustCompile(`(?i)\b(?:only (?:used|needed|valid|applies|relevant) (?:when|if|for)|required (?:if|when)|ignored (?:if|when|unless)|needed (?:if|when))\b`)

// resolveRequired reads from a sentence whether a field is required,
// optional, or needed only in some cases. What was said first is kept.
func resolveRequired(field *lang.Field, text string) {
	term := strings.ToLower(requiredLevel.FindString(text))
	switch {
	case conditionPhrase.MatchString(text):
		field.Required.Resolve(lang.RequirementConditional, lang.SourceDocs)
	case term == "optional":
		field.Required.Resolve(lang.RequirementOptional, lang.SourceDocs)
	case term != "":
		field.Required.Resolve(lang.RequirementAlways, lang.SourceDocs)
	}
}

func resolveDefault(field *lang.Field, value string) {
	if value != "" {
		field.Default.Resolve(value, lang.SourceDocs)
	}
}

// alternatives is a value written as several joined by / or |, as yes/no
// is.
var alternatives = regexp.MustCompile(`^([A-Za-z0-9_.+-]+(?:[/|][A-Za-z0-9_.+-]+)+)$`)

// resolveValue reads a field's type from the value a doc writes for it,
// whether a placeholder, a list such as a/b, or a sample value. What a
// placeholder writes in parentheses is ignored.
func (d *docReader) resolveValue(field *lang.Field, value string) {
	inner, isPlaceholder := strings.CutPrefix(value, "<")
	inner = strings.ToLower(strings.TrimSuffix(inner, ">"))
	if isPlaceholder {
		inner, _, _ = strings.Cut(inner, "(")
		inner = strings.TrimSpace(inner)
	}

	_, isType := docTypes[lang.ToPhrase(inner)]
	_, isDate := jomini.ParseDate(value)
	stem, isKey := strings.CutSuffix(inner, "_key")
	database, hasDatabase := d.schema.ResolveTypeDatabase(stem)

	// a placeholder that names a game type, as <pop type>, names its
	// definitions; a doc never means a GUI or graphics database by it
	isDrawn := strings.HasPrefix(database, lang.GUIDatabasePrefix) || strings.HasPrefix(database, "gfx/")
	namesDatabase := hasDatabase && !isType && (isKey || isPlaceholder && !isDrawn)
	switch {
	case value == "": // nothing is written

	case strings.HasPrefix(value, "{"):
		resolveDocType(field, "block", lang.SourceDocs)

	case alternatives.MatchString(inner):
		// types joined by a slash, as number/scripted, are no list of values
		parts := strings.FieldsFunc(inner, func(r rune) bool { return r == '/' || r == '|' })
		if !slices.ContainsFunc(parts, func(part string) bool { _, ok := docTypes[lang.ToPhrase(part)]; return ok }) {
			resolveChoices(field, parts, lang.SourceDocs)
		}

	case namesDatabase:
		field.Value.Resolve(lang.Expected{Type: lang.ValueTypeReference, Database: database}, lang.SourceDocs)

	case isPlaceholder || isType:
		resolveDocType(field, inner, lang.SourceDocs)

	case jomini.Classify(value) == jomini.ClassNumber:
		resolveDocType(field, "number", lang.SourceDocs)

	case slices.Contains(lang.Bools, value):
		resolveDocType(field, "bool", lang.SourceDocs)

	case isDate:
		resolveDocType(field, "date", lang.SourceDocs)

	case strings.HasPrefix(value, `"`):
		resolveDocType(field, "string", lang.SourceDocs)
	}
}

// resolveChoices adds values to the list a field takes one of, replacing any
// other type the field was given. A value marked "(default" is also the
// default, and a list of only yes and no makes the field a yes or no.
func resolveChoices(field *lang.Field, values []string, source lang.Source) {
	var choices []string
	if field.Value.Value.Type == lang.ValueTypeChoice {
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

	expected := lang.Expected{Type: lang.ValueTypeChoice, Choices: choices}
	if slices.Equal(slices.Sorted(slices.Values(choices)), slices.Sorted(slices.Values(lang.Bools))) {
		expected = lang.Expected{Type: lang.ValueTypeBool}
	}

	field.Value = lang.Claim[lang.Expected]{Value: expected, Source: source}
}

// findTerm returns the first word of a text, without quotes, parentheses,
// commas or semicolons.
func findTerm(text string) string {
	terms := strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'(),;`, r) })
	if len(terms) == 0 {
		return ""
	}

	return terms[0]
}

// buildDocBlurbs gives each field with no hover text the one the docs write
// for a field of the same key elsewhere in its database.
func (b *builder) buildDocBlurbs() {
	for _, database := range b.Databases {
		blurbs := map[string]string{}
		lang.WalkField(database.Field, func(key string, field *lang.Field) {
			if field.Blurb.Source == lang.SourceDocs && blurbs[key] == "" {
				blurbs[key] = field.Blurb.Value
			}
		})
		lang.WalkField(database.Field, func(key string, field *lang.Field) {
			if blurb := blurbs[key]; blurb != "" && !field.Blurb.IsStated() {
				field.Blurb.Resolve(blurb, lang.SourceDocs)
			}
		})
	}
}
