// persist.go keeps a scanned install between runs: the schema as the text file a person can read and diff, and the rest as one JSON file through package persist, which refuses a wrong format from the file's first bytes. Only the five record slices, the vocabulary and the game info are stored. The lookups are rebuilt on load, and every word is made one shared copy again, because a decoder gives each record its own copy of "traits".
package index

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/persist"
)

// FormatVersion is the cache's format. A file of another format is scanned again, never migrated.
const FormatVersion = 1

// stored is the JSON file. formatVersion and scannedAt come first: persist reads them from the head of the file without decoding the rest.
type stored struct {
	FormatVersion int           `json:"formatVersion"`
	ScannedAt     string        `json:"scannedAt"`
	Index         *Index        `json:"index"`
	Vocab         *schema.Vocab `json:"vocab"`
	Info          schema.Info   `json:"info"`
}

var unsafeInName = regexp.MustCompile(`[^A-Za-z0-9]+`)

// CachePath is where an install's cache lives, without an extension: vanilla-<install>-<version> in the app's cache folder, with latest for an install that states no version.
func CachePath(installID, version string) (string, error) {
	dir, err := persist.CacheDir()
	if version == "" {
		version = "latest"
	}
	return filepath.Join(dir, "vanilla-"+unsafeInName.ReplaceAllString(installID, "_")+"-"+unsafeInName.ReplaceAllString(version, "_")), err
}

// ScannedAt is when the cache at base was written, "" where there is none or it is of another format.
func ScannedAt(base string) string {
	version, scannedAt, err := persist.Peek(base + ".json")
	if err != nil || version != FormatVersion {
		return ""
	}
	return scannedAt
}

// Save writes an install to base.json and base.schema.txt.
func Save(base, scannedAt string, in *Install) error {
	if err := os.WriteFile(base+".schema.txt", []byte(schema.Write(in.Schema)), 0o644); err != nil {
		return err
	}
	return persist.SaveJSON(base+".json", stored{FormatVersion: FormatVersion, ScannedAt: scannedAt, Index: in.Index, Vocab: in.Vocab, Info: in.Info})
}

// Load reads an install back, its lookups built before it is shared. Its slices are cut to their length, because a decoder grows them by doubling, and its words are shared again.
func Load(base string) (*Install, error) {
	text, err := os.ReadFile(base + ".schema.txt")
	if err != nil {
		return nil, err
	}
	s, err := schema.Read(string(text))
	if err != nil {
		return nil, err
	}
	kept, err := persist.LoadJSON[stored](base+".json", FormatVersion)
	if err != nil {
		return nil, err
	}

	x := kept.Index
	x.shared = map[string]string{}
	x.Files, x.Defs, x.Refs, x.Edges, x.Loc = slices.Clone(x.Files), slices.Clone(x.Defs), slices.Clone(x.Refs), slices.Clone(x.Edges), slices.Clone(x.Loc)
	for i := range x.Defs {
		d := &x.Defs[i]
		d.Kind, d.Name, d.Parent, d.Label, d.Mode, d.ScopeType = x.own(d.Kind), x.own(d.Name), x.own(d.Parent), x.own(d.Label), x.own(d.Mode), x.own(d.ScopeType)
	}
	for i := range x.Refs {
		r := &x.Refs[i]
		r.Kind, r.Name, r.Parent = x.own(r.Kind), x.own(r.Name), x.own(r.Parent)
	}
	for i := range x.Edges {
		e := &x.Edges[i]
		e.FromKind, e.From, e.ToKind, e.To, e.Via = x.own(e.FromKind), x.own(e.From), x.own(e.ToKind), x.own(e.To), x.own(e.Via)
	}
	for i := range x.Loc {
		x.Loc[i].Key = x.own(x.Loc[i].Key)
	}
	x.shared = nil
	x.Ready()
	return &Install{Index: x, Schema: s, Vocab: kept.Vocab, Info: kept.Info}, nil
}

// Drop removes every cache file of an install, whatever its version.
func Drop(installID string) error {
	base, err := CachePath(installID, "x")
	if err != nil {
		return err
	}
	prefix := strings.TrimSuffix(filepath.Base(base), "x")
	entries, err := os.ReadDir(filepath.Dir(base))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			err = os.Remove(filepath.Join(filepath.Dir(base), e.Name()))
		}
	}
	return err
}
