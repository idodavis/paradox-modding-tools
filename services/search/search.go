// Package search is the IDE's search service. It runs a text search over the
// open workspace's files, replaces matches in the mods' files, and finds files
// by name for Ctrl P.
//
// # Text search
//
// A search reads the text files of the origins it names, the mods' if it names
// none, narrowed by its include and exclude globs. It ignores case unless the
// query matches case, and takes the text literally unless the query is a
// regular expression. A match never spans two lines and is never empty. A
// whole-word match has no letter, digit or underscore beside it. A file open
// in the editor is searched as the editor has it, unsaved.
//
// # Replace
//
// Replace changes the mods' files only, never the game's. A file open in the
// editor is skipped, since its unsaved text would be lost, and the error says
// how many were.
//
// # File search
//
// A file matches when its path holds the query's characters in order. A file
// named as the query comes first, then a name that starts with it, then a path
// that holds it. The mods' files come before the game's.
//
// # Robustness
//
// A search refuses an empty text and a regular expression that does not
// parse. It reads past a file it cannot load, which gives no hits. Replace
// stops at the first file it cannot read or write.
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

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventHits is sent with one file's hits as a text search finds them.
const EventHits = "search:hits"

func init() { application.RegisterEvent[Hits](EventHits) }

// Service is the search service. Starting a new text search stops the last.
type Service struct {
	Workspace *workspace.Service

	// mu guards stop, which is closed to end the running search and is nil
	// while none runs.
	mu   sync.Mutex
	stop chan struct{}
}

// Query is one text search: what to find and where.
type Query struct {
	Text      string   `json:"text" jsonschema:"the text to find, or a regular expression when regex is true"`
	Regex     bool     `json:"regex" jsonschema:"whether the text is a regular expression"`
	MatchCase bool     `json:"matchCase" jsonschema:"whether a letter's case must match"`
	WholeWord bool     `json:"wholeWord" jsonschema:"whether a match must be a whole word"`
	Include   string   `json:"include" jsonschema:"glob patterns of the files searched, comma-separated; all if empty"`
	Exclude   string   `json:"exclude" jsonschema:"glob patterns of the files left out, comma-separated"`
	Origins   []string `json:"origins" jsonschema:"the mods searched, by id, and vanilla for the game's files; the mods if empty"`
}

// Hits is the matches in one file. Place is the file's, not each match's.
type Hits struct {
	Place workspace.Place `json:"place"`
	Hits  []lsp.Site      `json:"hits"`
}

// tally counts the hits of one text search and passes each file's hits on.
type tally struct {
	mu    sync.Mutex
	count int
	limit int

	// receive is called with one file's hits at a time, under the lock.
	receive func(Hits)
}

// ServiceShutdown stops a running search.
func (s *Service) ServiceShutdown() error {
	s.setStop(nil)

	return nil
}

const maxHits = 10000

// SearchText runs a text search and sends each file's hits as a search:hits
// event. It returns true if it stopped at maxHits, and an error for an empty
// query or a bad regular expression.
func (s *Service) SearchText(query Query) (bool, error) {
	stop := make(chan struct{})
	s.setStop(stop)

	ws, err := s.Workspace.Session()
	if err != nil {
		return false, err
	}

	return run(ws, query, stop, &tally{limit: maxHits, receive: func(hits Hits) {
		application.Get().Event.Emit(EventHits, hits)
	}})
}

// CollectText runs a text search and returns each file's hits, up to a total
// count, and whether it stopped at the count. Closing the channel stops it.
func CollectText(ws *workspace.Session, query Query, limit int, stop <-chan struct{}) ([]Hits, bool, error) {
	collected := []Hits{}
	full, err := run(ws, query, stop, &tally{limit: limit, receive: func(hits Hits) {
		collected = append(collected, hits)
	}})

	return collected, full, err
}

// StopSearch ends the running search, if any.
func (s *Service) StopSearch() { s.setStop(nil) }

// SearchFile returns one file's hits for a query. A file the query does not
// search has none.
func (s *Service) SearchFile(query Query, path string) (Hits, error) {
	ws, err := s.Workspace.Session()
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

// ReplaceText replaces every match of a query in the mods' files and returns
// how many it replaced. A file open in the editor is skipped, and the error
// says how many were.
func (s *Service) ReplaceText(query Query, replacement string) (int, error) {
	ws, err := s.Workspace.Session()
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

		fileCount, err := s.replaceFile(file.Path, expression, query.WholeWord, replacement)
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

// SearchFiles returns the files whose path holds the query's characters in
// order, best match first. It returns nil if no workspace is open.
func (s *Service) SearchFiles(query string, limit int) []workspace.Place {
	ws, err := s.Workspace.Session()
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

	places := make([]workspace.Place, max(min(limit, len(matches)), 0))
	for position := range places {
		places[position] = ws.Locate(mods, matches[position].file.Path)
	}

	return places
}

// setStop ends the running search and keeps a new stop channel.
func (s *Service) setStop(stop chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		close(s.stop)
	}

	s.stop = stop
}

// run runs one text search and gives each file's hits to the tally. It returns
// whether the tally's limit was reached, and an error for a bad query.
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

// parseQuery returns the expression a query searches for, or an error if its
// text is empty or does not parse.
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

// makeTest returns a quick test a file's text must pass before a line search.
func makeTest(query Query) func(text string) bool {
	lower := strings.ToLower(query.Text)

	switch {
	case query.Regex:
		return func(string) bool { return true }
	case query.MatchCase:
		return func(text string) bool { return strings.Contains(text, query.Text) }
	// containsFold folds ASCII only.
	case utf8.RuneCountInString(lower) != len(lower):
		return func(text string) bool { return strings.Contains(strings.ToLower(text), lower) }
	}

	return func(text string) bool { return containsFold(text, lower) }
}

// containsFold reports whether a text holds a piece of lower-case ASCII in any
// case.
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

// listFiles returns the text files a query searches, the mods' first and the
// game's after.
func listFiles(mods *index.Index, query Query) []files.File {
	isSearched := makeFilter(query)

	var own, vanilla []files.File
	for position, file := range mods.Files {
		if !mods.IsListed(int32(position)) || !isSearched(file) {
			continue
		}

		if file.Origin == files.OriginVanilla {
			vanilla = append(vanilla, file)
		} else {
			own = append(own, file)
		}
	}

	return slices.Concat(own, vanilla)
}

// makeFilter returns the test of whether a query searches a file.
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

// parseGlobs turns glob patterns, split at commas outside braces and at line
// breaks, into ignore rules.
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

// findSites returns the matches in a file's text with their lines.
func findSites(source string, expression *regexp.Regexp, wholeWord bool) []lsp.Site {
	lines := jomini.NewLines(source)
	matches := findMatches(source, lines, expression, wholeWord)
	sites := make([]lsp.Site, 0, len(matches))

	for _, match := range matches {
		sites = append(sites, lsp.LocateSpan(workspace.Place{}, source, lines, jomini.Span{Start: match[0], End: match[1]}))
	}

	return sites
}

// findMatches returns the matches of an expression in a text as byte offsets.
// A match never spans two lines and is never empty.
func findMatches(text string, lines jomini.Lines, expression *regexp.Regexp, wholeWord bool) [][]int {
	var matches [][]int

	for line := range lines.CountLines() {
		span := lines.LineSpan(line)
		content := text[span.Start:span.End]

		for _, match := range expression.FindAllStringSubmatchIndex(content, -1) {
			if match[0] == match[1] || (wholeWord && !isWholeWord(content, match[0], match[1])) {
				continue
			}

			// A group that did not match keeps its -1.
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

// isWholeWord reports whether no letter, digit or underscore is beside a
// match.
func isWholeWord(content string, start, end int) bool {
	inWord := func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_'
	}

	before, _ := utf8.DecodeLastRuneInString(content[:start])
	after, _ := utf8.DecodeRuneInString(content[end:])

	return !inWord(before) && !inWord(after)
}

// send gives one file's hits to the receiver, up to the limit.
func (t *tally) send(hits Hits) {
	t.mu.Lock()
	defer t.mu.Unlock()

	hits.Hits = hits.Hits[:min(len(hits.Hits), t.limit-t.count)]
	if len(hits.Hits) == 0 {
		return
	}

	t.count += len(hits.Hits)
	t.receive(hits)
}

func (t *tally) isFull() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.count >= t.limit
}

func isClosed(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// replaceFile replaces every match in a file and returns how many. A file with
// no match is not written.
func (s *Service) replaceFile(
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

	return len(matches), s.Workspace.Write(path, text)
}
