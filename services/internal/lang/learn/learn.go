// learn.go is the learner's two entries, the one read of an install's files, and the order everything is worked out in: count, then kinds, then the joins that need every kind's names (prefixes, macros), then each kind's fields, then what nothing explained.
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
	"paradox-modding-tools/services/internal/source"
)

// Input is what a scan hands the learner: the inventory, and what was read before it (§12: loc, game info, vocabulary).
type Input struct {
	Game, Version string
	Files         []source.File
	Vocab         *schema.Vocab
	Info          schema.Info
	Loc           *loc.Keys
	EntryModes    []string // the words the game's registry gives a meaning, for the drift report
	Subjects      []string // the folders the game's registry says hold definitions named after other kinds' (history, coats of arms, flags): a kind under one is about the kind that defines its names
}

// Learn reads an install's files once and gives the schema of what they hold.
func Learn(ctx context.Context, in Input) (*schema.Schema, error) {
	c := newCensus(in.Vocab)
	c.keys, c.info, c.aboutDirs = in.Loc, in.Info, in.Subjects
	images := map[string][]string{}
	for _, f := range in.Files {
		rel := strings.ToLower(f.Rel)
		c.files[rel], c.files[path.Base(rel)] = true, true
		if f.Format == source.Image {
			base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
			images[base] = append(images[base], path.Dir(f.Rel)+"/{id}"+path.Ext(rel))
		}
	}
	parseErrors, err := c.read(ctx, in.Files)
	if err != nil {
		return nil, err
	}
	c.tables(in.Files)
	c.witnesses()

	// kinds, then what needs every kind's names
	drafts := c.kinds()
	c.index(drafts)
	c.prefixes(drafts)
	c.settle(drafts)
	c.index(drafts)
	for _, d := range drafts {
		c.lift(d.shape)
		c.hoist(d.shape)
	}
	for c.moved = true; c.moved; {
		c.moved = false
		for _, token := range slices.Sorted(maps.Keys(c.tokens)) {
			// a token whose own block the dump says holds effects or triggers is that body, whatever the install writes in it
			n := c.tokens[token]
			effect, trigger := c.vocab.Effects[token], c.vocab.Triggers[token]
			var declared schema.BodyClass
			if c.effectBlocks[token] && (effect.Usage == "" || holdsDirectly(effect.Usage, "effects>") || holdsDirectly(effect.Usage, "effect>")) {
				declared |= schema.BodyEffect
			}
			if c.triggerBlocks[token] && (trigger.Usage == "" || holdsDirectly(trigger.Usage, "triggers>") || holdsDirectly(trigger.Usage, "trigger>")) {
				declared |= schema.BodyTrigger
			}
			if !n.classed && declared != 0 {
				n.classed, n.class, n.body = true, schema.ClassBody, declared
			}

			// and so is a member the usage writes with a block of them: limit = { <triggers> }
			for key, m := range n.members {
				inside := holds(effect.Usage+" "+trigger.Usage, key)
				if !m.classed && inside != 0 {
					m.classed, m.class, m.body = true, schema.ClassBody, inside
				}
			}
			c.lift(n)
		}
	}
	c.macros(drafts)
	s := &schema.Schema{Game: in.Game, Version: in.Version, Kinds: map[string]*schema.Kind{}, Arguments: map[string]*schema.Field{}}
	for _, d := range drafts {
		k := d.kind
		k.Shape = c.field("", d.shape, k.Folders, "")
		k.Shape.Name = ""
		k.LocTemplates = loc.Learn(slices.Sorted(maps.Keys(d.names)), slices.Sorted(maps.Keys(d.shape.members)), in.Loc)
		k.FileTemplates = fileTemplates(d.names, images)
		k.LabelMember = c.label(d)
		k.Params = k.Macro != 0 && d.shape.holdsParams()
		s.Kinds[k.Name] = k
	}

	// the kinds script makes while it runs, and the constants of any file
	for kind, prefix := range schema.ScriptKinds {
		k := &schema.Kind{Name: kind, Format: source.Script, Naming: schema.Naming{Form: "token"}}
		if prefix != "" {
			k.Prefixes = []string{prefix}
		}
		s.Kinds[kind] = k
	}
	if len(c.constants) > 0 {
		s.Kinds["constants"] = &schema.Kind{Name: "constants", Format: source.Script, Naming: schema.Naming{Form: "constant"}}
	}
	for _, token := range slices.Sorted(maps.Keys(c.tokens)) {
		s.Arguments[token] = c.field(token, c.tokens[token], nil, "")
	}
	maps.Copy(s.Kinds, c.locKinds)
	s.Drift = c.drift(in, parseErrors, s)
	return s, nil
}

// Relearn gives a workspace's schema: the install's, with a kind for each folder in which the install defines nothing, and an entry for each declared token the install never writes. It never adds a member to a field the install learned, so a mod's typo is never learned as valid.
func Relearn(ctx context.Context, in Input, install *schema.Schema) (*schema.Schema, error) {
	mods, err := Learn(ctx, in)
	if err != nil {
		return nil, err
	}
	out := &schema.Schema{Game: install.Game, Version: install.Version, Kinds: maps.Clone(install.Kinds), Arguments: maps.Clone(install.Arguments), Drift: install.Drift}
	for _, name := range slices.Sorted(maps.Keys(mods.Kinds)) {
		k := mods.Kinds[name]
		fresh := len(k.Folders) > 0 && !slices.ContainsFunc(k.Folders, func(f string) bool { return len(install.KindsAt(f+"/x")) > 0 })
		if fresh && out.Kinds[name] == nil {
			out.Kinds[name] = k
		}
	}
	for token, f := range mods.Arguments {
		if out.Arguments[token] == nil {
			out.Arguments[token] = f
		}
	}
	return out, nil
}

// read parses every script file with one reader per CPU and counts them in the inventory's order, so two scans of one install agree. A first read of a file costs milliseconds of waiting whatever its size, and that wait divides among readers.
func (c *census) read(ctx context.Context, files []source.File) (parseErrors []schema.Tally, err error) {
	type parsed struct {
		i   int
		res jomini.Result
	}
	var scripts []int
	for i, f := range files {
		if f.Format == source.Script {
			scripts = append(scripts, i)
		}
	}

	// readers take the files in order, and no more are in flight than the window holds
	workers := runtime.NumCPU()
	window, jobs, out := make(chan struct{}, workers*4), make(chan int), make(chan parsed, workers*4)
	go func() {
		defer close(jobs)
		for _, i := range scripts {
			select {
			case window <- struct{}{}:
				jobs <- i
			case <-ctx.Done():
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				text, _ := files[i].Text()
				out <- parsed{i, jomini.Parse(text)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	// count each file when its turn comes
	waiting, turn := map[int]jomini.Result{}, 0
	for p := range out {
		waiting[p.i] = p.res
		for turn < len(scripts) {
			res, ready := waiting[scripts[turn]]
			if !ready {
				break
			}
			f := files[scripts[turn]]
			if len(res.Errors) > 0 {
				parseErrors = append(parseErrors, schema.Tally{Name: f.Rel, Count: len(res.Errors)})
			}
			c.file(f, res.Root)
			delete(waiting, scripts[turn])
			turn++
			<-window
		}
	}
	return parseErrors, ctx.Err()
}

// tables reads the files that are no script and hold definitions: a table, whose kind is named by the first column whose every value is its own (map_data/definition.csv by column 0) and defines nothing where no column is; a list, named by what follows the GUID on each line. Each is a kind of its own, named after its folder and file (§5.2).
func (c *census) tables(files []source.File) {
	for _, f := range files {
		if f.Format != source.Table && f.Format != source.List {
			continue
		}
		text, err := f.Text()
		if err != nil {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(path.Base(f.Rel), path.Ext(f.Rel)))
		d := &draft{kind: &schema.Kind{Name: path.Base(path.Dir(f.Rel)) + "." + base, Format: f.Format, Folders: []string{f.Rel}, Naming: schema.Naming{Form: "none"}}, names: map[string]int{}, shape: &node{members: map[string]*node{}}}

		var rows [][]string
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if f.Format == source.List {
				_, name, _ := strings.Cut(line, " ")
				d.names[strings.TrimSpace(name)]++
				continue
			}
			rows = append(rows, strings.Split(line, ";"))
		}
		if f.Format == source.List {
			d.kind.Naming.Form = "line"
			c.extra = append(c.extra, d)
			continue
		}

		// every column is a position, and the first whose values are all their own names the rows
		for _, row := range rows {
			d.shape.count, d.shape.blocks = d.shape.count+1, d.shape.blocks+1
			for i, cell := range row {
				key := "<" + strconv.Itoa(i) + ">"
				if d.shape.members[key] == nil {
					d.shape.members[key] = &node{}
				}
				col := d.shape.members[key]
				col.count, col.in = col.count+1, col.in+1
				c.value(col, &jomini.Scalar{Text: strings.TrimSpace(cell)})
			}
		}
		for i := 0; d.kind.Naming.Form == "none" && d.shape.members["<"+strconv.Itoa(i)+">"] != nil; i++ {
			if col := d.shape.members["<"+strconv.Itoa(i)+">"]; len(col.words) == len(rows) {
				d.kind.Naming = schema.Naming{Form: "column", Column: i}
				d.names = maps.Clone(col.words)
			}
		}
		c.extra = append(c.extra, d)
	}
}

// index is every kind by name, every definition's name to the kinds that define it, and the kinds named by number.
func (c *census) index(drafts []*draft) {
	c.byName, c.defined, c.numbered = map[string]*draft{}, map[string][]*draft{}, nil
	for _, d := range drafts {
		c.byName[d.kind.Name] = d
		numbers := 0
		for name := range d.names {
			if d.subject == nil {
				c.defined[name] = append(c.defined[name], d)
			}
			if jomini.Literal(name) == jomini.LitNumber {
				numbers++
			}
		}
		if numbers*2 > len(d.names) && d.subject == nil {
			c.numbered = append(c.numbered, d)
		}
	}
}

// holdsDirectly reports a usage whose own block holds the placeholder, not a block inside it: if = { limit = { <triggers> } <effects> } holds effects, and its limit holds the triggers.
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

// prefixes gives each prefix the vocabulary declares to the kind that defines most of the names the install writes after it, and with it the scope type the prefix leads to. Of two such kinds the one that defines more of them (every dynasty that dynasty: cites is a dynasty, and most have a coat of arms named after them), then the one with fewer names, which is the closer fit: six epidemics carry the names of six traits, and epidemic_type: cites the epidemics. A kind the registry says is named after others owns one only where no other kind could (character: cites the characters of history): every country has a coat of arms, and c: cites the countries. A prefix only the install spells is reported as drift, never learned: all: and player: cite a handful of common words that are also game-rule settings. A prefix of a script-made kind cites names script makes, which no file defines.
func (c *census) prefixes(drafts []*draft) {
	c.prefixed = map[string][]*draft{}
	made := slices.Collect(maps.Values(schema.ScriptKinds))
	for _, prefix := range slices.Sorted(maps.Keys(c.cites)) {
		names, link := c.cites[prefix], c.vocab.Links[prefix]
		if !link.Prefix || slices.Contains(made, prefix) {
			continue
		}
		hits := map[*draft]int{}
		for name := range names {
			for _, d := range c.defined[name] {
				hits[d]++
			}
		}
		var best *draft
		for _, d := range drafts {
			if hits[d]*2 > len(names) && (best == nil || (c.about(best) && !c.about(d)) || (c.about(best) == c.about(d) && (hits[d] > hits[best] || (hits[d] == hits[best] && len(d.names) < len(best.names))))) {
				best = d
			}
		}
		if best != nil {
			best.kind.Prefixes = append(best.kind.Prefixes, prefix)
			c.prefixed[prefix] = append(c.prefixed[prefix], best)
			if link.Out != "" && !strings.Contains(link.Out, ",") && best.kind.ScopeType == "" {
				best.kind.ScopeType = link.Out
			}
		}
	}
}

// macros finds the kinds whose definitions are invoked by writing their names as keys in a body, and in which classes of body. A kind whose definitions are bodies is one where most of its names are called, and more than one, because one call is no habit (nine scripted effects in ten are; a sound's trigger block shares one name with some key by chance). A kind whose definitions compute a number is one wherever a name is called, because a number may be compared in any trigger (a script value). And the names called behind an iterator prefix are those of the kind that defines most of them, then the smallest (every_adult_child is the scripted list adult_child; a game concept happens to be called courtier too).
func (c *census) macros(drafts []*draft) {
	for in, keys := range c.called {
		stems, lists := map[string]bool{}, map[*draft]int{}
		for key := range keys {
			for _, prefix := range []string{"any_", "every_", "random_", "ordered_"} {
				if stem, ok := strings.CutPrefix(key, prefix); ok && len(c.defined[stem]) > 0 {
					stems[stem] = true
				}
			}
		}
		var list *draft
		for _, d := range drafts {
			called := 0
			for name := range d.names {
				if keys[name] > 0 {
					called++
				}
				if stems[name] {
					lists[d]++
				}
			}
			class, body := c.class(d.shape)
			if d.parent == nil && d.subject == nil && class == schema.ClassBody && ((called > 1 && called*2 > len(d.names)) || (called > 0 && body == schema.BodyMath)) {
				d.kind.Macro |= in
			}
			if lists[d]*2 > len(stems) && (list == nil || lists[d] > lists[list] || (lists[d] == lists[list] && len(d.names) < len(list.names))) {
				list = d
			}
		}
		if list != nil {
			list.kind.Macro |= in
		}
	}
	for _, d := range drafts {
		if d.kind.Macro != 0 {
			for name := range d.names {
				c.macro[name] = true
			}
		}
	}
}

// fileTemplates is the files the game finds by a definition's name: a folder and extension in which most of the kind's names have an image.
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
	slices.SortStableFunc(out, func(a, b schema.FileTemplate) int { return int((b.Coverage - a.Coverage) * 1e6) })
	return out
}

// label is the member whose value is a definition's human name, for a kind named by number: the member most of its definitions hold whose value is quoted text that names no definition (a character's name, not its culture).
func (c *census) label(d *draft) string {
	numbers := 0
	for name := range d.names {
		if jomini.Literal(name) == jomini.LitNumber {
			numbers++
		}
	}
	best := ""
	for _, key := range slices.Sorted(maps.Keys(d.shape.members)) {
		m, refs := d.shape.members[key], 0
		for w := range m.words {
			if len(c.defined[w]) > 0 {
				refs++
			}
		}
		if numbers*2 >= len(d.names) && m.in*2 >= d.shape.blocks && m.quoted*2 >= m.count && refs*2 < len(m.words) && (best == "" || m.in > d.shape.members[best].in) {
			best = key
		}
	}
	return best
}

// holdsParams reports a position that holds $NAME$ anywhere below it.
func (n *node) holdsParams() bool {
	for _, m := range n.members {
		if m.holdsParams() {
			return true
		}
	}
	return n.params > 0
}
