// textures.go finds the image file or the color that a value in a GUI file
// shows.

package draw

import (
	"cmp"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// library finds the pictures and colors that the definitions of one index
// show, and keeps what it looks up.
type library struct {
	ws   *workspace.Session
	mods *index.Index

	// folders holds the game's icon folders by the define that names each.
	// It is nil until first asked for.
	folders map[string]string

	// defines holds the defines looked up, by name, "" for one the game
	// lacks.
	defines map[string]string

	// firsts and colors hold, by list, the name of the first object that
	// shows the list's picture or writes its color, "" for none.
	firsts map[PictureList]string
	colors map[PictureList]string

	// kinds holds the icon define of each kind of object, "" for a kind no
	// define names.
	kinds map[string]string

	// objects holds the subfolders each folder list picks among.
	objects map[PictureList][]string
}

// placeholderShape is a shape drawn where a picture has no file, with what
// the picture may be of.
type placeholderShape struct {
	// Games are the games the row holds for, all if none.
	Games []string

	Shape string

	// Terms are what the picture may be of, as keys, data types and data
	// functions write it, in lower case with spaces.
	Terms []string

	// Runtime is whether only the running game draws these pictures, so no
	// file is looked for.
	Runtime bool
}

// illustrationFolder says where the pictures are that a definition names by
// a path below a folder.
type illustrationFolder struct {
	// FolderKey is the key by which a definition names the folder. Folder is
	// the folder of one that names none.
	FolderKey string
	Folder    string
}

// pictureKeys are the keys a definition names its picture by, tried in this
// order.
var pictureKeys = []string{"icon", "texture"}

// portraitKey is the key whose binding draws a character's portrait, and
// coatKey the one whose binding draws a coat of arms.
const (
	portraitKey = "portrait_texture"
	coatKey     = "coat_of_arms"
)

// drawingKeys are the keys whose value draws a picture on a widget.
var drawingKeys = slices.Concat(pictureKeys, []string{portraitKey, coatKey})

// maskKeys are the keys whose image cuts a widget's picture to a shape: a
// coat of arms' mask, and a widget's own.
var maskKeys = []string{"coat_of_arms_mask", "mask"}

// defineGroups holds, by game, the group of defines that say where the
// game's icons and pictures are.
var defineGroups = map[string]string{
	game.CK3: "NGameIcons",
	game.EU5: "NGameIcons",
}

// iconPathSuffix ends the name of a define that names a folder of icons.
const iconPathSuffix = "_ICON_PATH"

// The endings of the defines that say where a data function finds its
// pictures, after a database and a field: the folder, how each file's name
// starts, and how it ends.
const (
	folderDefineSuffix = "_PATH"
	prefixDefineSuffix = "_PREFIX"
	nameDefineSuffix   = "_SUFFIX"
)

// levelEnding is the last word of a name that is a level, as prestige_level
// is. levelPrefix starts the names of a level's picture files, as in
// icon_prestige_00.
const (
	levelEnding = "level"
	levelPrefix = "icon_"
)

// makeLibrary returns a library of what the files of an index show.
func makeLibrary(ws *workspace.Session, mods *index.Index) *library {
	return &library{
		ws: ws, mods: mods, defines: map[string]string{}, firsts: map[PictureList]string{}, colors: map[PictureList]string{},
		kinds: map[string]string{}, objects: map[PictureList][]string{},
	}
}

// lookupDefineDatabase returns the database of the defines a GetDefine call
// names.
func lookupDefineDatabase() string {
	targets := lang.GUICallTargets[lang.GUIDefineCall]

	return targets[len(targets)-1]
}

// resolveTexture returns the image file of a texture value as a GUI or coat
// file writes it: a path, an icon's name or a data call. It returns "" if no
// file is found.
func (l *library) resolveTexture(value string) string {
	call, ok := jomini.ParseBracketed(value, 0)
	if !ok {
		return l.findFile(value, "")
	}

	list, references := l.makePictureList("", call)
	for _, reference := range references {
		if file := l.findShown(list, reference); file != "" {
			return file
		}
	}

	return l.findLevelPicture(list)
}

// makePictureList returns the list of pictures a picture binding may draw,
// and the definitions the binding names.
func (l *library) makePictureList(key string, call jomini.Call) (PictureList, []lang.Reference) {
	last := call.Steps[len(call.Steps)-1]
	references := l.ws.Schema.ListCallReferences(call, "")
	object := findObjectType(l.ws.Schema, call)
	argument := ""
	if len(last.Arguments) > 0 && last.Arguments[0].Quoted {
		argument = last.Arguments[0].Text
	}

	rest := lang.DropGetter(last.Text)
	parts := strings.Fields(lang.ToPhrase(rest))
	if list, ok := l.findKind(parts, object, argument); ok {
		list.Key = key
		if argument != "" && list.Folder == "" {
			references = append([]lang.Reference{{Database: list.Database, Name: argument}}, references...)
		}

		return list, references
	}

	// a function with no getter before its name takes no field
	field := ""
	if rest != last.Text {
		field = toSnakeCase(rest)
	}

	list := PictureList{Key: key, Type: object, Field: field, Argument: toWrittenValue(argument)}
	list.Database, _ = l.ws.Schema.ResolveTypeDatabase(object)

	return list, references
}

// wordBreaks are where a data function's name is cut into words: after a
// lower-case letter before an upper-case letter or a digit, between a letter
// and a digit, and before the last capital of a run, as in AIGreed.
// digitBreak catches a digit before a letter that the first cut leaves.
var (
	wordBreaks = regexp.MustCompile(`([a-z])([A-Z0-9])|([a-zA-Z])([0-9])|([0-9])([a-zA-Z])|([A-Z])([A-Z])([a-z])`)
	digitBreak = regexp.MustCompile(`([0-9])([a-zA-Z])`)
)

// toSnakeCase returns a data function's name as a key is written, ai_greed
// for AIGreed.
func toSnakeCase(text string) string {
	spaced := wordBreaks.ReplaceAllString(text, "$1$3$5$7 $2$4$6$8$9")
	spaced = digitBreak.ReplaceAllString(spaced, "$1 $2")
	words := strings.FieldsFunc(spaced, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })

	return strings.ToLower(strings.Join(words, "_"))
}

// findKind returns the kind of thing and the field of it that a data
// function's name, given as its words, asks for: concept and texture for
// GetConceptTexture. It returns false if the words name no kind.
func (l *library) findKind(parts []string, object, argument string) (PictureList, bool) {
	for end := len(parts) - 1; end > 0; end-- {
		kind, field := strings.Join(parts[:end], " "), strings.Join(parts[end:], "_")
		define := l.findIconDefine(kind)
		list := PictureList{Type: kind, Field: field, Argument: argument, Folder: l.listIconFolders()[define]}
		if define != "" && argument != "" && len(l.listFolderObjects(list)) > 0 {
			return list, true
		}

		if l.findLevelFolder(kind) != "" {
			return PictureList{Type: kind, Field: field}, true
		}

		kind, database, ok := l.resolveKind(parts[:end], object)
		if !ok {
			continue
		}

		_, isDefined := findKeptClause(l.ws, l.mods, lang.Reference{Database: database, Name: argument})
		if argument == "" || isDefined {
			return PictureList{Type: kind, Database: database, Field: field}, true
		}
	}

	return PictureList{}, false
}

// resolveKind returns the kind of thing that some words of a data function's
// name stand for, and its database. The words are tried after the data type
// the function is called on, then alone. It returns false if they stand for
// no kind.
func (l *library) resolveKind(parts []string, object string) (kind, database string, ok bool) {
	phrase := strings.Join(parts, " ")
	kinds := []string{phrase}
	if object != "" {
		kinds = []string{lang.ToPhrase(object) + " " + phrase, phrase}
	}

	for _, kind := range kinds {
		if database, ok := l.ws.Schema.ResolveTypeDatabase(kind); ok {
			return kind, database, true
		}
	}

	var function strings.Builder
	for _, part := range parts {
		function.WriteString(toSentence(part))
	}

	if targets := lang.GUICallTargets[function.String()]; len(targets) > 0 {
		return lang.DescribeDefinition(targets[0], false), targets[0], true
	}

	return "", "", false
}

// findLevelFolder returns the folder the defines name for a level's
// pictures, or "" if the kind is no level or no define names its folder.
func (l *library) findLevelFolder(kind string) string {
	words := strings.Fields(lang.ToPhrase(kind))
	if len(words) < 2 || words[len(words)-1] != levelEnding {
		return ""
	}

	key := toDefineWords(kind) + folderDefineSuffix

	return strings.TrimSuffix(l.lookupDefine(defineGroups[l.ws.Game.ID], key), "/")
}

// findLevelPicture returns the first picture of a level, such as
// icon_prestige_00, or "" if there is none.
func (l *library) findLevelPicture(list PictureList) string {
	folder := l.findLevelFolder(list.Type)
	if folder == "" {
		return ""
	}

	words := strings.Fields(lang.ToPhrase(list.Type))
	prefix := levelPrefix + strings.Join(words[:len(words)-1], "_") + "_"
	for _, image := range listImages(l.ws, l.mods, folder) {
		if strings.HasPrefix(strings.ToLower(image.Label), prefix) {
			return image.Place.Path
		}
	}

	return ""
}

// listFolderObjects returns, sorted, the subfolders of a list's folder that
// hold the image its argument names.
func (l *library) listFolderObjects(list PictureList) []string {
	if objects, ok := l.objects[list]; ok {
		return objects
	}

	folder := strings.ToLower(list.Folder) + "/"
	var objects []string
	for _, image := range l.mods.ListImages(folder) {
		rest := strings.ToLower(image.StageRel())
		object, file, ok := strings.Cut(strings.TrimPrefix(rest, folder), "/")
		if ok && strings.TrimSuffix(file, path.Ext(file)) == strings.ToLower(list.Argument) {
			objects = append(objects, object)
		}
	}

	slices.Sort(objects)
	l.objects[list] = slices.Compact(objects)

	return l.objects[list]
}

// findShown returns the image file a list's binding shows of one definition
// or folder, or "" if there is none. A definition's picture is the field the
// binding reads, else what the defines give its kind, else its picture keys
// or its name as an icon, else the one image it names.
func (l *library) findShown(list PictureList, reference lang.Reference) string {
	if list.Folder != "" {
		file, _ := l.mods.ResolveTexture(list.Folder + "/" + reference.Name + "/" + list.Argument)

		return file
	}

	definition, ok := findKeptClause(l.ws, l.mods, reference)
	if !ok {
		return ""
	}

	if value, ok := definition.tree.FindChildValue(definition.statement, list.Field); ok && list.Field != "" && value != "" {
		if file := l.findFile(value, ""); file != "" {
			return file
		}
	}

	file, isFolder := l.findDefinedPicture(list, reference.Name)
	if file != "" {
		return file
	}

	icon := reference.Name
	if isFolder {
		icon = ""
	}

	if file := l.findPicture(definition, icon, l.findIconDefine(list.Type)); file != "" {
		return file
	}

	if images := listDefinitionImages(l.ws, l.mods, definition); len(images) == 1 {
		return images[0]
	}

	return ""
}

// defaultPicture is the picture in a definition's picture folder drawn for a
// definition with none of its own, as _default_62_frame.dds is for a
// frameless government.
const defaultPicture = "_default"

// findDefinedPicture returns the picture the defines give the definition of
// a name, "" if there is none, and whether they name a folder for such
// pictures.
func (l *library) findDefinedPicture(list PictureList, label string) (string, bool) {
	folder, prefix, suffix := l.listPictureDefines(list)
	toFile := func(owner string) string {
		stem := owner
		if owner != "" && list.Argument != "" {
			stem += "_"
		}

		return prefix + stem + list.Argument + suffix
	}

	if folder == "" {
		if prefix == "" && suffix == "" {
			return "", false
		}

		return l.findIcon(toFile(label), ""), false
	}

	for _, owner := range []string{label, defaultPicture} {
		if file, ok := l.mods.ResolveTexture(strings.TrimSuffix(folder, "/") + "/" + toFile(owner)); ok {
			return file, true
		}
	}

	return "", true
}

// suffixStems holds, by game, the define stems whose suffix define is written
// under another stem, each with that stem.
var suffixStems = map[string]map[string]string{
	game.CK3: {"GOVERNMENT_TYPE_REALM_MASK": "GOVERNMENT_TYPE_REALM_FRAME_MASK"},
}

// listPictureDefines returns the folder, prefix and suffix the defines give a
// list's pictures, each "" where the game has no such define.
func (l *library) listPictureDefines(list PictureList) (folder, prefix, suffix string) {
	if list.Field == "" {
		return "", "", ""
	}

	stem := toDefineWords(list.Type) + "_" + strings.ToUpper(list.Field)
	suffixStem := cmp.Or(suffixStems[l.ws.Game.ID][stem], stem)
	group := defineGroups[l.ws.Game.ID]

	return l.lookupDefine(group, stem+folderDefineSuffix),
		l.lookupDefine(group, stem+prefixDefineSuffix),
		l.lookupDefine(group, suffixStem+nameDefineSuffix)
}

// findPicture returns a definition's image file: the one a picture key of
// its block names, else the one the icon's name leads to, or "".
func (l *library) findPicture(definition clause, icon, preferred string) string {
	for _, key := range pictureKeys {
		if picture, ok := definition.tree.FindChildValue(definition.statement, key); ok && picture != "" {
			return l.findFile(picture, preferred)
		}
	}

	return l.findFile(icon, preferred)
}

// findFirst returns the first object a list picks among for which find gives
// a value, and that value, or "" for both. The object's name is kept in the
// cache by list, so a later ask looks up only that one.
func (l *library) findFirst(
	cache map[PictureList]string, list PictureList, find func(PictureList, lang.Reference) string,
) (label, value string) {
	if label, known := cache[list]; known {
		if label == "" {
			return "", ""
		}

		return label, find(list, lang.Reference{Database: list.Database, Name: label})
	}

	for _, reference := range l.listObjects(list) {
		if value = find(list, reference); value != "" {
			cache[list] = reference.Name

			return reference.Name, value
		}
	}

	cache[list] = ""

	return "", ""
}

// listObjects returns the folders or definitions a list picks among, in
// order. Where only the first definition may show its picture, it alone is
// returned.
func (l *library) listObjects(list PictureList) []lang.Reference {
	var objects []lang.Reference
	if list.Folder != "" {
		for _, folder := range l.listFolderObjects(list) {
			objects = append(objects, lang.Reference{Name: folder})
		}

		return objects
	}

	sites := l.mods.ListDefinitions(list.Database)
	if !l.canShowMany(list) {
		sites = sites[:min(1, len(sites))]
	}

	for _, site := range sites {
		objects = append(objects, l.mods.ToReference(site))
	}

	return objects
}

// pickPicture returns the choice a picked value of a list gives its widget:
// an image file in a list of one definition's images, else a definition.
func (l *library) pickPicture(list PictureList, picked string) Choice {
	file := picked
	if list.Name == "" {
		file = l.findShown(list, lang.Reference{Database: list.Database, Name: picked})
	}

	return Choice{Key: list.Key, Value: picked, Texture: file}
}

// canShowMany reports whether more than the first definition of a list's
// database may show a picture of its own.
func (l *library) canShowMany(list PictureList) bool {
	folder, prefix, suffix := l.listPictureDefines(list)

	return l.writesField(list) || l.findIconDefine(list.Type) != "" || folder+prefix+suffix != ""
}

// writesField reports whether the definitions of a list's database write the
// field its binding asks for.
func (l *library) writesField(list PictureList) bool {
	database := l.ws.Schema.Databases[list.Database]

	return database != nil && database.Field != nil && database.Field.Children[list.Field] != nil
}

// lookupDefine returns the value of a define in a group, or "" if the game
// has no such define.
func (l *library) lookupDefine(group, key string) string {
	define := l.ws.Schema.JoinNested(lookupDefineDatabase(), group, key)
	value, known := l.defines[define]
	if !known {
		reference := lang.Reference{Database: lookupDefineDatabase(), Name: define}
		if definition, ok := findKeptClause(l.ws, l.mods, reference); ok {
			value = definition.tree.Unquoted(definition.tree.Statements[definition.statement].ResolveValueSpan())
		}

		l.defines[define] = value
	}

	return value
}

// findFile returns the image file a texture value names as a path or as an
// icon's name, or "".
func (l *library) findFile(value, preferred string) string {
	if file, ok := l.mods.ResolveTexture(value); ok {
		return file
	}

	if value == "" || strings.Contains(value, "[") {
		return ""
	}

	return l.findIcon(value, preferred)
}

// findIcon returns the image file of an icon's name from the game's icon
// folders, or from a folder's overrides file. The folder of the preferred
// define is tried first. It returns "" if no folder has the icon.
func (l *library) findIcon(icon, preferred string) string {
	folders := l.listIconFolders()
	defines := slices.Sorted(maps.Keys(folders))
	if _, ok := folders[preferred]; ok {
		defines = slices.Concat([]string{preferred}, slices.DeleteFunc(defines, func(define string) bool { return define == preferred }))
	}

	stem := strings.TrimSuffix(icon, path.Ext(icon))
	for _, define := range defines {
		if file, ok := l.mods.ResolveTexture(folders[define] + "/" + icon); ok {
			return file
		}

		if file := l.findOverride(folders[define], stem); file != "" {
			return file
		}
	}

	return ""
}

// findIconDefine returns the define that names a kind's icon folder, such as
// GRAPHICAL_CULTURES_ICON_PATH for a graphical culture, or "".
func (l *library) findIconDefine(kind string) string {
	define, known := l.kinds[kind]
	if known {
		return define
	}

	folders := l.listIconFolders()
	phrase := lang.ToPhrase(kind)
	for _, key := range slices.Sorted(maps.Keys(folders)) {
		stem := strings.ToLower(strings.TrimSuffix(key, iconPathSuffix))
		if lang.DescribeDefinition(stem, false) == phrase {
			define = key

			break
		}
	}

	l.kinds[kind] = define

	return define
}

// listIconFolders returns the folder each icon define of the game's group
// names, by the define's key.
func (l *library) listIconFolders() map[string]string {
	if l.folders != nil {
		return l.folders
	}

	l.folders = map[string]string{}
	database, group := lookupDefineDatabase(), defineGroups[l.ws.Game.ID]
	for _, site := range l.mods.ListDefinitions(database) {
		define := l.mods.ToReference(site).Name
		key := l.ws.Schema.SplitNested(database, define)
		_, isListed := l.folders[key]
		if isListed || l.ws.Schema.JoinNested(database, group, key) != define || !strings.HasSuffix(key, iconPathSuffix) {
			continue
		}

		if folder := l.lookupDefine(group, key); folder != "" {
			l.folders[key] = strings.TrimSuffix(folder, "/")
		}
	}

	return l.folders
}

// iconPathFile is the file in an icon folder that names the paths its icons
// are loaded from.
const iconPathFile = "path_overrides.txt"

// findOverride returns the image file an icon folder's overrides file gives
// an icon named without its extension, or "".
func (l *library) findOverride(folder, stem string) string {
	loaded, _, ok := l.mods.FindLoaded(folder + "/" + iconPathFile)
	if !ok {
		return ""
	}

	tree, err := l.ws.LoadTree(loaded.Path)
	if err != nil {
		return ""
	}

	override, ok := tree.FindChildValue(0, stem)
	if !ok {
		return ""
	}

	file, _ := l.mods.ResolveTexture(override)

	return file
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

// toDefineWords returns a kind as the defines write it: GovernmentType and
// government type are GOVERNMENT_TYPE.
func toDefineWords(kind string) string {
	return strings.ToUpper(strings.ReplaceAll(lang.ToPhrase(kind), " ", "_"))
}

// colorType is the data type a color binding returns.
const colorType = "CVector4f"

// resolveBindings gives a widget what its data bindings lead to: pictures,
// masks, colors and the sample texts of its data calls.
func (r *resolver) resolveBindings(window *Window, widget *Widget, context context) {
	schema := r.ws.Schema
	for _, position := range widget.Properties {
		property := window.Properties[position]
		var call jomini.Call
		if written := window.calls[position]; written != nil {
			call = context.expand(schema, *written)
		}

		function, _ := schema.FindFunction(call, "", len(call.Steps)-1)
		switch {
		case slices.Contains(drawingKeys, property.Key) && property.Value != "" && widget.Textures[property.Key] == "":
			r.resolvePicture(widget, property, call)

		case slices.Contains(maskKeys, property.Key) && len(call.Steps) > 0 && widget.Textures[property.Key] == "":
			r.resolveListedPicture(widget, property.Key, call)

		case function.Returns == colorType:
			if color, list := r.library.findColor(call); color != "" {
				list.Key, widget.Colors[property.Key] = property.Key, color
				r.colorLists[widget.ID] = append(r.colorLists[widget.ID], list)
			}
		}
	}

	for key, text := range widget.Texts {
		widget.Texts[key] = r.library.sampleText(text, context, window.listProperties(*widget))
	}
}

// resolvePicture gives a widget the file one of its pictures leads to, else
// a placeholder shape.
func (r *resolver) resolvePicture(widget *Widget, property Property, call jomini.Call) {
	placeholder := findPlaceholder(lang.LookupDialect(r.ws.Game.ID), r.describePicture(property, call))
	if len(call.Steps) > 0 && !placeholder.Runtime {
		r.resolveListedPicture(widget, property.Key, call)
	}

	if widget.Textures[property.Key] == "" && !widgetSlots[widget.Slot].Dresses && widget.Placeholder == "" {
		widget.Placeholder = placeholder.Shape
	}
}

// describePicture returns the words that say what a picture is of, which
// pick its placeholder: its key with the object and step its binding reads,
// or with its path's folders and name.
func (r *resolver) describePicture(property Property, call jomini.Call) string {
	if len(call.Steps) == 0 {
		return lang.ToPhrase(property.Key + " " + strings.NewReplacer("/", " ", ".", " ").Replace(property.Value))
	}

	object, last := findObjectType(r.ws.Schema, call), lang.DropGetter(call.Steps[len(call.Steps)-1].Text)

	return lang.ToPhrase(property.Key + " " + object + " " + last)
}

// placeholders are the shapes drawn where a picture has no file, one game's
// rows first. The last is the plain icon, for a picture no term fits.
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

// findPlaceholder returns the game's first placeholder with a term the
// phrase holds as whole words, else the plain icon.
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

// resolveListedPicture gives a widget the picture a binding draws, and the
// list of pictures it may draw. Where the binding names no definition that
// shows one, the widget draws the first that does.
func (r *resolver) resolveListedPicture(widget *Widget, key string, call jomini.Call) {
	ws, mods, library := r.ws, r.mods, r.library
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
		if label, file := library.findFirst(library.firsts, list, library.findShown); file != "" {
			widget.Textures[key] = file
			list.Shown = label
			widget.Pictures = &list

			return
		}
	}

	file, _ := library.findDefinedPicture(list, "")
	if file = cmp.Or(file, library.findLevelPicture(list)); file != "" {
		widget.Textures[key] = file
	}
}

// illustrations holds, by game, where the pictures are that a definition
// names by a path below a folder.
var illustrations = map[string]illustrationFolder{
	game.CK3: {FolderKey: "folder", Folder: "gfx/interface/illustrations"},
}

// listDefinitionImages returns the image files a definition names, each
// once, those it shows only on a condition last.
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

	unique := []string{}
	for _, file := range append(images, conditional...) {
		unique = appendNew(unique, file)
	}

	return unique
}

// findColor returns the CSS color a binding of a definition's color leads
// to, "" if none, and the list of the definitions it asks about. Where the
// binding names no definition, the first that writes a color gives it.
func (l *library) findColor(call jomini.Call) (string, PictureList) {
	list, references := l.makePictureList("", call)
	if list.Field == "" {
		return "", list
	}

	for _, reference := range references {
		if color := l.findFieldColor(list, reference); color != "" {
			return color, list
		}
	}

	if !l.writesField(list) ||
		slices.ContainsFunc(references, func(reference lang.Reference) bool { return reference.Database == list.Database }) {
		return "", list
	}

	_, color := l.findFirst(l.colors, list, l.findFieldColor)

	return color, list
}

// findFieldColor returns the CSS color one definition writes in a list's
// field: a color block, or the color of the definition the field names. It
// returns "" if it writes none.
func (l *library) findFieldColor(list PictureList, reference lang.Reference) string {
	definition, ok := findKeptClause(l.ws, l.mods, reference)
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
	definition, ok = findKeptClause(l.ws, l.mods, reference)
	if !ok {
		return ""
	}

	return makeColor(definition)
}
