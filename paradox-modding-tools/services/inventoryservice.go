package services

import (
	"context"
	"database/sql"
	"encoding/json"
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

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

var maxConcurrency = max(1, runtime.GOMAXPROCS(0))

// InventoryService exposes inventory functionality to the Wails frontend (supported types, schema, extraction, save, list).
type InventoryService struct {
	DB *sql.DB
}

// InventoryItem represents a single extracted game object with metadata.
type InventoryItem struct {
	Key           string            `json:"key"`
	Type          string            `json:"type"`
	FilePath      string            `json:"filePath"`
	LineStart     int               `json:"lineStart"`
	LineEnd       int               `json:"lineEnd"`
	RawText       string            `json:"rawText"`
	PotentialRefs []string          `json:"-"`
	References    []ObjectReference `json:"references,omitempty"`
	Referrers     []ObjectReference `json:"referrers,omitempty"`
	Attributes    map[string]bool   `json:"attributes,omitempty"`
}

// ObjectReference represents a reference between two game objects.
type ObjectReference struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	FilePath  string `json:"filePath"`
	LineStart int    `json:"lineStart,omitempty"`
	LineEnd   int    `json:"lineEnd,omitempty"`
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
func (v *extractVisitor) VisitExpression(expr *parser.Expression, ctx *walk.Context) {
	if expr == nil || expr.Key == "" {
		return
	}
	inline := ctx.Depth > 0
	hasObject := expr.Object != nil
	var attrs map[string]bool
	if expr.Object != nil {
		attrs = walk.TopLevelKeys(expr.Object)
	}
	typeName, displayKey, ok := ck3.ClassifyKey(expr.Key, hasObject, attrs, v.objectTypes, inline)
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

// processFile walks the AST with the interpreter and returns inventory items.
func processFile(path string, game string, objectTypes []string) ([]InventoryItem, error) {
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
func enrichWithReferences(ctx context.Context, items []InventoryItem) {
	if ctx.Err() != nil {
		return
	}

	itemIndex := make(map[string]int)
	for i := range items {
		itemIndex[items[i].Key] = i
	}

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

// ExtractInventoryResult is returned by ExtractInventory after saving to DB.
type ExtractInventoryResult struct {
	InventoryId string `json:"inventoryId"`
	TotalCount  int    `json:"totalCount"`
}

// InventorySummary is a lightweight inventory listing (for ListInventories).
type InventorySummary struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Game       string `json:"game"`
	CreatedAt  string `json:"createdAt"`
	TotalCount int    `json:"totalCount"`
}

// InventoryItemRow is a lightweight grid row (key, type, file_path, lines, counts).
type InventoryItemRow struct {
	Key             string `json:"key"`
	Type            string `json:"type"`
	FilePath        string `json:"filePath"`
	LineStart       int    `json:"lineStart"`
	LineEnd         int    `json:"lineEnd"`
	ReferencesCount int    `json:"referencesCount"`
	ReferrersCount  int    `json:"referrersCount"`
}

// ItemDetails is the full details for a single item (for GetItemDetails).
type ItemDetails struct {
	RawText    string            `json:"rawText"`
	References []ObjectReference `json:"references"`
	Referrers  []ObjectReference `json:"referrers"`
	Attributes map[string]bool   `json:"attributes"`
}

// ExtractInventory extracts inventory items from basePaths that match objectTypes. Writes to DB as temporary; returns inventoryId and totalCount. Only .txt files are processed. Deletes prior temporary inventories before inserting.
func (i *InventoryService) ExtractInventory(ctx context.Context, game string, basePaths []string, objectTypes []string) (*ExtractInventoryResult, error) {
	if i.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	semaphore := make(chan struct{}, maxConcurrency)

	eg, egCtx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var items []InventoryItem

	for _, basePath := range basePaths {
		walkErr := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			filePath := path
			eg.Go(func() error {
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				if filepath.Ext(path) != ".txt" {
					return nil
				}

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
	}

	enrichWithReferences(ctx, items)

	_, _ = i.DB.Exec(`DELETE FROM inventories WHERE is_temporary = 1`)

	inventoryId := uuid.New().String()
	createdAt := time.Now().Format("2006-01-02")
	basePathsJSON, _ := json.Marshal(basePaths)
	objectTypesJSON, _ := json.Marshal(objectTypes)
	name := fmt.Sprintf("%s - %s", game, createdAt)

	_, err := i.DB.Exec(`INSERT INTO inventories (id, name, game, base_paths, object_types, created_at, is_temporary) VALUES (?, ?, ?, ?, ?, ?, 1)`,
		inventoryId, name, game, string(basePathsJSON), string(objectTypesJSON), createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert inventory: %w", err)
	}

	batchSize := 1000
	for b := 0; b < len(items); b += batchSize {
		end := b + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[b:end]
		tx, err := i.DB.Begin()
		if err != nil {
			return nil, err
		}
		stmt, err := tx.Prepare(`INSERT INTO inventory_items (inventory_id, key, type, file_path, line_start, line_end, raw_text, "references", referrers, attributes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(inventory_id, type, key) DO UPDATE SET
			file_path = excluded.file_path, line_start = excluded.line_start, line_end = excluded.line_end,
			raw_text = excluded.raw_text, "references" = excluded."references", referrers = excluded.referrers, attributes = excluded.attributes`)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		for _, it := range batch {
			refsJSON, _ := json.Marshal(it.References)
			referrersJSON, _ := json.Marshal(it.Referrers)
			attrsJSON, _ := json.Marshal(it.Attributes)
			_, err = stmt.Exec(inventoryId, it.Key, it.Type, it.FilePath, it.LineStart, it.LineEnd, it.RawText, string(refsJSON), string(referrersJSON), string(attrsJSON))
			if err != nil {
				stmt.Close()
				tx.Rollback()
				return nil, err
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}

	return &ExtractInventoryResult{InventoryId: inventoryId, TotalCount: len(items)}, nil
}

// ListInventoriesForGame returns saved (non-temporary) inventories for the given game, ordered by created_at DESC.
func (i *InventoryService) ListInventoriesForGame(game string) ([]InventorySummary, error) {
	if i.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := i.DB.Query(`SELECT inv.id, inv.name, inv.game, inv.created_at,
		(SELECT COUNT(*) FROM inventory_items WHERE inventory_id = inv.id) AS total_count
		FROM inventories inv WHERE inv.game = ? AND (inv.is_temporary = 0 OR inv.is_temporary IS NULL) ORDER BY inv.created_at DESC`, game)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InventorySummary
	for rows.Next() {
		var s InventorySummary
		if err := rows.Scan(&s.Id, &s.Name, &s.Game, &s.CreatedAt, &s.TotalCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveInventory marks an inventory as saved (persists name and sets is_temporary=0).
func (i *InventoryService) SaveInventory(id, name string) error {
	if i.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := i.DB.Exec(`UPDATE inventories SET name = ?, is_temporary = 0 WHERE id = ?`, name, id)
	return err
}

// GetInventoryItems returns lightweight rows for the grid (no raw_text, references, referrers).
func (i *InventoryService) GetInventoryItems(inventoryId string) ([]InventoryItemRow, error) {
	if i.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := i.DB.Query(`SELECT key, type, file_path, line_start, line_end,
		COALESCE(json_array_length("references"), 0), COALESCE(json_array_length(referrers), 0)
		FROM inventory_items WHERE inventory_id = ? ORDER BY type, key`, inventoryId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InventoryItemRow
	for rows.Next() {
		var r InventoryItemRow
		if err := rows.Scan(&r.Key, &r.Type, &r.FilePath, &r.LineStart, &r.LineEnd, &r.ReferencesCount, &r.ReferrersCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetItemDetails returns full details for a single item (on row selection).
func (i *InventoryService) GetItemDetails(inventoryId, itemType, itemKey string) (*ItemDetails, error) {
	if i.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	var rawText, refsJSON, referrersJSON, attrsJSON sql.NullString
	err := i.DB.QueryRow(`SELECT raw_text, "references", referrers, attributes FROM inventory_items WHERE inventory_id = ? AND type = ? AND key = ?`,
		inventoryId, itemType, itemKey).Scan(&rawText, &refsJSON, &referrersJSON, &attrsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &ItemDetails{RawText: rawText.String}
	if refsJSON.Valid && refsJSON.String != "" {
		json.Unmarshal([]byte(refsJSON.String), &out.References)
	}
	if referrersJSON.Valid && referrersJSON.String != "" {
		json.Unmarshal([]byte(referrersJSON.String), &out.Referrers)
	}
	if attrsJSON.Valid && attrsJSON.String != "" {
		json.Unmarshal([]byte(attrsJSON.String), &out.Attributes)
	}
	if out.Attributes == nil {
		out.Attributes = make(map[string]bool)
	}
	return out, nil
}

// RenameInventory updates the inventory name.
func (i *InventoryService) RenameInventory(id, newName string) error {
	if i.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := i.DB.Exec(`UPDATE inventories SET name = ? WHERE id = ?`, newName, id)
	return err
}

// DeleteInventory removes an inventory and its items (cascade).
func (i *InventoryService) DeleteInventory(id string) error {
	if i.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := i.DB.Exec(`DELETE FROM inventories WHERE id = ?`, id)
	return err
}

// ServiceShutdown deletes temporary inventories. Called by Wails when the app exits.
func (i *InventoryService) ServiceShutdown() error {
	if i.DB != nil {
		_, err := i.DB.Exec(`DELETE FROM inventories WHERE is_temporary = 1`)
		if err != nil {
			return fmt.Errorf("failed to delete temporary inventories: %w", err)
		}
	}
	return nil
}
