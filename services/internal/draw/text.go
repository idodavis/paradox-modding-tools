// text.go is a widget's text: its loc entry or literal as runs with the formats' styles, the text icons and the
// concepts' names, and the fonts the page draws them with.

package draw

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
)

// The databases a text reads, the keys it is written with, and the style a face is unless written.
const (
	fontDatabase     = "font.font"
	fontFileDatabase = "font.fontfiles"
	formatDatabase   = "gui.format"
	textIconDatabase = "gui.texticon"
	textKey          = "text"
	rawTextKey       = "raw_text"
	fontKey          = "font"
	colorKey         = "color"
	regularStyle     = "regular"
)

// maxNesting is how deep formats and loc keys name one another before a cycle is assumed.
const maxNesting = 10

// boundText is what a data call in a text is drawn as.
const boundText = "[…]"

// plainKeys are the words of a format the preview draws as plain text.
var plainKeys = []string{sizeKey, "glow_color", "glow_offset", "shadow", "underline", fontKey, "tooltip"}

// textKeys are the keys a widget's text is written with, which a label takes from its button.
var textKeys = []string{textKey, rawTextKey, fontKey, "fontsize", "fontcolor", "default_format"}

// text is a widget's text, the later written of text and raw_text, as runs in its font at its size and color, in its
// default format, each face the runs use listed; nil for none, or for no font size or no font file, which is listed.
func (resolver *guiResolver) text(widget *widget) *Text {
	key := ""
	for _, statement := range widget.statements {
		statementKey := strings.ToLower(statement.assignment.Key.Text)
		if statementKey == textKey || statementKey == rawTextKey {
			key = statementKey
		}
	}
	value, has := widget.scalar(key)
	if !has {
		return nil
	}
	sizes, isSized := widget.numbers("fontsize")
	if !isSized {
		sizes, isSized = resolver.textbox().numbers("fontsize")
	}
	if !isSized {
		resolver.addMissing("fontsize", widget.builtIn)
		return nil
	}
	regular := resolver.fontOf(widget, regularStyle)
	if regular == nil {
		return nil
	}
	base := Run{}
	base.Color, _ = widget.color("fontcolor")
	format, isFormatted := widget.scalar("default_format")
	if !isFormatted {
		format, isFormatted = resolver.textbox().scalar("default_format")
	}
	if isFormatted {
		base = resolver.format(strings.TrimPrefix(format, "#"), base, 0)
	}
	runs := resolver.runsOf(resolver.locText(value, key == rawTextKey), base, 0)
	for _, run := range runs {
		switch {
		case run.IsBold:
			resolver.fontOf(widget, "bold")
		case run.IsItalic:
			resolver.fontOf(widget, "italic")
		}
	}
	return &Text{Font: regular.Name, Size: int(sizes[0]), Color: base.Color, Runs: runs}
}

// textStatements are the statements that write a widget's text keys, in the order they apply.
func textStatements(widget *widget) []statement {
	return slices.DeleteFunc(slices.Clone(widget.statements), func(statement statement) bool {
		return !slices.ContainsFunc(textKeys, func(key string) bool { return strings.EqualFold(key, statement.assignment.Key.Text) })
	})
}

// runsOf turns a text's markup into runs over a style: a format applied, an icon's texture, a $key$'s text, a [call]
// as a concept's name or as a value the game fills.
func (resolver *guiResolver) runsOf(value string, base Run, depth int) []Run {
	var runs []Run
	for _, segment := range loc.Segments(value) {
		run := base
		run.IsBold, run.IsItalic = run.IsBold || segment.Bold, run.IsItalic || segment.Italic
		if segment.Format != "" {
			run = resolver.format(segment.Format, run, 0)
		}
		switch {
		case segment.Icon != "":
			run.Icon = resolver.iconURL(segment.Icon)
			runs = append(runs, run)
		case segment.Key != "" && depth < maxNesting:
			runs = append(runs, resolver.runsOf(resolver.locText(segment.Key, false), run, depth+1)...)
		case segment.Call != "":
			if text, isConcept := resolver.conceptText(value[segment.Range.Start:segment.Range.End]); isConcept && depth < maxNesting {
				runs = append(runs, resolver.runsOf(text, run, depth+1)...)
				continue
			}
			run.Text, run.Call = boundText, "["+segment.Call+"]"
			runs = append(runs, run)
		case segment.Text != "":
			run.Text = segment.Text
			runs = append(runs, run)
		}
	}
	return runs
}

// locText is a key's loc entry in the workspace's language, else the value as written; a missing key is listed unless
// the text is raw.
func (resolver *guiResolver) locText(value string, isRaw bool) string {
	if !loc.LooksLikeKey(value) || len(jomini.Calls(value, 0)) > 0 {
		return value
	}
	if entry, isKnown := resolver.ws.Loc(value, resolver.ws.Language); isKnown {
		return entry.Text()
	}
	if !isRaw {
		resolver.addMissing("loc", value)
	}
	return value
}

// conceptText is the text a concept link (a data call written with its brackets) shows: what the link writes, else
// the concept's title; false for none.
func (resolver *guiResolver) conceptText(call string) (string, bool) {
	calls, database := jomini.DataCalls(call, 0), resolver.databaseOf(game.GlobalType)
	if len(calls) == 0 || database == "" {
		return "", false
	}
	for _, concept := range loc.Concepts(calls[0], call) {
		winner, isDefined := resolver.winner(schema.Occurrence{Database: database, ID: concept.Name})
		if !isDefined {
			continue
		}
		if concept.Text != "" {
			return concept.Text, true
		}
		if title := resolver.ws.Title(concept.Name, winner, resolver.ws.Language); title != "" {
			return title, true
		}
	}
	return "", false
}

// format applies a format's ;-joined words to a style: a color, bold, italic, a plain word, or a named format's
// words in turn, matched without regard to case; a format no entry defines is listed.
func (resolver *guiResolver) format(text string, base Run, depth int) Run {
	for piece := range strings.SplitSeq(text, ";") {
		key, value, _ := strings.Cut(strings.TrimSpace(piece), ":")
		key = strings.ToLower(key)
		switch {
		case key == colorKey:
			base.Color = formatColor(value, base.Color)
		case key == "bold":
			base.IsBold = true
		case key == "italic":
			base.IsItalic = true
		case key == "" || slices.Contains(plainKeys, key):
		default:
			entry, isDefined := resolver.entryProperties(formatDatabase, resolver.formats()[key])
			if !isDefined || depth >= maxNesting {
				resolver.addMissing("format", key)
				continue
			}
			inner, _ := entry.scalar("format")
			base = resolver.format(inner, base, depth+1)
		}
	}
	return base
}

// formatColor is the CSS color of a format's {r, g, b} or {r, g, b, a}; the fallback where it reads as none.
func formatColor(value, fallback string) string {
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
	if color, isKnown := game.ColorOf("", parts); isKnown {
		return color.CSS()
	}
	return fallback
}

// iconURL is the URL of a text icon's texture; "" where no entry or file has it, which is listed.
func (resolver *guiResolver) iconURL(id string) string {
	if entry, isDefined := resolver.entryProperties(textIconDatabase, id); isDefined {
		if iconsize, has := entry.last("iconsize"); has {
			path, _ := propertiesOf(iconsize).scalar("texture")
			if file, hasFile := resolver.fileOf(path); hasFile {
				return resolver.urlOf(file)
			}
		}
	}
	resolver.addMissing("texticon", id)
	return ""
}

// entryProperties is the winning entry of an id in a database, as properties; false for none.
func (resolver *guiResolver) entryProperties(database, id string) (*properties, bool) {
	definition, isDefined := resolver.definition(schema.Occurrence{Database: database, ID: id})
	if !isDefined {
		return nil, false
	}
	return propertiesOf(definition), true
}

// fontOf is the face a widget's text is drawn in for a style: its font, else the textbox type's, else the install's
// first; the first of its files for the workspace's language, listed for the page; nil, listed, where there is none.
func (resolver *guiResolver) fontOf(widget *widget, fontStyle string) *Font {
	id, hasFont := widget.scalar(fontKey)
	if !hasFont {
		id, hasFont = resolver.textbox().scalar(fontKey)
	}
	if !hasFont {
		ids := index.Names(resolver.ws.Indexes(), fontDatabase)
		if len(ids) == 0 {
			resolver.addMissing(fontKey, "no font entry")
			return nil
		}
		id = ids[0]
	}
	key := id + "/" + fontStyle
	if font, isKnown := resolver.fonts[key]; isKnown {
		return font
	}
	var font *Font
	if entry, isDefined := resolver.entryProperties(fontDatabase, id); isDefined {
		files, style := resolver.fontFiles(entry, fontStyle)
		for _, rel := range files {
			if file, hasFile := index.FileAt(resolver.ws.Indexes(), rel); hasFile {
				font = &Font{Name: id, Style: style, URL: resolver.urlOf(file.Path)}
				break
			}
		}
	}
	if font == nil {
		resolver.addMissing(fontKey, id)
	} else if !slices.Contains(resolver.list.Fonts, *font) {
		resolver.list.Fonts = append(resolver.list.Fonts, *font)
	}
	resolver.fonts[key] = font
	return font
}

// fontFiles are the files a font entry draws a style with, and the style taken (regular where it has none for the
// style): the fontfiles group for the workspace's language, else the base language's.
func (resolver *guiResolver) fontFiles(entry *properties, fontStyle string) (files []string, style string) {
	byStyle := map[string]string{}
	for _, statement := range entry.statements {
		if strings.EqualFold(statement.assignment.Key.Text, "fontstyle") {
			fontstyle := propertiesOf(statement)
			key, _ := fontstyle.scalar("style")
			byStyle[strings.ToLower(key)], _ = fontstyle.scalar("fontfiles")
		}
	}
	style = fontStyle
	if byStyle[style] == "" {
		style = regularStyle
	}
	groups, isDefined := resolver.entryProperties(fontFileDatabase, byStyle[style])
	if !isDefined {
		return nil, style
	}
	for _, statement := range groups.statements {
		group := propertiesOf(statement)
		languages, hasLanguages := group.last("languages")
		paths, hasPaths := group.last("files")
		if !strings.EqualFold(statement.assignment.Key.Text, "group") || !hasLanguages || !hasPaths {
			continue
		}
		switch {
		case slices.Contains(languages.scalars(), "l_"+resolver.ws.Language):
			return paths.scalars(), style
		case files == nil && slices.Contains(languages.scalars(), "l_"+game.BaseLanguage):
			files = paths.scalars()
		}
	}
	return files, style
}
