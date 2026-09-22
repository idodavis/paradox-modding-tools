// hover.go is the card for the word under the cursor: one switch on what the reader made of it. A literal has no card; a comment has none. Signature help is a declared token's usage.
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
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// Hover is the card for the word at a position, or nil where nothing is said about it.
func Hover(s *session.Session, path string, line, col int) (*Inspect, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	if f.place.Format == source.Loc {
		return f.locCard(f.offset(line, col)), nil
	}
	w := f.at(f.offset(line, col))
	if w == nil {
		return nil, nil
	}
	return f.card(w), nil
}

// card fills the card for a word by its type.
func (f *file) card(w *site) *Inspect {
	s, v := f.s, w.Value
	card := &Inspect{Value: v, Range: f.span(w.Range), Winner: -1, Scope: w.Level.Scope}
	if a, ok := w.Level.Statement.(*jomini.Assignment); ok {
		card.Key = a.Key.Text
	}

	switch v.Type {
	case schema.TypeNone, schema.TypeString, schema.TypeBool, schema.TypeCall:
		return nil
	case schema.TypeNumber:
		if w.Weight == 0 {
			return nil
		}
		card.Weight = w.Weight
	case schema.TypeDate:
		card.Date = date(w.Text, f.place.Calendar)
	case schema.TypeToken:
		docs, usage, in, out := tokenDocs(s.Install.Vocab, v.Name)
		card.Docs, card.Usage, card.ScopeOut, card.Accepts = docs, usage, out, spell(w.Level.Below)
		if card.Scope == "" && len(in) == 1 {
			card.Scope = in[0]
		}
	case schema.TypeScopePath:
		first, _, _ := strings.Cut(w.Text, ".")
		if link, ok := s.Install.Vocab.Links[strings.ToLower(first)]; ok {
			card.Docs, card.ScopeOut = link.Doc, link.Out
		}
	case schema.TypeMember, schema.TypeEnum:
		card.Class, card.Accepts, card.Docs = w.Level.Below.Class.String(), spell(w.Level.Below), fieldDocs(s, w.Level, w.Text)
	case schema.TypeRef:
		f.defined(card, v)
	case schema.TypeLoc:
		if said, ok := s.Loc(v.Name, ""); ok {
			card.Label, card.Sites, card.Winner = said.Value, []Location{locate(said.At, said.Line, 0)}, 0
		}
	case schema.TypeFile:
		if found, ok := index.All(s.All()).Find(v.Name); ok {
			card.Sites, card.Winner = []Location{locate(found, 0, 0)}, 0
			if found.Format == source.Image {
				if info, err := source.ReadImageInfo(found.Path); err == nil {
					card.Image = &info
				}
			}
		}
	case schema.TypeConstant:
		for _, found := range s.Defs(v.Name) {
			if found.Kind == "constants" && index.PathKey(found.At.Path) == index.PathKey(f.path) {
				card.Sites, card.Winner = []Location{locate(found.At, int(found.Line), int(found.Col))}, 0
				if text, err := s.Text(found.At.Path); err == nil {
					_, value, _ := strings.Cut(lineOf(text, int(found.Line)), "=")
					card.Label = strings.TrimSpace(value)
				}
			}
		}
	case schema.TypeParam:
		if k := s.Schema.Kinds[w.Path.Kind]; k != nil && k.Params {
			f.parameter(card, v.Name, w.Path.Top)
		}
	case schema.TypeUnresolved:
		if !w.IsKey && w.Level.Key.Type == schema.TypeToken {
			card.Docs, _, _, _ = tokenDocs(s.Install.Vocab, w.Level.Key.Name)
		}
	}
	if tagged, ok := f.colour(w); ok {
		card.Color = tagged
	}
	return card
}

// defined fills a definition's sites in load order, the one the game keeps, its label and the comment above it.
func (f *file) defined(card *Inspect, v schema.Value) {
	s := f.s
	var found []index.Found
	for _, d := range s.Defs(v.Name) {
		if d.Kind == v.Kind && (v.Parent == "" || d.Parent == v.Parent) {
			found = append(found, d)
		}
	}
	if len(found) == 0 {
		return
	}
	kept, at := s.Winner(found)
	for _, d := range found {
		card.Sites = append(card.Sites, locate(d.At, int(d.Line), int(d.Col)))
	}
	card.Winner, card.Label, card.ScopeOut = at, kept.Label, kept.ScopeType
	if k := s.Schema.Kinds[v.Kind]; card.Label == "" && k != nil && len(k.LocTemplates) > 0 {
		if said, ok := s.Loc(loc.Expand(v.Name, k.LocTemplates[:1])[0], ""); ok {
			card.Label = said.Value
		}
	}
	if text, err := s.Text(kept.At.Path); err == nil {
		card.Docs = comment(text, int(kept.Line))
	}
}

// parameter fills a macro parameter's sites and the words written for it where the macro is called.
func (f *file) parameter(card *Inspect, name, macro string) {
	s := f.s
	for _, d := range s.Defs(name) {
		if d.Kind == "params" && d.Parent == macro {
			card.Sites, card.Winner = append(card.Sites, locate(d.At, int(d.Line), int(d.Col))), 0
		}
	}
	seen := map[string]int{}
	for _, x := range s.All() {
		for _, r := range x.Referring(name) {
			if r.Kind == "params" && r.Parent == macro {
				if text, err := s.Text(r.At.Path); err == nil {
					if _, value, ok := strings.Cut(lineOf(text, int(r.Line)), "="); ok {
						seen[strings.TrimSpace(value)]++
					}
				}
			}
		}
	}
	for text, n := range seen {
		card.Values = append(card.Values, Seen{Text: text, Count: n})
	}
	slices.SortFunc(card.Values, func(a, b Seen) int { return strings.Compare(a.Text, b.Text) })
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

// date writes a date out (January 1st, 8082 AD), and in the mod's calendar where one is declared and the year stays positive.
func date(text string, calendar session.Calendar) string {
	y, m, d, _, ok := jomini.ParseDate(strings.Trim(text, `"`))
	if !ok {
		return ""
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	out := fmt.Sprintf("%s %s, %d AD", months[m], ordinal(d), y)
	if calendar.Era != "" && y-calendar.EpochOffset > 0 {
		out += fmt.Sprintf("; %d.%d.%d %s in this mod's calendar", y-calendar.EpochOffset, m, d, calendar.Era)
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

// colour is the #rrggbb of a block tagged rgb, hsv or hex that the cursor's statement writes, for a swatch.
func (f *file) colour(w *site) (string, bool) {
	a, ok := w.Level.Statement.(*jomini.Assignment)
	if !ok || !w.IsKey {
		return "", false
	}
	tagged, ok := a.Value.(*jomini.TaggedBlock)
	if !ok {
		return "", false
	}
	var parts []float64
	for _, st := range tagged.Block.Statements {
		if v, ok := st.(*jomini.ValueStmt); ok {
			if scalar, ok := v.Value.(*jomini.Scalar); ok {
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
			h, sat, val := parts[0], parts[1], parts[2]
			if strings.ToLower(tagged.Tag.Text) == "hsv360" {
				h, sat, val = h/360, sat/100, val/100
			}
			r, g, b := hsv(h, sat, val)
			return hex(r*255, g*255, b*255), true
		}
	case "hex":
		if len(tagged.Block.Statements) == 1 {
			if v, ok := tagged.Block.Statements[0].(*jomini.ValueStmt); ok {
				if scalar, ok := v.Value.(*jomini.Scalar); ok {
					return "#" + strings.TrimPrefix(strings.ToLower(scalar.Text), "0x")[:6], true
				}
			}
		}
	}
	return "", false
}

// hex spells a colour whose parts are 0 to 255, or 0 to 1 where every part is at most 1.
func hex(r, g, b float64) string {
	if r <= 1 && g <= 1 && b <= 1 {
		r, g, b = r*255, g*255, b*255
	}
	return fmt.Sprintf("#%02x%02x%02x", int(r+0.5), int(g+0.5), int(b+0.5))
}

// hsv turns hue, saturation and value, each 0 to 1, into red, green and blue, each 0 to 1.
func hsv(h, s, v float64) (float64, float64, float64) {
	i := int(h*6) % 6
	f := h*6 - float64(int(h*6))
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	switch i {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	}
	return v, p, q
}

// locCard is the card in a localization file: the key at the line's start, or a $key$ the value interpolates, which is a loc key where one exists and an engine value otherwise.
func (f *file) locCard(offset int) *Inspect {
	lines := jomini.NewLineIndex(f.text)
	pos := lines.PositionAt(offset)
	line := lineOf(f.text, pos.Line)
	start := lines.OffsetAt(pos.Line, 0)
	for _, span := range loc.Interps(line, start) {
		if within(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End}, offset) {
			name := span.Key
			card := &Inspect{Value: schema.Value{Type: schema.TypeParam, Name: name}, Winner: -1, Range: f.span(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End})}
			if said, ok := f.s.Loc(name, ""); ok {
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
	card := &Inspect{Value: schema.Value{Type: schema.TypeLoc, Name: key}, Winner: -1, Range: f.span(jomini.Range{Start: start + indent, End: start + indent + len(key)})}
	if said, ok := f.s.Loc(key, ""); ok {
		card.Label, card.Sites, card.Winner = said.Value, []Location{locate(said.At, said.Line, 0)}, 0
	}
	return card
}

// SignatureHelp is the declared usage of the token whose block the cursor is in, with its $NAME$ and {NAME} parameters; nil for a word with no usage.
func SignatureHelp(s *session.Session, path string, line, col int) (*Signature, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	found, _, _ := s.Located(path)
	d := index.Probe(found, f.text, s.Reader(), f.offset(line, col))
	for i := len(d.Chain) - 1; i >= 0; i-- {
		if d.Chain[i].Key.Type != schema.TypeToken {
			continue
		}
		docs, usage, _, _ := tokenDocs(s.Install.Vocab, d.Chain[i].Key.Name)
		if usage == "" {
			return nil, nil
		}
		help := &Signature{Label: usage, Docs: docs}
		for _, span := range jomini.Interpolations(usage) {
			help.Parameters = append(help.Parameters, span.Name)
		}
		for _, part := range strings.Split(usage, "{") {
			if name, _, ok := strings.Cut(part, "}"); ok && jomini.IsDefName(strings.TrimSpace(name)) {
				help.Parameters = append(help.Parameters, strings.TrimSpace(name))
			}
		}
		return help, nil
	}
	return nil, nil
}
