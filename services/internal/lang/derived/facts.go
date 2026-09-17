// facts.go is the one product of parsing a file: the flat Fact array a
// pre-order walk produces, and the interner that folds the corpus's repeated
// keys and values into one copy each so a file's text is released with its
// parse tree.

package derived

import (
	"path/filepath"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// Fact is one statement of a parsed file: an assignment (Key set) or a bare value (Key ""). Start/End address the key token, or the bare value; VStart/VEnd the scalar right-hand side. The children of a block are the facts whose Parent is its index. Field order is by width: an install is tens of millions of facts and the padding is real.
type Fact struct {
	Key    string // assignment key as written; "" for a bare value statement
	Op     string // "=", "?=", "==", "<", ...; "" for a bare value (defs need "=" or "?=")
	Value  string // scalar RHS or bare value; "" when the RHS is a block
	Parent int32  // index of the enclosing assignment fact; -1 at top level
	// Line/Start/End and VLine/VStart/VEnd are the two token spans a row turns into a Def or a Ref.
	Line, Start, End    int32
	VLine, VStart, VEnd int32
	Block               bool // the RHS is a block, tagged (rgb/hsv) or plain
	// Quoted is the key's quoting (the bare value's, for a value statement), VQuoted the scalar RHS's: a quoted key is never a definition and a quoted value is never a citation, and the two answers differ within one statement.
	Quoted, VQuoted bool
	Depth           uint8
}

// internShards is how many maps the interner is split across: one map behind one lock turned eight workers into one, and cost more time than the strings saved.
const internShards = 64

// interner hands out one shared copy of every key and value in the corpus. A fact's strings are slices of the file's text, so one definition, one counter or one candidate pins that whole file until the model is dropped; interning the facts as they arrive frees a file's text with its parse tree and folds the corpus's repeats into one copy each.
type interner [internShards]struct {
	mu   sync.RWMutex
	seen map[string]string
	_    [24]byte // keeps two shards' locks off one cache line
}

func (in *interner) of(s string) string {
	if s == "" {
		return s
	}
	// A sample of the string rather than all of it: the map hashes the whole thing again anyway, and these are short.
	h := uint(len(s))*131 + uint(s[0])
	h = h*131 + uint(s[len(s)-1])
	sh := &in[h&(internShards-1)]
	sh.mu.RLock()
	c, ok := sh.seen[s]
	sh.mu.RUnlock()
	if ok {
		return c
	}
	c = strings.Clone(s)
	sh.mu.Lock()
	if was, ok := sh.seen[c]; ok {
		c = was
	} else {
		if sh.seen == nil {
			sh.seen = map[string]string{}
		}
		sh.seen[c] = c
	}
	sh.mu.Unlock()
	return c
}

// facts interns the two fields every row reads, which is every string a file's harvest goes on to keep.
func (in *interner) facts(ff *FileFacts) {
	for i := range ff.Facts {
		ff.Facts[i].Key = in.of(ff.Facts[i].Key)
		ff.Facts[i].Value = in.of(ff.Facts[i].Value)
	}
}

// fileID is what a definition or a reference needs to know about the file it came from; Rule is game.MatchExtract's answer for it.
type fileID struct {
	GameID, Path, Origin string
	Rule                 game.ExtractRule
	// Order is the file's index in the walk list, i.e. its load order.
	Order int
}

// FileFacts is the one product of parsing a file. Src stays because a leading comment is not a statement and so cannot be a row over Facts.
type FileFacts struct {
	fileID
	Src   string
	Facts []Fact
}

// FactsOf turns one parsed file into its facts, in one jomini.Walk.
func FactsOf(gameID, path, rel, origin string, res jomini.Result) FileFacts {
	ff := FileFacts{
		fileID: fileID{
			GameID: gameID, Path: filepath.Clean(path), Origin: origin,
			Rule: game.MatchExtract(gameID, rel),
		},
		Src: res.Src,
	}
	if res.Root == nil {
		return ff
	}
	// A statement is about twelve bytes of script, so the source sizes the slice and append never has to copy it.
	ff.Facts = make([]Fact, 0, len(res.Src)/12+8)
	// A pre-order walk asks for offsets in source order, so the line is a cursor over Src rather than a binary search per token. The break rule is NewLineIndex's: a newline, or a carriage return not part of one.
	src, line, at := res.Src, int32(0), 0
	lineAt := func(off int32) int32 {
		for ; at < int(off) && at < len(src); at++ {
			if c := src[at]; c == '\n' ||
				(c == '\r' && (at+1 == len(src) || src[at+1] != '\n')) {
				line++
			}
		}
		return line
	}
	// open[d] is the fact open at depth d; jomini.Walk is pre-order, so it parents everything arriving at d+1.
	var open []int32
	jomini.Walk(res.Root, func(st jomini.Statement, depth int, _ *jomini.Block) bool {
		f := Fact{Parent: -1, Depth: uint8(min(depth, 255))}
		var val jomini.Value
		if depth > 0 && depth <= len(open) {
			f.Parent = open[depth-1]
		}
		switch n := st.(type) {
		case *jomini.Assignment:
			f.Key, f.Op, f.Quoted = n.Key.Text, n.Op, n.Key.Quoted
			f.Line = lineAt(int32(n.Key.Range.Start))
			f.Start, f.End = int32(n.Key.Range.Start), int32(n.Key.Range.End)
			val = n.Value
		case *jomini.ValueStmt:
			f.Line = lineAt(int32(n.Range.Start))
			f.Start, f.End = int32(n.Range.Start), int32(n.Range.End)
			val = n.Value
		}
		switch v := val.(type) {
		case *jomini.Scalar:
			f.Value, f.VQuoted = v.Text, v.Quoted
			f.VLine = lineAt(int32(v.Range.Start))
			f.VStart, f.VEnd = int32(v.Range.Start), int32(v.Range.End)
			if f.Key == "" {
				f.Quoted, f.Line, f.Start, f.End = v.Quoted, f.VLine, f.VStart, f.VEnd
			}
		case *jomini.TaggedBlock, *jomini.Block:
			f.Block = true
		}
		for len(open) <= depth {
			open = append(open, -1)
		}
		open[depth] = int32(len(ff.Facts))
		ff.Facts = append(ff.Facts, f)
		return true
	})
	return ff
}

// A row reads the facts under i as `for j := i + 1; j < len(facts) && facts[j].Parent >= i; j++`: a pre-order walk makes a subtree contiguous, and the first fact past it is a sibling or an uncle, whose parent is therefore older than i.
func (id fileID) def(kind, key string, line, start, end int32) Def {
	return Def{
		Kind: jomini.CanonicalKind(kind), Key: key, Path: id.Path,
		Line: int(line), Start: int(start), End: int(end), Origin: id.Origin,
	}
}

func (id fileID) ref(kind, key string, line, start, end int32) Ref {
	return Ref{
		Key: key, Kind: kind, Path: id.Path,
		Line: int(line), Start: int(start), End: int(end),
	}
}
