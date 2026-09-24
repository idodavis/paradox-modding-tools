// Package wiki fetches, sanitizes, and looks up Paradox wiki guides and patch
// notes. It persists game-keyed sidecars under local.CacheDir.
// WikiService owns Wails RPCs; this package must not import session or lsp.

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

var (
	httpClient  = wikiHTTP()
	minInterval = time.Second
	apiOverride = ""
	userAgent   = "ParadoxModdingTools/dev (https://github.com/idodavis/paradox-modding-tools; wiki-sidecar)"
	lastMu      sync.Mutex
	lastCall    time.Time
)

// wikiHTTP uses a fresh HTTP/1.1 transport. Fastly (paradoxwikis.com) returns
// 403 54113 on Go's HTTP/2 fingerprint. Cloning DefaultTransport and clearing
// TLSNextProto still offers h2 via ALPN and the connection EOFs.
func wikiHTTP() *http.Client {
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

func wikiAPI(gameID string) string {
	if apiOverride != "" {
		return apiOverride
	}
	info := game.Get(gameID)
	if info == nil {
		return ""
	}
	return info.WikiAPI
}

func throttle(ctx context.Context) error {
	lastMu.Lock()
	defer lastMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if minInterval <= 0 {
		return nil
	}
	wait := minInterval - time.Since(lastCall)
	if wait <= 0 {
		lastCall = time.Now()
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		lastCall = time.Now()
		return nil
	}
}

func clipBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 120 {
		return text[:120]
	}
	return text
}

func looksJSON(body []byte) bool {
	text := bytes.TrimSpace(body)
	return len(text) > 0 && (text[0] == '{' || text[0] == '[')
}

type mwMember struct {
	Title     string `json:"title"`
	LastRevid int    `json:"lastrevid"`
}

func apiGet(ctx context.Context, api string, vals url.Values) ([]byte, error) {
	if err := throttle(ctx); err != nil {
		return nil, err
	}
	vals.Set("format", "json")
	vals.Set("formatversion", "2")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"?"+vals.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, htmlCap+64*1024))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK || !looksJSON(body) {
		return nil, fmt.Errorf("wiki http %d: %s", res.StatusCode, clipBody(body))
	}
	return body, nil
}

// categoryMembers lists a category's articles with each one's latest revision, following the
// API's continue values until the list ends or reaches memberCap.
func categoryMembers(ctx context.Context, api, category string) ([]mwMember, error) {
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
		raw, err := apiGet(ctx, api, vals)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Continue map[string]string `json:"continue"`
			Query    struct {
				Pages []mwMember `json:"pages"`
			} `json:"query"`
			Error *struct {
				Code string `json:"code"`
				Info string `json:"info"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("categorymembers: %w", err)
		}
		if parsed.Error != nil {
			return nil, fmt.Errorf("wiki %s: %s", parsed.Error.Code, parsed.Error.Info)
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
func parsePage(ctx context.Context, api, title string) (html string, revid int, err error) {
	vals := url.Values{
		"action":             {"parse"},
		"page":               {title},
		"prop":               {"text|revid"},
		"disablelimitreport": {"1"},
		"disableeditsection": {"1"},
		"disabletoc":         {"1"},
	}
	raw, err := apiGet(ctx, api, vals)
	if err != nil {
		return "", 0, err
	}
	var parsed struct {
		Parse struct {
			Revid int    `json:"revid"`
			Text  string `json:"text"`
		} `json:"parse"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, fmt.Errorf("parse: %w", err)
	}
	if parsed.Error != nil {
		return "", 0, fmt.Errorf("wiki parse %s: %s", parsed.Error.Code, parsed.Error.Info)
	}
	return parsed.Parse.Text, parsed.Parse.Revid, nil
}
