// gui.go is a GUI file's entry drawn as a list: the request, the calls the service makes (Entries, GUI, Properties),
// the widget the resolver builds with the block reader that every lookup of a property goes through, and the layout
// words the page is handed.

package draw

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/workspace"
)

// The words several readers and tables share: the blend modes the page draws, the anchor word for the middle of a
// box, a yes and a no, and the words a color, a font and a template are written with.
const (
	blendNormal = "normal"
	blendMask   = "mask"
	center      = "center"
	yes         = "yes"
	no          = "no"
	colorKey    = "color"
	fontKey     = "font"
	usingKey    = "using"
)

// FileRoute is where the page reads a file, on Wails' asset server: ?path= its absolute path and &v= when it changed.
const FileRoute = "/pmt/file"

// Request is what a GUI render is asked for: the file, the entry drawn, the resolution a window's percents are of,
// and the language of its texts.
type Request struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Language string `json:"language"`
}

// widget is one resolved widget: its element, its kind, every statement that applies to it in the order they apply (the
// chain's, each template's where it is used, its own), and its children.
type widget struct {
	element    int
	kind       string
	statements []statement
	children   []*widget
	pending    []statement          // the child statements found while expanding, resolved once it is done
	items      []statement          // the widgets of a data model's item, the template of its rows
	overrides  map[string]statement // the blockoverrides in force, by the block's name
}

// statement is one assignment that applies to a widget, with the file it is in and who wrote it: "" the widget's
// own block, else the type or template.
type statement struct {
	assign    *jomini.Assignment
	parsed    *jomini.Result
	path      string
	writtenBy string
}

// layoutKeys are the words a widget is laid out by, handed to the page as written; the page's table says what each
// means to the browser's layout, and a key with no row there is not carried.
var layoutKeys = []string{
	"size", "minimumsize", "maximumsize", "min_width", "min_height", "max_width", "max_height",
	"position", "position_x", "position_y", "parentanchor", "widgetanchor",
	"layoutpolicy_horizontal", "layoutpolicy_vertical", "layoutstretchfactor_horizontal", "layoutstretchfactor_vertical",
	"margin", "margin_left", "margin_right", "margin_top", "margin_bottom", "spacing",
	"direction", "datamodel_wrap", "flipdirection", "addcolumn", "addrow", "ignoreinvisible",
	"align", "autoresize", "multiline", "elide", "scissor", "alpha",
}

// blendModes is each blend_mode word as the page draws it: a CSS mix-blend-mode, or "mask" for the two that
// multiply the first texture's alpha.
var blendModes = map[string]string{
	blendNormal: blendNormal, "overlay": "overlay", "colordodge": "color-dodge", "multiply": "multiply", "add": "plus-lighter",
	"alphamultiply": blendMask, blendMask: blendMask,
}

// fitTypes are the fittype words a texture is drawn by, the page's background rule for each being its own.
var fitTypes = map[string]bool{"centercrop": true, center: true, "start": true, "end": true, "fill": true}

// missingKinds is what each kind of thing not found is called on the status line; %s is its name.
var missingKinds = map[string]string{
	"template": "Template not found: %s", "type": "Type not found: %s",
	"cycle": "Type cycle: %s", "templatecycle": "Template cycle: %s",
	"texture": "Texture not found: %s", fontKey: "Font not found: %s", "fontsize": "Font size missing: %s",
	"format": "Text format not found: %s", "texticon": "Text icon not found: %s", "loc": "Localization key not found: %s",
	"blend": "Blend mode not drawn: %s",
	"empty": "Nothing to draw: %s has no size, no texture, no text and no children of its own",
}

// misses is what a render did not find, each a sentence the status line shows.
type misses map[string]bool

// add lists a thing not found: its kind's sentence with its name.
func (misses misses) add(kind, name string) {
	misses[fmt.Sprintf(missingKinds[kind], name)] = true
}

// Entries is what a GUI file can draw: the symbols whose database an entry is drawn from (a window, type, template,
// text icon or format), in the file's order.
func Entries(ws *workspace.Session, path string) ([]lsp.Symbol, error) {
	symbols, err := lsp.DocumentSymbols(ws, path)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(symbols, func(symbol lsp.Symbol) bool { return entryHow[symbol.Database] == "" }), nil
}

// GUI resolves the file's entry into a list over the parse and the indexes. The scene is a window's resolution; an
// entry drawn alone is its own size, and one with nothing to show is listed. The caller holds the workspace's read
// lock.
func GUI(ws *workspace.Session, request Request) (*List, error) {
	resolved, root, err := resolve(ws, request)
	if err != nil {
		return nil, err
	}
	switch {
	case !resolved.alone:
		resolved.list.Width, resolved.list.Height = request.Width, request.Height
	case drawsNothing(resolved.list.Elements[0], root):
		resolved.missing.add("empty", request.Name)
	}
	resolved.list.Missing = slices.Sorted(maps.Keys(resolved.missing))
	return resolved.list, nil
}

// drawsNothing reports whether an entry drawn alone shows nothing: no size, texture, text or child of its own.
func drawsNothing(element Element, root *widget) bool {
	return element.Layout["size"] == "" && len(element.Textures) == 0 && element.Text == nil && len(root.children) == 0
}

// Properties is the element's effective properties: every key that applies to it with its last value and who wrote it.
func Properties(ws *workspace.Session, request Request, element int) ([]Property, error) {
	_, root, err := resolve(ws, request)
	if err != nil {
		return nil, err
	}
	target := root.find(element)
	if target == nil {
		return nil, fmt.Errorf("the window has no element %d", element)
	}
	var rows []Property
	positions := map[string]int{}
	for _, assignment := range target.statements {
		key := strings.ToLower(assignment.assign.Key.Text)
		if key == "state" {
			continue
		}
		row := Property{Key: assignment.assign.Key.Text, Value: valueText(assignment), WrittenBy: assignment.writtenBy}
		if seen, repeated := positions[key]; repeated {
			rows[seen] = row
			continue
		}
		positions[key] = len(rows)
		rows = append(rows, row)
	}
	return rows, nil
}

// find is the widget of an element in a tree; nil for none.
func (node *widget) find(element int) *widget {
	if node.element == element {
		return node
	}
	for _, child := range node.children {
		if descendant := child.find(element); descendant != nil {
			return descendant
		}
	}
	return nil
}

// valueText is what a statement's value is written as: a word's text, or a block as written.
func valueText(assignment statement) string {
	if scalar, isScalar := assignment.assign.Value.(*jomini.Scalar); isScalar {
		return scalar.Text
	}
	if assignment.assign.Value == nil {
		return ""
	}
	return assignment.parsed.Flat(assignment.assign.Value.NodeRange())
}

// layoutWords are the widget's layout words as written, by key, the last written of each: a word's text, or a block's
// words with a space between (size = { 100 20 } is "100 20").
func layoutWords(node *widget) map[string]string {
	words := map[string]string{}
	for _, key := range layoutKeys {
		if latest, has := node.last(key); has {
			words[key] = strings.Join(valuesOf(latest.assign.Value), " ")
		}
	}
	return words
}

// last is the statement that last wrote a key, without regard to case; false for none. The block reader: every
// lookup of a property goes through it, so the last written wins everywhere.
func (node *widget) last(key string) (statement, bool) {
	for _, assignment := range slices.Backward(node.statements) {
		if strings.EqualFold(assignment.assign.Key.Text, key) {
			return assignment, true
		}
	}
	return statement{}, false
}

// numbers are the numbers a key's last value holds (framesize = { 32 32 }, alpha = 0.5), a percent kept as a negative
// fraction (100% is -1); false where the key is never written or its value holds anything else.
func (node *widget) numbers(key string) ([]float64, bool) {
	latest, has := node.last(key)
	if !has {
		return nil, false
	}
	var numbers []float64
	for _, item := range valuesOf(latest.assign.Value) {
		number, isNumber := numberOf(item)
		if !isNumber {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, len(numbers) > 0
}

// choices are the lower-cased words a key's last value names (fittype = centercrop, spritetype = Corneredtiled).
func (node *widget) choices(key string) ([]string, bool) {
	latest, has := node.last(key)
	if !has {
		return nil, false
	}
	var choices []string
	for _, item := range valuesOf(latest.assign.Value) {
		for element := range strings.FieldsFuncSeq(strings.ToLower(item), func(char rune) bool { return char == '|' || char == ' ' }) {
			choices = append(choices, element)
		}
	}
	return choices, len(choices) > 0
}

// scalar is the one-word value of a key's last statement; false where it is not written or holds a block.
func (node *widget) scalar(key string) (string, bool) {
	latest, has := node.last(key)
	if !has {
		return "", false
	}
	value, isScalar := latest.assign.Value.(*jomini.Scalar)
	if !isScalar {
		return "", false
	}
	return value.Text, true
}

// valuesOf is the texts of a value's words: a scalar's, or those a block lists.
func valuesOf(value jomini.Value) []string {
	switch typed := value.(type) {
	case *jomini.Scalar:
		return []string{typed.Text}
	case *jomini.Block:
		var texts []string
		for _, item := range typed.Statements {
			if listed, isListed := item.(*jomini.ValueStmt); isListed {
				if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar {
					texts = append(texts, scalar.Text)
				}
			}
		}
		return texts
	}
	return nil
}

// numberOf reads a number or a percent (50% is -0.5); false for anything else.
func numberOf(text string) (float64, bool) {
	if percent, isPercent := strings.CutSuffix(text, "%"); isPercent {
		number, err := strconv.ParseFloat(percent, 64)
		return -number / 100, err == nil
	}
	if jomini.Literal(text) != jomini.LitNumber {
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)
	return number, err == nil
}
