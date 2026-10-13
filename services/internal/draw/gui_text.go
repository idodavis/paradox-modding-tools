// gui_text.go is a widget's text: the loc entry or literal it holds, its markup read into runs with the formats'
// colors and the text icons' textures, each word measured with the real font files; the lines are broken where the
// widget is placed.

package draw

import (
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"

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

// face is one opened font at one size: the row the page registers, and the files of the language's group in order,
// a glyph being measured in the first that has it.
type face struct {
	font  Font
	files []font.Face
}

// style is what the runs of a text are drawn in: the formats' effect on top of the widget's own font.
type style struct {
	bold, italic bool
	color        string
}

// piece is one word of a text with the spaces after it, or a line break, and how wide it measures.
type piece struct {
	Run
	advance float64
}

// text resolves a widget's text: the value's loc entry in the request's language else english (a key), the value
// itself (a literal), its markup as runs, measured word by word; the lines are broken where the widget is placed
// (gui_layout.go's linesAt). A text with no font size, or a font nothing opens, is listed and drawn as none.
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
	regular := resolver.fontOf(node, "regular", int(pixels*resolver.scale()))
	if regular == nil {
		return nil
	}
	base := style{}
	if color, isColor := resolver.color(node, "fontcolor"); isColor {
		base.color = color
	}
	scaled := int(pixels * resolver.scale())
	node.lineHeight = lineHeight(regular)
	for _, run := range resolver.runs(resolver.valueText(value, raw), base, 0) {
		for _, word := range pieces(run) {
			measured := piece{Run: word}
			if word.Text != "\n" {
				measured.advance = resolver.advance(word, resolver.runFace(node, word, scaled), node.lineHeight)
			}
			node.pieces = append(node.pieces, measured)
		}
	}
	node.textLimit = textLimit(node)
	multiline, _ := node.scalar("multiline")
	node.multiline = strings.EqualFold(multiline, yes)
	return &Text{Font: regular.font.Name, Size: scaled, Color: base.color}
}

// textLimit is the width a text's own words break it at: its max_width, else its declared width; zero for none.
func textLimit(node *widget) float64 {
	if maxWidth, has := node.single("max_width"); has && maxWidth > 0 {
		return maxWidth
	}
	return node.extent("size").width
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

// fontOf opens the face a widget names (font, else the install's first font entry) for a style at a pixel size: the
// font entry's fontstyle for the style, its fontfiles entry's group for the language, each file opened once per
// render; a font that is not found goes to Missing and gives none.
func (resolver *resolver) fontOf(node *widget, fontStyle string, pixels int) *face {
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
	key := name + "/" + fontStyle + "/" + strconv.Itoa(pixels)
	if opened, cached := resolver.fonts[key]; cached {
		return opened
	}
	opened := resolver.openFace(name, fontStyle, pixels)
	resolver.fonts[key] = opened
	if opened != nil && !slices.Contains(resolver.list.Fonts, opened.font) {
		resolver.list.Fonts = append(resolver.list.Fonts, opened.font)
	}
	return opened
}

// scale is the request's UI scale, one where it is none.
func (resolver *resolver) scale() float64 {
	if resolver.request.Scale <= 0 {
		return 1
	}
	return resolver.request.Scale
}

// openFace opens a font's style: its fontfiles entry's group for the request's language, the files in order.
func (resolver *resolver) openFace(name, fontStyle string, pixels int) *face {
	definition, defined := resolver.entryFields(fontDatabase, name)
	if !defined {
		resolver.missing.add(fontKey, name)
		return nil
	}
	files, resolved := resolver.styleFiles(definition, fontStyle)
	opened := &face{font: Font{Name: name, Style: resolved}}
	for _, rel := range resolver.languageGroup(files) {
		file, listed := index.FileAt(resolver.ws.Indexes(), rel)
		parsed, err := parseFont(file, listed)
		if err != nil {
			resolver.missing.add(fontKey, rel)
			continue
		}
		sized, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(pixels), DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			resolver.missing.add(fontKey, rel)
			continue
		}
		if opened.font.URL == "" {
			opened.font.URL = fileURL(file)
		}
		opened.files = append(opened.files, sized)
	}
	if len(opened.files) == 0 {
		resolver.missing.add(fontKey, name)
		return nil
	}
	return opened
}

// parseFont reads a font file into a font.
func parseFont(file inventory.File, listed bool) (*opentype.Font, error) {
	if !listed {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, err
	}
	return opentype.Parse(data)
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

// linesAt breaks a text's measured pieces into lines at a width limit (zero breaks never), between words, each line
// at its top; widths are each line's width. There is always one line.
func linesAt(node *widget, limit float64) (lines []Line, widths []float64) {
	line, offset := Line{}, 0.0
	finish := func() {
		line.Y = float64(len(lines)) * node.lineHeight
		lines, widths = append(lines, line), append(widths, offset)
		line, offset = Line{}, 0
	}
	for _, word := range node.pieces {
		if word.Text == "\n" {
			finish()
			continue
		}
		if limit > 0 && offset > 0 && offset+word.advance > limit && strings.TrimSpace(word.Text) != "" {
			finish()
		}
		offset += word.advance
		if last := len(line.Runs); last > 0 && sameStyle(line.Runs[last-1], word.Run) {
			line.Runs[last-1].Text += word.Text
			continue
		}
		run := word.Run
		run.X = offset - word.advance
		line.Runs = append(line.Runs, run)
	}
	if len(line.Runs) > 0 || len(lines) == 0 {
		finish()
	}
	return lines, widths
}

// sameStyle reports whether two runs are text drawn alike, so the second continues the first.
func sameStyle(first, second Run) bool {
	return first.Icon == "" && second.Icon == "" && first.Bold == second.Bold && first.Italic == second.Italic &&
		first.Color == second.Color
}

// runFace is the face a run is measured in: its bold or italic file where the font has one, else the regular.
func (resolver *resolver) runFace(node *widget, run Run, pixels int) *face {
	fontStyle := "regular"
	switch {
	case run.Bold:
		fontStyle = "bold"
	case run.Italic:
		fontStyle = "italic"
	}
	return resolver.fontOf(node, fontStyle, pixels)
}

// pieces splits a run at its spaces and line breaks, so a line breaks between words: each piece is a word with the
// spaces after it, and a break is a piece of its own.
func pieces(run Run) []Run {
	if run.Icon != "" || run.Text == "" {
		return []Run{run}
	}
	var out []Run
	for i, part := range strings.Split(run.Text, "\n") {
		if i > 0 {
			out = append(out, Run{Text: "\n"})
		}
		for len(part) > 0 {
			end := 0
			for end < len(part) && part[end] != ' ' {
				end++
			}
			for end < len(part) && part[end] == ' ' {
				end++
			}
			head := run
			head.Text, part = part[:end], part[end:]
			out = append(out, head)
		}
	}
	return out
}

// advance is the width of a piece in pixels: each rune in the first file of the group that has the glyph, plus the
// kerning between neighbors; a text icon is as wide as the line is high. A glyph no file has is listed.
func (resolver *resolver) advance(piece Run, typeface *face, height float64) float64 {
	if piece.Icon != "" || typeface == nil {
		return height
	}
	var total float64
	previous := rune(-1)
	for _, char := range piece.Text {
		measured := false
		for _, candidate := range typeface.files {
			width, hasGlyph := candidate.GlyphAdvance(char)
			if !hasGlyph {
				continue
			}
			total += float64(width) / 64
			if previous >= 0 {
				total += float64(candidate.Kern(previous, char)) / 64
			}
			measured = true
			break
		}
		if !measured {
			resolver.missing.add("glyph", resolver.request.Language)
		}
		previous = char
	}
	return total
}

// lineHeight is how far apart the lines of a face's text sit: the first file's line height.
func lineHeight(of *face) float64 {
	if of == nil || len(of.files) == 0 {
		return 0
	}
	return float64(of.files[0].Metrics().Height) / 64
}

// fileURL is the route's URL for a file, which names when the file changed so a changed file is read again.
func fileURL(file inventory.File) string {
	modified := "0"
	if stat, err := os.Stat(file.Path); err == nil {
		modified = strconv.FormatInt(stat.ModTime().UnixMilli(), 10)
	}
	return FileRoute + "?path=" + url.QueryEscape(file.Path) + "&v=" + modified
}
