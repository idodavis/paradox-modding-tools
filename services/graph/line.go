// line.go answers for a building: its line as a table, a column for each
// level and a row for each fact, and its production method groups where the
// game has them, with what the picked methods come to together.

package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/lang"
)

// Line is a building's line as a table.
type Line struct {
	// Head is the first level, with the line's levels. Group is the group the
	// line is listed under, the zero node where it is in none.
	Head  Node `json:"head"`
	Group Node `json:"group"`

	// Levels are the columns, first to last.
	Levels []LineLevel `json:"levels"`

	// Rows are the facts the levels say differently, or only some say. Same
	// are the facts every level says alike, each with one cell.
	Rows []LineRow `json:"rows"`
	Same []LineRow `json:"same"`

	// Groups are the production method groups, none where the game has none.
	// Totals is what the picked methods come to, nil with no groups.
	Groups []MethodGroup `json:"groups"`
	Totals *Totals       `json:"totals,omitempty"`
}

// LineLevel is one level of a line.
type LineLevel struct {
	Level  Node `json:"level"`
	Number int  `json:"number"`

	// Unlock is the first technology that unlocks the level, and Era that
	// technology's era or age. Each is nil where there is none.
	Unlock *Node `json:"unlock,omitempty"`
	Era    *Node `json:"era,omitempty"`
}

// LineRow is one fact of a line across its levels.
type LineRow struct {
	// Says is what the fact is: output, boost, gives, input, need, employment,
	// cost or bonus.
	Says string `json:"says"`

	// Subject is the good or pop type the fact is about, nil for a cost and
	// a bonus. Label is the fact in the game's words.
	Subject *Node  `json:"subject,omitempty"`
	Label   string `json:"label"`

	// When is the condition the fact's block writes, "" for none. Where is
	// what a bonus applies to and Tone whether it is a gain or a loss, as a
	// Fact has them.
	When  string `json:"when,omitempty"`
	Where string `json:"where,omitempty"`
	Tone  Tone   `json:"tone,omitempty"`

	// Need is the id of the good or category whose need gives the fact when
	// it is met: the subject of one of the line's need rows. It is "" for a
	// fact no need gives.
	Need string `json:"need,omitempty"`

	// Among says which of a level's production methods take an input.
	Among Among `json:"among,omitempty"`

	// Cells holds the fact for each level, in the order of the levels.
	Cells []Cell `json:"cells"`
}

// Cell is what one level says of a fact.
type Cell struct {
	// Text is the fact as the page shows it, "" where the level does not say
	// it.
	Text string `json:"text"`

	// Part is the cell's number as a share of the row's largest, for a bar.
	// It is 0 where the text is no single number.
	Part float64 `json:"part"`
}

// MethodGroup is a production method group of a building.
type MethodGroup struct {
	// Group is the group, the zero node for the methods a building names
	// itself in no group, as EU5's buildings do.
	Group   Node     `json:"group"`
	Methods []Method `json:"methods"`

	// Picked is the id of the method the group runs.
	Picked string `json:"picked"`
}

// Method is a production method of a group.
type Method struct {
	Method Node `json:"method"`

	// Unlock is the first technology that unlocks it, and Era that
	// technology's era. Each is nil where there is none.
	Unlock *Node `json:"unlock,omitempty"`
	Era    *Node `json:"era,omitempty"`

	// Takes, Makes and Employs are its goods and its pop types, with how
	// much. Gives is what else its modifiers write.
	Takes   []Amount `json:"takes"`
	Makes   []Amount `json:"makes"`
	Employs []Amount `json:"employs"`
	Gives   []Fact   `json:"gives"`
}

// Totals is what the picked production methods of a building come to.
type Totals struct {
	Takes   []Total `json:"takes"`
	Makes   []Total `json:"makes"`
	Employs []Total `json:"employs"`

	// Jobs is how many pops they employ in all, and JobsBefore how many the
	// first method of each group employs.
	Jobs       string `json:"jobs"`
	JobsBefore string `json:"jobsBefore"`
}

// Total is one good or pop type with how much the picked methods come to.
type Total struct {
	Good Node   `json:"good"`
	Text string `json:"text"`

	// Before is how much the first method of each group comes to, "" where
	// those have none of it.
	Before string `json:"before"`
}

// The facts of a line that no link says: what a level costs, what its
// modifiers give, and a good it asks for as a need in a game of needKeys.
const (
	saysCost  = "cost"
	saysBonus = "bonus"
	saysNeed  = "need"
)

// BuildLine returns the line of a building. A production method as the id is
// its building. Each of the given production methods is the one its group
// runs, and a group none of them names runs its first. It returns an error
// if no workspace is open, the game keeps no definition of the id, or it is
// no building and part of none.
func (s *Service) BuildLine(id string, picked []string) (Line, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Line{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return Line{}, err
	}

	draft := newDraft(ws, mods)
	buildings := draft.listBuildings(toID(mods, site))
	if len(buildings) == 0 {
		return Line{}, fmt.Errorf("%s is no building", id)
	}

	levels := draft.model.listLine(buildings[0])
	line := draft.makeLine(levels, draft.makeTable(levels))
	line.Groups = draft.listMethodGroups(buildings[0], picked)
	if len(line.Groups) > 0 && line.Groups[0].Group.ID != "" {
		line.Totals = draft.makeTotals(line.Groups)
	}

	return line, nil
}

// makeLine returns a line from its levels, first to last, with the rows of
// the given table and no production method groups.
func (d *draft) makeLine(levels []string, rows *table) Line {
	line := Line{Head: d.lookupNode(levels[0]), Levels: make([]LineLevel, len(levels))}
	line.Group, _ = d.makeGroup(line.Head.Group, 0)
	for position, level := range levels {
		line.Levels[position] = LineLevel{Level: d.lookupNode(level), Number: position + 1}
		line.Levels[position].Unlock, line.Levels[position].Era = d.findUnlock(level)
	}

	line.Rows, line.Same = rows.split()

	return line
}

// findUnlock returns the first technology that unlocks a definition and that
// technology's era. Each is nil where there is none.
func (d *draft) findUnlock(id string) (*Node, *Node) {
	unlockers := d.model.listTo(id, saysUnlock)
	if len(unlockers) == 0 {
		return nil, nil
	}

	unlock := d.lookupNode(unlockers[0].From)
	if unlock.Era = d.findEra(unlock.ID); unlock.Era == "" {
		return &unlock, nil
	}

	era := d.lookupNode(unlock.Era)

	return &unlock, &era
}

// table is the rows of a line while its levels are read, in the order their
// facts are first found.
type table struct {
	rows []LineRow

	// positions holds the position in rows of each fact, by what it says, its
	// subject, its label, its condition, what it applies to and its need.
	positions map[[6]string]int

	// width is how many levels the line has.
	width int
}

// set writes what a level says of a fact, adding the fact's row the first
// time a level says it.
func (t *table) set(level int, row LineRow, text string) {
	key := [6]string{row.Says, "", row.Label, row.When, row.Where, row.Need}
	if row.Subject != nil {
		key[1] = row.Subject.ID
	}

	position, isListed := t.positions[key]
	if !isListed {
		row.Cells = make([]Cell, t.width)
		position, t.positions[key] = len(t.rows), len(t.rows)
		t.rows = append(t.rows, row)
	}

	t.rows[position].Cells[level].Text = text
}

// split returns the rows the levels say differently and those every level
// says alike, each of the latter with one cell. A cell that is one number
// gets its part of its row's largest.
func (t *table) split() (rows, same []LineRow) {
	rows, same = []LineRow{}, []LineRow{}
	for _, row := range t.rows {
		isSame := t.width > 1 && !slices.ContainsFunc(row.Cells, func(cell Cell) bool {
			return cell.Text == "" || cell.Text != row.Cells[0].Text
		})
		if isSame {
			row.Cells = row.Cells[:1]
			same = append(same, row)

			continue
		}

		numbers := make([]float64, len(row.Cells))
		for position, cell := range row.Cells {
			number := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(cell.Text, "+"), "%"), ",", "")
			numbers[position], _ = strconv.ParseFloat(number, 64)
		}

		if largest := slices.Max(numbers); largest > 0 {
			for position, number := range numbers {
				row.Cells[position].Part = max(number, 0) / largest
			}
		}

		rows = append(rows, row)
	}

	return rows, same
}

// makeTable returns the table of a line's levels: what each makes, takes and
// employs by the links, what it costs, and what its modifiers give.
func (d *draft) makeTable(levels []string) *table {
	rows := &table{positions: map[[6]string]int{}, width: len(levels)}
	for position, level := range levels {
		for _, says := range makerRelations {
			d.setMade(rows, position, level, says, nil)
		}

		d.setTaken(rows, position, level)
		for _, employed := range d.listAmounts(d.listLevelEdges(level, saysEmployment)) {
			row := LineRow{Says: saysEmployment, Subject: &employed.Good, Label: labelNode(employed.Good)}
			rows.set(position, row, employed.Text)
		}

		for _, cost := range d.listCosts(level) {
			rows.set(position, LineRow{Says: saysCost, Label: cost.Label}, cost.Text)
		}

		for _, fact := range d.listGives(level) {
			row := LineRow{
				Says: saysBonus, Label: fact.Label, When: fact.When, Where: fact.Where, Tone: fact.Tone, Need: fact.Need,
			}
			rows.set(position, row, fact.Text)
		}
	}

	return rows
}

// setTaken writes what a level takes into a table: a good it uses up to
// produce, or in a game of needKeys a good it asks for as a need.
func (d *draft) setTaken(rows *table, position int, level string) {
	taking := saysInput
	if _, hasNeeds := needKeys[d.ws.Game.ID]; hasNeeds {
		taking = saysNeed
	}

	for _, amount := range d.listTakes(level) {
		row := LineRow{Says: taking, Subject: &amount.Good, Label: labelNode(amount.Good), Among: amount.Among}
		rows.set(position, row, amount.Text)
	}
}

// setMade writes what a level makes by a relation into a table, a row for
// each good under each condition its block writes. Where goods are given,
// only those are written. What meeting a need gives says which need.
func (d *draft) setMade(rows *table, position int, level, says string, goods []string) {
	type output struct{ good, when, need string }

	var order []output

	own, links := d.model.listFrom(level), map[output][]Edge{}
	for _, edge := range d.listLevelEdges(level, says) {
		if len(goods) > 0 && !slices.Contains(goods, edge.To) {
			continue
		}

		key := output{good: edge.To, when: d.findLinkCondition(level, edge, own)}
		if says == saysGives {
			key.need = d.findNeed(level, edge)
		}

		if _, isListed := links[key]; !isListed {
			order = append(order, key)
		}

		links[key] = append(links[key], edge)
	}

	for _, key := range order {
		good := d.lookupNode(key.good)
		row := LineRow{Says: says, Subject: &good, Label: labelNode(good), When: key.when, Need: key.need}
		rows.set(position, row, d.describeMade(links[key]))
	}
}

// listAmounts returns the far end of the given links, each once, with the
// amount of its links as one.
func (d *draft) listAmounts(edges []Edge) []Amount {
	var targets []string

	links := map[string][]Edge{}
	for _, edge := range edges {
		targets = appendNew(targets, edge.To)
		links[edge.To] = append(links[edge.To], edge)
	}

	amounts := make([]Amount, len(targets))
	for position, target := range targets {
		amounts[position] = Amount{Good: d.lookupNode(target), Text: d.describeAmounts(links[target])}
	}

	return amounts
}

// labelNode returns a node's title, or its name where it has none.
func labelNode(node Node) string { return cmp.Or(node.Title, node.Name) }

// listMethodGroups returns the production method groups of a building, each
// with its production methods and the one it runs: the one of the given
// methods that is in it, else its first. The methods a building names itself
// come first, as a group with no node.
func (d *draft) listMethodGroups(building string, picked []string) []MethodGroup {
	groups := []MethodGroup{}
	holders := []string{building}
	for _, part := range d.model.listTo(building, saysGroup) {
		if d.typeOf(part.From) == lang.TypeProductionMethodGroup {
			holders = append(holders, part.From)
		}
	}

	for _, holder := range holders {
		group := MethodGroup{}
		if holder != building {
			group.Group = d.lookupNode(holder)
		}

		for _, member := range d.model.listTo(holder, saysGroup) {
			if d.typeOf(member.From) != lang.TypeProductionMethod {
				continue
			}

			group.Methods = append(group.Methods, d.makeMethod(member.From))
			if group.Picked == "" || slices.Contains(picked, member.From) {
				group.Picked = member.From
			}
		}

		if len(group.Methods) > 0 {
			groups = append(groups, group)
		}
	}

	return groups
}

// makeMethod returns a production method with what it takes, makes and
// employs, what unlocks it, and what else its modifiers write.
func (d *draft) makeMethod(id string) Method {
	method := Method{
		Method:  d.lookupNode(id),
		Takes:   d.listAmounts(d.model.listFrom(id, saysInput)),
		Makes:   d.listAmounts(d.model.listFrom(id, saysOutput)),
		Employs: d.listAmounts(d.model.listFrom(id, saysEmployment)),
		Gives:   d.listGives(id),
	}
	method.Unlock, method.Era = d.findUnlock(id)

	return method
}

// makeTotals returns what the picked production methods of the given groups
// come to, beside what the first method of each comes to.
func (d *draft) makeTotals(groups []MethodGroup) *Totals {
	picked, first := make([]string, len(groups)), make([]string, len(groups))
	for position, group := range groups {
		picked[position], first[position] = group.Picked, group.Methods[0].Method.ID
	}

	totals := &Totals{
		Takes:   d.sumLinks(picked, first, saysInput),
		Makes:   d.sumLinks(picked, first, saysOutput),
		Employs: d.sumLinks(picked, first, saysEmployment),
	}
	_, jobs := d.sumAmounts(picked, saysEmployment)
	_, before := d.sumAmounts(first, saysEmployment)
	totals.Jobs, totals.JobsBefore = d.describeAmount(sumValues(jobs), false), d.describeAmount(sumValues(before), false)

	return totals
}

// sumLinks returns, for each good or pop type the picked methods link to by a
// relation, how much they come to together and how much the first methods do.
func (d *draft) sumLinks(picked, first []string, says string) []Total {
	targets, sums := d.sumAmounts(picked, says)
	_, before := d.sumAmounts(first, says)
	totals := make([]Total, len(targets))
	for position, target := range targets {
		totals[position] = Total{Good: d.lookupNode(target), Text: d.describeAmount(sums[target], false)}
		if amount, isLinked := before[target]; isLinked {
			totals[position].Before = d.describeAmount(amount, false)
		}
	}

	return totals
}

// sumAmounts returns the far ends of the links of a relation that the given
// production methods write, in the order found, and the sum of the amounts
// of each. An amount that is no number counts as none.
func (d *draft) sumAmounts(methods []string, says string) ([]string, map[string]float64) {
	var targets []string

	sums := map[string]float64{}
	for _, method := range methods {
		for _, edge := range d.model.listFrom(method, says) {
			amount, _ := strconv.ParseFloat(edge.Amount, 64)
			targets = appendNew(targets, edge.To)
			sums[edge.To] += amount
		}
	}

	return targets, sums
}

func sumValues(amounts map[string]float64) float64 {
	sum := 0.0
	for _, amount := range amounts {
		sum += amount
	}

	return sum
}
