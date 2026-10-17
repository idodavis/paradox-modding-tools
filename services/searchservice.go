// searchservice.go is the IDE's Find in Files, Replace all and file search: three calls into pages, a text search's
// hits sent to the frontend as search:hits as each file is done.

package services

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/pages"
)

// EventSearchHits is a text search done with a file: data is pages.TextSearchHits.
const EventSearchHits = "search:hits"

// registers the event so the bindings generator types it
func init() { application.RegisterEvent[pages.TextSearchHits](EventSearchHits) }

// SearchService answers the IDE's searches.
type SearchService struct{}

// NewSearchService is the search service; it holds nothing.
func NewSearchService() *SearchService { return &SearchService{} }

// TextSearch greps the workspace's folders, sending each file's hits as it finds them.
func (sch *SearchService) TextSearch(ctx context.Context, query pages.TextSearchQuery) (pages.TextSearchResult, error) {
	return pages.TextSearch(ctx, query, func(hits pages.TextSearchHits) { emit(EventSearchHits, hits) })
}

// Replace writes every match of a text search as the replacement, in the files on disk and the ones the editor
// holds.
func (sch *SearchService) Replace(ctx context.Context, query pages.TextReplaceQuery) (pages.TextReplaceResult, error) {
	return pages.Replace(ctx, query)
}

// FileSearch lists the files under the workspace's folders whose path holds a pattern.
func (sch *SearchService) FileSearch(ctx context.Context, query pages.FileSearchQuery) pages.FileSearchResult {
	return pages.FileSearch(ctx, query)
}
