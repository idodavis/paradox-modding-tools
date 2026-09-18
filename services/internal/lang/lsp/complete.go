// complete.go serves completion for script, loc, and mod files. For a script
// position the source is chosen by the block type at the cursor: a database
// offers definition names, a body offers its declared tokens and macros, a
// structure offers its unified members, and a value position offers what the
// cell's type resolved to.
package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/modfile"
)

const maxComplete = 80

// source is one row of the completion table: when it applies, and the items it offers.
type source struct {
	when  func(s *session.Session, path string, at probe) bool
	items func(s *session.Session, at probe, prefix string) []CompletionItem
}

// sources is the whole of completion; the first row whose when matches answers.
var sources = []source{
	{isMod, modCompletions},
	{isDataFn, dataFnCompletions},
	{isLoc, locCompletions},
	{isContainer, containerCompletions},
	{isValue, valueCompletions},
}

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
	for _, src := range sources {
		if src.when(s, path, at) {
			return stampRange(at, completionList(src.items(s, at, prefix)))
		}
	}
	return CompletionList{}
}

func isMod(s *session.Session, path string, _ probe) bool { return s.KindFor(path) == "mod" }

func isDataFn(s *session.Session, path string, at probe) bool {
	kind := s.KindFor(path)
	return (kind == "loc" || kind == "gui") && dataFnOpen(at)
}

func isLoc(s *session.Session, path string, _ probe) bool { return s.KindFor(path) == "loc" }

// isContainer is a position choosing a KEY: a nested block member, or the bare
// start of a new top-level definition.
func isContainer(s *session.Session, path string, at probe) bool {
	return scriptFile(s, path) && (at.inKey || at.slotKey == "")
}

// isValue is a position writing the scalar or list value of a known key.
func isValue(s *session.Session, path string, at probe) bool {
	return scriptFile(s, path) && !at.inKey && at.slotKey != ""
}

func scriptFile(s *session.Session, path string) bool {
	switch s.KindFor(path) {
	case "mod", "loc":
		return false
	}
	return true
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

// citeStart widens the replaced span leftwards over a `name:` the offered
// citations already contain, so accepting one replaces the whole expression.
//
// The word scan behind TokenAt stops at `:` even though the lexer does not, so
// with `culture_group:tur` typed it reports only `tur`. Replacing just that and
// inserting `culture_group:culture_group:turkic_group` produced
// `culture_group:culture_group:turkic_group`.
//
// The widening is exact rather than heuristic: it happens only when the text
// immediately before the span is literally the label's own prefix plus a colon,
// so the only characters ever swallowed are ones the label puts back.
func citeStart(src string, start int, items []CompletionItem) int {
	if len(items) == 0 || start <= 0 || src[start-1] != ':' {
		return start
	}
	cite, _, ok := strings.Cut(items[0].Label, ":")
	if !ok || cite == "" {
		return start
	}
	lead := start - 1 - len(cite)
	if lead < 0 || !strings.EqualFold(src[lead:start-1], cite) {
		return start
	}
	return lead
}

func capComplete(items []CompletionItem) []CompletionItem {
	if len(items) > maxComplete {
		return items[:maxComplete]
	}
	return items
}

// completionList caps items at maxComplete and flags IsIncomplete when the cap
// actually cut something off, so the editor knows to ask again as the reader
// keeps typing.
func completionList(items []CompletionItem) CompletionList {
	return CompletionList{Items: capComplete(items), IsIncomplete: len(items) > maxComplete}
}

// completionItem is the one constructor for every offered item: label, its LSP
// kind, the detail shown beside it, and the SortText bucket ("0" a declared
// token in scope, "1" a structure key, "2" a def, "3" a loc key, "" don't care).
// Documentation is left empty; ResolveDoc fetches it lazily.
func completionItem(label, kind, detail, bucket string) CompletionItem {
	it := CompletionItem{Label: label, Kind: kind, Detail: detail}
	if bucket != "" {
		it.SortText = bucket + label
	}
	return it
}

// ResolveDoc is a completion item's documentation, fetched lazily through
// IdeService.ResolveCompletion once the editor is about to show it rather than
// for every item up front.
func ResolveDoc(s *session.Session, label, kind string) string {
	doc := s.FieldDoc(label, kind, nil)
	if u := s.TokenUsage(label); u != "" {
		if doc != "" {
			doc += "\n\n"
		}
		doc += "```\n" + u + "\n```"
	}
	return doc
}

func modCompletions(_ *session.Session, _ probe, prefix string) []CompletionItem {
	var out []CompletionItem
	for _, k := range modfile.DescriptorModKeys {
		if !lowerPrefix(k, prefix) {
			continue
		}
		out = append(out, completionItem(k, "property", "mod descriptor", ""))
	}
	return out
}

// dataFnOpen reports the cursor sitting inside an unclosed `[` in a loc or GUI
// value; the bracket, not the token, is where a data-function expression starts.
func dataFnOpen(at probe) bool {
	if at.off < 0 || at.off > len(at.src) {
		return false
	}
	open := strings.LastIndex(at.src[:at.off], "[")
	return open >= 0 && !strings.Contains(at.src[open:at.off], "]")
}

func dataFnCompletions(s *session.Session, at probe, _ string) []CompletionItem {
	open := strings.LastIndex(at.src[:at.off], "[")
	typed := at.src[open+1 : at.off]
	var out []CompletionItem
	for _, k := range s.Vocab("datafunction") {
		if !lowerPrefix(k, typed) {
			continue
		}
		out = append(out, completionItem(k, "value", "data function", ""))
	}
	return out
}

func locCompletions(s *session.Session, _ probe, prefix string) []CompletionItem {
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

// containerCompletions offers the keys of the block at the cursor, chosen by
// its block type: a database's rows are definition names, a body's are its
// declared tokens and macros, a structure's are its unified members.
func containerCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	bt := s.BlockAt(at.fileKind, at.path)
	if bt == nil {
		if at.path == "" {
			return defCompletions(s, prefix, at.fileKind)
		}
		// A pure list holds no keys, so its own position never earns a Blocks
		// entry; its cell is the field one level up.
		if out := listCompletions(s, at, prefix); len(out) > 0 {
			return out
		}
		return unknownBlockCompletions(s, at, prefix)
	}
	switch bt.Class {
	case derived.BlockBody:
		return bodyCompletions(s, at, prefix, bt)
	case derived.BlockDatabase:
		kind := bt.Database
		if kind == "" {
			kind = at.fileKind
		}
		return defCompletions(s, prefix, kind)
	case derived.BlockStructure:
		if out := structureCompletions(s, at, prefix, bt); len(out) > 0 {
			return out
		}
		return unknownBlockCompletions(s, at, prefix)
	case derived.BlockList:
		return listCompletions(s, at, prefix)
	}
	return nil
}

// unknownBlockCompletions is asymmetric consequences applied to completion: a
// position nothing classifies must not offer nothing, since a missing type
// system or an unresolved scope narrows what PMT knows, never hides what it
// does know. Offers the file kind's own top-level members, every declared
// trigger/effect token (the harvested vocabulary when none are declared), and
// the mod's own macros, unranked by scope.
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
	for _, k := range s.Members(at.fileKind, "") {
		add(k, at.fileKind, "1")
	}
	tokens := append(append([]string{}, s.Vocab("trigger")...), s.Vocab("effect")...)
	if len(tokens) == 0 {
		tokens = s.Vocab("vocabulary")
	}
	for _, k := range tokens {
		add(k, "engine", "2")
	}
	for _, d := range s.FindMacroDefs(prefix, maxComplete) {
		add(d.Key, d.Kind, "2")
	}
	return out
}

// listCompletions is a bare list member's cell, one level up from its own
// (keyless) position.
func listCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	parent, key := splitPath(at.path)
	return cellCompletions(s, at.fileKind, parent, key, prefix)
}

// splitPath separates a folded block path into its parent and last segment.
func splitPath(path string) (parent, key string) {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

// bodyCompletions offers the declared trigger/effect tokens the body's class
// carries, scope-first when the scope is known, plus the mod's own scripted
// macros of a matching class.
func bodyCompletions(s *session.Session, at probe, prefix string, bt *derived.BlockType) []CompletionItem {
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
	if bt.Body&derived.BodyTrigger != 0 {
		for _, k := range scopeFirst(s.Vocab("trigger"), inScope) {
			add(k, "trigger")
		}
	}
	if bt.Body&derived.BodyEffect != 0 {
		for _, k := range scopeFirst(s.Vocab("effect"), inScope) {
			add(k, "effect")
		}
	}
	for _, d := range s.FindMacroDefs(prefix, maxComplete) {
		if macroInClass(d.Kind, bt.Body) {
			add(d.Key, d.Kind)
		}
	}
	return out
}

// macroInClass reports a scripted_* macro's own kind matching a body's
// declared classes, the same rule the schema unifies bodies with.
func macroInClass(defKind string, body uint8) bool {
	switch {
	case strings.Contains(defKind, "trigger"):
		return body&derived.BodyTrigger != 0
	case strings.Contains(defKind, "modifier"):
		return body&derived.BodyModifier != 0
	default:
		return body&derived.BodyEffect != 0
	}
}

// structureCompletions offers a structure's unified members: what the corpus
// wrote at every position of this block's key, plus the game's own docs.
func structureCompletions(s *session.Session, at probe, prefix string, bt *derived.BlockType) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	for _, k := range s.StructureMembers(at.fileKind, at.path) {
		if k == "" || seen[k] || !lowerPrefix(k, prefix) {
			continue
		}
		seen[k] = true
		it := completionItem(k, "property", bt.Key, "1")
		it.DocKind = bt.Key
		if s.TypeOf(at.fileKind, at.path, k).IsBlock() {
			it.InsertText = k + " = { $0 }"
		} else {
			it.InsertText = k + " = "
		}
		out = append(out, it)
	}
	return out
}

// valueCompletions offers what the cell under the cursor was seen to hold:
// definitions of its resolved kinds, loc keys, and any closed set the game
// declares for the field, falling back to the bare boolean.
func valueCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	out := scriptNameCompletions(s, at, prefix)
	out = append(out, cellCompletions(s, at.fileKind, at.path, at.slotKey, prefix)...)
	out = append(out, enumCompletions(s, at, prefix)...)
	if len(out) > 0 {
		return out
	}
	return boolCompletions(prefix)
}

// scriptNameCompletions is grammar row 4 (jomini.ScriptName): has_variable,
// has_global_variable, exists = scope:… and their kin name an ephemeral value
// rather than an object, keyed off the one table rather than per-entry code.
// A use site offers the ephemeral definitions of that kind the workspace made.
func scriptNameCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	r, ok := jomini.ScriptName(at.slotKey)
	if !ok || r.IsDef {
		return nil
	}
	return defCompletions(s, prefix, r.Kind)
}

// cellCompletions offers what one cell was seen to hold: definitions of every
// database its values resolved to, and localization keys where they were keys.
func cellCompletions(s *session.Session, kind, path, key, prefix string) []CompletionItem {
	t := s.TypeOf(kind, path, key)
	if t == nil {
		return nil
	}
	var out []CompletionItem
	for _, k := range t.Kinds {
		if k == derived.LabelLoc {
			out = append(out, locCompletions(s, probe{}, prefix)...)
			continue
		}
		out = append(out, defCompletions(s, prefix, k)...)
	}
	return out
}

// enumCompletions offers a closed set the game's own documentation declares
// for this field, e.g. a fixed list of valid words.
func enumCompletions(s *session.Session, at probe, prefix string) []CompletionItem {
	dt := s.DeclaredFieldType(at.kind, at.slotKey)
	if len(dt.Values) == 0 {
		return nil
	}
	var out []CompletionItem
	for _, v := range dt.Values {
		if !lowerPrefix(v, prefix) {
			continue
		}
		out = append(out, completionItem(v, "value", "value", "2"))
	}
	return out
}

func boolCompletions(prefix string) []CompletionItem {
	var out []CompletionItem
	for _, label := range []string{"yes", "no"} {
		if lowerPrefix(label, prefix) {
			out = append(out, completionItem(label, "value", "bool", ""))
		}
	}
	return out
}

// defCompletions offers definitions of one kind, in the form the corpus writes
// them: `prefix:id` when the kind is bound to a citation prefix, bare
// otherwise. The typed text is matched against either half, so a reader typing
// `turkic` still finds `culture_group:turkic_group`.
func defCompletions(s *session.Session, prefix, kind string) []CompletionItem {
	if kind == "" {
		return nil
	}
	pfx := s.Work.Schema.KindPrefix[kind]
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

// scopeFirst puts the tokens the game declares usable here at the front,
// keeping the rest behind them rather than dropping them. inScope nil means the
// scope is unknown, which is not the same as "nothing is valid".
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
