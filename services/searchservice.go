// searchservice.go runs in-process Find in Files for the IDE workbench.
package services

import (
	"bytes"
	"context"
	"errors"
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
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/lang/source"
)

// SearchService is the Wails RPC shell for Find in Files. A text search sends each file's hits as
// it finds them: to found, or where it is nil to the frontend as search:hits.
type SearchService struct {
	found func(TextSearchHits)
}

// the generator types the hits a text search sends
func init() { application.RegisterEvent[TextSearchHits](EventSearchHits) }

const (
	searchMaxResults   = 10000
	searchMaxFileBytes = 8 * 1024 * 1024
	searchNulProbe     = 8 * 1024
)

// TextSearchFolder is one workspace folder plus optional glob filters.
type TextSearchFolder struct {
	Path                 string   `json:"path"`
	Includes             []string `json:"includes,omitempty"`
	Excludes             []string `json:"excludes,omitempty"`
	DisregardIgnoreFiles bool     `json:"disregardIgnoreFiles,omitempty"`
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

func rgGlob(glob string) string {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return ""
	}
	neg := strings.HasPrefix(glob, "!")
	if neg {
		glob = strings.TrimSpace(glob[1:])
	}
	if glob != "" && !strings.Contains(glob, "**") {
		glob = "**/" + strings.TrimPrefix(glob, "/")
	}
	if neg {
		return "!" + glob
	}
	return glob
}

func folderGlobs(folder TextSearchFolder) []string {
	seen := map[string]bool{}
	var out []string
	add := func(glob string) {
		glob = rgGlob(glob)
		if glob == "" || seen[glob] {
			return
		}
		seen[glob] = true
		out = append(out, glob)
	}
	for _, include := range folder.Includes {
		add(include)
	}
	for _, exclude := range folder.Excludes {
		if strings.HasPrefix(exclude, "!") {
			add(exclude)
			continue
		}
		add("!" + exclude)
	}
	return out
}

func splitGlobs(globs []string) globSet {
	var gs globSet
	for _, glob := range globs {
		if after, ok := strings.CutPrefix(glob, "!"); ok {
			gs.excludes = append(gs.excludes, after)
			continue
		}
		gs.includes = append(gs.includes, glob)
	}
	return gs
}

func pathMatchPattern(pattern, rel string) bool {
	if ok, _ := doublestar.PathMatch(pattern, rel); ok {
		return true
	}
	if !strings.HasSuffix(pattern, "/**") {
		return false
	}
	dirPat := strings.TrimSuffix(pattern, "/**")
	if dirPat == "" {
		return true
	}
	if after, ok := strings.CutPrefix(dirPat, "**/"); ok {
		suffix := after
		if suffix == "" {
			return true
		}
		if ok, _ := doublestar.PathMatch(suffix+"/**", rel); ok {
			return true
		}
		return strings.HasPrefix(rel, suffix+"/") || rel == suffix
	}
	ok, _ := doublestar.PathMatch(dirPat+"/**", rel)
	return ok
}

func pathMatchesGlobs(rel string, gs globSet) bool {
	rel = filepath.ToSlash(rel)
	for _, ex := range gs.excludes {
		if pathMatchPattern(ex, rel) {
			return false
		}
	}
	if len(gs.includes) == 0 {
		return true
	}
	for _, inc := range gs.includes {
		if pathMatchPattern(inc, rel) {
			return true
		}
	}
	return false
}

func byteToRune(text string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(text) {
		return utf8.RuneCountInString(text)
	}
	return utf8.RuneCountInString(text[:byteOff])
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

func skipSearchDir(name string, isDir bool) bool {
	return isDir && name == ".git"
}

func fileHasNUL(data []byte) bool {
	n := min(len(data), searchNulProbe)
	return bytes.Contains(data[:n], []byte{0})
}

func grepFile(path string, re *regexp.Regexp, holds func(data []byte) bool) ([]TextSearchHit, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 || info.Size() > searchMaxFileBytes {
		return nil, nil
	}
	// a NUL in the head marks the file binary and the rest is never read, which keeps a mod's meshes
	// and images out of the search without listing extensions
	head := make([]byte, min(int64(searchNulProbe), info.Size()))
	if _, err := io.ReadFull(file, head); err != nil {
		return nil, err
	}
	if fileHasNUL(head) {
		return nil, nil
	}
	rest, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	data := append(head, rest...)
	if !holds(data) {
		return nil, nil
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
				Col:     byteToRune(line, loc[0]),
				EndCol:  byteToRune(line, loc[1]),
				Preview: line,
			})
		}
		lineNo++
		text = rest
	}
	return hits, nil
}

func textSearchFolder(
	ctx context.Context, root string, gs globSet, re *regexp.Regexp, holds func(data []byte) bool, maxHits int,
	send func(hits []TextSearchHit),
) (int, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	found, limitHit := 0, false

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.NumCPU())

	walkErr := searchFiles(gctx, root, gs, func(path, rel string) error {
		// an image the format table names is never opened: two in five of an install's files
		if source.Classify(path, filepath.ToSlash(rel)) == source.Image {
			return nil
		}
		group.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			fileHits, err := grepFile(path, re, holds)
			if err != nil {
				return nil
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
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return found, limitHit, err
	}
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		return found, limitHit, walkErr
	}
	return found, limitHit, nil
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
		case entry.IsDir() && skipSearchDir(entry.Name(), true):
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
	send := func(hits []TextSearchHit) {
		sent := TextSearchHits{ID: query.ID, Hits: hits}
		if s.found != nil {
			s.found(sent)
		} else {
			emit(EventSearchHits, sent)
		}
	}
	maxHits := capResults(query.MaxResults)
	out := TextSearchResult{}
	for _, folder := range query.Folders {
		path := folderPath(folder)
		if path == "" {
			continue
		}
		remain := maxHits - out.Found
		if remain <= 0 {
			out.LimitHit = true
			break
		}
		gs := splitGlobs(folderGlobs(folder))
		found, limit, err := textSearchFolder(ctx, path, gs, re, holds, remain, send)
		if err != nil && !errors.Is(err, context.Canceled) {
			return TextSearchResult{}, err
		}
		out.Found += found
		if limit || errors.Is(err, context.Canceled) {
			out.LimitHit = true
			break
		}
	}
	return out, nil
}

// FileSearch lists files whose path contains FilePattern under workspace folders.
func (s *SearchService) FileSearch(ctx context.Context, query FileSearchQuery) (FileSearchResult, error) {
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
		gs := splitGlobs(folderGlobs(folder))
		err := searchFiles(ctx, root, gs, func(path, _ string) error {
			switch {
			case len(out.Hits) >= maxHits:
				out.LimitHit = true
				return filepath.SkipAll
			case strings.Contains(strings.ToLower(path), needle):
				out.Hits = append(out.Hits, FileSearchHit{Path: path})
			}
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			return FileSearchResult{}, err
		}
		if out.LimitHit {
			break
		}
	}
	return out, nil
}
