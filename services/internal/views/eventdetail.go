// eventdetail.go is the read-only inspector for one resolved graph def.

package views

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

const (
	maxBlockLines = 60
	maxTargets    = 40
)

var (
	sectionKeys = map[string]bool{
		"trigger": true, "immediate": true, "after": true,
		"on_trigger_fail": true, "cancellation_trigger": true,
	}
	optionNonEffect = map[string]bool{
		"name": true, "trigger": true, "ai_chance": true, "ai_value": true,
	}
)

// Detail returns inspector content for one resolved def, or nil if it is unknown.
func Detail(s *session.Session, id string) *EventDetail {
	d := s.Resolve(id)
	if d == nil {
		return nil
	}
	kind := jomini.CanonicalKind(d.Kind)
	res := s.Parsed(d.Path)
	if res.Root == nil {
		if kind == "event" {
			return nil
		}
		return &EventDetail{
			ID: id, Kind: kind, Path: d.Path, Rel: s.DisplayRel(d.Path),
			Origin: originID(d.Origin), OriginName: s.OriginName(originID(d.Origin)),
			Line: d.Line, Title: locIfAny(s, id),
			RefGroups: refGroups(s, d.Path), Incoming: incomingEdges(s, id),
		}
	}
	li := res.Lines()
	lineOf := func(off int) int { return li.PositionAt(off).Line }
	var stmt *jomini.Assignment
	for _, st := range res.Root.Statements {
		a, ok := st.(*jomini.Assignment)
		if ok && a.Key.Text == id && jomini.BlockOf(a.Value) != nil {
			stmt = a
			break
		}
	}
	if stmt == nil {
		if kind == "event" {
			return nil
		}
		return &EventDetail{
			ID: id, Kind: kind, Path: d.Path, Rel: s.DisplayRel(d.Path),
			Origin: originID(d.Origin), OriginName: s.OriginName(originID(d.Origin)),
			Line: d.Line, Title: locIfAny(s, id),
			RefGroups: refGroups(s, d.Path), Incoming: incomingEdges(s, id),
		}
	}
	block := jomini.BlockOf(stmt.Value)
	lines, targets, fields := inspectBlock(s, block, lineOf, nil)
	detail := &EventDetail{
		ID: id, Kind: kind, Path: d.Path, Rel: s.DisplayRel(d.Path),
		Origin: originID(d.Origin), OriginName: s.OriginName(originID(d.Origin)),
		Line: lineOf(stmt.Key.Range.Start), Fields: fields,
		RefGroups: refGroups(s, d.Path), Incoming: incomingEdges(s, id),
	}
	if kind == "event" {
		detail.Namespace = eventNamespace(s, d, id)
	}
	if kind != "event" {
		detail.Title = locIfAny(s, id)
		detail.Sections = []EventSectionInfo{{
			Name: kind, Lines: lines, Targets: targets,
		}}
		return detail
	}
	for _, ch := range block.Statements {
		a, ok := ch.(*jomini.Assignment)
		if !ok {
			continue
		}
		key := strings.ToLower(a.Key.Text)
		scalar, _ := a.Value.(*jomini.Scalar)
		sub := jomini.BlockOf(a.Value)
		switch {
		case key == "type" && scalar != nil:
			detail.Type = scalar.Text
		case key == "hidden" && scalar != nil:
			detail.Hidden = scalar.Text == "yes"
		case key == "theme" && scalar != nil:
			detail.Theme = scalar.Text
		case key == "title":
			detail.Title = locField(s, a)
		case key == "desc":
			detail.Desc = locField(s, a)
		case key == "flavor":
			detail.Flavor = locField(s, a)
		case sectionKeys[key] && sub != nil:
			detail.Sections = append(detail.Sections, sectionOf(s, a.Key.Text, sub, lineOf))
		case key == "option" && sub != nil:
			detail.Options = append(detail.Options, optionOf(s, sub, lineOf))
		}
	}
	return detail
}

func refGroups(s *session.Session, path string) []RefKindGroup {
	refs := s.RefsInFile(path)
	if len(refs) == 0 {
		return nil
	}
	byKind := map[string][]EventRefInfo{}
	for _, r := range refs {
		info := EventRefInfo{Name: r.Key, Kind: r.Kind, Line: r.Line}
		if d := s.Resolve(r.Key); d != nil {
			info.DefFile, info.DefLine = d.Path, d.Line
		}
		byKind[r.Kind] = append(byKind[r.Kind], info)
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	out := make([]RefKindGroup, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, RefKindGroup{Kind: k, Refs: byKind[k]})
	}
	return out
}

func incomingEdges(s *session.Session, id string) []EventGraphEdge {
	edges := s.EdgesTo(id)
	if len(edges) == 0 {
		return nil
	}
	out := make([]EventGraphEdge, 0, len(edges))
	for _, e := range edges {
		if e.From == id {
			continue
		}
		out = append(out, labeledEdge(s, e))
	}
	return out
}

func viaHopLabel(chain string) string {
	parts := strings.Split(chain, " → ")
	if len(parts) <= 2 {
		return "via " + chain
	}
	return "via " + parts[0] + " … " + parts[len(parts)-1]
}

func labeledEdge(s *session.Session, e derived.Edge) EventGraphEdge {
	out := EventGraphEdge{From: e.From, To: e.To, Via: e.Via, Kind: e.Kind}
	if e.Kind == "via" {
		chain := e.NameKey
		if chain == "" {
			chain = e.Via
		}
		if chain != "" {
			out.Via = chain
			out.Label = viaHopLabel(chain)
		}
		return out
	}
	if out.Kind == "" {
		out.Kind = "effect"
	}
	switch e.Kind {
	case "option":
		out.Label = "option"
		if e.NameKey != "" {
			if t := locValue(s, e.NameKey); t != "" {
				out.Label = "option: " + clip(t, 28)
			}
		}
	case "immediate":
		out.Label = "immediate"
	case "trigger":
		out.Label = "trigger"
	case "on_action":
		out.Label = "on_actions"
	case "events":
		out.Label = "events"
	case "effect":
		out.Label = e.Via
	}
	return out
}

func locIfAny(s *session.Session, id string) *EventLocField {
	for _, key := range []string{id, id + ".t"} {
		if t := locValue(s, key); t != "" {
			f := &EventLocField{Key: key, Text: t}
			if file, line, _, _, ok := s.LocSite(key); ok {
				f.Path, f.Line = file, line
			}
			return f
		}
	}
	return nil
}

func locField(s *session.Session, a *jomini.Assignment) *EventLocField {
	sc, ok := a.Value.(*jomini.Scalar)
	if !ok {
		return nil
	}
	f := &EventLocField{Key: sc.Text, Text: locValue(s, sc.Text)}
	if hit := Lookup(s, sc.Text); hit != nil && hit.Path != "" {
		f.Path, f.Line = hit.Path, hit.Line
	}
	return f
}

func sectionOf(s *session.Session, name string, block *jomini.Block, lineOf func(int) int) EventSectionInfo {
	lines, targets, _ := inspectBlock(s, block, lineOf, nil)
	return EventSectionInfo{
		Name: name, Role: jomini.EventSectionRole(name),
		Lines: lines, Targets: targets,
	}
}

func optionOf(s *session.Session, block *jomini.Block, lineOf func(int) int) EventOptionInfo {
	lines, targets, fields := inspectBlock(s, block, lineOf, optionNonEffect)
	info := EventOptionInfo{Fields: fields, Lines: lines, Targets: targets}
	for _, st := range block.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		key := strings.ToLower(a.Key.Text)
		sub := jomini.BlockOf(a.Value)
		if key == "name" {
			info.Name = locField(s, a)
			continue
		}
		if key == "trigger" && sub != nil && info.Trigger == nil {
			info.Trigger = sectionPtr(s, "trigger", sub, lineOf)
		}
		if key == "ai_chance" && sub != nil && info.AiChance == nil {
			info.AiChance = sectionPtr(s, "ai_chance", sub, lineOf)
		}
	}
	return info
}

func sectionPtr(s *session.Session, name string, block *jomini.Block, lineOf func(int) int) *EventSectionInfo {
	sec := sectionOf(s, name, block, lineOf)
	return &sec
}

// inspectBlock walks a script block once: rendered lines, fire targets, and
// depth-0 scalar fields. skip keys (option name/trigger/ai) omit line emission
// but still contribute targets.
func inspectBlock(
	s *session.Session, block *jomini.Block, lineOf func(int) int, skip map[string]bool,
) (lines []EventScriptLine, targets []EventStepTarget, fields []EventFieldInfo) {
	if block == nil {
		return nil, nil, nil
	}
	seen := map[string]bool{}
	byKey := map[string]EventFieldInfo{}
	addTarget := func(name string) {
		if !targetNameRe.MatchString(name) || seen[name] || len(targets) >= maxTargets {
			return
		}
		seen[name] = true
		targets = append(targets, EventStepTarget{Name: name})
	}
	push := func(depth int, text string) {
		if len(lines) < maxBlockLines {
			lines = append(lines, EventScriptLine{Depth: depth, Text: text})
		}
	}
	var walk func(b *jomini.Block, depth int, inherited string, emit bool)
	walk = func(b *jomini.Block, depth int, inherited string, emit bool) {
		for _, st := range b.Statements {
			if vs, ok := st.(*jomini.ValueStmt); ok {
				if inherited != "" {
					if sc, ok := vs.Value.(*jomini.Scalar); ok && !sc.Quoted {
						addTarget(sc.Text)
					}
				}
				if sc, ok := vs.Value.(*jomini.Scalar); ok {
					if emit {
						push(depth, sc.Text)
					}
					continue
				}
				if inner := jomini.BlockOf(vs.Value); inner != nil {
					if emit {
						push(depth, "{")
					}
					walk(inner, depth+1, "", emit)
					if emit {
						push(depth, "}")
					}
				}
				continue
			}
			a, ok := st.(*jomini.Assignment)
			if !ok {
				continue
			}
			key := ""
			if !a.Key.Quoted {
				key = strings.ToLower(a.Key.Text)
			}
			if depth == 0 && emit && !a.Key.Quoted {
				if sc, ok := a.Value.(*jomini.Scalar); ok &&
					key != "type" && key != "title" && key != "desc" &&
					key != "flavor" && key != "hidden" && key != "theme" {
					byKey[a.Key.Text] = EventFieldInfo{
						Key: a.Key.Text, Value: sc.Text,
						Line: lineOf(a.Key.Range.Start), Quoted: sc.Quoted,
					}
				}
			}
			own := s.FireKind(a.Key.Text)
			if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted {
				if own != "" {
					addTarget(sc.Text)
				} else if inherited != "" {
					_, err := strconv.ParseUint(key, 10, 64)
					if key == "id" || err == nil {
						addTarget(sc.Text)
					}
				}
			}
			sub := jomini.BlockOf(a.Value)
			next := inherited
			if own != "" {
				next = key
			}
			skipThis := depth == 0 && skip[key]
			if skipThis || !emit {
				if sub != nil {
					walk(sub, depth+1, next, false)
				}
				continue
			}
			head := a.Key.Text
			if a.Op != "" {
				head = a.Key.Text + " " + a.Op
			}
			if a.Value == nil {
				push(depth, head)
				continue
			}
			if sc, ok := a.Value.(*jomini.Scalar); ok {
				rhs := sc.Text
				if sc.Quoted {
					rhs = `"` + sc.Text + `"`
				}
				push(depth, head+" "+rhs)
				continue
			}
			if sub == nil {
				continue
			}
			tag := ""
			if tb, ok := a.Value.(*jomini.TaggedBlock); ok {
				tag = tb.Tag.Text + " "
			}
			push(depth, head+" "+tag+"{")
			walk(sub, depth+1, next, true)
			push(depth, "}")
		}
	}
	walk(block, 0, "", true)
	fields = make([]EventFieldInfo, 0, len(byKey))
	for _, f := range byKey {
		fields = append(fields, f)
	}
	slices.SortFunc(fields, func(a, b EventFieldInfo) int {
		return cmp.Compare(a.Line, b.Line)
	})
	return lines, targets, fields
}
