// persist.go is the one JSON encode and decode pair every cache and sidecar file goes through: a save that survives a reader holding the file, and a load that refuses a wrong formatVersion from the first 64 KB without decoding the body. sonic is measurably faster than the stdlib on a cache this size, and CopyString frees the read buffer instead of pinning it behind decoded strings.

package persist

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
)

var codec = sonic.Config{CopyString: true}.Froze()

var ErrFormat = errors.New("format version")

// CacheDir is the user-data directory for cached models.
func CacheDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Paradox Modding Tools", "cache")
	return dir, os.MkdirAll(dir, 0o755)
}

// SaveJSON writes v to path via a .tmp file. Rename first: on POSIX that replaces atomically, and Go's Windows Rename does pass MOVEFILE_REPLACE_EXISTING. Remove only when it fails -- replace-existing returns ERROR_ACCESS_DENIED whenever anything holds the destination open, and a large cache being read by the app or walked by an antivirus is that case.
func SaveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, path)
}

// peekWindow bounds the header read. formatVersion and scannedAt are the first and sixth fields every persisted struct declares, so a large cache is never read whole to answer either.
const peekWindow = 64 << 10

// peekHeader reads formatVersion and scannedAt out of the head of a JSON file, in one open, without decoding the body.
func peekHeader(path string) (formatVersion int, scannedAt string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	head := make([]byte, peekWindow)
	// A file shorter than the window is the normal case for a wiki sidecar.
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, "", err
	}
	head = head[:n]
	node, gerr := sonic.Get(head, "formatVersion")
	got, nerr := node.Int64()
	if gerr != nil || nerr != nil {
		return 0, "", fmt.Errorf("missing formatVersion: %w", ErrFormat)
	}
	if at, aerr := sonic.Get(head, "scannedAt"); aerr == nil {
		scannedAt, _ = at.String()
	}
	return int(got), scannedAt, nil
}

// LoadJSON reads path and rejects a missing or mismatched formatVersion, without decoding the body of a file it is going to refuse.
func LoadJSON[T any](path string, wantVersion int) (*T, error) {
	got, _, err := peekHeader(path)
	if err != nil {
		return nil, err
	}
	if got != wantVersion {
		return nil, fmt.Errorf("format %d != %d: %w", got, wantVersion, ErrFormat)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var dst T
	if err := codec.Unmarshal(raw, &dst); err != nil {
		return nil, err
	}
	return &dst, nil
}
