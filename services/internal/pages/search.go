// search.go is Find in Files and Replace all over the workspace's folders, with their include and exclude globs,
// and the file search: each file greped in a worker per core, its hits sent as it is done.

package pages

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// TextSearchQuery is a Find-in-Files request from the workbench.
type TextSearchQuery struct {
	ID              string             `json:"id"` // the search its hits are sent under
	Pattern         string             `json:"pattern"`
	IsRegexp        bool               `json:"isRegexp"`
	IsCaseSensitive bool               `json:"isCaseSensitive"`
	IsWordMatch     bool               `json:"isWordMatch"`
	MaxResults      int                `json:"maxResults"`
	Folders         []TextSearchFolder `json:"folders"`
}

// TextSearchFolder is one workspace folder plus optional glob filters.
type TextSearchFolder struct {
	Path     string   `json:"path"`
	Origin   string   `json:"origin,omitempty"` // told back on its hits
	Includes []string `json:"includes,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

// TextSearchHit is one match line, or a reference, as the Search results list it: its file by origin, its 0-based
// line, its span in UTF-8 bytes as every backend place, and the line's text ("" for a reference: read when shown).
type TextSearchHit struct {
	Path    string `json:"path"`
	Rel     string `json:"rel"`    // its path from the folder it was found in, slash separated
	Origin  string `json:"origin"` // that folder's
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	End     int    `json:"end"`
	Preview string `json:"preview"`
}

// TextSearchHits is one file's hits, sent under their search's ID as it finds them.
type TextSearchHits struct {
	ID   string          `json:"id"`
	Hits []TextSearchHit `json:"hits"`
}

// TextSearchResult is how a text search ended: how many hits it sent, and whether it stopped at the cap.
type TextSearchResult struct {
	Found    int  `json:"found"`
	LimitHit bool `json:"limitHit"`
}

// TextReplaceQuery is a Replace all from the IDE's search: the search, over the mods' folders only (the game's
// files are never written), the text each match becomes, and the files the editor holds, which it changes itself.
type TextReplaceQuery struct {
	Search TextSearchQuery   `json:"search"`
	Text   string            `json:"text"`
	Open   map[string]string `json:"open"` // the files open in the editor, by path: their text there, unsaved
}

// TextReplaceResult is how many matches a Replace all changed, in how many files, and the new text of each open
// file it changed, which the editor takes (unsaved).
type TextReplaceResult struct {
	Replaced int               `json:"replaced"`
	Files    int               `json:"files"`
	Texts    map[string]string `json:"texts"`
}

// FileSearchQuery is a filename search across workspace folders.
type FileSearchQuery struct {
	FilePattern string             `json:"filePattern"`
	MaxResults  int                `json:"maxResults"`
	Folders     []TextSearchFolder `json:"folders"`
}

// FileSearchHit is one path from FileSearch.
type FileSearchHit struct {
	Path string `json:"path"`
}

// FileSearchResult is the capped path list from FileSearch.
type FileSearchResult struct {
	Hits     []FileSearchHit `json:"hits"`
	LimitHit bool            `json:"limitHit"`
}

// grep is one text search over the folders of a query: what every worker shares (the expression, the whole-file
// test, the cap, where a file's hits go) and what they count together (the hits sent, whether the cap was hit).
type grep struct {
	re      *regexp.Regexp
	holds   func(text string) bool
	maxHits int
	send    func(hits []TextSearchHit)

	mu       sync.Mutex
	found    int
	limitHit bool
}

// replacer is one Replace all: the search, the text each match becomes, the files the editor holds (by path key),
// and what it changed.
type replacer struct {
	re       *regexp.Regexp
	isRegexp bool
	text     string
	open     map[string]string // the editor's files by inventory.PathKey: their path
	out      TextReplaceResult
}

// globSet is a folder's filters as ignore rules: the paths to keep and the paths to drop.
type globSet struct {
	includes inventory.Rules
	excludes inventory.Rules
}

// maxResults is the most hits a search sends, whatever the page asks for.
const maxResults = 10000

// TextSearch walks workspace folders and greps file bodies in-process, sending each file's hits as it finds them.
func TextSearch(ctx context.Context, query TextSearchQuery, send func(TextSearchHits)) (TextSearchResult, error) {
	if strings.TrimSpace(query.Pattern) == "" {
		return TextSearchResult{}, nil
	}
	re, err := compileSearch(query)
	if err != nil {
		return TextSearchResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	search := &grep{re: re, holds: wholeFileTest(query), send: func(hits []TextSearchHit) {
		send(TextSearchHits{ID: query.ID, Hits: hits})
	}}
	maxHits := capResults(query.MaxResults)
	out := TextSearchResult{}
	for _, folder := range query.Folders {
		root := folderPath(folder)
		if root == "" {
			continue
		}
		if out.Found >= maxHits {
			out.LimitHit = true
			break
		}
		search.maxHits = maxHits - out.Found
		count, stopped := search.folder(ctx, root, folder.Origin, folderGlobs(folder))
		out.Found += count
		if stopped {
			out.LimitHit = true
			break
		}
	}
	return out, nil
}

// Replace writes every match of a text search as the replacement, line by line as the search matches; a regular
// expression's replacement names its groups ($1), a plain one is written as it is.
func Replace(ctx context.Context, query TextReplaceQuery) (TextReplaceResult, error) {
	if strings.TrimSpace(query.Search.Pattern) == "" {
		return TextReplaceResult{}, nil
	}
	re, err := compileSearch(query.Search)
	if err != nil {
		return TextReplaceResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	replace := &replacer{
		re: re, isRegexp: query.Search.IsRegexp, text: query.Text, open: map[string]string{},
		out: TextReplaceResult{Texts: map[string]string{}},
	}
	for path := range query.Open {
		replace.open[inventory.PathKey(path)] = path
	}
	for _, folder := range query.Search.Folders {
		root := folderPath(folder)
		if root == "" {
			continue
		}
		globs := folderGlobs(folder)
		if err := replace.disk(ctx, root, globs); err != nil {
			return replace.out, err
		}
		replace.openFiles(root, globs, query.Open)
	}
	return replace.out, nil
}

// FileSearch lists files whose path contains FilePattern under workspace folders.
func FileSearch(ctx context.Context, query FileSearchQuery) FileSearchResult {
	maxHits := capResults(query.MaxResults)
	needle := strings.ToLower(strings.TrimSpace(query.FilePattern))
	out := FileSearchResult{Hits: []FileSearchHit{}}
	for _, folder := range query.Folders {
		root := folderPath(folder)
		if root == "" {
			continue
		}
		if len(out.Hits) >= maxHits {
			out.LimitHit = true
			break
		}
		// the walk ends early only on a cancel, which ends the search too
		err := searchFiles(ctx, root, folderGlobs(folder), func(path, _ string) error {
			switch {
			case len(out.Hits) >= maxHits:
				out.LimitHit = true
				return filepath.SkipAll
			case strings.Contains(strings.ToLower(path), needle):
				out.Hits = append(out.Hits, FileSearchHit{Path: path})
			}
			return nil
		})
		if err != nil || out.LimitHit {
			break
		}
	}
	return out
}

// wholeFileTest is the first look at a file for a plain query, in the whole of it, in any case where case is
// ignored (an ASCII one compared in place, any other in a lower-cased copy); most files fail that, and only one
// that holds it is matched line by line. A regular expression is tested by nothing: every file is matched.
func wholeFileTest(query TextSearchQuery) func(text string) bool {
	literal := query.Pattern
	switch ascii := utf8.RuneCountInString(literal) == len(literal); {
	case query.IsRegexp:
		return func(string) bool { return true }
	case query.IsCaseSensitive:
		return func(text string) bool { return strings.Contains(text, literal) }
	case ascii:
		literal = strings.ToLower(literal)
		return func(text string) bool { return containsFold(text, literal) }
	}
	literal = strings.ToLower(literal)
	return func(text string) bool { return strings.Contains(strings.ToLower(text), literal) }
}

// compileSearch is the query's pattern as an expression: quoted unless it is a regular expression, held to whole
// words and to its case as the query says.
func compileSearch(query TextSearchQuery) (*regexp.Regexp, error) {
	pattern := query.Pattern
	if !query.IsRegexp {
		pattern = regexp.QuoteMeta(pattern)
	}
	if query.IsWordMatch {
		pattern = `\b(?:` + pattern + `)\b`
	}
	if !query.IsCaseSensitive {
		pattern = `(?i)` + pattern
	}
	return regexp.Compile(pattern)
}

// folderPath is a folder's path as the system writes it; "" for a folder with none.
func folderPath(folder TextSearchFolder) string {
	path := filepath.Clean(filepath.FromSlash(folder.Path))
	if path == "" || path == "." {
		return ""
	}
	return path
}

// folderGlobs is a folder's filters as ignore rules, one rule a glob: a leading "!" makes an include an exclude.
func folderGlobs(folder TextSearchFolder) globSet {
	var includes, excludes []string
	for _, include := range folder.Includes {
		if rest, negated := strings.CutPrefix(strings.TrimSpace(include), "!"); negated {
			excludes = append(excludes, rest)
		} else {
			includes = append(includes, include)
		}
	}
	for _, exclude := range folder.Excludes {
		excludes = append(excludes, strings.TrimPrefix(strings.TrimSpace(exclude), "!"))
	}
	return globSet{
		includes: inventory.IgnoreRules(strings.Join(includes, "\n")),
		excludes: inventory.IgnoreRules(strings.Join(excludes, "\n")),
	}
}

// keeps is whether the filters keep a file at rel under the folder: no exclude covers it, and with includes, one does
// (a rule that matches a folder covers every file under it).
func (globs globSet) keeps(rel string) bool {
	if globs.excludes.Covers(rel) {
		return false
	}
	return len(globs.includes) == 0 || globs.includes.Covers(rel)
}

// folder greps the files under root, sending each one's hits as it is done, at most the search's maxHits in all,
// and says how many it sent and whether it stopped at the cap.
func (grep *grep) folder(ctx context.Context, root, origin string, globs globSet) (int, bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// a worker per core greps the files the walk hands it
	var group errgroup.Group
	files := make(chan [2]string)
	for range runtime.NumCPU() {
		group.Go(func() error {
			grep.worker(ctx, cancel, files, origin)
			return nil
		})
	}

	// only a cancel ends the walk early, and the caller's ctx already says so
	_ = searchFiles(ctx, root, globs, func(path, rel string) error {
		// a file whose format holds no text (an image, a bank, a model) is never opened
		if !inventory.Classify(path, rel).HoldsText() {
			return nil
		}
		select {
		case files <- [2]string{path, rel}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	close(files)
	_ = group.Wait()
	count, stopped := grep.found, grep.limitHit
	grep.found, grep.limitHit = 0, false
	return count, stopped
}

// worker greps the files it is handed and sends each file's hits as it is done, as many as the cap leaves; the hit
// that passes the cap cancels the search.
func (grep *grep) worker(ctx context.Context, cancel context.CancelFunc, files <-chan [2]string, origin string) {
	for file := range files {
		if ctx.Err() != nil {
			continue
		}
		path, rel := file[0], file[1]
		hits := grep.file(path)
		for i := range hits {
			hits[i].Rel, hits[i].Origin = rel, origin
		}
		grep.mu.Lock()
		kept := hits[:min(len(hits), grep.maxHits-grep.found)]
		grep.found += len(kept)
		if len(kept) > 0 {
			grep.send(kept)
		}
		if len(kept) < len(hits) {
			grep.limitHit = true
			cancel()
		}
		grep.mu.Unlock()
	}
}

// file is path's matches; none for a file that is not text (inventory.LoadText).
func (grep *grep) file(path string) []TextSearchHit {
	loaded, err := inventory.LoadText(path)
	if err != nil || !grep.holds(loaded.Content) {
		return nil
	}
	text := loaded.Content
	lines := jomini.NewLineIndex(text)
	var hits []TextSearchHit
	// the empty line after a last line ending is none: it starts at the end of the text
	for lineNo := 0; lines.OffsetAt(lineNo, 0) < len(text); lineNo++ {
		line := lines.LineText(text, lineNo)
		for _, loc := range grep.re.FindAllStringIndex(line, -1) {
			hits = append(hits, TextSearchHit{Path: path, Line: lineNo, Col: loc[0], End: loc[1], Preview: line})
		}
	}
	return hits
}

// disk replaces in the files under root that the globs keep, except the ones the editor holds and the images,
// writing each changed file whole.
func (replacer *replacer) disk(ctx context.Context, root string, globs globSet) error {
	return searchFiles(ctx, root, globs, func(path, rel string) error {
		_, isOpen := replacer.open[inventory.PathKey(path)]
		if isOpen || !inventory.Classify(path, rel).HoldsText() {
			return nil
		}
		text, err := inventory.LoadText(path)
		if err != nil {
			return nil
		}
		content, replaced := replaceLines(text.Content, replacer.re, replacer.isRegexp, replacer.text)
		if replaced == 0 {
			return nil
		}
		// the file is written as it was read: its byte order mark, and Windows-1252 where it was that
		text.Content = content
		if err := inventory.SaveText(path, text); err != nil {
			return err
		}
		replacer.out.Replaced += replaced
		replacer.out.Files++
		return nil
	})
}

// openFiles replaces in the open files in this folder that its globs keep, in their unsaved text, and takes each
// off the editor's list so no later folder meets it again.
func (replacer *replacer) openFiles(root string, globs globSet, texts map[string]string) {
	for key, path := range replacer.open {
		if rel, inside := inventory.RelOf(root, path); !inside || !globs.keeps(rel) {
			continue
		}
		if text, replaced := replaceLines(texts[path], replacer.re, replacer.isRegexp, replacer.text); replaced > 0 {
			replacer.out.Texts[path] = text
			replacer.out.Replaced += replaced
			replacer.out.Files++
		}
		delete(replacer.open, key)
	}
}

// replaceLines is a text with every match of the search replaced, a line at a time as the search matches (a line's
// ending kept), and how many it replaced.
func replaceLines(text string, re *regexp.Regexp, isRegexp bool, replacement string) (string, int) {
	lines, replaced := jomini.NewLineIndex(text), 0
	var replacedText strings.Builder
	for line := range lines.LineCount() {
		start, body := lines.OffsetAt(line, 0), lines.LineText(text, line)
		ending := text[start+len(body) : lines.OffsetAt(line+1, 0)]
		matches := len(re.FindAllStringIndex(body, -1))
		replaced += matches
		switch {
		case matches == 0:
		case isRegexp:
			body = re.ReplaceAllString(body, replacement)
		default:
			body = re.ReplaceAllLiteralString(body, replacement)
		}
		replacedText.WriteString(body + ending)
	}
	return replacedText.String(), replaced
}

// capResults is the most hits a query gets: what it asks for, or the cap where it asks for none or more.
func capResults(requested int) int {
	if requested <= 0 || requested > maxResults {
		return maxResults
	}
	return requested
}

// containsFold is whether text holds lit in any case, lit lower-cased ASCII, compared in place.
func containsFold(text, lit string) bool {
	cases := lit[:1] + strings.ToUpper(lit[:1])
	for len(text) >= len(lit) {
		i := strings.IndexAny(text, cases)
		if i < 0 || len(text)-i < len(lit) {
			return false
		}
		if strings.EqualFold(text[i:i+len(lit)], lit) {
			return true
		}
		text = text[i+1:]
	}
	return false
}

// searchFiles visits each file under root that the folder's globs keep, by the engine's walk (linked folders
// followed, dot names, files and folders, left out); visit returning filepath.SkipAll stops it, and so does a canceled
// ctx.
func searchFiles(ctx context.Context, root string, globs globSet, visit func(path, rel string) error) error {
	return inventory.Walk(root, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rel, inside := inventory.RelOf(root, path); inside && globs.keeps(rel) {
			return visit(path, rel)
		}
		return nil
	})
}
