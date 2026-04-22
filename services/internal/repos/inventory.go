package repos

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

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

// InventorySummary is a lightweight inventory listing.
type InventorySummary struct {
	Id         string `json:"id" db:"id"`
	Name       string `json:"name" db:"name"`
	Game       string `json:"game" db:"game"`
	CreatedAt  string `json:"createdAt" db:"created_at"`
	TotalCount int    `json:"totalCount" db:"total_count"`
}

// InventoryItemRow is a lightweight grid row.
type InventoryItemRow struct {
	Key             string `json:"key" db:"key"`
	Type            string `json:"type" db:"type"`
	FilePath        string `json:"filePath" db:"file_path"`
	LineStart       int    `json:"lineStart" db:"line_start"`
	LineEnd         int    `json:"lineEnd" db:"line_end"`
	ReferencesCount int    `json:"referencesCount" db:"references_count"`
	ReferrersCount  int    `json:"referrersCount" db:"referrers_count"`
}

// ItemDetails is the full details for a single item.
type ItemDetails struct {
	RawText    string            `json:"rawText"`
	References []ObjectReference `json:"references"`
	Referrers  []ObjectReference `json:"referrers"`
	Attributes map[string]bool   `json:"attributes"`
}

type InventoryRepository struct {
	db *sqlx.DB
}

func NewInventoryRepository(db *sqlx.DB) *InventoryRepository {
	return &InventoryRepository{db: db}
}

func (r *InventoryRepository) DeleteTemporaryInventories() error {
	// Manual cascade to ensure items are deleted regardless of FK settings
	_, _ = r.db.Exec(`DELETE FROM inventory_items WHERE inventory_id IN (SELECT id FROM inventories WHERE is_temporary = 1)`)
	_, err := r.db.Exec(`DELETE FROM inventories WHERE is_temporary = 1`)
	return err
}

func (r *InventoryRepository) CreateInventory(id, name, game, basePath string, objectTypes []string) error {
	createdAt := time.Now().Format("2006-01-02")
	objectTypesJSON, _ := json.Marshal(objectTypes)

	_, err := r.db.Exec(`INSERT INTO inventories (id, name, game, base_path, object_types, created_at, is_temporary) VALUES (?, ?, ?, ?, ?, ?, 1)`,
		id, name, game, basePath, string(objectTypesJSON), createdAt)
	return err
}

func (r *InventoryRepository) SaveInventoryItems(inventoryId string, items []InventoryItem) error {
	const batchSize = 1000

	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for b := 0; b < len(items); b += batchSize {
		end := b + batchSize
		if end > len(items) {
			end = len(items)
		}

		batch := items[b:end]
		valueStrings := make([]string, 0, len(batch))
		valueArgs := make([]interface{}, 0, len(batch)*10)

		for _, it := range items[b:end] {
			refsJSON, _ := json.Marshal(it.References)
			referrersJSON, _ := json.Marshal(it.Referrers)
			attrsJSON, _ := json.Marshal(it.Attributes)

			valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			valueArgs = append(valueArgs,
				inventoryId,
				it.Key,
				it.Type,
				it.FilePath,
				it.LineStart,
				it.LineEnd,
				it.RawText,
				string(refsJSON),
				string(referrersJSON),
				string(attrsJSON),
			)
		}

		query := `INSERT INTO inventory_items (inventory_id, key, type, file_path, line_start, line_end, raw_text, "references", referrers, attributes) VALUES ` +
			strings.Join(valueStrings, ",") +
			` ON CONFLICT(inventory_id, type, key) DO UPDATE SET
			file_path = excluded.file_path, line_start = excluded.line_start, line_end = excluded.line_end,
			raw_text = excluded.raw_text, "references" = excluded."references", referrers = excluded.referrers, attributes = excluded.attributes`

		_, err = tx.Exec(query, valueArgs...)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *InventoryRepository) ListInventories(game string) ([]InventorySummary, error) {
	var out []InventorySummary
	err := r.db.Select(&out, `SELECT inv.id, inv.name, inv.game, inv.created_at,
		(SELECT COUNT(*) FROM inventory_items WHERE inventory_id = inv.id) AS total_count
		FROM inventories inv WHERE inv.game = ? AND (inv.is_temporary = 0 OR inv.is_temporary IS NULL) ORDER BY inv.created_at DESC`, game)
	return out, err
}

func (r *InventoryRepository) SaveInventory(id, name string) error {
	_, err := r.db.Exec(`UPDATE inventories SET name = ?, is_temporary = 0 WHERE id = ?`, name, id)
	return err
}

func (r *InventoryRepository) GetInventoryItems(inventoryId string) ([]InventoryItemRow, error) {
	var out []InventoryItemRow
	err := r.db.Select(&out, `SELECT key, type, file_path, line_start, line_end,
		COALESCE(json_array_length("references"), 0) AS references_count, 
		COALESCE(json_array_length(referrers), 0) AS referrers_count
		FROM inventory_items WHERE inventory_id = ? ORDER BY type, key`, inventoryId)
	return out, err
}

func (r *InventoryRepository) GetItemDetails(inventoryId, itemType, itemKey string) (*ItemDetails, error) {
	var row struct {
		RawText    string `db:"raw_text"`
		References string `db:"references"`
		Referrers  string `db:"referrers"`
		Attributes string `db:"attributes"`
	}
	err := r.db.Get(&row, `SELECT raw_text, "references", referrers, attributes FROM inventory_items WHERE inventory_id = ? AND type = ? AND key = ?`,
		inventoryId, itemType, itemKey)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := &ItemDetails{RawText: row.RawText}
	// Ignore errors on unmarshal, fields will just be empty
	_ = json.Unmarshal([]byte(row.References), &out.References)
	_ = json.Unmarshal([]byte(row.Referrers), &out.Referrers)
	_ = json.Unmarshal([]byte(row.Attributes), &out.Attributes)

	if out.Attributes == nil {
		out.Attributes = make(map[string]bool)
	}
	return out, nil
}

func (r *InventoryRepository) RenameInventory(id, newName string) error {
	_, err := r.db.Exec(`UPDATE inventories SET name = ? WHERE id = ?`, newName, id)
	return err
}

func (r *InventoryRepository) DeleteInventory(id string) error {
	// Manual cascade to ensure items are deleted regardless of FK settings
	_, _ = r.db.Exec(`DELETE FROM inventory_items WHERE inventory_id = ?`, id)
	_, err := r.db.Exec(`DELETE FROM inventories WHERE id = ?`, id)
	return err
}
