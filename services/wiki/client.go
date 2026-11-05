// Package wiki reads the community wiki for the open workspace's game. It caches the pages, lists the patch notes and
// builds the lexicon of the game's commands.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
)

// A Page is one wiki page as the cache keeps it, its HTML sanitized.
type Page struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"` // the page on the wiki
	HTML      string    `json:"html"`
	FetchedAt time.Time `json:"fetchedAt"`
	Modding   bool      `json:"modding"`  // the page has a heading that holds the word Modding
	Released  string    `json:"released"` // the release date a patch note gives in its opening text; "" if none
}

// A Client fetches one game's wiki, rate limited, and caches its pages.
type Client struct {
	game    *game.Game
	api     string // the address of the wiki's API
	limiter *rate.Limiter
	dir     string
}

// The limits on a client's requests.
const (
	requestTimeout    = 30 * time.Second
	requestsPerSecond = 2
)

// NewClient returns a client for a game's wiki that caches its pages in the given folder.
func NewClient(cacheDir string, row *game.Game) *Client {
	return &Client{
		game:    row,
		api:     fmt.Sprintf(game.WikiAPI, row.ID),
		limiter: rate.NewLimiter(requestsPerSecond, 1),
		dir:     filepath.Join(cacheDir, "wiki", row.ID),
	}
}

// FetchPage fetches a page, sanitizes it and caches it. It returns an error if the fetch fails or the wiki has no such
// page.
func (client *Client) FetchPage(title string) (Page, error) {
	params := url.Values{"action": {"parse"}, "page": {title}, "prop": {"text"}}
	var response struct {
		Parse struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"parse"`
		Error struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := client.fetch(params, &response); err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}
	if response.Error.Code != "" {
		return Page{}, fmt.Errorf("wiki page %q: %s", title, response.Error.Info)
	}
	api, err := url.Parse(client.api)
	if err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}
	origin := &url.URL{Scheme: api.Scheme, Host: api.Host}
	sanitized := Sanitize(response.Parse.Text, origin)
	root, err := parseFragment(sanitized)
	if err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}
	page := Page{
		Title: response.Parse.Title, URL: origin.JoinPath(strings.ReplaceAll(response.Parse.Title, " ", "_")).String(),
		HTML: sanitized, FetchedAt: time.Now(), Modding: hasModding(root), Released: resolveReleased(root),
	}
	if err := app.SaveJSON(client.resolvePath(title), page); err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}
	return page, nil
}

// LoadPage returns a cached page, or false if the page is not cached.
func (client *Client) LoadPage(title string) (Page, bool) {
	page, err := app.LoadJSON[Page](client.resolvePath(title))
	return page, err == nil && !page.FetchedAt.IsZero()
}

// fetch sends the given query to the wiki's API and decodes the JSON answer into the given value. It returns an error
// if the request fails or the answer is not JSON.
func (client *Client) fetch(params url.Values, target any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	if err := client.limiter.Wait(context.Background()); err != nil {
		return err
	}
	body, err := app.FetchURL(client.api+"?"+params.Encode(), requestTimeout)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

// resolvePath returns the file a page is cached in.
func (client *Client) resolvePath(title string) string {
	return filepath.Join(client.dir, files.ToSlug(title)+".json")
}
