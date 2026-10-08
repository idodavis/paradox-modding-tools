package wiki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(tests *testing.M) {
	minInterval = 0
	os.Exit(tests.Run())
}

func forget(gameID string) {
	memMu.Lock()
	defer memMu.Unlock()
	delete(mem, gameID)
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
		want, not, sections            []string
	}{
		{
			name:    "events ignore Event targets",
			game:    "ck3",
			kind:    "event",
			rel:     "events/foo.txt",
			primary: "Event modding",
			pages: append([]Page{
				{Title: "Event modding", HTML: `<h2 id="Effects">Effects</h2><p>events</p>`,
					Sections: []Section{{TocLevel: 2, Line: "Effects", Anchor: "Effects"}}},
				{Title: "Event targets", HTML: "<p>t</p>"},
				{Title: "Modding", HTML: "<p>hub</p>"},
			}, lang...),
			want:     []string{"Triggers", "Effects"},
			not:      []string{"Event targets", "Modding"},
			sections: []string{"Effects"},
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
			kind:    "gui",
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
			kind:    "gui",
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
			kind:    "yml",
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
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			useTempCache(t)
			if err := save(testCase.game, &Sidecar{Guides: testCase.pages}); err != nil {
				t.Fatal(err)
			}
			got := PagesFor(testCase.game, testCase.kind, testCase.rel)
			if testCase.primary == "" {
				if len(got) != 0 {
					t.Fatalf("pages = %#v want empty", got)
				}
				return
			}
			if len(got) == 0 || got[0].Title != testCase.primary {
				t.Fatalf("primary = %#v want %q", got, testCase.primary)
			}
			if lines := sectionLines(got[0].Sections); !slices.Equal(lines, testCase.sections) {
				t.Fatalf("sections = %v, want %v", lines, testCase.sections)
			}
			joined := ""
			for _, page := range got {
				joined += page.Title + ","
			}
			for _, title := range testCase.want {
				if !strings.Contains(joined, title) {
					t.Fatalf("missing %q in %s", title, joined)
				}
			}
			for _, unwanted := range testCase.not {
				if strings.Contains(joined, unwanted) {
					t.Fatalf("unexpected %q in %s", unwanted, joined)
				}
			}
		})
	}
}

func TestClean(t *testing.T) {
	t.Parallel()
	var dump strings.Builder
	dump.WriteString(`<p>intro</p><table>`)
	for i := range 25 {
		fmt.Fprintf(&dump, "<tr><td>dump%d</td></tr>", i)
	}
	dump.WriteString(`</table><table><tr><td>keep0</td></tr><tr><td>keep1</td></tr></table>`)
	cases := []struct {
		name, body          string
		want, not, sections []string
		modding             []string
	}{
		{
			name: "chrome",
			body: `<div class="navbox">nav</div><div class="thumb">pic</div><img src="x.png"/>
<p>Hello <a href="/Events">Events</a> <a href="#Location">here</a></p><pre>namespace = foo</pre>`,
			want: []string{"Hello", `href="https://ck3.paradoxwikis.com/Events"`, `href="#Location"`, "namespace = foo"},
			not:  []string{"nav", "pic", "<img"},
		},
		{
			name: "a table of more than two rows kept folded, closed; a two-row one as it is",
			body: dump.String(),
			want: []string{"intro", "<details><summary>Table, 25 rows</summary><table>", "dump0", "dump24", "</table></details><table>", "keep0", "keep1"},
			not:  []string{"Table, 2 rows"},
		},
		{
			name:     "heading id",
			body:     `<h2><span class="mw-headline" id="Effects">Effects</span></h2><p>body</p>`,
			want:     []string{`id="Effects"`},
			sections: []string{"Effects"},
		},
		{name: "sup", body: `<p>x<sup>2</sup></p>`, want: []string{"<sup>2</sup>"}},
		{
			name: "citations",
			body: `<p>released<sup id="cite_ref-1" class="reference"><a href="#cite_note-1">[1]</a></sup></p>
<h2>References</h2><ol class="references"><li id="cite_note-1">forum post</li></ol>`,
			want: []string{"released"},
			not:  []string{"[1]", "forum post", "cite_note", "References"},
		},
		{
			name:     "top links",
			body:     `<h2 id="Loc">Location</h2><a href="#top">[top]</a><p>here</p><a href="#Top_folders">Top folders</a>`,
			want:     []string{"here", "Top folders"},
			not:      []string{"[top]"},
			sections: []string{"Location"},
		},
		{
			name: "sections dropped and moved",
			body: `<h2 id="Loc">Location</h2><p>a</p><h2 id="Tools">Scripting Tools</h2><p>vscode</p>
<h2 id="See_also">See also</h2><ul><li>x</li></ul><h2 id="Refs">References</h2><ol><li>n</li></ol>
<h2 id="Ext">External links</h2><ul><li>y</li></ul>`,
			not:      []string{"See also", "References", "External links"},
			sections: []string{"Location", "Scripting Tools"},
		},
		{
			name: "modding",
			body: `<h2>Balance</h2><p>nerf</p><h2>User modding</h2><ul><li>renamed x to y</li></ul>
<h3>Script</h3><p>deeper</p><h2>Bugfixes</h2><p>crash</p>`,
			sections: []string{"Balance", "User modding", "Script", "Bugfixes"},
			modding:  []string{"User modding", "Script"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			page, sections, modding := clean(`<div class="mw-parser-output">`+testCase.body+`</div>`, "https://ck3.paradoxwikis.com/")
			for _, text := range testCase.want {
				if !strings.Contains(page, text) {
					t.Errorf("%q missing: %s", text, page)
				}
			}
			for _, text := range testCase.not {
				if strings.Contains(page, text) {
					t.Errorf("%q left: %s", text, page)
				}
			}
			if lines := sectionLines(sections); !slices.Equal(lines, testCase.sections) {
				t.Errorf("sections = %v, want %v", lines, testCase.sections)
			}
			if modding != (testCase.modding != nil) {
				t.Errorf("modding = %v", modding)
			}
		})
	}
}

func sectionLines(sections []Section) []string {
	var lines []string
	for _, section := range sections {
		lines = append(lines, section.Line)
	}
	return lines
}

type wikiMock struct {
	parses    atomic.Int32
	revid     int
	challenge bool // answer every request with an HTML challenge page
}

func (wikiMock *wikiMock) handler() http.Handler {
	if wikiMock.revid == 0 {
		wikiMock.revid = 10
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case wikiMock.challenge:
			writer.Header().Set("Content-Type", "text/html")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("<!DOCTYPE html><html><body>Just a moment...</body></html>"))
		case query.Get("generator") == "categorymembers":
			titles := map[string][]string{
				"Category:Mods": {"Way of Kings"},
				"Category:Modding": {"Event modding", "Triggers", "Effects list", "Way of Kings",
					"List of baronies", "GUI script functions list", "PDX DeepL", "User:Foo"},
				"Category:Patches": {"Patch 1.16"},
			}[query.Get("gcmtitle")]
			var pages []map[string]any
			for _, title := range titles {
				pages = append(pages, map[string]any{"title": title, "lastrevid": wikiMock.revid})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"query": map[string]any{"pages": pages},
			})
		case query.Get("action") == "parse":
			wikiMock.parses.Add(1)
			title := query.Get("page")
			html := fmt.Sprintf(
				`<div class="mw-parser-output"><p>%s body</p><pre>foo_bar = {}</pre></div>`,
				title,
			)
			if title == "Patch 1.16" {
				html = `<div class="mw-parser-output"><h2>Balance</h2><p>nerf</p>` +
					`<h2>User modding</h2><ul><li>renamed ` + "`ai_war_chest`" + ` to ` + "`ai_war_gold`" + `</li></ul>` +
					`<h2>Bugfixes</h2><p>crash</p></div>`
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"parse": map[string]any{
					"title": title, "revid": wikiMock.revid, "text": html,
				},
			})
		default:
			http.Error(writer, "unexpected "+request.URL.RawQuery, 400)
		}
	})
}

func TestRefreshFixtureAndSkipUnchanged(t *testing.T) {
	useTempCache(t)
	mock := &wikiMock{revid: 42}
	srv := httptest.NewServer(mock.handler())
	t.Cleanup(srv.Close)
	apiOverride = srv.URL

	if err := Refresh(t.Context(), "ck3", func(string, int, int) {}); err != nil {
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
	for _, page := range pages {
		if page.Title == "Effects list" || page.Title == "Way of Kings" {
			t.Fatalf("denied page kept: %s", page.Title)
		}
	}

	// the title filter: saved guides keep the modding pages and drop mods, lists, tools, user pages
	sidecar, err := load("ck3")
	if err != nil {
		t.Fatal(err)
	}
	var saved []string
	for _, page := range sidecar.Guides {
		saved = append(saved, page.Title)
	}
	for _, testCase := range []struct {
		title string
		keep  bool
	}{
		{"Event modding", true},
		{"Way of Kings", false},
		{"Effects list", false},
		{"List of baronies", false},
		{"GUI script functions list", false},
		{"PDX DeepL", false},
		{"User:Foo", false},
		{"Triggers", true},
	} {
		if slices.Contains(saved, testCase.title) != testCase.keep {
			t.Errorf("%q saved = %v want %v", testCase.title, !testCase.keep, testCase.keep)
		}
	}

	if patch := PatchByVersion("ck3", "1.16"); patch == nil || !patch.Modding {
		t.Fatalf("patch modding: %#v", patch)
	}

	if err := Refresh(t.Context(), "ck3", func(string, int, int) {}); err != nil {
		t.Fatal(err)
	}
	if mock.parses.Load() != first {
		t.Fatalf("unchanged revid issued parse: %d -> %d", first, mock.parses.Load())
	}

	mock.revid = 99
	if err := Refresh(t.Context(), "ck3", func(string, int, int) {}); err != nil {
		t.Fatal(err)
	}
	if mock.parses.Load() <= first {
		t.Fatal("revid change should re-parse")
	}

	// an HTML challenge page answered with 200 is an error that carries its text
	mock.challenge = true
	err = apiGet(t.Context(), srv.URL, url.Values{"action": {"query"}}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "wiki http 200") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Just a moment") {
		t.Fatalf("want body snippet: %v", err)
	}

	// a dead server is an error, and the failed fetch saves nothing over the pages already kept
	srv.Close()
	if err := Refresh(t.Context(), "ck3", func(string, int, int) {}); err == nil {
		t.Fatal("expected error")
	}
	forget("ck3")
	if pages := PagesFor("ck3", "event", "events/foo.txt"); len(pages) == 0 || pages[0].Title != "Event modding" {
		t.Fatalf("failed fetch must not write sidecar: %#v", pages)
	}
}

func TestLiveCK3Refresh(t *testing.T) {
	if os.Getenv("PMT_LIVE_WIKI") == "" {
		t.Skip("set PMT_LIVE_WIKI=1")
	}
	useTempCache(t)
	prev := minInterval
	minInterval = time.Second
	t.Cleanup(func() { minInterval = prev })
	if err := Refresh(t.Context(), "ck3", func(string, int, int) {}); err != nil {
		t.Fatal(err)
	}
	sidecar, err := load("ck3")
	if err != nil || len(sidecar.Guides) == 0 || len(sidecar.Patches) == 0 {
		t.Fatalf("live refresh saved %v, %v", sidecar, err)
	}
	t.Logf("%d guides, %d patch notes", len(sidecar.Guides), len(sidecar.Patches))
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
