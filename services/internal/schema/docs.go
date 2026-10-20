// docs.go reads the prose the install ships beside its script and the wiki's examples, in the documentation
// dialect: parseDocScript, the one reader of docscript that the docs, the examples and the dumps' usages share.
// The docs and examples are parsed to trees with every key they write noted for the entry detector; their claims
// are merged into the entry trees after vanilla.

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

// DocNode is one statement of docscript with its prose.
type DocNode struct {
	Key      string     // the key as written; "" for a bare value (a list item, a placeholder line)
	Value    string     // the value slot as written (a <placeholder>, a word, alternatives, a "quoted text"); "" none
	Block    bool       // the statement opens a block
	Prose    []string   // the comments that belong to it: above it, trailing it, or the rest of its line
	Children []*DocNode // the statements of its block
}

// doc is one doc or wiki example, parsed once.
type doc struct {
	database string      // the database it documents
	tree     *DocNode    // its Children are the top statements
	source   game.Source // docs or wiki
}

// docToken is one token of a docscript line.
type docToken struct {
	kind docTokenKind // word, placeholder, note, comment, brace...
	text string       // a word; a <placeholder> or "quoted text" as written; a (note)'s inside; a comment
	col  int          // where it starts on the line
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

// docParse is one text being read into a tree.
type docParse struct {
	root      *DocNode   // the tree
	blocks    []docBlock // the blocks open, the root first, innermost last
	offside   bool       // the text leaves blocks open: a code line back at an open block's indentation closes it
	run       []string   // the whole-line comments on consecutive lines so far, for the next statement
	runLine   int        // the line the run ends on
	trailing  *DocNode   // the statement whose trailing comment the next whole-line comment may continue
	trailLine int        // the line that trailing comment is on
	trailCol  int        // the column it starts at
}

// docBlock is one open block.
type docBlock struct {
	node   *DocNode // the statement that opened it
	indent int      // the indentation of its line
}

// docPhrase reads one claim from a comment's text or a value as written; one that claims nothing marks a text that
// says nothing (a saved scope, a label, a line leading into the next).
type docPhrase struct {
	pattern   *regexp.Regexp // what the text must match
	fallback  bool           // read a text only when no other phrase does
	valueType ValueType      // the kind of value the pattern names
	shape     game.Shape     // the shape of block the pattern names

	// claim writes what the match says.
	claim func(schema *Schema, claims *Claims, match []string, source game.Source)
}

// docPhrases is the phrase table, in the order the phrases are tried. jomini keeps a comment's text as written
// and reads no meaning from it; this table is how the docs' reader gets a claim out of one.
var docPhrases = []docPhrase{
	// default is x, defaults to x, (default = x), default: x, and default x when x is a value: one word
	// that ends its clause
	{pattern: regexp.MustCompile(`(?i)\bdefaults?(?: is| to| =|:) ['"\x60]?([^\s,;()'"\x60]+?)['"\x60]?` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), claim: func(_ *Schema, claims *Claims, match []string, source game.Source) {
		claims.Default.Set(match[1], source)
	}},
	{pattern: regexp.MustCompile(`(?i)\bdefault ((?:yes|no|none)\b|-?\d+(?:\.\d+)?|[A-Za-z]\w*(?:[._]\w+)+)` +
		`(?:[,;)]|\.(?:\s|$)|\s+-\s|$)`), claim: func(_ *Schema, claims *Claims, match []string, source game.Source) {
		claims.Default.Set(match[1], source)
	}},
	{pattern: regexp.MustCompile(`(?i)\boptional\b`), claim: func(_ *Schema, claims *Claims, _ []string, source game.Source) {
		claims.Required.Set(Requirement{Level: LevelOptional}, source)
	}},
	{pattern: regexp.MustCompile(`(?i)^required (?:if|when) (.+)`), claim: func(_ *Schema, claims *Claims, match []string,
		source game.Source) {
		claims.Required.Set(Requirement{Level: LevelConditional, Condition: match[1]}, source)
	}},
	{pattern: regexp.MustCompile(`(?i)^required(?:[.!]| field\b|$)|\bthis field is required\b`),
		claim: func(_ *Schema, claims *Claims, _ []string, source game.Source) {
			claims.Required.Set(Requirement{Level: LevelAlways}, source)
		}},
	// root = x: the root scope is the first word of x that is a scope the dumps declare
	{pattern: regexp.MustCompile(`(?i)^root (?:=|-) (.+)`), claim: func(schema *Schema, claims *Claims, match []string,
		source game.Source) {
		for _, word := range strings.FieldsFunc(match[1], func(char rune) bool {
			return !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_'
		}) {
			if _, declared := schema.Scopes[word]; declared {
				claims.Scope.Set(word, source)
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
		`fixed[ _]point)|scripted? value|script_value|0\.0-1\.0)(?:[^\w/]|$)`), valueType: ValueNumber},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:bool(?:ean)?|yes/no)(?:[^\w/]|$)`), valueType: ValueBool},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:date)(?:[^\w/]|$)`), valueType: ValueDate},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:string|name|text)(?:\s*\)|"?$)`), valueType: ValueString},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:scope)(?:\s*\)|"?$)`), valueType: ValueScope},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:loc key|localization[ _]key)(?:[^\w/]|$)`), valueType: ValueLoc},
	// event ID names the events database; on action (name) the on_actions one
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:(event[ _]id)|on[ _]action(?: name)?)(?:[^\w/]|$)`),
		claim: func(schema *Schema, claims *Claims, match []string, source game.Source) {
			database := schema.Events
			if match[1] == "" {
				database = schema.findHousingDatabase("on_actions")
			}
			if database != "" {
				claims.Value.Set(Expected{Type: ValueReference, Database: database}, source)
			}
		}},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:triggers?)(?:[^\w/]|$)`), shape: game.ShapeTriggers},
	{pattern: regexp.MustCompile(`(?:^"?|\(\s*)(?i:effects?)(?:[^\w/]|$)`), shape: game.ShapeEffects},
	// "path/to/file": one quoted word with a slash
	{pattern: regexp.MustCompile(`^"[^"\s<>]*/[^"\s<>]*"$`), valueType: ValueFile},
	// - item: blurb and item - blurb: one word per line
	{pattern: regexp.MustCompile(`^(?:[-*]\s+([\w.]+):\s+[^\s<]|(?:[-*]\s+)?([\w.]+) - \S)`), fallback: true,
		claim: func(_ *Schema, claims *Claims, match []string, source game.Source) {
			claims.Value.Value.Choices = append(claims.Value.Value.Choices, match[1]+match[2])
			claims.Value.Source = source
		}},
}

// alternatives matches a value written as alternatives (yes/no/all, num/all).
var alternatives = regexp.MustCompile(`^[\w.]+(?:/[\w.]+)+$`)

// exampleTail matches an example in a line of prose, from the sentence that opens with E.G., e.g. or Example to
// the end of the line; its first group is what comes before it, up to the sentence's end.
var exampleTail = regexp.MustCompile(`(?i)^(?:(.*?[.!?])\s+)?(?:e\.g\.|examples?\b).*$`)

// keyWord matches a word that can be a statement's key: it starts with a word character, @ or $ (a - before
// it allowed) and ends in one, with alternatives joined by / or | (days/months/years). It is the tokenizer's own
// rule and differs from jomini.IsIdentifier, which allows a dot or a hyphen inside a name but no @, $, :, / or |.
var keyWord = regexp.MustCompile(`^-?[\w@$](?:[\w.:@$/|-]*[\w$])?$`)

// parseDocs parses each prose file of the install into the database it documents, then the wiki's examples, and notes
// every key a tree writes in its database's Fields for the entry detector.
func (schema *Schema) parseDocs(ctx context.Context, input Input) (docs []doc, err error) {
	for _, file := range input.Install {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Format != inventory.Prose {
			continue
		}
		database := schema.resolveDocDatabase(file.Rel)
		if database == "" {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		lines := docLines(text.Content)
		if strings.EqualFold(path.Ext(file.Rel), ".md") {
			for number, line := range lines {
				if strings.HasPrefix(line, "#") {
					lines[number] = ""
				}
			}
		}
		docs = append(docs, doc{database, parseDocScript(lines), game.SourceDocs})
	}
	schema.Sources[game.SourceDocs] = SourceState{Origin: "install", WrittenAt: input.Updated}

	wiki, err := schema.parseWiki(ctx, input)
	if err != nil {
		return nil, err
	}
	docs = append(docs, wiki...)
	for _, document := range docs {
		noteFields(document.tree, schema.Databases[document.database].Fields)
	}
	return docs, nil
}

// resolveDocDatabase returns the database a prose file documents: its folder's, or the one its stem names beside it
// (history/_characters.info documents history/characters); "" for neither.
func (schema *Schema) resolveDocDatabase(rel string) string {
	folder, database := schema.folder(rel)
	stem := strings.TrimPrefix(strings.TrimSuffix(path.Base(rel), path.Ext(rel)), "_")
	if beside := game.DeriveDatabaseName(path.Join(path.Dir(rel), stem)); schema.Databases[beside] != nil {
		folder, database = path.Dir(rel), beside
	}
	if schema.Databases[database] == nil || folder != path.Dir(rel) {
		return ""
	}
	return database
}

// docLines returns a text split into its lines as the line index ends them, none keeping its ending.
func docLines(text string) []string {
	lines := jomini.NewLineIndex(text)
	texts := make([]string, lines.LineCount())
	for number := range texts {
		texts[number] = lines.LineText(text, number)
	}
	return texts
}

// parseWiki parses the wiki's script examples for the databases declared so far; a history database takes none. The
// wiki is never stale, since a sanitized page keeps no version label.
func (schema *Schema) parseWiki(ctx context.Context, input Input) (docs []doc, err error) {
	schema.Sources[game.SourceWiki] = SourceState{Origin: "none"}
	if input.Wiki == nil {
		return nil, nil
	}
	names := slices.Sorted(maps.Keys(schema.Databases))
	examples, updated, err := input.Wiki(names)
	if err != nil {
		return nil, err
	}
	if !updated.IsZero() {
		schema.Sources[game.SourceWiki] = SourceState{Origin: "yours", WrittenAt: updated}
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if schema.Databases[name].Rule.Houses == "history" {
			continue
		}
		for _, text := range examples[name] {
			docs = append(docs, doc{name, parseDocScript(docLines(text)), game.SourceWiki})
		}
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

// parseDocScript reads the lines of a doc, wiki example or dump usage into a tree, line by line: a line that is no
// script is prose, braces build the tree, and a trailing comment belongs to the first statement on its line.
func parseDocScript(lines []string) *DocNode {
	parse := &docParse{root: &DocNode{}, runLine: -2, trailLine: -2}
	parse.blocks = []docBlock{{node: parse.root}}
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
		text, col, inBlock, commented := line, 0, len(parse.blocks) > 1, strings.HasPrefix(trimmed, "#")
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

// tokenizeDocLine splits a line into tokens: a <placeholder> or (note) is one token, a # opens a comment to the line's
// end, a bullet's - or * is dropped. Unlike jomini's lexer it reads prose, so a quote closes at the line's end.
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

// indentOf returns the width of a line's leading blanks, a tab reaching the next multiple of four.
func indentOf(line string) (indent int) {
	for _, char := range line[:len(line)-len(strings.TrimLeft(line, " \t"))] {
		indent++
		if char == '\t' {
			indent += 3 - (indent-1)%4
		}
	}
	return indent
}

// comment keeps a whole-line comment: the continuation of the trailing comment above it in its column, else part of
// the run leading the next statement. A heading and an empty comment keep the run together but say nothing.
func (parse *docParse) comment(line, col int, text string) {
	prose := strings.TrimSpace(strings.TrimLeft(text, "# \t"))
	heading := strings.HasPrefix(strings.TrimLeft(text, " \t"), "#") ||
		strings.ToUpper(prose) == prose && strings.ToLower(prose) != prose ||
		strings.HasPrefix(prose, "==") && strings.HasSuffix(prose, "==")
	if heading {
		prose = ""
	}
	if parse.trailing != nil && line == parse.trailLine+1 && col == parse.trailCol {
		parse.trailing.Prose = append(parse.trailing.Prose, prose)
		parse.trailLine = line
		return
	}
	if line != parse.runLine+1 {
		parse.run = nil
	}
	parse.run, parse.runLine = append(parse.run, prose), line
}

// statements reads one code line's tokens into the tree: each statement is a node of the innermost open block, braces
// open and close blocks, and what follows a value or a closed block is the statement's prose.
func (parse *docParse) statements(line, indent int, tokens []docToken) {
	var first, previous *DocNode
	if parse.offside {
		// a code line back at an open block's indentation closes that block first; the line's own leading } is one
		// of those closes
		closes := 0
		for closes < len(parse.blocks)-1 && parse.blocks[len(parse.blocks)-1-closes].indent >= indent {
			closes++
		}
		if closes > 0 && tokens[0].kind == docClose {
			closes--
		}
		for ; closes > 0; closes-- {
			previous = parse.closeBlock()
		}
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token.kind {
		case docClose:
			if len(parse.blocks) > 1 {
				previous = parse.closeBlock()
			}
		case docOpen:
			if previous != nil && !previous.Block && previous.Key != "" {
				previous.Block = true
			} else {
				previous = parse.add(&DocNode{Block: true})
			}
			parse.blocks = append(parse.blocks, docBlock{previous, indent})
		case docComment:
			if first != nil {
				first.Prose = append(first.Prose, strings.TrimSpace(strings.TrimLeft(token.text, "# \t")))
				parse.trailing, parse.trailLine, parse.trailCol = first, line, token.col
			}
		case docNote:
			if previous != nil {
				previous.Prose = append(previous.Prose, token.text)
			}
		case docOperator:
		default:
			node, count := parse.statement(tokens[i:], previous)
			i += count - 1
			if node != nil && first == nil {
				first = node
			}
			if node != nil {
				previous = node
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
	closed := parse.blocks[len(parse.blocks)-1].node
	parse.blocks = parse.blocks[:len(parse.blocks)-1]
	return closed
}

// statement reads one statement from the front of tokens and returns it with how many tokens it took; nil when they are
// the previous statement's prose or an alternative's separator.
func (parse *docParse) statement(tokens []docToken, previous *DocNode) (*DocNode, int) {
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
		placeholder := previous != nil && previous.Key != "" && !previous.Block && strings.HasPrefix(previous.Value, "<")
		closed := previous != nil && previous.Key != "" && previous.Block && parse.blocks[len(parse.blocks)-1].node != previous
		if first.kind != docPlaceholder && !strings.Contains(first.text, "...") && (placeholder || closed) {
			words, notes, count := wordsOf(tokens)
			previous.Prose = append(append(previous.Prose, strings.Join(words, " ")), notes...)
			return nil, max(count, 1)
		}
		return parse.add(&DocNode{Value: first.text}), 1
	}
	node, count := &DocNode{Key: strings.Trim(first.text, `"`)}, 2
	switch {
	case tokens[1].kind == docOpen:
		count = 1 // the brace is read by the caller
	case len(tokens) > 2 && tokens[2].kind == docPlaceholder:
		node.Value, count = tokens[2].text, 3
	case len(tokens) > 2 && (tokens[2].kind == docWord || tokens[2].kind == docQuoted):
		// a value of several words is one quoted text (ai_value = script value), unless a statement follows on
		// the line (modifier = X days = Y); the notes among them are the statement's prose
		words, notes, wordCount := wordsOf(tokens[2:])
		node.Value, node.Prose, count = strings.Join(words, " "), notes, 2+wordCount
		if len(words) > 1 {
			node.Value = `"` + strings.Trim(node.Value, `"`) + `"`
		}
	}
	return parse.add(node), count
}

// wordsOf takes the words and notes at the front of tokens, up to a brace, a placeholder or the comment; when an
// operator follows them, the last word is the next statement's key and is left (modifier = name days/weeks = int).
func wordsOf(tokens []docToken) (words, notes []string, count int) {
	for count < len(tokens) && (tokens[count].kind == docWord || tokens[count].kind == docQuoted || tokens[count].kind == docNote) &&
		(count == 0 || !strings.Contains(tokens[count].text, "...")) {
		count++
	}
	if count < len(tokens) && tokens[count].kind == docOperator {
		count--
	}
	for i, token := range tokens[:count] {
		switch {
		case token.kind == docNote:
			notes = append(notes, token.text)
		case i == 0 && token.text == "-":
		default:
			words = append(words, token.text)
		}
	}
	return words, notes, count
}

// add appends a node to the innermost open block.
func (parse *docParse) add(node *DocNode) *DocNode {
	holder := parse.blocks[len(parse.blocks)-1].node
	holder.Children = append(holder.Children, node)
	return node
}

// mergeDocs merges the docs' and wiki's claims into the databases they document, each statement's claims at the
// position the walker reads its key at.
func (schema *Schema) mergeDocs(docs []doc) {
	for _, document := range docs {
		context := schema.EntryContext(schema.Databases[document.database], "", nil)
		for _, node := range document.tree.Children {
			schema.mergeNode(node, context, document.source, true)
		}
	}
}

// mergeNode merges one node at a context: a bare value's claims are the holder's, a key vanilla writes nowhere below
// the holder is a template, and a command or link gets none. A doc never makes a position.
func (schema *Schema) mergeNode(node *DocNode, context Context, source game.Source, top bool) {
	if node.Key == "" {
		if context.BlockPosition != nil && context.BlockPosition.Vanilla.Count > 0 {
			context.BlockPosition.declare(schema.docClaims(node, source), source)
		}
		for _, child := range node.Children { // a block with no key ({ { name = x } }) reads in place
			schema.mergeNode(child, context, source, false)
		}
		return
	}
	for _, name := range keyNames(node.Key) {
		inner := schema.Enter(context, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, false)
		isCommand := inner.KeyRule == game.KeyRuleCommand || inner.KeyRule == game.KeyRuleLink
		switch {
		case !isCommand && (context.BlockPosition == nil || !writtenBelow(context.BlockPosition, positionName(name))):
			schema.mergeTemplate(node, context, source, top)
			continue
		case !isCommand && inner.Position() != nil && inner.Position().Vanilla.Count > 0:
			inner.Position().declare(schema.docClaims(node, source), source)
		}
		for _, child := range node.Children {
			schema.mergeNode(child, inner, source, false)
		}
	}
}

// mergeTemplate merges a node whose key vanilla writes nowhere below (an example entry's name, a <key>): at the doc's
// top its claims are the holder's, and its block merges in the holder's place or under each child that has one.
func (schema *Schema) mergeTemplate(node *DocNode, context Context, source game.Source, top bool) {
	if context.BlockPosition != nil && top && (node.Block || context.BlockPosition.Vanilla.Writes != WritesBlock) {
		context.BlockPosition.declare(schema.docClaims(node, source), source)
	}
	holds := func(position *Position) bool {
		return node.Block && slices.ContainsFunc(node.Children, func(child *DocNode) bool {
			return child.Key != "" && position.Child(child.Key) != nil
		})
	}
	if context.BlockPosition == nil || holds(context.BlockPosition) {
		for _, child := range node.Children {
			schema.mergeNode(child, context, source, false)
		}
		return
	}
	for _, below := range context.BlockPosition.Children {
		if holds(below) {
			context.BlockPosition = below
			for _, child := range node.Children {
				schema.mergeNode(child, context, source, false)
			}
		}
	}
}

// writtenBelow reports whether vanilla writes a position of a name anywhere below one.
func writtenBelow(position *Position, name string) bool {
	for key, child := range position.Children {
		if key == name && child.Vanilla.Count > 0 || writtenBelow(child, name) {
			return true
		}
	}
	return false
}

// docClaims returns a doc node's claims by the tables; a block whose kind nothing names states only that the key takes
// a block, and a literal states its kind (expiration_days = 3650).
func (schema *Schema) docClaims(node *DocNode, source game.Source) Claims {
	claims := schema.claimsOf(node, source)
	if node.Block && claims.Shape.Source == game.SourceNone {
		claims.Value.Set(Expected{Type: ValueBlock}, source)
	}
	if node.Value != "" && !strings.HasPrefix(node.Value, "<") && !strings.HasPrefix(node.Value, `"`) {
		literals := map[Writes]ValueType{WritesNumber: ValueNumber, WritesBool: ValueBool, WritesDate: ValueDate}
		if kind, literal := literals[writesOf(&jomini.Scalar{Text: node.Value})]; literal {
			claims.Value.Set(Expected{Type: kind}, source)
		}
	}
	return claims
}

// claimsOf collects one node's claims: its value slot and block placeholders through the phrase table, then each line
// of its prose; what no phrase reads becomes the blurb, cut at the example it opens.
func (schema *Schema) claimsOf(node *DocNode, source game.Source) (claims Claims) {
	var blurb []string
	if word, isPlaceholder := strings.CutPrefix(node.Value, "<"); isPlaceholder {
		word = strings.TrimSuffix(word, ">")
		named := schema.ResolveNamedDatabase(word)
		if schema.claimPhrases(&claims, word, source) == word && named != "" {
			claims.Value.Set(Expected{Type: ValueReference, Database: named}, source)
		}
	} else if node.Value != "" {
		rest := schema.claimPhrases(&claims, node.Value, source)
		if text := cutExample(strings.Trim(node.Value, `"`)); isBlurb(rest) && strings.ContainsAny(text, " \t") {
			blurb = append(blurb, text)
		}
	}
	for _, child := range node.Children {
		if child.Key == "" && strings.HasPrefix(child.Value, "<") { // a block placeholder listed: { <display_triggers> }
			var placeholder Claims
			schema.claimPhrases(&placeholder, strings.TrimSuffix(child.Value[strings.LastIndexAny(child.Value, "<_ ")+1:], ">"), source)
			claims.Shape.Set(placeholder.Shape.Value, placeholder.Shape.Source)
		}
	}
	for _, prose := range node.Prose {
		if rest := schema.claimPhrases(&claims, prose, source); isBlurb(rest) {
			if text := cutExample(prose); text != "" {
				blurb = append(blurb, text)
			}
		}
	}
	slices.Sort(claims.Value.Value.Choices)
	claims.Value.Value.Choices = slices.Compact(claims.Value.Value.Choices)
	if len(blurb) > 0 {
		claims.Blurb.Set(strings.Join(blurb, " "), source)
	}
	return claims
}

// claimPhrases writes every claim the phrase table reads from a text and returns what is left for a blurb: nothing
// when a phrase read it whole, marked it as claiming nothing, or it was an a/b/c list, whose words became choices.
func (schema *Schema) claimPhrases(claims *Claims, text string, source game.Source) (rest string) {
	rest, claimsNothing := text, false
	for _, phrase := range docPhrases {
		match := phrase.pattern.FindStringSubmatch(text)
		if match == nil || phrase.fallback && rest != text {
			continue
		}
		rest = strings.Replace(rest, match[0], "", 1)
		switch {
		case phrase.valueType != ValueUnknown:
			claims.Value.Set(Expected{Type: phrase.valueType}, source)
		case phrase.shape != game.ShapeUnknown:
			claims.Shape.Set(phrase.shape, source)
		case phrase.claim != nil:
			phrase.claim(schema, claims, match, source)
		default:
			claimsNothing = true
		}
	}
	switch {
	case rest == text && alternatives.MatchString(text):
		// a/b/c no phrase reads whole is read word by word: a type word is its type, the rest are choices
		for word := range strings.SplitSeq(text, "/") {
			if schema.claimPhrases(claims, word, source) == word {
				claims.Value.Value.Choices, claims.Value.Source = append(claims.Value.Value.Choices, word), source
			}
		}
		return ""
	case claimsNothing:
		return ""
	}
	return rest
}

// isBlurb reports whether what the table left of a text still has words in it: prose, kept as a blurb.
func isBlurb(rest string) bool { return strings.ContainsFunc(rest, unicode.IsLetter) }

// cutExample returns a line of prose cut from the sentence that opens with E.G., e.g. or Example to its end: an example
// shows how to write a key, so no blurb holds it.
func cutExample(line string) string {
	return strings.TrimSpace(exampleTail.ReplaceAllString(line, "$1"))
}
