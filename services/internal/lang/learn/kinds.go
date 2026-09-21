// kinds.go turns the census of folders into kinds: which top-level keys are names and which are words of the file, how each kind names its definitions, which folders are one database, which kinds are nested in others or are about others, and what each is called (the plan's §5.2 and §5.5).
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
)

// draft is a kind being worked out: what it will be, the names of its definitions, and the census of what one definition holds.
type draft struct {
	kind    *schema.Kind
	names   map[string]int
	shape   *node
	parent  *draft // a nested kind: the kind whose definitions hold this one's
	subject *draft // a subject kind: the kind its names are definitions of
}

// merge adds the counts of one position into another, members and all.
func (n *node) merge(o *node) {
	// a position already taken in by an earlier merge is the same position, not more of it
	if n == o {
		return
	}
	n.count, n.in, n.blocks, n.quoted = n.count+o.count, n.in+o.in, n.blocks+o.blocks, n.quoted+o.quoted
	n.numbers, n.dates, n.bools, n.params, n.scoped = n.numbers+o.numbers, n.dates+o.dates, n.bools+o.bools, n.params+o.params, n.scoped+o.scoped
	for _, pair := range []struct{ to, from *map[string]int }{{&n.ops, &o.ops}, {&n.tags, &o.tags}, {&n.names, &o.names}, {&n.scopes, &o.scopes}} {
		for k, v := range *pair.from {
			if *pair.to == nil {
				*pair.to = map[string]int{}
			}
			(*pair.to)[k] += v
		}
	}
	// where the words may not all fit they are taken in order, so two scans of one install keep the same ones
	words := slices.Collect(maps.Keys(o.words))
	if len(n.words)+len(words) > maxWords {
		slices.Sort(words)
	}
	for _, w := range words {
		if n.words == nil {
			n.words = map[string]int{}
		}
		if _, known := n.words[w]; known || len(n.words) < maxWords || len(n.words)*2 > n.count {
			n.words[w] += o.words[w]
		}
	}
	for key, child := range o.members {
		if n.members == nil {
			n.members = map[string]*node{}
		}
		if mine := n.members[key]; mine != nil {
			mine.merge(child)
		} else {
			n.members[key] = child
		}
	}
}

// shallow is a position's members without what is under them, which is all the comparison of two shapes reads.
func (n *node) shallow() *node {
	out := &node{count: n.count, in: n.in, blocks: n.blocks, numbers: n.numbers, members: map[string]*node{}}
	for key, m := range n.members {
		out.members[key] = &node{count: m.count, in: m.in, blocks: m.blocks}
	}
	return out
}

// join adds one draft's definitions and folders to another's.
func (d *draft) join(o *draft) {
	d.shape.merge(o.shape)
	for name, n := range o.names {
		d.names[name] += n
	}
	for _, f := range o.kind.Folders {
		if !slices.Contains(d.kind.Folders, f) {
			d.kind.Folders = append(d.kind.Folders, f)
		}
	}
}

// again reports a top-level key written twice in one file, or in most of its folder's files. A name is written once, in one file.
func again(m *node, files int) bool {
	return m.count > m.in || (files > 1 && m.in*2 > files)
}

// isWord reports a top-level key that is a word of the file format in this folder and no name: one whose block here holds the member that names such blocks all over the install (a pdxmesh with its name, written once in a leaf folder; war is a name in three kinds, whose blocks hold no name), one written again in most of the folders that write it, one in most of this folder's files (namespace), or one written twice in a file where most of the folder's keys are: war is written again in history/wars, whose only key it is, while a trait defined twice in one file by mistake is still a name.
func (c *census) isWord(fo *folder, key string, m *node) bool {
	return c.word[key] || m.members[c.namedBy[key]] != nil || (fo.files > 1 && m.in*2 > fo.files) || (m.count > m.in && fo.wordy)
}

// words finds the words of the file formats: the top-level keys written again in most of the folders that write them, and, for any key written again in some folder of words (a name is not, even one defined twice by mistake among names written once), the member its blocks are named by: the member most of those blocks hold, written as a value, whose values are mostly their own and mostly new names: no number, file, call or sentence, and no name defined at the top of some file, which would be a reference (a coat of arms written twice differs in its color2, and yellow is a named color). Of several the one most blocks hold, then the one with the most values of its own (a war has a name and a casus belli, and only the name never repeats). It is counted over the whole install, because in one leaf folder pdxmesh is written once, which shows nothing.
func (c *census) words() {
	folders, repeated, blocks := map[string]int{}, map[string]int{}, map[string]int{}
	for _, fo := range c.folders {
		keys, more := 0, 0
		for key, m := range fo.root.members {
			if !strings.HasPrefix(key, "<") {
				keys++
				if again(m, fo.files) {
					more++
				}
			}
		}
		fo.wordy = more*2 > keys
		for key, m := range fo.root.members {
			if !strings.HasPrefix(key, "<") {
				folders[key], blocks[key] = folders[key]+1, blocks[key]+m.blocks
				if again(m, fo.files) && fo.wordy {
					repeated[key]++
				}
			}
		}
	}
	c.word, c.namedBy = map[string]bool{}, map[string]string{}
	for key, n := range folders {
		c.word[key] = n > 1 && repeated[key]*2 > n
	}

	// the values of each member directly inside the blocks of a key that opens several
	held, values, names := map[[2]string]int{}, map[[2]string]int{}, map[[2]string]map[string]bool{}
	for _, fo := range c.folders {
		for key, m := range fo.root.members {
			if strings.HasPrefix(key, "<") || repeated[key] == 0 {
				continue
			}
			for name, v := range m.members {
				at := [2]string{key, name}
				if names[at] == nil {
					names[at] = map[string]bool{}
				}
				held[at], values[at] = held[at]+v.in, values[at]+v.count-v.blocks
				for w := range v.words {
					names[at][w] = true
				}
			}
		}
	}
	for _, at := range slices.SortedFunc(maps.Keys(held), func(a, b [2]string) int { return strings.Compare(a[0]+" "+a[1], b[0]+" "+b[1]) }) {
		named := 0
		for w := range names[at] {
			if jomini.IsDefName(w) && jomini.Literal(w) != jomini.LitNumber && !c.files[strings.ToLower(w)] && folders[w] == 0 {
				named++
			}
		}
		by := [2]string{at[0], c.namedBy[at[0]]}
		better := held[at] > held[by] || (held[at] == held[by] && len(names[at]) > len(names[by]))
		if !strings.HasPrefix(at[1], "<") && held[at]*2 > blocks[at[0]] && values[at]*2 > held[at] && len(names[at])*2 > values[at] && named*2 > len(names[at]) && better {
			c.namedBy[at[0]] = at[1]
		}
	}
}

// drafts is the kinds one folder holds by itself, before folders are joined: one for the names at its top level, one for each word that names its blocks by a member, one for each declaration keyword, and one for the files themselves where nothing in them is a name.
func (c *census) drafts(fo *folder) []*draft {
	var out []*draft
	base := path.Base(fo.rel)
	add := func(name string, naming schema.Naming, names map[string]int, shape *node) {
		out = append(out, &draft{kind: &schema.Kind{Name: name, Format: fo.format, Folders: []string{fo.rel}, Naming: naming}, names: names, shape: shape})
	}

	// the names at the top level, and the words among them
	names, shape := maps.Clone(fo.numbered), &node{}
	named, worded, events := 0, 0, 0
	var plain []string
	for _, key := range slices.Sorted(maps.Keys(fo.root.members)) {
		m := fo.root.members[key]
		switch {
		case key == "<name>" || key == "<number>":
			shape.merge(m)
			named += m.count
		case strings.HasPrefix(key, "<"):
			worded += m.count
		case c.isWord(fo, key, m):
			worded += m.count
			if by := c.namedBy[key]; by != "" && m.members[by] != nil {
				add(base+"."+key, schema.Naming{Form: "member", Keyword: key, Member: by}, maps.Clone(m.members[by].words), m)
			}
		default:
			plain, named = append(plain, key), named+m.count
		}
	}
	for key, n := range fo.root.names {
		names[key] += n
	}

	// rule 5: files side by side are one kind where they are alike; a file unlike the rest is a kind of its own, named after it
	groups, files := c.sideBySide(fo, plain)
	for i, group := range groups {
		held, odd := names, shape
		if i > 0 && len(group) > 0 {
			held, odd = map[string]int{}, &node{}
			file := strings.TrimSuffix(files[i], path.Ext(files[i]))
			out = append(out, &draft{kind: &schema.Kind{Name: base + "." + file, Format: fo.format, Folders: []string{fo.rel + "/" + files[i]}, Naming: schema.Naming{Form: "key"}}, names: held, shape: odd})
		}
		for _, key := range group {
			odd.merge(fo.root.members[key])
			held[key] += fo.root.members[key].count
		}
	}
	for key := range names {
		if jomini.IsEventID(key) {
			events++
		}
	}

	// the names are a kind when they are most of what the files hold; a few stray words among a folder of widgets are not
	switch {
	case named <= worded || len(names) == 0:
	case events*2 > len(names):
		add(base, schema.Naming{Form: "event_id"}, names, shape)
	case len(fo.numbered) == len(names):
		add(base, schema.Naming{Form: "number"}, names, shape)
	default:
		add(base, schema.Naming{Form: "key"}, names, shape)
	}
	for _, keyword := range slices.Sorted(maps.Keys(fo.keywords)) {
		add(base+"."+keyword, schema.Naming{Form: "declaration", Keyword: keyword}, fo.keywords[keyword], fo.declared[keyword])
	}

	// nothing in the files is a name: the files themselves may be (history/cultures/afghan.txt), which the subject join decides
	if len(out) == 0 && len(fo.root.members) > 0 {
		files := map[string]int{}
		for _, b := range fo.bases {
			files[b]++
		}
		add(fo.rel, schema.Naming{Form: "none"}, files, fo.root)
	}
	return out
}

// sideBySide sorts a folder's names by the files that hold them (§5.2 rule 5): first the names of the files that belong together, then those of each file that is a kind of its own, with that file's name. Rule 2's test is too fine for one file: a small file of scripted effects may write no if, and a file of laws members no other law has (measured: it split 76 CK3 kinds by file). What differs between map_data's default.map and its region files is starker: they share nothing. So a file belongs when a definition in it writes some key another file's definitions write, or declared tokens of a vocabulary another file's write; where its definitions write no key at all, when they are empty blocks, which say nothing, or another file's are written without keys too (plain script values beside formulas); and where the folder's keys are mostly names written once each (define groups, the laws of a law group), one that shares none holds more names.
func (c *census) sideBySide(fo *folder, keys []string) (groups [][]string, files []string) {
	byFile, probes, writers, whole := map[string][]string{}, map[string]*node{}, map[string]int{}, &node{}
	for _, key := range keys {
		byFile[fo.fileOf[key]] = append(byFile[fo.fileOf[key]], key)
	}
	for file, held := range byFile {
		probes[file] = &node{}
		for _, key := range held {
			probes[file].merge(fo.root.members[key].shallow())
		}
		for _, word := range c.shared(probes[file]) {
			writers[word]++
		}
		whole.merge(probes[file].shallow())
	}
	k := c.keysOf(whole)

	groups, files = [][]string{nil}, []string{""}
	for _, file := range slices.Sorted(maps.Keys(byFile)) {
		probe := probes[file]
		belongs := len(byFile) == 1 || len(k.once)*2 > k.distinct
		if len(probe.members) == 0 {
			belongs = belongs || probe.blocks == probe.count || whole.count-whole.blocks > probe.count-probe.blocks
		}
		for _, word := range c.shared(probe) {
			belongs = belongs || writers[word] > 1
		}
		if belongs {
			groups[0], files[0] = append(groups[0], byFile[file]...), cmp.Or(files[0], file)
		} else {
			groups, files = append(groups, byFile[file]), append(files, file)
		}
	}

	// the folder's kind is the group that holds the most names: traits beside two lookup tables, one file of animations beside a few settings
	most := 0
	for i := range groups {
		if len(groups[i]) > len(groups[most]) {
			most = i
		}
	}
	groups[0], groups[most], files[0], files[most] = groups[most], groups[0], files[most], files[0]
	return groups, files
}

// shared is what a file's definitions write that another file's may write too: their keys, the vocabulary itself where a key is a declared effect or trigger (one file adds to the treasury and another only branches, and both write effects), and arithmetic, which one file writes as plain numbers and another as formulas.
func (c *census) shared(probe *node) []string {
	var out []string
	if (len(probe.members) == 0 && probe.numbers > 0) || c.keysOf(probe).sums {
		out = append(out, "<sum>")
	}
	for key := range probe.members {
		low := strings.ToLower(key)
		if _, ok := c.vocab.Effects[low]; ok {
			out = append(out, "<effect>")
		}
		if _, ok := c.vocab.Triggers[low]; ok {
			out = append(out, "<trigger>")
		}
		out = append(out, key)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// alike reports two shapes that are one database in two places: what a definition holds is of one class, and, where that is a structure, most of the smaller one's members are the larger one's.
func (c *census) alike(a, b *node) bool {
	classA, bodyA := c.class(a)
	classB, bodyB := c.class(b)
	if classA != classB || bodyA != bodyB {
		return false
	}

	// two shapes whose keys are mostly names written once hold databases, whatever the names are: the groups of common/defines and of common/defines/ai share no entry
	ka, kb := c.keysOf(a), c.keysOf(b)
	if classA != schema.ClassStructure || (c.named(ka) && c.named(kb)) {
		return true
	}

	small, large := a.members, b.members
	if len(small) > len(large) {
		small, large = large, small
	}
	shared, total := 0, 0
	for key := range small {
		if !strings.HasPrefix(key, "<") {
			total++
			if large[key] != nil {
				shared++
			}
		}
	}
	return shared*2 >= total
}

// kinds joins the folders' drafts into kinds, then finds the kinds nested in them and the kinds that are about others, and settles every name.
func (c *census) kinds() []*draft {
	c.words()
	perFolder := map[string][]*draft{}
	for key, fo := range c.folders {
		perFolder[key] = c.drafts(fo)
	}

	var all []*draft
	byFolder, byWord := map[string][]*draft{}, map[string]*draft{}
	for _, key := range slices.Sorted(maps.Keys(c.folders)) {
		for _, d := range perFolder[key] {
			// rule 3: a kind told by its opening word is one kind over its tree, whose top is the shallowest folder above that writes such a word itself: gfx/models, gui
			if d.kind.Naming.Keyword != "" {
				top := key
				for up := path.Dir(key); up != "."; up = path.Dir(up) {
					if slices.ContainsFunc(perFolder[up], func(o *draft) bool { return o.kind.Naming.Keyword != "" }) {
						top = up
					}
				}
				at := top + " " + d.kind.Naming.Form + " " + d.kind.Naming.Keyword
				if byWord[at] == nil {
					d.kind.Name = path.Base(c.folders[top].rel) + "." + d.kind.Naming.Keyword
					byWord[at], all = d, append(all, d)
				} else {
					byWord[at].join(d)
				}
				continue
			}

			// rule 2: a subfolder joins the kind of its nearest ancestor that holds files, when the two are alike
			up := path.Dir(key)
			for up != "." && c.folders[up] == nil {
				up = path.Dir(up)
			}
			host := slices.IndexFunc(byFolder[up], func(h *draft) bool {
				return h.kind.Naming == d.kind.Naming && h.kind.Format == d.kind.Format && c.alike(h.shape, d.shape)
			})
			if host >= 0 {
				byFolder[up][host].join(d)
				byFolder[key] = append(byFolder[key], byFolder[up][host])
				continue
			}
			byFolder[key], all = append(byFolder[key], d), append(all, d)
		}
	}

	// a keyword that is a kind's name in the singular declares that kind inline where the two are alike: scripted_trigger x = { } in an events file is a trigger body like the folder's, and a GUI type is nothing like the entries of a folder that happens to be called types. Any other keyword is a word of the format when it is written again: a licence text parsed as script declares one name
	keyed := map[string]*draft{}
	for _, d := range all {
		if d.kind.Naming.Form == "key" {
			keyed[d.kind.Name] = d
		}
	}
	all = slices.DeleteFunc(all, func(d *draft) bool {
		if d.kind.Naming.Form != "declaration" {
			return false
		}
		if host := keyed[d.kind.Naming.Keyword+"s"]; host != nil && (c.alike(host.shape, d.shape) || c.speaks(host.shape, d.shape)) {
			folders := host.kind.Folders
			host.join(d)
			host.kind.Folders, host.kind.Naming.Keyword = folders, d.kind.Naming.Keyword
			return true
		}
		return len(d.names) < 2
	})

	all = append(all, c.extra...)
	c.index(all)
	found := map[string]*draft{}
	for _, d := range slices.Clone(all) {
		all = append(all, c.nested(d, d.shape, "", found)...)
	}
	c.subjects(all)
	return all
}

// speaks reports a small shape written wholly in the vocabulary of a body: every key in it is a token of the body's classes or a scope change. One inline scripted trigger that holds is_adult = yes writes no block-taking token, and is a trigger body all the same.
func (c *census) speaks(body, small *node) bool {
	_, classes := c.class(body)
	for key := range small.members {
		low := strings.ToLower(key)
		_, effect := c.vocab.Effects[low]
		_, trigger := c.vocab.Triggers[low]
		if !(effect && classes&schema.BodyEffect != 0) && !(trigger && classes&schema.BodyTrigger != 0) && !c.hops(key) {
			return false
		}
	}
	return classes != 0 && len(small.members) > 0
}

// nested finds the kinds written inside a kind's definitions: a position that is no body and whose keys are mostly names written once each (faiths inside a religion, settings inside a game rule, the entries of a define group). Where most of those names are definitions made elsewhere the position cites them and defines nothing (goods = { wheat = 0.5 }), and is keyed by the kind that defines the most of them. Else the names leave the position's members and the position carries the kind it defines. A nested kind with no group word whose definitions hold the members the parent's hold is the parent nested in itself (a landed title inside a landed title), and its names are the parent's. Nothing is looked for inside a body: what stands under a token is that token's, wherever it is written.
func (c *census) nested(parent *draft, at *node, group string, found map[string]*draft) []*draft {
	class, _ := c.class(at)
	if class == schema.ClassBody {
		return nil
	}

	// a kind nested in itself brings its own nested names with it, so the position is read again until it holds no more
	var out []*draft
	for k := c.keysOf(at); c.named(k); k = c.keysOf(at) {
		names, shape, cited := map[string]int{}, &node{}, map[*draft]int{}
		for _, key := range k.once {
			if m := at.members[key]; m != nil {
				names[key] += m.count
			}
			for _, o := range c.defined[key] {
				cited[o]++
			}
		}
		for key, n := range at.names {
			names[key] += n
		}

		// the parent nested in itself is looked for first: a landed title's names are also title history's, which cites them
		for _, key := range k.once {
			if m := at.members[key]; m != nil {
				shape.merge(m.shallow())
			}
		}
		self := group == "" && c.sameMembers(shape, parent.shape) && c.citedTogether(parent.names, names)
		elsewhere := 0
		for _, key := range k.once {
			if slices.ContainsFunc(c.defined[key], func(o *draft) bool { return o != parent }) {
				elsewhere++
			}
		}
		for _, o := range slices.SortedFunc(maps.Keys(cited), func(a, b *draft) int { return cmp.Or(cited[b]-cited[a], strings.Compare(a.kind.Name, b.kind.Name)) }) {
			if !self && o != parent && elsewhere*2 > len(k.once) && at.keyedBy == nil {
				at.keyedBy = o
			}
		}
		shape = &node{}

		if at.keyedBy == nil {
			for _, key := range k.once {
				if m := at.members[key]; m != nil {
					shape.merge(m)
					delete(at.members, key)
				}
			}
			if overflow := at.members["<name>"]; overflow != nil {
				shape.merge(overflow)
				delete(at.members, "<name>")
			}
			at.names = nil

			switch host := found[parent.kind.Name+" "+group]; {
			case self:
				for name, n := range names {
					parent.names[name] += n
				}
				parent.shape.merge(shape)
				at.defines = parent
			case host != nil:
				host.join(&draft{kind: &schema.Kind{}, names: names, shape: shape})
				at.defines = host
			default:
				child := &draft{kind: &schema.Kind{Name: group, Format: parent.kind.Format, Naming: schema.Naming{Form: "key"}}, names: names, shape: shape, parent: parent}
				at.defines, found[parent.kind.Name+" "+group] = child, child
				out = append(append(out, child), c.nested(child, shape, "", found)...)
			}
		}
		if !self {
			break
		}
	}

	for _, key := range slices.Sorted(maps.Keys(at.members)) {
		if !strings.HasPrefix(key, "<") || key == "<date>" {
			out = append(out, c.nested(parent, at.members[key], key, found)...)
		}
	}
	return out
}

// citedTogether reports two sets of names that one declared prefix cites: title: is written before an empire's name and before a barony's, which is what makes the landed titles of every depth one kind (§5.2 rule 6). Nothing cites the inner blocks of a trait.
func (c *census) citedTogether(outer, inner map[string]int) bool {
	for prefix, names := range c.cites {
		above, below := false, false
		for name := range names {
			above, below = above || outer[name] > 0, below || inner[name] > 0
		}
		if above && below && c.vocab.Links[prefix].Prefix {
			return true
		}
	}
	return false
}

// sameMembers reports two shapes that hold the same members, counting only members written in more than one block: most of the smaller one's are the larger one's.
func (c *census) sameMembers(a, b *node) bool {
	common := func(n *node) map[string]bool {
		out := map[string]bool{}
		for key, m := range n.members {
			if !strings.HasPrefix(key, "<") && m.in > 1 {
				out[key] = true
			}
		}
		return out
	}
	small, large := common(a), common(b)
	if len(small) > len(large) {
		small, large = large, small
	}
	shared := 0
	for key := range small {
		if large[key] {
			shared++
		}
	}
	return len(small) > 0 && shared*2 >= len(small)
}

// subjects finds the kinds that are about another kind and define nothing. Names alone cannot tell one: the games name a thing's icon, mesh, flag and coat of arms after the thing, so two kinds that both define often share most of their names. What tells one is where it is: the game's registry names the folders whose definitions are named after others (history, coats of arms, flags), and a kind under one of them is about the kind outside them that defines most of its names (history/titles about landed_titles, flag_definitions about countries), or defines them itself where none does (history/characters). A declared kind is about another declared kind of its tree in the same way: a GUI blockoverride is named after a block. Of several kinds that define most of the names, the one that defines the most, then the largest, which is the list the others are named after. Where nothing in the files is a name and the files' own names are another kind's, the files are named after it (history/cultures/afghan.txt).
func (c *census) subjects(all []*draft) {
	for _, d := range all {
		declared, most := d.kind.Naming.Form == "declaration" && d.parent == nil, 0
		if !c.about(d) && !declared {
			continue
		}
		for _, o := range all {
			if o == d || c.about(o) || (declared && (o.kind.Naming.Form != "declaration" || len(o.names) < len(d.names) || strings.Split(o.kind.Name, ".")[0] != strings.Split(d.kind.Name, ".")[0])) {
				continue
			}
			shared := 0
			for name := range d.names {
				if o.names[name] > 0 {
					shared++
				}
			}
			if shared*2 > len(d.names) && (shared > most || (shared == most && len(o.names) > len(d.subject.names))) {
				d.subject, most = o, shared
			}
		}
		if d.subject != nil && d.kind.Naming.Form == "none" {
			d.kind.Naming.Form = "file"
		}
	}
}

// about reports a kind whose folder is one the registry says holds definitions named after other kinds'.
func (c *census) about(d *draft) bool {
	return d.parent == nil && len(d.kind.Folders) > 0 && slices.ContainsFunc(c.aboutDirs, func(h string) bool {
		folder := strings.ToLower(d.kind.Folders[0])
		return folder == h || strings.HasPrefix(folder, h+"/")
	})
}

// settle gives every kind its name (§5.2): a subject kind and a kind that defines nothing its full folder; two defining kinds with the same last segment one more leading segment each; a nested kind its parent's name and its group word, or entry where it has none. Parents come before the kinds nested in them, so one pass in order settles both.
func (c *census) settle(all []*draft) {
	byName := map[string][]*draft{}
	for _, d := range all {
		if d.subject != nil || d.kind.Naming.Form == "none" || d.kind.Naming.Form == "file" {
			d.kind.Name = d.kind.Folders[0]
		}
		if d.parent == nil {
			byName[d.kind.Name] = append(byName[d.kind.Name], d)
		}
	}
	for name, same := range byName {
		// a kind named after things that shares a thing's last segment gives way: common/cultures stays cultures beside history/cultures
		present := slices.DeleteFunc(slices.Clone(same), c.about)
		for _, d := range same {
			if c.about(d) && len(present) > 0 {
				d.kind.Name = d.kind.Folders[0]
			} else if len(present) > 1 && strings.Contains(d.kind.Folders[0], "/") {
				d.kind.Name = path.Base(path.Dir(d.kind.Folders[0])) + "/" + name
			}
		}
	}

	for _, d := range all {
		if d.parent != nil {
			// a nested kind with no group word: the prefix the game cites it by, else the literal stem of its most covered loc template, else entry
			if d.kind.Name == "" && len(d.kind.Prefixes) > 0 {
				d.kind.Name = d.kind.Prefixes[0]
			}
			if templates := loc.Learn(slices.Sorted(maps.Keys(d.names)), nil, c.keys); d.kind.Name == "" && len(templates) > 0 {
				d.kind.Name = strings.Trim(strings.ReplaceAll(string(templates[0].Template), "{id}", ""), "_")
			}
			if d.kind.Name == "" {
				d.kind.Name = "entry"
			}
			d.kind.Parent, d.kind.Name = d.parent.kind.Name, d.parent.kind.Name+"."+d.kind.Name
		}
	}
	for _, d := range all {
		if d.subject != nil {
			d.kind.Subject = d.subject.kind.Name
		}
	}
}
