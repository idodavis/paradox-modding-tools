// persist.go saves and loads the Index and its data tier as JSON. The Index is
// the file: the posting lists are written with the records and nothing is
// rebuilt on load beyond the path lookup. SaveJSON/LoadJSON are the one
// encode/decode pair in the package; sonic is measurably faster than the stdlib
// on a cache this size, and CopyString frees the read buffer instead of pinning
// it behind decoded strings.

package index

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

func vanillaJSON(kind string, parts ...string) (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	var name strings.Builder
	name.WriteString(kind)
	for _, p := range parts {
		name.WriteByte('-')
		name.WriteString(sanitize(p))
	}
	return filepath.Join(dir, name.String()+".json"), nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
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

func CacheVersion(v string) string {
	if v == "" {
		return "latest"
	}
	return v
}

// SaveCache writes the index and its data tier keyed by installId + version, and drops leftover cache-*.json.
func SaveCache(x, data *Index) error {
	path, err := vanillaJSON("vanilla", x.InstallID, x.GameVersion)
	if err != nil {
		return err
	}
	if err := SaveJSON(path, x); err != nil {
		return err
	}
	// Only a scan carries a tier; a loaded index has none and must not truncate the sidecar.
	if data != nil {
		dpath, err := vanillaJSON("vanilla-data", x.InstallID, x.GameVersion)
		if err != nil {
			return err
		}
		if err := SaveJSON(dpath, data); err != nil {
			return err
		}
	}
	// Legacy per-workspace caches, from before the vanilla-* naming.
	legacy, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "cache-*.json"))
	for _, p := range legacy {
		_ = os.Remove(p)
	}
	return nil
}

// LoadCache reads the Index for installID + version.
func LoadCache(installID, version string) (*Index, error) {
	path, err := vanillaJSON("vanilla", installID, version)
	if err != nil {
		return nil, err
	}
	return loadIndex(path)
}

// LoadData reads the data-row tier for installID + version.
func LoadData(installID, version string) (*Index, error) {
	path, err := vanillaJSON("vanilla-data", installID, version)
	if err != nil {
		return nil, err
	}
	return loadIndex(path)
}

// loadIndex decodes one index file and wakes the lookups it does not store.
func loadIndex(path string) (*Index, error) {
	x, err := LoadJSON[Index](path, CacheFormatVersion)
	if err != nil {
		return nil, err
	}
	x.wake()
	return x, nil
}

// PeekCacheScannedAt returns scannedAt from the vanilla JSON without loading defs, and "" when the file is one LoadCache would refuse -- without the version check the install list went on showing the previous scan date for a model the engine had already discarded.
func PeekCacheScannedAt(installID, version string) string {
	path, err := vanillaJSON("vanilla", installID, version)
	if err != nil {
		return ""
	}
	got, scannedAt, err := peekHeader(path)
	if err != nil || got != CacheFormatVersion {
		return ""
	}
	return scannedAt
}

// DropVanillaFiles removes every vanilla-* file for installID -- this is the "forget this install" path.
func DropVanillaFiles(installID string) error {
	sid := sanitize(installID)
	return dropCacheFiles("vanilla-"+sid, "vanilla-data-"+sid, "vanilla-loc-"+sid)
}

func dropCacheFiles(prefixes ...string) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		for _, p := range prefixes {
			if strings.HasPrefix(e.Name(), p) {
				_ = os.RemoveAll(filepath.Join(dir, e.Name()))
				break
			}
		}
	}
	return nil
}
