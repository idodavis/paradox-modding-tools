// template.go reads docs, wiki examples and dump usages: the normalizer turns them into script with
// their prose kept as comments, and the phrase catalogue reads each statement's comments as claims.

package schema

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gmtext "github.com/yuin/goldmark/text"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// rewrites is the rewrite catalogue, applied in order to a code line before its comment: each match
// becomes replace, and move (expanded from the match) is added to the line's trailing comment.
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

// islands are the regexes that tell a script line from prose. A word ends in a letter, digit, _ or
// $ (not prose's : or .); a key is a word, a quoted string or a <placeholder>, and a | b | c names
// alternatives.
var islands = func() (islands struct{ field, statement, code, word, skip *regexp.Regexp }) {
	word, placeholder := `-?[\w@$](?:[\w.:@$/-]*[\w$])?`, `<[^\s<>=][^<>]*>`
	key := `(?:"[^"]*"|` + placeholder + `|` + word + `)(?:[ \t]*\|[ \t]*` + word + `)*`
	operator := `\s*(?:==|!=|<=|>=|\?=|=|<\s|>\s|<$|>$|\{)`
	islands.field = regexp.MustCompile(`^[-*]\s+[\w.]+:\s+` + placeholder)
	islands.statement = regexp.MustCompile(`^` + key + operator)
	islands.code = regexp.MustCompile(`^(?:[{}]|` + key + operator + `|` + placeholder + `[\s}]*(?:#.*)?$)`)
	islands.word = regexp.MustCompile(`^(?:` + word + `|` + placeholder + `)[\s}]*(?:#.*)?$`)
	islands.skip = regexp.MustCompile(`^(?:` + placeholder + `|\([^()]*\))`)
	return islands
}()

// choices is a value written as alternatives (yes/no/all, num/all).
var choices = regexp.MustCompile(`^[\w.]+(?:/[\w.]+)+$`)

// phrases is the phrase catalogue: each row reads one claim from a comment's text or a value as
// written. A row marked alone reads a text only when no other row does.
var phrases = []struct {
	pattern *regexp.Regexp
	alone   bool
	set     func(schema *Schema, found *Position, match []string, from Source)
}{
	// default is x, defaults to x, (default = x), default: x, and default x when x is a value: one
	// word that ends its clause
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
	{regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), false, func(schema *Schema, found *Position, match []string,
		from Source) {
		for _, word := range strings.FieldsFunc(match[1], func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
		}) {
			if _, declared := schema.Scopes[word]; declared {
				SetFirst(&found.Scope, word, from)
				return
			}
		}
	}},
	// scope:x = … names a saved scope, Brief: key (type) labels the field below, and a line ending
	// in : leads into what follows; none is a blurb
	{regexp.MustCompile(`(?i)^scope:\S+ (?:=|-) `), false, nil},
	{regexp.MustCompile(`(?i)^brief\b`), false, nil},
	{regexp.MustCompile(`:$`), false, nil},
	// type words opening the text or a parenthesis ("Localization key that …", ( bool, optional )),
	// not one of a/b alternatives; text, name and scope only as the whole text: prose opens with them
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(?:scripted )?(?:int|integer|int32|int31|num|number|float|double|` +
		`fixed[ _]point)|scripted? value|script_value|0\.0-1\.0)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordNumber}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool(?:ean)?|yes/no)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordBool}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordDate}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordText}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordScope}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:[^\w/]|$)`), false,
		func(_ *Schema, found *Position, _ []string, from Source) {
			SetFirst(&found.Value, Expected{Type: WordLoc}, from)
		}},
	// event ID names the database whose registry row is Events; on action (name) the OnActions one
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:[^\w/]|$)`), false,
		func(schema *Schema, found *Position, match []string, from Source) {
			for name, database := range schema.Databases {
				if match[1] != "" && database.Row.Events || match[1] == "" && database.Row.OnActions {
					SetFirst(&found.Value, Expected{Type: WordReference, Database: name}, from)
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
		SetFirst(&found.Value, Expected{Type: WordFile}, from)
	}},
	// - item: blurb and item - blurb: one word per line
	{regexp.MustCompile(`^(?:[-*]\s+([\w.]+):\s+[^\s<]|(?:[-*]\s+)?([\w.]+) - \S)`), true,
		func(_ *Schema, found *Position, match []string, from Source) {
			found.Value.Value.Words = append(found.Value.Value.Words, match[1]+match[2])
			found.Value.Source = from
		}},
}

// normalize turns the lines of a doc, a wiki example or a usage into script: code lines kept (with
// the rewrite catalogue), every other line a # comment verbatim, open blocks closed by indentation.
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
		cut, quoted := len(uncommented), false
		for i := 0; i < len(uncommented) && cut == len(uncommented); i++ {
			switch {
			case uncommented[i] == '"':
				quoted = !quoted
			case uncommented[i] == '#' && !quoted:
				cut = i
			}
		}
		body, comment := uncommented[:cut], uncommented[cut:]

		// blank lines stay; a line not shaped like script is prose; a bare word is a list element only
		// inside a block, on a line that was never a comment
		shape := strings.TrimLeft(body, " \t")
		script := islands.field.MatchString(shape) || islands.code.MatchString(shape) ||
			len(open) > 0 && !commented && islands.word.MatchString(shape)
		said, read := strings.TrimSpace(strings.TrimLeft(body, "# \t")), false
		for i := 0; commented && !read && i < len(phrases); i++ {
			where := phrases[i].pattern.FindStringIndex(said)
			read = where != nil && where[0] == 0
		}
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

		// the rewrite catalogue
		var moved []string
		for _, rewrite := range rewrites {
			for _, match := range rewrite.pattern.FindAllStringSubmatchIndex(body, -1) {
				if note := string(rewrite.pattern.ExpandString(nil, rewrite.move, body, match)); note != "" {
					moved = append(moved, note)
				}
			}
			body = rewrite.pattern.ReplaceAllString(body, rewrite.replace)
		}
		if len(moved) > 0 {
			comment = strings.TrimRight(" # "+strings.Join(moved, " ")+" "+comment, " ")
		}

		// in a text that leaves blocks open, a code line back at an open block's indentation closes
		// that block first; a line's own leading } closes one
		indent := 0
		for _, char := range body[:len(body)-len(strings.TrimLeft(body, " \t"))] {
			indent++
			if char == '\t' {
				indent += 3 - (indent-1)%4
			}
		}
		closes := 0
		for offside && closes < len(open) && open[len(open)-1-closes] >= indent {
			closes++
		}
		if closes > 0 && strings.HasPrefix(strings.TrimLeft(body, " \t"), "}") {
			closes--
		}
		open = open[:len(open)-closes]
		out.WriteString(strings.Repeat("} ", closes) + body + comment + "\n")

		// the line's own braces, outside quotes
		quoted = false
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
	}

	// blocks still open at the end of the text close there
	out.WriteString(strings.Repeat("} ", len(open)))
	return out.String()
}

// usageLines is a dump usage's lines for normalize: a line that starts with a statement is split
// before each statement on its own level or its first brace level, skipping placeholders and
// (notes), so a note trails its argument.
func usageLines(text string) (split []string) {
	for line := range strings.SplitSeq(text, "\n") {
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if !islands.statement.MatchString(line[len(indent):]) {
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
			case (line[i] == '<' || line[i] == '(') && islands.skip.MatchString(line[i:]):
				i += len(islands.skip.FindString(line[i:])) - 1
			case line[i] == '{':
				depth++
			case line[i] == '}':
				depth--
			case depth < 2 && strings.TrimSpace(line[start:i]) != "" && strings.ContainsAny(line[i-1:i], " \t{") &&
				islands.statement.MatchString(line[i:]):
				split = append(split, prefix+line[start:i])
				start, prefix = i, indent+strings.Repeat("\t", depth)
			}
		}
		split = append(split, prefix+line[start:])
	}
	return split
}

// markdown is a markdown doc's lines for normalize, as goldmark reads them: a heading ends a
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
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") && !islands.field.MatchString(trimmed) {
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

// failed is whether a statement holds a parse error none of its children holds: it is left out.
func (doc doc) failed(statement jomini.Statement) bool {
	span, children := statement.NodeRange(), []jomini.Statement(nil)
	if block := jomini.ChildBlock(statement); block != nil {
		children = block.Statements
	}
	return slices.ContainsFunc(doc.parsed.Errors, func(failure jomini.ParseError) bool {
		start := failure.Range.Start
		return start >= span.Start && start < span.End && !slices.ContainsFunc(children, func(child jomini.Statement) bool {
			return start >= child.NodeRange().Start && start < child.NodeRange().End
		})
	})
}

// claims is a statement's claims: its value as written, then its comments, through the phrase
// catalogue; the comment lines the rows do not read whole, headings aside, are its blurb.
func (doc doc) claims(statement jomini.Statement) (found Position) {
	// catalogue reads a text and returns what no row read; a row that claims nothing reads it all
	var catalogue func(text string) string
	catalogue = func(text string) string {
		rest, ignored := text, false
		for _, phrase := range phrases {
			if match := phrase.pattern.FindStringSubmatch(text); match != nil && (!phrase.alone || rest == text) {
				rest, ignored = strings.Replace(rest, match[0], "", 1), ignored || phrase.set == nil
				if phrase.set != nil {
					phrase.set(doc.schema, &found, match, doc.from)
				}
			}
		}

		// a/b/c no row reads whole is read word by word: a type word is its type, the rest its words
		if rest == text && choices.MatchString(text) {
			for word := range strings.SplitSeq(text, "/") {
				if catalogue(word) == word {
					found.Value.Value.Words, found.Value.Source = append(found.Value.Value.Words, word), doc.from
				}
			}
			return ""
		}
		if ignored {
			return ""
		}
		return rest
	}
	prose := func(text string) bool { return strings.ContainsFunc(catalogue(text), unicode.IsLetter) }
	_, isAssign := statement.(*jomini.Assignment)

	// a placeholder no row reads names a database (Named); a quoted text no row reads whole is prose
	var blurb []string
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		written := doc.parsed.Src[scalar.Range.Start:scalar.Range.End]
		word := strings.TrimSuffix(strings.TrimPrefix(written, `"<`), `>"`)
		placeholder := len(word)+4 == len(written)
		switch {
		case placeholder && catalogue(word) == word:
			if named := doc.schema.DatabaseNamed(word); named != "" {
				SetFirst(&found.Value, Expected{Type: WordReference, Database: named}, doc.from)
			}
		case !placeholder && prose(written) && scalar.Quoted && strings.ContainsAny(scalar.Text, " \t"):
			blurb = append(blurb, scalar.Text)
		case !placeholder && !doc.usage: // a doc's literal states its form (expiration_days = 3650)
			literals := map[Forms]WordType{FormNumber: WordNumber, FormBool: WordBool, FormDate: WordDate}
			if typed, literal := literals[formOf(scalar)]; literal {
				SetFirst(&found.Value, Expected{Type: typed}, doc.from)
			}
		}
		if isAssign && doc.usage && found.Block.Source != SourceNone {
			SetFirst(&found.Scope, "", doc.from)
		}
	}

	// a block that lists a block placeholder holds that kind (limit = { <display_triggers> })
	block := jomini.ChildBlock(statement)
	if block != nil {
		for _, inner := range block.Statements {
			_, isList := inner.(*jomini.ValueStmt)
			word, isWord := jomini.ValueOf(inner).(*jomini.Scalar)
			if isList && isWord && strings.HasPrefix(word.Text, "<") {
				last := strings.TrimSuffix(word.Text[strings.LastIndexAny(word.Text, "<_ ")+1:], ">")
				if kind, named := game.Placeholders[strings.ToLower(last)]; named {
					SetFirst(&found.Block, kind, doc.from)
				}
			}
		}
	}
	for _, comment := range doc.attached[statement] {
		raw := doc.parsed.Src[comment.Range.Start:comment.Range.End]
		said := strings.TrimSpace(strings.TrimLeft(raw, "# \t"))
		if prose(said) && !strings.HasPrefix(strings.TrimLeft(raw[1:], " \t"), "#") {
			blurb = append(blurb, said)
		}
	}

	// a doc's block whose kind nothing names states only that the key takes a block
	if block != nil && found.Block.Source == SourceNone && !doc.usage {
		SetFirst(&found.Value, Expected{Type: WordBlock}, doc.from)
	}
	slices.Sort(found.Value.Value.Words)
	found.Value.Value.Words = slices.Compact(found.Value.Value.Words)
	if len(blurb) > 0 {
		SetFirst(&found.Blurb, strings.Join(blurb, " "), doc.from)
	}
	return found
}

// state writes a statement's claims at a position, which keeps the first declared source that
// writes it.
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

// join reads a doc or wiki example into a database's tree. A key at a position vanilla writes gives
// it its claims; a bare value's go to the position holding it. A key vanilla writes nowhere below
// where it stands (an example entry's name, <key>) is a stand-in: its block is read in its place,
// or at each position below where vanilla writes its keys, and at the top its claims are the
// entry's. A doc never makes a position.
func (schema *Schema) join(parsed *jomini.Result, database *Database, from Source) {
	attached, _ := parsed.Attach()
	doc, seen := doc{schema, parsed, attached, from, false}, map[*Position]map[string]bool{}
	var gather func(names map[string]bool, key *Position)
	gather = func(names map[string]bool, key *Position) {
		for name, child := range key.Children {
			names[name] = names[name] || child.Evidence.In > 0
			gather(names, child)
		}
	}
	var reader Visitor
	reader = Visitor{Names: alternatives, Visit: func(step *Reading) bool {
		_, isAssign := step.Statement.(*jomini.Assignment)
		if under := step.BlockContext.Position; under != nil && seen[under] == nil {
			seen[under] = map[string]bool{}
			gather(seen[under], under)
		}
		switch {
		case doc.failed(step.Statement):
			return false
		case isAssign && step.Command == nil && !seen[step.BlockContext.Position][collapse(step.Name)]:
			// a value where vanilla writes every entry as a block is a field it never writes, no entry
			block, place := jomini.ChildBlock(step.Statement), step.BlockContext
			if step.Inner = place; step.Holder == nil && (block != nil || place.Position.Evidence.Forms != FormBlock) {
				doc.state(place.Position, step.Statement)
			}

			// its block reads in its place where vanilla writes its keys there, else at each position
			// one level below that writes them (my_phase_key = { }); else it reads nothing
			holds := func(at *Position) bool {
				return block != nil && slices.ContainsFunc(block.Statements, func(inner jomini.Statement) bool {
					written, isWritten := inner.(*jomini.Assignment)
					return isWritten && at.Child(written.Key.Text) != nil
				})
			}
			if place.Position == nil || holds(place.Position) {
				break
			}
			for _, below := range place.Position.Children {
				if holds(below) {
					place.Position = below
					schema.WalkBlock(reader, block, block.Statements, place)
				}
			}
			return false
		case step.Position != nil && step.Command == nil && step.Position.Evidence.In > 0:
			doc.state(step.Position, step.Statement)
		}
		return true
	}}
	schema.WalkBlock(reader, nil, parsed.Root.Statements, schema.EntryContext(database))
}

// usage reads a dump usage into a command's arguments. Each statement named written, at any depth
// (in a scope placeholder's block too), gives the command's own claims and each key in its block is
// an argument; written "" is a usage that lists the arguments themselves. Blocks below are walked.
func (schema *Schema) usage(parsed *jomini.Result, command *Command, written string) {
	attached, _ := parsed.Attach()
	doc := doc{schema, parsed, attached, SourceDumps, true}
	statements, at := parsed.Root.Statements, BlockContext{Position: command.Args, Kind: game.BlockFields, InEntry: true}
	if written != "" {
		statements, at.Position = nil, &Position{Children: map[string]*Position{collapse(written): command.Args}}
		jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Text == written {
				statements = append(statements, statement)
				return false
			}
			return true
		})
	}
	reader := Visitor{Create: true, Names: alternatives, Visit: func(step *Reading) bool {
		switch {
		case doc.failed(step.Statement):
			return false
		case step.Position == command.Args:
			step.Inner.Kind = game.BlockFields
		}
		if step.Position != nil && step.Command == nil {
			doc.state(step.Position, step.Statement)
		}
		return true
	}}
	schema.WalkBlock(reader, nil, statements, at)
}

// alternatives is a docs key's names: a/b/c and a|b|c name alternatives.
func alternatives(key string) []string {
	return strings.FieldsFunc(key, func(char rune) bool { return char == '/' || char == '|' })
}
