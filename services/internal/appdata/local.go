// local.go is the folders, the write every other file here goes through, and the stored form.

// Package appdata is this machine's files: the app's own folders, the write that keeps a reader from
// seeing half a file, the form a cache or shipped file is stored in, and a mod's own files
// (descriptor, thumbnail, the skeleton of a new mod). Nothing here understands the script language.
package appdata

import (
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// PathKey is the key a file on this machine is known by: its path cleaned, and on Windows, where the file system
// ignores case, lower-cased; elsewhere two paths that differ only by case are two files.
func PathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// Dir is the app's own folder under the user's config directory, made if missing.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Paradox Modding Tools")
	return dir, os.MkdirAll(dir, 0o755)
}

// CacheDir is where the app keeps what it learned about games and their wikis, made if missing.
func CacheDir() (string, error) {
	app, err := Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(app, "cache")
	return dir, os.MkdirAll(dir, 0o755)
}

// writing is the temporary files a Write of this process is filling; tempPattern matches the names CreateTemp gives
// them.
var (
	writing     sync.Map
	tempPattern = regexp.MustCompile(`^\..+\.pmt\d+$`)
)

// Leftover is whether path is a Write's temporary file that no Write of this process is filling: one a run that
// ended mid-write left behind, which nothing will finish or remove.
func Leftover(path string) bool {
	_, held := writing.Load(path)
	return tempPattern.MatchString(filepath.Base(path)) && !held
}

// Write builds path's new contents through fill in a temporary file beside it, then puts that file
// in its place. A reader never sees half a file; a failure leaves either the current file or, once
// that is removed, the new contents in the temporary file.
func Write(path string, fill func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// same directory, so the rename stays on one filesystem and stays atomic
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".pmt*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	writing.Store(tempName, true)
	defer writing.Delete(tempName)
	defer func() { _ = os.Remove(tempName) }() // a no-op once the rename has succeeded

	if err := fill(temp); err != nil {
		temp.Close()
		return err
	}
	// sync before the rename: a rename can be durable while the bytes are not, which leaves a
	// file that exists and is empty
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o644); err != nil {
		return err
	}

	// rename first, remove only if that fails: a Windows replace-rename is refused while anything
	// holds the target open (the game, another editor, an antivirus), while unlinking succeeds
	if err := os.Rename(tempName, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		tempName = "" // the old file is gone: the new contents stay in the temp file
		return fmt.Errorf("%w; the new contents are kept in %s", err, temp.Name())
	}
	return nil
}

// WriteBytes is Write for contents already in memory.
func WriteBytes(path string, data []byte) error {
	return Write(path, func(out io.Writer) error {
		_, err := out.Write(data)
		return err
	})
}

// Encode writes value to out as gob inside gzip, the form of the install cache and the shipped
// schemas.
func Encode(out io.Writer, value any) error {
	zip := gzip.NewWriter(out)
	if err := gob.NewEncoder(zip).Encode(value); err != nil {
		return err
	}
	return zip.Close()
}

// Decode reads what Encode wrote into value, a pointer.
func Decode(in io.Reader, value any) error {
	unzip, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer unzip.Close()
	return gob.NewDecoder(unzip).Decode(value)
}

// CopyFile copies src over dest, which is written whole or not at all.
func CopyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return Write(dest, func(out io.Writer) error {
		_, err := io.Copy(out, in)
		return err
	})
}

// unsafeName is what SafeName replaces.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SafeName is a name fit for a file name, each character outside [A-Za-z0-9._-] turned into "_":
// "build 24187685" is build_24187685. The shipped schemas and the install cache are named by it.
func SafeName(name string) string {
	return unsafeName.ReplaceAllString(name, "_")
}
