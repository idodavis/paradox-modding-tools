// Package catalog builds and persists VanillaCache (install scan) and harvests
// workspace mods into session maps. It answers "what objects/refs/edges exist?"
// by scanning the game install and indexing mods. All game knowledge is DERIVED
// here from the install — nothing is bundled. It imports jomini, loc, and game.
// It must not import session. Wails DTOs with Rel/OriginName live in views.
//
// Files: types.go (Def/VanillaCache + format versions), scan.go, extract.go
// (ExtractFile, ExtractParsed, BuildIndex, Harvest), override.go (Winner + Contests), persist.go.
package derived
