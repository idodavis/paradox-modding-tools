// ignore.go is the ignore rules of a .gitignore, as Steam's upload and a search's folders read them.

package inventory

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Rules are ignore rules in the order written; the last one that matches a path decides.
type Rules []rule

// rule is one line of an ignore file: the glob doublestar matches, whether it takes a match back, and whether it
// matches folders only.
type rule struct {
	glob    string
	negated bool
	dirOnly bool
}

// IgnoreRules is the rules of an ignore text, one a line in the git form: a blank line and a # comment are no rule, a
// leading ! takes a match back, a trailing / matches folders only, a leading / anchors the rule to the root, any other
// rule matches at any depth, and ** spans folders.
func IgnoreRules(text string) Rules {
	var rules Rules
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var next rule
		if rest, negated := strings.CutPrefix(line, "!"); negated {
			line, next.negated = strings.TrimSpace(rest), true
		}
		line, next.dirOnly = strings.CutSuffix(line, "/")
		if rest, anchored := strings.CutPrefix(line, "/"); anchored {
			line = rest
		} else {
			line = "**/" + line
		}
		if line != "**/" && line != "" {
			next.glob = line
			rules = append(rules, next)
		}
	}
	return rules
}

// Covers is whether the rules leave a file out, rel its path under the root written with slashes: they match the file
// or a folder above it, which takes in everything under it and which no later rule can take back.
func (rules Rules) Covers(rel string) bool {
	parts := strings.Split(rel, "/")
	for depth := range parts {
		if rules.Matches(strings.Join(parts[:depth+1], "/"), depth < len(parts)-1) {
			return true
		}
	}
	return false
}

// Matches is whether the rules match rel, a path under the root written with slashes (isDir: a folder): the last rule
// that matches it decides.
func (rules Rules) Matches(rel string, isDir bool) bool {
	matched := false
	for _, next := range rules {
		if next.dirOnly && !isDir {
			continue
		}
		if matches, err := doublestar.Match(next.glob, rel); err == nil && matches {
			matched = !next.negated
		}
	}
	return matched
}
