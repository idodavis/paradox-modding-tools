// Package wiki reads the community wiki for the open workspace's game. It fetches pages, strips them down to their
// article, caches them, finds the pages that belong to a file, and lists the patch notes.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/time/rate"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
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

// What a client asks of the wiki: how long a request may take, how many it may send each second, how large an answer
// it reads, and the user agent it names itself by.
const (
	requestTimeout    = 30 * time.Second
	requestsPerSecond = 2
	maxBytes          = 8 << 20
	userAgent         = "paradox-modding-tools (+https://github.com/idodavis/paradox-modding-tools)"
)

// NewClient makes a client for a game's wiki that caches in the given folder.
func NewClient(cacheDir string, game *game.Game) *Client {
	return &Client{
		game:    game,
		http:    &http.Client{Timeout: requestTimeout},
		limiter: rate.NewLimiter(requestsPerSecond, 1),
		dir:     filepath.Join(cacheDir, "wiki", game.ID),
	}
}

// FetchPage fetches a page through the wiki's API, sanitizes it and caches it.
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
	api, err := url.Parse(client.game.WikiAPI)
	if err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}
	origin := url.URL{Scheme: api.Scheme, Host: api.Host}
	page := Page{Title: response.Parse.Title, HTML: Sanitize(response.Parse.Text, origin.String()), FetchedAt: time.Now()}
	if err := client.save(client.resolvePath(title), page); err != nil {
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
// if the request fails, the wiki answers with anything but OK or the answer is not JSON.
func (client *Client) fetch(params url.Values, target any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	if err := client.limiter.Wait(context.Background()); err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodGet, client.game.WikiAPI+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the wiki answered %s", response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, maxBytes)).Decode(target)
}

// save writes a value as JSON to the given file, making the client's folder first. It returns an error if the folder
// or the file cannot be written.
func (client *Client) save(path string, value any) error {
	if err := os.MkdirAll(client.dir, 0o755); err != nil {
		return err
	}
	return app.SaveJSON(path, value)
}

// resolvePath returns the file a page is cached in.
func (client *Client) resolvePath(title string) string {
	return filepath.Join(client.dir, files.ToSlug(title)+".json")
}

// resolvePage returns a page from the cache, or fetches it when it is not cached. It returns an error if the fetch
// fails.
func (client *Client) resolvePage(title string) (Page, error) {
	if page, ok := client.LoadPage(title); ok {
		return page, nil
	}
	return client.FetchPage(title)
}
