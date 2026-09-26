// Package learn is the one place in the engine that infers. It reads every file of an install
// once, counts what is written where (the census, this file), and turns the counts into a
// schema: which folders are one database, how each names its definitions, what each field
// holds. It decides by a fact the game declares, then by a row of the registry, then by once,
// most or every, and never by a tuned number. The drift report is here too: what a scan saw and
// nothing explained, the alarm for a game update that changes the language, with every list
// sorted by name so two scans of one install write the same report.
package learn

import (
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// maxMembers is how many distinct keys one position keeps a subtree for, and maxWords how many
// distinct words it keeps, unless most of its values are their own word: such a position holds
// names (a mesh's name, a list of provinces), and every one is needed. Past the first, a
// declared token's subtree goes to the census of that token and any other key shares the
// position <name>. Both bound memory and decide nothing.
const (
	maxMembers = 512
	maxWords   = 512
)

// node is what was written at one position, over every file: a key under the same parent, or a
// placeholder that gathers keys by their spelling: <number>, <date>, <cite>, <word>, <name>.
type node struct {
	count   int            // times written
	in      int            // blocks of the parent that hold it at least once
	blocks  int            // times its value was a block
	quoted  int            // times its value was a quoted word
	numbers int            // values spelled as a number
	dates   int            // values spelled as a date
	bools   int            // values spelled yes or no
	params  int            // values holding $NAME$
	ops     map[string]int // operators other than =
	tags    map[string]int // the tag of a tagged block: rgb, hsv, RANGE
	words   map[string]int // the words written as its value, the first maxWords distinct
	members map[string]*node
	names   map[string]int // keys past maxMembers, by how often written
	scoped  int            // declared tokens written directly inside that state their scopes
	scopes  map[string]int // how many of those may run in each scope type
	defines *draft         // the nested kind whose definitions are this position's name keys, once kinds are worked out
	keyedBy *draft         // the kind this position's name keys cite, where the definitions are made elsewhere
	classed bool           // its class is settled: read from a body's tokens before they are lifted out of it
	class   schema.Class
	body    schema.BodyClass
}

// folder is what the files of one folder hold at their top level.
type folder struct {
	rel      string // from a root, as first seen
	format   source.Format
	files    int
	bases    []string                  // the base names of its files, without extension
	root     *node                     // the top-level statements of its files
	keywords map[string]map[string]int // a bare word before a named block, and the names it declared
	declared map[string]*node          // what those blocks hold, by keyword, wherever in the folder they stand
	numbered map[string]int            // the numbers written as top-level keys: 186004 = { }
	fileOf   map[string]string         // a top-level key to the file that first wrote it, by file name
	current  string                    // the file being counted
	wordy    bool                      // most of its top-level keys recur: a key written twice is a word, not a name
}

// census is the counts of one install.
type census struct {
	vocab     *schema.Vocab
	folders   map[string]*folder         // by lower-cased folder
	order     []string                   // the folders in the order first seen
	tokens    map[string]*node           // what is written after a declared token, past a position's maxMembers
	cites     map[string]map[string]int  // prefix to the names cited under it
	constants map[string]int             // @name = value, at the top of any file
	files     map[string]bool            // every file, lower-cased, by path, name and stem.* (.tga is written for .dds)
	modes     map[string]int             // the entry modes written before top-level names: REPLACE, INJECT
	written   map[string]int             // every declared token written as a key, wherever
	holds     map[string]map[string]bool // a declared token to the declared tokens written directly inside its blocks
	blocked   map[string]int             // times a declared token was written with a block
	keys      *loc.Keys                  // the install's localization keys
	info      schema.Info                // what the game's own prose says of its fields
	aboutDirs []string                   // lower-cased folders of definitions named after other kinds' (history, flags)

	// worked out from the counts, in this order
	effectBlocks  map[string]bool                     // effects whose block holds effects: if, hidden_effect
	triggerBlocks map[string]bool                     // triggers whose block holds triggers: any_child, not
	math          map[string]bool                     // the words of arithmetic: value, add, multiply
	valued        map[string]bool                     // every word written as a value or after a prefix, wherever
	citing        map[string][]string                 // a name to the declared prefixes it is cited under
	keyedAt       map[string]int                      // a key to how many positions it is written at
	word          map[string]bool                     // top-level keys that are a file format's words, wherever written
	namedBy       map[string]string                   // a key opening several blocks to the member that names them
	extra         []*draft                            // the kinds read from tables and lists, which no script file holds
	byName        map[string]*draft                   // every kind by its settled name
	defined       map[string][]*draft                 // every definition's name to the kinds that define it
	numbered      []*draft                            // the kinds whose definitions are named by number
	prefixed      map[string][]*draft                 // a prefix to the kinds whose definitions are cited under it
	called        map[schema.BodyClass]map[string]int // keys written in each class of body that no vocabulary declares
	macro         map[string]bool                     // the names of every macro kind's definitions
	moved         map[string]bool                     // tokens a lift moved something into: the next pass lifts them
}

func newCensus(vocab *schema.Vocab) *census {
	return &census{vocab: vocab, folders: map[string]*folder{}, tokens: map[string]*node{}, cites: map[string]map[string]int{}, constants: map[string]int{},
		called: map[schema.BodyClass]map[string]int{}, macro: map[string]bool{}, moved: map[string]bool{}, files: map[string]bool{}, modes: map[string]int{}, written: map[string]int{}, holds: map[string]map[string]bool{}, blocked: map[string]int{}}
}

// file counts one parsed script file.
func (census *census) file(file source.File, root *jomini.Root) {
	// a file that sits in no folder is the install's own bookkeeping, not a database
	if path.Dir(file.Rel) == "." {
		return
	}
	key := strings.ToLower(path.Dir(file.Rel))
	fo := census.folders[key]
	if fo == nil {
		fo = &folder{rel: path.Dir(file.Rel), format: file.Format, root: &node{}, keywords: map[string]map[string]int{}, declared: map[string]*node{}, numbered: map[string]int{}, fileOf: map[string]string{}}
		census.folders[key], census.order = fo, append(census.order, key)
	}
	fo.files, fo.current = fo.files+1, path.Base(file.Rel)
	fo.bases = append(fo.bases, strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)))
	census.block(fo, fo.root, fo.root, "", root.Statements)
}

// block counts the statements of one block into pos. owner is the position whose scope the
// statements run in, nil once a scope change has been passed; under is the declared token whose
// block this is, lower-cased, or "".
func (census *census) block(fo *folder, pos, owner *node, under string, stmts []jomini.Statement) {
	seen := map[*node]bool{}
	for i := 0; i < len(stmts); i++ {
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			// a bare word before a named block declares: type x = button { } and template x { }
			// anywhere, scripted_trigger x = { } only at the top of a file, because inside a
			// block a bare word before delay = { } is a list's word
			word, isWord := st.Value.(*jomini.Scalar)
			if next, ok := at(stmts, i+1).(*jomini.Assignment); ok && isWord && !word.Quoted && jomini.IsLowerName(word.Text) && jomini.BlockOf(next.Value) != nil {
				if _, tagged := next.Value.(*jomini.TaggedBlock); tagged || next.Op == "" || pos == fo.root {
					if fo.keywords[word.Text] == nil {
						fo.keywords[word.Text], fo.declared[word.Text] = map[string]int{}, &node{}
					}
					fo.keywords[word.Text][next.Key.Text]++
					shape := fo.declared[word.Text]
					shape.count, shape.in = shape.count+1, shape.in+1
					census.value(shape, next.Value)
					census.block(fo, shape, shape, "", jomini.BlockOf(next.Value).Statements)
					i++
					continue
				}
			}

			// a bare word in a list, or a block among them
			child := census.member(pos, "<word>", seen)
			census.value(child, st.Value)
			if block := jomini.BlockOf(st.Value); block != nil {
				census.block(fo, child, child, "", block.Statements)
			}

		case *jomini.Assignment:
			key := st.Key.Text
			if pos == fo.root {
				mode := ""
				if mode, key = jomini.Mode(key); mode != "" {
					census.modes[mode]++
				}
				if jomini.Literal(key) == jomini.LitNumber && !st.Key.Quoted {
					fo.numbered[key]++
				}
				if name, ok := jomini.Constant(key); ok && name != "" {
					census.constants[name]++
					continue
				}
				if fo.fileOf[key] == "" {
					fo.fileOf[key] = fo.current
				}
			}

			// the key's position, and what the vocabulary says of it
			child, next := census.keyed(pos, key, st.Key.Quoted, seen), owner
			token, isToken := census.token(key)
			inside := ""
			if isToken {
				inside = strings.ToLower(key)
				census.written[inside]++
				if jomini.BlockOf(st.Value) != nil {
					census.blocked[inside]++
				}
				if under != "" {
					if census.holds[under] == nil {
						census.holds[under] = map[string]bool{}
					}
					census.holds[under][inside] = true
				}
			}
			if isToken && owner != nil && len(token.In) > 0 && token.In[0] != "none" {
				owner.scoped++
				for _, in := range token.In {
					if owner.scopes == nil {
						owner.scopes = map[string]int{}
					}
					owner.scopes[in]++
				}
			}
			if isToken && token.Out != "" || census.hops(key) {
				next = nil
			} else if !isToken {
				next = child
			}

			if st.Op != "=" && st.Op != "" {
				if child.ops == nil {
					child.ops = map[string]int{}
				}
				child.ops[st.Op]++
			}
			census.value(child, st.Value)
			if block := jomini.BlockOf(st.Value); block != nil {
				census.block(fo, child, next, inside, block.Statements)
			}
		}
	}
}

// at is the statement at i, or nil past the end.
func at(stmts []jomini.Statement, i int) jomini.Statement {
	if i < len(stmts) {
		return stmts[i]
	}
	return nil
}

// keyed is the position a key stands at under pos: <number>, <date> or <cite> by its spelling,
// else the key itself.
func (census *census) keyed(pos *node, key string, quoted bool, seen map[*node]bool) *node {
	if !quoted {
		switch jomini.Literal(key) {
		case jomini.LitNumber:
			return census.member(pos, "<number>", seen)
		case jomini.LitDate:
			return census.member(pos, "<date>", seen)
		}
		if cited, ok := jomini.Cite(key); ok && cited.Chain == "" {
			census.cite(cited)
			return census.member(pos, "<cite>", seen)
		}
	}
	return census.member(pos, key, seen)
}

// member is the child of pos for a key, counted once per block in its in. Past maxMembers a
// declared token's child is the census of that token, and any other key shares <name>.
func (census *census) member(pos *node, key string, seen map[*node]bool) *node {
	child := pos.members[key]
	if child == nil && len(pos.members) >= maxMembers && !strings.HasPrefix(key, "<") {
		if pos.names == nil {
			pos.names = map[string]int{}
		}
		pos.names[key]++
		if _, isToken := census.token(key); isToken {
			child = census.tokens[strings.ToLower(key)]
			if child == nil {
				child = &node{}
				census.tokens[strings.ToLower(key)] = child
			}
		} else {
			key, child = "<name>", pos.members["<name>"]
		}
	}
	if child == nil {
		if pos.members == nil {
			pos.members = map[string]*node{}
		}
		child = &node{}
		pos.members[key] = child
	}

	child.count++
	if !seen[child] {
		seen[child], child.in = true, child.in+1
	}
	return child
}

// value counts what is written as a value at pos.
func (census *census) value(pos *node, value jomini.Value) {
	switch typed := value.(type) {
	case *jomini.Block:
		pos.blocks++
	case *jomini.TaggedBlock:
		pos.blocks++
		if pos.tags == nil {
			pos.tags = map[string]int{}
		}
		pos.tags[typed.Tag.Text]++
	case *jomini.Scalar:
		if typed.Quoted {
			pos.quoted++
		}
		switch kind := jomini.Literal(typed.Text); {
		case kind == jomini.LitNumber && !typed.Quoted:
			pos.numbers++
		case kind == jomini.LitDate:
			pos.dates++
		case !typed.Quoted && (typed.Text == "yes" || typed.Text == "no"):
			pos.bools++
		}
		if len(jomini.Interpolations(typed.Text)) > 0 {
			pos.params++
		}
		if cited, ok := jomini.Cite(typed.Text); ok && cited.Chain == "" {
			census.cite(cited)
		}

		if pos.words == nil {
			pos.words = map[string]int{}
		}
		if _, known := pos.words[typed.Text]; known || len(pos.words) < maxWords || len(pos.words)*2 > pos.count {
			pos.words[typed.Text]++
		}
	}
}

// cite counts a name written under a prefix.
func (census *census) cite(cited jomini.Cited) {
	if census.cites[cited.Prefix] == nil {
		census.cites[cited.Prefix] = map[string]int{}
	}
	name := cited.Name
	if cited.Member != "" {
		name = cited.Member
	}
	census.cites[cited.Prefix][name]++
}

// token is the effect or trigger a key names, looked up lower-cased as the reader does.
func (census *census) token(key string) (schema.EngineToken, bool) {
	name := strings.ToLower(key)
	if effect, ok := census.vocab.Effects[name]; ok {
		return effect, true
	}
	text, ok := census.vocab.Triggers[name]
	return text, ok
}

// hops reports a key that moves the scope: a declared link, a path of them, a cite with a
// chain, or a parameter, which stands for one of those where a macro's text is written
// ($ACTOR$ = { }).
func (census *census) hops(key string) bool {
	if len(jomini.Interpolations(key)) > 0 {
		return true
	}
	first, _, _ := strings.Cut(key, ".")
	if _, isLink := census.vocab.Links[first]; isLink {
		return true
	}
	_, isCite := jomini.Cite(key)
	return isCite
}

// drift is the report for a learned schema.
func (census *census) drift(in Input, parseErrors []schema.Tally, sch *schema.Schema) schema.Drift {
	drift := schema.Drift{ParseErrors: parseErrors, DumpLabels: in.Vocab.Unknown}

	// files with no format, and top-level folders of script in which no kind has a folder
	extensions, folders, held := map[string]int{}, map[string]int{}, map[string]bool{}
	for _, kind := range sch.Kinds {
		for _, folder := range kind.Folders {
			top, _, _ := strings.Cut(strings.ToLower(folder), "/")
			held[top] = true
		}
	}
	for _, file := range in.Files {
		top, _, nested := strings.Cut(strings.ToLower(file.Rel), "/")
		switch {
		case file.Format == source.Unknown:
			extensions[strings.ToLower(path.Ext(file.Rel))]++
		case file.Format == source.Script && nested && !held[top]:
			folders[top]++
		}
	}
	drift.Extensions, drift.Folders = tallies(extensions), tallies(folders)

	// entry modes the registry gives no meaning, and prefixes nothing declares and no kind owns
	modes, prefixes := map[string]int{}, map[string]int{}
	for mode, count := range census.modes {
		if !slices.Contains(in.EntryModes, mode) {
			modes[mode] = count
		}
	}
	made := slices.Collect(maps.Values(schema.ScriptKinds))
	for prefix, names := range census.cites {
		written := 0
		for _, count := range names {
			written += count
		}
		if _, declared := in.Vocab.Prefixes[prefix]; !declared && len(census.prefixed[prefix]) == 0 && !slices.Contains(made, prefix) {
			prefixes[prefix] = written
		}
	}
	drift.EntryModes, drift.Prefixes = tallies(modes), tallies(prefixes)

	// an effect outside the hand table most of whose values are names that nothing declares or
	// defines and that are cited under a script-made kind's prefix. Only an effect makes a name:
	// a trigger that reads one (has_sexuality) takes words that any saved scope may share, root
	// is a declared link that scope:root also spells, and a trait's name is also written flag:brave
	tokens, unclassified := map[string]int{}, map[string]int{}
	for token, pos := range census.tokens {
		if _, makes := in.Vocab.Effects[token]; !makes || len(schema.NameTokensOf(token)) > 0 {
			continue
		}
		written, named := 0, 0
		for word, times := range pos.words {
			written += times
			_, isLink := in.Vocab.Links[word]
			for _, prefix := range made {
				if prefix != "" && census.cites[prefix][word] > 0 && !isLink && len(census.defined[word]) == 0 {
					named += times
					break
				}
			}
		}
		if named*2 > written {
			tokens[token] = written
		}
	}
	for token, count := range census.written {
		if sch.Arguments[token] == nil {
			unclassified[token] = count
		}
	}
	drift.NameTokens, drift.Unclassified = tallies(tokens), tallies(unclassified)
	return drift
}

// tallies is a count by name as a sorted list, nil when there is nothing.
func tallies(counts map[string]int) []schema.Tally {
	var out []schema.Tally
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, schema.Tally{Name: name, Count: counts[name]})
	}
	return out
}
