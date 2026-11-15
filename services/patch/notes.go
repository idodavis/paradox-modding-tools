// notes.go lists the versions the Patch Center shows, and gives the patch of
// one.

package patch

import (
	"errors"
	"slices"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/services/wiki"
)

// Note is one version the Patch Center lists: the wiki's note of it, if it has
// one, and what the game's patches and the workspace's mods say of it.
type Note struct {
	// Note is the wiki's note. A version the wiki has no note for has only
	// its Version.
	wiki.Note

	// Release is the version's release, as 1.15 for 1.15.1, which the page
	// lists it under.
	Release string `json:"release"`

	// Known is whether a patch holds the version, so its changes are known.
	Known bool `json:"known"`

	// Beta is whether the version is an open beta.
	Beta bool `json:"beta"`

	// Mods are the ids of the user's own mods in the open workspace that are
	// made for this version: their supported_version matches it. An external
	// mod is never listed, since it is not the user's to patch.
	Mods []string `json:"mods"`
}

// ListPatches returns the versions the workspace's patches hold, and the
// versions of the wiki's notes that no patch takes, newest first. A patch is
// listed with the note that holds its version, as wiki.ResolveNote says. Until
// the wiki's listing
// is fetched, it lists the patches' versions, which have none of the wiki's
// fields. It returns an error if no workspace is open or the wiki fails
// otherwise.
func (s *Service) ListPatches() ([]Note, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	notes, err := s.Wiki.ListNotes()
	if err != nil && !errors.Is(err, wiki.ErrNoListing) {
		return nil, err
	}

	patches := *ws.Patches.Load()
	pairedNotes := map[string]bool{}
	var rows []Note
	for _, gamePatch := range patches {
		noteVersion := wiki.ResolveNote(gamePatch.Version)
		pairedNotes[noteVersion] = true

		row := Note{Known: true}
		position := slices.IndexFunc(notes, func(note wiki.Note) bool { return note.Version == noteVersion })
		if position >= 0 {
			row.Note = notes[position]
		}

		row.Version = gamePatch.Version
		rows = append(rows, row)
	}

	for _, note := range notes {
		if !pairedNotes[note.Version] {
			rows = append(rows, Note{Note: note})
		}
	}

	for position := range rows {
		row := &rows[position]
		row.Release = game.ResolveRelease(row.Version)
		row.Beta, row.Mods = patches.IsBeta(row.Version), []string{}
		for _, mod := range *ws.Order.Load() {
			if matches, _ := patch.MatchVersion(mod.Descriptor.SupportedVersion, row.Version); matches && !mod.External {
				row.Mods = append(row.Mods, mod.ID)
			}
		}
	}

	slices.SortFunc(rows, func(first, second Note) int { return game.CompareVersions(second.Version, first.Version) })

	return rows, nil
}

// FindPatch returns the patch of a version. It returns the zero patch, with no
// version, for a version the workspace's patches do not hold, which is a
// version whose changes are not known yet. It returns an error if no workspace
// is open.
func (s *Service) FindPatch(version string) (patch.Patch, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return patch.Patch{}, err
	}

	patches := *ws.Patches.Load()
	position := slices.IndexFunc(patches, func(gamePatch patch.Patch) bool { return gamePatch.Version == version })
	if position < 0 {
		return patch.Patch{}, nil
	}

	return patches[position], nil
}
