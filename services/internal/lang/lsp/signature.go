// signature.go serves Monaco signature help from script_docs usage text.
package lsp

import (
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

// SignatureHelpResult is the Monaco signature popup.
type SignatureHelpResult struct {
	Label           string   `json:"label"`
	Documentation   string   `json:"documentation,omitempty"`
	Parameters      []string `json:"parameters,omitempty"`
	ActiveParameter int      `json:"activeParameter"`
}

var usageParamRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)\$|\{([A-Za-z0-9_]+)\}`)

// SignatureHelp returns usage-based signature help at (line, UTF-8 column).
func SignatureHelp(s *session.Session, path string, line, col int) *SignatureHelpResult {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	key := at.slotKey
	if key == "" {
		key = at.word
	}
	usage := tokenUsage(s, key)
	if usage == "" {
		if d := s.Resolve(key); d != nil && jomini.IsMacroKind(d.Kind) {
			usage = tokenUsage(s, d.Key)
		}
	}
	if usage == "" {
		return nil
	}
	return &SignatureHelpResult{
		Label:         usage,
		Documentation: fieldDoc(s, key, at.kind, ""),
		Parameters:    paramsFromUsage(usage),
	}
}

func paramsFromUsage(usage string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range usageParamRe.FindAllStringSubmatch(usage, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 && strings.Contains(usage, "{") {
		out = append(out, "block")
	}
	return out
}
