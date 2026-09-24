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
	"golang.org/x/sync/errgroup"
)

// SearchService is the Wails RPC shell for Find in Files.
type SearchService struct{}

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

// TextSearchResult is the capped hit list from TextSearch.
type TextSearchResult struct {
	Hits     []TextSearchHit `json:"hits"`
	LimitHit bool            `json:"limitHit"`
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

func grepFile(path string, re *regexp.Regexp) ([]TextSearchHit, error) {
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
	text := string(append(head, rest...))
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
	ctx context.Context, root string, gs globSet, re *regexp.Regexp, maxHits int,
) ([]TextSearchHit, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	hits := make([]TextSearchHit, 0, min(maxHits, 32))
	limitHit := false

	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.NumCPU())

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if gctx.Err() != nil {
			return gctx.Err()
		}
		if entry.IsDir() {
			if skipSearchDir(entry.Name(), true) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !pathMatchesGlobs(rel, gs) {
			return nil
		}
		group.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			fileHits, err := grepFile(path, re)
			if err != nil {
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			for _, fileHit := range fileHits {
				if len(hits) >= maxHits {
					limitHit = true
					cancel()
					return nil
				}
				hits = append(hits, fileHit)
			}
			return nil
		})
		return nil
	})
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return hits, limitHit, err
	}
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		return hits, limitHit, walkErr
	}
	return hits, limitHit, nil
}

// TextSearch walks workspace folders and greps file bodies in-process.
func (s *SearchService) TextSearch(ctx context.Context, query TextSearchQuery) (TextSearchResult, error) {
	if strings.TrimSpace(query.Pattern) == "" {
		return TextSearchResult{Hits: []TextSearchHit{}}, nil
	}
	re, err := compileSearchRegex(query)
	if err != nil {
		return TextSearchResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	maxHits := capResults(query.MaxResults)
	out := TextSearchResult{Hits: []TextSearchHit{}}
	for _, folder := range query.Folders {
		path := folderPath(folder)
		if path == "" {
			continue
		}
		remain := maxHits - len(out.Hits)
		if remain <= 0 {
			out.LimitHit = true
			break
		}
		gs := splitGlobs(folderGlobs(folder))
		hits, limit, err := textSearchFolder(ctx, path, gs, re, remain)
		if err != nil && !errors.Is(err, context.Canceled) {
			return TextSearchResult{}, err
		}
		out.Hits = append(out.Hits, hits...)
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
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() {
				if skipSearchDir(entry.Name(), true) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(out.Hits) >= maxHits {
				out.LimitHit = true
				return filepath.SkipAll
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || !pathMatchesGlobs(rel, gs) {
				return nil
			}
			if needle != "" &&
				!strings.Contains(strings.ToLower(path), needle) &&
				!strings.Contains(strings.ToLower(filepath.Base(path)), needle) {
				return nil
			}
			out.Hits = append(out.Hits, FileSearchHit{Path: path})
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
