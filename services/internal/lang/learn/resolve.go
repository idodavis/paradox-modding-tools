// resolve.go turns counts into what a field is: its class, the vocabularies a body draws on,
// the names hidden among its keys, and the types of value it accepts.
package learn

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
)

// witnesses works out, once the install is read, what the vocabulary says of blocks. A token
// whose usage holds <effects> or <triggers> takes a block of them (if, any_child); a token with
// no usage that the install always writes with a block takes what the tokens inside it take
// (not = { any_child = { } }). Such a token at a position witnesses that it is a body: a
// structure never writes one, however many members are named like triggers (a cost's gold).
// Likewise a position with a key of its own (no scope change) written as a number whose blocks
// hold if computes a number, and the undeclared keys there are the words of arithmetic.
func (census *census) witnesses() {
	census.effectBlocks, census.triggerBlocks, census.math, census.valued, census.keyedAt = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]int{}
	for name, effect := range census.vocab.Effects {
		census.effectBlocks[name] = strings.Contains(effect.Usage, "effects>") || strings.Contains(effect.Usage, "effect>")
	}
	for name, trigger := range census.vocab.Triggers {
		census.triggerBlocks[name] = strings.Contains(trigger.Usage, "triggers>") || strings.Contains(trigger.Usage, "trigger>")
	}
	for grew := true; grew; {
		grew = false
		for token, inside := range census.holds {
			if census.blocked[token] != census.written[token] {
				continue
			}
			for child := range inside {
				if effect, ok := census.vocab.Effects[token]; ok && effect.Usage == "" && census.effectBlocks[child] && !census.effectBlocks[token] {
					census.effectBlocks[token], grew = true, true
				}
				if trigger, ok := census.vocab.Triggers[token]; ok && trigger.Usage == "" && census.triggerBlocks[child] && !census.triggerBlocks[token] {
					census.triggerBlocks[token], grew = true, true
				}
			}
		}
	}

	// the words of arithmetic, every word the install writes as a value or after a prefix, and
	// at how many positions each key is written
	var walk func(pos *node, named, tally bool)
	walk = func(pos *node, named, tally bool) {
		for word := range pos.words {
			census.valued[word] = true
		}
		sums := false
		for key, member := range pos.members {
			sums = sums || (named && pos.numbers > 0 && member.blocks > 0 && census.effectBlocks[strings.ToLower(key)])
			if tally {
				census.keyedAt[key]++
			}
			walk(member, !census.hops(key) && !strings.HasPrefix(key, "<"), tally)
		}
		for key := range pos.members {
			if _, isToken := census.token(key); sums && !isToken && !census.hops(key) && !strings.HasPrefix(key, "<") {
				census.math[key] = true
			}
		}
	}
	for _, fo := range census.folders {
		// the entries at the top of a folder are one position, and so is each member of theirs:
		// a script value is written as a number in one entry and as a block in the next
		top, below := &node{}, map[string]*node{}
		for _, member := range fo.root.members {
			top.merge(member.shallow())
			for key, mm := range member.members {
				if below[key] == nil {
					below[key] = &node{}
				}
				below[key].merge(mm.shallow())
			}
		}
		walk(fo.root, false, true)

		// the merged positions are counted for what they hold, not for where a key stands: that
		// was counted in the folder's own tree
		walk(top, true, false)
		for key, shape := range below {
			walk(shape, !census.hops(key) && !strings.HasPrefix(key, "<"), false)
		}
		for _, shape := range fo.declared {
			walk(shape, true, true)
		}
	}
	for _, pos := range census.tokens {
		walk(pos, true, true)
	}
	census.citing = map[string][]string{}
	for prefix, names := range census.cites {
		for name := range names {
			census.valued[name] = true
			if census.vocab.Links[prefix].Prefix {
				census.citing[name] = append(census.citing[name], prefix)
			}
		}
	}
}

// keys is what the keys written directly inside one position are, by the vocabulary and by how
// often they recur.
type keys struct {
	distinct          int      // keyed members that are no position
	effects, triggers int      // distinct block-taking tokens declared only as effects, or only as triggers
	sums              bool     // it computes a number: a number holding if, or all arithmetic and branches
	once              []string // undeclared keys written in one block of several: a name is written once
	words             int      // bare words written among them
}

// keysOf counts the keys of a position.
func (census *census) keysOf(pos *node) keys {
	counted := keys{sums: true}
	branches, summed := false, false
	count := func(key string, in, blocks int) {
		low := strings.ToLower(key)
		_, effect := census.vocab.Effects[low]
		_, trigger := census.vocab.Triggers[low]
		_, modifier := census.vocab.Modifiers[low]
		counted.distinct++
		branch := effect && !trigger && blocks > 0 && census.effectBlocks[low] && !census.hops(key)
		switch {
		case branch:
			counted.effects, branches = counted.effects+1, true
		case trigger && !effect && blocks > 0 && census.triggerBlocks[low] && !census.hops(key):
			counted.triggers++
		case !effect && !trigger && !modifier && !census.hops(key) && in == 1 && pos.blocks > 1:
			counted.once = append(counted.once, key)
		}
		summed = summed || census.math[key]
		counted.sums = counted.sums && (branch || census.math[key] || census.hops(key))
	}
	for key, member := range pos.members {
		switch {
		case key == "<word>":
			counted.words += member.count
		case !strings.HasPrefix(key, "<"):
			count(key, member.in, member.blocks)
		}
	}
	for key := range pos.names {
		count(key, 1, 0)
	}
	counted.sums = (counted.sums && summed) || (pos.numbers > 0 && branches)
	slices.Sort(counted.once)
	return counted
}

// named reports a position whose keys are mostly names: written once each, and most of those
// either definitions made elsewhere, which the position cites, or names of its own: something
// the game refers to, by citing, localizing or writing it as a value, that is a key nowhere
// else. The optional members of a kind with three definitions are written once each too, but
// nothing refers to them, and a field word (id, years) is a key at many positions. Where only
// the engine reads the names (defines), the declared prefix that mostly cites them says so.
func (census *census) named(counted keys) bool {
	names, cited := 0, map[string]int{}
	for _, key := range counted.once {
		if len(census.defined[key]) > 0 || ((census.valued[key] || census.keys.Has(key)) && census.keyedAt[key] < 2) {
			names++
		}
		for _, prefix := range census.citing[key] {
			cited[prefix]++
		}
	}
	for prefix, here := range cited {
		if here*2 > len(census.cites[prefix]) {
			names = len(counted.once)
		}
	}
	return len(counted.once)*2 > counted.distinct && names*2 > len(counted.once)
}

// class is what is written at a position, and for a body the vocabularies it draws on: those
// whose block-taking tokens are written directly inside it. Arithmetic is told before effects,
// because it branches with the effects' if; a body before a value, because a trigger written as
// yes in most places is still a trigger.
func (census *census) class(pos *node) (schema.Class, schema.BodyClass) {
	if pos.classed {
		return pos.class, pos.body
	}
	counted := census.keysOf(pos)
	switch {
	case counted.triggers > 0 && counted.effects > 0 && !counted.sums:
		return schema.ClassBody, schema.BodyTrigger | schema.BodyEffect
	case counted.triggers > 0:
		return schema.ClassBody, schema.BodyTrigger
	case counted.sums:
		return schema.ClassBody, schema.BodyMath
	case counted.effects > 0:
		return schema.ClassBody, schema.BodyEffect
	case pos.blocks*2 < pos.count:
		return schema.ClassValue, 0
	case counted.distinct == 0 && counted.words > 0:
		return schema.ClassList, 0
	case counted.distinct == 0 && (pos.defines != nil || pos.keyedBy != nil):
		return schema.ClassDatabase, 0
	}
	return schema.ClassStructure, 0
}

// lift moves what stands under each declared token of a body into the census of that token,
// because the engine declares a token once and what it takes is the same wherever it is
// written. A scope change is the same body in another scope, so what stands under it is lifted
// too. The other keys written in the body are counted by its class, for the join that finds
// which kinds are macros.
func (census *census) lift(pos *node) {
	class, body := census.class(pos)
	pos.classed, pos.class, pos.body = true, class, body
	for _, key := range slices.Sorted(maps.Keys(pos.members)) {
		member := pos.members[key]
		_, isToken := census.token(key)
		switch {
		case class != schema.ClassBody:
			census.lift(member)
		case isToken:
			low := strings.ToLower(key)
			if census.tokens[low] == nil {
				census.tokens[low] = &node{}
			}
			// taken out first: an if inside an if is merged into the position it stood in
			delete(pos.members, key)
			census.tokens[low].merge(member)
			census.moved[low] = true
		case census.hops(key) || key == "<cite>":
			delete(pos.members, key)
			pos.merge(&node{members: member.members, names: member.names})
			census.lift(pos)
			return
		case !strings.HasPrefix(key, "<"):
			if census.called[body] == nil {
				census.called[body] = map[string]int{}
			}
			census.called[body][key] += member.count
			census.lift(member)
		}
	}
	if class == schema.ClassBody {
		for key, written := range pos.names {
			if _, isToken := census.token(key); !isToken {
				if census.called[body] == nil {
					census.called[body] = map[string]int{}
				}
				census.called[body][key] += written
			}
		}
	}
}

// hoist gives a kind one field for each key that can hold itself (a GUI vbox holds vboxes, a
// dynamic description holds descriptions), where read position by position each path would be
// a field tree thirty levels deep. A block-valued key on a cycle of what-holds-what is merged
// from every place into one member at the top of the shape, where any descent finds it. A key
// that holds itself nowhere stays put: one word at two positions is often two things.
func (census *census) hoist(shape *node) {
	holds := map[string]map[string]bool{}
	var walk func(pos *node)
	walk = func(pos *node) {
		for key, member := range pos.members {
			if member.blocks == 0 || strings.HasPrefix(key, "<") {
				continue
			}
			if holds[key] == nil {
				holds[key] = map[string]bool{}
			}
			for child, cm := range member.members {
				if cm.blocks > 0 && !strings.HasPrefix(child, "<") {
					holds[key][child] = true
				}
			}
			walk(member)
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
	var lift func(pos *node)
	lift = func(pos *node) {
		for _, key := range slices.Sorted(maps.Keys(pos.members)) {
			member := pos.members[key]
			lift(member)
			if cyclic[key] && member.blocks > 0 {
				if top[key] == nil {
					top[key] = &node{}
				}
				top[key].merge(member)
				delete(pos.members, key)
			}
		}
	}
	lift(shape)
	maps.Copy(shape.members, top)
}

// fold gathers a position's one-off keys into the position <name>, and says which kind they
// cite when most of them are its definitions: the cases of a switch are traits, the keys of a
// goods block are goods. A position whose names are a nested kind's has given them up already.
func (census *census) fold(pos *node) {
	counted := census.keysOf(pos)
	if !census.named(counted) || pos.defines != nil {
		return
	}
	cited := map[*draft]int{}
	for _, key := range counted.once {
		for _, definer := range census.defined[key] {
			cited[definer]++
		}
		if member := pos.members[key]; member != nil {
			if pos.members["<name>"] == nil {
				pos.members["<name>"] = &node{}
			}
			pos.members["<name>"].merge(member)
			delete(pos.members, key)
		}
	}
	for _, candidate := range slices.SortedFunc(maps.Keys(cited), func(first, second *draft) int {
		return cmp.Or(len(second.names)-len(first.names), strings.Compare(first.kind.Name, second.kind.Name))
	}) {
		if pos.keyedBy == nil && cited[candidate]*2 > len(counted.once) {
			pos.keyedBy = candidate
		}
	}
}

// field is the schema's field for a position: its class, what it accepts, and the fields below
// it. In a body the members are what is left once tokens, scope changes, macros and one-off
// keys are gone: limit under if, name under an option. folders and parent find what the game's
// own info says of the field, whose declared type comes first among the accepts.
func (census *census) field(name string, pos *node, folders []string, parent string) *schema.Field {
	field := &schema.Field{Name: name, Count: pos.count}
	field.Class, field.Body = census.class(pos)
	if name != "" && field.Class != schema.ClassBody {
		census.fold(pos)
	}
	for _, op := range slices.SortedFunc(maps.Keys(pos.ops), func(first, second string) int {
		return cmp.Or(pos.ops[second]-pos.ops[first], strings.Compare(first, second))
	}) {
		field.Ops = append(field.Ops, op)
	}
	if pos.defines != nil {
		field.Defines = pos.defines.kind.Name
	}
	if pos.keyedBy != nil {
		field.KeyedBy = pos.keyedBy.kind.Name
	}

	// the scope a body runs in: the one scope type every token written directly inside may run in
	if field.Class == schema.ClassBody {
		var held []string
		for scope, tokens := range pos.scopes {
			if tokens == pos.scoped {
				held = append(held, scope)
			}
		}
		if len(held) == 1 {
			field.BodyScope = held[0]
		}
	}

	field.Accepts = census.accepts(name, pos, field.Class)
	if declared, ok := census.info.Field(folders, parent, name).Declared(); ok {
		if at := slices.IndexFunc(field.Accepts, func(accept schema.Accept) bool { return accept.Type == declared.Type && accept.Kind == declared.Kind }); at >= 0 {
			declared.Count = field.Accepts[at].Count
		}
		field.Accepts = slices.DeleteFunc(field.Accepts, func(accept schema.Accept) bool { return accept.Type == declared.Type && accept.Kind == declared.Kind })
		field.Accepts = append([]schema.Accept{declared}, field.Accepts...)
	}

	for _, key := range slices.Sorted(maps.Keys(pos.members)) {
		member := pos.members[key]
		if key == "<word>" || (field.Class == schema.ClassBody && ((member.in == 1 && pos.blocks > 1) || census.macro[key])) {
			continue
		}
		if field.Members == nil {
			field.Members = map[string]*schema.Field{}
		}
		field.Members[key] = census.field(key, member, folders, name)
	}
	return field
}

// accepts is the types of value written at a position, most frequent first. Words are joined
// against every kind's names, the loc keys and the inventory's files. The kind that explains
// the most words is tried first, and is accepted when it defines most of the words no kind
// before it explains (one of feed, toast, popup being an animation makes none of them one); of
// two that explain the same words the one with fewer names fits closer. A number names a
// number-named kind only where the game says so: the field is a link or token whose declared
// output is that kind's scope type (holder, father), or a cite at the same field names the kind.
func (census *census) accepts(key string, at *node, class schema.Class) []schema.Accept {
	// a list's values are its words; a block tagged as a colour is a colour, and its words are
	// the colour's parts
	values, colours := at, at.tags["rgb"]+at.tags["hsv"]+at.tags["hsv360"]+at.tags["hex"]
	if words := at.members["<word>"]; words != nil && class == schema.ClassList {
		values = words
	}
	if colours > 0 && colours == at.blocks {
		return []schema.Accept{{Type: schema.TypeColor, Count: colours}}
	}
	var out []schema.Accept
	add := func(accept schema.Accept) {
		if accept.Count > 0 {
			out = append(out, accept)
		}
	}
	add(schema.Accept{Type: schema.TypeBool, Count: values.bools})
	add(schema.Accept{Type: schema.TypeDate, Count: values.dates})
	add(schema.Accept{Type: schema.TypeColor, Count: colours})

	// every word that is no literal, by what could define it
	hits, open, cited := map[string]map[string]int{}, map[string]bool{}, map[string]bool{}
	var numbers []string
	paths := 0
	for word, times := range values.words {
		name := word
		if ref, ok := jomini.Cite(word); ok && ref.Chain == "" {
			name = ref.Name
			for _, definer := range census.prefixed[ref.Prefix] {
				cited[definer.kind.Name] = true
			}
		}
		switch kind := jomini.Literal(word); {
		case kind == jomini.LitDate || word == "yes" || word == "no" || len(jomini.Interpolations(word)) > 0 || strings.HasPrefix(word, "@") || strings.HasPrefix(word, "["):
			continue
		case kind == jomini.LitNumber:
			numbers = append(numbers, word)
			continue
		}

		var by []string
		for _, definer := range census.defined[name] {
			by = append(by, definer.kind.Name)
		}
		if census.keys.Has(word) {
			by = append(by, "<loc>")
		}
		if base := strings.ToLower(word[strings.LastIndexByte(word, '/')+1:]); census.files[strings.ToLower(word)] || (strings.Contains(base, ".") && (census.files[base] || census.files[strings.TrimSuffix(base, path.Ext(base))+".*"])) {
			by = append(by, "<file>")
		}
		for _, kind := range by {
			if hits[kind] == nil {
				hits[kind] = map[string]int{}
			}
			hits[kind][word] = times
		}

		first, _, _ := strings.Cut(word, ".")
		if _, isLink := census.vocab.Links[first]; len(by) == 0 && (isLink || (strings.Contains(word, ":") && strings.Contains(word, "."))) {
			paths += times
		} else {
			open[word] = true
		}
	}

	// the kinds, best first: most words explained, then fewest names
	for _, kind := range slices.SortedFunc(maps.Keys(hits), func(first, second string) int {
		return cmp.Or(len(hits[second])-len(hits[first]), census.size(first)-census.size(second), strings.Compare(first, second))
	}) {
		fresh, total := 0, 0
		for word, times := range hits[kind] {
			total += times
			if open[word] {
				fresh++
			}
		}
		if fresh*2 <= len(open) {
			continue
		}
		for word := range hits[kind] {
			delete(open, word)
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

	// numbers: the names of a number-named kind where a cite at the same field says so, or the
	// field's own declared output does
	leads := census.vocab.Links[strings.ToLower(key)].Out
	if token, ok := census.token(key); ok && leads == "" {
		leads = token.Out
	}
	plain := values.numbers
	for _, candidate := range census.numbered {
		total := 0
		for _, number := range numbers {
			if candidate.names[number] > 0 {
				total += values.words[number]
			}
		}
		if total > 0 && (cited[candidate.kind.Name] || (leads != "" && candidate.kind.ScopeType == leads)) {
			add(schema.Accept{Type: schema.TypeRef, Kind: candidate.kind.Name, Count: total})
			plain -= total
			break
		}
	}
	add(schema.Accept{Type: schema.TypeNumber, Count: plain})

	// what nothing explains: a closed set of words when the whole set was kept and most of its
	// words are written again, else text
	names, text, again := 0, 0, 0
	var words []string
	for word := range open {
		if jomini.IsDefName(word) {
			names, words = names+values.words[word], append(words, word)
			if values.words[word] > 1 {
				again++
			}
		} else {
			text += values.words[word]
		}
	}
	if len(values.words) < maxWords && again*2 > len(words) {
		slices.Sort(words)
		add(schema.Accept{Type: schema.TypeEnum, Enum: words, Count: names})
	} else {
		text += names
	}
	add(schema.Accept{Type: schema.TypeString, Count: text})

	slices.SortStableFunc(out, func(first, second schema.Accept) int { return second.Count - first.Count })
	return out
}

// size is how many names a kind has; the loc keys and the files are larger than any kind.
func (census *census) size(kind string) int {
	if draft := census.byName[kind]; draft != nil {
		return len(draft.names)
	}
	return 1 << 30
}
