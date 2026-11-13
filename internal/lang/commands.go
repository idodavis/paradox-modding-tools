// commands.go looks up what the dumps declare: effects, triggers, links and
// scope types. It also follows a chain of links, as root.father.

package lang

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/jomini"
)

// ChainLink is one link of a chain, as father is in root.father.
type ChainLink struct {
	// Definition is nil for a link the dumps do not declare.
	Definition *Definition

	// Scope is the scope type the chain has reached after the link, "" where
	// it is not known.
	Scope string
}

// CommandDatabases are the databases of effects, triggers and links, in the
// order a key is looked up in them.
var CommandDatabases = []string{DumpEffects, DumpTriggers, DumpLinks}

// NoScope holds the words the dumps write where a command has no scope.
var NoScope = []string{"none", "all", "none/any"}

// LookupDefinition returns a database's definition for a key, as written or
// else in lower case.
func (s *Schema) LookupDefinition(database, key string) *Definition {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return cmp.Or(owner.Definitions[key], owner.Definitions[strings.ToLower(key)])
}

// ListKeys returns the sorted keys of a database's definitions.
func (s *Schema) ListKeys(database string) []string {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	return owner.Keys
}

// FindCommand returns the effect, the trigger or the link a key names, the
// first of them that has it.
func (s *Schema) FindCommand(key string) *Definition {
	for _, database := range CommandDatabases {
		if definition := s.LookupDefinition(database, key); definition != nil {
			return definition
		}
	}

	return nil
}

// DeclaresModifier reports whether the dumps list a modifier of the key.
func (s *Schema) DeclaresModifier(key string) bool {
	return s.Databases[DumpModifiers].Definitions[key] != nil
}

// FindKeyCall reads a key written with an argument in parentheses, as
// squared_distance(root), and returns its parts and the command it calls.
// The key's text starts at the given offset of its file.
func (s *Schema) FindKeyCall(key string, start int) (jomini.Step, *Definition, bool) {
	step, ok := jomini.ParseParenthesized(key, start)
	if !ok {
		return jomini.Step{}, nil, false
	}

	// a link that takes a name after a colon takes it in parentheses too
	command := cmp.Or(s.FindCommand(step.Text), s.LookupLink(step.Text, true))

	return step, command, command != nil
}

// findStatementCall returns what FindKeyCall returns for a statement's key.
func (s *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Definition, bool) {
	span := tree.UnquotedSpan(tree.Statements[statement].Key)

	return s.FindKeyCall(tree.Text(span), span.Start)
}

// FindCallee returns the name a statement's key calls: the command of a key
// with an argument in parentheses, as aptitude in aptitude(x), else the key.
func (s *Schema) FindCallee(tree *jomini.Tree, statement int32) string {
	if step, _, ok := s.findStatementCall(tree, statement); ok {
		return step.Text
	}

	return tree.Unquoted(tree.Statements[statement].Key)
}

// findStar returns the part of a lower-case key that fills the star of a key
// such as has_relation_*. It returns false where the key does not fit around
// the star, or the filling is empty or holds a dot.
func (d *Definition) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(d.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)

	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the star keys of a database that a key fits, as
// has_relation_* for has_relation_rival. A key the dumps declare has them
// listed. For any other key they are the ones whose star the fewest letters
// fill, ties going to the first key.
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
		case !fits:
		case end-start < shortest:
			best, shortest = []*Definition{pattern}, end-start
		case end-start == shortest && len(best) > 0 && pattern.Key == best[0].Key:
			best = append(best, pattern)
		}
	}

	return best
}

// LookupLink returns the link the dumps declare for a key. With prefixed it
// is the link written with a name after a colon: the dumps list some links
// both ways, as culture:norse and root.culture, each with its own scopes.
func (s *Schema) LookupLink(key string, prefixed bool) *Definition {
	if prefixed {
		key += ":"
	}

	return s.LookupDefinition(DumpLinks, key)
}

// FindLink returns the link a key or a value starts with, as root in
// root.father. A link with a name after a colon, as scope:target, is found by
// the word before the colon, and a wild card is a link only when written so.
func (s *Schema) FindLink(text string) *Definition {
	head := jomini.FirstLink(text, 0)
	if !head.Prefix.IsEmpty() {
		prefix := text[head.Prefix.Start:head.Prefix.End]
		if link := s.LookupLink(prefix, true); link != nil {
			return link
		}

		if link := s.LookupLink(prefix, false); link != nil && link.Wild {
			return link
		}

		return nil
	}

	link := s.LookupLink(text[head.Span.Start:head.Span.End], false)
	if link == nil || link.Wild {
		return nil
	}

	return link
}

// IsLink reports whether a key or a value starts with a link.
func (s *Schema) IsLink(text string) bool { return s.FindLink(text) != nil }

// isChain reports whether a text is a chain whose every part is a link, as
// owner.ruler is and war.0051.start is not.
func (s *Schema) isChain(text string) bool {
	links := jomini.Links(text, 0)

	return len(links) > 0 && !slices.ContainsFunc(links, func(link jomini.Link) bool {
		return s.FindLink(text[link.Span.Start:link.Span.End]) == nil
	})
}

// LookupPrefix returns the database of the names written after a link's
// colon, as the saved scopes for scope:x, "" where it is not known. It
// returns false for a word that is no link taking a name.
func (s *Schema) LookupPrefix(prefix string) (string, bool) {
	link := s.LookupLink(prefix, true)
	if link == nil {
		return "", false
	}

	if !link.Field.claimsDatabase() {
		return "", true
	}

	return link.Field.Value.Value.Database, true
}

// ResolveChain follows a chain of links, as root.father.culture, written in
// the given statement's block, and returns each link with the scope type
// reached after it. A saved scope has the type it was saved with. Once a link
// cannot be followed, every later scope is "".
func (s *Schema) ResolveChain(text string, holder *Context, saved []Saved) []ChainLink {
	links := jomini.Links(text, 0)
	chain := make([]ChainLink, len(links))
	scope, isKnown := holder.Scope, true
	for position, link := range links {
		label := strings.ToLower(text[link.Span.Start:link.Span.End])
		prefix := strings.ToLower(text[link.Prefix.Start:link.Prefix.End])
		definition := s.LookupLink(cmp.Or(prefix, label), prefix != "")
		chain[position].Definition = definition
		if !isKnown {
			continue
		}

		switch {
		case label == LinkRoot:
			scope = holder.Root

		case label == LinkThis:
			scope = holder.Scope

		case label == LinkPrevious:
			scope = ""
			if len(holder.Previous) > 0 {
				scope = holder.Previous[0]
			}

		case prefix == SavedScopes:
			scope = findSavedType(saved, text[link.Name.Start:link.Name.End])

		case prefix != "":
			scope = definition.ResolveTarget("")

		default:
			scope = definition.ResolveTarget(scope)
		}

		chain[position].Scope = scope
		isKnown = scope != ""
	}

	return chain
}

// findSavedType returns the type of the scope last saved under a name, ""
// where none was or its type is not known.
func findSavedType(saved []Saved, label string) string {
	for _, scope := range slices.Backward(saved) {
		if scope.Name == label {
			return scope.Type
		}
	}

	return ""
}

// ResolveTarget returns the scope type a link leads to from a scope of the
// given type. It returns "" for a nil link, a wild card, a link to no scope
// and one not allowed there. An unknown scope, "", is taken as allowed.
func (d *Definition) ResolveTarget(scope string) string {
	if d == nil || d.Wild || scope != "" && len(d.Scopes) > 0 && !slices.Contains(d.Scopes, scope) {
		return ""
	}

	return d.Target
}

// IsScope reports whether a word names a scope type that script runs in. The
// words the dumps write for no scope, such as none, do not.
func (s *Schema) IsScope(scope string) bool {
	definition := s.LookupDefinition(DumpTypes, scope)

	return definition != nil && definition.Runs && !slices.Contains(NoScope, scope)
}
