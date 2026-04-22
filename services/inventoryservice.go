package services

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	internal "paradox-modding-tools/services/internal"
	parser "paradox-modding-tools/services/internal/interpreter"
	ck3 "paradox-modding-tools/services/internal/interpreter/ck3-evaluator"
	walk "paradox-modding-tools/services/internal/interpreter/walk"
	"paradox-modding-tools/services/internal/repos"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"golang.org/x/sync/errgroup"
)

var blackListPaths = []string{
	"content_source",
	"common/console_groups",
	"fonts",
	"gfx",
	"licenses",
	"sound",
	"tests",
	"tools",
	"map_data",
	"gui",
	"reader_export",
	"dlc",
	"dlc_metadata",
	"data_binding",
}

var maxConcurrency = max(1, int(float64(runtime.GOMAXPROCS(0))*0.80))

// InventoryService exposes inventory functionality to the Wails frontend (supported types, schema, extraction, save, list).
type InventoryService struct {
	DB   *sqlx.DB
	repo *repos.InventoryRepository
}

type (
	InventorySummary = repos.InventorySummary
	InventoryItemRow = repos.InventoryItemRow
	ItemDetails      = repos.ItemDetails
)

func (i *InventoryService) getRepo() *repos.InventoryRepository {
	if i.repo == nil {
		i.repo = repos.NewInventoryRepository(i.DB)
	}
	return i.repo
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
	onItem      func(repos.InventoryItem)
}

// VisitExpression classifies top-level and inline keys with the CK3 evaluator and emits inventory items.
func (v *extractVisitor) VisitExpression(expr *parser.Expression, ctx *walk.Context) {
	if expr == nil || expr.Key == "" {
		return
	}
	inline := ctx.Depth > 0
	hasObject := expr.Object != nil

	// Optimization: Try to classify based on Key first to avoid expensive TopLevelKeys allocation.
	typeName, displayKey, ok := ck3.ClassifyKey(expr.Key, hasObject, nil, v.objectTypes, inline)

	var attrs map[string]bool
	if ok && hasObject {
		attrs = walk.TopLevelKeys(expr.Object)
		// Re-classify to ensure attrs don't disqualify it (or if they refine the type).
		typeName, displayKey, ok = ck3.ClassifyKey(expr.Key, hasObject, attrs, v.objectTypes, inline)
	}
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

	v.onItem(repos.InventoryItem{
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

// processFile walks the AST with the interpreter and returns inventory items.
func processFile(path string, game string, objectTypes []string) ([]repos.InventoryItem, error) {
	if game != "CK3" || len(objectTypes) == 0 {
		return nil, nil
	}

	// Filter to schemas whose paths match this file (prevents multiple object matches or false positives).
	// When path matches no schema (e.g. custom export folder), fall back to all objectTypes.
	// Inline types (scripted_trigger, scripted_effect) can appear nested in any file - always include when selected.
	pathApplicable := ck3.ApplicableTypesForPath(path)
	applicableTypes := internal.IntersectStrings(objectTypes, pathApplicable)
	if len(applicableTypes) == 0 {
		applicableTypes = objectTypes
	}
	inlineTypes := ck3.InlineTypesFor(objectTypes)
	for _, t := range inlineTypes {
		if !slices.Contains(applicableTypes, t) {
			applicableTypes = append(applicableTypes, t)
		}
	}

	ast, err := parser.ParseFile(path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]map[string]bool)
	for _, t := range applicableTypes {
		seen[t] = make(map[string]bool)
	}

	var items []repos.InventoryItem
	v := &extractVisitor{
		filePath:    path,
		objectTypes: applicableTypes,
		inlineTypes: inlineTypes,
		seen:        seen,
		onItem:      func(it repos.InventoryItem) { items = append(items, it) },
	}
	walk.Walk(ast, v)

	return items, nil
}

// enrichWithReferences resolves PotentialRefs and adds reference information to all inventory items.
func enrichWithReferences(ctx context.Context, items []repos.InventoryItem) {
	if ctx.Err() != nil {
		return
	}

	itemIndex := make(map[string]int)
	for i := range items {
		itemIndex[items[i].Key] = i
	}

	for index := range items {
		for _, potentialRef := range items[index].PotentialRefs {
			items[index].References = append(items[index].References, repos.ObjectReference{
				Key:       potentialRef,
				Type:      items[index].Type,
				FilePath:  items[index].FilePath,
				LineStart: items[index].LineStart,
				LineEnd:   items[index].LineEnd,
			})
			if j, ok := itemIndex[potentialRef]; ok {
				items[j].Referrers = append(items[j].Referrers, repos.ObjectReference{
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

// ExtractInventory extracts inventory items from basePath that match objectTypes. Writes to DB as temporary; returns inventoryId and totalCount. Only .txt files are processed. Deletes prior temporary inventories before inserting.
func (i *InventoryService) ExtractInventory(ctx context.Context, game, basePath string, objectTypes []string) (*string, error) {
	// Wipe any previous temporary inventories to keep the DB clean
	_ = i.getRepo().DeleteTemporaryInventories()

	shouldSkip := func(path string) bool {
		rel, _ := filepath.Rel(basePath, path)
		return slices.Contains(blackListPaths, filepath.ToSlash(rel))
	}

	semaphore := make(chan struct{}, maxConcurrency)
	eg, egCtx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var items []repos.InventoryItem

	walkErr := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkip(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".txt" {
			return nil
		}

		filePath := path
		eg.Go(func() error {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if egCtx.Err() != nil {
				return egCtx.Err()
			}

			itemsForFile, err := processFile(filePath, game, objectTypes)
			if err != nil {
				log.Printf("inventory: skip (parse error) %s: %v", filePath, err)
				return nil
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

	enrichWithReferences(ctx, items)

	repo := i.getRepo()

	inventoryId := uuid.New().String()
	name := fmt.Sprintf("%s - %s", game, time.Now().Format("2006-01-02"))
	if err := repo.CreateInventory(inventoryId, name, game, basePath, objectTypes); err != nil {
		return nil, fmt.Errorf("insert inventory: %w", err)
	}

	if err := repo.SaveInventoryItems(inventoryId, items); err != nil {
		return nil, err
	}

	return &inventoryId, nil
}

// ListInventoriesForGame returns saved (non-temporary) inventories for the given game, ordered by created_at DESC.
func (i *InventoryService) ListInventoriesForGame(game string) ([]InventorySummary, error) {
	return i.getRepo().ListInventories(game)
}

// SaveInventory marks an inventory as saved (persists name and sets is_temporary=0).
func (i *InventoryService) SaveInventory(id, name string) error {
	return i.getRepo().SaveInventory(id, name)
}

// GetInventoryItems returns lightweight rows for the grid (no raw_text, references, referrers).
func (i *InventoryService) GetInventoryItems(inventoryId string) ([]InventoryItemRow, error) {
	return i.getRepo().GetInventoryItems(inventoryId)
}

// GetItemDetails returns full details for a single item (on row selection).
func (i *InventoryService) GetItemDetails(inventoryId, itemType, itemKey string) (*ItemDetails, error) {
	return i.getRepo().GetItemDetails(inventoryId, itemType, itemKey)
}

// RenameInventory updates the inventory name.
func (i *InventoryService) RenameInventory(id, newName string) error {
	return i.getRepo().RenameInventory(id, newName)
}

// DeleteInventory removes an inventory and its items (cascade).
func (i *InventoryService) DeleteInventory(id string) error {
	return i.getRepo().DeleteInventory(id)
}

// ServiceShutdown deletes temporary inventories. Called by Wails when the app exits.
func (i *InventoryService) ServiceShutdown() error {
	return i.getRepo().DeleteTemporaryInventories()
}
