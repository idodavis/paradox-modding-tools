// searchservice.go runs in-process Find in Files for the IDE workbench.
package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"paradox-modding-tools/services/internal/source"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/sync/errgroup"
)

// SearchService is the Wails RPC shell for Find in Files. FileService stays FS CRUD.
type SearchService struct{}

const (
	searchMaxResults   = 10000
	searchMaxFileBytes = 8 * 1024 * 1024
	searchNulProbe     = 8 * 1024
	searchConcurrency  = 4
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

func rgGlob(g string) string {
	g = strings.TrimSpace(g)
	if g == "" {
		return ""
	}
	neg := strings.HasPrefix(g, "!")
	if neg {
		g = strings.TrimSpace(g[1:])
	}
	if g != "" && !strings.Contains(g, "**") {
		g = "**/" + strings.TrimPrefix(g, "/")
	}
	if neg {
		return "!" + g
	}
	return g
}

func folderGlobs(f TextSearchFolder) []string {
	seen := map[string]bool{}
	var out []string
	add := func(g string) {
		g = rgGlob(g)
		if g == "" || seen[g] {
			return
		}
		seen[g] = true
		out = append(out, g)
	}
	for _, g := range f.Includes {
		add(g)
	}
	for _, g := range f.Excludes {
		if strings.HasPrefix(g, "!") {
			add(g)
			continue
		}
		add("!" + g)
	}
	return out
}

func splitGlobs(all []string) globSet {
	var gs globSet
	for _, g := range all {
		if after, ok := strings.CutPrefix(g, "!"); ok {
			gs.excludes = append(gs.excludes, after)
			continue
		}
		gs.includes = append(gs.includes, g)
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

func byteToRune(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteOff])
}

func folderPath(f TextSearchFolder) string {
	p := filepath.Clean(filepath.FromSlash(f.Path))
	if p == "" || p == "." {
		return ""
	}
	return p
}

func compileSearchRegex(q TextSearchQuery) (*regexp.Regexp, error) {
	pattern := q.Pattern
	if !q.IsRegexp {
		pattern = regexp.QuoteMeta(pattern)
	}
	if q.IsWordMatch {
		pattern = `\b(?:` + pattern + `)\b`
	}
	if !q.IsCaseSensitive {
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > searchMaxFileBytes || fileHasNUL(data) {
		return nil, nil
	}
	text := string(data)
	if len(text) == 0 {
		return nil, nil
	}
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
	ctx context.Context, root string, gs globSet, re *regexp.Regexp, max int,
) ([]TextSearchHit, bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	hits := make([]TextSearchHit, 0, min(max, 32))
	limitHit := false

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(searchConcurrency)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if gctx.Err() != nil {
			return gctx.Err()
		}
		if d.IsDir() {
			if skipSearchDir(d.Name(), true) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !pathMatchesGlobs(rel, gs) {
			return nil
		}
		if f := source.Classify(filepath.ToSlash(rel)); f == source.Image || f == source.Opaque {
			return nil
		}
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			fileHits, err := grepFile(path, re)
			if err != nil {
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			for _, h := range fileHits {
				if len(hits) >= max {
					limitHit = true
					cancel()
					return nil
				}
				hits = append(hits, h)
			}
			return nil
		})
		return nil
	})
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return hits, limitHit, err
	}
	if walkErr != nil && !errors.Is(walkErr, context.Canceled) {
		return hits, limitHit, walkErr
	}
	return hits, limitHit, nil
}

// TextSearch walks workspace folders and greps file bodies in-process.
func (s *SearchService) TextSearch(ctx context.Context, q TextSearchQuery) (TextSearchResult, error) {
	if strings.TrimSpace(q.Pattern) == "" {
		return TextSearchResult{Hits: []TextSearchHit{}}, nil
	}
	re, err := compileSearchRegex(q)
	if err != nil {
		return TextSearchResult{}, fmt.Errorf("invalid pattern: %w", err)
	}
	max := capResults(q.MaxResults)
	out := TextSearchResult{Hits: []TextSearchHit{}}
	for _, folder := range q.Folders {
		p := folderPath(folder)
		if p == "" {
			continue
		}
		remain := max - len(out.Hits)
		if remain <= 0 {
			out.LimitHit = true
			break
		}
		gs := splitGlobs(folderGlobs(folder))
		hits, limit, err := textSearchFolder(ctx, p, gs, re, remain)
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
func (s *SearchService) FileSearch(ctx context.Context, q FileSearchQuery) (FileSearchResult, error) {
	max := capResults(q.MaxResults)
	needle := strings.ToLower(strings.TrimSpace(q.FilePattern))
	out := FileSearchResult{Hits: []FileSearchHit{}}
	for _, folder := range q.Folders {
		p := folderPath(folder)
		if p == "" {
			continue
		}
		if len(out.Hits) >= max {
			out.LimitHit = true
			break
		}
		gs := splitGlobs(folderGlobs(folder))
		err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				if skipSearchDir(d.Name(), true) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(out.Hits) >= max {
				out.LimitHit = true
				return filepath.SkipAll
			}
			rel, err := filepath.Rel(p, path)
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
