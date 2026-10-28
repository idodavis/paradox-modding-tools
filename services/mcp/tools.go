package mcp

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/search"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// A position is a place in a file that a tool asks about.
type position struct {
	Path   string `json:"path" jsonschema:"the file's absolute path"`
	Line   int    `json:"line" jsonschema:"the line, counted from one"`
	Column int    `json:"column" jsonschema:"the column, counted from one"`
}

// A document is a file a tool reads.
type document struct {
	Path string `json:"path" jsonschema:"the file's absolute path"`
}

// A draft is a file and, if the agent has one, the text it would write there.
type draft struct {
	Path string `json:"path" jsonschema:"the file's absolute path"`
	Text string `json:"text,omitempty" jsonschema:"text to judge in place of the file's own; leave it out for the file as it is"`
}

// A textQuery is a text to find and the most answers wanted.
type textQuery struct {
	Query string `json:"query" jsonschema:"the text to find"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most answers wanted; 50 if left out"`
}

// A findingFilter picks Health's findings by mod, by check and by level.
type findingFilter struct {
	Mod   string `json:"mod,omitempty" jsonschema:"a mod's id: only the findings about it"`
	Code  string `json:"code,omitempty" jsonschema:"a check's id from list_codes, such as wrong-scope or coverage"`
	Level string `json:"level,omitempty" jsonschema:"serious, warning or info"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most findings wanted; 50 if left out"`
}

// A graphRoot is what an Event Graph is drawn from, and how.
type graphRoot struct {
	Root   string        `json:"root" jsonschema:"a node's id from find_roots, such as event:my_mod.0001"`
	Filter *graph.Filter `json:"filter,omitempty" jsonschema:"what is drawn; the Event Graph's defaults if left out"`
}

// A node names one node of an Event Graph.
type node struct {
	ID string `json:"id" jsonschema:"the node's id, as find_roots and build_graph give it"`
}

// A pageTitle names one page of the game's wiki.
type pageTitle struct {
	Title string `json:"title" jsonschema:"the page's title, as search_pages gives it"`
}

// A lexiconFilter picks rows of the Lexicon.
type lexiconFilter struct {
	Class string `json:"class" jsonschema:"effect, trigger, link, scope or function"`
	Query string `json:"query,omitempty" jsonschema:"text the row's name holds"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most rows wanted; 50 if left out"`
}

// A textSearch is a text search over the workspace's files and the most hits wanted.
type textSearch struct {
	Query search.Query `json:"query"`
	Limit int          `json:"limit,omitempty" jsonschema:"the most hits wanted; 200 if left out"`
}

// A guideTopic names one topic of PMT's guide.
type guideTopic struct {
	Topic string `json:"topic,omitempty" jsonschema:"pages, workflow or workspace; leave it out for the list"`
}

// A renaming is a rename of the name at a position.
type renaming struct {
	Path   string `json:"path" jsonschema:"the file's absolute path"`
	Line   int    `json:"line" jsonschema:"the line, counted from one"`
	Column int    `json:"column" jsonschema:"the column, counted from one"`
	Text   string `json:"text" jsonschema:"the new name"`
	Token  string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// An entryEdit replaces the whole text of one entry of a file.
type entryEdit struct {
	Path  string `json:"path" jsonschema:"the file's absolute path"`
	Entry string `json:"entry" jsonschema:"the entry's name, as list_symbols gives it"`
	Text  string `json:"text" jsonschema:"the entry's new text, from its key to its closing brace"`
	Token string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// A scaffolding is a mod's missing localization keys in a language.
type scaffolding struct {
	Mod      string `json:"mod" jsonschema:"the mod's id"`
	Language string `json:"language" jsonschema:"the language, such as french"`
	Token    string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// A Preview is a write tool's answer before it changes anything: the change it would make, and the token that makes
// it.
type Preview struct {
	Change any    `json:"change"`
	Token  string `json:"token"`
}

// defaultLimit is how many answers a tool gives when it is not told.
const defaultLimit = 50

// The guide's topics. Each says what PMT does in the words its pages use.
const (
	guideTopics = `PMT's guide has three topics:
- pages: each page of PMT and its controls, and what show can bring to the front on it.
- workflow: how to make a change with PMT's tools, from finding a name to checking the file.
- workspace: the open workspace's game, its mods in load order with their folders, and the game's folders.`
	guideWorkflow = `How to make a change with PMT:
1. Find what to change. find_symbols finds a definition by its name in the mods and the game; definition and ` +
		`references follow a name; list_symbols lists a file's entries; search_text searches the files' text. ` +
		`read_file reads any file of the workspace or the game, with the user's unsaved text as its draft.
2. Learn what is valid. hover says what a key or a value is, complete lists what fits at a position, and signature ` +
		`what an effect or a macro takes. build_lexicon lists the game's effects, triggers, links, scopes and ` +
		`functions; search_pages and read_page read the game's wiki.
3. Write it in the mod that should own it, never in the game's files: with your own file tools, or with ` +
		`apply_edit, rename and write_scaffold, which preview first.
4. Check it. diagnose the file, or your text before you write it. build_report gives Health's findings: how the ` +
		`mods touch the same things, each mod's own problems, and each language's coverage of its localization ` +
		`(the coverage check). Fix the problems you brought in.
5. Show the user. show opens the file at the lines you changed, or the page that shows the result, such as the ` +
		`Event Graph of an event you wired.

Events: find_roots finds an event, an on_action, a decision or a scripted effect; build_graph draws what it fires ` +
		`and what fires it, and build_detail gives one node's texts, script and links.
Localization: a mod's localization files are YAML under localization/<language>/, each beginning with ` +
		`l_<language>: and saved with a byte order mark. write_scaffold writes the keys a language lacks, with the ` +
		`base language's text as the placeholder.`
	guideQueries = `What show brings to the front, by page id and its query:
- workspace-ide: give path, with startLine and endLine to select lines; drawable and picks draw a window of a ` +
		`.gui file in the Visual view at the states picked; lookUp looks a word up in the Wiki pane.
- event-graph: root (a node id), node (a node id, selected with its path drawn) and filters (an Event Graph ` +
		`filter as JSON).
- health: view (compatibility, mods or log), mod (a mod's id), code (a check's id), row and name.
- patch-notes: patch (a version's notes) and find (text found in them).
- release: mod (a mod's id), compare (yes to open the compare with Steam) and listing.`
)

// addEditorTools adds the tools that answer as PMT's editor does: a file's problems and names, what is valid at a
// position, and the files' text.
func (mcp *Service) addEditorTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "search_text", Description: "Searches the text of the workspace's " +
		"files, the user's unsaved text included."}, mcp.searchText)
	sdk.AddTool(server, &sdk.Tool{Name: "read_file", Description: "Reads a file of the workspace or the game, " +
		"with where it sits and the user's unsaved text of it as its draft."},
		answer(func(request document) (workspace.FileText, error) {
			return mcp.Workspace.Read(request.Path)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "diagnose", Description: "Lists a file's problems as PMT's editor shows " +
		"them, or the problems of text you would write there."}, answer(func(request draft) ([]lsp.Diagnostic, error) {
		if request.Text != "" {
			return mcp.LSP.DiagnoseText(request.Path, request.Text)
		}
		return mcp.LSP.Diagnose(request.Path)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "definition", Description: "Finds where the name at a position is " +
		"defined, in the mods and the game."}, answer(func(request position) ([]lsp.Location, error) {
		return mcp.LSP.Definition(request.Path, request.Line, request.Column)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "references", Description: "Finds every place the name at a position " +
		"is defined or used."}, answer(func(request position) ([]lsp.Location, error) {
		return mcp.LSP.References(request.Path, request.Line, request.Column)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_symbols", Description: "Lists a file's entries, nested as " +
		"they are written, or a localization file's keys."}, answer(func(request document) ([]lsp.Symbol, error) {
		return mcp.LSP.ListSymbols(request.Path)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "find_symbols", Description: "Finds definitions by their name in the " +
		"mods and the game."}, answer(func(request textQuery) ([]lsp.WorkspaceSymbol, error) {
		return mcp.LSP.FindSymbols(request.Query, cmp.Or(request.Limit, defaultLimit))
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "complete", Description: "Lists what can be written at a position: " +
		"the keys, values and names that fit there."}, answer(func(request position) ([]lsp.Completion, error) {
		return mcp.LSP.Complete(request.Path, request.Line, request.Column)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "hover", Description: "Says what the key or value at a position is " +
		"and where it is defined, as PMT's hover card does."}, answer(func(request position) (*lsp.Card, error) {
		return mcp.LSP.Hover(request.Path, request.Line, request.Column)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "signature", Description: "Says what the effect or macro being " +
		"filled in at a position takes."}, answer(func(request position) (*lsp.Signature, error) {
		return mcp.LSP.Signature(request.Path, request.Line, request.Column)
	}))
}

// addPageTools adds the tools that answer as PMT's pages do: Health, the Event Graph and the game's wiki.
func (mcp *Service) addPageTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "build_report", Description: "Gives Health's findings for the " +
		"workspace's load order: how the mods touch the same things, each mod's own problems, and each " +
		"language's localization coverage (the coverage check)."}, answer(mcp.buildReport))
	sdk.AddTool(server, &sdk.Tool{Name: "list_overview", Description: "Counts what each mod holds: its " +
		"definitions by kind, its files and its localization keys."}, answer(func(struct{}) ([]health.Overview, error) {
		return mcp.Health.ListOverview()
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_codes", Description: "Lists Health's checks: each one's id, " +
		"question, level and message."}, answer(func(struct{}) ([]lsp.Code, error) { return mcp.Health.ListCodes(), nil }))
	sdk.AddTool(server, &sdk.Tool{Name: "find_roots", Description: "Finds the events, on_actions, decisions " +
		"and scripted effects an Event Graph can be drawn from, by id, namespace or number."},
		answer(func(request textQuery) ([]graph.Node, error) {
			return mcp.Graph.FindRoots(request.Query, cmp.Or(request.Limit, defaultLimit)), nil
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "build_graph", Description: "Draws the Event Graph of a root: what it " +
		"fires and what fires it, step by step."}, answer(func(request graphRoot) (graph.Graph, error) {
		return mcp.Graph.BuildGraph(request.Root, lo.FromPtrOr(request.Filter, mcp.Graph.Defaults()))
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "build_detail", Description: "Gives one Event Graph node's detail: its " +
		"texts, what it fires and what fires it, what it reads, and its script."},
		answer(func(request node) (graph.Detail, error) { return mcp.Graph.BuildDetail(request.ID) }))
	sdk.AddTool(server, &sdk.Tool{Name: "search_pages", Description: "Finds the game wiki's modding pages " +
		"that hold a text."}, answer(func(request textQuery) ([]wiki.Summary, error) {
		pages, err := mcp.Wiki.SearchPages(request.Query)
		return pages[:min(len(pages), cmp.Or(request.Limit, defaultLimit))], err
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "read_page", Description: "Reads a page of the game's wiki, as HTML."},
		answer(func(request pageTitle) (wiki.Page, error) { return mcp.Wiki.ReadPage(request.Title) }))
	sdk.AddTool(server, &sdk.Tool{Name: "build_lexicon", Description: "Lists the game's effects, triggers, " +
		"links, scopes or functions, each with what it does, where it runs and examples from the game's files."},
		answer(mcp.buildLexicon))
}

// addAppTools adds the tools of PMT itself: its guide, what the user has in front of them, what an agent asks PMT to
// show, and the changes that preview first.
func (mcp *Service) addAppTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "read_guide", Description: "Reads PMT's guide: its pages and their " +
		"controls, how to make a change with PMT, or the open workspace's mods and folders."}, answer(mcp.readGuide))
	sdk.AddTool(server, &sdk.Tool{Name: "describe_view", Description: "Says what the user has in front of them in " +
		"PMT: the page shown, what it shows, and the file, lines or item they have selected."}, answer(mcp.describeView))
	sdk.AddTool(server, &sdk.Tool{Name: "rename", Description: "Renames the name at a position in every mod " +
		"file. Without a token it answers with the edits and a token; called again with the token, it makes " +
		"them."}, answer(mcp.rename))
	sdk.AddTool(server, &sdk.Tool{Name: "apply_edit", Description: "Replaces one entry of a mod's file with " +
		"new text. Without a token it answers with the edit and a token; called again with the token, it makes " +
		"it."}, answer(mcp.applyEdit))
	sdk.AddTool(server, &sdk.Tool{Name: "write_scaffold", Description: "Writes the localization keys a mod " +
		"lacks in a language, with the base language's text as the placeholder, at the end of the language's " +
		"files, creating those that do not exist. Without a token it answers with the files and a token; called " +
		"again with the token, it writes them."}, answer(mcp.writeScaffold))
	sdk.AddTool(server, &sdk.Tool{Name: "show", Description: "Brings a file, a page or a Wiki look-up to the " +
		"front in PMT's main window. read_guide's pages topic says what each page can show."}, answer(mcp.show))
}

// answer makes a tool's handler from a function that answers from the tool's input alone.
func answer[In, Out any](respond func(In) (Out, error)) sdk.ToolHandlerFor[In, any] {
	return func(_ context.Context, _ *sdk.CallToolRequest, input In) (*sdk.CallToolResult, any, error) {
		output, err := respond(input)
		return nil, output, err
	}
}

// readGuide returns one topic of PMT's guide, or with no topic the list of topics. It returns an error for a topic
// the guide does not have.
func (mcp *Service) readGuide(request guideTopic) (string, error) {
	switch request.Topic {
	case "":
		return guideTopics, nil
	case "pages":
		return mcp.describePages(), nil
	case "workflow":
		return guideWorkflow, nil
	case "workspace":
		return mcp.describeWorkspace()
	}
	return "", fmt.Errorf("PMT's guide has no topic %q; its topics are pages, workflow and workspace", request.Topic)
}

// describePages returns the guide's pages topic: each page's help as the page itself gives it, then what show can
// bring to the front on each.
func (mcp *Service) describePages() string {
	mcp.mu.Lock()
	defer mcp.mu.Unlock()
	var guide strings.Builder
	for _, page := range mcp.pages {
		fmt.Fprintf(&guide, "# %s (%s)\n\n%s\n\n", page.Title, page.ID, strings.Join(page.Help, "\n\n"))
	}
	guide.WriteString(guideQueries)
	return guide.String()
}

// describeWorkspace returns the guide's workspace topic: the open workspace's game and install, its mods in load
// order with their folders, and the game's folders with what each holds. It returns an error if no workspace is open.
func (mcp *Service) describeWorkspace() (string, error) {
	ws, err := mcp.Workspace.Session()
	if err != nil {
		return "", err
	}
	state := mcp.Workspace.State()
	var guide strings.Builder
	fmt.Fprintf(&guide, "Game: %s %s, installed at %s. Its files are read only.\n", state.Game.Name,
		state.Install.Version, state.Install.Path)
	fmt.Fprintf(&guide, "Language: %s, beside the base language, %s.\n\nMods in load order:\n", state.Language,
		game.BaseLanguage)
	for _, mod := range state.Mods {
		fmt.Fprintf(&guide, "- %s (id %s): %s%s\n", mod.Descriptor.Name, mod.ID, mod.Dir,
			lo.Ternary(mod.Broken, ", a folder that is missing", ""))
	}
	guide.WriteString("\nThe game's folders and what each holds:\n")
	databases := lo.Filter(slices.Collect(maps.Values(ws.Schema.Databases)), func(database *schema.Database, _ int) bool {
		return database.Folder != ""
	})
	slices.SortFunc(databases, func(a, b *schema.Database) int { return strings.Compare(a.Folder, b.Folder) })
	for _, database := range databases {
		fmt.Fprintf(&guide, "- %s: %s\n", database.Folder, game.DescribeDefinition(database.Name, true))
	}
	return guide.String(), nil
}

// buildReport returns Health's findings for the workspace's load order that pass the filter, at most its limit.
func (mcp *Service) buildReport(filter findingFilter) (health.Report, error) {
	report, err := mcp.Health.BuildReport(nil)
	findings := lo.Filter(report.Findings, func(finding health.Finding, _ int) bool {
		modMatches := filter.Mod == "" || finding.Origin == filter.Mod || slices.Contains(finding.Mods, filter.Mod)
		codeMatches := filter.Code == "" || finding.Code == filter.Code
		levelMatches := filter.Level == "" || finding.Level == filter.Level
		return modMatches && codeMatches && levelMatches
	})
	report.Findings = findings[:min(len(findings), cmp.Or(filter.Limit, defaultLimit))]
	return report, err
}

// buildLexicon returns the Lexicon's rows of a class whose name holds the filter's text, at most its limit.
func (mcp *Service) buildLexicon(filter lexiconFilter) ([]wiki.LexiconRow, error) {
	rows, err := mcp.Wiki.BuildLexicon(filter.Class)
	query := strings.ToLower(filter.Query)
	rows = lo.Filter(rows, func(row wiki.LexiconRow, _ int) bool { return strings.Contains(strings.ToLower(row.Name), query) })
	return rows[:min(len(rows), cmp.Or(filter.Limit, defaultLimit))], err
}

// searchText answers search_text: each file's hits, at most the request's limit in all. It stops when the agent
// cancels the call.
func (mcp *Service) searchText(ctx context.Context, _ *sdk.CallToolRequest, request textSearch) (
	*sdk.CallToolResult, any, error,
) {
	ws, err := mcp.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}
	hits, _, err := search.CollectText(ws, request.Query, cmp.Or(request.Limit, 200), ctx.Done())
	return nil, hits, err
}

// rename previews giving the name at a position a new name in every mod file, or makes the change a token names. It
// returns an error if no name that only the mods define is at the position.
func (mcp *Service) rename(request renaming) (any, error) {
	if request.Token != "" {
		return mcp.confirm(request.Token)
	}
	renamable, err := mcp.LSP.PrepareRename(request.Path, request.Line, request.Column)
	if err != nil || renamable == nil {
		return nil, errors.Join(err, errors.New("no name that only the mods define is at this position"))
	}
	edits, err := mcp.LSP.Rename(request.Path, request.Line, request.Column, request.Text)
	if err != nil {
		return nil, err
	}
	if err := mcp.checkSaved(slices.Collect(maps.Keys(edits))...); err != nil {
		return nil, err
	}
	return mcp.preview(edits, func() (any, error) { return mcp.writeEdits(edits) }), nil
}

// applyEdit previews replacing one entry of a file with new text, or makes the change a token names. It returns an
// error if the file has no entry of that name, or several.
func (mcp *Service) applyEdit(request entryEdit) (any, error) {
	if request.Token != "" {
		return mcp.confirm(request.Token)
	}
	if err := mcp.checkSaved(request.Path); err != nil {
		return nil, err
	}
	symbols, err := mcp.LSP.ListSymbols(request.Path)
	if err != nil {
		return nil, err
	}
	entries := collectEntries(symbols, request.Entry)
	if len(entries) != 1 {
		return nil, fmt.Errorf("the file has %d entries named %s; list_symbols lists them", len(entries), request.Entry)
	}
	edits := map[string][]lsp.Edit{request.Path: {{Range: entries[0].Range, Text: request.Text}}}
	return mcp.preview(edits, func() (any, error) { return mcp.writeEdits(edits) }), nil
}

// collectEntries returns the symbols of the given name among a file's symbols, nested ones included.
func collectEntries(symbols []lsp.Symbol, entry string) []lsp.Symbol {
	var entries []lsp.Symbol
	for _, symbol := range symbols {
		if symbol.Name == entry {
			entries = append(entries, symbol)
		}
		entries = append(entries, collectEntries(symbol.Children, entry)...)
	}
	return entries
}

// writeScaffold previews writing the localization keys a mod lacks in a language, by how many keys each file gets, or
// writes those a token names. It returns an error if the workspace has no such mod, the mod lacks no key, or a file
// it would change has text the user has not saved.
func (mcp *Service) writeScaffold(request scaffolding) (any, error) {
	if request.Token != "" {
		return mcp.confirm(request.Token)
	}
	ws, err := mcp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mod, ok := mcp.Workspace.LookupMod(request.Mod)
	if !ok {
		return nil, fmt.Errorf("the workspace has no mod %s", request.Mod)
	}
	scaffolds := health.ListScaffolds(ws.Mods.Load(), mod, request.Language)
	if len(scaffolds) == 0 {
		return nil, fmt.Errorf("%s lacks no localization key in %s", request.Mod, request.Language)
	}
	if err := mcp.checkSaved(slices.Collect(maps.Keys(scaffolds))...); err != nil {
		return nil, err
	}
	counts := lo.MapValues(scaffolds, func(messages []loc.Message, _ string) int { return len(messages) })
	return mcp.preview(counts, func() (any, error) { return mcp.Health.WriteScaffold(request.Mod, request.Language) }), nil
}

// checkSaved returns an error naming the first of the files whose text in the editor the user has not saved.
func (mcp *Service) checkSaved(paths ...string) error {
	for _, target := range paths {
		if text, err := mcp.Workspace.Read(target); err == nil && text.Draft != "" {
			return fmt.Errorf("%s has changes the user has not saved; ask them to save or discard them first", target)
		}
	}
	return nil
}

// writeEdits applies the edits to each file's text on disk and writes it in its encoding, and returns the files
// written. It returns an error, with the files written so far, if a file cannot be read or written.
func (mcp *Service) writeEdits(edits map[string][]lsp.Edit) ([]string, error) {
	paths := slices.Sorted(maps.Keys(edits))
	for position, target := range paths {
		text, err := files.ReadText(target)
		if err != nil {
			return paths[:position], err
		}
		lines := jomini.NewLines(text.Text)
		text.Text = jomini.ApplyEdits(text.Text, lo.Map(edits[target], func(edit lsp.Edit, _ int) jomini.Edit {
			return jomini.Edit{Span: lsp.ToSpan(text.Text, lines, edit.Range), Text: edit.Text}
		}))
		if err := mcp.Workspace.Write(target, text); err != nil {
			return paths[:position], err
		}
	}
	return paths, nil
}

// preview keeps a change under a new token, and returns what the change would be with the token that makes it.
func (mcp *Service) preview(change any, apply func() (any, error)) Preview {
	token := rand.Text()
	mcp.mu.Lock()
	defer mcp.mu.Unlock()
	if mcp.pending == nil {
		mcp.pending = map[string]func() (any, error){}
	}
	mcp.pending[token] = apply
	return Preview{Change: change, Token: token}
}

// confirm makes the change kept under a token, once. It returns an error if no change has the token.
func (mcp *Service) confirm(token string) (any, error) {
	mcp.mu.Lock()
	apply, ok := mcp.pending[token]
	delete(mcp.pending, token)
	mcp.mu.Unlock()
	if !ok {
		return nil, errors.New("no preview has this token; call the tool again without one for a new preview")
	}
	return apply()
}
