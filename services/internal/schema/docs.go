// docs.go is steps 3, 4 and 7: the install's docs and the wiki's examples parsed, every key they write noted for
// the entry detector, and their claims applied last, filling what vanilla left. The dumps' usages (step 2) go
// through the same path, from dumps.go.

package schema

import (
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
// (history/_characters.info); one under a row above its folder is not read. A sanitized wiki page keeps no
// version label, so the wiki is never stale.
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

// phraseRule reads one claim from a comment's text or from a value as written: set writes into the position
// what pattern matched. A rule marked alone reads a text only when no other rule does; one with no set marks
// a text that claims nothing (a saved scope, a label, a line leading into the next).
type phraseRule struct {
	pattern *regexp.Regexp
	alone   bool
	set     func(s *Schema, found *Position, match []string, from Source)
}

// phraseRules is the phrase catalog, in the order the rules are tried.
var phraseRules = []phraseRule{
	// default is x, defaults to x, (default = x), default: x, and default x when x is a value: one word
	// that ends its clause
	{regexp.MustCompile(`(?i)\bdefaults?(?: is| to| =|:) ['"\x60]?([^\s,;()'"\x60]+?)['"\x60]?` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Position, match []string, from Source) {
		found.Default.Set(match[1], from)
	}},
	{regexp.MustCompile(`(?i)\bdefault ((?:yes|no|none)\b|-?\d+(?:\.\d+)?|[A-Za-z]\w*(?:[._]\w+)+)` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Position, match []string, from Source) {
		found.Default.Set(match[1], from)
	}},
	{regexp.MustCompile(`(?i)\boptional\b`), false, func(_ *Schema, found *Position, _ []string, from Source) {
		found.Required.Set(Requirement{Level: LevelOptional}, from)
	}},
	{regexp.MustCompile(`(?i)^required (?:if|when) (.+)`), false, func(_ *Schema, found *Position, match []string,
		from Source) {
		found.Required.Set(Requirement{Level: LevelConditional, When: match[1]}, from)
	}},
	{regexp.MustCompile(`(?i)^required(?:[.!]| field\b|$)|\bthis field is required\b`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Required.Set(Requirement{Level: LevelAlways}, from)
		}},
	// root = x: the root scope is the first word of x that is a scope the dumps declare
	{regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), false, func(s *Schema, found *Position, match []string,
		from Source) {
		for _, word := range strings.FieldsFunc(match[1], func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
		}) {
			if _, declared := s.Scopes[word]; declared {
				found.Scope.Set(word, from)
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
			found.Value.Set(Expected{Kind: ExpectsNumber}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool(?:ean)?|yes/no)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Value.Set(Expected{Kind: ExpectsBool}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Value.Set(Expected{Kind: ExpectsDate}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Value.Set(Expected{Kind: ExpectsString}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Value.Set(Expected{Kind: ExpectsScope}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Value.Set(Expected{Kind: ExpectsLoc}, from)
		}},
	// event ID names the database whose registry row is Events; on action (name) the OnActions one
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:[^\w/]|$)`), false,
		func(s *Schema, found *Position, match []string, from Source) {
			for name, database := range s.Databases {
				if match[1] != "" && database.Row.Events || match[1] == "" && database.Row.OnActions {
					found.Value.Set(Expected{Kind: ExpectsReference, Database: name}, from)
				}
			}
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:triggers?)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Block.Set(game.BlockTriggers, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:effects?)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			found.Block.Set(game.BlockEffects, from)
		}},
	// "path/to/file": one quoted word with a slash
	{regexp.MustCompile(`^"[^"\s<>]*/[^"\s<>]*"$`), false, func(_ *Schema, found *Position, _ []string, from Source) {
		found.Value.Set(Expected{Kind: ExpectsFile}, from)
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
	return slices.ContainsFunc(phraseRules, func(rule phraseRule) bool {
		where := rule.pattern.FindStringIndex(said)
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

// claimPhrases writes into found every claim the phrase catalog reads from a text, and returns what is left
// for a blurb: the text no rule read; nothing when a rule read it whole, a rule marked it as claiming
// nothing, or it was an a/b/c list, whose words became choices.
func (doc doc) claimPhrases(found *Position, text string) (left string) {
	left, claimsNothing := text, false
	for _, rule := range phraseRules {
		match := rule.pattern.FindStringSubmatch(text)
		if match == nil || rule.alone && left != text {
			continue
		}
		left = strings.Replace(left, match[0], "", 1)
		if rule.set == nil {
			claimsNothing = true
			continue
		}
		rule.set(doc.schema, found, match, doc.from)
	}
	switch {
	case left == text && choices.MatchString(text):
		// a/b/c no rule reads whole is read word by word: a type word is its type, the rest are choices
		for word := range strings.SplitSeq(text, "/") {
			if doc.claimPhrases(found, word) == word {
				found.Value.Value.Choices, found.Value.Source = append(found.Value.Value.Choices, word), doc.from
			}
		}
		return ""
	case claimsNothing:
		return ""
	}
	return left
}

// isBlurb reports whether what the catalog left of a text still has words in it: prose, kept as a blurb.
func isBlurb(left string) bool { return strings.ContainsFunc(left, unicode.IsLetter) }

// claimsOf collects one statement's claims into a position of its own, in this order: its scalar value as
// written, the block placeholders its block lists, then each attached comment through the phrase catalog.
// What the catalog leaves of a comment is blurb, a ## line aside. A doc's block whose kind nothing names
// states only that the key takes a block.
func (doc doc) claimsOf(statement jomini.Statement) (found Position) {
	var blurb []string
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		blurb = doc.claimScalar(&found, statement, scalar)
	}
	block := jomini.ChildBlock(statement)
	doc.claimPlaceholders(&found, block)
	for _, comment := range doc.attached[statement] {
		raw := doc.parsed.Src[comment.Range.Start:comment.Range.End]
		said := strings.TrimSpace(strings.TrimLeft(raw, "# \t"))
		left := doc.claimPhrases(&found, said)
		if isBlurb(left) && !strings.HasPrefix(strings.TrimLeft(raw[1:], " \t"), "#") {
			blurb = append(blurb, said)
		}
	}
	if block != nil && found.Block.Source == SourceNone && !doc.usage {
		found.Value.Set(Expected{Kind: ExpectsBlock}, doc.from)
	}
	slices.Sort(found.Value.Value.Choices)
	found.Value.Value.Choices = slices.Compact(found.Value.Value.Choices)
	if len(blurb) > 0 {
		found.Blurb.Set(strings.Join(blurb, " "), doc.from)
	}
	return found
}

// claimScalar writes the claims a statement's scalar value states and returns the blurb it adds: a
// placeholder (<trait>) one way, any other value the other. In a usage, a key whose block has a kind runs in
// no known scope.
func (doc doc) claimScalar(found *Position, statement jomini.Statement, scalar *jomini.Scalar) (blurb []string) {
	written := doc.parsed.Src[scalar.Range.Start:scalar.Range.End]
	word := strings.TrimSuffix(strings.TrimPrefix(written, `"<`), `>"`)
	if placeholder := len(word)+4 == len(written); placeholder {
		doc.claimPlaceholder(found, word)
	} else {
		blurb = doc.claimValue(found, scalar, written)
	}
	if _, isAssign := statement.(*jomini.Assignment); isAssign && doc.usage && found.Block.Source != SourceNone {
		found.Scope.Set("", doc.from)
	}
	return blurb
}

// claimPlaceholder runs a placeholder's word through the catalog; one no rule reads names a database.
func (doc doc) claimPlaceholder(found *Position, word string) {
	if doc.claimPhrases(found, word) != word {
		return
	}
	if named := doc.schema.DatabaseNamed(word); named != "" {
		found.Value.Set(Expected{Kind: ExpectsReference, Database: named}, doc.from)
	}
}

// claimValue runs a value as written through the catalog and returns the blurb it adds: a quoted text with
// words left is prose; else, outside a usage, a literal states its kind (expiration_days = 3650).
func (doc doc) claimValue(found *Position, scalar *jomini.Scalar, written string) (blurb []string) {
	left := doc.claimPhrases(found, written)
	switch {
	case isBlurb(left) && scalar.Quoted && strings.ContainsAny(scalar.Text, " \t"):
		return []string{scalar.Text}
	case !doc.usage:
		literals := map[Shapes]ExpectedKind{ShapeNumber: ExpectsNumber, ShapeBool: ExpectsBool, ShapeDate: ExpectsDate}
		if kind, literal := literals[shapeOf(scalar)]; literal {
			found.Value.Set(Expected{Kind: kind}, doc.from)
		}
	}
	return nil
}

// claimPlaceholders writes the kind of block a block states by listing a block placeholder
// (limit = { <display_triggers> }).
func (doc doc) claimPlaceholders(found *Position, block *jomini.Block) {
	if block == nil {
		return
	}
	for _, inner := range block.Statements {
		_, isList := inner.(*jomini.ValueStmt)
		scalar, isScalar := jomini.ValueOf(inner).(*jomini.Scalar)
		if isList && isScalar && strings.HasPrefix(scalar.Text, "<") {
			last := strings.TrimSuffix(scalar.Text[strings.LastIndexAny(scalar.Text, "<_ ")+1:], ">")
			if kind, named := game.Placeholders[strings.ToLower(last)]; named {
				found.Block.Set(kind, doc.from)
			}
		}
	}
}

// store writes a statement's claims into a position, each where this source outranks the claim's, and marks
// the position declared by the highest-ranked source that writes it.
func (doc doc) store(position *Position, statement jomini.Statement) {
	found := doc.claimsOf(statement)
	if position.DeclaredBy == SourceNone || doc.from < position.DeclaredBy {
		position.DeclaredBy = doc.from
	}
	position.Value.Set(found.Value.Value, found.Value.Source)
	position.Block.Set(found.Block.Value, found.Block.Source)
	position.Scope.Set(found.Scope.Value, found.Scope.Source)
	position.Required.Set(found.Required.Value, found.Required.Source)
	position.Default.Set(found.Default.Value, found.Default.Source)
	position.Blurb.Set(found.Blurb.Value, found.Blurb.Source)
}

// docWalk is one walk of a doc into a database's tree: the doc, the keys seen under each position, and the
// visitor it walks with.
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
	walk := &docWalk{doc: doc{schema, parsed, attached, from, false}, seen: map[*Position]map[string]bool{}}
	walk.visitor = Visitor{Alternatives: alternatives, Visit: walk.visit}
	schema.WalkBlock(walk.visitor, nil, parsed.Root.Statements, schema.EntryContext(database))
}

// gather adds to names every key name at or below a position, with whether vanilla writes it.
func gather(names map[string]bool, p *Position) {
	for name, child := range p.Children {
		names[name] = names[name] || child.Vanilla.In > 0
		gather(names, child)
	}
}

// visit stores a placement's claims and returns whether to walk its block: a statement with a parse error
// is skipped whole; a key vanilla writes nowhere below the position it stands in is a stand-in; a key at a
// position vanilla writes stores its claims there.
func (walk *docWalk) visit(placement *Placement) bool {
	under := placement.BlockContext.Position
	if under != nil && walk.seen[under] == nil {
		walk.seen[under] = map[string]bool{}
		gather(walk.seen[under], under)
	}
	_, isAssign := placement.Statement.(*jomini.Assignment)
	switch {
	case walk.doc.failed(placement.Statement):
		return false
	case isAssign && placement.Command == nil && !walk.seen[under][positionName(placement.Name)]:
		return walk.standIn(placement)
	case placement.Position != nil && placement.Command == nil && placement.Position.Vanilla.In > 0:
		walk.doc.store(placement.Position, placement.Statement)
	}
	return true
}

// standIn handles a key vanilla writes nowhere below where it stands. A value where vanilla writes every
// entry as a block is a field it never writes, no entry. The key's block is walked in its place where vanilla
// writes its keys there, else at each position one level below that writes them (my_phase_key = { }); else
// it is not walked. For the positions below, the walker is entered again from inside this visit, once per
// position: a visit can move where one block is read, but cannot read it in several places.
func (walk *docWalk) standIn(placement *Placement) bool {
	block, context := jomini.ChildBlock(placement.Statement), placement.BlockContext
	placement.Inner = context
	if context.Position != nil && placement.Holder == nil &&
		(block != nil || context.Position.Vanilla.Shapes != ShapeBlock) {
		walk.doc.store(context.Position, placement.Statement)
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
			walk.doc.schema.WalkBlock(walk.visitor, block, block.Statements, context)
		}
	}
	return false
}

// applyUsage stores a dump usage's claims in a command's arguments. Each statement named written, at any
// depth (in a scope placeholder's block too), gives the command's own claims, and each key in its block is
// an argument; written "" is a usage that lists the arguments themselves. Blocks below are walked.
func (schema *Schema) applyUsage(parsed *jomini.Result, command *Command, written string) {
	attached, _ := parsed.Attach()
	usageDoc := doc{schema, parsed, attached, SourceDumps, true}
	statements, in := parsed.Root.Statements, BlockContext{Position: command.Args, Kind: game.BlockFields, InEntry: true}
	if written != "" {
		// the usage's root: the command's name is its one key and the command's arguments the position
		// below it, so the walker places each statement named written at the arguments and reads its block
		// there
		root := &Position{Children: map[string]*Position{positionName(written): command.Args}}
		statements, in.Position = nil, root
		jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Text == written {
				statements = append(statements, statement)
				return false
			}
			return true
		})
	}
	visitor := Visitor{Create: true, Alternatives: alternatives, Visit: func(placement *Placement) bool {
		if usageDoc.failed(placement.Statement) {
			return false
		}
		if placement.Position == command.Args {
			placement.Inner.Kind = game.BlockFields // the command's own block lists its arguments
		}
		if placement.Position != nil && placement.Command == nil {
			usageDoc.store(placement.Position, placement.Statement)
		}
		return true
	}}
	schema.WalkBlock(visitor, nil, statements, in)
}

// alternatives returns the names a docs key stands for: a/b/c and a|b|c name alternatives.
func alternatives(key string) []string {
	return strings.FieldsFunc(key, func(char rune) bool { return char == '/' || char == '|' })
}
