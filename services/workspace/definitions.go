// definitions.go reads what the game shows of a definition: a text as the
// one line a player reads, a definition's picture and the painting behind
// it, and the number a script value stands for.

package workspace

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/texture"
)

// unshown stands in a one-line text for what only the running game can fill
// in: a data call PMT cannot word, or a name no message holds.
const unshown = "…"

// maxJoinedTexts is how many texts of a customizable localization a
// one-line text lists before it ends in unshown. One of CK3's shows 300.
const maxJoinedTexts = 3

// titleStep ends a data call that shows the name of what it reached.
const titleStep = "GetName"

// conceptCall is the data function that links to a concept with the words
// it shows as its second argument: Vic3's achievements_l_english.yml:468
// writes [Concept('concept_state', '$concept_states|q$')].
const conceptCall = "Concept"

// iconEnding ends the name of a data function that shows an icon, as
// GetTextIcon. nameEndings end one that shows a name beside its icon or
// without it, as CK3's GetNameWithIcon and Vic3's GetNameNoIcon.
const iconEnding = "Icon"

var nameEndings = []string{"WithIcon", "NoIcon"}

// A customizable localization holds a block for each text it can show,
// which names the text's localization key.
const (
	customTextBlock = "text"
	customTextKey   = "localization_key"
)

// FlattenText returns a text of the game as the one line a player reads.
// Nested keys are filled in, a break is a space, and formats and icons are
// left out. A data call is the name of the concept or definition it shows,
// or the texts of its customizable localization joined by slashes. Any other
// call, and a key no message holds, is "…".
func (s *Session) FlattenText(mods *index.Index, text string) string {
	return strings.Join(strings.Fields(s.flattenText(mods, text, 0, nil)), " ")
}

func (s *Session) flattenText(mods *index.Index, text string, depth int, customs []index.Site) string {
	if depth > maxTextDepth {
		return unshown
	}

	var line strings.Builder
	formatEnd := 0
	for _, segment := range loc.Segments(text, 0) {
		// The markup inside a format's name follows the format's segment.
		if segment.Span.Start < formatEnd {
			continue
		}

		switch segment.Markup {
		case loc.MarkupText:
			line.WriteString(segment.Text)
		case loc.MarkupBreak:
			line.WriteByte(' ')
		case loc.MarkupFormat:
			formatEnd = segment.Span.End
		case loc.MarkupCall:
			line.WriteString(s.describeCall(mods, segment.Call, depth, customs))
		case loc.MarkupKey:
			message, _, ok := s.LookupText(mods, segment.Text)
			if !ok {
				line.WriteString(unshown)

				continue
			}

			line.WriteString(s.flattenText(mods, message, depth+1, customs))
		}
	}

	return line.String()
}

// describeCall returns a data call as a line of text says it: "" for an
// icon, the words a Concept call shows, the title of the concept or the
// definition the call names, the texts of its customizable localization, or
// "…" for any other call.
func (s *Session) describeCall(mods *index.Index, call *jomini.Call, depth int, customs []index.Site) string {
	if call == nil {
		return unshown
	}

	last := call.Steps[len(call.Steps)-1]
	function := strings.TrimSuffix(last.Text, lang.GUINoTooltip)
	named := function
	for _, ending := range nameEndings {
		named = strings.TrimSuffix(named, ending)
	}

	switch {
	case function == spaceCall:
		return " "
	case function == conceptCall && len(last.Arguments) == 2:
		return s.flattenText(mods, last.Arguments[1].Text, depth+1, customs)
	case named == function && strings.HasSuffix(function, iconEnding):
		return ""
	}

	references := s.Schema.ListCallReferences(*call, "")
	if len(call.Steps) == 1 && len(last.Arguments) == 0 {
		references = append(references, lang.Reference{Database: s.Schema.Concepts, Name: last.Text})
	}

	for _, reference := range references {
		winner, ok := mods.FindKept(s.Schema, reference)
		switch {
		case !ok:
		case reference.Database == lang.CustomLocalization:
			return s.describeCustom(mods, winner.Site, depth, customs)
		case reference.Database == s.Schema.Concepts || named == titleStep:
			return s.flattenText(mods, s.LookupDefinitionText(mods, winner.Site, lang.TitleKey), depth+1, customs)
		}
	}

	return unshown
}

// describeCustom returns the texts a customizable localization can show,
// each once, joined by slashes, as "daughter/son". Past maxJoinedTexts the
// rest are "…". It returns "…" if its file cannot be read, and for one of
// the customizable localizations already being described: CK3's
// MyCharacterIntroductionNoComma shows itself for a spouse or a parent.
func (s *Session) describeCustom(mods *index.Index, custom index.Site, depth int, customs []index.Site) string {
	tree, statement, err := s.FindStatement(mods, custom)
	if err != nil || slices.Contains(customs, custom) {
		return unshown
	}

	customs = append(customs, custom)

	var texts []string
	for child := range tree.Children(statement) {
		if len(texts) == maxJoinedTexts+1 {
			texts[maxJoinedTexts] = unshown

			break
		}

		key, _ := tree.FindChildValue(child, customTextKey)
		message, _, ok := s.LookupText(mods, key)
		if tree.Text(tree.Statements[child].Key) != customTextBlock || !ok {
			continue
		}

		text := strings.TrimSpace(s.flattenText(mods, message, depth+1, customs))
		if text != "" && !slices.Contains(texts, text) {
			texts = append(texts, text)
		}
	}

	return strings.Join(texts, "/")
}

// LookupTitle returns a definition's title as one line. It returns "" for a
// definition with no title, and for a title that is nothing but what the
// running game fills in.
func (s *Session) LookupTitle(mods *index.Index, definition index.Site) string {
	title := s.FlattenText(mods, s.LookupDefinitionText(mods, definition, lang.TitleKey))
	if strings.Trim(title, unshown+" /") == "" {
		return ""
	}

	return title
}

// LookupModifierTitle returns a modifier's name in the game's words, as one
// line: the text its format's prefix key names without that key's ending,
// else the text of the first key the game builds from the modifier's name.
// It returns "" where no text names the modifier.
func (s *Session) LookupModifierTitle(mods *index.Index, key string) string {
	reference := lang.Reference{Database: cmp.Or(s.Schema.ModifierTypes, lang.ModifierFormats), Name: key}
	if winner, ok := mods.FindKept(s.Schema, reference); ok {
		if tree, statement, err := s.FindStatement(mods, winner.Site); err == nil {
			prefix, _ := tree.FindChildValue(statement, lang.ModifierPrefix)
			text, _, _ := s.LookupText(mods, strings.TrimSuffix(prefix, lang.ModifierPrefixEnding))
			if title := s.FlattenText(mods, text); title != "" {
				return title
			}
		}
	}

	return s.FlattenText(mods, s.resolveText(mods, "", reference, lang.TitleKey))
}

// maxValueDepth is how many script values one may name in turn before its
// number is taken as not known.
const maxValueDepth = 8

// ResolveScriptValue returns the one number a script value stands for,
// through values that name another. It returns false where the game keeps no
// such value, where the value is a block that computes its number, and where
// values name each other.
func (s *Session) ResolveScriptValue(mods *index.Index, name string) (float64, bool) {
	for range maxValueDepth {
		winner, ok := mods.FindKept(s.Schema, lang.Reference{Database: s.Schema.Values, Name: name})
		if !ok {
			return 0, false
		}

		tree, statement, err := s.FindStatement(mods, winner.Site)
		if err != nil || tree.Statements[statement].IsBlock() {
			return 0, false
		}

		value := tree.Statements[statement].Value
		if number, ok := tree.ResolveNumber(value); ok {
			return number, true
		}

		name = tree.Unquoted(value)
	}

	return 0, false
}

// FindPicture returns the file of the picture a definition shows, and
// whether the game tints it in the color of the text beside it. An event
// shows the file it names, else the one its game's first chain leads to. Any
// other definition shows what the first of its database's picture rows leads
// to, and a later level of a building with none shows an earlier level's. It
// returns "" for a definition with no picture.
func (s *Session) FindPicture(mods *index.Index, definition index.Site) (string, bool) {
	if mods.ToReference(definition).Database == s.Schema.Events {
		return s.findEventPicture(mods, definition), false
	}

	seen := map[index.Site]bool{}
	for !seen[definition] {
		seen[definition] = true
		if file, silhouette := s.findRowPicture(mods, definition, false); file != "" {
			return file, silhouette
		}

		for _, link := range mods.ListLinks(s.Schema, definition, index.RelationLevel) {
			if link.Target == definition {
				definition = link.Source
			}
		}
	}

	return "", false
}

// FindPainting returns the file of the painting behind a definition's head.
// It returns "" for a definition with none.
func (s *Session) FindPainting(mods *index.Index, definition index.Site) string {
	file, _ := s.findRowPicture(mods, definition, true)

	return file
}

// findRowPicture returns the file the first of a database's picture rows
// leads to, a painting's rows or a picture's, and whether the game tints it.
func (s *Session) findRowPicture(mods *index.Index, definition index.Site, painting bool) (string, bool) {
	reference := mods.ToReference(definition)
	dialect := lang.LookupDialect(s.Game.ID)
	rows := slices.DeleteFunc(slices.Clone(lang.DefinitionPictures), func(row lang.PictureRow) bool {
		return row.Database != reference.Database || row.Painting != painting || !dialect.Matches(row.Games)
	})
	if len(rows) == 0 {
		return "", false
	}

	tree, statement, err := s.FindStatement(mods, definition)
	if err != nil {
		return "", false
	}

	for _, row := range rows {
		if file, ok := mods.ResolveTexture(resolvePicture(row, tree, statement, reference.Name)); ok {
			return file, row.Silhouette
		}
	}

	return "", false
}

// resolvePicture returns the path a picture row gives a definition: the file
// its key names, or the file named after it. It returns "" where the
// definition does not write the row's key.
func resolvePicture(row lang.PictureRow, tree *jomini.Tree, statement int32, name string) string {
	if row.Key == "" && row.Field == "" {
		return strings.Replace(row.Pattern, "%s", name, 1)
	}

	values := listValues(tree, statement, cmp.Or(row.Key, row.Field))
	if len(values) == 0 {
		return ""
	}

	value := values[0]
	if strings.HasPrefix(value, "@") {
		value, _ = tree.FindChildValue(0, value)
	}

	switch {
	case row.Field != "":
		return strings.Replace(row.Pattern, "%s", value, 1)
	case row.Folder != "" && !strings.HasPrefix(value, "gfx/"):
		return row.Folder + "/" + value
	}

	return value
}

// findEventPicture returns the picture file an event shows: the file it
// names under the game's file key, else the one the first of the game's
// chains leads to. A file that is no image, as a Vic3 .bk2, is none.
func (s *Session) findEventPicture(mods *index.Index, event index.Site) string {
	tree, statement, err := s.FindStatement(mods, event)
	if err != nil {
		return ""
	}

	rows := lang.LookupDialect(s.Game.ID).Events.Picture
	if file := resolveImage(mods, tree, statement, rows.FileKey); file != "" {
		return file
	}

	for _, chain := range rows.Chains {
		if file := s.followChain(mods, chain, tree, statement); file != "" {
			return file
		}
	}

	return ""
}

// followChain returns the picture file one chain leads an event to, step by
// step, "" where a step finds no definition. Where the last definition names
// no image, it goes on to the one its fallback key names.
func (s *Session) followChain(mods *index.Index, chain lang.PictureChain, tree *jomini.Tree, statement int32) string {
	var err error

	database := ""
	for _, step := range chain.Steps {
		target, ok := s.findStepTarget(mods, step, listValues(tree, statement, step.Key))
		if !ok {
			return ""
		}

		if tree, statement, err = s.FindStatement(mods, target); err != nil {
			return ""
		}

		database = step.Database
	}

	for range maxValueDepth {
		file := resolveImage(mods, tree, statement, chain.FileKey)
		names := listValues(tree, statement, chain.Fallback)
		if file != "" || chain.Fallback == "" || len(names) == 0 {
			return file
		}

		winner, ok := mods.FindKept(s.Schema, lang.Reference{Database: database, Name: names[0]})
		if !ok {
			return ""
		}

		if tree, statement, err = s.FindStatement(mods, winner.Site); err != nil {
			return ""
		}
	}

	return ""
}

// findStepTarget returns the definition one step of a chain leads to: the
// one a value names, or with a match key the first the game keeps that
// writes every value under that key. It returns false for no values and
// where no definition fits.
func (s *Session) findStepTarget(mods *index.Index, step lang.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}

	if step.Match == "" {
		winner, ok := mods.FindKept(s.Schema, lang.Reference{Database: step.Database, Name: values[0]})

		return winner.Site, ok
	}

	for _, winner := range mods.ListKept(s.Schema, step.Database) {
		tree, statement, err := s.FindStatement(mods, winner.Site)
		if err != nil {
			continue
		}

		written := listValues(tree, statement, step.Match)
		if !slices.ContainsFunc(values, func(value string) bool { return !slices.Contains(written, value) }) {
			return winner.Site, true
		}
	}

	return index.Site{}, false
}

// resolveImage returns the image file the value under a key path names. It
// returns "" where no value is written, where the game reads no such file,
// and for a file that is no image.
func resolveImage(mods *index.Index, tree *jomini.Tree, statement int32, keyPath string) string {
	values := listValues(tree, statement, keyPath)
	if len(values) == 0 {
		return ""
	}

	file, ok := mods.ResolveTexture(values[0])
	if !ok || !texture.IsImage(file) {
		return ""
	}

	return file
}

// listValues returns the values a key path leads to below a statement, the
// path's keys joined by slashes. Each key but the last names a block. Of
// several blocks of one key, key[field=value] takes the one whose field has
// that value; otherwise the first with no trigger is read, else the first.
// A last key that is a block gives the values written in it. An empty path
// gives none.
func listValues(tree *jomini.Tree, statement int32, keyPath string) []string {
	if keyPath == "" {
		return nil
	}

	keys := strings.Split(keyPath, "/")
	for _, key := range keys[:len(keys)-1] {
		block, ok := pickBlock(tree, statement, key)
		if !ok {
			return nil
		}

		statement = block
	}

	var values []string
	for child := range tree.Children(statement) {
		current := tree.Statements[child]
		switch {
		case tree.Text(current.Key) != keys[len(keys)-1]:
		case !current.IsBlock():
			values = append(values, tree.Unquoted(current.Value))
		default:
			for item := range tree.Children(child) {
				if !tree.Statements[item].IsBlock() {
					values = append(values, tree.Unquoted(tree.Statements[item].Value))
				}
			}
		}
	}

	return values
}

// pickBlock returns the block one key of a path names among a statement's
// children, as listValues says. It returns false if there is none.
func pickBlock(tree *jomini.Tree, statement int32, key string) (int32, bool) {
	name, filter, isFiltered := strings.Cut(strings.TrimSuffix(key, "]"), "[")
	field, wanted, _ := strings.Cut(filter, "=")

	first := int32(0)
	for child := range tree.Children(statement) {
		current := tree.Statements[child]
		if tree.Text(current.Key) != name || !current.IsBlock() {
			continue
		}

		written, _ := tree.FindChildValue(child, field)
		_, isConditional := tree.FindChild(child, lang.TriggerKey)
		switch {
		case isFiltered && written == wanted, !isFiltered && !isConditional:
			return child, true
		case !isFiltered && first == 0:
			first = child
		}
	}

	return first, first != 0
}
