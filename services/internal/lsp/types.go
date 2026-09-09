// types.go holds LSP DTOs and shared path helpers.
package lsp

import (
	"os"
	"strings"

	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/session"
)

// Position is a 0-based line and UTF-8 byte column.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open LSP-style span in UTF-8 byte columns.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic is a parse or semantic issue for a file.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1 error, 2 warning
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
}

// HoverValue is one unique harvested RHS or save-site expression.
type HoverValue struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// HoverResult is a structured hover card. The frontend templates Kind/Key/Hint/Docs.
// Origin/rel/path/line describe the winning def site. Vanilla* is the install
// site this winner overlays. Body is loc text. Values are unique ephemeral
// RHS/save-site expressions (frontend formats counts). More is unique names
// omitted after the cap. Owner is the enclosing scripted_* key for a param.
// Usage is TokenUsage.
type HoverResult struct {
	Kind string `json:"kind,omitempty"`
	Key  string `json:"key,omitempty"`
	Hint string `json:"hint,omitempty"`
	Docs string `json:"docs,omitempty"`
	// Scope is the scope type in force where the cursor sits; ScopeOut is where
	// the token moves to when it is a scope link. The single most useful thing
	// to tell a new modder, and computed already for completion.
	Scope    string `json:"scope,omitempty"`
	ScopeOut string `json:"scopeOut,omitempty"`
	// Accepts states what this key's value may be — often knowable even where
	// nothing documents what the key means.
	Accepts           string       `json:"accepts,omitempty"`
	Body              string       `json:"body,omitempty"`
	Values            []HoverValue `json:"values,omitempty"`
	More              int          `json:"more,omitempty"`
	Owner             string       `json:"owner,omitempty"`
	Usage             string       `json:"usage,omitempty"`
	Origin            string       `json:"origin,omitempty"`
	OriginName        string       `json:"originName,omitempty"`
	Rel               string       `json:"rel,omitempty"`
	Path              string       `json:"path,omitempty"`
	Line              int          `json:"line,omitempty"`
	Col               int          `json:"col,omitempty"`
	VanillaOriginName string       `json:"vanillaOriginName,omitempty"`
	VanillaRel        string       `json:"vanillaRel,omitempty"`
	VanillaPath       string       `json:"vanillaPath,omitempty"`
	VanillaLine       int          `json:"vanillaLine,omitempty"`
	VanillaCol        int          `json:"vanillaCol,omitempty"`
}

// CompletionItem is one completion suggestion.
type CompletionItem struct {
	Label         string `json:"label"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	Kind          string `json:"kind,omitempty"` // property | value
	Range         Range  `json:"range,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// Location points at a definition or reference.
// Range is the key (F12 landing). TargetRange is the assignment block for Peek.
type Location struct {
	URI         string `json:"uri"`
	Range       Range  `json:"range"`
	TargetRange *Range `json:"targetRange,omitempty"`
}

// TextEdit is an LSP-style replacement.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit maps document URIs (absolute paths) to edits. Create lists
// files that should be created before the edits are applied.
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
	Create  []string              `json:"create,omitempty"`
}

// FoldingRange is a foldable line span (0-based, inclusive).
type FoldingRange struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// CodeAction is a quick-fix.
type CodeAction struct {
	Title string         `json:"title"`
	Edit  *WorkspaceEdit `json:"edit,omitempty"`
}

// SymbolInformation describes a document or workspace symbol.
type SymbolInformation struct {
	Name          string   `json:"name"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

const (
	sevError   = 1
	sevWarning = 2
)

// byteRange converts a UTF-8 byte span using a cached LineIndex.
func byteRange(li *jomini.LineIndex, start, end int) Range {
	a := li.PositionAt(start)
	b := li.PositionAt(end)
	return Range{
		Start: Position{Line: a.Line, Character: a.Character},
		End:   Position{Line: b.Line, Character: b.Character},
	}
}

func isMetaFile(s *session.Session, path string) bool {
	if s.KindFor(path) != "meta" {
		return false
	}
	l := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	return strings.Contains(l, "/.metadata/") && strings.HasSuffix(l, "/metadata.json")
}

func locDefined(s *session.Session, key string) bool {
	if _, ok := s.DefaultLoc(key); ok {
		return true
	}
	_, _, _, _, ok := s.LocSite(key)
	return ok
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func modRootOf(s *session.Session, path string) string {
	origin, _, ok := s.Locate(path)
	if !ok {
		return ""
	}
	for _, m := range s.Mods() {
		if m.Origin == origin {
			return m.Root
		}
	}
	return ""
}

func lowerPrefix(s, prefix string) bool {
	return prefix == "" || strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}
