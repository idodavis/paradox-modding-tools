// docs.go is the documentation dialect: parseDocScript, the one reader of docscript that the install's docs, the
// wiki's examples and the dumps' usages share; step 3, the docs and examples parsed to trees with every key they
// write noted for the entry detector; step 6, their claims merged into the entry trees, after vanilla.

package schema

import (
	"context"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// DocNode is one statement of docscript with its prose: its key and value slot as written, the comments that
// belong to it (above it, trailing it, or the rest of its line), and the statements of its block.
type DocNode struct {
	Key      string // the key as written; "" for a bare value (a list item, a placeholder line)
	Value    string // the value slot as written: a <placeholder>, a word, alternatives, a "quoted text"; "" for none
	Block    bool   // the statement opens a block
	Prose    []string
	Children []*DocNode
}

// doc is one doc or wiki example, parsed once, with the database it documents.
type doc struct {
	database string
	tree     *DocNode // its Children are the top statements
	from     Source
}

// docToken is one token of a docscript line.
type docToken struct {
	kind docTokenKind
	text string // a word, a <placeholder> or "quoted text" with its brackets or quotes, a (note)'s inside, a comment
	col  int    // where it starts on the line
}

// docTokenKind tells the tokens of a line apart.
type docTokenKind uint8

const (
	docWord docTokenKind = iota
	docQuoted
	docPlaceholder
	docNote
	docOpen
	docClose
	docOperator
	docComment
)

// docParse is one text being read into a tree: the open blocks, and the comments waiting for a statement.
type docParse struct {
	root      *DocNode
	open      []docBlock // the blocks open, the root first, innermost last
	offside   bool       // the text leaves blocks open: a code line back at an open block's indentation closes it
	run       []string   // the whole-line comments on consecutive lines so far, for the next statement
	runLine   int        // the line the run ends on
	trailing  *DocNode   // the statement whose trailing comment the next whole-line comment may continue
	trailLine int
	trailCol  int
}

// docBlock is one open block: its node, and the indentation of the line that opened it.
type docBlock struct {
	node   *DocNode
	indent int
}

// docPhrase reads one claim from a comment's text or from a value as written: the kind of value or of block
// the pattern names, else set, which writes what the match says. A phrase marked alone reads a text only when
// no other phrase does; one that names nothing marks a text that claims nothing (a saved scope, a label, a line
// leading into the next).
type docPhrase struct {
	pattern *regexp.Regexp
	alone   bool
	kind    ExpectedKind
	block   game.Block
	set     func(s *Schema, found *Claims, match []string, from Source)
}

// docMerge is one doc being merged into a database's tree.
type docMerge struct {
	schema *Schema
	from   Source
}

// docPhrases is the phrase table, in the order the phrases are tried.
var docPhrases = []docPhrase{
	// default is x, defaults to x, (default = x), default: x, and default x when x is a value: one word
	// that ends its clause
	{pattern: regexp.MustCompile(`(?i)\bdefaults?(?: is| to| =|:) ['"\x60]?([^\s,;()'"\x60]+?)['"\x60]?` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), set: func(_ *Schema, found *Claims, match []string, from Source) {
		found.Default.Set(match[1], from)
	}},
	{pattern: regexp.MustCompile(`(?i)\bdefault ((?:yes|no|none)\b|-?\d+(?:\.\d+)?|[A-Za-z]\w*(?:[._]\w+)+)` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), set: func(_ *Schema, found *Claims, match []string, from Source) {
		found.Default.Set(match[1], from)
	}},
	{pattern: regexp.MustCompile(`(?i)\boptional\b`), set: func(_ *Schema, found *Claims, _ []string, from Source) {
		found.Required.Set(Requirement{Level: LevelOptional}, from)
	}},
	{pattern: regexp.MustCompile(`(?i)^required (?:if|when) (.+)`), set: func(_ *Schema, found *Claims, match []string,
		from Source) {
		found.Required.Set(Requirement{Level: LevelConditional, When: match[1]}, from)
	}},
	{pattern: regexp.MustCompile(`(?i)^required(?:[.!]| field\b|$)|\bthis field is required\b`),
		set: func(_ *Schema, found *Claims, _ []string, from Source) {
			found.Required.Set(Requirement{Level: LevelAlways}, from)
		}},
	// root = x: the root scope is the first word of x that is a scope the dumps declare
	{pattern: regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), set: func(s *Schema, found *Claims, match []string,
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
	{pattern: regexp.MustCompile(`(?i)^scope:\S+ (?:=|-) `)},
	{pattern: regexp.MustCompile(`(?i)^brief\b`)},
	{pattern: regexp.MustCompile(`:$`)},
	// type words opening the text or a parenthesis ("Localization key that …", ( bool, optional )), not one
	// of a/b alternatives; text, name and scope only as the whole text: prose opens with them
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:(?:scripted )?(?:int|integer|int32|int31|num|number|float|double|` +
		`fixed[ _]point)|scripted? value|script_value|0\.0-1\.0)(?:[^\w/]|$)`), kind: ExpectsNumber},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool(?:ean)?|yes/no)(?:[^\w/]|$)`), kind: ExpectsBool},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:[^\w/]|$)`), kind: ExpectsDate},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), kind: ExpectsString},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), kind: ExpectsScope},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:[^\w/]|$)`), kind: ExpectsLoc},
	// event ID names the database whose registry row is Events; on action (name) the OnActions one
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:[^\w/]|$)`),
		set: func(s *Schema, found *Claims, match []string, from Source) {
			for name, database := range s.Databases {
				if match[1] != "" && database.Row.Events || match[1] == "" && database.Row.OnActions {
					found.Value.Set(Expected{Kind: ExpectsReference, Database: name}, from)
				}
			}
		}},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:triggers?)(?:[^\w/]|$)`), block: game.BlockTriggers},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:effects?)(?:[^\w/]|$)`), block: game.BlockEffects},
	// "path/to/file": one quoted word with a slash
	{pattern: regexp.MustCompile(`^"[^"\s<>]*/[^"\s<>]*"$`), kind: ExpectsFile},
	// - item: blurb and item - blurb: one word per line
	{pattern: regexp.MustCompile(`^(?:[-*]\s+([\w.]+):\s+[^\s<]|(?:[-*]\s+)?([\w.]+) - \S)`), alone: true,
		set: func(_ *Schema, found *Claims, match []string, from Source) {
			found.Value.Value.Choices = append(found.Value.Value.Choices, match[1]+match[2])
			found.Value.Source = from
		}},
}

// alternatives matches a value written as alternatives (yes/no/all, num/all).
var alternatives = regexp.MustCompile(`^[\w.]+(?:/[\w.]+)+$`)

// keyWord matches a word that can be a statement's key: it starts with a word character, @ or $ (a - before
// it allowed) and ends in one, with alternatives joined by / or | (days/months/years).
var keyWord = regexp.MustCompile(`^-?[\w@$](?:[\w.:@$/|-]*[\w$])?$`)

// parseDocScript reads the lines of a doc, a wiki example or a dump usage into a tree. Each line is a
// statement, a commented-out statement, a comment, prose, a brace, a heading or a fence; a statement's value
// slot is one token (a <placeholder> to its matching >, a "quoted text", a word, alternatives), what follows it
// on the line is its prose; braces build the tree, several statements on one line included, and blocks still
// open at the end close there; in a text that leaves blocks open, a code line back at an open block's
// indentation closes that block first. A comment trailing a line belongs to the first statement starting on it; a
// whole-line comment under a trailing comment, in its column, continues it; a run of whole-line comments on
// consecutive lines leads the statement on the next line; a heading or an empty comment keeps a run together but
// says nothing.
func parseDocScript(lines []string) *DocNode {
	parse := &docParse{root: &DocNode{}, runLine: -2, trailLine: -2}
	parse.open = []docBlock{{node: parse.root}}
	braces := 0
	for _, line := range lines {
		braces += strings.Count(line, "{") - strings.Count(line, "}")
	}
	parse.offside = braces != 0
	for n, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			parse.run = nil
			continue
		}
		// a commented-out statement reads as one, unless a phrase opens it (# default = yes, # root = character)
		text, col, inBlock, commented := line, 0, len(parse.open) > 1, strings.HasPrefix(trimmed, "#")
		if commented {
			text, col, inBlock = trimmed[1:], len(line)-len(trimmed), false
		}
		if tokens := tokenizeDocLine(text); isDocCode(tokens, inBlock) && (!commented || !opensWithPhrase(strings.TrimSpace(text))) {
			parse.statements(n, indentOf(strings.Replace(line, "#", "", 1)), tokens)
		} else {
			parse.comment(n, col, text)
		}
	}
	return parse.root
}

// opensWithPhrase reports whether a phrase of the table opens a text.
func opensWithPhrase(text string) bool {
	return slices.ContainsFunc(docPhrases, func(phrase docPhrase) bool {
		where := phrase.pattern.FindStringIndex(text)
		return where != nil && where[0] == 0
	})
}

// tokenizeDocLine splits a line into its tokens. A < followed by a non-blank and closed by a > on the line is a
// placeholder; a ( or [ closed on the line is a note; a # opens the comment that is the rest of the line; a
// bullet's - or * is left out, so - key: <placeholder> prose reads as the statement it documents.
func tokenizeDocLine(line string) (tokens []docToken) {
	closers := map[byte]byte{'(': ')', '[': ']'}
	if bullet := strings.TrimLeft(line, " \t"); strings.HasPrefix(bullet, "- ") || strings.HasPrefix(bullet, "* ") {
		line = strings.Replace(line, bullet[:1], " ", 1)
	}
	for i := 0; i < len(line); {
		char := line[i]
		switch {
		case char == ' ' || char == '\t':
			i++
		case char == '#':
			return append(tokens, docToken{docComment, line[i+1:], i})
		case char == '{':
			tokens, i = append(tokens, docToken{docOpen, "{", i}), i+1
		case char == '}':
			tokens, i = append(tokens, docToken{docClose, "}", i}), i+1
		case char == '"':
			end := len(line) // an unclosed quote runs to the end of the line
			if closing := strings.IndexByte(line[i+1:], '"'); closing >= 0 {
				end = i + 2 + closing
			}
			tokens, i = append(tokens, docToken{docQuoted, line[i:end], i}), end
		case char == '<' && i+1 < len(line) && line[i+1] != ' ' && line[i+1] != '=' && strings.IndexByte(line[i+1:], '>') >= 0:
			end := i + 2 + strings.IndexByte(line[i+1:], '>')
			tokens, i = append(tokens, docToken{docPlaceholder, line[i:end], i}), end
		case closers[char] != 0 && strings.IndexByte(line[i+1:], closers[char]) >= 0:
			end := i + 1 + strings.IndexByte(line[i+1:], closers[char])
			tokens, i = append(tokens, docToken{docNote, strings.TrimSpace(line[i+1 : end]), i}), end+1
		case strings.ContainsRune("=<>!?", rune(char)):
			end := i + 1
			if end < len(line) && line[end] == '=' {
				end++
			}
			tokens, i = append(tokens, docToken{docOperator, line[i:end], i}), end
		default:
			end := i
			for end < len(line) && !strings.ContainsRune(" \t{}#\"=<>", rune(line[end])) {
				end++
			}
			tokens, i = append(tokens, docToken{docWord, line[i:end], i}), end
		}
	}
	return joinAlternatives(tokens)
}

// joinAlternatives makes a | b | c written with blanks one word of alternatives.
func joinAlternatives(tokens []docToken) []docToken {
	for i := 1; i+1 < len(tokens); i++ {
		if tokens[i].kind == docWord && tokens[i].text == "|" && tokens[i-1].kind == docWord && tokens[i+1].kind == docWord {
			tokens[i-1].text += "|" + tokens[i+1].text
			tokens = slices.Delete(tokens, i, i+2)
			i--
		}
	}
	return tokens
}

// isDocCode reports whether a line's tokens are script: a brace, a statement (a key before an operator or a
// brace), a placeholder standing alone, or, inside a block, a word standing alone (a list element).
func isDocCode(tokens []docToken, inBlock bool) bool {
	if len(tokens) == 0 || tokens[0].kind == docComment {
		return false
	}
	alone := len(tokens) == 1 || tokens[1].kind == docComment
	switch first := tokens[0]; {
	case first.kind == docOpen || first.kind == docClose:
		return true
	case first.kind == docOperator || first.kind == docNote:
		return false
	case first.kind == docWord && isFieldKey(tokens):
		return true
	case first.kind == docWord && !keyWord.MatchString(first.text):
		return false
	case len(tokens) > 1 && (tokens[1].kind == docOperator || tokens[1].kind == docOpen):
		return true
	case first.kind == docPlaceholder:
		return strings.Contains(first.text, "=") || !slices.ContainsFunc(tokens[1:], func(token docToken) bool {
			return token.kind != docClose && token.kind != docComment
		})
	}
	return inBlock && alone
}

// isFieldKey reports a bullet documenting a key, key: <placeholder> prose, at the front of the tokens.
func isFieldKey(tokens []docToken) bool {
	key := strings.TrimSuffix(tokens[0].text, ":")
	return len(tokens) > 1 && len(key) < len(tokens[0].text) && keyWord.MatchString(key) && tokens[1].kind == docPlaceholder
}

// indentOf is the width of a line's leading blanks, a tab reaching the next multiple of four.
func indentOf(line string) (indent int) {
	for _, char := range line[:len(line)-len(strings.TrimLeft(line, " \t"))] {
		indent++
		if char == '\t' {
			indent += 3 - (indent-1)%4
		}
	}
	return indent
}

// comment keeps a whole-line comment: the continuation of the trailing comment above it in its column, else
// part of the run leading the next statement. A heading (a text opening with # or written in capitals) and an
// empty comment keep the run together but say nothing.
func (parse *docParse) comment(line, col int, text string) {
	said := strings.TrimSpace(strings.TrimLeft(text, "# \t"))
	if strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") || strings.ToUpper(said) == said && strings.ToLower(said) != said {
		said = ""
	}
	if parse.trailing != nil && line == parse.trailLine+1 && col == parse.trailCol {
		parse.trailing.Prose = append(parse.trailing.Prose, said)
		parse.trailLine = line
		return
	}
	if line != parse.runLine+1 {
		parse.run = nil
	}
	parse.run, parse.runLine = append(parse.run, said), line
}

// statements reads one code line's tokens into the tree: each statement becomes a node of the innermost open
// block, braces open and close blocks, a note or the words after a value or a closed block are the statement's
// prose, and the line's comment and the run above lead its first statement.
func (parse *docParse) statements(line, indent int, tokens []docToken) {
	var first, last *DocNode
	if parse.offside {
		// a code line back at an open block's indentation closes that block first; the line's own leading } is one
		// of those closes
		closes := 0
		for closes < len(parse.open)-1 && parse.open[len(parse.open)-1-closes].indent >= indent {
			closes++
		}
		if closes > 0 && tokens[0].kind == docClose {
			closes--
		}
		for ; closes > 0; closes-- {
			last = parse.closeBlock()
		}
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token.kind {
		case docClose:
			if len(parse.open) > 1 {
				last = parse.closeBlock()
			}
		case docOpen:
			if last != nil && !last.Block && last.Key != "" {
				last.Block = true
			} else {
				last = parse.add(&DocNode{Block: true})
			}
			parse.open = append(parse.open, docBlock{last, indent})
		case docComment:
			if first != nil {
				first.Prose = append(first.Prose, strings.TrimSpace(strings.TrimLeft(token.text, "# \t")))
				parse.trailing, parse.trailLine, parse.trailCol = first, line, token.col
			}
		case docNote:
			if last != nil {
				last.Prose = append(last.Prose, token.text)
			}
		case docOperator:
		default:
			node, taken := parse.statement(tokens[i:], last)
			i += taken - 1
			if node != nil && first == nil {
				first = node
			}
			if node != nil {
				last = node
			}
		}
	}
	if first != nil && parse.runLine == line-1 {
		first.Prose = append(slices.Clone(parse.run), first.Prose...)
	}
	parse.run = nil
}

// closeBlock closes the innermost open block and returns its statement, which what follows the brace belongs to.
func (parse *docParse) closeBlock() *DocNode {
	closed := parse.open[len(parse.open)-1].node
	parse.open = parse.open[:len(parse.open)-1]
	return closed
}

// statement reads one statement from the front of tokens: a key before an operator or a brace with its value
// slot, a <key=value> placeholder, a bullet's key: <placeholder>, or a bare value; nil when the tokens are prose
// of the statement before, which takes them, or an alternative's separator. It returns how many tokens it took.
func (parse *docParse) statement(tokens []docToken, last *DocNode) (*DocNode, int) {
	first := tokens[0]
	if key, value, cut := strings.Cut(strings.Trim(first.text, "<>"), "="); first.kind == docPlaceholder && cut {
		return parse.add(&DocNode{Key: strings.TrimSpace(key), Value: "<" + strings.TrimSpace(value) + ">"}), 1
	}
	if first.kind == docWord && first.text == "/" {
		return nil, 1
	}
	if first.kind == docWord && isFieldKey(tokens) {
		return parse.add(&DocNode{Key: strings.TrimSuffix(first.text, ":"), Value: tokens[1].text}), 2
	}
	if len(tokens) == 1 || tokens[1].kind != docOperator && tokens[1].kind != docOpen || first.kind == docWord && !keyWord.MatchString(first.text) {
		// after a key = <placeholder> statement on this line, or after a block that closed on it, words are its
		// prose up to a brace or the comment; a placeholder, or a word standing for a block (effects...), is a
		// value of its own
		placeholder := last != nil && last.Key != "" && !last.Block && strings.HasPrefix(last.Value, "<")
		closed := last != nil && last.Key != "" && last.Block && parse.open[len(parse.open)-1].node != last
		if first.kind != docPlaceholder && !strings.Contains(first.text, "...") && (placeholder || closed) {
			words, notes, taken := wordsOf(tokens)
			last.Prose = append(append(last.Prose, strings.Join(words, " ")), notes...)
			return nil, max(taken, 1)
		}
		return parse.add(&DocNode{Value: first.text}), 1
	}
	node, taken := &DocNode{Key: strings.Trim(first.text, `"`)}, 2
	switch {
	case tokens[1].kind == docOpen:
		taken = 1 // the brace is read by the caller
	case len(tokens) > 2 && tokens[2].kind == docPlaceholder:
		node.Value, taken = tokens[2].text, 3
	case len(tokens) > 2 && (tokens[2].kind == docWord || tokens[2].kind == docQuoted):
		// a value of several words is one quoted text (ai_value = script value), unless a statement follows on
		// the line (modifier = X days = Y); the notes among them are the statement's prose
		words, notes, took := wordsOf(tokens[2:])
		node.Value, node.Prose, taken = strings.Join(words, " "), notes, 2+took
		if len(words) > 1 {
			node.Value = `"` + strings.Trim(node.Value, `"`) + `"`
		}
	}
	return parse.add(node), taken
}

// wordsOf takes the words and the notes at the front of tokens, up to a brace, a placeholder or the comment; when
// an operator follows them on the line, the last word is the next statement's key and is left (modifier = name
// days/weeks/months/years = int), and a word standing for a block after the first (modifier = Y effects...) is a
// value of its own. A dash leading the words introduces them (<name> - name of the flag).
func wordsOf(tokens []docToken) (words, notes []string, taken int) {
	for taken < len(tokens) && (tokens[taken].kind == docWord || tokens[taken].kind == docQuoted || tokens[taken].kind == docNote) &&
		(taken == 0 || !strings.Contains(tokens[taken].text, "...")) {
		taken++
	}
	if taken < len(tokens) && tokens[taken].kind == docOperator {
		taken--
	}
	for i, token := range tokens[:taken] {
		switch {
		case token.kind == docNote:
			notes = append(notes, token.text)
		case i == 0 && token.text == "-":
		default:
			words = append(words, token.text)
		}
	}
	return words, notes, taken
}

// add appends a node to the innermost open block.
func (parse *docParse) add(node *DocNode) *DocNode {
	holder := parse.open[len(parse.open)-1].node
	holder.Children = append(holder.Children, node)
	return node
}

// parseDocs is step 3: each prose file of the install, read into its own folder's database, or the folder
// beside it that it is named after (history/_characters.info); one under a row above its folder is not read,
// nor one over a megabyte. A Markdown heading (a # at the start of a .md line) is no comment. Then the wiki's
// examples, by the declared names. Every key a tree writes is noted in its database's Fields, for the entry
// detector. A sanitized wiki page keeps no version label, so the wiki is never stale.
func (schema *Schema) parseDocs(ctx context.Context, input Input) (docs []doc, err error) {
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
		if err != nil || len(text) > 1<<20 {
			continue
		}
		lines := strings.Split(text, "\n")
		if strings.EqualFold(path.Ext(file.Rel), ".md") {
			for n, line := range lines {
				if strings.HasPrefix(line, "#") {
					lines[n] = ""
				}
			}
		}
		docs = append(docs, doc{name, parseDocScript(lines), SourceDocs})
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
			docs = append(docs, doc{name, parseDocScript(strings.Split(text, "\n")), SourceWiki})
		}
	}
	for _, written := range docs {
		noteFields(written.tree, schema.Databases[written.database].Fields)
	}
	return docs, nil
}

// noteFields records every key name a tree writes, at any depth, by its position name.
func noteFields(node *DocNode, fields map[string]bool) {
	for _, child := range node.Children {
		for _, name := range keyNames(child.Key) {
			fields[positionName(name)] = true
		}
		noteFields(child, fields)
	}
}

// keyNames returns the names a docs key stands for: a/b/c and a|b|c name alternatives.
func keyNames(key string) []string {
	return strings.FieldsFunc(key, func(char rune) bool { return char == '/' || char == '|' })
}

// claimsOf collects one node's claims: its value slot (a placeholder's word through the phrase table, else the
// database it names; any other value through the table, a quoted text with words left as a blurb), the block
// placeholders its block lists (limit = { <display_triggers> }), then each line of its prose through the table,
// what the table leaves of a line being blurb.
func (schema *Schema) claimsOf(node *DocNode, from Source) (found Claims) {
	var blurb []string
	if word, isPlaceholder := strings.CutPrefix(node.Value, "<"); isPlaceholder {
		word = strings.TrimSuffix(word, ">")
		named := schema.DatabaseNamed(word)
		if schema.claimPhrases(&found, word, from) == word && named != "" {
			found.Value.Set(Expected{Kind: ExpectsReference, Database: named}, from)
		}
	} else if node.Value != "" {
		left := schema.claimPhrases(&found, node.Value, from)
		if text := strings.Trim(node.Value, `"`); isBlurb(left) && strings.ContainsAny(text, " \t") {
			blurb = append(blurb, text)
		}
	}
	for _, child := range node.Children {
		if child.Key == "" && strings.HasPrefix(child.Value, "<") {
			last := strings.TrimSuffix(child.Value[strings.LastIndexAny(child.Value, "<_ ")+1:], ">")
			if kind, named := game.Placeholders[strings.ToLower(last)]; named {
				found.Block.Set(kind, from)
			}
		}
	}
	for _, said := range node.Prose {
		if left := schema.claimPhrases(&found, said, from); isBlurb(left) {
			blurb = append(blurb, said)
		}
	}
	slices.Sort(found.Value.Value.Choices)
	found.Value.Value.Choices = slices.Compact(found.Value.Value.Choices)
	if len(blurb) > 0 {
		found.Blurb.Set(strings.Join(blurb, " "), from)
	}
	return found
}

// claimPhrases writes into found every claim the phrase table reads from a text, and returns what is left for
// a blurb: the text no phrase read; nothing when a phrase read it whole, a phrase marked it as claiming nothing,
// or it was an a/b/c list, whose words became choices.
func (schema *Schema) claimPhrases(found *Claims, text string, from Source) (left string) {
	left, claimsNothing := text, false
	for _, phrase := range docPhrases {
		match := phrase.pattern.FindStringSubmatch(text)
		if match == nil || phrase.alone && left != text {
			continue
		}
		left = strings.Replace(left, match[0], "", 1)
		switch {
		case phrase.kind != ExpectsUnknown:
			found.Value.Set(Expected{Kind: phrase.kind}, from)
		case phrase.block != game.BlockUnknown:
			found.Block.Set(phrase.block, from)
		case phrase.set != nil:
			phrase.set(schema, found, match, from)
		default:
			claimsNothing = true
		}
	}
	switch {
	case left == text && alternatives.MatchString(text):
		// a/b/c no phrase reads whole is read word by word: a type word is its type, the rest are choices
		for word := range strings.SplitSeq(text, "/") {
			if schema.claimPhrases(found, word, from) == word {
				found.Value.Value.Choices, found.Value.Source = append(found.Value.Value.Choices, word), from
			}
		}
		return ""
	case claimsNothing:
		return ""
	}
	return left
}

// isBlurb reports whether what the table left of a text still has words in it: prose, kept as a blurb.
func isBlurb(left string) bool { return strings.ContainsFunc(left, unicode.IsLetter) }

// mergeDocs is step 6: the docs' and wiki's claims merged into the databases they document, after vanilla.
func (schema *Schema) mergeDocs(docs []doc) {
	for _, written := range docs {
		merge := &docMerge{schema: schema, from: written.from}
		in := schema.EntryContext(schema.Databases[written.database])
		for _, node := range written.tree.Children {
			merge.node(node, in, true)
		}
	}
}

// node merges one node where its key reads at a context. A bare value's claims are the holder's. A key vanilla
// writes nowhere below the holder is a template (template). A key read as a position vanilla writes gets the
// node's claims, then its block merges where the walker reads that key's block; a key read as a command or a
// link gets none, and its block merges where the walker reads it. A doc never makes a position.
func (merge *docMerge) node(node *DocNode, in BlockContext, top bool) {
	if node.Key == "" {
		if in.Position != nil && in.Position.Vanilla.In > 0 {
			merge.set(in.Position, node)
		}
		for _, child := range node.Children { // a block with no key ({ { name = x } }) reads in place
			merge.node(child, in, false)
		}
		return
	}
	for _, name := range keyNames(node.Key) {
		placement := merge.schema.PlaceKey(in, nil, name, false)
		switch {
		case placement.Command == nil && (in.Position == nil || !writtenBelow(in.Position, positionName(name))):
			merge.template(node, in, top)
			continue
		case placement.Position != nil && placement.Command == nil && placement.Position.Vanilla.In > 0:
			merge.set(placement.Position, node)
		}
		for _, child := range node.Children {
			merge.node(child, placement.Inner, false)
		}
	}
}

// template merges a node whose key vanilla writes nowhere below where it stands: an example entry's name, a
// <key>. At the doc's top its claims are the holder's, where the holder is written as more than a block or the
// node has one. Its block merges in the holder's place where the holder has a child the block writes, else
// under each child of the holder that has one (my_phase_key = { }); else nowhere.
func (merge *docMerge) template(node *DocNode, in BlockContext, top bool) {
	if in.Position != nil && top && (node.Block || in.Position.Vanilla.Shapes != ShapeBlock) {
		merge.set(in.Position, node)
	}
	holds := func(at *Position) bool {
		return node.Block && slices.ContainsFunc(node.Children, func(child *DocNode) bool {
			return child.Key != "" && at.Child(child.Key) != nil
		})
	}
	if in.Position == nil || holds(in.Position) {
		for _, child := range node.Children {
			merge.node(child, in, false)
		}
		return
	}
	for _, below := range in.Position.Children {
		if holds(below) {
			in.Position = below
			for _, child := range node.Children {
				merge.node(child, in, false)
			}
		}
	}
}

// writtenBelow reports whether vanilla writes a position of a name anywhere below one.
func writtenBelow(position *Position, name string) bool {
	for written, child := range position.Children {
		if written == name && child.Vanilla.In > 0 || writtenBelow(child, name) {
			return true
		}
	}
	return false
}

// set writes a node's claims into a position. A doc's block whose shape nothing names states only that the key
// takes a block, and a doc's literal states its kind (expiration_days = 3650).
func (merge *docMerge) set(position *Position, node *DocNode) {
	found := merge.schema.claimsOf(node, merge.from)
	if node.Block && found.Block.Source == SourceNone {
		found.Value.Set(Expected{Kind: ExpectsBlock}, merge.from)
	}
	if node.Value != "" && !strings.HasPrefix(node.Value, "<") && !strings.HasPrefix(node.Value, `"`) {
		literals := map[Shapes]ExpectedKind{ShapeNumber: ExpectsNumber, ShapeBool: ExpectsBool, ShapeDate: ExpectsDate}
		if kind, literal := literals[shapeOf(&jomini.Scalar{Text: node.Value})]; literal {
			found.Value.Set(Expected{Kind: kind}, merge.from)
		}
	}
	position.declare(found, merge.from)
}
