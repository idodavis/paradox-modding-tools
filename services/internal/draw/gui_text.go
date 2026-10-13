// gui_text.go is a widget's text: the loc entry or literal it holds, its markup read into runs with the formats'
// colors and the text icons' textures, and the font files the page loads to draw them; the browser measures the words
// and breaks the lines.

package draw

import (
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
)

// The databases the text reads: the fonts and their files, the named formats and the text icons.
const (
	fontDatabase     = "font.font"
	fontFileDatabase = "font.fontfiles"
	formatDatabase   = "gui.format"
	textIconDatabase = "gui.texticon"
)

// maxNesting is how deep formats name one another and loc keys interpolate one another before a cycle is assumed.
const maxNesting = 10

// formatKeys are the words of a gui.format entry's format string the engine reads; any other word names another
// format, but for the words that are drawn as plain text.
var (
	formatKeys = []string{colorKey, "bold", "italic"}
	plainKeys  = []string{"size", "glow_color", "glow_offset", "shadow", "underline", fontKey}
)

// style is what the runs of a text are drawn in: the formats' effect on top of the widget's own font.
type style struct {
	bold, italic bool
	color        string
}

// text resolves a widget's text: the value's loc entry in the request's language else english (a key), the value
// itself (a literal), its markup as runs, in the font and at the size written, each style the runs use listed for the
// page to load. A text with no font size, or a font with no file, is listed and drawn as none.
func (resolver *resolver) text(node *widget) *Text {
	value, has := node.scalar("text")
	raw := false
	if !has {
		value, has = node.scalar("raw_text")
		raw = true
	}
	if !has {
		return nil
	}
	pixels, sized := resolver.number(node, "fontsize")
	if !sized {
		pixels, sized = resolver.number(resolver.fallback, "fontsize")
	}
	if !sized {
		resolver.missing.add("fontsize", node.kind)
		return nil
	}
	regular := resolver.fontOf(node, "regular")
	if regular == nil {
		return nil
	}
	base := style{}
	if color, isColor := resolver.color(node, "fontcolor"); isColor {
		base.color = color
	}
	runs := resolver.runs(resolver.valueText(value, raw), base, 0)
	for _, run := range runs {
		switch {
		case run.Bold:
			resolver.fontOf(node, "bold")
		case run.Italic:
			resolver.fontOf(node, "italic")
		}
	}
	return &Text{Font: regular.Name, Size: int(pixels), Color: base.color, Runs: runs}
}

// valueText is the text a widget's text value stands for: a key's loc entry in the request's language, else the value
// as written, which a binding is too, since only the game answers it; a raw text is its value as written.
func (resolver *resolver) valueText(value string, raw bool) string {
	if raw || !loc.LooksLikeKey(value) || len(jomini.Calls(value, 0)) > 0 {
		return value
	}
	if entry, known := resolver.ws.Loc(value, resolver.request.Language); known {
		return entry.Text()
	}
	resolver.missing.add("loc", value)
	return value
}

// runs turns a text's markup into runs: a format's words resolved through the gui.format entries, an icon's
// texture through the gui.texticon entries, a $key$'s text through the loc entries, a [call] as written; an
// interpolation nested too deep is dropped.
func (resolver *resolver) runs(value string, base style, depth int) []Run {
	var runs []Run
	for _, segment := range loc.Segments(value) {
		current := base
		current.bold, current.italic = current.bold || segment.Bold, current.italic || segment.Italic
		if segment.Format != "" {
			current = resolver.format(segment.Format, current, 0)
		}
		run := Run{Bold: current.bold, Italic: current.italic, Color: current.color}
		switch {
		case segment.Icon != "":
			run.Icon = resolver.icon(segment.Icon)
			runs = append(runs, run)
		case segment.Key != "" && depth < maxNesting:
			runs = append(runs, resolver.runs(resolver.valueText(segment.Key, false), current, depth+1)...)
		case segment.Call != "":
			run.Text = "[" + segment.Call + "]"
			runs = append(runs, run)
		case segment.Text != "":
			run.Text = segment.Text
			runs = append(runs, run)
		}
	}
	return runs
}

// textDefault is the install's textbox type as a widget whose statements are its chain's fields: the font and size a
// text with none of its own is drawn in.
func (resolver *resolver) textDefault() *widget {
	_, blocks := resolver.chain("textbox")
	holder := &widget{}
	for _, block := range blocks {
		holder.statements = append(holder.statements, fieldStatements(block)...)
	}
	return holder
}

// format applies a named format's words to a style: its color, bold and italic, and the formats it names in turn; a
// name no entry has is drawn plain and listed.
func (resolver *resolver) format(name string, current style, depth int) style {
	definition, defined := resolver.entryFields(formatDatabase, name)
	if !defined || depth >= maxNesting {
		resolver.missing.add("format", name)
		return current
	}
	text, _ := definition.scalar("format")
	for piece := range strings.SplitSeq(text, ";") {
		key, value, _ := strings.Cut(strings.TrimSpace(piece), ":")
		switch {
		case key == formatKeys[0]:
			current.color = colorOf(value, current.color)
		case key == formatKeys[1]:
			current.bold = true
		case key == formatKeys[2]:
			current.italic = true
		case key != "" && !slices.Contains(plainKeys, key):
			current = resolver.format(key, current, depth+1)
		}
	}
	return current
}

// colorOf is the CSS color of a format's {r, g, b} or {r, g, b, a}; the one given where it reads as none.
func colorOf(value, fallback string) string {
	var parts []float64
	for part := range strings.FieldsFuncSeq(strings.Trim(strings.TrimSpace(value), "{}"), func(char rune) bool {
		return char == ',' || char == ' '
	}) {
		number, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return fallback
		}
		parts = append(parts, number)
	}
	if color, known := game.ColorOf("", parts); known {
		return color.CSS()
	}
	return fallback
}

// color is a widget's color key (fontcolor = { 0.61 0.6 0.56 1.0 }) as a CSS color.
func (resolver *resolver) color(node *widget, key string) (string, bool) {
	latest, has := node.last(key)
	if !has {
		return "", false
	}
	tag, parts, isColor := jomini.ColorParts(latest.assign.Value)
	if color, known := game.ColorOf(tag, parts); isColor && known {
		return color.CSS(), true
	}
	return "", false
}

// icon is the URL of the texture a text icon is drawn from; "" where no entry names it, which is listed.
func (resolver *resolver) icon(name string) string {
	definition, defined := resolver.entryFields(textIconDatabase, name)
	if defined {
		for _, assignment := range definition.statements {
			if !strings.EqualFold(assignment.assign.Key.Text, "iconsize") {
				continue
			}
			texture := jomini.ScalarOf(jomini.BlockOf(assignment.assign.Value), "texture")
			if file, listed := resolver.file(texture); listed {
				return fileURL(file)
			}
		}
	}
	resolver.missing.add("texticon", name)
	return ""
}

// file is the file a scalar's text names across the indexes; false where it is no scalar or no file is there.
func (resolver *resolver) file(scalar *jomini.Scalar) (inventory.File, bool) {
	if scalar == nil {
		return inventory.File{}, false
	}
	return index.FileAt(resolver.ws.Indexes(), scalar.Text)
}

// entryFields is the winning entry a database names, as a widget whose statements are the fields of its block;
// false for none.
func (resolver *resolver) entryFields(database, name string) (*widget, bool) {
	defs := index.Lookup(resolver.ws.Indexes(), schema.Defines, database, name)
	winner, _, _, _ := resolver.ws.Winner(defs)
	if !slices.Contains(defs, winner) {
		return nil, false
	}
	holder, isStatement := resolver.definition(winner, name)
	if !isStatement {
		return nil, false
	}
	return &widget{statements: fieldStatements(holder)}, true
}

// fontOf is the face a widget names (font, else the install's first font entry) for a style: the font entry's
// fontstyle for the style, its fontfiles entry's group for the language, listed once per render for the page to load;
// a font with no file goes to Missing and is nil.
func (resolver *resolver) fontOf(node *widget, fontStyle string) *Font {
	name, hasFont := node.scalar(fontKey)
	if !hasFont {
		name, hasFont = resolver.fallback.scalar(fontKey)
	}
	if !hasFont {
		names := index.Names(resolver.ws.Indexes(), fontDatabase)
		if len(names) == 0 {
			resolver.missing.add(fontKey, "no font entry")
			return nil
		}
		name = names[0]
	}
	key := name + "/" + fontStyle
	if found, cached := resolver.fonts[key]; cached {
		return found
	}
	found := resolver.fontFile(name, fontStyle)
	resolver.fonts[key] = found
	if found != nil && !slices.Contains(resolver.list.Fonts, *found) {
		resolver.list.Fonts = append(resolver.list.Fonts, *found)
	}
	return found
}

// fontFile is a font's style as the page loads it: the first file of its fontfiles entry's group for the request's
// language that the install has; nil, listed, where there is none.
func (resolver *resolver) fontFile(name, fontStyle string) *Font {
	definition, defined := resolver.entryFields(fontDatabase, name)
	if !defined {
		resolver.missing.add(fontKey, name)
		return nil
	}
	groups, resolved := resolver.styleFiles(definition, fontStyle)
	for _, rel := range resolver.languageGroup(groups) {
		if file, listed := index.FileAt(resolver.ws.Indexes(), rel); listed {
			return &Font{Name: name, Style: resolved, URL: fileURL(file)}
		}
	}
	resolver.missing.add(fontKey, name)
	return nil
}

// fileGroup is the files a fontfiles entry lists for some languages.
type fileGroup struct {
	languages []string
	files     []string
}

// styleFiles is the groups of the fontfiles entry a font names for a style, regular where it names none for it, and
// the style taken.
func (resolver *resolver) styleFiles(definition *widget, fontStyle string) ([]fileGroup, string) {
	byStyle := map[string]string{}
	for _, assignment := range definition.statements {
		if !strings.EqualFold(assignment.assign.Key.Text, "fontstyle") {
			continue
		}
		block := jomini.ChildBlock(assignment.assign)
		fontStyle, fontFiles := jomini.ScalarOf(block, "style"), jomini.ScalarOf(block, "fontfiles")
		if fontStyle != nil && fontFiles != nil {
			byStyle[strings.ToLower(fontStyle.Text)] = fontFiles.Text
		}
	}
	resolved := fontStyle
	if _, has := byStyle[resolved]; !has {
		resolved = "regular"
	}
	files, listed := resolver.entryFields(fontFileDatabase, byStyle[resolved])
	if !listed {
		return nil, resolved
	}
	var groups []fileGroup
	for _, assignment := range files.statements {
		if strings.EqualFold(assignment.assign.Key.Text, "group") {
			block := jomini.ChildBlock(assignment.assign)
			groups = append(groups, fileGroup{languages: listOf(block, "languages"), files: listOf(block, "files")})
		}
	}
	return groups, resolved
}

// listOf is the quoted words a block's key lists ({ "l_english" "l_french" }).
func listOf(block *jomini.Block, key string) []string {
	for _, field := range jomini.Fields(block) {
		if strings.EqualFold(field.Key.Text, key) {
			return valuesOf(field.Value)
		}
	}
	return nil
}

// languageGroup is the files of the group whose languages hold the request's language, else the base language.
func (resolver *resolver) languageGroup(groups []fileGroup) []string {
	for _, language := range []string{resolver.request.Language, game.BaseLanguage} {
		for _, group := range groups {
			if slices.Contains(group.languages, "l_"+language) {
				return group.files
			}
		}
	}
	return nil
}

// fileURL is the route's URL for a file, which names when the file changed so a changed file is read again.
func fileURL(file inventory.File) string {
	modified := "0"
	if stat, err := os.Stat(file.Path); err == nil {
		modified = strconv.FormatInt(stat.ModTime().UnixMilli(), 10)
	}
	return FileRoute + "?path=" + url.QueryEscape(file.Path) + "&v=" + modified
}
