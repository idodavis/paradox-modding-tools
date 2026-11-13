// Package health builds the report of what is wrong in the open workspace's
// mods. It also lists the game's error log and watches it while a workspace is
// open.
//
// A report holds the findings for one load order:
//
//   - A contest: two or more mods define one name and the game keeps one
//     copy, or two or more mods ship one file. It is critical unless the
//     winner depends on every beaten mod or the workspace ignores the pair.
//   - An undecided contest: copies of one name whose kept copy no source
//     names.
//   - An override, an unused copy and removed names: a mod and the game write
//     one name or one file.
//   - A localization key that two or more mods define in one language.
//   - A missing dependency, a folder outside the game's stage folders, the
//     editor's own problems, and how far each language covers a mod.
//
// The compatibility checks judge the mods as if those the workspace leaves
// out were not loaded. Each mod's own checks see every mod.
//
// # Robustness
//
// A previewed load order reads no file again: it takes what the workspace's
// own order read, so a call of a scripted definition that two mods declare is
// judged as the saved order judges it. A report fails only if a previewed
// order does not name each mod once. A file that cannot be read writes no
// block and has no snippet. The error log's list is empty if the game wrote
// no log, and every mod counts as read if the game wrote no debug log. A log
// that cannot be watched is not watched.
package health

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Health page.
type Service struct {
	Workspace *workspace.Service

	// mu guards stop and reports.
	mu sync.Mutex

	// stop ends the error log's watch, nil while nothing is watched.
	stop func()

	// reports holds the reports kept, by the load order each judges.
	reports map[string]*build
}

// Use is one name a finding is about, with how often the files the game
// reads use it and where they first do.
type Use struct {
	Name  string   `json:"name"`
	Count int      `json:"count"`
	First lsp.Site `json:"first"`
}

// Finding is one problem, or one fact about the mods.
type Finding struct {
	// Code is an lsp.Codes id. Label and Topic are the code's title and topic.
	Code  string `json:"code"`
	Label string `json:"label"`
	Topic string `json:"topic"`

	// Question is the code's question, or compatibility where another mod is
	// concerned.
	Question string `json:"question"`

	// Level is the code's level, or a contest's own.
	Level string `json:"level"`

	// Message is the code's template filled with the facts, or a log row's
	// line.
	Message []lsp.Part `json:"message"`

	// Brief is the line under the finding's name in a list.
	Brief []lsp.Part `json:"brief,omitempty"`

	// Detail is the finding's place, such as a.txt:12, or what a log row
	// names.
	Detail string `json:"detail"`

	// Why says why the winner's copy wins, or why a log row is a mod's.
	Why []lsp.Part `json:"why,omitempty"`

	Name string `json:"name"`

	// Database is what the database calls a definition.
	Database string `json:"database"`

	// Origin is the mod the finding is about, "" for the game's own log row.
	Origin string `json:"origin"`

	// Mods are the mods the finding concerns, in load order.
	Mods []string `json:"mods"`

	Facts map[string]string `json:"facts"`
	Sites []lsp.Site        `json:"sites"`

	// Removed are the names the replaced sites define that the kept file
	// does not.
	Removed []string `json:"removed,omitempty"`

	// Uses are the removed names the files the game reads still use, the
	// most used first.
	Uses []Use `json:"uses,omitempty"`

	// Defined are where the workspace defines the name, in a mod the
	// finding's mod does not depend on.
	Defined []lsp.Site `json:"defined,omitempty"`

	// Diagnosed is set for one of the editor's own diagnostics, which the
	// editor shows itself.
	Diagnosed bool `json:"diagnosed"`

	// Comparable is set where the kept statement and those it replaces can
	// be shown side by side.
	Comparable bool `json:"comparable"`

	// Order is a load order that fixes a contest.
	Order []string `json:"order,omitempty"`

	// Count is how many findings or log lines the row stands for: 1 unless
	// others were folded into it.
	Count int `json:"count"`

	// Repeats are the sites of the findings folded into this one.
	Repeats []lsp.Site `json:"repeats,omitempty"`

	// Pairs are each two workspace mods the finding sets against each other,
	// in load order. A pair can be ignored.
	Pairs [][2]string `json:"pairs"`
}

// Change is one name whose kept copy moves to another mod in a previewed
// load order.
type Change struct {
	// Database is what the database calls a definition.
	Database string `json:"database"`

	Name string `json:"name"`

	// Before and After are the mods whose copy the workspace's order and the
	// previewed order keep.
	Before string `json:"before"`
	After  string `json:"after"`
}

// Tally is how many findings there are of each level.
type Tally struct {
	// Counts holds the count of every level, by level.
	Counts map[string]int `json:"counts"`

	// Phrases holds each count in words, such as "235 critical problems", by
	// level. A level with no finding has none.
	Phrases map[string]string `json:"phrases"`

	// Worst is the worst level that has a finding, "" if none has.
	Worst string `json:"worst"`
}

// Check is one kind of finding at one level, and how many findings it has.
type Check struct {
	// Code is an lsp.Codes id, and Label the code's title.
	Code  string `json:"code"`
	Label string `json:"label"`

	Level string `json:"level"`

	// Count includes the findings folded into others.
	Count int `json:"count"`
}

// Group is the findings of the workspace's compatibility or of one mod,
// folded, counted and sorted.
type Group struct {
	Tally

	// Mod is the mod's id, "" for the workspace's compatibility.
	Mod string `json:"mod"`

	// Findings are sorted worst level first, then by check, the largest
	// first, then by weight. A mod's leave out its coverage.
	Findings []Finding `json:"findings"`

	// Languages are the mod's coverage findings, one for each language
	// beside the base one.
	Languages []Finding `json:"languages"`

	// Checks are in the order of the findings.
	Checks []Check `json:"checks"`
}

// Report is the findings for one load order, grouped and counted, and what
// that order changes.
type Report struct {
	// Tally counts every finding of the compatibility and of the mods.
	Tally

	Compatibility Group `json:"compatibility"`

	// Mods is in load order.
	Mods []Group `json:"mods"`

	// Own counts the findings of the user's own mods, external ones left out.
	Own Tally `json:"own"`

	CheckedAt time.Time `json:"checkedAt"`

	// Changes is empty for the workspace's own order.
	Changes []Change `json:"changes"`
}

// GameLog is what the game's last run wrote to its error log, folded into
// rows.
type GameLog struct {
	Findings []Finding `json:"findings"`

	// Path is the log file's full path.
	Path string `json:"path"`

	// WrittenAt is when the game last wrote the log, zero if there is none.
	WrittenAt time.Time `json:"writtenAt"`

	// Unloaded are the workspace's mods the run did not read. Their rows
	// stay the game's.
	Unloaded []string `json:"unloaded"`

	// Copies holds the folder of another copy of a workspace mod that the
	// run read in its place, by mod id.
	Copies map[string]string `json:"copies"`
}

// Overview is what one mod holds, counted, and the mods it is judged with.
type Overview struct {
	Mod string `json:"mod"`

	// Counts holds the definitions, by what their database calls them.
	Counts map[string]int `json:"counts"`

	Files    int `json:"files"`
	Messages int `json:"messages"`

	// Judged are the mods this mod's own problems are judged with: its
	// dependencies, direct or not.
	Judged []string `json:"judged"`
}

// Compared is the texts of two statements, as the two sides of a compare.
type Compared struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

// Snippet is the code around one finding site.
type Snippet struct {
	Text string `json:"text"`

	// Line is the line the snippet begins at, and Last the last line of the
	// site's own code, both counting from one.
	Line int `json:"line"`
	Last int `json:"last"`
}

// EventErrorLogChanged is sent when the game's error log grows while a
// workspace is open.
const EventErrorLogChanged = "errorlog:changed"

func init() { application.RegisterEvent[struct{}](EventErrorLogChanged) }

// ServiceStartup watches the game's error log while a workspace is open.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	events := application.Get().Event
	events.On(workspace.EventOpened, func(*application.CustomEvent) {
		ws, err := s.Workspace.Session()
		if err != nil {
			s.setStop(nil)

			return
		}

		s.setStop(watchErrorLog(ws, func() { events.Emit(EventErrorLogChanged, struct{}{}) }))
	})
	events.On(workspace.EventClosed, func(*application.CustomEvent) {
		s.setStop(nil)
		s.mu.Lock()
		defer s.mu.Unlock()

		clear(s.reports)
	})

	return nil
}

// ServiceShutdown stops the error log's watch.
func (s *Service) ServiceShutdown() error {
	s.setStop(nil)

	return nil
}

// setStop ends the running watch, if any, and keeps the given function to
// end the next one.
func (s *Service) setStop(stop func()) {
	s.mu.Lock()
	running := s.stop
	s.stop = stop
	s.mu.Unlock()

	if running != nil {
		running()
	}
}

// ListCodes returns every problem code.
func (s *Service) ListCodes() []lsp.Code { return lsp.Codes }

// ListLevels returns the levels of a finding, the worst first.
func (s *Service) ListLevels() []lsp.Level { return lsp.Levels }

// ListTopics returns the topics of the checks, in the order they are listed.
func (s *Service) ListTopics() []lsp.Topic { return lsp.Topics }

// ListOverview counts each mod's definitions, files and messages, and lists
// the mods each is judged with.
func (s *Service) ListOverview() ([]Overview, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods, order := ws.Mods.Load(), *ws.Order.Load()
	overviews := make([]Overview, len(order))
	positions := map[string]int{}
	for position, mod := range order {
		overviews[position] = Overview{
			Mod: mod.ID, Counts: map[string]int{}, Judged: append([]string{}, ws.ListDependencies(mod.ID)...),
		}
		positions[mod.ID] = position
	}

	for _, file := range mods.ListOwnFiles() {
		position, ok := positions[mods.Files[file].Origin]
		if !ok {
			continue
		}

		overviews[position].Files++
		for database, count := range mods.CountDefinitions(file) {
			overviews[position].Counts[lang.DescribeDefinition(database, true)] += count
		}
	}

	// A key counts once, however many languages the mod writes it in.
	_, messages := listOwnSites(mods)
	keys := listUnique(messages, func(site index.Site) [2]string {
		return [2]string{mods.File(site).Origin, mods.ToReference(site).Name}
	})
	for _, key := range keys {
		if position, ok := positions[key[0]]; ok {
			overviews[position].Messages++
		}
	}

	return overviews, nil
}

// CompareStatements returns the texts of the statements at two sites. It
// returns an error if a file cannot be read or no statement starts at a site.
func (s *Service) CompareStatements(left, right lsp.Site) (Compared, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Compared{}, err
	}

	first, err := findStatementText(ws, left)
	if err != nil {
		return Compared{}, err
	}

	second, err := findStatementText(ws, right)

	return Compared{Left: first, Right: second}, err
}

// findStatementText returns the text of the statement that starts on a
// site's line.
func findStatementText(ws *workspace.Session, site lsp.Site) (string, error) {
	tree, err := ws.LoadTree(site.Place.Path)
	if err != nil {
		return "", err
	}

	statement, ok := findLineStatement(tree, site.Line)
	if !ok {
		return "", fmt.Errorf("no statement starts at line %d of %s", site.Line, site.Place.Path)
	}

	return tree.Text(tree.Statements[statement].Span), nil
}

// findLineStatement returns the statement that starts on a line, counting
// from one. It returns false if none does.
func findLineStatement(tree *jomini.Tree, line int) (int32, bool) {
	span := tree.Lines.LineSpan(min(max(line-1, 0), tree.Lines.CountLines()-1))
	text := tree.Src[span.Start:span.End]

	return tree.LookupStatement(span.Start + len(text) - len(strings.TrimLeft(text, " \t")))
}

// snippetLines is the most lines a snippet shows of a site, and
// snippetContext how many more it shows on each side of a one-line site.
const (
	snippetLines   = 10
	snippetContext = 2
)

// ListSnippets returns the code at each finding site: its range, or the
// statement on its line where it has none. It returns an error if a file
// cannot be read.
func (s *Service) ListSnippets(sites []lsp.Site) ([]Snippet, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	snippets := make([]Snippet, len(sites))
	for position, site := range sites {
		tree, err := ws.LoadTree(site.Place.Path)
		if err != nil {
			return nil, err
		}

		last := tree.Lines.CountLines() - 1
		first := min(max(site.Line-1, 0), last)
		end := min(max(site.Range.EndLine-1, first), last, first+snippetLines-1)
		if statement, ok := findLineStatement(tree, site.Line); site.Range.EndLine == 0 && ok {
			end = min(tree.Lines.ToPosition(tree.Statements[statement].Span.End).Line, first+snippetLines-1)
		}

		margin := 0
		if end == first {
			margin = snippetContext
		}

		top, bottom := max(first-margin, 0), min(end+margin, last)
		snippets[position] = Snippet{
			Text: tree.Src[tree.Lines.LineSpan(top).Start:tree.Lines.LineSpan(bottom).End], Line: top + 1, Last: end + 1,
		}
	}

	return snippets, nil
}

// ListReaders returns every place in script and localization that reads a
// localization key.
func (s *Service) ListReaders(key string) ([]lsp.Site, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	uses := mods.ListUses(lang.Localization, key)
	sites := make([]lsp.Site, len(uses))
	for position, use := range uses {
		sites[position] = lsp.MakeSite(ws, mods, use)
	}

	return sites, nil
}

// WriteScaffold writes the localization keys a mod lacks in a language,
// with the base text as a placeholder, and returns the files it wrote. Each
// key goes at the end of the matching file, which gets its header first if
// it is missing or empty. It returns an error if the mod or the language is
// not valid, or a file cannot be read or written.
func (s *Service) WriteScaffold(modID, language string) ([]string, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mod, ok := s.Workspace.LookupMod(modID)
	if !ok || mod.Broken {
		return nil, fmt.Errorf("the mod %s is not in the workspace or has no folder", modID)
	}

	if language == "" || language != files.ToSlug(language) {
		return nil, fmt.Errorf("%q is not a language", language)
	}

	scaffolds := ListScaffolds(ws.Mods.Load(), mod, language)
	paths := slices.Sorted(maps.Keys(scaffolds))
	changes := make([]workspace.FileChange, len(paths))
	for position, target := range paths {
		text, err := files.LoadText(target)
		if err == nil && strings.TrimSpace(text.Text) == "" {
			if err = s.Workspace.CreateLocalization(target); err == nil {
				text, err = files.LoadText(target)
			}
		}

		if err == nil {
			text.Text += loc.EncodeAddition(text.Text, scaffolds[target])
			err = s.Workspace.Write(target, text)
		}

		if err != nil {
			return paths[:position], err
		}

		changes[position] = workspace.FileChange{Path: target}
	}

	// A file the rescan cannot read waits for the watcher.
	_ = ws.Rescan(changes)

	return paths, nil
}

// ListScaffolds returns a mod's base-language messages whose key has no
// message in a language, by the file of that language they belong in.
func ListScaffolds(mods *index.Index, mod workspace.Mod, language string) map[string][]loc.Message {
	_, messages := listOwnSites(mods)
	scaffolds := map[string][]loc.Message{}
	for _, site := range listUntranslated(mods, groupMessages(mods, messages)[mod.ID], language) {
		target := files.Join(mod.Dir, loc.ToLanguageRel(mods.File(site).Rel, language))
		scaffolds[target] = append(scaffolds[target], loc.Message{Key: mods.ToReference(site).Name, Text: mods.Text(site)})
	}

	return scaffolds
}

// groupMessages returns messages by mod and then by language, each list in
// the order given.
func groupMessages(mods *index.Index, messages []index.Site) map[string]map[string][]index.Site {
	groups := map[string]map[string][]index.Site{}
	for _, site := range messages {
		origin, language := mods.File(site).Origin, mods.Language(site)
		if groups[origin] == nil {
			groups[origin] = map[string][]index.Site{}
		}

		groups[origin][language] = append(groups[origin][language], site)
	}

	return groups
}

// listUntranslated returns one mod's base-language messages whose key has
// no message in a language, one for each key.
func listUntranslated(mods *index.Index, byLanguage map[string][]index.Site, language string) []index.Site {
	// listed holds the keys the language has, and then each key returned.
	listed := map[string]bool{}
	for _, site := range byLanguage[language] {
		listed[mods.ToReference(site).Name] = true
	}

	var untranslated []index.Site
	for _, site := range byLanguage[loc.BaseLanguage] {
		if key := mods.ToReference(site).Name; !listed[key] {
			listed[key] = true
			untranslated = append(untranslated, site)
		}
	}

	return untranslated
}
