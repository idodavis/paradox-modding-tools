// local.go is the folders, the write every other file here goes through, and the stored form.

// Package local is this machine's files: the app's own folders, the write that keeps a reader from
// seeing half a file, the form a cache or shipped file is stored in, and a mod's own files
// (descriptor, thumbnail, the skeleton of a new mod). Nothing here understands the script language.
package local

import (
	"compress/gzip"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AppDir is the app's own folder under the user's config directory, made if missing.
func AppDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Paradox Modding Tools")
	return dir, os.MkdirAll(dir, 0o755)
}

// CacheDir is where the app keeps what it learned about games and their wikis, made if missing.
func CacheDir() (string, error) {
	app, err := AppDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(app, "cache")
	return dir, os.MkdirAll(dir, 0o755)
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
