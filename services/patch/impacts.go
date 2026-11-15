// impacts.go says what the game's patches mean for each mod of the open
// workspace: the findings Health raises and the articles that touch the mod.

package patch

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/workspace"
)

// Tie is one article of a patch that touches a mod, because it links the type
// doc of a folder the mod writes files in or writes a key in backticks that
// the mod defines or uses.
type Tie struct {
	ModID   string `json:"modId"`
	Version string `json:"version"`

	// Article is the article that touches the mod, in the patch of Version.
	Article workspace.Article `json:"article"`

	// Count is the mod's files in the article's folders and its definitions
	// and uses of the article's keys, together. It is never 0.
	Count int `json:"count"`
}

// Impact is what the game's patches mean for one mod, from the version it is
// made for to the install's.
type Impact struct {
	ModID string `json:"modId"`

	// Start and End are the versions the mod's patches run from and to, as
	// Session.ResolveRange gives them. Start is "" where the mod has no
	// version to count from.
	Start string `json:"start"`
	End   string `json:"end"`

	// Judged is true where the mod's supported_version decides its range. It
	// is false for a blank one, or one the launcher's reading of is not known,
	// as 1.13*, so the range starts at the mod's base version.
	Judged bool `json:"judged"`

	// Findings are the mod's findings of Health's patch question.
	Findings []health.Finding `json:"findings"`

	// Ties are the articles of the mod's patches that touch it, oldest
	// version first.
	Ties []Tie `json:"ties"`
}

// ListImpacts returns the impact of the patches on each mod of the open
// workspace, in load order. A mod with no start version has no ties, and a
// tie is made only by an exact link or key, never by a likeness of words. It
// returns an error if no workspace is open or Health's report cannot be built.
func (s *Service) ListImpacts() ([]Impact, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	report, err := s.Health.BuildReport(nil)
	if err != nil {
		return nil, err
	}

	patches, order := *ws.Patches.Load(), *ws.Order.Load()
	impacts := make([]Impact, len(order))
	ranges := make([][]workspace.Patch, len(order))
	var every []workspace.Patch
	for position, mod := range order {
		start, end := ws.ResolveRange(mod.ID)
		_, judged := workspace.MatchVersion(mod.Descriptor.SupportedVersion, end)
		ranges[position] = patches.ListRange(start, end)
		every = append(every, ranges[position]...)
		impacts[position] = Impact{
			ModID: mod.ID, Start: start, End: end, Judged: judged, Findings: listFindings(report.Patch.Findings, mod.ID),
		}
	}

	rels, uses := listHoldings(ws.Mods.Load(), every)
	for position := range impacts {
		impacts[position].Ties = listTies(impacts[position].ModID, ranges[position], rels, uses)
	}

	return impacts, nil
}

// FindPatch returns the patch of a version and the ties of every mod whose
// patches include it. It returns the zero patch, with no version, for a
// version the workspace's patches do not hold, which is a version whose
// changes are not known yet. It returns an error if no workspace is open.
func (s *Service) FindPatch(version string) (workspace.Patch, []Tie, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return workspace.Patch{}, nil, err
	}

	patches := *ws.Patches.Load()
	position := slices.IndexFunc(patches, func(patch workspace.Patch) bool { return patch.Version == version })
	if position < 0 {
		return workspace.Patch{}, []Tie{}, nil
	}

	one := []workspace.Patch{patches[position]}
	rels, uses := listHoldings(ws.Mods.Load(), one)
	ties := []Tie{}
	for _, mod := range *ws.Order.Load() {
		start, end := ws.ResolveRange(mod.ID)
		inRange := slices.ContainsFunc(patches.ListRange(start, end), func(patch workspace.Patch) bool { return patch.Version == version })
		if inRange {
			ties = append(ties, listTies(mod.ID, one, rels, uses)...)
		}
	}

	return one[0], ties, nil
}

// UpdateSupportedVersion makes a mod made for the install's release, as 1.15.*
// for 1.15.1. A mod of the user's own has a Publish listing, so the listing is
// written, which keeps the descriptor and Steam's version tag the same; any
// other mod's descriptor is written alone. It returns an error if no workspace
// is open, the mod is not in it, or the install's version names no release.
func (s *Service) UpdateSupportedVersion(modID string) error {
	ws, err := s.Workspace.Session()
	if err != nil {
		return err
	}

	mod, ok := s.Workspace.LookupMod(modID)
	if !ok {
		return fmt.Errorf("the mod %s is not in the open workspace", modID)
	}

	supported := workspace.ToSupportedVersion(ws.Schema.Version)
	if supported == "" {
		return fmt.Errorf("the install's version %q names no release to make the mod for", ws.Schema.Version)
	}

	if mod.External {
		mod.Descriptor.SupportedVersion = supported

		return ws.WriteDescriptor(modID, mod.Descriptor)
	}

	listing, err := s.Steam.ReadListing(modID, "")
	if err != nil {
		return err
	}

	listing.SupportedVersion = supported

	return s.Steam.WriteListing(modID, "", listing)
}

// listFindings returns the findings of a mod, as Health's patch question
// raises them. It is empty, never nil, for a mod with none.
func listFindings(findings []health.Finding, modID string) []health.Finding {
	listed := []health.Finding{}
	for _, finding := range findings {
		if finding.Origin == modID {
			listed = append(listed, finding)
		}
	}

	return listed
}

// listHoldings returns what the mods hold that an article can tie to: each
// mod's files by their path as the game reads them in lower case, and for each
// key the patches' articles write in backticks, the mod's definitions and uses
// of it. Both are by the mod's id.
func listHoldings(mods *index.Index, patches []workspace.Patch) (rels map[string][]string, uses map[string]map[string]int) {
	rels = map[string][]string{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		rels[file.Origin] = append(rels[file.Origin], strings.ToLower(file.StageRel()))
	}

	uses = map[string]map[string]int{}
	for _, patch := range patches {
		for _, article := range patch.Articles {
			for _, key := range article.Keys {
				if uses[key] == nil {
					uses[key] = countUses(mods, key)
				}
			}
		}
	}

	return rels, uses
}

// commandDatabases are the databases of the game's built-in commands. The
// index holds their uses, but only the schema holds their definitions, so no
// lookup of the index's definitions names them.
var commandDatabases = []string{lang.DumpEffects, lang.DumpTriggers}

// countUses returns, by the mod's id, how many times each mod defines or uses a
// key: in every database that defines the key, and as a built-in effect or
// trigger. A key nothing defines or uses has none.
func countUses(mods *index.Index, key string) map[string]int {
	counts := map[string]int{}
	for _, database := range slices.Concat(mods.ListDatabases(key), commandDatabases) {
		sites := slices.Concat(mods.Resolve(lang.Reference{Database: database, Name: key}), mods.ListUses(database, key))
		for _, site := range sites {
			counts[mods.Files[site.File].Origin]++
		}
	}

	return counts
}

// listTies returns the articles of the given patches that touch a mod, as Tie
// says. It is empty, never nil, where none does.
func listTies(modID string, patches []workspace.Patch, rels map[string][]string, uses map[string]map[string]int) []Tie {
	ties := []Tie{}
	for _, patch := range patches {
		for _, article := range patch.Articles {
			count := countFiles(rels[modID], article.Folders)
			for _, key := range article.Keys {
				count += uses[key][modID]
			}

			if count > 0 {
				ties = append(ties, Tie{ModID: modID, Version: patch.Version, Article: article, Count: count})
			}
		}
	}

	return ties
}

// countFiles returns how many of the given files are in one of the folders.
func countFiles(rels, folders []string) int {
	count := 0
	for _, rel := range rels {
		if slices.ContainsFunc(folders, func(folder string) bool { return strings.HasPrefix(rel, folder+"/") }) {
			count++
		}
	}

	return count
}
