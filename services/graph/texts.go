// texts.go reads the texts a definition shows, and writes a text of the game
// as the one line a player reads.

package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Section is one key at the top of a definition that shows a text, or one of
// its options.
type Section struct {
	Key string `json:"key"`

	// Line counts from one.
	Line int `json:"line"`

	// When is a block's own limit, else its trigger, in words.
	When []Condition `json:"when,omitempty"`

	Texts []Version `json:"texts,omitempty"`
}

// Version is one text a key can show, with the trigger that decides it.
type Version struct {
	// Key is the text's localization key.
	Key  string `json:"key"`
	Text string `json:"text"`

	// Segments are the pieces of the text, each data call in words.
	Segments []loc.Segment `json:"segments"`

	// Missing is set where the detail's language has no message for the key.
	Missing bool `json:"missing,omitempty"`

	// When is the text's trigger in words, nil for a text that always shows.
	When []Condition `json:"when,omitempty"`

	// Line counts from one.
	Line int `json:"line"`

	// Message is where the key's text is written: the detail's language's
	// line, else the base language's. It is the zero Site for neither.
	Message lsp.Site `json:"message"`
}

// callSample is the Sample of a text's piece that holds a data call in words.
const callSample = "text"

// textKeys are the top keys that are a good's, building's or technology's own
// texts. Any other text it writes is a link.
var textKeys = []string{lang.TitleKey, lang.DescriptionKey}

// listSections returns the keys at the top of a definition that show a text,
// and its options. A plain key shows a text where the index reads its value
// as a localization key, so a plain value that happens to be one is no text.
func listSections(
	ws *workspace.Session, mods *index.Index, site index.Site, tree *jomini.Tree, statement int32,
) ([]Section, []Section) {
	localized := map[int32]bool{}
	for _, use := range mods.ListReferences(site) {
		if mods.ToReference(use).Database == lang.Localization {
			localized[mods.Occurrence(use).Statement] = true
		}
	}

	sections, options := []Section{}, []Section{}
	for child := range tree.Children(statement) {
		current := tree.Statements[child]
		switch {
		case tree.Text(current.Key) == optionKey:
			options = append(options, makeSection(ws, mods, tree, child))

		case localized[child] || current.IsBlock():
			if section := makeSection(ws, mods, tree, child); len(section.Texts) > 0 {
				sections = append(sections, section)
			}
		}
	}

	return sections, options
}

// makeSection returns the section of one statement at the top of a
// definition, with its texts.
func makeSection(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Section {
	current := tree.Statements[statement]
	section := Section{Key: tree.Text(current.Key), Line: toSourceLine(tree, current.Key)}
	if !current.IsBlock() {
		if version, ok := makeVersion(ws, mods, tree, statement); ok {
			section.Texts = []Version{version}
		}

		return section
	}

	section.When = findCondition(ws, mods, tree, statement)
	section.Texts = listVersions(ws, mods, tree, statement)

	return section
}

// triggeredText is a description's text shown when its trigger holds, and
// textChoices the description blocks that choose one text inside them.
const triggeredText = "triggered_desc"

var textChoices = []string{"first_valid", "random_valid"}

// listVersions returns the texts a block can show, in the order written.
func listVersions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Version {
	var versions []Version
	for child := range tree.Children(statement) {
		key := strings.ToLower(tree.Text(tree.Statements[child].Key))
		switch {
		case key == triggeredText || key == lang.DescriptionKey || key == optionTitleKey:
			if version, ok := makeVersion(ws, mods, tree, child); ok {
				version.When = findCondition(ws, mods, tree, child)
				versions = append(versions, version)
			}

		case slices.Contains(textChoices, key):
			versions = append(versions, listVersions(ws, mods, tree, child)...)
		}
	}

	return versions
}

// makeVersion returns the text a statement shows. It returns false if it
// names no localization key.
func makeVersion(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (Version, bool) {
	label := statement
	if tree.Statements[statement].IsBlock() {
		var hasLabel bool
		if label, hasLabel = findLabel(tree, statement); !hasLabel {
			return Version{}, false
		}
	}

	value := tree.Statements[label].Value
	if value.IsEmpty() {
		return Version{}, false
	}

	key := tree.Unquoted(value)
	text, winner, ok := ws.LookupText(mods, key)
	hasMessage := ok && mods.Language(winner.Site) == resolveLanguage(ws)
	isKey := tree.Statements[label].ValueClass.IsIdentifier()
	version := Version{
		Key: key, Text: text, Segments: describeCalls(ws, mods, text),
		Missing: isKey && !hasMessage,
		Line:    toSourceLine(tree, tree.Statements[label].Key),
	}
	if ok {
		version.Message = lsp.MakeSite(ws, mods, winner.Site)
	}

	return version, true
}

// findLabel returns the statement of a block that holds its text: its
// description, else its label. It returns false if it has neither.
func findLabel(tree *jomini.Tree, block int32) (int32, bool) {
	for _, key := range []string{lang.DescriptionKey, lang.LabelKey} {
		if child, ok := tree.FindChild(block, key); ok {
			return child, true
		}
	}

	return 0, false
}

// describeCalls returns the pieces of a text with each data call in words.
func describeCalls(ws *workspace.Session, mods *index.Index, text string) []loc.Segment {
	segments := ws.ExpandText(mods, text)
	for position, segment := range segments {
		if segment.Call == nil {
			continue
		}

		step := finalStep(segment.Call)
		segments[position].Text = cmp.Or(describeCustom(ws, mods, step), lang.DescribeStep(step, true))
		segments[position].Sample = callSample
	}

	return segments
}

// finalStep returns the last step of a data call, without the ending of its
// name that only turns off its tooltip.
func finalStep(call *jomini.Call) jomini.Step {
	step := call.Steps[len(call.Steps)-1]
	step.Text = strings.TrimSuffix(step.Text, lang.GUINoTooltip)

	return step
}

// A customizable localization holds a block for each text it can show,
// which names the text's localization key.
const (
	customTextBlock = "text"
	customTextKey   = "localization_key"
)

// describeCustom returns the texts the customizable localization a call
// names can show, each once, joined by slashes, as "daughter/son". It
// returns "" if the call names none the workspace defines.
func describeCustom(ws *workspace.Session, mods *index.Index, step jomini.Step) string {
	position := slices.Index(lang.GUICallTargets[step.Text], lang.CustomLocalization)
	if position < 0 || position >= len(step.Arguments) || !step.Arguments[position].Quoted {
		return ""
	}

	reference := lang.Reference{Database: lang.CustomLocalization, Name: step.Arguments[position].Text}
	winner, ok := mods.FindKept(ws.Schema, reference)
	if !ok {
		return ""
	}

	tree, statement, err := ws.FindStatement(mods, winner.Site)
	if err != nil {
		return ""
	}

	var texts []string
	for child := range tree.Children(statement) {
		key, ok := tree.FindChildValue(child, customTextKey)
		if !ok || tree.Text(tree.Statements[child].Key) != customTextBlock {
			continue
		}

		message, _, _ := ws.LookupText(mods, key)
		if text := strings.TrimSpace(loc.ToPlain(loc.Segments(message, 0))); text != "" {
			texts = appendNew(texts, text)
		}
	}

	return strings.Join(texts, "/")
}

// describeKey returns the text a localization key holds as one line. It
// returns false if no message holds the key.
func describeKey(ws *workspace.Session, mods *index.Index, key string) (string, bool) {
	text, _, ok := ws.LookupText(mods, key)

	return describeLabel(ws, mods, text), ok
}

// describeLabel returns a text of the game as the one line a player reads:
// its nested keys filled in, each break a space and each data call in words.
// Its formats and icons are left out, with the calls in a format's name, as
// #tooltip:[GetPlayer.GetTooltipTag].
func describeLabel(ws *workspace.Session, mods *index.Index, text string) string {
	var label strings.Builder
	var format jomini.Span
	for _, segment := range ws.ExpandText(mods, text) {
		if segment.Span.Start >= format.Start && segment.Span.End <= format.End {
			continue
		}

		format = jomini.Span{}
		switch segment.Markup {
		case loc.MarkupFormat:
			format = segment.TextSpan
		case loc.MarkupText:
			label.WriteString(segment.Text)
		case loc.MarkupBreak:
			label.WriteString(" ")
		case loc.MarkupCall:
			label.WriteString(describeCall(ws, mods, segment))
		}
	}

	return strings.Join(strings.Fields(label.String()), " ")
}

// conceptTexts holds the localization key each game names a concept by, with
// %s for the concept. CK3's [woodland_taxes|E] is game_concept_woodland_taxes,
// "Hinterland Taxes", and EU5's [building|e] is game_concept_building,
// "Building" (main_menu/localization/english/game_concepts_l_english.yml:896).
// Vic3 links by the whole key, as [concept_bureaucracy].
var conceptTexts = map[string]string{game.CK3: "game_concept_%s", game.Vic3: "%s", game.EU5: "game_concept_%s"}

// titleStep ends a data call that shows the name of what it reached.
const titleStep = "GetName"

// conceptCall is the data function that links to a concept with the words
// it shows as its second argument: Vic3's achievements_l_english.yml:468
// writes [Concept('concept_state', '$concept_states|q$')].
const conceptCall = "Concept"

// iconEnding ends the name of a data function that shows an icon, as
// GetTextIcon, and nameEndings those of one that shows a name beside its
// icon or without it, as CK3's GetNameWithIcon and Vic3's GetNameNoIcon.
const iconEnding = "Icon"

var nameEndings = []string{"WithIcon", "NoIcon"}

// describeCall returns a data call as a line of text says it: the name of the
// concept it links to, as CK3's [de_jure|E], or the words it shows for one;
// nothing for an icon; the title of the definition it names, as
// [GetTradeGoodType('trade_good_wood').GetName]; the texts of the
// customizable localization it names, joined by slashes, as daughter/son;
// and any other call in its words between brackets. A concept that is only
// an icon has no words.
func describeCall(ws *workspace.Session, mods *index.Index, segment loc.Segment) string {
	if segment.Call == nil {
		return "[" + segment.Text + "]"
	}

	first, last := segment.Call.Steps[0], finalStep(segment.Call)
	if pattern, hasConcepts := conceptTexts[ws.Game.ID]; hasConcepts {
		if concept, _, isConcept := ws.LookupText(mods, fmt.Sprintf(pattern, last.Text)); isConcept {
			return loc.ToPlain(ws.ExpandText(mods, concept))
		}
	}

	isName := slices.ContainsFunc(nameEndings, func(ending string) bool { return strings.HasSuffix(last.Text, ending) })
	switch {
	case last.Text == conceptCall && len(last.Arguments) == 2 && last.Arguments[1].Quoted:
		return loc.ToPlain(ws.ExpandText(mods, last.Arguments[1].Text))

	case strings.HasSuffix(last.Text, iconEnding) && !isName:
		return ""

	case last.Text == titleStep && len(first.Arguments) == 1 && first.Arguments[0].Quoted:
		if title, _, isNamed := ws.LookupText(mods, first.Arguments[0].Text); isNamed {
			return loc.ToPlain(ws.ExpandText(mods, title))
		}
	}

	if texts := describeCustom(ws, mods, last); texts != "" {
		return texts
	}

	return "[" + lang.DescribeStep(last, true) + "]"
}

// keepOwnTexts leaves a good, building or technology only its title and
// description; any other value that names a localization key is a link. A
// description the definition does not write is the one the game builds from
// the first of the given ids that has one.
func keepOwnTexts(ws *workspace.Session, mods *index.Index, ids []string, sections []Section) []Section {
	sections = slices.DeleteFunc(sections, func(section Section) bool { return !slices.Contains(textKeys, section.Key) })
	if slices.ContainsFunc(sections, func(section Section) bool { return section.Key == lang.DescriptionKey }) {
		return sections
	}

	for _, id := range ids {
		if key, ok := ws.FindBuiltKey(mods, toReference(id), lang.DescriptionKey); ok {
			text, winner, _ := ws.LookupText(mods, key)
			version := Version{
				Key: key, Text: text, Segments: describeCalls(ws, mods, text), Message: lsp.MakeSite(ws, mods, winner.Site),
			}

			return append(sections, Section{Key: lang.DescriptionKey, Texts: []Version{version}})
		}
	}

	return sections
}

func resolveLanguage(ws *workspace.Session) string {
	return cmp.Or(ws.Language, loc.BaseLanguage)
}

// toSourceLine returns the line, counting from one, a span starts on.
func toSourceLine(tree *jomini.Tree, span jomini.Span) int {
	return tree.Lines.ToPosition(span.Start).Line + 1
}
