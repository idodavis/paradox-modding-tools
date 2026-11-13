// textures.go finds the image file or the color that a value in a GUI file
// shows.

package draw

import (
	"bytes"
	"cmp"
	"image/png"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// A library finds what a session's files define and show: the pictures and
// colors of definitions. It keeps what it looks up.
type library struct {
	ws      *workspace.Session
	mods    *index.Index
	folders map[string]string        // the icon folders of the game, by the define that names each
	defines map[string]string        // the defines read so far, by name; "" for one the game lacks
	firsts  map[PictureList]string   // the first definition that shows a list's picture, by the list; "" for none
	colors  map[PictureList]string   // the color of the first definition that writes a list's color; "" for none
	kinds   map[string]string        // the icon define of each kind looked up; "" for one no define names
	objects map[PictureList][]string // the folder objects of each list walked
}

// placeholderShape is a shape PMT draws where a picture has no file, for the
// things the picture can be of.
type placeholderShape struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	Shape string

	// Terms are what the picture can be of, as keys, data types and data
	// functions write it, in lower case with spaces.
	Terms []string

	// Runtime is whether only the running game draws these pictures, from
	// what it holds, so that no file is looked for.
	Runtime bool
}

// illustrationFolder is where the pictures are that a definition names by a
// path below a folder.
type illustrationFolder struct {
	// FolderKey is the key that names the folder, and Folder the folder where
	// the definition names none.
	FolderKey string
	Folder    string
}

// pictureKeys are the keys a definition names its picture by, tried in this
// order.
var pictureKeys = []string{"icon", "texture"}

// portraitKey is the key whose binding draws a character's portrait, and
// coatKey the key whose binding draws a coat of arms.
const (
	portraitKey = "portrait_texture"

	// CK3 gui/shared/coat_of_arms.gui
	coatKey = "coat_of_arms"
)

// maskKeys are the keys whose image cuts a widget's picture to its shape: a
// coat of arms' mask, and a widget's own.
var maskKeys = []string{
	// CK3 gui/shared/coat_of_arms.gui:128 [GovernmentType.GetRealmMask]; Vic3
	// gui/company_panel.gui's portrait
	"coat_of_arms_mask", "mask",
}

// defineGroups are the define groups whose defines say where each game's icons
// and pictures are, by the game's id.
var defineGroups = map[string]string{
	// common/defines/graphic/00_graphics.txt:782 writes TRAIT_ICON_PATH
	// in NGameIcons, and :812 the realm mask's path
	game.CK3: "NGameIcons",
	// loading_screen/common/defines/graphic/00_graphics.txt writes its
	// icon paths in NGameIcons, such as :851 UNIT_STATUS_ICON_PATH
	game.EU5: "NGameIcons",
}

// iconPathSuffix is the ending of the defines that name a folder of icons.
const iconPathSuffix = "_ICON_PATH"

// folderDefineSuffix, prefixDefineSuffix and nameDefineSuffix are the
// endings, after a database and a field, of the defines that say where a data
// function finds its pictures, how each picture file's name starts and how it
// ends.
//
// CK3 common/defines/graphic/00_graphics.txt:814-817,
// GOVERNMENT_TYPE_REALM_FRAME_PATH and _SUFFIX; EU5
// loading_screen/common/defines/graphic/00_graphics.txt:836,
// GOODS_ICON_PREFIX
const (
	folderDefineSuffix = "_PATH"
	prefixDefineSuffix = "_PREFIX"
	nameDefineSuffix   = "_SUFFIX"
)

// The level ending and prefix say how a level, such as prestige_level, finds
// its pictures. levelEnding marks a name as a level, and levelPrefix starts
// the names of its files.
//
// CK3 common/defines/graphic/00_graphics.txt:846-851, PRESTIGE_LEVEL_PATH
// and the piety, influence and merit ones, name
// gfx/interface/icons/modifiers, where the files are icon_prestige_00 to
// _05 and icon_piety_christian_00
const (
	levelEnding = "level"
	levelPrefix = "icon_"
)

// makeLibrary returns a library of what the files of the given index define and
// show.
func makeLibrary(ws *workspace.Session, mods *index.Index) *library {
	return &library{
		ws: ws, mods: mods, defines: map[string]string{}, firsts: map[PictureList]string{}, colors: map[PictureList]string{},
		kinds: map[string]string{}, objects: map[PictureList][]string{},
	}
}

// ExportPNG writes a PNG the page made to the given file. It returns an error
// if the data is not a PNG or the file cannot be written.
func (drw *Service) ExportPNG(target string, data []byte) error {
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

// lookupDefineDatabase returns the database the GetDefine call reads.
func lookupDefineDatabase() string {
	targets := lang.GUICallTargets[lang.GUIDefineCall]
	return targets[len(targets)-1]
}

// resolveTexture returns the image file for a texture value as a GUI or coat
// file writes it, a path, an icon name or a texture binding, or "" if no file
// is found.
func (library *library) resolveTexture(value string) string {
	call, ok := jomini.ParseBracketed(value, 0)
	if !ok {
		return library.findFile(value, "")
	}
	list, references := library.makePictureList("", call)
	for _, reference := range references {
		if file := library.findShown(list, reference); file != "" {
			return file
		}
	}
	return library.findLevelPicture(list)
}

// makePictureList returns the list of pictures a picture binding may draw, and
// the definitions it names.
func (library *library) makePictureList(key string, call jomini.Call) (PictureList, []lang.Reference) {
	last := call.Steps[len(call.Steps)-1]
	references := library.ws.Schema.ListCallReferences(call, "")
	object := findObjectType(library.ws.Schema, call)
	argument := ""
	if len(last.Arguments) > 0 && last.Arguments[0].Quoted {
		argument = last.Arguments[0].Text
	}
	rest := lang.DropGetter(last.Text)
	parts := strings.Fields(lang.ToPhrase(rest))
	if list, ok := library.findKind(parts, object, argument); ok {
		list.Key = key
		if argument != "" && list.Folder == "" {
			references = append([]lang.Reference{{Database: list.Database, Name: argument}}, references...)
		}
		return list, references
	}
	field := lo.Ternary(rest == last.Text, "", lo.SnakeCase(rest))
	list := PictureList{Key: key, Type: object, Field: field, Argument: toWrittenValue(argument)}
	list.Database, _ = library.ws.Schema.ResolveTypeDatabase(object)
	return list, references
}

// findKind returns the kind and field a data function's name reads, such as
// concept and texture for GetConceptTexture. It returns false if its name names
// no kind.
func (library *library) findKind(parts []string, object, argument string) (PictureList, bool) {
	for end := len(parts) - 1; end > 0; end-- {
		kind, field := strings.Join(parts[:end], " "), strings.Join(parts[end:], "_")
		define := library.findIconDefine(kind)
		list := PictureList{Type: kind, Field: field, Argument: argument, Folder: library.listIconFolders()[define]}
		if define != "" && argument != "" && len(library.listFolderObjects(list)) > 0 {
			return list, true
		}
		if library.findLevelFolder(kind) != "" {
			return PictureList{Type: kind, Field: field}, true
		}
		kind, database, ok := library.resolveKind(parts[:end], object)
		if !ok {
			continue
		}
		_, isDefined := findKeptClause(library.ws, library.mods, lang.Reference{Database: database, Name: argument})
		if argument == "" || isDefined {
			return PictureList{Type: kind, Database: database, Field: field}, true
		}
	}
	return PictureList{}, false
}

// resolveKind returns the kind the given words of a data function's name stand
// for, and its database, or false if they stand for none.
func (library *library) resolveKind(parts []string, object string) (kind, database string, ok bool) {
	phrase := strings.Join(parts, " ")
	kinds := []string{phrase}
	if object != "" {
		kinds = []string{lang.ToPhrase(object) + " " + phrase, phrase}
	}
	for _, kind := range kinds {
		if database, ok := library.ws.Schema.ResolveTypeDatabase(kind); ok {
			return kind, database, true
		}
	}
	function := strings.Join(lo.Map(parts, func(part string, _ int) string { return lo.Capitalize(part) }), "")
	if targets := lang.GUICallTargets[function]; len(targets) > 0 {
		return lang.DescribeDefinition(targets[0], false), targets[0], true
	}
	return "", "", false
}

// findLevelFolder returns the folder the defines name for a level's pictures,
// or "" if there is none.
func (library *library) findLevelFolder(kind string) string {
	words := strings.Fields(lang.ToPhrase(kind))
	if len(words) < 2 || words[len(words)-1] != levelEnding {
		return ""
	}
	key := toDefineWords(kind) + folderDefineSuffix
	return strings.TrimSuffix(library.lookupDefine(defineGroups[library.ws.Game.ID], key), "/")
}

// findLevelPicture returns the first picture of a level, such as
// icon_prestige_00, or "" if there is none.
func (library *library) findLevelPicture(list PictureList) string {
	folder := library.findLevelFolder(list.Type)
	if folder == "" {
		return ""
	}
	words := strings.Fields(lang.ToPhrase(list.Type))
	prefix := levelPrefix + strings.Join(words[:len(words)-1], "_") + "_"
	picture, ok := lo.Find(listImages(library.ws, library.mods, folder), func(image Image) bool {
		return strings.HasPrefix(strings.ToLower(image.Label), prefix)
	})
	return lo.Ternary(ok, picture.Place.Path, "")
}

// listFolderObjects returns the subfolders of a list's folder that hold the
// image its argument names, sorted by name.
func (library *library) listFolderObjects(list PictureList) []string {
	if objects, ok := library.objects[list]; ok {
		return objects
	}
	folder := strings.ToLower(list.Folder) + "/"
	var objects []string
	for _, image := range library.mods.ListImages(folder) {
		rest := strings.ToLower(image.StageRel())
		object, file, ok := strings.Cut(strings.TrimPrefix(rest, folder), "/")
		if ok && strings.TrimSuffix(file, path.Ext(file)) == strings.ToLower(list.Argument) {
			objects = append(objects, object)
		}
	}
	slices.Sort(objects)
	library.objects[list] = slices.Compact(objects)
	return library.objects[list]
}

// findShown returns the image file a list's binding shows of one definition or
// folder, or "" if there is none.
func (library *library) findShown(list PictureList, reference lang.Reference) string {
	if list.Folder != "" {
		file, _ := library.mods.ResolveTexture(list.Folder + "/" + reference.Name + "/" + list.Argument)
		return file
	}
	definition, ok := findKeptClause(library.ws, library.mods, reference)
	if !ok {
		return ""
	}
	if value, ok := definition.tree.FindChildValue(definition.statement, list.Field); ok && list.Field != "" && value != "" {
		if file := library.findFile(value, ""); file != "" {
			return file
		}
	}
	file, isFolder := library.findDefinedPicture(list, reference.Name)
	if file != "" {
		return file
	}
	icon := lo.Ternary(isFolder, "", reference.Name)
	if file := library.findPicture(definition, icon, library.findIconDefine(list.Type)); file != "" {
		return file
	}
	if images := listDefinitionImages(library.ws, library.mods, definition); len(images) == 1 {
		return images[0]
	}
	return ""
}

// defaultPicture is the name, in the folder of a definition's pictures, of the
// picture drawn for a definition with none of its own.
//
// CK3 gfx/interface/icons/realm_frames/_default_62_frame.dds, drawn for
// frameless feudal_government
const defaultPicture = "_default"

// findDefinedPicture returns the picture of a definition the defines name, or
// "" if there is none, and whether they name its folder.
func (library *library) findDefinedPicture(list PictureList, name string) (string, bool) {
	folder, prefix, suffix := library.listPictureDefines(list)
	toFile := func(owner string) string {
		return prefix + strings.Join(lo.Compact([]string{owner, list.Argument}), "_") + suffix
	}
	if folder == "" {
		if prefix == "" && suffix == "" {
			return "", false
		}
		return library.findIcon(toFile(name), ""), false
	}
	for _, owner := range []string{name, defaultPicture} {
		if file, ok := library.mods.ResolveTexture(strings.TrimSuffix(folder, "/") + "/" + toFile(owner)); ok {
			return file, true
		}
	}
	return "", true
}

// suffixStems are the stems of the defines for a picture field, each with the
// stem its suffix define is written under, where the two differ, by the game's
// id.
var suffixStems = map[string]map[string]string{
	// common/defines/graphic/00_graphics.txt:812-813:
	// GOVERNMENT_TYPE_REALM_MASK_PATH with
	// GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX
	game.CK3: {"GOVERNMENT_TYPE_REALM_MASK": "GOVERNMENT_TYPE_REALM_FRAME_MASK"},
}

// listPictureDefines returns the folder, prefix and suffix the defines give a
// list's pictures, each "" if the game has no such define.
func (library *library) listPictureDefines(list PictureList) (folder, prefix, suffix string) {
	if list.Field == "" {
		return "", "", ""
	}
	stem := toDefineWords(list.Type) + "_" + strings.ToUpper(list.Field)
	suffixStem := cmp.Or(suffixStems[library.ws.Game.ID][stem], stem)
	group := defineGroups[library.ws.Game.ID]
	return library.lookupDefine(group, stem+folderDefineSuffix),
		library.lookupDefine(group, stem+prefixDefineSuffix),
		library.lookupDefine(group, suffixStem+nameDefineSuffix)
}

// findPicture returns the image file of a definition, from its picture keys or
// an icon name, or "" if there is none.
func (library *library) findPicture(definition clause, icon, preferred string) string {
	for _, key := range pictureKeys {
		if picture, ok := definition.tree.FindChildValue(definition.statement, key); ok && picture != "" {
			return library.findFile(picture, preferred)
		}
	}
	return library.findFile(icon, preferred)
}

// findFirstShown returns the first object a list picks among that shows a
// picture, with that picture, or "" for both if none does.
func (library *library) findFirstShown(list PictureList) (name, file string) {
	name, known := library.firsts[list]
	if known {
		if name == "" {
			return "", ""
		}
		return name, library.findShown(list, lang.Reference{Database: list.Database, Name: name})
	}
	for _, reference := range library.listObjects(list) {
		if file = library.findShown(list, reference); file != "" {
			library.firsts[list] = reference.Name
			return reference.Name, file
		}
	}
	library.firsts[list] = ""
	return "", ""
}

// listObjects returns the folders or definitions a list picks among, in order.
func (library *library) listObjects(list PictureList) []lang.Reference {
	if list.Folder != "" {
		return lo.Map(library.listFolderObjects(list), func(name string, _ int) lang.Reference { return lang.Reference{Name: name} })
	}
	sites := library.mods.ListDefinitions(list.Database)
	if !library.canShowMany(list) {
		sites = sites[:min(1, len(sites))]
	}
	return lo.Map(sites, func(site index.Site, _ int) lang.Reference { return library.mods.ToReference(site) })
}

// pickPicture returns the choice that picking a value of a list gives its
// widget. In a list of one definition's images the value is the image file, and
// in any other it is a definition.
func (library *library) pickPicture(list PictureList, picked string) Choice {
	texture := picked
	if list.Name == "" {
		texture = library.findShown(list, lang.Reference{Database: list.Database, Name: picked})
	}
	return Choice{Key: list.Key, Value: picked, Texture: texture}
}

// canShowMany reports whether more than the first definition of a list's
// database may show its picture.
func (library *library) canShowMany(list PictureList) bool {
	folder, prefix, suffix := library.listPictureDefines(list)
	return library.writesField(list) || library.findIconDefine(list.Type) != "" || folder+prefix+suffix != ""
}

// writesField reports whether the definitions of a list's database write the
// field its binding reads.
func (library *library) writesField(list PictureList) bool {
	database := library.ws.Schema.Databases[list.Database]
	return database != nil && database.Field != nil && database.Field.Children[list.Field] != nil
}

// lookupDefine returns the value of the define with a key in a group, or "" if
// the game has none.
func (library *library) lookupDefine(group, key string) string {
	define := library.ws.Schema.JoinNested(lookupDefineDatabase(), group, key)
	value, known := library.defines[define]
	if !known {
		reference := lang.Reference{Database: lookupDefineDatabase(), Name: define}
		if clause, ok := findKeptClause(library.ws, library.mods, reference); ok {
			value = clause.tree.Unquoted(clause.tree.Statements[clause.statement].ResolveValueSpan())
		}
		library.defines[define] = value
	}
	return value
}

// findFile returns the image file a texture value names as a path or an icon
// name, or "" if there is none.
func (library *library) findFile(value, preferred string) string {
	if file, ok := library.mods.ResolveTexture(value); ok {
		return file
	}
	if value == "" || strings.Contains(value, "[") {
		return ""
	}
	return library.findIcon(value, preferred)
}

// findIcon returns the image file of an icon name, from the icon folders or a
// folder's overrides file, or "" if none has it.
func (library *library) findIcon(icon, preferred string) string {
	folders := library.listIconFolders()
	defines := slices.Sorted(maps.Keys(folders))
	if _, ok := folders[preferred]; ok {
		defines = slices.Concat([]string{preferred}, slices.DeleteFunc(defines, func(define string) bool { return define == preferred }))
	}
	stem := strings.TrimSuffix(icon, path.Ext(icon))
	for _, define := range defines {
		if file, ok := library.mods.ResolveTexture(folders[define] + "/" + icon); ok {
			return file
		}
		if file := library.findOverride(folders[define], stem); file != "" {
			return file
		}
	}
	return ""
}

// findIconDefine returns the define that names the icon folder of a kind of
// object, such as GRAPHICAL_CULTURES_ICON_PATH for a graphical culture, or ""
// where no define does.
func (library *library) findIconDefine(kind string) string {
	define, known := library.kinds[kind]
	if known {
		return define
	}
	folders := library.listIconFolders()
	phrase := lang.ToPhrase(kind)
	for _, key := range slices.Sorted(maps.Keys(folders)) {
		stem := strings.ToLower(strings.TrimSuffix(key, iconPathSuffix))
		if lang.DescribeDefinition(stem, false) == phrase {
			define = key
			break
		}
	}
	library.kinds[kind] = define
	return define
}

// listIconFolders returns the folder each define of the game's GUI group names
// as holding icons, by the define's key.
func (library *library) listIconFolders() map[string]string {
	if library.folders != nil {
		return library.folders
	}
	library.folders = map[string]string{}
	database, group := lookupDefineDatabase(), defineGroups[library.ws.Game.ID]
	for _, site := range library.mods.ListDefinitions(database) {
		define := library.mods.ToReference(site).Name
		key := library.ws.Schema.SplitNested(database, define)
		_, listed := library.folders[key]
		if listed || library.ws.Schema.JoinNested(database, group, key) != define || !strings.HasSuffix(key, iconPathSuffix) {
			continue
		}
		if folder := library.lookupDefine(group, key); folder != "" {
			library.folders[key] = strings.TrimSuffix(folder, "/")
		}
	}
	return library.folders
}

// iconPathFile is the file in an icon folder that names the paths its icons
// are read from.
const iconPathFile = "path_overrides.txt"

// findOverride returns the image file an icon folder's overrides file gives an
// icon stem, or "" if it gives none.
func (library *library) findOverride(folder, stem string) string {
	loaded, _, ok := library.mods.FindLoaded(folder + "/" + iconPathFile)
	if !ok {
		return ""
	}
	tree, err := library.ws.LoadTree(loaded.Path)
	if err != nil {
		return ""
	}
	for statement := range tree.Children(0) {
		if current := tree.Statements[statement]; tree.Unquoted(current.Key) == stem {
			file, _ := library.mods.ResolveTexture(tree.Unquoted(current.Value))
			return file
		}
	}
	return ""
}

// findObjectType returns the data type of the object whose picture a texture
// binding draws, or "" for a binding of one step or one the schema cannot
// follow.
func findObjectType(schema *lang.Schema, call jomini.Call) string {
	types := schema.ResolveCallTypes(call, "")
	if len(call.Steps) < 2 || len(types) < len(call.Steps)-1 {
		return ""
	}
	return types[len(call.Steps)-2]
}

// toDefineWords returns a kind of object as the defines write it:
// GovernmentType and government type are GOVERNMENT_TYPE.
func toDefineWords(kind string) string {
	return strings.ToUpper(strings.ReplaceAll(lang.ToPhrase(kind), " ", "_"))
}

// colorType is the data type that a binding of a color returns.
//
// the dumps' Return type of GetColor in the three installs
const colorType = "CVector4f"

// resolveBindings gives a widget the pictures, masks, colors and sample texts
// its data bindings lead to.
func (resolver *resolver) resolveBindings(window *Window, widget *Widget, context context) {
	schema := resolver.ws.Schema
	for property := range window.listProperties(*widget) {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		if isCall {
			call = context.expand(schema, call)
		}
		isPicture := property.Key == portraitKey || property.Key == coatKey ||
			slices.Contains(pictureKeys, property.Key)
		function, _ := schema.FindFunction(call, "", len(call.Steps)-1)
		switch {
		case isPicture && property.Value != "" && widget.Textures[property.Key] == "":
			resolver.resolvePicture(widget, property, call)
		case slices.Contains(maskKeys, property.Key) && len(call.Steps) > 0 && widget.Textures[property.Key] == "":
			resolver.resolveListedPicture(widget, property.Key, call)
		case function.Returns == colorType:
			if color, list := resolver.library.findColor(call); color != "" {
				list.Key, widget.Colors[property.Key] = property.Key, color
				resolver.colorLists[widget.ID] = append(resolver.colorLists[widget.ID], list)
			}
		}
	}
	for key, text := range widget.Texts {
		widget.Texts[key] = resolver.library.sampleText(text, context, window.listProperties(*widget))
	}
}

// resolvePicture gives a widget the file one of its pictures leads to, else a
// placeholder shape.
func (resolver *resolver) resolvePicture(widget *Widget, property Property, call jomini.Call) {
	placeholder := findPlaceholder(lang.LookupDialect(resolver.ws.Game.ID), resolver.describePicture(property, call))
	if len(call.Steps) > 0 && !placeholder.Runtime {
		resolver.resolveListedPicture(widget, property.Key, call)
	}
	if widget.Textures[property.Key] == "" && !widgetSlots[widget.Slot].Dresses && widget.Placeholder == "" {
		widget.Placeholder = placeholder.Shape
	}
}

// describePicture returns the words that say what a picture is of, which pick
// its placeholder.
func (resolver *resolver) describePicture(property Property, call jomini.Call) string {
	if len(call.Steps) == 0 {
		return lang.ToPhrase(property.Key + " " + strings.NewReplacer("/", " ", ".", " ").Replace(property.Value))
	}
	object, last := findObjectType(resolver.ws.Schema, call), lang.DropGetter(call.Steps[len(call.Steps)-1].Text)
	return lang.ToPhrase(property.Key + " " + object + " " + last)
}

// placeholders are the shapes drawn where a picture has no file, a game's own
// first. The last is the plain icon, for a picture no term fits.
var placeholders = []placeholderShape{
	{Games: []string{game.CK3}, Shape: "crowned-ruler", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: []string{game.CK3}, Shape: "shield", Terms: []string{"coat of arms"}, Runtime: true},
	{Games: []string{game.Vic3}, Shape: "top-hat", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: []string{game.EU5}, Shape: "plumed-hat", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: []string{game.Vic3, game.EU5}, Shape: "flag", Terms: []string{"coat of arms"}, Runtime: true},
	{Shape: "illustration", Terms: []string{"illustration", "image", "picture"}},
	{Shape: "flag", Terms: []string{"flag"}},
	{Shape: "faith", Terms: []string{"faith", "religion", "religious", "rite", "tenet", "doctrine"}},
	{Shape: "goods", Terms: []string{"goods", "resource"}},
	{Shape: "action", Terms: []string{"action", "actions", "interaction", "task"}},
	{Shape: "military", Terms: []string{"unit", "formation", "battle", "war", "military"}},
	{Shape: "building", Terms: []string{"building", "holding", "domicile", "construction", "great project"}},
	{Shape: "trait", Terms: []string{"trait", "perk", "role", "tradition", "pillar"}},
	{Shape: "modifier", Terms: []string{"modifier"}},
	{Shape: "concept", Terms: []string{"concept", "encyclopedia", "article"}},
	{Shape: "rank", Terms: []string{"rank", "level", "status"}},
	{Shape: "map", Terms: []string{"map", "terrain", "strait", "location"}},
	{Shape: "message", Terms: []string{"message", "alert", "notification"}},
	{Shape: "icon"},
}

// findPlaceholder returns the first placeholder of the given game with a term
// that a phrase holds whole, such as "coat of arms". The phrase says what the
// picture is of. It returns the plain icon if no term fits.
func findPlaceholder(dialect *lang.Dialect, phrase string) placeholderShape {
	padded := " " + phrase + " "
	for _, placeholder := range placeholders {
		isNamed := dialect.Matches(placeholder.Games) && slices.ContainsFunc(placeholder.Terms, func(term string) bool {
			return strings.Contains(padded, " "+term+" ")
		})
		if isNamed {
			return placeholder
		}
	}

	return placeholders[len(placeholders)-1]
}

// resolveListedPicture gives a widget the picture a binding draws, and the list
// of pictures it may draw.
func (resolver *resolver) resolveListedPicture(widget *Widget, key string, call jomini.Call) {
	ws, mods, library := resolver.ws, resolver.mods, resolver.library
	list, references := library.makePictureList(key, call)
	for _, reference := range references {
		if file := library.findShown(list, reference); file != "" {
			widget.Textures[key] = file
			return
		}
	}
	namesDefinition := slices.ContainsFunc(references, func(reference lang.Reference) bool { return reference.Database == list.Database })
	if widget.Pictures != nil || (namesDefinition && list.Database != "") {
		return
	}
	for _, candidate := range references {
		reference := resolveReference(mods, candidate)
		definition, ok := findKeptClause(ws, mods, reference)
		if images := listDefinitionImages(ws, mods, definition); ok && len(images) > 1 {
			widget.Textures[key] = images[0]
			widget.Pictures = &PictureList{Key: key, Database: reference.Database, Name: reference.Name, Shown: images[0]}
			return
		}
	}
	if list.Database != "" || list.Folder != "" {
		if name, file := library.findFirstShown(list); file != "" {
			widget.Textures[key] = file
			list.Shown = name
			widget.Pictures = &list
			return
		}
	}
	file, _ := library.findDefinedPicture(list, "")
	if file = cmp.Or(file, library.findLevelPicture(list)); file != "" {
		widget.Textures[key] = file
	}
}

// illustrations are where the pictures are that a definition names by a path
// below a folder, by the game's id.
var illustrations = map[string]illustrationFolder{
	// gfx/interface/illustrations/scripted_illustrations
	// /_scripted_illustrations.info
	game.CK3: {FolderKey: "folder", Folder: "gfx/interface/illustrations"},
}

// listDefinitionImages returns the image files a definition names, the ones it
// shows on a condition last.
func listDefinitionImages(ws *workspace.Session, mods *index.Index, definition clause) []string {
	if definition.tree == nil {
		return nil
	}
	rules := illustrations[ws.Game.ID]
	folder, ok := definition.tree.FindChildValue(definition.statement, rules.FolderKey)
	if !ok {
		folder = rules.Folder
	}
	var images, conditional []string
	for _, part := range listChildren(definition) {
		_, isConditional := part.tree.FindChild(part.statement, lang.TriggerKey)
		last := part.tree.Statements[part.statement].Last
		for _, inner := range part.tree.Statements[part.statement:max(last, part.statement+1)] {
			value := part.tree.Unquoted(inner.Value)
			if !texture.IsImage(value) {
				continue
			}
			file, ok := mods.ResolveTexture(value)
			if !ok {
				file, ok = mods.ResolveTexture(path.Join(folder, value))
			}
			switch {
			case !ok: // no file has this image
			case isConditional:
				conditional = append(conditional, file)
			default:
				images = append(images, file)
			}
		}
	}
	return lo.Uniq(append(images, conditional...))
}

// findColor returns the CSS color a binding of a definition's color leads to,
// or "" if there is none, and the list of the definitions it reads.
func (library *library) findColor(call jomini.Call) (string, PictureList) {
	list, references := library.makePictureList("", call)
	if list.Field == "" {
		return "", list
	}
	for _, reference := range references {
		if color := library.findFieldColor(list, reference); color != "" {
			return color, list
		}
	}
	if slices.ContainsFunc(references, func(reference lang.Reference) bool { return reference.Database == list.Database }) {
		return "", list
	}
	return library.findFirstColor(list), list
}

// findFirstColor returns the first CSS color a list's definitions write in its
// field, or "" if there is none.
func (library *library) findFirstColor(list PictureList) string {
	color, known := library.colors[list]
	if known || !library.writesField(list) {
		return color
	}
	for _, reference := range library.listObjects(list) {
		if color = library.findFieldColor(list, reference); color != "" {
			break
		}
	}
	library.colors[list] = color
	return color
}

// findFieldColor returns the CSS color one definition writes in a list's field,
// or "" if it writes none.
func (library *library) findFieldColor(list PictureList, reference lang.Reference) string {
	definition, ok := findKeptClause(library.ws, library.mods, reference)
	if !ok {
		return ""
	}
	field, ok := definition.tree.FindChild(definition.statement, list.Field)
	if !ok {
		return ""
	}
	if color := makeColor(clause{tree: definition.tree, statement: field}); color != "" {
		return color
	}
	reference = lang.Reference{Name: definition.tree.Unquoted(definition.tree.Statements[field].Value)}
	definition, ok = findKeptClause(library.ws, library.mods, reference)
	if !ok {
		return ""
	}
	return makeColor(definition)
}
