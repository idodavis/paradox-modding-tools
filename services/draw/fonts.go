// fonts.go finds the files of the fonts a window's texts are written in.

package draw

import (
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// A Font is one style of a font a window's texts are written in, with the file the game reads it from.
type Font struct {
	Name  string `json:"name"`
	Style string `json:"style"` // the style's words: regular, bold, italic or bold|italic
	Path  string `json:"path"`
}

// The font keys say how a font file names a font's files: a block for each
// style names a definition of files, which has groups of languages, each with
// the files they read, the font's own file first.
//
// CK3 and Vic3 fonts/fonts.font, EU5 in_game/fonts/in_game_fonts.font
const (
	// fontSizeKey is the key that sets the size of a widget's text.
	fontSizeKey = "fontsize"

	// fontTextType is the built-in type of a text, whose own type in the
	// game's files gives a text with no font its font and size.
	fontTextType = "textbox"

	// fontStyleKey is the key of a font's block for one of its styles, and
	// fontStyleValueKey the key in that block whose value is the style, such
	// as regular or bold|italic.
	fontStyleKey      = "fontstyle"
	fontStyleValueKey = "style"

	// fontFilesKey is the key in that block that names the definition of the
	// style's files, and fontFilesDatabase the database of those definitions.
	fontFilesKey      = "fontfiles"
	fontFilesDatabase = "font.fontfiles"

	// fontGroupKey is the key of a definition's block for a group of
	// languages, and fontLanguagesKey and fontPathsKey the keys in a group of
	// its languages and of its files.
	fontGroupKey     = "group"
	fontLanguagesKey = "languages"
	fontPathsKey     = "files"
)

// findFonts returns every style of each font the widgets of a window name, and of the font a text with none of its own
// is drawn in, each with its file. A font the game does not define and a style with no file are left out.
func findFonts(ws *workspace.Session, mods *index.Index, window *Window) []Font {
	labels := lo.Uniq(lo.Compact(append(lo.Map(window.Widgets, func(widget Widget, _ int) string {
		return findProperty(window.listProperties(widget), lang.GUIFontKey).Value
	}), window.TextFont)))
	fonts := []Font{}
	for _, label := range labels {
		definition, ok := findKeptClause(ws, mods, lang.Reference{Database: lang.GUIFontDatabase, Name: label})
		if !ok {
			continue
		}
		for _, style := range listChildren(definition) {
			key, _, _ := splitField(style)
			words, _ := style.tree.FindChildValue(style.statement, fontStyleValueKey)
			files, _ := style.tree.FindChildValue(style.statement, fontFilesKey)
			if path := findFontFile(ws, mods, files); key == fontStyleKey && path != "" {
				fonts = append(fonts, Font{Name: label, Style: words, Path: path})
			}
		}
	}
	return fonts
}

// findFontFile returns the file a definition of a font's files gives the session's language, else the base language,
// else the first group. It returns "" if the game does not define the files or the workspace has no such file.
func findFontFile(ws *workspace.Session, mods *index.Index, label string) string {
	definition, ok := findKeptClause(ws, mods, lang.Reference{Database: fontFilesDatabase, Name: label})
	if !ok {
		return ""
	}
	groups := lo.Filter(listChildren(definition), func(field clause, _ int) bool {
		key, _, _ := splitField(field)
		return key == fontGroupKey
	})
	if len(groups) == 0 {
		return ""
	}
	position := -1
	for _, language := range []string{ws.Language, loc.BaseLanguage} {
		if position < 0 {
			position = slices.IndexFunc(groups, func(group clause) bool {
				return slices.Contains(listValues(group, fontLanguagesKey), loc.ToLanguageKey(language))
			})
		}
	}
	paths := listValues(groups[max(position, 0)], fontPathsKey)
	if len(paths) == 0 {
		return ""
	}
	loaded, _, isLoaded := mods.FindLoaded(paths[0])
	return lo.Ternary(isLoaded, loaded.Path, "")
}

// listValues returns the values written inside the block a clause holds under the given key, or none if it holds no
// such block.
func listValues(holder clause, key string) []string {
	field, ok := findField(listChildren(holder), key)
	if !ok {
		return nil
	}
	return lo.FilterMap(listChildren(field), func(inner clause, _ int) (string, bool) {
		current := inner.tree.Statements[inner.statement]
		return inner.tree.Unquoted(current.Value), current.IsListed()
	})
}
