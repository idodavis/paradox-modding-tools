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

// A Detail is what the page shows about one node: its texts, its keys, its firings and what it reads.
type Detail struct {
	Node       Node            `json:"node"`              // its Fires are every firing it makes
	Language   string          `json:"language"`          // the workspace's language, or the base language if it has none
	Source     string          `json:"source"`            // the definition as written
	Trigger    []Condition     `json:"trigger,omitempty"` // the definition's own trigger in words; nil for none
	Sections   []Section       `json:"sections"`          // the keys at the top of the definition, except its options
	Options    []Section       `json:"options"`           // the definition's option blocks
	FiredBy    []Edge          `json:"firedBy"`
	Linked     map[string]Node `json:"linked"` // the node at the other end of each of its firings, by its id
	References []Node          `json:"references"`
	Uses       map[string]int  `json:"uses"`           // how many times the definition reads each of its references, by the node's id
	Rules      []Rule          `json:"rules"`          // the game rules the definition reads
	Copy       *Node           `json:"copy,omitempty"` // the copy of its name that the definition hides; nil for none
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

// The keys of an event that its sections read for their texts, their trigger and their split into options.
const (
	nameKey    = "name"
	optionKey  = "option"
	triggerKey = "trigger"
)

// callKind is the kind of a data call's segment once it holds the call's words.
const callKind = "text"

// textKeys are the keys at the top of a definition whose value is always a localization key. Each is shown as a text
// even when no message has its key.
var textKeys = []string{game.TitleKey, game.DescriptionKey}

// BuildDetail returns the detail of the node with the given id, read from the definition the game keeps of it, with
// the copy that definition hides. It returns an error if no workspace is open, if the game keeps no definition of the
// node, or if its file cannot be read.
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
	detail, err := buildDetail(ws, mods, site)
	if err != nil {
		return Detail{}, err
	}
	if hidden, ok := findCopy(ws, mods, site); ok {
		other := makeNode(ws, mods, hidden, 0)
		detail.Copy = &other
	}
	return detail, nil
}

// buildDetail returns the detail of one definition, without the copy it hides. It returns an error if its file cannot
// be read.
func buildDetail(ws *workspace.Session, mods *index.Index, site index.Site) (Detail, error) {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Detail{}, err
	}
	node := drawNode(ws, mods, site, 0)
	fires, firedBy := listFires(ws, mods, site, true), mods.ListFiredBy(ws.Schema, site, true)
	node.Fires = makeEdges(ws, mods, fires)
	sections := lo.Map(slices.Collect(tree.Children(statement)), func(child int32, _ int) Section {
		return makeSection(ws, mods, tree, child)
	})
	sections, options := lo.FilterReject(sections, func(section Section, _ int) bool { return section.Key != optionKey })
	references, uses := listReferences(ws, mods, site)
	detail := Detail{
		Node:       node,
		Language:   resolveLanguage(ws),
		Source:     tree.Text(tree.Statements[statement].Span),
		Trigger:    findCondition(ws, tree, statement),
		Sections:   sections,
		Options:    options,
		FiredBy:    makeEdges(ws, mods, firedBy),
		Linked:     listLinked(ws, mods, fires, firedBy),
		References: references,
		Uses:       uses,
		Rules:      listRules(ws, mods, tree, statement),
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
	section.When = findCondition(ws, tree, statement)
	section.Texts = listVersions(ws, mods, tree, statement)
	return section
}

// listVersions returns the texts a block can show in the order it writes them. A text is a desc or a name, or a block
// with its own trigger that holds one. A first_valid or random_valid block holds several.
func listVersions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Version {
	var versions []Version
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		row, _ := game.LookupKeyword(game.ShapeDescription, "", key)
		switch {
		case row.Shows == "leaf" || key == game.DescriptionKey || key == nameKey:
			if version, ok := makeVersion(ws, mods, tree, child); ok {
				version.When = findCondition(ws, tree, child)
				versions = append(versions, version)
			}
		case row.Shows != "":
			versions = append(versions, listVersions(ws, mods, tree, child)...)
		}
	}
	return versions
}

// makeVersion returns the text a statement shows. It returns false if the statement writes no localization key.
func makeVersion(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (Version, bool) {
	label := statement
	if tree.Statements[statement].IsBlock() {
		// a desc or text that is not found leaves 0, the file, which has no value
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
	// a key that is no identifier is shown as written, so it misses nothing
	missing := jomini.IsIdentifier(key) && (!ok || mods.Language(winner.Site) != resolveLanguage(ws))
	return Version{
		Key: key, Text: text, Segments: describeCalls(ws, mods, text), Missing: missing,
		Line: toSourceLine(tree, tree.Statements[label].Key),
	}, true
}

// describeCalls returns a text's pieces with each data call made its words, as the call's last step reads: a call that
// names a customizable localization reads as the texts it can show, one that names any other definition as that
// name, and any other call as its function. A call keeps its markup and has the kind callKind.
func describeCalls(ws *workspace.Session, mods *index.Index, text string) []loc.Segment {
	segments := loc.Segments(text, 0)
	for position, segment := range segments {
		call, ok := jomini.ParseCall(segment.Text, 0)
		if segment.Markup != loc.MarkupCall || !ok || len(call.Steps) == 0 {
			continue
		}
		step := call.Steps[len(call.Steps)-1]
		segments[position].Text = cmp.Or(describeCustom(ws, mods, step), schema.DescribeStep(step, true))
		segments[position].Kind = callKind
	}
	return segments
}

// describeCustom returns the texts a call's customizable localization can show, each once in the order written, joined
// by slashes, such as "daughter/son". It returns "" for a call that names none, or one the workspace does not define.
func describeCustom(ws *workspace.Session, mods *index.Index, step jomini.Step) string {
	position := slices.Index(game.GUI.CallTargets[step.Text], game.CustomTexts.Database)
	if position < 0 || position >= len(step.Arguments) || !step.Arguments[position].Quoted {
		return ""
	}
	reference := schema.Reference{Database: game.CustomTexts.Database, Name: step.Arguments[position].Text}
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

// toTitle returns a text as the one line a player reads, each data call in its words. Its keys, formats, icons and
// breaks are left out.
func toTitle(ws *workspace.Session, mods *index.Index, text string) string {
	var title strings.Builder
	for _, segment := range describeCalls(ws, mods, text) {
		if segment.Markup == loc.MarkupText || segment.Markup == loc.MarkupCall {
			title.WriteString(segment.Text)
		}
	}
	return strings.TrimSpace(title.String())
}

// resolveLanguage returns the language the detail shows its texts in: the workspace's own, or the game's base language
// if the workspace has none.
func resolveLanguage(ws *workspace.Session) string {
	return cmp.Or(ws.Language, game.BaseLanguage)
}

// toSourceLine returns the line of a span in a tree, counted from one.
func toSourceLine(tree *jomini.Tree, span jomini.Span) int {
	return tree.Lines.ToPosition(span.Start).Line + 1
}

// listLinked returns the nodes at the other end of a node's firings, by id: the targets of what it fires and the
// sources of what fires it.
func listLinked(ws *workspace.Session, mods *index.Index, fires, firedBy []index.Fire) map[string]Node {
	linked := map[string]Node{}
	for _, fire := range fires {
		node := makeNode(ws, mods, fire.Target, 0)
		linked[node.ID] = node
	}
	for _, fire := range firedBy {
		node := makeNode(ws, mods, fire.Source, 0)
		linked[node.ID] = node
	}
	return linked
}

// listReferences returns the nodes a node's definitions refer to, each once, in the order first written, and how many
// times each is read, by the node's id.
func listReferences(ws *workspace.Session, mods *index.Index, site index.Site) ([]Node, map[string]int) {
	references, uses := []Node{}, map[string]int{}
	ids := map[schema.Reference]string{}
	for _, use := range listUses(ws, mods, site) {
		for _, reference := range index.ListCallees(mods.ToReference(use)) {
			if id, seen := ids[reference]; seen {
				uses[id]++
			} else if node, ok := findReference(ws, mods, reference, use); ok {
				ids[reference] = node.ID
				references = append(references, node)
				uses[node.ID]++
			}
		}
	}
	return references, uses
}

// listUses returns the references written inside a node's definitions, in the order written.
func listUses(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	var uses []index.Site
	for _, part := range listActing(ws, mods, site) {
		occurrences := mods.Occurrences[part.File]
		for position := part.Occurrence + 1; int(position) < len(occurrences); position++ {
			if !isInside(occurrences, position, part.Occurrence) {
				break
			}
			if occurrences[position].Role == index.RoleRefers {
				uses = append(uses, index.Site{File: part.File, Occurrence: position})
			}
		}
	}
	return uses
}

// isInside reports whether an occurrence is inside a definition.
func isInside(occurrences []index.Occurrence, position, definition int32) bool {
	for current := occurrences[position]; current.Nested; current = occurrences[current.Parent] {
		if current.Parent == definition {
			return true
		}
	}
	return false
}

// findReference returns the node of a name a definition reads. It returns false if the game keeps no definition of it.
func findReference(ws *workspace.Session, mods *index.Index, reference schema.Reference, use index.Site) (Node, bool) {
	if slices.Contains(game.Links, reference.Database) {
		return makeNode(ws, mods, use, 0), true
	}
	winner, ok := mods.FindKeptLocal(ws.Schema, reference, use.File)
	if !ok {
		return Node{}, false
	}
	return makeNode(ws, mods, winner.Site, 0), true
}

// listRules returns the game rules a definition reads, each once, in the order first read. A setting that no rule the
// game keeps holds is left out.
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

// findRule returns the game rule the game keeps that holds a setting, with its settings. It returns false if no such
// rule holds it.
func findRule(ws *workspace.Session, mods *index.Index, setting string) (Rule, bool) {
	for _, winner := range mods.ListKept(ws.Schema, game.GameRules.Database) {
		tree, statement, err := ws.FindStatement(mods, winner.Site)
		if err != nil {
			continue
		}
		if child, ok := tree.FindChild(statement, setting); ok && tree.Statements[child].IsBlock() {
			return makeRule(ws, mods, tree, statement), true
		}
	}
	return Rule{}, false
}

// makeRule returns the game rule a statement defines, with its settings in the order written and its default marked:
// the setting it names as its default, else its first.
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
