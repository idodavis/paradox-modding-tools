package draw

import (
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// defineCall is the data call that reads a define, whose database lists the folders of the game's icons.
const defineCall = "GetDefine"

// resolveTexture finds the image file for a texture as a GUI or coat file writes it: a path under the game's gfx
// folders, an icon name looked up in the given icon folders, or a texture binding, each found in the given index of
// the mods. The icon folders are keyed by the define that names each; with none, an icon name finds nothing. It
// returns "" if no file is found.
func resolveTexture(ws *workspace.Session, mods *index.Index, folders map[string]string, written string) string {
	if call, ok := jomini.ParseBracketed(written, 0); ok {
		return resolveBinding(ws, mods, folders, call)
	}
	return findFile(ws, mods, folders, written, "")
}

// resolveBinding returns the image file of a texture binding: the picture of the first definition that a quoted
// argument of the call names. The icons of the data type that the call's last step belongs to are looked for first.
// It returns "" if no argument names a definition with a picture.
func resolveBinding(ws *workspace.Session, mods *index.Index, folders map[string]string, call jomini.Call) string {
	preferred := ""
	if types := ws.Schema.ResolveCallTypes(call, ""); len(types) >= len(call.Steps)-1 && len(call.Steps) > 1 {
		preferred = strings.ToUpper(lo.SnakeCase(types[len(call.Steps)-2])) + game.GUI.IconPathSuffix
	}
	for _, reference := range ws.Schema.ListCallReferences(call, "") {
		if file := findPicture(ws, mods, folders, reference, preferred); file != "" {
			return file
		}
	}
	return ""
}

// findPicture returns the image file of a definition: the first of the game's picture keys that its block writes, else
// its name, found as a path or an icon name. It returns "" if no definition has the name.
func findPicture(ws *workspace.Session, mods *index.Index, folders map[string]string, reference schema.Reference, preferred string) string {
	definition, ok := findKeptClause(ws, mods, reference)
	if !ok {
		return ""
	}
	for _, key := range game.GUI.PictureKeys {
		if picture, ok := definition.tree.FindChild(definition.statement, key); ok && !definition.tree.Statements[picture].Value.IsEmpty() {
			return findFile(ws, mods, folders, definition.tree.Unquoted(definition.tree.Statements[picture].Value), preferred)
		}
	}
	return findFile(ws, mods, folders, reference.Name, preferred)
}

// findFile returns the image file that a written texture names: a path, else an icon name. The folder of the define
// with the given name is searched first for an icon. It returns "" if neither names a file the game reads.
func findFile(ws *workspace.Session, mods *index.Index, folders map[string]string, texture, preferred string) string {
	if file, ok := mods.ResolveTexture(texture); ok {
		return file
	}
	if texture == "" || strings.ContainsAny(texture, "/[") {
		return ""
	}
	return findIcon(ws, mods, folders, texture, preferred)
}

// findIcon returns the image file of an icon name: its file in the first of the given icon folders that has one, or the
// path that the folder's overrides file gives the name. It returns "" if no folder has the icon.
func findIcon(ws *workspace.Session, mods *index.Index, folders map[string]string, icon, preferred string) string {
	defines := slices.Sorted(maps.Keys(folders))
	if _, ok := folders[preferred]; ok {
		defines = slices.Concat([]string{preferred}, slices.DeleteFunc(defines, func(define string) bool { return define == preferred }))
	}
	stem := strings.TrimSuffix(icon, path.Ext(icon))
	for _, define := range defines {
		if file, ok := mods.ResolveTexture(folders[define] + "/" + icon); ok {
			return file
		}
		if file := findOverride(ws, mods, folders[define], stem); file != "" {
			return file
		}
	}
	return ""
}

// findIconFolders returns the folder that each define the game keeps names as holding icons, with no slash at its end.
func findIconFolders(ws *workspace.Session, mods *index.Index) map[string]string {
	folders := map[string]string{}
	for _, winner := range mods.ListKept(ws.Schema, game.GUI.Answered[defineCall]) {
		name := mods.ToReference(winner.Site).Name
		if !strings.HasSuffix(name, game.GUI.IconPathSuffix) {
			continue
		}
		if define, ok := makeClause(ws, mods, winner); ok {
			folders[name] = strings.TrimSuffix(define.tree.Unquoted(define.tree.Statements[define.statement].Value), "/")
		}
	}
	return folders
}

// findOverride returns the image file that an icon folder's overrides file gives an icon stem. It returns "" if the
// folder has no such file or the file does not name the icon.
func findOverride(ws *workspace.Session, mods *index.Index, folder, stem string) string {
	loaded, _, ok := mods.FindLoaded(folder + "/" + game.GUI.IconPathFile)
	if !ok {
		return ""
	}
	tree, err := ws.LoadTree(loaded.Path)
	if err != nil {
		return ""
	}
	for statement := range tree.Children(0) {
		if current := tree.Statements[statement]; tree.Unquoted(current.Key) == stem {
			file, _ := mods.ResolveTexture(tree.Unquoted(current.Value))
			return file
		}
	}
	return ""
}
