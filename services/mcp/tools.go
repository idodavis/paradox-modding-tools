// tools.go defines the tools the server gives agents and what each one does.

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

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/search"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

type position struct {
	Path   string `json:"path" jsonschema:"the file's absolute path"`
	Line   int    `json:"line" jsonschema:"the line, counted from one"`
	Column int    `json:"column" jsonschema:"the column, counted from one"`
}

type document struct {
	Path string `json:"path" jsonschema:"the file's absolute path"`
}

// draft is a file and, if the agent has one, the text it would write there.
type draft struct {
	Path string `json:"path" jsonschema:"the file's absolute path"`
	Text string `json:"text,omitempty" jsonschema:"text to judge in place of the file's own; leave it out for the file as it is"`
}

type textQuery struct {
	Query string `json:"query" jsonschema:"the text to find"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most answers wanted; 50 if left out"`
}

type findingFilter struct {
	Mod   string `json:"mod,omitempty" jsonschema:"a mod's id: only the findings about it"`
	Code  string `json:"code,omitempty" jsonschema:"a check's id from list_codes, such as wrong-scope or coverage"`
	Level string `json:"level,omitempty" jsonschema:"critical, error, warning or info"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most findings wanted; 50 if left out"`
}

// graphRoot is what an Event Graph is drawn from, and how.
type graphRoot struct {
	Root   string        `json:"root" jsonschema:"a node's id from find_roots, such as events:my_mod.0001"`
	Filter *graph.Filter `json:"filter,omitempty" jsonschema:"what is drawn; the Event Graph's defaults if left out"`
}

type node struct {
	ID string `json:"id" jsonschema:"the node's id, as find_roots and build_graph give it"`
}

type pageTitle struct {
	Title string `json:"title" jsonschema:"the page's title, as search_pages gives it"`
}

type lexiconFilter struct {
	Class string `json:"class" jsonschema:"effect, trigger, link, scope or function"`
	Query string `json:"query,omitempty" jsonschema:"text the row's name holds"`
	Limit int    `json:"limit,omitempty" jsonschema:"the most rows wanted; 50 if left out"`
}

type textSearch struct {
	Query search.Query `json:"query"`
	Limit int          `json:"limit,omitempty" jsonschema:"the most hits wanted; 200 if left out"`
}

type guideTopic struct {
	Topic string `json:"topic,omitempty" jsonschema:"pages, workflow, rules or workspace; leave it out for the list"`
}

type renaming struct {
	Path   string `json:"path" jsonschema:"the file's absolute path"`
	Line   int    `json:"line" jsonschema:"the line, counted from one"`
	Column int    `json:"column" jsonschema:"the column, counted from one"`
	Text   string `json:"text" jsonschema:"the new name"`
	Token  string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

type entryEdit struct {
	Path  string `json:"path" jsonschema:"the file's absolute path"`
	Entry string `json:"entry" jsonschema:"the entry's name, as list_symbols gives it"`
	Text  string `json:"text" jsonschema:"the entry's new text, from its key to its closing brace"`
	Token string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// scaffolding is a mod's missing localization keys in a language.
type scaffolding struct {
	Mod      string `json:"mod" jsonschema:"the mod's id"`
	Language string `json:"language" jsonschema:"the language, such as french"`
	Token    string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// Preview is a write tool's answer before it changes anything.
type Preview struct {
	Change any    `json:"change"`
	Token  string `json:"token"`
}

const defaultLimit = 50

// readsOnly marks a tool that changes no file.
var readsOnly = &sdk.ToolAnnotations{ReadOnlyHint: true}

func (s *Service) addEditorTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "search_text", Title: "Search the files' text", Annotations: readsOnly,
		Description: "Searches the text of the workspace's files, the user's unsaved text included."}, s.searchText)
	sdk.AddTool(server, &sdk.Tool{Name: "read_file", Title: "Read a file", Annotations: readsOnly,
		Description: "Reads a file of the workspace or the game, with where it sits and the user's unsaved text of " +
			"it as its draft."},
		answer(func(request document) (workspace.FileText, error) {
			if _, err := s.locateFile(request.Path); err != nil {
				return workspace.FileText{}, err
			}

			return s.Workspace.Read(request.Path)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "diagnose", Title: "Check a file for problems", Annotations: readsOnly,
		Description: "Lists a file's problems as PMT's editor shows them, or the problems of text you would write " +
			"there."}, answer(func(request draft) ([]lsp.Diagnostic, error) {
		if request.Text != "" {
			return s.LSP.DiagnoseText(request.Path, request.Text)
		}

		return s.LSP.Diagnose(request.Path)
	}))
	sdk.AddTool(server, &sdk.Tool{Name: "definition", Title: "Find where a name is defined", Annotations: readsOnly,
		Description: "Finds where the name at a position is defined, in the mods and the game."},
		answer(func(request position) ([]lsp.Site, error) {
			return s.LSP.Definition(request.Path, request.Line, request.Column)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "references", Title: "Find where a name is used", Annotations: readsOnly,
		Description: "Finds every place the name at a position is defined or used."},
		answer(func(request position) ([]lsp.Site, error) {
			return s.LSP.References(request.Path, request.Line, request.Column)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_symbols", Title: "List a file's entries", Annotations: readsOnly,
		Description: "Lists a file's entries, nested as they are written, or a localization file's keys."},
		answer(func(request document) ([]lsp.Symbol, error) {
			return s.LSP.ListSymbols(request.Path)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "find_symbols", Title: "Find definitions by name", Annotations: readsOnly,
		Description: "Finds definitions by their name in the mods and the game."},
		answer(func(request textQuery) ([]lsp.WorkspaceSymbol, error) {
			return s.LSP.FindSymbols(request.Query, false, resolveLimit(request.Limit, defaultLimit))
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "complete", Title: "List what fits at a position", Annotations: readsOnly,
		Description: "Lists what can be written at a position: the keys, values and names that fit there."},
		answer(func(request position) (lsp.CompletionList, error) {
			return s.LSP.Complete(request.Path, request.Line, request.Column)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "hover", Title: "Look up a name", Annotations: readsOnly,
		Description: "Says what the key or value at a position is and where it is defined, as PMT's hover card does."},
		answer(func(request position) (*lsp.Card, error) {
			return s.LSP.Hover(request.Path, request.Line, request.Column)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "signature", Title: "Look up what an effect takes", Annotations: readsOnly,
		Description: "Says what the effect or macro being filled in at a position takes."},
		answer(func(request position) (*lsp.Signature, error) {
			return s.LSP.Signature(request.Path, request.Line, request.Column)
		}))
}

func (s *Service) addPageTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "build_report", Title: "Get Health's findings", Annotations: readsOnly,
		Description: "Gives Health's findings for the workspace's load order: how the mods touch the same things, " +
			"each mod's own problems, and each language's localization coverage (the coverage check). The findings " +
			"come in groups, the compatibility and each mod, with the counts of each level and check; a finding's " +
			"count says how many it folds."}, answer(s.buildReport))
	sdk.AddTool(server, &sdk.Tool{Name: "list_overview", Title: "Count what each mod holds", Annotations: readsOnly,
		Description: "Counts what each mod holds: its definitions by kind, its files and its localization keys."},
		answer(func(struct{}) ([]health.Overview, error) {
			return s.Health.ListOverview()
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_codes", Title: "List Health's checks", Annotations: readsOnly,
		Description: "Lists Health's checks: each one's id, question, level and message."},
		answer(func(struct{}) ([]lsp.Code, error) { return s.Health.ListCodes(), nil }))
	sdk.AddTool(server, &sdk.Tool{Name: "find_roots", Title: "Find an event to draw", Annotations: readsOnly,
		Description: "Finds the events, on_actions, decisions, messages and scripted effects an Event Graph can be " +
			"drawn from, by id, namespace or number."},
		answer(func(request textQuery) ([]graph.Node, error) {
			return s.Graph.FindRoots(request.Query, resolveLimit(request.Limit, defaultLimit)), nil
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "build_graph", Title: "Draw an Event Graph", Annotations: readsOnly,
		Description: "Draws the Event Graph of a root: what it fires and what fires it, step by step, each firing " +
			"with the blocks it sits in and its delay."},
		answer(func(request graphRoot) (graph.Graph, error) {
			filter := s.Graph.Defaults()
			if request.Filter != nil {
				filter = *request.Filter
			}

			eventGraph, err := s.Graph.BuildGraph(request.Root, filter)
			if err != nil {
				return eventGraph, fmt.Errorf("%w; %s", err, nodeHint)
			}

			return eventGraph, nil
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "build_detail", Title: "Read an Event Graph node", Annotations: readsOnly,
		Description: "Gives one Event Graph node's detail: its texts, what it fires and what fires it, the game " +
			"rules it reads, the game's copy it replaces, and its script."}, answer(s.buildDetail))
	sdk.AddTool(server, &sdk.Tool{Name: "search_pages", Title: "Search the game's wiki", Annotations: readsOnly,
		Description: "Finds the game wiki's modding pages that hold a text."},
		answer(func(request textQuery) ([]wiki.Summary, error) {
			pages, err := s.Wiki.SearchPages(request.Query)

			return pages[:min(len(pages), resolveLimit(request.Limit, defaultLimit))], err
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "read_page", Title: "Read a wiki page", Annotations: readsOnly,
		Description: "Reads a page of the game's wiki, as HTML."},
		answer(func(request pageTitle) (wiki.Page, error) { return s.Wiki.ReadPage(request.Title) }))
	sdk.AddTool(server, &sdk.Tool{Name: "build_lexicon", Title: "Look up the game's effects and triggers",
		Annotations: readsOnly, Description: "Lists the game's effects, triggers, links, scopes or functions, each " +
			"with what it does, where it runs and examples from the game's files."}, answer(s.buildLexicon))
}

func (s *Service) addAppTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "read_guide", Title: "Read PMT's guide", Annotations: readsOnly,
		Description: "Reads PMT's guide: its pages and their controls, how to make a change with PMT, which copy " +
			"the game keeps of a definition in each folder, or the open workspace's mods and folders."},
		answer(s.readGuide))
	sdk.AddTool(server, &sdk.Tool{Name: "describe_view", Title: "See what is in front of you", Annotations: readsOnly,
		Description: "Says what the user has in front of them in PMT: the page shown, what it shows, and the file, " +
			"lines or item they have selected."}, answer(s.describeView))
	sdk.AddTool(server, &sdk.Tool{Name: "rename", Title: "Rename a name in the mods",
		Description: "Renames the name at a position in every mod file. Without a token it answers with the edits " +
			"and a token; called again with the token, it makes them."}, answer(s.rename))
	sdk.AddTool(server, &sdk.Tool{Name: "apply_edit", Title: "Change a file",
		Description: "Replaces one entry of a mod's file with new text. Without a token it answers with the edit " +
			"and a token; called again with the token, it makes it."}, answer(s.applyEdit))
	sdk.AddTool(server, &sdk.Tool{Name: "write_scaffold", Title: "Write a mod's missing localization",
		Description: "Writes the localization keys a mod lacks in a language, with the base language's text as the " +
			"placeholder, at the end of the language's files, creating those that do not exist. Without a token it " +
			"answers with the files and a token; called again with the token, it writes them."},
		answer(s.writeScaffold))
	sdk.AddTool(server, &sdk.Tool{Name: "show", Title: "Show something in PMT", Annotations: readsOnly,
		Description: "Brings a file, a page or a Wiki look-up to the front in PMT's main window. read_guide's pages " +
			"topic says what each page can show."}, answer(s.show))
}

// answer makes a tool's handler from a function that needs only the tool's
// input.
func answer[In, Out any](respond func(In) (Out, error)) sdk.ToolHandlerFor[In, any] {
	return func(_ context.Context, _ *sdk.CallToolRequest, input In) (*sdk.CallToolResult, any, error) {
		output, err := respond(input)

		return nil, output, err
	}
}

// resolveLimit returns the limit asked for, or the fallback if it is left out,
// zero or negative.
func resolveLimit(limit, fallback int) int {
	if limit < 1 {
		return fallback
	}

	return limit
}

// searchText answers search_text, and stops when the agent cancels the call.
func (s *Service) searchText(ctx context.Context, _ *sdk.CallToolRequest, request textSearch) (
	*sdk.CallToolResult, any, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}

	hits, _, err := search.CollectText(ws, request.Query, resolveLimit(request.Limit, 200), ctx.Done())

	return nil, hits, err
}

// buildReport returns Health's report with only the findings that pass the
// filter, up to its limit. The tallies still count every finding. It returns an
// error if the filter names a mod, check or level PMT does not have.
func (s *Service) buildReport(filter findingFilter) (health.Report, error) {
	var errs []error
	if filter.Mod != "" {
		errs = append(errs, s.checkMod(filter.Mod))
	}

	if filter.Code != "" {
		errs = append(errs, s.checkCode(filter.Code))
	}

	levels := make([]string, 0, len(lsp.Levels))
	for _, level := range lsp.Levels {
		levels = append(levels, level.ID)
	}

	if filter.Level != "" && !slices.Contains(levels, filter.Level) {
		errs = append(errs, fmt.Errorf("a finding's level is %s, not %q", lang.JoinList(levels, "or"), filter.Level))
	}

	if err := errors.Join(errs...); err != nil {
		return health.Report{}, err
	}

	report, err := s.Health.BuildReport(nil)
	room := resolveLimit(filter.Limit, defaultLimit)
	pick := func(findings []health.Finding) []health.Finding {
		matches := make([]health.Finding, 0, len(findings))
		for _, finding := range findings {
			modMatches := filter.Mod == "" || finding.Origin == filter.Mod || slices.Contains(finding.Mods, filter.Mod)
			codeMatches := filter.Code == "" || finding.Code == filter.Code
			levelMatches := filter.Level == "" || finding.Level == filter.Level
			if modMatches && codeMatches && levelMatches {
				matches = append(matches, finding)
			}
		}

		matches = matches[:min(len(matches), room)]
		room -= len(matches)

		return matches
	}

	report.Compatibility.Findings = pick(report.Compatibility.Findings)
	for position := range report.Mods {
		group := &report.Mods[position]
		group.Findings, group.Languages = pick(group.Findings), pick(group.Languages)
	}

	return report, err
}

// buildLexicon returns the rows of a class whose name holds the filter's text.
func (s *Service) buildLexicon(filter lexiconFilter) ([]wiki.LexiconRow, error) {
	rows, err := s.Wiki.BuildLexicon(filter.Class)
	query := strings.ToLower(filter.Query)

	matches := make([]wiki.LexiconRow, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Name), query) {
			matches = append(matches, row)
		}
	}

	return matches[:min(len(matches), resolveLimit(filter.Limit, defaultLimit))], err
}

const (
	guideTopics = `PMT's guide has four topics:
- pages: each page of PMT and its controls, and what show can bring to the front on it.
- workflow: how to make a change with PMT's tools, from finding a name to checking the file.
- rules: for each of the game's folders, which copy the game keeps when several definitions share a name, and what a ` +
		`mod writes to override one.
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
	guideShow = `What show brings to the front. A file in the IDE: path, with startLine and endLine to select lines, ` +
		`and for a .gui file drawable, a window drawn in the Visual view, and picks, the option each of its state ` +
		`groups is drawn at. A word looked up in the IDE's Wiki pane: lookUp. A page: page, its id, and query, which ` +
		`the pages below take; the others take none. show checks each and says what is wrong instead of showing it.`
)

// readGuide returns one topic of PMT's guide, or the list of topics. It
// returns an error for an unknown topic.
func (s *Service) readGuide(request guideTopic) (string, error) {
	switch request.Topic {
	case "":
		return guideTopics, nil
	case "pages":
		return s.describePages(), nil
	case "workflow":
		return guideWorkflow, nil
	case "rules":
		return s.describeRules()
	case "workspace":
		return s.describeWorkspace()
	}

	return "", fmt.Errorf("PMT's guide has no topic %q; its topics are pages, workflow, rules and workspace", request.Topic)
}

// describePages returns each page's help, then what show can bring to the
// front on each page.
func (s *Service) describePages() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var guide strings.Builder
	for _, page := range s.pages {
		fmt.Fprintf(&guide, "# %s (%s)\n\n%s\n\n", page.Title, page.ID, strings.Join(page.Help, "\n\n"))
	}

	guide.WriteString(guideShow)

	var pageIDs []string
	keysByPage := map[string][]string{}
	for _, row := range queryKeys {
		if _, ok := keysByPage[row.Page]; !ok {
			pageIDs = append(pageIDs, row.Page)
		}

		values := ""
		if len(row.Values) > 0 {
			values = lang.JoinList(row.Values, "or") + ": "
		}

		keysByPage[row.Page] = append(keysByPage[row.Page], fmt.Sprintf("%s (%s%s)", row.Key, values, row.Help))
	}

	for _, id := range pageIDs {
		fmt.Fprintf(&guide, "\n- %s: %s.", id, strings.Join(keysByPage[id], "; "))
	}

	return guide.String()
}

// describeRules returns which copy the open game keeps when definitions share
// a name, with the folders each rule covers. It returns an error if no
// workspace is open.
func (s *Service) describeRules() (string, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return "", err
	}

	folders := map[string][]string{}
	for _, database := range listFolderDatabases(ws) {
		sentence := database.Precedence.Describe()
		folders[sentence] = append(folders[sentence], database.Declaration.Folder)
	}

	sentences := slices.SortedFunc(maps.Keys(folders), func(first, second string) int {
		return cmp.Or(len(folders[second])-len(folders[first]), strings.Compare(first, second))
	})

	var guide strings.Builder
	guide.WriteString("Which copy the game keeps when several definitions share a name, and the folders where it does so:\n")
	for _, sentence := range sentences {
		fmt.Fprintf(&guide, "\n%s\n%s\n", sentence, strings.Join(folders[sentence], ", "))
	}

	return guide.String(), nil
}

const settingGuidance = "guidance.newModders"

// describeWorkspace returns the open workspace's game, language, mods and the
// game's folders. It returns an error if no workspace is open.
func (s *Service) describeWorkspace() (string, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return "", err
	}

	state := s.Workspace.State()

	var guide strings.Builder
	fmt.Fprintf(&guide, "Game: %s %s, installed at %s. Its files are read only.\n", state.Game.Name,
		state.Install.Version, state.Install.Path)
	fmt.Fprintf(&guide, "Language: %s, beside the base language, %s.\n", ws.Language, loc.BaseLanguage)

	if guided, _ := s.Workspace.Config.LookupSetting("", settingGuidance); guided == "true" {
		guide.WriteString("The user has Guidance for new modders on: explain the game's terms when you use them.\n")
	}

	guide.WriteString("\nMods in load order:\n")
	for _, mod := range s.Workspace.ListMods() {
		missing := ""
		if mod.Broken {
			missing = ", a folder that is missing"
		}

		fmt.Fprintf(&guide, "- %s (id %s): %s%s\n", mod.Descriptor.Name, mod.ID, mod.Dir, missing)
	}

	guide.WriteString("\nThe game's folders and what each holds:\n")
	for _, database := range listFolderDatabases(ws) {
		fmt.Fprintf(&guide, "- %s: %s\n", database.Declaration.Folder, lang.DescribeDefinition(database.Name, true))
	}

	return guide.String(), nil
}

// listFolderDatabases returns the game's databases that have a folder, sorted
// by folder.
func listFolderDatabases(ws *workspace.Session) []*lang.Database {
	var databases []*lang.Database
	for _, database := range ws.Schema.Databases {
		if database.Declaration.Folder != "" {
			databases = append(databases, database)
		}
	}

	slices.SortFunc(databases, func(first, second *lang.Database) int {
		return strings.Compare(first.Declaration.Folder, second.Declaration.Folder)
	})

	return databases
}

// rename previews renaming a name in the mods, or makes the change a token
// names. It returns an error if no name that only the mods define is there.
func (s *Service) rename(request renaming) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	if _, err := s.Workspace.FindWritable(request.Path); err != nil {
		return nil, err
	}

	renamable, err := s.LSP.PrepareRename(request.Path, request.Line, request.Column)
	if err != nil || renamable == nil {
		return nil, errors.Join(err, errors.New("no name that only the mods define is at this position"))
	}

	edits, err := s.LSP.Rename(request.Path, request.Line, request.Column, request.Text)
	if err != nil {
		return nil, err
	}

	return s.preview(slices.Collect(maps.Keys(edits)), edits, func() (any, error) { return s.writeEdits(edits) })
}

// applyEdit previews replacing one entry of a file, or makes the change a
// token names. It returns an error unless the file has exactly one such entry.
func (s *Service) applyEdit(request entryEdit) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	symbols, err := s.LSP.ListSymbols(request.Path)
	if err != nil {
		return nil, err
	}

	entries := collectEntries(symbols, request.Entry)
	if len(entries) != 1 {
		return nil, fmt.Errorf("the file has %d entries named %s; list_symbols lists them", len(entries), request.Entry)
	}

	edits := map[string][]lsp.Edit{request.Path: {{Range: entries[0].Range, Text: request.Text}}}

	return s.preview([]string{request.Path}, edits, func() (any, error) { return s.writeEdits(edits) })
}

// collectEntries returns the symbols with that name, nested ones included.
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

// writeScaffold previews writing the localization keys a mod lacks in a
// language, or makes the change a token names. It returns an error if the mod
// or language does not exist or the mod lacks no key.
func (s *Service) writeScaffold(request scaffolding) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	if err := errors.Join(s.checkMod(request.Mod), s.checkLanguage(request.Language)); err != nil {
		return nil, err
	}

	mod, _ := s.Workspace.LookupMod(request.Mod)

	scaffolds := health.ListScaffolds(ws.Mods.Load(), mod, request.Language)
	if len(scaffolds) == 0 {
		return nil, fmt.Errorf("%s lacks no localization key in %s", ws.DescribeMod(request.Mod), request.Language)
	}

	counts := make(map[string]int, len(scaffolds))
	for file, messages := range scaffolds {
		counts[file] = len(messages)
	}

	return s.preview(slices.Collect(maps.Keys(counts)), counts, func() (any, error) {
		return s.Health.WriteScaffold(request.Mod, request.Language)
	})
}

// confirm makes the change kept under a token, once. It returns an error if no
// change has the token.
func (s *Service) confirm(token string) (any, error) {
	s.mu.Lock()
	apply, ok := s.pending[token]
	delete(s.pending, token)
	s.mu.Unlock()

	if !ok {
		return nil, errors.New("no preview has this token; call the tool again without one for a new preview")
	}

	return apply()
}

// preview keeps a change under a new token and returns it with the token. It
// returns an error, and keeps nothing, if a file is not one the mods may write
// or has text the user has not saved.
func (s *Service) preview(paths []string, change any, apply func() (any, error)) (any, error) {
	if _, err := s.Workspace.FindWritable(paths...); err != nil {
		return nil, err
	}

	for _, target := range paths {
		if text, err := s.Workspace.Read(target); err == nil && text.Draft != "" {
			return nil, fmt.Errorf("%s has changes the user has not saved; ask them to save or discard them first", target)
		}
	}

	token := rand.Text()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = map[string]func() (any, error){}
	}

	s.pending[token] = apply

	return Preview{Change: change, Token: token}, nil
}

// writeEdits applies the edits to each file on disk, keeping its encoding, and
// returns the files written. On an error it returns the files written before
// it.
func (s *Service) writeEdits(edits map[string][]lsp.Edit) ([]string, error) {
	paths := slices.Sorted(maps.Keys(edits))
	for position, target := range paths {
		text, err := files.ReadText(target)
		if err != nil {
			return paths[:position], err
		}

		lines := jomini.NewLines(text.Text)
		changes := make([]jomini.Edit, 0, len(edits[target]))
		for _, edit := range edits[target] {
			changes = append(changes, jomini.Edit{Span: jomini.ToSpan(text.Text, lines, edit.Range), Text: edit.Text})
		}

		text.Text = jomini.ApplyEdits(text.Text, changes)
		if err := s.Workspace.Write(target, text); err != nil {
			return paths[:position], err
		}
	}

	return paths, nil
}
