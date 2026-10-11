// graph_walk.go is the one hop out of a definition: the fires drawn, through scripted entries only where the page
// follows them, and the count of what a depth holds with no cap.

package pages

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/workspace"
)

// followed is the fires drawn, through a scripted effect only when the page follows them: every fire with Follow,
// else the ones with no scripted effect between.
func followed(params Params, fires []workspace.Fire) []workspace.Fire {
	if params.Follow {
		return fires
	}
	return slices.DeleteFunc(slices.Clone(fires), func(fire workspace.Fire) bool { return fire.Via != "" })
}

// countReach counts the definitions within depth hops of the root, each once, by kind and by origin; with
// Scripted, the scripted effects a chain passes through too.
func (grapher *grapher) countReach() {
	grapher.out.Kinds, grapher.out.Origins = map[string]int{}, map[string]int{}
	seen := map[string]bool{}
	grapher.countOnce(grapher.params.Root, seen)
	frontier := []string{grapher.params.Root}
	for hop := 0; hop < grapher.params.Depth && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			next = append(next, grapher.reachFrom(id, seen)...)
		}
		frontier = next
	}
}

// reachFrom counts what one node fires that is not counted yet, and says which are new, for the next hop.
func (grapher *grapher) reachFrom(id string, seen map[string]bool) (next []string) {
	for _, fire := range followed(grapher.params, grapher.ws.Fires(grapher.def(id))) {
		if grapher.params.Scripted && fire.Via != "" {
			for _, scripted := range strings.Split(fire.Via, " → ") {
				grapher.countOnce(scripted, seen)
			}
		}
		if !seen[fire.Name] {
			grapher.countOnce(fire.Name, seen)
			next = append(next, fire.Name)
		}
	}
	return next
}

// countOnce counts one definition, by its kind and by the origin that defines it, unless it is counted.
func (grapher *grapher) countOnce(id string, seen map[string]bool) {
	if seen[id] {
		return
	}
	seen[id] = true
	def := grapher.def(id)
	grapher.out.Reach++
	grapher.out.Kinds[game.KindName(def.Occurrence().Database)]++
	grapher.out.Origins[def.File().Origin]++
}
