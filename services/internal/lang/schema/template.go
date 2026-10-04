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
	"paradox-modding-tools/services/internal/lang/jomini"
)

// rewrites is the rewrite catalogue, applied in order to a code line before its comment: each match
// becomes replace, and move (expanded from the match) is added to the line's trailing comment.
var rewrites = []struct {
	pattern       *regexp.Regexp
	replace, move string
}{
	// (… optional …), (… default = x …) and [x = y] notes trail the statement they follow
	{regexp.MustCompile(`\([^()]*\b(?:(?i:optional)|default =)[^()]*\)`), "", "$0"},
	{regexp.MustCompile(`\[[^\[\]]*=[^\[\]]*\]`), "", "$0"},
	// a | b | c names alternatives, read like a/b/c
	{regexp.MustCompile(`[ \t]*\|[ \t]*`), "|", ""},
	// <placeholder> read as one quoted word
	{regexp.MustCompile(`(^|[\s{=])(<[^\s<>=][^<>=]*>)`), `$1"$2"`, ""},
	// - key: <placeholder> text is the field key, its text a comment
	{regexp.MustCompile(`^(\s*)[-*]\s+([\w.]+):\s+("<[^"]*>")\s*(.*)$`), `$1$2 = $3`, "$4"},
	// key = words with spaces → key = "words with spaces"
	{regexp.MustCompile(`^(\s*(?:"[^"]*"|[^\s{}#"=<>]+)\s*(?:==|!=|<=|>=|\?=|=|<|>)\s*)` +
		`([^\s{}"=<>#]+(?:[ \t]+[^\s{}"=<>#]+)+)(\s*)$`), `$1"$2"$3`, ""},
}

// islands are the island test's script shapes. A word ends in a letter, digit, _ or $ (not prose's
// : or .); a key is a word, a quoted string or a <placeholder>, and a | b | c names alternatives.
var islands = func() (islands struct{ field, statement, code, word, skip *regexp.Regexp }) {
	word, placeholder := `-?[\w@$](?:[\w.:@$/-]*[\w$])?`, `<[^\s<>=][^<>=]*>`
	key := `(?:"[^"]*"|` + placeholder + `|` + word + `)(?:[ \t]*\|[ \t]*` + word + `)*`
	operator := `\s*(?:==|!=|<=|>=|\?=|=|<\s|>\s|<$|>$|\{)`
	islands.field = regexp.MustCompile(`^[-*]\s+[\w.]+:\s+` + placeholder)
	islands.statement = regexp.MustCompile(`^` + key + operator)
	islands.code = regexp.MustCompile(`^(?:[{}]|` + key + operator + `|` + placeholder + `[\s}]*(?:#.*)?$)`)
	islands.word = regexp.MustCompile(`^(?:` + word + `|` + placeholder + `)[\s}]*(?:#.*)?$`)
	islands.skip = regexp.MustCompile(`^(?:` + placeholder + `|\([^()]*\))`)
	return islands
}()

// phrases is the phrase catalogue: each row reads one claim from a comment's text or a value as
// written. A row marked alone reads a text only when no other row does.
var phrases = []struct {
	pattern *regexp.Regexp
	alone   bool
	set     func(schema *Schema, found *Key, match []string, from Source)
}{
	// default is x, defaults to x, (default = x), and default x when x is a value: one word that
	// ends its clause
	{regexp.MustCompile(`(?i)\bdefaults? (?:is|to|=) ['"\x60]?([^\s,;()'"\x60]+?)['"\x60]?` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Key, match []string, from Source) {
		Set(&found.Default, match[1], from)
	}},
	{regexp.MustCompile(`(?i)\bdefault ((?:yes|no|none)\b|-?\d+(?:\.\d+)?|[A-Za-z]\w*(?:[._]\w+)+)` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), false, func(_ *Schema, found *Key, match []string, from Source) {
		Set(&found.Default, match[1], from)
	}},
	{regexp.MustCompile(`(?i)\boptional\b`), false, func(_ *Schema, found *Key, _ []string, from Source) {
		Set(&found.Required, Required{Need: NeedOptional}, from)
	}},
	{regexp.MustCompile(`(?i)^required (?:if|when) (.+)`), false, func(_ *Schema, found *Key, match []string,
		from Source) {
		Set(&found.Required, Required{Need: NeedWhen, When: match[1]}, from)
	}},
	{regexp.MustCompile(`(?i)^required(?:[.!]| field\b|$)|\bthis field is required\b`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Required, Required{Need: NeedAlways}, from)
		}},
	// root = x: the root scope is the first word of x that is a scope the dumps declare
	{regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), false, func(schema *Schema, found *Key, match []string,
		from Source) {
		for _, word := range strings.FieldsFunc(match[1], func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
		}) {
			if _, declared := schema.Scopes[word]; declared {
				Set(&found.Scope, word, from)
				return
			}
		}
	}},
	// scope:x = … names a saved scope and Brief: key (type) the field below; neither is a blurb
	{regexp.MustCompile(`(?i)^scope:\S+ (?:=|-) `), false, nil},
	{regexp.MustCompile(`(?i)^brief\b`), false, nil},
	// type words, alone (quoted or not) or alone in parentheses; a placeholder's word is read here
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:int|integer|int32|int31|num|number|fixed_point|script value|` +
		`script_value|0\.0-1\.0)(?:\s*\)|"?$)`), false, func(_ *Schema, found *Key, _ []string, from Source) {
		Set(&found.Value, Value{Type: WordNumber}, from)
	}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool|yes/no)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Value, Value{Type: WordBool}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Value, Value{Type: WordDate}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Value, Value{Type: WordText}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Value, Value{Type: WordScope}, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Value, Value{Type: WordLoc}, from)
		}},
	// event ID names the database whose registry row is Events; on action (name) the OnActions one
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:\s*\)|"?$)`), false,
		func(schema *Schema, found *Key, match []string, from Source) {
			for name, database := range schema.Databases {
				if match[1] != "" && database.Naming.Events || match[1] == "" && database.Naming.OnActions {
					Set(&found.Value, Value{Type: WordReference, Database: name}, from)
				}
			}
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:triggers?)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Block, game.BlockTriggers, from)
		}},
	{regexp.MustCompile(`(?:^"?|\(\s*)(?i:effects?)(?:\s*\)|"?$)`), false,
		func(_ *Schema, found *Key, _ []string, from Source) {
			Set(&found.Block, game.BlockEffects, from)
		}},
	// "path/to/file": one quoted word with a slash
	{regexp.MustCompile(`^"[^"\s<>]*/[^"\s<>]*"$`), false, func(_ *Schema, found *Key, _ []string, from Source) {
		Set(&found.Value, Value{Type: WordFile}, from)
	}},
	// a/b/c as a value: its words; - item: blurb and item - blurb: one word per line
	{regexp.MustCompile(`^[\w.]+(?:/[\w.]+)+$`), true, func(_ *Schema, found *Key, match []string, from Source) {
		found.Value.Value.Words = append(found.Value.Value.Words, strings.Split(match[0], "/")...)
		found.Value.From = from
	}},
	{regexp.MustCompile(`^(?:[-*]\s+([\w.]+):\s+[^\s<]|(?:[-*]\s+)?([\w.]+) - \S)`), true,
		func(_ *Schema, found *Key, match []string, from Source) {
			found.Value.Value.Words = append(found.Value.Value.Words, match[1]+match[2])
			found.Value.From = from
		}},
}

// normalize turns a doc, a wiki example or a usage into script: code lines kept (with the rewrite
// catalogue), every other line a # comment verbatim, open blocks closed by indentation.
func normalize(text string, markdown, usage bool) string {
	var out strings.Builder
	var open []int // the indentation of each open block's opening line
	code := func(line string) {
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
		case trimmed == "":
			out.WriteString(line + "\n")
			return
		case commented && (said == "" || !script || read):
			out.WriteString(line + "\n")
			return
		case !script:
			out.WriteString("# " + line + "\n")
			return
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

		// offside: a block still open when a code line returns to its opening line's indentation
		// is closed before that line; a line's own leading } closes one of them
		indent := 0
		for _, char := range body[:len(body)-len(strings.TrimLeft(body, " \t"))] {
			indent++
			if char == '\t' {
				indent += 3 - (indent-1)%4
			}
		}
		closes := 0
		for closes < len(open) && open[len(open)-1-closes] >= indent {
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

	// a usage line that starts with a statement is split before each statement on its own level or
	// its first brace level, skipping placeholders and (notes), so a note trails its argument
	lines := strings.Split(text, "\n")
	if usage {
		var split []string
		for _, line := range lines {
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
		lines = split
	}

	// markdown: goldmark marks headings and lists; a list line is prose unless it is a field, and
	// every other line goes through the island test
	next := 0
	upto := func(until int) {
		for ; next < until; next++ {
			code(lines[next])
		}
	}
	if markdown {
		source, index := []byte(text), jomini.NewLineIndex(text)
		document := goldmark.New().Parser().Parse(gmtext.NewReader(source))
		_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			switch {
			case !entering:
				return ast.WalkContinue, nil
			case node.Kind() == ast.KindHeading:
				for i := 0; i < node.Lines().Len(); i++ {
					segment := node.Lines().At(i)
					upto(index.PositionAt(segment.Start).Line)
					out.WriteString("# ## " + strings.TrimSuffix(string(segment.Value(source)), "\n") + "\n")
					next++
				}
				return ast.WalkSkipChildren, nil
			case node.Kind() != ast.KindList:
				return ast.WalkContinue, nil
			}
			last := -1
			_ = ast.Walk(node, func(inner ast.Node, entering bool) (ast.WalkStatus, error) {
				for i := 0; entering && inner.Type() == ast.TypeBlock && i < inner.Lines().Len(); i++ {
					line := index.PositionAt(inner.Lines().At(i).Start).Line
					if last < 0 {
						upto(line)
					}
					last = max(last, line)
				}
				return ast.WalkContinue, nil
			})
			for ; next <= last; next++ {
				switch trimmed := strings.TrimLeft(lines[next], " \t"); {
				case islands.field.MatchString(trimmed):
					code(lines[next])
				case trimmed == "":
					out.WriteString(lines[next] + "\n")
				default:
					out.WriteString("# " + lines[next] + "\n")
				}
			}
			return ast.WalkSkipChildren, nil
		})
	}
	upto(len(lines))

	// blocks still open at the end of the text close there
	out.WriteString(strings.Repeat("} ", len(open)))
	return out.String()
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
// catalogue; the comment lines no row reads, headings aside, are its blurb.
func (doc doc) claims(statement jomini.Statement) (found Key) {
	catalogue := func(text string) (known bool) {
		for _, phrase := range phrases {
			if match := phrase.pattern.FindStringSubmatch(text); match != nil && (!phrase.alone || !known) {
				known = true
				if phrase.set != nil {
					phrase.set(doc.schema, &found, match, doc.from)
				}
			}
		}
		return known
	}
	_, isAssign := statement.(*jomini.Assignment)

	// a placeholder no type row reads names a database (Named); a quoted text no row reads is prose
	var blurb []string
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		written := doc.parsed.Src[scalar.Range.Start:scalar.Range.End]
		word := strings.TrimSuffix(strings.TrimPrefix(written, `"<`), `>"`)
		placeholder := len(word)+4 == len(written)
		switch {
		case placeholder && !catalogue(word):
			if named := doc.schema.Named(word); named != "" {
				Set(&found.Value, Value{Type: WordReference, Database: named}, doc.from)
			}
		case !placeholder && !catalogue(written) && scalar.Quoted && strings.ContainsAny(scalar.Text, " \t"):
			blurb = append(blurb, scalar.Text)
		}
		if isAssign && doc.usage && found.Block.From != SourceNone {
			Set(&found.Scope, "", doc.from)
		}
	}
	for _, comment := range doc.attached[statement] {
		raw := doc.parsed.Src[comment.Range.Start:comment.Range.End]
		said := strings.TrimSpace(strings.TrimLeft(raw, "# \t"))
		if !catalogue(said) && said != "" && !strings.HasPrefix(strings.TrimLeft(raw[1:], " \t"), "#") {
			blurb = append(blurb, said)
		}
	}
	slices.Sort(found.Value.Value.Words)
	found.Value.Value.Words = slices.Compact(found.Value.Value.Words)
	if len(blurb) > 0 {
		Set(&found.Blurb, strings.Join(blurb, " "), doc.from)
	}
	return found
}

// state writes a statement's claims at a position, which keeps the first declared source that
// writes it.
func (doc doc) state(position *Key, statement jomini.Statement) {
	found := doc.claims(statement)
	position.Written = cmp.Or(position.Written, doc.from)
	Set(&position.Value, found.Value.Value, found.Value.From)
	Set(&position.Block, found.Block.Value, found.Block.From)
	Set(&position.Scope, found.Scope.Value, found.Scope.From)
	Set(&position.Required, found.Required.Value, found.Required.From)
	Set(&position.Default, found.Default.Value, found.Default.From)
	Set(&position.Blurb, found.Blurb.Value, found.Blurb.From)
}

// join reads a doc or wiki example into a database's tree. A key at a position vanilla writes gives
// it its claims; a list word's go to the position holding it. A key vanilla writes nowhere below
// where it stands (an example entry's name) is transparent: its block is read in its place, and at
// the top its claims are the entry's. A doc never makes a position.
func (schema *Schema) join(parsed *jomini.Result, database *Database, from Source) {
	attached, _ := parsed.Attach()
	doc, seen := doc{schema, parsed, attached, from, false}, map[*Key]map[string]bool{}
	var gather func(names map[string]bool, key *Key)
	gather = func(names map[string]bool, key *Key) {
		for name, child := range key.Keys {
			names[name] = names[name] || child.Seen.In > 0
			gather(names, child)
		}
	}
	reader := Reader{Names: alternatives, Visit: func(step *Step) bool {
		_, isAssign := step.Statement.(*jomini.Assignment)
		if under := step.Place.Key; under != nil && seen[under] == nil {
			seen[under] = map[string]bool{}
			gather(seen[under], under)
		}
		switch {
		case doc.failed(step.Statement):
			return false
		case isAssign && step.Command == nil && !seen[step.Place.Key][collapse(step.Name)]:
			if step.Inner = step.Place; step.Holder == nil {
				doc.state(step.Place.Key, step.Statement)
			}
		case step.Key != nil && step.Command == nil && step.Key.Seen.In > 0:
			doc.state(step.Key, step.Statement)
		}
		return true
	}}
	schema.Read(reader, nil, parsed.Root.Statements, schema.Start(database))
}

// usage reads a dump usage into a command's arguments. The statement named written (the command's
// own name, or a sibling command's the usage is written under) gives the command's own claims; each
// key in its block is an argument whatever else its name is; each block below is read by the walker.
func (schema *Schema) usage(parsed *jomini.Result, command *Command, written string) {
	attached, _ := parsed.Attach()
	doc := doc{schema, parsed, attached, SourceDumps, true}
	top := &Key{Keys: map[string]*Key{collapse(written): command.Args}}
	reader := Reader{Create: true, Names: alternatives, Visit: func(step *Step) bool {
		switch {
		case doc.failed(step.Statement), step.Holder == nil && step.Key != command.Args:
			return false
		case step.Key == command.Args:
			step.Inner.Kind = game.BlockFields
		}
		if step.Key != nil && step.Command == nil {
			doc.state(step.Key, step.Statement)
		}
		return true
	}}
	schema.Read(reader, nil, parsed.Root.Statements, Place{Key: top, Kind: game.BlockFields, InEntry: true})
}

// alternatives is a docs key's names: a/b/c and a|b|c name alternatives.
func alternatives(key string) []string {
	return strings.FieldsFunc(key, func(char rune) bool { return char == '/' || char == '|' })
}
