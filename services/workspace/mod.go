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
	"strconv"
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
	ID               string   `json:"id"` // the id metadata.json gives the mod; "" means none
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`
	ReplacePaths     []string `json:"replacePaths"` // folders whose earlier files the game does not load
	Thumbnail        string   `json:"thumbnail"`    // the picture file the descriptor names, from the mod's folder
	WorkshopID       string   `json:"workshopId"`
	// Dependencies are the mods it needs: their ids in metadata.json, their names in a script descriptor. They are read
	// and never written.
	Dependencies []string `json:"dependencies"`
}

// A Mod is one mod of the open workspace: the mod as the config records it, with its descriptor.
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

// ReadDescriptor reads the descriptor of the mod in the given folder, from the file the given game keeps it in. It
// returns an error if the file cannot be read or is not a valid descriptor.
func ReadDescriptor(row *game.Game, dir string) (Descriptor, error) {
	file, metadata := resolveDescriptor(row, dir)
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
		descriptor.Thumbnail = path.Join(row.ThumbnailDir, descriptor.Thumbnail)
	}
	return descriptor, nil
}

// WriteDescriptor writes the given descriptor for the mod in the given folder, in the format the given game uses. It
// keeps the fields of the existing file that a Descriptor does not model, and makes the file if it is missing.
func WriteDescriptor(row *game.Game, dir string, descriptor Descriptor) error {
	file, metadata := resolveDescriptor(row, dir)
	existing, err := files.ReadText(file)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	existing.Encoding = cmp.Or(existing.Encoding, files.EncodingUTF8)
	descriptor.Thumbnail = strings.TrimPrefix(descriptor.Thumbnail, path.Join(row.ThumbnailDir, "")+"/")
	if metadata {
		existing.Text, err = encodeDescriptorMetadata(existing.Text, descriptor)
	} else {
		existing.Text = encodeDescriptorScript(existing.Text, descriptor)
	}
	if err != nil {
		return err
	}
	return files.WriteText(file, existing)
}

// createModFolder makes a new mod in the given folder, with its descriptor and the folders the game expects. It returns
// the mod's folder. The descriptor names the mod, gives it version 1.0 and supports the release of the given game
// version. A game that lists mods by a launcher file gets that file beside the mod's folder. It returns an error if the
// mod's folder exists already.
func createModFolder(row *game.Game, dir, name, version string) (string, error) {
	folder := resolveModFolder(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		return "", err
	}
	for _, rel := range row.ModFolders {
		if err := os.MkdirAll(files.Join(folder, rel), 0o755); err != nil {
			return "", err
		}
	}
	descriptor := Descriptor{Name: name, Version: "1.0", SupportedVersion: toSupportedVersion(version)}
	if err := WriteDescriptor(row, folder, descriptor); err != nil {
		return "", err
	}
	if _, metadata := resolveDescriptor(row, folder); metadata {
		return folder, nil
	}
	// the launcher lists a mod by a .mod file beside its folder, with the descriptor's keys and the folder's path
	launcher := encodeDescriptorScript("", descriptor) + "path=" + toQuoted(filepath.ToSlash(folder)) + "\n"
	err := files.WriteText(filepath.Join(dir, files.ToSlug(name)+".mod"), files.Text{Text: launcher, Encoding: files.EncodingUTF8})
	return folder, err
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

// IsOlderRelease reports whether the release a mod supports is older than the release of a game version. Only the major
// and minor numbers count, so 1.19.* is older than 1.20.1.2. It is false if either does not start with two numbers.
func IsOlderRelease(supported, version string) bool {
	older, hasOlder := toRelease(supported)
	newer, hasNewer := toRelease(version)
	return hasOlder && hasNewer && slices.Compare(older[:], newer[:]) < 0
}

// toRelease returns the major and minor numbers a version starts with. It returns false if it does not start with
// two numbers.
func toRelease(version string) ([2]int, bool) {
	match := releaseVersion.FindStringSubmatch(version)
	if match == nil {
		return [2]int{}, false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	return [2]int{major, minor}, true
}

// resolveModFolder returns the folder for a new mod with the given name, inside the given folder of the game's mods.
func resolveModFolder(dir, name string) string { return filepath.Join(dir, files.ToSlug(name)) }

// resolveDescriptor returns the path of a mod's descriptor, and whether the game writes it as metadata.json rather
// than as script.
func resolveDescriptor(row *game.Game, dir string) (file string, metadata bool) {
	return files.Join(dir, row.ModDescriptor), path.Ext(row.ModDescriptor) == ".json"
}

// parseDescriptorScript returns the descriptor that a script text writes, for a game that writes it as script.
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
			descriptor.Tags = append(descriptor.Tags, listBlockValues(tree, statement)...)
		case "dependencies":
			descriptor.Dependencies = append(descriptor.Dependencies, listBlockValues(tree, statement)...)
		}
	}
	return descriptor
}

// listBlockValues returns the values written in the block of a statement, without their quotes.
func listBlockValues(tree *jomini.Tree, statement int32) []string {
	var values []string
	for child := range tree.Children(statement) {
		values = append(values, tree.Unquoted(tree.Statements[child].Value))
	}
	return values
}

// decodeDescriptorMetadata returns the descriptor that a metadata.json text writes. Its dependencies are the mods the
// file lists as dependencies; a dependency on anything else, such as a DLC, is left out. It returns an error if the
// text is not JSON.
func decodeDescriptorMetadata(text string) (Descriptor, error) {
	var metadata struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		Version          string   `json:"version"`
		SupportedVersion string   `json:"supported_game_version"`
		Tags             []string `json:"tags"`
		Picture          string   `json:"picture"`
		Relationships    []struct {
			Kind     string `json:"rel_type"`
			ID       string `json:"id"`
			Resource string `json:"resource_type"`
		} `json:"relationships"`
		GameCustomData struct {
			ReplacePaths []string `json:"replace_paths"`
		} `json:"game_custom_data"`
	}
	if err := json.Unmarshal([]byte(text), &metadata); err != nil {
		return Descriptor{}, err
	}
	descriptor := Descriptor{
		ID: metadata.ID, Name: metadata.Name, Version: metadata.Version, SupportedVersion: metadata.SupportedVersion,
		Tags: metadata.Tags, ReplacePaths: metadata.GameCustomData.ReplacePaths, Thumbnail: metadata.Picture,
	}
	for _, relationship := range metadata.Relationships {
		if relationship.Kind == "dependency" && relationship.Resource == "mod" {
			descriptor.Dependencies = append(descriptor.Dependencies, relationship.ID)
		}
	}
	return descriptor, nil
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
