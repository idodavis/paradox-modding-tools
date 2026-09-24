// learn.go is the learner's two entries, the one read of an install's files, and the order
// everything is worked out in: count, then kinds, then the joins that need every kind's names
// (prefixes, macros), then each kind's fields, then what nothing explained.
package learn

import (
	"context"
	"maps"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Input is what a scan hands the learner: the inventory, and what was read before it (loc, game
// info, vocabulary).
type Input struct {
	Game, Version string
	Files         []source.File
	Vocab         *schema.Vocab
	Info          schema.Info
	Loc           *loc.Keys
	EntryModes    []string // the words the game's registry gives a meaning, for the drift report
	Subjects      []string // the registry's folders whose definitions are named after other kinds' (history, flags)
}

// Learn reads an install's files once and gives the schema of what they hold.
func Learn(ctx context.Context, in Input) (*schema.Schema, error) {
	census := newCensus(in.Vocab)
	census.keys, census.info, census.aboutDirs = in.Loc, in.Info, in.Subjects
	images := map[string][]string{}
	for _, file := range in.Files {
		rel := strings.ToLower(file.Rel)
		census.files[rel], census.files[path.Base(rel)], census.files[strings.TrimSuffix(path.Base(rel), path.Ext(rel))+".*"] = true, true, true
		if file.Format == source.Image {
			base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
			images[base] = append(images[base], path.Dir(file.Rel)+"/{id}"+path.Ext(rel))
		}
	}
	parseErrors, err := census.read(ctx, in.Files)
	if err != nil {
		return nil, err
	}
	census.tables(in.Files)
	census.witnesses()

	// kinds, then what needs every kind's names
	drafts := census.kinds()
	census.index(drafts)
	census.prefixes(drafts)
	census.settle(drafts)
	census.index(drafts)
	for _, draft := range drafts {
		// read before the lift: a parameter written under a token leaves the shape with it
		draft.params = draft.shape.holdsParams()
		census.lift(draft.shape)
		census.hoist(draft.shape)
	}
	for next := slices.Sorted(maps.Keys(census.tokens)); len(next) > 0; next = slices.Sorted(maps.Keys(census.moved)) {
		clear(census.moved)
		for _, token := range next {
			// a token whose own block the dump says holds effects or triggers is that body,
			// whatever the install writes in it
			n := census.tokens[token]
			effect, trigger := census.vocab.Effects[token], census.vocab.Triggers[token]
			var declared schema.BodyClass
			if census.effectBlocks[token] && (effect.Usage == "" || holdsDirectly(effect.Usage, "effects>") || holdsDirectly(effect.Usage, "effect>")) {
				declared |= schema.BodyEffect
			}
			if census.triggerBlocks[token] && (trigger.Usage == "" || holdsDirectly(trigger.Usage, "triggers>") || holdsDirectly(trigger.Usage, "trigger>")) {
				declared |= schema.BodyTrigger
			}
			if !n.classed && declared != 0 {
				n.classed, n.class, n.body = true, schema.ClassBody, declared
			}

			// and so is a member the usage writes with a block of them: limit = { <triggers> }
			for key, member := range n.members {
				inside := holds(effect.Usage+" "+trigger.Usage, key)
				if !member.classed && inside != 0 {
					member.classed, member.class, member.body = true, schema.ClassBody, inside
				}
			}
			census.lift(n)
		}
	}
	census.macros(drafts)
	sch := &schema.Schema{Game: in.Game, Version: in.Version, Kinds: map[string]*schema.Kind{}, Arguments: map[string]*schema.Field{}}
	for _, draft := range drafts {
		kind := draft.kind
		kind.Shape = census.field("", draft.shape, kind.Folders, "")
		kind.Shape.Name = ""
		kind.LocTemplates = loc.Learn(slices.Sorted(maps.Keys(draft.names)), slices.Sorted(maps.Keys(draft.shape.members)), in.Loc)
		kind.FileTemplates = fileTemplates(draft.names, images)
		kind.LabelMember = census.label(draft)
		kind.Params = kind.Macro != 0 && draft.params
		sch.Kinds[kind.Name] = kind
	}

	// the kinds script makes as it runs, the constants of any file, and the parameters of any macro
	for kind, prefix := range schema.ScriptKinds {
		made := &schema.Kind{Name: kind, Format: source.Script, Naming: schema.Naming{Form: "token"}}
		if prefix != "" {
			made.Prefixes = []string{prefix}
		}
		sch.Kinds[kind] = made
	}
	if len(census.constants) > 0 {
		sch.Kinds["constants"] = &schema.Kind{Name: "constants", Format: source.Script, Naming: schema.Naming{Form: "constant"}}
	}
	for _, kind := range sch.Kinds {
		if kind.Params {
			sch.Kinds["params"] = &schema.Kind{Name: "params", Format: source.Script, Naming: schema.Naming{Form: "param"}}
			break
		}
	}
	for _, token := range slices.Sorted(maps.Keys(census.tokens)) {
		sch.Arguments[token] = census.field(token, census.tokens[token], nil, "")
	}
	sch.Drift = census.drift(in, parseErrors, sch)
	return sch, nil
}

// Relearn gives a workspace's schema: the install's, with a kind for each folder in which the
// install defines nothing, and an entry for each declared token the install never writes. It
// never adds a member to a field the install learned, so a mod's typo is never learned as valid.
func Relearn(ctx context.Context, in Input, install *schema.Schema) (*schema.Schema, error) {
	mods, err := Learn(ctx, in)
	if err != nil {
		return nil, err
	}
	out := &schema.Schema{Game: install.Game, Version: install.Version, Kinds: maps.Clone(install.Kinds), Arguments: maps.Clone(install.Arguments), Drift: install.Drift}
	for _, name := range slices.Sorted(maps.Keys(mods.Kinds)) {
		kind := mods.Kinds[name]
		fresh := len(kind.Folders) > 0 && !slices.ContainsFunc(kind.Folders, func(folder string) bool { return len(install.KindsAt(folder+"/x")) > 0 })
		if fresh && out.Kinds[name] == nil {
			out.Kinds[name] = kind
		}
	}
	for token, argument := range mods.Arguments {
		if out.Arguments[token] == nil {
			out.Arguments[token] = argument
		}
	}
	return out, nil
}

// read parses every script file with one reader per CPU and counts them in the inventory's
// order, so two scans of one install agree. A first read of a file costs milliseconds of
// waiting whatever its size, and that wait divides among readers.
func (census *census) read(ctx context.Context, files []source.File) (parseErrors []schema.Tally, err error) {
	type parsed struct {
		i   int
		res jomini.Result
	}
	var scripts []int
	for i, file := range files {
		if file.Format == source.Script {
			scripts = append(scripts, i)
		}
	}

	// readers take the files in order, and no more are in flight than the window holds
	workers := runtime.NumCPU()
	window, jobs, out := make(chan struct{}, workers), make(chan int), make(chan parsed, workers)
	go func() {
		defer close(jobs)
		for _, script := range scripts {
			select {
			case window <- struct{}{}:
				jobs <- script
			case <-ctx.Done():
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for script := range jobs {
				text, _ := files[script].Text()
				out <- parsed{script, jomini.Parse(text)}
			}
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	// count each file when its turn comes
	waiting, turn := map[int]jomini.Result{}, 0
	for result := range out {
		waiting[result.i] = result.res
		for turn < len(scripts) {
			res, ready := waiting[scripts[turn]]
			if !ready {
				break
			}
			file := files[scripts[turn]]
			if len(res.Errors) > 0 {
				parseErrors = append(parseErrors, schema.Tally{Name: file.Rel, Count: len(res.Errors)})
			}
			census.file(file, res.Root)
			delete(waiting, scripts[turn])
			turn++
			<-window
		}
	}
	return parseErrors, ctx.Err()
}

// tables reads the files that are no script and hold definitions: a table, whose kind is named
// by the first column whose every value is its own (map_data/definition.csv by column 0) and
// defines nothing where no column is; a list, named by what follows the GUID on each line.
// Each is a kind of its own, named after its folder and file.
func (census *census) tables(files []source.File) {
	for _, file := range files {
		if file.Format != source.Table && file.Format != source.List {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)))
		draft := &draft{kind: &schema.Kind{Name: path.Base(path.Dir(file.Rel)) + "." + base, Format: file.Format, Folders: []string{file.Rel}, Naming: schema.Naming{Form: "none"}}, names: map[string]int{}, shape: &node{members: map[string]*node{}}}

		var rows [][]string
		for line := range strings.SplitSeq(text, "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if file.Format == source.List {
				_, name, _ := strings.Cut(line, " ")
				draft.names[strings.TrimSpace(name)]++
				continue
			}
			rows = append(rows, strings.Split(line, ";"))
		}
		if file.Format == source.List {
			draft.kind.Naming.Form = "line"
			census.extra = append(census.extra, draft)
			continue
		}

		// every column is a position, and the first whose values are all their own names the rows
		for _, row := range rows {
			draft.shape.count, draft.shape.blocks = draft.shape.count+1, draft.shape.blocks+1
			for i, cell := range row {
				key := "<" + strconv.Itoa(i) + ">"
				if draft.shape.members[key] == nil {
					draft.shape.members[key] = &node{}
				}
				col := draft.shape.members[key]
				col.count, col.in = col.count+1, col.in+1
				census.value(col, &jomini.Scalar{Text: strings.TrimSpace(cell)})
			}
		}
		for i := 0; draft.kind.Naming.Form == "none" && draft.shape.members["<"+strconv.Itoa(i)+">"] != nil; i++ {
			if col := draft.shape.members["<"+strconv.Itoa(i)+">"]; len(col.words) == len(rows) {
				draft.kind.Naming = schema.Naming{Form: "column", Column: i}
				draft.names = maps.Clone(col.words)
			}
		}
		census.extra = append(census.extra, draft)
	}
}

// index is every kind by name, every definition's name to the kinds that define it, and the
// kinds named by number.
func (census *census) index(drafts []*draft) {
	census.byName, census.defined, census.numbered = map[string]*draft{}, map[string][]*draft{}, nil
	for _, draft := range drafts {
		census.byName[draft.kind.Name] = draft
		numbers := 0
		for name := range draft.names {
			if draft.subject == nil {
				census.defined[name] = append(census.defined[name], draft)
			}
			if jomini.Literal(name) == jomini.LitNumber {
				numbers++
			}
		}
		if numbers*2 > len(draft.names) && draft.subject == nil {
			census.numbered = append(census.numbered, draft)
		}
	}
}

// holdsDirectly reports a usage whose own block holds the placeholder, not a block inside it:
// if = { limit = { <triggers> } <effects> } holds effects, and its limit holds the triggers.
func holdsDirectly(usage, placeholder string) bool {
	at := strings.Index(usage, placeholder)
	return at >= 0 && strings.Count(usage[:at], "{")-strings.Count(usage[:at], "}") == 1
}

// holds is what a usage says a member's block holds: limit = { <triggers> } holds triggers.
func holds(usage, member string) schema.BodyClass {
	_, after, found := strings.Cut(usage, member+" = {")
	block, _, _ := strings.Cut(after, "}")
	switch {
	case !found:
		return 0
	case strings.Contains(block, "triggers>") || strings.Contains(block, "trigger>"):
		return schema.BodyTrigger
	case strings.Contains(block, "effects>") || strings.Contains(block, "effect>"):
		return schema.BodyEffect
	}
	return 0
}

// prefixes gives each prefix the vocabulary declares to the kind that defines most of the names
// the install writes after it, and with it the scope type the prefix leads to. Of two such
// kinds, the one that defines more of them, then the one with fewer names (epidemic_type: cites
// six epidemics that share six traits' names). A kind the registry says is named after others
// owns one only where no other kind could. A prefix only the install spells is drift, never
// learned; a script-made kind's prefix cites names script makes, which no file defines.
func (census *census) prefixes(drafts []*draft) {
	census.prefixed = map[string][]*draft{}
	made := slices.Collect(maps.Values(schema.ScriptKinds))
	for _, prefix := range slices.Sorted(maps.Keys(census.cites)) {
		names, link := census.cites[prefix], census.vocab.Links[prefix]
		if !link.Prefix || slices.Contains(made, prefix) {
			continue
		}
		hits := map[*draft]int{}
		for name := range names {
			for _, definer := range census.defined[name] {
				hits[definer]++
			}
		}
		var best *draft
		for _, draft := range drafts {
			if hits[draft]*2 > len(names) && (best == nil || (census.about(best) && !census.about(draft)) || (census.about(best) == census.about(draft) && (hits[draft] > hits[best] || (hits[draft] == hits[best] && len(draft.names) < len(best.names))))) {
				best = draft
			}
		}
		if best != nil {
			best.kind.Prefixes = append(best.kind.Prefixes, prefix)
			census.prefixed[prefix] = append(census.prefixed[prefix], best)
			if link.Out != "" && !strings.Contains(link.Out, ",") && best.kind.ScopeType == "" {
				best.kind.ScopeType = link.Out
			}
		}
		// a nested kind named by the prefix holds it too: EU5 spreads its defines over three
		// folders, three kinds, one prefix
		for _, draft := range drafts {
			if draft != best && strings.HasSuffix(draft.kind.Name, "."+prefix) && !slices.Contains(draft.kind.Prefixes, prefix) {
				draft.kind.Prefixes = append(draft.kind.Prefixes, prefix)
				census.prefixed[prefix] = append(census.prefixed[prefix], draft)
			}
		}
	}
}

// macros finds the kinds whose definitions are invoked by writing their names as keys in a
// body, and in which classes of body: a kind of bodies where more than one and most of its
// names are called (one call may be chance), a kind that computes a number wherever one is
// called (any trigger may compare a number), and behind an iterator prefix the kind that
// defines most of the names called, then the smallest (every_adult_child calls adult_child).
func (census *census) macros(drafts []*draft) {
	for in, keys := range census.called {
		stems, lists := map[string]bool{}, map[*draft]int{}
		for key := range keys {
			for _, prefix := range []string{"any_", "every_", "random_", "ordered_"} {
				if stem, ok := strings.CutPrefix(key, prefix); ok && len(census.defined[stem]) > 0 {
					stems[stem] = true
				}
			}
		}
		var list *draft
		for _, draft := range drafts {
			called := 0
			for name := range draft.names {
				if keys[name] > 0 {
					called++
				}
				if stems[name] {
					lists[draft]++
				}
			}
			class, body := census.class(draft.shape)
			if draft.parent == nil && draft.subject == nil && class == schema.ClassBody && ((called > 1 && called*2 > len(draft.names)) || (called > 0 && body == schema.BodyMath)) {
				draft.kind.Macro |= in
			}
			if lists[draft]*2 > len(stems) && (list == nil || lists[draft] > lists[list] || (lists[draft] == lists[list] && len(draft.names) < len(list.names))) {
				list = draft
			}
		}
		if list != nil {
			list.kind.Macro |= in
		}
	}
	for _, draft := range drafts {
		if draft.kind.Macro != 0 {
			for name := range draft.names {
				census.macro[name] = true
			}
		}
	}
}

// fileTemplates is the files the game finds by a definition's name: a folder and extension in
// which most of the kind's names have an image.
func fileTemplates(names map[string]int, images map[string][]string) []schema.FileTemplate {
	held := map[string]int{}
	for name := range names {
		for _, pattern := range images[strings.ToLower(name)] {
			held[pattern]++
		}
	}
	var out []schema.FileTemplate
	for _, pattern := range slices.Sorted(maps.Keys(held)) {
		if held[pattern]*2 > len(names) {
			out = append(out, schema.FileTemplate{Pattern: pattern, Coverage: float64(held[pattern]) / float64(len(names))})
		}
	}
	slices.SortStableFunc(out, func(first, second schema.FileTemplate) int { return int((second.Coverage - first.Coverage) * 1e6) })
	return out
}

// label is the member whose value is a definition's human name, for a kind named by number: the
// member most of its definitions hold whose value is quoted text that names no definition (a
// character's name, not its culture).
func (census *census) label(draft *draft) string {
	numbers := 0
	for name := range draft.names {
		if jomini.Literal(name) == jomini.LitNumber {
			numbers++
		}
	}
	best := ""
	for _, key := range slices.Sorted(maps.Keys(draft.shape.members)) {
		member, refs := draft.shape.members[key], 0
		for word := range member.words {
			if len(census.defined[word]) > 0 {
				refs++
			}
		}
		if numbers*2 >= len(draft.names) && member.in*2 >= draft.shape.blocks && member.quoted*2 >= member.count && refs*2 < len(member.words) && (best == "" || member.in > draft.shape.members[best].in) {
			best = key
		}
	}
	return best
}

// holdsParams reports a position that holds $NAME$ anywhere below it.
func (pos *node) holdsParams() bool {
	for _, member := range pos.members {
		if member.holdsParams() {
			return true
		}
	}
	return pos.params > 0
}
