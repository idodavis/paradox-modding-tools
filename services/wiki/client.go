// client.go fetches a game's wiki pages and caches them.

package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
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

	// Revision is the wiki's revision of the page as it was fetched. It is 0
	// for a page cached without one, which a refresh fetches again.
	Revision int64 `json:"revision"`

	// Modding is true when a heading of the page holds the word Modding.
	Modding bool `json:"modding"`

	// Released is the release date a patch note gives, or "" for none.
	Released string `json:"released"`
}

// Client fetches one game's wiki, rate limited, and caches its pages.
type Client struct {
	game *game.Game

	// origin is the wiki's address, which its API and its links are under.
	origin  *url.URL
	limiter *rate.Limiter
	dir     string

	// mu guards pages and texts.
	mu sync.Mutex

	// pages holds the cached pages read so far, by title, so that a page's
	// file is decoded once.
	pages map[string]Page

	// texts holds the text of the pages searched so far, in lower case, by
	// title.
	texts map[string]string
}

const (
	requestTimeout    = 30 * time.Second
	requestsPerSecond = 2

	// actionParam is the API parameter that names what a request asks for.
	actionParam = "action"
)

func newClient(cacheDir string, row *game.Game) *Client {
	return &Client{
		game:    row,
		origin:  &url.URL{Scheme: "https", Host: row.ID + ".paradoxwikis.com"},
		limiter: rate.NewLimiter(requestsPerSecond, 1),
		dir:     filepath.Join(cacheDir, "wiki", row.ID),
		pages:   map[string]Page{},
		texts:   map[string]string{},
	}
}

// resolveLink is the link rule of a wiki page. A link inside the page stays as
// it is. Any other keeps only a web address, made absolute, and a link to
// another page of the wiki also gets that page's title as data-page.
func (c *Client) resolveLink(target string) []html.Attribute {
	if strings.HasPrefix(target, "#") {
		return []html.Attribute{{Key: "href", Val: target}}
	}

	address, err := url.Parse(target)
	if err != nil {
		return nil
	}

	address = c.origin.ResolveReference(address)
	if address.Scheme != "http" && address.Scheme != "https" {
		return nil
	}

	var link []html.Attribute
	title := strings.TrimPrefix(strings.TrimPrefix(address.Path, "/"), "wiki/")
	if address.Host == c.origin.Host && title != "" && title != "index.php" {
		link = append(link, html.Attribute{Key: "data-page", Val: strings.ReplaceAll(title, "_", " ")})
	}

	return append(link, html.Attribute{Key: "href", Val: address.String()})
}

// FetchPage fetches a page and caches it sanitized, with the revision the wiki
// gave. It returns an error if the fetch fails or the wiki has no such page.
func (c *Client) FetchPage(title string) (Page, error) {
	params := url.Values{actionParam: {"parse"}, "page": {title}, "prop": {"text|revid"}}
	var response struct {
		Parse struct {
			Title    string `json:"title"`
			Text     string `json:"text"`
			Revision int64  `json:"revid"`
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

	// The facts are read from the sanitized page.
	sanitized := app.Sanitize(response.Parse.Text, c.resolveLink)
	root, err := app.ParseFragment(sanitized)
	if err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}

	page := Page{
		Title: response.Parse.Title, URL: c.origin.JoinPath(strings.ReplaceAll(response.Parse.Title, " ", "_")).String(),
		HTML: sanitized, FetchedAt: time.Now(), Revision: response.Parse.Revision, Modding: hasModding(root),
		Released: resolveReleased(root),
	}
	if err := app.SaveJSON(c.resolvePath(title), page); err != nil {
		return Page{}, fmt.Errorf("wiki page %q: %w", title, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.pages[title] = page
	delete(c.texts, title)

	return page, nil
}

// LoadPage returns a cached page, or false if it is not cached. A page's file
// is decoded once and the page kept in memory.
func (c *Client) LoadPage(title string) (Page, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if page, ok := c.pages[title]; ok {
		return page, true
	}

	page, err := app.LoadJSON[Page](c.resolvePath(title))
	if err != nil || page.FetchedAt.IsZero() {
		return page, false
	}

	c.pages[title] = page

	return page, true
}

// loadText returns the text of a cached page in lower case, which is worked
// out once. It returns false if the page is not cached or its HTML cannot be
// parsed.
func (c *Client) loadText(title string) (string, bool) {
	page, ok := c.LoadPage(title)
	if !ok {
		return "", false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	text, ok := c.texts[title]
	if !ok {
		root, err := app.ParseFragment(page.HTML)
		if err != nil {
			return "", false
		}

		text = strings.ToLower(app.TextOf(root))
		c.texts[title] = text
	}

	return text, true
}

// fetch sends a query to the wiki's API and decodes the JSON answer.
func (c *Client) fetch(params url.Values, target any) error {
	params.Set("format", "json")
	params.Set("formatversion", "2")
	if err := c.limiter.Wait(context.Background()); err != nil {
		return err
	}

	body, err := app.FetchURL(c.origin.JoinPath("api.php").String()+"?"+params.Encode(), requestTimeout)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, target)
}

func (c *Client) resolvePath(title string) string {
	return filepath.Join(c.dir, files.ToSlug(title)+".json")
}
