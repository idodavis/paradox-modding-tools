// mod.go adds, makes, orders and removes a workspace's mods, and reads and writes their descriptors.

package workspace

import (
	"cmp"
	"crypto/rand"
	"encoding/json"
	"errors"
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
	ID               string   `json:"id"` // the id metadata.json gives the mod; "" if none
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`
	ReplacePaths     []string `json:"replacePaths"` // folders whose earlier files the game does not load
	Thumbnail        string   `json:"thumbnail"`    // the full path of the picture file the descriptor names
	WorkshopID       string   `json:"workshopId"`
	// Dependencies are the mods it needs. They are never written.
	Dependencies []Dependency `json:"dependencies"`
}

// A Dependency is one mod a descriptor says its mod needs.
type Dependency struct {
	ID    string `json:"id"`    // the mod's id or name, as the descriptor writes it
	Title string `json:"title"` // the title metadata.json gives it; "" if none
}

// A Mod is one mod of a workspace, with its descriptor.
type Mod struct {
	app.Mod
	Descriptor Descriptor `json:"descriptor"`
	Broken     bool       `json:"broken"`    // its folder is missing
	Thumbnail  string     `json:"thumbnail"` // the full path of the picture shown for it; "" if none
	Workshop   bool       `json:"workshop"`  // its folder is Steam's copy of a Workshop mod
}

// thumbnailName is the name, without its extension, of a mod's thumbnail file.
const thumbnailName = "thumbnail"

// slotCount is how many color slots there are for mods.
const slotCount = 16

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

// ParseDescriptor parses a descriptor's text as JSON if the given file name is a .json file, else as script. It returns
// an error if the JSON cannot be parsed.
func ParseDescriptor(file, text string) (Descriptor, error) {
	if isMetadata(file) {
		return decodeDescriptorMetadata(text)
	}
	return parseDescriptorScript(text), nil
}

// findThumbnail returns the full path of the picture shown for a mod: its thumbnail file, else the given picture its
// descriptor names. It returns "" if neither exists.
func findThumbnail(row *game.Game, dir, picture string) string {
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
	thumbnail, _ := lo.Find(append(candidates, picture), func(file string) bool {
		_, err := os.Stat(file)
		return file != "" && err == nil
	})
	return thumbnail
}

// WriteDescriptor writes a mod's descriptor in its game's format, making the file if it is missing. Fields a
// Descriptor does not model keep their values. It returns an error if the thumbnail is outside the mod's folder.
func WriteDescriptor(row *game.Game, dir string, descriptor Descriptor) error {
	file, _ := resolveDescriptor(row, dir)
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
	if existing.Text, err = EncodeDescriptor(file, existing.Text, descriptor); err != nil {
		return err
	}
	return files.WriteText(file, existing)
}

// EncodeDescriptor returns a descriptor's text as JSON if the given file name is a .json file, else as script. Fields
// a Descriptor does not model keep their values in the existing text. It returns an error if that JSON cannot be
// parsed.
func EncodeDescriptor(file, existing string, descriptor Descriptor) (string, error) {
	if isMetadata(file) {
		return encodeDescriptorMetadata(existing, descriptor)
	}
	return encodeDescriptorScript(existing, descriptor), nil
}

// WriteDescriptor writes a mod's descriptor and refreshes the session's copy of it in the same call. It returns an
// error if the session holds no such mod or the file cannot be written.
func (ws *Session) WriteDescriptor(modID string, descriptor Descriptor) error {
	mod, ok := lo.Find(*ws.Order.Load(), func(mod Mod) bool { return mod.ID == modID })
	if !ok {
		return fmt.Errorf("the mod %s is not in the open workspace", modID)
	}
	if err := WriteDescriptor(ws.Game, mod.Dir, descriptor); err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.refreshMod(modID)
	return nil
}

// FindMod returns the mod that a dependency names, by its id or its name. It returns false if no mod matches.
func FindMod(mods []Mod, dependency Dependency) (Mod, bool) {
	return lo.Find(mods, func(mod Mod) bool {
		return mod.Descriptor.ID == dependency.ID || mod.Descriptor.Name == dependency.ID
	})
}

// ListDependencies returns the ids of the workspace's mods that a mod depends on, directly or through other
// dependencies. A dependency the workspace lacks is left out.
func (ws *Session) ListDependencies(modID string) []string {
	order := *ws.Order.Load()
	var dependencies []string
	for pending := []string{modID}; len(pending) > 0; {
		mod, _ := lo.Find(order, func(mod Mod) bool { return mod.ID == pending[0] })
		pending = pending[1:]
		for _, dependency := range mod.Descriptor.Dependencies {
			if other, ok := FindMod(order, dependency); ok && other.ID != modID && !slices.Contains(dependencies, other.ID) {
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
	mod, ok := lo.Find(*ws.Order.Load(), func(mod Mod) bool { return mod.ID == modID })
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

// AddMod adds a mod folder to a workspace and gives the mod a color slot. It returns an error if the workspace is not
// recorded, the folder cannot be a mod of its game (see CheckModFolder), or the workspace has the mod already.
func (wsp *Service) AddMod(workspaceID, dir string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	if err = checkModFolder(row, install, dir); err != nil {
		return Mod{}, err
	}
	record := app.Mod{ID: rand.Text(), Dir: dir}
	err = wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		if lo.ContainsBy(workspace.Mods, func(mod app.Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(dir) }) {
			return fmt.Errorf("%s is a mod of the workspace already", dir)
		}
		record.Color = resolveSlot(workspace.Mods)
		workspace.Mods = append(workspace.Mods, record)
		return nil
	})
	if err != nil {
		return Mod{}, err
	}
	return readMod(row, record), nil
}

// ListMods returns the open workspace's mods in load order, each with its descriptor as the session holds it and
// its record as config.json holds it now. It is empty while no workspace is open.
//
//wails:ignore
func (wsp *Service) ListMods() []Mod {
	ws, err := wsp.Session()
	if err != nil {
		return nil
	}
	record, _, _, _ := findRecords(wsp.Config.Config(), ws.ID)
	return lo.Map(*ws.Order.Load(), func(mod Mod, _ int) Mod {
		if position := slices.IndexFunc(record.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			mod.Mod = record.Mods[position]
		}
		return mod
	})
}

// resolveSlot returns the first color slot that no mod has. If all are taken, it returns the slot of the next place
// in the load order.
func resolveSlot(mods []app.Mod) string {
	for position := range slotCount {
		slot := makeSlot(position)
		if !lo.ContainsBy(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}
	return makeSlot(len(mods))
}

// makeSlot returns the color slot of a place in the load order, wrapping around after the last slot.
func makeSlot(position int) string {
	return fmt.Sprintf("o%d", position%slotCount+1)
}

// CreateMod makes a mod with a localization file and adds it to a workspace. It returns an error if the workspace is
// not recorded, the name has no letter or digit to make a folder name from, or the folder exists.
func (wsp *Service) CreateMod(workspaceID, dir, name, language string) (Mod, error) {
	_, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}
	folder, err := createModFolder(row, resolveModDir(row, install, dir), name, resolveVersion(row, install))
	if err != nil {
		return Mod{}, err
	}
	if file, ok := FindLocalizationFile(row, folder, name, language); ok {
		if err := createLocalization(file); err != nil {
			return Mod{}, err
		}
	}
	return wsp.AddMod(workspaceID, folder)
}

// ResolveModFolder returns the folder CreateMod would make for a mod of the given install. With no name, it returns
// the folder the mod goes in. It returns an error if the install is not recorded, the name has no letter or digit to
// make a folder name from, or the folder exists already.
func (wsp *Service) ResolveModFolder(installID, dir, name string) (string, error) {
	install, row, err := findInstall(wsp.Config.Config(), installID)
	if err != nil {
		return "", err
	}
	parent := resolveModDir(row, install, dir)
	if name == "" {
		return parent, nil
	}
	return resolveModFolder(parent, name)
}

// CheckModFolder returns an error if a folder cannot be added as a mod of the given install's game, or if the install
// is not recorded.
func (wsp *Service) CheckModFolder(installID, dir string) error {
	install, row, err := findInstall(wsp.Config.Config(), installID)
	if err != nil {
		return err
	}
	return checkModFolder(row, install, dir)
}

// resolveModDir returns the folder a mod is made in. With no folder, it is the game's mod folder.
func resolveModDir(row *game.Game, install app.Install, dir string) string {
	return cmp.Or(dir, filepath.Join(row.FindUserDataDir(install.Path), game.LauncherModFolder))
}

// RemoveMod takes a mod out of a workspace. Its folder stays.
func (wsp *Service) RemoveMod(workspaceID, modID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.Mods = slices.DeleteFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })
		return nil
	})
}

// ReorderMods sets a workspace's load order. It returns an error unless the order names each of its mods once.
func (wsp *Service) ReorderMods(workspaceID string, modIDs []string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		ordered, err := reorderMods(workspace.Mods, modIDs, func(mod app.Mod) string { return mod.ID })
		if err == nil {
			workspace.Mods = ordered
		}
		return err
	})
}

// reorderMods returns mods in the order of a list of ids. It returns an error unless the list names each mod once.
func reorderMods[T any](mods []T, modIDs []string, id func(mod T) string) ([]T, error) {
	remaining := slices.Clone(mods)
	var ordered []T
	for _, modID := range modIDs {
		position := slices.IndexFunc(remaining, func(mod T) bool { return id(mod) == modID })
		if position < 0 {
			return nil, fmt.Errorf("the mod %s is not in the workspace or is named twice", modID)
		}
		ordered = append(ordered, remaining[position])
		remaining = slices.Delete(remaining, position, position+1)
	}
	if len(remaining) > 0 {
		return nil, fmt.Errorf("the order leaves out %d of the workspace's mods", len(remaining))
	}
	return ordered, nil
}

// ResetColors gives a workspace's mods the color slots of their places in the load order and clears the game's color.
func (wsp *Service) ResetColors(workspaceID string) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		workspace.GameColor = ""
		for position := range workspace.Mods {
			workspace.Mods[position].Color = makeSlot(position)
		}
		return nil
	})
}

// UpdateMod replaces a mod's record in a workspace. A mod the workspace does not have is ignored.
func (wsp *Service) UpdateMod(workspaceID string, mod app.Mod) error {
	return wsp.updateWorkspace(workspaceID, func(workspace *app.Workspace) error {
		if position := slices.IndexFunc(workspace.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			workspace.Mods[position] = mod
		}
		return nil
	})
}

// createModFolder makes a mod's folder with its descriptor and the folders the game expects, and returns it. It returns
// an error if the name makes no folder name or the folder exists already.
func createModFolder(row *game.Game, dir, name, version string) (string, error) {
	folder, err := resolveModFolder(dir, name)
	if err != nil {
		return "", err
	}
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
	// a game with a script descriptor lists its mods by a .mod file beside the folder
	launcher := encodeDescriptorScript("", descriptor) + `path="` + filepath.ToSlash(folder) + "\"\n"
	err = files.WriteText(filepath.Join(dir, files.ToSlug(name)+".mod"), files.Text{Text: launcher, Encoding: files.EncodingUTF8})
	return folder, err
}

// FindLocalizationFile returns the path of a mod's localization file in the given language, which is the base language
// if empty. It returns false for a game with no localization folder.
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
	if _, err := wsp.FindWritable(path); err != nil {
		return err
	}
	return createLocalization(path)
}

// createLocalization writes a localization file with only its header, in the language its name ends in, with a byte
// order mark. It returns an error for a name that ends in no language.
func createLocalization(file string) error {
	language := loc.FindSuffixLanguage(file)
	if language == "" {
		return fmt.Errorf("%s does not end in a language, such as _l_%s.yml", filepath.Base(file), game.BaseLanguage)
	}
	return files.WriteText(file, files.Text{Text: loc.Encode(language, nil), Encoding: files.EncodingUTF8BOM})
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
// source shows how the games match, such as a supported version with a * inside a part, or a build id.
func MatchVersion(supported, version string) (matches, judged bool) {
	wanted, actual := strings.Split(supported, "."), strings.Split(version, ".")
	isWanted := func(part string) bool { return part == "*" || jomini.IsNumber(part) }
	judged = supported != "" && lo.EveryBy(wanted, isWanted) && lo.EveryBy(actual, jomini.IsNumber)
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

// resolveModFolder returns the folder for a mod inside a folder of mods, named after the mod. It returns an error for
// a name with no letter or digit, and for a folder that exists already.
func resolveModFolder(dir, name string) (string, error) {
	slug := files.ToSlug(name)
	if slug == "" {
		return "", errors.New("use a letter A to Z or a digit in the name; the mod's folder is named from it")
	}
	folder := filepath.Join(dir, slug)
	if _, err := os.Stat(folder); err == nil {
		return "", fmt.Errorf("%s exists already; pick another name", folder)
	}
	return folder, nil
}

// checkModFolder returns an error if a folder is no folder, is the game's own folder or inside it, or lacks the game's
// descriptor.
func checkModFolder(row *game.Game, install app.Install, dir string) error {
	switch {
	case !files.IsFolder(dir):
		return fmt.Errorf("%s is not a folder", dir)
	case files.ToKey(dir) == files.ToKey(install.Path) || files.IsUnder(install.Path, dir):
		return fmt.Errorf("%s is the game's own folder; a mod is a folder of its own, such as one in %s",
			dir, resolveModDir(row, install, ""))
	case !files.IsFile(files.Join(dir, row.ModDescriptor)):
		return fmt.Errorf("%s has no %s, so it is not a mod's folder", dir, row.ModDescriptor)
	}
	return nil
}

// resolveDescriptor returns the path of a mod's descriptor and whether it is metadata.json rather than script.
func resolveDescriptor(row *game.Game, dir string) (file string, metadata bool) {
	return files.Join(dir, row.ModDescriptor), isMetadata(row.ModDescriptor)
}

// isMetadata reports whether a descriptor's file name is a .json file, which holds it as JSON rather than script.
func isMetadata(file string) bool {
	return path.Ext(file) == ".json"
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
