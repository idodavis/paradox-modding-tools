// texts.go reads the texts a definition shows: the keys at the top of it, the
// localization each one names, the trigger that decides which shows, and the
// data calls in a text put into words.

package graph

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Section is one key at the top of a definition.
type Section struct {
	Key string `json:"key"`

	// Value is a plain value as written, "" for a block.
	Value string `json:"value"`

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

	// Message is where the key's text is written in the localization file:
	// the detail's language's line, else the base language's, which is the
	// text to translate from. It is the zero Site where neither has one.
	Message lsp.Site `json:"message"`
}

// callSample is the Sample of a text's piece that holds a data call in words.
const callSample = "text"

// textKeys are the top keys whose value is always a localization key, so
// they show a text even where its message is missing.
var textKeys = []string{lang.TitleKey, lang.DescriptionKey}

// makeSection returns the section of one statement at the top of a
// definition, with its texts.
func makeSection(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Section {
	current := tree.Statements[statement]
	section := Section{Key: tree.Text(current.Key), Value: tree.Unquoted(current.Value), Line: toSourceLine(tree, current.Key)}
	if !current.IsBlock() {
		version, ok := makeVersion(ws, mods, tree, statement)
		if ok && (version.Text != "" || slices.Contains(textKeys, section.Key)) {
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

// describeCalls returns the pieces of a text with each data call in words,
// without the ending of a function's name that only turns off its tooltip.
func describeCalls(ws *workspace.Session, mods *index.Index, text string) []loc.Segment {
	segments := ws.ExpandText(mods, text)
	for position, segment := range segments {
		call := segment.Call
		if call == nil {
			continue
		}

		step := call.Steps[len(call.Steps)-1]
		step.Text = strings.TrimSuffix(step.Text, lang.GUINoTooltip)
		segments[position].Text = cmp.Or(describeCustom(ws, mods, step), lang.DescribeStep(step, true))
		segments[position].Sample = callSample
	}

	return segments
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

// toTitle returns a text as the one line a player sees, each data call in
// its words between brackets.
func toTitle(ws *workspace.Session, mods *index.Index, text string) string {
	var title strings.Builder
	for _, segment := range describeCalls(ws, mods, text) {
		switch segment.Markup {
		case loc.MarkupText:
			title.WriteString(segment.Text)
		case loc.MarkupCall:
			title.WriteString("[")
			title.WriteString(segment.Text)
			title.WriteString("]")
		}
	}

	return strings.Join(strings.Fields(title.String()), " ")
}

// keepOwnTexts leaves a good, building or technology only its title and
// description. Any other value that names a localization key, as a building's
// building_group, is a link. A description the definition does not write is
// the one the game builds from the first of the given ids that has one: the
// definition's own, then its line's first level.
func keepOwnTexts(ws *workspace.Session, mods *index.Index, ids []string, sections []Section) []Section {
	for position, section := range sections {
		if !slices.Contains(textKeys, section.Key) {
			sections[position].Texts = nil
		}
	}

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
