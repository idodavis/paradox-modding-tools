package lsp

import (
	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Code is one kind of problem: its id, its severity, the Health section it is listed under, and its message,
// which is a template filled from a diagnostic's facts.
type Code struct {
	ID       string `json:"id"`       // kebab: unresolved-reference, duplicate-definition, wrong-scope …
	Severity int    `json:"severity"` // 1 error, 2 warning, 3 information, 4 hint
	Section  string `json:"section"`  // the Health section: dangling, localization, script
	Label    string `json:"label"`    // the code's title on Health
	Message  string `json:"message"`  // with {name}, {database}, {scope}, {expected} filled from the facts
}

// Codes is the one table of every code, the parser's and localization's included.
var Codes = []Code{}

// A Diagnostic is one problem found in a file: its code, where it is, and the facts its message is written from.
type Diagnostic struct {
	Code  string            `json:"code"`
	Range Range             `json:"range"`
	Facts map[string]string `json:"facts"`
}

// Diagnose returns a file's problems: its parse errors and every verdict that fails on it, judged against the
// index for what other files say. It reads the open text if the editor has the file, else the file on disk.
func (lsp *Service) Diagnose(path string) ([]Diagnostic, error) { panic("stub") }

// A diagnoseVisitor is the visitor behind Diagnose. At each statement it judges the five verdicts the context
// allows and records each one that fails.
type diagnoseVisitor struct {
	ws          *workspace.Session
	file        files.File
	diagnostics []Diagnostic
}

// Enter judges the statement by the five verdicts the context allows and records each failure.
func (visitor *diagnoseVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	panic("stub")
}

// Leave judges what the block as a whole had to contain: the keys its definition must write.
func (visitor *diagnoseVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	panic("stub")
}
