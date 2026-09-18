// definition.go resolves definition, references, and workspace symbols.
package lsp

import (
	"slices"
	"strconv"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

// Definition returns go-to-definition locations for the word at pos. It reads
// the probe's identity directly and never fills a hover card.
func Definition(s *session.Session, path string, line, col int) []Location {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	id, ok := identify(s, path, at)
	if !ok || id.kind == "loc_value" || id.fieldKey {
		return nil
	}
	c := newFileCache(s)
	if id.kind == "loc_key" && id.def == nil {
		if id.spanEnd > id.spanStart {
			return []Location{{
				URI:   path,
				Range: byteRange(jomini.NewLineIndex(s.FileText(path)), id.spanStart, id.spanEnd),
			}}
		}
		return nil
	}
	if id.local {
		if id.assignEnd <= id.assignStart {
			return nil
		}
		return []Location{defLocation(c, index.Def{
			Kind: id.kind, Key: id.name, Path: path, Line: id.assignLine,
			Start: id.assignStart, End: id.assignEnd,
		})}
	}
	if id.def != nil {
		return siteLocations(c, path, id.name, id.def, "", "", false)
	}
	return nil
}

// References returns every workspace site of the identifier at pos.
func References(s *session.Session, path string, line, col int) []Location {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	id, ok := identify(s, path, at)
	if !ok || id.kind == "loc_value" {
		return nil
	}
	c := newFileCache(s)
	if id.kind == "script_param" {
		return siteLocations(c, path, id.name, nil, "script_param", ownerOf(id), true)
	}
	if jomini.IsEphemeral(id.kind) {
		return siteLocations(c, path, id.name, nil, id.kind, "", true)
	}
	return siteLocations(c, path, id.name, nil, "", "", true)
}

// ownerOf is the script_param owner: the identity's own, or its def's.
func ownerOf(id identity) string {
	if id.owner != "" {
		return id.owner
	}
	if id.def != nil {
		return id.def.OwnerKey
	}
	return ""
}

// siteLocations answers both Definition and References from one site walk.
// A winner (F12 on a resolved def) confines results to its own kind and
// owner and excludes namespace/local mismatches; refs additionally adds call
// sites and the loc-site fallback instead of the broader FindDefsOf search.
func siteLocations(
	c *fileCache, from, key string, winner *index.Def, kind, owner string, refs bool,
) []Location {
	s := c.s
	seen := map[string]bool{}
	here := index.CanonPath(from)
	var out []Location
	addLoc := func(loc Location) {
		// Canonicalise the path before deduping: the same definition reaches
		// this function from several sources that do not agree on how to spell
		// a Windows path, and two results for one place makes the editor open
		// a peek listing the current file instead of simply jumping.
		id := index.CanonPath(loc.URI) + ":" +
			strconv.Itoa(loc.Range.Start.Line) + ":" +
			strconv.Itoa(loc.Range.Start.Character) + ":" +
			strconv.Itoa(loc.Range.End.Line) + ":" +
			strconv.Itoa(loc.Range.End.Character)
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, loc)
	}
	// An ephemeral winner (saved scope, script param) or an explicit ephemeral
	// kind confines the sites to its own kind; a real def does not.
	wantKind := kind
	if winner != nil && jomini.IsEphemeral(winner.Kind) {
		wantKind = winner.Kind
	}
	ck := jomini.CanonicalKind(wantKind)
	kindOK := func(k string) bool { return wantKind == "" || jomini.CanonicalKind(k) == ck }
	ownerKey := owner
	if ownerKey == "" && winner != nil {
		ownerKey = winner.OwnerKey
	}
	ownerOK := func(okey string) bool { return ownerKey == "" || okey == "" || okey == ownerKey }
	wantNamespace := winner != nil && jomini.CanonicalKind(winner.Kind) == "namespace"
	add := func(d index.Def) {
		if d.Key != key || !kindOK(d.Kind) || !ownerOK(d.OwnerKey) {
			return
		}
		// F12 only: References does not confine an inline declaration to its own
		// file, and does not hide the namespace line (`namespace = conqueror` is
		// not where the `conqueror` trait is defined, but it is still a site).
		if !refs {
			if d.Local && index.CanonPath(d.Path) != here {
				return
			}
			if !wantNamespace && jomini.CanonicalKind(d.Kind) == "namespace" {
				return
			}
		}
		addLoc(defLocation(c, d))
	}
	if winner != nil {
		add(*winner)
	}
	hasLocDef := false
	for _, d := range s.ModDefs(key) {
		add(d)
		hasLocDef = hasLocDef || d.Kind == "loc_key"
	}
	for _, d := range s.VanillaDefs(key) {
		if winner != nil && d.Kind != winner.Kind {
			continue
		}
		add(d)
		hasLocDef = hasLocDef || d.Kind == "loc_key"
	}
	if !refs {
		for _, d := range s.FindDefsOf(key, "", 64, false, true) {
			if d.Key == key {
				add(d)
			}
		}
		return out
	}
	if kind == "" && !hasLocDef {
		if file, ln, _, _, ok := s.LocSite(key); ok {
			add(index.Def{Kind: "loc_key", Key: key, Path: file, Line: ln})
		}
	}
	for _, r := range s.RefsTo(key) {
		if r.Key != key || !kindOK(r.Kind) || !ownerOK(r.OwnerKey) {
			continue
		}
		addLoc(Location{URI: r.Path, Range: byteRange(c.lines(r.Path), r.Start, r.End)})
	}
	return out
}

// fileCache memoises one file's parse per Definition/References request, so
// an answer with N results parses or indexes each backing file at most once.
type fileCache struct {
	s    *session.Session
	res  map[string]jomini.Result
	line map[string]*jomini.LineIndex
}

func newFileCache(s *session.Session) *fileCache {
	return &fileCache{s: s, res: map[string]jomini.Result{}, line: map[string]*jomini.LineIndex{}}
}

func (c *fileCache) parsed(path string) jomini.Result {
	if r, ok := c.res[path]; ok {
		return r
	}
	r := c.s.Parsed(path)
	c.res[path] = r
	return r
}

func (c *fileCache) lines(path string) *jomini.LineIndex {
	if li, ok := c.line[path]; ok {
		return li
	}
	li := c.parsed(path).Lines()
	c.line[path] = li
	return li
}

func defLocation(c *fileCache, d index.Def) Location {
	loc := Location{URI: d.Path}
	if d.End > d.Start {
		res := c.parsed(d.Path)
		if res.Src != "" {
			li := c.lines(d.Path)
			loc.Range = byteRange(li, d.Start, d.End)
			if b := assignmentBlock(res, d); b != nil {
				tr := byteRange(li, d.Start, b.Range.End)
				loc.TargetRange = &tr
			}
			return loc
		}
	}
	loc.Range = Range{
		Start: Position{Line: d.Line, Character: 0},
		End:   Position{Line: d.Line, Character: len(d.Key)},
	}
	return loc
}

func assignmentBlock(res jomini.Result, d index.Def) *jomini.Block {
	if res.Root == nil {
		return nil
	}
	if d.Start > 0 {
		chain := jomini.NodeAtOffset(res.Root, d.Start)
		for _, c := range slices.Backward(chain) {
			a, ok := c.(*jomini.Assignment)
			if !ok || a.Key.Quoted || a.Key.Text != d.Key {
				continue
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				return b
			}
		}
	}
	return jomini.BlockForKey(res.Root, d.Key)
}

const maxWorkspaceSymbols = 200

// DocumentSymbols returns definitions declared in path.
func DocumentSymbols(s *session.Session, path string) []SymbolInformation {
	c := newFileCache(s)
	var out []SymbolInformation
	for _, d := range s.DefsInFile(path) {
		if jomini.IsEphemeral(d.Kind) {
			continue
		}
		out = append(out, SymbolInformation{Name: d.Key, Location: defLocation(c, d)})
	}
	return out
}

// WorkspaceSymbols searches definitions whose keys contain query.
func WorkspaceSymbols(s *session.Session, query string) []SymbolInformation {
	c := newFileCache(s)
	var out []SymbolInformation
	for _, d := range s.FindDefsOf(query, "", maxWorkspaceSymbols, false, false) {
		out = append(out, SymbolInformation{
			Name: d.Key, Location: defLocation(c, d), ContainerName: d.Kind,
		})
	}
	return out
}
