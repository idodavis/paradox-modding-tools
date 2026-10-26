// ignored.go keeps the pairs of mods whose conflicts the open workspace ignores, and lists a finding for each pair.

package health

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// AddIgnoredPair makes the open workspace ignore the conflicts between two of its mods. It returns an error if they
// are not two mods of the workspace, or the config cannot be saved.
func (hlt *Service) AddIgnoredPair(first, second string) error {
	return hlt.updatePairs(first, second, func(pairs [][2]string, pair [2]string) ([][2]string, error) {
		if slices.Contains(pairs, pair) {
			return pairs, nil
		}
		return append(pairs, pair), nil
	})
}

// RemoveIgnoredPair makes the open workspace show the conflicts between two of its mods again. It returns an error if
// they are not two mods of the workspace, the pair is not ignored, or the config cannot be saved.
func (hlt *Service) RemoveIgnoredPair(first, second string) error {
	return hlt.updatePairs(first, second, func(pairs [][2]string, pair [2]string) ([][2]string, error) {
		if !slices.Contains(pairs, pair) {
			return nil, fmt.Errorf("the conflicts between %s and %s are not ignored", first, second)
		}
		return slices.DeleteFunc(pairs, func(other [2]string) bool { return other == pair }), nil
	})
}

// updatePairs changes the ignored pairs in the open workspace's record. It returns an error if the mods are not two of
// the workspace's, or the change or the save fails.
func (hlt *Service) updatePairs(first, second string, change func(pairs [][2]string, pair [2]string) ([][2]string, error)) error {
	ws, err := hlt.Workspace.Session()
	if err != nil {
		return err
	}
	if _, ok := findPair(ws, first, second); !ok {
		return fmt.Errorf("%s and %s are not two mods of the workspace", first, second)
	}
	return hlt.Workspace.Config.Update("workspaces", func(config *app.Config) error {
		position := slices.IndexFunc(config.Workspaces, func(record app.Workspace) bool { return record.ID == ws.ID })
		if position < 0 {
			return fmt.Errorf("the workspace %s is not recorded", ws.ID)
		}
		pairs, err := change(config.Workspaces[position].IgnoredPairs, makePair(first, second))
		if err == nil {
			config.Workspaces[position].IgnoredPairs = pairs
		}
		return err
	})
}

// listPairs returns the pairs of mods whose conflicts the workspace ignores. It returns none if the workspace has no
// record.
func (hlt *Service) listPairs(ws *workspace.Session) [][2]string {
	record, _ := lo.Find(hlt.Workspace.Config.Config().Workspaces, func(record app.Workspace) bool { return record.ID == ws.ID })
	return record.IgnoredPairs
}

// listPairFindings lists a finding for each ignored pair whose mods are both in the workspace. It counts the contests
// and the shared localization keys between the two.
func listPairFindings(ws *workspace.Session, mods *index.Index, findings []Finding, pairs [][2]string) []Finding {
	var pairFindings []Finding
	for _, pair := range pairs {
		ids, ok := findPair(ws, pair[0], pair[1])
		if !ok {
			continue
		}
		if ordered := listMods(mods, ids); len(ordered) == 2 {
			ids = ordered
		}
		count := lo.CountBy(findings, func(finding Finding) bool { return slices.Contains(finding.Pairs, [2]string(ids)) })
		facts := lsp.DescribeReference(schema.Reference{Name: describeMod(ws, ids[0])})
		facts["other"], facts["count"] = describeMod(ws, ids[1]), strconv.Itoa(count)
		finding := makeFinding(mods, "ignored-pair", facts, nil)
		finding.Origin, finding.Mods = ids[0], ids
		pairFindings = append(pairFindings, finding)
	}
	return pairFindings
}

// findPair returns two mods in load order. It returns false if either is not in the workspace or they are one mod.
func findPair(ws *workspace.Session, first, second string) ([]string, bool) {
	ids := lo.FilterMap(ws.Order, func(mod workspace.Mod, _ int) (string, bool) {
		return mod.ID, mod.ID == first || mod.ID == second
	})
	return ids, len(ids) == 2
}

// makePair returns a pair of mods in sorted order.
func makePair(first, second string) [2]string {
	return lo.Ternary(first <= second, [2]string{first, second}, [2]string{second, first})
}
