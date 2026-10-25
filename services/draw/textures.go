// textures.go finds what a value shows: the image file that a path, an icon name or a data binding leads to. One
// library answers each, from the game's definitions, icon folders and defines.

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

// makePictureList returns what a data binding under a key reads, as the list of the pictures it may draw: the kind of
// object it reads, the database of that kind, the field it reads and the argument it passes. A binding of two steps or
// more reads its last step of the object the step before leads to: [GovernmentType.GetRealmFrame('(int32)62')] reads
// realm_frame of a government with the argument 62. A binding of one step named for a kind and a field reads that
// field of the object its quoted argument names: [GetConceptTexture('tax_base')] reads texture of the concept
// tax_base. It also returns the definitions the binding names, the object first where it names one.
func (library *library) makePictureList(key string, call jomini.Call) (PictureList, []schema.Reference) {
	last := call.Steps[len(call.Steps)-1]
	references := library.ws.Schema.ListCallReferences(call, "")
	list := PictureList{Key: key, Type: findObjectType(library.ws.Schema, call), Field: toField(last.Text)}
	if len(last.Arguments) > 0 && last.Arguments[0].Quoted {
		list.Argument = toWrittenValue(last.Arguments[0].Text)
	}
	if split, ok := library.splitFunction(last); list.Type == "" && len(call.Steps) == 1 && ok {
		split.Key = key
		if argument := last.Arguments[0]; argument.Quoted {
			references = append([]schema.Reference{{Database: split.Database, Name: argument.Text}}, references...)
		}
		return split, references
	}
	list.Database, _ = library.ws.Schema.ResolveTypeDatabase(list.Type)
	return list, references
}

// splitFunction splits the name of a data function into a kind of object and the field it reads of one, by its first
// argument. A data call there gives the kind by its type, whose words or whose database's begin the name:
// GetGoodsIcon(Goods.Self) reads icon of a goods. A quoted name there must name a definition of the kind's database,
// the kind tried longest first and, where it names no type's database, looked up among the functions whose quoted
// arguments are names: GetConceptTexture('tax_base') reads texture of a concept. It returns the list's kind, database
// and field, and false where no split fits.
func (library *library) splitFunction(step jomini.Step) (PictureList, bool) {
	words := strings.Fields(schema.ToPhrase(trimGetter(step.Text)))
	if len(step.Arguments) == 0 || len(words) < 2 {
		return PictureList{}, false
	}
	if argument := step.Arguments[0].Call; argument != nil {
		return library.splitByType(words, *argument)
	}
	if !step.Arguments[0].Quoted {
		return PictureList{}, false
	}
	if list, ok := library.splitByFolder(words, step.Arguments[0].Text); ok {
		return list, true
	}
	for end := len(words) - 1; end > 0; end-- {
		kind := strings.Join(words[:end], " ")
		database, ok := library.ws.Schema.ResolveTypeDatabase(kind)
		if targets := game.GUI.CallTargets[toCamel(words[:end])]; !ok && len(targets) > 0 {
			database, ok = targets[0], true
		}
		_, defined := findKeptClause(library.ws, library.mods, schema.Reference{Database: database, Name: step.Arguments[0].Text})
		if ok && defined {
			kind = game.DescribeDefinition(database, false)
			return PictureList{Type: kind, Database: database, Field: strings.Join(words[end:], "_")}, true
		}
	}
	return PictureList{}, false
}

// splitByFolder splits the words of a data function's name by an icon folder whose define, in the singular, names the
// kind they begin with, where that folder's subfolders hold the image the function's quoted argument names:
// GetGraphicalCultureTexture('top_header_bg') reads a graphical culture's top_header_bg, in
// gfx/interface/graphical_cultures/<culture>. It returns false where no such folder holds the image.
func (library *library) splitByFolder(words []string, argument string) (PictureList, bool) {
	folders := library.listIconFolders()
	for _, define := range slices.Sorted(maps.Keys(folders)) {
		folder, stem := folders[define], strings.TrimSuffix(define, game.GUI.IconPathSuffix)
		kind := game.DescribeDefinition(strings.ToLower(stem), false)
		length := len(strings.Fields(kind))
		if length >= len(words) || strings.Join(words[:length], " ") != kind {
			continue
		}
		list := PictureList{Type: kind, Field: strings.Join(words[length:], "_"), Argument: argument, Folder: folder}
		if len(library.listFolderObjects(list)) > 0 {
			return list, true
		}
	}
	return PictureList{}, false
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

// splitByType splits the words of a data function's name by the type of the data call it is given: the words that
// begin the name and are the type's, or the singular of the type's database, either without a last word type, are the
// kind, and the rest the field (GetBuildingIcon(Building.GetType) reads icon of a building type). It returns false where
// the call's type reaches no database or no such words begin the name.
func (library *library) splitByType(words []string, argument jomini.Call) (PictureList, bool) {
	types := library.ws.Schema.ResolveCallTypes(argument, "")
	if len(types) < len(argument.Steps) {
		return PictureList{}, false
	}
	dataType := types[len(argument.Steps)-1]
	database, ok := library.ws.Schema.ResolveTypeDatabase(dataType)
	if !ok {
		return PictureList{}, false
	}
	phrase, singular := schema.ToPhrase(dataType), game.DescribeDefinition(database, false)
	kinds := []string{phrase, singular, strings.TrimSuffix(phrase, " type"), strings.TrimSuffix(singular, " type")}
	for _, kind := range lo.Uniq(kinds) {
		length := len(strings.Fields(kind))
		if length < len(words) && strings.Join(words[:length], " ") == kind {
			return PictureList{Type: dataType, Database: database, Field: strings.Join(words[length:], "_")}, true
		}
	}
	return PictureList{}, false
}

// findShown returns the image file that a list's binding shows of one definition, or of one folder for a list whose
// objects are folders, where it is the image the argument names. For a definition it is tried in this order: the field of
// the definition that the binding reads (GetIcon reads icon); the folder that a define names for the kind and the field,
// as findFolderPicture says; the definition's picture by the game's picture keys, else its name as an icon; the one
// image it writes, where it writes exactly one. It returns "" if none is found.
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
	if file := library.findFolderPicture(list, reference.Name); file != "" {
		return file
	}
	if file := library.findPicture(definition, reference.Name, toDefineWords(list.Type)+game.GUI.IconPathSuffix); file != "" {
		return file
	}
	if images := listDefinitionImages(library.ws, library.mods, definition); len(images) == 1 {
		return images[0]
	}
	return ""
}

// findFolderPicture returns the picture of a definition in the folder that a define names for a list's kind and field:
// <KIND>_<FIELD>_PATH, the file named by the definition's name, the list's argument and the <KIND>_<FIELD>_SUFFIX
// define, the first two joined by an underscore (gfx/interface/icons/realm_frames/feudal_government_62_frame.dds). It
// returns "" if the game has no such define or no such file.
func (library *library) findFolderPicture(list PictureList, name string) string {
	stem := toDefineWords(list.Type) + "_" + strings.ToUpper(list.Field)
	folder := library.lookupDefine(stem + game.GUI.FolderDefineSuffix)
	if folder == "" || list.Field == "" {
		return ""
	}
	parts := lo.Compact([]string{name, list.Argument})
	file, _ := library.mods.ResolveTexture(strings.TrimSuffix(folder, "/") + "/" + strings.Join(parts, "_") +
		library.lookupDefine(stem+game.GUI.NameDefineSuffix))
	return file
}

// findPicture returns the image file of a definition: the first of the game's picture keys that its block writes, else
// its name, found as a path or an icon name. The folder of the preferred define is searched first for an icon. It
// returns "" if neither leads to a file.
func (library *library) findPicture(definition clause, name, preferred string) string {
	for _, key := range game.GUI.PictureKeys {
		if picture, ok := definition.tree.FindChildValue(definition.statement, key); ok && picture != "" {
			return library.findFile(picture, preferred)
		}
	}
	return library.findFile(name, preferred)
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
// database's definitions write the field the binding reads, a define names a folder for the kind and the field, or the
// kind has an icon folder.
func (library *library) canShowMany(list PictureList) bool {
	database := library.ws.Schema.Databases[list.Database]
	hasField := database != nil && database.Definition != nil && database.Definition.Children[list.Field] != nil
	stem := toDefineWords(list.Type)
	_, hasIcons := library.listIconFolders()[stem+game.GUI.IconPathSuffix]
	hasFolder := list.Field != "" && library.lookupDefine(stem+"_"+strings.ToUpper(list.Field)+game.GUI.FolderDefineSuffix) != ""
	return hasField || hasFolder || hasIcons
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

// findFile returns the image file that a texture value names: a path, else an icon name or a path below an icon folder
// (modifier_types/is_appointed_as_heir). The folder of the preferred define is searched first for an icon. It returns ""
// if neither names a file the game reads.
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
func toCamel(words []string) string {
	return strings.Join(lo.Map(words, func(word string, _ int) string { return lo.Capitalize(word) }), "")
}

// toDefineWords returns a kind of object as the defines write it: GovernmentType and government type are
// GOVERNMENT_TYPE.
func toDefineWords(kind string) string {
	return strings.ToUpper(strings.ReplaceAll(schema.ToPhrase(kind), " ", "_"))
}

// resolveBindings gives a widget the textures of its picture bindings that name no definition as written, read
// through the data contexts the widget reads in: [Trait.GetIcon] in a widget that reads [GetTrait('brave')] draws
// brave's icon. A portrait with no file draws the game's silhouette, and a picture binding that still leads to no
// file draws the game's placeholder for the data type of its object, if the game has one, else what
// resolveListedPicture finds.
func (resolver *resolver) resolveBindings(widget *Widget, context context) {
	for _, property := range widget.Properties {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		_, hasTexture := widget.Textures[property.Key]
		isPicture := property.Key == game.GUI.CoatKey || slices.Contains(game.GUI.PictureKeys, property.Key)
		if hasTexture || !isCall && property.Key != game.GUI.PortraitKey {
			continue
		}
		call = context.reach(resolver.ws.Schema.ResolveMacro(call, context.latest))
		placeholder, hasPlaceholder := resolver.ws.Game.Placeholders[findObjectType(resolver.ws.Schema, call)]
		switch {
		case property.Key == game.GUI.PortraitKey:
			widget.Placeholder = resolver.ws.Game.Portrait
		case !isPicture:
		case hasPlaceholder:
			widget.Placeholder = placeholder
		default:
			resolver.resolveListedPicture(widget, property.Key, call)
		}
	}
}

// resolveListedPicture gives a widget the picture that a binding under the given key draws, and where the pictures it
// may draw are listed: what the binding shows of a definition it names; else the images of the first definition it
// names that writes several, listed to pick among; else, where it names no object of its kind, what it shows of the
// first definition of that kind that shows one, those definitions listed to pick among. A widget given a list already
// keeps it.
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
	if list.Database == "" && list.Folder == "" {
		return
	}
	if name, file := library.findFirstShown(list); file != "" {
		widget.Textures[key] = file
		list.Shown = name
		widget.Pictures = &list
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
