// resolve.go turns counts into what a field is: its class, the vocabularies a body draws on, the names hidden among its keys, and the types of value it accepts.
package learn

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// witnesses works out, once the install is read, what the vocabulary says of blocks. A token whose usage holds <effects> or <triggers> takes a block of them (if, every_vassal, any_child, trigger_if), and a token the dump gives no usage, which the install always writes with a block, takes what the tokens written inside it take (not = { any_child = { } }, hidden_effect = { if = { } }; martial is written with a number). Such a token written at a position is the witness that the position is a body: a structure never writes one, however many of its members are named like triggers (a cost block's gold and prestige, a character's culture and faith). Arithmetic is found the same way: a position with a key of its own (no scope change, which stands in every kind of body) written as a number whose blocks hold if computes a number, and the undeclared keys written there are the words of arithmetic (value, add, multiply).
func (c *census) witnesses() {
	c.effectBlocks, c.triggerBlocks, c.math, c.valued, c.keyedAt = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]int{}
	for name, t := range c.vocab.Effects {
		c.effectBlocks[name] = strings.Contains(t.Usage, "effects>") || strings.Contains(t.Usage, "effect>")
	}
	for name, t := range c.vocab.Triggers {
		c.triggerBlocks[name] = strings.Contains(t.Usage, "triggers>") || strings.Contains(t.Usage, "trigger>")
	}
	for grew := true; grew; {
		grew = false
		for token, inside := range c.holds {
			if c.blocked[token] != c.written[token] {
				continue
			}
			for child := range inside {
				if e, ok := c.vocab.Effects[token]; ok && e.Usage == "" && c.effectBlocks[child] && !c.effectBlocks[token] {
					c.effectBlocks[token], grew = true, true
				}
				if t, ok := c.vocab.Triggers[token]; ok && t.Usage == "" && c.triggerBlocks[child] && !c.triggerBlocks[token] {
					c.triggerBlocks[token], grew = true, true
				}
			}
		}
	}

	// the words of arithmetic, every word the install writes as a value or after a prefix, and at how many positions each key is written
	tally := true
	var walk func(n *node, named bool)
	walk = func(n *node, named bool) {
		for w := range n.words {
			c.valued[w] = true
		}
		sums := false
		for key, m := range n.members {
			sums = sums || (named && n.numbers > 0 && m.blocks > 0 && c.effectBlocks[strings.ToLower(key)])
			if tally {
				c.keyedAt[key]++
			}
			walk(m, !c.hops(key) && !strings.HasPrefix(key, "<"))
		}
		for key := range n.members {
			if _, isToken := c.token(key); sums && !isToken && !c.hops(key) && !strings.HasPrefix(key, "<") {
				c.math[key] = true
			}
		}
	}
	for _, fo := range c.folders {
		// the entries at the top of a folder are one position, and so is each member of theirs: a script value is written as a number in one entry and as a block in the next
		top, below := &node{}, map[string]*node{}
		for _, m := range fo.root.members {
			top.merge(m.shallow())
			for key, mm := range m.members {
				if below[key] == nil {
					below[key] = &node{}
				}
				below[key].merge(mm.shallow())
			}
		}
		walk(fo.root, false)
		tally = false
		walk(top, true)
		for key, m := range below {
			walk(m, !c.hops(key) && !strings.HasPrefix(key, "<"))
		}
		tally = true
		for _, shape := range fo.declared {
			walk(shape, true)
		}
	}
	for _, n := range c.tokens {
		walk(n, true)
	}
	c.citing = map[string][]string{}
	for prefix, names := range c.cites {
		for name := range names {
			c.valued[name] = true
			if c.vocab.Links[prefix].Prefix {
				c.citing[name] = append(c.citing[name], prefix)
			}
		}
	}
}

// keys is what the keys written directly inside one position are, by the vocabulary and by how often they recur.
type keys struct {
	distinct          int      // keyed members that are no position
	effects, triggers int      // distinct tokens written with a block that only the effects, or only the triggers, declare to hold their own
	sums              bool     // it computes a number: written as a number and holding if, or every key a word of arithmetic or a branch around one
	once              []string // keys the vocabulary does not declare, written in one block of several: a name is written once
	words             int      // bare words written among them
}

// keysOf counts the keys of a position.
func (c *census) keysOf(n *node) keys {
	k := keys{sums: true}
	branches, summed := false, false
	count := func(key string, in, blocks int) {
		low := strings.ToLower(key)
		_, effect := c.vocab.Effects[low]
		_, trigger := c.vocab.Triggers[low]
		_, modifier := c.vocab.Modifiers[low]
		k.distinct++
		branch := effect && !trigger && blocks > 0 && c.effectBlocks[low] && !c.hops(key)
		switch {
		case branch:
			k.effects, branches = k.effects+1, true
		case trigger && !effect && blocks > 0 && c.triggerBlocks[low] && !c.hops(key):
			k.triggers++
		case !effect && !trigger && !modifier && !c.hops(key) && in == 1 && n.blocks > 1:
			k.once = append(k.once, key)
		}
		summed = summed || c.math[key]
		k.sums = k.sums && (branch || c.math[key] || c.hops(key))
	}
	for key, m := range n.members {
		switch {
		case key == "<word>":
			k.words += m.count
		case !strings.HasPrefix(key, "<"):
			count(key, m.in, m.blocks)
		}
	}
	for key := range n.names {
		count(key, 1, 0)
	}
	k.sums = (k.sums && summed) || (n.numbers > 0 && branches)
	slices.Sort(k.once)
	return k
}

// named reports a position whose keys are mostly names: written once each, and most of those either definitions made elsewhere, which the position cites, or names of its own: something the game refers to, by citing, localizing or writing it as a value, that is a key nowhere else. The optional members of a kind with three definitions are written once each too, but nothing refers to them, and a field word (id, years, margin) is a key at hundreds of positions. Where only the engine reads the names (defines), the declared prefix that mostly cites names written here says so.
func (c *census) named(k keys) bool {
	names, cited := 0, map[string]int{}
	for _, key := range k.once {
		if len(c.defined[key]) > 0 || ((c.valued[key] || c.keys.Has(key)) && c.keyedAt[key] < 2) {
			names++
		}
		for _, prefix := range c.citing[key] {
			cited[prefix]++
		}
	}
	for prefix, here := range cited {
		if here*2 > len(c.cites[prefix]) {
			names = len(k.once)
		}
	}
	return len(k.once)*2 > k.distinct && names*2 > len(k.once)
}

// class is what is written at a position, and for a body the vocabularies it draws on: those whose block-taking tokens are written directly inside it. Arithmetic is told before effects, because it branches with the effects' if; a body before a value, because a trigger written as yes in most places is still a trigger.
func (c *census) class(n *node) (schema.Class, schema.BodyClass) {
	if n.classed {
		return n.class, n.body
	}
	k := c.keysOf(n)
	switch {
	case k.triggers > 0 && k.effects > 0 && !k.sums:
		return schema.ClassBody, schema.BodyTrigger | schema.BodyEffect
	case k.triggers > 0:
		return schema.ClassBody, schema.BodyTrigger
	case k.sums:
		return schema.ClassBody, schema.BodyMath
	case k.effects > 0:
		return schema.ClassBody, schema.BodyEffect
	case n.blocks*2 < n.count:
		return schema.ClassValue, 0
	case k.distinct == 0 && k.words > 0:
		return schema.ClassList, 0
	case k.distinct == 0 && (n.defines != nil || n.keyedBy != nil):
		return schema.ClassDatabase, 0
	}
	return schema.ClassStructure, 0
}

// lift moves what stands under each declared token of a body into the census of that token, because the engine declares a token once and what it takes is the same wherever it is written. A scope change is the same body in another scope, so what stands under it is lifted too. The other keys written in the body are counted by its class, for the join that finds which kinds are macros.
func (c *census) lift(n *node) {
	class, body := c.class(n)
	n.classed, n.class, n.body = true, class, body
	for _, key := range slices.Sorted(maps.Keys(n.members)) {
		m := n.members[key]
		_, isToken := c.token(key)
		switch {
		case class != schema.ClassBody:
			c.lift(m)
		case isToken:
			low := strings.ToLower(key)
			if c.tokens[low] == nil {
				c.tokens[low] = &node{}
			}
			// taken out first: an if inside an if is merged into the position it stood in
			delete(n.members, key)
			c.tokens[low].merge(m)
			c.moved = true
		case c.hops(key) || key == "<cite>":
			delete(n.members, key)
			c.moved = true
			n.merge(&node{members: m.members, names: m.names})
			c.lift(n)
			return
		case !strings.HasPrefix(key, "<"):
			if c.called[body] == nil {
				c.called[body] = map[string]int{}
			}
			c.called[body][key] += m.count
			c.lift(m)
		}
	}
	if class == schema.ClassBody {
		for key, written := range n.names {
			if _, isToken := c.token(key); !isToken {
				if c.called[body] == nil {
					c.called[body] = map[string]int{}
				}
				c.called[body][key] += written
			}
		}
	}
}

// hoist gives a kind one field for each key that can hold itself. A GUI widget holds widgets that hold widgets (vbox, item, vbox, dynamicgridbox ...), and a dynamic description holds descriptions; read position by position each path would be a field tree of its own, thirty levels deep and all alike. So a block-valued key that lies on a cycle of what-holds-what is merged from every place it stands into one member at the top of the kind's shape, and removed everywhere else; a descent that meets the key below a parent that does not list it finds it there. A key that holds itself nowhere stays where it is, because the same word at two positions is often two things (desc).
func (c *census) hoist(shape *node) {
	holds := map[string]map[string]bool{}
	var walk func(n *node)
	walk = func(n *node) {
		for key, m := range n.members {
			if m.blocks == 0 || strings.HasPrefix(key, "<") {
				continue
			}
			if holds[key] == nil {
				holds[key] = map[string]bool{}
			}
			for child, cm := range m.members {
				if cm.blocks > 0 && !strings.HasPrefix(child, "<") {
					holds[key][child] = true
				}
			}
			walk(m)
		}
	}
	walk(shape)

	// a key is on a cycle when it can be reached from itself
	cyclic := map[string]bool{}
	for start := range holds {
		seen, queue := map[string]bool{}, slices.Collect(maps.Keys(holds[start]))
		for len(queue) > 0 && !cyclic[start] {
			key := queue[0]
			queue = queue[1:]
			if key == start {
				cyclic[start] = true
			}
			if !seen[key] {
				seen[key] = true
				queue = append(queue, slices.Collect(maps.Keys(holds[key]))...)
			}
		}
	}
	if len(cyclic) == 0 {
		return
	}

	// every occurrence leaves its parent and joins the one at the top
	top := map[string]*node{}
	var lift func(n *node, isTop bool)
	lift = func(n *node, isTop bool) {
		for _, key := range slices.Sorted(maps.Keys(n.members)) {
			m := n.members[key]
			lift(m, false)
			if cyclic[key] && m.blocks > 0 {
				if top[key] == nil {
					top[key] = &node{}
				}
				top[key].merge(m)
				delete(n.members, key)
			}
		}
	}
	lift(shape, true)
	for key, m := range top {
		shape.members[key] = m
	}
}

// fold gathers a position's one-off keys into the position <name> (§5.3), and says which kind they cite when most of them are its definitions: the cases of a switch are traits, the keys of a goods block are goods. A position whose names are a nested kind's has given them up already.
func (c *census) fold(n *node) {
	k := c.keysOf(n)
	if !c.named(k) || n.defines != nil {
		return
	}
	cited := map[*draft]int{}
	for _, key := range k.once {
		for _, d := range c.defined[key] {
			cited[d]++
		}
		if m := n.members[key]; m != nil {
			if n.members["<name>"] == nil {
				n.members["<name>"] = &node{}
			}
			n.members["<name>"].merge(m)
			delete(n.members, key)
		}
	}
	for _, d := range slices.SortedFunc(maps.Keys(cited), func(a, b *draft) int {
		return cmp.Or(len(b.names)-len(a.names), strings.Compare(a.kind.Name, b.kind.Name))
	}) {
		if n.keyedBy == nil && cited[d]*2 > len(k.once) {
			n.keyedBy = d
		}
	}
}

// field is the schema's field for a position: its class, what it accepts, and the fields below it. In a body the members are what is left once tokens, scope changes, macros and one-off keys are gone: limit under if, name under an option. folders and parent find what the game's own info says of the field, whose declared type comes first among the accepts.
func (c *census) field(name string, n *node, folders []string, parent string) *schema.Field {
	f := &schema.Field{Name: name, Count: n.count}
	f.Class, f.Body = c.class(n)
	if name != "" && f.Class != schema.ClassBody {
		c.fold(n)
	}
	for _, op := range slices.SortedFunc(maps.Keys(n.ops), func(a, b string) int { return cmp.Or(n.ops[b]-n.ops[a], strings.Compare(a, b)) }) {
		f.Ops = append(f.Ops, op)
	}
	if n.defines != nil {
		f.Defines = n.defines.kind.Name
	}
	if n.keyedBy != nil {
		f.KeyedBy = n.keyedBy.kind.Name
	}

	// the scope a body runs in: the one scope type every token written directly inside may run in
	if f.Class == schema.ClassBody {
		var held []string
		for scope, tokens := range n.scopes {
			if tokens == n.scoped {
				held = append(held, scope)
			}
		}
		if len(held) == 1 {
			f.BodyScope = held[0]
		}
	}

	f.Accepts = c.accepts(name, n, f.Class)
	if declared, ok := c.info.Field(folders, parent, name).Declared(); ok {
		if at := slices.IndexFunc(f.Accepts, func(a schema.Accept) bool { return a.Type == declared.Type && a.Kind == declared.Kind }); at >= 0 {
			declared.Count = f.Accepts[at].Count
		}
		f.Accepts = slices.DeleteFunc(f.Accepts, func(a schema.Accept) bool { return a.Type == declared.Type && a.Kind == declared.Kind })
		f.Accepts = append([]schema.Accept{declared}, f.Accepts...)
	}

	for _, key := range slices.Sorted(maps.Keys(n.members)) {
		m := n.members[key]
		if key == "<word>" || (f.Class == schema.ClassBody && ((m.in == 1 && n.blocks > 1) || c.macro[key])) {
			continue
		}
		if f.Members == nil {
			f.Members = map[string]*schema.Field{}
		}
		f.Members[key] = c.field(key, m, folders, name)
	}
	return f
}

// accepts is the types of value written at a position, most frequent first. A word is joined against every kind's names, with the loc keys and the files of the inventory as two more. The kind that explains the most words is tried first, and a kind is accepted when it defines most of the words no kind before it explains: a message's display is feed, toast or popup, and one of those being an animation's name makes none of them animations. Of two kinds that explain the same words the one with fewer names is the closer fit (goods, not the text icons named after them). A number is the name of a number-named kind only where the game says so: the field is a link or a token whose declared output is that kind's scope type (holder, father), or a cite at the same field names the kind.
func (c *census) accepts(key string, at *node, class schema.Class) []schema.Accept {
	// a list's values are its words; a block tagged as a colour is a colour, and its words are the colour's parts
	n, colours := at, at.tags["rgb"]+at.tags["hsv"]+at.tags["hsv360"]+at.tags["hex"]
	if words := at.members["<word>"]; words != nil && class == schema.ClassList {
		n = words
	}
	if colours > 0 && colours == at.blocks {
		return []schema.Accept{{Type: schema.TypeColor, Count: colours}}
	}
	var out []schema.Accept
	add := func(a schema.Accept) {
		if a.Count > 0 {
			out = append(out, a)
		}
	}
	add(schema.Accept{Type: schema.TypeBool, Count: n.bools})
	add(schema.Accept{Type: schema.TypeDate, Count: n.dates})
	add(schema.Accept{Type: schema.TypeColor, Count: colours})

	// every word that is no literal, by what could define it
	hits, open, cited := map[string]map[string]int{}, map[string]bool{}, map[string]bool{}
	var numbers []string
	paths := 0
	for w, times := range n.words {
		name := w
		if ref, ok := jomini.Cite(w); ok && ref.Chain == "" {
			name = ref.Name
			for _, k := range c.prefixed[ref.Prefix] {
				cited[k.kind.Name] = true
			}
		}
		switch kind := jomini.Literal(w); {
		case kind == jomini.LitDate || w == "yes" || w == "no" || len(jomini.Interpolations(w)) > 0 || strings.HasPrefix(w, "@") || strings.HasPrefix(w, "["):
			continue
		case kind == jomini.LitNumber:
			numbers = append(numbers, w)
			continue
		}

		var by []string
		for _, d := range c.defined[name] {
			by = append(by, d.kind.Name)
		}
		if c.keys.Has(w) {
			by = append(by, "<loc>")
		}
		if base := strings.ToLower(w[strings.LastIndexByte(w, '/')+1:]); c.files[strings.ToLower(w)] || (strings.Contains(base, ".") && (c.files[base] || c.files[strings.TrimSuffix(base, path.Ext(base))+".*"])) {
			by = append(by, "<file>")
		}
		for _, kind := range by {
			if hits[kind] == nil {
				hits[kind] = map[string]int{}
			}
			hits[kind][w] = times
		}

		first, _, _ := strings.Cut(w, ".")
		if _, isLink := c.vocab.Links[first]; len(by) == 0 && (isLink || (strings.Contains(w, ":") && strings.Contains(w, "."))) {
			paths += times
		} else {
			open[w] = true
		}
	}

	// the kinds, best first: most words explained, then fewest names
	for _, kind := range slices.SortedFunc(maps.Keys(hits), func(a, b string) int {
		return cmp.Or(len(hits[b])-len(hits[a]), c.size(a)-c.size(b), strings.Compare(a, b))
	}) {
		fresh, total := 0, 0
		for w, times := range hits[kind] {
			total += times
			if open[w] {
				fresh++
			}
		}
		if fresh*2 <= len(open) {
			continue
		}
		for w := range hits[kind] {
			delete(open, w)
		}
		switch kind {
		case "<loc>":
			add(schema.Accept{Type: schema.TypeLoc, Count: total})
		case "<file>":
			add(schema.Accept{Type: schema.TypeFile, Count: total})
		default:
			add(schema.Accept{Type: schema.TypeRef, Kind: kind, Count: total})
		}
	}
	add(schema.Accept{Type: schema.TypeScopePath, Count: paths})

	// numbers: the names of a number-named kind where a cite at the same field says so, or the field's own declared output does
	leads := c.vocab.Links[strings.ToLower(key)].Out
	if token, ok := c.token(key); ok && leads == "" {
		leads = token.Out
	}
	plain := n.numbers
	for _, d := range c.numbered {
		total := 0
		for _, w := range numbers {
			if d.names[w] > 0 {
				total += n.words[w]
			}
		}
		if total > 0 && (cited[d.kind.Name] || (leads != "" && d.kind.ScopeType == leads)) {
			add(schema.Accept{Type: schema.TypeRef, Kind: d.kind.Name, Count: total})
			plain -= total
			break
		}
	}
	add(schema.Accept{Type: schema.TypeNumber, Count: plain})

	// what nothing explains: a closed set of words when the whole set was kept and most of its words are written again, else text
	names, text, again := 0, 0, 0
	var words []string
	for w := range open {
		if jomini.IsDefName(w) {
			names, words = names+n.words[w], append(words, w)
			if n.words[w] > 1 {
				again++
			}
		} else {
			text += n.words[w]
		}
	}
	if len(n.words) < maxWords && again*2 > len(words) {
		slices.Sort(words)
		if kind := c.localized(words); kind != "" {
			add(schema.Accept{Type: schema.TypeRef, Kind: kind, Count: names})
		} else {
			add(schema.Accept{Type: schema.TypeEnum, Enum: words, Count: names})
		}
	} else {
		text += names
	}
	add(schema.Accept{Type: schema.TypeString, Count: text})

	slices.SortStableFunc(out, func(a, b schema.Accept) int { return b.Count - a.Count })
	return out
}

// localized is the kind a closed set of words belongs to where no file defines them and the loc does: every word has a key under one template with a stem of its own (game_rule_category_{id}), and the words are most of the keys the template matches, so they are the definitions of a kind named by that stem, whose definitions are the loc keys that match (§5.8). Three words with a key under war_{id} are not what war_ names: hundreds of keys start so. The kind is made on first sight.
func (c *census) localized(words []string) string {
	for _, t := range loc.Learn(words, nil, c.keys) {
		stem := strings.Trim(strings.ReplaceAll(string(t.Template), "{id}", ""), "_")
		if t.Coverage == 1 && stem != "" && c.byName[stem] == nil && len(words)*2 > c.keys.Count(t.Template) {
			c.locKinds[stem] = &schema.Kind{Name: stem, Format: source.Loc, Naming: schema.Naming{Form: "loc_key"}, LocTemplates: []loc.Convention{t}}
			return stem
		}
	}
	return ""
}

// size is how many names a kind has; the loc keys and the files are larger than any kind.
func (c *census) size(kind string) int {
	if d := c.byName[kind]; d != nil {
		return len(d.names)
	}
	return 1 << 30
}
