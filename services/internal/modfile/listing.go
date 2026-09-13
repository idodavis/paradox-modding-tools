// listing.go reads and patches descriptor.mod / metadata.json and listing descriptions.

package modfile

import (
	"encoding/json"
	"fmt"
	"os"
	"paradox-modding-tools/services/internal/game"
	"path"
	"path/filepath"
	"strings"
)

// ListingFields are the descriptor keys the Release page edits.
type ListingFields struct {
	Name             string
	Version          string
	SupportedVersion string
	Tags             []string
	RemoteFileID     string
	Picture          string
	ShortDescription string
}

type listingField struct {
	mod  string
	json string
	// jsonAlso are keys read but never written. EU5's Basileia Romaion
	// declares `thumbnail` where the game itself writes `picture`.
	jsonAlso []string
	// modDropEmpty removes the line rather than writing `key=""`. The launcher
	// requires name and version, so those two keep an empty value; the rest are
	// optional and an empty one is noise.
	modDropEmpty bool
	// jsonKeepEmpty leaves the key alone instead of deleting it. Only picture,
	// which ThumbnailPath owns — clearing it from here would drop a thumbnail
	// the Release page never touched.
	jsonKeepEmpty bool

	get func(*ListingFields) string
	set func(*ListingFields, string)
}

// listingFields is also the order missing keys are appended to a .mod file.
var listingFields = []listingField{
	{mod: "name", json: "name",
		get: func(f *ListingFields) string { return f.Name },
		set: func(f *ListingFields, v string) { f.Name = v }},
	{mod: "version", json: "version",
		get: func(f *ListingFields) string { return f.Version },
		set: func(f *ListingFields, v string) { f.Version = v }},
	{mod: "supported_version", json: "supported_game_version", modDropEmpty: true,
		get: func(f *ListingFields) string { return f.SupportedVersion },
		set: func(f *ListingFields, v string) { f.SupportedVersion = v }},
	{mod: "remote_file_id", json: "remote_file_id", modDropEmpty: true,
		get: func(f *ListingFields) string { return f.RemoteFileID },
		set: func(f *ListingFields, v string) { f.RemoteFileID = v }},
	{mod: "picture", json: "picture", jsonAlso: []string{"thumbnail"},
		modDropEmpty: true, jsonKeepEmpty: true,
		get: func(f *ListingFields) string { return f.Picture },
		set: func(f *ListingFields, v string) { f.Picture = v }},
	{json: "short_description",
		get: func(f *ListingFields) string { return f.ShortDescription },
		set: func(f *ListingFields, v string) { f.ShortDescription = v }},
}

// ReadListingFields loads CK3 .mod or Vic3/EU5 metadata.json under root.

func ReadListingFields(gameID, root string) (ListingFields, error) {
	path := game.DescriptorPath(gameID, root)
	raw, err := os.ReadFile(path)
	if err != nil {
		return ListingFields{}, err
	}
	info := game.Get(gameID)
	if info != nil && info.Descriptor == "metadata" {
		return readMetadataJSON(raw)
	}
	return readDescriptorMod(string(raw)), nil
}

// WriteListingFields patches known keys and preserves the rest of the file.
func WriteListingFields(gameID, root string, f ListingFields) error {
	path := game.DescriptorPath(gameID, root)
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	info := game.Get(gameID)
	var next []byte
	if info != nil && info.Descriptor == "metadata" {
		next, err = patchMetadataJSON(raw, f)
	} else {
		next = []byte(patchDescriptorMod(string(raw), f))
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, next, 0o644)
}

const (
	// DescMdName is the default Steam/Paradox description Markdown file.
	DescMdName = "mod-description.md"
	// DescBbName is the default Workshop BBCode description file.
	DescBbName = "mod-description.bbcode"
)

// EnsureDescriptions creates missing default description files without overwriting.
func EnsureDescriptions(root, name, description string) error {
	body := descOrName(description, name)
	md := filepath.Join(root, DescMdName)
	bb := filepath.Join(root, DescBbName)
	if _, err := os.Stat(md); os.IsNotExist(err) {
		if err := os.WriteFile(md, []byte("# "+name+"\n\n"+body+"\n"), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(bb); os.IsNotExist(err) {
		if err := os.WriteFile(bb, []byte("[b]"+name+"[/b]\n\n"+body+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

var ThumbnailExts = []string{".png", ".gif", ".jpg", ".jpeg"}

// ThumbnailPath answers both thumbnail questions at once: where this mod's
// listing image IS, and where a new one should go.
//
// exists reports that the mod already has one, in which case rel is that exact
// file and it keeps its place. Only a mod with none is given the game's
// convention (game.GameInfo.ThumbnailDir) with ext.
//
// Both locations are searched for every game, because mods do not follow one
// convention: 4 of 14 Victoria 3 workshop mods carry a thumbnail in both places
// and 10 carry it only at the root, while EU5 is the mirror image. The game's
// own directory is searched first so a mod with both resolves the way the game
// reads it.
//
// One function rather than the three that used to disagree: ThumbnailRel keyed
// the location off descriptor format, copyThumbnail ignored the game entirely
// and always wrote to the root, and listingThumb fell back to the declared
// `picture` only after ThumbnailRel missed. A Vic3 mod made by the wizard and
// re-thumbnailed on the Release page ended up with two thumbnail files and
// `picture` naming the stale one.
func ThumbnailPath(gameID, root, ext string) (rel string, exists bool) {
	dirs := []string{"", game.MetadataDir}
	if conv := thumbnailDir(gameID); conv != "" {
		dirs = []string{conv, ""}
	}
	for _, dir := range dirs {
		for _, e := range ThumbnailExts {
			r := path.Join(dir, "thumbnail"+e)
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(r))); err == nil {
				return r, true
			}
		}
	}
	if ext == "" {
		ext = ThumbnailExts[0]
	}
	return path.Join(thumbnailDir(gameID), "thumbnail"+strings.ToLower(ext)), false
}

func thumbnailDir(gameID string) string {
	if info := game.Get(gameID); info != nil {
		return info.ThumbnailDir
	}
	return ""
}

func readDescriptorMod(src string) ListingFields {
	var f ListingFields
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "tags") && strings.Contains(trim, "{") {
			f.Tags, i = collectModTags(lines, i)
			continue
		}
		k, v, ok := splitModKV(lines[i])
		if !ok {
			continue
		}
		for _, lf := range listingFields {
			if lf.mod == k {
				lf.set(&f, v)
				break
			}
		}
	}
	return f
}

func patchDescriptorMod(src string, f ListingFields) string {
	if strings.TrimSpace(src) == "" {
		src = "version=\"1.0\"\nname=\"mod\"\n"
	}
	byMod := map[string]listingField{}
	for _, lf := range listingFields {
		if lf.mod != "" {
			byMod[lf.mod] = lf
		}
	}
	seen := map[string]bool{}
	var out []string
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "tags") && strings.Contains(trim, "{") {
			_, i = collectModTags(lines, i)
			out = append(out, formatModTags(f.Tags)...)
			seen["tags"] = true
			continue
		}
		k, _, ok := splitModKV(line)
		if ok {
			if lf, want := byMod[k]; want {
				seen[k] = true
				v := lf.get(&f)
				if v == "" && lf.modDropEmpty {
					continue
				}
				out = append(out, k+"="+quoteMod(v))
				continue
			}
		}
		out = append(out, line)
	}
	// A key the file never had is appended only when it has something to say.
	for _, lf := range listingFields {
		if lf.mod == "" || seen[lf.mod] || lf.get(&f) == "" {
			continue
		}
		out = append(out, lf.mod+"="+quoteMod(lf.get(&f)))
	}
	if !seen["tags"] && len(f.Tags) > 0 {
		out = append(out, formatModTags(f.Tags)...)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func splitModKV(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	eq := strings.IndexByte(line, '=')
	if eq < 1 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.Trim(strings.TrimSpace(line[eq+1:]), `"`)
	return key, val, true
}

func collectModTags(lines []string, i int) ([]string, int) {
	var tags []string
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "}") && !strings.Contains(line, "{") {
			return tags, i
		}
		if s := strings.Trim(line, `"`); s != line && s != "" {
			tags = append(tags, s)
		}
		i++
	}
	return tags, i
}

func formatModTags(tags []string) []string {
	out := []string{"tags={"}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, "\t\""+strings.ReplaceAll(t, `"`, `'`)+"\"")
	}
	out = append(out, "}")
	return out
}

func quoteMod(s string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(s), `"`, `'`) + `"`
}

func readMetadataJSON(raw []byte) (ListingFields, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ListingFields{}, err
	}
	var f ListingFields
	for _, lf := range listingFields {
		lf.set(&f, firstStr(m, append([]string{lf.json}, lf.jsonAlso...)...))
	}
	if arr, ok := m["tags"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && s != "" {
				f.Tags = append(f.Tags, s)
			}
		}
	}
	return f, nil
}

func patchMetadataJSON(raw []byte, f ListingFields) ([]byte, error) {
	var m map[string]any
	if len(raw) == 0 {
		m = map[string]any{}
	} else if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for _, lf := range listingFields {
		v := strings.TrimSpace(lf.get(&f))
		if v == "" {
			if !lf.jsonKeepEmpty {
				delete(m, lf.json)
			}
			continue
		}
		m[lf.json] = v
	}
	if f.Tags != nil {
		tags := make([]any, 0, len(f.Tags))
		for _, t := range f.Tags {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		m["tags"] = tags
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func strField(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strField(m, k); s != "" {
			return s
		}
	}
	return ""
}

// FirstParagraph is the first non-empty paragraph of listing text (short_description).
func FirstParagraph(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for p := range strings.SplitSeq(s, "\n\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			if len(p) > 200 {
				return p[:200]
			}
			return p
		}
	}
	return ""
}

// BumpPatch increments the last numeric component of a dotted version.
func BumpPatch(v string) string {
	return bumpAt(v, 2)
}

func BumpMinor(v string) string {
	return bumpAt(v, 1)
}

func bumpAt(v string, idx int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "1.0.0"
	}
	parts := strings.Split(v, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	n := 0
	fmt.Sscanf(parts[idx], "%d", &n)
	parts[idx] = fmt.Sprintf("%d", n+1)
	for i := idx + 1; i < len(parts); i++ {
		if _, err := fmt.Sscanf(parts[i], "%d", new(int)); err == nil {
			parts[i] = "0"
		}
	}
	return strings.Join(parts, ".")
}

var DescriptorModKeys = []string{
	"name", "version", "tags", "supported_version", "path",
	"remote_file_id", "picture", "dependencies",
}
