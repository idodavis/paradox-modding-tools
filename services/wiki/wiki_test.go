package wiki

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/time/rate"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// newApp returns the one application the tests send events through. It is never run.
var newApp = sync.OnceValue(func() *application.App { return application.New(application.Options{}) })

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// openService opens a workspace on the CK3 fixture install and returns the wiki service over it with its session. The
// workspace is closed when the test ends.
func openService(t *testing.T) (*Service, *workspace.Session) {
	t.Helper()
	newApp()
	// the folders PMT uses come from the XDG variables, read again now and once more when the test ends
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: "ck3", Path: resolveFixture(t, "ck3")}}
		records.Workspaces = []app.Workspace{{ID: "w1", Name: "Test", Install: "i1", Language: "english"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wsp := &workspace.Service{Config: config, Build: "1"}
	wsp.Open("w1")
	t.Cleanup(wsp.Close)
	ws, err := wsp.Session()
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}, ws
}

// keepPage caches a page with the given title and HTML in a client's folder, as a refresh would have.
func keepPage(t *testing.T, client *Client, html, title string) {
	t.Helper()
	if err := app.SaveJSON(client.resolvePath(title), Page{Title: title, HTML: html, FetchedAt: time.Now(), Modding: hasModding(html)}); err != nil {
		t.Fatal(err)
	}
}

// TestSanitizeKeepsAndDropsElements checks each kept element, each dropped one, the attributes that survive, an anchor
// on a heading's span, a relative link made absolute, the dropped sections and the folded table.
func TestSanitizeKeepsAndDropsElements(t *testing.T) {
	input := `<script>alert(1)</script><style>p{}</style><noscript>none</noscript><nav>menu</nav>` +
		`<div class="navbox"><p>navigation</p></div><div id="toc"><ul><li>contents</li></ul></div><!-- comment -->` +
		`<table class="infobox"><tr><td>box</td></tr></table><p>cited<sup id="cite_ref-1">[1]</sup><sup></sup></p>` +
		`<h1 id="one" class="x">One</h1>` +
		`<h2 id="two"><span class="mw-headline" id="Two">Two</span><span class="mw-editsection">[edit]</span></h2>` +
		`<h3 id="three">Three</h3><h4>Four</h4>` +
		`<div class="box" style="color:red"><p onclick="x()">Text <b>b</b> <i>i</i> <strong>s</strong> <em>e</em> ` +
		`<code>c</code><br>after</p></div>` +
		`<p><a href="/wiki/Foo" title="t">relative</a> <a href="#Two">inside</a> <a href="http://example.com/x">web</a> ` +
		`<a href="javascript:alert(2)">script</a> <a href="mailto:a@b.c">mail</a></p>` +
		`<ul><li>item</li></ul><ol><li>step</li></ol><img src="/images/a.png" alt="Alt" width="5"><hr>` +
		`<table><thead><tr><th>head</th></tr></thead><tbody><tr><td colspan="2" style="x">cell</td></tr></tbody></table>` +
		`<table><tr><td>1</td></tr><tr><td>2</td></tr><tr><td>3</td></tr></table>` +
		`<pre>code block</pre><section><span id="stray">wrapped</span></section>` +
		`<h2>References</h2><p>gone</p><h3>Under it</h3><p>gone too</p><h2>Last</h2><p>kept</p>`
	want := `<p>cited</p><h1 id="one">One</h1><h2 id="two"><span id="Two">Two</span></h2><h3 id="three">Three</h3>` +
		`<h4>Four</h4><p>Text <b>b</b> <i>i</i> <strong>s</strong> <em>e</em> <code>c</code><br/>after</p>` +
		`<p><a data-page="Foo" href="http://wiki.test/wiki/Foo">relative</a> <a href="#Two">inside</a> ` +
		`<a href="http://example.com/x">web</a> <a>script</a> <a>mail</a></p>` +
		`<ul><li>item</li></ul><ol><li>step</li></ol><hr/>` +
		`<table><thead><tr><th>head</th></tr></thead><tbody><tr><td colspan="2">cell</td></tr></tbody></table>` +
		`<details><summary>Table, 3 rows</summary><table><tbody><tr><td>1</td></tr><tr><td>2</td></tr>` +
		`<tr><td>3</td></tr></tbody></table></details><pre>code block</pre><span>wrapped</span><h2>Last</h2><p>kept</p>`
	if got := Sanitize(input, &url.URL{Scheme: "http", Host: "wiki.test"}); got != want {
		t.Errorf("Sanitize =\n%s\nwant\n%s", got, want)
	}
}

// TestFindPagesRanksTitles checks the order of the pages for a script file in a database and for a GUI file, through
// the service over a kept listing.
func TestFindPagesRanksTitles(t *testing.T) {
	wik, ws := openService(t)
	titles := []string{
		"Event modding", "Scripting", "Scripted effects", "GUI modding", "Script", "Advanced scripted effects tips",
		"Scripted effect", "GUI",
	}
	listing := Listing{RefreshedAt: time.Now()}
	for _, title := range titles {
		listing.Pages = append(listing.Pages, Summary{Title: title, Category: "Modding"})
	}
	client := wik.resolveClient(ws)
	if err := app.SaveJSON(client.resolveListing(), listing); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want []string
	}{
		{
			filepath.Join(resolveFixture(t, "ck3"), "game", "common", "scripted_effects", "v_effects.txt"),
			[]string{"Scripted effect", "Scripted effects", "Advanced scripted effects tips", "Script", "Scripting"},
		},
		{filepath.Join(resolveFixture(t, "ck3"), "game", "gui", "window.gui"), []string{"GUI", "GUI modding"}},
	}
	for _, test := range tests {
		pages, err := wik.FindPages(test.path)
		var got []string
		for _, page := range pages {
			got = append(got, page.Title)
		}
		if err != nil || !slices.Equal(got, test.want) {
			t.Errorf("FindPages(%s) = %q, %v, want %q", filepath.Base(test.path), got, err, test.want)
		}
	}
}

// TestListNotesOrdersNewestFirst checks the order of the patch notes, newest first, which lists a minor version's
// hotfix notes after its release and 1.10 before 1.9, and marks the pages that have a Modding heading.
func TestListNotesOrdersNewestFirst(t *testing.T) {
	wik, ws := openService(t)
	client := wik.resolveClient(ws)
	const minor = "1.30"
	listing := Listing{Versions: []string{"1.9", minor, "1.30.X", "1.10"}, RefreshedAt: time.Now()}
	if err := app.SaveJSON(client.resolveListing(), listing); err != nil {
		t.Fatal(err)
	}
	keepPage(t, client, `<h2><span id="Modding">Modding</span></h2><p>text</p>`, "Patch 1.9")
	keepPage(t, client, `<h2>Balance</h2><p>some modding is mentioned in a <b>paragraph</b>, released on 2026-09-30</p>`,
		"Patch 1.30")
	keepPage(t, client, `<h3>Modding changes</h3>`, "Patch 1.30.X")
	notes, err := wik.ListNotes()
	want := []Note{
		{minor, "", "2026-09-30", "Patch 1.30", false}, {"1.30.X", minor, "", "Patch 1.30.X", true},
		{"1.10", "", "", "Patch 1.10", false}, {"1.9", "", "", "Patch 1.9", true},
	}
	if err != nil || !slices.Equal(notes, want) {
		t.Errorf("ListNotes = %+v, %v, want %+v", notes, err, want)
	}
}

// TestResolveClientFollowsGame checks that the service keeps one client for a game and makes another for the next.
func TestResolveClientFollowsGame(t *testing.T) {
	wik := &Service{Workspace: &workspace.Service{Config: &app.ConfigFile{Dirs: app.Dirs{Cache: t.TempDir()}}}}
	ck3 := &workspace.Session{Game: game.LookupGame("ck3")}
	first := wik.resolveClient(ck3)
	if wik.resolveClient(ck3) != first {
		t.Error("a second call for the same game made a new client")
	}
	vic3 := wik.resolveClient(&workspace.Session{Game: game.LookupGame("vic3")})
	if vic3 == first || vic3.game.ID != "vic3" {
		t.Errorf("a workspace of another game kept the client of %s", vic3.game.ID)
	}
}

// TestClientRefreshesFromServer checks the client against a wiki that serves the modding category in two answers, a
// patch category, the pages' HTML and a missing page. A page that is already cached is not fetched again.
func TestClientRefreshesFromServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if request.Header.Get("User-Agent") != userAgent {
			t.Errorf("user agent = %q", request.Header.Get("User-Agent"))
		}
		switch {
		case query.Get("action") == "parse" && query.Get("page") == "Missing":
			_, _ = writer.Write([]byte(`{"error":{"code":"missingtitle","info":"no such page"}}`))
		case query.Get("action") == "parse":
			text := `<script>x</script><h2>Modding</h2><p>` + query.Get("page") + `</p><a href="/wiki/Foo">link</a>`
			_, _ = fmt.Fprintf(writer, `{"parse":{"title":%q,"text":%q}}`, query.Get("page"), text)
		case query.Get("cmtitle") == "Category:Modding" && query.Get("cmcontinue") == "":
			_, _ = writer.Write([]byte(`{"continue":{"cmcontinue":"next","continue":"-||"},
				"query":{"categorymembers":[{"title":"Traits"}]}}`))
		case query.Get("cmtitle") == "Category:Modding":
			_, _ = writer.Write([]byte(`{"query":{"categorymembers":[{"title":"Decisions"}]}}`))
		default:
			_, _ = writer.Write([]byte(`{"query":{"categorymembers":[{"title":"Patches"},{"title":"Patch 1.20"},
				{"title":"Patch 1.1.X"},{"title":"Patch notes"}]}}`))
		}
	}))
	defer server.Close()

	client := NewClient(t.TempDir(), &game.Game{ID: "test", WikiAPI: server.URL + "/api.php"})
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	keepPage(t, client, "<p>kept</p>", "Traits")
	var lastDone, lastTotal int
	if err := client.Refresh(func(done, total int) { lastDone, lastTotal = done, total }); err != nil {
		t.Fatal(err)
	}
	if lastDone != 3 || lastTotal != 3 {
		t.Errorf("progress ended at %d of %d, want 3 of 3", lastDone, lastTotal)
	}
	listing, ok := client.LoadListing()
	if !ok || len(listing.Pages) != 2 || listing.Pages[1] != (Summary{Title: "Decisions", Category: "Modding"}) ||
		!slices.Equal(listing.Versions, []string{"1.20", "1.1.X"}) {
		t.Errorf("listing = %+v, %v", listing, ok)
	}
	if page, ok := client.LoadPage("Traits"); !ok || page.HTML != "<p>kept</p>" {
		t.Errorf("a fresh page was fetched again: %+v, %v", page, ok)
	}
	want := `<h2>Modding</h2><p>Decisions</p><a data-page="Foo" href="` + server.URL + `/wiki/Foo">link</a>`
	if page, ok := client.LoadPage("Decisions"); !ok || page.HTML != want {
		t.Errorf("page = %+v, %v, want %s", page, ok, want)
	}
	if _, err := client.FetchPage("Missing"); err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Errorf("FetchPage of a missing page returned %v", err)
	}
}

// TestRefreshEmitsUpdatedWithoutWorkspace checks that a refresh with no workspace open returns the error and still
// sends wiki:updated.
func TestRefreshEmitsUpdatedWithoutWorkspace(t *testing.T) {
	newApp()
	sent := make(chan struct{}, 1)
	remove := application.Get().Event.On(EventUpdated, func(*application.CustomEvent) { sent <- struct{}{} })
	defer remove()
	wik := &Service{Workspace: &workspace.Service{}}
	if err := wik.Refresh(); err == nil {
		t.Error("Refresh with no workspace open returned no error")
	}
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Error("Refresh did not send wiki:updated")
	}
}

// TestBuildLexiconListsEffectsAndFunctions checks the rows of effects, of scopes and of data functions over the CK3
// fixture install: one row for each declared effect or function and each scripted effect, in name order, with the
// usage and the places of its examples, counted from one; and an error for a class the lexicon does not have.
func TestBuildLexiconListsEffectsAndFunctions(t *testing.T) {
	wik, ws := openService(t)
	effects, err := wik.BuildLexicon("effect")
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	for _, command := range ws.Schema.Commands {
		if slices.Contains(command.Declared, game.DeclaredEffect) {
			declared++
		}
	}
	mods, scripted := ws.Mods.Load(), map[string]bool{}
	for _, site := range mods.ListDefinitions(ws.Schema.Effects) {
		scripted[mods.ToReference(site).Name] = true
	}
	if len(effects) == 0 || len(effects) != declared+len(scripted) || !slices.IsSortedFunc(effects, func(first, second LexiconRow) int {
		return strings.Compare(first.Name, second.Name)
	}) {
		t.Fatalf("%d effect rows for %d declared and %d scripted effects, sorted by name", len(effects), declared, len(scripted))
	}
	examples := 0
	for _, row := range effects {
		if row.Defined != nil {
			if !scripted[row.Name] || !strings.HasPrefix(row.Usage, row.Name+" = ") {
				t.Errorf("scripted row %+v", row)
			}
			continue
		}
		command := ws.Schema.Commands[row.Name]
		if row.Class != "effect" || row.Usage != command.Usage || row.Blurb != command.Blurb ||
			len(row.Examples) != len(command.Examples) {
			t.Errorf("row %+v does not match command %+v", row, command)
		}
		for i, example := range command.Examples {
			examples++
			if row.Examples[i].Line != example.Line+1 || row.Examples[i].Place.Path != example.Path {
				t.Errorf("%s example %d = %+v, want %+v", row.Name, i, row.Examples[i], example)
			}
		}
	}
	if examples == 0 {
		t.Error("no effect has an example")
	}
	checkFunctions(t, wik, ws)
	if scopes, err := wik.BuildLexicon("scope"); err != nil || len(scopes) != len(ws.Schema.Scopes) {
		t.Errorf("%d scope rows for %d scopes, %v", len(scopes), len(ws.Schema.Scopes), err)
	}
	if _, err := wik.BuildLexicon("modifier"); err == nil {
		t.Error("BuildLexicon accepted a class the lexicon does not have")
	}
}

// checkFunctions checks that the function rows are one for each data function of the schema, named by their type.
func checkFunctions(t *testing.T, wik *Service, ws *workspace.Session) {
	t.Helper()
	rows, err := wik.BuildLexicon("function")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, dataType := range ws.Schema.DataTypes {
		count += len(dataType.Functions)
	}
	if len(rows) == 0 || len(rows) != count {
		t.Fatalf("%d function rows for %d data functions", len(rows), count)
	}
	for _, row := range rows {
		typeName, functionName, ok := strings.Cut(row.Name, ".")
		function := ws.Schema.DataTypes[typeName].Functions[functionName]
		if !ok || row.Class != "function" || row.Target != function.Returns || row.Blurb != function.Blurb ||
			len(row.Examples) != len(function.Examples) {
			t.Fatalf("row %+v does not match function %+v of %s", row, function, typeName)
		}
		for i, example := range function.Examples {
			if row.Examples[i].Line != example.Line+1 || row.Examples[i].Place.Path != example.Path {
				t.Errorf("%s example %d = %+v, want %+v", row.Name, i, row.Examples[i], example)
			}
		}
	}
}

// TestRefreshInstalls refreshes the real wiki of each game whose PMT_TEST_INSTALL_<GAME> is set.
func TestRefreshInstalls(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			if os.Getenv(variable) == "" {
				t.Skipf("%s is not set", variable)
			}
			client := NewClient(t.TempDir(), game.LookupGame(gameID))
			pages := 0
			start := time.Now()
			if err := client.Refresh(func(done, _ int) { pages = done }); err != nil {
				t.Fatal(err)
			}
			listing, _ := client.LoadListing()
			t.Logf("%d pages read in %s: %d modding pages, %d patch versions", pages,
				time.Since(start).Round(time.Second), len(listing.Pages), len(listing.Versions))
		})
	}
}
