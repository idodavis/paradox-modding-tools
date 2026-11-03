// game.go holds each game's row: the facts that are its own beside the shared tables.

// Package game holds every fact about the supported games: which games exist, where each is installed, and what
// its script, GUI and coat of arms files mean. Every fact is a row of a table or a field of a game's row.
package game

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Source is where a fact the schema learned came from. A lower value outranks a higher one, and a claim keeps its
// best source.
type Source uint8

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone     Source = iota // no source
	SourceRegistry               // this package's rows: they classify and never accuse
	SourceDumps                  // the console dumps: what the game declares about its own language
	SourceExe                    // a message the game's executable carries
	SourceLog                    // a line the game's own logs write
	SourceDocs                   // the prose the install ships beside its script (.info, .md, readme.txt)
	SourceWiki                   // the community wiki, taken where no game source contradicts it
	SourceVanilla                // the install's own files: evidence, never a claim that accuses
	SourceInferred               // no source states it: written as inferred, it never fails a verdict
	// SourceDefinitions is what a definition in the files says its name takes. It claims on fields of its own, so its
	// rank among the others is never tried.
	SourceDefinitions
)

// A Program is how a game is started on one operating system: its executable and the flags for debug mode. A game
// whose row has no executable cannot be started.
type Program struct {
	Executable string   // from the install root (binaries/ck3.exe); "" means none is known, so the game is not started
	DebugArgs  []string // the flags that start it in debug mode, which hot-loads script
	Settings   string   // the launcher settings file whose exePath and debug arguments win; "" means none
}

// A Log is one of the game's log files: where it is written under the user-data folder and how each of its lines
// starts.
type Log struct {
	Rel  string
	Line *regexp.Regexp
}

// A Game is one supported game with every fact that is its own. The tables beside it hold the facts it shares, each
// row naming the games it holds for.
type Game struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"` // also its folder under Documents
	ShortName     string             `json:"shortName"`
	StageRoots    []string           `json:"-"`                    // EU5's stages at a root, in the order it mounts them
	ModDescriptor string             `json:"descriptor,omitempty"` // the descriptor file's path from the mod's root
	ThumbnailDir  string             `json:"-"`                    // where a new listing image goes; "" means the root
	SteamAppID    int                `json:"-"`
	SteamLibrary  map[string]string  `json:"-"` // by GOOS, where its copy of Steam's library sits
	ModFolders    []string           `json:"-"` // the folders a new mod gets, from its root
	Program       map[string]Program `json:"-"` // by GOOS; a GOOS with no row has no build
	// ErrorLocations are the forms in which the error log names a line of a script file, each with the groups path and
	// line; empty where it names none.
	ErrorLocations []*regexp.Regexp `json:"-"`
	Events         GameEvents       `json:"-"`
	// TakesModes says whether a definition's name may start with an entry mode, as REPLACE:x does.
	TakesModes    bool   `json:"-"`
	ConceptPrefix string `json:"-"` // a concept's loc key prefix; "" means none
	// LocReplaceFolders are the forms of the folder whose localization overrides every other, with the language at %s,
	// the one that wins first.
	LocReplaceFolders []string `json:"-"`
	// LogsRepeatedKey says whether the game logs a localization key that a mod and the game both write outside a
	// replace folder.
	LogsRepeatedKey bool `json:"-"`
	// DecisionEffects is the effects that run the decision they name, as its dumps declare them. With none, no effect
	// fires a decision.
	DecisionEffects []string `json:"-"`
	// MessageTitle is the localization key of a message's title where the message writes none, with its name at %s.
	// "" means the name itself.
	MessageTitle string `json:"-"`
	// BookmarkFolder is the folder of the files that write the game's bookmarks, and SetupKey the key in a bookmark
	// whose value is the folder of its start setup. A game with a single start has neither.
	BookmarkFolder, SetupKey string   `json:"-"`
	GUI                      GameGUI  `json:"-"`
	Coat                     GameCoat `json:"-"`
	Guide                    Guide    `json:"guide"`
}

// A Guide is how a game picks which copy of a file or a definition it keeps, in the sentences where the games differ.
type Guide struct {
	Hint    string `json:"hint"`    // what the load order decides, in one sentence
	Files   string `json:"files"`   // which of a mod's files the game reads
	Kept    string `json:"kept"`    // which copy each kind of definition keeps
	Changes string `json:"changes"` // what the load order changes
}

// The supported games' ids.
const (
	ck3  = "ck3"
	vic3 = "vic3"
	eu5  = "eu5"
)

// The sets of two games that rows of the tables hold for.
var (
	ck3AndEU5  = []string{ck3, eu5}
	ck3AndVic3 = []string{ck3, vic3}
	vic3AndEU5 = []string{vic3, eu5}
)

// ScriptRoot is the folder under an install where the game's files start. Every scan and every relative path
// begins there.
const ScriptRoot = "game"

// InstallRoots are the folders under an install that the game mounts, in the order it mounts them. A relative
// path begins under one of them.
var InstallRoots = []string{"clausewitz", "jomini", ScriptRoot}

// BaseLanguage is the language that every other language's coverage is measured against.
const BaseLanguage = "english"

// OriginVanilla is the origin of the game's own files. A mod's files have the mod's id as their origin.
const OriginVanilla = "vanilla"

// MetadataDir is where a game whose mods are described by a metadata.json file keeps it and, for EU5, the thumbnail.
const MetadataDir = ".metadata"

// PictureExtensions are the extensions a mod's pictures may have: its thumbnail and the pictures of its gallery.
var PictureExtensions = []string{".png", ".gif", ".jpg", ".jpeg", ".webp"}

// WikiAPI is the address of a game's wiki API, with %s for the game's id.
const WikiAPI = "https://%s.paradoxwikis.com/api.php"

// The revision log every start writes, the launcher's settings file and the build's checksum.
const (
	revisionLogRel = "logs/code_revisions.log"
	launcherFile   = "launcher/launcher-settings.json"
	checksumRel    = "binaries/checksum.txt"
)

// ErrorLog is the log in which the game writes its errors, each line starting with the time and the source file and
// line that wrote it.
var ErrorLog = Log{
	"logs/error.log",
	regexp.MustCompile(`^\[(\d\d):(\d\d):(\d\d)\](?:\[([A-Z])\])?\[([A-Za-z0-9_]+\.(?:cpp|h|hpp|inl)):(\d+)\]: ?(.*)$`),
}

// The forms in which an error log names a line of a script file, each with the groups path and line. None matches a
// bare path and line: every log line's header holds its source as [file.cpp:N], so a path must follow a space.
var (
	// errorLogFileLine is CK3's "file: P line: N", after "Script location:", "at", "location '" or "from database".
	errorLogFileLine = regexp.MustCompile(`file: *(?P<path>\S+) line: *(?P<line>\d+)`)
	// errorLogQuotedLine is the line a quoted message names before its file, as in "Unknown effect: x, near line: 3501"
	// in file: "P" near line: 3502. All three games write it. The quoted line is the statement's. The file's line after
	// it is where the reader stopped, often the end of the statement's block.
	errorLogQuotedLine = regexp.MustCompile(`near line: (?P<line>\d+)" in file: "(?P<path>[^"]+)"`)
	// errorLogNearLine is the reader's "in file: "P" near line: N", written by all three games.
	errorLogNearLine = regexp.MustCompile(`in file: "(?P<path>[^"]+)" near line: (?P<line>\d+)`)
	// errorLogPathLine is "P:N" after other words, as in "Script location: P:N" and "]: P:N - Missing file".
	errorLogPathLine = regexp.MustCompile(`(?:^|\s)(?P<path>[^\s\[\]:'"]+\.\w+):(?P<line>\d+)\b`)
	// errorLogLineColumn is EU5's "line N and column C in P".
	errorLogLineColumn = regexp.MustCompile(`line (?P<line>\d+) and column \d+ in (?P<path>\S+)`)
)

// MountLog is the log line in which the game names each folder it reads.
var MountLog = Log{"logs/debug.log", regexp.MustCompile(`Mounted Data: (.+?)\s*$`)}

// DumpFolders are the folders under a game's user data where the console's dumps land.
var DumpFolders = []string{"logs", "docs", "logs/data_types"}

var steamLibrary = map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"}

var games = map[string]*Game{
	ck3: {
		ID:            ck3,
		Name:          "Crusader Kings III",
		ShortName:     "CK3",
		ModDescriptor: "descriptor.mod",
		SteamAppID:    1158310,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/ck3.exe", DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode", "-develop"}, Settings: launcherFile},
		},
		ErrorLocations: []*regexp.Regexp{errorLogFileLine, errorLogQuotedLine, errorLogNearLine, errorLogPathLine},
		// _events.info, _event_themes.info
		Events: GameEvents{DefaultScope: "character", Picture: Picture{
			Chains: []PictureChain{
				{
					Steps:   []PictureStep{{Key: "override_background/reference", Database: "event_backgrounds"}},
					FileKey: "background/reference",
				},
				{
					Steps: []PictureStep{
						{Key: "theme", Database: "event_themes"}, {Key: "background/reference", Database: "event_backgrounds"},
					},
					FileKey: "background/reference",
				},
			},
		}},
		ConceptPrefix: "game_concept_",
		// the wiki's Localization page: localization/replace/english takes precedence over localization/english/replace
		LocReplaceFolders: []string{"localization/replace/%s", "localization/%s/replace"},
		// the wiki's Modding page; ck3.exe "Duplicate localization key. Key '%s' is defined in both '%s' and '%s'."
		LogsRepeatedKey: true,
		// effects.log: "execute_decision - Execute the specified decision for the scoped character"
		DecisionEffects: []string{"execute_decision"},
		GUI: GameGUI{
			Undeclared: []string{"zoomarea", "text_occluder"},
			// common/defines/graphic/00_graphics.txt:812-813: GOVERNMENT_TYPE_REALM_MASK_PATH with
			// GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX
			SuffixStems: map[string]string{"GOVERNMENT_TYPE_REALM_MASK": "GOVERNMENT_TYPE_REALM_FRAME_MASK"},
			// common/defines/graphic/00_graphics.txt:782 writes TRAIT_ICON_PATH in it, and :812 the realm mask's path
			DefineGroup: "NGameIcons",
			// seven tiers: cell 3 for a count, 4 for a duke, from 5 for a king or an emperor
			Tiers: TierFrames{
				Function: "GetTierFrame", Tiers: []string{"unlanded", "baron", "count", "duke", "king", "emperor", "hegemon"},
			},
			// gfx/interface/illustrations/scripted_illustrations/_scripted_illustrations.info
			Illustrations: Illustrations{FolderKey: "folder", Folder: "gfx/interface/illustrations"},
		},
		Coat: GameCoat{
			Folder: "common/coat_of_arms/coat_of_arms",
			Shade: CoatShade{ // game/gfx/FX/coat_of_arms
				Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
				Overlay: CoatStrength{Channel: "b", Strength: 0.2},
				Grain:   CoatStrength{Channel: "g", Strength: 2},
			},
			// the frames of gui/shared/coa_designer.gui, without the unit's 3D banner. Each define is named in full, as
			// common/defines/graphic/00_graphics.txt:309,319 write them in NCoatOfArms.
			Frames: []CoatFrame{
				{
					Label: "Dynasty", Define: "NCoatOfArms|DEFAULT_DYNASTY_COA_FRAME_NAME",
					Override: CoatOverride{
						Frame: "NCoatOfArms|OVERRIDE_DYNASTY_COA_FRAME_NAME", Offset: "NCoatOfArms|OVERRIDE_DYNASTY_COA_FRAME_OFFSET",
						Scale: "NCoatOfArms|OVERRIDE_DYNASTY_COA_FRAME_SCALE",
					},
				},
				{
					Label: "House", Define: "NCoatOfArms|DEFAULT_HOUSE_COA_FRAME_NAME",
					Override: CoatOverride{
						Frame: "NCoatOfArms|OVERRIDE_HOUSE_COA_FRAME_NAME", Offset: "NCoatOfArms|OVERRIDE_HOUSE_COA_FRAME_OFFSET",
						Scale: "NCoatOfArms|OVERRIDE_HOUSE_COA_FRAME_SCALE",
					},
				},
				{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
				{
					Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds",
					Override: CoatOverride{
						Enabled: "NCoatOfArms|OVERRIDE_REALM_MASK_ENABLED", Offset: "NCoatOfArms|OVERRIDE_REALM_MASK_OFFSET",
						Scale: "NCoatOfArms|OVERRIDE_REALM_MASK_SCALE",
					},
				},
			},
			ColorForm: jomini.ColorForm{Tag: "rgb", Example: []float64{255, 255, 255}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events keep the highest override priority, and on a tie the game logs an error and keeps one copy without saying which; a character takes its name, traits, skills, parents and birth from the copy with the highest override priority and combines the rest, and the game calls a tie an error without saying what it keeps; on_actions combine their lists.",
			Changes: "Only files at the same path.",
		},
	},
	vic3: {
		ID:            vic3,
		Name:          "Victoria 3",
		ShortName:     "Vic3",
		ModDescriptor: MetadataDir + "/metadata.json",
		SteamAppID:    529340,
		SteamLibrary:  steamLibrary,
		ModFolders:    []string{"common", "events", "localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/victoria3.exe", DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
			"linux":   {DebugArgs: []string{"-debug_mode"}, Settings: launcherFile},
		},
		ErrorLocations: []*regexp.Regexp{errorLogQuotedLine, errorLogNearLine, errorLogPathLine},
		Events: GameEvents{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: Picture{
			Chains: []PictureChain{{
				Steps:   []PictureStep{{Key: "event_image/video", Database: "gfx/media_aliases"}},
				FileKey: "video", Fallback: "fallback",
			}},
		}},
		TakesModes: true,
		// localization/english/content_304_l_english.yml:228 names arranged_accident_failed's title so
		MessageTitle: "notification_%s_name",
		// victoria3.exe matches the path part /replace/; no source says which form wins
		LocReplaceFolders: []string{"localization/%s/replace", "localization/replace/%s"},
		GUI: GameGUI{
			Undeclared:  []string{"overlappingitembox", "treemapchart", "portrait_button", "axis"},
			DropsHidden: []string{"flowcontainer"},
		},
		Coat: GameCoat{
			Folder:    "common/coat_of_arms/coat_of_arms",
			ColorForm: jomini.ColorForm{Tag: jomini.TagHSV360, Example: []float64{360, 100, 100}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder. This is documented for CK3 and EU5, whose engine Vic3 shares; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events and GUI types keep the first; where a folder takes INJECT: and REPLACE:, the first plain copy stays and a later one is refused, so mods change it with those keywords, which act after every plain copy whatever their file's name, by kind (INJECT_OR_CREATE first, REPLACE last) and then by file name; character DNA copies all exist.",
			Changes: "Only files at the same path.",
		},
	},
	eu5: {
		ID:        eu5,
		Name:      "Europa Universalis V",
		ShortName: "EU5",
		// debug.log:5372-5394 mounts each root's stages in this order, and only them; the wiki's Mod structure, l.46-57
		StageRoots:    []string{"loading_screen", "main_menu", "in_game"},
		ModDescriptor: MetadataDir + "/metadata.json",
		ThumbnailDir:  MetadataDir, // where EU5 mods keep it
		SteamAppID:    3450310,
		SteamLibrary:  map[string]string{"windows": "binaries/steam_api64.dll"}, // no Linux build: Proton
		ModFolders:    []string{"in_game/common", "in_game/events", "main_menu/localization"},
		Program: map[string]Program{
			"windows": {Executable: "binaries/eu5.exe", DebugArgs: []string{"-debug_mode"}},
		},
		ErrorLocations: []*regexp.Regexp{errorLogQuotedLine, errorLogNearLine, errorLogPathLine, errorLogLineColumn},
		Events: GameEvents{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: Picture{
			FileKey: "image",
			Chains: []PictureChain{{
				Steps: []PictureStep{
					{Key: "illustration_tags", Database: "gfx/illustrations/database", Match: "tags"},
					{Key: "scene", Database: "gfx/scenes"},
					// a scene's layers are written front to back, and its backdrop is the one drawn on background2_mesh,
					// as in_game/gfx/scenes/00_scenes_events.txt writes each of its characters_ scenes
					{Key: "ecs_illustration_image[pdxmesh=background2_mesh]/list", Database: "gfx/images", Match: "tag"},
				},
				FileKey: "illustration_image/texture_file",
			}},
		}},
		TakesModes:    true,
		ConceptPrefix: "game_concept_",
		// the wiki's Mod compatibility, l.209, and Localization, l.2, name only this form
		LocReplaceFolders: []string{"localization/%s/replace"},
		// main_menu/common/bookmarks/00_bookmarks.txt:4
		BookmarkFolder: "common/bookmarks", SetupKey: "setup_folder",
		GUI: GameGUI{
			Holders:    map[string]string{"lateralview": "lateralview_left_layer"},
			Undeclared: []string{"axis", "plotline", "screen_grab", "newsfeed_carousel"},
			// loading_screen/common/defines/graphic/00_graphics.txt writes its kinds' icon paths in it
			DefineGroup: "NGameIcons",
		},
		Coat: GameCoat{
			Folder:    "main_menu/common/coat_of_arms/coat_of_arms",
			ColorForm: jomini.ColorForm{Tag: jomini.TagHSV360, Example: []float64{360, 100, 100}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path, and GUI types and templates.",
			Files:   "A mod's files count only inside its in_game, main_menu or loading_screen folder. A mod's replace_path makes the game skip its own files in that folder, not in its subfolders.",
			Kept:    "Under common/ the first plain copy stays and mods change it with INJECT: and REPLACE:, which act after every plain copy whatever their file's name, by kind (INJECT_OR_CREATE first, REPLACE last) and then by file name; events keep the first; GUI types and templates follow the load order; coats of arms and defines keep the last.",
			Changes: "Only files at the same path and GUI types and templates.",
		},
	},
}

// CanAccuse reports whether a claim from this source is firm enough to fail a verdict. Only the dumps, the docs, the
// wiki and the definitions are.
func (source Source) CanAccuse() bool {
	return source == SourceDumps || source == SourceDocs || source == SourceWiki || source == SourceDefinitions
}

// LookupGame returns the game with the given id, or nil if this build does not support it.
func LookupGame(id string) *Game {
	return games[id]
}

// ListGames returns the supported games in the order the app shows them.
func ListGames() []Game {
	return []Game{*games[ck3], *games[eu5], *games[vic3]}
}

// Matches reports whether the game is among the given games. An empty list names every game.
func (game *Game) Matches(games []string) bool {
	return len(games) == 0 || slices.Contains(games, game.ID)
}

// SplitStage splits a path into the stage folder that opens it and the rest, for the games that keep their files
// under stages. A path under no stage, or a game with none, gives "" and the path as it is.
func (game *Game) SplitStage(rel string) (stage, rest string) {
	if game == nil {
		return "", rel
	}
	stage, rest, ok := strings.Cut(rel, "/")
	if ok && slices.Contains(game.StageRoots, stage) {
		return stage, rest
	}
	return "", rel
}

// FindLocReplacement returns the rank of the localization replace folder a file is in, the lowest winning. It returns
// false if the file is in none.
func (game *Game) FindLocReplacement(rel, language string) (int, bool) {
	_, rel = game.SplitStage(rel)
	rank := slices.IndexFunc(game.LocReplaceFolders, func(folder string) bool {
		return strings.HasPrefix(rel, fmt.Sprintf(folder, language)+"/")
	})
	return rank, rank >= 0
}

// IsUnmounted reports whether the game never mounts a file: a mod's file outside the stage folders, in a game that
// keeps its files under stages (EU5 debug.log:80-83, 5392; the wiki's Mod structure, l.46-48).
func (game *Game) IsUnmounted(file files.File) bool {
	stage, _ := game.SplitStage(strings.ToLower(file.Rel))
	return game != nil && len(game.StageRoots) > 0 && file.Origin != OriginVanilla && stage == ""
}
