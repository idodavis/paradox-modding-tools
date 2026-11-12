// pictures.go finds the picture an event shows.

package graph

import (
	"strings"
	"sync"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// findPicture returns the path of the picture an event shows, or "" if there is
// none.
func findPicture(ws *workspace.Session, mods *index.Index, site index.Site, picture game.Picture) string {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return ""
	}
	if path, ok := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, picture.FileKey), "")); ok {
		return path
	}
	for _, chain := range picture.Chains {
		values := listValues(tree, statement, lo.FirstOr(chain.Steps, game.PictureStep{}).Key)
		if path := resolveChain(ws, mods, chain, values); path != "" {
			return path
		}
	}
	return ""
}

// resolveChain returns the path of the picture a chain of definitions ends in,
// from the given values for its first step, or "" if it reaches none. If the
// last definition names no file, the chain's Fallback key names another to try.
func resolveChain(ws *workspace.Session, mods *index.Index, chain game.PictureChain, values []string) string {
	var tree *jomini.Tree
	var statement int32
	for position, step := range chain.Steps {
		if position > 0 {
			values = listValues(tree, statement, step.Key)
		}
		site, ok := findHop(ws, mods, step, values)
		if !ok {
			return ""
		}
		var err error
		if tree, statement, err = ws.FindStatement(mods, site); err != nil {
			return ""
		}
	}
	if path, ok := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, chain.FileKey), "")); ok || chain.Fallback == "" {
		return path
	}
	last := game.PictureStep{Database: lo.LastOr(chain.Steps, game.PictureStep{}).Database}
	fallback, ok := findHop(ws, mods, last, listValues(tree, statement, chain.Fallback))
	if !ok {
		return ""
	}
	tree, statement, err := ws.FindStatement(mods, fallback)
	if err != nil {
		return ""
	}
	path, _ := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, chain.FileKey), ""))
	return path
}

// findHop returns the definition a hop leads to from the given values, or false
// if there is none.
func findHop(ws *workspace.Session, mods *index.Index, step game.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}
	if step.Match == "" {
		winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: step.Database, Name: values[0]})
		return winner.Site, ok
	}

	for _, candidate := range listCandidates(ws, mods, step) {
		if lo.Every(candidate.values, values) {
			return candidate.site, true
		}
	}

	return index.Site{}, false
}

// candidate is one kept definition a hop may lead to, with the values it
// writes at the hop's match path.
type candidate struct {
	site   index.Site
	values []string
}

// candidates holds, for one index of the mods, the candidates of each hop
// that matches by a key path. Listing them reads every kept definition of
// the hop's database, which a graph would else do again for each event it
// draws. A scan puts a new index in place, and the old lists go.
var candidates struct {
	sync.Mutex
	mods  *index.Index
	lists map[game.PictureStep][]candidate
}

// listCandidates returns the candidates of a hop that matches by a key path,
// in load order. A definition whose file cannot be read is left out.
func listCandidates(ws *workspace.Session, mods *index.Index, step game.PictureStep) []candidate {
	candidates.Lock()
	list, listed := candidates.lists[step]
	listed = listed && candidates.mods == mods
	candidates.Unlock()
	if listed {
		return list
	}

	list = nil
	for _, winner := range mods.ListKept(ws.Schema, step.Database) {
		if tree, statement, err := ws.FindStatement(mods, winner.Site); err == nil {
			list = append(list, candidate{site: winner.Site, values: listValues(tree, statement, step.Match)})
		}
	}

	candidates.Lock()
	if candidates.mods != mods {
		candidates.mods, candidates.lists = mods, map[game.PictureStep][]candidate{}
	}

	candidates.lists[step] = list
	candidates.Unlock()

	return list
}

// listValues returns the values written at a path of keys joined by slashes,
// each picked by pickChild. It returns nil if the path is empty or a key is not
// written.
func listValues(tree *jomini.Tree, statement int32, path string) []string {
	if path == "" {
		return nil
	}
	for key := range strings.SplitSeq(path, "/") {
		child, ok := pickChild(tree, statement, key)
		if !ok {
			return nil
		}
		statement = child
	}
	if value := tree.Statements[statement].Value; !value.IsEmpty() {
		return []string{tree.Unquoted(value)}
	}
	var values []string
	for child := range tree.Children(statement) {
		if value := tree.Statements[child].Value; !value.IsEmpty() {
			values = append(values, tree.Unquoted(value))
		}
	}
	return values
}

// pickChild returns the child of a statement with the given key, or false if
// there is none. A key written key[field=value] picks the child that writes the
// value at the field. Of several plain ones, such as a background written for
// some cases and then for the rest, it picks the first with no trigger of its
// own, else the first.
func pickChild(tree *jomini.Tree, statement int32, key string) (int32, bool) {
	key, selector, isSelected := strings.Cut(strings.TrimSuffix(key, "]"), "[")
	field, value, _ := strings.Cut(selector, "=")
	first, ok := int32(0), false
	for child := range tree.Children(statement) {
		if tree.Text(tree.Statements[child].Key) != key {
			continue
		}
		if isSelected {
			if text, _ := tree.FindChildValue(child, field); text == value {
				return child, true
			}
			continue
		}
		if _, triggered := tree.FindChild(child, game.TriggerKey); !triggered {
			return child, true
		}
		if !ok {
			first, ok = child, true
		}
	}
	return first, ok
}
