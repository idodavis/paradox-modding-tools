package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"paradox-modding-tools/services/internal/repos"

	"github.com/jmoiron/sqlx"
)

// ############
// SettingsService
// ############

// SettingsService provides persisted app settings.
type SettingsService struct {
	DB   *sqlx.DB
	repo *repos.SettingsRepository
}

func (s *SettingsService) getRepo() *repos.SettingsRepository {
	if s.repo == nil {
		s.repo = repos.NewSettingsRepository(s.DB)
	}
	return s.repo
}

// GetSettings loads settings as a map "game.key" -> value.
func (s *SettingsService) GetSettings() (map[string]string, error) {
	settings, err := s.getRepo().GetAllSettings()
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}

	out := make(map[string]string)
	for _, setting := range settings {
		out[setting.Game+"."+setting.Key] = setting.Value
	}
	return out, nil
}

// SaveSettings writes user settings to app_settings table.
func (s *SettingsService) SaveSettings(settings map[string]string) error {
	repo := s.getRepo()
	for k, v := range settings {
		parts := strings.SplitN(k, ".", 2)
		if len(parts) == 2 {
			if err := repo.UpsertSetting(parts[0], parts[1], v); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergePreset holds a named merge options profile (JSON-safe for bindings)
type MergePreset struct {
	Name    string        `json:"name"`
	Options MergerOptions `json:"options"`
}

// GetMergePresets returns saved merge presets from app_settings.
func (s *SettingsService) GetMergePresets() ([]MergePreset, error) {
	settings, err := s.getRepo().GetMergePresets()
	if err != nil {
		return nil, err
	}
	var presets []MergePreset
	for _, setting := range settings {
		name := strings.TrimPrefix(setting.Key, "merge_preset_")
		var opt MergerOptions
		if err := json.Unmarshal([]byte(setting.Value), &opt); err != nil {
			continue
		}
		presets = append(presets, MergePreset{Name: name, Options: opt})
	}
	return presets, nil
}

// SaveMergePreset saves a merge preset by name.
func (s *SettingsService) SaveMergePreset(name string, options MergerOptions) error {
	if name == "" {
		return fmt.Errorf("preset name required")
	}
	key := "merge_preset_" + name
	val, err := json.Marshal(options)
	if err != nil {
		return err
	}
	return s.getRepo().UpsertSetting("_global", key, string(val))
}

// DeleteMergePreset removes a preset by name.
func (s *SettingsService) DeleteMergePreset(name string) error {
	return s.getRepo().DeleteMergePreset("merge_preset_" + name)
}
