// cache.go is an install's scan kept between runs: vanilla-<install>-<version>.gob holds when
// it was scanned and then the install, its index and schema. The engine under lang/ says what a
// scan is; this file only says where it lives and how it is written and read back.

package local

import (
	"cmp"
	"compress/gzip"
	"encoding/gob"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
)

var unsafeInCacheName = regexp.MustCompile(`[^A-Za-z0-9]+`)

// cachePath is an install's cache without its extension; "latest" stands in for no version.
func cachePath(installID, version string) (string, error) {
	dir, err := CacheDir()
	name := "vanilla-" + unsafeInCacheName.ReplaceAllString(installID, "_") + "-" + unsafeInCacheName.ReplaceAllString(cmp.Or(version, "latest"), "_")
	return filepath.Join(dir, name), err
}

// SaveInstall writes a scanned install and stamps it with the time now. The scan time goes
// first, so ScannedAt reads it without decoding the index after it.
func SaveInstall(installID, version string, install *index.Install) error {
	base, err := cachePath(installID, version)
	if err != nil {
		return err
	}
	return Write(base+".gob", func(out io.Writer) error {
		encoder := gob.NewEncoder(out)
		if err := encoder.Encode(time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		return encoder.Encode(install)
	})
}

// LoadInstall reads an install's cache back, its lookups rebuilt before it is shared. A cache with
// no index or schema is an old or broken one, and fails so the session scans again.
func LoadInstall(installID, version string) (*index.Install, error) {
	base, err := cachePath(installID, version)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(base + ".gob")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := gob.NewDecoder(file)
	var scannedAt string
	if err := decoder.Decode(&scannedAt); err != nil {
		return nil, err
	}
	var install index.Install
	if err := decoder.Decode(&install); err != nil {
		return nil, err
	}
	if install.Index == nil || install.Schema == nil {
		return nil, errors.New("the cache holds no index or schema")
	}
	install.Restore()
	return &install, nil
}

// ScannedAt is when an install's cache was written, "" where there is none.
func ScannedAt(installID, version string) string {
	base, err := cachePath(installID, version)
	if err != nil {
		return ""
	}
	file, err := os.Open(base + ".gob")
	if err != nil {
		return ""
	}
	defer file.Close()
	var scannedAt string
	if gob.NewDecoder(file).Decode(&scannedAt) != nil {
		return ""
	}
	return scannedAt
}

// SaveShipped writes a game's layers as shipped-<game>.gob.gz in the cache folder, for the owner
// to copy into the schema's shipped folder when they choose.
func SaveShipped(gameID string, shipped schema.Shipped) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	return Write(filepath.Join(dir, "shipped-"+gameID+".gob.gz"), func(out io.Writer) error {
		zipped := gzip.NewWriter(out)
		if err := gob.NewEncoder(zipped).Encode(shipped); err != nil {
			return err
		}
		return zipped.Close()
	})
}

// DropInstall removes every cache file of an install, whatever its version.
func DropInstall(installID string) error {
	base, err := cachePath(installID, "x")
	if err != nil {
		return err
	}
	dir, prefix := filepath.Dir(base), strings.TrimSuffix(filepath.Base(base), "x")
	entries, err := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			err = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return err
}
