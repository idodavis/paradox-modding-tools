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

	// Installed is whether the workspace's install is on this version.
	Installed bool `json:"installed"`

	// Mods are the ids of the user's own mods made for this version. An
	// external mod is never listed.
	Mods []string `json:"mods"`
}

// ListPatches returns the versions the workspace's patches hold, and the
// versions of the wiki's notes that no patch covers, newest first. Until the
// wiki's listing is fetched, the rows have none of the wiki's fields. It
// returns an error if no workspace is open or the wiki fails.
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
		row.Installed = game.IsInstalled(row.Version, ws.Schema.Version)
		for _, mod := range *ws.Order.Load() {
			if matches, _ := patch.MatchVersion(mod.Descriptor.SupportedVersion, row.Version); matches && !mod.External {
				row.Mods = append(row.Mods, mod.ID)
			}
		}
	}

	slices.SortFunc(rows, func(first, second Note) int { return game.CompareVersions(second.Version, first.Version) })

	return rows, nil
}

// FindPatch returns the patch of a version, or the zero patch for a version
// whose changes are not known. It returns an error if no workspace is open.
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
