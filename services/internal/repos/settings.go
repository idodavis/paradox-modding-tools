package repos

import (
	"github.com/jmoiron/sqlx"
)

type Setting struct {
	Game  string `db:"game"`
	Key   string `db:"key"`
	Value string `db:"value"`
}

type SettingsRepository struct {
	db *sqlx.DB
}

func NewSettingsRepository(db *sqlx.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) GetAllSettings() ([]Setting, error) {
	var settings []Setting
	err := r.db.Select(&settings, `SELECT game, key, value FROM app_settings`)
	return settings, err
}

func (r *SettingsRepository) UpsertSetting(game, key, value string) error {
	_, err := r.db.Exec(`INSERT INTO app_settings (game, key, value) VALUES (?, ?, ?) ON CONFLICT(game, key) DO UPDATE SET value = excluded.value`, game, key, value)
	return err
}

func (r *SettingsRepository) GetMergePresets() ([]Setting, error) {
	var settings []Setting
	err := r.db.Select(&settings, `SELECT key, value FROM app_settings WHERE game = '_global' AND key LIKE 'merge_preset_%'`)
	return settings, err
}

func (r *SettingsRepository) DeleteMergePreset(key string) error {
	_, err := r.db.Exec(`DELETE FROM app_settings WHERE game = '_global' AND key = ?`, key)
	return err
}
