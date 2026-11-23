// chain.go answers for a good: the building lines that make it, what they
// take, and the lines that use it.

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
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// Chain is a good with the building lines that make it, what they take, and
// the lines that use it.
type Chain struct {
	Good Node `json:"good"`

	// Found is where the good is found, none for a good no place holds.
	Found []Source `json:"found"`

	// Makers are the lines that make the good, those that produce it first,
	// then those that boost it, then those that give it when a need is met.
	// A line that makes it in two ways is there twice, and Lines counts it
	// once.
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

	// Line is the line with the rows of what its levels make of the good that
	// way, and of what they take to do it. A level that does not write the
	// good has an empty cell.
	Line Line `json:"line"`
}

// Amount is a good with how much of it.
type Amount struct {
	// Good is the good, or the category a need asks for.
	Good Node `json:"good"`

	// Text is how much, as the page shows it: a number, a range or a share.
	// It is "" where the game writes no number.
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

	// AmongEither is one way of several: each production method takes
	// exactly one of the goods marked so, as an EU5 printer runs with lumber
	// or with leather.
	AmongEither Among = "either"
)

// Pile is the lines that take a good under one group.
type Pile struct {
	// Group is the widest group its lines are in, with how many they are as
	// its Count. It is the zero node for what is part of no building.
	Group Node `json:"group"`

	Users []User `json:"users"`
}

// User is a line that takes a good.
type User struct {
	Line Node `json:"line"`

	// Amount is how much it takes, from the first level that takes it to the
	// last, as the page shows it.
	Amount string `json:"amount"`

	// LevelCount is how many levels the line has. FirstLevel and LastLevel
	// are the first and the last that take the good, counting from 1.
	LevelCount int `json:"levelCount"`
	FirstLevel int `json:"firstLevel"`
	LastLevel  int `json:"lastLevel"`

	// Through is the category its need asks for, nil where it names the good.
	Through *Node `json:"through,omitempty"`
}

// makerRelations are the ways a line makes a good, in the order a chain
// lists its makers.
var makerRelations = []string{saysOutput, saysBoost, saysGives}

// BuildChain returns a good with the lines that make it and the lines that
// use it, under the filter's origins and era. It returns an error if no
// workspace is open, the game keeps no definition of the good, or the
// definition is no good and no category of goods.
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

// makeMaker returns a line that makes a good by a relation. Its rows are what
// each level makes of the good, or of a category it is in, and what the level
// takes. In a game of needKeys a level takes nothing to produce, so its needs
// are listed only where meeting one gives the good.
func (d *draft) makeMaker(line, good, says string) Maker {
	levels := d.model.listLine(line)
	rows := &table{positions: map[[6]string]int{}, width: len(levels)}
	_, hasNeeds := needKeys[d.ws.Game.ID]
	for position, level := range levels {
		d.setMade(rows, position, level, says, d.model.listTargets(good))
		if !hasNeeds || says == saysGives {
			d.setTaken(rows, position, level)
		}
	}

	return Maker{Says: says, Line: d.makeLine(levels, rows)}
}

// needKeys holds, for each game whose buildings ask for goods as needs, the
// key of the block that holds a need. A need is met for what it gives, and is
// not what a level uses up to produce. Line 73 of CK3's common/buildings/
// regular_district_buildings/00_guild_district_buildings.txt writes one.
var needKeys = map[string]string{game.CK3: "trade_good_need"}

// listTakes returns what a level takes, each good once, with its amount over
// the production methods that take it and which of them do.
func (d *draft) listTakes(level string) []Amount {
	var goods, methods []string

	links, takers := map[string][]Edge{}, map[string][]string{}
	for _, member := range d.model.listMembers([]string{level}) {
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

// findNeed returns the id of the good or category a level asks for in the
// need whose block writes the given link. It returns "" if the game writes
// no needs, the level cannot be read, or no need holds the link.
func (d *draft) findNeed(level string, link Edge) string {
	key, hasNeeds := needKeys[d.ws.Game.ID]
	tree, statement, isRead := d.findStatement(level)
	if !hasNeeds || !isRead {
		return ""
	}

	for child := range tree.Children(statement) {
		if tree.Text(tree.Statements[child].Key) == key && isInside(tree, child, link) {
			return d.findNeedGood(level, tree, child)
		}
	}

	return ""
}

// findNeedGood returns the id of the good or category that one need block of
// a level asks for, "" if the block names none the game keeps.
func (d *draft) findNeedGood(level string, tree *jomini.Tree, block int32) string {
	for _, need := range d.model.listFrom(level, saysInput) {
		if isInside(tree, block, need) {
			return need.To
		}
	}

	return ""
}

// isInside reports whether a link is written on a line of a block.
func isInside(tree *jomini.Tree, block int32, link Edge) bool {
	lines := jomini.ToRange(tree.Src, tree.Lines, tree.Statements[block].Span)

	return lines.StartLine <= link.Site.Line && link.Site.Line <= lines.EndLine
}

// makePiles returns the lines that take a good, by the widest group each is
// in, in the order the links are found, and how many lines they are. A pile
// lists the line that takes the most first.
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

	for _, pile := range piles {
		slices.SortStableFunc(pile.Users, func(first, second User) int {
			return cmp.Compare(most[second.Line.ID], most[first.Line.ID])
		})
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

// keeps reports whether the filter's conditions keep a line: a drawn origin
// writes it, the workspace's mods add or change it where only that is asked
// for, and it is available by the filter's era. A filter with no origins
// draws them all.
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

// describeMade returns how much of a good the given links of one level make.
// Links that different production methods write are a range, since one
// method runs at a time. Links that one definition writes all hold at once,
// as a level's two boosts under two conditions, so each is said.
func (d *draft) describeMade(links []Edge) string {
	isOneWriter := !slices.ContainsFunc(links, func(edge Edge) bool { return edge.From != links[0].From })
	if !isOneWriter {
		return d.describeAmounts(links)
	}

	var amounts []string
	for _, edge := range links {
		if amount := d.describeAmounts([]Edge{edge}); amount != "" {
			amounts = appendNew(amounts, amount)
		}
	}

	return lang.JoinList(amounts, "and")
}

// amountDecimals holds how many decimals the amounts of a game are shown
// with, where it writes more than a reader needs: EU5 writes lead = 0.2138.
// Any other game's are shown as written.
var amountDecimals = map[string]float64{game.EU5: 2}

// describeAmounts returns the amounts of the given links as one: the number,
// or the range from the least to the most. A share is a signed percentage.
// An amount that is no number, as a script value with conditions, is left
// out, and the result is "" if none is a number.
func (d *draft) describeAmounts(edges []Edge) string {
	low, high, isShare := math.Inf(1), math.Inf(-1), false
	for _, edge := range edges {
		if value, err := strconv.ParseFloat(edge.Amount, 64); err == nil {
			low, high, isShare = min(low, value), max(high, value), edge.Share
		}
	}

	switch {
	case math.IsInf(low, 1):
		return ""

	case low == high:
		return d.describeAmount(low, isShare)
	}

	return d.describeAmount(low, isShare) + "–" + d.describeAmount(high, isShare)
}

func (d *draft) describeAmount(amount float64, isShare bool) string {
	if isShare {
		return fmt.Sprintf("%+g%%", math.Round(amount*10000)/100)
	}

	if decimals, isRounded := amountDecimals[d.ws.Game.ID]; isRounded {
		scale := math.Pow(10, decimals)
		amount = math.Round(amount*scale) / scale
	}

	return groupThousands(formatNumber(amount))
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
