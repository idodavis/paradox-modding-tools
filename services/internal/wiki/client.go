// client.go is the wiki client: the HTTP client the three wikis accept, the pause between calls, and the MediaWiki
// API calls a refresh makes.

// Package wiki fetches, sanitizes, and looks up Paradox wiki guides and patch notes. It keeps game-keyed sidecars in
// the cache folder it is given (appdata.CacheDir). WikiService owns the Wails RPCs; this package imports game and
// appdata and nothing above them.
package wiki

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
)

// Client is one wiki reader: the HTTP client the three wikis accept, the pause between calls, where the sidecars are
// kept, the sidecars read into memory, and the lock that lets one refresh run at a time.
type Client struct {
	http        *http.Client
	interval    time.Duration // between two API calls; the tests set 0
	apiOverride string        // the tests' server in place of each game's api.php
	cacheDir    string        // the folder the sidecars are in (appdata.CacheDir)

	callMu     sync.Mutex // guards lastCall
	lastCall   time.Time
	refreshing sync.Mutex // one refresh at a time
	memLock    sync.Mutex // guards mem
	mem        map[string]*Sidecar
}

// mwMember is one article of a category as the API lists it: its title and its latest revision.
type mwMember struct {
	Title     string `json:"title"`
	LastRevid int    `json:"lastrevid"`
}

const (
	userAgent = "ParadoxModdingTools/dev (https://github.com/idodavis/paradox-modding-tools; wiki-sidecar)"
	htmlCap   = 1_500_000 // the most bytes of HTML one page may hold and be kept
	memberCap = 500       // the most articles a category listing returns
)

// New is a wiki reader keeping its sidecars in cacheDir.
func New(cacheDir string) *Client {
	return &Client{http: newHTTP(), interval: time.Second, cacheDir: cacheDir, mem: map[string]*Sidecar{}}
}

// newHTTP returns an HTTP client over a fresh HTTP/1.1 transport. Fastly (paradoxwikis.com) returns
// 403 54113 on Go's HTTP/2 fingerprint. Cloning DefaultTransport and clearing
// TLSNextProto still offers h2 via ALPN and the connection EOFs.
func newHTTP() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			ForceAttemptHTTP2:   false,
			TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
			TLSClientConfig:     &tls.Config{NextProtos: []string{"http/1.1"}},
			TLSHandshakeTimeout: 10 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// api is the api.php endpoint of a game's wiki: the client's override where one is set, "" for a game with none.
func (client *Client) api(gameID string) string {
	if client.apiOverride != "" {
		return client.apiOverride
	}
	info := game.Get(gameID)
	if info == nil {
		return ""
	}
	return info.WikiAPI
}

// throttle waits until the client's interval has passed since the last API call, and gives up when ctx ends.
func (client *Client) throttle(ctx context.Context) error {
	client.callMu.Lock()
	defer client.callMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if client.interval <= 0 {
		return nil
	}
	wait := client.interval - time.Since(client.lastCall)
	if wait <= 0 {
		client.lastCall = time.Now()
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		client.lastCall = time.Now()
		return nil
	}
}

// apiGet asks the API at api for vals as JSON and decodes the reply into answer; a reply that is not JSON, or that
// the API reports as failed, is an error carrying what it said.
func (client *Client) apiGet(ctx context.Context, api string, vals url.Values, answer any) error {
	if err := client.throttle(ctx); err != nil {
		return err
	}
	vals.Set("format", "json")
	vals.Set("formatversion", "2")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"?"+vals.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, htmlCap+64*1024))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK || !looksJSON(body) {
		return fmt.Errorf("wiki http %d: %s", res.StatusCode, clipBody(body))
	}

	// the API answers a failed request with an error object, whatever the action
	var failed struct {
		Error *struct{ Code, Info string } `json:"error"`
	}
	if json.Unmarshal(body, &failed) == nil && failed.Error != nil {
		return fmt.Errorf("wiki %s %s: %s", vals.Get("action"), failed.Error.Code, failed.Error.Info)
	}
	return json.Unmarshal(body, answer)
}

// clipBody is the start of an answer body on one line, for an error message.
func clipBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 120 {
		return text[:120]
	}
	return text
}

// looksJSON reports whether a body starts as a JSON object or array, which a wiki's challenge page does not.
func looksJSON(body []byte) bool {
	text := bytes.TrimSpace(body)
	return len(text) > 0 && (text[0] == '{' || text[0] == '[')
}

// categoryMembers lists a category's articles with each one's latest revision, following the
// API's continue values until the list ends or reaches memberCap.
func (client *Client) categoryMembers(ctx context.Context, api, category string) ([]mwMember, error) {
	var out []mwMember
	vals := url.Values{
		"action":       {"query"},
		"generator":    {"categorymembers"},
		"gcmtitle":     {category},
		"gcmnamespace": {"0"},
		"gcmlimit":     {"500"},
		"prop":         {"info"},
	}
	for {
		var parsed struct {
			Continue map[string]string `json:"continue"`
			Query    struct {
				Pages []mwMember `json:"pages"`
			} `json:"query"`
		}
		if err := client.apiGet(ctx, api, vals, &parsed); err != nil {
			return nil, err
		}
		out = append(out, parsed.Query.Pages...)
		if len(out) >= memberCap || len(parsed.Continue) == 0 {
			return out[:min(len(out), memberCap)], nil
		}
		for key, value := range parsed.Continue {
			vals.Set(key, value)
		}
	}
}

// parsePage fetches a page's HTML without its edit links and contents box.
func (client *Client) parsePage(ctx context.Context, api, title string) (html string, revid int, err error) {
	vals := url.Values{
		"action":             {"parse"},
		"page":               {title},
		"prop":               {"text|revid"},
		"disablelimitreport": {"1"},
		"disableeditsection": {"1"},
		"disabletoc":         {"1"},
	}
	var parsed struct {
		Parse struct {
			Revid int    `json:"revid"`
			Text  string `json:"text"`
		} `json:"parse"`
	}
	if err := client.apiGet(ctx, api, vals, &parsed); err != nil {
		return "", 0, err
	}
	return parsed.Parse.Text, parsed.Parse.Revid, nil
}
