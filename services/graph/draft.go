// draft.go holds what one request reads of the game's goods, buildings and
// technologies: the workspace, its links, and what was worked out once.

package graph

import (
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// draft is one request about the game's goods, buildings and technologies.
type draft struct {
	ws    *workspace.Session
	mods  *index.Index
	model *model

	// groups holds the id of the group each definition asked about is listed
	// under, "" for one in none.
	groups map[string]string

	// known holds the node of each definition already asked for.
	known map[string]Node

	// parameters holds the titles of the definitions that grant each
	// parameter a condition may name, read the first time one is asked for.
	parameters map[string][]string

	// eras are the ids of the game's eras in the order its files define
	// them, read the first time they are needed.
	eras []string
}

func newDraft(ws *workspace.Session, mods *index.Index) *draft {
	return &draft{
		ws: ws, mods: mods, model: loadModel(ws, mods),
		groups: map[string]string{}, known: map[string]Node{},
	}
}

// findStatement returns the tree of the file that defines an id and the
// statement of the definition in it. It returns false if the game keeps no
// such definition or its file cannot be read.
func (d *draft) findStatement(id string) (*jomini.Tree, int32, bool) {
	site, err := resolveNode(d.ws, d.mods, id)
	if err != nil {
		return nil, 0, false
	}

	tree, statement, err := d.ws.FindStatement(d.mods, site)

	return tree, statement, err == nil
}

// toReference returns the reference an id is of: its database, before the
// colon, and its name.
func toReference(id string) lang.Reference {
	database, name, _ := strings.Cut(id, ":")

	return lang.Reference{Database: database, Name: name}
}

func toDatabase(id string) string { return toReference(id).Database }
