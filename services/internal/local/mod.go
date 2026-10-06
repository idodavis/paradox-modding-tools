// mod.go is a mod's own Paradox files: the descriptor its game wants (descriptor.mod, or
// .metadata/metadata.json), the thumbnail where that game keeps it, and the skeleton written
// for a new mod. Which descriptor and which thumbnail folder a game uses are rows in
// internal/game; what only the Steam Workshop reads lives in internal/steam.

package local

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/game"
)

// SteamItemRel is a metadata.json mod's Workshop id, what descriptor.mod keeps as remote_file_id:
// the format has no key for one (neither wiki lists one, and no Workshop mod writes one).
const steamItemRel = "workshop/steam-item.txt"

// ListingFields are the descriptor keys the Release page edits, and the folders the mod unloads.
type ListingFields struct {
	Name             string
	Version          string
	SupportedVersion string
	Tags             []string
	RemoteFileID     string
	Picture          string
	ShortDescription string
	ID               string   // metadata.json only: written with a new mod, never cleared
	ReplacePaths     []string // read, never written: replace_path lines, or game_custom_data.replace_paths
}

// listingField is one descriptor key, spelled for .mod and for metadata.json.
type listingField struct {
	mod           string
	json          string
	jsonAlso      []string // keys read, never written: an EU5 mod may write `thumbnail` where the game writes `picture`
	modDropEmpty  bool     // an empty value removes the line; name and version never drop, the launcher needs them
	jsonKeepEmpty bool     // an empty value leaves the key: picture (CopyThumbnail owns it), id (a new mod writes it)

	value func(*ListingFields) *string
}

// listingFields is also the order missing keys are appended to a .mod file.
var listingFields = []listingField{
	{mod: "name", json: "name", value: func(fields *ListingFields) *string { return &fields.Name }},
	{mod: "version", json: "version", value: func(fields *ListingFields) *string { return &fields.Version }},
	{mod: "supported_version", json: "supported_game_version", modDropEmpty: true,
		value: func(fields *ListingFields) *string { return &fields.SupportedVersion }},
	// metadata.json's is steamItemRel
	{mod: "remote_file_id", modDropEmpty: true, value: func(fields *ListingFields) *string { return &fields.RemoteFileID }},
	{mod: "picture", json: "picture", jsonAlso: []string{"thumbnail"}, modDropEmpty: true, jsonKeepEmpty: true,
		value: func(fields *ListingFields) *string { return &fields.Picture }},
	{json: "short_description", value: func(fields *ListingFields) *string { return &fields.ShortDescription }},
	{json: "id", jsonKeepEmpty: true, value: func(fields *ListingFields) *string { return &fields.ID }},
}

// ReadListingFields loads the mod's descriptor: descriptor.mod or metadata.json, per its game. A
// missing one reads as no fields.
func ReadListingFields(gameID, root string) (ListingFields, error) {
	raw, err := os.ReadFile(game.DescriptorPath(gameID, root))
	if os.IsNotExist(err) {
		return ListingFields{}, nil
	}
	if err != nil {
		return ListingFields{}, err
	}
	if info := game.Get(gameID); info != nil && info.Descriptor == "metadata" {
		fields, err := readMetadataJSON(raw)
		id, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(steamItemRel)))
		fields.RemoteFileID = strings.TrimSpace(string(id))
		return fields, err
	}
	return readDescriptorMod(string(raw)), nil
}

// WriteListingFields patches the known keys and keeps the rest of the file as written.
func WriteListingFields(gameID, root string, fields ListingFields) error {
	descriptor := game.DescriptorPath(gameID, root)
	raw, err := os.ReadFile(descriptor)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// a descriptor that is not there yet is patched from nothing
	var next []byte
	if info := game.Get(gameID); info != nil && info.Descriptor == "metadata" {
		if next, err = patchMetadataJSON(raw, fields); err != nil {
			return err
		}
		if err := writeSteamItem(root, fields.RemoteFileID); err != nil {
			return err
		}
	} else {
		next = []byte(patchDescriptorMod(string(raw), fields))
	}
	return WriteBytes(descriptor, next)
}

// writeSteamItem keeps a metadata.json mod's Workshop id in steamItemRel; no id, no file.
func writeSteamItem(root, id string) error {
	file := filepath.Join(root, filepath.FromSlash(steamItemRel))
	if id = strings.TrimSpace(id); id == "" {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return WriteBytes(file, []byte(id+"\n"))
}

// ThumbnailExts are the image types a listing thumbnail may be.
var ThumbnailExts = []string{".png", ".gif", ".jpg", ".jpeg"}

// thumbnailDirs is where a new listing image goes, and every folder a mod may keep one in, the
// game's own first: mods follow no one convention, and some keep one in both places.
func thumbnailDirs(gameID string) (convention string, dirs []string) {
	// a game the registry does not know states no convention, so a thumbnail goes at the root
	if info := game.Get(gameID); info != nil {
		convention = info.ThumbnailDir
	}
	if convention != "" {
		return convention, []string{convention, ""}
	}
	return "", []string{"", game.MetadataDir}
}

// FindThumbnail is the mod's listing image relative to its root: thumbnail.<ext> where its game
// or the mod keeps one, else the file the descriptor's picture names beside it or in .metadata.
func FindThumbnail(gameID, root, picture string) (string, bool) {
	var candidates []string
	_, dirs := thumbnailDirs(gameID)
	for _, dir := range dirs {
		for _, ext := range ThumbnailExts {
			candidates = append(candidates, path.Join(dir, "thumbnail"+ext))
		}
	}
	if picture != "" {
		candidates = append(candidates, path.Clean(filepath.ToSlash(picture)), path.Join(game.MetadataDir, filepath.ToSlash(picture)))
	}
	for _, rel := range candidates {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return rel, true
		}
	}
	return "", false
}

// CopyThumbnail installs src as the mod's listing image and returns where it went relative to
// root: in place of the mod's current one (same folder, src's extension), else the game's folder.
func CopyThumbnail(gameID, root, src string) (string, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if !slices.Contains(ThumbnailExts, ext) {
		return "", fmt.Errorf("thumbnail must be one of %s", strings.Join(ThumbnailExts, ", "))
	}
	convention, _ := thumbnailDirs(gameID)
	rel := path.Join(convention, "thumbnail"+ext)
	existing, found := FindThumbnail(gameID, root, "")
	if found {
		rel = strings.TrimSuffix(existing, path.Ext(existing)) + ext
	}
	if err := CopyFile(src, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		return "", err
	}
	// a new extension: the superseded file goes, so there are never two to choose between
	if found && rel != existing {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(existing)))
	}
	return rel, nil
}

func readDescriptorMod(src string) ListingFields {
	var fields ListingFields
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "tags") && strings.Contains(trim, "{") {
			fields.Tags, i = collectModTags(lines, i)
			continue
		}
		key, value, ok := splitModKV(lines[i])
		if !ok {
			continue
		}
		if key == "replace_path" {
			fields.ReplacePaths = append(fields.ReplacePaths, value)
		}
		for _, field := range listingFields {
			if field.mod == key {
				*field.value(&fields) = value
				break
			}
		}
	}
	return fields
}

func patchDescriptorMod(src string, fields ListingFields) string {
	if strings.TrimSpace(src) == "" {
		src = "version=\"1.0\"\nname=\"mod\"\n"
	}
	byKey := map[string]listingField{}
	for _, field := range listingFields {
		if field.mod != "" {
			byKey[field.mod] = field
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
			out = append(out, formatModTags(fields.Tags)...)
			seen["tags"] = true
			continue
		}
		key, _, ok := splitModKV(line)
		if ok {
			if field, known := byKey[key]; known {
				seen[key] = true
				value := *field.value(&fields)
				if value == "" && field.modDropEmpty {
					continue
				}
				out = append(out, key+"="+quoteMod(value))
				continue
			}
		}
		out = append(out, line)
	}
	// a key the file never had is appended only when it has something to say
	for _, field := range listingFields {
		if field.mod == "" || seen[field.mod] || *field.value(&fields) == "" {
			continue
		}
		out = append(out, field.mod+"="+quoteMod(*field.value(&fields)))
	}
	if !seen["tags"] && len(fields.Tags) > 0 {
		out = append(out, formatModTags(fields.Tags)...)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func splitModKV(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	eq := strings.IndexByte(line, '=')
	if eq < 1 {
		return "", "", false
	}
	// a quoted value ends at its closing quote, a bare one at a comment
	key, value = strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:])
	if body, quoted := strings.CutPrefix(value, `"`); quoted {
		value, _, _ = strings.Cut(body, `"`)
		return key, value, true
	}
	value, _, _ = strings.Cut(value, "#")
	return key, strings.TrimSpace(value), true
}

var quotedTag = regexp.MustCompile(`"([^"]*)"`)

// collectModTags reads the tags block opening on lines[i], on one line or many: each quoted tag up to
// the closing brace, and the line the block ends on.
func collectModTags(lines []string, i int) ([]string, int) {
	var tags []string
	_, text, _ := strings.Cut(lines[i], "{")
	for {
		body, _, closed := strings.Cut(text, "}")
		for _, match := range quotedTag.FindAllStringSubmatch(body, -1) {
			if match[1] != "" {
				tags = append(tags, match[1])
			}
		}
		if closed || i+1 == len(lines) {
			return tags, i
		}
		i++
		text = lines[i]
	}
}

func formatModTags(tags []string) []string {
	out := []string{"tags={"}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		out = append(out, "\t\""+strings.ReplaceAll(tag, `"`, `'`)+"\"")
	}
	out = append(out, "}")
	return out
}

func quoteMod(text string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(text), `"`, `'`) + `"`
}

// readMetadataJSON reads a metadata.json's listing fields; a leading byte-order mark, common in EU5
// mods, is dropped first, as encoding/json rejects it.
func readMetadataJSON(raw []byte) (ListingFields, error) {
	var metadata map[string]any
	if err := json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), &metadata); err != nil {
		return ListingFields{}, err
	}
	var fields ListingFields
	for _, field := range listingFields {
		*field.value(&fields) = firstString(metadata, append([]string{field.json}, field.jsonAlso...)...)
	}
	// the two lists of strings: the tags, and the folders game_custom_data says the mod replaces
	custom, _ := metadata["game_custom_data"].(map[string]any)
	for into, values := range map[*[]string]any{&fields.Tags: metadata["tags"], &fields.ReplacePaths: custom["replace_paths"]} {
		listed, _ := values.([]any)
		for _, value := range listed {
			if text, ok := value.(string); ok && text != "" {
				*into = append(*into, text)
			}
		}
	}
	return fields, nil
}

// patchMetadataJSON writes the listing fields into a metadata.json, keeping its byte-order mark.
func patchMetadataJSON(raw []byte, fields ListingFields) ([]byte, error) {
	metadata := map[string]any{}
	body, marked := bytes.CutPrefix(raw, utf8BOM)
	if len(body) > 0 {
		if err := json.Unmarshal(body, &metadata); err != nil {
			return nil, err
		}
	}
	for _, field := range listingFields {
		if field.json == "" {
			continue
		}
		value := strings.TrimSpace(*field.value(&fields))
		if value == "" {
			if !field.jsonKeepEmpty {
				delete(metadata, field.json)
			}
			continue
		}
		metadata[field.json] = value
	}
	if fields.Tags != nil {
		tags := make([]any, 0, len(fields.Tags))
		for _, tag := range fields.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		metadata["tags"] = tags
	}
	out, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, err
	}
	if marked {
		out = append(append([]byte{}, utf8BOM...), out...)
	}
	return append(out, '\n'), nil
}

// firstString is the first of keys that holds a non-empty string.
func firstString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := metadata[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DefaultModParent is the game's user data folder's mod/, or "" where the game has none.
func DefaultModParent(gameID string) string {
	userData := game.UserDataDir(gameID, "")
	if userData == "" {
		return ""
	}
	return filepath.Join(userData, "mod")
}

// ModSlug is a filesystem-safe folder name from a display name.
func ModSlug(name string) string {
	var slug strings.Builder
	afterUnderscore := false
	for _, char := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			slug.WriteRune(char)
			afterUnderscore = false
			continue
		}
		if !afterUnderscore {
			slug.WriteByte('_')
			afterUnderscore = true
		}
	}
	return cmp.Or(strings.Trim(slug.String(), "_"), "mod")
}

// NewModOpts is what WriteNewMod is asked to make.
type NewModOpts struct {
	GameID, Root, Name, SupportedVersion, LocLang, ThumbnailSrc string
}

// WriteNewMod creates root with a descriptor, each of the game's ModFolders empty but for a loc
// stub in the localization one's language folder, and the thumbnail when one is given.
func WriteNewMod(opts NewModOpts) error {
	info := game.Get(opts.GameID)
	if info == nil {
		return fmt.Errorf("unknown game %s", opts.GameID)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	root := opts.Root
	descriptor := game.DescriptorPath(opts.GameID, root)
	if _, err := os.Stat(descriptor); err == nil {
		return fmt.Errorf("mod already exists: %s", descriptor)
	}

	// picture names the bare file even where it sits in .metadata/, as workshop mods declare it
	picture := ""
	if src := strings.TrimSpace(opts.ThumbnailSrc); src != "" {
		rel, err := CopyThumbnail(opts.GameID, root, src)
		if err != nil {
			return err
		}
		picture = path.Base(rel)
	}

	// "latest" is not a version a descriptor may state; each format's version is spelled the
	// way its games' mods spell it, and metadata.json also carries an id
	fields := ListingFields{Name: name, Version: "1.0", SupportedVersion: strings.TrimSpace(opts.SupportedVersion), Picture: picture}
	if strings.EqualFold(fields.SupportedVersion, "latest") {
		fields.SupportedVersion = ""
	}
	if info.Descriptor == "metadata" {
		fields.Version, fields.ID = "1.0.0", ModSlug(name)
	}
	if err := WriteListingFields(opts.GameID, root, fields); err != nil {
		return err
	}
	language, locDir := cmp.Or(strings.TrimSpace(opts.LocLang), game.BaseLanguage), ""
	for _, folder := range info.ModFolders {
		dir := filepath.Join(root, filepath.FromSlash(folder))
		if strings.HasSuffix("/"+folder, "/localization") {
			dir = filepath.Join(dir, language)
			locDir = dir
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if locDir == "" {
		return nil
	}
	locName := ModSlug(name) + "_l_" + language + ".yml"
	locBody := append(append([]byte{}, utf8BOM...), []byte("l_"+language+":\n")...)
	return WriteBytes(filepath.Join(locDir, locName), locBody)
}
