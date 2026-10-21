package lsp

// A Fix is one quick fix offered at a problem: its title and the edits that apply it, by file path.
type Fix struct {
	Title string            `json:"title"`
	Code  string            `json:"code"` // the diagnostic it fixes
	Edits map[string][]Edit `json:"edits"`
}

// Format formats a file and returns the edits that turn the old text into the new. It keeps the file's own
// indentation, or uses the editor's settings when the file has none.
func (lsp *Service) Format(path string, tabSize int, insertSpaces bool) ([]Edit, error) {
	panic("stub")
}

// ListFixes returns the quick fixes for the problems inside a range of a file, one per problem that has a fix.
func (lsp *Service) ListFixes(path string, within Range) ([]Fix, error) { panic("stub") }
