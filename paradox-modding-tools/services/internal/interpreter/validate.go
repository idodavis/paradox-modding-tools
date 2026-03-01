package interpreter

// ValidationResult holds parse error info for a single file.
type ValidationResult struct {
	Path  string
	Line  int
	Error string
}

// ValidatePaths runs the Paradox parser on each path and returns parse errors.
func ValidatePaths(paths []string) []ValidationResult {
	var out []ValidationResult
	for _, p := range paths {
		_, err := ParseFile(p)
		if err != nil {
			line := 0
			if pe, ok := err.(interface{ Line() int }); ok {
				line = pe.Line()
			}
			out = append(out, ValidationResult{Path: p, Line: line, Error: err.Error()})
		}
	}
	return out
}
