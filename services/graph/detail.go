// detail.go builds what the page shows about one node of the Event Graph.

package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Detail is what the page shows about one node: its texts, its keys, its firings and the game rules it reads.
type Detail struct {
	Node     Node            `json:"node"`     // its Fires are every firing it makes
	Language string          `json:"language"` // the workspace's language, or the base language if it has none
	Source   string          `json:"source"`   // the definition as written
	Sections []Section       `json:"sections"` // the keys at the top of the definition, except its options
	Options  []Section       `json:"options"`  // the definition's option blocks
	FiredBy  []Edge          `json:"firedBy"`
	Linked   map[string]Node `json:"linked"`         // the node at the other end of each of its firings, by its id
	Rules    []Rule          `json:"rules"`          // the game rules the definition reads
	Copy     *Node           `json:"copy,omitempty"` // the copy of its name that the definition hides; nil for none
}

// A Section is one key at the top of a definition, as the page shows it.
type Section struct {
	Key   string      `json:"key"`
	Value string      `json:"value"`          // a scalar as written; "" for a block
	Line  int         `json:"line"`           // counted from one
	When  []Condition `json:"when,omitempty"` // a block's own limit, else its trigger, in words
	Texts []Version   `json:"texts,omitempty"`
}

// A Version is one text a key can show, with the trigger that decides it.
type Version struct {
	Key      string        `json:"key"` // its localization key
	Text     string        `json:"text"`
	Segments []loc.Segment `json:"segments"`          // the text's pieces, each data call in words
	Missing  bool          `json:"missing,omitempty"` // the detail's language has no message with its key
	When     []Condition   `json:"when,omitempty"`    // its trigger in words; nil if it always can
	Line     int           `json:"line"`              // counted from one
}

// A Rule is one game rule a definition reads, with its settings.
type Rule struct {
	ID      string       `json:"id"` // the rule's name
	Options []RuleOption `json:"options"`
}

// A RuleOption is one setting of a game rule.
type RuleOption struct {
	Key         string `json:"key"`
	Text        string `json:"text"`        // its localized name; "" if it has none
	Description string `json:"description"` // its localized description; "" if it has none
	Default     bool   `json:"default,omitempty"`
}

// callKind is the kind of a data call's segment once it holds the call's words.
const callKind = "text"

// textKeys are the keys at the top of a definition whose value is always a localization key.
var textKeys = []string{game.TitleKey, game.DescriptionKey}

// BuildDetail returns the detail of the node with the given id. It returns an error if no workspace is open, the game
// keeps no definition of the node or its file cannot be read.
func (gph *Service) BuildDetail(id string) (Detail, error) {
	ws, err := gph.Workspace.Session()
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
	sections := lo.Map(slices.Collect(tree.Children(statement)), func(child int32, _ int) Section {
		return makeSection(ws, mods, tree, child)
	})
	sections, options := lo.FilterReject(sections, func(section Section, _ int) bool { return section.Key != game.OptionKey })
	detail := Detail{
		Node:     node,
		Language: resolveLanguage(ws),
		Source:   tree.Text(tree.Statements[statement].Span),
		Sections: sections,
		Options:  options,
		FiredBy:  makeEdges(ws, mods, firedBy),
		Linked:   linked,
		Rules:    listRules(ws, mods, tree, statement),
	}
	if hidden, ok := findCopy(ws, mods, site); ok {
		other := makeNode(ws, mods, hidden, 0)
		detail.Copy = &other
	}
	return detail, nil
}

// makeSection returns the section of one statement at the top of a definition, with its texts.
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

// triggeredText is the description key of a text shown when its trigger
// holds, and textChoices the description keys that choose one of the texts
// inside them.
const triggeredText = "triggered_desc"

var textChoices = []string{"first_valid", "random_valid"}

// listVersions returns the texts a block can show, in the order written.
func listVersions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Version {
	var versions []Version
	for child := range tree.Children(statement) {
		key := strings.ToLower(tree.Text(tree.Statements[child].Key))
		switch {
		case key == triggeredText || key == game.DescriptionKey || key == game.OptionTitleKey:
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

// makeVersion returns the text a statement shows. It returns false if the statement writes no localization key.
func makeVersion(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (Version, bool) {
	label := statement
	if tree.Statements[statement].IsBlock() {
		// FindChild leaves 0 when there is no desc, and statement 0 is the file, which has no value.
		label, _ = tree.FindChild(statement, game.DescriptionKey)
		if label == 0 {
			label, _ = tree.FindChild(statement, game.LabelKey)
		}
	}
	value := tree.Statements[label].Value
	if value.IsEmpty() {
		return Version{}, false
	}
	key := tree.Unquoted(value)
	text, winner, ok := ws.LookupText(mods, key)
	// A key that is no identifier is shown as written, so it is never missing.
	missing := tree.Statements[label].ValueClass.IsIdentifier() && (!ok || mods.Language(winner.Site) != resolveLanguage(ws))
	return Version{
		Key: key, Text: text, Segments: describeCalls(ws, mods, text), Missing: missing,
		Line: toSourceLine(tree, tree.Statements[label].Key),
	}, true
}

// describeCalls returns a text's pieces with each data call replaced by its words, which leave out the function's
// ending that only turns off its tooltip. A call keeps its markup and has the kind callKind.
func describeCalls(ws *workspace.Session, mods *index.Index, text string) []loc.Segment {
	segments := loc.Segments(text, 0)
	for position, segment := range segments {
		call := segment.Call
		if call == nil {
			continue
		}
		step := call.Steps[len(call.Steps)-1]
		step.Text = strings.TrimSuffix(step.Text, game.GUI.NoTooltip)
		segments[position].Text = cmp.Or(describeCustom(ws, mods, step), schema.DescribeStep(step, true))
		segments[position].Kind = callKind
	}
	return segments
}

// describeCustom returns the texts a call's customizable localization can show, joined by slashes, such as
// "daughter/son". It returns "" for a call that names none, or one the workspace does not define.
func describeCustom(ws *workspace.Session, mods *index.Index, step jomini.Step) string {
	position := slices.Index(game.GUI.CallTargets[step.Text], game.CustomLocalization)
	if position < 0 || position >= len(step.Arguments) || !step.Arguments[position].Quoted {
		return ""
	}
	reference := schema.Reference{Database: game.CustomLocalization, Name: step.Arguments[position].Text}
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
		key, ok := tree.FindChildValue(child, game.CustomTexts.Key)
		if !ok || tree.Text(tree.Statements[child].Key) != game.CustomTexts.Block {
			continue
		}
		text, _, _ := ws.LookupText(mods, key)
		texts = append(texts, strings.TrimSpace(loc.ToPlain(loc.Segments(text, 0))))
	}
	return strings.Join(lo.Uniq(lo.Compact(texts)), "/")
}

// toTitle returns a text as the one line a player reads, each data call in its words between brackets.
func toTitle(ws *workspace.Session, mods *index.Index, text string) string {
	var title strings.Builder
	for _, segment := range describeCalls(ws, mods, text) {
		switch segment.Markup {
		case loc.MarkupText:
			title.WriteString(segment.Text)
		case loc.MarkupCall:
			title.WriteString("[" + segment.Text + "]")
		}
	}
	return strings.TrimSpace(title.String())
}

// resolveLanguage returns the workspace's language, or the game's base language if it has none.
func resolveLanguage(ws *workspace.Session) string {
	return cmp.Or(ws.Language, game.BaseLanguage)
}

// toSourceLine returns the line of a span in a tree, counted from one.
func toSourceLine(tree *jomini.Tree, span jomini.Span) int {
	return tree.Lines.ToPosition(span.Start).Line + 1
}

// listRules returns the game rules a definition reads, each once.
func listRules(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Rule {
	rules := []Rule{}
	for child := statement + 1; child < tree.Statements[statement].Last; child++ {
		if tree.Text(tree.Statements[child].Key) != game.GameRules.Trigger {
			continue
		}
		rule, ok := findRule(ws, mods, tree.Unquoted(tree.Statements[child].Value))
		if ok && !slices.ContainsFunc(rules, func(other Rule) bool { return other.ID == rule.ID }) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// findRule returns the game rule that holds the given setting: the rule the
// setting's definition is written in. It returns false if nothing defines
// the setting.
func findRule(ws *workspace.Session, mods *index.Index, setting string) (Rule, bool) {
	winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: game.GameRules.Settings, Name: setting})
	if !ok {
		return Rule{}, false
	}
	rule, ok := mods.FindDefinition(winner.Site)
	if !ok {
		return Rule{}, false
	}
	tree, statement, err := ws.FindStatement(mods, rule)
	if err != nil {
		return Rule{}, false
	}
	return makeRule(ws, mods, tree, statement), true
}

// makeRule returns the game rule a statement defines. Its default is the setting it names as one, else its first.
func makeRule(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Rule {
	preset, hasDefault := tree.FindChildValue(statement, game.GameRules.DefaultKey)
	options := []RuleOption{}
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		if !tree.Statements[child].IsBlock() || key == game.GameRules.CategoriesKey {
			continue
		}
		text, _, _ := ws.LookupText(mods, fmt.Sprintf(game.GameRules.Setting, key))
		description, _, _ := ws.LookupText(mods, fmt.Sprintf(game.GameRules.Description, key))
		options = append(options, RuleOption{Key: key, Text: text, Description: description, Default: key == preset})
	}
	if !hasDefault && len(options) > 0 {
		options[0].Default = true
	}
	return Rule{ID: tree.Text(tree.Statements[statement].Key), Options: options}
}
