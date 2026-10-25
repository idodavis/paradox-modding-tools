// Package search is the IDE's search service: text search over the open workspace's files, replace, and the file
// search behind Ctrl P.
package search

import (
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
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// EventHits is sent with one file's hits as a text search finds them. It carries a Hits.
const EventHits = "search:hits"

// maxHits is the most hits a text search sends. A search that reaches it stops there.
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
	Text      string   `json:"text"`
	Regex     bool     `json:"regex"`
	MatchCase bool     `json:"matchCase"`
	WholeWord bool     `json:"wholeWord"`
	Include   string   `json:"include"` // glob patterns, comma-separated
	Exclude   string   `json:"exclude"`
	Origins   []string `json:"origins"` // the mods and the game searched; empty for the mods
}

// A Hit is one match of a text search. Its range's columns also mark the match inside the text of its line.
type Hit struct {
	Place workspace.Place `json:"place"`
	Range lsp.Range       `json:"range"`
	Text  string          `json:"text"`
}

// Hits holds the matches in one file. It is what the search:hits event carries.
type Hits struct {
	Place workspace.Place `json:"place"`
	Hits  []Hit           `json:"hits"`
}

// A tally is the count of the hits one text search has sent, which stops at maxHits.
type tally struct {
	mu    sync.Mutex
	count int
}

// SearchText runs a text search over the files the query names. It sends each file's hits as a search:hits event, at
// most maxHits in all, and returns when the search ends or a newer search stops it. It returns whether the search
// stopped at maxHits. It returns an error if the query is empty or is a regular expression that does not parse.
func (sch *Service) SearchText(query Query) (bool, error) {
	sch.setStop(nil)
	ws, err := sch.Workspace.Session()
	if err != nil {
		return false, err
	}
	expression, err := parseQuery(query)
	if err != nil {
		return false, err
	}
	stop := make(chan struct{})
	sch.setStop(stop)
	return run(ws, query, expression, stop), nil
}

// StopSearch ends the running search, if any.
func (sch *Service) StopSearch() { sch.setStop(nil) }

// ReplaceText replaces every match of a query in the mods' files and returns how many it replaced. Each file keeps its
// encoding. A file open in the editor is skipped, and the error says how many were.
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
		if file.Origin == game.OriginVanilla {
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

// SearchFiles returns the files whose path contains the query's characters in order, the mods' files first, up to the
// given limit.
func (sch *Service) SearchFiles(query string, limit int) []workspace.Place {
	ws, err := sch.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	characters := []rune(strings.ToLower(query))
	var places []workspace.Place
	for _, position := range slices.Concat(mods.ListOwnFiles(), ws.Install.ListOwnFiles()) {
		if len(places) >= limit {
			return places
		}
		if file := mods.Files[position]; isSubsequence(strings.ToLower(file.Rel), characters) {
			places = append(places, ws.Locate(mods, file.Path))
		}
	}
	return places
}

// run runs one text search with the given expression over the files the query names, sending each file's hits. It
// returns when the search finishes, has sent maxHits or the given channel closes, and reports whether it sent maxHits.
func run(ws *workspace.Session, query Query, expression *regexp.Regexp, stop <-chan struct{}) bool {
	var group errgroup.Group
	group.SetLimit(files.Workers)
	mods := ws.Mods.Load()
	holds := makeTest(query)
	sent := &tally{}
	for _, file := range listFiles(mods, query) {
		if isClosed(stop) || sent.isFull() {
			break
		}
		group.Go(func() error {
			source, ok := readSource(ws, file.Path)
			if !ok || !holds(source) {
				return nil
			}
			if hits := findHits(source, expression, query.WholeWord); len(hits) > 0 && !isClosed(stop) {
				sent.send(ws.Locate(mods, file.Path), hits)
			}
			return nil
		})
	}
	_ = group.Wait()
	return sent.isFull()
}

// makeTest returns the quick test a file's whole text must pass before it is searched line by line: it holds a plain
// query's text, in any case unless the query matches case. A regular expression is matched in every file.
func makeTest(query Query) func(text string) bool {
	lower := strings.ToLower(query.Text)
	switch {
	case query.Regex:
		return func(string) bool { return true }
	case query.MatchCase:
		return func(text string) bool { return strings.Contains(text, query.Text) }
	case utf8.RuneCountInString(lower) != len(lower):
		return func(text string) bool { return strings.Contains(strings.ToLower(text), lower) }
	}
	return func(text string) bool { return containsFold(text, lower) }
}

// containsFold reports whether a text holds a piece of lower-case ASCII in any case, without copying the text.
func containsFold(text, piece string) bool {
	first := piece[:1] + strings.ToUpper(piece[:1])
	for len(text) >= len(piece) {
		index := strings.IndexAny(text, first)
		if index < 0 || len(text)-index < len(piece) {
			return false
		}
		if strings.EqualFold(text[index:index+len(piece)], piece) {
			return true
		}
		text = text[index+1:]
	}
	return false
}

// readSource returns a file's text: the editor's where the file is open, else the one on disk. It returns false if the
// file cannot be read.
func readSource(ws *workspace.Session, path string) (string, bool) {
	if source, open := ws.LookupBuffer(path); open {
		return source, true
	}
	text, err := files.ReadText(path)
	return text.Text, err == nil
}

// send sends one file's hits at the given place as a search:hits event, as many as maxHits leaves room for.
func (tally *tally) send(place workspace.Place, hits []Hit) {
	tally.mu.Lock()
	defer tally.mu.Unlock()
	hits = hits[:min(len(hits), maxHits-tally.count)]
	if len(hits) == 0 {
		return
	}
	tally.count += len(hits)
	for index := range hits {
		hits[index].Place = place
	}
	application.Get().Event.Emit(EventHits, Hits{Place: place, Hits: hits})
}

// isFull reports whether the search has sent maxHits.
func (tally *tally) isFull() bool {
	tally.mu.Lock()
	defer tally.mu.Unlock()
	return tally.count >= maxHits
}

// setStop ends the running search, if any, and keeps the given channel as the one that ends the next. A nil channel
// means no search runs.
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

// listFiles returns the text files a query names: those of its origins, or of the mods if it names none, that its
// include and exclude globs let through.
func listFiles(mods *index.Index, query Query) []files.File {
	include, exclude := parseGlobs(query.Include), parseGlobs(query.Exclude)
	return lo.Filter(mods.Files, func(file files.File, position int) bool {
		searched := file.Origin != game.OriginVanilla
		if len(query.Origins) > 0 {
			searched = slices.Contains(query.Origins, file.Origin)
		}
		if !mods.IsListed(int32(position)) || !searched || !file.Format.IsText() {
			return false
		}
		return (include == nil || include.Match(file.Rel, false)) && !exclude.Match(file.Rel, false)
	})
}

// parseGlobs returns the rules of a list of glob patterns, separated by commas outside braces and by line breaks. So
// "*.{txt,yml}" is one pattern.
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

// findMatches returns the matches of an expression in a text, line by line, each as byte offsets. A match never spans
// two lines and is never empty. With the whole-word flag, a match must stand apart from the letters around it.
func findMatches(text string, lines jomini.Lines, expression *regexp.Regexp, wholeWord bool) [][]int {
	var matches [][]int
	for line := range lines.CountLines() {
		span := lines.LineSpan(line)
		content := text[span.Start:span.End]
		for _, match := range expression.FindAllStringSubmatchIndex(content, -1) {
			if match[0] == match[1] || wholeWord && !isWholeWord(content, match[0], match[1]) {
				continue
			}
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

// isWholeWord reports whether the characters on each side of a match in a line are neither letters, digits nor an
// underscore. The start and end of the line count as neither.
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

// isSubsequence reports whether a text holds the given characters in order, not necessarily next to each other.
func isSubsequence(text string, characters []rune) bool {
	for _, character := range text {
		if len(characters) == 0 {
			break
		}
		if character == characters[0] {
			characters = characters[1:]
		}
	}
	return len(characters) == 0
}

// findHits returns the hits of an expression in a file's text, in order, without their place.
func findHits(source string, expression *regexp.Regexp, wholeWord bool) []Hit {
	lines := jomini.NewLines(source)
	return lo.Map(findMatches(source, lines, expression, wholeWord), func(match []int, _ int) Hit {
		line := lines.LineSpan(lines.ToPosition(match[0]).Line)
		return Hit{
			Range: lsp.ToRange(source, lines, jomini.Span{Start: match[0], End: match[1]}),
			Text:  source[line.Start:line.End],
		}
	})
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
