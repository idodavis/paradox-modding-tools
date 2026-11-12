// client.go fetches a game's wiki pages and caches them.

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

// Page is one wiki page as the cache keeps it.
type Page struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	HTML      string    `json:"html"`
	FetchedAt time.Time `json:"fetchedAt"`

	// Modding is true when a heading of the page holds the word Modding.
	Modding bool `json:"modding"`

	// Released is the release date a patch note gives, or "" for none.
	Released string `json:"released"`
}

// Client fetches one game's wiki, rate limited, and caches its pages.
type Client struct {
	game    *game.Game
	api     string
	limiter *rate.Limiter
	dir     string
}

const (
	requestTimeout    = 30 * time.Second
	requestsPerSecond = 2
)

// NewClient returns a client for a game's wiki.
func NewClient(cacheDir string, row *game.Game) *Client {
	return &Client{
		game:    row,
		api:     fmt.Sprintf(game.WikiAPI, row.ID),
		limiter: rate.NewLimiter(requestsPerSecond, 1),
		dir:     filepath.Join(cacheDir, "wiki", row.ID),
	}
}

// FetchPage fetches a page and caches it sanitized. It returns an error if the
// fetch fails or the wiki has no such page.
func (c *Client) FetchPage(title string) (Page, error) {
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
	if err := c.fetch(params, &response); err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}

	if response.Error.Code != "" {
		return Page{}, fmt.Errorf("wiki page %q: %s", title, response.Error.Info)
	}

	api, err := url.Parse(c.api)
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
	if err := app.SaveJSON(c.resolvePath(title), page); err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}

	return page, nil
}

// LoadPage returns a cached page, or false if it is not cached.
func (c *Client) LoadPage(title string) (Page, bool) {
	page, err := app.LoadJSON[Page](c.resolvePath(title))

	return page, err == nil && !page.FetchedAt.IsZero()
}

// fetch sends a query to the wiki's API and decodes the JSON answer.
func (c *Client) fetch(params url.Values, target any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	if err := c.limiter.Wait(context.Background()); err != nil {
		return err
	}

	body, err := app.FetchURL(c.api+"?"+params.Encode(), requestTimeout)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, target)
}

func (c *Client) resolvePath(title string) string {
	return filepath.Join(c.dir, files.ToSlug(title)+".json")
}
