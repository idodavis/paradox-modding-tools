// graph_stubs.go is the two stubs: more fires out of a node than the page draws, and the callers into the root until
// the page opens them.

package pages

import (
	"fmt"
	"slices"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// stubMore is the database every stub is drawn in, and the Stub of one standing for fires out of a node.
const stubMore = "more"

// moreStub draws what a node's fan-out hides as one stub, joined to the node.
func (grapher *grapher) moreStub(from string, hidden []workspace.Fire) {
	grapher.out.Nodes = append(grapher.out.Nodes, Node{ID: "more:" + from, Database: stubMore, Stub: stubMore, Title: stubTitle(hidden)})
	grapher.out.Edges = append(grapher.out.Edges, Edge{From: from, To: "more:" + from})
}

// callersStub draws what fires the root, one hop in, as a stub until the page opens it; a root the game never
// fires has none.
func (grapher *grapher) callersStub(root index.At) {
	params := grapher.params
	var callers []workspace.Fire
	if index.Fires(grapher.ws.Install.Schema, schema.Occurrence{Database: root.Occurrence().Database}) {
		callers = followed(params, grapher.ws.FiredBy(root, true))
	}
	if len(callers) == 0 {
		return
	}
	if !slices.Contains(params.Expand, "more-in:"+params.Root) {
		stub := "more-in:" + params.Root
		grapher.out.Nodes = append(grapher.out.Nodes, Node{
			ID: stub, Database: stubMore, Stub: "fired-by", Title: fmt.Sprintf("+%d fired by", len(callers)),
		})
		grapher.out.Edges = append(grapher.out.Edges, Edge{From: stub, To: params.Root})
		return
	}
	for _, caller := range callers {
		if !grapher.drawn[caller.Name] {
			grapher.node(caller.Name, false)
		}
		grapher.out.Edges = append(grapher.out.Edges, Edge{From: caller.Name, To: params.Root, Via: caller.Via, Block: caller.Block})
	}
}

// stubTitle is how many fires a stub hides, and of what kind where they are all of one database.
func stubTitle(hidden []workspace.Fire) string {
	title := fmt.Sprintf("+%d more", len(hidden))
	database := hidden[0].Database
	if slices.ContainsFunc(hidden, func(fire workspace.Fire) bool { return fire.Database != database }) {
		return title
	}
	title += " " + game.KindName(database)
	if len(hidden) > 1 {
		title += "s"
	}
	return title
}
