package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ############
// SettingsService
// ############

const (
	appConfigDirName        = "Paradox Modding Tools"
	settingsFileName        = "settings.json"
	patchnotesCacheFileName = "patch-notes-cache.json"
	docpathsCacheFileName   = "modding-docs-cache.json"
	patchnotesCacheTTL      = 7 * 24 * time.Hour

	// Steam app IDs (used for SteamDB PatchnotesRSS)
	steamAppIDCK3 = "1158310"
	steamAppIDEU5 = "3450310"

	steamDBPatchnotesRSS = "https://steamdb.info/api/PatchnotesRSS/?appid="
)

// AppSettings holds persisted app settings (JSON-safe for bindings).
type AppSettings struct {
	GameInstallPathCk3 string `json:"gameInstallPathCk3"`
	GameInstallPathEu5 string `json:"gameInstallPathEu5"`
}

// SettingsService provides persisted app settings, doc path cache, and latest patch notes (SteamDB RSS).
type SettingsService struct{}

// settingsPath returns the path to settings.json in the app config directory.
func settingsPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	appDir := filepath.Join(configDir, appConfigDirName)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return filepath.Join(appDir, settingsFileName), nil
}

// configDirPath returns the path to a file in the app config directory (same dir as settings, patchnotes, docpaths).
func configDirPath(filename string) (string, error) {
	sp, err := settingsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(sp), filename), nil
}

// readConfigFile reads a JSON file from the config dir into dest. If the file does not exist, returns nil without touching dest.
func readConfigFile(filename string, dest any) error {
	path, err := configDirPath(filename)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, dest)
}

// writeConfigFile writes data as JSON to a file in the config dir.
func writeConfigFile(filename string, data any) error {
	path, err := configDirPath(filename)
	if err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0o644)
}

// GetSettings loads settings from disk. Returns default values if file does not exist.
func (s *SettingsService) GetSettings() (AppSettings, error) {
	var out AppSettings
	if err := readConfigFile(settingsFileName, &out); err != nil {
		return AppSettings{}, fmt.Errorf("read settings: %w", err)
	}
	return out, nil
}

// SaveSettings writes settings to disk.
func (s *SettingsService) SaveSettings(settings AppSettings) error {
	return writeConfigFile(settingsFileName, settings)
}

// LatestPatchNotes is the latest patch notes entry for a game (JSON-safe for bindings).
type LatestPatchNotes struct {
	Url         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// patchnotesCacheEntry holds cached RSS result for one game.
type patchnotesCacheEntry struct {
	FetchedAt         string `json:"fetchedAt"`
	LatestLink        string `json:"latestLink"`
	LatestTitle       string `json:"latestTitle"`
	LatestDescription string `json:"latestDescription"`
}

// patchnotesCache is the on-disk cache for both games.
type patchnotesCache struct {
	Ck3 *patchnotesCacheEntry `json:"ck3,omitempty"`
	Eu5 *patchnotesCacheEntry `json:"eu5,omitempty"`
}

// GetLatestPatchNotes returns the latest patch notes URL and title for the game, using SteamDB RSS.
// Caches the result for 7 days in patchnotes-cache.json.
func (s *SettingsService) GetLatestPatchNotes(game string) (LatestPatchNotes, error) {
	game = strings.ToLower(strings.TrimSpace(game))
	if game != "ck3" && game != "eu5" {
		return LatestPatchNotes{}, fmt.Errorf("unknown game: %s", game)
	}
	feedURL := steamDBPatchnotesRSS + steamAppIDCK3
	if game == "eu5" {
		feedURL = steamDBPatchnotesRSS + steamAppIDEU5
	}
	cache, _ := loadPatchnotesCache()
	var entry *patchnotesCacheEntry
	if game == "ck3" {
		entry = cache.Ck3
	} else {
		entry = cache.Eu5
	}
	if entry != nil && entry.LatestLink != "" {
		if t, err := time.Parse(time.RFC3339, entry.FetchedAt); err == nil && time.Since(t) < patchnotesCacheTTL {
			return LatestPatchNotes{Url: entry.LatestLink, Title: entry.LatestTitle, Description: entry.LatestDescription}, nil
		}
	}
	link, title, description, err := fetchSteamDBPatchnotesFirstItem(feedURL)
	if err != nil {
		if entry != nil && entry.LatestLink != "" {
			return LatestPatchNotes{Url: entry.LatestLink, Title: entry.LatestTitle, Description: entry.LatestDescription}, nil
		}
		return LatestPatchNotes{}, err
	}
	newEntry := &patchnotesCacheEntry{
		FetchedAt:         time.Now().UTC().Format(time.RFC3339),
		LatestLink:        link,
		LatestTitle:       title,
		LatestDescription: description,
	}
	if game == "ck3" {
		cache.Ck3 = newEntry
	} else {
		cache.Eu5 = newEntry
	}
	_ = writeConfigFile(patchnotesCacheFileName, cache)
	return LatestPatchNotes{Url: link, Title: title, Description: description}, nil
}

func loadPatchnotesCache() (*patchnotesCache, error) {
	var c patchnotesCache
	_ = readConfigFile(patchnotesCacheFileName, &c)
	return &c, nil
}

func fetchSteamDBPatchnotesFirstItem(feedURL string) (link, title, description string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, feedURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "Paradox-Modding-Tool/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}
	return parseSteamDBPatchnotesFirstItem(body)
}

// parseSteamDBPatchnotesFirstItem parses SteamDB Patchnotes RSS first <item> and returns <link>, <title> and <description>.
func parseSteamDBPatchnotesFirstItem(body []byte) (link, title, description string, err error) {
	s := string(body)
	const (
		rssItem         = "<item>"
		rssTitle        = "<title>"
		rssTitleC       = "</title>"
		rssDescription  = "<description>"
		rssDescriptionC = "</description>"
		rssLink         = "<link>"
		rssLinkC        = "</link>"
	)
	lower := strings.ToLower(s)
	idx := strings.Index(lower, rssItem)
	if idx < 0 {
		return "", "", "", fmt.Errorf("no <item> in feed")
	}
	itemBlock := s[idx : idx+min(8192, len(s)-idx)]
	itemLower := strings.ToLower(itemBlock)
	// <title>...</title>
	if i := strings.Index(itemLower, rssTitle); i >= 0 {
		start := i + len(rssTitle)
		if j := strings.Index(itemBlock[start:], rssTitleC); j >= 0 {
			title = strings.TrimSpace(itemBlock[start : start+j])
			title = decodeXMLText(title)
		}
	}
	// <link>...</link>
	if i := strings.Index(itemLower, rssLink); i >= 0 {
		start := i + len(rssLink)
		if j := strings.Index(itemBlock[start:], rssLinkC); j >= 0 {
			link = strings.TrimSpace(itemBlock[start : start+j])
		}
	}
	if link == "" {
		return "", "", "", fmt.Errorf("no <link> in first item")
	}

	// <description>...</description>
	if i := strings.Index(itemLower, rssDescription); i >= 0 {
		start := i + len(rssDescription)
		if j := strings.Index(itemBlock[start:], rssDescriptionC); j >= 0 {
			description = strings.TrimSpace(itemBlock[start : start+j])
			description = decodeXMLText(description)
		}
	}
	return link, title, description, nil
}

func decodeXMLText(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&apos;", "'")
	return s
}
