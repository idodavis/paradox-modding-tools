package wiki

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"

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
	g := game.Get(gameID)
	if g == nil {
		return ""
	}
	return g.WikiAPI
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

func clipBody(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

func looksJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

type mwMember struct {
	PageID int    `json:"pageid"`
	Ns     int    `json:"ns"`
	Title  string `json:"title"`
}

type mwPageInfo struct {
	PageID    int    `json:"pageid"`
	Title     string `json:"title"`
	LastRevid int    `json:"lastrevid"`
	Missing   bool   `json:"missing"`
}

type mwSection struct {
	TocLevel int    `json:"toclevel"`
	Line     string `json:"line"`
	Anchor   string `json:"anchor"`
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

func categoryMembers(ctx context.Context, api, category string) ([]mwMember, error) {
	var out []mwMember
	cont := ""
	for {
		vals := url.Values{
			"action":      {"query"},
			"list":        {"categorymembers"},
			"cmtitle":     {category},
			"cmnamespace": {"0"},
			"cmlimit":     {"500"},
		}
		if cont != "" {
			vals.Set("cmcontinue", cont)
		}
		raw, err := apiGet(ctx, api, vals)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Continue struct {
				CmContinue string `json:"cmcontinue"`
			} `json:"continue"`
			Query struct {
				Categorymembers []mwMember `json:"categorymembers"`
			} `json:"query"`
			Error *struct {
				Code string `json:"code"`
				Info string `json:"info"`
			} `json:"error"`
		}
		if err := sonic.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("categorymembers: %w", err)
		}
		if parsed.Error != nil {
			return nil, fmt.Errorf("wiki %s: %s", parsed.Error.Code, parsed.Error.Info)
		}
		out = append(out, parsed.Query.Categorymembers...)
		if len(out) >= memberCap || parsed.Continue.CmContinue == "" {
			if len(out) > memberCap {
				out = out[:memberCap]
			}
			return out, nil
		}
		cont = parsed.Continue.CmContinue
	}
}

func pageInfo(ctx context.Context, api string, titles []string) (map[string]mwPageInfo, error) {
	out := map[string]mwPageInfo{}
	for i := 0; i < len(titles); i += infoBatch {
		end := min(i+infoBatch, len(titles))
		vals := url.Values{
			"action": {"query"},
			"prop":   {"info"},
			"titles": {strings.Join(titles[i:end], "|")},
		}
		raw, err := apiGet(ctx, api, vals)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Query struct {
				Pages []mwPageInfo `json:"pages"`
			} `json:"query"`
		}
		if err := sonic.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("pageinfo: %w", err)
		}
		for _, p := range parsed.Query.Pages {
			out[normTitle(p.Title)] = p
		}
	}
	return out, nil
}

func parsePage(ctx context.Context, api, title string) (html string, revid int, sections []Section, err error) {
	vals := url.Values{
		"action":             {"parse"},
		"page":               {title},
		"prop":               {"text|sections|revid"},
		"disablelimitreport": {"1"},
	}
	raw, err := apiGet(ctx, api, vals)
	if err != nil {
		return "", 0, nil, err
	}
	var parsed struct {
		Parse struct {
			Title    string      `json:"title"`
			Revid    int         `json:"revid"`
			Text     string      `json:"text"`
			Sections []mwSection `json:"sections"`
		} `json:"parse"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := sonic.Unmarshal(raw, &parsed); err != nil {
		return "", 0, nil, fmt.Errorf("parse: %w", err)
	}
	if parsed.Error != nil {
		return "", 0, nil, fmt.Errorf("wiki parse %s: %s", parsed.Error.Code, parsed.Error.Info)
	}
	secs := make([]Section, 0, len(parsed.Parse.Sections))
	for _, s := range parsed.Parse.Sections {
		secs = append(secs, Section(s))
	}
	return parsed.Parse.Text, parsed.Parse.Revid, secs, nil
}
