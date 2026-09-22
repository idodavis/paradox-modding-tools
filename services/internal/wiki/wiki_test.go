package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	minInterval = 0
	os.Exit(m.Run())
}

func forget(gameID string) {
	memMu.Lock()
	defer memMu.Unlock()
	for _, k := range sidecarKinds {
		delete(mem[k], gameID)
	}
}

func useTempCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev := cacheDirFn
	cacheDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() {
		cacheDirFn = prev
		forget("ck3")
		forget("vic3")
		forget("eu5")
		apiOverride = ""
	})
}

func TestDenied(t *testing.T) {
	t.Parallel()
	mods := map[string]bool{"way of kings": true}
	cases := []struct {
		title string
		ns    int
		keep  bool
	}{
		{"Event modding", 0, true},
		{"Way of Kings", 0, false},
		{"Effects list", 0, false},
		{"List of baronies", 0, false},
		{"GUI script functions list", 0, false},
		{"PDX DeepL", 0, false},
		{"User:Foo", 0, false},
		{"Talk:Event modding", 1, false},
		{"Triggers", 0, true},
	}
	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			if got := keepMember(c.title, c.ns, mods); got != c.keep {
				t.Fatalf("keepMember(%q ns=%d) = %v want %v", c.title, c.ns, got, c.keep)
			}
		})
	}
}

func TestParseMajorMinor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		maj, min int
		ok       bool
	}{
		{"1.19.6", 1, 19, true},
		{"1.20", 1, 20, true},
		{"latest", 0, 0, false},
		{"", 0, 0, false},
		{"dev", 0, 0, false},
	}
	for _, c := range cases {
		maj, min, ok := ParseMajorMinor(c.in)
		if maj != c.maj || min != c.min || ok != c.ok {
			t.Errorf("ParseMajorMinor(%q) = %d %d %v want %d %d %v",
				c.in, maj, min, ok, c.maj, c.min, c.ok)
		}
	}
}

func TestNeededHotfixVsMinor(t *testing.T) {
	useTempCache(t)
	h := header{GameID: "ck3", FetchedVersion: "1.19.6", FetchedMajor: 1, FetchedMinor: 19}
	if err := saveGuides(&Sidecar{
		header: h,
		Pages:  []Page{{Title: "Event modding", HTML: "<p>x</p>", Revid: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := savePatches(&Sidecar{header: h}); err != nil {
		t.Fatal(err)
	}
	if Needed("ck3", "1.19.7") {
		t.Fatal("hotfix should not recache")
	}
	if Needed("ck3", "1.19.6") {
		t.Fatal("same version should not recache")
	}
	if !Needed("ck3", "1.20.0") {
		t.Fatal("minor bump should recache")
	}
	if !Needed("ck3", "2.0") {
		t.Fatal("major bump should recache")
	}
	if Needed("ck3", "latest") {
		t.Fatal("unparseable should not be stale")
	}
	if Needed("ck3", "1.18.0") {
		t.Fatal("older install should keep dump")
	}
}

func TestNeededMissingPatches(t *testing.T) {
	useTempCache(t)
	if err := saveGuides(&Sidecar{
		GameID: "ck3", FetchedVersion: "1.19.6", FetchedMajor: 1, FetchedMinor: 19,
		Pages: []Page{{Title: "Event modding", HTML: "<p>x</p>", Revid: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if !Needed("ck3", "1.19.6") {
		t.Fatal("missing patches sidecar should refetch")
	}
}

func TestForgetAllForcesNeeded(t *testing.T) {
	useTempCache(t)
	h := header{GameID: "ck3", FetchedVersion: "1.19.6", FetchedMajor: 1, FetchedMinor: 19}
	if err := saveGuides(&Sidecar{
		header: h,
		Pages:  []Page{{Title: "Event modding", HTML: "<p>x</p>", Revid: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := savePatches(&Sidecar{header: h}); err != nil {
		t.Fatal(err)
	}
	if !HasSidecar("ck3") {
		t.Fatal("want sidecar after save")
	}
	if Needed("ck3", "1.19.6") {
		t.Fatal("same version should not recache")
	}
	dir, err := cacheDirFn()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if !HasSidecar("ck3") {
		t.Fatal("RAM should still report sidecar before ForgetAll")
	}
	ForgetAll()
	if HasSidecar("ck3") {
		t.Fatal("want no sidecar after ForgetAll")
	}
	if !Needed("ck3", "1.19.6") {
		t.Fatal("reset must force wiki fetch")
	}
}

func TestSanitizeStripsJunk(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output">
<div class="navbox">nav</div>
<div class="thumb">pic</div>
<span class="mw-editsection">[edit]</span>
<img src="x.png"/>
<p>Hello <a href="/wiki/Events">Events</a> <a href="#Location">here</a></p>
<pre>namespace = foo</pre>
</div>`
	clean, snippets := sanitize(raw, "https://ck3.paradoxwikis.com/")
	if strings.Contains(clean, "nav") || strings.Contains(clean, "pic") ||
		strings.Contains(clean, "[edit]") || strings.Contains(clean, "<img") {
		t.Fatalf("junk left: %s", clean)
	}
	if !strings.Contains(clean, "Hello") || !strings.Contains(clean, "https://ck3.paradoxwikis.com/Events") {
		t.Fatalf("kept content missing: %s", clean)
	}
	if !strings.Contains(clean, `href="#Location"`) {
		t.Fatalf("hash href stripped: %s", clean)
	}
	if len(snippets) != 1 || snippets[0] != "namespace = foo" {
		t.Fatalf("snippets = %#v", snippets)
	}
}

func TestExtractModding(t *testing.T) {
	t.Parallel()
	html := `<h2>Balance</h2><p>nerf</p><h2>User modding</h2><ul><li>renamed x to y</li></ul><h3>Script</h3><p>deeper</p><h2>Bugfixes</h2><p>crash</p>`
	got := extractModding(html)
	if !strings.Contains(got, "renamed") || !strings.Contains(got, "deeper") {
		t.Fatalf("modding slice missing content: %s", got)
	}
	if strings.Contains(got, "nerf") || strings.Contains(got, "crash") {
		t.Fatalf("modding slice leaked siblings: %s", got)
	}
}

func TestLookupEventFile(t *testing.T) {
	useTempCache(t)
	if err := saveGuides(&Sidecar{
		GameID: "ck3", FetchedMajor: 1, FetchedMinor: 16,
		Pages: []Page{
			{Title: "Event modding", HTML: `<h2 id="Effects">Effects</h2><p>events</p>`,
				Snippets: []string{"namespace = x"}},
			{Title: "Triggers", HTML: "<p>trig</p>"},
			{Title: "Effects", HTML: "<p>eff</p>"},
			{Title: "Scopes", HTML: "<p>sc</p>"},
			{Title: "On actions", HTML: "<p>oa</p>"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	pages := PagesFor("ck3", "event", "events/foo.txt")
	if len(pages) < 3 {
		t.Fatalf("pages = %#v want Event + related", pages)
	}
	if pages[0].Title != "Event modding" {
		t.Fatalf("primary = %#v", pages[0])
	}
	if len(pages[0].Sections) != 1 || pages[0].Sections[0].Line != "Effects" {
		t.Fatalf("sections = %#v", pages[0].Sections)
	}
	var titles []string
	for _, p := range pages {
		titles = append(titles, p.Title)
	}
	joined := strings.Join(titles, ",")
	if !strings.Contains(joined, "Triggers") || !strings.Contains(joined, "Effects") {
		t.Fatalf("related missing: %s", joined)
	}
}

func TestLookupRouting(t *testing.T) {
	lang := []Page{
		{Title: "Triggers", HTML: "<p>trig</p>"},
		{Title: "Effects", HTML: "<p>eff</p>"},
		{Title: "Scopes", HTML: "<p>sc</p>"},
		{Title: "On actions", HTML: "<p>oa</p>"},
	}
	cases := []struct {
		name, game, kind, rel, primary string
		pages                          []Page
		want, not                      []string
	}{
		{
			name:    "events ignore Event targets",
			game:    "ck3",
			kind:    "event",
			rel:     "events/foo.txt",
			primary: "Event modding",
			pages: append([]Page{
				{Title: "Event modding", HTML: "<p>e</p>"},
				{Title: "Event targets", HTML: "<p>t</p>"},
				{Title: "Modding", HTML: "<p>hub</p>"},
			}, lang...),
			want: []string{"Triggers", "Effects"},
			not:  []string{"Event targets", "Modding"},
		},
		{
			name:    "eu5 vanilla events under in_game",
			game:    "eu5",
			kind:    "event",
			rel:     "in_game/events/character/court.txt",
			primary: "Event modding",
			pages: append([]Page{
				{Title: "Event modding", HTML: "<p>e</p>"},
				{Title: "Event targets", HTML: "<p>t</p>"},
				{Title: "Modding", HTML: "<p>hub</p>"},
			}, lang...),
			want: []string{"Triggers", "Effects"},
			not:  []string{"Event targets", "Modding"},
		},
		{
			name:    "vic3 scripted effect",
			game:    "vic3",
			kind:    "scripted_effect",
			rel:     "common/scripted_effects/x.txt",
			primary: "Effect",
			pages: []Page{
				{Title: "Effect", HTML: "<p>e</p>"},
				{Title: "Trigger", HTML: "<p>t</p>"},
				{Title: "Scope", HTML: "<p>s</p>"},
				{Title: "On action", HTML: "<p>o</p>"},
				{Title: "Event modding", HTML: "<p>ev</p>"},
			},
			want: []string{"Trigger"},
			not:  []string{"Event modding"},
		},
		{
			name:    "traits modding",
			game:    "ck3",
			kind:    "traits",
			rel:     "common/traits/x.txt",
			primary: "Trait modding",
			pages: append([]Page{
				{Title: "Trait modding", HTML: "<p>t</p>"},
				{Title: "Traits", HTML: "<p>ts</p>"},
			}, lang...),
			want: []string{"Triggers"},
		},
		{
			name:    "ck3 gui",
			game:    "ck3",
			kind:    "gui_type",
			rel:     "gui/window.gui",
			primary: "Interface",
			pages: []Page{
				{Title: "Interface", HTML: "<p>i</p>"},
				{Title: "GUI script", HTML: "<p>g</p>"},
				{Title: "Effects", HTML: "<p>e</p>"},
			},
			want: []string{"GUI script"},
			not:  []string{"Effects"},
		},
		{
			name:    "vic3 gui",
			game:    "vic3",
			kind:    "gui_type",
			rel:     "gui/window.gui",
			primary: "GUI script",
			pages: []Page{
				{Title: "GUI script", HTML: "<p>g</p>"},
				{Title: "Effect", HTML: "<p>e</p>"},
			},
			not: []string{"Effect"},
		},
		{
			name:    "ck3 loc",
			game:    "ck3",
			kind:    "loc_key",
			rel:     "localization/english/foo.yml",
			primary: "Localization",
			pages: []Page{
				{Title: "Localization", HTML: "<p>l</p>"},
				{Title: "Customizable localization", HTML: "<p>c</p>"},
				{Title: "Effects", HTML: "<p>e</p>"},
			},
			want: []string{"Customizable localization"},
			not:  []string{"Effects"},
		},
		{
			name: "hub only is empty",
			game: "ck3",
			kind: "event",
			rel:  "events/foo.txt",
			pages: []Page{
				{Title: "Modding", HTML: "<p>hub</p>"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useTempCache(t)
			if err := saveGuides(&Sidecar{
				GameID: c.game, FetchedMajor: 1, FetchedMinor: 1,
				Pages: c.pages,
			}); err != nil {
				t.Fatal(err)
			}
			got := PagesFor(c.game, c.kind, c.rel)
			if c.primary == "" {
				if len(got) != 0 {
					t.Fatalf("pages = %#v want empty", got)
				}
				return
			}
			if len(got) == 0 || got[0].Title != c.primary {
				t.Fatalf("primary = %#v want %q", got, c.primary)
			}
			joined := ""
			for _, p := range got {
				joined += p.Title + ","
			}
			for _, w := range c.want {
				if !strings.Contains(joined, w) {
					t.Fatalf("missing %q in %s", w, joined)
				}
			}
			for _, n := range c.not {
				if strings.Contains(joined, n) {
					t.Fatalf("unexpected %q in %s", n, joined)
				}
			}
		})
	}
}

func TestSanitizeDropsDumpTables(t *testing.T) {
	t.Parallel()
	var dump strings.Builder
	dump.WriteString(`<div class="mw-parser-output"><p>intro</p><table>`)
	for i := range 25 {
		fmt.Fprintf(&dump, "<tr><td>dump%d</td></tr>", i)
	}
	dump.WriteString(`</table><table><tr><td>keep0</td></tr><tr><td>keep1</td></tr>` +
		`<tr><td>keep2</td></tr></table></div>`)
	clean, _ := sanitize(dump.String(), "https://example.com/")
	if strings.Contains(clean, "dump0") || strings.Contains(clean, "dump24") {
		t.Fatalf("dump table kept: %s", clean)
	}
	if !strings.Contains(clean, "intro") || !strings.Contains(clean, "keep0") ||
		!strings.Contains(clean, "keep2") {
		t.Fatalf("kept content missing: %s", clean)
	}
}

func TestFormatMismatchEmpty(t *testing.T) {
	useTempCache(t)
	path, err := sidecarPath(kindGuides, "ck3")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"formatVersion":99,"gameId":"ck3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if HasSidecar("ck3") {
		t.Fatal("mismatch should not count as present")
	}
	if PagesFor("ck3", "event", "events/foo.txt") != nil {
		t.Fatal("mismatch lookup should be empty")
	}
}

type wikiMock struct {
	parses atomic.Int32
	revid  int
}

func (m *wikiMock) handler() http.Handler {
	if m.revid == 0 {
		m.revid = 10
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case q.Get("list") == "categorymembers":
			cat := q.Get("cmtitle")
			var members []map[string]any
			switch cat {
			case "Category:Mods":
				members = []map[string]any{{"pageid": 9, "ns": 0, "title": "Way of Kings"}}
			case "Category:Modding":
				members = []map[string]any{
					{"pageid": 1, "ns": 0, "title": "Event modding"},
					{"pageid": 2, "ns": 0, "title": "Triggers"},
					{"pageid": 3, "ns": 0, "title": "Effects list"},
					{"pageid": 9, "ns": 0, "title": "Way of Kings"},
				}
			case "Category:Patches":
				members = []map[string]any{
					{"pageid": 4, "ns": 0, "title": "Patch 1.16"},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"categorymembers": members},
			})
		case q.Get("prop") == "info":
			titles := strings.Split(q.Get("titles"), "|")
			var pages []map[string]any
			for i, title := range titles {
				pages = append(pages, map[string]any{
					"pageid": i + 1, "title": title, "lastrevid": m.revid,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"query": map[string]any{"pages": pages},
			})
		case q.Get("action") == "parse":
			m.parses.Add(1)
			title := q.Get("page")
			html := fmt.Sprintf(
				`<div class="mw-parser-output"><p>%s body</p><pre>foo_bar = {}</pre></div>`,
				title,
			)
			if title == "Patch 1.16" {
				html = `<div class="mw-parser-output"><h2>Balance</h2><p>nerf</p>` +
					`<h2>User modding</h2><ul><li>renamed ` + "`ai_war_chest`" + ` to ` + "`ai_war_gold`" + `</li></ul>` +
					`<h2>Bugfixes</h2><p>crash</p></div>`
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"parse": map[string]any{
					"title": title, "revid": m.revid, "text": html,
					"sections": []map[string]any{},
				},
			})
		default:
			http.Error(w, "unexpected "+r.URL.RawQuery, 400)
		}
	})
}

func TestRefreshFixtureAndSkipUnchanged(t *testing.T) {
	useTempCache(t)
	mock := &wikiMock{revid: 42}
	srv := httptest.NewServer(mock.handler())
	t.Cleanup(srv.Close)
	apiOverride = srv.URL

	if err := RefreshIfNeeded(context.Background(), "ck3", "1.16.0", nil); err != nil {
		t.Fatal(err)
	}
	first := mock.parses.Load()
	if first < 2 {
		t.Fatalf("parses = %d, want Event + Triggers (+ patch)", first)
	}
	pages := PagesFor("ck3", "event", "events/foo.txt")
	if len(pages) == 0 || pages[0].Title != "Event modding" {
		t.Fatalf("lookup after refresh: %#v", pages)
	}
	for _, p := range pages {
		if p.Title == "Effects list" || p.Title == "Way of Kings" {
			t.Fatalf("denied page kept: %s", p.Title)
		}
	}
	patch := PatchByVersion("ck3", "1.16")
	if patch == nil || !patch.HasModding || !strings.Contains(patch.ModdingHTML, "ai_war_chest") {
		t.Fatalf("patch modding: %#v", patch)
	}
	if strings.Contains(patch.ModdingHTML, "crash") {
		t.Fatalf("modding leaked bugfixes: %s", patch.ModdingHTML)
	}

	if Needed("ck3", "1.16.1") {
		t.Fatal("hotfix must not need fetch")
	}
	if err := RefreshIfNeeded(context.Background(), "ck3", "1.16.1", nil); err != nil {
		t.Fatal(err)
	}
	if mock.parses.Load() != first {
		t.Fatalf("hotfix scan issued parse calls: %d -> %d", first, mock.parses.Load())
	}

	RefreshExisting(context.Background(), nil)
	if mock.parses.Load() != first {
		t.Fatalf("unchanged revid issued parse: %d -> %d", first, mock.parses.Load())
	}

	mock.revid = 99
	RefreshExisting(context.Background(), nil)
	if mock.parses.Load() <= first {
		t.Fatal("revid change should re-parse")
	}
}

func TestRefreshOfflineDoesNotSaveEmpty(t *testing.T) {
	useTempCache(t)
	apiOverride = "http://127.0.0.1:1"
	err := RefreshIfNeeded(context.Background(), "ck3", "1.16", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if HasSidecar("ck3") {
		t.Fatal("failed fetch must not write sidecar")
	}
}

func TestLiveCK3Refresh(t *testing.T) {
	if os.Getenv("PMT_LIVE_WIKI") == "" {
		t.Skip("set PMT_LIVE_WIKI=1")
	}
	prev := minInterval
	minInterval = 0
	t.Cleanup(func() { minInterval = prev })
	err := RefreshIfNeeded(context.Background(), "ck3", "1.19.0.6", func(done, total int, kind string) {
		if done == 0 || done == total || done%20 == 0 {
			t.Logf("%s %d/%d", kind, done, total)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !HasSidecar("ck3") {
		t.Fatal("expected ck3 sidecar after live refresh")
	}
}

func TestWikiClientDisablesHTTP2(t *testing.T) {
	tr, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("want *http.Transport")
	}
	if tr.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 must be false")
	}
	if tr.TLSNextProto == nil {
		t.Fatal("nil TLSNextProto allows HTTP/2")
	}
	if tr.TLSClientConfig == nil || len(tr.TLSClientConfig.NextProtos) != 1 ||
		tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("TLS NextProtos = %v", tr.TLSClientConfig)
	}
}

func TestAPIGetHTML200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Just a moment...</body></html>"))
	}))
	t.Cleanup(srv.Close)
	_, err := apiGet(context.Background(), srv.URL, url.Values{"action": {"query"}})
	if err == nil || !strings.Contains(err.Error(), "wiki http 200") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Just a moment") {
		t.Fatalf("want body snippet: %v", err)
	}
}

func TestThrottleCancel(t *testing.T) {
	prevI := minInterval
	minInterval = time.Hour
	lastMu.Lock()
	lastCall = time.Now()
	lastMu.Unlock()
	t.Cleanup(func() {
		minInterval = prevI
		lastMu.Lock()
		lastCall = time.Time{}
		lastMu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := throttle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("throttle did not cancel promptly")
	}
}

func TestSanitizeHeadingID(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output">
<h2><span class="mw-headline" id="Effects">Effects</span></h2>
<p>body</p>
</div>`
	clean, _ := sanitize(raw, "https://ck3.paradoxwikis.com/")
	if !strings.Contains(clean, `id="Effects"`) {
		t.Fatalf("heading lost id: %s", clean)
	}
}

func TestSanitizeDropsTocTree(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output">
<div id="toc" class="toc"><h2>Contents</h2><ol><li>1 Location</li></ol></div>
<h2 id="Location">Location</h2><p>here</p>
</div>`
	clean, _ := sanitize(raw, "https://ck3.paradoxwikis.com/")
	if strings.Contains(clean, "Contents") || strings.Contains(clean, "1 Location") {
		t.Fatalf("toc left: %s", clean)
	}
	if !strings.Contains(clean, "Location") || !strings.Contains(clean, "here") {
		t.Fatalf("body missing: %s", clean)
	}
}

func TestSanitizeKeepsSup(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output"><p>x<sup>2</sup></p></div>`
	clean, _ := sanitize(raw, "https://ck3.paradoxwikis.com/")
	if !strings.Contains(clean, "<sup>") || !strings.Contains(clean, "2") {
		t.Fatalf("sup lost: %s", clean)
	}
}

func TestSanitizeDropsCitations(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output">
<p>released<sup id="cite_ref-1" class="reference"><a href="#cite_note-1">[1]</a></sup></p>
<h2>References</h2>
<ol class="references"><li id="cite_note-1">forum post</li></ol>
</div>`
	clean, _ := sanitize(raw, "https://ck3.paradoxwikis.com/")
	for _, junk := range []string{"[1]", "forum post", "cite_note", "References"} {
		if strings.Contains(clean, junk) {
			t.Fatalf("%s left: %s", junk, clean)
		}
	}
	if !strings.Contains(clean, "released") {
		t.Fatalf("body missing: %s", clean)
	}
}

func TestReshapeDropsCiteAnchors(t *testing.T) {
	t.Parallel()
	html := `<p>released<sup><a href="https://ck3.paradoxwikis.com/Patch#cite_note-1">[1]</a></sup></p>`
	got, _ := reshape(html)
	if strings.Contains(got, "[1]") || strings.Contains(got, "cite_note") {
		t.Fatalf("cite left: %s", got)
	}
	if !strings.Contains(got, "released") {
		t.Fatalf("body missing: %s", got)
	}
}

func TestSanitizeDropsTopLinks(t *testing.T) {
	t.Parallel()
	raw := `<div class="mw-parser-output">
<h2 id="Loc">Location</h2><a href="#top">[top]</a><p>here</p>
<a href="#Top_folders">Top folders</a>
</div>`
	clean, _ := sanitize(raw, "https://ck3.paradoxwikis.com/")
	if strings.Contains(clean, "[top]") {
		t.Fatalf("top left: %s", clean)
	}
	if !strings.Contains(clean, "here") || !strings.Contains(clean, "Top folders") {
		t.Fatalf("body missing: %s", clean)
	}
}

func TestReshapeDropsTopLinks(t *testing.T) {
	t.Parallel()
	html := `<h2 id="Loc">Location</h2><a>[top]</a><p>here</p><a href="#top">Return to top</a>`
	got, _ := reshape(html)
	if strings.Contains(got, "[top]") || strings.Contains(got, "Return to top") {
		t.Fatalf("top left: %s", got)
	}
	if !strings.Contains(got, "here") {
		t.Fatalf("body missing: %s", got)
	}
}

func TestReshapeDropsBareCiteMarks(t *testing.T) {
	t.Parallel()
	html := `<p>released on 2026-04-23<a>[1]</a>.</p>`
	got, _ := reshape(html)
	if strings.Contains(got, "[1]") || strings.Contains(got, "<a>") {
		t.Fatalf("cite left: %s", got)
	}
	if !strings.Contains(got, "released") {
		t.Fatalf("body missing: %s", got)
	}
}

func TestReshapeMovesAndDrops(t *testing.T) {
	t.Parallel()
	html := `<h2 id="Loc">Location</h2><p>a</p>
<h2 id="Tools">Scripting Tools</h2><p>vscode</p>
<h2 id="See_also">See also</h2><ul><li>x</li></ul>
<h2 id="Refs">References</h2><ol><li>n</li></ol>
<h2 id="Ext">External links</h2><ul><li>y</li></ul>`
	got, secs := reshape(html)
	for _, junk := range []string{"See also", "References", "External links"} {
		if strings.Contains(got, junk) {
			t.Fatalf("%s left: %s", junk, got)
		}
	}
	loc := strings.Index(got, "Location")
	tools := strings.Index(got, "Scripting Tools")
	if loc < 0 || tools < 0 || tools < loc {
		t.Fatalf("order: %s", got)
	}
	if len(secs) != 2 || secs[0].Line != "Location" || secs[1].Line != "Scripting Tools" {
		t.Fatalf("sections = %#v", secs)
	}
}

func TestServeTimeReshape(t *testing.T) {
	useTempCache(t)
	if err := saveGuides(&Sidecar{
		GameID: "ck3", FetchedMajor: 1, FetchedMinor: 16,
		Pages: []Page{{
			Title: "Event modding",
			HTML: `<h2>Contents</h2><ol><li>1 Location</li></ol>
<h2 id="Location">Location</h2><p>here</p>
<h2 id="See_also">See also</h2><ul><li>x</li></ul>`,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	pages := PagesFor("ck3", "event", "events/foo.txt")
	if len(pages) == 0 {
		t.Fatal("no pages")
	}
	if strings.Contains(pages[0].HTML, "Contents") || strings.Contains(pages[0].HTML, "See also") {
		t.Fatalf("stale chrome left: %s", pages[0].HTML)
	}
	if len(pages[0].Sections) != 1 || pages[0].Sections[0].Line != "Location" {
		t.Fatalf("sections = %#v", pages[0].Sections)
	}
}
