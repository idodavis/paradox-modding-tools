// checks.go checks what an agent asks the app to show before the app shows it: the page, each key of its query, and the
// node, mod, file or lookup the query names. It also gives an agent one Event Graph node's detail.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// A queryKey is one key of a page's query that show takes, with its help in the guide's words, the values it takes
// where they are few, and the check of a value.
type queryKey struct {
	Page   string
	Key    string
	Help   string
	Values []string
	Check  func(*Service, string) error
}

// queryKeys holds every key of the pages' queries that show takes, in the order the guide lists them.
var queryKeys = []queryKey{
	{Page: "event-graph", Key: "root", Help: "a node's id from find_roots, such as events:my_mod.0001",
		Check: (*Service).checkNode},
	{Page: "event-graph", Key: "node", Help: "a node's id, selected with its path drawn", Check: (*Service).checkNode},
	{Page: "event-graph", Key: "filters", Help: "an Event Graph filter as JSON, as build_graph takes it, its parts " +
		"left out at the defaults", Check: (*Service).checkFilter},
	{Page: "health", Key: "view", Help: "the view shown; left out, the first with a row for code or name",
		Values: []string{"summary", "compatibility", "mod", "log"}},
	{Page: "health", Key: "mod", Help: "the mod the mod view shows, by its id", Check: (*Service).checkMod},
	{Page: "health", Key: "code", Help: "a check's id from list_codes, whose rows are shown", Check: (*Service).checkCode},
	{Page: "health", Key: "name", Help: "a name, whose rows are shown"},
	{Page: "patch-notes", Key: "patch", Help: "the version whose notes are shown", Check: (*Service).checkPatch},
	{Page: "patch-notes", Key: "find", Help: "a text found in them"},
	{Page: "release", Key: "mod", Help: "the mod shown, by its id", Check: (*Service).checkMod},
	{Page: "release", Key: "compare", Help: "the compare of the mod with its Steam item", Values: []string{"steam"}},
}

// nodeHint says where an Event Graph node's id comes from, for an error about one.
const nodeHint = "find_roots gives node ids, such as events:my_mod.0001"

// checkPage returns an error if PMT has no page of the given id, or if the query has a key the page does not take or a
// value the key does not accept. A query needs a page.
func (mcp *Service) checkPage(page string, query map[string]string) error {
	if page == "" {
		return lo.Ternary(len(query) > 0, errors.New("a query needs a page"), nil)
	}
	mcp.mu.Lock()
	ids := lo.Map(mcp.pages, func(one Page, _ int) string { return one.ID })
	mcp.mu.Unlock()
	if len(ids) > 0 && !slices.Contains(ids, page) {
		return fmt.Errorf("PMT has no page %q; its pages are %s", page, strings.Join(ids, ", "))
	}
	rows := lo.Filter(queryKeys, func(row queryKey, _ int) bool { return row.Page == page })
	keys := lo.Map(rows, func(row queryKey, _ int) string { return row.Key })
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(query)) {
		row, ok := lo.Find(rows, func(row queryKey) bool { return row.Key == key })
		value := query[key]
		switch {
		case !ok && len(keys) == 0:
			errs = append(errs, fmt.Errorf("%s takes no query", page))
		case !ok:
			errs = append(errs, fmt.Errorf("%s takes no %q in its query; it takes %s", page, key, health.JoinList(keys, "or")))
		case len(row.Values) > 0 && !slices.Contains(row.Values, value):
			errs = append(errs, fmt.Errorf("%s's %s is %s, not %q", page, key, health.JoinList(row.Values, "or"), value))
		case row.Check != nil:
			errs = append(errs, row.Check(mcp, value))
		}
	}
	return errors.Join(errs...)
}

// checkNode returns an error if the Event Graph has no node of the given id.
func (mcp *Service) checkNode(id string) error {
	_, err := mcp.buildDetail(node{ID: id})
	return err
}

// buildDetail returns one Event Graph node's detail. It returns an error, which says where node ids come from, if the
// graph has no such node.
func (mcp *Service) buildDetail(request node) (graph.Detail, error) {
	detail, err := mcp.Graph.BuildDetail(request.ID)
	if err != nil {
		return detail, fmt.Errorf("%w; %s", err, nodeHint)
	}
	return detail, nil
}

// checkFilter returns an error if a text is not an Event Graph filter in JSON, or holds a part a filter does not have.
func (mcp *Service) checkFilter(text string) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&graph.Filter{}); err != nil {
		return fmt.Errorf("filters is not an Event Graph filter as JSON: %w", err)
	}
	return nil
}

// checkMod returns an error if the workspace has no mod of the given id.
func (mcp *Service) checkMod(id string) error {
	if _, ok := mcp.Workspace.LookupMod(id); !ok {
		return fmt.Errorf("the workspace has no mod %q; read_guide's workspace topic lists the mods' ids", id)
	}
	return nil
}

// checkCode returns an error if Health has no check of the given id.
func (mcp *Service) checkCode(id string) error {
	if !slices.ContainsFunc(mcp.Health.ListCodes(), func(code lsp.Code) bool { return code.ID == id }) {
		return fmt.Errorf("no Health check has the id %q; list_codes lists them", id)
	}
	return nil
}

// checkPatch returns an error if the game's wiki has no patch notes of the given version, naming the newest it has.
func (mcp *Service) checkPatch(version string) error {
	notes, err := mcp.Wiki.ListNotes()
	if err != nil {
		return err
	}
	versions := lo.Map(notes, func(note wiki.Note, _ int) string { return note.Version })
	if !slices.Contains(versions, version) {
		return fmt.Errorf("no patch notes of %q; the newest are %s", version, strings.Join(versions[:min(len(versions), 8)], ", "))
	}
	return nil
}

// checkLanguage returns an error if the workspace's install has no localization in the given language, naming those
// it has.
func (mcp *Service) checkLanguage(language string) error {
	languages, err := mcp.Workspace.ListLanguages(mcp.Workspace.State().Install.ID)
	if err != nil {
		return err
	}
	folders := lo.Map(languages, func(language loc.Language, _ int) string { return language.Folder })
	if !slices.Contains(folders, language) {
		return fmt.Errorf("the game has no language %q; its languages are %s", language, strings.Join(folders, ", "))
	}
	return nil
}

// checkFile returns an error if a request's lines, window or picks come without a file, or if the file, lines, window
// or states it names are not valid.
func (mcp *Service) checkFile(request Show) error {
	if request.Path == "" {
		hasFileParts := request.StartLine != 0 || request.EndLine != 0 || request.Drawable != "" || len(request.Picks) > 0
		return lo.Ternary(hasFileParts, errors.New("startLine, endLine, drawable and picks need a path"), nil)
	}
	place, err := mcp.locateFile(request.Path)
	if err != nil {
		return err
	}
	if request.StartLine < 0 || request.EndLine < 0 || request.EndLine != 0 && request.EndLine < request.StartLine {
		return errors.New("lines count from one, and endLine is not before startLine")
	}
	if request.Drawable == "" {
		return lo.Ternary(len(request.Picks) > 0, errors.New("picks need a drawable"), nil)
	}
	return mcp.checkDrawable(place, request.Drawable, request.Picks)
}

// locateFile returns the place of a file of the workspace's mods or the game. It returns an error if the path is not
// a file in either.
func (mcp *Service) locateFile(path string) (workspace.Place, error) {
	place := mcp.Workspace.Locate(path)
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return place, fmt.Errorf("PMT finds no file %s", path)
	case info.IsDir():
		return place, fmt.Errorf("%s is a folder; give a file's path", path)
	case place.Root == "":
		return place, fmt.Errorf("%s is in none of the workspace's mods and the game's folders", path)
	}
	return place, nil
}

// checkDrawable returns an error, naming what the file does draw, if it draws nothing of the given name. It returns
// one for each pick that names a state group or option the window of a .gui file lacks. A pick given as an option's
// caption is replaced by the option. A coat's picks are not checked.
func (mcp *Service) checkDrawable(place workspace.Place, drawable string, picks map[string]string) error {
	symbols, err := mcp.Draw.ListDrawables(place.Path)
	if err != nil {
		return err
	}
	names := lo.Map(symbols, func(symbol lsp.Symbol, _ int) string { return symbol.Name })
	switch {
	case len(names) == 0:
		return fmt.Errorf("%s draws nothing", place.Name)
	case !slices.Contains(names, drawable):
		return fmt.Errorf("%s draws no %q; it draws %s", place.Name, drawable, strings.Join(names, ", "))
	}
	if len(picks) == 0 || place.Format != files.FormatGUI {
		return nil
	}
	window, err := mcp.Draw.ResolveWindow(place.Path, drawable)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(picks)) {
		group, ok := lo.Find(window.Groups, func(group draw.Group) bool { return group.ID == id })
		if !ok {
			ids := lo.Map(window.Groups, func(group draw.Group, _ int) string { return group.ID })
			errs = append(errs, fmt.Errorf("%s has no state group %q; its groups are %s", window.Name, id, strings.Join(ids, ", ")))
			continue
		}
		if option, isOption := findOption(group, picks[id]); isOption {
			picks[id] = option
			continue
		}
		words := lo.Map(group.Options, func(option string, _ int) string { return describeOption(group, option) })
		errs = append(errs, fmt.Errorf("%s's group %q takes %s, not %q", window.Name, id, health.JoinList(words, "or"), picks[id]))
	}
	return errors.Join(errs...)
}

// findOption returns the option of a group that the given text names, either itself or by its caption in the States
// panel. It returns false if the text names none.
func findOption(group draw.Group, text string) (string, bool) {
	if slices.Contains(group.Options, text) {
		return text, true
	}
	return lo.Find(group.Options, func(option string) bool { return strings.EqualFold(group.Captions[option], text) })
}

// describeOption returns an option of a group as the States panel writes it, with the option itself in brackets if
// that is another word.
func describeOption(group draw.Group, option string) string {
	caption := group.Captions[option]
	return lo.Ternary(strings.EqualFold(caption, option), caption, fmt.Sprintf("%s (%s)", caption, option))
}

// checkLookUp returns an error if a look-up names no wiki page or Lexicon entry the game's wiki has, or names neither.
func (mcp *Service) checkLookUp(lookUp *Lookup) error {
	switch {
	case lookUp == nil:
		return nil
	case lookUp.Title != "":
		if _, err := mcp.Wiki.ReadPage(lookUp.Title); err != nil {
			return fmt.Errorf("%w; search_pages gives the wiki's titles", err)
		}
		return nil
	case lookUp.Class == "" || lookUp.Name == "":
		return errors.New("a look-up names a wiki page's title, or a Lexicon entry's class and name")
	}
	rows, err := mcp.Wiki.BuildLexicon(lookUp.Class)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(rows, func(row wiki.LexiconRow) bool { return row.Name == lookUp.Name }) {
		return fmt.Errorf("the Lexicon has no %s %q; build_lexicon lists them", lookUp.Class, lookUp.Name)
	}
	return nil
}
