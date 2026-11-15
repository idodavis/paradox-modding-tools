// notes.go lists the versions the Patch Center shows.

package patch

import (
	"errors"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// Note is one version the Patch Center lists: the wiki's note of it, if it has
// one, and what the game's digest and the workspace's mods say of it.
type Note struct {
	// Note is the wiki's note. A version the wiki has no note for has only
	// its Version.
	wiki.Note

	// Known is whether a patch holds the version, so its changes are known.
	Known bool `json:"known"`

	// Beta is whether the version is an open beta.
	Beta bool `json:"beta"`

	// Mods are the ids of the open workspace's mods made for this version,
	// whose supported_version matches it.
	Mods []string `json:"mods"`
}

// ListPatches returns the versions the workspace's patches hold, from the
// digest or PMT's own comparison, and the versions of the wiki's notes that no
// patch takes, newest first. A patch is listed with the note it takes, as
// resolveNote says. Until the wiki's listing is fetched, it lists the patches'
// versions, which have none of the wiki's fields. It returns an error if no
// workspace is open or the wiki fails otherwise.
func (s *Service) ListPatches() ([]Note, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	notes, err := s.Wiki.ListNotes()
	if err != nil && !errors.Is(err, wiki.ErrNoListing) {
		return nil, err
	}

	patches, order := *ws.Patches.Load(), *ws.Order.Load()
	taken := map[string]bool{}
	var listed []Note
	for _, patch := range patches {
		key := resolveNote(patch.Version)
		note := findNote(notes, key)
		taken[key] = true
		note.Version = patch.Version

		row := makeNote(note, patches, order)
		row.Known = true
		listed = append(listed, row)
	}

	for _, note := range notes {
		if !taken[note.Version] {
			listed = append(listed, makeNote(note, patches, order))
		}
	}

	slices.SortFunc(listed, func(first, second Note) int { return game.CompareVersions(second.Version, first.Version) })

	return listed, nil
}

// resolveNote returns the version of the wiki's note that a patch takes. The
// wiki writes one note for a release, as 1.15, and one for its hotfixes, as
// 1.15.X. A patch whose numbers after the first two are all 0, as 1.15.0, takes
// the release's note. Any other patch of the release, as 1.15.1, takes the
// hotfixes'. An open beta is read by its number. It returns "" for a version
// with fewer than two numbers.
func resolveNote(version string) string {
	number, _, _ := strings.Cut(version, "-")
	parts := strings.Split(number, ".")
	if len(parts) < 2 {
		return ""
	}

	release := parts[0] + "." + parts[1]
	if slices.ContainsFunc(parts[2:], func(part string) bool { return part != "0" }) {
		return release + ".X"
	}

	return release
}

// findNote returns the note of a version, or the zero note if there is none.
func findNote(notes []wiki.Note, version string) wiki.Note {
	position := slices.IndexFunc(notes, func(note wiki.Note) bool { return note.Version == version })
	if position < 0 {
		return wiki.Note{}
	}

	return notes[position]
}

// makeNote returns the note of a version with whether it is an open beta, and
// the mods made for it.
func makeNote(note wiki.Note, patches workspace.Patches, mods []workspace.Mod) Note {
	listed := Note{Note: note, Beta: patches.IsBeta(note.Version), Mods: []string{}}
	for _, mod := range mods {
		if matches, _ := workspace.MatchVersion(mod.Descriptor.SupportedVersion, note.Version); matches {
			listed.Mods = append(listed.Mods, mod.ID)
		}
	}

	return listed
}
