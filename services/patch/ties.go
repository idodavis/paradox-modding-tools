// ties.go says which articles of the game's patches touch each mod of the
// open workspace, and makes a mod made for the install's release.

package patch

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Tie is one article of a patch that touches a mod, because it links the type
// doc of a folder the mod writes files in or writes a key in backticks that
// the mod defines or uses.
type Tie struct {
	ModID   string `json:"modId"`
	Version string `json:"version"`

	// ID, Title, Breaking and New are the article's, in the patch of Version.
	ID       string `json:"id"`
	Title    string `json:"title"`
	Breaking bool   `json:"breaking"`
	New      bool   `json:"new"`

	// Count is the mod's files in the article's folders and its definitions
	// and uses of the article's keys, together. It is never 0.
	Count int `json:"count"`
}

// ListTies returns the articles that touch each mod of the open workspace,
// among the patches after the version the mod is made for. The mods are in
// load order, and a mod's ties oldest version first. A mod with no version to
// count from has none, and a tie is made only by an exact link or key, never
// by a likeness of words. It returns an error if no workspace is open.
func (s *Service) ListTies() ([]Tie, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	// A mod's files are held by their path as the game reads them, in lower
	// case, and a key's definitions and uses are counted once for all the mods.
	mods := ws.Mods.Load()
	rels := map[string][]string{}
	for _, position := range mods.ListOwnFiles() {
		file := mods.Files[position]
		rels[file.Origin] = append(rels[file.Origin], strings.ToLower(file.StageRel()))
	}

	uses := map[string]map[string]int{}
	ties := []Tie{}
	for _, mod := range *ws.Order.Load() {
		for _, gamePatch := range ws.ListModPatches(mod.ID) {
			for _, article := range gamePatch.Articles {
				count := countFiles(rels[mod.ID], article.Folders)
				for _, key := range article.Keys {
					count += countUses(mods, uses, key)[mod.ID]
				}

				if count > 0 {
					ties = append(ties, Tie{
						ModID: mod.ID, Version: gamePatch.Version, ID: article.ID, Title: article.Title,
						Breaking: article.Breaking, New: article.New, Count: count,
					})
				}
			}
		}
	}

	return ties, nil
}

// commandDatabases are the databases of the game's built-in commands. The
// index holds their uses, but only the schema holds their definitions, so no
// lookup of the index's definitions names them.
var commandDatabases = []string{lang.DumpEffects, lang.DumpTriggers}

// countUses returns, by the mod's id, how many times each mod defines or uses a
// key: in every database that defines the key, and as a built-in effect or
// trigger. A key nothing defines or uses has none. Each key's counts are kept
// in the given map, so a key is counted once.
func countUses(mods *index.Index, uses map[string]map[string]int, key string) map[string]int {
	if counts, ok := uses[key]; ok {
		return counts
	}

	counts := map[string]int{}
	for _, database := range slices.Concat(mods.ListDatabases(key), commandDatabases) {
		sites := slices.Concat(mods.Resolve(lang.Reference{Database: database, Name: key}), mods.ListUses(database, key))
		for _, site := range sites {
			counts[mods.Files[site.File].Origin]++
		}
	}

	uses[key] = counts

	return counts
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

// UpdateSupportedVersion makes a mod of the user's own made for the install's
// release, as 1.15.* for 1.15.1. It writes that version into the mod's
// descriptor and changes nothing else the descriptor says. It returns an
// error if no workspace is open, the mod is not in it or is someone else's,
// or the install's version names no release.
func (s *Service) UpdateSupportedVersion(modID string) error {
	ws, err := s.Workspace.Session()
	if err != nil {
		return err
	}

	mod, ok := s.Workspace.LookupMod(modID)
	if !ok || mod.External {
		return fmt.Errorf("%s is not a mod of yours in the open workspace", modID)
	}

	supported := workspace.ToSupportedVersion(ws.Schema.Version)
	if supported == "" {
		return fmt.Errorf("the install's version %q names no release to make the mod for", ws.Schema.Version)
	}

	mod.Descriptor.SupportedVersion = supported

	return ws.WriteDescriptor(modID, mod.Descriptor)
}
