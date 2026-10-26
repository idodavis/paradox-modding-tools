// textures.go finds the image file or the color that a value in a GUI file shows.

package draw

import (
	"bytes"
	"cmp"
	"image/png"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// A library finds the pictures and colors that a session's definitions show, and keeps what it looks up.
type library struct {
	ws      *workspace.Session
	mods    *index.Index
	folders map[string]string      // the icon folders of the game, by the define that names each
	defines map[string]string      // the defines read so far, by name; "" for one the game lacks
	firsts  map[PictureList]string // the first definition that shows a list's picture, by the list; "" for none
	colors  map[PictureList]string // the color of the first definition that writes a list's color; "" for none
}

// defineCall is the data call that reads a define, whose database lists the folders of the game's icons.
const defineCall = "GetDefine"

// ExportPNG writes a PNG that the page made, such as a coat as drawn or one mip level of an image, to the given file,
// which the user chose. It returns an error if the data is not a PNG or the file cannot be written.
func (drw *Service) ExportPNG(target string, data []byte) error {
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

// lookupDefineDatabase returns the database of the defines that the data call reading a define names: the one its
// last argument names.
func lookupDefineDatabase() string {
	targets := game.GUI.CallTargets[defineCall]
	return targets[len(targets)-1]
}

// resolveTexture returns the image file for a texture value as a GUI or coat file writes it: a path under the game's
// gfx folders, an icon name looked up in the icon folders, or a texture binding. It returns "" if no file is found.
func (library *library) resolveTexture(written string) string {
	if call, ok := jomini.ParseBracketed(written, 0); ok {
		return library.resolveBinding(call)
	}
	return library.findFile(written, "")
}

// resolveBinding returns the image file of a texture binding: what it shows of the first definition that it names and
// that shows a picture. It returns "" if it names none that does.
func (library *library) resolveBinding(call jomini.Call) string {
	list, references := library.makePictureList("", call)
	for _, reference := range references {
		if file := library.findShown(list, reference); file != "" {
			return file
		}
	}
	return library.findLevelPicture(list)
}

// makePictureList returns the list of pictures a picture binding may draw, and the definitions it names.
func (library *library) makePictureList(key string, call jomini.Call) (PictureList, []schema.Reference) {
	last := call.Steps[len(call.Steps)-1]
	references := library.ws.Schema.ListCallReferences(call, "")
	object := findObjectType(library.ws.Schema, call)
	argument := ""
	if len(last.Arguments) > 0 && last.Arguments[0].Quoted {
		argument = last.Arguments[0].Text
	}
	parts := strings.Fields(schema.ToPhrase(trimGetter(last.Text)))
	if list, ok := library.findKind(parts, object, argument); ok {
		list.Key = key
		if argument != "" && list.Folder == "" {
			references = append([]schema.Reference{{Database: list.Database, Name: argument}}, references...)
		}
		return list, references
	}
	list := PictureList{Key: key, Type: object, Field: toField(last.Text), Argument: toWrittenValue(argument)}
	list.Database, _ = library.ws.Schema.ResolveTypeDatabase(object)
	return list, references
}

// findKind returns the kind and field that a data function's name reads, such as concept and texture for
// GetConceptTexture. It returns false if its name names no kind.
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
		_, isDefined := findKeptClause(library.ws, library.mods, schema.Reference{Database: database, Name: argument})
		if argument == "" || isDefined {
			return PictureList{Type: kind, Database: database, Field: field}, true
		}
	}
	return PictureList{}, false
}

// resolveKind returns the kind that parts of a data function's name name, and its database. It returns false if they
// name none.
func (library *library) resolveKind(parts []string, object string) (kind, database string, ok bool) {
	phrase := strings.Join(parts, " ")
	kinds := []string{phrase}
	if object != "" {
		kinds = []string{schema.ToPhrase(object) + " " + phrase, phrase}
	}
	for _, kind := range kinds {
		if database, ok := library.ws.Schema.ResolveTypeDatabase(kind); ok {
			return kind, database, true
		}
	}
	if targets := game.GUI.CallTargets[toCamel(parts)]; len(targets) > 0 {
		return game.DescribeDefinition(targets[0], false), targets[0], true
	}
	return "", "", false
}

// findLevelFolder returns the folder the defines name for a level's pictures, or "" if there is none.
func (library *library) findLevelFolder(kind string) string {
	words := strings.Fields(schema.ToPhrase(kind))
	if len(words) < 2 || words[len(words)-1] != game.GUI.Levels.Word {
		return ""
	}
	key := toDefineWords(kind) + game.GUI.FolderDefineSuffix
	return strings.TrimSuffix(library.lookupDefine(library.ws.Game.GUIDefineGroup, key), "/")
}

// findLevelPicture returns the first picture of a level, such as icon_prestige_00, or "" if there is none.
func (library *library) findLevelPicture(list PictureList) string {
	folder := library.findLevelFolder(list.Type)
	if folder == "" {
		return ""
	}
	words := strings.Fields(schema.ToPhrase(list.Type))
	prefix := game.GUI.Levels.Prefix + strings.Join(words[:len(words)-1], "_") + "_"
	var candidates []files.File
	for _, image := range library.mods.ListImages(folder + "/") {
		rel := strings.ToLower(filepath.ToSlash(image.Rel))
		if path.Dir(rel) == strings.ToLower(folder) && strings.HasPrefix(path.Base(rel), prefix) {
			candidates = append(candidates, image)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return slices.MinFunc(candidates, func(first, second files.File) int {
		return cmp.Compare(strings.ToLower(first.Rel), strings.ToLower(second.Rel))
	}).Path
}

// listFolderObjects returns the subfolders of a list's folder that hold the image its argument names, one per object,
// in the order of their names.
func (library *library) listFolderObjects(list PictureList) []string {
	folder := strings.ToLower(list.Folder) + "/"
	var objects []string
	for _, image := range library.mods.ListImages(folder) {
		rel := strings.ToLower(filepath.ToSlash(image.Rel))
		rest := rel[strings.Index(rel, folder)+len(folder):]
		object, file, ok := strings.Cut(rest, "/")
		if ok && strings.TrimSuffix(file, path.Ext(file)) == strings.ToLower(list.Argument) {
			objects = append(objects, object)
		}
	}
	slices.Sort(objects)
	return slices.Compact(objects)
}

// findShown returns the image file a list's binding shows of one definition or folder, or "" if there is none.
func (library *library) findShown(list PictureList, reference schema.Reference) string {
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

// findDefinedPicture returns the picture of a definition that the defines name, and whether they name its folder. It
// returns "" if there is none.
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
	for _, owner := range []string{name, game.GUI.DefaultPicture} {
		if file, ok := library.mods.ResolveTexture(strings.TrimSuffix(folder, "/") + "/" + toFile(owner)); ok {
			return file, true
		}
	}
	return "", true
}

// listPictureDefines returns the folder, prefix and suffix that the defines give a list's pictures. Each is "" if the
// game has no such define.
func (library *library) listPictureDefines(list PictureList) (folder, prefix, suffix string) {
	if list.Field == "" {
		return "", "", ""
	}
	stem := toDefineWords(list.Type) + "_" + strings.ToUpper(list.Field)
	suffixStem := cmp.Or(library.ws.Game.GUISuffixStems[stem], stem)
	group := library.ws.Game.GUIDefineGroup
	return library.lookupDefine(group, stem+game.GUI.FolderDefineSuffix),
		library.lookupDefine(group, stem+game.GUI.PrefixDefineSuffix),
		library.lookupDefine(group, suffixStem+game.GUI.NameDefineSuffix)
}

// findPicture returns the image file of a definition, from its picture keys or an icon name, or "" if there is none.
func (library *library) findPicture(definition clause, icon, preferred string) string {
	for _, key := range game.GUI.PictureKeys {
		if picture, ok := definition.tree.FindChildValue(definition.statement, key); ok && picture != "" {
			return library.findFile(picture, preferred)
		}
	}
	return library.findFile(icon, preferred)
}

// findFirstShown returns the first object a list picks among that the list's binding shows a picture of, with that
// picture. It returns "" for both if none shows one.
func (library *library) findFirstShown(list PictureList) (name, file string) {
	if library.firsts == nil {
		library.firsts = map[PictureList]string{}
	}
	name, known := library.firsts[list]
	if known {
		return name, lo.Ternary(name == "", "", library.findShown(list, schema.Reference{Database: list.Database, Name: name}))
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

// listShown returns the pictures that a list's binding shows of each object it picks among, labeled by the object,
// picking which sets the object.
func (library *library) listShown(list PictureList) []Picture {
	pictures := []Picture{}
	for _, reference := range library.listObjects(list) {
		if file := library.findShown(list, reference); file != "" {
			pictures = append(pictures, Picture{Label: reference.Name, Path: file, Value: reference.Name})
		}
	}
	return lo.UniqBy(pictures, func(picture Picture) string { return picture.Label })
}

// listObjects returns the folders or definitions a list picks among, in order.
func (library *library) listObjects(list PictureList) []schema.Reference {
	if list.Folder != "" {
		return lo.Map(library.listFolderObjects(list), func(name string, _ int) schema.Reference { return schema.Reference{Name: name} })
	}
	sites := library.mods.ListDefinitions(list.Database)
	if !library.canShowMany(list) {
		sites = sites[:min(1, len(sites))]
	}
	return lo.Map(sites, func(site index.Site, _ int) schema.Reference { return library.mods.ToReference(site) })
}

// pickPicture returns what picking a value of a list gives the widget whose list it is: the file picked, for a list
// of one definition's images, else what the widget's binding shows of the definition picked.
func (library *library) pickPicture(list PictureList, picked string) Choice {
	texture := picked
	if list.Name == "" {
		texture = library.findShown(list, schema.Reference{Database: list.Database, Name: picked})
	}
	return Choice{Key: list.Key, Value: picked, Texture: texture}
}

// canShowMany reports whether more than the first definition of a list's database may show its picture.
func (library *library) canShowMany(list PictureList) bool {
	folder, prefix, suffix := library.listPictureDefines(list)
	return library.writesField(list) || library.findIconDefine(list.Type) != "" || folder+prefix+suffix != ""
}

// writesField reports whether the definitions of a list's database write the field its binding reads, as the game's
// own files show.
func (library *library) writesField(list PictureList) bool {
	database := library.ws.Schema.Databases[list.Database]
	return database != nil && database.Definition != nil && database.Definition.Children[list.Field] != nil
}

// lookupDefine returns the value of the define with a key in a group, as the game reads it, or "" if the game has
// none.
func (library *library) lookupDefine(group, key string) string {
	if library.defines == nil {
		library.defines = map[string]string{}
	}
	define := library.ws.Schema.JoinNested(lookupDefineDatabase(), group, key)
	value, known := library.defines[define]
	if !known {
		reference := schema.Reference{Database: lookupDefineDatabase(), Name: define}
		if clause, ok := findKeptClause(library.ws, library.mods, reference); ok {
			value = clause.tree.Unquoted(clause.tree.Statements[clause.statement].ResolveValueSpan())
		}
		library.defines[define] = value
	}
	return value
}

// findFile returns the image file a texture value names as a path or an icon name, or "" if there is none.
func (library *library) findFile(written, preferred string) string {
	if file, ok := library.mods.ResolveTexture(written); ok {
		return file
	}
	if written == "" || strings.Contains(written, "[") {
		return ""
	}
	return library.findIcon(written, preferred)
}

// findIcon returns the image file of an icon name: its file in the first of the icon folders that has one, or the path
// that the folder's overrides file gives the name. It returns "" if no folder has the icon.
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

// findIconDefine returns the define that names the icon folder of a kind of object, which it writes in the singular or
// the plural: GRAPHICAL_CULTURES_ICON_PATH for a graphical culture. It returns "" where no define does.
func (library *library) findIconDefine(kind string) string {
	folders := library.listIconFolders()
	phrase := schema.ToPhrase(kind)
	for _, define := range slices.Sorted(maps.Keys(folders)) {
		stem := strings.ToLower(strings.TrimSuffix(define, game.GUI.IconPathSuffix))
		if game.DescribeDefinition(stem, false) == phrase {
			return define
		}
	}
	return ""
}

// listIconFolders returns the folder that each define of the game's GUI group names as holding icons, by the define's
// key, with no slash at its end. It finds them the first time and keeps them.
func (library *library) listIconFolders() map[string]string {
	if library.folders != nil {
		return library.folders
	}
	library.folders = map[string]string{}
	database, group := lookupDefineDatabase(), library.ws.Game.GUIDefineGroup
	for _, site := range library.mods.ListDefinitions(database) {
		define := library.mods.ToReference(site).Name
		key := library.ws.Schema.SplitNested(database, define)
		_, listed := library.folders[key]
		if listed || library.ws.Schema.JoinNested(database, group, key) != define || !strings.HasSuffix(key, game.GUI.IconPathSuffix) {
			continue
		}
		if folder := library.lookupDefine(group, key); folder != "" {
			library.folders[key] = strings.TrimSuffix(folder, "/")
		}
	}
	return library.folders
}

// findOverride returns the image file that an icon folder's overrides file gives an icon stem. It returns "" if the
// folder has no such file or the file does not name the icon.
func (library *library) findOverride(folder, stem string) string {
	loaded, _, ok := library.mods.FindLoaded(folder + "/" + game.GUI.IconPathFile)
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

// findObjectType returns the data type of the object whose picture a texture binding draws: the type that the step
// before its last leads to. It returns "" for a binding of one step, or one whose steps the schema cannot follow.
func findObjectType(schema *schema.Schema, call jomini.Call) string {
	types := schema.ResolveCallTypes(call, "")
	if len(call.Steps) < 2 || len(types) < len(call.Steps)-1 {
		return ""
	}
	return types[len(call.Steps)-2]
}

// toField returns the field of its object that a data function reads: its name without the word that starts a getter,
// in snake case (GetRealmFrame reads realm_frame). It returns "" for a function that starts with no such word.
func toField(function string) string {
	trimmed := trimGetter(function)
	return lo.Ternary(trimmed == function, "", lo.SnakeCase(trimmed))
}

// trimGetter returns a data function's name without the word that starts a getter, such as Get or Access.
func trimGetter(function string) string {
	for _, getter := range game.GUI.Getters {
		if rest, ok := strings.CutPrefix(function, getter); ok && rest != "" {
			return rest
		}
	}
	return function
}

// toCamel joins lower-case words into the camel case a data function's name is written in: game concept is
// GameConcept.
func toCamel(parts []string) string {
	return strings.Join(lo.Map(parts, func(part string, _ int) string { return lo.Capitalize(part) }), "")
}

// toDefineWords returns a kind of object as the defines write it: GovernmentType and government type are
// GOVERNMENT_TYPE.
func toDefineWords(kind string) string {
	return strings.ToUpper(strings.ReplaceAll(schema.ToPhrase(kind), " ", "_"))
}

// resolveBindings gives a widget the pictures, masks, colors and sample texts its data bindings lead to.
func (resolver *resolver) resolveBindings(window *Window, widget *Widget, context context) {
	schema := resolver.ws.Schema
	for property := range window.listProperties(*widget) {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		if isCall {
			call = context.expand(schema, call)
		}
		isPicture := property.Key == game.GUI.PortraitKey || property.Key == game.GUI.CoatKey ||
			slices.Contains(game.GUI.PictureKeys, property.Key)
		function, _ := schema.FindFunction(call, "", len(call.Steps)-1)
		switch {
		case isPicture && property.Value != "" && widget.Textures[property.Key] == "":
			resolver.resolvePicture(widget, property, call)
		case slices.Contains(game.GUI.MaskKeys, property.Key) && len(call.Steps) > 0 && widget.Textures[property.Key] == "":
			resolver.resolveListedPicture(widget, property.Key, call)
		case function.Returns == game.GUI.ColorType:
			if color := resolver.library.findColor(call); color != "" {
				widget.Colors[property.Key] = color
			}
		}
	}
	for key, text := range widget.Texts {
		widget.Texts[key] = resolver.library.sampleText(text, context, window.listProperties(*widget))
	}
}

// resolvePicture gives a widget the file one of its pictures leads to, else a placeholder shape.
func (resolver *resolver) resolvePicture(widget *Widget, property Property, call jomini.Call) {
	placeholder := resolver.ws.Game.FindPlaceholder(resolver.describePicture(property, call))
	if len(call.Steps) > 0 && !placeholder.Runtime {
		resolver.resolveListedPicture(widget, property.Key, call)
	}
	if widget.Textures[property.Key] == "" && widget.Role != "skin" && widget.Placeholder == "" {
		widget.Placeholder = placeholder.Shape
	}
}

// describePicture returns the words that say what a picture is of, which pick its placeholder.
func (resolver *resolver) describePicture(property Property, call jomini.Call) string {
	if len(call.Steps) == 0 {
		return schema.ToPhrase(property.Key + " " + strings.NewReplacer("/", " ", ".", " ").Replace(property.Value))
	}
	object, last := findObjectType(resolver.ws.Schema, call), trimGetter(call.Steps[len(call.Steps)-1].Text)
	return schema.ToPhrase(property.Key + " " + object + " " + last)
}

// resolveListedPicture gives a widget the picture a binding draws, and the list of pictures it may draw.
func (resolver *resolver) resolveListedPicture(widget *Widget, key string, call jomini.Call) {
	ws, mods, library := resolver.ws, resolver.mods, resolver.library
	list, references := library.makePictureList(key, call)
	for _, reference := range references {
		if file := library.findShown(list, reference); file != "" {
			widget.Textures[key] = file
			return
		}
	}
	isNamed := slices.ContainsFunc(references, func(reference schema.Reference) bool { return reference.Database == list.Database })
	if widget.Pictures != nil || isNamed && list.Database != "" {
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

// listDefinitionImages returns the image files a definition names, the ones it shows on a condition last.
func listDefinitionImages(ws *workspace.Session, mods *index.Index, definition clause) []string {
	if definition.tree == nil {
		return nil
	}
	rules := ws.Game.Illustrations
	folder, ok := definition.tree.FindChildValue(definition.statement, rules.FolderKey)
	if !ok {
		folder = rules.Folder
	}
	var images, conditional []string
	for _, part := range listChildren(definition) {
		_, isConditional := part.tree.FindChild(part.statement, rules.TriggerKey)
		isConditional = isConditional && rules.TriggerKey != ""
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
			case !ok:
			case isConditional:
				conditional = append(conditional, file)
			default:
				images = append(images, file)
			}
		}
	}
	return lo.Uniq(append(images, conditional...))
}

// findColor returns the CSS color a binding of a definition's color leads to, or "" if there is none.
func (library *library) findColor(call jomini.Call) string {
	list, references := library.makePictureList("", call)
	if list.Field == "" {
		return ""
	}
	for _, reference := range references {
		if color := library.findFieldColor(list, reference); color != "" {
			return color
		}
	}
	if slices.ContainsFunc(references, func(reference schema.Reference) bool { return reference.Database == list.Database }) {
		return ""
	}
	return library.findFirstColor(list)
}

// findFirstColor returns the first CSS color a list's definitions write in its field, or "" if there is none.
func (library *library) findFirstColor(list PictureList) string {
	if library.colors == nil {
		library.colors = map[PictureList]string{}
	}
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

// findFieldColor returns the CSS color one definition writes in a list's field, or "" if it writes none.
func (library *library) findFieldColor(list PictureList, reference schema.Reference) string {
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
	reference = schema.Reference{Name: definition.tree.Unquoted(definition.tree.Statements[field].Value)}
	definition, ok = findKeptClause(library.ws, library.mods, reference)
	return lo.Ternary(ok, makeColor(definition), "")
}
