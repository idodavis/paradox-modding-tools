// modfiles.go is a mod's own Paradox files: the descriptor its game wants, the thumbnail where that game keeps it,
// and the skeleton written for a new mod.

// Package modfiles is a mod's own Paradox files: the descriptor its game wants (descriptor.mod, or
// .metadata/metadata.json), the thumbnail where that game keeps it, and the skeleton written for a new mod. Which
// descriptor and which thumbnail folder a game uses are rows in internal/game; what only the Steam Workshop reads
// lives in internal/steam.
package modfiles

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
)

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

// NewModOpts is what Create is asked to make.
type NewModOpts struct {
	GameID, Root, Name, SupportedVersion, LocLang, ThumbnailSrc string
}

// steamItemRel is a metadata.json mod's Workshop id, what descriptor.mod keeps as remote_file_id: the format has no
// key for one (neither wiki lists one, and no Workshop mod writes one).
const steamItemRel = "workshop/steam-item.txt"

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

// ThumbnailExts are the image types a listing thumbnail may be.
var ThumbnailExts = []string{".png", ".gif", ".jpg", ".jpeg"}

// LoadDescriptor loads the mod's descriptor: descriptor.mod or metadata.json, per its game. A missing one loads as
// no fields.
func LoadDescriptor(gameID, root string) (ListingFields, error) {
	text, err := inventory.LoadText(game.DescriptorPath(gameID, root))
	if os.IsNotExist(err) {
		return ListingFields{}, nil
	}
	if err != nil {
		return ListingFields{}, err
	}
	if info := game.Get(gameID); info != nil && info.ModDescriptor == game.MetadataDescriptor {
		fields, err := readMetadataJSON(text.Content)
		steamItem, _ := inventory.LoadText(inventory.Join(root, steamItemRel))
		fields.RemoteFileID = strings.TrimSpace(steamItem.Content)
		return fields, err
	}
	return loadDescriptorMod(text.Content), nil
}

// loadDescriptorMod reads a descriptor.mod's listing fields from its top-level statements: a key a listing field
// spells, every replace_path, and the quoted or bare entries of the tags block. A key set twice keeps its last value.
func loadDescriptorMod(src string) ListingFields {
	var fields ListingFields
	for _, statement := range jomini.Parse(src).Root.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			continue
		}
		if block := jomini.BlockOf(assign.Value); block != nil && assign.Key.Text == "tags" {
			fields.Tags = nil
			for _, entry := range block.Statements {
				item, isItem := entry.(*jomini.ValueStmt)
				if !isItem {
					continue
				}
				if tag, isScalar := item.Value.(*jomini.Scalar); isScalar && tag.Text != "" {
					fields.Tags = append(fields.Tags, tag.Text)
				}
			}
			continue
		}
		scalar, isScalar := assign.Value.(*jomini.Scalar)
		if !isScalar {
			continue
		}
		if assign.Key.Text == "replace_path" {
			fields.ReplacePaths = append(fields.ReplacePaths, scalar.Text)
		}
		if field, known := modField(assign.Key.Text); known {
			*field.value(&fields) = scalar.Text
		}
	}
	return fields
}

// modField is the listing field a descriptor.mod key spells.
func modField(key string) (listingField, bool) {
	for _, field := range listingFields {
		if field.mod != "" && field.mod == key {
			return field, true
		}
	}
	return listingField{}, false
}

// readMetadataJSON reads a metadata.json's listing fields.
func readMetadataJSON(content string) (ListingFields, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		return ListingFields{}, err
	}
	var fields ListingFields
	for _, field := range listingFields {
		*field.value(&fields) = firstString(metadata, append([]string{field.json}, field.jsonAlso...)...)
	}
	// the two lists of strings: the tags, and the folders game_custom_data says the mod replaces
	custom, _ := metadata["game_custom_data"].(map[string]any)
	for list, values := range map[*[]string]any{&fields.Tags: metadata["tags"], &fields.ReplacePaths: custom["replace_paths"]} {
		items, _ := values.([]any)
		for _, value := range items {
			if text, ok := value.(string); ok && text != "" {
				*list = append(*list, text)
			}
		}
	}
	return fields, nil
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

// WriteDescriptor patches the known keys and keeps the rest of the file as written, its byte-order mark and encoding
// too.
func WriteDescriptor(gameID, root string, fields ListingFields) error {
	descriptor := game.DescriptorPath(gameID, root)
	text, err := inventory.LoadText(descriptor)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// a descriptor that is not there yet is patched from nothing
	if info := game.Get(gameID); info != nil && info.ModDescriptor == game.MetadataDescriptor {
		if text.Content, err = patchMetadataJSON(text.Content, fields); err != nil {
			return err
		}
		if err := writeSteamItem(root, fields.RemoteFileID); err != nil {
			return err
		}
	} else {
		text.Content = patchDescriptorMod(text.Content, fields)
	}
	return inventory.SaveText(descriptor, text)
}

// patchDescriptorMod writes the listing fields into a descriptor.mod by the range of each statement it changes, so
// comments and keys it does not know stay as written. A key the file never had is appended when it has a value.
func patchDescriptorMod(src string, fields ListingFields) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if strings.TrimSpace(src) == "" {
		src = "version=\"1.0\"\nname=\"mod\"\n"
	}
	result := jomini.Parse(src)
	index := result.Lines()
	seen := map[string]bool{}
	var patched strings.Builder
	cursor := 0 // src[:cursor] is in patched: the statements go in source order, so the ranges never overlap
	for _, statement := range result.Root.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			continue
		}
		key := assign.Key.Text
		field, known := modField(key)
		span, text := assign.Range, ""
		switch {
		case known && field.modDropEmpty && *field.value(&fields) == "":
			// the statement and the blanks after it go, and its whole line when it is the line's only content
			start := index.OffsetAt(index.PositionAt(span.Start).Line, 0)
			end := index.OffsetAt(index.PositionAt(span.End).Line+1, 0)
			span.End = len(src) - len(strings.TrimLeft(src[span.End:], " \t"))
			if strings.TrimSpace(src[start:span.Start]) == "" && strings.TrimSpace(src[span.End:end]) == "" {
				span = jomini.Range{Start: start, End: end}
			}
		case known:
			// a key with no value yet takes one where the operator ends
			span, text = jomini.Range{Start: span.End, End: span.End}, quoted(*field.value(&fields))
			if assign.Value != nil {
				span = assign.Value.NodeRange()
			}
		case key == "tags" && jomini.BlockOf(assign.Value) != nil:
			text = tagsBlock(fields.Tags)
		default:
			continue
		}
		seen[key] = true
		patched.WriteString(src[cursor:span.Start] + text)
		cursor = span.End
	}
	patched.WriteString(src[cursor:])
	// a key the file never had is appended only when it has something to say
	for _, field := range listingFields {
		if value := *field.value(&fields); field.mod != "" && !seen[field.mod] && value != "" {
			patched.WriteString("\n" + field.mod + "=" + quoted(value))
		}
	}
	if !seen["tags"] && len(fields.Tags) > 0 {
		patched.WriteString("\n" + tagsBlock(fields.Tags))
	}
	return strings.TrimRight(patched.String(), "\n") + "\n"
}

// quoted is a value as a descriptor.mod writes it: trimmed, its inner quotes as apostrophes, in quotes.
func quoted(text string) string {
	return `"` + strings.ReplaceAll(strings.TrimSpace(text), `"`, `'`) + `"`
}

// tagsBlock is a descriptor.mod's tags statement: each non-empty tag on its own tab-indented line, in quotes.
func tagsBlock(tags []string) string {
	block := "tags={"
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			block += "\n\t" + quoted(tag)
		}
	}
	return block + "\n}"
}

// patchMetadataJSON writes the listing fields into a metadata.json's content.
func patchMetadataJSON(content string, fields ListingFields) (string, error) {
	metadata := map[string]any{}
	if content != "" {
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			return "", err
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
	return string(out) + "\n", err
}

// writeSteamItem keeps a metadata.json mod's Workshop id in steamItemRel; no id, no file.
func writeSteamItem(root, id string) error {
	file := inventory.Join(root, steamItemRel)
	if id = strings.TrimSpace(id); id == "" {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return inventory.SaveText(file, inventory.Text{Content: id + "\n"})
}

// Thumbnail is the mod's listing image relative to its root: thumbnail.<ext> where its game or the mod keeps one,
// else the file the descriptor's picture names beside it or in .metadata.
func Thumbnail(gameID, root, picture string) (string, bool) {
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
		file := inventory.Join(root, rel)
		if _, inside := inventory.RelOf(root, file); !inside {
			continue
		}
		if _, err := os.Stat(file); err == nil {
			return rel, true
		}
	}
	return "", false
}

// thumbnailDirs is where a new listing image goes, and every folder a mod may keep one in, the game's own first:
// mods follow no one convention, and some keep one in both places.
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

// CopyThumbnail installs src as the mod's listing image and returns where it went relative to root: in place of the
// mod's current one (same folder, src's extension), else the game's folder.
func CopyThumbnail(gameID, root, src string) (string, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if !slices.Contains(ThumbnailExts, ext) {
		return "", fmt.Errorf("thumbnail must be one of %s", strings.Join(ThumbnailExts, ", "))
	}
	convention, _ := thumbnailDirs(gameID)
	rel := path.Join(convention, "thumbnail"+ext)
	existing, known := Thumbnail(gameID, root, "")
	if known {
		rel = strings.TrimSuffix(existing, path.Ext(existing)) + ext
	}
	if err := appdata.CopyFile(src, inventory.Join(root, rel)); err != nil {
		return "", err
	}
	// a new extension: the superseded file goes, so there are never two to choose between
	if known && rel != existing {
		_ = os.Remove(inventory.Join(root, existing))
	}
	return rel, nil
}

// DefaultModParent is the game's user data folder's mod/, or "" where the game has none.
func DefaultModParent(gameID string) string {
	userData := game.UserDataDir(gameID, "")
	if userData == "" {
		return ""
	}
	return filepath.Join(userData, "mod")
}

// Slug is the one slug of a mod called name: its folder, its metadata id and its localization stub; inventory.Slug of
// it, and "mod" for a name that leaves nothing.
func Slug(name string) string {
	return cmp.Or(inventory.Slug(name), "mod")
}

// Create creates root with a descriptor, each of the game's ModFolders empty but for a loc stub in the localization
// one's language folder, and the thumbnail when one is given.
func Create(opts NewModOpts) error {
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
	if info.ModDescriptor == game.MetadataDescriptor {
		fields.Version, fields.ID = "1.0.0", Slug(name)
	}
	if err := WriteDescriptor(opts.GameID, root, fields); err != nil {
		return err
	}
	language, locDir := cmp.Or(strings.TrimSpace(opts.LocLang), game.BaseLanguage), ""
	for _, folder := range info.ModFolders {
		dir := inventory.Join(root, folder)
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
	locName := Slug(name) + "_l_" + language + ".yml"
	return appdata.WriteBytes(filepath.Join(locDir, locName), loc.File(language, nil))
}
