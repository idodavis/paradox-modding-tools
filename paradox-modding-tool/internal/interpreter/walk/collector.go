package walk

import (
	"strconv"
	"strings"

	parser "paradox-modding-tool/internal/interpreter"
)

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
	var out []string
	if expr.Literal != nil {
		out = append(out, collectFromLiteral(expr.Literal)...)
	}
	if expr.Object != nil {
		out = append(out, collectFromObject(expr.Object, selfKey)...)
	}
	return dedupeStrings(out)
}

func collectFromLiteral(lit *parser.Literal) []string {
	if lit == nil {
		return nil
	}
	var out []string
	if lit.Identifier != nil {
		out = append(out, *lit.Identifier)
	}
	if lit.Number != nil {
		n := *lit.Number
		if n == float64(int64(n)) {
			out = append(out, strconv.FormatInt(int64(n), 10))
		}
	}
	if lit.String != nil {
		s := *lit.String
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			out = append(out, s[1:len(s)-1])
		}
	}
	if lit.Array != nil {
		for _, elem := range lit.Array {
			out = append(out, collectFromLiteral(elem)...)
		}
	}
	return out
}

func collectFromObject(obj *parser.Object, selfKey string) []string {
	if obj == nil {
		return nil
	}
	var out []string
	for _, entry := range obj.Entries {
		if entry.Expression != nil {
			k := entry.Expression.Key
			if k != "" && k != selfKey {
				out = append(out, k)
			}
			out = append(out, CollectIdentifiers(entry.Expression, selfKey)...)
		}
		if entry.Literal != nil {
			out = append(out, collectFromLiteral(entry.Literal)...)
		}
		if entry.Object != nil {
			out = append(out, collectFromObject(entry.Object, selfKey)...)
		}
	}
	return out
}

func dedupeStrings(ss []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// LineEnd returns the line number of the last line of the node's raw text (for expressions, use expr.GetRawText()).
func LineEnd(lineStart int, raw string) int {
	return lineStart + strings.Count(raw, "\n")
}
