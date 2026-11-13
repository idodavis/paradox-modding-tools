// fonts.go finds the files of the fonts a window's texts are written in.

package draw

import (
	"slices"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// Font is one style of a font a window's texts are written in, with the file
// the game loads it from.
type Font struct {
	Name string `json:"name"`

	// Style is the style in the font file's words, such as bold|italic.
	Style string `json:"style"`

	Path string `json:"path"`
}

// The keys of a font file. A font has a block per style, which names a
// definition of files. That definition has a group per set of languages,
// each listing the files those languages load, the font's own first.
const (
	// fontSizeKey sets the size of a widget's text.
	fontSizeKey = "fontsize"

	// fontTextType is the built-in type of a text. The game's own type of
	// that name gives a text that sets no font its font and size.
	fontTextType = "textbox"

	fontStyleKey      = "fontstyle"
	fontStyleValueKey = "style"
	fontFilesKey      = "fontfiles"
	fontFilesDatabase = "font.fontfiles"
	fontGroupKey      = "group"
	fontLanguagesKey  = "languages"
	fontPathsKey      = "files"
)

// findFonts returns every style of each font the window's widgets name, and
// of the font a text that sets none is drawn in, each with its file. A font
// the game does not define, and a style with no file, are left out.
func findFonts(ws *workspace.Session, mods *index.Index, window *Window) []Font {
	var labels []string
	for _, widget := range window.Widgets {
		labels = appendNew(labels, findProperty(window.listProperties(widget), lang.GUIFontKey).Value)
	}

	labels = appendNew(labels, window.TextFont)
	fonts := []Font{}
	for _, label := range labels {
		definition, ok := findKeptClause(ws, mods, lang.Reference{Database: lang.GUIFontDatabase, Name: label})
		if !ok {
			continue
		}

		for _, block := range listChildren(definition) {
			key, _, _ := splitField(block)
			style, _ := block.tree.FindChildValue(block.statement, fontStyleValueKey)
			files, _ := block.tree.FindChildValue(block.statement, fontFilesKey)
			if path := findFontFile(ws, mods, files); key == fontStyleKey && path != "" {
				fonts = append(fonts, Font{Name: label, Style: style, Path: path})
			}
		}
	}

	return fonts
}

// findFontFile returns the file a definition of a font's files gives the
// workspace's language, else the base language, else its first group. It
// returns "" if the game does not define the files or no such file exists.
func findFontFile(ws *workspace.Session, mods *index.Index, label string) string {
	definition, ok := findKeptClause(ws, mods, lang.Reference{Database: fontFilesDatabase, Name: label})
	if !ok {
		return ""
	}

	groups := slices.DeleteFunc(listChildren(definition), func(field clause) bool {
		key, _, _ := splitField(field)

		return key != fontGroupKey
	})
	if len(groups) == 0 {
		return ""
	}

	position := 0
	for _, language := range []string{ws.Language, loc.BaseLanguage} {
		found := slices.IndexFunc(groups, func(group clause) bool {
			return slices.Contains(listValues(group, fontLanguagesKey), loc.ToLanguageKey(language))
		})
		if found >= 0 {
			position = found

			break
		}
	}

	paths := listValues(groups[position], fontPathsKey)
	if len(paths) == 0 {
		return ""
	}

	if loaded, _, isLoaded := mods.FindLoaded(paths[0]); isLoaded {
		return loaded.Path
	}

	return ""
}

// listValues returns the values written alone in the block a clause holds
// under a key, or none if it holds no such block.
func listValues(holder clause, key string) []string {
	field, ok := findField(listChildren(holder), key)
	if !ok {
		return nil
	}

	var values []string
	for _, inner := range listChildren(field) {
		if current := inner.tree.Statements[inner.statement]; current.IsListed() {
			values = append(values, inner.tree.Unquoted(current.Value))
		}
	}

	return values
}
