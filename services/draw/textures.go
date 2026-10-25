// textures.go finds what a value shows: the image file that a path, an icon name or a data binding leads to, and the
// shape drawn where none does. One library answers each, from the game's definitions, icon folders and defines.

package draw

import (
	"bytes"
	"image/png"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// A library finds the pictures that the definitions of a session show. It keeps what it looks up as it first needs it:
// the game's icon folders, the defines it has read, and the first definition of each list that shows a picture.
type library struct {
	ws      *workspace.Session
	mods    *index.Index
	folders map[string]string      // the icon folders of the game, by the define that names each
	defines map[string]string      // the defines read so far, by name; "" for one the game lacks
	firsts  map[PictureList]string // the first definition that shows a list's picture, by the list; "" for none
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
	return ""
}

// makePictureList returns what a picture binding under a key reads, as the list of the pictures it may draw, with the
// definitions it names, the object first where it names one. Its last step reads a field of the object the step
// before leads to, passing its quoted argument as written: [GovernmentType.GetRealmFrame('(int32)62')] reads
// realm_frame of a government with the argument 62. Where the step's first words name a kind of object instead, as
// findKind says, it reads the rest of its words of a definition of that kind, which its quoted argument names:
// [GetConceptTexture('tax_base')] reads texture of the concept tax_base, and [GetGoodsIcon(Goods.Self)] reads icon of
// a goods.
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

// findKind returns the kind whose field a data function reads, where the first parts of its name after the getter
// name one, with the field the rest name. The longest parts are tried first, and at least one is left for the field.
// Parts name an icon folder whose subfolders hold the image a quoted argument names
// (GetGraphicalCultureTexture('top_header_bg') reads gfx/interface/graphical_cultures/<culture>/top_header_bg), else a
// kind with a database, as resolveKind says, of which a quoted argument names a definition. It returns false where no
// parts name a kind.
func (library *library) findKind(parts []string, object, argument string) (PictureList, bool) {
	for end := len(parts) - 1; end > 0; end-- {
		kind, field := strings.Join(parts[:end], " "), strings.Join(parts[end:], "_")
		define := library.findIconDefine(kind)
		list := PictureList{Type: kind, Field: field, Argument: argument, Folder: library.listIconFolders()[define]}
		if define != "" && argument != "" && len(library.listFolderObjects(list)) > 0 {
			return list, true
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

// resolveKind returns the kind that some parts of a data function's name name, with the database of its definitions:
// the kind that the type the function is read of and the parts name together (Location and rank are a location rank),
// else the one the parts name, else the kind of the names that the function the parts make takes (Concept takes game
// concepts). It returns false where none is.
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

// findShown returns the image file that a list's binding shows of one definition, or of one folder for a list whose
// objects are folders, where it is the image the argument names. For a definition it is tried in this order: the
// field of the definition that the binding reads (GetIcon reads icon); the file that the defines for the kind and the
// field name, as findDefinedPicture says; the definition's picture by the game's picture keys, else its name as an
// icon where the defines name no folder for the picture; the one image it writes, where it writes exactly one. It
// returns "" if none is found.
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

// findDefinedPicture returns the picture of a definition that the defines for a list's kind and field name, as
// listPictureDefines says: the file named by the prefix, the definition's name and the list's argument joined by an
// underscore, then the suffix. It is in the folder the defines name, where a definition with no file of its own takes
// the folder's default (realm_frames/_default_62_frame for feudal_government), else among the icon folders
// (icon_goods_wheat). The last result is true where the defines name a folder. It returns "" where the defines give
// none of the three, or no such file is.
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

// listPictureDefines returns what the defines for a list's kind and field say of its pictures: <KIND>_<FIELD>_PATH,
// the folder they are in, and <KIND>_<FIELD>_PREFIX and _SUFFIX, the start and the end of each file's name (CK3's
// GOVERNMENT_TYPE_REALM_FRAME_SUFFIX, EU5's GOODS_ICON_PREFIX). Each is "" where the game has no such define, and all
// are for a list that reads no field.
func (library *library) listPictureDefines(list PictureList) (folder, prefix, suffix string) {
	if list.Field == "" {
		return "", "", ""
	}
	stem := toDefineWords(list.Type) + "_" + strings.ToUpper(list.Field)
	return library.lookupDefine(stem + game.GUI.FolderDefineSuffix), library.lookupDefine(stem + game.GUI.PrefixDefineSuffix),
		library.lookupDefine(stem + game.GUI.NameDefineSuffix)
}

// findPicture returns the image file of a definition: the first of the game's picture keys that its block writes, else
// the given icon name, found as a path or an icon name. The folder of the preferred define is searched first for an
// icon. It returns "" if neither leads to a file; an empty icon name leads to none.
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

// listObjects returns the objects a list picks among, in order: the folders that hold its image, for a list whose
// objects are folders, else the definitions of its database, in the order the index lists them, only the first where
// no other can show its picture, as canShowMany says.
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

// canShowMany reports whether more than the first definition of a list's database may show the list's picture: the
// database's definitions write the field the binding reads, the defines for the kind and the field say where its
// pictures are, or the kind has an icon folder.
func (library *library) canShowMany(list PictureList) bool {
	database := library.ws.Schema.Databases[list.Database]
	hasField := database != nil && database.Definition != nil && database.Definition.Children[list.Field] != nil
	folder, prefix, suffix := library.listPictureDefines(list)
	return hasField || library.findIconDefine(list.Type) != "" || folder+prefix+suffix != ""
}

// lookupDefine returns the value of the define with the given name, as the game reads it, or "" if the game has none.
func (library *library) lookupDefine(name string) string {
	if library.defines == nil {
		library.defines = map[string]string{}
	}
	value, known := library.defines[name]
	if !known {
		reference := schema.Reference{Database: lookupDefineDatabase(), Name: name}
		if define, ok := findKeptClause(library.ws, library.mods, reference); ok {
			value = define.tree.Unquoted(define.tree.Statements[define.statement].ResolveValueSpan())
		}
		library.defines[name] = value
	}
	return value
}

// findFile returns the image file that a texture value names: a path, else an icon name or a path below an icon
// folder (modifier_types/is_appointed_as_heir). The folder of the preferred define is searched first for an icon. It
// returns "" if neither names a file the game reads.
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

// listIconFolders returns the folder that each define the game keeps names as holding icons, by the define, with no
// slash at its end. It finds them the first time and keeps them.
func (library *library) listIconFolders() map[string]string {
	if library.folders != nil {
		return library.folders
	}
	library.folders = map[string]string{}
	for _, site := range library.mods.ListDefinitions(lookupDefineDatabase()) {
		name := library.mods.ToReference(site).Name
		if _, listed := library.folders[name]; listed || !strings.HasSuffix(name, game.GUI.IconPathSuffix) {
			continue
		}
		if folder := library.lookupDefine(name); folder != "" {
			library.folders[name] = strings.TrimSuffix(folder, "/")
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

// resolveBindings gives a widget the files that its pictures' bindings lead to, read through the data contexts the
// widget reads in: [Trait.GetIcon] in a widget that reads [GetTrait('brave')] draws brave's icon, as
// resolveListedPicture finds. No file is looked for a picture that only the running game draws, such as a portrait. A
// picture that leads to no file gives its widget the shape of what it is of, as describePicture says, unless the
// widget is a background, which only dresses its holder.
func (resolver *resolver) resolveBindings(widget *Widget, context context) {
	for _, property := range widget.Properties {
		isPicture := property.Key == game.GUI.PortraitKey || property.Key == game.GUI.CoatKey ||
			slices.Contains(game.GUI.PictureKeys, property.Key)
		if !isPicture || property.Value == "" || widget.Textures[property.Key] != "" {
			continue
		}
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		if isCall {
			call = context.reach(resolver.ws.Schema.ResolveMacro(call, context.latest))
		}
		placeholder := resolver.ws.Game.FindPlaceholder(resolver.describePicture(property, call))
		if isCall && !placeholder.Runtime {
			resolver.resolveListedPicture(widget, property.Key, call)
		}
		if widget.Textures[property.Key] == "" && widget.Role != "skin" && widget.Placeholder == "" {
			widget.Placeholder = placeholder.Shape
		}
	}
}

// describePicture returns the phrase that says what a picture is of, which its placeholder is chosen by: its key, the
// object its binding's last step reads, and that step without the getter (Location.GetRankIcon under texture is
// texture location rank icon). A picture written as a path gives the path's folders and name. The binding is the zero
// call for a path.
func (resolver *resolver) describePicture(property Property, call jomini.Call) string {
	if len(call.Steps) == 0 {
		return schema.ToPhrase(property.Key + " " + strings.NewReplacer("/", " ", ".", " ").Replace(property.Value))
	}
	object, last := findObjectType(resolver.ws.Schema, call), trimGetter(call.Steps[len(call.Steps)-1].Text)
	return schema.ToPhrase(property.Key + " " + object + " " + last)
}

// resolveListedPicture gives a widget the picture that a binding under the given key draws, and where the pictures it
// may draw are listed: what the binding shows of a definition it names; else the images of the first definition it
// names that writes several, listed to pick among; else, where it names no object of its kind, what it shows of the
// first definition of that kind that shows one, those definitions listed to pick among; else the default picture of
// the folder the defines name for the kind and the field (disasters/_default). A widget given a list already keeps it.
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
	if file, _ := library.findDefinedPicture(list, ""); file != "" {
		widget.Textures[key] = file
	}
}

// listDefinitionImages returns the image files that a definition names: each value with an image's extension that is a
// file of the workspace as written, else below the folder that the definition names, else below the game's folder of
// illustrations. The images a part of the definition draws only on a condition come after the others, each in the
// order written. It returns none if the definition names no image, or for the zero clause.
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
