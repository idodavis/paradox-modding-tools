// Package local is this machine's files: the app's own folders, the one way a file is written
// so no reader ever sees half of it, the install cache, and a mod's own files (descriptor,
// thumbnail, the skeleton of a new mod). Nothing here understands the script language; the
// engine under lang/ does not import it.
//
// local.go is the folders and the write every other file here goes through.
package local

import (
	"errors"
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

// Write builds path's new contents through fill in a temporary file beside it, then puts that
// file in its place. A reader never sees half a file, and a failure leaves the current file
// untouched and no leftovers.
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

	// Rename first; remove only if it fails. A Windows replace-rename returns
	// ERROR_ACCESS_DENIED while anything holds the destination open (the game, another editor,
	// an antivirus scanning the cache), while unlinking the name succeeds.
	if err := os.Rename(tempName, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tempName, path)
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
