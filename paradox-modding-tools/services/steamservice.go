package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	steamBBCode "paradox-modding-tools/services/internal"
)

const (
	steamNewsAPIURL     = "https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%s&count=20&maxlength=0"
	steamDBPatchnotes   = "https://steamdb.info/app/%s/patchnotes/"
	patchnotesStaleDays = 7
	eu5SteamAppID       = "3450310"
	ck3SteamAppID       = "1158310"
)

// SteamService centralizes Steam API integrations: patch notes (News API) now, depot/build API later.
type SteamService struct {
	DB *sql.DB
}

// LatestPatchNotes is the latest patch notes entry for a game (JSON-safe for bindings).
type LatestPatchNotes struct {
	Url      string `json:"url"`
	Title    string `json:"title"`
	Contents string `json:"contents"`
}

type steamNewsResponse struct {
	AppNews struct {
		AppID     int `json:"appid"`
		NewsItems []struct {
			GID      string `json:"gid"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			Contents string `json:"contents"`
			FeedName string `json:"feedname"`
			FeedType int    `json:"feed_type"`
		} `json:"newsitems"`
	} `json:"appnews"`
}

// GetLatestPatchNotes returns the latest patch notes for the game using Steam News API.
// Uses patchnotes table; fetches from API if stale or missing. Description is a short preview.
func (s *SteamService) GetLatestPatchNotes(game string) (LatestPatchNotes, error) {
	game = strings.ToLower(strings.TrimSpace(game))
	if game != "ck3" && game != "eu5" {
		return LatestPatchNotes{}, fmt.Errorf("unknown game: %s", game)
	}

	appID := eu5SteamAppID
	if game == "ck3" {
		appID = ck3SteamAppID
	}
	steamDBURL := fmt.Sprintf(steamDBPatchnotes, appID)

	if s.DB != nil {
		var fetchedAt, title, contents, steamURL, dbSteamDBURL string
		err := s.DB.QueryRow(`SELECT fetched_at, title, contents, steam_url, steamdb_url FROM patchnotes WHERE game = ?`, game).Scan(&fetchedAt, &title, &contents, &steamURL, &dbSteamDBURL)
		if err == nil && title != "" {
			if t, parseErr := time.Parse(time.RFC3339, fetchedAt); parseErr == nil && time.Since(t) < patchnotesStaleDays*24*time.Hour {
				url := steamDBURL
				if dbSteamDBURL != "" {
					url = dbSteamDBURL
				}
				return LatestPatchNotes{Url: url, Title: title, Contents: contents}, nil
			}
		}
	}

	// Fetch from Steam News API
	apiURL := fmt.Sprintf(steamNewsAPIURL, appID)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return LatestPatchNotes{}, err
	}
	req.Header.Set("User-Agent", "Paradox-Modding-Tool/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return LatestPatchNotes{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return LatestPatchNotes{}, fmt.Errorf("Steam API HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return LatestPatchNotes{}, err
	}

	var apiResp steamNewsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return LatestPatchNotes{}, err
	}

	var best *struct {
		Title    string
		URL      string
		Contents string
	}
	for i := range apiResp.AppNews.NewsItems {
		item := &apiResp.AppNews.NewsItems[i]
		if item.FeedName != "steam_community_announcements" {
			continue
		}
		lower := strings.ToLower(item.Title + " " + item.Contents)
		if strings.Contains(lower, "patch") || strings.Contains(lower, "update") || strings.Contains(lower, "1.") {
			best = &struct {
				Title    string
				URL      string
				Contents string
			}{item.Title, item.URL, item.Contents}
			break
		}
	}
	if best == nil && len(apiResp.AppNews.NewsItems) > 0 {
		for i := range apiResp.AppNews.NewsItems {
			item := &apiResp.AppNews.NewsItems[i]
			if item.FeedName == "steam_community_announcements" {
				best = &struct {
					Title    string
					URL      string
					Contents string
				}{item.Title, item.URL, item.Contents}
				break
			}
		}
	}
	if best == nil {
		return LatestPatchNotes{Url: steamDBURL, Title: "", Contents: ""}, nil
	}

	// Convert Steam BBCode to HTML before storing and returning
	htmlContents := steamBBCode.SteamBBCodeToHTML(best.Contents)

	if s.DB != nil {
		fetchedAt := time.Now().UTC().Format(time.RFC3339)
		_, _ = s.DB.Exec(`INSERT INTO patchnotes (game, fetched_at, title, contents, steam_url, steamdb_url) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(game) DO UPDATE SET fetched_at=excluded.fetched_at, title=excluded.title, contents=excluded.contents, steam_url=excluded.steam_url, steamdb_url=excluded.steamdb_url`,
			game, fetchedAt, best.Title, htmlContents, best.URL, steamDBURL)
	}

	return LatestPatchNotes{Url: steamDBURL, Title: best.Title, Contents: htmlContents}, nil
}
