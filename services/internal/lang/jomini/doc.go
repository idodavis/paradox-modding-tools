// Package jomini is the game-agnostic Paradox/Jomini script language: its
// syntax tree and the grammar rules that every game built on the engine shares.
//
// The parser half answers "what is the syntax tree of this script text?" It
// produces an error-tolerant concrete syntax tree (CST) and never panics on any
// input. All offsets are UTF-8 byte indices into the source string (unlike the
// toolkit it is ported from, which used UTF-16 code units); the UTF-16 seam
// lives only at the Monaco boundary in the frontend.
//
// The grammar half answers "what does this piece of script mean in any game?" —
// typed prefixes (`culture:english`), ephemeral ones (`scope:` / `var:`), how a
// folder name becomes a kind, which block names open a trigger or effect slot,
// and the assignment keys that name a saved scope, a variable or a `$PARAM$`.
//
// Files: encoding.go (Decode), lexer.go, cst.go, parse.go (Parse / ParseFile),
// prefix.go (typed and ephemeral prefixes), script.go (shared grammar).
//
// This package is a leaf: it must never import game or workspace state. Where a
// title reserves part of the shared grammar for itself — EU5 spending `INJECT:`
// on entry modes — the game package wraps the rule here rather than this
// package learning about games. Parent parser/ is a folder, not a package.
package jomini
