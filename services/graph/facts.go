// facts.go reads what a definition gives, in the game's words.

package graph

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// Fact is one thing a definition gives: a modifier, or a line in the game's
// own words.
type Fact struct {
	// Label is the modifier's name, or the line, in the game's words. Text is
	// the modifier's value as the game shows it, "" for one that is simply on
	// and for a line.
	Label string `json:"label"`
	Text  string `json:"text"`

	// Tone says whether the value is a gain or a loss, "" for neither.
	Tone Tone `json:"tone,omitempty"`

	// Where is what the modifier's block applies to, as "In the county", ""
	// where the block says nothing of it.
	Where string `json:"where,omitempty"`

	// When is the condition the modifier's block writes, "" for none.
	When string `json:"when,omitempty"`

	// Need is the id of the good or category whose need gives it, "" for a
	// modifier no need gives.
	Need string `json:"need,omitempty"`
}

// Tone says whether a value is good or bad for whoever has it.
type Tone string

// The tones. The zero value is neither.
const (
	ToneGain Tone = "gain"
	ToneLoss Tone = "loss"
)

// modifierBlock is a block of a definition that holds modifiers.
type modifierBlock struct {
	Key string

	// Where is what the block's modifiers apply to, "" where nothing needs
	// saying.
	Where string
}

// modifierText is one localization key that may hold a modifier's name.
type modifierText struct {
	// Key is the localization key, with %s for the modifier's key.
	Key string

	// Upper is set where it writes the modifier's key in capitals.
	Upper bool
}

// modifierTexts holds the localization keys each game may name a modifier
// by, tried in order. CK3's modifiers_l_english.yml has archers_damage_mult
// (272), MOD_MONTHLY_WOODLAND_TAX (717),
// MOD_BUILDING_SLOTS_FOR_HOST_DISTRICT_ADD (140) and
// MOD_MONTHLY_PROSPERITY_GROWTH_FACTOR (1123). Vic3's
// modifiers_l_english.yml:500 has state_pollution_generation_add, and EU5's
// modifier_types_l_english.yml:418 MODIFIER_TYPE_NAME_local_monthly_food.
var modifierTexts = map[string][]modifierText{
	game.CK3: {
		{Key: "%s"}, {Key: "MOD_%s", Upper: true}, {Key: "MOD_%s_ADD", Upper: true}, {Key: "MOD_MONTHLY_%s", Upper: true},
	},
	game.Vic3: {{Key: "%s"}},
	game.EU5:  {{Key: "MODIFIER_TYPE_NAME_%s"}},
}

// parameterHolder is where a game's definitions grant the parameters a
// condition may name: the database, and the block that lists them.
type parameterHolder struct{ Database, Block string }

// parameterHolders holds where each game whose conditions name a parameter
// grants them. CK3's Industrious tradition writes parameters = {
// industrious_productivity_bonus = yes }
// (common/culture/traditions/00_societal_traditions.txt:259), and a building
// asks for it with parameter = industrious_productivity_bonus. A doctrine
// lists its parameters as bare names, as parameters = { meat_forbidden }
// (common/religion/doctrine_types/40_doctrines_special.txt:628-629).
var parameterHolders = map[string][]parameterHolder{
	game.CK3: {
		{Database: "culture/traditions", Block: "parameters"},
		{Database: "religion/doctrine_types", Block: "parameters"},
	},
}

// negatingPrefix starts the key of a condition that holds when its value does
// not, as CK3's not_parameter.
const negatingPrefix = "not_"

// listGives returns what a definition gives: what its modifiers give, in its
// modifier blocks, inside its needs and among its other keys where its
// database writes them there, and each line it writes in the game's own
// words. A modifier takes the condition of its own block, else of the
// nearest block around it that writes one. A line whose key no message holds
// is its key.
func (d *draft) listGives(id string) []Fact {
	facts := []Fact{}
	row := findDetailRow(d.ws.Game.ID, toDatabase(id))
	for _, clause := range d.model.clauses[id] {
		isTop := len(clause.Blocks) == 0
		fact, isFact := d.findFact(clause)
		switch {
		case isTop && slices.Contains(row.Custom, clause.Key):
			label, _ := describeKey(d.ws, d.mods, clause.Amount)
			facts = append(facts, Fact{Label: cmp.Or(label, clause.Amount)})

		case isFact && (!isTop || row.Loose):
			for _, block := range clause.Blocks {
				if fact.When = d.describeWhen(id, block); fact.When != "" {
					break
				}
			}

			facts = append(facts, fact)
		}
	}

	return facts
}

// findFact returns what a clause gives as a modifier. It returns false for a
// key that is no modifier and for a value that is no number, yes or no: a
// plain word is a condition, as terrain = mountains.
func (d *draft) findFact(clause clause) (Fact, bool) {
	label, isModifier := d.findModifierText(clause.Key)
	amount, err := strconv.ParseFloat(clause.Amount, 64)
	fact := Fact{Label: label, Where: clause.Where, Need: clause.Need}
	switch {
	case !isModifier:
		return Fact{}, false

	case err == nil:
		format, color := d.findFormat(clause.Key)
		fact.Text, fact.Tone = describeAmount(amount, format), describeTone(amount, color)

	case clause.Amount == lang.No:
		fact.Text = lang.No

	case clause.Amount != lang.Yes:
		return Fact{}, false
	}

	return fact, true
}

// describeWhen returns the condition one block of a definition writes, in
// words: its clauses that are no modifier and no number, joined by and. It
// is "" for a block with none.
func (d *draft) describeWhen(id string, block int32) string {
	var conditions []string
	for _, clause := range d.model.clauses[id] {
		_, err := strconv.ParseFloat(clause.Amount, 64)
		if len(clause.Blocks) == 0 || clause.Blocks[0] != block || err == nil {
			continue
		}

		if _, isFact := d.findFact(clause); !isFact {
			conditions = append(conditions, d.describeCondition(clause.Key, clause.Amount))
		}
	}

	return lang.JoinList(conditions, "and")
}

// formatDatabases holds the database each game declares a modifier's format
// in. CK3's common/modifier_definition_formats/00_definitions.txt:881 writes
// monthly_dynasty_prestige_mult = { decimals = 0 percent = yes }. Vic3 and
// EU5 write the same keys.
var formatDatabases = map[string]string{
	game.CK3:  "modifier_definition_formats",
	game.Vic3: "modifier_type_definitions",
	game.EU5:  "modifier_type_definitions",
}

// The keys of a modifier's format and the values they take.
const (
	// formatPercent marks a share written as a fraction, 0.25 for 25%, and
	// formatScaled one written as the percentage.
	formatPercent = "percent"
	formatScaled  = "already_percent"

	// formatDecimals is how many decimals show, defaultDecimals if left out.
	formatDecimals  = "decimals"
	defaultDecimals = 2

	// formatColor says whether more is good, bad or neutral. Left out, more
	// is good.
	formatColor  = "color"
	colorBad     = "bad"
	colorNeutral = "neutral"

	// formatSuffix is the localization key of what follows the number, as
	// CK3's "/month". formatUnsigned drops the plus sign.
	formatSuffix   = "suffix"
	formatUnsigned = "no_difference_sign"
)

// suffixMarkup is the characters of a data call, a formatting code or an
// icon. A suffix that holds one is left out.
const suffixMarkup = "[#$@"

// findFormat returns how the game writes a modifier's value, and its color:
// whether more of it is good, bad or neutral. A modifier with no declared
// format is written as the number, and is neutral.
func (d *draft) findFormat(key string) (numberFormat, string) {
	tree, statement, isDeclared := d.findStatement(lang.Reference{Database: formatDatabases[d.ws.Game.ID], Name: key}.ToID())
	if !isDeclared {
		return numberFormat{}, colorNeutral
	}

	say := func(name string) string {
		value, _ := tree.FindChildValue(statement, name)

		return value
	}

	decimals, err := strconv.Atoi(say(formatDecimals))
	if err != nil {
		decimals = defaultDecimals
	}

	format := numberFormat{Precision: math.Pow10(decimals), Signed: say(formatUnsigned) != lang.Yes}

	switch {
	case say(formatPercent) == lang.Yes:
		format.Scale, format.Unit = 100, "%"

	case say(formatScaled) == lang.Yes:
		format.Unit = "%"
	}

	if suffix, _, isNamed := d.ws.LookupText(d.mods, say(formatSuffix)); isNamed && !strings.ContainsAny(suffix, suffixMarkup) {
		format.Unit += suffix
	}

	return format, say(formatColor)
}

// describeTone returns whether a modifier's amount is a gain or a loss for
// whoever has it: more is good unless its color says bad, and 0 or a
// neutral modifier is neither.
func describeTone(amount float64, color string) Tone {
	switch {
	case amount == 0 || color == colorNeutral:
		return ""

	case (amount > 0) == (color != colorBad):
		return ToneGain
	}

	return ToneLoss
}

// findModifierText returns the game's name for a modifier, or its key where
// the dumps list it and no text names it. It returns false for a key that is
// no modifier.
func (d *draft) findModifierText(key string) (string, bool) {
	for _, row := range modifierTexts[d.ws.Game.ID] {
		modifier := key
		if row.Upper {
			modifier = strings.ToUpper(key)
		}

		if label, isNamed := describeKey(d.ws, d.mods, fmt.Sprintf(row.Key, modifier)); isNamed {
			return label, true
		}
	}

	return key, d.ws.Schema.DeclaresModifier(key)
}

// negatedText starts the words of a condition that holds when its value does
// not.
const negatedText = "Not: "

// describeCondition returns a condition of a modifier's block in words: the
// game's text for its value, else the definitions that grant the parameter
// it names, joined by or, else the statement as written. A negating key says
// so once. Where the words are the statement, its key loses that prefix and
// its underscores: not_parameter = meat_forbidden is "Not: parameter
// meat_forbidden".
func (d *draft) describeCondition(key, value string) string {
	rest, isNegated := strings.CutPrefix(key, negatingPrefix)
	label, isNamed := describeKey(d.ws, d.mods, value)
	granters := d.listParameters()[value]
	words := key + " " + value
	switch {
	case isNamed:
		words = label

	case len(granters) > 0:
		words = lang.JoinList(granters, "or")

	case isNegated:
		words = toWords(rest) + " " + value
	}

	if isNegated {
		return negatedText + words
	}

	return words
}

// listParameters returns the titles of the definitions that grant each
// parameter, by the parameter's name, read once. A block lists a parameter
// as its key, as industrious_productivity_bonus = yes, or as a bare name.
func (d *draft) listParameters() map[string][]string {
	if d.parameters != nil {
		return d.parameters
	}

	d.parameters = map[string][]string{}
	for _, row := range parameterHolders[d.ws.Game.ID] {
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
				parameter := cmp.Or(tree.Text(tree.Statements[child].Key), tree.Unquoted(tree.Statements[child].Value))
				d.parameters[parameter] = appendNew(d.parameters[parameter], title)
			}
		}
	}

	return d.parameters
}
