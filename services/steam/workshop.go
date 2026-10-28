package steam

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"golang.org/x/net/html"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/agent"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A DLC is one of the game's DLCs that an item may require.
type DLC struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// A Rule is one pattern of files an upload leaves out, with the reason shown for it.
type Rule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// Tags is the set of Workshop tags a listing may carry.
type Tags struct {
	Categories []string `json:"categories"` // Steam's category tags, with those the workspace's mods carry
	Versions   []string `json:"versions"`   // Steam's version tags
}

// The endings of a DLC's descriptor, a script or a JSON file. A folder with both is read from the script.
const (
	dlcScript = ".dlc"
	dlcJSON   = ".dlc.json"
)

// A game's Workshop browse page, and the id of the script in it that holds the page's data.
const (
	browsePage = "https://steamcommunity.com/workshop/browse/?appid=%d&l=english"
	pageScript = "valve-ssr-data"
)

// Rules lists what an upload leaves out.
var Rules = []Rule{
	{Pattern: ".*", Reason: "Files that start with a dot belong to the author's tools, not to the game."},
	{Pattern: gitignoreFile, Reason: "What the mod's own .gitignore names stays out too."},
	{Pattern: "", Reason: "What the mod's own exclusions name stays out too."}, // the patterns in the mod's record
}

// listRules returns what an upload leaves out: the rules, then the instruction files of the coding agents PMT offers
// that the first rule does not cover, as the agents' rows name them.
func listRules() []Rule {
	instructions := lo.Reject(agent.ListFiles(), func(file string, _ int) bool { return strings.HasPrefix(file, ".") })
	return append(slices.Clone(Rules), Rule{Pattern: strings.Join(instructions, "\n"),
		Reason: "Instructions for coding agents belong to the author's tools, not to the game."})
}

// releaseNumber matches the major and minor numbers that a version or a version tag starts with.
var releaseNumber = regexp.MustCompile(`^\d+\.\d+`)

// ListDLC returns the DLCs of the open workspace's install that have a Steam id. It returns an error if no workspace is
// open or a descriptor cannot be read.
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

// loadDLC returns the DLC that a folder holds, read from its descriptor. Its id is "" if the descriptor is missing or
// has no Steam id. It returns an error if a file cannot be read.
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

// decodeDLCScript returns the Steam id and the name in a DLC's script descriptor. Each is "" if the script has none.
func decodeDLCScript(text string) (steamID, name string) {
	tree := jomini.Parse(text)
	steamID, _ = tree.FindChildValue(0, "steam_id")
	name, _ = tree.FindChildValue(0, "name")
	return steamID, name
}

// decodeDLCJSON returns the Steam id and the name in a DLC's JSON descriptor. The name is localized where the mods'
// index has its key. It returns an error if the text is not valid JSON.
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

// ListTags returns the Workshop tags a listing of the game may carry. It returns an error if no workspace is open, or
// the fetch fails with no tags kept.
func (stm *Service) ListTags() (Tags, error) {
	ws, err := stm.Workspace.Session()
	if err != nil {
		return Tags{}, err
	}
	cacheDir := stm.Workspace.Config.Dirs.Cache
	declared, err := fetchTags(ws.Game.SteamAppID)
	if err == nil {
		err = app.SaveJSON(resolveTagsPath(cacheDir, ws.Game.SteamAppID), declared)
	} else if kept := loadTags(cacheDir, ws.Game.SteamAppID); hasTags(kept) {
		declared, err = kept, nil
	}
	if err != nil {
		return Tags{}, err
	}
	modTags := lo.FlatMap(ws.Order, func(mod workspace.Mod, _ int) []string { return mod.Descriptor.Tags })
	categories := toCategoryTags(declared.Versions, lo.Uniq(slices.Concat(declared.Categories, modTags)))
	slices.Sort(categories)
	return Tags{Categories: categories, Versions: declared.Versions}, nil
}

// listVersionTags returns Steam's version tags for a game. With none kept, it fetches them at most once per run. A
// failed fetch leaves none until ListTags fetches again.
func (stm *Service) listVersionTags(appID int) []string {
	cacheDir := stm.Workspace.Config.Dirs.Cache
	if kept := loadTags(cacheDir, appID); hasTags(kept) {
		return kept.Versions
	}
	shared, _, _ := stm.flight.Do("tags "+strconv.Itoa(appID), func() (any, error) {
		stm.mu.Lock()
		tried := stm.tagFetches[appID]
		if stm.tagFetches == nil {
			stm.tagFetches = map[int]bool{}
		}
		stm.tagFetches[appID] = true
		stm.mu.Unlock()
		if tried {
			return loadTags(cacheDir, appID), nil
		}
		declared, err := fetchTags(appID)
		if err != nil {
			return Tags{}, nil
		}
		return declared, app.SaveJSON(resolveTagsPath(cacheDir, appID), declared)
	})
	return shared.(Tags).Versions
}

// hasTags reports whether there is any category or version tag.
func hasTags(tags Tags) bool {
	return len(tags.Categories)+len(tags.Versions) > 0
}

// toCategoryTags returns tags without those that name a game version.
func toCategoryTags(versions, tags []string) []string {
	return lo.Reject(tags, func(tag string, _ int) bool { return isVersionTag(versions, tag) })
}

// fetchTags fetches the tags that a game's Workshop page declares for its items, without those only Steam's admins may
// set. It returns an error if the page cannot be fetched or declares no tags.
func fetchTags(appID int) (Tags, error) {
	body, _, err := fetchURL(fmt.Sprintf(browsePage, appID), 30*time.Second)
	if err != nil {
		return Tags{}, err
	}
	page, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return Tags{}, err
	}
	tags := Tags{Categories: []string{}, Versions: []string{}}
	for _, names := range decodeTagGroups(findPageScript(page)) {
		if slices.ContainsFunc(names, releaseNumber.MatchString) {
			tags.Versions = append(tags.Versions, names...)
		} else {
			tags.Categories = append(tags.Categories, names...)
		}
	}
	if !hasTags(tags) {
		return Tags{}, errors.New("steam's Workshop page declares no tags; its layout may have changed")
	}
	return tags, nil
}

// findPageScript returns the text of the script that holds a Workshop page's data, or "" if the page has none.
func findPageScript(page *html.Node) string {
	for node := range page.Descendants() {
		if node.Type == html.ElementNode && node.FirstChild != nil &&
			slices.Contains(node.Attr, html.Attribute{Key: "id", Val: pageScript}) {
			return node.FirstChild.Data
		}
	}
	return ""
}

// decodeTagGroups returns the groups of tags that a Workshop page's script declares, without those only Steam's admins
// may set. It returns none if the script declares no tags.
func decodeTagGroups(script string) [][]string {
	var page struct {
		LoaderData []string `json:"loaderData"`
	}
	_ = json.Unmarshal([]byte(script), &page)
	for _, text := range page.LoaderData {
		var entry struct {
			DeclaredTags struct {
				ReadyToUse []struct {
					Tags []struct {
						Name      string `json:"name"`
						AdminOnly bool   `json:"admin_only"`
					} `json:"tags"`
				} `json:"readytouse_tags"`
			} `json:"declaredTags"`
		}
		if json.Unmarshal([]byte(text), &entry) != nil || entry.DeclaredTags.ReadyToUse == nil {
			continue
		}
		groups := make([][]string, len(entry.DeclaredTags.ReadyToUse))
		for position, group := range entry.DeclaredTags.ReadyToUse {
			for _, tag := range group.Tags {
				if !tag.AdminOnly {
					groups[position] = append(groups[position], tag.Name)
				}
			}
		}
		return groups
	}
	return nil
}

// loadTags loads the tags kept for a game's Workshop. It returns empty tags if none are kept.
func loadTags(cacheDir string, appID int) Tags {
	tags, _ := app.LoadJSON[Tags](resolveTagsPath(cacheDir, appID))
	return tags
}

// resolveTagsPath returns the path of the file that keeps a game's Workshop tags.
func resolveTagsPath(cacheDir string, appID int) string {
	return filepath.Join(cacheDir, "tags-"+strconv.Itoa(appID)+".json")
}

// isVersionTag reports whether a tag is one of Steam's version tags or starts with a release number.
func isVersionTag(versions []string, tag string) bool {
	return slices.Contains(versions, tag) || releaseNumber.MatchString(tag)
}

// resolveVersionTag returns the version tag for the release of a supported version, such as 1.20 'Crozier' for 1.20.*.
// It returns "" if there is none.
func resolveVersionTag(versions []string, supported string) string {
	release := releaseNumber.FindString(supported)
	tag, _ := lo.Find(versions, func(tag string) bool {
		return release != "" && releaseNumber.FindString(tag) == release
	})
	return tag
}
