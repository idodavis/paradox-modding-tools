// folder.go keeps a game's patches in one folder of the app's cache, and
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

// The files of a folder of patches: the digest's patches, the commit they
// were read from, PMT's own patches, and a digest's dumps of one version.
// The commit file's time is when GitHub was last asked for the newest commit.
const (
	digestFile = "digest.gob"
	shaFile    = "sha.txt"
	localFile  = "local.gob"
	dumpsFile  = "dumps-%s.gob"
)

// ResolveDir returns the folder that holds one game's patches. It is outside
// the game's schema caches, which a workspace open empties.
func ResolveDir(cacheDir string, row *game.Game) string {
	return filepath.Join(cacheDir, "patches", row.ID)
}

// ResolveDumpsFile returns the file that holds a digest's dumps of one
// version. The version "*" gives the pattern of every such file.
func ResolveDumpsFile(dir, version string) string {
	return filepath.Join(dir, fmt.Sprintf(dumpsFile, version))
}

// histories holds each game's shipped history of patches, as gen wrote it.
//
//go:embed shipped/*.gob
var histories embed.FS

// LoadHistory returns the patches the app ships for a game, none for a game
// it ships none for. It returns an error if the history cannot be decoded.
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

// Load returns a game's patches: the digest's, the shipped ones and PMT's
// own, merged.
func Load(dir, gameID string) Patches {
	digest, _ := files.LoadGob[[]Patch](filepath.Join(dir, digestFile))
	history, _ := LoadHistory(gameID)
	local, _ := files.LoadGob[[]Patch](filepath.Join(dir, localFile))

	return Merge(digest, history, local)
}

// FindAsked returns when GitHub was last asked for the digest's newest
// commit. It is the zero time if the digest is missing or cannot be read, as
// one an older build saved, so that it is fetched again at once.
func FindAsked(dir string) time.Time {
	stat, err := os.Stat(filepath.Join(dir, shaFile))
	if _, ok := files.LoadGob[[]Patch](filepath.Join(dir, digestFile)); err != nil || !ok {
		return time.Time{}
	}

	return stat.ModTime()
}

// MarkAsked records that GitHub was asked now and the digest is still the
// newest.
func MarkAsked(dir string) error {
	now := time.Now()

	return os.Chtimes(filepath.Join(dir, shaFile), now, now)
}

// IsCurrent reports whether nothing is to be downloaded: the digest was read
// from the given commit, and the folder holds the dumps of each installed
// version the digest has.
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

// SaveDigest saves a digest's patches, its dumps of the installed versions,
// and last the commit they were read from, so that a failed save is read
// again. It deletes the dumps of any other version.
//
// A version's dumps are written only if the folder has none it can read. The
// file's time names the install's schema cache, so writing it again would
// build the schema again.
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
