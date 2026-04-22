package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	steamBBCode "paradox-modding-tools/services/internal"
	"paradox-modding-tools/services/internal/repos"

	"github.com/jmoiron/sqlx"
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
	DB   *sqlx.DB
	repo *repos.SteamRepository
}

func (s *SteamService) getRepo() *repos.SteamRepository {
	if s.repo == nil {
		s.repo = repos.NewSteamRepository(s.DB)
	}
	return s.repo
}

// LatestPatchNotes is the latest patch notes entry for a game (JSON-safe for bindings).
type LatestPatchNotes struct {
	Url      string `json:"url"`
	Title    string `json:"title"`
	Contents string `json:"contents"`
}

type NewsItem struct {
	GID      string `json:"gid"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Contents string `json:"contents"`
	FeedName string `json:"feedname"`
	FeedType int    `json:"feed_type"`
}

type steamNewsResponse struct {
	AppNews struct {
		AppID     int        `json:"appid"`
		NewsItems []NewsItem `json:"newsitems"`
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

	repo := s.getRepo()
	note, err := repo.GetPatchNote(game)
	if err == nil && note != nil {
		if t, parseErr := time.Parse(time.RFC3339, note.FetchedAt); parseErr == nil && time.Since(t) < patchnotesStaleDays*24*time.Hour {
			url := steamDBURL
			if note.SteamDBURL != "" {
				url = note.SteamDBURL
			}
			return LatestPatchNotes{Url: url, Title: note.Title, Contents: note.Contents}, nil
		}
	}

	items, err := s.fetchSteamNews(appID)
	if err != nil {
		return LatestPatchNotes{}, err
	}

	best := s.findBestPatchNote(items)
	if best == nil {
		return LatestPatchNotes{Url: steamDBURL, Title: "", Contents: ""}, nil
	}

	// Convert Steam BBCode to HTML before storing and returning
	htmlContents := steamBBCode.SteamBBCodeToHTML(best.Contents)
	_ = repo.UpsertPatchNote(game, best.Title, htmlContents, best.URL, steamDBURL)

	return LatestPatchNotes{Url: steamDBURL, Title: best.Title, Contents: htmlContents}, nil
}

func (s *SteamService) fetchSteamNews(appID string) ([]NewsItem, error) {
	// Fetch from Steam News API
	apiURL := fmt.Sprintf(steamNewsAPIURL, appID)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Paradox-Modding-Tool/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Steam API HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp steamNewsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, err
	}
	return apiResp.AppNews.NewsItems, nil
}

func (s *SteamService) findBestPatchNote(items []NewsItem) *NewsItem {
	var firstAnnouncement *NewsItem

	for i := range items {
		item := &items[i]
		if item.FeedName != "steam_community_announcements" {
			continue
		}

		if firstAnnouncement == nil {
			firstAnnouncement = item
		}

		lower := strings.ToLower(item.Title + " " + item.Contents)
		if strings.Contains(lower, "patch") || strings.Contains(lower, "update") || strings.Contains(lower, "1.") {
			return item
		}
	}
	return firstAnnouncement
}
