// Package learn is the one place in the engine that infers. It reads every file of an install once, counts what is written where (the census), and turns the counts into a schema: which folders are one database, how each names its definitions, what every field holds, which prefix cites which kind. Nothing here is a rule about a game, and nothing is decided by a tuned number. A question is settled by what the game declares (a token's usage, a link's output, an info file's type), else by a row of the game's registry, else by once, most or every: a name is written once, a field accepts the kind that defines most of its words. What none of those explains is reported as drift, never guessed.
//
// census.go is the counting. It decides nothing. Every key is recorded the same way wherever it stands, because whether a block is a body of effects or a structure of members cannot be told while reading it: a character's history block writes culture, faith and dynasty, which are also triggers.
package learn

import (
	"path"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// maxMembers is how many distinct keys one position keeps a subtree for, and maxWords how many distinct words it keeps, unless most of its values are their own word: such a position holds names (a mesh's name, a list of provinces), and every one is needed. Past the first, a declared token's subtree goes to the census of that token and any other key shares the position <name>. Both bound memory and decide nothing.
const (
	maxMembers = 512
	maxWords   = 512
)

// node is what was written at one position, over every file: a key under the same parent, or a position of §5.3.
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
	classed bool           // its class is settled: what a body's tokens say of it is read before they are lifted out of it
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
	wordy    bool                      // most of its top-level keys are written again: its files are made of words, and a key written twice is no name defined twice
}

// census is the counts of one install.
type census struct {
	vocab     *schema.Vocab
	folders   map[string]*folder         // by lower-cased folder
	order     []string                   // the folders in the order first seen
	tokens    map[string]*node           // what is written after a declared token, past a position's maxMembers
	cites     map[string]map[string]int  // prefix to the names cited under it
	constants map[string]int             // @name = value, at the top of any file
	files     map[string]bool            // the inventory: every file by its lower-cased path from a root, by its lower-cased name, and by that name with .* for its extension (.tga is written where .dds ships)
	modes     map[string]int             // the entry modes written before top-level names: REPLACE, INJECT
	written   map[string]int             // every declared token written as a key, wherever
	holds     map[string]map[string]bool // a declared token to the declared tokens written directly inside its blocks
	blocked   map[string]int             // times a declared token was written with a block
	keys      *loc.Keys                  // the install's localization keys
	info      schema.Info                // what the game's own prose says of its fields
	aboutDirs []string                   // the folders the registry says hold definitions named after other kinds': history, coats of arms, flags; lower-cased

	// worked out from the counts, in this order
	effectBlocks  map[string]bool                     // the effects whose block holds effects: if, every_vassal, hidden_effect
	triggerBlocks map[string]bool                     // the triggers whose block holds triggers: any_child, trigger_if, not
	math          map[string]bool                     // the words of arithmetic: value, add, multiply
	valued        map[string]bool                     // every word written as a value or after a prefix, wherever
	citing        map[string][]string                 // a name to the declared prefixes it is cited under
	keyedAt       map[string]int                      // a key to how many positions it is written at
	word          map[string]bool                     // the top-level keys that are words of a file format wherever they are written
	namedBy       map[string]string                   // a key that opens several blocks to the member those blocks are named by
	extra         []*draft                            // the kinds read from tables and lists, which no script file holds
	byName        map[string]*draft                   // every kind by its settled name
	defined       map[string][]*draft                 // every definition's name to the kinds that define it
	numbered      []*draft                            // the kinds whose definitions are named by number
	prefixed      map[string][]*draft                 // a prefix to the kinds whose definitions are cited under it
	called        map[schema.BodyClass]map[string]int // the keys written in bodies of each class that no vocabulary declares
	macro         map[string]bool                     // the names of every macro kind's definitions
	moved         bool                                // a lift took a token or a scope change out of a position: the tokens are lifted again until none does
	locKinds      map[string]*schema.Kind             // the kinds whose definitions are loc keys, by name, made where a field is found to hold them
}

func newCensus(vocab *schema.Vocab) *census {
	return &census{vocab: vocab, folders: map[string]*folder{}, tokens: map[string]*node{}, cites: map[string]map[string]int{}, constants: map[string]int{},
		called: map[schema.BodyClass]map[string]int{}, macro: map[string]bool{}, locKinds: map[string]*schema.Kind{}, files: map[string]bool{}, modes: map[string]int{}, written: map[string]int{}, holds: map[string]map[string]bool{}, blocked: map[string]int{}}
}

// file counts one parsed script file.
func (c *census) file(f source.File, root *jomini.Root) {
	// a file that sits in no folder is the install's own bookkeeping, not a database
	if path.Dir(f.Rel) == "." {
		return
	}
	key := strings.ToLower(path.Dir(f.Rel))
	fo := c.folders[key]
	if fo == nil {
		fo = &folder{rel: path.Dir(f.Rel), format: f.Format, root: &node{}, keywords: map[string]map[string]int{}, declared: map[string]*node{}, numbered: map[string]int{}, fileOf: map[string]string{}}
		c.folders[key], c.order = fo, append(c.order, key)
	}
	fo.files, fo.current = fo.files+1, path.Base(f.Rel)
	fo.bases = append(fo.bases, strings.TrimSuffix(path.Base(f.Rel), path.Ext(f.Rel)))
	c.block(fo, fo.root, fo.root, "", root.Statements)
}

// block counts the statements of one block into n. owner is the position whose scope the statements run in, nil once a scope change has been passed; under is the declared token whose block this is, lower-cased, or "".
func (c *census) block(fo *folder, n, owner *node, under string, stmts []jomini.Statement) {
	seen := map[*node]bool{}
	for i := 0; i < len(stmts); i++ {
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			// a bare word before a named block declares: type x = button { } and template x { } anywhere, scripted_trigger x = { } only at the top of a file, because inside a block a bare word before delay = { } is a list's word
			word, isWord := st.Value.(*jomini.Scalar)
			if next, ok := at(stmts, i+1).(*jomini.Assignment); ok && isWord && !word.Quoted && jomini.IsLowerName(word.Text) && jomini.BlockOf(next.Value) != nil {
				if _, tagged := next.Value.(*jomini.TaggedBlock); tagged || next.Op == "" || n == fo.root {
					if fo.keywords[word.Text] == nil {
						fo.keywords[word.Text], fo.declared[word.Text] = map[string]int{}, &node{}
					}
					fo.keywords[word.Text][next.Key.Text]++
					shape := fo.declared[word.Text]
					shape.count, shape.in = shape.count+1, shape.in+1
					c.value(shape, next.Value)
					c.block(fo, shape, shape, "", jomini.BlockOf(next.Value).Statements)
					i++
					continue
				}
			}

			// a bare word in a list, or a block among them
			child := c.member(n, "<word>", seen)
			c.value(child, st.Value)
			if b := jomini.BlockOf(st.Value); b != nil {
				c.block(fo, child, child, "", b.Statements)
			}

		case *jomini.Assignment:
			key := st.Key.Text
			if n == fo.root {
				mode := ""
				if mode, key = jomini.Mode(key); mode != "" {
					c.modes[mode]++
				}
				if jomini.Literal(key) == jomini.LitNumber && !st.Key.Quoted {
					fo.numbered[key]++
				}
				if name, ok := jomini.Constant(key); ok && name != "" {
					c.constants[name]++
					continue
				}
				if fo.fileOf[key] == "" {
					fo.fileOf[key] = fo.current
				}
			}

			// the key's position, and what the vocabulary says of it
			child, next := c.keyed(n, key, st.Key.Quoted, seen), owner
			token, isToken := c.token(key)
			inside := ""
			if isToken {
				inside = strings.ToLower(key)
				c.written[inside]++
				if jomini.BlockOf(st.Value) != nil {
					c.blocked[inside]++
				}
				if under != "" {
					if c.holds[under] == nil {
						c.holds[under] = map[string]bool{}
					}
					c.holds[under][inside] = true
				}
			}
			if isToken && owner != nil && len(token.In) > 0 && token.In[0] != "none" {
				owner.scoped++
				for _, s := range token.In {
					if owner.scopes == nil {
						owner.scopes = map[string]int{}
					}
					owner.scopes[s]++
				}
			}
			if isToken && token.Out != "" || c.hops(key) {
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
			c.value(child, st.Value)
			if b := jomini.BlockOf(st.Value); b != nil {
				c.block(fo, child, next, inside, b.Statements)
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

// keyed is the position a key stands at under n: <number>, <date> or <cite> by its spelling, else the key itself.
func (c *census) keyed(n *node, key string, quoted bool, seen map[*node]bool) *node {
	if !quoted {
		switch jomini.Literal(key) {
		case jomini.LitNumber:
			return c.member(n, "<number>", seen)
		case jomini.LitDate:
			return c.member(n, "<date>", seen)
		}
		if cited, ok := jomini.Cite(key); ok && cited.Chain == "" {
			c.cite(cited)
			return c.member(n, "<cite>", seen)
		}
	}
	return c.member(n, key, seen)
}

// member is the child of n for a key, counted once per block in its in. Past maxMembers a declared token's child is the census of that token, and any other key shares <name>.
func (c *census) member(n *node, key string, seen map[*node]bool) *node {
	child := n.members[key]
	if child == nil && len(n.members) >= maxMembers && !strings.HasPrefix(key, "<") {
		if n.names == nil {
			n.names = map[string]int{}
		}
		n.names[key]++
		if _, isToken := c.token(key); isToken {
			child = c.tokens[strings.ToLower(key)]
			if child == nil {
				child = &node{}
				c.tokens[strings.ToLower(key)] = child
			}
		} else {
			key, child = "<name>", n.members["<name>"]
		}
	}
	if child == nil {
		if n.members == nil {
			n.members = map[string]*node{}
		}
		child = &node{}
		n.members[key] = child
	}

	child.count++
	if !seen[child] {
		seen[child], child.in = true, child.in+1
	}
	return child
}

// value counts what is written as a value at n.
func (c *census) value(n *node, v jomini.Value) {
	switch v := v.(type) {
	case *jomini.Block:
		n.blocks++
	case *jomini.TaggedBlock:
		n.blocks++
		if n.tags == nil {
			n.tags = map[string]int{}
		}
		n.tags[v.Tag.Text]++
	case *jomini.Scalar:
		if v.Quoted {
			n.quoted++
		}
		switch kind := jomini.Literal(v.Text); {
		case kind == jomini.LitNumber && !v.Quoted:
			n.numbers++
		case kind == jomini.LitDate:
			n.dates++
		case !v.Quoted && (v.Text == "yes" || v.Text == "no"):
			n.bools++
		}
		if len(jomini.Interpolations(v.Text)) > 0 {
			n.params++
		}
		if cited, ok := jomini.Cite(v.Text); ok && cited.Chain == "" {
			c.cite(cited)
		}

		if n.words == nil {
			n.words = map[string]int{}
		}
		if _, known := n.words[v.Text]; known || len(n.words) < maxWords || len(n.words)*2 > n.count {
			n.words[v.Text]++
		}
	}
}

// cite counts a name written under a prefix.
func (c *census) cite(cited jomini.Cited) {
	if c.cites[cited.Prefix] == nil {
		c.cites[cited.Prefix] = map[string]int{}
	}
	name := cited.Name
	if cited.Member != "" {
		name = cited.Member
	}
	c.cites[cited.Prefix][name]++
}

// token is the effect or trigger a key names, looked up lower-cased as the reader does.
func (c *census) token(key string) (schema.EngineToken, bool) {
	name := strings.ToLower(key)
	if t, ok := c.vocab.Effects[name]; ok {
		return t, true
	}
	t, ok := c.vocab.Triggers[name]
	return t, ok
}

// hops reports a key that moves the scope: a declared link, a path of them, a cite with a chain, or a parameter, which stands for one of those where a macro's text is written ($ACTOR$ = { }).
func (c *census) hops(key string) bool {
	if len(jomini.Interpolations(key)) > 0 {
		return true
	}
	first, _, _ := strings.Cut(key, ".")
	if _, isLink := c.vocab.Links[first]; isLink {
		return true
	}
	_, isCite := jomini.Cite(key)
	return isCite
}
