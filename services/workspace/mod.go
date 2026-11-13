// mod.go manages a workspace's mods and their descriptors.

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

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
)

// Descriptor is a mod's own description, read from its game's descriptor
// file.
type Descriptor struct {
	Path string `json:"path"`

	// ID is the id metadata.json gives the mod, "" if none.
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`

	// ReplacePaths are the folders whose earlier files the game does not load.
	ReplacePaths []string `json:"replacePaths"`

	Thumbnail    string       `json:"thumbnail"`
	WorkshopID   string       `json:"workshopId"`
	Dependencies []Dependency `json:"dependencies"`
}

// Dependency is one mod a descriptor says its mod needs.
type Dependency struct {
	// ID is the mod's id or name, as the descriptor writes it.
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Mod is one mod of a workspace, with its descriptor.
type Mod struct {
	app.Mod
	Descriptor Descriptor `json:"descriptor"`

	// Broken is true if the mod's folder is missing.
	Broken bool `json:"broken"`

	// Thumbnail is the full path of the picture shown for the mod, "" if none.
	Thumbnail string `json:"thumbnail"`

	// Workshop is true for Steam's copy of a Workshop mod, which is read only.
	Workshop bool `json:"workshop"`
}

// ModFolder is the folder a new mod would be made in, or why it cannot be
// made.
type ModFolder struct {
	// Path and Problem are exclusive: one is "" when the other is set.
	Path    string `json:"path"`
	Problem string `json:"problem"`
}

// ReadDescriptor reads the descriptor of the mod in a folder.
func ReadDescriptor(row *game.Game, dir string) (Descriptor, error) {
	file := resolveDescriptor(row, dir)
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

// ParseDescriptor parses a descriptor's text, as JSON for a .json file name.
func ParseDescriptor(file, text string) (Descriptor, error) {
	if isMetadata(file) {
		return decodeDescriptorMetadata(text)
	}

	return parseDescriptorScript(text), nil
}

const thumbnailName = "thumbnail"

// findThumbnail returns the picture shown for a mod and sent with its upload:
// the one its descriptor names, else its thumbnail file, else "". A picture
// that is not there is passed over.
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

	for _, file := range append([]string{picture}, candidates...) {
		if _, err := os.Stat(file); file != "" && err == nil {
			return file
		}
	}

	return ""
}

// WriteDescriptor writes a mod's descriptor in its game's format, making the
// file if it is missing. Fields a Descriptor does not model keep their values.
func WriteDescriptor(row *game.Game, dir string, descriptor Descriptor) error {
	file := resolveDescriptor(row, dir)
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

// EncodeDescriptor returns a descriptor's text, as JSON for a .json file name.
// Fields a Descriptor does not model keep their values in the existing text.
func EncodeDescriptor(file, existing string, descriptor Descriptor) (string, error) {
	if isMetadata(file) {
		return encodeDescriptorMetadata(existing, descriptor)
	}

	return encodeDescriptorScript(existing, descriptor), nil
}

// WriteDescriptor writes a mod's descriptor and refreshes the session's copy.
func (s *Session) WriteDescriptor(modID string, descriptor Descriptor) error {
	mod, ok := findMod(*s.Order.Load(), modID)
	if !ok {
		return fmt.Errorf("the mod %s is not in the open workspace", modID)
	}

	if err := WriteDescriptor(s.Game, mod.Dir, descriptor); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.refreshMod(modID)

	return nil
}

// FindMod returns the mod a dependency names, by id or name.
func FindMod(mods []Mod, dependency Dependency) (Mod, bool) {
	for _, mod := range mods {
		if mod.Descriptor.ID == dependency.ID || mod.Descriptor.Name == dependency.ID {
			return mod, true
		}
	}

	return Mod{}, false
}

func findMod(mods []Mod, modID string) (Mod, bool) {
	for _, mod := range mods {
		if mod.ID == modID {
			return mod, true
		}
	}

	return Mod{}, false
}

// ListDependencies returns the ids of the mods a mod depends on, directly or
// through other mods. A mod the workspace lacks is left out.
func (s *Session) ListDependencies(modID string) []string {
	order := *s.Order.Load()
	var dependencies []string
	for pending := []string{modID}; len(pending) > 0; {
		mod, _ := findMod(order, pending[0])
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

// DescribeMod returns a mod's title, else its folder's name. It returns the
// game's name for the game, and the id itself for a mod the workspace lacks.
func (s *Session) DescribeMod(modID string) string {
	mod, ok := findMod(*s.Order.Load(), modID)
	switch {
	case ok:
		return cmp.Or(mod.Descriptor.Name, filepath.Base(mod.Dir))
	case modID == files.OriginVanilla:
		return s.Game.Name
	}

	return modID
}

// ResolveWorkshopID returns the Workshop id of the mod's record, else its
// descriptor's.
func (m Mod) ResolveWorkshopID() string {
	return cmp.Or(m.WorkshopID, m.Descriptor.WorkshopID)
}

// withRecord returns the mod with another record. A Workshop mod stays
// external, whatever the record says.
func (m Mod) withRecord(record app.Mod) Mod {
	m.Mod = record
	m.External = record.External || m.Workshop

	return m
}

// AddMod adds a mod folder to a workspace and gives it a color slot.
func (s *Service) AddMod(workspaceID, dir string, external bool) (Mod, error) {
	_, install, row, err := findRecords(s.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}

	if err = checkModFolder(row, install, dir); err != nil {
		return Mod{}, err
	}

	record := app.Mod{ID: rand.Text(), Dir: dir, External: external}
	err = s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		if slices.ContainsFunc(workspace.Mods, func(mod app.Mod) bool { return files.ToKey(mod.Dir) == files.ToKey(dir) }) {
			return fmt.Errorf("%s is a mod of the workspace already", dir)
		}

		record.Color = resolveSlot(workspace.Mods)
		workspace.Mods = append(workspace.Mods, record)

		return nil
	})
	if err != nil {
		return Mod{}, err
	}

	return readMod(row, row.ListWorkshopRoots(), record), nil
}

// ListMods returns the open workspace's mods in load order. Their records are
// read from config.json, which may hold changes the session has not opened.
//
//wails:ignore
func (s *Service) ListMods() []Mod {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	record, _, _, _ := findRecords(s.Config.Config(), ws.ID)
	order := *ws.Order.Load()
	mods := make([]Mod, len(order))
	for position, mod := range order {
		if recorded := slices.IndexFunc(record.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); recorded >= 0 {
			mod = mod.withRecord(record.Mods[recorded])
		}

		mods[position] = mod
	}

	return mods
}

const slotCount = 16

// resolveSlot returns the first color slot that no mod has, else the slot of
// the next place in the load order.
func resolveSlot(mods []app.Mod) string {
	for position := range slotCount {
		slot := makeSlot(position)
		if !slices.ContainsFunc(mods, func(mod app.Mod) bool { return mod.Color == slot }) {
			return slot
		}
	}

	return makeSlot(len(mods))
}

// makeSlot returns the color slot of a place in the load order, wrapping.
func makeSlot(position int) string {
	return fmt.Sprintf("o%d", position%slotCount+1)
}

// CreateMod makes a mod with a localization file and adds it to a workspace.
func (s *Service) CreateMod(workspaceID, dir, name, language string) (Mod, error) {
	_, install, row, err := findRecords(s.Config.Config(), workspaceID)
	if err != nil {
		return Mod{}, err
	}

	folder, err := createModFolder(row, resolveModDir(row, install, dir), resolveModDir(row, install, ""), name,
		resolveVersion(row, install))
	if err != nil {
		return Mod{}, err
	}

	if file, ok := FindLocalizationFile(row, folder, name, language); ok {
		if err := createLocalization(file); err != nil {
			return Mod{}, err
		}
	}

	return s.AddMod(workspaceID, folder, false)
}

// ResolveModFolder returns the folder CreateMod would make for a name, or the
// problem with it. With no name it returns the folder the mod goes in.
func (s *Service) ResolveModFolder(installID, dir, name string) (ModFolder, error) {
	install, row, err := findInstall(s.Config.Config(), installID)
	if err != nil {
		return ModFolder{}, err
	}

	parent := resolveModDir(row, install, dir)
	if name == "" {
		return ModFolder{Path: parent}, nil
	}

	folder, err := resolveModFolder(parent, name)
	if err != nil {
		return ModFolder{Problem: err.Error()}, nil
	}

	return ModFolder{Path: folder}, nil
}

// resolveModDir returns the folder a mod is made in, the game's mod folder by
// default. Some games' launchers read local mods only from that folder.
func resolveModDir(row *game.Game, install app.Install, dir string) string {
	if row.FixedModFolder {
		dir = ""
	}

	return cmp.Or(dir, filepath.Join(row.FindUserDataDir(install.Path), game.LauncherModFolder))
}

// RemoveMod takes a mod out of a workspace. Its folder stays.
func (s *Service) RemoveMod(workspaceID, modID string) error {
	return s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		workspace.Mods = slices.DeleteFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })

		return nil
	})
}

// ReorderMods sets a workspace's load order. It fails unless the ids name each
// mod once.
func (s *Service) ReorderMods(workspaceID string, modIDs []string) error {
	return s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		ordered, err := reorderMods(workspace.Mods, modIDs, func(mod app.Mod) string { return mod.ID })
		if err == nil {
			workspace.Mods = ordered
		}

		return err
	})
}

// reorderMods returns mods in the order of the ids, and fails unless they name
// each mod once.
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

// ResetColors gives each mod the color slot of its place in the load order and
// clears the game's color.
func (s *Service) ResetColors(workspaceID string) error {
	return s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		workspace.GameColor = ""
		for position := range workspace.Mods {
			workspace.Mods[position].Color = makeSlot(position)
		}

		return nil
	})
}

// ListWorkshopMods returns the Workshop mods Steam has downloaded for an
// install's game, by name. A mod's id is "" until it is added.
func (s *Service) ListWorkshopMods(installID string) ([]Mod, error) {
	_, row, err := findInstall(s.Config.Config(), installID)
	if err != nil {
		return nil, err
	}

	dirs, roots := row.ListWorkshopFolders(), row.ListWorkshopRoots()
	mods := make([]Mod, len(dirs))
	for position, dir := range dirs {
		mods[position] = readMod(row, roots, app.Mod{Dir: dir})
	}

	slices.SortFunc(mods, func(mod, other Mod) int {
		return cmp.Compare(strings.ToLower(mod.Descriptor.Name), strings.ToLower(other.Descriptor.Name))
	})

	return mods, nil
}

// SetExternal makes a mod an external mod, or one of the user's own. It fails
// for a Workshop mod, which is always external.
func (s *Service) SetExternal(workspaceID, modID string, external bool) error {
	_, _, row, err := findRecords(s.Config.Config(), workspaceID)
	if err != nil {
		return err
	}

	return s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		position := slices.IndexFunc(workspace.Mods, func(mod app.Mod) bool { return mod.ID == modID })
		switch {
		case position < 0:
			return fmt.Errorf("the workspace has no mod %s", modID)
		case game.IsWorkshopFolder(row.ListWorkshopRoots(), workspace.Mods[position].Dir):
			return errors.New("steam's copy of a Workshop mod is always an external mod")
		}

		workspace.Mods[position].External = external

		return nil
	})
}

// UpdateMod replaces a mod's record.
func (s *Service) UpdateMod(workspaceID string, mod app.Mod) error {
	return s.UpdateRecord(workspaceID, func(workspace *app.Workspace) error {
		if position := slices.IndexFunc(workspace.Mods, func(other app.Mod) bool { return other.ID == mod.ID }); position >= 0 {
			workspace.Mods[position] = mod
		}

		return nil
	})
}

// createModFolder makes a mod's folder with its descriptor and the folders the
// game expects, and returns it.
func createModFolder(row *game.Game, dir, launcherDir, name, version string) (string, error) {
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

	if isMetadata(row.ModDescriptor) {
		return folder, nil
	}

	// A script descriptor is listed by a .mod file in the launcher's folder.
	launcher := encodeDescriptorScript("", descriptor) + `path="` + filepath.ToSlash(folder) + "\"\n"
	err = files.WriteText(filepath.Join(launcherDir, files.ToSlug(name)+".mod"), files.Text{Text: launcher, Encoding: files.EncodingUTF8})

	return folder, err
}

// FindLocalizationFile returns the path of a mod's localization file in a
// language, the base one if empty. It returns false for a game with none.
func FindLocalizationFile(row *game.Game, folder, name, language string) (string, bool) {
	language = cmp.Or(language, loc.BaseLanguage)
	rel, ok := findLocalizationFolder(row)
	if !ok {
		return "", false
	}

	return files.Join(folder, path.Join(rel, language, loc.ToLanguageFile(files.ToSlug(name), language))), true
}

// CreateLocalization writes a localization file with only its header, in the
// language its name ends in.
func (s *Service) CreateLocalization(path string) error {
	if _, err := s.FindWritable(path); err != nil {
		return err
	}

	return createLocalization(path)
}

func createLocalization(file string) error {
	language := loc.FindSuffixLanguage(file)
	if language == "" {
		return fmt.Errorf("%s does not end in a language, such as _l_%s.yml", filepath.Base(file), loc.BaseLanguage)
	}

	return files.WriteText(file, files.Text{Text: loc.Encode(language, nil), Encoding: files.EncodingUTF8BOM})
}

// findLocalizationFolder returns the folder a game keeps localization in.
func findLocalizationFolder(row *game.Game) (string, bool) {
	for _, rel := range row.ModFolders {
		if path.Base(rel) == lang.Localization {
			return rel, true
		}
	}

	return "", false
}

var releaseVersion = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.|$)`)

// toSupportedVersion turns a game version into the release a mod supports, such
// as 1.20.* for 1.20.1.2. It returns "" for a version without two numbers.
func toSupportedVersion(version string) string {
	match := releaseVersion.FindStringSubmatch(version)
	if match == nil {
		return ""
	}

	return match[1] + "." + match[2] + ".*"
}

// MatchVersion reports whether a mod made for a supported version runs on a
// game version. It compares part by part, and a * matches the rest. judged is
// false where it is not known how the games compare, such as for a * inside a
// part or a build id.
func MatchVersion(supported, version string) (matches, judged bool) {
	wanted, actual := strings.Split(supported, "."), strings.Split(version, ".")
	notNumber := func(part string) bool { return jomini.Classify(part) != jomini.ClassNumber }
	notWanted := func(part string) bool { return part != "*" && notNumber(part) }
	judged = supported != "" && !slices.ContainsFunc(wanted, notWanted) && !slices.ContainsFunc(actual, notNumber)

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

// resolveModFolder returns the folder for a mod, named after it. It fails for a
// name with no letter or digit, and for a folder that exists.
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

// checkModFolder fails if a folder cannot be a mod: it is the game's own folder
// or inside it, or lacks the game's descriptor.
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

func resolveDescriptor(row *game.Game, dir string) string { return files.Join(dir, row.ModDescriptor) }

func isMetadata(file string) bool {
	return path.Ext(file) == ".json"
}

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

func listBlockValues(tree *jomini.Tree, statement int32) []string {
	var values []string
	for child := range tree.Children(statement) {
		values = append(values, tree.Unquoted(tree.Statements[child].Value))
	}

	return values
}

// decodeDescriptorMetadata decodes a metadata.json descriptor. A dependency on
// anything but a mod, such as a DLC, is left out.
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

// scriptKeys are the keys encodeDescriptorScript writes itself. It keeps the
// existing file's other statements as they are.
var scriptKeys = []string{
	"name", "version", "supported_version", "picture", "remote_file_id", "replace_path", "tags",
}

// encodeDescriptorScript returns a descriptor's text as script, followed by the
// existing statements a Descriptor does not model.
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

// encodeDescriptorMetadata returns a descriptor's text as metadata.json, with
// the existing keys a Descriptor does not model.
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

	// The appends write an empty list as [], not null.
	custom["replace_paths"] = append([]string{}, descriptor.ReplacePaths...)
	fields["game_custom_data"] = custom
	fields["name"] = descriptor.Name
	fields["version"] = descriptor.Version
	fields["supported_game_version"] = descriptor.SupportedVersion
	fields["tags"] = append([]string{}, descriptor.Tags...)

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
