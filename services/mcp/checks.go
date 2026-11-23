// checks.go checks what an agent asks the app to show before it is shown.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// queryKey is one key of a page's query that show takes.
type queryKey struct {
	Page string
	Key  string
	Help string

	// Values lists the values the key takes, where they are few.
	Values []string

	// Check is nil where any value will do.
	Check func(*Service, string) error
}

// queryKeys holds the keys show takes, in the order the guide lists them.
var queryKeys = []queryKey{
	{Page: "graph", Key: "subject", Help: "a node's id from find_roots, such as events:my_mod.0001 or goods:books",
		Check: (*Service).checkNode},
	{Page: "graph", Key: "node", Help: "a node's id, selected with its detail shown", Check: (*Service).checkNode},
	{Page: "graph", Key: "conditions", Help: "the view's filter as JSON, as build_graph, build_chain and " +
		"build_tree take it, its parts left out at the defaults", Check: (*Service).checkFilter},
	{Page: "graph", Key: "picked", Help: "a production method's id, the one its group of a Vic3 building runs"},
	{Page: "graph", Key: "view", Help: "a technology's chain alone instead of its whole tree", Values: []string{"chain"}},
	{Page: "graph", Key: "direction", Help: "a tree read top to bottom instead of left to right", Values: []string{"down"}},
	{Page: "graph", Key: "wires", Help: "a tree without the wires outside the chain", Values: []string{"hidden"}},
	{Page: "health", Key: "view", Help: "the view shown; left out, the first with a row for code or name",
		Values: []string{"summary", "compatibility", "patch", "mod", "log"}},
	{Page: "health", Key: "mod", Help: "the mod the mod view shows, by its id", Check: (*Service).checkMod},
	{Page: "health", Key: "code", Help: "a check's id from list_codes, whose rows are shown", Check: (*Service).checkCode},
	{Page: "health", Key: "name", Help: "a name, whose rows are shown"},
	{Page: "patch-center", Key: "patch", Help: "the version shown, left out for Your mods",
		Check: (*Service).checkPatch},
	{Page: "patch-center", Key: "find", Help: "a text found in the version's notes"},
	{Page: "release", Key: "mod", Help: "the mod shown, by its id", Check: (*Service).checkMod},
	{Page: "release", Key: "compare", Help: "the compare of the mod with its Steam item", Values: []string{"steam"}},
}

const nodeHint = "find_roots gives node ids, such as events:my_mod.0001 or goods:books"

// checkPage returns an error if PMT has no such page, or if the query has a
// key the page does not take or a value the key does not accept. A query needs
// a page.
func (s *Service) checkPage(page string, query map[string]string) error {
	if page == "" {
		if len(query) > 0 {
			return errors.New("a query needs a page")
		}

		return nil
	}

	s.mu.Lock()
	ids := make([]string, 0, len(s.pages))
	for _, known := range s.pages {
		ids = append(ids, known.ID)
	}
	s.mu.Unlock()

	if len(ids) > 0 && !slices.Contains(ids, page) {
		return fmt.Errorf("PMT has no page %q; its pages are %s", page, strings.Join(ids, ", "))
	}

	var keys []string
	for _, row := range queryKeys {
		if row.Page == page {
			keys = append(keys, row.Key)
		}
	}

	var errs []error
	for _, key := range slices.Sorted(maps.Keys(query)) {
		position := slices.IndexFunc(queryKeys, func(row queryKey) bool { return row.Page == page && row.Key == key })
		var row queryKey
		if position >= 0 {
			row = queryKeys[position]
		}

		value := query[key]
		switch {
		case position < 0 && len(keys) == 0:
			errs = append(errs, fmt.Errorf("%s takes no query", page))
		case position < 0:
			errs = append(errs, fmt.Errorf("%s takes no %q in its query; it takes %s", page, key, lang.JoinList(keys, "or")))
		case len(row.Values) > 0 && !slices.Contains(row.Values, value):
			errs = append(errs, fmt.Errorf("%s's %s is %s, not %q", page, key, lang.JoinList(row.Values, "or"), value))
		case row.Check != nil:
			errs = append(errs, row.Check(s, value))
		}
	}

	return errors.Join(errs...)
}

func (s *Service) checkNode(id string) error {
	_, err := s.buildDetail(node{ID: id})

	return err
}

// buildDetail returns one Graph node's detail. The error for a missing
// node says where node ids come from.
func (s *Service) buildDetail(request node) (graph.Detail, error) {
	detail, err := s.Graph.BuildDetail(request.ID)

	return detail, hintNode(err)
}

// hintNode adds where node ids come from to an error of the Graph. It
// returns nil for no error.
func hintNode(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w; %s", err, nodeHint)
}

// checkFilter returns an error if the text is not a Graph filter in JSON.
func (s *Service) checkFilter(text string) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&graph.Filter{}); err != nil {
		return fmt.Errorf("conditions is not a Graph filter as JSON: %w", err)
	}

	return nil
}

func (s *Service) checkMod(id string) error {
	if _, ok := s.Workspace.LookupMod(id); !ok {
		return fmt.Errorf("the workspace has no mod %q; read_guide's workspace topic lists the mods' ids", id)
	}

	return nil
}

func (s *Service) checkCode(id string) error {
	if !slices.ContainsFunc(s.Health.ListCodes(), func(code lsp.Code) bool { return code.ID == id }) {
		return fmt.Errorf("no Health check has the id %q; list_codes lists them", id)
	}

	return nil
}

// checkPatch returns an error, naming the newest versions, if the Patch
// Center lists no such version.
func (s *Service) checkPatch(version string) error {
	notes, err := s.Patch.ListPatches()
	if err != nil {
		return err
	}

	versions := make([]string, len(notes))
	for position, note := range notes {
		versions[position] = note.Version
	}

	if !slices.Contains(versions, version) {
		return fmt.Errorf("no version %q; the newest are %s", version, strings.Join(versions[:min(len(versions), 8)], ", "))
	}

	return nil
}

// checkLanguage returns an error, naming the languages the install has, if it
// has no localization in that one.
func (s *Service) checkLanguage(language string) error {
	languages, err := s.Workspace.ListLanguages(s.Workspace.State().Install.ID)
	if err != nil {
		return err
	}

	folders := make([]string, 0, len(languages))
	for _, installed := range languages {
		folders = append(folders, installed.Folder)
	}

	if !slices.Contains(folders, language) {
		return fmt.Errorf("the game has no language %q; its languages are %s", language, strings.Join(folders, ", "))
	}

	return nil
}

// checkFile returns an error if the request has lines, a window or picks
// without a file, or if the file, lines, window or states are wrong.
func (s *Service) checkFile(request Show) error {
	if request.Path == "" {
		hasFileParts := request.StartLine != 0 || request.EndLine != 0 || request.Drawable != "" || len(request.Picks) > 0
		if hasFileParts {
			return errors.New("startLine, endLine, drawable and picks need a path")
		}

		return nil
	}

	place, err := s.locateFile(request.Path)
	if err != nil {
		return err
	}

	if request.StartLine < 0 || request.EndLine < 0 || request.EndLine != 0 && request.EndLine < request.StartLine {
		return errors.New("lines count from one, and endLine is not before startLine")
	}

	if request.Drawable == "" {
		if len(request.Picks) > 0 {
			return errors.New("picks need a drawable")
		}

		return nil
	}

	return s.checkDrawable(place, request.Drawable, request.Picks)
}

// locateFile returns the place of a file of the workspace's mods or the game.
// It returns an error if the path is not a file in either.
func (s *Service) locateFile(path string) (workspace.Place, error) {
	place := s.Workspace.Locate(path)
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

// checkDrawable returns an error, naming what the file draws, if it draws
// nothing of that name. For a .gui file it also checks the picks, replacing a
// caption with its option. A coat's picks are not checked.
func (s *Service) checkDrawable(place workspace.Place, drawable string, picks map[string]string) error {
	symbols, err := s.Draw.ListDrawables(place.Path)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}

	switch {
	case len(names) == 0:
		return fmt.Errorf("%s draws nothing", place.Name)
	case !slices.Contains(names, drawable):
		return fmt.Errorf("%s draws no %q; it draws %s", place.Name, drawable, strings.Join(names, ", "))
	}

	if len(picks) == 0 || place.Format != files.FormatGUI {
		return nil
	}

	window, err := s.Draw.ResolveWindow(place.Path, drawable)
	if err != nil {
		return err
	}

	var errs []error
	for _, id := range slices.Sorted(maps.Keys(picks)) {
		position := slices.IndexFunc(window.Groups, func(group draw.Group) bool { return group.ID == id })
		if position < 0 {
			ids := make([]string, 0, len(window.Groups))
			for _, group := range window.Groups {
				ids = append(ids, group.ID)
			}

			errs = append(errs, fmt.Errorf("%s has no state group %q; its groups are %s", window.Name, id, strings.Join(ids, ", ")))

			continue
		}

		group := window.Groups[position]
		if option, isOption := findOption(group, picks[id]); isOption {
			picks[id] = option

			continue
		}

		words := make([]string, 0, len(group.Options))
		for _, option := range group.Options {
			words = append(words, describeOption(group, option))
		}

		errs = append(errs, fmt.Errorf("%s's group %q takes %s, not %q", window.Name, id, lang.JoinList(words, "or"), picks[id]))
	}

	return errors.Join(errs...)
}

// findOption returns the option of a group that the text names, itself or by
// its caption in the States panel. It returns false if the text names none.
func findOption(group draw.Group, text string) (string, bool) {
	if slices.Contains(group.Options, text) {
		return text, true
	}

	position := slices.IndexFunc(group.Options, func(option string) bool {
		return strings.EqualFold(group.Captions[option], text)
	})
	if position < 0 {
		return "", false
	}

	return group.Options[position], true
}

// describeOption returns an option as the States panel writes it: its caption,
// with the option itself in brackets if that is another word.
func describeOption(group draw.Group, option string) string {
	caption := group.Captions[option]
	if strings.EqualFold(caption, option) {
		return caption
	}

	return fmt.Sprintf("%s (%s)", caption, option)
}

// checkLookUp returns an error if the look-up names no wiki page or Lexicon
// entry the game's wiki has, or names neither.
func (s *Service) checkLookUp(lookUp *Lookup) error {
	switch {
	case lookUp == nil:
		return nil

	case lookUp.Title != "":
		if _, err := s.Wiki.ReadPage(lookUp.Title); err != nil {
			return fmt.Errorf("%w; search_pages gives the wiki's titles", err)
		}

		return nil

	case lookUp.Class == "" || lookUp.Name == "":
		return errors.New("a look-up names a wiki page's title, or a Lexicon entry's class and name")
	}

	rows, err := s.LSP.BuildLexicon(lookUp.Class, "", "")
	if err != nil {
		return err
	}

	if !slices.ContainsFunc(rows, func(row lsp.LexiconRow) bool { return row.Name == lookUp.Name }) {
		return fmt.Errorf("the Lexicon has no %s %q; build_lexicon lists them", lookUp.Class, lookUp.Name)
	}

	return nil
}
