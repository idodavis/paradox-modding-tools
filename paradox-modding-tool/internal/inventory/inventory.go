package inventory

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	parser "paradox-modding-tool/internal/interpreter"
	ck3 "paradox-modding-tool/internal/interpreter/ck3-evaluator"
	"paradox-modding-tool/internal/interpreter/walk"
)

// ErrExtractionCancelled is returned when the user cancels extraction.
var ErrExtractionCancelled = errors.New("extraction cancelled")

const maxParseErrorsBeforeAbort = 5

var cancelExtract atomic.Bool

// CancelExtraction signals any running ExtractInventory to stop and discard results.
// Call from the frontend when the user presses Cancel. The flag is reset when extraction exits.
func CancelExtraction() {
	cancelExtract.Store(true)
}

// applicableTypesForFile returns object types that apply to relPath for the given game,
// intersected with the requested types. For "ck3" uses ck3.ApplicableTypesForPath; other games return nil.
func applicableTypesForFile(game, relPath string, objectTypes []string) []string {
	if game != "ck3" {
		return nil
	}
	pathTypes := ck3.ApplicableTypesForPath(relPath)
	return intersectStrings(pathTypes, objectTypes)
}

func intersectStrings(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

func containsString(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

type extractVisitor struct {
	walk.NoopVisitor
	filePath    string
	objectTypes []string
	inlineTypes []string
	seen        map[string]map[string]bool
	onItem      func(InventoryItem)
}

// VisitExpression classifies top-level and inline keys with the CK3 evaluator and emits inventory items.
// Only top-level (depth 0) expressions or inlineTypes are emitted as events
func (v *extractVisitor) VisitExpression(expr *parser.Expression, ctx *walk.Context) {
	if expr == nil || expr.Key == "" {
		return
	}
	inline := ctx.Depth > 0
	hasObject := expr.Object != nil
	typeName, displayKey, ok := ck3.ClassifyKey(expr.Key, hasObject, v.objectTypes, inline)
	if !ok {
		return
	}
	if inline && !containsString(v.inlineTypes, typeName) {
		return
	}
	if v.seen[typeName][displayKey] {
		return
	}
	v.seen[typeName][displayKey] = true

	lineStart := 1
	if expr.Pos.Line > 0 {
		lineStart = expr.Pos.Line
	}
	raw := expr.GetRawText()
	lineEnd := walk.LineEnd(lineStart, raw)
	potentialRefs := walk.CollectIdentifiers(expr, displayKey)
	var attrs map[string]bool
	if expr.Object != nil {
		attrs = walk.TopLevelKeys(expr.Object)
	}

	v.onItem(InventoryItem{
		Key:           displayKey,
		Type:          typeName,
		FilePath:      v.filePath,
		LineStart:     lineStart,
		LineEnd:       lineEnd,
		RawText:       raw,
		PotentialRefs: potentialRefs,
		Attributes:    attrs,
	})
}

// ExtractFromFile walks the AST with the interpreter (walk + CK3 classification) and returns inventory items.
// filePath is the path used for schema matching (pass relative path or use baseDir to derive it).
// objectTypes: nil/empty = path-driven (ApplicableTypesForPath); otherwise only those types.
func ExtractFromFile(ast *parser.ParadoxFile, filePath string, game string, objectTypes []string) ([]InventoryItem, error) {
	if ast == nil {
		return nil, nil
	}

	types := objectTypes
	if len(types) == 0 {
		types = ck3.ApplicableTypesForPath(filePath)
	}
	if len(types) == 0 {
		return nil, nil
	}

	inlineTypes := ck3.InlineTypesFor(types)
	seen := make(map[string]map[string]bool)
	for _, t := range types {
		seen[t] = make(map[string]bool)
	}

	var items []InventoryItem
	v := &extractVisitor{
		filePath:    filePath,
		objectTypes: types,
		inlineTypes: inlineTypes,
		seen:        seen,
		onItem:      func(it InventoryItem) { items = append(items, it) },
	}
	walk.Walk(ast, v)
	return items, nil
}

// ExtractInventory extracts inventory items from the given basePaths that match the given objectTypes.
// Only .txt files are processed. Returns ExtractResult (items keyed by type + parse errors) or a fatal error if cancelled or too many parse failures.
func ExtractInventory(game string, basePaths []string, objectTypes []string) (*ExtractResult, error) {
	cancelExtract.Store(false)
	defer cancelExtract.Store(false)

	result := make(map[string][]InventoryItem)
	var parseErrors []string
	var parseErrorMu sync.Mutex

	for _, basePath := range basePaths {
		if cancelExtract.Load() {
			return nil, ErrExtractionCancelled
		}

		basePath := basePath
		walkErr := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				parseErrorMu.Lock()
				parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", path, err))
				n := len(parseErrors)
				parseErrorMu.Unlock()
				if n > maxParseErrorsBeforeAbort {
					return fs.SkipAll
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".txt" {
				return nil
			}

			if cancelExtract.Load() {
				return fs.SkipAll
			}

			relPath, err := filepath.Rel(basePath, path)
			if err != nil {
				relPath = path
			}
			relPath = filepath.ToSlash(relPath)
			// Schema paths are like "events", "history/characters". When user selects a subfolder
			// (e.g. .../events), relPath is just "foo.txt" and wouldn't match. Build a path that
			// includes the segment so we get "events/foo.txt" for schema matching.
			schemaPath := relPath
			if relPath == "." || relPath == "" {
				// Single file selected: path is the file; use parent dir name + filename.
				schemaPath = filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)))
			} else if !strings.Contains(relPath, "/") {
				// File directly under selected dir: prepend basePath's last component.
				schemaPath = filepath.ToSlash(filepath.Join(filepath.Base(basePath), relPath))
			}

			applicableTypes := applicableTypesForFile(game, schemaPath, objectTypes)
			if len(applicableTypes) == 0 {
				return nil
			}

			ast, err := parser.ParseFile(path)
			if err != nil {
				parseErrorMu.Lock()
				parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", path, err))
				n := len(parseErrors)
				parseErrorMu.Unlock()
				if n > maxParseErrorsBeforeAbort {
					return fs.SkipAll
				}
				return nil
			}

			items, _ := ExtractFromFile(ast, path, game, applicableTypes)
			for _, it := range items {
				result[it.Type] = append(result[it.Type], it)
			}
			return nil
		})

		if walkErr != nil && walkErr != fs.SkipAll {
			return &ExtractResult{nil, parseErrors}, fmt.Errorf("walk %s: %w", basePath, walkErr)
		}
		if cancelExtract.Load() {
			return nil, ErrExtractionCancelled
		}
		parseErrorMu.Lock()
		n := len(parseErrors)
		parseErrorMu.Unlock()
		if n > maxParseErrorsBeforeAbort {
			return &ExtractResult{nil, parseErrors}, fmt.Errorf("too many parse errors (%d); extraction aborted", n)
		}
	}

	if cancelExtract.Load() {
		return nil, ErrExtractionCancelled
	}

	EnrichAllWithReferences(result)
	return &ExtractResult{Items: result, Errors: parseErrors}, nil
}

// GetSupportedTypes returns the sorted list of object type names for the given game.
// For "ck3" returns CK3 schema type names; other games return nil.
func GetSupportedTypes(game string) ([]string, error) {
	if game != "ck3" {
		return nil, nil
	}
	names := ck3.GetSchemaNames()
	sort.Strings(names)
	return names, nil
}

// GetAttributes returns the list of attribute names for an object type and game.
// For "ck3" returns the schema's Attributes; other games or unknown types return nil.
func GetAttributes(game, typeName string) ([]string, error) {
	if game != "ck3" {
		return nil, nil
	}
	schema, ok := ck3.GetSchema(typeName)
	if !ok {
		return nil, nil
	}
	return schema.Attributes, nil
}
