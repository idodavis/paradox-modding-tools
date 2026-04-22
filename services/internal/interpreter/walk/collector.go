package walk

import (
	"strconv"
	"strings"
	"sync"

	parser "paradox-modding-tools/services/internal/interpreter"
)

var mapPool = sync.Pool{
	New: func() any {
		return make(map[string]bool)
	},
}

// TopLevelKeys returns the set of expression keys at the top level of obj (for attribute detection).
func TopLevelKeys(obj *parser.Object) map[string]bool {
	out := make(map[string]bool)
	if obj == nil {
		return out
	}
	for _, e := range obj.Entries {
		if e.Expression != nil && e.Expression.Key != "" {
			out[e.Expression.Key] = true
		}
	}
	return out
}

// CollectIdentifiers returns identifiers, numeric IDs, and quoted string values from expr (for potential refs).
func CollectIdentifiers(expr *parser.Expression, selfKey string) []string {
	if expr == nil {
		return nil
	}
	seen := mapPool.Get().(map[string]bool)
	defer func() {
		clear(seen)
		mapPool.Put(seen)
	}()
	collectFromExpression(expr, selfKey, seen)

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func collectFromExpression(expr *parser.Expression, selfKey string, seen map[string]bool) {
	if expr.Literal != nil {
		collectFromLiteral(expr.Literal, seen)
	}
	if expr.Object != nil {
		collectFromObject(expr.Object, selfKey, seen)
	}
}

func collectFromLiteral(lit *parser.Literal, seen map[string]bool) {
	if lit == nil {
		return
	}
	if lit.Identifier != nil {
		seen[*lit.Identifier] = true
	}
	if lit.Number != nil {
		n := *lit.Number
		if n == float64(int64(n)) {
			seen[strconv.FormatInt(int64(n), 10)] = true
		}
	}
	if lit.String != nil {
		s := *lit.String
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			seen[s[1:len(s)-1]] = true
		}
	}
	if lit.Array != nil {
		for _, elem := range lit.Array {
			collectFromLiteral(elem, seen)
		}
	}
}

func collectFromObject(obj *parser.Object, selfKey string, seen map[string]bool) {
	if obj == nil {
		return
	}
	for _, entry := range obj.Entries {
		if entry.Expression != nil {
			k := entry.Expression.Key
			if k != "" && k != selfKey {
				seen[k] = true
			}
			collectFromExpression(entry.Expression, selfKey, seen)
		}
		if entry.Literal != nil {
			collectFromLiteral(entry.Literal, seen)
		}
		if entry.Object != nil {
			collectFromObject(entry.Object, selfKey, seen)
		}
	}
}

// LineEnd returns the line number of the last line of the node's raw text (for expressions, use expr.GetRawText()).
func LineEnd(lineStart int, raw string) int {
	return lineStart + strings.Count(raw, "\n")
}
