// chain.go answers for a good: the building lines that make it and use it.

package graph

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
)

// Chain is a good with the building lines that make it and use it.
type Chain struct {
	Good Node `json:"good"`

	// Found is where the good is found, none for a good no place holds.
	Found []Source `json:"found"`

	// Makers are the lines that make the good: those that produce it, then
	// those that boost it, then those that give it when a need is met. A line
	// that makes it in two ways is there twice, and Lines counts it once.
	Makers []Maker `json:"makers"`
	Lines  int     `json:"lines"`

	// Piles are the lines that take the good, by the widest group each is in.
	// Users is how many lines and needs they are.
	Piles []Pile `json:"piles"`
	Users int    `json:"users"`
}

// Maker is a building line that makes a good.
type Maker struct {
	// Says is how it makes the good: output, boost or gives.
	Says string `json:"says"`

	// Line is the line, its rows what the levels make of the good that way
	// and what they take to do it.
	Line Line `json:"line"`
}

// Amount is a good with how much of it.
type Amount struct {
	// Good is the good, or the category a need asks for.
	Good Node `json:"good"`

	// Text is how much: a number, a range or a share, "" where the game
	// writes no number.
	Text string `json:"text"`

	// Among says which of a level's production methods take it.
	Among Among `json:"among,omitempty"`
}

// Among says which of a level's production methods take a good.
type Among string

// Which production methods take a good. The zero value is every one.
const (
	// AmongSome is only some of them.
	AmongSome Among = "some"

	// AmongEither is one of several: each production method takes exactly
	// one of the goods marked so, as an EU5 printer runs on lumber or leather.
	AmongEither Among = "either"
)

// Pile is the lines that take a good under one group.
type Pile struct {
	// Group is the widest group its lines are in, its Count how many they
	// are. It is the zero node for what is part of no building.
	Group Node `json:"group"`

	Users []User `json:"users"`
}

// User is a line that takes a good.
type User struct {
	Line Node `json:"line"`

	// Amount is how much it takes, over the levels that take it.
	Amount string `json:"amount"`

	// LevelCount is how many levels the line has. FirstLevel and LastLevel
	// are the first and the last that take the good, counting from 1.
	LevelCount int `json:"levelCount"`
	FirstLevel int `json:"firstLevel"`
	LastLevel  int `json:"lastLevel"`

	// Through is the category its need asks for, nil where it names the good.
	Through *Node `json:"through,omitempty"`
}

// makerRelations are the ways a line makes a good, in a chain's order.
var makerRelations = []string{saysOutput, saysBoost, saysGives}

// BuildChain returns a good with the lines that make it and use it, under
// the filter's origins and era. It returns an error if no workspace is open,
// the game keeps no definition of the good, or it is no good and no category.
func (s *Service) BuildChain(id string, filter Filter) (Chain, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Chain{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return Chain{}, err
	}

	draft := newDraft(ws, mods)
	good := toID(mods, site)
	if goodType := draft.typeOf(good); goodType != lang.TypeGood && goodType != lang.TypeGoodCategory {
		return Chain{}, fmt.Errorf("%s is no good", id)
	}

	chain := Chain{Good: draft.lookupNode(good), Found: draft.makeSources(good), Makers: []Maker{}}

	var lines []string
	for _, says := range makerRelations {
		for _, line := range draft.listLines(good, says) {
			if draft.keeps(line, filter) {
				lines = appendNew(lines, line)
				chain.Makers = append(chain.Makers, draft.makeMaker(line, good, says))
			}
		}
	}

	chain.Lines = len(lines)
	chain.Piles, chain.Users = draft.makePiles(good, filter)

	return chain, nil
}

// makeMaker returns a line that makes a good by a relation, its rows what
// each level makes of the good or of its category, and what the level takes.
// Where the game writes needs, a need is listed only if meeting it gives the
// good.
func (d *draft) makeMaker(line, good, says string) Maker {
	levels, targets := d.model.listLine(line), d.model.listTargets(good)
	rows := &table{positions: map[[6]string]int{}, width: len(levels)}
	_, hasNeeds := needKeys[d.ws.Game.ID]
	for position, level := range levels {
		d.setMade(rows, position, level, says, targets)
		if !hasNeeds || says == saysGives {
			makers, needs := d.listMakers(level, says, targets)
			d.setTaken(rows, position, makers, needs)
		}
	}

	return Maker{Says: says, Line: d.makeLine(levels, rows)}
}

// listMakers returns the parts of a level that make one of the given goods by
// a relation, and the needs whose meeting gives it. The parts are the
// production methods that write it, or the level with all its parts where
// the level writes it itself: an EU5 village takes iron only for the one
// method that makes tools, and a CK3 guild hall gives meat for one need of
// its six.
func (d *draft) listMakers(level, says string, goods []string) (makers, needs []string) {
	for _, edge := range d.listLevelEdges(level, says) {
		if !slices.Contains(goods, edge.To) {
			continue
		}

		makers = appendNew(makers, edge.From)
		if edge.need != "" {
			needs = appendNew(needs, edge.need)
		}
	}

	if slices.Contains(makers, level) {
		makers = d.model.listMembers([]string{level})
	}

	return makers, needs
}

// needKeys holds the key of a need's block, for each game whose buildings
// ask for goods as needs. A need is met for what it gives; nothing is used
// up to produce. CK3's 00_guild_district_buildings.txt:73 writes one.
var needKeys = map[string]string{game.CK3: "trade_good_need"}

// listTakes returns what the given parts of a level take, each good once,
// with its amount over the production methods that take it and which of them
// do.
func (d *draft) listTakes(members []string) []Amount {
	var goods, methods []string

	links, takers := map[string][]Edge{}, map[string][]string{}
	for _, member := range members {
		if d.typeOf(member) == lang.TypeProductionMethod {
			methods = append(methods, member)
		}

		for _, edge := range d.model.listFrom(member, saysInput) {
			goods = appendNew(goods, edge.To)
			links[edge.To] = append(links[edge.To], edge)
			takers[edge.To] = appendNew(takers[edge.To], member)
		}
	}

	among := AmongSome
	if isEither(methods, takers) {
		among = AmongEither
	}

	takes := make([]Amount, len(goods))
	for position, good := range goods {
		takes[position] = Amount{Good: d.lookupNode(good), Text: d.describeAmounts(links[good])}
		if len(takers[good]) < len(methods) {
			takes[position].Among = among
		}
	}

	return takes
}

// isEither reports whether each of the given production methods takes
// exactly one of the goods that not all of them take.
func isEither(methods []string, takers map[string][]string) bool {
	for _, method := range methods {
		own := 0
		for _, members := range takers {
			if len(members) < len(methods) && slices.Contains(members, method) {
				own++
			}
		}

		if own != 1 {
			return false
		}
	}

	return true
}

// makePiles returns the lines that take a good, by the widest group each is
// in, and how many they are. A pile lists the line that takes the most first,
// and a group with no picture of its own shows that line's.
func (d *draft) makePiles(good string, filter Filter) ([]Pile, int) {
	piles, users := []Pile{}, 0
	most := map[string]float64{}
	for _, line := range d.listLines(good, saysInput) {
		if !d.keeps(line, filter) {
			continue
		}

		widest := ""
		if groups := d.listGroupChain(line); len(groups) > 0 {
			widest = groups[len(groups)-1]
		}

		position := slices.IndexFunc(piles, func(pile Pile) bool { return pile.Group.ID == widest })
		if position < 0 {
			group, _ := d.makeGroup(widest, 0)
			position, piles = len(piles), append(piles, Pile{Group: group})
		}

		user, amount := d.makeUser(line, good)
		most[line] = amount
		users++
		piles[position].Group.Count++
		piles[position].Users = append(piles[position].Users, user)
	}

	for position, pile := range piles {
		slices.SortStableFunc(pile.Users, func(first, second User) int {
			return cmp.Compare(most[second.Line.ID], most[first.Line.ID])
		})

		if first := pile.Users[0].Line; pile.Group.Picture == "" {
			piles[position].Group.Picture, piles[position].Group.Silhouette = first.Picture, first.Silhouette
		}
	}

	return piles, users
}

// makeUser returns a line that takes a good, with how much over the levels
// that take it, and the most any of them takes.
func (d *draft) makeUser(line, good string) (User, float64) {
	levels, targets := d.model.listLine(line), d.model.listTargets(good)
	user := User{Line: d.lookupNode(line), LevelCount: len(levels)}

	var inputs []Edge
	for position, level := range levels {
		links := d.listLevelEdges(level, saysInput)
		links = slices.DeleteFunc(links, func(edge Edge) bool { return !slices.Contains(targets, edge.To) })
		if len(links) == 0 {
			continue
		}

		if user.FirstLevel == 0 {
			user.FirstLevel = position + 1
		}

		user.LastLevel = position + 1
		inputs = append(inputs, links...)
	}

	user.Amount = d.describeAmounts(inputs)
	if len(inputs) > 0 && inputs[0].To != good {
		category := d.lookupNode(inputs[0].To)
		user.Through = &category
	}

	most := 0.0
	for _, edge := range inputs {
		amount, _ := strconv.ParseFloat(edge.Amount, 64)
		most = max(most, amount)
	}

	return user, most
}

// keeps reports whether the filter keeps a line: a drawn origin writes it,
// a mod adds or changes it where only that is asked for, and it is available
// by the filter's era. A filter with no origins draws them all.
func (d *draft) keeps(line string, filter Filter) bool {
	origin := d.lookupNode(line).Site.Place.Origin
	if len(filter.Origins) > 0 && !slices.Contains(filter.Origins, origin) {
		return false
	}

	if filter.Changed && !d.isChanged(line) {
		return false
	}

	return filter.Era == "" || d.isAvailable(line, filter.Era)
}

// isChanged reports whether a mod writes a copy of a definition that the game
// reads: the copy it keeps, or one merged into it.
func (d *draft) isChanged(id string) bool {
	winner, isKept := d.mods.FindKept(d.ws.Schema, toReference(id))

	return isKept && slices.ContainsFunc(slices.Concat([]index.Site{winner.Site}, winner.Merged), func(site index.Site) bool {
		return d.mods.File(site).Origin != files.OriginVanilla
	})
}

// isAvailable reports whether a line is available by an era: no technology
// unlocks its first level, or one of that era or an earlier one does.
func (d *draft) isAvailable(line, era string) bool {
	eras := d.listEras()
	limit := slices.Index(eras, era)
	unlockers := d.model.listTo(line, saysUnlock)

	return len(unlockers) == 0 || slices.ContainsFunc(unlockers, func(edge Edge) bool {
		return slices.Index(eras, d.findEra(edge.From)) <= limit
	})
}

// numberFormat is how a number is written.
type numberFormat struct {
	// Scale multiplies the number first, as 100 for a share the game writes
	// as a fraction. It is 0 for none.
	Scale float64

	// Precision is 10 to the number of decimals shown, as 100 for two. It is
	// 0 where every decimal shows, and a number that would round to 0 shows
	// every decimal too.
	Precision float64

	// Signed puts a plus before a number above 0. Unit follows the number,
	// as % or /month.
	Signed bool
	Unit   string

	// Days marks a number of days, written in whole years where it makes
	// them, else in whole months.
	Days bool
}

// shareFormat writes a share of what is produced, 0.25 as +25%, and
// timeFormat a time the game writes in days.
var (
	shareFormat = numberFormat{Scale: 100, Precision: 100, Signed: true, Unit: "%"}
	timeFormat  = numberFormat{Unit: " days", Days: true}
)

// timeUnits are what a number of days is written in where it makes a whole
// number of them, largest first.
var timeUnits = []struct {
	Days       float64
	One, Other string
}{{365, "year", "years"}, {30, "month", "months"}}

// amountPrecision holds the precision of a game's amounts, where it writes
// more decimals than a reader needs: EU5 writes lead = 0.2138, shown as 0.21.
var amountPrecision = map[string]float64{game.EU5: 100}

// formatAmount returns how the game's amounts are written: a share as a
// signed percentage, any other as the number.
func (d *draft) formatAmount(share bool) numberFormat {
	if share {
		return shareFormat
	}

	return numberFormat{Precision: amountPrecision[d.ws.Game.ID]}
}

// describeAmount returns a number in words as its format says: scaled,
// rounded, its thousands grouped, signed and followed by its unit, as 4,500,
// +25%, +0.1/month or 2 years.
func describeAmount(amount float64, format numberFormat) string {
	for _, unit := range timeUnits {
		count := amount / unit.Days
		switch {
		case !format.Days || count != math.Trunc(count):
			continue

		case count == 1:
			return "1 " + unit.One
		}

		return formatNumber(count) + " " + unit.Other
	}

	amount *= cmp.Or(format.Scale, 1)
	if format.Precision != 0 {
		if rounded := math.Round(amount*format.Precision) / format.Precision; rounded != 0 {
			amount = rounded
		}
	}

	text := groupThousands(formatNumber(amount))
	if format.Signed && amount > 0 {
		text = "+" + text
	}

	return text + format.Unit
}

// describeAmounts returns the amounts of the given links as one: the number,
// or the range from the least to the most. A range of two ends below zero is
// written whole, -25% to -5%; any other says its unit once, and its sign once
// where both ends are above zero: +10–30%. A production method's amount
// below zero takes away from what the other groups give, as a Vic3 method
// that automates laborers away: it lowers the least by the most its group
// takes away, and is said as written where nothing else is given. An amount
// that is no number is left out, and the result is "" if none is.
func (d *draft) describeAmounts(edges []Edge) string {
	low, high, isShare := math.Inf(1), math.Inf(-1), false
	reductions := map[string]float64{}
	for _, edge := range edges {
		value, err := strconv.ParseFloat(edge.Amount, 64)
		switch {
		case err != nil:
			continue

		case value < 0 && !edge.Share && d.typeOf(edge.From) == lang.TypeProductionMethod:
			group := d.findGroupID(edge.From)
			reductions[group] = min(reductions[group], value)

		default:
			low, high, isShare = min(low, value), max(high, value), edge.Share
		}
	}

	isNetted := !math.IsInf(low, 1)
	for _, reduction := range reductions {
		if isNetted {
			low += reduction
		} else {
			low, high = min(low, reduction), max(high, reduction)
		}
	}

	format := d.formatAmount(isShare)
	switch {
	case math.IsInf(low, 1):
		return ""

	case low == high:
		return describeAmount(low, format)

	case high < 0:
		return describeAmount(low, format) + " to " + describeAmount(high, format)
	}

	unit := format.Unit
	format.Unit = ""
	first := describeAmount(low, format)
	format.Signed = format.Signed && low < 0

	return first + "–" + describeAmount(high, format) + unit
}

// groupThousands returns a number's text with a comma between each three
// digits of its whole part, as 4,500.
func groupThousands(number string) string {
	whole, rest, _ := strings.Cut(number, ".")
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign, whole = "-", whole[1:]
	}

	for position := len(whole) - 3; position > 0; position -= 3 {
		whole = whole[:position] + "," + whole[position:]
	}

	if rest != "" {
		rest = "." + rest
	}

	return sign + whole + rest
}
