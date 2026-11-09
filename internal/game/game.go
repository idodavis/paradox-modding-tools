// Package game holds facts about the games PMT supports, Crusader Kings III,
// Victoria 3 and Europa Universalis V, as tables of rows: what each folder
// holds, which copy of a definition the game keeps, how a block of script
// reads its keys, and how GUI files and coats of arms are drawn. It also finds
// a game's installs on this machine, their versions, the user-data and dump
// folders, and the program that starts the game.
//
// It holds the rows that several packages read. A table that one package
// reads is that package's, beside the function that reads it. A fact that is
// one value for each game is a field of the game's row; a fact the games
// share is a row of a table that names the games it holds for.
//
// # Rows
//
//   - A row holds for the games its Games lists, and for every game when it
//     lists none.
//   - A game's own rows come before the rows of every game, and a lookup
//     takes the first row that fits.
//   - A row's comment cites where its fact comes from: a file of the install
//     and its line, a dump line, a message in the game's executable or a wiki
//     page. Nothing reads it; it lets a reader check the row against the game.
//   - A row classifies and never accuses. Only a claim from the dumps, the
//     docs, the wiki or a definition in the files is firm enough to fail a
//     verdict (Source.CanAccuse).
//
// # Databases
//
//   - A folder names its database: common/ and the extension are dropped, so
//     common/traits is traits. A folder with no Databases row is still a
//     database, whose top keys name its definitions.
//   - Which copy of a name the game keeps is said by the first Precedences
//     row that holds for the database, by its name or its folder. Where none
//     holds, the last copy by file name wins.
//   - In a game whose definitions take entry modes, a name may start with
//     one, as REPLACE:x does. A prefix that is no entry mode is part of the
//     name.
//   - A database with no DefinitionTexts row titles its definitions by their
//     names, and gives them no description.
//   - Of a game's localization replace folders, the one listed first wins.
//     For Vic3 no source says which wins, and the row's order is taken.
//
// # The language
//
//   - A block's shape lists the rules its keys are read by, in the order they
//     are tried. The first rule a key fits wins.
//   - A keyword fits a key in any letter case. Where several rows fit, the
//     first in Keywords wins, and a row written command/key fits only in that
//     command's block.
//
// # Installs
//
//   - The installs are found in Steam's own folders and in the libraries its
//     libraryfolders.vdf names, each counted once after links are resolved.
//   - The version is the one in the launcher's settings. Without it, it is
//     the version the game checks, from its debug log, or else the day the
//     game was built, as "build <date>", from its revision log, both read only
//     if the game started since its last update; else Steam's build number.
//   - The user-data folder is the first that exists of the launcher's
//     gameDataPath and the usual folders: Documents on Windows; the XDG data
//     folder, Documents and each Proton prefix on Linux. Where none exists, it
//     is the first of them.
//   - An install was last updated when the newest file in its binaries
//     folder, else at its root, was written. A folder's own time is no use,
//     since replacing a file in place leaves it unchanged.
//
// # Robustness
//
// Nothing here fails over what it reads from disk. A launcher settings file
// that is missing or does not decode reads as none, and a Steam library file,
// a manifest or a log that cannot be read is skipped. ListInstallRoots returns
// an error only for a DLC folder that exists and cannot be read. LookupGame
// returns nil for an id this build does not support.
package game

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// Source is where a fact the schema learned came from. A lower value outranks
// a higher one, and a claim keeps its best source.
type Source uint8

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone Source = iota

	// SourceRegistry is this package's rows: they classify and never accuse.
	SourceRegistry

	// SourceDumps is the console dumps: what the game declares about its own
	// language.
	SourceDumps

	// SourceExe is a message the game's executable carries, and SourceLog a
	// line the game's own logs write.
	SourceExe
	SourceLog

	// SourceDocs is the prose the install ships beside its script, in .info,
	// .md and readme.txt files.
	SourceDocs

	// SourceWiki is the community wiki, taken where no game source
	// contradicts it.
	SourceWiki

	// SourceVanilla is the install's own files: evidence, never a claim that
	// accuses.
	SourceVanilla

	// SourceInferred is what no source states, so it never fails a verdict.
	SourceInferred

	// SourceDefinitions is what a definition in the files says its name
	// takes. It claims on fields of its own, so its rank is never tried.
	SourceDefinitions
)

// Game is one supported game with every fact that is its own. The tables
// beside it hold the facts it shares, each row naming the games it holds for.
type Game struct {
	ID string `json:"id"`

	// Name is the game's name, which is also its folder under Documents.
	Name string `json:"name"`

	ShortName string `json:"shortName"`

	// Stages are the stage folders at a root, in the order the game
	// mounts them. They are none for a game without stages.
	Stages []string `json:"-"`

	// ModDescriptor is the descriptor file's path from a mod's root.
	ModDescriptor string `json:"descriptor,omitempty"`

	// FixedModFolder is whether the launcher reads local mods only from its
	// mod folder, so that a mod made elsewhere is never loaded.
	FixedModFolder bool `json:"fixedModFolder"`

	// ThumbnailDir is where a new listing image goes. It is "" for the root.
	ThumbnailDir string `json:"-"`

	SteamAppID int `json:"-"`

	// SteamLibrary is where the game keeps its copy of Steam's library, by
	// GOOS.
	SteamLibrary map[string]string `json:"-"`

	// ModFolders are the folders a new mod gets, from its root.
	ModFolders []string `json:"-"`

	// Program is how the game is started, by GOOS. A GOOS with no row has no
	// build.
	Program map[string]Program `json:"-"`

	// ErrorLocations are the forms in which the error log names a line of a
	// script file, with the groups path and line. None means it names none.
	ErrorLocations []*regexp.Regexp `json:"-"`

	Events GameEvents `json:"-"`

	// TakesModes is whether a definition's name may start with an entry mode,
	// as REPLACE:x does.
	TakesModes bool `json:"-"`

	// LocReplaceFolders are the forms of the folder whose localization
	// overrides every other, with the language at %s, the winning one first.
	LocReplaceFolders []string `json:"-"`

	// LogsRepeatedKey is whether the game logs a localization key that a mod
	// and the game both write outside a replace folder.
	LogsRepeatedKey bool `json:"-"`

	// DecisionEffects are the effects that run the decision they name, as the
	// dumps declare them. None means no effect fires a decision.
	DecisionEffects []string `json:"-"`

	// BookmarkFolder is the folder of the files that write the bookmarks, and
	// SetupKey the key in a bookmark whose value is the folder of its start
	// setup. Both are "" for a game with a single start.
	BookmarkFolder string `json:"-"`
	SetupKey       string `json:"-"`

	GUI     GameGUI  `json:"-"`
	Coat    GameCoat `json:"-"`
	Guide   Guide    `json:"guide"`
	Example Example  `json:"example"`
}

// Program is how a game is started on one operating system: its executable
// and the flags for debug mode.
type Program struct {
	// Executable is the program's path from the install root, as
	// binaries/ck3.exe. It is "" where none is known, and the game is then
	// not started.
	Executable string

	// DebugArgs are the flags that start the game in debug mode, which
	// hot-loads script.
	DebugArgs []string

	// Settings is the launcher settings file whose exePath and debug
	// arguments win. It is "" for none.
	Settings string
}

// Log is one of the game's log files.
type Log struct {
	// Rel is the log's path from the user-data folder, and Line how each of
	// its lines starts.
	Rel  string
	Line *regexp.Regexp
}

// Example is three definitions of one database that show how a mod and the
// game's files work together.
type Example struct {
	Database string `json:"database"`

	// Folder is the definitions' folder, from the game's folder.
	Folder string `json:"folder"`

	Names []string `json:"names"`

	// Modes is whether a mod changes one of the definitions by writing
	// REPLACE: before its name, as its precedence row says.
	Modes bool `json:"modes"`
}

// Guide is how a game picks which copy of a file or a definition it keeps, in
// the sentences where the games differ.
type Guide struct {
	// Hint is what the load order decides, in one sentence.
	Hint string `json:"hint"`

	// Files is which of a mod's files the game reads.
	Files string `json:"files"`

	// Kept is which copy of a definition each database keeps.
	Kept string `json:"kept"`

	// Changes is what the load order changes.
	Changes string `json:"changes"`
}

// WikiTopic gives the files of a format, or under a folder, the terms the
// games' wikis title their pages with.
type WikiTopic struct {
	// Format is the files' format, "" for any, and Folder their folder, ""
	// for any.
	Format files.Format
	Folder string

	Terms []string
}

// The supported games' ids.
const (
	CK3  = "ck3"
	Vic3 = "vic3"
	EU5  = "eu5"
)

// The sets of two games that rows of the tables hold for.
var (
	ck3AndEU5  = []string{CK3, EU5}
	ck3AndVic3 = []string{CK3, Vic3}
	vic3AndEU5 = []string{Vic3, EU5}
)

// ScriptRoot is the folder under an install where the game's files start.
const ScriptRoot = "game"

// BaseLanguage is the language that every other language's coverage is
// measured against.
const BaseLanguage = "english"

// MetadataDir is where a game whose mods are described by a metadata.json
// file keeps it and, for EU5, the thumbnail.
const MetadataDir = ".metadata"

// PictureExtensions are the extensions a mod's pictures may have: its
// thumbnail and the pictures of its gallery.
var PictureExtensions = []string{".png", ".gif", ".jpg", ".jpeg", ".webp"}

// WikiAPI is the address of a game's wiki API, with %s for the game's id.
const WikiAPI = "https://%s.paradoxwikis.com/api.php"

// WikiTopics are the topics the games' wikis write pages on beside a file
// format's own name.
var WikiTopics = []WikiTopic{
	// CK3's Interface, Vic3's Interface modding and EU5's Interface modding
	// guide, which also write of gfx/interface
	{Format: files.FormatGUI, Terms: []string{"interface"}},
	{Folder: "gfx/interface", Terms: []string{"interface"}},
	// CK3's 3D models and Vic3's Model modding, which write of .asset files,
	// meshes and gfx/models
	{Format: files.FormatAsset, Terms: []string{"model"}},
	{Folder: "gfx/models", Terms: []string{"model"}},
	// CK3's Graphical assets and Vic3's Graphical asset modding, which write
	// of .dds textures
	{Format: files.FormatImage, Terms: []string{"graphical asset"}},
	// the three games' Map modding, which write of gfx/map
	{Folder: "gfx/map", Terms: []string{"map modding"}},
}

// launcherFile is the launcher's settings file, from the install root.
const launcherFile = "launcher/launcher-settings.json"

// ErrorLog is the log in which the game writes its errors, each line starting
// with the time and the source file and line that wrote it.
var ErrorLog = Log{
	"logs/error.log",
	regexp.MustCompile(`^\[(\d\d):(\d\d):(\d\d)\](?:\[([A-Z])\])?\[([A-Za-z0-9_]+\.(?:cpp|h|hpp|inl)):(\d+)\]: ?(.*)$`),
}

// The forms in which an error log names a line of a script file. None matches
// a bare path and line: every log line's header holds its source as
// [file.cpp:N], so a path must follow a space.
var (
	// errorLogFileLine is CK3's "file: P line: N", after "Script location:",
	// "at", "location '" or "from database".
	errorLogFileLine = regexp.MustCompile(`file: *(?P<path>\S+) line: *(?P<line>\d+)`)

	// errorLogQuotedLine is the statement's line that a quoted message names,
	// as all three games write in "Unknown effect: x, near line: 3501" in
	// file: "P" near line: 3502, where the last line is where the reader
	// stopped.
	errorLogQuotedLine = regexp.MustCompile(`near line: (?P<line>\d+)" in file: "(?P<path>[^"]+)"`)

	// errorLogNearLine is the reader's "in file: "P" near line: N", written by
	// all three games.
	errorLogNearLine = regexp.MustCompile(`in file: "(?P<path>[^"]+)" near line: (?P<line>\d+)`)

	// errorLogPathLine is "P:N" after other text, as in
	// "Script location: P:N" and "]: P:N - Missing file".
	errorLogPathLine = regexp.MustCompile(`(?:^|\s)(?P<path>[^\s\[\]:'"]+\.\w+):(?P<line>\d+)\b`)

	// errorLogLineColumn is EU5's "line N and column C in P".
	errorLogLineColumn = regexp.MustCompile(`line (?P<line>\d+) and column \d+ in (?P<path>\S+)`)
)

// MountLog is the log line in which the game names each folder it reads.
var MountLog = Log{"logs/debug.log", regexp.MustCompile(`Mounted Data: (.+?)\s*$`)}

// steamLibrary is where CK3 and Vic3 keep their copy of Steam's library, by
// GOOS.
var steamLibrary = map[string]string{"windows": "binaries/steam_api64.dll", "linux": "binaries/libsteam_api.so"}

// games are the supported games, by id.
var games = map[string]*Game{
	CK3: {
		ID:            CK3,
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
		// the wiki's Localization page: localization/replace/english takes
		// precedence over localization/english/replace
		LocReplaceFolders: []string{"localization/replace/%s", "localization/%s/replace"},
		// the wiki's Modding page; ck3.exe "Duplicate localization key. Key
		// '%s' is defined in both '%s' and '%s'."
		LogsRepeatedKey: true,
		// effects.log: "execute_decision - Execute the specified decision for
		// the scoped character"
		DecisionEffects: []string{"execute_decision"},
		GUI: GameGUI{
			Undeclared: []string{"zoomarea", "text_occluder"},
			// common/defines/graphic/00_graphics.txt:812-813:
			// GOVERNMENT_TYPE_REALM_MASK_PATH with
			// GOVERNMENT_TYPE_REALM_FRAME_MASK_SUFFIX
			SuffixStems: map[string]string{"GOVERNMENT_TYPE_REALM_MASK": "GOVERNMENT_TYPE_REALM_FRAME_MASK"},
			// common/defines/graphic/00_graphics.txt:782 writes TRAIT_ICON_PATH
			// in NGameIcons, and :812 the realm mask's path
			DefineGroup: "NGameIcons",
			// seven tiers: cell 3 for a count, 4 for a duke, from 5 for a king
			// or an emperor
			Tiers: TierFrames{
				Function: "GetTierFrame", Tiers: []string{"unlanded", "baron", "count", "duke", "king", "emperor", "hegemon"},
			},
			// gfx/interface/illustrations/scripted_illustrations
			// /_scripted_illustrations.info
			Illustrations: Illustrations{FolderKey: "folder", Folder: "gfx/interface/illustrations"},
		},
		Coat: GameCoat{
			Folder: "common/coat_of_arms/coat_of_arms",
			// game/gfx/FX/coat_of_arms
			Shade: CoatShade{
				Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
				Overlay: CoatStrength{Channel: "b", Strength: 0.2},
				Grain:   CoatStrength{Channel: "g", Strength: 2},
			},
			// the frames of gui/shared/coa_designer.gui, without the unit's 3D
			// banner. Each define is named in full, as
			// common/defines/graphic/00_graphics.txt:309,319 write them in
			// NCoatOfArms.
			Frames: []CoatFrame{
				{Label: "Dynasty", Define: "NCoatOfArms|DEFAULT_DYNASTY_COA_FRAME_NAME"},
				{Label: "House", Define: "NCoatOfArms|DEFAULT_HOUSE_COA_FRAME_NAME"},
				{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
				{Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds"},
			},
			ColorForm: jomini.ColorForm{Tag: "rgb", Example: []string{"255", "255", "255"}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events keep the highest override priority, and on a tie the game logs an error and keeps one copy without saying which; a character takes its name, traits, skills, parents and birth from the copy with the highest override priority and combines the rest, and the game calls a tie an error without saying what it keeps; on_actions combine their lists.",
			Changes: "Only files at the same path.",
		},
		// common/traits/00_traits.txt
		Example: Example{Database: "traits", Folder: "common/traits", Names: []string{"gregarious", "shy", "brave"}},
	},
	Vic3: {
		ID:             Vic3,
		Name:           "Victoria 3",
		ShortName:      "Vic3",
		ModDescriptor:  MetadataDir + "/metadata.json",
		FixedModFolder: true,
		SteamAppID:     529340,
		SteamLibrary:   steamLibrary,
		ModFolders:     []string{"common", "events", "localization"},
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
		// victoria3.exe matches the path part /replace/; no source says which
		// form wins
		LocReplaceFolders: []string{"localization/%s/replace", "localization/replace/%s"},
		GUI: GameGUI{
			Undeclared:  []string{"overlappingitembox", "treemapchart", "portrait_button", "axis"},
			DropsHidden: []string{"flowcontainer"},
		},
		Coat: GameCoat{
			Folder:    "common/coat_of_arms/coat_of_arms",
			ColorForm: jomini.ColorForm{Tag: jomini.TagHSV360, Example: []string{"360", "100", "100"}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path.",
			Files:   "A mod's replace_path makes the game skip its own files in that folder. This is documented for CK3 and EU5, whose engine Vic3 shares; whether it also skips the folder's subfolders is not documented.",
			Kept:    "Most things keep the copy read last; events and GUI types keep the first; where a folder takes INJECT: and REPLACE:, the first plain copy stays and a later one is refused, so mods change it with those keywords, which act after every plain copy whatever their file's name, by mode (INJECT_OR_CREATE first, REPLACE last) and then by file name; character DNA copies all exist.",
			Changes: "Only files at the same path.",
		},
		// common/buildings/01_industry.txt
		Example: Example{
			Database: "buildings", Folder: "common/buildings",
			Names: []string{"building_food_industry", "building_textile_mill", "building_furniture_manufactory"},
		},
	},
	EU5: {
		ID:        EU5,
		Name:      "Europa Universalis V",
		ShortName: "EU5",
		// debug.log:5372-5394 mounts each root's stages in this order, and only
		// them; the wiki's Mod structure, l.46-57
		Stages:         []string{"loading_screen", "main_menu", "in_game"},
		ModDescriptor:  MetadataDir + "/metadata.json",
		FixedModFolder: true,
		// where EU5 mods keep it
		ThumbnailDir: MetadataDir,
		SteamAppID:   3450310,
		// no Linux build: Proton
		SteamLibrary: map[string]string{"windows": "binaries/steam_api64.dll"},
		ModFolders:   []string{"in_game/common", "in_game/events", "main_menu/localization"},
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
					// a scene's backdrop is the layer drawn on background2_mesh
					// (in_game/gfx/scenes/00_scenes_events.txt)
					{Key: "ecs_illustration_image[pdxmesh=background2_mesh]/list", Database: "gfx/images", Match: "tag"},
				},
				FileKey: "illustration_image/texture_file",
			}},
		}},
		TakesModes: true,
		// the wiki's Mod compatibility, l.209, and Localization, l.2, name only
		// this form
		LocReplaceFolders: []string{"localization/%s/replace"},
		// main_menu/common/bookmarks/00_bookmarks.txt:4
		BookmarkFolder: "common/bookmarks", SetupKey: "setup_folder",
		GUI: GameGUI{
			Holders:    map[string]string{"lateralview": "lateralview_left_layer"},
			Undeclared: []string{"axis", "plotline", "screen_grab", "newsfeed_carousel"},
			// loading_screen/common/defines/graphic/00_graphics.txt writes its
			// icon paths in NGameIcons, such as :851 UNIT_STATUS_ICON_PATH
			DefineGroup: "NGameIcons",
		},
		Coat: GameCoat{
			Folder:    "main_menu/common/coat_of_arms/coat_of_arms",
			ColorForm: jomini.ColorForm{Tag: jomini.TagHSV360, Example: []string{"360", "100", "100"}},
		},
		Guide: Guide{
			Hint:    "The load order decides only files that two mods ship at the same path, and GUI types and templates.",
			Files:   "A mod's files count only inside its in_game, main_menu or loading_screen folder. A mod's replace_path makes the game skip its own files in that folder, not in its subfolders.",
			Kept:    "Under common/ the first plain copy stays and mods change it with INJECT: and REPLACE:, which act after every plain copy whatever their file's name, by mode (INJECT_OR_CREATE first, REPLACE last) and then by file name; events keep the first; GUI types and templates follow the load order; coats of arms and defines keep the last.",
			Changes: "Only files at the same path and GUI types and templates.",
		},
		// in_game/common/traits/00_ruler.txt
		Example: Example{Database: "traits", Folder: "in_game/common/traits", Names: []string{"righteous", "cruel", "tolerant"}},
	},
}

// LookupGame returns the game with the given id, or nil if this build does not
// support it.
func LookupGame(id string) *Game {
	return games[id]
}

// ListGames returns the supported games in the order the app shows them, each
// example's Modes filled from its database's precedence row.
func ListGames() []Game {
	rows := []Game{*games[CK3], *games[EU5], *games[Vic3]}
	for position := range rows {
		row := &rows[position]
		_, folder := files.SplitStage(row.Example.Folder, row.Stages)
		row.Example.Modes = row.LookupPrecedence(row.Example.Database, folder).Modes != ""
	}

	return rows
}

// Matches reports whether the game is among the given games. An empty list
// names every game, even a nil one.
func (g *Game) Matches(games []string) bool {
	return len(games) == 0 || g != nil && slices.Contains(games, g.ID)
}

// FindLocReplacement returns the rank of the localization replace folder a
// file is in, the lowest winning. It returns false if the file is in none.
func (g *Game) FindLocReplacement(rel, language string) (int, bool) {
	_, rel = files.SplitStage(rel, g.Stages)
	rank := slices.IndexFunc(g.LocReplaceFolders, func(folder string) bool {
		return strings.HasPrefix(rel, fmt.Sprintf(folder, language)+"/")
	})

	return rank, rank >= 0
}

// CanAccuse reports whether a claim from the source is firm enough to fail a
// verdict. Only the dumps, the docs, the wiki and the definitions are.
func (s Source) CanAccuse() bool {
	return s == SourceDumps || s == SourceDocs || s == SourceWiki || s == SourceDefinitions
}
