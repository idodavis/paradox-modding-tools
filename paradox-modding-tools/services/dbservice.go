package services

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	appConfigDirName = "Paradox Modding Tools"
	dbFileName       = "pmt.db"
)

// DbService manages the SQLite database.
type DbService struct {
	DB *sql.DB
}

// ServiceShutdown closes the database connection. Called by Wails when the app exits.
func (d *DbService) ServiceShutdown() error {
	if d.DB != nil {
		_, _ = d.DB.Exec("VACUUM")
		return d.DB.Close()
	}
	return nil
}

// ServiceStartup opens the database and initializes the schema. Call from main before creating services that need DB.
func (d *DbService) ServiceStartup() error {
	if d.DB != nil {
		return nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("user config dir: %w", err)
	}
	appDir := filepath.Join(configDir, appConfigDirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	dbPath := filepath.Join(appDir, dbFileName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	d.DB = db

	// Enable foreign keys so ON DELETE CASCADE works
	if _, err := d.DB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	return d.initSchema()
}

func (d *DbService) initSchema() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS inventories (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			game TEXT NOT NULL,
			base_paths TEXT NOT NULL,
			object_types TEXT NOT NULL,
			created_at TEXT NOT NULL,
			is_temporary INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS inventory_items (
			id INTEGER PRIMARY KEY,
			inventory_id TEXT NOT NULL REFERENCES inventories(id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			type TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line_start INTEGER NOT NULL,
			line_end INTEGER NOT NULL,
			raw_text TEXT,
			"references" TEXT,
			referrers TEXT,
			attributes TEXT,
			UNIQUE(inventory_id, type, key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inv_items_lookup ON inventory_items(inventory_id, type)`,
		`CREATE TABLE IF NOT EXISTS doc_files (
			game TEXT NOT NULL,
			install_path_hash TEXT NOT NULL,
			rel_path TEXT NOT NULL,
			abs_path TEXT,
			content TEXT NOT NULL,
			fetched_at TEXT NOT NULL,
			PRIMARY KEY (game, install_path_hash, rel_path)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_doc_files_paths ON doc_files(game, install_path_hash)`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			game TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			PRIMARY KEY (game, key)
		)`,
		`CREATE TABLE IF NOT EXISTS patchnotes (
			game TEXT PRIMARY KEY,
			fetched_at TEXT NOT NULL,
			title TEXT NOT NULL,
			contents TEXT NOT NULL,
			steam_url TEXT,
			steamdb_url TEXT NOT NULL
		)`,
	}
	for _, m := range migrations {
		if _, err := d.DB.Exec(m); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}
	return d.seedGameConstants()
}

func (d *DbService) seedGameConstants() error {
	seeds := []struct {
		game string
		key  string
		val  string
	}{
		{"ck3", "ck3_steamAppId", "1158310"},
		{"ck3", "ck3_wikiUrl", "https://ck3.paradoxwikis.com/Modding"},
		{"ck3", "ck3_scriptRootFolder", "game"},
		{"ck3", "ck3_docFileName", ".info"},
		{"eu5", "eu5_steamAppId", "3450310"},
		{"eu5", "eu5_wikiUrl", "https://eu5.paradoxwikis.com/Modding"},
		{"eu5", "eu5_scriptRootFolder", "game/in_game"},
		{"eu5", "eu5_docFileName", "readme.txt"},
	}
	for _, s := range seeds {
		_, err := d.DB.Exec(`INSERT OR IGNORE INTO app_settings (game, key, value) VALUES (?, ?, ?)`, s.game, s.key, s.val)
		if err != nil {
			return fmt.Errorf("seed %s/%s: %w", s.game, s.key, err)
		}
	}
	return nil
}

// ResetData wipes all user data (inventories, doc cache, patchnotes) but preserves app_settings. Re-seeds game constants.
func (d *DbService) ResetData() error {
	if d.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := d.DB.Exec(`DELETE FROM inventory_items`)
	if err != nil {
		return fmt.Errorf("delete inventory_items: %w", err)
	}
	_, err = d.DB.Exec(`DELETE FROM inventories`)
	if err != nil {
		return fmt.Errorf("delete inventories: %w", err)
	}
	_, err = d.DB.Exec(`DELETE FROM doc_files`)
	if err != nil {
		return fmt.Errorf("delete doc_files: %w", err)
	}
	_, err = d.DB.Exec(`DELETE FROM patchnotes`)
	if err != nil {
		return fmt.Errorf("delete patchnotes: %w", err)
	}
	return d.seedGameConstants()
}

// installPathHash returns a SHA256 hex hash of the install path for use as install_path_hash.
func installPathHash(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:])
}
