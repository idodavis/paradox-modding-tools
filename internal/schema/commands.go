// commands.go looks up what the dumps declare: commands, links, patterns and
// scope types.

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
	// Definition is the link the dumps declare. It is nil for none.
	Definition *Definition

	// Scope is the scope type after the link. It is "" if not known.
	Scope string
}

// commandDatabases are the dumps' command databases, in lookup order.
var commandDatabases = []string{game.DumpEffects, game.DumpTriggers, game.DumpLinks}

// LookupDefinition returns the definition a database declares for a key, as
// written or in lower case. It returns nil for none.
func (s *Schema) LookupDefinition(database, key string) *Definition {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return cmp.Or(owner.Definitions[key], owner.Definitions[strings.ToLower(key)])
}

// ListKeys returns the sorted keys a database declares, or nil.
func (s *Schema) ListKeys(database string) []string {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return owner.Keys
}

// FindCommand returns the effect, trigger or link a key names, or nil.
func (s *Schema) FindCommand(key string) *Definition {
	for _, database := range commandDatabases {
		if definition := s.LookupDefinition(database, key); definition != nil {
			return definition
		}
	}

	return nil
}

func (s *Schema) declaresModifier(key string) bool {
	return s.Databases[game.DumpModifiers].Definitions[key] != nil
}

func (d *Definition) makeField(source game.Source) *Field {
	if d.Field == nil {
		d.Field = &Field{Source: source}
	}

	return d.Field
}

// findKeyCall returns the command a key calls with an argument in parentheses,
// as in squared_distance(root), and the call's step. The key starts at the
// given offset. It returns false for any other key.
func (s *Schema) findKeyCall(key string, start int) (jomini.Step, *Definition, bool) {
	step, ok := jomini.ParseParenthesized(key, start)
	if !ok {
		return jomini.Step{}, nil, false
	}

	// a link that takes a name after a colon takes it in parentheses too
	command := cmp.Or(s.FindCommand(step.Text), s.lookupLink(step.Text, true))

	return step, command, command != nil
}

func (s *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Definition, bool) {
	span := tree.UnquotedSpan(tree.Statements[statement].Key)

	return s.findKeyCall(tree.Text(span), span.Start)
}

// isCompared reports whether a statement compares its key with a block, as in
// list_size(x) < { value = … }. A block with no operator, as in a GUI file's
// Name { }, compares nothing.
func isCompared(tree *jomini.Tree, statement int32) bool {
	current := tree.Statements[statement]
	operator := tree.Text(current.Op)

	return current.IsBlock() && operator != "" && operator != "=" && operator != "?="
}

func (s *Schema) findCallee(tree *jomini.Tree, statement int32) string {
	if step, _, ok := s.findStatementCall(tree, statement); ok {
		return step.Text
	}

	return tree.Unquoted(tree.Statements[statement].Key)
}

// findStar returns where a lower-case key fills the pattern's star. It returns
// false if the key does not fit or the star is empty or holds a dot.
func (d *Definition) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(d.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)

	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the patterns that make a key: those a declared command
// lists, else the fitting ones with the shortest star. On a tie the first
// one's key decides. It returns nil for none.
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

// lookupLink returns the link the dumps declare for a key, in its colon form
// if prefixed is true. The dumps list some links both ways, as culture:norse
// and root.culture, each with its own scopes and target.
func (s *Schema) lookupLink(key string, prefixed bool) *Definition {
	if prefixed {
		key += ":"
	}

	return s.LookupDefinition(game.DumpLinks, key)
}

// FindLink returns the link a key or value starts with, in any case. A link
// with a name after a colon, as in scope:target, is found by its prefix. A
// wild card fits only there. It returns nil for none.
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

// IsLink reports whether FindLink finds a link.
func (s *Schema) IsLink(text string) bool { return s.FindLink(text) != nil }

// isChain reports whether every step of a dotted value is a scope link, as in
// owner.ruler but not in war.0051.start.
func (s *Schema) isChain(text string) bool {
	return !slices.ContainsFunc(jomini.Links(text, 0), func(link jomini.Link) bool {
		return s.FindLink(text[link.Span.Start:link.Span.End]) == nil
	})
}

// LookupPrefix returns the database of the names after a link prefix, as in
// scope:x, and whether the text is a prefix. The database is "" if unknown.
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

// ResolveChain returns each link of a chain written in the given context's
// block, with the scope type after it. A saved scope has the type it was
// saved with. The scope after a link that cannot be followed is "", and so is
// every later one.
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

// findSavedType returns the type of the scope last saved under a label, or ""
// if none is or its type is not known.
func findSavedType(saved []Saved, label string) string {
	for _, scope := range slices.Backward(saved) {
		if scope.Name == label {
			return scope.Type
		}
	}

	return ""
}

// ResolveTarget returns the scope a link leads to from the given scope. It
// returns "" for a nil link, a wild card, a link that leads nowhere and one
// that does not run there. Every link runs in scope "".
func (d *Definition) ResolveTarget(scope string) string {
	if d == nil || d.Wild || scope != "" && len(d.Scopes) > 0 && !slices.Contains(d.Scopes, scope) {
		return ""
	}

	return d.Target
}

// isScope reports whether a name is a scope type, and not one of the names the
// dumps use for no scope, such as none.
func (s *Schema) isScope(scope string) bool {
	definition := s.LookupDefinition(game.DumpTypes, scope)

	return definition != nil && definition.Runs && !slices.Contains(dumpPhrases.NoScope, scope)
}
