// Package patch holds what each version of a game changed. It keeps a game's
// patches in a folder of the app's cache, orders the versions, and answers
// what the patches of a range did to a name or a file.
//
// A patch comes from the game's Modding Co-op digest, from the history the
// app ships, or from PMT comparing two versions itself. The rules it applies:
//
//   - A version is compared against its base: the latest earlier version
//     that is not an open beta.
//   - An open beta's version ends in -openbeta. Open betas count in a range
//     only when the range ends at one.
//   - A mod whose supported version matches the game's version has an empty
//     range. Else the range starts at the oldest version the supported
//     version matches, as 1.13.0 for 1.13.*, and leaves that version out.
//   - Where several sources have a version, the digest's patch wins, then
//     the shipped one, then PMT's own. An empty part is filled from the next.
//
// # Robustness
//
// A file of the folder that is missing or unreadable holds nothing. A
// supported version that cannot be judged, as 1.13*, decides no range. A
// version no patch covers is read as having changed nothing.
package patch

import (
	"cmp"
	"iter"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// Patch is what one version of a game changed against its base version.
type Patch struct {
	Version string `json:"version"`

	// Base is the version this one is compared against, "" for none.
	Base string `json:"base"`

	// Local is true where PMT compared the two versions itself.
	Local bool `json:"local"`

	// DumpChanges are the changed keys of the dumps. DefinitionChanges are
	// the names the game's files began or stopped defining, which no digest
	// lists.
	DumpChanges       []schema.KeyChange `json:"dumpChanges"`
	DefinitionChanges []schema.KeyChange `json:"definitionChanges"`

	KeyRenames []KeyRename `json:"keyRenames"`

	// GameFiles are the game's files the version changed, sorted by path.
	GameFiles []GameFile `json:"gameFiles"`

	// Articles are a digest's written notes.
	Articles []Article `json:"articles"`
}

// KeyRename is a key that a patch gave a new key.
type KeyRename struct {
	Key         string `json:"key"`
	Replacement string `json:"replacement"`
}

// GameFile is a file of the game that a patch added, changed or removed.
type GameFile struct {
	// Rel is the file's path from the game's folder.
	Rel    string        `json:"rel"`
	Status schema.Status `json:"status"`
}

// Article is one titled part of a digest's written notes.
type Article struct {
	// ID is the id of the article's heading.
	ID    string `json:"id"`
	Title string `json:"title"`

	// HTML is the article's heading and body.
	HTML string `json:"html"`

	// Breaking and New mark a breaking change and a new feature.
	Breaking bool `json:"breaking"`
	New      bool `json:"new"`

	// Folders are the script folders the article links, and Keys the names
	// it writes in backticks. They tie the article to a mod.
	Folders []string `json:"folders"`
	Keys    []string `json:"keys"`
}

// Patches is the patches of one game, or of one range of its versions, oldest
// version first.
type Patches []Patch

// Merge returns one patch per version, oldest first, from the three sources.
// The digest's patch wins, then the shipped one, and an empty part of the
// winner is filled from the next.
func Merge(digest, shipped, local []Patch) Patches {
	var merged Patches
	for _, patches := range [][]Patch{digest, shipped, local} {
		for _, patch := range patches {
			position := slices.IndexFunc(merged, func(other Patch) bool { return other.Version == patch.Version })
			if position < 0 {
				merged = append(merged, patch)

				continue
			}

			target := &merged[position]
			fillPart(&target.DumpChanges, patch.DumpChanges)
			fillPart(&target.DefinitionChanges, patch.DefinitionChanges)
			fillPart(&target.KeyRenames, patch.KeyRenames)
			fillPart(&target.GameFiles, patch.GameFiles)
			fillPart(&target.Articles, patch.Articles)
		}
	}

	slices.SortFunc(merged, func(first, second Patch) int {
		return game.CompareVersions(first.Version, second.Version)
	})

	return merged
}

// fillPart sets an empty part of a patch from another patch.
func fillPart[T any](part *[]T, other []T) {
	if len(*part) == 0 {
		*part = other
	}
}

// Put returns the patches with one added, replacing the patch of the same
// version.
func Put(patches []Patch, patch Patch) []Patch {
	patches = slices.DeleteFunc(patches, func(other Patch) bool { return other.Version == patch.Version })

	return append(patches, patch)
}

// MatchVersion reports whether a mod made for a supported version runs on a
// game version. A * matches the rest. judged is false where the comparison
// is not known, as for 1.13* or a build id.
func MatchVersion(supported, version string) (matches, judged bool) {
	wanted, actual := strings.Split(supported, "."), strings.Split(version, ".")
	notNumber := func(part string) bool { return jomini.Classify(part) != jomini.ClassNumber }
	notWanted := func(part string) bool { return part != "*" && notNumber(part) }
	judged = supported != "" && !slices.ContainsFunc(wanted, notWanted) && !slices.ContainsFunc(actual, notNumber)

	for position, part := range wanted {
		switch {
		case part == "*":
			return true, judged
		case position >= len(actual) || part != actual[position]:
			return false, judged
		}
	}

	return len(wanted) == len(actual), judged
}

// ResolveStart returns the version a mod's range starts at, for a range that
// ends at the given version, and whether the mod's supported version decided
// it. A supported version that matches the end gives the end. Else the start
// is the oldest version it matches, as 1.13.0 for 1.13.*. A blank or unjudged
// supported version gives the mod's base version. The start is "" for none.
func (p Patches) ResolveStart(supported, base, end string) (start string, judged bool) {
	switch matches, judged := MatchVersion(supported, end); {
	case !judged:
		return base, false
	case matches:
		return end, true
	}

	// A patch's base counts too: PMT's first patch of an update is compared
	// against a version that no patch holds.
	skipBetas := !p.IsBeta(end)
	beta := ""
	for _, patch := range p {
		for _, version := range []string{patch.Base, patch.Version} {
			number, _, _ := strings.Cut(version, "-")
			if matches, _ := MatchVersion(supported, number); !matches || game.CompareVersions(version, end) > 0 {
				continue
			}

			if !skipBetas || !game.IsBeta(version) {
				return version, true
			}

			beta = cmp.Or(beta, version)
		}
	}

	return p.ResolveBase(beta), true
}

// IsBeta reports whether a version is an open beta: one game.IsBeta names, or
// a number the patches hold only as an open beta.
func (p Patches) IsBeta(version string) bool {
	if game.IsBeta(version) {
		return true
	}

	isHeld := slices.ContainsFunc(p, func(patch Patch) bool { return patch.Version == version })

	return !isHeld && slices.ContainsFunc(p, func(patch Patch) bool {
		return game.IsBeta(patch.Version) && strings.HasPrefix(patch.Version, version+"-")
	})
}

// ResolveBase returns the latest version before the given one that is not an
// open beta, "" for none.
func (p Patches) ResolveBase(version string) string {
	base := ""
	for _, patch := range p {
		if game.IsBeta(patch.Version) || game.CompareVersions(patch.Version, version) >= 0 {
			continue
		}

		if base == "" || game.CompareVersions(patch.Version, base) > 0 {
			base = patch.Version
		}
	}

	return base
}

// ListRange returns the patches after a start version, up to and including
// an end version. Open betas are left out unless the end is one. It returns
// none if either version is "".
func (p Patches) ListRange(start, end string) Patches {
	if start == "" || end == "" {
		return nil
	}

	skipBetas := !p.IsBeta(end)
	var patches Patches
	for _, patch := range p {
		if game.CompareVersions(patch.Version, start) <= 0 || game.CompareVersions(patch.Version, end) > 0 {
			continue
		}

		if !skipBetas || !game.IsBeta(patch.Version) {
			patches = append(patches, patch)
		}
	}

	return patches
}

// Changes yields each change the patches made to a key of one database of
// the dumps, with its patch's version, oldest patch first.
func (p Patches) Changes(database string) iter.Seq2[string, schema.KeyChange] {
	return func(yield func(string, schema.KeyChange) bool) {
		for _, patch := range p {
			for _, change := range patch.DumpChanges {
				if change.Database == database && !yield(patch.Version, change) {
					return
				}
			}
		}
	}
}

// FindRename returns the key the patches gave in place of a key, and the
// version of the oldest patch that did. It returns false if none renamed it.
func (p Patches) FindRename(key string) (replacement, version string, ok bool) {
	for _, patch := range p {
		for _, rename := range patch.KeyRenames {
			if rename.Key == key {
				return rename.Replacement, patch.Version, true
			}
		}
	}

	return "", "", false
}

// FindRemoval returns the version of the oldest patch that removed a name.
// It looks among the keys of the dumps, and with countDefined among the
// names the game's files defined too.
func (p Patches) FindRemoval(name string, countDefined bool) (string, bool) {
	isRemoved := func(change schema.KeyChange) bool {
		return change.Key == name && change.Status == schema.StatusRemoved
	}
	for _, patch := range p {
		isDefined := countDefined && slices.ContainsFunc(patch.DefinitionChanges, isRemoved)
		if isDefined || slices.ContainsFunc(patch.DumpChanges, isRemoved) {
			return patch.Version, true
		}
	}

	return "", false
}

// FindFile returns what the newest patch that lists a game file did to it,
// and that patch's version. It returns false if no patch lists the file.
func (p Patches) FindFile(rel string) (GameFile, string, bool) {
	for _, patch := range slices.Backward(p) {
		if position, ok := slices.BinarySearchFunc(patch.GameFiles, GameFile{Rel: rel}, CompareFiles); ok {
			return patch.GameFiles[position], patch.Version, true
		}
	}

	return GameFile{}, "", false
}

// CompareFiles orders game files by path, which is how a patch lists them.
func CompareFiles(first, second GameFile) int {
	return strings.Compare(first.Rel, second.Rel)
}
