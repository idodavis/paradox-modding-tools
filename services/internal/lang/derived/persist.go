// persist.go saves and loads VanillaCache and vanilla loc sidecars as JSON.

package derived

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
)

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

func SaveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	// Rename first: on POSIX that replaces atomically, and Go's Windows Rename
	// does pass MOVEFILE_REPLACE_EXISTING. Remove only when it fails.
	//
	// Removing unconditionally is wrong — it opens a window where the good file
	// is gone and the replacement is still only a .tmp. But NOT removing at all
	// was worse, and specifically worse on Windows: replace-existing returns
	// ERROR_ACCESS_DENIED whenever anything holds the destination open, and Go
	// opens files with FILE_SHARE_DELETE, so unlinking the name succeeds exactly
	// where replacing it does not. A 98 MB cache being read by the app or walked
	// by an antivirus is that case, and it left every rescan writing a .tmp the
	// engine then refused — a scan that ran, cost minutes, and changed nothing.
	//
	// So: atomic where the platform gives it, and a fallback that still lands
	// the file where it does not. The window only opens after the safe path has
	// already failed.
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, path)
}

func CacheVersion(v string) string {
	if v == "" {
		return "latest"
	}
	return v
}

var ErrFormat = errors.New("format version")

// LoadJSON reads path and rejects a missing or mismatched formatVersion.
func LoadJSON[T any](path string, wantVersion int) (*T, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := sonic.Get(raw, "formatVersion")
	got, nerr := n.Int64()
	if err != nil || nerr != nil {
		return nil, fmt.Errorf("missing formatVersion: %w", ErrFormat)
	}
	if int(got) != wantVersion {
		return nil, fmt.Errorf("format %d != %d: %w", got, wantVersion, ErrFormat)
	}
	var dst T
	if err := sonic.Unmarshal(raw, &dst); err != nil {
		return nil, err
	}
	return &dst, nil
}

func dropLegacyWorkspaceCaches(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "cache-*.json"))
	for _, p := range matches {
		_ = os.Remove(p)
	}
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
	dropLegacyWorkspaceCaches(filepath.Dir(path))
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

// PeekCacheScannedAt returns scannedAt from the vanilla JSON without loading
// defs, and "" when the file is one LoadCache would refuse.
//
// The version check is the point, not an extra. Without it this answered "is
// there a cache?" by a different rule than LoadCache — file exists, versus file
// exists AND the format matches — so after a CacheFormatVersion bump the install
// list went on showing the previous scan date for a model the engine had already
// discarded. Nothing looked wrong while hover had no vanilla model at all.
func PeekCacheScannedAt(installID, version string) string {
	path, err := vanillaJSON("vanilla", installID, version)
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if n, err := sonic.Get(raw, "formatVersion"); err == nil {
		if got, nerr := n.Int64(); nerr != nil || int(got) != CacheFormatVersion {
			return ""
		}
	} else {
		return ""
	}
	n, err := sonic.Get(raw, "scannedAt")
	if err != nil {
		return ""
	}
	s, _ := n.String()
	return s
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

// DropVanillaFiles removes vanilla-* files for installID (script + loc sidecars)
// and the archived script_docs copies. Dropping the archive here is deliberate:
// this is the "forget this install" path, and a stale archive would otherwise
// outlive the install it describes.
// DropVanillaLocFiles removes an install's localization sidecars, leaving the
// model cache and the script_docs archive alone.
//
// Rescan harvests only english (Scan is called with LocLang "english"; every
// other language is harvested on first use). With a pinned or "latest" version
// key the other languages keep the previous patch's filename, and LoadVanillaLoc
// accepts them because the format still matches — so a rescan after a game patch
// left every non-english workspace reading pre-patch vanilla loc. Dropping them
// costs one re-harvest on next demand, which loadVanillaLoc already does.
func DropVanillaLocFiles(installID string) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	prefix := "vanilla-loc-" + sanitize(installID)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

func DropVanillaFiles(installID string) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	sid := sanitize(installID)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "vanilla-"+sid),
			strings.HasPrefix(name, "vanilla-data-"+sid),
			strings.HasPrefix(name, "vanilla-loc-"+sid):
			_ = os.Remove(filepath.Join(dir, name))
		case strings.HasPrefix(name, "script-docs-"+sid):
			_ = os.RemoveAll(filepath.Join(dir, name))
		}
	}
	return nil
}

// Edge into one table plus indices.
//
// A CK3 install produces millions of records and each one carried its own copy
// of a ~110-byte install path, which dominated the on-disk model. Interning
// happens only at the save/load boundary: Path holds a real path everywhere
// else, so no consumer changes.
//
// Loading also hands every record with the same path the one string from the
// table, so the saving shows up in RAM as well as on disk.

// internPaths returns a copy of c whose Def/Ref/Edge paths are decimal indices
// into the returned Paths table. The receiver is left untouched: a live session
// may be holding it while the scan writes.
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

// expandPaths turns interned indices back into paths, in place. The cache has
// just been decoded and is not shared yet. A cache written before interning has
// no table and is left alone.
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
		if err != nil || i < 0 || i >= len(table) {
			// Not an index: a pre-interning file, or a corrupt one. Keep the
			// value rather than losing the location.
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
