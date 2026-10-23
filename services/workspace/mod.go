package workspace

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/app"
)

// A Descriptor is a mod's own description as its game writes it: CK3's `.mod` file, or Vic3's and EU5's
// `.metadata/metadata.json`.
type Descriptor struct {
	Path             string   `json:"path"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`
	ReplacePaths     []string `json:"replacePaths"` // folders whose earlier files the game does not load
	Thumbnail        string   `json:"thumbnail"`    // the picture file the descriptor names, from the mod's folder
	WorkshopID       string   `json:"workshopId"`
}

// A Mod is one mod of the open workspace: its record in the config, together with its descriptor as read from its
// folder.
type Mod struct {
	app.Mod
	Descriptor Descriptor `json:"descriptor"`
	Broken     bool       `json:"broken"` // its folder is missing
}

// scriptKeys are the keys of a script descriptor that a Descriptor models.
var scriptKeys = []string{
	"name", "version", "supported_version", "picture", "remote_file_id", "replace_path", "tags",
}

// releaseVersion matches a game version that starts with a major and a minor release number.
var releaseVersion = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.|$)`)

// ReadDescriptor reads a mod's descriptor from its folder, where the game's rule says it is.
func ReadDescriptor(game *game.Game, dir string) (Descriptor, error) {
	file, metadata := resolveDescriptor(game, dir)
	text, err := files.ReadText(file)
	if err != nil {
		return Descriptor{}, err
	}
	var descriptor Descriptor
	if metadata {
		descriptor, err = decodeDescriptorMetadata(text.Text)
	} else {
		descriptor = parseDescriptorScript(text.Text)
	}
	if err != nil {
		return Descriptor{}, err
	}
	descriptor.Path = file
	if descriptor.Thumbnail != "" {
		descriptor.Thumbnail = path.Join(game.ThumbnailDir, descriptor.Thumbnail)
	}
	return descriptor, nil
}

// WriteDescriptor writes a mod's descriptor back in the game's own form, keeping any fields it does not model.
func WriteDescriptor(game *game.Game, dir string, descriptor Descriptor) error {
	file, metadata := resolveDescriptor(game, dir)
	existing, err := files.ReadText(file)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	existing.Encoding = cmp.Or(existing.Encoding, files.EncodingUTF8)
	descriptor.Thumbnail = strings.TrimPrefix(descriptor.Thumbnail, path.Join(game.ThumbnailDir, "")+"/")
	if metadata {
		existing.Text, err = encodeDescriptorMetadata(existing.Text, descriptor)
	} else {
		existing.Text = encodeDescriptorScript(existing.Text, descriptor)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return files.WriteText(file, existing)
}

// createModFolder creates a new mod on disk: its folder, its descriptor and the folders the game expects. The
// descriptor names the mod, gives it version 1.0, and supports the given game version's major and minor release
// (1.20.* for 1.20.1.2); a game version that is not a release number, such as a Steam build, supports none.
func createModFolder(game *game.Game, dir, name, version string) (Descriptor, error) {
	folder := resolveModFolder(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Descriptor{}, err
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		return Descriptor{}, err
	}
	for _, rel := range game.ModFolders {
		if err := os.MkdirAll(files.Join(folder, rel), 0o755); err != nil {
			return Descriptor{}, err
		}
	}
	descriptor := Descriptor{Name: name, Version: "1.0", SupportedVersion: toSupportedVersion(version)}
	if err := WriteDescriptor(game, folder, descriptor); err != nil {
		return Descriptor{}, err
	}
	file, metadata := resolveDescriptor(game, folder)
	descriptor.Path = file
	if metadata {
		return descriptor, nil
	}
	// the launcher lists a mod by a .mod file beside its folder, with the descriptor's keys and the folder's path
	launcher := encodeDescriptorScript("", descriptor) + "path=" + toQuoted(filepath.ToSlash(folder)) + "\n"
	err := files.WriteText(filepath.Join(dir, files.ToSlug(name)+".mod"), files.Text{Text: launcher, Encoding: files.EncodingUTF8})
	return descriptor, err
}

// toSupportedVersion returns the release a mod supports for a game version: its major and minor numbers followed by
// a star, such as 1.20.* for 1.20.1.2. It returns "" for a version that does not start with two numbers, such as a
// Steam build.
func toSupportedVersion(version string) string {
	match := releaseVersion.FindStringSubmatch(version)
	if match == nil {
		return ""
	}
	return match[1] + "." + match[2] + ".*"
}

// resolveModFolder returns the folder of the mod with the given name in the folder that holds the game's mods.
func resolveModFolder(dir, name string) string { return filepath.Join(dir, files.ToSlug(name)) }

// resolveDescriptor returns the path of a mod's descriptor, and whether the game writes it as metadata.json rather
// than as script.
func resolveDescriptor(row *game.Game, dir string) (file string, metadata bool) {
	return game.BuildDescriptorPath(row.ID, dir), row.ModDescriptor == game.MetadataDescriptor
}

// parseDescriptorScript reads the descriptor of a game that writes it as script.
func parseDescriptorScript(text string) Descriptor {
	tree := jomini.Parse(text)
	var descriptor Descriptor
	for statement := range tree.Children(0) {
		current := tree.Statements[statement]
		value := tree.Unquoted(current.Value)
		switch tree.Text(current.Key) {
		case "name":
			descriptor.Name = value
		case "version":
			descriptor.Version = value
		case "supported_version":
			descriptor.SupportedVersion = value
		case "picture":
			descriptor.Thumbnail = value
		case "remote_file_id":
			descriptor.WorkshopID = value
		case "replace_path":
			descriptor.ReplacePaths = append(descriptor.ReplacePaths, value)
		case "tags":
			for tag := range tree.Children(statement) {
				descriptor.Tags = append(descriptor.Tags, tree.Unquoted(tree.Statements[tag].Value))
			}
		}
	}
	return descriptor
}

// decodeDescriptorMetadata reads the descriptor of a game that writes it as metadata.json. The file has no Workshop
// item, so that field stays empty.
func decodeDescriptorMetadata(text string) (Descriptor, error) {
	var metadata struct {
		Name             string   `json:"name"`
		Version          string   `json:"version"`
		SupportedVersion string   `json:"supported_game_version"`
		Tags             []string `json:"tags"`
		Picture          string   `json:"picture"`
		GameCustomData   struct {
			ReplacePaths []string `json:"replace_paths"`
		} `json:"game_custom_data"`
	}
	if err := json.Unmarshal([]byte(text), &metadata); err != nil {
		return Descriptor{}, err
	}
	return Descriptor{
		Name: metadata.Name, Version: metadata.Version, SupportedVersion: metadata.SupportedVersion,
		Tags: metadata.Tags, ReplacePaths: metadata.GameCustomData.ReplacePaths, Thumbnail: metadata.Picture,
	}, nil
}

// encodeDescriptorScript returns the text of a descriptor written as script: the fields the descriptor models, then
// every other statement of the existing text as it was written.
func encodeDescriptorScript(existing string, descriptor Descriptor) string {
	var text strings.Builder
	write := func(key, value string) {
		if value != "" {
			text.WriteString(key + "=" + toQuoted(value) + "\n")
		}
	}
	write("name", descriptor.Name)
	write("version", descriptor.Version)
	write("supported_version", descriptor.SupportedVersion)
	write("picture", descriptor.Thumbnail)
	write("remote_file_id", descriptor.WorkshopID)
	for _, replaced := range descriptor.ReplacePaths {
		write("replace_path", replaced)
	}
	if len(descriptor.Tags) > 0 {
		text.WriteString("tags={\n")
		for _, tag := range descriptor.Tags {
			text.WriteString("\t" + toQuoted(tag) + "\n")
		}
		text.WriteString("}\n")
	}
	tree := jomini.Parse(existing)
	for statement := range tree.Children(0) {
		current := tree.Statements[statement]
		if !slices.Contains(scriptKeys, tree.Text(current.Key)) {
			text.WriteString(tree.Text(current.Span) + "\n")
		}
	}
	return text.String()
}

// toQuoted returns a value in quotes.
func toQuoted(value string) string { return `"` + value + `"` }

// encodeDescriptorMetadata returns the text of a descriptor written as metadata.json: the existing text with the
// fields the descriptor models set, and every other key as it was. It returns an error if the existing text is not
// JSON.
func encodeDescriptorMetadata(existing string, descriptor Descriptor) (string, error) {
	fields := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		decoder := json.NewDecoder(strings.NewReader(existing))
		decoder.UseNumber()
		if err := decoder.Decode(&fields); err != nil {
			return "", err
		}
	}
	custom, _ := fields["game_custom_data"].(map[string]any)
	if custom == nil {
		custom = map[string]any{}
	}
	custom["replace_paths"] = lo.Ternary(descriptor.ReplacePaths == nil, []string{}, descriptor.ReplacePaths)
	fields["game_custom_data"] = custom
	fields["name"] = descriptor.Name
	fields["version"] = descriptor.Version
	fields["supported_game_version"] = descriptor.SupportedVersion
	fields["tags"] = lo.Ternary(descriptor.Tags == nil, []string{}, descriptor.Tags)
	if descriptor.Thumbnail != "" {
		fields["picture"] = descriptor.Thumbnail
	} else {
		delete(fields, "picture")
	}
	var text strings.Builder
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "\t")
	err := encoder.Encode(fields)
	return text.String(), err
}
