// kinds.go turns the census of folders into kinds: which top-level keys are names and which are
// words of the file, how each kind names its definitions, which folders are one database,
// which kinds are nested in others or are about others, and what each is called.
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

// draft is a kind being worked out: what it will be, the names of its definitions, and the
// census of what one definition holds.
type draft struct {
	kind    *schema.Kind
	names   map[string]int
	shape   *node
	parent  *draft // a nested kind: the kind whose definitions hold this one's
	subject *draft // a subject kind: the kind its names are definitions of
	params  bool   // its definitions' text holds $NAME$ somewhere
}

// merge adds the counts of one position into another, members and all.
func (pos *node) merge(other *node) {
	// a position already taken in by an earlier merge is the same position, not more of it
	if pos == other {
		return
	}
	pos.count, pos.in, pos.blocks, pos.quoted = pos.count+other.count, pos.in+other.in, pos.blocks+other.blocks, pos.quoted+other.quoted
	pos.numbers, pos.dates, pos.bools, pos.params, pos.scoped = pos.numbers+other.numbers, pos.dates+other.dates, pos.bools+other.bools, pos.params+other.params, pos.scoped+other.scoped
	for _, pair := range []struct{ to, from *map[string]int }{{&pos.ops, &other.ops}, {&pos.tags, &other.tags}, {&pos.names, &other.names}, {&pos.scopes, &other.scopes}} {
		for k, value := range *pair.from {
			if *pair.to == nil {
				*pair.to = map[string]int{}
			}
			(*pair.to)[k] += value
		}
	}
	// where the words may not all fit they are taken in order, so two scans of one install keep
	// the same ones
	words := slices.Collect(maps.Keys(other.words))
	if len(pos.words)+len(words) > maxWords {
		slices.Sort(words)
	}
	for _, word := range words {
		if pos.words == nil {
			pos.words = map[string]int{}
		}
		if _, known := pos.words[word]; known || len(pos.words) < maxWords || len(pos.words)*2 > pos.count {
			pos.words[word] += other.words[word]
		}
	}
	for key, child := range other.members {
		if pos.members == nil {
			pos.members = map[string]*node{}
		}
		if mine := pos.members[key]; mine != nil {
			mine.merge(child)
		} else {
			pos.members[key] = child
		}
	}
}

// shallow is a position's members without what is under them, which is all the comparison of
// two shapes reads.
func (pos *node) shallow() *node {
	out := &node{count: pos.count, in: pos.in, blocks: pos.blocks, numbers: pos.numbers, members: map[string]*node{}}
	for key, member := range pos.members {
		out.members[key] = &node{count: member.count, in: member.in, blocks: member.blocks}
	}
	return out
}

// join adds one draft's definitions and folders to another's.
func (draft *draft) join(other *draft) {
	draft.shape.merge(other.shape)
	for name, count := range other.names {
		draft.names[name] += count
	}
	for _, folder := range other.kind.Folders {
		if !slices.Contains(draft.kind.Folders, folder) {
			draft.kind.Folders = append(draft.kind.Folders, folder)
		}
	}
}

// again reports a top-level key written twice in one file, or in most of its folder's files. A
// name is written once, in one file.
func again(pos *node, files int) bool {
	return pos.count > pos.in || (files > 1 && pos.in*2 > files)
}

// isWord reports a top-level key that is a word of the file format in this folder and no name:
// one whose block here holds the member that names such blocks all over the install (a pdxmesh
// with its name, written once in a leaf folder), one written again in most of the folders that
// write it, one in most of this folder's files (namespace), or one written twice in a file
// where most of the folder's keys are (war in history/wars, whose only key it is). A trait
// defined twice in one file by mistake is still a name.
func (census *census) isWord(fo *folder, key string, pos *node) bool {
	return census.word[key] || pos.members[census.namedBy[key]] != nil || (fo.files > 1 && pos.in*2 > fo.files) || (pos.count > pos.in && fo.wordy)
}

// words finds the words of the file formats: the top-level keys written again in most of the
// folders that write them, and, for a key written again in some folder of words, the member its
// blocks are named by: one most of those blocks hold, written as a value, whose values are
// mostly their own and mostly new names (no number, file, call, sentence, or name defined at
// the top of some file). Of several, the one most blocks hold, then the one with the most values
// of its own. Counted over the whole install: in one leaf folder pdxmesh is written once.
func (census *census) words() {
	folders, repeated, blocks := map[string]int{}, map[string]int{}, map[string]int{}
	for _, fo := range census.folders {
		keys, more := 0, 0
		for key, member := range fo.root.members {
			if !strings.HasPrefix(key, "<") {
				keys++
				if again(member, fo.files) {
					more++
				}
			}
		}
		fo.wordy = more*2 > keys
		for key, member := range fo.root.members {
			if !strings.HasPrefix(key, "<") {
				folders[key], blocks[key] = folders[key]+1, blocks[key]+member.blocks
				if again(member, fo.files) && fo.wordy {
					repeated[key]++
				}
			}
		}
	}
	census.word, census.namedBy = map[string]bool{}, map[string]string{}
	for key, count := range folders {
		census.word[key] = count > 1 && repeated[key]*2 > count
	}

	// the values of each member directly inside the blocks of a key that opens several
	held, values, names := map[[2]string]int{}, map[[2]string]int{}, map[[2]string]map[string]bool{}
	for _, fo := range census.folders {
		for key, member := range fo.root.members {
			if strings.HasPrefix(key, "<") || repeated[key] == 0 {
				continue
			}
			for name, child := range member.members {
				at := [2]string{key, name}
				if names[at] == nil {
					names[at] = map[string]bool{}
				}
				held[at], values[at] = held[at]+child.in, values[at]+child.count-child.blocks
				for word := range child.words {
					names[at][word] = true
				}
			}
		}
	}
	for _, at := range slices.SortedFunc(maps.Keys(held), func(first, second [2]string) int {
		return strings.Compare(first[0]+" "+first[1], second[0]+" "+second[1])
	}) {
		named := 0
		for word := range names[at] {
			if jomini.IsDefName(word) && jomini.Literal(word) != jomini.LitNumber && !census.files[strings.ToLower(word)] && folders[word] == 0 {
				named++
			}
		}
		by := [2]string{at[0], census.namedBy[at[0]]}
		better := held[at] > held[by] || (held[at] == held[by] && len(names[at]) > len(names[by]))
		if !strings.HasPrefix(at[1], "<") && held[at]*2 > blocks[at[0]] && values[at]*2 > held[at] && len(names[at])*2 > values[at] && named*2 > len(names[at]) && better {
			census.namedBy[at[0]] = at[1]
		}
	}
}

// drafts is the kinds one folder holds by itself, before folders are joined: one for the names
// at its top level, one for each word that names its blocks by a member, one for each
// declaration keyword, and one for the files themselves where nothing in them is a name.
func (census *census) drafts(fo *folder) []*draft {
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
		member := fo.root.members[key]
		switch {
		case key == "<name>" || key == "<number>":
			shape.merge(member)
			named += member.count
		case strings.HasPrefix(key, "<"):
			worded += member.count
		case census.isWord(fo, key, member):
			worded += member.count
			if by := census.namedBy[key]; by != "" && member.members[by] != nil {
				add(base+"."+key, schema.Naming{Form: "member", Keyword: key, Member: by}, maps.Clone(member.members[by].words), member)
			}
		default:
			plain, named = append(plain, key), named+member.count
		}
	}
	for key, count := range fo.root.names {
		names[key] += count
	}

	// files side by side are one kind where they are alike; a file unlike the rest is a kind of
	// its own, named after it
	groups, files := census.sideBySide(fo, plain)
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

	// the names are a kind when they are most of what the files hold; a few stray words among a
	// folder of widgets are not
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

	// nothing in the files is a name: the files themselves may be (history/cultures/afghan.txt),
	// which the subject join decides
	if len(out) == 0 && len(fo.root.members) > 0 {
		files := map[string]int{}
		for _, base := range fo.bases {
			files[base]++
		}
		add(fo.rel, schema.Naming{Form: "none"}, files, fo.root)
	}
	return out
}

// sideBySide sorts a folder's names by the files that hold them: first the names of the files
// that belong together, then those of each file that is a kind of its own, with that file's
// name. alike is too fine for one file (a small file of scripted effects may write no if). A
// file belongs when a definition in it writes a key or a vocabulary another file's write; when
// its definitions write no key and are empty blocks, or another file's write none either (plain
// script values beside formulas); and wherever the folder's keys are mostly names written once.
func (census *census) sideBySide(fo *folder, keys []string) (groups [][]string, files []string) {
	byFile, probes, writers, whole := map[string][]string{}, map[string]*node{}, map[string]int{}, &node{}
	for _, key := range keys {
		byFile[fo.fileOf[key]] = append(byFile[fo.fileOf[key]], key)
	}
	for file, held := range byFile {
		probes[file] = &node{}
		for _, key := range held {
			probes[file].merge(fo.root.members[key].shallow())
		}
		for _, word := range census.shared(probes[file]) {
			writers[word]++
		}
		whole.merge(probes[file].shallow())
	}
	counted := census.keysOf(whole)

	groups, files = [][]string{nil}, []string{""}
	for _, file := range slices.Sorted(maps.Keys(byFile)) {
		probe := probes[file]
		belongs := len(byFile) == 1 || len(counted.once)*2 > counted.distinct
		if len(probe.members) == 0 {
			belongs = belongs || probe.blocks == probe.count || whole.count-whole.blocks > probe.count-probe.blocks
		}
		for _, word := range census.shared(probe) {
			belongs = belongs || writers[word] > 1
		}
		if belongs {
			groups[0], files[0] = append(groups[0], byFile[file]...), cmp.Or(files[0], file)
		} else {
			groups, files = append(groups, byFile[file]), append(files, file)
		}
	}

	// the folder's kind is the group that holds the most names: traits beside two lookup tables,
	// one file of animations beside a few settings
	most := 0
	for i := range groups {
		if len(groups[i]) > len(groups[most]) {
			most = i
		}
	}
	groups[0], groups[most], files[0], files[most] = groups[most], groups[0], files[most], files[0]
	return groups, files
}

// shared is what a file's definitions write that another file's may write too: their keys, the
// vocabulary itself where a key is a declared effect or trigger (one file adds to the treasury
// and another only branches, and both write effects), and arithmetic, which one file writes as
// plain numbers and another as formulas.
func (census *census) shared(probe *node) []string {
	var out []string
	if (len(probe.members) == 0 && probe.numbers > 0) || census.keysOf(probe).sums {
		out = append(out, "<sum>")
	}
	for key := range probe.members {
		low := strings.ToLower(key)
		if _, ok := census.vocab.Effects[low]; ok {
			out = append(out, "<effect>")
		}
		if _, ok := census.vocab.Triggers[low]; ok {
			out = append(out, "<trigger>")
		}
		out = append(out, key)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// alike reports two shapes that are one database in two places: what a definition holds is of
// one class, and, where that is a structure, most of the smaller one's members are the larger
// one's.
func (census *census) alike(first, second *node) bool {
	classA, bodyA := census.class(first)
	classB, bodyB := census.class(second)
	if classA != classB || bodyA != bodyB {
		return false
	}

	// two shapes whose keys are mostly names written once hold databases, whatever the names
	// are: the groups of common/defines and of common/defines/ai share no entry
	ka, kb := census.keysOf(first), census.keysOf(second)
	if classA != schema.ClassStructure || (census.named(ka) && census.named(kb)) {
		return true
	}

	small, large := first.members, second.members
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

// kinds joins the folders' drafts into kinds, then finds the kinds nested in them and the kinds
// that are about others.
func (census *census) kinds() []*draft {
	census.words()
	perFolder := map[string][]*draft{}
	for key, fo := range census.folders {
		perFolder[key] = census.drafts(fo)
	}

	var all []*draft
	byFolder, byWord := map[string][]*draft{}, map[string]*draft{}
	for _, key := range slices.Sorted(maps.Keys(census.folders)) {
		for _, sketch := range perFolder[key] {
			// a kind told by its opening word is one kind over its tree, whose top is the
			// shallowest folder above that writes such a word itself: gfx/models, gui
			if sketch.kind.Naming.Keyword != "" {
				top := key
				for up := path.Dir(key); up != "."; up = path.Dir(up) {
					if slices.ContainsFunc(perFolder[up], func(draft *draft) bool { return draft.kind.Naming.Keyword != "" }) {
						top = up
					}
				}
				at := top + " " + sketch.kind.Naming.Form + " " + sketch.kind.Naming.Keyword
				if byWord[at] == nil {
					sketch.kind.Name = path.Base(census.folders[top].rel) + "." + sketch.kind.Naming.Keyword
					byWord[at], all = sketch, append(all, sketch)
				} else {
					byWord[at].join(sketch)
				}
				continue
			}

			// a subfolder joins the kind of its nearest ancestor that holds files, when the two are alike
			up := path.Dir(key)
			for up != "." && census.folders[up] == nil {
				up = path.Dir(up)
			}
			host := slices.IndexFunc(byFolder[up], func(draft *draft) bool {
				return draft.kind.Naming == sketch.kind.Naming && draft.kind.Format == sketch.kind.Format && census.alike(draft.shape, sketch.shape)
			})
			if host >= 0 {
				byFolder[up][host].join(sketch)
				byFolder[key] = append(byFolder[key], byFolder[up][host])
				continue
			}
			byFolder[key], all = append(byFolder[key], sketch), append(all, sketch)
		}
	}

	// a keyword that is a kind's name in the singular declares that kind inline where the two are
	// alike: scripted_trigger x = { } in an events file is a trigger body like the folder's, and a
	// GUI type is nothing like the entries of a folder that happens to be called types. Any other
	// keyword is a word of the format when it is written again: a licence text parsed as script
	// declares one name
	keyed := map[string]*draft{}
	for _, draft := range all {
		if draft.kind.Naming.Form == "key" {
			keyed[draft.kind.Name] = draft
		}
	}
	all = slices.DeleteFunc(all, func(draft *draft) bool {
		if draft.kind.Naming.Form != "declaration" {
			return false
		}
		if host := keyed[draft.kind.Naming.Keyword+"s"]; host != nil && (census.alike(host.shape, draft.shape) || census.speaks(host.shape, draft.shape)) {
			folders := host.kind.Folders
			host.join(draft)
			host.kind.Folders, host.kind.Naming.Keyword = folders, draft.kind.Naming.Keyword
			return true
		}
		return len(draft.names) < 2
	})

	all = append(all, census.extra...)
	census.index(all)
	found := map[string]*draft{}
	for _, draft := range slices.Clone(all) {
		all = append(all, census.nested(draft, draft.shape, "", found)...)
	}
	census.subjects(all)
	return all
}

// speaks reports a small shape written wholly in the vocabulary of a body: every key in it is a
// token of the body's classes or a scope change. One inline scripted trigger that holds
// is_adult = yes writes no block-taking token, and is a trigger body all the same.
func (census *census) speaks(body, small *node) bool {
	_, classes := census.class(body)
	for key := range small.members {
		low := strings.ToLower(key)
		_, effect := census.vocab.Effects[low]
		_, trigger := census.vocab.Triggers[low]
		if !(effect && classes&schema.BodyEffect != 0) && !(trigger && classes&schema.BodyTrigger != 0) && !census.hops(key) {
			return false
		}
	}
	return classes != 0 && len(small.members) > 0
}

// nested finds the kinds written inside a kind's definitions: a position that is no body and
// whose keys are mostly names written once each (faiths inside a religion, settings inside a
// game rule). Where most of those names are defined elsewhere the position is keyed by the kind
// that defines the most of them (goods = { wheat = 0.5 }); else the names leave the position,
// which carries the kind it defines. A nested kind with no group word that holds the parent's
// members is the parent nested in itself (a landed title in a landed title), and its names are
// the parent's. Nothing is looked for inside a body: what stands under a token is the token's.
func (census *census) nested(parent *draft, at *node, group string, found map[string]*draft) []*draft {
	class, _ := census.class(at)
	if class == schema.ClassBody {
		return nil
	}

	// a kind nested in itself brings its own nested names with it, so the position is read again
	// until it holds no more
	var out []*draft
	for counted := census.keysOf(at); census.named(counted); counted = census.keysOf(at) {
		names, shape, cited := map[string]int{}, &node{}, map[*draft]int{}
		for _, key := range counted.once {
			if member := at.members[key]; member != nil {
				names[key] += member.count
			}
			for _, definer := range census.defined[key] {
				cited[definer]++
			}
		}
		for key, count := range at.names {
			names[key] += count
		}

		// the parent nested in itself is looked for first: a landed title's names are also title
		// history's, which cites them
		for _, key := range counted.once {
			if member := at.members[key]; member != nil {
				shape.merge(member.shallow())
			}
		}
		self := group == "" && census.sameMembers(shape, parent.shape) && census.citedTogether(parent.names, names)
		elsewhere := 0
		for _, key := range counted.once {
			if slices.ContainsFunc(census.defined[key], func(draft *draft) bool { return draft != parent }) {
				elsewhere++
			}
		}
		for _, candidate := range slices.SortedFunc(maps.Keys(cited), func(first, second *draft) int {
			return cmp.Or(cited[second]-cited[first], strings.Compare(first.kind.Name, second.kind.Name))
		}) {
			if !self && candidate != parent && elsewhere*2 > len(counted.once) && at.keyedBy == nil {
				at.keyedBy = candidate
			}
		}
		shape = &node{}

		if at.keyedBy == nil {
			for _, key := range counted.once {
				if member := at.members[key]; member != nil {
					shape.merge(member)
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
				for name, count := range names {
					parent.names[name] += count
				}
				parent.shape.merge(shape)
				at.defines = parent
			case host != nil:
				host.join(&draft{kind: &schema.Kind{}, names: names, shape: shape})
				at.defines = host
			default:
				child := &draft{kind: &schema.Kind{Name: group, Format: parent.kind.Format, Naming: schema.Naming{Form: "key"}}, names: names, shape: shape, parent: parent}
				at.defines, found[parent.kind.Name+" "+group] = child, child
				out = append(append(out, child), census.nested(child, shape, "", found)...)
			}
		}
		if !self {
			break
		}
	}

	for _, key := range slices.Sorted(maps.Keys(at.members)) {
		if !strings.HasPrefix(key, "<") || key == "<date>" {
			out = append(out, census.nested(parent, at.members[key], key, found)...)
		}
	}
	return out
}

// citedTogether reports two sets of names that one declared prefix cites: title: is written
// before an empire's name and before a barony's, which is what makes the landed titles of every
// depth one kind. Nothing cites the inner blocks of a trait.
func (census *census) citedTogether(outer, inner map[string]int) bool {
	for prefix, names := range census.cites {
		above, below := false, false
		for name := range names {
			above, below = above || outer[name] > 0, below || inner[name] > 0
		}
		if _, declared := census.vocab.Prefixes[prefix]; above && below && declared {
			return true
		}
	}
	return false
}

// sameMembers reports two shapes that hold the same members, counting only members written in
// more than one block: most of the smaller one's are the larger one's.
func (census *census) sameMembers(first, second *node) bool {
	common := func(pos *node) map[string]bool {
		out := map[string]bool{}
		for key, member := range pos.members {
			if !strings.HasPrefix(key, "<") && member.in > 1 {
				out[key] = true
			}
		}
		return out
	}
	small, large := common(first), common(second)
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

// subjects finds the kinds that are about another kind and define nothing. Names alone cannot
// tell one, since the games name a thing's icon, mesh and flag after it; where it is can: a kind
// under a folder the registry names is about the kind outside those that defines most of its
// names (history/titles about landed_titles), or defines them itself where none does. A
// declared kind is about another declared kind of its tree the same way (blockoverride, block).
// Of several, the one that defines the most, then the largest. Where nothing in the files is a
// name, the files are named after the subject (history/cultures/afghan.txt).
func (census *census) subjects(all []*draft) {
	for _, named := range all {
		declared, most := named.kind.Naming.Form == "declaration" && named.parent == nil, 0
		if !census.about(named) && !declared {
			continue
		}
		for _, owner := range all {
			if owner == named || census.about(owner) || (declared && (owner.kind.Naming.Form != "declaration" || len(owner.names) < len(named.names) || strings.Split(owner.kind.Name, ".")[0] != strings.Split(named.kind.Name, ".")[0])) {
				continue
			}
			shared := 0
			for name := range named.names {
				if owner.names[name] > 0 {
					shared++
				}
			}
			if shared*2 > len(named.names) && (shared > most || (shared == most && len(owner.names) > len(named.subject.names))) {
				named.subject, most = owner, shared
			}
		}
		if named.subject != nil && named.kind.Naming.Form == "none" {
			named.kind.Naming.Form = "file"
		}
	}
}

// about reports a kind whose folder is one the registry says holds definitions named after
// other kinds'.
func (census *census) about(draft *draft) bool {
	return draft.parent == nil && len(draft.kind.Folders) > 0 && slices.ContainsFunc(census.aboutDirs, func(dir string) bool {
		folder := strings.ToLower(draft.kind.Folders[0])
		return folder == dir || strings.HasPrefix(folder, dir+"/")
	})
}

// settle gives every kind its name: a subject kind and a kind that defines nothing its full
// folder; two defining kinds with the same last segment one more leading segment each; a nested
// kind its parent's name and its group word, or entry where it has none. Parents come before
// the kinds nested in them, so one pass in order settles both.
func (census *census) settle(all []*draft) {
	byName := map[string][]*draft{}
	for _, draft := range all {
		if draft.subject != nil || draft.kind.Naming.Form == "none" || draft.kind.Naming.Form == "file" {
			draft.kind.Name = draft.kind.Folders[0]
		}
		if draft.parent == nil {
			byName[draft.kind.Name] = append(byName[draft.kind.Name], draft)
		}
	}
	for name, same := range byName {
		// a kind named after things that shares a thing's last segment gives way: common/cultures
		// stays cultures beside history/cultures
		present := slices.DeleteFunc(slices.Clone(same), census.about)
		for _, draft := range same {
			if census.about(draft) && len(present) > 0 {
				draft.kind.Name = draft.kind.Folders[0]
			} else if len(present) > 1 && strings.Contains(draft.kind.Folders[0], "/") {
				draft.kind.Name = path.Base(path.Dir(draft.kind.Folders[0])) + "/" + name
			}
		}
	}

	for _, draft := range all {
		if draft.parent != nil {
			// a nested kind with no group word: the prefix the game cites it by, else the literal
			// stem of its most covered loc template, else entry
			if draft.kind.Name == "" && len(draft.kind.Prefixes) > 0 {
				draft.kind.Name = draft.kind.Prefixes[0]
			}
			if templates := loc.Learn(slices.Sorted(maps.Keys(draft.names)), nil, census.keys); draft.kind.Name == "" && len(templates) > 0 {
				draft.kind.Name = strings.Trim(strings.ReplaceAll(string(templates[0].Template), "{id}", ""), "_")
			}
			draft.kind.Name = cmp.Or(draft.kind.Name, "entry")
			draft.kind.Parent, draft.kind.Name = draft.parent.kind.Name, draft.parent.kind.Name+"."+draft.kind.Name
		}
	}
	for _, draft := range all {
		if draft.subject != nil {
			draft.kind.Subject = draft.subject.kind.Name
		}
	}
}
