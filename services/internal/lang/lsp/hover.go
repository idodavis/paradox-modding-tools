// hover.go answers Hover and SignatureHelp: one probe, one identity, one card
// filled by the identity's class, so what a card says is decided by the class,
// never by the order the branches happened to be written in.

package lsp

import (
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

const inspectValueCap = 8

// Hover resolves the identifier at (line, UTF-8 column) and fills its card, or returns nil where there is nothing to say.
func Hover(s *session.Session, path string, line, col int) *Inspect {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	id, ok := identify(s, path, at)
	if !ok {
		return nil
	}
	ins := card(s, id)
	if ins == nil {
		return nil
	}
	ins.Scope = at.scope

	// A scope link is the one token whose whole purpose is to move somewhere else, so the transition is the useful thing to show.
	if l, ok := s.Install.Declared.Link(id.name); ok && l.Out != "" {
		ins.ScopeOut = l.Out
	}
	if ins.Owner == "" {
		ins.Owner = id.owner
	}
	return ins
}

// card fills the one card an identity class gets.
func card(s *session.Session, id identity) *Inspect {
	switch id.class {
	case idLocValue:
		ins := inspectOf(s, "loc_value", "$"+id.name+"$", "")
		ins.Body = "format " + id.locFilter
		return ins
	case idParam:
		ins := inspectOf(s, jomini.KindScriptParam, "$"+id.name+"$", "")
		owner := id.owner
		if owner == "" && id.def != nil {
			owner = id.def.OwnerKey
		}
		ins.Owner = owner
		ins.Values, ins.More = rankInspectValues(s.ValuesOf(jomini.KindScriptParam, owner, id.name))
		if id.def != nil {
			attachSite(s, ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
		}
		return ins
	case idProse:
		return &Inspect{Kind: id.kind, Key: id.name, Docs: id.docs}
	case idEphemeral:
		// Its defining site when the workspace has one, and the values the corpus writes for the name.
		key := id.name
		if id.kind == "saved_scope" {
			key = "scope:" + id.name
		}
		ins := inspectOf(s, id.kind, key, "")
		ins.Values, ins.More = rankInspectValues(s.ValuesOf(id.kind, "", id.name))
		if id.def != nil {
			attachSite(s, ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
		}
		return ins
	case idLocalDef:
		docs := fieldDoc(s, id.name, id.extractKind, id.block)
		if docs == "" && !isMember(s, id.hang, id.name) {
			return nil
		}
		ins := inspectOf(s, id.extractKind, id.name, docs)
		ins.Kind += " key"
		origin, _, _ := s.Locate(id.path)
		attachSite(s, ins, id.path, id.assignLine, origin, id.assignStart)
		return ins
	case idField:
		return fieldCard(s, id)
	case idLocKey:
		text, _ := s.DefaultLoc(id.name)
		file, line, start, origin, ok := s.LocSite(id.name)
		if text == "" && !ok {
			return nil
		}
		ins := inspectOf(s, "loc_key", id.name, "")
		ins.Body = unescapeLocDisplay(text)
		if ok {
			// start, not 0: attachSite computes Col only when start > 0, and the client gates the selection on Col.
			attachSite(s, ins, file, line, origin, start)
			attachOverlay(s, ins, &index.Def{Kind: "loc_key", Key: id.name, Path: file, Line: line, Origin: origin})
		}
		return ins
	case idDef:
		d := id.def
		docs := docsOnly(s, id.name, d.Kind)

		// A named script body has no other description: the author's comment above it is the only one there will ever be.
		if docs == "" && d.Doc != "" {
			docs = d.Doc
		}
		ins := inspectOf(s, d.Kind, d.Key, docs)
		ins.Usage = clipUsage(tokenUsage(s, id.name))
		if ins.Usage == "" {
			ins.Usage = macroUsage(s, d)
		}

		// The name a row carries shows first (a character's), then the localization the kind's convention names; conventions are sorted by coverage, then by the plainest shape.
		if !jomini.IsEphemeral(d.Kind) && d.Value != "" {
			ins.Body = d.Value
			if text, ok := s.DefaultLoc(d.Value); ok && text != "" {
				ins.Body = unescapeLocDisplay(text)
			}
		}
		for _, key := range loc.Expand(d.Key, s.LocConventions(d.Kind)) {
			if ins.Body != "" {
				break
			}
			if text, ok := s.DefaultLoc(key); ok && text != "" {
				ins.Body = unescapeLocDisplay(text)
				break
			}
		}
		attachSite(s, ins, d.Path, d.Line, d.Origin, d.Start)
		attachOverlay(s, ins, d)
		return ins
	case idToken:
		ins := inspectOf(s, id.kind, id.name, docsOnly(s, id.name, id.kind))
		ins.Usage = clipUsage(tokenUsage(s, id.name))
		_, ins.Accepts, _ = declaredLines(s, id.kind, id.hang, id.name)
		return vanillaSite(s, ins)
	case idSlotValue:
		// The floor: a value nothing in the model names, described by the key that holds it. `add_character_flag = born_as_taltos` says what born_as_taltos is even though no database defines it.
		ins := inspectOf(s, id.slot, id.name, fieldDoc(s, id.slot, id.extractKind, id.block))
		ins.Kind += " value"
		ins.Hint = ""
		_, ins.Accepts, _ = declaredLines(s, id.extractKind, id.hang, id.slot)
		return ins
	}
	return nil
}

// fieldCard is a key written inside a block, named by the block type at that position: a structure shows its own (structure, field) prose, and a body holds the arguments of the token that opens it rather than keys of a container. The structure is the unit the game documents and the unit the schema unifies, so it is also the label.
func fieldCard(s *session.Session, id identity) *Inspect {
	structure := id.block

	// A folded segment names a position rather than a structure a reader could type, so the written container stands in for it.
	if id.hang != nil && !strings.ContainsAny(id.hang.Struct, "<.") {
		structure = id.hang.Struct
	}
	label, suffix := structure, " key"
	if label == "" {
		label = id.extractKind
	}

	// A block that is itself a declared token holds that token's arguments: "set variable key" reads as though set_variable were a container.
	if s.Install.Declared.HasName(structure) {
		suffix = " argument"
	}
	ins := inspectOf(s, label, id.name, fieldDoc(s, id.name, id.extractKind, structure))
	ins.Kind += suffix

	// The hint describes the KIND, none of which is about the field under the cursor.
	ins.Hint = ""

	// A key can be a declared token as well: `add_to_list` is also a structure key of the kind being edited, and the signature the game states for it was being dropped.
	ins.Usage = clipUsage(tokenUsage(s, id.name))
	_, ins.Accepts, _ = declaredLines(s, id.extractKind, id.hang, id.name)
	return vanillaSite(s, ins)
}

// inspectOf fills Kind/Hint/Docs for a resolved identity.
func inspectOf(s *session.Session, kind, key, docs string) *Inspect {
	out := &Inspect{Kind: PrettyKind(kind), Key: key, Docs: docs}
	if docs == "" {
		_, _, out.Hint = declaredLines(s, kind, nil, "")
	}
	return out
}

// ---------------------------------------------------------------- the sites

func attachSite(s *session.Session, ins *Inspect, path string, line int, origin string, start int) {
	if ins == nil || path == "" {
		return
	}
	ins.Path = path
	ins.Rel = s.DisplayRel(path)
	ins.Line = line
	if origin == "" {
		origin = game.OriginVanilla
	}
	ins.Origin = origin
	ins.OriginName = s.OriginName(origin)
	if start > 0 {
		ins.Col = s.Parsed(path).Lines().PositionAt(start).Character
	}
}

func attachOverlay(s *session.Session, ins *Inspect, d *index.Def) {
	if ins == nil || d == nil || isVanillaOrigin(d.Origin) || d.Kind == "mod_descriptor" {
		return
	}
	var v *index.Def
	for _, cand := range s.VanillaDefs(d.Key) {
		if cand.Kind == d.Kind {
			c := cand
			v = &c
			break
		}
	}
	if v == nil || v.Path == "" || index.SamePath(v.Path, d.Path) {
		return
	}
	ins.VanillaPath = v.Path
	ins.VanillaRel = s.DisplayRel(v.Path)
	ins.VanillaLine = v.Line
	ins.VanillaOriginName = s.OriginName(game.OriginVanilla)
	if v.Start > 0 {
		ins.VanillaCol = s.Parsed(v.Path).Lines().PositionAt(v.Start).Character
	}
}

func isVanillaOrigin(origin string) bool {
	return origin == "" || origin == game.OriginVanilla
}

func vanillaSite(s *session.Session, ins *Inspect) *Inspect {
	if ins == nil {
		return nil
	}
	ins.Origin = game.OriginVanilla
	ins.OriginName = s.OriginName(game.OriginVanilla)
	return ins
}

func rankInspectValues(counts map[string]int) ([]InspectValue, int) {
	type pair struct {
		text string
		n    int
	}
	var ps []pair
	for t, n := range counts {
		if t != "" && n > 0 {
			ps = append(ps, pair{t, n})
		}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.text, b.text)
	})
	more := 0
	if len(ps) > inspectValueCap {
		more = len(ps) - inspectValueCap
		ps = ps[:inspectValueCap]
	}
	out := make([]InspectValue, len(ps))
	for i, p := range ps {
		out[i] = InspectValue{Text: p.text, Count: p.n}
	}
	return out, more
}

func unescapeLocDisplay(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		default:
			b.WriteByte(s[i])
			continue
		}
		i++
	}
	return strings.TrimSpace(b.String())
}

// ------------------------------------------------------------ signature help

var usageParamRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)\$|\{([A-Za-z0-9_]+)\}`)

// SignatureHelp returns usage-based signature help at (line, UTF-8 column).
func SignatureHelp(s *session.Session, path string, line, col int) *SignatureHelpResult {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	key := at.slotKey
	if key == "" {
		key = at.word
	}
	usage := tokenUsage(s, key)
	if usage == "" {
		if d := s.Resolve(key); d != nil && jomini.IsMacroKind(d.Kind) {
			usage = tokenUsage(s, d.Key)
		}
	}
	if usage == "" {
		return nil
	}
	return &SignatureHelpResult{Label: usage, Documentation: fieldDoc(s, key, at.kind, ""), Parameters: paramsFromUsage(usage)}
}

func paramsFromUsage(usage string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range usageParamRe.FindAllStringSubmatch(usage, -1) {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 && strings.Contains(usage, "{") {
		out = append(out, "block")
	}
	return out
}
