// registry.go is the one file in the backend that knows the games apart.
//
// Everything per-game lives in the GameInfo table below — script root, stage
// roots, descriptor format, docs folder, entry-mode prefixes, which kinds
// resolve first-in-order — and the handful of functions under it read that
// table rather than switching on a game id. The shared Clausewitz/Jomini
// grammar those functions build on is in parser/jomini, which knows nothing
// about any game; install discovery is in detect.go, which reads this table.
//
// The membership test for anything added here: does its behaviour differ
// between CK3, Victoria 3 and EU5? If not, it is language or filesystem work
// and belongs elsewhere, whatever parameters it happens to take.

package game

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/parser/jomini"
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

	// EntryModes are EU5 database entry-mode prefixes (INJECT:, REPLACE:, ...).
	EntryModes []string `json:"entryModes,omitempty"`

	SteamAppID int `json:"steamAppId,omitempty"`

	// FIOSKinds are the kinds this game resolves first-in-order; every other
	// kind is last-in-order. Backend-only — FirstWins is the same fact as the
	// sentence the Conflicts and Settings pages show.
	FIOSKinds []string `json:"-"`
	FirstWins string   `json:"firstWins,omitempty"`
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
		FIOSKinds:        []string{"gui_type"},
		FirstWins:        "GUI types and templates are first-wins.",
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
		EntryModes:       []string{"INJECT", "REPLACE"},
		SteamAppID:       529340,
		FIOSKinds:        []string{"gui_type", "event"},
		FirstWins:        "GUI types, templates, and events are first-wins.",
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
		EntryModes:       []string{"INJECT", "REPLACE", "TRY_INJECT", "TRY_REPLACE", "INJECT_OR_CREATE", "REPLACE_OR_CREATE"},
		SteamAppID:       3450310,
		FIOSKinds:        []string{"event"},
		FirstWins:        "Events are first-wins; GUI types last-wins.",
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

// ExtractMode names how definition keys are read out of a file.
type ExtractMode string

const (
	ModeTopLevelKey ExtractMode = "top-level-key" // root `key = { ... }`
	ModeEventID     ExtractMode = "event-id"      // `namespace.1 = { ... }`
	ModeGUIType     ExtractMode = "gui-type"      // GUI `type X = ...` / `template X { }`
	ModeLocKey      ExtractMode = "loc-key"       // localization `key: "..."`
)

// ExtractRule says what kind of definitions a file yields and how to read them.
type ExtractRule struct {
	Kind string
	Mode ExtractMode
}

// MatchExtract maps a file path (relative to the game/mod script root) to its
// extract rule. The default is "parent folder name => kind, top-level-key";
// events/gui/localization override that. EU5 stage-root prefixes are ignored.
func MatchExtract(gameID, relPath string) ExtractRule {
	segs := stripStageRoot(gameID, pathSegments(relPath))
	if slices.Contains(segs, "localization") {
		return ExtractRule{Kind: "loc_key", Mode: ModeLocKey}
	}
	if isModFile(relPath) {
		return ExtractRule{Kind: "mod_descriptor", Mode: ModeTopLevelKey}
	}
	parent := parentFolder(segs)
	switch {
	case parent == "events" || slices.Contains(segs, "events"):
		return ExtractRule{Kind: "event", Mode: ModeEventID}
	case parent == "on_action" || parent == "on_actions" ||
		slices.Contains(segs, "on_action") || slices.Contains(segs, "on_actions"):
		return ExtractRule{Kind: "on_action", Mode: ModeTopLevelKey}
	case parent == "gui" || strings.HasSuffix(strings.ToLower(relPath), ".gui"):
		return ExtractRule{Kind: "gui_type", Mode: ModeGUIType}
	default:
		return ExtractRule{Kind: jomini.CanonicalKind(parent), Mode: ModeTopLevelKey}
	}
}

// isModFile reports whether relPath is a Paradox .mod descriptor.
func isModFile(relPath string) bool {
	p := strings.ReplaceAll(relPath, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return strings.HasSuffix(strings.ToLower(p), ".mod")
}

// IsFIOS reports whether kind resolves first-in-order for this game.
// Empty/unknown gameID keeps the historical gui_type-only default.
func IsFIOS(gameID, kind string) bool {
	if g := Get(gameID); g != nil {
		return slices.Contains(g.FIOSKinds, kind)
	}
	return kind == "gui_type"
}

// ParseTyped splits `culture:english` into a harvested kind and id, rejecting
// the prefixes this game spends on entry modes. Callers with a game id should
// use this rather than jomini.ParseTyped, or EU5's `INJECT:foo` harvests a
// database named "inject".
func ParseTyped(gameID, text string) (kind, id string, ok bool) {
	if i := strings.IndexByte(text, ':'); i > 0 && isEntryMode(gameID, text[:i]) {
		return "", "", false
	}
	return jomini.ParseTyped(text)
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
			m[mode] = true
		}
	}
	return m
}()

func isEntryMode(gameID, tok string) bool {
	if g := Get(gameID); g != nil {
		return slices.Contains(g.EntryModes, tok)
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

// pathSegments splits a slash- or backslash-separated path into non-empty segments.
func pathSegments(p string) []string {
	raw := strings.Split(strings.ReplaceAll(p, "\\", "/"), "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	return out
}

// parentFolder returns the directory segment immediately containing the file.
func parentFolder(segs []string) string {
	if len(segs) >= 2 {
		return segs[len(segs)-2]
	}
	return ""
}

// stripStageRoot drops a leading EU5 stage-root segment (in_game, etc.) so extract
// rules see the same folder layout across games.
func stripStageRoot(gameID string, segs []string) []string {
	g := Get(gameID)
	if g == nil || len(segs) == 0 {
		return segs
	}
	if slices.Contains(g.StageRoots, segs[0]) {
		return segs[1:]
	}
	return segs
}
