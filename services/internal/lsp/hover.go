// hover.go is the card for the word under the cursor: one switch on what the reader made of it. A
// literal or a comment has no card. Signature help is a declared token's usage. A card's prose is
// what the game declares about a token, link or field, and the comment above a definition, read
// from the file when the card is built and never stored.
package lsp

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// Hover is the card for the word at a position, or nil where nothing is said about it.
func Hover(sess *session.Session, path string, line, col int) (*Inspect, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	if file.place.Format == source.Loc {
		return file.locCard(file.offset(line, col)), nil
	}
	word := file.at(file.offset(line, col))
	if word == nil {
		return nil, nil
	}
	return file.card(word), nil
}

// card fills the card for a word by its type.
func (file *file) card(site *site) *Inspect {
	sess, value := file.sess, site.Value
	card := &Inspect{Value: value, Range: file.span(site.Range), Winner: -1, Scope: site.Level.Scope}
	if assignment, ok := site.Level.Statement.(*jomini.Assignment); ok {
		card.Key = assignment.Key.Text
	}

	switch value.Type {
	case schema.TypeNone, schema.TypeString, schema.TypeBool, schema.TypeCall:
		return nil
	case schema.TypeNumber:
		if site.Weight == 0 {
			return nil
		}
		card.Weight = site.Weight
	case schema.TypeDate:
		card.Date = date(site.Text, file.place.Calendar)
	case schema.TypeToken:
		docs, usage, in, out := tokenDocs(sess.Install.Vocab, value.Name)
		card.Docs, card.Usage, card.ScopeOut, card.Accepts = docs, usage, out, spell(site.Level.Below)
		if card.Scope == "" && len(in) == 1 {
			card.Scope = in[0]
		}
	case schema.TypeScopePath:
		first, _, _ := strings.Cut(site.Text, ".")
		if link, ok := sess.Install.Vocab.Links[strings.ToLower(first)]; ok {
			card.Docs, card.ScopeOut = link.Doc, link.Out
		}
	case schema.TypeMember, schema.TypeEnum:
		card.Accepts, card.Docs = spell(site.Level.Below), fieldDocs(sess, site.Level, site.Text)
		if site.Level.Below != nil {
			card.Class = site.Level.Below.Class.String()
		}
	case schema.TypeRef:
		file.defined(card, value)
	case schema.TypeLoc:
		if said, ok := sess.Loc(value.Name, ""); ok {
			card.Label, card.Sites, card.Winner = said.Value, []Location{locate(said.At, said.Line, 0)}, 0
		}
	case schema.TypeFile:
		if found, ok := index.All(sess.All()).Find(value.Name); ok {
			card.Sites, card.Winner = []Location{locate(found, 0, 0)}, 0
			if found.Format == source.Image {
				if info, err := source.ReadImageInfo(found.Path); err == nil {
					card.Image = &info
				}
			}
		}
	case schema.TypeConstant:
		for _, found := range sess.Defs(value.Name) {
			if found.Kind == "constants" && index.PathKey(found.At.Path) == index.PathKey(file.path) {
				card.Sites, card.Winner = []Location{locate(found.At, int(found.Line), int(found.Col))}, 0
				if text, err := sess.Text(found.At.Path); err == nil {
					_, value, _ := strings.Cut(lineOf(text, int(found.Line)), "=")
					card.Label = strings.TrimSpace(value)
				}
			}
		}
	case schema.TypeParam:
		if kind := sess.Schema.Kinds[site.Path.Kind]; kind != nil && kind.Params {
			file.parameter(card, value.Name, site.Path.Top)
		}
	case schema.TypeUnresolved:
		if !site.IsKey && site.Level.Key.Type == schema.TypeToken {
			card.Docs, _, _, _ = tokenDocs(sess.Install.Vocab, site.Level.Key.Name)
		}
	}
	if tagged, ok := file.colour(site); ok {
		card.Color = tagged
	}
	return card
}

// defined fills a definition's sites in load order, the kept one, its label and the comment above.
func (file *file) defined(card *Inspect, value schema.Value) {
	sess := file.sess
	var found []index.Found
	for _, name := range sess.Defs(value.Name) {
		if name.Kind == value.Kind && (value.Parent == "" || name.Parent == value.Parent) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return
	}
	kept, at := sess.Winner(found)
	for _, definition := range found {
		card.Sites = append(card.Sites, locate(definition.At, int(definition.Line), int(definition.Col)))
	}
	card.Winner, card.Label, card.ScopeOut = at, kept.Label, kept.ScopeType
	if kind := sess.Schema.Kinds[value.Kind]; card.Label == "" && kind != nil && len(kind.LocTemplates) > 0 {
		if said, ok := sess.Loc(loc.Expand(value.Name, kind.LocTemplates[:1])[0], ""); ok {
			card.Label = said.Value
		}
	}
	if text, err := sess.Text(kept.At.Path); err == nil {
		card.Docs = comment(text, int(kept.Line))
	}
}

// parameter fills a macro parameter's sites and the words written for it where the macro is called.
func (file *file) parameter(card *Inspect, name, macro string) {
	sess := file.sess
	for _, definition := range sess.Defs(name) {
		if definition.Kind == "params" && definition.Parent == macro {
			card.Sites, card.Winner = append(card.Sites, locate(definition.At, int(definition.Line), int(definition.Col))), 0
		}
	}
	seen := map[string]int{}
	for _, idx := range sess.All() {
		for _, cited := range idx.Referring(name) {
			if cited.Kind == "params" && cited.Parent == macro {
				if text, err := sess.Text(cited.At.Path); err == nil {
					if _, value, ok := strings.Cut(lineOf(text, int(cited.Line)), "="); ok {
						seen[strings.TrimSpace(value)]++
					}
				}
			}
		}
	}
	for text, count := range seen {
		card.Values = append(card.Values, Seen{Text: text, Count: count})
	}
	slices.SortFunc(card.Values, func(first, second Seen) int { return strings.Compare(first.Text, second.Text) })
}

// locate is the editor's location of a line and column of a file, 0-based as the records keep them.
func locate(at source.File, line, col int) Location {
	return Location{Path: at.Path, Origin: at.Origin, Rel: at.Rel, Range: Range{Start: Position{Line: line, Character: col}, End: Position{Line: line, Character: col}}}
}

// lineOf is one 0-based line of a text.
func lineOf(text string, line int) string {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return lines[line]
}

// date writes a date out (January 1st, 8082 AD), and in the mod's calendar where one is declared
// and the year stays positive.
func date(text string, calendar session.Calendar) string {
	year, month, day, _, ok := jomini.ParseDate(strings.Trim(text, `"`))
	if !ok {
		return ""
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	out := fmt.Sprintf("%s %s, %d AD", months[month], ordinal(day), year)
	if calendar.Era != "" && year-calendar.EpochOffset > 0 {
		out += fmt.Sprintf("; %d.%d.%d %s in this mod's calendar", year-calendar.EpochOffset, month, day, calendar.Era)
	}
	return out
}

// ordinal is 1st, 2nd, 3rd, 4th … 11th, 12th, 13th … 21st.
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

// colour is the #rrggbb of the rgb, hsv or hex block the cursor's statement writes, for a swatch.
func (file *file) colour(site *site) (string, bool) {
	assignment, ok := site.Level.Statement.(*jomini.Assignment)
	if !ok || !site.IsKey {
		return "", false
	}
	tagged, ok := assignment.Value.(*jomini.TaggedBlock)
	if !ok {
		return "", false
	}
	var parts []float64
	for _, st := range tagged.Block.Statements {
		if bare, ok := st.(*jomini.ValueStmt); ok {
			if scalar, ok := bare.Value.(*jomini.Scalar); ok {
				n, _ := strconv.ParseFloat(scalar.Text, 64)
				parts = append(parts, n)
			}
		}
	}
	switch strings.ToLower(tagged.Tag.Text) {
	case "rgb":
		if len(parts) >= 3 {
			return hex(parts[0], parts[1], parts[2]), true
		}
	case "hsv", "hsv360":
		if len(parts) >= 3 {
			hue, saturation, value := parts[0], parts[1], parts[2]
			if strings.ToLower(tagged.Tag.Text) == "hsv360" {
				hue, saturation, value = hue/360, saturation/100, value/100
			}
			red, green, blue := hsv(hue, saturation, value)
			return hex(red*255, green*255, blue*255), true
		}
	case "hex":
		if len(tagged.Block.Statements) == 1 {
			if bare, ok := tagged.Block.Statements[0].(*jomini.ValueStmt); ok {
				if scalar, ok := bare.Value.(*jomini.Scalar); ok {
					return "#" + strings.TrimPrefix(strings.ToLower(scalar.Text), "0x")[:6], true
				}
			}
		}
	}
	return "", false
}

// hex spells a colour whose parts are 0 to 255, or 0 to 1 where every part is at most 1.
func hex(red, green, blue float64) string {
	if red <= 1 && green <= 1 && blue <= 1 {
		red, green, blue = red*255, green*255, blue*255
	}
	return fmt.Sprintf("#%02x%02x%02x", int(red+0.5), int(green+0.5), int(blue+0.5))
}

// hsv turns hue, saturation and value, each 0 to 1, into red, green and blue, each 0 to 1.
func hsv(hue, saturation, value float64) (float64, float64, float64) {
	// the hue's sixth of the colour wheel, how far into it, and the three levels a channel takes
	sector := int(hue*6) % 6
	fraction := hue*6 - float64(int(hue*6))
	bottom, falling, rising := value*(1-saturation), value*(1-fraction*saturation), value*(1-(1-fraction)*saturation)
	switch sector {
	case 0:
		return value, rising, bottom
	case 1:
		return falling, value, bottom
	case 2:
		return bottom, value, rising
	case 3:
		return bottom, falling, value
	case 4:
		return rising, bottom, value
	}
	return value, bottom, falling
}

// locCard is the card in a localization file: the key at the line's start, or a $key$ the value
// interpolates, which is a loc key where one exists and an engine value otherwise.
func (file *file) locCard(offset int) *Inspect {
	lines := jomini.NewLineIndex(file.text)
	pos := lines.PositionAt(offset)
	line := lineOf(file.text, pos.Line)
	start := lines.OffsetAt(pos.Line, 0)
	for _, span := range loc.Interps(line, start) {
		if within(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End}, offset) {
			name := span.Key
			card := &Inspect{Value: schema.Value{Type: schema.TypeParam, Name: name}, Winner: -1, Range: file.span(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End})}
			if said, ok := file.sess.Loc(name, ""); ok {
				card.Value.Type, card.Label, card.Sites, card.Winner = schema.TypeLoc, said.Value, []Location{locate(said.At, said.Line, 0)}, 0
			}
			return card
		}
	}
	key, _, _ := strings.Cut(strings.TrimLeft(line, " \t"), ":")
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	if key == "" || strings.HasPrefix(key, "#") || strings.HasPrefix(key, "l_") || !within(jomini.Range{Start: start + indent, End: start + indent + len(key)}, offset) {
		return nil
	}
	card := &Inspect{Value: schema.Value{Type: schema.TypeLoc, Name: key}, Winner: -1, Range: file.span(jomini.Range{Start: start + indent, End: start + indent + len(key)})}
	if said, ok := file.sess.Loc(key, ""); ok {
		card.Label, card.Sites, card.Winner = said.Value, []Location{locate(said.At, said.Line, 0)}, 0
	}
	return card
}

// SignatureHelp is the declared usage of the token whose block holds the cursor, with its $NAME$
// and {NAME} parameters; nil for a word with no usage.
func SignatureHelp(sess *session.Session, path string, line, col int) (*Signature, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	found, _, _ := sess.Located(path)
	reader := index.Probe(found, file.parsed, sess.Reader(), file.offset(line, col))
	for _, level := range slices.Backward(reader.Chain) {
		if level.Key.Type != schema.TypeToken {
			continue
		}
		docs, usage, _, _ := tokenDocs(sess.Install.Vocab, level.Key.Name)
		if usage == "" {
			return nil, nil
		}
		help := &Signature{Label: usage, Docs: docs}
		for _, span := range jomini.Interpolations(usage) {
			help.Parameters = append(help.Parameters, span.Name)
		}
		for part := range strings.SplitSeq(usage, "{") {
			if name, _, ok := strings.Cut(part, "}"); ok && jomini.IsDefName(strings.TrimSpace(name)) {
				help.Parameters = append(help.Parameters, strings.TrimSpace(name))
			}
		}
		return help, nil
	}
	return nil, nil
}

// tokenDocs is a declared token's prose and usage, from whichever vocabulary declares it.
func tokenDocs(vocab *schema.Vocab, name string) (docs, usage string, in []string, out string) {
	if effect, ok := vocab.Effects[name]; ok {
		return effect.Doc, effect.Usage, effect.In, effect.Out
	}
	if trigger, ok := vocab.Triggers[name]; ok {
		return trigger.Doc, trigger.Usage, trigger.In, trigger.Out
	}
	if modifier, ok := vocab.Modifiers[name]; ok {
		return modifier.Doc, "", modifier.Categories, ""
	}
	return "", "", nil, ""
}

// fieldDocs is what the game's info file says of a key under its parent, in the kind's folders.
func fieldDocs(sess *session.Session, level index.Level, key string) string {
	if level.Kind == nil {
		return ""
	}
	parent := ""
	if level.Field != nil {
		parent = level.Field.Name
	}
	return sess.Install.Info.Field(level.Kind.Folders, parent, key).Prose
}

// comment is the comment on the lines just above a line of a text, joined; "" where there is none.
func comment(text string, line int) string {
	lines := strings.Split(text, "\n")
	var above []string
	for i := line - 1; i >= 0 && i < len(lines); i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		above = append([]string{strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))}, above...)
	}
	return strings.Join(above, "\n")
}

// spell writes a field's accepts as the schema file spells them.
func spell(field *schema.Field) []string {
	if field == nil {
		return nil
	}
	var out []string
	for _, accept := range field.Accepts {
		switch accept.Type {
		case schema.TypeRef:
			out = append(out, "ref:"+accept.Kind)
		case schema.TypeEnum:
			out = append(out, "enum:"+strings.Join(accept.Enum, "|"))
		default:
			out = append(out, accept.Type.String())
		}
	}
	return out
}
