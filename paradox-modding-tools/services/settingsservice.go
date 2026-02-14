package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// ############
// SettingsService
// ############

// SettingsService provides persisted app settings.
type SettingsService struct {
	DB *sql.DB
}

// AppSettings holds persisted app settings and game constants (JSON-safe for bindings).
type AppSettings struct {
	GameInstallPathCk3  string `json:"gameInstallPathCk3"`
	GameInstallPathEu5 string `json:"gameInstallPathEu5"`
	MergeOutputDir     string `json:"mergeOutputDir"`
	Ck3SteamAppId      string `json:"ck3_steamAppId"`
	Ck3WikiUrl         string `json:"ck3_wikiUrl"`
	Ck3ScriptRootFolder string `json:"ck3_scriptRootFolder"`
	Ck3DocFileName     string `json:"ck3_docFileName"`
	Eu5SteamAppId      string `json:"eu5_steamAppId"`
	Eu5WikiUrl         string `json:"eu5_wikiUrl"`
	Eu5ScriptRootFolder string `json:"eu5_scriptRootFolder"`
	Eu5DocFileName     string `json:"eu5_docFileName"`
}

// GetSettings loads settings and game constants from app_settings table.
func (s *SettingsService) GetSettings() (AppSettings, error) {
	if s.DB == nil {
		return AppSettings{}, fmt.Errorf("database not initialized")
	}
	out := AppSettings{}
	rows, err := s.DB.Query(`SELECT game, key, value FROM app_settings`)
	if err != nil {
		return AppSettings{}, fmt.Errorf("read settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var game, key, value string
		if err := rows.Scan(&game, &key, &value); err != nil {
			return AppSettings{}, err
		}
		switch {
		case game == "ck3" && key == "install_path":
			out.GameInstallPathCk3 = value
		case game == "eu5" && key == "install_path":
			out.GameInstallPathEu5 = value
		case game == "_global" && key == "merge_output_dir":
			out.MergeOutputDir = value
		case key == "ck3_steamAppId":
			out.Ck3SteamAppId = value
		case key == "ck3_wikiUrl":
			out.Ck3WikiUrl = value
		case key == "ck3_scriptRootFolder":
			out.Ck3ScriptRootFolder = value
		case key == "ck3_docFileName":
			out.Ck3DocFileName = value
		case key == "eu5_steamAppId":
			out.Eu5SteamAppId = value
		case key == "eu5_wikiUrl":
			out.Eu5WikiUrl = value
		case key == "eu5_scriptRootFolder":
			out.Eu5ScriptRootFolder = value
		case key == "eu5_docFileName":
			out.Eu5DocFileName = value
		}
	}
	return out, rows.Err()
}

// SaveSettings writes user settings to app_settings table.
func (s *SettingsService) SaveSettings(settings AppSettings) error {
	if s.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	upsert := func(game, key, value string) error {
		_, err := s.DB.Exec(`INSERT INTO app_settings (game, key, value) VALUES (?, ?, ?) ON CONFLICT(game, key) DO UPDATE SET value = excluded.value`, game, key, value)
		return err
	}
	if err := upsert("ck3", "install_path", settings.GameInstallPathCk3); err != nil {
		return err
	}
	if err := upsert("eu5", "install_path", settings.GameInstallPathEu5); err != nil {
		return err
	}
	return upsert("_global", "merge_output_dir", settings.MergeOutputDir)
}

// MergePreset holds a named merge options profile (JSON-safe for bindings)
type MergePreset struct {
	Name    string       `json:"name"`
	Options MergerOptions `json:"options"`
}

// GetMergePresets returns saved merge presets from app_settings.
func (s *SettingsService) GetMergePresets() ([]MergePreset, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := s.DB.Query(`SELECT key, value FROM app_settings WHERE game = '_global' AND key LIKE 'merge_preset_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var presets []MergePreset
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(key, "merge_preset_")
		var opt MergerOptions
		if err := json.Unmarshal([]byte(value), &opt); err != nil {
			continue
		}
		presets = append(presets, MergePreset{Name: name, Options: opt})
	}
	return presets, rows.Err()
}

// SaveMergePreset saves a merge preset by name.
func (s *SettingsService) SaveMergePreset(name string, options MergerOptions) error {
	if s.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	if name == "" {
		return fmt.Errorf("preset name required")
	}
	key := "merge_preset_" + name
	val, err := json.Marshal(options)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO app_settings (game, key, value) VALUES (?, ?, ?) ON CONFLICT(game, key) DO UPDATE SET value = excluded.value`, "_global", key, string(val))
	return err
}

// DeleteMergePreset removes a preset by name.
func (s *SettingsService) DeleteMergePreset(name string) error {
	if s.DB == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := s.DB.Exec(`DELETE FROM app_settings WHERE game = '_global' AND key = ?`, "merge_preset_"+name)
	return err
}
