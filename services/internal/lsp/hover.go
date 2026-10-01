// hover.go is the card for the word under the cursor: one switch on what the reader made of it. A
// literal or a comment has no card. Signature help is a declared command's usage. A card's prose is
// what the game declares about a command, event target or key, and the comment above a definition,
// read from the file when the card is built and never stored.
package lsp

import (
	"cmp"
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
	sess, word := file.sess, site.Value
	card := &Inspect{Word: word, Range: file.span(site.Range), Winner: -1, Scopes: site.Level.Scopes}
	if assignment, ok := site.Level.Statement.(*jomini.Assignment); ok {
		card.Key = assignment.Key.Text
	}
	card.Head = file.head(word, card.Key)
	start := site.Range.Start
	if site.Quoted {
		start++
	}
	if steps, concept := dataCall(site.Text, start, site.Offset); steps != nil {
		file.dataCard(card, steps, concept, site.Offset)
		return card
	}

	dumps, below := sess.Schema.Dumps, site.Level.Below
	switch word.Type {
	case schema.WordNone, schema.WordString, schema.WordBool, schema.WordDataCall:
		return nil
	case schema.WordNumber:
		if site.Weight == 0 {
			return nil
		}
		card.Weight = site.Weight
	case schema.WordDate:
		card.Date = date(site.Text, file.place.Calendar)
	case schema.WordCommand:
		file.command(card, word.Name)
	case schema.WordScopePath:
		first, _, _ := strings.Cut(site.Text, ".")
		if target, ok := dumps.EventTargets[strings.ToLower(first)]; ok {
			card.Prose, card.Source = target.Doc, "the dumps"
		}
	case schema.WordKey, schema.WordEnum:
		if below != nil {
			card.Prose, card.Takes, card.Source = below.Doc, file.takes(below), layer(below)
		}
		// a block's key named like a target is the target where the dump lets it stand in this scope
		// (owner = c:FRA in a state), and a key elsewhere (a message's title = some_key)
		at := site.Level.At
		if target, ok := dumps.EventTargets[site.Text]; ok && at != nil && at.Block != schema.BlockNone && slices.Contains(target.In, site.Level.Scopes.This) {
			card.Prose, card.Source = target.Doc, "the dumps"
		}
	case schema.WordReference:
		file.defined(card, word, site.Path.Top)
	case schema.WordLoc:
		if said, ok := sess.Loc(word.Name, ""); ok {
			card.Name, card.Sites, card.Winner = loc.Plain(said.Value), []Location{locate(said.At, said.Line, 0)}, 0
		}
	case schema.WordFile:
		if found, ok := index.All(sess.All()).Find(word.Name); ok {
			card.Sites, card.Winner = []Location{locate(found, 0, 0)}, 0
			if found.Format == source.Image {
				if info, err := source.ReadImageInfo(found.Path); err == nil {
					card.Image = &info
				}
			}
		}
	case schema.WordConstant:
		for _, found := range sess.Defs(word.Name) {
			if found.Database == "constants" && index.PathKey(found.At.Path) == index.PathKey(file.path) {
				card.Sites, card.Winner = []Location{locate(found.At, int(found.Line), int(found.Col))}, 0
				if text, err := sess.Text(found.At.Path); err == nil {
					_, value, _ := strings.Cut(lineOf(text, int(found.Line)), "=")
					card.Name = strings.TrimSpace(value)
				}
			}
		}
	case schema.WordParameter:
		if database := sess.Schema.Databases[site.Path.Database]; database != nil && database.Parameters {
			file.parameter(card, word.Name, site.Path.Top)
		}
	case schema.WordUnknown:
		if !site.IsKey && site.Level.Key.Type == schema.WordCommand {
			card.Prose, _, _, _ = commandDocs(dumps, site.Level.Key.Name)
		}
	}
	if tagged, ok := file.colour(site); ok {
		card.Color = tagged
	}
	if word.Type == schema.WordUnknown && card.Prose == "" && card.Color == "" {
		return nil // a word nothing claims, with nothing known about it, has no card
	}
	return card
}

// defined fills a definition's sites in load order, the kept one, its name from its database's
// first loc key, and the comment above.
// A name a script makes is saved, not defined: its sites are the saves in top, the definition the
// cursor is in, else in this file, else anywhere, and none of them replaces another.
func (file *file) defined(card *Inspect, word schema.Word, top string) {
	sess := file.sess
	var found []index.Found
	for _, name := range sess.Defs(word.Name) {
		if name.Database == word.Database && (word.Parent == "" || name.Parent == word.Parent) {
			found = append(found, name)
		}
	}
	// a name a script makes is nearest in the definition, then the file; an inline entry or a GUI local
	// one in the file
	_, scriptMade := schema.ScriptMade[word.Database]
	database := sess.Schema.Databases[word.Database]
	inline := database != nil && len(database.Folders) > 0 && slices.ContainsFunc(found, func(name index.Found) bool {
		return !slices.Contains(sess.Schema.DatabasesAt(name.At.Rel), database)
	})
	nearest := []func(index.Found) bool{
		func(name index.Found) bool { return name.Parent == top },
		func(name index.Found) bool { return index.PathKey(name.At.Path) == index.PathKey(file.path) },
	}
	if !scriptMade {
		nearest = nearest[1:]
	}
	for i := 0; (scriptMade || inline || database != nil && strings.HasPrefix(database.Naming.Keyword, "local_")) && i < len(nearest); i++ {
		if local := slices.DeleteFunc(slices.Clone(found), func(name index.Found) bool { return !nearest[i](name) }); len(local) > 0 {
			found = local
			break
		}
	}
	if len(found) == 0 {
		return
	}
	kept, at := sess.Winner(found)
	for _, definition := range found {
		card.Sites = append(card.Sites, locate(definition.At, int(definition.Line), int(definition.Col)))
	}
	card.Winner = at
	if scriptMade {
		card.Winner = -1
	}
	if database == nil || len(database.CalledIn) == 0 {
		card.Graphed = sess.Fired(word.Database) || len(sess.Fires(word.Name, map[string]bool{})) > 0
	}
	if database == nil {
		return
	}

	// its name and description from its loc keys (a _desc key describes), else the docs' blurb of it,
	// else the comment above it; the database's first paragraph of docs
	for _, key := range loc.Expand(word.Name, database.LocKeys) {
		said, ok := sess.Loc(key, "")
		switch {
		case !ok:
		case strings.HasSuffix(key, "_desc"):
			card.Prose = cmp.Or(card.Prose, loc.Plain(said.Value))
		default:
			card.Name = cmp.Or(card.Name, loc.Plain(said.Value))
		}
	}
	for _, folder := range database.Folders {
		if docs := sess.Schema.Docs[strings.ToLower(folder)]; docs != nil {
			about, _, _ := strings.Cut(docs.About, "\n\n")
			card.About, card.Prose = cmp.Or(card.About, about), cmp.Or(card.Prose, docs.Blurbs[word.Name])
		}
	}
	text, err := sess.Text(kept.At.Path)
	if err == nil {
		card.Prose = cmp.Or(card.Prose, comment(text, int(kept.Line)))
	}

	// a scripted entry's parameters; an entry written as one number shows it (a script value)
	var parameters []string
	for _, idx := range sess.All() {
		for _, def := range idx.Defs {
			if database.Parameters && def.Database == "params" && def.Parent == word.Name {
				parameters = append(parameters, "$"+def.Name+"$")
			}
		}
	}
	if parameters = slices.Compact(slices.Sorted(slices.Values(parameters))); len(parameters) > 0 {
		card.Prose = strings.TrimSpace(card.Prose + "\n\nParameters: " + strings.Join(parameters, ", "))
	}
	if _, value, ok := strings.Cut(lineOf(text, int(kept.Line)), "="); ok && jomini.Literal(strings.TrimSpace(value)) == jomini.LitNumber {
		card.Name = strings.TrimSpace(value)
	}
}

// parameter fills a scripted effect parameter's sites and the words written for it where the
// scripted effect is called.
func (file *file) parameter(card *Inspect, name, scripted string) {
	sess := file.sess
	for _, definition := range sess.Defs(name) {
		if definition.Database == "params" && definition.Parent == scripted {
			card.Sites, card.Winner = append(card.Sites, locate(definition.At, int(definition.Line), int(definition.Col))), 0
		}
	}
	seen, all := map[string]int{}, sess.All()
	for _, idx := range all {
		for _, cited := range idx.Referring(name) {
			if cited.Database == "params" && cited.Parent == scripted && all.Loaded(idx, cited.File) {
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

// date writes a date out (January 1st, 8082 AD; year -221 is 221 BC), and in the mod's calendar
// where one of its eras applies (82.1.1 A.C.).
func date(text string, calendar session.Calendar) string {
	year, month, day, _, ok := jomini.ParseDate(strings.Trim(text, `"`))
	if !ok {
		return ""
	}
	months := [...]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	out := fmt.Sprintf("%s %s, %d AD", months[month], ordinal(day), year)
	if year < 0 {
		out = fmt.Sprintf("%s %s, %d BC", months[month], ordinal(day), -year)
	}
	if inEra, era, ok := calendar.Read(year); ok {
		out += fmt.Sprintf("; %d.%d.%d %s in this mod's calendar", inEra, month, day, era)
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
	if steps, concept := dataCall(line, start, offset); steps != nil {
		card := &Inspect{Winner: -1}
		file.dataCard(card, steps, concept, offset)
		return card
	}
	for _, span := range loc.Interps(line, start) {
		if within(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End}, offset) {
			name := span.Key
			card := &Inspect{Word: schema.Word{Type: schema.WordParameter, Name: name}, Winner: -1, Range: file.span(jomini.Range{Start: span.WrapRange.Start, End: span.WrapRange.End})}
			if said, ok := file.sess.Loc(name, ""); ok {
				card.Word.Type, card.Name, card.Sites, card.Winner = schema.WordLoc, said.Value, []Location{locate(said.At, said.Line, 0)}, 0
			}
			return card
		}
	}
	key, _, _ := strings.Cut(strings.TrimLeft(line, " \t"), ":")
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	if key == "" || strings.HasPrefix(key, "#") || strings.HasPrefix(key, "l_") || !within(jomini.Range{Start: start + indent, End: start + indent + len(key)}, offset) {
		return nil
	}
	card := &Inspect{Word: schema.Word{Type: schema.WordLoc, Name: key}, Winner: -1, Range: file.span(jomini.Range{Start: start + indent, End: start + indent + len(key)})}
	if said, ok := file.sess.Loc(key, ""); ok {
		card.Name, card.Sites, card.Winner = said.Value, []Location{locate(said.At, said.Line, 0)}, 0
	}
	return card
}

// SignatureHelp is the declared usage of the command whose block holds the cursor, with its $NAME$
// and {NAME} parameters; nil for a word with no usage.
func SignatureHelp(sess *session.Session, path string, line, col int) (*Signature, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	found, _, _ := sess.Located(path)
	offset := file.offset(line, col)
	reader := index.Probe(found, file.parsed, sess.Reader(), offset)
	// only the block the cursor is directly in, past the statement it is on: a scope or a key
	// written inside a command's block is no argument of it
	for _, level := range slices.Backward(reader.Chain) {
		assignment, ok := level.Statement.(*jomini.Assignment)
		if !ok {
			continue
		}
		if block := jomini.BlockOf(assignment.Value); block == nil || offset <= block.OpenBrace || offset > block.CloseBrace {
			continue
		}
		if level.Key.Type != schema.WordCommand {
			return nil, nil
		}
		docs, usage, _, _ := commandDocs(sess.Schema.Dumps, level.Key.Name)
		if usage == "" {
			return nil, nil
		}
		if takes := file.takes(sess.Schema.Arguments[level.Key.Name]); len(takes) > 0 {
			docs = strings.TrimSpace(docs + "\n\nTakes: " + strings.Join(takes, ", "))
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

// commandDocs is a declared command's prose and usage, from whichever dump declares it.
func commandDocs(dumps *schema.Dumps, name string) (docs, usage string, in []string, out string) {
	if effect, ok := dumps.Effects[name]; ok {
		return effect.Doc, effect.Usage, effect.In, effect.Out
	}
	if trigger, ok := dumps.Triggers[name]; ok {
		return trigger.Doc, trigger.Usage, trigger.In, trigger.Out
	}
	if modifier, ok := dumps.Modifiers[name]; ok {
		return modifier.Doc, "", modifier.Categories, ""
	}
	return "", "", nil, ""
}

// head is what a word is, in plain words: a command's kind, an entry's head, a key by its name.
func (file *file) head(word schema.Word, key string) string {
	dumps := file.sess.Schema.Dumps
	_, effect := dumps.Effects[word.Name]
	_, trigger := dumps.Triggers[word.Name]
	switch {
	case word.Type == schema.WordCommand && effect && trigger:
		return "effect and trigger"
	case word.Type == schema.WordCommand && effect:
		return "effect"
	case word.Type == schema.WordCommand && trigger:
		return "trigger"
	case word.Type == schema.WordReference:
		return file.entryHead(word.Database)
	case word.Type == schema.WordKey || word.Type == schema.WordEnum:
		return key + " key"
	}
	return map[schema.WordType]string{
		schema.WordCommand: "modifier", schema.WordScopePath: "event target", schema.WordLoc: "localization key",
		schema.WordFile: "file", schema.WordConstant: "script constant", schema.WordParameter: "parameter",
		schema.WordDataCall: "data function", schema.WordDate: "date",
	}[word.Type]
}

// entryHead is an entry's head: the game concept its database is named for, else the database's
// name made singular; a list's entries are headed by its folder (sound).
func (file *file) entryHead(name string) string {
	if database := file.sess.Schema.Databases[name]; database != nil && database.Naming.Form == "line" {
		folder, _, _ := strings.Cut(name, ".")
		return folder
	}
	last := name[strings.LastIndexAny(name, "./")+1:]
	singular := strings.TrimSuffix(last, "s")
	if stem, ok := strings.CutSuffix(last, "ies"); ok {
		singular = stem + "y"
	}
	return cmp.Or(file.concept(singular), strings.ReplaceAll(singular, "_", " "))
}

// concept is the text of the game concept named word, or concept_word; "" for none.
func (file *file) concept(word string) string {
	sess := file.sess
	concepts := sess.Schema.Databases["game_concepts"]
	for _, name := range []string{word, "concept_" + word} {
		if concepts != nil && len(concepts.LocKeys) > 0 && sess.All().Def(concepts.Name, name) {
			if said, ok := sess.Loc(loc.Expand(name, concepts.LocKeys[:1])[0], ""); ok {
				return loc.Plain(said.Value)
			}
		}
	}
	return ""
}

// command fills a command's card: its effect or trigger localization's text, else the dumps'
// description; what it takes; where an iterator goes; a modifier's own text and its dump name.
func (file *file) command(card *Inspect, name string) {
	sess, dumps := file.sess, file.sess.Schema.Dumps
	docs, _, in, out := commandDocs(dumps, name)
	card.Prose, card.Takes, card.Source = cmp.Or(file.localized(name), docs), file.takes(sess.Schema.Arguments[name]), "the dumps"
	if out != "" {
		card.Head += " that goes through each " + strings.ReplaceAll(out, "_", " ")
	}
	if modifier, ok := dumps.Modifier(name); ok && card.Head == "modifier" {
		said, _ := sess.Loc(name, "")
		card.Name = loc.Plain(cmp.Or(said.Value, modifier.Label))
	}
	if card.Scopes.This == "" && len(in) == 1 {
		card.Scopes.This = in[0]
	}
}

// localized is the text an effect_localization or trigger_localization entry gives a command: its
// global line's; "" for none.
func (file *file) localized(command string) string {
	sess := file.sess
	for _, found := range sess.Defs(command) {
		if found.Database != "effect_localization" && found.Database != "trigger_localization" {
			continue
		}
		parsed, _, err := sess.Parsed(found.At.Path)
		if err != nil {
			continue
		}
		for _, st := range parsed.Root.Statements {
			entry, ok := st.(*jomini.Assignment)
			if !ok || entry.Key.Text != command || jomini.BlockOf(entry.Value) == nil {
				continue
			}
			for _, line := range jomini.BlockOf(entry.Value).Statements {
				global, ok := line.(*jomini.Assignment)
				if !ok || global.Key.Text != "global" {
					continue
				}
				if scalar, isWord := global.Value.(*jomini.Scalar); isWord {
					said, _ := sess.Loc(scalar.Text, "")
					return loc.Plain(said.Value)
				}
			}
		}
	}
	return ""
}

// takes is what a word at a key may be, in plain words; a long word list ends with how many more.
func (file *file) takes(key *schema.Key) []string {
	if key == nil {
		return nil
	}
	var out []string
	for _, take := range key.Takes {
		switch take.Type {
		case schema.WordReference:
			out = append(out, plain(file.entryHead(take.Database)))
		case schema.WordScopePath:
			out = append(out, plain(take.Database))
		case schema.WordEnum:
			words := take.Enum
			if len(words) > 6 {
				words = append(words[:6:6], fmt.Sprintf("… and %d more", len(take.Enum)-6))
			}
			out = append(out, "one of "+strings.Join(words, ", "))
		default:
			out = append(out, map[schema.WordType]string{
				schema.WordBool: "yes or no", schema.WordNumber: "a number", schema.WordDate: "a date",
				schema.WordLoc: "a localization key", schema.WordFile: "a file path",
			}[take.Type])
		}
	}
	return slices.Compact(out)
}

// dataCard fills a data call step's card: a concept link's game concept; a quoted argument's entry
// where its function returns a database's; a function the globals or the step before's return type
// declare; a data type written first; else a loaded data_binding macro.
func (file *file) dataCard(card *Inspect, steps []jomini.Step, concept bool, offset int) {
	step := steps[len(steps)-1]
	function, dataType := file.function(steps)
	card.Range, card.Word = file.span(step.Range), schema.Word{Type: schema.WordDataCall, Name: step.Name}
	for _, arg := range step.Args {
		if database := file.sess.Schema.DatabaseNamed(function.Returns); arg.Quoted && within(arg.Range, offset) && database != "" {
			card.Range, card.Word = file.span(arg.Range), schema.Word{Type: schema.WordReference, Database: database, Name: arg.Text}
		}
	}
	switch {
	case card.Word.Type == schema.WordReference:
	case concept && len(steps) == 1:
		card.Word = schema.Word{Type: schema.WordReference, Database: "game_concepts", Name: step.Name}
	case function.Name != "":
		card.Head, card.Prose = "data function", strings.TrimSpace(function.Doc+"\n\nReturns "+function.Returns)
		return
	case dataType != "":
		card.Head, card.Prose = "data type", file.sess.Schema.Dumps.DataTypes[dataType].Doc
		return
	default:
		card.Word = schema.Word{Type: schema.WordReference, Database: "data_binding", Name: step.Name}
	}
	card.Head = file.entryHead(card.Word.Database)
	file.defined(card, card.Word, "")
}

// function is what a data call's last step is: a function the globals or the step before's return
// type declare, or, written first, the data type of its name.
func (file *file) function(steps []jomini.Step) (schema.DataFunction, string) {
	dumps, dataType := file.sess.Schema.Dumps, ""
	for i, step := range steps {
		function, ok := dumps.DataTypes[dataType].Functions[step.Name]
		if i == 0 {
			function, ok = dumps.Globals[step.Name]
		}
		if _, isType := dumps.DataTypes[step.Name]; i == 0 && !ok && isType {
			dataType = step.Name
			continue
		}
		if !ok {
			return schema.DataFunction{}, ""
		}
		if i == len(steps)-1 {
			return function, ""
		}
		dataType = function.Returns
	}
	return schema.DataFunction{}, dataType
}

// layer is where a key's facts come from, in plain words.
func layer(key *schema.Key) string {
	if key.Source == schema.SourceWiki {
		return fmt.Sprintf("the wiki: %s (r%d)", key.Page, key.Revision)
	}
	return map[schema.Source]string{
		schema.SourceVanilla: "vanilla", schema.SourceDumps: "the dumps", schema.SourceDocs: "the docs", schema.SourceRegistry: "the registry",
	}[key.Source]
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
