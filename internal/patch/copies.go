// copies.go keeps copies of the game's files as they were at each game
// version, for the game's later changes to a file to be merged from.

package patch

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"paradox-modding-tools/internal/files"
)

// The folder of the copies in a folder of patches, and what a copy's name
// ends in. A copy is gzipped, in a folder named for the game version it was
// taken at, under the game file's own path.
const (
	copiesDir = "copies"
	copyExt   = ".gz"
)

// KeepCopy keeps a copy of a text file of the game as it is at a game
// version, unless the folder has one. A picture or other binary file is not
// kept, since a merge reads text.
func KeepCopy(dir, version string, file files.File) error {
	target := resolveCopy(dir, version, file.Rel)
	if !file.Format.IsText() || files.IsFile(target) {
		return nil
	}

	content, err := os.ReadFile(file.Path)
	if err != nil {
		return err
	}

	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, err = writer.Write(content)
	if err = errors.Join(err, writer.Close()); err != nil {
		return err
	}

	return files.WriteAtomic(target, packed.Bytes())
}

// FindCopy returns the kept copy of a game file that the game's changes can
// be merged from into a mod's file: the copy's version and its file. The mod
// is given by the version its range starts at, and the game file by its path
// from the game's folder. It is the copy of the latest kept version at or
// before the start, if every patch after the copy's version, up to the
// start, lists the files it changed and none lists this one. It returns false
// if there is no such copy, which is so of a file no mod replaced when PMT
// saw that version, and of a picture or other binary file.
func FindCopy(dir string, patches Patches, start, rel string) (version, path string, ok bool) {
	version, ok = findLatest(listVersions(filepath.Join(dir, copiesDir), ""), start)
	path = resolveCopy(dir, version, rel)
	if !ok || !files.IsFile(path) {
		return "", "", false
	}

	for _, patch := range patches.ListRange(version, start) {
		_, isListed := slices.BinarySearchFunc(patch.GameFiles, GameFile{Rel: rel}, CompareFiles)
		if len(patch.GameFiles) == 0 || isListed {
			return "", "", false
		}
	}

	return version, path, true
}

// ReadCopy returns the content of the copy in a file FindCopy gave.
func ReadCopy(path string) ([]byte, error) {
	packed, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = packed.Close() }()

	unpacked, err := gzip.NewReader(packed)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(unpacked)
}

// DeleteCopies deletes the copies of each version that no install of the game
// is on and no mod's range needs. The mods are given by the version each
// one's range starts at. A mod needs the latest kept version at or before its
// start, which is where FindCopy looks. A folder that cannot be deleted goes
// at a later open.
func DeleteCopies(dir string, installs, starts []string) {
	versions := listVersions(filepath.Join(dir, copiesDir), "")
	needed := slices.Clone(installs)
	for _, start := range starts {
		if version, ok := findLatest(versions, start); ok {
			needed = append(needed, version)
		}
	}

	for _, version := range versions {
		if !slices.Contains(needed, version) {
			_ = os.RemoveAll(filepath.Join(dir, copiesDir, version))
		}
	}
}

// resolveCopy returns the file that holds the copy of a game file kept at a
// game version.
func resolveCopy(dir, version, rel string) string {
	return filepath.Join(dir, copiesDir, version, filepath.FromSlash(rel)+copyExt)
}
