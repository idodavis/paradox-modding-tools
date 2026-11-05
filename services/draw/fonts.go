// fonts.go finds the files of the fonts a window's texts are written in.

package draw

import (
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Font is one style of a font a window's texts are written in, with the file the game reads it from.
type Font struct {
	Name  string `json:"name"`
	Style string `json:"style"` // the style's words: regular, bold, italic or bold|italic
	Path  string `json:"path"`
}

// findFonts returns every style of each font the widgets of a window name, and of the font a text with none of its own
// is drawn in, each with its file. A font the game does not define and a style with no file are left out.
func findFonts(ws *workspace.Session, mods *index.Index, window *Window) []Font {
	rules := game.GUI.Fonts
	labels := lo.Uniq(lo.Compact(append(lo.Map(window.Widgets, func(widget Widget, _ int) string {
		return findProperty(window.listProperties(widget), rules.Key).Value
	}), window.TextFont)))
	fonts := []Font{}
	for _, label := range labels {
		definition, ok := findKeptClause(ws, mods, schema.Reference{Database: rules.Database, Name: label})
		if !ok {
			continue
		}
		for _, style := range listChildren(definition) {
			key, _, _ := splitField(style)
			words, _ := style.tree.FindChildValue(style.statement, rules.StyleValueKey)
			files, _ := style.tree.FindChildValue(style.statement, rules.FilesKey)
			if path := findFontFile(ws, mods, files); key == rules.StyleKey && path != "" {
				fonts = append(fonts, Font{Name: label, Style: words, Path: path})
			}
		}
	}
	return fonts
}

// findFontFile returns the file a definition of a font's files gives the session's language, else the base language,
// else the first group. It returns "" if the game does not define the files or the workspace has no such file.
func findFontFile(ws *workspace.Session, mods *index.Index, label string) string {
	rules := game.GUI.Fonts
	definition, ok := findKeptClause(ws, mods, schema.Reference{Database: rules.FilesDatabase, Name: label})
	if !ok {
		return ""
	}
	groups := lo.Filter(listChildren(definition), func(field clause, _ int) bool {
		key, _, _ := splitField(field)
		return key == rules.GroupKey
	})
	if len(groups) == 0 {
		return ""
	}
	position := -1
	for _, language := range []string{ws.Language, game.BaseLanguage} {
		if position < 0 {
			position = slices.IndexFunc(groups, func(group clause) bool {
				return slices.Contains(listValues(group, rules.LanguagesKey), loc.ToLanguageKey(language))
			})
		}
	}
	paths := listValues(groups[max(position, 0)], rules.PathsKey)
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
