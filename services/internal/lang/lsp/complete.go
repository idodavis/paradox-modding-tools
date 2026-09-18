// complete.go serves completion for script, loc, and mod files. For a script
// position the offer is chosen by the block type at the cursor: a database
// offers definition names, a body offers its declared tokens and macros, a
// structure offers its unified members, and a value position offers what the
// cell's type resolved to.

package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/modfile"
)

const maxComplete = 80

// Complete returns completion items at (line, UTF-8 column).
func Complete(s *session.Session, path string, line, col int) CompletionList {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return CompletionList{}
	}
	prefix := ""
	if at.word != "" && at.start < at.off {
		prefix = at.src[at.start:at.off]
	}
	var items []CompletionItem
	switch kind := s.KindFor(path); {
	case kind == "mod":
		for _, k := range modfile.DescriptorModKeys {
			if lowerPrefix(k, prefix) {
				items = append(items, completionItem(k, "property", "mod descriptor", ""))
			}
		}
	case (kind == "loc" || kind == "gui") && dataFnOpen(at):
		// The bracket, not the token, is where a data-function expression starts.
		typed := at.src[strings.LastIndex(at.src[:at.off], "[")+1 : at.off]
		for _, k := range s.Install.Tree.DataFunctions {
			if lowerPrefix(k, typed) {
				items = append(items, completionItem(k, "value", "data function", ""))
			}
		}
	case kind == "loc":
		items = locCompletions(s, prefix)
	case at.inKey || at.slotKey == "":
		items = keyCompletions(s, at, prefix)
	default:
		items = valueCompletions(s, at, prefix)
	}
	list := CompletionList{Items: items, IsIncomplete: len(items) > maxComplete}
	if list.IsIncomplete {
		list.Items = items[:maxComplete]
	}
	return stampRange(at, list)
}

// dataFnOpen reports the cursor sitting inside an unclosed `[` in a loc or GUI value.
func dataFnOpen(at probe) bool {
	if at.off < 0 || at.off > len(at.src) {
		return false
	}
	open := strings.LastIndex(at.src[:at.off], "[")
	return open >= 0 && !strings.Contains(at.src[open:at.off], "]")
}

func stampRange(at probe, list CompletionList) CompletionList {
	if len(list.Items) == 0 || at.src == "" {
		return list
	}
	start, end := at.start, at.end
	if start == end {
		start, end = at.off, at.off
	}
	start = citeStart(at.src, start, list.Items)
	rg := byteRange(at.res.Lines(), start, end)
	for i := range list.Items {
		list.Items[i].Range = rg
	}
	return list
}

// citeStart widens the replaced span leftwards over a `name:` the offered citations already contain, so accepting one replaces the whole expression. The word scan stops at `:`, so with `culture_group:tur` typed it reports only `tur`. Exact rather than heuristic: only when the text before the span is literally the label's own prefix plus a colon.
func citeStart(src string, start int, items []CompletionItem) int {
	if start <= 0 || src[start-1] != ':' {
		return start
	}
	for _, it := range items {
		cite, _, ok := strings.Cut(it.Label, ":")
		if lead := start - 1 - len(cite); ok && cite != "" && lead >= 0 && strings.EqualFold(src[lead:start-1], cite) {
			return lead
		}
	}
	return start
}

// completionItem is the one constructor for every offered item: label, its LSP kind, the detail shown beside it, and the SortText bucket ("0" a declared token in scope, "1" a structure key, "2" a def, "3" a loc key, "" don't care). Documentation is left empty; ResolveDoc fetches it lazily.
func completionItem(label, kind, detail, bucket string) CompletionItem {
	it := CompletionItem{Label: label, Kind: kind, Detail: detail}
	if bucket != "" {
		it.SortText = bucket + label
	}
	return it
}

// ResolveDoc is a completion item's documentation, fetched lazily through IdeService.ResolveCompletion once the editor is about to show it.
func ResolveDoc(s *session.Session, label, kind string) string {
	doc := fieldDoc(s, label, kind, "")
	if u := clipUsage(tokenUsage(s, label)); u != "" {
		if doc != "" {
			doc += "\n\n"
		}
		doc += "```\n" + u + "\n```"
	}
	return doc
}

func locCompletions(s *session.Session, prefix string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	for _, k := range s.LocKeys(prefix, maxComplete) {
		if k == "" || seen[k] || !lowerPrefix(k, prefix) {
			continue
		}
		seen[k] = true
		out = append(out, completionItem(k, "value", "loc key", "3"))
	}
	return out
}

// keyCompletions offers the keys of the block at the cursor, chosen by its block type: a database's rows are definition names, a body's are its declared tokens and macros, a structure's are its unified members, a list's are its cell.
func keyCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	// A bare word at the file's own level is a new definition's name.
	if len(at.segs) == 0 && at.slotKey == "" {
		return defCompletions(s, prefix, at.fileKind)
	}
	bt := at.hang
	if bt == nil {
		// A pure list holds no keys, so its own position never earns a Blocks entry; its cell is the field one level up.
		if out := cellCompletions(s, at.cell, prefix); len(out) > 0 {
			return out
		}
		return unknownBlockCompletions(s, at, prefix)
	}
	switch bt.Class {
	case index.Body:
		return bodyCompletions(s, at, prefix, bt)
	case index.Database:
		kind := bt.Def
		if kind == "" {
			kind = at.fileKind
		}
		return defCompletions(s, prefix, kind)
	case index.Structure:
		if out := structureCompletions(s, prefix, bt); len(out) > 0 {
			return out
		}
		return unknownBlockCompletions(s, at, prefix)
	case index.List:
		return cellCompletions(s, bt, prefix)
	}
	return nil
}

// unknownBlockCompletions is asymmetric consequences applied to completion: a position nothing classifies must not offer nothing. The file kind's own top-level members, every declared token (the harvested vocabulary when none are declared), and the mod's own macros, unranked by scope.
func unknownBlockCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(label, detail, bucket string) {
		if label == "" || seen[label] || !lowerPrefix(label, prefix) {
			return
		}
		seen[label] = true
		out = append(out, completionItem(label, "property", detail, bucket))
	}
	for _, k := range s.Node(at.fileKind).Members() {
		add(k, at.fileKind, "1")
	}
	names := append(tokens(s, "trigger"), tokens(s, "effect")...)
	if len(names) == 0 {
		names = s.Install.Tree.Known
	}
	for _, k := range names {
		add(k, "engine", "2")
	}
	for _, d := range s.Macros(prefix, maxComplete) {
		add(d.Key, d.Kind, "2")
	}
	return out
}

// bodyCompletions offers the declared trigger/effect tokens the body's class carries, scope-first when the scope is known, plus the mod's own scripted macros of a matching class.
func bodyCompletions(s *session.Session, at probe, prefix string, bt *index.Node) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	inScope := map[string]bool{}
	if at.scope != "" && !s.Install.Declared.Empty() {
		inScope = s.Install.Declared.ScopeTokens(at.scope)
	}
	add := func(label, detail string) {
		if label == "" || seen[label] || !lowerPrefix(label, prefix) {
			return
		}
		seen[label] = true
		bucket := "2"
		if inScope[strings.ToLower(label)] {
			bucket = "0"
		}
		out = append(out, completionItem(label, "property", detail, bucket))
	}
	if bt.Body&index.BodyTrigger != 0 {
		for _, k := range scopeFirst(tokens(s, "trigger"), inScope) {
			add(k, "trigger")
		}
	}
	if bt.Body&index.BodyEffect != 0 {
		for _, k := range scopeFirst(tokens(s, "effect"), inScope) {
			add(k, "effect")
		}
	}
	for _, d := range s.Macros(prefix, maxComplete) {
		if macroInClass(d.Kind, bt.Body) {
			add(d.Key, d.Kind)
		}
	}
	return out
}

// macroInClass reports a scripted_* macro's own kind matching a body's declared classes, the same rule the schema unifies bodies with.
func macroInClass(defKind string, body uint8) bool {
	switch {
	case strings.Contains(defKind, "trigger"):
		return body&index.BodyTrigger != 0
	case strings.Contains(defKind, "modifier"):
		return body&index.BodyModifier != 0
	default:
		return body&index.BodyEffect != 0
	}
}

// structureCompletions offers a structure's unified members: what the corpus wrote at every position of this block's key, plus the game's own docs.
func structureCompletions(s *session.Session, prefix string, bt *index.Node) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	for _, k := range s.Install.Tree.StructMembers(bt) {
		if k == "" || seen[k] || !lowerPrefix(k, prefix) {
			continue
		}
		seen[k] = true
		it := completionItem(k, "property", bt.Struct, "1")
		it.DocKind = bt.Struct
		if c := childOf(s, bt, k); c != nil && c.Flags&index.IsBlock != 0 {
			it.InsertText = k + " = { $0 }"
		} else {
			it.InsertText = k + " = "
		}
		out = append(out, it)
	}
	return out
}

// valueCompletions offers what the cell under the cursor was seen to hold: the ephemeral names a script-name key takes, definitions of its resolved kinds, loc keys, and any closed set the game declares for the field, falling back to the bare boolean.
func valueCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	var out []CompletionItem
	if r, ok := jomini.ScriptName(at.slotKey); ok && !r.IsDef {
		out = defCompletions(s, prefix, r.Kind)
	}
	out = append(out, cellCompletions(s, at.cell, prefix)...)
	for _, v := range declaredFieldType(s, at.kind, at.slotKey).Values {
		if lowerPrefix(v, prefix) {
			out = append(out, completionItem(v, "value", "value", "2"))
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, label := range []string{"yes", "no"} {
		if lowerPrefix(label, prefix) {
			out = append(out, completionItem(label, "value", "bool", ""))
		}
	}
	return out
}

// cellCompletions offers what one cell was seen to hold: definitions of every database its values resolved to, and localization keys where they were keys.
func cellCompletions(s *session.Session, t *index.Node, prefix string) []CompletionItem {
	if t == nil {
		return nil
	}
	var out []CompletionItem
	for _, k := range t.Kinds {
		if k == index.LabelLoc {
			out = append(out, locCompletions(s, prefix)...)
			continue
		}
		out = append(out, defCompletions(s, prefix, k)...)
	}
	return out
}

// defCompletions offers definitions of one kind, in the form the corpus writes them: `prefix:id` when the kind is bound to a citation prefix, bare otherwise. The typed text is matched against either half.
func defCompletions(s *session.Session, prefix, kind string) []CompletionItem {
	if kind == "" {
		return nil
	}
	pfx := s.Install.Tree.Prefix(kind)
	query := prefix
	if pfx != "" {
		if before, after, ok := strings.Cut(prefix, ":"); ok && lowerPrefix(pfx, before) {
			query = after
		}
	}
	seen := map[string]bool{}
	var out []CompletionItem
	for _, d := range s.FindDefsOf(query, kind, maxComplete, true, true) {
		if d.Key == "" {
			continue
		}
		label := d.Key
		if pfx != "" {
			label = pfx + ":" + d.Key
		}
		if seen[label] || (!lowerPrefix(label, prefix) && !lowerPrefix(d.Key, query)) {
			continue
		}
		seen[label] = true
		out = append(out, completionItem(label, "value", d.Kind, "2"))
	}
	return out
}

// scopeFirst puts the tokens the game declares usable here at the front, keeping the rest behind them rather than dropping them; an empty inScope means the scope is unknown, not that nothing is valid.
func scopeFirst(tokens []string, inScope map[string]bool) []string {
	if len(inScope) == 0 {
		return tokens
	}
	valid := make([]string, 0, len(tokens))
	rest := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if inScope[strings.ToLower(t)] {
			valid = append(valid, t)
		} else {
			rest = append(rest, t)
		}
	}
	return append(valid, rest...)
}
