// Package patch holds what each version of a game changed, as plain data. It
// keeps a game's patches in one folder of the app's cache, puts the versions
// in order and in ranges, answers what the patches of a range did to a name
// or a file, and compares what PMT keeps of two versions.
//
// A patch comes from one of three places: the game's Modding Co-op wrote it in
// its digest, the app ships it in the game's history, or PMT compared two
// versions itself when the game updated. The game's rules, as the package
// applies them:
//
//   - A version is compared against its base, the latest earlier version that
//     is not an open beta. So a release's changes are the sum of the betas'
//     before it.
//   - An open beta's version ends in -openbeta. Open betas count in a range
//     only where the range ends at one, which is where an install on a beta
//     is.
//   - A mod whose supported version matches the game's version is made for
//     it, and its range is empty. Else the range starts at the oldest version
//     the supported version matches, as 1.13.0 for 1.13.*. The patch of that
//     version is not in the range, since a mod made for it already holds its
//     changes.
//   - Where several sources have a version, the digest's patch is kept, then
//     the shipped one, then PMT's own. A part the kept patch lacks is taken
//     from the next that has it.
//
// # Robustness
//
// A file of the folder that is missing or cannot be read holds nothing: no
// patches, no snapshot, no copy. A supported version the launcher's reading of
// is not known, as 1.13*, decides no range. A version between two patches
// that no patch covers is not known, and is read as having changed nothing.
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

	// Local is whether PMT compared the two versions itself, when the game
	// updated or when the history the app ships was made. It is false for a
	// patch from a digest.
	Local bool `json:"local"`

	// DumpChanges are the keys of the dumps the version changed, and
	// DefinitionChanges the names the game's files began or stopped to
	// define, localization keys included. A digest lists no defined names,
	// so its version takes them from the shipped or PMT's own patch.
	DumpChanges       []schema.KeyChange `json:"dumpChanges"`
	DefinitionChanges []schema.KeyChange `json:"definitionChanges"`

	KeyRenames []KeyRename `json:"keyRenames"`

	// GameFiles are the game's files the version changed, sorted by path.
	GameFiles []GameFile `json:"gameFiles"`

	// Articles are the written notes of a digest. A patch PMT compared
	// itself has none.
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

// Article is one titled part of the written notes of a digest.
type Article struct {
	// ID names the article within its patch, and is the id of its heading.
	ID    string `json:"id"`
	Title string `json:"title"`

	// HTML is the article as a document the pages show: its heading, then
	// what the digest writes under it.
	HTML string `json:"html"`

	// Breaking is whether the article is one of the digest's breaking
	// changes, and New whether it is a new feature.
	Breaking bool `json:"breaking"`
	New      bool `json:"new"`

	// Folders are the folders of the game's script the article links a type
	// doc of, and Keys the names it writes in backticks. They are what ties
	// the article to a mod.
	Folders []string `json:"folders"`
	Keys    []string `json:"keys"`
}

// Patches is the patches of one game, or of one range of its versions, oldest
// version first.
type Patches []Patch

// Merge returns the patches of a digest, the ones the app ships and the ones
// PMT compared itself, one per version, oldest version first. Where several
// have a version, the digest's patch is kept, since a person wrote it down
// with the whole of the version's notes, then the shipped one. Each part of
// the kept patch that is empty is taken from the next patch that has it, such
// as the defined names, which no digest lists.
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

// fillPart gives an empty part of a patch the same part of another patch of
// the version.
func fillPart[T any](part *[]T, other []T) {
	if len(*part) == 0 {
		*part = other
	}
}

// Put returns the patches with one added, in place of the patch of the same
// version where they have one.
func Put(patches []Patch, patch Patch) []Patch {
	patches = slices.DeleteFunc(patches, func(other Patch) bool { return other.Version == patch.Version })

	return append(patches, patch)
}

// MatchVersion reports whether a mod made for a supported version runs on a
// game version. It compares part by part, and a * matches the rest. judged is
// false where it is not known how the games compare, such as for a * inside a
// part or a build id.
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

// ResolveStart returns the version the range of a mod starts at when it ends
// at the given version, and whether the mod's supported version decides it.
// A supported version that matches the end gives the end, as 1.15.* does for
// 1.15.1: the mod is made for the game as it is, so no patch counts. Else the
// start is the oldest version the supported version matches, as 1.13.0 for
// 1.13.*, among the patches' versions and the versions they are compared
// against. A supported version that is blank, or that cannot be judged, as
// 1.13* cannot, gives the mod's base version, which the user picks. One that
// matches only open betas, which count only where the range ends at one,
// gives the latest release before them. The start is "" if there is none of
// these. A version past the end is never matched.
func (p Patches) ResolveStart(supported, base, end string) (start string, judged bool) {
	switch matches, judged := MatchVersion(supported, end); {
	case !judged:
		return base, false
	case matches:
		return end, true
	}

	// A version a patch is compared against counts as much as the patch's own:
	// PMT's first patch of an update is compared against a version that no
	// patch of its own holds.
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

// IsBeta reports whether a version is an open beta. That is a version that
// game.IsBeta says is one, and a number the patches hold only as an open
// beta, since the digests never list one number as a release and as a beta.
func (p Patches) IsBeta(version string) bool {
	if game.IsBeta(version) {
		return true
	}

	isHeld := slices.ContainsFunc(p, func(patch Patch) bool { return patch.Version == version })

	return !isHeld && slices.ContainsFunc(p, func(patch Patch) bool {
		return game.IsBeta(patch.Version) && strings.HasPrefix(patch.Version, version+"-")
	})
}

// ResolveBase returns the version the patch of a version is compared
// against: the latest earlier version that is not an open beta. It returns
// "" if there is none.
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
// none if the start or the end is "".
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

// Changes yields each change the patches made to a key of the dumps in one
// database, with the version of its patch, oldest patch first.
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

// FindRename returns the key that the patches gave in place of a key, and the
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

// FindRemoval returns the version of the oldest patch that removed a name: a
// key of the dumps, as a command is, or, where defined names count too, a name
// the game's files defined, a localization key included. It returns false if
// none removed it. A key that a patch only changed is not removed.
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
// and that patch's version. The file is given by its path from the game's
// folder. It returns false if no patch lists it.
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
