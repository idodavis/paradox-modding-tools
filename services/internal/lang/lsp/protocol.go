// protocol.go holds the LSP-shaped DTOs the Wails services return and the path
// helpers every request shares.
package lsp

import (
	"os"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
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

// InspectValue is one value the corpus was seen writing for the name under the cursor.
type InspectValue struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// Inspect is the hover card, and the DTO the Hover RPC returns; the frontend
// templates it as it stands. A nil Inspect means there is no card to show.
type Inspect struct {
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
	Accepts           string         `json:"accepts,omitempty"`
	Body              string         `json:"body,omitempty"`
	Values            []InspectValue `json:"values,omitempty"`
	More              int            `json:"more,omitempty"`
	Owner             string         `json:"owner,omitempty"`
	Usage             string         `json:"usage,omitempty"`
	Origin            string         `json:"origin,omitempty"`
	OriginName        string         `json:"originName,omitempty"`
	Rel               string         `json:"rel,omitempty"`
	Path              string         `json:"path,omitempty"`
	Line              int            `json:"line,omitempty"`
	Col               int            `json:"col,omitempty"`
	VanillaOriginName string         `json:"vanillaOriginName,omitempty"`
	VanillaRel        string         `json:"vanillaRel,omitempty"`
	VanillaPath       string         `json:"vanillaPath,omitempty"`
	VanillaLine       int            `json:"vanillaLine,omitempty"`
	VanillaCol        int            `json:"vanillaCol,omitempty"`
}

// CompletionItem is one completion suggestion. Documentation is empty here and
// fetched lazily through IdeService.ResolveCompletion, not computed for every
// item up front.
type CompletionItem struct {
	Label         string `json:"label"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	Kind          string `json:"kind,omitempty"` // property | value
	Range         Range  `json:"range"`
	InsertText    string `json:"insertText,omitempty"`
	// SortText places the item in one of the completion buckets: "0" a declared
	// token in scope, "1" a structure key, "2" a def, "3" a loc key.
	SortText string `json:"sortText,omitempty"`
	// DocKind is the kind ResolveCompletion should look documentation up under,
	// when it differs from Detail (a structure key shows its enclosing block as
	// Detail but its doc lives under the file's own kind).
	DocKind string `json:"docKind,omitempty"`
}

// CompletionList is the completion response: the items, and whether the cap
// truncated them (the editor should ask again as the reader keeps typing).
type CompletionList struct {
	Items        []CompletionItem `json:"items"`
	IsIncomplete bool             `json:"isIncomplete,omitempty"`
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func modRootOf(s *session.Session, path string) string {
	origin, _, ok := s.Locate(path)
	if !ok {
		return ""
	}
	for _, m := range s.Mods {
		if m.Origin == origin {
			return m.Root
		}
	}
	return ""
}

func lowerPrefix(s, prefix string) bool {
	return prefix == "" || strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}

// SignatureHelpResult is the Monaco signature popup.
type SignatureHelpResult struct {
	Label           string   `json:"label"`
	Documentation   string   `json:"documentation,omitempty"`
	Parameters      []string `json:"parameters,omitempty"`
	ActiveParameter int      `json:"activeParameter"`
}

// InlayHint is text the editor draws inline at Range.End without editing the buffer; columns are UTF-8 bytes like every other range PMT returns.
type InlayHint struct {
	Range   Range  `json:"range"`
	Label   string `json:"label"`
	Tooltip string `json:"tooltip,omitempty"`
}
