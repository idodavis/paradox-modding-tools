// commands.go looks up the commands, the links and the command patterns that
// the dumps declare, and the scopes they run in.

package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// ArgumentsOf returns the keys a command takes under the given declaration,
// or under its first one if the dumps do not declare it as the given one. It
// returns nil if it takes none there.
func (c *Command) ArgumentsOf(declaration game.Declaration) *Field {
	return c.Arguments[c.resolveDeclaration(declaration)]
}

// resolveDeclaration returns the given declaration if the dumps declare the
// command as it or as nothing, else the first one they declare it as.
func (c *Command) resolveDeclaration(declaration game.Declaration) game.Declaration {
	if slices.Contains(c.Declared, declaration) || len(c.Declared) == 0 {
		return declaration
	}

	return c.Declared[0]
}

// FindCommand returns the command a key names, as written or else in lower
// case. It returns false if the dumps declare none.
func (s *Schema) FindCommand(key string) (*Command, bool) {
	command, ok := s.Commands[key]
	if !ok {
		command, ok = s.Commands[strings.ToLower(key)]
	}

	return command, ok
}

// findKeyCall parses a key written as a command's name with one argument in
// parentheses, such as squared_distance(root), and returns the call's step
// and the command. The step's spans are offsets in the file, where the key
// starts at the given offset. It returns false for any other key, and when
// the name is no command.
func (s *Schema) findKeyCall(key string, start int) (jomini.Step, *Command, bool) {
	if !strings.HasSuffix(key, ")") {
		return jomini.Step{}, nil, false
	}

	call, ok := jomini.ParseCall(key, start)
	if !ok || len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 1 || call.Span.End != start+len(key) {
		return jomini.Step{}, nil, false
	}

	command, ok := s.FindCommand(call.Steps[0].Text)

	return call.Steps[0], command, ok
}

// findStatementCall does what findKeyCall does for a statement's key. A key
// compared with a block, such as list_size(x) < { value = … }, writes no
// call: the game reads that block as a value to work out.
func (s *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Command, bool) {
	current := tree.Statements[statement]
	if operator := tree.Text(current.Op); current.IsBlock() && operator != "=" && operator != "?=" {
		return jomini.Step{}, nil, false
	}

	span := tree.UnquotedSpan(current.Key)

	return s.findKeyCall(tree.Text(span), span.Start)
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
// fills the pattern's star. It returns false if the key does not fit the
// pattern, leaves nothing for the star, or has a dot there. A dot means a
// chain of links, and no definition's name holds one.
func (c CommandPattern) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(c.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)

	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the command patterns that make a key, or none. A
// command the dumps declare is made by the patterns that list it, and any
// other key by the fitting pattern with the shortest star, the first on a
// tie, in every database that has it.
func (s *Schema) findPatterns(key string) []CommandPattern {
	key = strings.ToLower(key)
	command := s.Commands[key]
	if !strings.Contains(key, "_") || command != nil && !command.Patterned {
		return nil
	}

	var best []CommandPattern
	shortest := len(key)
	for _, pattern := range s.CommandPatterns {
		start, end, fits := pattern.findStar(key)
		switch {
		case !fits: // the key does not fit this pattern
		case command != nil:
			if _, listed := slices.BinarySearch(pattern.Commands, key); listed {
				best = append(best, pattern)
			}
		case end-start < shortest:
			best, shortest = []CommandPattern{pattern}, end-start
		case end-start == shortest && pattern.Key == best[0].Key:
			best = append(best, pattern)
		}
	}

	return best
}

// ResolveLink returns the scope that a link leads to from the given scope. It
// returns false if the link is unknown, wild, leads nowhere or does not run in
// that scope.
func (s *Schema) ResolveLink(link, scope string) (string, bool) {
	command, ok := s.Commands[link]
	if !ok || command.Wild || !slices.Contains(command.Declared, game.DeclarationLink) || command.Target == "" {
		return "", false
	}

	if scope != "" && len(command.Scopes) > 0 && !slices.Contains(command.Scopes, scope) {
		return "", false
	}

	return command.Target, true
}

// isScope reports whether a name is a scope type that script runs in, meaning
// the dumps declare triggers or effects for it. It is false for names the
// dumps use for no scope, such as none, though they declare them like one.
func (s *Schema) isScope(scope string) bool {
	scopeType := s.Scopes[scope]

	return (scopeType.Triggers || scopeType.Effects) && !slices.Contains(dumpPhrases.NoScope, scope)
}

// IsLink reports whether a key or a value starts with a scope link, ignoring
// case, as scope:target and prev do.
func (s *Schema) IsLink(key string) bool {
	head, prefixed := splitLinkHead(key)
	command := s.Commands[head]
	if prefixed {
		_, reserved := s.Prefixes[head]
		return reserved || command != nil && command.Wild
	}

	return command != nil && !command.Wild && slices.Contains(command.Declared, game.DeclarationLink)
}

// isKeyLink reports whether a key starts with a scope link, as IsLink does,
// but a link that requires data, as EU5's modifier:<key> does, is none when
// written alone.
func (s *Schema) isKeyLink(key string) bool {
	head, prefixed := splitLinkHead(key)

	return s.IsLink(key) && (prefixed || s.Commands[head].Bare)
}

// splitLinkHead returns the first link of a chain in lower case, or only the
// prefix of one such as scope:target, with true.
func splitLinkHead(key string) (string, bool) {
	head := jomini.Links(key, 0)[0]
	if !head.Prefix.IsEmpty() {
		return strings.ToLower(key[head.Prefix.Start:head.Prefix.End]), true
	}

	return strings.ToLower(key[head.Span.Start:head.Span.End]), false
}
