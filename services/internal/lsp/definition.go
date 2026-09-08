// definition.go resolves definition, references, rename, and workspace symbols.
package lsp

import (
	"strconv"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/session"
)

// Definition returns go-to-definition locations for the word at pos.
func Definition(s *session.Session, path string, line, col int) []Location {
	ins := s.Inspect(path, line, col)
	if ins == nil || ins.RawKind == "loc_value" || ins.FieldKey {
		return nil
	}
	if ins.RawKind == "loc_key" && ins.Def == nil {
		if ins.SpanEnd > ins.SpanStart {
			return []Location{{
				URI:   path,
				Range: byteRange(jomini.NewLineIndex(s.FileText(path)), ins.SpanStart, ins.SpanEnd),
			}}
		}
		return nil
	}
	if ins.Local {
		if ins.AssignEnd <= ins.AssignStart {
			return nil
		}
		return []Location{defLocation(s, catalog.Def{
			Kind: ins.RawKind, Key: ins.Name, Path: path, Line: ins.AssignLine,
			Start: ins.AssignStart, End: ins.AssignEnd,
		})}
	}
	if ins.Def != nil {
		return definitionSites(s, path, ins.Name, ins.Def)
	}
	return nil
}

// References returns every workspace site of the identifier at pos.
func References(s *session.Session, path string, line, col int) []Location {
	ins := s.Inspect(path, line, col)
	if ins == nil || ins.RawKind == "loc_value" || ins.Name == "" {
		return nil
	}
	if ins.RawKind == "script_param" {
		return identReferencesOwned(s, ins.Name, "script_param", ins.Owner)
	}
	if jomini.IsEphemeral(ins.RawKind) {
		return identReferences(s, ins.Name, ins.RawKind)
	}
	return identReferences(s, ins.Name, "")
}

// definitionSites lists every def of key, FIOS/Resolve winner first.
func definitionSites(
	s *session.Session, from, key string, winner *catalog.Def,
) []Location {
	seen := map[string]bool{}
	here := session.CanonPath(from)
	var out []Location
	// An ephemeral winner (saved scope, script param) confines the sites to its
	// own kind; a real def does not, so this is resolved once up front.
	ephemeralKind := ""
	if winner != nil && jomini.IsEphemeral(winner.Kind) {
		ephemeralKind = jomini.CanonicalKind(winner.Kind)
	}
	// Same reasoning, one kind further: `namespace = conqueror` is not where the
	// `conqueror` trait is defined. Left in, F12 on the trait returned two
	// places and the editor opened a peek listing instead of jumping.
	wantNamespace := winner != nil && jomini.CanonicalKind(winner.Kind) == "namespace"
	add := func(d catalog.Def) {
		if d.Key != key {
			return
		}
		// A definition the other file declares inline is invisible to this one:
		// the engine resolves an inline `scripted_trigger foo = { ... }` within
		// its own file, which is why mods carry the same one in several event
		// files. Offering it turned F12 into a peek listing two bodies, one of
		// which this file cannot call. See catalog.Def.Local.
		if d.Local && session.CanonPath(d.Path) != here {
			return
		}
		if ephemeralKind != "" && jomini.CanonicalKind(d.Kind) != ephemeralKind {
			return
		}
		if !wantNamespace && jomini.CanonicalKind(d.Kind) == "namespace" {
			return
		}
		if winner != nil && winner.OwnerKey != "" && d.OwnerKey != "" &&
			d.OwnerKey != winner.OwnerKey {
			return
		}
		loc := defLocation(s, d)
		// Canonicalise the path before deduping, exactly as find-references
		// does. The same definition reaches this function from several sources
		// and they do not agree on how to spell a Windows path, so comparing
		// the raw string let one site through twice — and two results for one
		// place makes the editor open a peek listing the current file instead
		// of simply jumping.
		id := session.CanonPath(loc.URI) + ":" +
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
	if winner != nil {
		add(*winner)
	}
	for _, d := range s.ModDefsOf(key) {
		add(d)
	}
	for _, d := range s.VanillaDefs(key) {
		if winner != nil && d.Kind != winner.Kind {
			continue
		}
		add(d)
	}
	for _, d := range s.FindDefs(key, 64, false, true) {
		if d.Key == key {
			add(d)
		}
	}
	return out
}

func defLocation(s *session.Session, d catalog.Def) Location {
	loc := Location{URI: d.Path}
	if d.End > d.Start {
		if src := s.FileText(d.Path); src != "" {
			li := jomini.NewLineIndex(src)
			loc.Range = byteRange(li, d.Start, d.End)
			if b := assignmentBlock(s, d); b != nil {
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

func assignmentBlock(s *session.Session, d catalog.Def) *jomini.Block {
	res := s.Parsed(d.Path)
	if res.Root == nil {
		return nil
	}
	if d.Start > 0 {
		chain := jomini.NodeAtOffset(res.Root, d.Start)
		for i := len(chain) - 1; i >= 0; i-- {
			a, ok := chain[i].(*jomini.Assignment)
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

// identReferences lists defs and refs of word. kind "" keeps all kinds;
// a non-empty kind filters to CanonicalKind(kind) (ephemeral F12/rename).
func identReferences(s *session.Session, word, kind string) []Location {
	return identReferencesOwned(s, word, kind, "")
}

func identReferencesOwned(s *session.Session, word, kind, owner string) []Location {
	seen := map[string]bool{}
	var out []Location
	add := func(loc Location) {
		k := session.CanonPath(loc.URI) + ":" +
			strconv.Itoa(loc.Range.Start.Line) + ":" +
			strconv.Itoa(loc.Range.Start.Character) + ":" +
			strconv.Itoa(loc.Range.End.Line) + ":" +
			strconv.Itoa(loc.Range.End.Character)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, loc)
	}
	wantKind := jomini.CanonicalKind(kind)
	kindOK := func(k string) bool {
		return kind == "" || jomini.CanonicalKind(k) == wantKind
	}
	ownerOK := func(okey string) bool {
		return owner == "" || okey == "" || okey == owner
	}
	lis := map[string]*jomini.LineIndex{}
	lineIndex := func(p string) *jomini.LineIndex {
		if li, ok := lis[p]; ok {
			return li
		}
		src := s.FileText(p)
		if r, ok := s.Result(p); ok && r.Src == src {
			lis[p] = r.Lines()
			return lis[p]
		}
		lis[p] = jomini.NewLineIndex(src)
		return lis[p]
	}
	hasLocDef := false
	for _, d := range s.ModDefsOf(word) {
		if !kindOK(d.Kind) || !ownerOK(d.OwnerKey) {
			continue
		}
		add(defLocation(s, d))
		if d.Kind == "loc_key" {
			hasLocDef = true
		}
	}
	for _, d := range s.VanillaDefs(word) {
		if !kindOK(d.Kind) || !ownerOK(d.OwnerKey) {
			continue
		}
		add(defLocation(s, d))
		if d.Kind == "loc_key" {
			hasLocDef = true
		}
	}
	if kind == "" && !hasLocDef {
		if file, ln, _, ok := s.LocSite(word); ok {
			add(defLocation(s, catalog.Def{
				Kind: "loc_key", Key: word, Path: file, Line: ln,
			}))
		}
	}
	for _, r := range s.RefsTo(word) {
		if r.Key != word || !kindOK(r.Kind) || !ownerOK(r.OwnerKey) {
			continue
		}
		add(Location{URI: r.Path, Range: byteRange(lineIndex(r.Path), r.Start, r.End)})
	}
	return out
}

// Rename returns edits to rename the identifier at pos, or nil if none.
func Rename(s *session.Session, path string, line, col int, newName string) *WorkspaceEdit {
	if newName == "" {
		return nil
	}
	locs := References(s, path, line, col)
	if len(locs) == 0 {
		return nil
	}
	changes := map[string][]TextEdit{}
	for _, loc := range locs {
		if _, _, ok := s.Locate(loc.URI); !ok {
			continue
		}
		changes[loc.URI] = append(changes[loc.URI], TextEdit{Range: loc.Range, NewText: newName})
	}
	if len(changes) == 0 {
		return nil
	}
	return &WorkspaceEdit{Changes: changes}
}

const maxWorkspaceSymbols = 200

// DocumentSymbols returns definitions declared in path.
func DocumentSymbols(s *session.Session, path string) []SymbolInformation {
	var out []SymbolInformation
	for _, d := range s.DefsInFile(path) {
		if jomini.IsEphemeral(d.Kind) {
			continue
		}
		out = append(out, SymbolInformation{Name: d.Key, Location: defLocation(s, d)})
	}
	return out
}

// WorkspaceSymbols searches definitions whose keys contain query.
func WorkspaceSymbols(s *session.Session, query string) []SymbolInformation {
	var out []SymbolInformation
	for _, d := range s.FindDefs(query, maxWorkspaceSymbols, false, false) {
		out = append(out, SymbolInformation{
			Name: d.Key, Location: defLocation(s, d), ContainerName: d.Kind,
		})
	}
	return out
}
