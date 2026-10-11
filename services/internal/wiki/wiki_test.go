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
)

// The games the tests refresh, the one file kind and path they look pages up for, and the page titles they name.
const (
	gameCK3           = "ck3"
	gameVic3          = "vic3"
	gameEU5           = "eu5"
	kindEvent         = "event"
	relEvent          = "events/foo.txt"
	relGui            = "gui/window.gui"
	titleEventModding = "Event modding"
	titleEventTargets = "Event targets"
	titleModding      = "Modding"
	titleEffects      = "Effects"
	titleTriggers     = "Triggers"
	titleGUIScript    = "GUI script"
	titleWayOfKings   = "Way of Kings"
	titleEffectsList  = "Effects list"
	titleUserFoo      = "User:Foo"
	baseCK3           = "https://ck3.paradoxwikis.com/"
)

// languagePages are the scripting-language pages most games' wikis have, as the guides a file may show.
var languagePages = []Page{
	plain(titleTriggers),
	plain(titleEffects),
	plain("Scopes"),
	plain("On actions"),
}

// routingCase is a game's guides and the tabs a file of one kind should get from them.
type routingCase struct {
	name, game, kind, rel, primary string
	pages                          []Page
	want, not, sections            []string
}

// cleanCase is a fetched page body and what clean should keep of it, drop from it and read from its headings.
type cleanCase struct {
	name, body          string
	want, not, sections []string
	modding             []string
}

// plain is a guide page with a paragraph for its text and no headings.
func plain(title string) Page {
	return Page{Title: title, HTML: "<p>" + title + "</p>"}
}

// sectionLines are the headings of a page as their text.
func sectionLines(sections []Section) []string {
	var lines []string
	for _, section := range sections {
		lines = append(lines, section.Line)
	}
	return lines
}

// assertRouting saves each case's guides and checks the tabs PagesFor picks for its file.
func assertRouting(t *testing.T, cases []routingCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := New(t.TempDir())
			if err := client.save(testCase.game, &Sidecar{Guides: testCase.pages}); err != nil {
				t.Fatal(err)
			}
			got := client.PagesFor(testCase.game, testCase.kind, testCase.rel)
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

// assertClean cleans each case's body and checks what stayed, what went and the headings found.
func assertClean(t *testing.T, cases []cleanCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			page, sections, modding := clean(`<div class="mw-parser-output">`+testCase.body+`</div>`, baseCK3)
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

// TestLookupRoutesEventFiles pins that an event file's tabs lead with the event page, add the scripting-language
// pages and leave out the hub and the targets page, and that a hub alone gives none.
func TestLookupRoutesEventFiles(t *testing.T) {
	t.Parallel()
	assertRouting(t, []routingCase{
		{
			name:    "events ignore Event targets",
			game:    gameCK3,
			kind:    kindEvent,
			rel:     relEvent,
			primary: titleEventModding,
			pages: append([]Page{
				{Title: titleEventModding, HTML: `<h2 id="Effects">Effects</h2><p>events</p>`,
					Sections: []Section{{TocLevel: 2, Line: titleEffects, Anchor: titleEffects}}},
				plain(titleEventTargets),
				plain(titleModding),
			}, languagePages...),
			want:     []string{titleTriggers, titleEffects},
			not:      []string{titleEventTargets, titleModding},
			sections: []string{titleEffects},
		},
		{
			name:    "eu5 vanilla events under in_game",
			game:    gameEU5,
			kind:    kindEvent,
			rel:     "in_game/events/character/court.txt",
			primary: titleEventModding,
			pages:   append([]Page{plain(titleEventModding), plain(titleEventTargets), plain(titleModding)}, languagePages...),
			want:    []string{titleTriggers, titleEffects},
			not:     []string{titleEventTargets, titleModding},
		},
		{
			name: "hub only is empty",
			game: gameCK3,
			kind: kindEvent,
			rel:  relEvent,
			pages: []Page{
				plain(titleModding),
			},
		},
	})
}

// TestLookupRoutesScriptFiles pins the tabs a scripted effect and a trait file get from their game's pages.
func TestLookupRoutesScriptFiles(t *testing.T) {
	t.Parallel()
	assertRouting(t, []routingCase{
		{
			name:    "vic3 scripted effect",
			game:    gameVic3,
			kind:    "scripted_effect",
			rel:     "common/scripted_effects/x.txt",
			primary: "Effect",
			pages: []Page{
				plain("Effect"),
				plain("Trigger"),
				plain("Scope"),
				plain("On action"),
				plain(titleEventModding),
			},
			want: []string{"Trigger"},
			not:  []string{titleEventModding},
		},
		{
			name:    "traits modding",
			game:    gameCK3,
			kind:    "traits",
			rel:     "common/traits/x.txt",
			primary: "Trait modding",
			pages:   append([]Page{plain("Trait modding"), plain("Traits")}, languagePages...),
			want:    []string{titleTriggers},
		},
	})
}

// TestLookupRoutesByExtension pins the tabs a GUI or localization file gets by its extension.
func TestLookupRoutesByExtension(t *testing.T) {
	t.Parallel()
	assertRouting(t, []routingCase{
		{
			name:    "ck3 gui",
			game:    gameCK3,
			kind:    "gui",
			rel:     relGui,
			primary: "Interface",
			pages:   []Page{plain("Interface"), plain(titleGUIScript), plain(titleEffects)},
			want:    []string{titleGUIScript},
			not:     []string{titleEffects},
		},
		{
			name:    "vic3 gui",
			game:    gameVic3,
			kind:    "gui",
			rel:     relGui,
			primary: titleGUIScript,
			pages:   []Page{plain(titleGUIScript), plain("Effect")},
			not:     []string{"Effect"},
		},
		{
			name:    "ck3 loc",
			game:    gameCK3,
			kind:    "yml",
			rel:     "localization/english/foo.yml",
			primary: "Localization",
			pages:   []Page{plain("Localization"), plain("Customizable localization"), plain(titleEffects)},
			want:    []string{"Customizable localization"},
			not:     []string{titleEffects},
		},
	})
}

// TestCleanDropsWikiChrome pins that navigation, thumbnails, citations and links back to the top are dropped and
// links made absolute.
func TestCleanDropsWikiChrome(t *testing.T) {
	t.Parallel()
	assertClean(t, []cleanCase{
		{
			name: "chrome",
			body: `<div class="navbox">nav</div><div class="thumb">pic</div><img src="x.png"/>
<p>Hello <a href="/Events">Events</a> <a href="#Location">here</a></p><pre>namespace = foo</pre>`,
			want: []string{"Hello", `href="https://ck3.paradoxwikis.com/Events"`, `href="#Location"`, "namespace = foo"},
			not:  []string{"nav", "pic", "<img"},
		},
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
	})
}

// TestCleanKeepsTablesAndSuperscripts pins that a table of more than two rows is kept folded and a short one as it is.
func TestCleanKeepsTablesAndSuperscripts(t *testing.T) {
	t.Parallel()
	var dump strings.Builder
	dump.WriteString(`<p>intro</p><table>`)
	for i := range 25 {
		fmt.Fprintf(&dump, "<tr><td>dump%d</td></tr>", i)
	}
	dump.WriteString(`</table><table><tr><td>keep0</td></tr><tr><td>keep1</td></tr></table>`)
	assertClean(t, []cleanCase{
		{
			name: "a table of more than two rows kept folded, closed; a two-row one as it is",
			body: dump.String(),
			want: []string{"intro", "<details><summary>Table, 25 rows</summary><table>", "dump0", "dump24", "</table></details><table>", "keep0", "keep1"},
			not:  []string{"Table, 2 rows"},
		},
		{name: "sup", body: `<p>x<sup>2</sup></p>`, want: []string{"<sup>2</sup>"}},
	})
}

// TestCleanReadsHeadings pins the headings, the sections dropped or moved, and which headings make a Modding page.
func TestCleanReadsHeadings(t *testing.T) {
	t.Parallel()
	assertClean(t, []cleanCase{
		{
			name:     "heading id",
			body:     `<h2><span class="mw-headline" id="Effects">Effects</span></h2><p>body</p>`,
			want:     []string{`id="Effects"`},
			sections: []string{titleEffects},
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
	})
}

// wikiMock answers the MediaWiki API the way the three wikis do: category listings, parsed pages and, when told to,
// an HTML challenge page for every request.
type wikiMock struct {
	parses    atomic.Int32
	revid     int
	challenge bool // answer every request with an HTML challenge page
}

// handler is the mock's HTTP handler; a zero revid is 10.
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
				"Category:Mods": {titleWayOfKings},
				"Category:Modding": {titleEventModding, titleTriggers, titleEffectsList, titleWayOfKings,
					"List of baronies", "GUI script functions list", "PDX DeepL", titleUserFoo},
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

// refreshCK3 refreshes the client's ck3 pages without listening to the progress.
func refreshCK3(t *testing.T, client *Client) error {
	t.Helper()
	return client.Refresh(t.Context(), gameCK3, func(string, int, int) {})
}

// refreshedWiki is a client over a temporary cache and a mock wiki at revision 42, refreshed once.
func refreshedWiki(t *testing.T) (*Client, *wikiMock, *httptest.Server) {
	t.Helper()
	client := New(t.TempDir())
	client.interval = 0
	mock := &wikiMock{revid: 42}
	srv := httptest.NewServer(mock.handler())
	t.Cleanup(srv.Close)
	client.apiOverride = srv.URL
	if err := refreshCK3(t, client); err != nil {
		t.Fatal(err)
	}
	return client, mock, srv
}

// TestRefreshSavesTheModdingPages pins that a refresh saves the modding pages a lookup then finds, leaves the
// denied ones out, and marks a patch note with a Modding section.
func TestRefreshSavesTheModdingPages(t *testing.T) {
	client, mock, _ := refreshedWiki(t)
	if parses := mock.parses.Load(); parses < 2 {
		t.Fatalf("parses = %d, want Event + Triggers (+ patch)", parses)
	}
	pages := client.PagesFor(gameCK3, kindEvent, relEvent)
	if len(pages) == 0 || pages[0].Title != titleEventModding {
		t.Fatalf("lookup after refresh: %#v", pages)
	}
	for _, page := range pages {
		if page.Title == titleEffectsList || page.Title == titleWayOfKings {
			t.Fatalf("denied page kept: %s", page.Title)
		}
	}
	if patch := client.PatchByVersion(gameCK3, "1.16"); patch == nil || !patch.Modding {
		t.Fatalf("patch modding: %#v", patch)
	}
}

// TestRefreshKeepsTheModdingTitles pins which titles the filter keeps: the modding pages, not mods, lists, tools
// or user pages.
func TestRefreshKeepsTheModdingTitles(t *testing.T) {
	client, _, _ := refreshedWiki(t)
	// the title filter: saved guides keep the modding pages and drop mods, lists, tools, user pages
	sidecar, err := client.load(gameCK3)
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
		{titleEventModding, true},
		{titleWayOfKings, false},
		{titleEffectsList, false},
		{"List of baronies", false},
		{"GUI script functions list", false},
		{"PDX DeepL", false},
		{titleUserFoo, false},
		{titleTriggers, true},
	} {
		if slices.Contains(saved, testCase.title) != testCase.keep {
			t.Errorf("%q saved = %v want %v", testCase.title, !testCase.keep, testCase.keep)
		}
	}
}

// TestRefreshSkipsUnchangedRevisions pins that a refresh fetches again only the pages whose revision changed.
func TestRefreshSkipsUnchangedRevisions(t *testing.T) {
	client, mock, _ := refreshedWiki(t)
	first := mock.parses.Load()
	if err := refreshCK3(t, client); err != nil {
		t.Fatal(err)
	}
	if mock.parses.Load() != first {
		t.Fatalf("unchanged revid issued parse: %d -> %d", first, mock.parses.Load())
	}
	mock.revid = 99
	if err := refreshCK3(t, client); err != nil {
		t.Fatal(err)
	}
	if mock.parses.Load() <= first {
		t.Fatal("revid change should re-parse")
	}
}

// TestRefreshFailureSavesNothing pins that a challenge page is an error carrying its text, and that a failed
// refresh leaves the saved pages as they were.
func TestRefreshFailureSavesNothing(t *testing.T) {
	client, mock, srv := refreshedWiki(t)
	// an HTML challenge page answered with 200 is an error that carries its text
	mock.challenge = true
	err := client.apiGet(t.Context(), srv.URL, url.Values{"action": {"query"}}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "wiki http 200") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Just a moment") {
		t.Fatalf("want body snippet: %v", err)
	}

	// a dead server is an error, and the failed fetch saves nothing over the pages already kept
	srv.Close()
	if err := refreshCK3(t, client); err == nil {
		t.Fatal("expected error")
	}
	client.ForgetAll()
	if pages := client.PagesFor(gameCK3, kindEvent, relEvent); len(pages) == 0 || pages[0].Title != titleEventModding {
		t.Fatalf("failed fetch must not write sidecar: %#v", pages)
	}
}

// TestLiveCK3Refresh refreshes the ck3 wiki over the network, behind PMT_LIVE_WIKI.
func TestLiveCK3Refresh(t *testing.T) {
	if os.Getenv("PMT_LIVE_WIKI") == "" {
		t.Skip("set PMT_LIVE_WIKI=1")
	}
	client := New(t.TempDir())
	if err := refreshCK3(t, client); err != nil {
		t.Fatal(err)
	}
	sidecar, err := client.load(gameCK3)
	if err != nil || len(sidecar.Guides) == 0 || len(sidecar.Patches) == 0 {
		t.Fatalf("live refresh saved %v, %v", sidecar, err)
	}
	t.Logf("%d guides, %d patch notes", len(sidecar.Guides), len(sidecar.Patches))
}

// TestWikiClientDisablesHTTP2 pins that the client's transport offers HTTP/1.1 only, which the wikis' CDN needs.
func TestWikiClientDisablesHTTP2(t *testing.T) {
	client := New(t.TempDir())
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok {
		t.Fatal("want *http.Transport")
	}
	if transport.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 must be false")
	}
	if transport.TLSNextProto == nil {
		t.Fatal("nil TLSNextProto allows HTTP/2")
	}
	if transport.TLSClientConfig == nil || len(transport.TLSClientConfig.NextProtos) != 1 ||
		transport.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("TLS NextProtos = %v", transport.TLSClientConfig)
	}
}
