// detail.go builds what the page shows about one node of the Event Graph.

package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// Detail is what the page shows about one node: its texts, its keys, its
// firings and the game rules it reads.
type Detail struct {
	// Node is the node, with every firing it makes as its Fires.
	Node Node `json:"node"`

	// Language is the workspace's language, else the base language.
	Language string `json:"language"`

	// Source is the definition as written.
	Source string `json:"source"`

	// Sections are the keys at the top of the definition but its option
	// blocks, which are Options.
	Sections []Section `json:"sections"`
	Options  []Section `json:"options"`

	FiredBy []Edge `json:"firedBy"`

	// Linked holds the node at the other end of each firing, by id.
	Linked map[string]Node `json:"linked"`

	// Rules are the game rules the definition reads.
	Rules []Rule `json:"rules"`

	// Copy is the copy of its name the definition hides, nil for none.
	Copy *Node `json:"copy,omitempty"`
}

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
}

// Rule is one game rule a definition reads, with its settings.
type Rule struct {
	// ID is the rule's name.
	ID      string       `json:"id"`
	Options []RuleOption `json:"options"`
}

// RuleOption is one setting of a game rule.
type RuleOption struct {
	Key string `json:"key"`

	// Text and Description are the setting's localized name and
	// description, "" where it has none.
	Text        string `json:"text"`
	Description string `json:"description"`

	Default bool `json:"default,omitempty"`
}

// callKind is the kind of a text's piece that holds a data call in words.
const callKind = "text"

// textKeys are the top keys whose value is always a localization key, so
// they show a text even where its message is missing.
var textKeys = []string{lang.TitleKey, lang.DescriptionKey}

// optionKey is an event's option, and optionTitleKey the key that names
// it: a localization key, or a block that chooses one.
const (
	optionKey      = "option"
	optionTitleKey = "name"
)

// BuildDetail returns the detail of a node. It returns an error if no
// workspace is open, the game keeps no definition of the node, or its file
// cannot be read.
func (s *Service) BuildDetail(id string) (Detail, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Detail{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return Detail{}, err
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Detail{}, err
	}

	node := drawNode(ws, mods, site, 0)
	fires, firedBy := listFires(ws, mods, site, true), mods.ListFiredBy(ws.Schema, site, true)
	node.Fires = makeEdges(ws, mods, fires)
	linked := map[string]Node{}
	for _, fire := range fires {
		target := makeNode(ws, mods, fire.Target, 0)
		linked[target.ID] = target
	}

	for _, fire := range firedBy {
		source := makeNode(ws, mods, fire.Source, 0)
		linked[source.ID] = source
	}

	detail := Detail{
		Node:     node,
		Language: resolveLanguage(ws),
		Source:   tree.Text(tree.Statements[statement].Span),
		Sections: []Section{},
		Options:  []Section{},
		FiredBy:  makeEdges(ws, mods, firedBy),
		Linked:   linked,
		Rules:    listRules(ws, mods, tree, statement),
	}
	for child := range tree.Children(statement) {
		if section := makeSection(ws, mods, tree, child); section.Key == optionKey {
			detail.Options = append(detail.Options, section)
		} else {
			detail.Sections = append(detail.Sections, section)
		}
	}

	if hidden, ok := findCopy(ws, mods, site); ok {
		other := makeNode(ws, mods, hidden, 0)
		detail.Copy = &other
	}

	return detail, nil
}

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
		// A block with neither key leaves label at 0, the file, which has
		// no value.
		label, _ = tree.FindChild(statement, lang.DescriptionKey)
		if label == 0 {
			label, _ = tree.FindChild(statement, lang.LabelKey)
		}
	}

	value := tree.Statements[label].Value
	if value.IsEmpty() {
		return Version{}, false
	}

	// A value that is no identifier is shown as written, so it is never
	// missing.
	key := tree.Unquoted(value)
	text, winner, ok := ws.LookupText(mods, key)
	isMissing := tree.Statements[label].ValueClass.IsIdentifier() && (!ok || mods.Language(winner.Site) != resolveLanguage(ws))

	return Version{
		Key: key, Text: text, Segments: describeCalls(ws, mods, text), Missing: isMissing,
		Line: toSourceLine(tree, tree.Statements[label].Key),
	}, true
}

// describeCalls returns the pieces of a text with each data call in words,
// without the ending of a function's name that only turns off its tooltip.
func describeCalls(ws *workspace.Session, mods *index.Index, text string) []loc.Segment {
	segments := loc.Segments(text, 0)
	for position, segment := range segments {
		call := segment.Call
		if call == nil {
			continue
		}

		step := call.Steps[len(call.Steps)-1]
		step.Text = strings.TrimSuffix(step.Text, lang.GUINoTooltip)
		segments[position].Text = cmp.Or(describeCustom(ws, mods, step), lang.DescribeStep(step, true))
		segments[position].Kind = callKind
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
		if text := strings.TrimSpace(loc.ToPlain(loc.Segments(message, 0))); text != "" && !slices.Contains(texts, text) {
			texts = append(texts, text)
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

	return strings.TrimSpace(title.String())
}

func resolveLanguage(ws *workspace.Session) string {
	return cmp.Or(ws.Language, loc.BaseLanguage)
}

func toSourceLine(tree *jomini.Tree, span jomini.Span) int {
	return tree.Lines.ToPosition(span.Start).Line + 1
}

// listRules returns the game rules a definition reads, each once.
func listRules(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Rule {
	rules := []Rule{}
	for child := statement + 1; child < tree.Statements[statement].Last; child++ {
		if tree.Text(tree.Statements[child].Key) != lang.GameRuleTrigger {
			continue
		}

		if rule, ok := findRule(ws, mods, tree.Unquoted(tree.Statements[child].Value)); ok {
			rules = append(rules, rule)
		}
	}

	return dropRepeats(rules, func(rule Rule) string { return rule.ID })
}

// How a game rule is written.
const (
	// ruleDefaultKey names a rule's default setting. A rule without it
	// defaults to its first.
	ruleDefaultKey = "default"

	// ruleCategoriesKey is the one block in a rule that is no setting.
	ruleCategoriesKey = "categories"

	// ruleSettingText and ruleSettingDescription are the localization keys
	// of a setting's name and description.
	ruleSettingText        = "setting_%s"
	ruleSettingDescription = "setting_%s_desc"
)

// findRule returns the game rule a setting belongs to, with its settings.
// It returns false if nothing defines the setting.
func findRule(ws *workspace.Session, mods *index.Index, setting string) (Rule, bool) {
	winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: lang.GameRuleSettings, Name: setting})
	if !ok {
		return Rule{}, false
	}

	site, ok := mods.FindDefinition(winner.Site)
	if !ok {
		return Rule{}, false
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Rule{}, false
	}

	preset, hasDefault := tree.FindChildValue(statement, ruleDefaultKey)
	options := []RuleOption{}
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		if !tree.Statements[child].IsBlock() || key == ruleCategoriesKey {
			continue
		}

		text, _, _ := ws.LookupText(mods, fmt.Sprintf(ruleSettingText, key))
		description, _, _ := ws.LookupText(mods, fmt.Sprintf(ruleSettingDescription, key))
		options = append(options, RuleOption{Key: key, Text: text, Description: description, Default: key == preset})
	}

	if !hasDefault && len(options) > 0 {
		options[0].Default = true
	}

	return Rule{ID: tree.Text(tree.Statements[statement].Key), Options: options}, true
}
