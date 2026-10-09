// docs.go is steps 3, 4 and 7: the install's docs and the wiki's examples parsed, every key they write noted for
// the entry detector, and their claims applied last, filling what vanilla left.

package schema

import (
	"cmp"
	"context"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gmtext "github.com/yuin/goldmark/text"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// example is a doc or wiki example, normalized and parsed once, with the database it documents.
type example struct {
	database string
	parsed   jomini.Result
	from     Source
}

// parseDocsAndExamples normalizes and parses each doc and wiki example once, with its database. A doc is
// read into its own folder's database, or the folder beside it that it is named after
// (history/_characters.info); one under a row above its folder is not read. The wiki's examples follow the
// docs', so a claim keeps the first source in trust order. A sanitized wiki page keeps no version label, so
// the wiki is never stale.
func (schema *Schema) parseDocsAndExamples(ctx context.Context, input Input) (examples []example, err error) {
	for _, file := range input.Install {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Format != inventory.Prose {
			continue
		}
		at, name := schema.folder(file.Rel)
		stem := strings.TrimPrefix(strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)), "_")
		if beside := NameOf(path.Join(path.Dir(file.Rel), stem)); schema.Databases[beside] != nil {
			at, name = path.Dir(file.Rel), beside
		}
		if schema.Databases[name] == nil || at != path.Dir(file.Rel) {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		lines := strings.Split(text, "\n")
		if strings.EqualFold(path.Ext(file.Rel), ".md") {
			lines = markdown(text)
		}
		examples = append(examples, example{name, jomini.Parse(normalize(lines)), SourceDocs})
	}
	schema.Sources[SourceDocs] = SourceState{From: "install", Written: input.Updated}

	schema.Sources[SourceWiki] = SourceState{From: "none"}
	if !input.WikiWritten.IsZero() {
		schema.Sources[SourceWiki] = SourceState{From: "yours", Written: input.WikiWritten}
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if schema.Databases[name].Row.History {
			continue
		}
		for _, text := range input.WikiExamples[name] {
			examples = append(examples, example{name, jomini.Parse(normalize(strings.Split(text, "\n"))), SourceWiki})
		}
	}
	return examples, nil
}

// collectDocFields records in each database every key name its docs and wiki examples write, at any depth,
// for the entry detector.
func (schema *Schema) collectDocFields(examples []example) {
	for _, written := range examples {
		fields := schema.Databases[written.database].Fields
		jomini.Walk(written.parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign {
				for _, name := range alternatives(assign.Key.Text) {
					fields[positionName(name)] = true
				}
			}
			return true
		})
	}
}

// applyExamples applies the docs' and wiki's claims, last, to the databases they document.
func (schema *Schema) applyExamples(examples []example) {
	for _, written := range examples {
		schema.applyDocClaims(&written.parsed, schema.Databases[written.database], written.from)
	}
}

// rewrites is the rewrite catalog, applied in order to a code line before its comment: each match becomes
// replace, and move, expanded from the match, is added to the line's trailing comment.
var rewrites = []struct {
	pattern       *regexp.Regexp
	replace, move string
}{
	// a | b | c names alternatives, read like a/b/c
	{regexp.MustCompile(`[ \t]*\|[ \t]*`), "|", ""},
	// <key = value> is the argument key = <value>; a / after it reads "or" (<count=num/all> / …)
	{regexp.MustCompile(`<([\w.]+)\s*=\s*([^<>=]+?)\s*>(?:[ \t]+/)?`), "$1 = <$2>", ""},
	// <placeholder> read as one quoted word
	{regexp.MustCompile(`(^|[\s{=])(<[^\s<>=][^<>=]*>)`), `$1"$2"`, ""},
	// - key: <placeholder> text is the field key, its text a comment
	{regexp.MustCompile(`^(\s*)[-*]\s+([\w.]+):\s+("<[^"]*>")\s*(.*)$`), `$1$2 = $3`, "$4"},
	// a (note) after a value and an [x = y] note move to the trailing comment; key = (number) stays
	{regexp.MustCompile(`([^\s=(])[ \t]*\(([^()]*)\)`), "$1", "$2"},
	{regexp.MustCompile(`\[[^\[\]]*=[^\[\]]*\]`), "", "$0"},
	// key = words with spaces → key = "words with spaces"
	{regexp.MustCompile(`^(\s*(?:"[^"]*"|[^\s{}#"=<>]+)\s*(?:==|!=|<=|>=|\?=|=|<|>)\s*)` +
		`([^\s{}"=<>#]+(?:[ \t]+[^\s{}"=<>#]+)+)(\s*)$`), `$1"$2"$3`, ""},
}

// scriptShapes are the regexes that tell a script line from prose. A word ends in a letter, digit, _ or $
// (prose ends in : or .); a key is a word, a quoted string or a <placeholder>; a | b | c names alternatives.
var scriptShapes = func() (shapes struct{ field, statement, code, word, skip *regexp.Regexp }) {
	word, placeholder := `-?[\w@$](?:[\w.:@$/-]*[\w$])?`, `<[^\s<>=][^<>]*>`
	key := `(?:"[^"]*"|` + placeholder + `|` + word + `)(?:[ \t]*\|[ \t]*` + word + `)*`
	operator := `\s*(?:==|!=|<=|>=|\?=|=|<\s|>\s|<$|>$|\{)`
	shapes.field = regexp.MustCompile(`^[-*]\s+[\w.]+:\s+` + placeholder)
	shapes.statement = regexp.MustCompile(`^` + key + operator)
	shapes.code = regexp.MustCompile(`^(?:[{}]|` + key + operator + `|` + placeholder + `[\s}]*(?:#.*)?$)`)
	shapes.word = regexp.MustCompile(`^(?:` + word + `|` + placeholder + `)[\s}]*(?:#.*)?$`)
	shapes.skip = regexp.MustCompile(`^(?:` + placeholder + `|\([^()]*\))`)
	return shapes
}()

// choices matches a value written as alternatives (yes/no/all, num/all).
var choices = regexp.MustCompile(`^[\w.]+(?:/[\w.]+)+$`)

// phraseRules is the phrase catalog: each row reads one claim from a comment's text or from a value as
// written. A row marked alone reads a text only when no other row does.
var phraseRules = []struct {
	pattern *regexp.Regexp
	alone   bool
	set     func(s *Schema, found *Position, match []string, from Source)
}{
	// default is x, defaults to x, (default = x), default: x, and default x when x is a value: one word
	// that ends its clause
	{regexp.MustCompile(`(?i)\bdefaults?(?: is| to| =|:) ['"\x60]?([^\s,;()'"\x60]+?)['"\x60]?` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Position, match []string, from Source) {
		SetFirst(&found.Default, match[1], from)
	}},
	{regexp.MustCompile(`(?i)\bdefault ((?:yes|no|none)\b|-?\d+(?:\.\d+)?|[A-Za-z]\w*(?:[._]\w+)+)` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Position, match []string, from Source) {
		SetFirst(&found.Default, match[1], from)
	}},
	{regexp.MustCompile(`(?i)\boptional\b`), false, func(_ *Schema, found *Position, _ []string, from Source) {
		SetFirst(&found.Required, Requirement{Level: LevelOptional}, from)
	}},
	{regexp.MustCompile(`(?i)^required (?:if|when) (.+)`), false, func(_ *Schema, found *Position, match []string,
		from Source) {
		SetFirst(&found.Required, Requirement{Level: LevelConditional, When: match[1]}, from)
	}},
	{regexp.MustCompile(`(?i)^required(?:[.!]| field\b|$)|\bthis field is required\b`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Required, Requirement{Level: LevelAlways}, from)
		}},
	// root = x: the root scope is the first word of x that is a scope the dumps declare
	{regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), false, func(s *Schema, found *Position, match []string,
		from Source) {
		for _, word := range strings.FieldsFunc(match[1], func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
		}) {
			if _, declared := s.Scopes[word]; declared {
				SetFirst(&found.Scope, word, from)
				return
			}
		}
	}},
	// scope:x = … names a saved scope, Brief: key (type) labels the field below, and a line ending in :
	// leads into what follows; none is a blurb
	{regexp.MustCompile(`(?i)^scope:\S+ (?:=|-) `), false, nil},
	{regexp.MustCompile(`(?i)^brief\b`), false, nil},
	{regexp.MustCompile(`:$`), false, nil},
	// type words opening the text or a parenthesis ("Localization key that …", ( bool, optional )), not one
	// of a/b alternatives; text, name and scope only as the whole text: prose opens with them
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(?:scripted )?(?:int|integer|int32|int31|num|number|float|double|` +
		`fixed[ _]point)|scripted? value|script_value|0\.0-1\.0)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsNumber}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool(?:ean)?|yes/no)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsBool}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsDate}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsString}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsScope}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Kind: ExpectsLoc}, from)
		}},
	// event ID names the database whose registry row is Events; on action (name) the OnActions one
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:[^\w/]|$)`), false,
		func(s *Schema, found *Position, match []string, from Source) {
			for name, database := range s.Databases {
				if match[1] != "" && database.Row.Events || match[1] == "" && database.Row.OnActions {
					SetFirst(&found.Value, Expected{Kind: ExpectsReference, Database: name}, from)
				}
			}
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:triggers?)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Block, game.BlockTriggers, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:effects?)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Block, game.BlockEffects, from)
		}},
	// "path/to/file": one quoted word with a slash
	{regexp.MustCompile(`^"[^"\s<>]*/[^"\s<>]*"$`), false, func(_ *Schema, found *Position, _ []string, from Source) {
		SetFirst(&found.Value, Expected{Kind: ExpectsFile}, from)
	}},
	// - item: blurb and item - blurb: one word per line
	{regexp.MustCompile(`^(?:[-*]\s+([\w.]+):\s+[^\s<]|(?:[-*]\s+)?([\w.]+) - \S)`), true,
		func(_ *Schema, found *Position, match []string, from Source) {
			found.Value.Value.Choices = append(found.Value.Value.Choices, match[1]+match[2])
			found.Value.Source = from
		}},
}

// normalize turns the lines of a doc, a wiki example or a usage into script: code lines kept, with the
// rewrite catalog applied, every other line a # comment verbatim, and open blocks closed by indentation.
// It is a parser of a fixed text shape, so it stays whole.
func normalize(lines []string) string {
	var out strings.Builder
	var open []int // the indentation of each open block's opening line
	text := strings.Join(lines, "\n")
	offside := strings.Count(text, "{") != strings.Count(text, "}") // only a text that leaves blocks open
	for _, line := range lines {
		// a # line is read without its first #, and stays a comment when a phrase opens it
		trimmed := strings.TrimLeft(line, " \t")
		commented := strings.HasPrefix(trimmed, "#")
		uncommented := line
		if commented {
			uncommented = strings.Replace(line, "#", "", 1)
		}

		// the line's own comment starts at the first # outside quotes
		cut := commentStart(uncommented)
		body, comment := uncommented[:cut], uncommented[cut:]

		// blank lines stay; a line not shaped like script is prose; a bare word is a list element only
		// inside a block, on a line that was never a comment
		shape := strings.TrimLeft(body, " \t")
		script := scriptShapes.field.MatchString(shape) || scriptShapes.code.MatchString(shape) ||
			len(open) > 0 && !commented && scriptShapes.word.MatchString(shape)
		said := strings.TrimSpace(strings.TrimLeft(body, "# \t"))
		read := commented && opensWithPhrase(said)
		switch {
		case trimmed == "", commented && said == "":
			out.WriteString("\n") // ends a paragraph: the comments above lead no statement
			continue
		case commented && (!script || read):
			if heading := strings.ToUpper(said) == said && strings.ToLower(said) != said; heading && !read {
				line = "# ## " + said // an upper-case line is a heading (ATTRIBUTES)
			}
			out.WriteString(line + "\n")
			continue
		case !script:
			out.WriteString("# " + line + "\n")
			continue
		}

		body, comment = rewrite(body, comment)

		// in a text that leaves blocks open, a code line back at an open block's indentation closes that
		// block first; a line's own leading } closes one
		indent := indentOf(body)
		closes := 0
		for offside && closes < len(open) && open[len(open)-1-closes] >= indent {
			closes++
		}
		if closes > 0 && strings.HasPrefix(strings.TrimLeft(body, " \t"), "}") {
			closes--
		}
		open = open[:len(open)-closes]
		out.WriteString(strings.Repeat("} ", closes) + body + comment + "\n")
		open = trackBraces(body, indent, open)
	}

	// blocks still open at the end of the text close there
	out.WriteString(strings.Repeat("} ", len(open)))
	return out.String()
}

// opensWithPhrase reports whether a phrase of the catalog opens a text.
func opensWithPhrase(said string) bool {
	return slices.ContainsFunc(phraseRules, func(phrase struct {
		pattern *regexp.Regexp
		alone   bool
		set     func(s *Schema, found *Position, match []string, from Source)
	}) bool {
		where := phrase.pattern.FindStringIndex(said)
		return where != nil && where[0] == 0
	})
}

// rewrite applies the rewrite catalog to a code line; what the rewrites move goes to the front of the
// line's trailing comment.
func rewrite(body, comment string) (string, string) {
	var moved []string
	for _, row := range rewrites {
		for _, match := range row.pattern.FindAllStringSubmatchIndex(body, -1) {
			if note := string(row.pattern.ExpandString(nil, row.move, body, match)); note != "" {
				moved = append(moved, note)
			}
		}
		body = row.pattern.ReplaceAllString(body, row.replace)
	}
	if len(moved) > 0 {
		comment = strings.TrimRight(" # "+strings.Join(moved, " ")+" "+comment, " ")
	}
	return body, comment
}

// indentOf returns the width of a line's leading blanks, a tab reaching the next multiple of four.
func indentOf(body string) (indent int) {
	for _, char := range body[:len(body)-len(strings.TrimLeft(body, " \t"))] {
		indent++
		if char == '\t' {
			indent += 3 - (indent-1)%4
		}
	}
	return indent
}

// commentStart returns the offset of the first # outside quotes in a line, or the line's length.
func commentStart(line string) int {
	quoted := false
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '"':
			quoted = !quoted
		case line[i] == '#' && !quoted:
			return i
		}
	}
	return len(line)
}

// trackBraces returns the open blocks after a line's own braces, outside quotes; a block opened on the line
// is open at the line's indentation.
func trackBraces(body string, indent int, open []int) []int {
	quoted := false
	for i := 0; i < len(body); i++ {
		switch {
		case body[i] == '"':
			quoted = !quoted
		case quoted:
		case body[i] == '{':
			open = append(open, indent)
		case body[i] == '}' && len(open) > 0:
			open = open[:len(open)-1]
		}
	}
	return open
}

// usageLines splits a dump usage's lines for normalize: a line that starts with a statement is split before
// each statement on its own level or its first brace level, skipping placeholders and (notes), so a note
// trails its argument.
func usageLines(text string) (split []string) {
	for line := range strings.SplitSeq(text, "\n") {
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !scriptShapes.statement.MatchString(line[len(indent):]) {
			split = append(split, line)
			continue
		}
		start, depth, quoted, prefix := 0, 0, false, ""
		for i := 0; i < len(line); i++ {
			switch {
			case line[i] == '"':
				quoted = !quoted
			case quoted:
			case line[i] == '#':
				i = len(line)
			case (line[i] == '<' || line[i] == '(') && scriptShapes.skip.MatchString(line[i:]):
				i += len(scriptShapes.skip.FindString(line[i:])) - 1
			case line[i] == '{':
				depth++
			case line[i] == '}':
				depth--
			case depth < 2 && strings.TrimSpace(line[start:i]) != "" && strings.ContainsAny(line[i-1:i], " \t{") &&
				scriptShapes.statement.MatchString(line[i:]):
				split = append(split, prefix+line[start:i])
				start, prefix = i, indent+strings.Repeat("\t", depth)
			}
		}
		split = append(split, prefix+line[start:])
	}
	return split
}

// markdown prepares a markdown doc's lines for normalize, as goldmark reads them: a heading ends a
// paragraph, and a list line is prose unless it is a field.
func markdown(text string) []string {
	lines, source, index := strings.Split(text, "\n"), []byte(text), jomini.NewLineIndex(text)
	document := goldmark.New().Parser().Parse(gmtext.NewReader(source))
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		switch {
		case !entering:
			return ast.WalkContinue, nil
		case node.Kind() == ast.KindHeading:
			for i := 0; i < node.Lines().Len(); i++ {
				lines[index.PositionAt(node.Lines().At(i).Start).Line] = ""
			}
			return ast.WalkSkipChildren, nil
		case node.Kind() != ast.KindList:
			return ast.WalkContinue, nil
		}
		_ = ast.Walk(node, func(inner ast.Node, entering bool) (ast.WalkStatus, error) {
			for i := 0; entering && inner.Type() == ast.TypeBlock && i < inner.Lines().Len(); i++ {
				at := index.PositionAt(inner.Lines().At(i).Start).Line
				trimmed := strings.TrimLeft(lines[at], " \t")
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !scriptShapes.field.MatchString(trimmed) {
					lines[at] = "# " + lines[at]
				}
			}
			return ast.WalkContinue, nil
		})
		return ast.WalkSkipChildren, nil
	})
	return lines
}

// doc is one normalized, parsed doc, wiki example or dump usage, read as one source. In a usage, an
// argument written as a block placeholder runs in an unknown scope.
type doc struct {
	schema   *Schema
	parsed   *jomini.Result
	attached map[jomini.Statement][]jomini.Comment
	from     Source
	usage    bool
}

// failed reports whether a statement is left out: its span holds a parse error that no child's span holds.
func (doc doc) failed(statement jomini.Statement) bool {
	var children []jomini.Range
	if block := jomini.ChildBlock(statement); block != nil {
		for _, inner := range block.Statements {
			children = append(children, inner.NodeRange())
		}
	}
	within := func(span, at jomini.Range) bool { return at.Start >= span.Start && at.End <= span.End }
	return slices.ContainsFunc(doc.parsed.Errors, func(issue jomini.ParseError) bool {
		return within(statement.NodeRange(), issue.Range) &&
			!slices.ContainsFunc(children, func(child jomini.Range) bool { return within(child, issue.Range) })
	})
}

// catalog reads a text through the phrase catalog into found and returns what no row read; a row that
// claims nothing reads it all.
func (doc doc) catalog(found *Position, text string) string {
	rest, ignored := text, false
	for _, phrase := range phraseRules {
		if match := phrase.pattern.FindStringSubmatch(text); match != nil && (!phrase.alone || rest == text) {
			rest, ignored = strings.Replace(rest, match[0], "", 1), ignored || phrase.set == nil
			if phrase.set != nil {
				phrase.set(doc.schema, found, match, doc.from)
			}
		}
	}

	// a/b/c no row reads whole is read word by word: a type word is its type, the rest its words
	if rest == text && choices.MatchString(text) {
		for word := range strings.SplitSeq(text, "/") {
			if doc.catalog(found, word) == word {
				found.Value.Value.Choices, found.Value.Source = append(found.Value.Value.Choices, word), doc.from
			}
		}
		return ""
	}
	if ignored {
		return ""
	}
	return rest
}

// prose reads a text through the catalog and reports whether letters are left over: it is a blurb.
func (doc doc) prose(found *Position, text string) bool {
	return strings.ContainsFunc(doc.catalog(found, text), unicode.IsLetter)
}

// claims reads a statement's claims: its value as written, then its comments, through the phrase catalog.
// The comment lines no row reads whole, headings aside, are its blurb.
func (doc doc) claims(statement jomini.Statement) (found Position) {
	var blurb []string
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		blurb = doc.scalarClaims(&found, statement, scalar)
	}
	block := jomini.ChildBlock(statement)
	doc.placeholderClaims(&found, block)
	for _, comment := range doc.attached[statement] {
		raw := doc.parsed.Src[comment.Range.Start:comment.Range.End]
		said := strings.TrimSpace(strings.TrimLeft(raw, "# \t"))
		if doc.prose(&found, said) && !strings.HasPrefix(strings.TrimLeft(raw[1:], " \t"), "#") {
			blurb = append(blurb, said)
		}
	}

	// a doc's block whose kind nothing names states only that the key takes a block
	if block != nil && found.Block.Source == SourceNone && !doc.usage {
		SetFirst(&found.Value, Expected{Kind: ExpectsBlock}, doc.from)
	}
	slices.Sort(found.Value.Value.Choices)
	found.Value.Value.Choices = slices.Compact(found.Value.Value.Choices)
	if len(blurb) > 0 {
		SetFirst(&found.Blurb, strings.Join(blurb, " "), doc.from)
	}
	return found
}

// scalarClaims reads the claims a statement's scalar value states and returns the blurb it adds. A
// placeholder no row reads names a database (DatabaseNamed); a quoted text no row reads whole is prose; a
// literal states its form (expiration_days = 3650).
func (doc doc) scalarClaims(found *Position, statement jomini.Statement, scalar *jomini.Scalar) (blurb []string) {
	written := doc.parsed.Src[scalar.Range.Start:scalar.Range.End]
	word := strings.TrimSuffix(strings.TrimPrefix(written, `"<`), `>"`)
	placeholder := len(word)+4 == len(written)
	switch {
	case placeholder && doc.catalog(found, word) == word:
		if named := doc.schema.DatabaseNamed(word); named != "" {
			SetFirst(&found.Value, Expected{Kind: ExpectsReference, Database: named}, doc.from)
		}
	case !placeholder && doc.prose(found, written) && scalar.Quoted && strings.ContainsAny(scalar.Text, " \t"):
		blurb = append(blurb, scalar.Text)
	case !placeholder && !doc.usage:
		literals := map[Shapes]ExpectedKind{ShapeNumber: ExpectsNumber, ShapeBool: ExpectsBool, ShapeDate: ExpectsDate}
		if typed, literal := literals[shapeOf(scalar)]; literal {
			SetFirst(&found.Value, Expected{Kind: typed}, doc.from)
		}
	}
	if _, isAssign := statement.(*jomini.Assignment); isAssign && doc.usage && found.Block.Source != SourceNone {
		SetFirst(&found.Scope, "", doc.from)
	}
	return blurb
}

// placeholderClaims states the kind of block a block that lists a block placeholder holds
// (limit = { <display_triggers> }).
func (doc doc) placeholderClaims(found *Position, block *jomini.Block) {
	if block == nil {
		return
	}
	for _, inner := range block.Statements {
		_, isList := inner.(*jomini.ValueStmt)
		scalar, isScalar := jomini.ValueOf(inner).(*jomini.Scalar)
		if isList && isScalar && strings.HasPrefix(scalar.Text, "<") {
			last := strings.TrimSuffix(scalar.Text[strings.LastIndexAny(scalar.Text, "<_ ")+1:], ">")
			if kind, named := game.Placeholders[strings.ToLower(last)]; named {
				SetFirst(&found.Block, kind, doc.from)
			}
		}
	}
}

// state writes a statement's claims into a position, each only where the position's claim is still empty.
func (doc doc) state(position *Position, statement jomini.Statement) {
	found := doc.claims(statement)
	position.DeclaredBy = cmp.Or(position.DeclaredBy, doc.from)
	SetFirst(&position.Value, found.Value.Value, found.Value.Source)
	SetFirst(&position.Block, found.Block.Value, found.Block.Source)
	SetFirst(&position.Scope, found.Scope.Value, found.Scope.Source)
	SetFirst(&position.Required, found.Required.Value, found.Required.Source)
	SetFirst(&position.Default, found.Default.Value, found.Default.Source)
	SetFirst(&position.Blurb, found.Blurb.Value, found.Blurb.Source)
}

// docWalk holds what reading a doc into a database's tree needs.
type docWalk struct {
	doc     doc
	seen    map[*Position]map[string]bool // under a position: every key name below it, with whether vanilla writes it
	visitor Visitor
}

// applyDocClaims reads a doc or wiki example into a database's tree. A key at a position vanilla writes
// gives it its claims; a bare value's go to the position holding it. A key vanilla writes nowhere below where
// it stands (an example entry's name, <key>) is a stand-in: its block is read in its place, or at each
// position below where vanilla writes its keys, and at the top its claims are the entry's. A doc never makes
// a position.
func (schema *Schema) applyDocClaims(parsed *jomini.Result, database *Database, from Source) {
	attached, _ := parsed.Attach()
	reader := &docWalk{doc: doc{schema, parsed, attached, from, false}, seen: map[*Position]map[string]bool{}}
	reader.visitor = Visitor{Alternatives: alternatives, Visit: reader.visit}
	schema.WalkBlock(reader.visitor, nil, parsed.Root.Statements, schema.EntryContext(database))
}

// gather adds to names every key name at or below a position, with whether vanilla writes it.
func gather(names map[string]bool, p *Position) {
	for name, child := range p.Children {
		names[name] = names[name] || child.Vanilla.In > 0
		gather(names, child)
	}
}

// visit states a reading's claims and returns whether to read its block.
func (walk *docWalk) visit(placement *Placement) bool {
	s, d := walk.doc.schema, walk.doc
	if under := placement.BlockContext.Position; under != nil && walk.seen[under] == nil {
		walk.seen[under] = map[string]bool{}
		gather(walk.seen[under], under)
	}
	_, isAssign := placement.Statement.(*jomini.Assignment)
	switch {
	case d.failed(placement.Statement):
		return false
	case isAssign && placement.Command == nil && !walk.seen[placement.BlockContext.Position][positionName(placement.Name)]:
		return walk.standIn(s, placement)
	case placement.Position != nil && placement.Command == nil && placement.Position.Vanilla.In > 0:
		d.state(placement.Position, placement.Statement)
	}
	return true
}

// standIn reads a key vanilla writes nowhere below where it stands. A value where vanilla writes every entry
// as a block is a field it never writes, no entry. The key's block is read in its place where vanilla writes
// its keys there, else at each position one level below that writes them (my_phase_key = { }); else it reads
// nothing.
func (walk *docWalk) standIn(s *Schema, placement *Placement) bool {
	block, context := jomini.ChildBlock(placement.Statement), placement.BlockContext
	placement.Inner = context
	if context.Position != nil && placement.Holder == nil &&
		(block != nil || context.Position.Vanilla.Shapes != ShapeBlock) {
		walk.doc.state(context.Position, placement.Statement)
	}
	holds := func(at *Position) bool {
		return block != nil && slices.ContainsFunc(block.Statements, func(inner jomini.Statement) bool {
			written, isWritten := inner.(*jomini.Assignment)
			return isWritten && at.Child(written.Key.Text) != nil
		})
	}
	if context.Position == nil || holds(context.Position) {
		return true
	}
	for _, below := range context.Position.Children {
		if holds(below) {
			context.Position = below
			s.WalkBlock(walk.visitor, block, block.Statements, context)
		}
	}
	return false
}

// applyUsage reads a dump usage into a command's arguments. Each statement named written, at any depth (in a
// scope placeholder's block too), gives the command's own claims, and each key in its block is an argument;
// written "" is a usage that lists the arguments themselves. Blocks below are walked.
func (schema *Schema) applyUsage(parsed *jomini.Result, command *Command, written string) {
	attached, _ := parsed.Attach()
	d := doc{schema, parsed, attached, SourceDumps, true}
	statements, in := parsed.Root.Statements, BlockContext{Position: command.Args, Kind: game.BlockFields, InEntry: true}
	if written != "" {
		statements, in.Position = nil, &Position{Children: map[string]*Position{positionName(written): command.Args}}
		jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Text == written {
				statements = append(statements, statement)
				return false
			}
			return true
		})
	}
	visitor := Visitor{Create: true, Alternatives: alternatives, Visit: func(placement *Placement) bool {
		switch {
		case d.failed(placement.Statement):
			return false
		case placement.Position == command.Args:
			placement.Inner.Kind = game.BlockFields
		}
		if placement.Position != nil && placement.Command == nil {
			d.state(placement.Position, placement.Statement)
		}
		return true
	}}
	schema.WalkBlock(visitor, nil, statements, in)
}

// alternatives returns the names a docs key stands for: a/b/c and a|b|c name alternatives.
func alternatives(key string) []string {
	return strings.FieldsFunc(key, func(char rune) bool { return char == '/' || char == '|' })
}
