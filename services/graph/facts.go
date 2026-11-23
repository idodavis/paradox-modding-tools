// facts.go reads what the modifiers of a definition give, in the game's
// words: the statements of its modifier blocks that the game names, each with
// the condition its block writes.

package graph

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// Fact is one thing a modifier of a definition gives.
type Fact struct {
	// Label is the modifier's name in the game's words. Text is its value as
	// the game shows it, "" for one that is simply on.
	Label string `json:"label"`
	Text  string `json:"text"`

	// Tone says whether the value is a gain or a loss, "" where the game
	// says neither.
	Tone Tone `json:"tone,omitempty"`

	// Where is what the modifier's block applies to, in words, as "In the
	// county". It is "" where the block says nothing of it.
	Where string `json:"where,omitempty"`

	// When is the condition the modifier's block writes, in the game's words
	// where it has any. It is "" where the block writes none.
	When string `json:"when,omitempty"`

	// Need is the id of the good or category whose need gives the modifier
	// when it is met, "" for a modifier no need gives.
	Need string `json:"need,omitempty"`
}

// Tone says whether a value is good or bad for whoever has it.
type Tone string

// The tones of a value. The zero value is neither.
const (
	ToneGain Tone = "gain"
	ToneLoss Tone = "loss"
)

// modifierBlock is a block of a definition that holds modifiers.
type modifierBlock struct {
	Key string

	// Where is what the block's modifiers apply to, in words, "" where the
	// definition has one such block and nothing needs saying.
	Where string
}

// modifierText is one localization key that may hold a modifier's name.
type modifierText struct {
	// Key is the localization key, with %s for the modifier's key.
	Key string

	// Upper is set where the localization key writes the modifier's key in
	// capitals.
	Upper bool
}

// modifierTexts holds how each game names a modifier, the keys tried in
// order. CK3's localization/english/modifiers/modifiers_l_english.yml:272
// names archers_damage_mult by its own key, line 717 writes
// MOD_MONTHLY_WOODLAND_TAX, line 140 MOD_BUILDING_SLOTS_FOR_HOST_
// DISTRICT_ADD for the key building_slots_for_host_district, and line 1123
// MOD_MONTHLY_PROSPERITY_GROWTH_FACTOR for prosperity_growth_factor. Vic3's
// localization/english/modifiers_l_english.yml:500 names
// state_pollution_generation_add by its own key. EU5's main_menu/
// localization/english/modifier_types_l_english.yml:418 writes
// MODIFIER_TYPE_NAME_local_monthly_food.
var modifierTexts = map[string][]modifierText{
	game.CK3: {
		{Key: "%s"}, {Key: "MOD_%s", Upper: true}, {Key: "MOD_%s_ADD", Upper: true}, {Key: "MOD_MONTHLY_%s", Upper: true},
	},
	game.Vic3: {{Key: "%s"}},
	game.EU5:  {{Key: "MODIFIER_TYPE_NAME_%s"}},
}

// conceptTexts holds, for each game that names a concept by a localization
// key, that key with %s for the concept. CK3's game_concept_woodland_taxes is
// "Hinterland Taxes", which the modifier's name links to as [woodland_taxes|E].
// Vic3 links to a concept by its whole key, as [concept_bureaucracy].
var conceptTexts = map[string]string{game.CK3: "game_concept_%s", game.Vic3: "%s"}

// parameterHolder says where a game's definitions grant the parameters a
// condition may name: the database, and the block that lists them.
type parameterHolder struct{ Database, Block string }

// parameterHolders holds each game whose conditions name a parameter. CK3's
// common/culture/traditions/00_societal_traditions.txt:259-261 writes
// parameters = { industrious_productivity_bonus = yes } in the Industrious
// tradition, and a building's modifier block asks for it with parameter =
// industrious_productivity_bonus.
var parameterHolders = map[string]parameterHolder{
	game.CK3: {Database: "culture/traditions", Block: "parameters"},
}

// negatingPrefix starts the key of a condition that holds when its value does
// not, as CK3's not_parameter.
const negatingPrefix = "not_"

// listGives returns what the modifiers of a definition give: those in the
// blocks its database keeps them in, those a need gives when it is met, and
// those among its other keys where its database writes them there. A
// statement on the line of one of its links is left out, since the link says
// it. It returns none for a definition that cannot be read.
func (d *draft) listGives(id string) []Fact {
	tree, statement, isRead := d.findStatement(id)
	if !isRead {
		return []Fact{}
	}

	return d.listBlocks(id, tree, statement, "")
}

// listBlocks returns what the modifier blocks under one statement of a
// definition give. The blocks inside a need are read with that need as
// theirs.
func (d *draft) listBlocks(id string, tree *jomini.Tree, statement int32, need string) []Fact {
	facts := []Fact{}
	row, links := d.findDetailRow(id), d.model.listFrom(id)
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		position := slices.IndexFunc(row.Bonuses, func(block modifierBlock) bool { return block.Key == key })
		isBlock := tree.Statements[child].IsBlock()

		switch {
		case isBlock && position >= 0:
			for _, fact := range d.listBlockFacts(tree, child, links) {
				fact.Where, fact.Need = row.Bonuses[position].Where, need
				facts = append(facts, fact)
			}

		case isBlock && need == "" && key == needKeys[d.ws.Game.ID]:
			facts = append(facts, d.listBlocks(id, tree, child, d.findNeedGood(id, tree, child))...)

		case isBlock && need != "":
			facts = append(facts, d.listBlocks(id, tree, child, need)...)

		case row.Loose:
			if fact, isFact := d.findFact(tree, child, links); isFact {
				facts = append(facts, fact)
			}
		}
	}

	return facts
}

// listBlockFacts returns what the modifiers of one block give, those of the
// blocks inside it too. Every fact with no condition of its own takes the
// block's.
func (d *draft) listBlockFacts(tree *jomini.Tree, block int32, links []Edge) []Fact {
	var facts []Fact
	for child := range tree.Children(block) {
		if tree.Statements[child].IsBlock() {
			facts = append(facts, d.listBlockFacts(tree, child, links)...)
		} else if fact, isFact := d.findFact(tree, child, links); isFact {
			facts = append(facts, fact)
		}
	}

	when := d.describeBlock(tree, block, links)
	for position := range facts {
		if facts[position].When == "" {
			facts[position].When = when
		}
	}

	return facts
}

// findFact returns what one statement gives as a modifier. It returns false
// for a block, for a statement on the line of one of the given links, for a
// key that is no modifier of the game, and for a value that is a plain word,
// as CK3's terrain = mountains, which is a condition.
func (d *draft) findFact(tree *jomini.Tree, statement int32, links []Edge) (Fact, bool) {
	current := tree.Statements[statement]
	label, isModifier := d.findModifierText(tree.Text(current.Key))
	text := describeNumber(d.ws, d.mods, d.values, tree, current.Value)
	_, err := strconv.ParseFloat(text, 64)
	isAmount := err == nil || text == lang.Yes || text == lang.No
	if current.IsBlock() || !isModifier || !isAmount || isLinked(tree, statement, links) {
		return Fact{}, false
	}

	shown, tone := d.describeModifier(tree.Text(current.Key), text)

	return Fact{Label: label, Text: shown, Tone: tone}, true
}

// formatDatabases holds the database each game declares how a modifier's
// value is shown in. CK3's common/modifier_definition_formats/
// 00_definitions.txt:881 writes monthly_dynasty_prestige_mult = { decimals =
// 0 percent = yes }, and its _definitions.info lists the keys. Vic3's and
// EU5's common/modifier_type_definitions write the same keys.
var formatDatabases = map[string]string{
	game.CK3:  "modifier_definition_formats",
	game.Vic3: "modifier_type_definitions",
	game.EU5:  "modifier_type_definitions",
}

// The keys of a modifier's format and the values they take.
const (
	// formatPercent is set for a share written as a fraction, as 0.25 for
	// 25%, and formatScaled for one written as the percentage itself.
	formatPercent = "percent"
	formatScaled  = "already_percent"

	// formatDecimals is how many decimals are shown, defaultDecimals where
	// it is left out.
	formatDecimals  = "decimals"
	defaultDecimals = 2

	// formatColor says whether more is good, bad or neutral. Left out, more
	// is good.
	formatColor  = "color"
	colorBad     = "bad"
	colorNeutral = "neutral"

	// formatSuffix is the localization key of what follows the number, as
	// CK3's "/month", and formatUnsigned is set for a number shown without
	// its plus sign.
	formatSuffix   = "suffix"
	formatUnsigned = "no_difference_sign"
)

// suffixMarkup is the characters of a suffix that is more than plain words:
// a data call, a formatting code or an icon. Such a suffix is left out.
const suffixMarkup = "[#$@"

// describeModifier returns a modifier's value as the game shows it, and its
// tone. A share is a percentage, the number is rounded to the decimals the
// game shows and signed, and a value more of which is bad is a loss where it
// is above zero. A modifier that is simply on, written yes, has no text. A
// modifier whose format the game does not declare is the number as written,
// with no tone.
func (d *draft) describeModifier(key, text string) (string, Tone) {
	amount, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return strings.TrimPrefix(text, lang.Yes), ""
	}

	format := lang.Reference{Database: formatDatabases[d.ws.Game.ID], Name: key}.ToID()
	tree, statement, isDeclared := d.findStatement(format)
	if !isDeclared {
		return groupThousands(formatNumber(amount)), ""
	}

	say := func(name string) string {
		value, _ := tree.FindChildValue(statement, name)

		return value
	}

	isShare := say(formatPercent) == lang.Yes || say(formatScaled) == lang.Yes
	if say(formatPercent) == lang.Yes {
		amount *= 100
	}

	decimals, err := strconv.Atoi(say(formatDecimals))
	if err != nil {
		decimals = defaultDecimals
	}

	if rounded := math.Round(amount*math.Pow10(decimals)) / math.Pow10(decimals); rounded != 0 {
		amount = rounded
	}

	shown := groupThousands(formatNumber(amount))
	if amount > 0 && say(formatUnsigned) != lang.Yes {
		shown = "+" + shown
	}

	if isShare {
		shown += "%"
	}

	if suffix, _, isNamed := d.ws.LookupText(d.mods, say(formatSuffix)); isNamed && !strings.ContainsAny(suffix, suffixMarkup) {
		shown += suffix
	}

	switch color := say(formatColor); {
	case amount == 0 || color == colorNeutral:
		return shown, ""

	case (amount > 0) == (color != colorBad):
		return shown, ToneGain
	}

	return shown, ToneLoss
}

// isLinked reports whether a statement is on the line of one of the given
// links.
func isLinked(tree *jomini.Tree, statement int32, links []Edge) bool {
	line := toSourceLine(tree, tree.Statements[statement].Key)

	return slices.ContainsFunc(links, func(edge Edge) bool { return edge.Site.Line == line })
}

// describeBlock returns the condition a modifier block writes, in words: its
// statements that are no block, no modifier, give no number and write none
// of the given links. A statement that gives a number is a modifier, named
// by the game or not. It returns "" for a block with no condition.
func (d *draft) describeBlock(tree *jomini.Tree, block int32, links []Edge) string {
	var conditions []string
	for child := range tree.Children(block) {
		current := tree.Statements[child]
		key := tree.Text(current.Key)
		_, isFact := d.findFact(tree, child, links)
		_, err := strconv.ParseFloat(describeNumber(d.ws, d.mods, d.values, tree, current.Value), 64)
		if key == "" || current.IsBlock() || isFact || err == nil || isLinked(tree, child, links) {
			continue
		}

		conditions = append(conditions, d.describeCondition(key, tree.Unquoted(current.Value)))
	}

	return lang.JoinList(conditions, "and")
}

// findLinkCondition returns the condition of the block a definition writes
// one of its links in, "" where the link is at the top of the definition, in
// what is a part of it, or cannot be found.
func (d *draft) findLinkCondition(id string, link Edge, links []Edge) string {
	tree, statement, isRead := d.findStatement(id)
	if link.From != id || !isRead {
		return ""
	}

	for child := statement + 1; child < tree.Statements[statement].Last; child++ {
		if parent := tree.Statements[child].Parent; parent != statement && isLinked(tree, child, []Edge{link}) {
			return d.describeBlock(tree, parent, links)
		}
	}

	return ""
}

// findModifierText returns the game's name for a modifier. Where the game's
// files hold no name for one its dumps list, as CK3's archers_damage_mult, it
// returns the key as the script writes it. It returns false for a key that
// is no modifier of the game.
func (d *draft) findModifierText(key string) (string, bool) {
	for _, row := range modifierTexts[d.ws.Game.ID] {
		modifier := key
		if row.Upper {
			modifier = strings.ToUpper(key)
		}

		if text, _, isNamed := d.ws.LookupText(d.mods, fmt.Sprintf(row.Key, modifier)); isNamed {
			return d.describeLabel(text), true
		}
	}

	return key, d.ws.Schema.DeclaresModifier(key)
}

// describeCondition returns a condition of a modifier's block in words: the
// game's text for its value where it has one, else the definition that
// grants the parameter it names, else the statement as written. A key that
// negates says not.
func (d *draft) describeCondition(key, value string) string {
	words := key + " " + value
	if text, _, isNamed := d.ws.LookupText(d.mods, value); isNamed {
		words = d.describeLabel(text)
	} else if holder, isGranted := d.listParameters()[value]; isGranted {
		words = holder
	}

	if strings.HasPrefix(key, negatingPrefix) {
		return "not " + words
	}

	return words
}

// listParameters returns the title of the definition that grants each
// parameter of the game, by the parameter's name, read once. It is empty for
// a game with no row in parameterHolders.
func (d *draft) listParameters() map[string]string {
	row, hasParameters := parameterHolders[d.ws.Game.ID]
	if d.parameters != nil || !hasParameters {
		return d.parameters
	}

	d.parameters = map[string]string{}
	for _, winner := range d.mods.ListKept(d.ws.Schema, row.Database) {
		tree, statement, err := d.ws.FindStatement(d.mods, winner.Site)
		if err != nil {
			continue
		}

		block, hasBlock := tree.FindChild(statement, row.Block)
		if !hasBlock {
			continue
		}

		title := labelNode(d.lookupNode(toID(d.mods, winner.Site)))
		for child := range tree.Children(block) {
			d.parameters[tree.Text(tree.Statements[child].Key)] = title
		}
	}

	return d.parameters
}

// describeLabel returns a text of the game as one line: a link to a concept
// as the concept's name, and any other data call in its words between
// brackets.
func (d *draft) describeLabel(text string) string {
	var label strings.Builder
	for _, segment := range describeCalls(d.ws, d.mods, text) {
		switch {
		case segment.Markup == loc.MarkupText:
			label.WriteString(segment.Text)

		case segment.Markup != loc.MarkupCall || segment.Call == nil:
			continue

		default:
			words, isNamed := d.describeCall(segment.Call)
			if !isNamed {
				words = "[" + segment.Text + "]"
			}

			label.WriteString(words)
		}
	}

	return strings.Join(strings.Fields(label.String()), " ")
}

// titleStep is the step a data call ends in to show the name of what it
// reached.
const titleStep = "GetName"

// describeCall returns the game's words for a data call in a text: the name
// of the concept it links to, as CK3's [woodland_taxes|E], or the text of
// the definition it names in quotes and shows the name of, as CK3's
// [GetTradeGoodType('trade_good_wood').GetName]. A concept that is only an
// icon, as CK3's command_modifier_i, has no words. It returns false for any
// other call.
func (d *draft) describeCall(call *jomini.Call) (string, bool) {
	first, last := call.Steps[0], call.Steps[len(call.Steps)-1]
	if pattern, hasConcepts := conceptTexts[d.ws.Game.ID]; hasConcepts {
		if concept, _, isConcept := d.ws.LookupText(d.mods, fmt.Sprintf(pattern, last.Text)); isConcept {
			return loc.ToPlain(d.ws.ExpandText(d.mods, concept)), true
		}
	}

	if last.Text != titleStep || len(first.Arguments) != 1 || !first.Arguments[0].Quoted {
		return "", false
	}

	title, _, isNamed := d.ws.LookupText(d.mods, first.Arguments[0].Text)

	return title, isNamed
}
