// snapshot.go keeps what PMT needs of each version of a game to compare the
// next version with, and makes the patch of a version from two of them.

package patch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/schema"
)

// Snapshot is what PMT keeps of one version of a game, for the next version
// to be compared with.
type Snapshot struct {
	// Dumps are the install's own dumps of the version. They are nil where
	// the dumps were older than the install, so of a version not known.
	Dumps *schema.Dumps

	// Defined are the names the game's files define, by database, and Hashes
	// the hash of each game file the index scan read, by its path from the
	// game's folder.
	Defined map[string][]string
	Hashes  map[string]string
}

// The folder of the snapshots in a folder of patches, and what a snapshot's
// name ends in after its version.
const (
	snapshotsDir = "snapshots"
	snapshotExt  = ".gob"
)

// TakeSnapshot returns the snapshot of the version an install's index was
// scanned at, with the install's own dumps of it, nil for none. A file the
// scan did not read has no hash, such as a picture.
func TakeSnapshot(dumps *schema.Dumps, install *index.Index) Snapshot {
	hashes := map[string]string{}
	for position, file := range install.Files {
		if file.Hash != "" && install.IsLoaded(int32(position)) {
			hashes[file.Rel] = file.Hash
		}
	}

	return Snapshot{Dumps: dumps, Defined: install.ListDefinedKeys(), Hashes: hashes}
}

// CompareSnapshots returns the patch of a version that PMT compared itself:
// what the version changed against the snapshot of its base. The dumps are
// compared where both snapshots have them. An old snapshot that kept no
// names, as gen has of a version it never saw installed, gives the dumps'
// changes alone.
func CompareSnapshots(base, version string, old, current Snapshot, language *lang.Schema) Patch {
	patch := Patch{
		Version: version, Base: base, Local: true,
		DumpChanges: schema.CompareDumps(old.Dumps, current.Dumps, language),
	}
	if old.Defined == nil {
		return patch
	}

	patch.DefinitionChanges = schema.CompareKeys(old.Defined, current.Defined)
	differs := func(earlier, latest string) bool { return earlier != latest }
	for rel, status := range schema.Compare(old.Hashes, current.Hashes, differs) {
		patch.GameFiles = append(patch.GameFiles, GameFile{Rel: rel, Status: status})
	}

	slices.SortFunc(patch.GameFiles, CompareFiles)

	return patch
}

// KeepSnapshot keeps the snapshot of an install's version in the game's
// folder of patches. First it compares it with the snapshot of the newest
// older version, if one is kept, and saves the result as PMT's own patch of
// the install's version. A snapshot of a newer version is compared with
// nothing, since another install of the game may be on that version.
//
// The older snapshots stay until a snapshot with the install's own dumps is
// kept, so that dumps the user makes later with script_docs can still be
// compared. Then the snapshots of versions that no install is on, which the
// given versions are, are deleted.
func KeepSnapshot(dir, version string, current Snapshot, installs []string, language *lang.Schema) error {
	folder := filepath.Join(dir, snapshotsDir)
	others := slices.DeleteFunc(listVersions(folder, snapshotExt), func(other string) bool { return other == version })
	if base, ok := findLatest(others, version); ok {
		// A snapshot or a file of patches that cannot be read is replaced.
		old, _ := files.LoadGob[Snapshot](filepath.Join(folder, base+snapshotExt))
		local, _ := files.LoadGob[[]Patch](filepath.Join(dir, localFile))
		local = Put(local, CompareSnapshots(base, version, old, current, language))
		if err := files.SaveGob(filepath.Join(dir, localFile), local); err != nil {
			return err
		}
	}

	if err := files.SaveGob(filepath.Join(folder, version+snapshotExt), current); err != nil || current.Dumps == nil {
		return err
	}

	// A file that cannot be deleted now goes at a later open.
	for _, other := range others {
		if !slices.Contains(installs, other) {
			_ = os.Remove(filepath.Join(folder, other+snapshotExt))
		}
	}

	return nil
}

// listVersions returns the game versions a folder keeps something of. Each of
// the folder's entries is named for its version, before the given ending. A
// folder that is missing keeps none.
func listVersions(dir, ending string) []string {
	entries, _ := os.ReadDir(dir)
	versions := make([]string, len(entries))
	for position, entry := range entries {
		versions[position] = strings.TrimSuffix(entry.Name(), ending)
	}

	return versions
}

// findLatest returns the latest of the versions that is not after a limit. It
// returns false if the limit is "" or no version is at or before it.
func findLatest(versions []string, limit string) (string, bool) {
	older := slices.DeleteFunc(slices.Clone(versions), func(version string) bool {
		return game.CompareVersions(version, limit) > 0
	})
	if limit == "" || len(older) == 0 {
		return "", false
	}

	return slices.MaxFunc(older, game.CompareVersions), true
}
