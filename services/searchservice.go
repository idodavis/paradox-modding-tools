// searchservice.go runs in-process Find in Files and Replace all for the IDE.

package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/local"
)

// SearchService is the Wails RPC shell for Find in Files. A text search sends each file's hits to
// the frontend as search:hits as it finds them.
type SearchService struct{}

const (
	searchMaxResults   = 10000
	searchMaxFileBytes = 8 * 1024 * 1024
	searchNulProbe     = 8 * 1024
)

// TextSearchFolder is one workspace folder plus optional glob filters.
type TextSearchFolder struct {
	Path     string   `json:"path"`
	Includes []string `json:"includes,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

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

// TextSearchHit is one match line mapped for monaco (0-based columns).
type TextSearchHit struct {
	Path    string `json:"path"`
	Rel     string `json:"rel"`    // its path from the folder it was found in, slash separated
	Folder  int    `json:"folder"` // that folder's index in the query
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	EndCol  int    `json:"endCol"`
	Preview string `json:"preview"`
}

// TextSearchHits is one file's hits, sent under their search's ID as it finds them.
type TextSearchHits struct {
	ID   string          `json:"id"`
	Hits []TextSearchHit `json:"hits"`
}

// TextSearchResult is how a text search ended: how many hits it sent, and whether it stopped at
// the cap.
type TextSearchResult struct {
	Found    int  `json:"found"`
	LimitHit bool `json:"limitHit"`
}

// TextReplaceQuery is a Replace all from the IDE's search: the search, over the mods' folders only (the game's
// files are never written), the text each match becomes, and the files the editor holds, which it changes itself.
type TextReplaceQuery struct {
	Search TextSearchQuery `json:"search"`
	Text   string          `json:"text"`
	Skip   []string        `json:"skip"`
}

// TextReplaceResult is how many matches a Replace all changed, in how many files.
type TextReplaceResult struct {
	Replaced int `json:"replaced"`
	Files    int `json:"files"`
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

type globSet struct {
	includes []string
	excludes []string
}

func capResults(n int) int {
	if n <= 0 || n > searchMaxResults {
		return searchMaxResults
	}
	return n
}

// folderGlobs is a folder's filters as rg reads them: a glob with no ** matches at any depth, and a
// leading "!" makes an include an exclude.
func folderGlobs(folder TextSearchFolder) globSet {
	var gs globSet
	add := func(glob string, exclude bool) {
		glob = strings.TrimSpace(glob)
		if after, negated := strings.CutPrefix(glob, "!"); negated {
			glob, exclude = strings.TrimSpace(after), true
		}
		if glob == "" {
			return
		}
		if !strings.Contains(glob, "**") {
			glob = "**/" + strings.TrimPrefix(glob, "/")
		}
		if exclude {
			gs.excludes = append(gs.excludes, glob)
		} else {
			gs.includes = append(gs.includes, glob)
		}
	}
	for _, include := range folder.Includes {
		add(include, false)
	}
	for _, exclude := range folder.Excludes {
		add(exclude, true)
	}
	return gs
}

func pathMatchesGlobs(rel string, gs globSet) bool {
	rel = filepath.ToSlash(rel)
	for _, ex := range gs.excludes {
		if ok, _ := doublestar.Match(ex, rel); ok {
			return false
		}
	}
	if len(gs.includes) == 0 {
		return true
	}
	for _, inc := range gs.includes {
		if ok, _ := doublestar.Match(inc, rel); ok {
			return true
		}
	}
	return false
}

func folderPath(folder TextSearchFolder) string {
	path := filepath.Clean(filepath.FromSlash(folder.Path))
	if path == "" || path == "." {
		return ""
	}
	return path
}

func compileSearchRegex(query TextSearchQuery) (*regexp.Regexp, error) {
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

// searchable is a file's bytes as a search reads them; none where it can't be read, is empty, is over the size cap
// or is binary.
func searchable(path string) []byte {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 || info.Size() > searchMaxFileBytes {
		return nil
	}
	// a NUL in the head marks the file binary and the rest is never read, which keeps a mod's meshes
	// and images out of the search without listing extensions
	head := make([]byte, min(int64(searchNulProbe), info.Size()))
	if _, err := io.ReadFull(file, head); err != nil || bytes.IndexByte(head, 0) >= 0 {
		return nil
	}
	rest, err := io.ReadAll(file)
	if err != nil {
		return nil
	}
	return append(head, rest...)
}

// grepFile is path's matches.
func grepFile(path string, re *regexp.Regexp, holds func(data []byte) bool) []TextSearchHit {
	data := searchable(path)
	if data == nil || !holds(data) {
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
		for _, loc := range re.FindAllStringIndex(line, -1) {
			hits = append(hits, TextSearchHit{
				Path:    path,
				Line:    lineNo,
				Col:     utf8.RuneCountInString(line[:loc[0]]),
				EndCol:  utf8.RuneCountInString(line[:loc[1]]),
				Preview: line,
			})
		}
		lineNo++
		text = rest
	}
	return hits
}

// textSearchFolder greps the files under root, sending each one's hits as it is done, at most maxHits
// in all, and says how many it sent and whether it stopped at the cap.
func textSearchFolder(
	ctx context.Context, root string, folder int, gs globSet, re *regexp.Regexp, holds func(data []byte) bool, maxHits int,
	send func(hits []TextSearchHit),
) (found int, limitHit bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var group errgroup.Group
	group.SetLimit(runtime.NumCPU())

	// only a cancel ends the walk early, and the caller's ctx already says so
	_ = searchFiles(ctx, root, gs, func(path, rel string) error {
		// an image the format table names is never opened
		if source.Classify(path, filepath.ToSlash(rel)) == source.Image {
			return nil
		}
		group.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			fileHits := grepFile(path, re, holds)
			for i := range fileHits {
				fileHits[i].Rel, fileHits[i].Folder = filepath.ToSlash(rel), folder
			}
			// a file's hits are sent as it is done, as many as the cap leaves
			mu.Lock()
			defer mu.Unlock()
			kept := fileHits[:min(len(fileHits), maxHits-found)]
			found += len(kept)
			if len(kept) > 0 {
				send(kept)
			}
			if len(kept) < len(fileHits) {
				limitHit = true
				cancel()
			}
			return nil
		})
		return nil
	})
	_ = group.Wait()
	return found, limitHit
}

// searchFiles visits each file under root that the folder's globs keep, never entering a folder
// search skips; visit returning filepath.SkipAll stops it, and so does a cancelled ctx.
func searchFiles(ctx context.Context, root string, gs globSet, visit func(path, rel string) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case ctx.Err() != nil:
			return ctx.Err()
		case entry.IsDir() && entry.Name() == ".git":
			return filepath.SkipDir
		case entry.IsDir():
			return nil
		}
		if rel, err := filepath.Rel(root, path); err == nil && pathMatchesGlobs(rel, gs) {
			return visit(path, rel)
		}
		return nil
	})
}

// TextSearch walks workspace folders and greps file bodies in-process, sending each file's hits
// as it finds them.
func (s *SearchService) TextSearch(ctx context.Context, query TextSearchQuery) (TextSearchResult, error) {
	if strings.TrimSpace(query.Pattern) == "" {
		return TextSearchResult{}, nil
	}
	re, err := compileSearchRegex(query)
	if err != nil {
		return TextSearchResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	// a plain query is first looked for in the whole of a file, lower-cased where case is ignored;
	// most files fail that, and only one that holds it is matched line by line
	holds := func(data []byte) bool { return true }
	if !query.IsRegexp {
		literal := []byte(query.Pattern)
		if !query.IsCaseSensitive {
			literal = bytes.ToLower(literal)
		}
		holds = func(data []byte) bool {
			if !query.IsCaseSensitive {
				data = bytes.ToLower(data)
			}
			return bytes.Contains(data, literal)
		}
	}
	send := func(hits []TextSearchHit) { emit(EventSearchHits, TextSearchHits{ID: query.ID, Hits: hits}) }
	maxHits := capResults(query.MaxResults)
	out := TextSearchResult{}
	for i, folder := range query.Folders {
		path := folderPath(folder)
		if path == "" {
			continue
		}
		if out.Found >= maxHits {
			out.LimitHit = true
			break
		}
		found, limit := textSearchFolder(ctx, path, i, folderGlobs(folder), re, holds, maxHits-out.Found, send)
		out.Found += found
		if limit {
			out.LimitHit = true
			break
		}
	}
	return out, nil
}

// Replace writes every match of a text search as the replacement, line by line as the search matches; a regular
// expression's replacement names its groups ($1), a plain one is written as it is.
func (s *SearchService) Replace(ctx context.Context, query TextReplaceQuery) (TextReplaceResult, error) {
	if strings.TrimSpace(query.Search.Pattern) == "" {
		return TextReplaceResult{}, nil
	}
	re, err := compileSearchRegex(query.Search)
	if err != nil {
		return TextReplaceResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	// a path as the session keys an open file
	key := func(path string) string { return strings.ToLower(filepath.Clean(filepath.FromSlash(path))) }
	skip := map[string]bool{}
	for _, path := range query.Skip {
		skip[key(path)] = true
	}
	var out TextReplaceResult
	for _, folder := range query.Search.Folders {
		root := folderPath(folder)
		if root == "" {
			continue
		}
		err := searchFiles(ctx, root, folderGlobs(folder), func(path, rel string) error {
			if skip[key(path)] || source.Classify(path, filepath.ToSlash(rel)) == source.Image {
				return nil
			}
			data := searchable(path)
			if data == nil {
				return nil
			}
			lines := strings.Split(string(data), "\n")
			replaced := 0
			for i, line := range lines {
				body, cr := strings.CutSuffix(line, "\r")
				found := len(re.FindAllStringIndex(body, -1))
				if found == 0 {
					continue
				}
				replaced += found
				if query.Search.IsRegexp {
					body = re.ReplaceAllString(body, query.Text)
				} else {
					body = re.ReplaceAllLiteralString(body, query.Text)
				}
				if cr {
					body += "\r"
				}
				lines[i] = body
			}
			if replaced == 0 {
				return nil
			}
			if err := local.WriteBytes(path, []byte(strings.Join(lines, "\n"))); err != nil {
				return err
			}
			out.Replaced += replaced
			out.Files++
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// FileSearch lists files whose path contains FilePattern under workspace folders.
func (s *SearchService) FileSearch(ctx context.Context, query FileSearchQuery) FileSearchResult {
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
