// docs.go reads the documentation files a game ships beside its script into
// the fields, types and defaults they describe.

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

// docReader reads one doc file line by line into the fields it describes.
type docReader struct {
	schema *Schema

	// root is the doc's definition: the fields its example writes, each
	// under the block that holds it.
	root *Field

	// blocks holds, for each block the line is in, the field its keys belong
	// to.
	blocks []*Field

	// current is the field the text attaches to. It is nil before the first.
	current *Field

	// headed is whether the current field came from a header and no
	// statement has followed it.
	headed bool

	// header is the key of the last header while no statement of it has
	// followed. It is "" if none.
	header string

	// comments are the comment lines directly above the next statement.
	comments []string

	// list is the list a label opened for the current field.
	list labelList

	// blurb is the first paragraph of prose before the first statement.
	blurb []string

	// started is whether a statement, a header or a field has been read, and
	// ended whether the first paragraph has ended.
	started bool
	ended   bool

	// script is the count of script lines and prose the count of other lines
	// with text, which tell a data file from a doc.
	script int
	prose  int
}

// labelList is the list that a label in a doc opens for the field above it,
// as "Possible values:" opens a list of choices.
type labelList string

// The lists a label opens: none, choices or scopes.
const (
	labelListNone    labelList = ""
	labelListChoices labelList = "choices"
	labelListScopes  labelList = "scopes"
)

// docType is what a type in the games' own docs stands for, such as bool or
// localization key.
type docType struct {
	// Value is bool, number, date, text, reference, color, block, or unknown
	// for a key or a block.
	Value ValueType

	// Database is the database a reference names. It is "" where that is not
	// known.
	Database string

	// Shape is a Shapes key for a block. It is "" for none.
	Shape game.Shape

	// Values is whether the term takes a script value.
	Values bool
}

// docTypes are the types the games' own docs (.info, .md and readme files)
// write, each with what it stands for.
var docTypes = map[string]docType{
	"bool": {Value: ValueTypeBool}, "boolean": {Value: ValueTypeBool}, "yes/no": {Value: ValueTypeBool},
	"int": {Value: ValueTypeNumber}, "integer": {Value: ValueTypeNumber}, "int32": {Value: ValueTypeNumber}, "number": {Value: ValueTypeNumber},
	"float": {Value: ValueTypeNumber}, "decimal": {Value: ValueTypeNumber}, "fixed point": {Value: ValueTypeNumber},
	// CK3 common/script_values/_script_values.info, "Inlining": "Formulas can
	// be written inline wherever scripted values work", so a block written
	// there is a value block
	"value":                {Value: ValueTypeNumber, Values: true, Shape: game.ShapeValue},
	"script value":         {Value: ValueTypeNumber, Values: true, Shape: game.ShapeValue},
	"scripted value":       {Value: ValueTypeNumber, Values: true, Shape: game.ShapeValue},
	"scripted value int32": {Value: ValueTypeNumber},
	"date":                 {Value: ValueTypeDate},
	"string":               {Value: ValueTypeText}, "text": {Value: ValueTypeText}, "name": {Value: ValueTypeText}, "identifier": {Value: ValueTypeText},
	"localization key": {Value: ValueTypeReference, Database: game.Localization},
	"loc key":          {Value: ValueTypeReference, Database: game.Localization},
	"localisation key": {Value: ValueTypeReference, Database: game.Localization},
	"database key":     {Value: ValueTypeReference}, "key": {Value: ValueTypeReference}, "array of database keys": {Value: ValueTypeReference},
	"string path": {Value: ValueTypeReference, Database: game.File}, "path": {Value: ValueTypeReference, Database: game.File},
	"texture": {Value: ValueTypeReference, Database: game.File}, "file": {Value: ValueTypeReference, Database: game.File},
	"color":   {Value: ValueTypeColor},
	"trigger": {Value: ValueTypeBlock, Shape: game.ShapeTriggers}, "triggers": {Value: ValueTypeBlock, Shape: game.ShapeTriggers},
	"effect": {Value: ValueTypeBlock, Shape: game.ShapeEffects}, "effects": {Value: ValueTypeBlock, Shape: game.ShapeEffects},
	"dynamic description": {Value: ValueTypeUnknown, Shape: game.ShapeDescription},
	"modifier":            {Value: ValueTypeBlock, Shape: game.ShapeModifiers}, "modifiers": {Value: ValueTypeBlock, Shape: game.ShapeModifiers},
	"modifier(s)": {Value: ValueTypeBlock, Shape: game.ShapeModifiers}, "block": {Value: ValueTypeBlock}, "mtth": {Value: ValueTypeBlock},
	"scripted cost": {Value: ValueTypeBlock}, "cost": {Value: ValueTypeBlock}, "vector 2": {Value: ValueTypeBlock}, "vector2": {Value: ValueTypeBlock},

	// EU5 in_game/common/estate_privileges/readme.txt:17 and 22 more lines of
	// its readmes
	"scaled and triggered modifier": {Value: ValueTypeBlock, Shape: game.ShapeModifiers},
	// CK3 court_positions/types/_court_positions.info:13 and
	// tasks/_court_position_tasks.info:12 write "Path to animation played in
	// GUI"; the game's files write an animation's name there, as
	// 00_court_positions.txt's reading
	"path to animation": {Value: ValueTypeReference, Database: "gfx/portraits/portrait_animations"},
	// CK3 story_cycles/_story_cycles.info:129 "Effect group will fire only if
	// trigger is satisfied" opens the comment on a block of triggers, which
	// names no type
	"effect group": {Value: ValueTypeBlock},
}

// requiredLevel is a term that says whether a field is required.
var requiredLevel = regexp.MustCompile(`(?i)\b(optional|required|mandatory)\b`)

// buildDocs reads the docs in the given database folders into the databases
// they describe, a later doc filling only what an earlier one left out. A
// doc that describes part of its database gives it no blurb. It returns an
// error if a doc cannot be read.
func (s *Schema) buildDocs(installPath string, listing []files.File, folders []string) error {
	for _, file := range listing {
		label, partial, ok := findDocDatabase(file.Rel, folders)
		database := s.Databases[label]
		if !ok || database == nil {
			continue
		}

		text, err := files.ReadText(file.Path)
		if err != nil {
			return err
		}

		if reader, ok := s.parseDoc(text.Text, file.Rel, database.Row); ok {
			if partial {
				reader.blurb = nil
			}

			readme, _ := files.ToRel(installPath, file.Path)
			reader.build(database, readme)
			if !partial && database.Readme == "" {
				database.Readme = readme
			}
		}
	}

	return nil
}

// docMarkers are the terms in a .txt file's name that mark it as a doc.
var docMarkers = []string{"readme", "info", "example", "examples", "template", "help", "documentation"}

// findDocDatabase returns the database that a doc file describes, by its
// folder or else its name, and whether the doc describes only part of it. It
// returns false if the file is no doc or describes no database.
func findDocDatabase(rel string, folders []string) (string, bool, bool) {
	base := strings.ToLower(path.Base(rel))
	stem := strings.TrimSuffix(base, path.Ext(base))
	terms := strings.FieldsFunc(stem, func(r rune) bool { return !unicode.IsLetter(r) })
	marked := slices.ContainsFunc(terms, func(term string) bool { return slices.Contains(docMarkers, term) })
	if path.Ext(base) != ".info" && path.Ext(base) != ".md" && !strings.HasPrefix(base, "readme") &&
		(path.Ext(base) != ".txt" || !marked) {
		return "", false, false
	}

	var candidates []string
	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		candidates = append(candidates, dir)
	}

	// the doc's own folder comes first and the folder its file name gives
	// last; any other is a folder above its own
	for position, candidate := range append(candidates, path.Dir(rel)+"/"+strings.TrimLeft(stem, "_")) {
		matches := slices.DeleteFunc(slices.Clone(folders), func(folder string) bool {
			return candidate != folder && !strings.HasSuffix(candidate, "/"+folder)
		})
		if len(matches) > 0 {
			longest := slices.MaxFunc(matches, func(first, second string) int { return len(first) - len(second) })

			return game.ToDatabase(longest), position > 0 && position < len(candidates), true
		}
	}

	return "", false, false
}

// parseDoc reads one doc file into a reader, given the row of the database
// it describes. It returns false for a file that is data: one with no prose
// and no comment, or a .txt file with more script lines than prose lines.
func (s *Schema) parseDoc(text, rel string, row game.Database) (*docReader, bool) {
	reader := &docReader{schema: s, root: &Field{}}

	// where each file is one definition, the doc is read as already inside it
	if row.DefinedBy == game.DefinedByFile {
		reader.blocks = []*Field{reader.root}
	}

	md := strings.EqualFold(path.Ext(rel), ".md")
	for raw := range strings.Lines(text) {
		reader.readLine(raw, md && strings.HasPrefix(raw, "#"))
	}

	reader.closeHeader()
	if reader.prose == 0 && !strings.Contains(text, "#") || strings.EqualFold(path.Ext(rel), ".txt") && reader.prose < reader.script {
		return nil, false
	}

	return reader, true
}

// build gives a database what one doc file states: its blurb, the fields its
// example writes with their claims, and the given path of the doc from the
// install. It never replaces a claim already stated. A database of triggers,
// effects or a value takes no fields from a doc.
func (d *docReader) build(database *Database, readme string) {
	definition := database.Definition
	if blurb := strings.Join(d.blurb, " "); blurb != "" && !definition.Blurb.IsStated() {
		definition.Blurb.resolve(blurb, game.SourceDocs)
		database.Readme = readme
	}

	for key := range d.root.Children {
		database.Documented[key] = true
	}

	switch cmp.Or(database.Callable.Shape, database.Row.Shape) {
	case game.ShapeTriggers, game.ShapeEffects, game.ShapeValue:
		return
	}

	shape := cmp.Or(database.Callable.Shape, database.Row.Shape, game.ShapeFields)
	buildDocFields(game.LookupGame(d.schema.Game), definition, d.root, "", shape)
}

// buildDocFields copies the fields that a doc writes onto a field, with
// their claims and the fields below them. It stops at a block that the given
// game's keywords say holds script. The doc's field sits under the given
// key, "" for the definition itself, in a block of the given shape.
func buildDocFields(row *game.Game, field, doc *Field, holder string, shape game.Shape) {
	for key, child := range doc.Children {
		target := field.buildChild(key, game.SourceDocs)
		target.take(child.Claims)
		if !holdsScript(row, shape, holder, key, child) {
			// what a block below a definition's own holds is not known
			buildDocFields(row, target, child, key, game.ShapeUnknown)
		}
	}
}

// holdsScript reports whether a field's block holds script rather than
// fields. It does if the field states a shape or a number to work out, or if
// its key is a keyword of the given game that gives its block a shape, in a
// block of the given shape under the key above.
func holdsScript(row *game.Game, shape game.Shape, holder, key string, field *Field) bool {
	block := len(field.Children) > 0 || field.Value.Value.Type == ValueTypeBlock
	keyword, ok := row.LookupKeyword(shape, holder, key, block)

	return field.Shape.IsStated() || field.Value.Value.Type == ValueTypeNumber || ok && keyword.Shape != ""
}

// The patterns a line of a doc is matched against.
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

// readLine reads one line of a doc by the pattern it matches. A bullet that
// writes a statement counts as that statement. The flag says whether
// markdown makes the line a heading.
func (d *docReader) readLine(raw string, heading bool) {
	line := strings.TrimSpace(raw)
	text := strings.TrimSpace(hashRun.ReplaceAllString(line, ""))
	code, comment := splitComment(text)

	// prose is not valid script, so a statement is matched by pattern
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
		d.ended = d.ended || len(d.blurb) > 0

	case text == "": // only hashes

	case strings.HasPrefix(line, "#") && briefLine.MatchString(text):
		brief := briefLine.FindStringSubmatch(text)
		d.readHeader(brief[1], brief[2])

	case isStatement:
		value, rest := splitValue(strings.TrimSpace(statement[2]))
		d.readStatement(statement[1], value, strings.TrimSpace(rest+" "+comment), opens)

	case braceLine.MatchString(code):
		d.readStatement("", "", "", opens)

	case heading || headingLine.MatchString(text):
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

// splitComment splits a line into its code and the comment after a # that
// is not inside quotes.
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

// isFact reports whether the given key is the root or a default, which name
// no field.
func isFact(key string) bool {
	lower := strings.ToLower(key)

	return lower == game.ScopeLinks.Root || lower == "default"
}

// isNote reports whether a statement is a note in prose rather than script,
// as scope:actor = the actor and days/weeks/months/years = X are: a key with
// a prefix, alternatives or a parameter, whose value is no block. Such a key
// that opens a block is script.
func isNote(key, value string) bool {
	return strings.ContainsAny(key, ":/$") && !strings.HasPrefix(value, "{")
}

// countLine counts a line as script or prose, to tell a data file from a
// doc. The flag says whether the line writes a statement or a brace.
func (d *docReader) countLine(line string, isCode bool) {
	switch {
	case line == "": // blank lines count toward neither
	case isCode && !strings.HasPrefix(line, "#"):
		d.script++
	default:
		d.prose++
	}
}

// readHeader reads a ### Brief: header, which gives the key it opens and its
// type in parentheses.
func (d *docReader) readHeader(key, text string) {
	d.closeHeader()
	field := &Field{}
	resolveType(field, text)
	d.current, d.headed, d.header, d.comments, d.list = field, true, key, nil, labelListNone
}

// closeHeader gives the last header's facts to its key at the top of the
// definition, if no statement of that key followed it.
func (d *docReader) closeHeader() {
	if d.header != "" {
		d.buildField(d.header).take(d.current.Claims)
		d.header = ""
	}
}

// buildField returns the field of the given key at the top of the
// definition, adding it if there is none. It marks the doc as begun.
func (d *docReader) buildField(key string) *Field {
	d.started = true

	return d.root.buildChild(key, game.SourceDocs)
}

// splitValue splits what a doc writes after a key into the value and the
// text that describes it. A block, a quoted text or a single term is all
// value. Otherwise the value is a placeholder in angle brackets or the first
// term, and the rest describes it.
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

// docDateKey is the key the games' own docs write for a date, as
// date = { … } stands for each dated block of a history file.
const docDateKey = "date"

// readStatement reads a line of script as a field of the block that holds
// it. The given count is the net blocks the line opens. The entry block of
// the example and a key that stands for names make no field. The comments
// above the line and after it are the field's text, its value gives its
// type, and a header just before it gives its claims.
func (d *docReader) readStatement(key, value, text string, opens int) {
	d.started = true
	holder := d.root
	if len(d.blocks) > 0 {
		holder = d.blocks[len(d.blocks)-1]
	}

	entry := len(d.blocks) == 0 && (opens > 0 || strings.HasPrefix(value, "{"))
	field := holder
	if key != "" && !entry && !isStandIn(key) {
		step := key
		if jomini.Classify(step).IsNumberKey() || step == docDateKey {
			step = jomini.NumberKey
		}

		if d.header != key {
			d.closeHeader()
		}

		field = holder.buildChild(step, game.SourceDocs)
		if d.header == key {
			field.take(d.current.Claims)
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
	d.comments, d.headed = nil, false
}

// isStandIn reports whether a key stands for names rather than naming a
// field, as <key>, scope:actor, trigger/effect and $target$ do.
func isStandIn(key string) bool {
	return strings.HasPrefix(key, "<") || strings.ContainsAny(key, ":/$")
}

// readProse reads a line of prose as the database's blurb, the text of the
// header's field or a comment on the next statement.
func (d *docReader) readProse(text string) {
	if !d.started && !d.ended {
		d.blurb = append(d.blurb, text)
	}

	if d.headed && d.current != nil {
		d.resolveText(d.current, text)

		return
	}

	d.comments = append(d.comments, text)
}

// choiceLabel is a label that opens a list of choices, as "Possible values"
// does, and scopeLabel one that opens a list of scopes.
var (
	choiceLabel = regexp.MustCompile(`(?i)^(?:possible|valid|available|allowed|supported) \w+$`)
	scopeLabel  = regexp.MustCompile(`(?i)^(?:supported |available )?scopes?$`)
)

// readLabel reads a "Label: text" line: a default, the opening of a list of
// values or scopes, or prose.
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
		resolveChoices(d.current, strings.Split(text, ","), game.SourceDocs)

	default:
		d.readProse(title + ": " + text)
	}
}

// readField reads a "key: text" line or a keyed bullet as a field at the top
// of the definition.
func (d *docReader) readField(key, text string) {
	field := d.buildField(key)
	d.resolveText(field, text)
	d.current, d.headed = field, false
}

// bulletKey is a bullet that opens with a key, with the text after it.
var bulletKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.]*)\s*(?:[:-]\s+(.*))?$`)

// readBullet reads a bullet. It is a choice of the field above if a label
// opened a list of choices, a scope of it that states nothing if a label
// opened a list of scopes, a field if it has a snake-case key, and prose
// otherwise.
func (d *docReader) readBullet(bullet string) {
	key, text := "", bullet
	if keyed := bulletKey.FindStringSubmatch(bullet); keyed != nil {
		key, text = keyed[1], keyed[2]
	}

	switch {
	case d.list == labelListChoices && d.current != nil:
		resolveChoices(d.current, []string{cmp.Or(key, text)}, game.SourceDocs)
	case d.list == labelListScopes && d.current != nil:
		// a scope in the list is no field and no blurb of the next one
	case key != "" && text != "" && strings.ToLower(key) == key && !strings.Contains(key, "."):
		d.readField(key, text)
	default:
		d.readProse(strings.TrimSpace(key + " " + text))
	}
}

// typeParens is a type in parentheses, with the terms after a comma, and
// typeBrackets a type in square brackets.
var (
	typeParens   = regexp.MustCompile(`\(\s*([a-z][a-z0-9 _]*?)\s*(?:,\s*([a-z ,]+?)\s*)?\)`)
	typeBrackets = regexp.MustCompile(`(?i)\[(string|boolean|bool|integer|int|float|number|trigger|effect|date)\]`)
)

// defaultPhrase is a default the text states, as "defaults to 0" does.
var defaultPhrase = regexp.MustCompile(`(?i)(?:^|[\s(\[,])default(?:s\s+to|\s+is|\s+value(?:\s+is)?|\s*[:=]|\s+)\s*["']?([A-Za-z0-9_.+-]+)`)

// defaultStops are the terms after "default" that give no default.
var defaultStops = []string{"value", "values", "is", "to", "the", "a", "an", "by", "behavior", "option", "one"}

// resolveText reads the lines of text written about a field. A placeholder
// at the start is the field's value. The rest is the blurb, and any type,
// requirement and default it states. A type in parentheses or brackets
// counts if the docs know the term.
func (d *docReader) resolveText(field *Field, lines ...string) {
	text := strings.TrimSpace(strings.Join(lines, " "))
	if value, rest := splitValue(text); strings.HasPrefix(value, "<") {
		d.resolveValue(field, value)
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
}

// findDocType returns the longest type term that opens the text, after an
// article such as "the". It returns false if no term opens the text, or if
// the term names no shape, no bool type and no database.
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
		if row, ok := docTypes[ToPhrase(phrase)]; ok {
			return phrase, row.Shape != "" || row.Value == ValueTypeBool || row.Database != ""
		}
	}

	return "", false
}

// resolveType reads a type written in parentheses. The terms optional,
// required and mandatory, inside it or after a comma, say whether the field
// is required. The rest is the type.
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

// resolveDocType gives a field the claims that a type term means: the shape
// of its block, and its value type with the database a reference names. The
// given source states them. An unknown term makes no claim.
func resolveDocType(field *Field, term string, source game.Source) {
	row := docTypes[ToPhrase(term)]
	if row.Shape != "" {
		field.Shape.resolve(row.Shape, source)
	}

	if row.Value != "" {
		field.Value.resolve(Expected{Type: row.Value, Database: row.Database, Values: row.Values}, source)
	}
}

// conditionPhrase is a phrase that makes a field required only under a
// condition, as "only used when" does.
var conditionPhrase = regexp.MustCompile(`(?i)\b(?:only (?:used|needed|valid|applies|relevant) (?:when|if|for)|required (?:if|when)|ignored (?:if|when|unless)|needed (?:if|when))\b`)

// resolveRequired reads from the text whether a field is required. A
// condition makes it conditional, and the terms optional, required and
// mandatory make it optional or always required. The first one stated is
// kept.
func resolveRequired(field *Field, text string) {
	term := strings.ToLower(requiredLevel.FindString(text))
	switch {
	case conditionPhrase.MatchString(text):
		field.Required.resolve(RequirementConditional, game.SourceDocs)
	case term == "optional":
		field.Required.resolve(RequirementOptional, game.SourceDocs)
	case term != "":
		field.Required.resolve(RequirementAlways, game.SourceDocs)
	}
}

// resolveDefault gives a field the default the docs write. It does nothing
// for an empty default or one already given.
func resolveDefault(field *Field, value string) {
	if value != "" {
		field.Default.resolve(value, game.SourceDocs)
	}
}

// alternatives is a value of several terms joined by slashes or bars, as
// yes/no is.
var alternatives = regexp.MustCompile(`^([A-Za-z0-9_.+-]+(?:[/|][A-Za-z0-9_.+-]+)+)$`)

// resolveValue reads a field's type from the value its example writes: a
// placeholder, alternatives, a type term, a sample key such as
// innovation_key that names a database, or a sample value. A placeholder's
// text in parentheses describes its term and is no part of it.
func (d *docReader) resolveValue(field *Field, value string) {
	inner, placeholder := strings.CutPrefix(value, "<")
	inner = strings.ToLower(strings.TrimSuffix(inner, ">"))
	if placeholder {
		inner, _, _ = strings.Cut(inner, "(")
		inner = strings.TrimSpace(inner)
	}

	_, known := docTypes[ToPhrase(inner)]
	_, date := jomini.ParseDate(value)
	words, keyed := strings.CutSuffix(inner, "_key")
	database, named := d.schema.ResolveTypeDatabase(words)
	switch {
	case value == "": // nothing is written

	case strings.HasPrefix(value, "{"):
		resolveDocType(field, "block", game.SourceDocs)

	case alternatives.MatchString(inner):
		// alternatives of types, such as number/scripted, state no choices
		parts := strings.FieldsFunc(inner, func(r rune) bool { return r == '/' || r == '|' })
		if !slices.ContainsFunc(parts, func(part string) bool { _, ok := docTypes[ToPhrase(part)]; return ok }) {
			resolveChoices(field, parts, game.SourceDocs)
		}

	case keyed && named && !known:
		field.Value.resolve(Expected{Type: ValueTypeReference, Database: database}, game.SourceDocs)

	case placeholder || known:
		resolveDocType(field, inner, game.SourceDocs)

	case jomini.Classify(value) == jomini.ClassNumber:
		resolveDocType(field, "number", game.SourceDocs)

	case slices.Contains(game.Bools, value):
		resolveDocType(field, "bool", game.SourceDocs)

	case date:
		resolveDocType(field, "date", game.SourceDocs)

	case strings.HasPrefix(value, `"`):
		resolveDocType(field, "string", game.SourceDocs)
	}
}

// resolveChoices adds the given values to the choices of a field, as the
// given source states them. They replace any value the field had before.
// The one marked "(default" is the default. If the choices are only the two
// bool terms, the field is a bool.
func resolveChoices(field *Field, values []string, source game.Source) {
	var choices []string
	if field.Value.Value.Type == ValueTypeChoice {
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

	expected := Expected{Type: ValueTypeChoice, Choices: choices}
	if slices.Equal(slices.Sorted(slices.Values(choices)), slices.Sorted(slices.Values(game.Bools))) {
		expected = Expected{Type: ValueTypeBool}
	}

	field.Value = Claim[Expected]{Value: expected, Source: source}
}

// findTerm returns the first term of the text without quotes and
// punctuation. It returns "" if the text has none.
func findTerm(text string) string {
	terms := strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"'(),;`, r) })
	if len(terms) == 0 {
		return ""
	}

	return terms[0]
}

// buildDocBlurbs gives each field without a blurb the blurb the docs write
// for the same key elsewhere in its database.
func (s *Schema) buildDocBlurbs() {
	for _, database := range s.Databases {
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
