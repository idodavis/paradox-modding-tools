// search.go is Find in Files and Replace all over the workspace's folders, and the file search: each file greped in
// a worker per core, its hits sent as it is done.

package pages

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/inventory"
)

// maxResults is the most hits a search sends, whatever the page asks for.
const maxResults = 10000

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
	holds   func(data []byte) bool
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
	open     map[string]string // the editor's files by appdata.PathKey: their path
	out      TextReplaceResult
}

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
		replace.open[appdata.PathKey(path)] = path
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
func wholeFileTest(query TextSearchQuery) func(data []byte) bool {
	literal := []byte(query.Pattern)
	switch ascii := utf8.RuneCount(literal) == len(literal); {
	case query.IsRegexp:
		return func([]byte) bool { return true }
	case query.IsCaseSensitive:
		return func(data []byte) bool { return bytes.Contains(data, literal) }
	case ascii:
		literal = bytes.ToLower(literal)
		return func(data []byte) bool { return containsFold(data, literal) }
	}
	literal = bytes.ToLower(literal)
	return func(data []byte) bool { return bytes.Contains(bytes.ToLower(data), literal) }
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

// folder greps the files under root, sending each one's hits as it is done, at most the search's maxHits in all,
// and says how many it sent and whether it stopped at the cap.
func (grep *grep) folder(ctx context.Context, root, origin string, globs globSet) (int, bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// a worker per core, each with its own read buffer for the whole search, greps the files the walk hands it
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
		// an image the format table names is never opened
		if inventory.Classify(path, filepath.ToSlash(rel)) == inventory.Image {
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

// worker greps the files it is handed, one buffer kept across them, and sends each file's hits as it is done, as
// many as the cap leaves; the hit that passes the cap cancels the search.
func (grep *grep) worker(ctx context.Context, cancel context.CancelFunc, files <-chan [2]string, origin string) {
	var buf []byte
	for file := range files {
		if ctx.Err() != nil {
			continue
		}
		path, rel := file[0], file[1]
		hits := grep.file(path, &buf)
		for i := range hits {
			hits[i].Rel, hits[i].Origin = filepath.ToSlash(rel), origin
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

// file is path's matches, the file read into buf, which keeps what it grew to for the next file.
func (grep *grep) file(path string, buf *[]byte) []TextSearchHit {
	data := searchable(path, *buf)
	if data != nil {
		*buf = data[:0]
	}
	if data == nil || !grep.holds(data) {
		return nil
	}
	text := string(data)
	var hits []TextSearchHit
	lineNo := 0
	for len(text) > 0 {
		line, rest, hadNL := strings.Cut(text, "\n")
		if !hadNL {
			rest = ""
		}
		line = strings.TrimRight(line, "\r")
		for _, loc := range grep.re.FindAllStringIndex(line, -1) {
			hits = append(hits, TextSearchHit{Path: path, Line: lineNo, Col: loc[0], End: loc[1], Preview: line})
		}
		lineNo++
		text = rest
	}
	return hits
}

// disk replaces in the files under root that the globs keep, except the ones the editor holds and the images,
// writing each changed file whole.
func (replacer *replacer) disk(ctx context.Context, root string, globs globSet) error {
	return searchFiles(ctx, root, globs, func(path, rel string) error {
		_, isOpen := replacer.open[appdata.PathKey(path)]
		if isOpen || inventory.Classify(path, filepath.ToSlash(rel)) == inventory.Image {
			return nil
		}
		data := searchable(path, nil)
		if data == nil {
			return nil
		}
		text, replaced := replaceLines(string(data), replacer.re, replacer.isRegexp, replacer.text)
		if replaced == 0 {
			return nil
		}
		if err := appdata.WriteBytes(path, []byte(text)); err != nil {
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
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) || !pathMatchesGlobs(rel, globs) {
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
// CR kept), and how many it replaced.
func replaceLines(text string, re *regexp.Regexp, isRegexp bool, replacement string) (string, int) {
	lines, replaced := strings.Split(text, "\n"), 0
	for i, line := range lines {
		body, cr := strings.CutSuffix(line, "\r")
		matches := len(re.FindAllStringIndex(body, -1))
		if matches == 0 {
			continue
		}
		replaced += matches
		if isRegexp {
			body = re.ReplaceAllString(body, replacement)
		} else {
			body = re.ReplaceAllLiteralString(body, replacement)
		}
		if cr {
			body += "\r"
		}
		lines[i] = body
	}
	return strings.Join(lines, "\n"), replaced
}

// capResults is the most hits a query gets: what it asks for, or the cap where it asks for none or more.
func capResults(requested int) int {
	if requested <= 0 || requested > maxResults {
		return maxResults
	}
	return requested
}

// searchable is a file's bytes as a search reads them, read into buf (grown as needed; nil for a new one); none
// where it can't be read, is empty, is over the size cap or is binary.
func searchable(path string, buf []byte) []byte {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 || info.Size() > inventory.MaxTextBytes {
		return nil
	}
	// a NUL in the head marks the file binary and the rest is never read, which keeps a mod's meshes
	// and images out of the search without listing extensions
	size, probe := int(info.Size()), min(inventory.BinaryHead, int(info.Size()))
	head := slices.Grow(buf[:0], probe)[:probe]
	if _, err := io.ReadFull(file, head); err != nil || bytes.IndexByte(head, 0) >= 0 {
		return nil
	}
	data := slices.Grow(head, size-probe)[:size]
	if _, err := io.ReadFull(file, data[probe:]); err != nil {
		return nil
	}
	return data
}

// containsFold is whether data holds lit in any case, lit lower-cased ASCII, compared in place.
func containsFold(data, lit []byte) bool {
	cases := string(lit[0]) + strings.ToUpper(string(lit[0]))
	for len(data) >= len(lit) {
		i := bytes.IndexAny(data, cases)
		if i < 0 || len(data)-i < len(lit) {
			return false
		}
		if bytes.EqualFold(data[i:i+len(lit)], lit) {
			return true
		}
		data = data[i+1:]
	}
	return false
}

// searchFiles visits each file under root that the folder's globs keep, by the engine's walk (linked folders
// followed, .git never entered); visit returning filepath.SkipAll stops it, and so does a canceled ctx.
func searchFiles(ctx context.Context, root string, globs globSet, visit func(path, rel string) error) error {
	return inventory.Walk(root, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rel, err := filepath.Rel(root, path); err == nil && pathMatchesGlobs(rel, globs) {
			return visit(path, rel)
		}
		return nil
	})
}
