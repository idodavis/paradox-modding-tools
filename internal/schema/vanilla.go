package schema

import "paradox-modding-tools/internal/jomini"

// CountVanilla learns from the game's own files by three plain rules and no tuned number. A key written under a
// database's definitions becomes a field with game.SourceVanilla once it has been seen once. Its shape claim is
// the shape most of its writes have. Its value claim is a type only when every written value matches that type.
// The names written after each prefix and the colors are counted the same way. Before counting, the top-level
// statements of the files that declare callables and widget types are read without going into their blocks, so
// that a key under a call is counted as a call. Each file is parsed with the given function. A vanilla claim helps
// completion and marks a hover card as inferred; it never fails a verdict.
func (schema *Schema) CountVanilla(paths []string, parse func(path string) (*jomini.Tree, error)) error {
	panic("stub")
}
