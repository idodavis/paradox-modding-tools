// building.go draws a building: its line of levels as a table, its production
// methods with what the picked ones come to, and around it the goods it takes
// and makes and the technology that unlocks it.

package atlas

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Line is a building's line of levels as a table, with its production
// methods.
type Line struct {
	// Levels are the columns, first to last.
	Levels []LineLevel `json:"levels"`

	// Bands are the rows of what holds with no condition, under a title
	// each, as Costs, Makes or In the province.
	Bands []LineBand `json:"bands"`

	// Conditional is how many more facts the line's levels give only under
	// a condition. The detail of a level lists them.
	Conditional int `json:"conditional"`

	// Groups are the production method groups, none where the building has
	// no methods. Totals is what the picked methods come to, nil where the
	// game names no groups.
	Groups []MethodGroup `json:"groups"`
	Totals *Totals       `json:"totals,omitempty"`
}

// LineLevel is one level of a line.
type LineLevel struct {
	Level  Node `json:"level"`
	Number int  `json:"number"`

	// Unlock is the first technology that unlocks the level, nil for none.
	Unlock *Node `json:"unlock,omitempty"`
}

// LineBand is the rows of a line's table that stand under one title.
type LineBand struct {
	Title string    `json:"title"`
	Rows  []LineRow `json:"rows"`
}

// LineRow is one fact of a line across its levels.
type LineRow struct {
	// Subject is the good or pop type the fact is about, nil for a cost and
	// a modifier. Label is the fact in the game's words.
	Subject *Node  `json:"subject,omitempty"`
	Label   string `json:"label"`
	Tone    Tone   `json:"tone,omitempty"`

	// Cells holds one text where every level says the same, else one for
	// each level, "" where a level does not say the fact.
	Cells []string `json:"cells"`
}

// MethodGroup is a production method group of a building.
type MethodGroup struct {
	// Group is the group, the zero node for the methods a building names in
	// no group, as EU5's buildings do.
	Group   Node     `json:"group"`
	Methods []Method `json:"methods"`

	// Picked is the id of the method the group runs.
	Picked string `json:"picked"`
}

// Method is a production method of a group.
type Method struct {
	Method Node `json:"method"`

	// Unlocks is whatever unlocks the method: technologies, laws and the
	// like. Access is the goods its building must reach. Late is set where
	// the filter's era comes before that of the technology that unlocks it.
	Unlocks []Node `json:"unlocks"`
	Access  []Node `json:"access"`
	Late    bool   `json:"late,omitempty"`

	// Takes, Makes and Employs are its goods and its pop types, with how
	// much. Gives is what else its modifiers write.
	Takes   []Amount `json:"takes"`
	Makes   []Amount `json:"makes"`
	Employs []Amount `json:"employs"`
	Gives   []Fact   `json:"gives"`
}

// Amount is a good or a pop type with how much of it.
type Amount struct {
	Good Node `json:"good"`

	// Text is how much, "" where the game writes no number.
	Text string `json:"text"`

	// Unscaled is set for an amount Vic3 writes under unscaled, which is for
	// the building whatever its level.
	Unscaled bool `json:"unscaled,omitempty"`

	number float64
}

// Totals is what the picked production methods of a building come to.
type Totals struct {
	Takes   []Total `json:"takes"`
	Makes   []Total `json:"makes"`
	Employs []Total `json:"employs"`

	// Jobs is how many pops they employ in all, in words, with how many the
	// first method of each group employs where that differs.
	Jobs string `json:"jobs"`
}

// Total is one good or pop type with how much the picked methods come to.
type Total struct {
	Good Node   `json:"good"`
	Text string `json:"text"`

	// Before says how the first method of each group differs: "new" where
	// they have none of it, how much they come to where that is another
	// number, and "" where they agree.
	Before string `json:"before"`

	// picked and first are the sums over the picked methods and over the
	// first method of each group. isFirst is set where a first method has
	// the good at all.
	picked, first float64
	isFirst       bool
}

// unscaledKey is the block of a Vic3 production method whose amounts are for
// the building whatever its level
// (common/production_methods/production_methods.md:13-27).
const unscaledKey = "unscaled"

// The titles of a line's bands that no key of the game names.
const (
	bandCosts   = "Costs"
	bandMakes   = "Makes"
	bandTakes   = "Takes"
	bandNeeds   = "Needs"
	bandEmploys = "Employs"
	bandGives   = "Gives"
)

// romans holds the Roman numerals of a line's levels by tens and ones.
var romans = [2][]string{
	{"", "X", "XX", "XXX", "XL", "L"},
	{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX"},
}

// toRoman returns a level's number as a Roman numeral, as IV for 4. A number
// past 59 is written in digits.
func toRoman(number int) string {
	if number <= 0 || number >= 60 {
		return describeNumber(float64(number))
	}

	return romans[0][number/10] + romans[1][number%10]
}

// pickLinks returns a definition's links of one relation in which it is the
// source, or the target.
func pickLinks(ws *workspace.Session, mods *index.Index, definition index.Site, relation index.Relation, isSource bool) []index.Link {
	return slices.DeleteFunc(mods.ListLinks(ws.Schema, definition, relation), func(link index.Link) bool {
		return (link.Source == definition) != isSource
	})
}

// listLevels returns the line a building's level is in, first to last. A
// level that leads to several is followed to the first, and a level that
// leads back ends the line. Anything that is no level is a line of itself.
func listLevels(ws *workspace.Session, mods *index.Index, level index.Site) []index.Site {
	first := level
	for seen := map[index.Site]bool{}; !seen[first]; {
		seen[first] = true
		if before := pickLinks(ws, mods, first, index.RelationLevel, false); len(before) > 0 {
			first = before[0].Source
		}
	}

	levels := []index.Site{first}
	for {
		after := pickLinks(ws, mods, levels[len(levels)-1], index.RelationLevel, true)
		if len(after) == 0 || slices.Contains(levels, after[0].Target) {
			return levels
		}

		levels = append(levels, after[0].Target)
	}
}

// buildBuilding returns the canvas of a building: the first level of its
// line, opened, with the goods it takes or needs on its left, the
// technologies that unlock it in a column of their own one step further out,
// and the goods it makes on its right. A production
// method opens the building that runs it, with that method picked. A building
// of one level with production methods says its costs as its facts.
func buildBuilding(ws *workspace.Session, mods *index.Index, subject index.Site, filter Filter) Graph {
	if ws.Schema.LookupType(mods.ToReference(subject).Database) == lang.TypeProductionMethod {
		filter.Picked = append(slices.Clone(filter.Picked), mods.ToReference(subject).ToID())
		subject = listRunners(ws, mods, subject)[0]
	}

	levels := listLevels(ws, mods, subject)
	r := startReach(ws, mods, levels[0], filter)
	r.graph.Eras = listEras(ws, mods)
	line := buildLine(ws, mods, levels, filter, r.graph.Eras)
	root := &r.graph.Nodes[0]
	root.Line, root.Summary = line, describeLine(ws, mods, levels, line)
	if len(levels) == 1 && len(line.Groups) > 0 {
		root.Facts = listCosts(ws, mods, levels[0])
	}

	takes, makes := listFlows(ws, mods, levels, line)
	taken := Column{Title: bandTakes, Summary: countOf(len(takes), "good")}
	if slices.ContainsFunc(takes, func(taken Amount) bool { return taken.Good.Summary != "" }) {
		taken = Column{Title: bandNeeds, Summary: countOf(len(takes), "good") + ", by what meeting them gives"}
	}

	r.drawFlows(takes, -1, index.RelationInput, taken)
	r.drawFlows(makes, 1, index.RelationOutput, Column{Title: bandMakes, Summary: countOf(len(makes), "good")})
	unlocks := pickLinks(ws, mods, levels[0], index.RelationUnlock, false)
	for _, link := range unlocks {
		node := makeUnlock(ws, mods, link.Source, root.Title, r.graph.Eras)
		node.Parent, node.Distance = r.graph.Root, -2
		r.put(node, link.Source)
		r.join(r.graph.Root, node.ID, -1, Edge{Relation: index.RelationUnlock})
	}

	if len(unlocks) > 0 {
		words := lang.DescribeDefinition(mods.ToReference(unlocks[0].Source).Database, len(unlocks) > 1)
		r.graph.Columns = append(r.graph.Columns, Column{
			Distance: -2, Title: "Unlocked by", Summary: describeNumber(float64(len(unlocks))) + " " + words,
		})
	}

	r.graph.Total = len(r.graph.Nodes)

	return r.graph
}

// drawFlows draws the goods a building takes or makes as pills on one side,
// under the given column. A side with no goods has no column.
func (r *reach) drawFlows(flows []Amount, direction int, relation index.Relation, column Column) {
	for _, flow := range flows {
		good := flow.Good
		good.Parent, good.Distance = r.graph.Root, direction
		if _, drawn := r.positions[good.ID]; !drawn {
			r.put(good, noSite)
		}

		r.join(r.graph.Root, good.ID, direction, Edge{Relation: relation, Amount: flow.Text})
	}

	if column.Distance = direction; len(flows) > 0 {
		r.graph.Columns = append(r.graph.Columns, column)
	}
}

// flow is the numbers a building's line writes of one good on one side.
type flow struct {
	good    Node
	numbers []float64
	isShare bool
	isMade  bool
}

// listFlows returns the goods a building takes and makes with no condition,
// each once with how much: what its picked production methods come to where
// the game names its groups, else what the picked methods and the levels
// write, as a range over the line. A good a need asks for says what meeting
// the need gives.
func listFlows(ws *workspace.Session, mods *index.Index, levels []index.Site, line *Line) (takes, makes []Amount) {
	if line.Totals != nil {
		for _, total := range line.Totals.Takes {
			takes = append(takes, Amount{Good: total.Good, Text: total.Text})
		}

		for _, total := range line.Totals.Makes {
			makes = append(makes, Amount{Good: total.Good, Text: total.Text})
		}

		return takes, makes
	}

	var flows []flow
	add := func(good Node, number float64, isShare, isMade bool) {
		position := slices.IndexFunc(flows, func(other flow) bool { return other.good.ID == good.ID && other.isMade == isMade })
		if position < 0 {
			position, flows = len(flows), append(flows, flow{good: good, isShare: isShare, isMade: isMade})
		}

		flows[position].numbers = append(flows[position].numbers, number)
	}

	for _, group := range line.Groups {
		for _, method := range group.Methods {
			if method.Method.ID != group.Picked {
				continue
			}

			for _, taken := range method.Takes {
				add(taken.Good, taken.number, false, false)
			}

			for _, made := range method.Makes {
				add(made.Good, made.number, false, true)
			}
		}
	}

	gives := listGives(ws, mods, levels[0])
	for _, level := range levels {
		for _, link := range mods.ListLinks(ws.Schema, level) {
			isMade := link.Relation == index.RelationOutput || link.Relation == index.RelationBoost
			number, isNumber := readAmount(ws, mods, link)
			isTaken := link.Relation == index.RelationInput
			if link.Source != level || !isNumber || !isMade && !isTaken || isMade && describeLinkCondition(ws, mods, link) != "" {
				continue
			}

			good := makeNode(ws, mods, link.Target)
			good.Summary = describeNeed(gives, good.ID)
			add(good, number, link.Share, isMade)
		}
	}

	for _, written := range flows {
		side := &takes
		if written.isMade {
			side = &makes
		}

		*side = append(*side, Amount{Good: written.good, Text: describeRange(written.numbers, written.isShare)})
	}

	return takes, makes
}

// describeNeed returns what meeting the need for a good gives, as "gives
// Development Growth +5% and 2 more". It returns "" for a good no need asks
// for.
func describeNeed(gives []Fact, good string) string {
	given := slices.DeleteFunc(slices.Clone(gives), func(fact Fact) bool { return fact.Need != good })
	if len(given) == 0 {
		return ""
	}

	summary := strings.TrimSpace("gives " + given[0].Label + " " + given[0].Text)
	if len(given) > 1 {
		summary += " and " + describeNumber(float64(len(given)-1)) + " more"
	}

	return summary
}

// describeLinkCondition returns the condition of the block a link is written
// in, "" where it holds always.
func describeLinkCondition(ws *workspace.Session, mods *index.Index, link index.Link) string {
	tree, statement, err := ws.FindStatement(mods, link.Use)
	if err != nil {
		return ""
	}

	return describeBlockCondition(ws, mods, tree, tree.Statements[statement].Parent, map[int32]bool{statement: true})
}

// describeLine returns the quiet line of a building's head: its group, how
// many levels and production methods it has, and the facts its database
// writes of where it can stand.
func describeLine(ws *workspace.Session, mods *index.Index, levels []index.Site, line *Line) string {
	var parts []string
	title := ws.LookupTitle(mods, levels[0])
	if groups := listGroups(ws, mods, levels[0]); len(groups) > 0 && groups[0].Title != title {
		parts = append(parts, groups[0].Title)
	}

	if len(levels) > 1 {
		parts = append(parts, countOf(len(levels), "level"))
	}

	methods := 0
	for _, group := range line.Groups {
		methods += len(group.Methods)
	}

	if methods > 0 {
		parts = append(parts, countOf(methods, "production method"))
	}

	for _, fact := range listHeadFacts(ws, mods, levels[0]) {
		parts = append(parts, fact.Label+" "+strings.ToLower(fact.Text))
	}

	return strings.Join(parts, " · ")
}

// buildLine returns a line as a table: a column for each level with what
// unlocks it, the rows of what the levels say with no condition, and the
// production methods with what the picked ones come to.
func buildLine(ws *workspace.Session, mods *index.Index, levels []index.Site, filter Filter, eras []Node) *Line {
	line := &Line{}
	conditional := map[Fact]bool{}
	for position, level := range levels {
		column := LineLevel{Level: makeNode(ws, mods, level), Number: position + 1}
		if unlocks := pickLinks(ws, mods, level, index.RelationUnlock, false); len(unlocks) > 0 {
			unlock := makeUnlock(ws, mods, unlocks[0].Source, column.Level.Title, eras)
			column.Unlock = &unlock
		}

		line.Levels = append(line.Levels, column)
		for _, written := range listLevelFacts(ws, mods, level) {
			if written.fact.When != "" || written.fact.Need != "" {
				written.fact.Text, written.fact.Tone = "", ""
				conditional[written.fact] = true

				continue
			}

			row := findRow(line, written, len(levels))
			row.Cells[position] = strings.TrimPrefix(row.Cells[position]+" and "+written.fact.Text, " and ")
		}
	}

	for band := range line.Bands {
		for position, row := range line.Bands[band].Rows {
			if len(slices.Compact(slices.Clone(row.Cells))) == 1 {
				line.Bands[band].Rows[position].Cells = row.Cells[:1]
			}
		}
	}

	line.Conditional = len(conditional)
	line.Groups = listMethodGroups(ws, mods, levels[0], filter, eras)
	if len(line.Groups) > 0 && line.Groups[0].Group.ID != "" {
		line.Totals = sumMethods(line.Groups)
	}

	return line
}

// makeUnlock returns the node of a technology that unlocks a building, with
// its era. A technology called the same as what it unlocks is said by its
// era.
func makeUnlock(ws *workspace.Session, mods *index.Index, technology index.Site, unlocked string, eras []Node) Node {
	unlock := makeNode(ws, mods, technology)
	unlock.Era = findEra(ws, mods, technology)
	era := slices.IndexFunc(eras, func(node Node) bool { return node.ID == unlock.Era })
	if era >= 0 && unlock.Title == unlocked {
		unlock.Title = eras[era].Title
	}

	return unlock
}

// levelFact is one thing a level says of itself: the band it stands under,
// the good or pop type it is about, and the fact.
type levelFact struct {
	band    string
	subject *Node
	fact    Fact
}

// findRow returns the row of a line's table that a level's fact belongs to,
// made on its first fact, with a cell for each level.
func findRow(line *Line, written levelFact, levels int) *LineRow {
	band := slices.IndexFunc(line.Bands, func(other LineBand) bool { return other.Title == written.band })
	if band < 0 {
		band, line.Bands = len(line.Bands), append(line.Bands, LineBand{Title: written.band})
	}

	subject := ""
	if written.subject != nil {
		subject = written.subject.ID
	}

	rows := line.Bands[band].Rows
	position := slices.IndexFunc(rows, func(row LineRow) bool {
		return row.Label == written.fact.Label && (row.Subject == nil && subject == "" || row.Subject != nil && row.Subject.ID == subject)
	})
	if position < 0 {
		position = len(rows)
		row := LineRow{Subject: written.subject, Label: written.fact.Label, Tone: written.fact.Tone, Cells: make([]string, levels)}
		line.Bands[band].Rows = append(line.Bands[band].Rows, row)
	}

	return &line.Bands[band].Rows[position]
}

// listLevelFacts returns what one level says, in the order its facts are
// first found: what it makes, takes and employs, what it costs, then what its
// modifiers give. A fact written in a block with a condition carries it.
func listLevelFacts(ws *workspace.Session, mods *index.Index, level index.Site) []levelFact {
	var facts []levelFact
	bands := map[index.Relation]string{
		index.RelationOutput: bandMakes, index.RelationBoost: bandMakes, index.RelationGives: bandMakes,
		index.RelationInput: bandTakes, index.RelationEmployment: bandEmploys,
	}
	for _, link := range mods.ListLinks(ws.Schema, level) {
		band, ok := bands[link.Relation]
		if !ok || link.Source != level {
			continue
		}

		if link.AmountKey != "" && link.Relation == index.RelationInput {
			band = bandNeeds
		}

		subject := makeNode(ws, mods, link.Target)
		fact := Fact{Label: cmp.Or(relationWords[link.Relation], "employs"), When: describeLinkCondition(ws, mods, link)}
		if number, isNumber := readAmount(ws, mods, link); isNumber {
			fact.Text = describeRange([]float64{number}, link.Share)
		}

		if link.Relation == index.RelationGives {
			fact.When = cmp.Or(fact.When, "when its need is met")
		}

		facts = append(facts, levelFact{band: band, subject: &subject, fact: fact})
	}

	for _, cost := range listCosts(ws, mods, level) {
		facts = append(facts, levelFact{band: bandCosts, fact: cost})
	}

	for _, given := range listGives(ws, mods, level) {
		facts = append(facts, levelFact{band: cmp.Or(given.Where, bandGives), fact: given})
	}

	return facts
}

// listMethodGroups returns a building's production method groups with their
// methods. The methods a building names in no group, as EU5's, are one group
// with no name. A group runs the picked method that is in it, else its first.
func listMethodGroups(ws *workspace.Session, mods *index.Index, building index.Site, filter Filter, eras []Node) []MethodGroup {
	var groups []MethodGroup
	var loose MethodGroup
	for _, part := range pickLinks(ws, mods, building, index.RelationPart, false) {
		switch ws.Schema.LookupType(mods.ToReference(part.Source).Database) {
		case lang.TypeProductionMethod:
			loose.Methods = append(loose.Methods, makeMethod(ws, mods, part.Source, filter, eras))

		case lang.TypeProductionMethodGroup:
			group := MethodGroup{Group: makeNode(ws, mods, part.Source)}
			for _, held := range pickLinks(ws, mods, part.Source, index.RelationPart, false) {
				group.Methods = append(group.Methods, makeMethod(ws, mods, held.Source, filter, eras))
			}

			groups = append(groups, group)
		}
	}

	if len(loose.Methods) > 0 {
		groups = append([]MethodGroup{loose}, groups...)
	}

	for position, group := range groups {
		for _, method := range group.Methods {
			if groups[position].Picked == "" || slices.Contains(filter.Picked, method.Method.ID) {
				groups[position].Picked = method.Method.ID
			}
		}

		if len(group.Methods) > 0 && !slices.Contains(filter.Picked, groups[position].Picked) {
			groups[position].Picked = group.Methods[0].Method.ID
		}
	}

	return groups
}

// makeMethod returns a production method with what it takes, makes and
// employs, what else it gives, what unlocks it and the goods its building
// must reach.
func makeMethod(ws *workspace.Session, mods *index.Index, definition index.Site, filter Filter, eras []Node) Method {
	method := Method{
		Method: makeNode(ws, mods, definition), Gives: listGives(ws, mods, definition),
		Unlocks: []Node{}, Access: []Node{}, Takes: []Amount{}, Makes: []Amount{}, Employs: []Amount{},
	}
	limit := slices.IndexFunc(eras, func(era Node) bool { return era.ID == filter.Era })
	for _, link := range mods.ListLinks(ws.Schema, definition) {
		number, isNumber := readAmount(ws, mods, link)
		flow := Amount{Good: makeNode(ws, mods, link.Target), number: number, Unscaled: isUnscaled(ws, mods, link)}
		if isNumber {
			flow.Text = describeNumber(number)
		}

		switch {
		case link.Relation == index.RelationUnlock && link.Target == definition:
			unlock := makeNode(ws, mods, link.Source)
			unlock.Era = findEra(ws, mods, link.Source)
			method.Unlocks = append(method.Unlocks, unlock)
			era := slices.IndexFunc(eras, func(other Node) bool { return other.ID == unlock.Era })
			method.Late = method.Late || limit >= 0 && era > limit

		case link.Source != definition:
		case link.Relation == index.RelationAccess:
			method.Access = append(method.Access, flow.Good)
		case link.Relation == index.RelationInput:
			method.Takes = append(method.Takes, flow)
		case link.Relation == index.RelationOutput:
			method.Makes = append(method.Makes, flow)
		case link.Relation == index.RelationEmployment:
			method.Employs = append(method.Employs, flow)
		}
	}

	return method
}

// isUnscaled reports whether a link's amount is written under a Vic3
// production method's unscaled block.
func isUnscaled(ws *workspace.Session, mods *index.Index, link index.Link) bool {
	tree, statement, err := ws.FindStatement(mods, link.Use)
	if err != nil {
		return false
	}

	return tree.Text(tree.Statements[tree.Statements[statement].Parent].Key) == unscaledKey
}

// sumMethods returns what the picked production methods come to: for each
// good and pop type the sum over the picked methods, beside the sum over the
// first method of each group where that differs, and the jobs in all. A
// method's amount below zero takes away from what the other groups give.
func sumMethods(groups []MethodGroup) *Totals {
	totals := &Totals{}
	jobs, jobsBefore := 0.0, 0.0
	for _, group := range groups {
		for position, method := range group.Methods {
			isPicked, isFirst := method.Method.ID == group.Picked, position == 0
			totals.Takes = addTotals(totals.Takes, method.Takes, isPicked, isFirst)
			totals.Makes = addTotals(totals.Makes, method.Makes, isPicked, isFirst)
			totals.Employs = addTotals(totals.Employs, method.Employs, isPicked, isFirst)
			for _, employed := range method.Employs {
				if isPicked {
					jobs += employed.number
				}

				if isFirst {
					jobsBefore += employed.number
				}
			}
		}
	}

	for _, side := range [][]Total{totals.Takes, totals.Makes, totals.Employs} {
		for position, total := range side {
			side[position].Text, side[position].Before = describeNumber(total.picked), ""
			switch {
			case !total.isFirst:
				side[position].Before = "new"
			case total.first != total.picked:
				side[position].Before = describeNumber(total.first) + " with the first methods"
			}
		}
	}

	totals.Jobs = describeNumber(jobs) + " jobs in all"
	if jobsBefore != jobs {
		totals.Jobs += ", against " + describeNumber(jobsBefore) + " with the first methods"
	}

	return totals
}

// addTotals adds one method's amounts to the totals of a side: to what the
// picked methods come to, and to what the first method of each group does.
func addTotals(totals []Total, flows []Amount, isPicked, isFirst bool) []Total {
	for _, flow := range flows {
		if !isPicked && !isFirst {
			continue
		}

		position := slices.IndexFunc(totals, func(total Total) bool { return total.Good.ID == flow.Good.ID })
		if position < 0 {
			position, totals = len(totals), append(totals, Total{Good: flow.Good})
		}

		if isPicked {
			totals[position].picked += flow.number
		}

		if isFirst {
			totals[position].first, totals[position].isFirst = totals[position].first+flow.number, true
		}
	}

	return totals
}
