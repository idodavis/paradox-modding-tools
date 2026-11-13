// property.go makes a widget's properties, each value as the game takes it
// and kept once in the window's ledger, with the texts and textures they show.

package draw

import (
	"cmp"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// ledger holds the properties of a window and its holders, each once, with
// the data call each value writes.
type ledger struct {
	table[Property, Property]

	// calls holds each property's data call, nil for a value that is none.
	calls []*jomini.Call
}

// What a property comes from.
const (
	originWidget   = "widget"
	originTemplate = "template"
	originType     = "type"
	originMissing  = "missing"
)

// textKeys are the keys that write a widget's one text: the localized one,
// then the one shown as written. The last one written wins.
var textKeys = []string{"text", "raw_text"}

// localizedKeys holds the widget keys whose value is a localization key.
var localizedKeys = sync.OnceValue(func() map[string]bool {
	keys := map[string]bool{}
	for _, declaration := range lang.Declarations {
		for _, reference := range declaration.References {
			if declaration.Name == lang.GUIWidgetDatabase && reference.Target == lang.Localization {
				keys[reference.KeyPath] = true
			}
		}
	}

	return keys
})

// intern returns the positions of the given properties in the window's
// ledger. A property's value is parsed for a data call the first time it is
// added.
func (r *resolver) intern(properties []Property) []int32 {
	positions := make([]int32, len(properties))
	for place, property := range properties {
		positions[place] = r.properties.add(property, func() Property {
			var written *jomini.Call
			if call, ok := jomini.ParseBracketed(property.Value, 0); ok {
				written = &call
			}

			r.properties.calls = append(r.properties.calls, written)

			return property
		})
	}

	return positions
}

// overlayTextureKey is the key of the texture in an overlayKey block.
const overlayTextureKey = "texture"

// makeOverlay returns the position in the window's overlays of the overlay a
// modify_texture block writes.
func (r *resolver) makeOverlay(field clause, depth int) int32 {
	var properties []Property
	for lead, inner := range r.listFields(field, depth+1) {
		if lead == "" {
			properties = append(properties, r.makeProperty(inner))
		}
	}

	properties = resolveProperties(properties)
	path := r.library.resolveTexture(findProperty(slices.Values(properties), overlayTextureKey).Value)
	positions := r.intern(properties)

	return r.overlays.add(fmt.Sprint(path, positions), func() Overlay { return Overlay{Path: path, Properties: positions} })
}

// makeProperty returns the property a field writes. A block's value is its
// text. A tagged color such as hsv { 0.5 1 1 } becomes the plain RGBA color,
// 0 to 1, that the page draws.
func (r *resolver) makeProperty(field clause) Property {
	current := field.tree.Statements[field.statement]
	value := field.tree.Unquoted(current.Value)
	if color, ok := jomini.ToColor(field.tree, field.statement); ok && !current.Tag.IsEmpty() {
		value = fmt.Sprintf("{ %.4g %.4g %.4g %.4g }", color.R, color.G, color.B, color.A)
	} else if current.IsBlock() {
		span := current.ResolveValueSpan()
		var edits []jomini.Edit
		for _, inner := range field.tree.Statements[field.statement+1 : current.Last] {
			if resolved, ok := r.resolveSpan(field.tree, inner.Value, inner.ValueClass); ok {
				shifted := jomini.Span{Start: inner.Value.Start - span.Start, End: inner.Value.End - span.Start}
				edits = append(edits, jomini.Edit{Span: shifted, Text: resolved})
			}
		}

		value = jomini.ApplyEdits(field.tree.Text(span), edits)
	} else if resolved, ok := r.resolveSpan(field.tree, current.Value, current.ValueClass); ok {
		value = resolved
	}

	return Property{
		Key: cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag)), Value: value, Origin: field.origin,
		Place: r.locate(field.path), Range: toRange(field), Writer: field.writer,
		Source: describeSource(field.origin, field.writer),
	}
}

// resolveSpan returns what the game reads for a value that is not taken as
// written: a constant or an expression is its number, and a GetDefine call is
// the define's value. It returns false for anything else.
func (r *resolver) resolveSpan(tree *jomini.Tree, span jomini.Span, class jomini.Class) (string, bool) {
	if number, ok := tree.ResolveNumber(span); ok && class != jomini.ClassNumber {
		return strconv.FormatFloat(number, 'f', -1, 64), true
	}

	call, ok := tree.ParseBracketed(span)
	if !ok || len(call.Steps) != 1 || call.Steps[0].Text != lang.GUIDefineCall || len(call.Steps[0].Arguments) == 0 {
		return "", false
	}

	// GetDefine('NAI', 'X') names the define X of the group NAI
	arguments, group := call.Steps[0].Arguments, ""
	if len(arguments) > 1 {
		group = arguments[0].Text
	}

	value := r.library.lookupDefine(group, arguments[len(arguments)-1].Text)

	return value, value != ""
}

// dropsHiddenKey is the key whose yes makes a widget's hidden children take
// no room.
const dropsHiddenKey = "ignoreinvisible"

// resolveWidget gives a widget its properties, the last written of each key,
// and the texts and textures they lead to.
func (r *resolver) resolveWidget(id int32, builtin string, properties []Property) {
	properties = resolveProperties(properties)
	texts, textures := map[string][]loc.Segment{}, map[string]string{}
	for _, property := range properties {
		if property.Origin == originMissing {
			continue
		}

		text, texture := r.library.resolveValue(property.Key, property.Value)
		if len(text) > 0 {
			texts[property.Key] = text
		}

		if texture != "" {
			textures[property.Key] = texture
		}
	}

	widget := &r.widgets[id]
	widget.Name = findProperty(slices.Values(properties), lang.GUIWidgetKey).Value
	widget.Row, widget.Builtin = lang.BuiltinWidgets[builtin], builtin
	dropsHidden := findProperty(slices.Values(properties), dropsHiddenKey).Value
	widget.Row.DropsHidden = r.isDropping(widget.Row, builtin, dropsHidden)
	widget.Properties, widget.Texts, widget.Textures = r.intern(properties), texts, textures
	widget.Colors, widget.Overlays = map[string]string{}, slices.Concat([]int32{}, r.gathered.overlays)
}

// dropsHiddenTypes holds, by game, the types beyond lang.BuiltinWidgets in
// which a hidden child takes no room.
var dropsHiddenTypes = map[string][]string{
	game.Vic3: {"flowcontainer"},
}

// isDropping reports whether a widget's hidden children take no room, as its
// type or its own setting says. A setting of no always wins.
func (r *resolver) isDropping(row lang.WidgetType, builtin, setting string) bool {
	literal, isLiteral := literals[strings.ToLower(setting)]
	if isLiteral && !literal {
		return false
	}

	return row.DropsHidden || slices.Contains(dropsHiddenTypes[r.ws.Game.ID], builtin) ||
		(isLiteral && !row.KeepsHidden)
}

// resolveValue returns the text a key's value shows, or the image file it
// names. Both are empty where the value leads to neither.
func (l *library) resolveValue(key, value string) (text []loc.Segment, texture string) {
	switch {
	case value == "": // an empty value shows nothing

	case slices.Contains(pictureKeys, key) || slices.Contains(maskKeys, key):
		texture = l.resolveTexture(value)

	case isLocalized(key):
		text = l.resolveText(l.lookupText(value), 0)

	case key == textKeys[1]: // the key that writes a text as shown
		text = l.resolveText(value, 0)
	}

	return text, texture
}

// isLocalized reports whether the value of a key is a localization key.
func isLocalized(key string) bool { return localizedKeys()[key] }

// findProperty returns the property with a key, letter case aside, or the
// zero Property if there is none.
func findProperty(properties iter.Seq[Property], key string) Property {
	for property := range properties {
		if strings.EqualFold(property.Key, key) {
			return property
		}
	}

	return Property{}
}

// repeatedKeys are the keys a widget keeps every value of, each adding to the
// ones before.
var repeatedKeys = []string{"datacontext", "onclick", "on_start"}

// resolveProperties returns the last written property of each key, in the
// order the keys are first written.
func resolveProperties(properties []Property) []Property {
	resolved := []Property{}
	positions := map[string]int{}
	for _, property := range properties {
		key := strings.ToLower(property.Key)
		switch {
		case property.Origin == originMissing || slices.Contains(repeatedKeys, key):
			key = property.Origin + key + property.Value
		case slices.Contains(textKeys, key):
			key = textKeys[0]
		}

		if position, ok := positions[key]; ok {
			resolved[position] = property

			continue
		}

		positions[key] = len(resolved)
		resolved = append(resolved, property)
	}

	return resolved
}
