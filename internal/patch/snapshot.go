// snapshot.go keeps what PMT needs of each game version to compare the next
// one with.

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

// Snapshot is what PMT keeps of one game version to compare the next with.
type Snapshot struct {
	// Dumps are the install's own dumps, nil where they were older than the
	// install.
	Dumps *schema.Dumps

	// Defined are the names the game's files define, by database. Texts run
	// beside them: the hash of each name's definitions, 0 where it has none.
	// Texts is nil in a snapshot that kept no hashes.
	Defined map[string][]string
	Texts   map[string][]uint32

	// Hashes are the game files' hashes, by path from the game's folder.
	Hashes map[string]string
}

// The snapshots' folder and file ending.
const (
	snapshotsDir = "snapshots"
	snapshotExt  = ".gob"
)

// TakeSnapshot returns the snapshot of an install as its index was scanned.
// A file the scan did not read, such as a picture, has no hash.
func TakeSnapshot(dumps *schema.Dumps, install *index.Index) Snapshot {
	hashes := map[string]string{}
	for position, file := range install.Files {
		if file.Hash != "" && install.IsLoaded(int32(position)) {
			hashes[file.Rel] = file.Hash
		}
	}

	defined, texts := install.ListDefinedKeys()

	return Snapshot{Dumps: dumps, Defined: defined, Texts: texts, Hashes: hashes}
}

// CompareSnapshots returns PMT's own patch of a version: what changed against
// the snapshot of its base. An old snapshot with no names, as gen makes of a
// version it never saw installed, gives the dumps' changes alone.
func CompareSnapshots(base, version string, old, current Snapshot, language *lang.Schema) Patch {
	patch := Patch{
		Version: version, Base: base, Local: true,
		DumpChanges: schema.CompareDumps(old.Dumps, current.Dumps, language),
	}
	if old.Defined == nil {
		return patch
	}

	patch.DefinitionChanges = schema.CompareKeys(old.Defined, current.Defined, old.Texts, current.Texts)
	differs := func(earlier, latest string) bool { return earlier != latest }
	for rel, status := range schema.Compare(old.Hashes, current.Hashes, differs) {
		patch.GameFiles = append(patch.GameFiles, GameFile{Rel: rel, Status: status})
	}

	slices.SortFunc(patch.GameFiles, CompareFiles)

	return patch
}

// KeepSnapshot saves the snapshot of an install's version. If an older
// snapshot is kept, it first saves the comparison of the two as PMT's own
// patch.
//
// Older snapshots stay until one with the install's own dumps is kept, so
// that dumps made later with script_docs can still be compared. Then the
// snapshots of versions no install is on are deleted.
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

// listVersions returns the versions a folder holds. Each entry is named for
// its version, before the given ending.
func listVersions(dir, ending string) []string {
	entries, _ := os.ReadDir(dir)
	versions := make([]string, len(entries))
	for position, entry := range entries {
		versions[position] = strings.TrimSuffix(entry.Name(), ending)
	}

	return versions
}

// findLatest returns the latest version at or before a limit. It returns
// false if there is none or the limit is "".
func findLatest(versions []string, limit string) (string, bool) {
	older := slices.DeleteFunc(slices.Clone(versions), func(version string) bool {
		return game.CompareVersions(version, limit) > 0
	})
	if limit == "" || len(older) == 0 {
		return "", false
	}

	return slices.MaxFunc(older, game.CompareVersions), true
}
