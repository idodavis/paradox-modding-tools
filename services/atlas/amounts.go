// amounts.go says numbers as the game shows them, and what a definition
// gives: each modifier with its name, its value, its tone, what it applies
// to and the condition it holds under.

package atlas

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Fact is one thing a definition says of itself: a modifier it gives, an
// amount on a card, or a line in the game's own words.
type Fact struct {
	// Label is the modifier's name, what an amount does, or the line, in the
	// game's words. Text is the value as the game shows it, "" for a
	// modifier that is simply on and for a line.
	Label string `json:"label"`
	Text  string `json:"text"`

	// Tone says whether the value is a gain or a loss, "" for neither.
	Tone Tone `json:"tone,omitempty"`

	// Where is what the modifier's block applies to, as "In the county", ""
	// where the block says nothing of it.
	Where string `json:"where,omitempty"`

	// When is the condition the fact holds under, "" for none.
	When string `json:"when,omitempty"`

	// Need is the id of the good or category whose need gives it, "" for a
	// fact no need gives.
	Need string `json:"need,omitempty"`
}

// Tone says whether a value is good or bad for whoever has it.
type Tone string

// The tones. The zero value is neither.
const (
	ToneGain Tone = "gain"
	ToneLoss Tone = "loss"
)

// modifierBlock is a block of a database's definitions that holds what a
// definition gives, with what it applies to.
type modifierBlock struct {
	Games    []string
	Database string

	// Key is the block's key, "" for modifiers written loose among the
	// definition's own keys.
	Key string

	// Where is what the block's modifiers apply to, "" where nothing needs
	// saying.
	Where string
}

// modifierBlocks are the blocks that hold modifiers, beside a CK3 building's,
// which findPlace reads by rule.
var modifierBlocks = []modifierBlock{
	// common/culture/innovations/00_tribal_innovations.txt writes a modifier
	// block for each of these scopes
	{Games: []string{game.CK3}, Database: ck3Innovations, Key: "character_modifier", Where: "For the culture's characters"},
	{Games: []string{game.CK3}, Database: ck3Innovations, Key: "county_modifier", Where: "In the culture's counties"},
	{Games: []string{game.CK3}, Database: ck3Innovations, Key: "province_modifier", Where: "In the culture's provinces"},
	{Games: []string{game.CK3}, Database: ck3Innovations, Key: "culture_modifier", Where: "For the culture"},
	// common/production_methods/01_industry.txt:1922
	{Games: []string{game.Vic3}, Database: "production_methods", Key: "country_modifiers", Where: "For the country"},
	{Games: []string{game.Vic3}, Database: "production_methods", Key: "state_modifiers", Where: "In the state"},
	// common/technology/technologies/10_production.txt:11
	{Games: []string{game.Vic3}, Database: "technology/technologies", Key: "modifier"},
	// in_game/common/building_types/production_books.txt:20;
	// building_types/readme.txt:30-36 says what each block applies to
	{Games: []string{game.EU5}, Database: "building_types", Key: "modifier", Where: "In the location"},
	{Games: []string{game.EU5}, Database: "building_types", Key: "raw_modifier", Where: "In the location"},
	{Games: []string{game.EU5}, Database: "building_types", Key: "capital_modifier", Where: "In the location, if it is the capital"},
	{Games: []string{game.EU5}, Database: "building_types", Key: "market_center_modifier", Where: "In the location, if it is a market center"},
	{Games: []string{game.EU5}, Database: "building_types", Key: "capital_country_modifier", Where: "For the country, if built in its capital"},
	// in_game/common/advances/0_age_of_discovery.txt:307 writes
	// global_institution_growth_modifier = 0.20 beside the advance's age
	{Games: []string{game.EU5}, Database: "advances"},
}

// ck3Places says what each place a CK3 building's modifier block is named
// for applies to. A block's key is a place, then a condition or nothing,
// then _modifier, as province_culture_modifier
// (regular_district_buildings/00_guild_district_buildings.txt:36 and 64;
// common/buildings/_buildings.info names the places and the conditions).
var ck3Places = []struct{ Key, Where string }{
	{"county_holder_character", "For the county's holder"},
	{"duchy_capital_county", "In the duchy's capital county"},
	{"province", "In the province"},
	{"county", "In the county"},
	{"market", "In the market"},
	{"character", "For the holder"},
}

// The parts of a CK3 building's modifier block: how its key ends, and how
// the key of a block of the AI's weights starts, which is no bonus.
const (
	modifierEnding = "_modifier"
	aiPrefix       = "ai_"
)

// ck3BuildingDatabases are the databases whose modifier blocks are read by
// the rule of ck3Places.
var ck3BuildingDatabases = []string{"buildings", "domiciles/buildings"}

// ck3Innovations is CK3's database of innovations. customKey is the key of a
// line an innovation gives in the game's own words, as
// custom = fabricate_claim_speed
// (common/culture/innovations/00_high_medieval_innovations.txt:366).
const (
	ck3Innovations = "culture/innovations"
	customKey      = "custom"
)

// negatingPrefix starts the key of a condition that must not hold.
const negatingPrefix = "not_"

// parameterEnding ends the database of the parameters a CK3 tradition or
// doctrine switches on.
const parameterEnding = ".parameter"

// The keys of a modifier's format (CK3 common/modifier_definition_formats,
// Vic3 and EU5 common/modifier_type_definitions).
const (
	formatPercent        = "percent"
	formatAlreadyPercent = "already_percent"
	formatDecimals       = "decimals"
	formatNoSign         = "no_difference_sign"
	formatSuffix         = "suffix"
	formatColor          = "color"
	colorBad             = "bad"
	colorNeutral         = "neutral"
	defaultDecimals      = 2
)

// listGives returns what a definition gives: the modifiers of its modifier
// blocks, of its needs and among its own keys, and an innovation's lines. A
// statement a link row reads is a link and no modifier. It returns nil for a
// definition whose file changed since the scan.
func listGives(ws *workspace.Session, mods *index.Index, definition index.Site) []Fact {
	tree, statement, err := ws.FindStatement(mods, definition)
	if err != nil {
		return nil
	}

	database := mods.ToReference(definition).Database
	linked, needs := map[int32]bool{}, map[int32]string{}
	for _, link := range mods.ListLinks(ws.Schema, definition) {
		use := mods.Occurrence(link.Use)
		if link.Use.File != definition.File || link.Source != definition {
			continue
		}

		linked[use.Statement] = true
		if link.Relation == index.RelationInput && link.AmountKey != "" {
			needs[tree.Statements[use.Statement].Parent] = mods.ToReference(link.Target).ToID()
		}
	}

	var facts []Fact
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		where, isBlock := findPlace(ws.Game.ID, database, key)
		need, isNeed := needs[child]
		switch {
		case isBlock:
			facts = append(facts, readModifiers(ws, mods, tree, child, linked, Fact{Where: where})...)
		case isNeed:
			facts = append(facts, readNeed(ws, mods, tree, child, linked, Fact{Need: need})...)
		case key == customKey && database == ck3Innovations:
			text, _, _ := ws.LookupText(mods, tree.Unquoted(tree.Statements[child].Value))
			facts = append(facts, Fact{Label: cmp.Or(ws.FlattenText(mods, text), tree.Unquoted(tree.Statements[child].Value))})
		}
	}

	if _, isLoose := findPlace(ws.Game.ID, database, ""); isLoose {
		facts = append(facts, readModifiers(ws, mods, tree, statement, linked, Fact{})...)
	}

	return facts
}

// readNeed returns what meeting a CK3 need gives: the modifiers of the blocks
// under the need's own blocks, each with the need's condition where its own
// block writes none.
func readNeed(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, need int32, linked map[int32]bool, fact Fact) []Fact {
	fact.When = describeBlockCondition(ws, mods, tree, need, linked)

	var facts []Fact
	for holder := range tree.Children(need) {
		for block := range tree.Children(holder) {
			where, ok := findPlace(ws.Game.ID, ck3BuildingDatabases[0], tree.Text(tree.Statements[block].Key))
			if ok {
				fact.Where = where
				facts = append(facts, readModifiers(ws, mods, tree, block, linked, fact)...)
			}
		}
	}

	return facts
}

// findPlace returns what a block of a database's definitions applies its
// modifiers to, and whether the block holds modifiers at all. A CK3
// building's block is read by its key: a place, a condition or nothing, then
// _modifier. A place the table lacks is the key in words.
func findPlace(gameID, database, key string) (string, bool) {
	for _, row := range modifierBlocks {
		if row.Database == database && row.Key == key && slices.Contains(row.Games, gameID) {
			return row.Where, true
		}
	}

	isBuilding := gameID == game.CK3 && slices.Contains(ck3BuildingDatabases, database)
	if !isBuilding || !strings.HasSuffix(key, modifierEnding) || strings.HasPrefix(key, aiPrefix) {
		return "", false
	}

	for _, place := range ck3Places {
		if strings.HasPrefix(key, place.Key) {
			return place.Where, true
		}
	}

	return toWords(key), true
}

// readModifiers returns the modifiers a block writes. A plain statement is a
// modifier where the game names its key or the dumps list it, and its value
// is a number, a yes or a no. The given fact carries what the block applies
// to, its need, and the condition of the block around it, which holds where
// the block writes none of its own.
func readModifiers(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, block int32, linked map[int32]bool, fact Fact) []Fact {
	fact.When = cmp.Or(describeBlockCondition(ws, mods, tree, block, linked), fact.When)

	var facts []Fact
	for child := range tree.Children(block) {
		current := tree.Statements[child]
		key, value := tree.Text(current.Key), tree.Unquoted(current.Value)
		number, isNumber := resolveNumber(ws, mods, tree, current.Value)
		title := ws.LookupModifierTitle(mods, key)
		isModifier := title != "" || ws.Schema.DeclaresModifier(key)
		if current.IsBlock() || linked[child] || !isModifier || !isNumber && !slices.Contains(lang.Bools, value) {
			continue
		}

		fact.Label, fact.Text, fact.Tone = cmp.Or(title, key), "", ""
		if isNumber {
			fact.Text, fact.Tone = formatModifier(ws, mods, key, number)
		}

		facts = append(facts, fact)
	}

	return facts
}

// describeBlockCondition returns the condition a block of modifiers or a
// need writes, in words: each plain word that is no modifier's value, as the
// game's text for it, else the titles of the definitions that grant the
// parameter it names, else the statement as written. Several are joined by
// "and", and a key that starts with not_ makes the whole "Not: …". It returns
// "" for a block with no condition.
func describeBlockCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, block int32, linked map[int32]bool) string {
	var parts []string

	isNegated := false
	for child := range tree.Children(block) {
		current := tree.Statements[child]
		key, value := tree.Text(current.Key), tree.Unquoted(current.Value)
		_, isNumber := resolveNumber(ws, mods, tree, current.Value)
		isWord := current.ValueClass.IsIdentifier() && !slices.Contains(lang.Bools, value)
		if current.IsBlock() || linked[child] || isNumber || !isWord {
			continue
		}

		isNegated = isNegated || strings.HasPrefix(key, negatingPrefix)
		text, _, _ := ws.LookupText(mods, value)
		granters := listGranters(ws, mods, value)
		switch {
		case text != "":
			parts = append(parts, ws.FlattenText(mods, text))
		case len(granters) > 0:
			parts = append(parts, lang.JoinList(granters, "or"))
		default:
			parts = append(parts, toWords(key)+" "+value)
		}
	}

	condition := lang.JoinList(parts, "and")
	if isNegated && condition != "" {
		return "Not: " + condition
	}

	return condition
}

// listGranters returns the titles of the definitions that switch a parameter
// on, as the traditions that grant a culture parameter, each once.
func listGranters(ws *workspace.Session, mods *index.Index, parameter string) []string {
	var titles []string
	for _, database := range mods.ListDatabases(parameter) {
		if !strings.HasSuffix(database, parameterEnding) {
			continue
		}

		for _, granted := range mods.Resolve(lang.Reference{Database: database, Name: parameter}) {
			holder, ok := mods.FindDefinition(granted)
			title := ""
			if ok {
				title = ws.LookupTitle(mods, holder)
			}

			if title != "" && !slices.Contains(titles, title) {
				titles = append(titles, title)
			}
		}
	}

	return titles
}

// resolveNumber returns the number a value stands for: the number written, a
// constant's, or the one number a script value is defined as. It returns
// false for any other value.
func resolveNumber(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, value jomini.Span) (float64, bool) {
	if number, ok := tree.ResolveNumber(value); ok {
		return number, true
	}

	if value.IsEmpty() {
		return 0, false
	}

	return ws.ResolveScriptValue(mods, tree.Unquoted(value))
}

// readAmount returns the amount of a link: the value of the key its row
// names beside it, else its own statement's value. It returns false where
// that is no number.
func readAmount(ws *workspace.Session, mods *index.Index, link index.Link) (float64, bool) {
	tree, statement, err := ws.FindStatement(mods, link.Use)
	if err != nil {
		return 0, false
	}

	if link.AmountKey != "" {
		beside, ok := tree.FindChild(tree.Statements[statement].Parent, link.AmountKey)
		if !ok {
			return 0, false
		}

		statement = beside
	}

	return resolveNumber(ws, mods, tree, tree.Statements[statement].Value)
}

// formatModifier returns a modifier's value as its format says the game
// shows it, and whether it is a gain or a loss. With no format it is the
// number, and neither.
func formatModifier(ws *workspace.Session, mods *index.Index, key string, number float64) (string, Tone) {
	formats := cmp.Or(ws.Schema.ModifierTypes, lang.ModifierFormats)
	winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: formats, Name: key})
	if !ok {
		return describeNumber(number), ""
	}

	tree, statement, err := ws.FindStatement(mods, winner.Site)
	if err != nil {
		return describeNumber(number), ""
	}

	read := func(field string) string {
		value, _ := tree.FindChildValue(statement, field)

		return value
	}

	decimals, unit, sign := defaultDecimals, "", ""
	if written, err := strconv.Atoi(read(formatDecimals)); err == nil {
		decimals = written
	}

	shown := number
	if read(formatPercent) == lang.Yes {
		shown, unit = number*100, "%"
	}

	if read(formatAlreadyPercent) == lang.Yes {
		unit = "%"
	}

	if number > 0 && read(formatNoSign) != lang.Yes {
		sign = "+"
	}

	suffix, _, _ := ws.LookupText(mods, read(formatSuffix))
	if strings.ContainsAny(suffix, "[#@$") {
		suffix = ""
	}

	text := sign + roundNumber(shown, decimals) + unit + suffix
	tone := ToneGain
	if (number < 0) != (read(formatColor) == colorBad) {
		tone = ToneLoss
	}

	if number == 0 || read(formatColor) == colorNeutral {
		tone = ""
	}

	return text, tone
}

// describeNumber returns a number as the game writes one: a comma between
// each three digits of its whole part, and two decimals at most. A number
// that two decimals would round to 0 keeps every decimal.
func describeNumber(number float64) string {
	return roundNumber(number, defaultDecimals)
}

// roundNumber returns a number with at most the given decimals and a comma
// between each three digits of its whole part. A number that those decimals
// would round to 0 keeps every decimal.
func roundNumber(number float64, decimals int) string {
	text := strconv.FormatFloat(number, 'f', decimals, 64)
	if rounded, _ := strconv.ParseFloat(text, 64); rounded == 0 && number != 0 {
		text = strconv.FormatFloat(number, 'f', -1, 64)
	}

	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}

	return groupDigits(text)
}

// countOf returns a count with its noun, as "9 building lines" or "1 group".
func countOf(count int, noun string) string {
	if count != 1 {
		noun += "s"
	}

	return describeNumber(float64(count)) + " " + noun
}

// groupDigits returns a written number with a comma between each three
// digits of its whole part, as 4,500.
func groupDigits(text string) string {
	whole, decimals, _ := strings.Cut(text, ".")
	sign := ""
	if strings.HasPrefix(whole, "-") || strings.HasPrefix(whole, "+") {
		sign, whole = whole[:1], whole[1:]
	}

	for position := len(whole) - 3; position > 0; position -= 3 {
		whole = whole[:position] + "," + whole[position:]
	}

	if decimals != "" {
		return sign + whole + "." + decimals
	}

	return sign + whole
}

// describeRange returns several amounts of one fact in words: the number
// where they agree, else the range from the least to the most, as "60–465".
// A share of what is produced is a signed percentage, as "+25%" for 0.25,
// "+10–30%" for a range above zero, "-25% to -5%" for one below and
// "-5–+10%" for one across zero. It returns "" for no amounts.
func describeRange(numbers []float64, isShare bool) string {
	if len(numbers) == 0 {
		return ""
	}

	scale, unit, sign := 1.0, "", ""
	if isShare {
		scale, unit, sign = 100, "%", "+"
	}

	low, high := slices.Min(numbers), slices.Max(numbers)
	lowText, highText := describeNumber(low*scale), describeNumber(high*scale)
	switch {
	case low == high && low <= 0:
		return lowText + unit
	case low == high:
		return sign + lowText + unit
	case high < 0:
		return lowText + unit + " to " + highText + unit
	case low < 0:
		return lowText + "–+" + highText + unit
	}

	return sign + lowText + "–" + highText + unit
}

// The days of a month and of a year, as the games count them.
const (
	monthDays = 30
	yearDays  = 365
)

// describeDays returns a time the games write in days as whole years where it
// makes them, else whole months, else days: "2 years", "1 month", "45 days".
func describeDays(days float64) string {
	count, unit := days, "day"
	switch whole := int(days); {
	case days == float64(whole) && whole > 0 && whole%yearDays == 0:
		count, unit = days/yearDays, "year"
	case days == float64(whole) && whole > 0 && whole%monthDays == 0:
		count, unit = days/monthDays, "month"
	}

	if count != 1 {
		unit += "s"
	}

	return describeNumber(count) + " " + unit
}
