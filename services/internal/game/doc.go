// Package game is the only game-aware package in the backend. It answers "what
// does this title call things, and where do they live?" for CK3, Victoria 3 and
// EU5, and nothing else needs a game id to read Paradox script.
//
// # Layout
//
// Files MIRROR the package whose game-specific logic they hold, so a break in
// internal/catalog is fixed in game/catalog.go and nowhere else:
//
//   - catalog.go — shipped documentation: file naming, the five field-doc
//     shapes, leading kind prose.
//   - registry.go — the GameInfo table plus the rules that read it: folder-to-
//     extract mapping, FIOS, entry-mode prefixes, key identity.
//   - detect.go — install discovery, user-data folders and install mtime.
//
// Every mirrored file opens with a WHAT / GAME / WHY table naming the games that
// caused each piece. Prefer expressing the knowledge as DATA a test drives —
// DocShapes in catalog.go is the model — over prose that can go stale silently.
//
// # What belongs here
//
// The membership test is behavioural: code belongs here only if it exists
// because a named title does something its siblings do not.
//
// Two things are often mistaken for that and do NOT belong:
//
//   - CALIBRATION. "CK3 types 861 fields of which 632 sit at >=90% coverage"
//     justifies a threshold in a rule that runs on every game. The rule is not
//     game-specific; only its evidence is. Those stay with the rule.
//   - SHARED GRAMMAR. Anything that takes a gameID without branching on it is
//     Clausewitz/Jomini and lives in parser/jomini (stop words, name shapes,
//     script slots, typed prefixes, ephemeral names) or parser/loc; mod
//     packaging lives in modfile.
//
// It contains NO bundled schema or data dumps — the type system comes from the
// game's own script_docs and the object databases from the install walk.
package game
