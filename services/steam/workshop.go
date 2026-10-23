package steam

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/workspace"
)

// A DLC is one DLC of the game that an item may require, with the id Steam knows it by.
type DLC struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// A Rule is one pattern of files an upload leaves out, with the reason shown for it.
type Rule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// The endings of a DLC's descriptor: a script file in CK3 and Vic3, a JSON file in EU5. A folder with both is read
// from the script.
const (
	dlcScript = ".dlc"
	dlcJSON   = ".dlc.json"
)

// Rules lists what an upload leaves out.
var Rules = []Rule{
	{Pattern: ".*", Reason: "Files that start with a dot belong to the author's tools, not to the game."},
	{Pattern: gitignoreFile, Reason: "What the mod's own .gitignore names stays out too."},
	{Pattern: previewsDir + "/", Reason: "Steam takes the previews as the page's gallery."},
	{Pattern: "", Reason: "What the mod's own exclusions name stays out too."}, // the patterns are the mod's record's
}

// ListDLC returns the DLCs the open workspace's install ships that Steam knows by an id, read from the descriptor in
// each DLC folder.
func (stm *Service) ListDLC() ([]DLC, error) {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods, dlcs := ws.Mods.Load(), []DLC{}
	for _, root := range ws.Install.Roots {
		if filepath.Base(filepath.Dir(root.Dir)) != game.DLCFolder {
			continue
		}
		dlc, err := loadDLC(ws, mods, root.Dir)
		if err != nil {
			return nil, err
		}
		if dlc.ID != "" {
			dlcs = append(dlcs, dlc)
		}
	}
	return dlcs, nil
}

// loadDLC reads the DLC that a folder holds from its descriptor. The id is the descriptor's Steam id; it is "" when
// the folder has no descriptor or the id is missing or 0. The name is the script's, or for a JSON descriptor the
// text of its localization key in the session's language, found in the given index of the mods, else its own name.
func loadDLC(ws *workspace.Session, mods *index.Index, dir string) (DLC, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return DLC{}, err
	}
	for _, ending := range []string{dlcScript, dlcJSON} {
		position := slices.IndexFunc(entries, func(entry os.DirEntry) bool { return strings.HasSuffix(entry.Name(), ending) })
		if position < 0 {
			continue
		}
		text, err := files.ReadText(filepath.Join(dir, entries[position].Name()))
		if err != nil {
			return DLC{}, err
		}
		var steamID, name string
		if ending == dlcScript {
			steamID, name = decodeDLCScript(text.Text)
		} else if steamID, name, err = decodeDLCJSON(ws, mods, text.Text); err != nil {
			return DLC{}, err
		}
		id, err := strconv.ParseUint(steamID, 10, 64)
		if err != nil || id == 0 {
			return DLC{}, nil
		}
		return DLC{ID: strconv.FormatUint(id, 10), Name: cmp.Or(name, filepath.Base(dir))}, nil
	}
	return DLC{}, nil
}

// decodeDLCScript returns the Steam id and the name in the text of a DLC's script descriptor. Each is "" when the
// script has none.
func decodeDLCScript(text string) (steamID, name string) {
	tree := jomini.Parse(text)
	value := func(key string) string {
		if statement, ok := tree.FindChild(0, key); ok {
			return tree.Unquoted(tree.Statements[statement].Value)
		}
		return ""
	}
	return value("steam_id"), value("name")
}

// decodeDLCJSON returns the Steam id and the name in the text of a DLC's JSON descriptor. The name is the text of
// its localization key in the session's language, found in the given index of the mods, else its own name.
func decodeDLCJSON(ws *workspace.Session, mods *index.Index, text string) (steamID, name string, err error) {
	var descriptor struct {
		Name            string      `json:"name"`
		LocalizableName string      `json:"localizable_name"`
		SteamID         json.Number `json:"steam_id"`
	}
	if err := json.Unmarshal([]byte(text), &descriptor); err != nil {
		return "", "", err
	}
	name = descriptor.Name
	if localized, _, ok := ws.LookupText(mods, descriptor.LocalizableName); ok {
		name = localized
	}
	return descriptor.SteamID.String(), name, nil
}

// ListTags returns the Workshop tags the game's items may carry.
func (stm *Service) ListTags() []string {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return []string{}
	}
	tags := lo.Uniq(lo.FlatMap(ws.Order, func(mod workspace.Mod, _ int) []string { return mod.Descriptor.Tags }))
	slices.Sort(tags)
	return tags
}
