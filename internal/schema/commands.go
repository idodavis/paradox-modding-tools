// commands.go looks up what the dumps declare: the effects, the triggers,
// the links, the patterns that make commands and the types of scope.

package schema

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A ChainLink is one link of a chain, as ResolveChain read it.
type ChainLink struct {
	// Definition is the link the dumps declare. It is nil for a link they do
	// not declare.
	Definition *Definition

	// Scope is the scope type the chain is in after the link. It is "" where
	// it is not known.
	Scope string
}

// commandDatabases are the databases of the dumps' commands, in the order a
// name is looked up in them.
var commandDatabases = []string{game.DumpEffects, game.DumpTriggers, game.DumpLinks}

// LookupDefinition returns the definition a database declares for a key, as
// written or else in lower case. It returns nil if the database declares
// none, or the schema has no such database.
func (s *Schema) LookupDefinition(database, key string) *Definition {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return cmp.Or(owner.Definitions[key], owner.Definitions[strings.ToLower(key)])
}

// ListKeys returns the keys a database declares outside the index, sorted. It
// returns none for a database that declares none, or that the schema lacks.
func (s *Schema) ListKeys(database string) []string {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return owner.Keys
}

// FindCommand returns the first effect, trigger or link that a key names, in
// that order. It returns nil if the dumps declare none.
func (s *Schema) FindCommand(key string) *Definition {
	for _, database := range commandDatabases {
		if definition := s.LookupDefinition(database, key); definition != nil {
			return definition
		}
	}

	return nil
}

// declaresModifier reports whether the dumps declare a modifier of the given
// key, as written.
func (s *Schema) declaresModifier(key string) bool {
	return s.Databases[game.DumpModifiers].Definitions[key] != nil
}

// makeField returns the definition's field. If it has none, it adds an empty
// one from the given source.
func (d *Definition) makeField(source game.Source) *Field {
	if d.Field == nil {
		d.Field = &Field{Source: source}
	}

	return d.Field
}

// findKeyCall parses a key written as a command's name with one argument in
// parentheses, such as squared_distance(root), and returns the call's step
// and the command. The step's spans are offsets in the file, where the key
// starts at the given offset. It returns false for any other key, and when
// the name is no command.
func (s *Schema) findKeyCall(key string, start int) (jomini.Step, *Definition, bool) {
	if !strings.HasSuffix(key, ")") {
		return jomini.Step{}, nil, false
	}

	call, ok := jomini.ParseCall(key, start)
	if !ok || len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 1 || call.Span.End != start+len(key) {
		return jomini.Step{}, nil, false
	}

	// a link that takes a name after a colon takes it in parentheses too
	label := call.Steps[0].Text
	command := cmp.Or(s.FindCommand(label), s.lookupLink(label, true))

	return call.Steps[0], command, command != nil
}

// findStatementCall does what findKeyCall does for a statement's key.
func (s *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Definition, bool) {
	span := tree.UnquotedSpan(tree.Statements[statement].Key)

	return s.findKeyCall(tree.Text(span), span.Start)
}

// isCompared reports whether a statement compares its key with a block, as
// list_size(x) < { value = … } does. The game reads such a block as a value
// to work out. A block written with no operator, as a GUI file's types Name
// { } is, compares nothing.
func isCompared(tree *jomini.Tree, statement int32) bool {
	current := tree.Statements[statement]
	operator := tree.Text(current.Op)

	return current.IsBlock() && operator != "" && operator != "=" && operator != "?="
}

// findCallee returns the name a statement's key calls: the command's name for
// a key with an argument in parentheses, else the unquoted key.
func (s *Schema) findCallee(tree *jomini.Tree, statement int32) string {
	if step, _, ok := s.findStatementCall(tree, statement); ok {
		return step.Text
	}

	return tree.Unquoted(tree.Statements[statement].Key)
}

// findStar returns the start and end of the text in a lower-case key that
// fills the star of a pattern's key. It returns false if the key does not
// fit the pattern, leaves nothing for the star, or has a dot there. A dot
// means a chain of links, and no definition's name holds one.
func (d *Definition) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(d.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)

	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the patterns of a database that make a key, or none.
// A command the dumps declare is made by the patterns that list it, and any
// other key by the fitting pattern with the shortest star, the first on a
// tie.
func (s *Schema) findPatterns(database, key string) []*Definition {
	owner := s.Databases[database]
	key = strings.ToLower(key)
	if owner == nil || !strings.Contains(key, "_") {
		return nil
	}

	if definition := owner.Definitions[key]; definition != nil {
		return definition.Patterns
	}

	var best []*Definition
	shortest := len(key)
	for _, pattern := range owner.Patterns {
		start, end, fits := pattern.findStar(key)
		switch {
		case !fits: // the key does not fit this pattern
		case end-start < shortest:
			best, shortest = []*Definition{pattern}, end-start
		case end-start == shortest && pattern.Key == best[0].Key:
			best = append(best, pattern)
		}
	}

	return best
}

// lookupLink returns the link the dumps declare for a key as it is written:
// with a name after a colon, as the flag says, or alone. The dumps list some
// links both ways, as culture:norse and root.culture are, each with its own
// text, scopes and target, so a link written with a colon is kept under its
// key and the colon. It returns nil if the dumps declare none.
func (s *Schema) lookupLink(key string, prefixed bool) *Definition {
	if prefixed {
		key += ":"
	}

	return s.LookupDefinition(game.DumpLinks, key)
}

// FindLink returns the link that a key or a value starts with, in any case:
// its first link, or that link's prefix where it is written with a name
// after a colon, as scope:target is. A wild card fits only as a prefix. It
// returns nil if the text starts with no link.
func (s *Schema) FindLink(text string) *Definition {
	head := jomini.Links(text, 0)[0]
	if !head.Prefix.IsEmpty() {
		prefix := text[head.Prefix.Start:head.Prefix.End]
		if link := s.lookupLink(prefix, true); link != nil {
			return link
		}

		if link := s.lookupLink(prefix, false); link != nil && link.Wild {
			return link
		}

		return nil
	}

	link := s.lookupLink(text[head.Span.Start:head.Span.End], false)
	if link == nil || link.Wild {
		return nil
	}

	return link
}

// IsLink reports whether a key or a value starts with a scope link, as
// FindLink finds one.
func (s *Schema) IsLink(text string) bool { return s.FindLink(text) != nil }

// isChain reports whether every step of a value written with dots is a scope
// link, as owner.ruler is and a text key such as war.0051.start is not.
func (s *Schema) isChain(text string) bool {
	return !slices.ContainsFunc(jomini.Links(text, 0), func(link jomini.Link) bool {
		return s.FindLink(text[link.Span.Start:link.Span.End]) == nil
	})
}

// LookupPrefix returns the database of the names written after a link
// prefix, as scope:x names a saved scope, and whether the text is a link
// prefix at all. The database is "" where none is known.
func (s *Schema) LookupPrefix(prefix string) (string, bool) {
	link := s.lookupLink(prefix, true)
	if link == nil {
		return "", false
	}

	if !link.Field.claimsDatabase() {
		return "", true
	}

	return link.Field.Value.Value.Database, true
}

// ResolveChain returns each link of a chain written in the block of the
// given context, with the scope type the chain is in after it. root, this
// and prev name the scopes the context holds, and a saved scope has the type
// it was saved with, among the given scopes saved before the chain. The
// scope after a link that cannot be followed is "", and so is every scope
// after it.
func (s *Schema) ResolveChain(text string, holder *Context, saved []Saved) []ChainLink {
	links := jomini.Links(text, 0)
	chain := make([]ChainLink, len(links))
	scope, followed := holder.Scope, true
	for position, link := range links {
		label := strings.ToLower(text[link.Span.Start:link.Span.End])
		prefix := strings.ToLower(text[link.Prefix.Start:link.Prefix.End])
		definition := s.lookupLink(cmp.Or(prefix, label), prefix != "")
		chain[position].Definition = definition
		if !followed {
			continue
		}

		switch {
		case label == game.ScopeLinks.Root:
			scope = holder.Root
		case label == game.ScopeLinks.This:
			scope = holder.Scope
		case label == game.ScopeLinks.Previous:
			scope = ""
			if len(holder.Previous) > 0 {
				scope = holder.Previous[0]
			}

		case prefix == game.SavedScopes:
			scope = findSavedType(saved, text[link.Name.Start:link.Name.End])
		case prefix != "":
			scope = definition.ResolveTarget("")
		default:
			scope = definition.ResolveTarget(scope)
		}

		chain[position].Scope = scope
		followed = scope != ""
	}

	return chain
}

// findSavedType returns the type of the scope last saved under a label. It
// returns "" if none is, or its type is not known.
func findSavedType(saved []Saved, label string) string {
	for _, scope := range slices.Backward(saved) {
		if scope.Name == label {
			return scope.Type
		}
	}

	return ""
}

// ResolveTarget returns the scope a link leads to from the given scope. It
// returns "" for no link, a wild card, a link that leads nowhere and one
// that does not run in that scope. Any link runs in a scope that is "".
func (d *Definition) ResolveTarget(scope string) string {
	if d == nil || d.Wild || scope != "" && len(d.Scopes) > 0 && !slices.Contains(d.Scopes, scope) {
		return ""
	}

	return d.Target
}

// isScope reports whether a name is a scope type that script runs in. It is
// false for the names the dumps use for no scope, such as none, though they
// declare them like one.
func (s *Schema) isScope(scope string) bool {
	definition := s.LookupDefinition(game.DumpTypes, scope)

	return definition != nil && definition.Runs && !slices.Contains(dumpPhrases.NoScope, scope)
}
