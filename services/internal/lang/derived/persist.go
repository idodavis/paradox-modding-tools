// persist.go saves and loads VanillaCache, the data tier and the loc sidecars
// as JSON. SaveJSON/LoadJSON are the one encode/decode pair in the package;
// sonic is measurably faster than the stdlib on the real 88 MB cache, and
// CopyString frees the read buffer instead of pinning it behind decoded strings.

package derived

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
)

var codec = sonic.Config{CopyString: true}.Froze()

var ErrFormat = errors.New("format version")

// CacheDir is the user-data directory for cached vanilla models.
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
		name.WriteString("-" + sanitize(p))
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

// SaveJSON writes v to path via a .tmp file. Rename first: on POSIX that replaces atomically, and Go's Windows Rename does pass MOVEFILE_REPLACE_EXISTING. Remove only when it fails -- replace-existing returns ERROR_ACCESS_DENIED whenever anything holds the destination open, and a 98 MB cache being read by the app or walked by an antivirus is that case, which left every rescan writing a .tmp the engine then refused.
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

// peekWindow bounds the header read. formatVersion and scannedAt are the first and sixth fields every persisted struct declares, so an 88 MB cache is never read whole to answer either.
const peekWindow = 64 << 10

// peekHeader reads formatVersion and scannedAt out of the head of a JSON file, in one open, without decoding the body.
func peekHeader(path string) (formatVersion int, scannedAt string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	head := make([]byte, peekWindow)
	// A file shorter than the window is the normal case for a loc or wiki sidecar.
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

// SaveCache writes c and its data tier keyed by installId + version, and drops leftover cache-*.json.
func SaveCache(c *VanillaCache) error {
	path, err := vanillaJSON("vanilla", c.InstallID, c.GameVersion)
	if err != nil {
		return err
	}
	if err := saveCacheFile(path, c); err != nil {
		return err
	}
	// Only a scan carries a tier; a loaded cache has none and must not truncate the sidecar.
	if c.data != nil {
		if err := SaveData(c.InstallID, c.GameVersion, c.data); err != nil {
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

// LoadCache reads the VanillaCache for installID + version.
func LoadCache(installID, version string) (*VanillaCache, error) {
	path, err := vanillaJSON("vanilla", installID, version)
	if err != nil {
		return nil, err
	}
	return loadCacheFile(path)
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

// saveCacheFile marshals c to an explicit path, interning repeated file paths.
func saveCacheFile(path string, c *VanillaCache) error {
	PrepareCache(c)
	return SaveJSON(path, internPaths(c))
}

// loadCacheFile reads a VanillaCache from an explicit path.
func loadCacheFile(path string) (*VanillaCache, error) {
	c, err := LoadJSON[VanillaCache](path, CacheFormatVersion)
	if err != nil {
		return nil, err
	}
	expandPaths(c)
	PrepareCache(c)
	return c, nil
}

// SaveVanillaLoc writes the loc sidecar for install + version + lang.
func SaveVanillaLoc(installID, version, lang string, vloc *VanillaLoc) error {
	if vloc == nil {
		return nil
	}
	path, err := vanillaJSON("vanilla-loc", installID, version, lang)
	if err != nil {
		return err
	}
	vloc.FormatVersion = LocFormatVersion
	return SaveJSON(path, vloc)
}

// LoadVanillaLoc reads the loc sidecar, or an error if absent / version mismatch.
func LoadVanillaLoc(installID, version, lang string) (*VanillaLoc, error) {
	path, err := vanillaJSON("vanilla-loc", installID, version, lang)
	if err != nil {
		return nil, err
	}
	vloc, err := LoadJSON[VanillaLoc](path, LocFormatVersion)
	if err != nil {
		return nil, err
	}
	if vloc.Sites == nil {
		vloc.Sites = map[string]LocEntry{}
	}
	return vloc, nil
}

// DropVanillaLocFiles removes an install's localization sidecars, leaving the model cache alone. A rescan harvests only english, so with a pinned or "latest" version key every other language keeps the previous patch's filename and LoadVanillaLoc accepts it.
func DropVanillaLocFiles(installID string) error {
	return dropCacheFiles("vanilla-loc-" + sanitize(installID))
}

// DropVanillaFiles removes every vanilla-* file for installID plus the archived script_docs copies -- this is the "forget this install" path, and a stale archive would outlive the install it describes.
func DropVanillaFiles(installID string) error {
	sid := sanitize(installID)
	return dropCacheFiles("vanilla-"+sid, "vanilla-data-"+sid,
		"vanilla-loc-"+sid, "script-docs-"+sid)
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

// internPaths returns a copy of c whose Def/Ref/Edge paths are decimal indices into the returned Paths table: a CK3 install produces millions of records and each one carried its own copy of a ~110-byte install path. The receiver is left untouched, since a live session may hold it while the scan writes.
func internPaths(c *VanillaCache) *VanillaCache {
	if c == nil {
		return nil
	}
	out := *c
	table := make([]string, 0, 4096)
	index := make(map[string]int, 4096)
	id := func(path string) string {
		if path == "" {
			return ""
		}
		i, ok := index[path]
		if !ok {
			i = len(table)
			index[path] = i
			table = append(table, path)
		}
		return strconv.Itoa(i)
	}

	out.Defs = make([]Def, len(c.Defs))
	for i, d := range c.Defs {
		d.Path = id(d.Path)
		out.Defs[i] = d
	}
	out.LocRefs = internRefs(c.LocRefs, id)
	out.CallRefs = internRefs(c.CallRefs, id)
	out.Edges = make([]Edge, len(c.Edges))
	for i, e := range c.Edges {
		e.Path = id(e.Path)
		out.Edges[i] = e
	}
	out.Paths = table
	return &out
}

func internRefs(refs []Ref, id func(string) string) []Ref {
	if refs == nil {
		return nil
	}
	out := make([]Ref, len(refs))
	for i, r := range refs {
		r.Path = id(r.Path)
		out[i] = r
	}
	return out
}

// expandPaths turns interned indices back into paths, in place; the cache has just been decoded and is not shared yet. A file written before interning has no table and is left alone.
func expandPaths(c *VanillaCache) {
	if c == nil || len(c.Paths) == 0 {
		return
	}
	table := c.Paths
	at := func(s string) string {
		if s == "" {
			return ""
		}
		i, err := strconv.Atoi(s)
		// Not an index: a pre-interning file, or a corrupt one. Keep the value rather than losing the location.
		if err != nil || i < 0 || i >= len(table) {
			return s
		}
		return table[i]
	}
	for i := range c.Defs {
		c.Defs[i].Path = at(c.Defs[i].Path)
	}
	for i := range c.LocRefs {
		c.LocRefs[i].Path = at(c.LocRefs[i].Path)
	}
	for i := range c.CallRefs {
		c.CallRefs[i].Path = at(c.CallRefs[i].Path)
	}
	for i := range c.Edges {
		c.Edges[i].Path = at(c.Edges[i].Path)
	}
	c.Paths = nil
}
