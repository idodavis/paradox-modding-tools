// pictures.go finds the picture an event shows.

package graph

import (
	"slices"
	"strings"
	"sync"
	"weak"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// findPicture returns the path of the picture an event's statement shows:
// the file it names itself, else the file the first of the game's chains
// leads to. It returns "" if there is none.
func findPicture(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, rows lang.Picture) string {
	if path, ok := resolveFile(mods, tree, statement, rows.FileKey); ok {
		return path
	}

	for _, chain := range rows.Chains {
		if path := resolveChain(ws, mods, chain, tree, statement); path != "" {
			return path
		}
	}

	return ""
}

// resolveFile returns the image file a definition names under a key. It
// returns false if it names none, or no file has that path.
func resolveFile(mods *index.Index, tree *jomini.Tree, statement int32, key string) (string, bool) {
	values := listValues(tree, statement, key)
	if len(values) == 0 {
		return "", false
	}

	return mods.ResolveTexture(values[0])
}

// resolveChain returns the picture a chain of definitions ends in, starting
// from a statement. Where the last definition names no file, the chain's
// fallback key is one more step, to another definition of the same
// database. It returns "" if the chain leads to no file.
func resolveChain(ws *workspace.Session, mods *index.Index, chain lang.PictureChain, tree *jomini.Tree, statement int32) string {
	for _, step := range chain.Steps {
		site, ok := findHop(ws, mods, step, listValues(tree, statement, step.Key))
		if !ok {
			return ""
		}

		var err error
		if tree, statement, err = ws.FindStatement(mods, site); err != nil {
			return ""
		}
	}

	path, ok := resolveFile(mods, tree, statement, chain.FileKey)
	if ok || chain.Fallback == "" {
		return path
	}

	last := lang.PictureStep{Key: chain.Fallback, Database: chain.Steps[len(chain.Steps)-1].Database}

	return resolveChain(ws, mods, lang.PictureChain{Steps: []lang.PictureStep{last}, FileKey: chain.FileKey}, tree, statement)
}

// findHop returns the definition one step of a chain leads to: the one the
// first value names, or, for a step that compares values, the first that
// writes every value. It returns false if there is none.
func findHop(ws *workspace.Session, mods *index.Index, step lang.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}

	if step.Match == "" {
		winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: step.Database, Name: values[0]})

		return winner.Site, ok
	}

	for _, candidate := range listCandidates(ws, mods, step) {
		hasEvery := !slices.ContainsFunc(values, func(value string) bool { return !slices.Contains(candidate.values, value) })
		if hasEvery {
			return candidate.site, true
		}
	}

	return index.Site{}, false
}

// candidate is one kept definition a step may lead to, with the values it
// writes under the key the step compares.
type candidate struct {
	site   index.Site
	values []string
}

// candidates caches the candidates of each comparing step for the latest
// index of the mods. Listing them loads every kept definition of a database,
// which a graph would otherwise do again for each event it draws. The index
// is held weakly, so a closed workspace's index is not kept alive.
var candidates struct {
	sync.Mutex
	mods  weak.Pointer[index.Index]
	lists map[lang.PictureStep][]candidate
}

// listCandidates returns the candidates of a comparing step, in load order.
// A definition whose file cannot be read is left out.
func listCandidates(ws *workspace.Session, mods *index.Index, step lang.PictureStep) []candidate {
	key := weak.Make(mods)
	candidates.Lock()
	list, isListed := candidates.lists[step]
	isListed = isListed && candidates.mods == key
	candidates.Unlock()

	if isListed {
		return list
	}

	list = nil
	for _, winner := range mods.ListKept(ws.Schema, step.Database) {
		if tree, statement, err := ws.FindStatement(mods, winner.Site); err == nil {
			list = append(list, candidate{site: winner.Site, values: listValues(tree, statement, step.Match)})
		}
	}

	candidates.Lock()
	if candidates.mods != key {
		candidates.mods, candidates.lists = key, map[lang.PictureStep][]candidate{}
	}

	candidates.lists[step] = list
	candidates.Unlock()

	return list
}

// listValues returns the values under a path of keys joined by slashes,
// each key picked by pickChild. A block gives the values of its statements.
// It returns nil if the path is empty or one of its keys is not written.
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

// pickChild returns the child of a statement with a key. A key written
// key[field=value] picks the child whose field has that value. Of several
// plain ones, as a background written for some cases and then for the rest,
// it picks the first with no trigger, else the first. It returns false if
// there is none.
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

		if _, hasTrigger := tree.FindChild(child, lang.TriggerKey); !hasTrigger {
			return child, true
		}

		if !ok {
			first, ok = child, true
		}
	}

	return first, ok
}
