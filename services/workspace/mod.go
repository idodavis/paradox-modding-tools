package workspace

import (
	"cmp"
	"encoding/json"
	"fmt"
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
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
)

// A Descriptor is a mod's own description, read from its game's descriptor file.
type Descriptor struct {
	Path             string   `json:"path"`
	ID               string   `json:"id"` // the id metadata.json gives the mod; "" means none
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`
	ReplacePaths     []string `json:"replacePaths"` // folders whose earlier files the game does not load
	Thumbnail        string   `json:"thumbnail"`    // the full path of the picture file the descriptor names
	WorkshopID       string   `json:"workshopId"`
	// Dependencies are the mods it needs. They are read and never written.
	Dependencies []Dependency `json:"dependencies"`
}

// A Dependency is one mod a descriptor says its mod needs.
type Dependency struct {
	ID    string `json:"id"`    // the mod's id or name, as the descriptor writes it
	Title string `json:"title"` // the title metadata.json gives it; "" where the descriptor gives none
}

// A Mod is one mod of a workspace, with its descriptor.
type Mod struct {
	app.Mod
	Descriptor Descriptor `json:"descriptor"`
	Broken     bool       `json:"broken"`    // its folder is missing
	Thumbnail  string     `json:"thumbnail"` // the full path of the picture shown for it; "" when it has none
}

// thumbnailName is the name, without its extension, of a mod's thumbnail file.
const thumbnailName = "thumbnail"

// scriptKeys are the keys of a script descriptor that a Descriptor models.
var scriptKeys = []string{
	"name", "version", "supported_version", "picture", "remote_file_id", "replace_path", "tags",
}

// releaseVersion matches a version that starts with a major and a minor number.
var releaseVersion = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.|$)`)

// ReadDescriptor reads the descriptor of the mod in a folder. Its thumbnail is a full path. It returns an error if
// the file cannot be read or is not a valid descriptor.
func ReadDescriptor(row *game.Game, dir string) (Descriptor, error) {
	file, _ := resolveDescriptor(row, dir)
	text, err := files.ReadText(file)
	if err != nil {
		return Descriptor{}, err
	}
	descriptor, err := ParseDescriptor(file, text.Text)
	if err != nil {
		return Descriptor{}, err
	}
	descriptor.Path = file
	if descriptor.Thumbnail != "" {
		descriptor.Thumbnail = files.Join(dir, path.Join(row.ThumbnailDir, descriptor.Thumbnail))
	}
	return descriptor, nil
}

// ParseDescriptor reads the text of a descriptor: as JSON where the file is a metadata.json, else as script. Its
// thumbnail is as written. It returns an error if the JSON cannot be read.
func ParseDescriptor(file, text string) (Descriptor, error) {
	if path.Ext(file) == ".json" {
		return decodeDescriptorMetadata(text)
	}
	return parseDescriptorScript(text), nil
}

// findThumbnail returns the full path of the picture shown for a mod. It is the mod's thumbnail file, else the
// picture its descriptor names. It returns "" if neither exists.
func findThumbnail(row *game.Game, dir, named string) string {
	folders := []string{row.ThumbnailDir, ""}
	if row.ThumbnailDir == "" {
		folders = []string{"", game.MetadataDir}
	}
	var candidates []string
	for _, folder := range folders {
		for _, extension := range game.PictureExtensions {
			candidates = append(candidates, files.Join(dir, path.Join(folder, thumbnailName+extension)))
		}
	}
	found, _ := lo.Find(append(candidates, named), func(file string) bool {
		_, err := os.Stat(file)
		return file != "" && err == nil
	})
	return found
}

// WriteDescriptor writes a mod's descriptor in its game's format, making the file if it is missing. Fields a
// Descriptor does not model keep their values. It returns an error if the thumbnail is outside the mod's folder.
func WriteDescriptor(row *game.Game, dir string, descriptor Descriptor) error {
	file, metadata := resolveDescriptor(row, dir)
	existing, err := files.LoadText(file)
	if err != nil {
		return err
	}
	existing.Encoding = cmp.Or(existing.Encoding, files.EncodingUTF8)
	if descriptor.Thumbnail != "" {
		rel, ok := files.ToRel(dir, descriptor.Thumbnail)
		if !ok {
			return fmt.Errorf("the thumbnail %s is outside the mod's folder", descriptor.Thumbnail)
		}
		descriptor.Thumbnail = strings.TrimPrefix(rel, path.Join(row.ThumbnailDir, "")+"/")
	}
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

// FindMod returns the mod that a dependency names, by its id or its name. It returns false if no mod matches.
func FindMod(mods []Mod, dependency Dependency) (Mod, bool) {
	return lo.Find(mods, func(mod Mod) bool {
		return mod.Descriptor.ID == dependency.ID || mod.Descriptor.Name == dependency.ID
	})
}

// ListDependencies returns the ids of the workspace's mods that a mod depends on, as its descriptor declares them, and
// theirs, all the way down, in the order they are found. A dependency the workspace lacks is left out, and so is the
// mod itself.
func (ws *Session) ListDependencies(modID string) []string {
	var dependencies []string
	for pending := []string{modID}; len(pending) > 0; {
		mod, _ := lo.Find(ws.Order, func(mod Mod) bool { return mod.ID == pending[0] })
		pending = pending[1:]
		for _, dependency := range mod.Descriptor.Dependencies {
			if other, ok := FindMod(ws.Order, dependency); ok && other.ID != modID && !slices.Contains(dependencies, other.ID) {
				dependencies = append(dependencies, other.ID)
				pending = append(pending, other.ID)
			}
		}
	}
	return dependencies
}

// DescribeMod returns a mod's title, or its folder's name if the descriptor gives none. It returns the game's name for
// the game, and the id itself for a mod the workspace lacks.
func (ws *Session) DescribeMod(modID string) string {
	mod, ok := lo.Find(ws.Order, func(mod Mod) bool { return mod.ID == modID })
	switch {
	case ok:
		return cmp.Or(mod.Descriptor.Name, filepath.Base(mod.Dir))
	case modID == game.OriginVanilla:
		return ws.Game.Name
	}
	return modID
}

// ResolveWorkshopID returns the Workshop id of the mod's record, else its descriptor's. It is "" if neither has one.
func (mod Mod) ResolveWorkshopID() string {
	return cmp.Or(mod.WorkshopID, mod.Descriptor.WorkshopID)
}

// createModFolder makes a mod's folder with its descriptor and the folders the game expects, and returns it. A game
// that lists mods by a launcher file gets one beside the folder. It returns an error if the folder exists already.
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
	// the launcher lists a mod by a .mod file beside its folder
	launcher := encodeDescriptorScript("", descriptor) + `path="` + filepath.ToSlash(folder) + "\"\n"
	err := files.WriteText(filepath.Join(dir, files.ToSlug(name)+".mod"), files.Text{Text: launcher, Encoding: files.EncodingUTF8})
	return folder, err
}

// FindLocalizationFile returns the localization file PMT makes for a mod with the given folder and name, in a
// language: the name's slug and the language's suffix, in the language's folder. With no language it uses the base
// language. It returns false for a game with no localization folder.
func FindLocalizationFile(row *game.Game, folder, name, language string) (string, bool) {
	language = cmp.Or(language, game.BaseLanguage)
	rel, ok := findLocalizationFolder(row)
	if !ok {
		return "", false
	}
	return files.Join(folder, path.Join(rel, language, files.ToSlug(name)+"_l_"+language+".yml")), true
}

// CreateLocalization writes a localization file of a mod with only its header, in the language its name ends in. It
// returns an error for a file outside the workspace's mods, or a name that ends in no language.
func (wsp *Service) CreateLocalization(path string) error {
	if _, err := wsp.findWritable(path); err != nil {
		return err
	}
	return createLocalization(path)
}

// createLocalization writes a localization file with only its header, in the language its name ends in, with a byte
// order mark. It returns an error for a name that ends in no language.
func createLocalization(path string) error {
	language := loc.FindSuffixLanguage(path)
	if language == "" {
		return fmt.Errorf("%s does not end in a language, such as _l_%s.yml", filepath.Base(path), game.BaseLanguage)
	}
	return files.WriteText(path, files.Text{Text: loc.Encode(language, nil), Encoding: files.EncodingUTF8BOM})
}

// findLocalizationFolder returns the folder a game keeps localization in. It returns false if the game has none.
func findLocalizationFolder(row *game.Game) (string, bool) {
	return lo.Find(row.ModFolders, func(rel string) bool { return path.Base(rel) == game.Localization })
}

// toSupportedVersion turns a game version into the release a mod supports, such as 1.20.* for 1.20.1.2. It returns ""
// if the version does not start with two numbers.
func toSupportedVersion(version string) string {
	match := releaseVersion.FindStringSubmatch(version)
	if match == nil {
		return ""
	}
	return match[1] + "." + match[2] + ".*"
}

// MatchVersion reports whether a mod made for a supported version runs on a game version. It matches them part by
// part, as EU5's and Vic3's logs show the games doing, and a * matches the rest. Its second result is false where no
// source shows how the games match: a supported version that is empty or has a * inside a part, such as 1.4*, or a
// game version that is not numbers between dots, such as a build id.
func MatchVersion(supported, version string) (matches, judged bool) {
	wanted, actual := strings.Split(supported, "."), strings.Split(version, ".")
	judged = supported != "" && lo.EveryBy(wanted, func(part string) bool { return part == "*" || jomini.IsNumber(part) }) &&
		lo.EveryBy(actual, jomini.IsNumber)
	for position, part := range wanted {
		switch {
		case part == "*":
			return true, judged
		case position >= len(actual) || part != actual[position]:
			return false, judged
		}
	}
	return len(wanted) == len(actual), judged
}

// resolveModFolder returns the folder for a mod inside a folder of mods, named after the mod.
func resolveModFolder(dir, name string) string { return filepath.Join(dir, files.ToSlug(name)) }

// resolveDescriptor returns the path of a mod's descriptor and whether it is metadata.json rather than script.
func resolveDescriptor(row *game.Game, dir string) (file string, metadata bool) {
	return files.Join(dir, row.ModDescriptor), path.Ext(row.ModDescriptor) == ".json"
}

// parseDescriptorScript parses a descriptor written as script.
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
			for _, dependency := range listBlockValues(tree, statement) {
				descriptor.Dependencies = append(descriptor.Dependencies, Dependency{ID: dependency})
			}
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

// decodeDescriptorMetadata decodes a descriptor written as metadata.json. A dependency on anything but a mod, such as
// a DLC, is left out. It returns an error if the text is not JSON.
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
			Title    string `json:"display_name"`
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
			descriptor.Dependencies = append(descriptor.Dependencies, Dependency{ID: relationship.ID, Title: relationship.Title})
		}
	}
	return descriptor, nil
}

// encodeDescriptorScript returns a descriptor's text as script. Statements of the existing text that a Descriptor
// does not model follow unchanged.
func encodeDescriptorScript(existing string, descriptor Descriptor) string {
	var text strings.Builder
	write := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&text, "%s=\"%s\"\n", key, value)
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
			fmt.Fprintf(&text, "\t\"%s\"\n", tag)
		}
		text.WriteString("}\n")
	}
	tree := jomini.Parse(existing)
	for statement := range tree.Children(0) {
		current := tree.Statements[statement]
		if !slices.Contains(scriptKeys, tree.Text(current.Key)) {
			fmt.Fprintln(&text, tree.Text(current.Span))
		}
	}
	return text.String()
}

// encodeDescriptorMetadata returns a descriptor's text as metadata.json. Keys of the existing text that a Descriptor
// does not model stay. It returns an error if the existing text is not JSON.
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
