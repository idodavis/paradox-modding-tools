// folder.go keeps a game's patches in one folder of the app's cache: the
// digest's patches with the commit they were read from, the digest's dumps of
// each version an install is on, and the patches PMT compared itself. It also
// loads the history of patches the app ships.

package patch

import (
	"bytes"
	"embed"
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// The files of a folder of patches. The digest's file holds a []Patch, and
// the sha file the id of the commit they were read from. Its time is when
// GitHub was last asked for the newest commit. The local file holds the
// patches PMT compared itself.
const (
	digestFile = "digest.gob"
	shaFile    = "sha.txt"
	localFile  = "local.gob"
	dumpsFile  = "dumps-%s.gob"
)

// ResolveDir returns the folder of the app's cache that holds one game's
// patches. It is not in the game's folder of schema caches, which a workspace
// open empties, so the patches outlive an update of the game.
func ResolveDir(cacheDir string, row *game.Game) string {
	return filepath.Join(cacheDir, "patches", row.ID)
}

// ResolveDumpsFile returns the file of a folder of patches that holds a
// digest's dumps of one version, as a schema.Dumps with no file or line on
// its commands. The version "*" gives the pattern of every such file.
func ResolveDumpsFile(dir, version string) string {
	return filepath.Join(dir, fmt.Sprintf(dumpsFile, version))
}

// histories holds each game's history of patches as the gen command wrote it:
// what each version changed, compared by PMT.
//
//go:embed shipped/*.gob
var histories embed.FS

// LoadHistory returns the patches the app ships for a game. It returns none
// for a game the app ships none for, and an error if the history cannot be
// decoded.
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

// Load returns a game's patches: the digest's and PMT's own from the game's
// folder of patches, merged with the ones the app ships.
func Load(dir, gameID string) Patches {
	digest, _ := files.LoadGob[[]Patch](filepath.Join(dir, digestFile))
	history, _ := LoadHistory(gameID)
	local, _ := files.LoadGob[[]Patch](filepath.Join(dir, localFile))

	return Merge(digest, history, local)
}

// FindAsked returns when GitHub was last asked for the newest commit of the
// game's digest. It is the zero time for a folder whose digest is missing or
// cannot be read, as one saved by an older build, so that it is fetched again
// at once.
func FindAsked(dir string) time.Time {
	stat, err := os.Stat(filepath.Join(dir, shaFile))
	if _, ok := files.LoadGob[[]Patch](filepath.Join(dir, digestFile)); err != nil || !ok {
		return time.Time{}
	}

	return stat.ModTime()
}

// MarkAsked records that GitHub was asked now and the folder's digest is
// still the newest.
func MarkAsked(dir string) error {
	now := time.Now()

	return os.Chtimes(filepath.Join(dir, shaFile), now, now)
}

// IsCurrent reports whether the folder's digest was read from the given
// commit and the folder holds the dumps of each version an install is on
// that the digest has, so that nothing is to be downloaded.
func IsCurrent(dir, head string, installs []string) bool {
	sha, err := os.ReadFile(filepath.Join(dir, shaFile))
	digest, ok := files.LoadGob[[]Patch](filepath.Join(dir, digestFile))
	if err != nil || string(sha) != head || !ok {
		return false
	}

	for _, patch := range digest {
		for _, install := range installs {
			if game.IsInstalled(patch.Version, install) && !files.IsFile(ResolveDumpsFile(dir, install)) {
				return false
			}
		}
	}

	return true
}

// SaveDigest saves a digest's patches, its dumps of the versions installs are
// on, by the install's version, and last the commit they were read from, so
// that a failed save is read again by the next refresh. It deletes the dumps
// of any other version. A version's dumps are written only if the folder has
// none of it yet, or its file cannot be read: a published version's dumps
// never change, and the file's time names the install's schema cache, so a
// file written again would build the schema again.
func SaveDigest(dir, head string, patches Patches, dumps map[string]*schema.Dumps) error {
	dumpFiles := map[string]bool{}
	for install, installDumps := range dumps {
		file := ResolveDumpsFile(dir, install)
		dumpFiles[file] = true
		if _, ok := files.LoadGob[*schema.Dumps](file); ok {
			continue
		}

		if err := files.SaveGob(file, installDumps); err != nil {
			return err
		}
	}

	// A file that cannot be deleted now goes at a later refresh.
	stale, _ := filepath.Glob(ResolveDumpsFile(dir, "*"))
	for _, file := range stale {
		if !dumpFiles[file] {
			_ = os.Remove(file)
		}
	}

	if err := files.SaveGob(filepath.Join(dir, digestFile), []Patch(patches)); err != nil {
		return err
	}

	return files.WriteAtomic(filepath.Join(dir, shaFile), []byte(head))
}
