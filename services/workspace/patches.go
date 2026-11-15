// patches.go holds what each version of a game changed, as plain data, and
// puts a game's versions in order and in ranges.

package workspace

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// Patch is what one version of a game changed against its base version.
type Patch struct {
	Version string `json:"version"`

	// Base is the version this one is compared against, "" for none.
	Base string `json:"base"`

	// Local is whether PMT compared the dumps itself, when the game updated or
	// when the history the app ships was made. It is false for a patch from a
	// digest.
	Local bool `json:"local"`

	// DumpChanges, KeyRenames, GameFiles and Articles are what the version
	// changed. A patch PMT compared itself has no Articles.
	DumpChanges []schema.DumpChange `json:"dumpChanges"`
	KeyRenames  []KeyRename         `json:"keyRenames"`
	GameFiles   []GameFile          `json:"gameFiles"`
	Articles    []Article           `json:"articles"`

	// DocChanges are the keys the game's docs began or stopped to document,
	// and DefinitionChanges the names the game's files began or stopped to
	// define, localization keys included. A digest lists neither, so a
	// version the digest has takes them from the shipped or PMT's own patch.
	DocChanges        []schema.KeyChange `json:"docChanges"`
	DefinitionChanges []schema.KeyChange `json:"definitionChanges"`
}

// KeyRename is a key that a patch gave a new key.
type KeyRename struct {
	// Key is the old key, and Replacement the key that took its place.
	Key         string `json:"key"`
	Replacement string `json:"replacement"`
}

// GameFile is a file of the game that a patch added, modified or deleted.
type GameFile struct {
	// Rel is the file's path from the game's folder.
	Rel string `json:"rel"`

	// Status is FileAdded, FileModified or FileDeleted.
	Status string `json:"status"`
}

// The statuses of a game file, in git's words.
const (
	FileAdded    = "added"
	FileModified = "modified"
	FileDeleted  = "deleted"
)

// Article is one titled part of the written notes of a digest.
type Article struct {
	Title string `json:"title"`

	// Markdown is the article as the digest writes it.
	Markdown string `json:"markdown"`

	// Breaking is whether the article is one of the digest's breaking
	// changes, and New whether it is a new feature.
	Breaking bool `json:"breaking"`
	New      bool `json:"new"`

	// Folders are the folders of the game's script the article links to a
	// type doc of, and Keys the definitions and commands it writes in
	// backticks. They are what ties the article to a mod.
	Folders []string `json:"folders"`
	Keys    []string `json:"keys"`
}

// Patches is the patches of one game, oldest version first.
type Patches []Patch

// MergePatches returns the patches of a digest, the ones the app ships and the
// ones PMT compared itself, one per version, oldest version first. Where
// several have a version, the digest's patch is kept, since a person wrote it
// down with the whole of the version's notes, then the shipped one, which was
// compared with the game's own files and not only its dumps. Each part of the
// kept patch that is empty is taken from the next patch that has it, such as
// the docs' keys, which no digest lists.
func MergePatches(digest, shipped, local []Patch) Patches {
	var merged Patches
	for _, patches := range [][]Patch{digest, shipped, local} {
		for _, patch := range patches {
			position := slices.IndexFunc(merged, func(other Patch) bool { return other.Version == patch.Version })
			if position < 0 {
				merged = append(merged, patch)

				continue
			}

			kept := &merged[position]
			fillPart(&kept.DumpChanges, patch.DumpChanges)
			fillPart(&kept.KeyRenames, patch.KeyRenames)
			fillPart(&kept.GameFiles, patch.GameFiles)
			fillPart(&kept.Articles, patch.Articles)
			fillPart(&kept.DocChanges, patch.DocChanges)
			fillPart(&kept.DefinitionChanges, patch.DefinitionChanges)
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

// ResolvePatchDir returns the folder of the app's cache that holds one game's
// patches. It is not in the game's folder of schema caches, which a workspace
// open empties, so the patches outlive an update of the game.
//
// The folder holds the patches of the game's digest, the id of the commit they
// were read from, when GitHub was last asked for it, and for each version that
// an install of the game is on and the digest has, that version's dumps. The
// patch service saves them. The
// folder also holds the patches PMT compared itself and, for each version an
// install is on, the install's own dumps of it from the last time they were
// not stale, which the session saves, and the keys of each version the session
// saw. It also holds the hash of each game file by version and the copies of
// the game files that mods replace, which the session keeps.
func ResolvePatchDir(cacheDir string, row *game.Game) string {
	return filepath.Join(cacheDir, "patches", row.ID)
}

// The files of a game's folder of patches. The first holds the digest's
// patches as a []Patch, the second the id of the commit they were read from,
// the third the time GitHub was last asked for that id, in RFC 3339, the fourth
// is the pattern of the file that holds one version's dumps as a schema.Dumps
// with no file or line on its commands, the fifth holds the patches PMT
// compared itself as a []Patch, the sixth is the pattern of the file that
// holds one version of the install's own dumps as a schema.Dumps, and the
// seventh is the pattern of the file that holds one version's keys as a
// VersionKeys.
const (
	digestFile  = "digest.gob"
	shaFile     = "sha.txt"
	checkedFile = "checked.txt"
	dumpsFile   = "dumps-%s.gob"
	localFile   = "local.gob"
	ownFile     = "own-%s.gob"
	keysFile    = "keys-%s.gob"
)

// VersionKeys are the keys the docs document and the names the game's files
// define in one version of the game, each by database, as ListDocKeys and
// ListDefinedKeys give them.
type VersionKeys struct {
	Docs    map[string][]string
	Defined map[string][]string
}

// ResolveDigestFile returns the file of a folder of patches that holds the
// digest's patches.
func ResolveDigestFile(dir string) string {
	return filepath.Join(dir, digestFile)
}

// ResolveShaFile returns the file of a folder of patches that holds the id of
// the commit of the digest the patches were read from.
func ResolveShaFile(dir string) string {
	return filepath.Join(dir, shaFile)
}

// ResolveCheckedFile returns the file of a folder of patches that holds the
// time GitHub was last asked for the newest commit of the digest.
func ResolveCheckedFile(dir string) string {
	return filepath.Join(dir, checkedFile)
}

// ResolveDumpsFile returns the file of a folder of patches that holds the dumps
// of one version. The version "*" gives the pattern of every such file.
func ResolveDumpsFile(dir, version string) string {
	return filepath.Join(dir, fmt.Sprintf(dumpsFile, version))
}

// ResolveLocalFile returns the file of a folder of patches that holds the
// patches PMT compared itself when the game updated.
func ResolveLocalFile(dir string) string {
	return filepath.Join(dir, localFile)
}

// ResolveOwnFile returns the file of a folder of patches that holds the
// install's own dumps of one version, from the last time they were not older
// than the install, so the next update has the old side of its comparison. The
// version "*" gives the pattern of every such file.
func ResolveOwnFile(dir, version string) string {
	return filepath.Join(dir, fmt.Sprintf(ownFile, version))
}

// ResolveKeysFile returns the file of a folder of patches that holds the keys
// the docs document and the names the game's files define in one version, so
// the next update has the old side of its comparison. The version "*" gives the
// pattern of every such file.
func ResolveKeysFile(dir, version string) string {
	return filepath.Join(dir, fmt.Sprintf(keysFile, version))
}

// listKeptVersions returns the versions a folder of patches keeps the
// install's own dumps or keys of, once each.
func listKeptVersions(dir string) []string {
	var versions []string

	for _, pattern := range []string{ownFile, keysFile} {
		prefix, suffix, _ := strings.Cut(pattern, "%s")
		kept, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf(pattern, "*")))

		for _, file := range kept {
			version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), prefix), suffix)
			if !slices.Contains(versions, version) {
				versions = append(versions, version)
			}
		}
	}

	return versions
}

// histories holds each game's history of patches as the gen command wrote it:
// what each version changed, compared by PMT.
//
//go:embed shipped/*.gob
var histories embed.FS

// LoadHistory returns the patches the app ships for a game, oldest version
// first. It returns none for a game the app ships none for, and an error if
// the history cannot be decoded.
func LoadHistory(gameID string) ([]Patch, error) {
	content, err := histories.ReadFile("shipped/" + gameID + ".gob")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var history []Patch
	if err := gob.NewDecoder(bytes.NewReader(content)).Decode(&history); err != nil {
		return nil, fmt.Errorf("cannot read the shipped patches of %s: %w", gameID, err)
	}

	return history, nil
}

// LoadPatches reads the digest's patches and the local ones from the game's
// folder of patches again, takes the patches the app ships, and merges them
// into the session's patches. A file that is not there is no patches. It
// returns an error for a file that is there and cannot be read, and the
// patches of the other sources stand.
func (s *Session) LoadPatches() error {
	digest, digestErr := loadPatchFile(ResolveDigestFile(s.patchDir))
	history, historyErr := LoadHistory(s.Game.ID)
	local, localErr := loadPatchFile(ResolveLocalFile(s.patchDir))
	merged := MergePatches(digest, history, local)
	s.Patches.Store(&merged)

	return errors.Join(digestErr, historyErr, localErr)
}

// loadPatchFile returns the patches a file holds, none for a file that is not
// there.
func loadPatchFile(file string) ([]Patch, error) {
	patches, ok := files.LoadGob[[]Patch](file)
	if !ok && files.IsFile(file) {
		return nil, fmt.Errorf("cannot read the patches in %s", file)
	}

	return patches, nil
}

// ResolveRange returns the versions a mod's patches run from and to. The end
// is the install's version. The start is the oldest version the mod's
// supported version matches, as 1.15.0 for 1.15.*, among the patches'
// versions and the versions they are compared against. A supported version that
// is blank, or that cannot be judged, as 1.13* cannot, gives the mod's base
// version. One that matches only open betas, which count only where the
// install is on one, gives the latest release before them. The start is "" if
// there is none of these, and for a mod that is not in the workspace. A
// version past the end is never matched.
func (s *Session) ResolveRange(modID string) (start, end string) {
	end = s.Schema.Version
	mod, ok := findMod(*s.Order.Load(), modID)
	if !ok {
		return "", end
	}

	return s.Patches.Load().resolveStart(mod, end), end
}

// resolveStart returns the version a mod's range starts at when it ends at the
// given version. ResolveRange says how it is chosen.
func (p Patches) resolveStart(mod Mod, end string) string {
	supported := mod.Descriptor.SupportedVersion
	if _, judged := MatchVersion(supported, end); !judged {
		return mod.Base
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
				return version
			}

			beta = cmp.Or(beta, version)
		}
	}

	return p.ResolveBase(beta)
}

// IsBeta reports whether a version is an open beta. That is a version that
// game.IsBeta says is one, and a number the patches hold only as an open
// beta, since the digests never list one number as a release and as a beta.
func (p Patches) IsBeta(version string) bool {
	if game.IsBeta(version) {
		return true
	}

	return !p.holds(version) && slices.ContainsFunc(p, func(patch Patch) bool {
		return game.IsBeta(patch.Version) && strings.HasPrefix(patch.Version, version+"-")
	})
}

// ResolveBase returns the version the patch of a version is compared
// against. It is the latest earlier version that is not an open beta,
// whether the version is a release or a beta, so a release's changes are
// the sum of the betas' before it. It returns "" if there is none.
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
// an end version. The start's own patch is left out, since a mod made for
// that version already holds its changes. Open betas are left out unless the
// end is one, which is where an install on a beta is. It returns none if the
// start or the end is "".
func (p Patches) ListRange(start, end string) []Patch {
	if start == "" || end == "" {
		return nil
	}

	skipBetas := !p.IsBeta(end)
	var patches []Patch
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

// holds reports whether there is a patch of exactly this version.
func (p Patches) holds(version string) bool {
	return slices.ContainsFunc(p, func(patch Patch) bool { return patch.Version == version })
}
