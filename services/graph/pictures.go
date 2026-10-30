// pictures.go finds the picture an event shows, through a chain of definitions that ends in an image file.

package graph

import (
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// findPicture returns the path of a picture an event shows: the file the event writes outright, else the end of the
// first chain that reaches a file. It returns "" if there is none or the event's file cannot be read.
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

// resolveChain returns the path of the picture a chain of definitions ends in. It returns "" if the chain reaches no
// picture.
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

// findHop returns the definition a hop leads to from the given values. It returns false if there is none.
func findHop(ws *workspace.Session, mods *index.Index, step game.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}
	if step.Match == "" {
		winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: step.Database, Name: values[0]})
		return winner.Site, ok
	}
	for _, winner := range mods.ListKept(ws.Schema, step.Database) {
		tree, statement, err := ws.FindStatement(mods, winner.Site)
		if err == nil && lo.Every(listValues(tree, statement, step.Match), values) {
			return winner.Site, true
		}
	}
	return index.Site{}, false
}

// listValues returns the values written at a key path in a definition. The path is keys joined by slashes, and each
// key is picked as pickChild picks it. It returns nil if the path is empty or a key is not written.
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

// pickChild returns the child of a statement with the given key. A key written key[field=value] is the child that
// writes the value at the field. Where several have a plain key, as a background written for some cases and then for
// the rest, it is the first with no trigger of its own, else the first. It returns false if there is none.
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
		if _, triggered := tree.FindChild(child, triggerKey); !triggered {
			return child, true
		}
		if !ok {
			first, ok = child, true
		}
	}
	return first, ok
}
