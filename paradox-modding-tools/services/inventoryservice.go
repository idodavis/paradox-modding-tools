package services

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"

	util "paradox-modding-tools/services/internal"
	parser "paradox-modding-tools/services/internal/interpreter"
	ck3 "paradox-modding-tools/services/internal/interpreter/ck3-evaluator"
	walk "paradox-modding-tools/services/internal/interpreter/walk"

	"golang.org/x/sync/errgroup"
)

var maxConcurrency = max(1, runtime.GOMAXPROCS(0)/2)

// InventoryService exposes inventory functionality to the Wails frontend (supported types, schema, extraction, cancel).
type InventoryService struct{}

// InventoryItem represents a single extracted game object with metadata.
type InventoryItem struct {
	Key           string            `json:"key"`                  // Key is the unique identifier for this object (e.g., character ID, event name)
	Type          string            `json:"type"`                 // Type is the object type (e.g., "characters", "events", "traits")
	FilePath      string            `json:"filePath"`             // FilePath is the relative path to the file containing this object
	LineStart     int               `json:"lineStart"`            // LineStart is the line number where the object definition begins
	LineEnd       int               `json:"lineEnd"`              // LineEnd is the line number where the object definition ends
	RawText       string            `json:"rawText"`              // RawText contains the original text of the object definition
	PotentialRefs []string          `json:"-"`                    // PotentialRefs are identifiers found in this object's AST during extraction, not serialized to JSON
	References    []ObjectReference `json:"references,omitempty"` // References contains resolved references to other objects
	Referrers     []ObjectReference `json:"referrers,omitempty"`  // Referrers contains references to this object from other objects
	Attributes    map[string]bool   `json:"attributes,omitempty"` // Attributes lists attributes in the object body and whether they are present
}

// ObjectReference represents a reference between two game objects
type ObjectReference struct {
	Key       string `json:"key"`                 // Key is the key of the referenced object
	Type      string `json:"type"`                // Type is the type of the referenced object
	FilePath  string `json:"filePath"`            // FilePath is the file where the reference was found
	LineStart int    `json:"lineStart,omitempty"` // LineStart is the line number where the object definition begins (only for referrers)
	LineEnd   int    `json:"lineEnd,omitempty"`   // LineEnd is the line number where the object definition ends (only for referrers)
}

// applicableTypesForFile returns object types that apply to relPath for the given game,
// intersected with the requested types. For CK3 uses ck3.ApplicableTypesForPath; other games return nil.
func applicableTypesForFile(game, relPath string, objectTypes []string) []string {
	if game != "CK3" {
		return nil
	}
	pathTypes := ck3.ApplicableTypesForPath(relPath)
	return util.IntersectStrings(pathTypes, objectTypes)
}

// GetSupportedTypes returns the sorted list of object type names for the given game.
func (i *InventoryService) GetSupportedTypes(game string) ([]string, error) {
	if game != "CK3" {
		return nil, nil
	}
	names := ck3.GetSchemaNames()
	sort.Strings(names)
	return names, nil
}

// GetAttributes returns the list of attribute names for an object type and game.
func (i *InventoryService) GetAttributes(game, typeName string) ([]string, error) {
	if game != "CK3" {
		return nil, nil
	}
	schema, ok := ck3.GetSchema(typeName)
	if !ok {
		return nil, nil
	}
	return schema.Attributes, nil
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
// Only top-level (depth 0) expressions or inlineTypes are emitted as valid objects
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
	if inline && !slices.Contains(v.inlineTypes, typeName) {
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

// processFile walks the AST with the interpreter (walk + CK3 classification) and returns inventory items.
func processFile(path string, game string, schemaPath string, objectTypes []string) ([]InventoryItem, error) {
	applicableTypes := applicableTypesForFile(game, schemaPath, objectTypes)
	if len(applicableTypes) == 0 {
		return nil, nil
	}

	ast, err := parser.ParseFile(path)
	if err != nil {
		return nil, err
	}

	inlineTypes := ck3.InlineTypesFor(applicableTypes)
	seen := make(map[string]map[string]bool)
	for _, t := range applicableTypes {
		seen[t] = make(map[string]bool)
	}

	var items []InventoryItem
	v := &extractVisitor{
		filePath:    path,
		objectTypes: applicableTypes,
		inlineTypes: inlineTypes,
		seen:        seen,
		onItem:      func(it InventoryItem) { items = append(items, it) },
	}
	walk.Walk(ast, v)

	return items, nil
}

// enrichWithReferences resolves PotentialRefs and adds reference information to all inventory items.
// References are stored on the SOURCE item (the item that contains the reference).
// Referrers are stored on the TARGET item (the item that is referenced).
func enrichWithReferences(ctx context.Context, items []InventoryItem) {
	if ctx.Err() != nil {
		return
	}

	itemIndex := make(map[string]int)
	for i := range items {
		itemIndex[items[i].Key] = i
	}

	// Just using index as I don't want to copy and only modify in place
	for index := range items {
		for _, potentialRef := range items[index].PotentialRefs {
			items[index].References = append(items[index].References, ObjectReference{
				Key:       potentialRef,
				Type:      items[index].Type,
				FilePath:  items[index].FilePath,
				LineStart: items[index].LineStart,
				LineEnd:   items[index].LineEnd,
			})
			if j, ok := itemIndex[potentialRef]; ok {
				items[j].Referrers = append(items[j].Referrers, ObjectReference{
					Key:       items[index].Key,
					Type:      items[index].Type,
					FilePath:  items[index].FilePath,
					LineStart: items[index].LineStart,
					LineEnd:   items[index].LineEnd,
				})
			}
		}
	}
}

// ExtractInventory extracts inventory items from the given basePaths that match the given objectTypes.
// Only .txt files are processed. Returns items and parse errors or a fatal error if cancelled or too many parse failures.
func (i *InventoryService) ExtractInventory(ctx context.Context, game string, basePaths []string, objectTypes []string) ([]InventoryItem, error) {
	semaphore := make(chan struct{}, maxConcurrency)

	eg, egCtx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var items []InventoryItem

	for _, basePath := range basePaths {
		walkErr := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			path, basePath := path, basePath
			eg.Go(func() error {
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				if filepath.Ext(path) != ".txt" {
					return nil
				}

				schemaPath, relErr := filepath.Rel(basePath, path)
				if relErr != nil {
					schemaPath = path
				}

				if egCtx.Err() != nil {
					return egCtx.Err()
				}

				itemsForFile, err := processFile(path, game, schemaPath, objectTypes)
				if err != nil {
					return err
				}

				mu.Lock()
				items = append(items, itemsForFile...)
				mu.Unlock()
				return nil
			})
			return nil
		})
		if err := eg.Wait(); err != nil {
			return nil, err
		}
		if walkErr != nil && walkErr != fs.SkipAll {
			return nil, fmt.Errorf("walk %s: %w", basePath, walkErr)
		}
	}

	enrichWithReferences(ctx, items)
	return items, nil
}
