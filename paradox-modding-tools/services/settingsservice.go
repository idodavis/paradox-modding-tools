package services

import (
	"database/sql"
	"fmt"
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
	GameInstallPathEu5  string `json:"gameInstallPathEu5"`
	Ck3SteamAppId       string `json:"ck3_steamAppId"`
	Ck3WikiUrl          string `json:"ck3_wikiUrl"`
	Ck3ScriptRootFolder string `json:"ck3_scriptRootFolder"`
	Ck3DocFileName      string `json:"ck3_docFileName"`
	Eu5SteamAppId       string `json:"eu5_steamAppId"`
	Eu5WikiUrl          string `json:"eu5_wikiUrl"`
	Eu5ScriptRootFolder string `json:"eu5_scriptRootFolder"`
	Eu5DocFileName      string `json:"eu5_docFileName"`
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
	return upsert("eu5", "install_path", settings.GameInstallPathEu5)
}
