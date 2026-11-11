// Package search is the IDE's search service: text search over the open workspace's files, replace, and the file
// search behind Ctrl P.
package search

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventHits is sent with one file's hits as a text search finds them. It carries a Hits.
const EventHits = "search:hits"

// maxHits is the most hits the IDE's text search sends. A search that reaches it stops there.
const maxHits = 10000

// init registers the search event with the application.
func init() { application.RegisterEvent[Hits](EventHits) }

// Service is the search service. It runs one search at a time; starting a new one stops the last.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex    // held while stop is read or replaced
	stop      chan struct{} // closed to end the running search; nil while none runs
}

// ServiceShutdown stops a running search.
func (sch *Service) ServiceShutdown() error {
	sch.setStop(nil)
	return nil
}

// A Query is one text search: what to find and where.
type Query struct {
	Text      string   `json:"text" jsonschema:"the text to find, or a regular expression when regex is true"`
	Regex     bool     `json:"regex" jsonschema:"whether the text is a regular expression"`
	MatchCase bool     `json:"matchCase" jsonschema:"whether a letter's case must match"`
	WholeWord bool     `json:"wholeWord" jsonschema:"whether a match must be a whole word"`
	Include   string   `json:"include" jsonschema:"glob patterns of the files searched, comma-separated; all if empty"`
	Exclude   string   `json:"exclude" jsonschema:"glob patterns of the files left out, comma-separated"`
	Origins   []string `json:"origins" jsonschema:"the mods searched, by id, and vanilla for the game's files; the mods if empty"`
}

// Hits holds the matches in one file, each with the range of the match and
// the text of its line. A match carries no place of its own: Place is the
// file's. It is what the search:hits event carries.
type Hits struct {
	Place workspace.Place `json:"place"`
	Hits  []lsp.Site      `json:"hits"`
}

// A tally counts the hits one text search has given and passes each file's hits on, up to a limit.
type tally struct {
	mu      sync.Mutex
	count   int
	limit   int        // the most hits the search gives
	receive func(Hits) // takes one file's hits, one file at a time
}

// SearchText runs a text search and sends each file's hits as a search:hits event, at most maxHits in all. It returns
// whether it stopped at maxHits, and an error if the query is empty or is a regular expression that does not parse.
func (sch *Service) SearchText(query Query) (bool, error) {
	sch.setStop(nil)
	ws, err := sch.Workspace.Session()
	if err != nil {
		return false, err
	}
	stop := make(chan struct{})
	sch.setStop(stop)
	return run(ws, query, stop, &tally{limit: maxHits, receive: func(hits Hits) {
		application.Get().Event.Emit(EventHits, hits)
	}})
}

// CollectText runs a text search over a workspace's files and returns each file's hits, at most the given count in
// all. It stops when the given channel closes. It returns whether it stopped at the count, and an error if the query
// is empty or is a regular expression that does not parse.
func CollectText(ws *workspace.Session, query Query, limit int, stop <-chan struct{}) ([]Hits, bool, error) {
	collected := []Hits{}
	full, err := run(ws, query, stop, &tally{limit: limit, receive: func(hits Hits) {
		collected = append(collected, hits)
	}})
	return collected, full, err
}

// StopSearch ends the running search, if any.
func (sch *Service) StopSearch() { sch.setStop(nil) }

// SearchFile runs a text search over one file, its unsaved text if the editor has some, and returns its hits. A file
// the query does not search, or one gone from the workspace, has none. It returns an error if no workspace is open, or
// if the query is empty or is a regular expression that does not parse.
func (sch *Service) SearchFile(query Query, path string) (Hits, error) {
	ws, err := sch.Workspace.Session()
	if err != nil {
		return Hits{}, err
	}
	expression, err := parseQuery(query)
	if err != nil {
		return Hits{}, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	position, ok := mods.FindFile(path)
	if !ok || !mods.IsListed(position) || !makeFilter(query)(mods.Files[position]) {
		return Hits{Place: place}, nil
	}
	source, err := ws.LoadSource(path)
	if err != nil {
		return Hits{Place: place}, nil
	}
	return Hits{Place: place, Hits: findSites(source, expression, query.WholeWord)}, nil
}

// ReplaceText replaces every match of a query in the mods' files and returns how many it replaced. A file open in the
// editor is skipped, and the error says how many were.
func (sch *Service) ReplaceText(query Query, replacement string) (int, error) {
	ws, err := sch.Workspace.Session()
	if err != nil {
		return 0, err
	}
	expression, err := parseQuery(query)
	if err != nil {
		return 0, err
	}
	if !query.Regex {
		replacement = strings.ReplaceAll(replacement, "$", "$$")
	}
	count, openFiles := 0, 0
	var failure error
	for _, file := range listFiles(ws.Mods.Load(), query) {
		if file.Origin == files.OriginVanilla {
			continue
		}
		if text, open := ws.LookupBuffer(file.Path); open {
			if len(findMatches(text, jomini.NewLines(text), expression, query.WholeWord)) > 0 {
				openFiles++
			}
			continue
		}
		fileCount, err := sch.replaceFile(file.Path, expression, query.WholeWord, replacement)
		count += fileCount
		if err != nil {
			failure = err
			break
		}
	}
	if openFiles > 0 {
		failure = errors.Join(failure, fmt.Errorf("files open in the editor were skipped: %d", openFiles))
	}
	return count, failure
}

// SearchFiles returns the files whose path contains the query's characters in order, up to the given limit. The best
// match comes first: a file named as the query, then one whose name starts with it, then one whose path holds it, then
// the rest. Within each of these the mods' files come before the game's.
func (sch *Service) SearchFiles(query string, limit int) []workspace.Place {
	ws, err := sch.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	query = strings.ToLower(query)
	type match struct {
		file files.File
		rank int
	}
	var matches []match
	for _, position := range slices.Concat(mods.ListOwnFiles(), ws.Install.ListOwnFiles()) {
		file := mods.Files[position]
		if rank := files.ClassifyMatch(strings.ToLower(file.Rel), query); rank >= 0 {
			matches = append(matches, match{file, rank})
		}
	}
	slices.SortStableFunc(matches, func(first, second match) int { return cmp.Compare(first.rank, second.rank) })
	return lo.Map(matches[:max(min(limit, len(matches)), 0)], func(one match, _ int) workspace.Place {
		return ws.Locate(mods, one.file.Path)
	})
}

// run runs one text search, giving each file's hits to the tally. It stops at the tally's limit or when the given
// channel closes. It returns whether the limit was reached, and an error if the query is empty or is a regular
// expression that does not parse.
func run(ws *workspace.Session, query Query, stop <-chan struct{}, tally *tally) (bool, error) {
	expression, err := parseQuery(query)
	if err != nil {
		return false, err
	}
	var group errgroup.Group
	group.SetLimit(files.Workers)
	mods := ws.Mods.Load()
	quickTest := makeTest(query)
	for _, file := range listFiles(mods, query) {
		if isClosed(stop) || tally.isFull() {
			break
		}
		group.Go(func() error {
			source, err := ws.LoadSource(file.Path)
			if err != nil || !quickTest(source) {
				return nil
			}
			if sites := findSites(source, expression, query.WholeWord); len(sites) > 0 && !isClosed(stop) {
				tally.send(Hits{Place: ws.Locate(mods, file.Path), Hits: sites})
			}
			return nil
		})
	}
	_ = group.Wait()
	return tally.isFull(), nil
}

// findSites returns the matches of an expression in a file's text, each with its line and that line's text.
func findSites(source string, expression *regexp.Regexp, wholeWord bool) []lsp.Site {
	lines := jomini.NewLines(source)
	return lo.Map(findMatches(source, lines, expression, wholeWord), func(match []int, _ int) lsp.Site {
		position := lines.ToPosition(match[0])
		line := lines.LineSpan(position.Line)
		return lsp.Site{
			Line:  position.Line + 1,
			Range: lsp.ToRange(source, lines, jomini.Span{Start: match[0], End: match[1]}),
			Text:  source[line.Start:line.End],
		}
	})
}

// makeTest returns the quick test a file's text must pass before it is searched line by line. A regular expression
// passes every file.
func makeTest(query Query) func(text string) bool {
	lower := strings.ToLower(query.Text)
	switch {
	case query.Regex:
		return func(string) bool { return true }
	case query.MatchCase:
		return func(text string) bool { return strings.Contains(text, query.Text) }
	case utf8.RuneCountInString(lower) != len(lower): // containsFold folds ASCII only
		return func(text string) bool { return strings.Contains(strings.ToLower(text), lower) }
	}
	return func(text string) bool { return containsFold(text, lower) }
}

// containsFold reports whether a text holds a piece of lower-case ASCII in any case.
func containsFold(text, piece string) bool {
	first := piece[:1] + strings.ToUpper(piece[:1])
	for len(text) >= len(piece) {
		offset := strings.IndexAny(text, first)
		if offset < 0 || len(text)-offset < len(piece) {
			return false
		}
		if strings.EqualFold(text[offset:offset+len(piece)], piece) {
			return true
		}
		text = text[offset+1:]
	}
	return false
}

// send gives one file's hits to the tally's receiver, as many as the limit leaves room for.
func (tally *tally) send(hits Hits) {
	tally.mu.Lock()
	defer tally.mu.Unlock()
	hits.Hits = hits.Hits[:min(len(hits.Hits), tally.limit-tally.count)]
	if len(hits.Hits) == 0 {
		return
	}
	tally.count += len(hits.Hits)
	tally.receive(hits)
}

// isFull reports whether the search has given as many hits as its limit.
func (tally *tally) isFull() bool {
	tally.mu.Lock()
	defer tally.mu.Unlock()
	return tally.count >= tally.limit
}

// setStop ends the running search, if any, and keeps the given channel as the next one's. A nil channel means none.
func (sch *Service) setStop(stop chan struct{}) {
	sch.mu.Lock()
	defer sch.mu.Unlock()
	if sch.stop != nil {
		close(sch.stop)
	}
	sch.stop = stop
}

// parseQuery returns the expression a query searches for. It returns an error if the query's text is empty or is a
// regular expression that does not parse.
func parseQuery(query Query) (*regexp.Regexp, error) {
	if query.Text == "" {
		return nil, errors.New("the search text is empty")
	}
	expression := query.Text
	if !query.Regex {
		expression = regexp.QuoteMeta(expression)
	}
	if !query.MatchCase {
		expression = "(?i)" + expression
	}
	return regexp.Compile(expression)
}

// listFiles returns the text files a query names: those of its origins that its globs let through. The mods' files
// come first, in the mods' load order, then the game's.
func listFiles(mods *index.Index, query Query) []files.File {
	isSearched := makeFilter(query)
	matches := lo.Filter(mods.Files, func(file files.File, position int) bool {
		return mods.IsListed(int32(position)) && isSearched(file)
	})
	own, vanilla := lo.FilterReject(matches, func(file files.File, _ int) bool { return file.Origin != files.OriginVanilla })
	return slices.Concat(own, vanilla)
}

// makeFilter returns the test of whether a query searches a file: a text file of one of its origins, the mods' if it
// names none, that its globs let through.
func makeFilter(query Query) func(file files.File) bool {
	include, exclude := parseGlobs(query.Include), parseGlobs(query.Exclude)
	return func(file files.File) bool {
		searched := file.Origin != files.OriginVanilla
		if len(query.Origins) > 0 {
			searched = slices.Contains(query.Origins, file.Origin)
		}
		if !searched || !file.Format.IsText() {
			return false
		}
		return (include == nil || include.Match(file.Rel, false)) && !exclude.Match(file.Rel, false)
	}
}

// parseGlobs parses glob patterns separated by commas outside braces, or by line breaks, into ignore rules.
func parseGlobs(text string) files.Ignore {
	var rules strings.Builder
	depth := 0
	for _, character := range text {
		switch {
		case character == '{':
			depth++
		case character == '}':
			depth = max(depth-1, 0)
		case character == ',' && depth == 0:
			character = '\n'
		}
		rules.WriteRune(character)
	}
	return files.ParseIgnore(rules.String())
}

// findMatches returns the matches of an expression in a text as byte offsets. A match never spans two lines and is
// never empty. With the whole-word flag, a match must stand apart from the letters around it.
func findMatches(text string, lines jomini.Lines, expression *regexp.Regexp, wholeWord bool) [][]int {
	var matches [][]int
	for line := range lines.CountLines() {
		span := lines.LineSpan(line)
		content := text[span.Start:span.End]
		for _, match := range expression.FindAllStringSubmatchIndex(content, -1) {
			if match[0] == match[1] || (wholeWord && !isWholeWord(content, match[0], match[1])) {
				continue
			}
			// Shift the offsets from the line to the text, leaving the -1 of a group that did not match.
			for position, offset := range match {
				if offset >= 0 {
					match[position] = offset + span.Start
				}
			}
			matches = append(matches, match)
		}
	}
	return matches
}

// isWholeWord reports whether the characters on each side of a match are neither letters, digits nor an underscore.
func isWholeWord(content string, start, end int) bool {
	inWord := func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_'
	}
	before, _ := utf8.DecodeLastRuneInString(content[:start])
	after, _ := utf8.DecodeRuneInString(content[end:])
	return !inWord(before) && !inWord(after)
}

// replaceFile replaces every match of an expression in a file and returns how many it replaced. The file keeps its
// encoding and is not written if nothing matched. It returns an error if the file cannot be read or written.
func (sch *Service) replaceFile(
	path string, expression *regexp.Regexp, wholeWord bool, replacement string,
) (int, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return 0, err
	}
	matches := findMatches(text.Text, jomini.NewLines(text.Text), expression, wholeWord)
	if len(matches) == 0 {
		return 0, nil
	}
	var replaced strings.Builder
	last := 0
	for _, match := range matches {
		replaced.WriteString(text.Text[last:match[0]])
		replaced.Write(expression.ExpandString(nil, replacement, text.Text, match))
		last = match[1]
	}
	replaced.WriteString(text.Text[last:])
	text.Text = replaced.String()
	return len(matches), sch.Workspace.Write(path, text)
}

// isClosed reports whether a stop channel is closed.
func isClosed(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}
