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

// EventHits is sent with one file's hits as a text search finds them; the data is a Hits.
const EventHits = "search:hits"

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

// A Hit is one match: the file, the match's range in Monaco's form, and the text of the line it is on. The range's
// columns also mark the match inside that text.
type Hit struct {
	Place workspace.Place `json:"place"`
	Range lsp.Range       `json:"range"`
	Text  string          `json:"text"`
}

// Hits is the data of the search:hits event: one file's hits, sent as soon as that file is done so the view fills
// as the search runs.
type Hits struct {
	Place workspace.Place `json:"place"`
	Hits  []Hit           `json:"hits"`
}

// SearchText runs a text search over the files the query names, reading an open file's text from the editor. It
// sends each file's hits as a search:hits event as that file is done, and returns when the search ends or a newer
// search stops it. Starting a new search stops the last.
func (sch *Service) SearchText(query Query) error {
	sch.setStop(nil)
	ws, err := sch.Workspace.Session()
	if err != nil {
		return err
	}
	if _, err := parseQuery(query); err != nil {
		return err
	}
	stop := make(chan struct{})
	sch.setStop(stop)
	run(ws, ws.Mods.Load(), query, stop)
	return nil
}

// StopSearch ends the running search, if any.
func (sch *Service) StopSearch() { sch.setStop(nil) }

// ReplaceText replaces every match of a query in the mods' files and returns how many it replaced. Each file is
// written back in its own encoding, and the watcher sends fs:changed for it. A file open in the editor is skipped,
// since the editor replaces in it; the error says how many were skipped.
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

// SearchFiles returns the files whose path contains the query's characters in order, the mods' files first, up
// to the given limit.
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
			places = append(places, ws.Locate(file.Path))
		}
	}
	return places
}

// run runs one text search over the files of the given index of the mods until it finishes or the stop channel
// closes, sending each file's hits as it goes. An open file is searched in the editor's text.
func run(ws *workspace.Session, mods *index.Index, query Query, stop <-chan struct{}) {
	expression, err := parseQuery(query)
	if err != nil {
		return
	}
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for _, file := range listFiles(mods, query) {
		if isClosed(stop) {
			break
		}
		group.Go(func() error {
			if hits := findHits(ws, file, expression, query.WholeWord); len(hits) > 0 && !isClosed(stop) {
				application.Get().Event.Emit(EventHits, Hits{Place: hits[0].Place, Hits: hits})
			}
			return nil
		})
	}
	_ = group.Wait()
}

// setStop closes the stop channel of the running search. When none runs, there is nothing to close. It keeps the
// given channel in its place, nil when none runs next.
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

// listFiles returns the files a query names. These are the files of its origins, or of the mods when it names
// none. Only text files are kept, and then those the include and exclude globs let through. A file the index no
// longer knows is left out.
func listFiles(mods *index.Index, query Query) []files.File {
	include, exclude := parseGlobs(query.Include), parseGlobs(query.Exclude)
	return lo.Filter(mods.Files, func(file files.File, position int) bool {
		searched := file.Origin != game.OriginVanilla
		if len(query.Origins) > 0 {
			searched = slices.Contains(query.Origins, file.Origin)
		}
		if !mods.IsListed(int32(position)) || !searched {
			return false
		}
		if file.Format == files.FormatImage || file.Format == files.FormatBinary {
			return false
		}
		return (include == nil || include.Match(file.Rel, false)) && !exclude.Match(file.Rel, false)
	})
}

// parseGlobs returns the rules of a list of glob patterns. The patterns are separated by commas outside braces and
// by line breaks, so "*.{txt,yml}" is one pattern.
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

// findMatches returns the matches of an expression in a text, line by line. A match never spans two lines and is
// never empty. With the whole-word flag, a match counts only if it stands apart from the letters around it. Each
// match is a list of byte offsets in the text, as the expression's own submatch indexes are.
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
// underscore. The start and the end of the line count as neither.
func isWholeWord(content string, start, end int) bool {
	inWord := func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_'
	}
	before, _ := utf8.DecodeLastRuneInString(content[:start])
	after, _ := utf8.DecodeRuneInString(content[end:])
	return !inWord(before) && !inWord(after)
}

// replaceFile replaces every match of an expression in a file on disk and returns how many it replaced. The file
// is written back in its own encoding, and not at all if nothing matched. It returns an error if the file cannot be
// read or written.
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

// findHits returns the hits of an expression in one file, in order. It searches the editor's text if the file is
// open there and the file on disk if not. A match never spans two lines, and an empty match is not a hit. It
// returns nothing for a file that cannot be read.
func findHits(ws *workspace.Session, file files.File, expression *regexp.Regexp, wholeWord bool) []Hit {
	source, open := ws.LookupBuffer(file.Path)
	if !open {
		text, err := files.ReadText(file.Path)
		if err != nil {
			return nil
		}
		source = text.Text
	}
	lines := jomini.NewLines(source)
	matches := findMatches(source, lines, expression, wholeWord)
	if len(matches) == 0 {
		return nil
	}
	place := ws.Locate(file.Path)
	return lo.Map(matches, func(match []int, _ int) Hit {
		line := lines.LineSpan(lines.ToPosition(match[0]).Line)
		return Hit{
			Place: place,
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
