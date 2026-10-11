// graph_text.go is a block's text as the page shows it: one line, its source lines, a block's own trigger, a dynamic
// description's parts and an option's names.

package pages

import (
	"strings"

	"paradox-modding-tools/services/internal/jomini"
)

// spanLines is the source lines a span covers, each without its line end; with a limit, the first limit lines and a
// "…" where the span runs on.
func spanLines(parsed *jomini.Result, span jomini.Range, limit int) []string {
	lines := parsed.Lines()
	first := lines.PositionAt(span.Start).Line
	last := lines.PositionAt(max(span.End-1, span.Start)).Line
	var out []string
	for line := first; line <= last; line++ {
		if limit > 0 && len(out) == limit {
			return append(out, "…")
		}
		from, to := lines.OffsetAt(line, 0), len(parsed.Src)
		if line+1 < lines.LineCount() {
			to = lines.OffsetAt(line+1, 0)
		}
		out = append(out, strings.TrimRight(parsed.Src[from:to], "\r\n"))
	}
	return out
}
