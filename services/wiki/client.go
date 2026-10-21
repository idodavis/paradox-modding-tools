// Package wiki reads the community wiki for the open workspace's game. It fetches pages, strips them down to their
// article, caches them, finds the pages that belong to a file, and lists the patch notes.
package wiki

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"paradox-modding-tools/internal/game"
)

// A Page is one cached wiki page: its title and its sanitized HTML.
type Page struct {
	Title     string    `json:"title"`
	HTML      string    `json:"html"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// A Client fetches one game's wiki, rate limited, and caches each page in the cache folder.
type Client struct {
	game    *game.Game
	http    *http.Client
	limiter *rate.Limiter
	dir     string
}

// NewClient makes a client for a game's wiki that caches in the given folder.
func NewClient(cacheDir string, game *game.Game) *Client { panic("stub") }

// FetchPage fetches a page through the wiki's API, sanitizes it and caches it.
func (client *Client) FetchPage(title string) (Page, error) { panic("stub") }

// LoadPage returns a cached page, or false if the page is not cached.
func (client *Client) LoadPage(title string) (Page, bool) { panic("stub") }
