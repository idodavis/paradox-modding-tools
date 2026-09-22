// Package game is the only game-aware package in the backend: it answers what
// a title calls things and where they live, for CK3, Vic3 and EU5. detect.go
// is install discovery. Shared grammar with no game branch belongs in
// lang/jomini, not here.
//
// registry.go is the one file that knows the games apart: everything
// per-game lives in the GameInfo table below, and the functions under it read
// that table rather than switching on a game id. Membership test for
// anything added here: does its behaviour differ between CK3, Vic3 and EU5?
package game

import (
	"slices"
	"strings"
)

// OriginVanilla is the session/IDE origin id for game files (not a cache filename).
const OriginVanilla = "vanilla"

// GameInfo is the static identity and convention set for one game. It carries no
// derived data — the model package scans the install for objects, vocabulary, etc.
type GameInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`

	// WikiAPI is the MediaWiki api.php endpoint for this game's wiki.
	WikiAPI string `json:"wikiApi,omitempty"`

	// ScriptRoot is the install-relative folder holding script (e.g. "game").
	// EU5 still uses this: stages live under game/{in_game,main_menu,loading_screen}.
	// Mods omit game/ and put StageRoots at the mod root.
	ScriptRoot string   `json:"scriptRoot,omitempty"`
	StageRoots []string `json:"stageRoots,omitempty"`

	// Descriptor is how a mod root is identified: "mod" (descriptor.mod) or
	// "metadata" (.metadata/metadata.json).
	Descriptor string `json:"descriptor,omitempty"`

	// ThumbnailDir is where a NEW listing image goes, relative to the mod root:
	// "" for the root itself, ".metadata" beside the metadata file.
	//
	// A separate field because it is a separate fact. Keying it off Descriptor
	// was wrong for a whole game: Victoria 3 uses metadata.json AND puts its
	// thumbnail at the root, and not one of the 14 Vic3 workshop mods measured
	// puts it in .metadata alone. GAME-SYNTAX has the table. Only consulted when
	// the mod has no thumbnail yet — an existing one keeps its place.
	//
	// Backend-only, like FIOSKinds: it decides where PMT writes a file, and the
	// frontend is told the resulting path on the listing.
	ThumbnailDir string `json:"-"`

	// DocsFolderName is the per-game Documents subfolder (e.g. "Crusader Kings III").
	DocsFolderName string `json:"docsFolderName,omitempty"`
	// ScriptDocsSubdir is where in-game console dumps land ("logs" or "docs").
	ScriptDocsSubdir string `json:"scriptDocsSubdir,omitempty"`

	// EntryModes are the words a mod may write before a definition's name (REPLACE:name), in the order the game processes them; file order decides only between entries of one mode. Victoria 3's and EU5's executables hold all six words and CK3's none (2026-09-21). Backend-only, like FIOSKinds.
	EntryModes []EntryMode `json:"-"`

	SteamAppID int `json:"steamAppId,omitempty"`

	// FIOSKinds are the kinds this game resolves first-in-order; every other
	// kind is last-in-order. Backend-only — FirstWins is the same fact as the
	// sentence the Conflicts and Settings pages show.
	FIOSKinds []string `json:"-"`
	FirstWins string   `json:"firstWins,omitempty"`

	// SubjectDirs are the folders whose definitions are named after other kinds' definitions: history, coats of arms, flags. The learner makes a kind under one a subject kind of the kind that defines its names, and gives it no prefix. They are parent folders, so a folder the game adds below one needs no row. Backend-only, like FIOSKinds.
	SubjectDirs []string `json:"-"`
}

// EntryMode is one word written before a definition's name, and what it does to the definition of that name already loaded.
type EntryMode struct {
	Word     string
	Replaces bool // takes the earlier definition's place; an inject adds to it and never displaces it
	Optional bool // TRY_: does nothing where there is none
	Creates  bool // _OR_CREATE: defines it where there is none
}

// entryModes is the six words Victoria 3 and EU5 share, in processing order (both wikis, Mod compatibility).
var entryModes = []EntryMode{
	{Word: "INJECT_OR_CREATE", Creates: true}, {Word: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Word: "TRY_INJECT", Optional: true}, {Word: "TRY_REPLACE", Replaces: true, Optional: true},
	{Word: "INJECT"}, {Word: "REPLACE", Replaces: true},
}

// GameList is the Wails payload for ListGames (consts are not exported).
type GameList struct {
	OriginVanilla string      `json:"originVanilla"`
	Games         []*GameInfo `json:"games"`
}

// registry is the supported-game table, keyed by id.
var registry = map[string]*GameInfo{
	"ck3": {
		ID:               "ck3",
		Name:             "Crusader Kings III",
		ShortName:        "CK3",
		WikiAPI:          "https://ck3.paradoxwikis.com/api.php",
		ScriptRoot:       "game",
		Descriptor:       "mod",
		DocsFolderName:   "Crusader Kings III",
		ScriptDocsSubdir: "logs",
		SteamAppID:       1158310,
		FIOSKinds:        []string{"gui.type", "gui.template"},
		FirstWins:        "GUI types and templates are first-wins.",
		SubjectDirs:      []string{"history", "common/coat_of_arms"},
	},
	"vic3": {
		ID:               "vic3",
		Name:             "Victoria 3",
		ShortName:        "Vic3",
		WikiAPI:          "https://vic3.paradoxwikis.com/api.php",
		ScriptRoot:       "game",
		Descriptor:       "metadata",
		DocsFolderName:   "Victoria 3",
		ScriptDocsSubdir: "docs",
		EntryModes:       entryModes,
		SteamAppID:       529340,
		FIOSKinds:        []string{"gui.type", "gui.template", "events"},
		FirstWins:        "GUI types, templates, and events are first-wins.",
		SubjectDirs:      []string{"common/history", "common/coat_of_arms", "common/flag_definitions"},
	},
	"eu5": {
		ID:         "eu5",
		Name:       "Europa Universalis V",
		ShortName:  "EU5",
		WikiAPI:    "https://eu5.paradoxwikis.com/api.php",
		ScriptRoot: "game",
		StageRoots: []string{"in_game", "main_menu", "loading_screen"},
		Descriptor: "metadata",
		// 11 of 13 EU5 workshop mods; none at the root only.
		ThumbnailDir:     ".metadata",
		DocsFolderName:   "Europa Universalis V",
		ScriptDocsSubdir: "docs",
		EntryModes:       entryModes,
		SteamAppID:       3450310,
		FIOSKinds:        []string{"events"},
		FirstWins:        "Events are first-wins; GUI types last-wins.",
		SubjectDirs:      []string{"setup/start", "common/coat_of_arms", "common/flag_definitions"},
	},
}

// Get returns the GameInfo for id, or nil if the game is not supported.
func Get(id string) *GameInfo {
	return registry[id]
}

// All returns supported games in UI order (ck3, eu5, vic3).
func All() []*GameInfo {
	return []*GameInfo{registry["ck3"], registry["eu5"], registry["vic3"]}
}

// IsFIOS reports whether kind resolves first-in-order for this game.
// For an empty or unknown gameID only GUI types are.
func IsFIOS(gameID, kind string) bool {
	if g := Get(gameID); g != nil {
		return slices.Contains(g.FIOSKinds, kind)
	}
	return kind == "gui.type"
}

// KeyIdentity canonicalizes a raw key for matching/merge: trims whitespace and
// surrounding quotes, and strips an EU5 entry-mode prefix (INJECT:foo -> foo).
// gameID "" (e.g. merge, which is game-agnostic) strips any known EU5 mode.
func KeyIdentity(gameID, key string) string {
	key = trimQuotes(strings.TrimSpace(key))
	if i := strings.IndexByte(key, ':'); i > 0 && isEntryMode(gameID, key[:i]) {
		return key[i+1:]
	}
	return key
}

// anyEntryMode is the union of entry-mode prefixes across the registry, used
// when the game is unknown (merge passes gameID "").
var anyEntryMode = func() map[string]bool {
	m := map[string]bool{}
	for _, g := range registry {
		for _, mode := range g.EntryModes {
			m[mode.Word] = true
		}
	}
	return m
}()

func isEntryMode(gameID, tok string) bool {
	if g := Get(gameID); g != nil {
		return slices.ContainsFunc(g.EntryModes, func(m EntryMode) bool { return m.Word == tok })
	}
	return anyEntryMode[tok]
}

// trimQuotes removes a single pair of surrounding single or double quotes.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
