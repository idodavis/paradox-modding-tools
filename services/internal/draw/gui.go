// gui.go is a GUI file's window drawn as a list: the request, the three calls the service makes (GUI, Properties,
// Edit), and the widget the two passes share with the block reader that every lookup of a property goes through.

package draw

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// The words several readers and tables share: the blend modes the page draws, the anchor word for the middle of a
// box, a yes and a no, and the word a color is written with.
const (
	blendNormal = "normal"
	blendMask   = "mask"
	center      = "center"
	yes         = "yes"
	no          = "no"
	colorKey    = "color"
	usingKey    = "using"
)

// FileRoute is where the page reads a file, on Wails' asset server: ?path= its absolute path and &v= when it changed.
const FileRoute = "/pmt/file"

// Request is what a GUI render is asked for: the file, the entry drawn, the resolution and UI scale the window is
// laid out at, the language of its texts, and whether an unknown widget is laid out as hidden.
type Request struct {
	Path        string  `json:"path"`
	Name        string  `json:"name"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Scale       float64 `json:"scale"`
	Language    string  `json:"language"`
	HideUnknown bool    `json:"hideUnknown"`
}

// widget is one resolved widget: its part, its kind and layout class, every statement that applies to it in the
// order they apply (the chain's, each template's where it is used, its own), its children, and what the passes
// found: rows, visibility, text, textures, sizes and box.
type widget struct {
	part       int
	source     statement // the statement the widget is written as: its own block is what an edit changes
	kind       string
	class      game.LayoutClass
	statements []statement
	children   []*widget
	pending    []statement          // the child statements found while expanding, resolved once it is done
	items      []statement          // the widgets of a data model's item, the template of its rows
	overrides  map[string]statement // the blockoverrides in force, by the block's name
	dataType   string               // the data type the nearest datacontext leads to; "" none
	rows       int                  // a data model's rows drawn; 0 for no data model
	row        int                  // the data model row this widget is drawn for; -1 for none
	inRows     int                  // the rows of the data model this widget is a template of; 0 for none
	byRow      []string             // a row template's visibility in each of its rows
	visibility string
	fact       string
	text       *Text
	lineWidths []float64 // the width of each of the text's lines
	lineHeight float64   // how far apart the text's lines sit
	textures   []Texture
	content    size    // what the widget's text or texture measures: the content its sizes fall back to
	template   bool    // a data model item's widget, drawn once per row
	scale      float64 // the request's UI scale, which a size written in pixels is multiplied by
	alpha      float64 // the widget's own opacity, from 0 to 1
	box        Box
	clip       *Box
}

// statement is one assignment that applies to a widget, with the file it is in and who wrote it: "" the widget's
// own block, else the type or template.
type statement struct {
	assign    *jomini.Assignment
	parsed    *jomini.Result
	path      string
	writtenBy string
	made      bool // the statement is made for an entry drawn alone: no text of the file's holds it
}

// size is a width and a height: positive in pixels, negative a fraction of the parent's (-1 is 100%), zero unset.
type size struct{ width, height float64 }

// blendModes is each blend_mode word as the page draws it: a CSS mix-blend-mode, or "mask" for the two that
// multiply the first texture's alpha.
var blendModes = map[string]string{
	blendNormal: blendNormal, "overlay": "overlay", "colordodge": "color-dodge", "multiply": "multiply", "add": "plus-lighter",
	"alphamultiply": blendMask, blendMask: blendMask,
}

// fitTypes are the fittype words a texture is drawn by, the page's background rule for each being its own.
var fitTypes = map[string]bool{"centercrop": true, center: true, "start": true, "end": true, "fill": true}

// FactKey is the key a fact is kept under: its call with the spaces folded, else its function.
func FactKey(fact Fact) string {
	return cmp.Or(fold(fact.Call), fact.Function)
}

// GUI resolves the file's entry into a list: pass 1 over the parse, the indexes and the facts, pass 2 over the
// resolved tree at the request's size. The caller holds the workspace's read lock.
func GUI(ws *workspace.Session, request Request, facts map[string]string) (*List, error) {
	resolved, root, err := resolve(ws, request, facts)
	if err != nil {
		return nil, err
	}
	resolved.list.Width, resolved.list.Height = request.Width, request.Height
	resolved.list.Elements = place(root, request)
	return resolved.list, nil
}

// Properties is the part's effective properties: every key that applies to it with its last value, who wrote it, the
// kind of input and its choices.
func Properties(ws *workspace.Session, request Request, facts map[string]string, part int) ([]Property, error) {
	resolved, root, err := resolve(ws, request, facts)
	if err != nil {
		return nil, err
	}
	target := root.find(part)
	if target == nil {
		return nil, fmt.Errorf("the window has no part %d", part)
	}
	var rows []Property
	positions := map[string]int{}
	for _, assignment := range target.statements {
		key := strings.ToLower(assignment.assign.Key.Text)
		if key == "state" {
			continue
		}
		row := resolved.property(assignment)
		if seen, repeated := positions[key]; repeated {
			rows[seen] = row
			continue
		}
		positions[key] = len(rows)
		rows = append(rows, row)
	}
	return rows, nil
}

// find is the widget of a part in a tree; nil for none.
func (node *widget) find(part int) *widget {
	if node.part == part {
		return node
	}
	for _, child := range node.children {
		if descendant := child.find(part); descendant != nil {
			return descendant
		}
	}
	return nil
}

// property is a statement as a row of the panel: its key and value, who wrote it, and the input its value is
// written for, with the choices a texture or a font offers.
func (resolver *resolver) property(assignment statement) Property {
	key := assignment.assign.Key.Text
	row := Property{Key: key, Value: valueText(assignment), WrittenBy: assignment.writtenBy, Kind: "text"}
	if ref, has := refOf(key); has && ref.Database == game.File {
		row.Kind, row.Options = "texture", resolver.imageFiles()
		return row
	} else if has && ref.Database == fontDatabase {
		row.Kind, row.Options = "font", index.Names(resolver.ws.Indexes(), fontDatabase)
		return row
	}
	switch value := strings.ToLower(row.Value); {
	case value == yes || value == no:
		row.Kind = game.DataBool
	case isNumbers(assignment.assign.Value):
		row.Kind = "number"
	default:
		if _, _, isColor := jomini.ColorParts(assignment.assign.Value); isColor && len(valuesOf(assignment.assign.Value)) >= 3 {
			row.Kind = colorKey
		}
	}
	return row
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

// isNumbers reports whether a value is a number or a block of numbers and percents.
func isNumbers(value jomini.Value) bool {
	texts := valuesOf(value)
	for _, text := range texts {
		if _, isNumber := numberOf(text); !isNumber {
			return false
		}
	}
	return len(texts) > 0
}

// refOf is the file type's Ref row a key is written under.
func refOf(key string) (game.Ref, bool) {
	refs := game.FileTypes[".gui"].Refs
	i := slices.IndexFunc(refs, func(ref game.Ref) bool { return ref.At == key })
	if i < 0 {
		return game.Ref{}, false
	}
	return refs[i], true
}

// imageFiles is every image the install and the mods hold under gfx, by its path from its root.
func (resolver *resolver) imageFiles() []string {
	var rels []string
	for _, scanned := range resolver.ws.Indexes() {
		for _, file := range scanned.Files {
			if file.Format == inventory.Image && strings.HasPrefix(file.Rel, "gfx/") {
				rels = append(rels, file.Rel)
			}
		}
	}
	slices.Sort(rels)
	return slices.Compact(rels)
}

// Edit is the text edits one change from the panel makes in the file: a key's own statement's value replaced, or the
// key added to the widget's own block, the part's statement removed, or moved before another part's. A file under
// the install is refused.
func Edit(ws *workspace.Session, request Request, facts map[string]string, change Change) ([]lsp.TextEdit, error) {
	_, root, err := resolve(ws, request, facts)
	if err != nil {
		return nil, err
	}
	target := root.find(change.Part)
	if target == nil || target.source.made || target.source.assign == nil {
		return nil, fmt.Errorf("part %d is no statement of the file to change", change.Part)
	}
	if file, _, _ := ws.Locate(target.source.path); file.Origin == game.OriginVanilla {
		return nil, errGameFile
	}
	switch {
	case change.Remove:
		return []lsp.TextEdit{removal(target.source)}, nil
	case change.Before >= 0:
		return move(root, target, change.Before)
	}
	return []lsp.TextEdit{setKey(target, change.Key, change.Value)}, nil
}

// PruneFacts is the workspace's facts without those whose function nothing declares or names, and the keys it
// dropped: a fact is kept where the key is named by a data function reference or leads to a function the dumps
// declare.
func PruneFacts(ws *workspace.Session, facts map[string]string) (remaining map[string]string, dropped []string) {
	remaining = map[string]string{}
	for key, value := range facts {
		if declared(ws, key) {
			remaining[key] = value
		} else {
			dropped = append(dropped, key)
		}
	}
	slices.Sort(dropped)
	return remaining, dropped
}

// declared reports whether a fact's key names a data function: one a binding refers to, or the last step of its
// call is a function of the type the steps before it lead to.
func declared(ws *workspace.Session, key string) bool {
	if len(index.Lookup(ws.Indexes(), schema.Refers, game.DataFunction, key)) > 0 {
		return true
	}
	steps := jomini.ParseCall(key, 0)
	if len(steps) == 0 {
		return false
	}
	_, _, exists := ws.Install.Schema.CallStep(steps, len(steps)-1, "")
	return exists
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

// numbers are the numbers a key's last value holds (size = { 380 36 }, alpha = 0.5), a percent kept as a negative
// fraction of the parent (100% is -1); false where the key is never written or its value holds anything else.
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

// choices are the lower-cased words a key's last value names (parentanchor = bottom|hcenter is bottom, hcenter).
func (node *widget) choices(key string) ([]string, bool) {
	latest, has := node.last(key)
	if !has {
		return nil, false
	}
	var choices []string
	for _, item := range valuesOf(latest.assign.Value) {
		for part := range strings.FieldsFuncSeq(strings.ToLower(item), func(char rune) bool { return char == '|' || char == ' ' }) {
			choices = append(choices, part)
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

// errGameFile is the refusal of an edit to a file under the install.
var errGameFile = errors.New("the game's files are read only")

// setKey is the edit that writes a key's value in the widget's own block: the value of the key's last statement
// there replaced, else the key added after the block's last statement, indented as the statements are.
func setKey(target *widget, key, value string) lsp.TextEdit {
	source := target.source
	block := jomini.ChildBlock(source.assign)
	fields := jomini.Fields(block)
	for _, field := range slices.Backward(fields) {
		if strings.EqualFold(field.Key.Text, key) && field.Value != nil {
			return textEdit(source.parsed, field.Value.NodeRange(), value)
		}
	}
	src := source.parsed.Src
	offset, indent := block.Range.Start+1, indentOf(src, source.assign.Range.Start)+"\t"
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		offset, indent = last.NodeRange().End, indentOf(src, last.Range.Start)
		if !strings.Contains(src[block.Range.Start:last.Range.Start], "\n") {
			indent += "\t"
		}
	}
	return textEdit(source.parsed, jomini.Range{Start: offset, End: offset}, "\n"+indent+key+" = "+value)
}

// removal is the edit that removes a statement and the line break before it.
func removal(source statement) lsp.TextEdit {
	src := source.parsed.Src
	start := source.assign.Range.Start
	if line := lineStart(src, start); strings.TrimSpace(src[line:start]) == "" {
		start = line
		if start > 0 {
			start--
		}
		if start > 0 && src[start] == '\n' && src[start-1] == '\r' {
			start--
		}
	}
	return textEdit(source.parsed, jomini.Range{Start: start, End: source.assign.Range.End}, "")
}

// move is the edits that take a part's statement out of its place and write it before another part's, in the same
// file.
func move(root, target *widget, before int) ([]lsp.TextEdit, error) {
	other := root.find(before)
	if other == nil || other.source.made || other.source.path != target.source.path {
		return nil, fmt.Errorf("part %d cannot be moved before part %d", target.part, before)
	}
	src := target.source.parsed.Src
	text := src[target.source.assign.Range.Start:target.source.assign.Range.End]
	line := lineStart(src, other.source.assign.Range.Start)
	insert := textEdit(target.source.parsed, jomini.Range{Start: line, End: line}, indentOf(src, line)+text+"\n")
	return []lsp.TextEdit{removal(target.source), insert}, nil
}

// textEdit is a replacement of a span of a parse's text, in the editor's positions.
func textEdit(parsed *jomini.Result, span jomini.Range, text string) lsp.TextEdit {
	lines := parsed.Lines()
	return lsp.TextEdit{
		Range:   lsp.Range{Start: lines.PositionAt(span.Start), End: lines.PositionAt(span.End)},
		NewText: text,
	}
}

// lineStart is the offset of the start of the line an offset is on.
func lineStart(src string, offset int) int {
	return strings.LastIndexByte(src[:offset], '\n') + 1
}

// indentOf is the blanks a line starts with: the line an offset is on, or the one that starts at it.
func indentOf(src string, offset int) string {
	start := lineStart(src, offset)
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return src[start:end]
}
