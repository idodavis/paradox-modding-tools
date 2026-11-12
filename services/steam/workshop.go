// workshop.go lists what a listing can choose from: rules, DLCs and tags.

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

	"golang.org/x/net/html"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/agent"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// DLC is one of the game's DLCs that an item may require.
type DLC struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Rule is one pattern of files an upload leaves out, with its reason.
type Rule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// Tags is the set of Workshop tags a listing may carry.
type Tags struct {
	Categories []string `json:"categories"`
	Versions   []string `json:"versions"`
}

var releaseNumber = regexp.MustCompile(`^\d+\.\d+`)

// ListRules returns the rules for the files every upload leaves out.
func (s *Service) ListRules() []Rule {
	instructions := []string{}
	for _, file := range agent.ListFiles() {
		if !strings.HasPrefix(file, ".") {
			instructions = append(instructions, file)
		}
	}

	return []Rule{
		{Pattern: ".*", Reason: "Files that start with a dot belong to the author's tools, not to the game."},
		{Pattern: gitignoreFile, Reason: "What the mod's own .gitignore names stays out too."},
		{Pattern: strings.Join(instructions, "\n"),
			Reason: "Instructions for coding agents belong to the author's tools, not to the game."},
	}
}

// ListDLC returns the DLCs of the open workspace's install that have a Steam
// id. It returns an error if a descriptor cannot be read.
func (s *Service) ListDLC() ([]DLC, error) {
	ws, err := s.Workspace.Session()
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

// A DLC folder with both descriptors is read from the script.
const (
	dlcScript = ".dlc"
	dlcJSON   = ".dlc.json"
)

// loadDLC returns the DLC a folder holds. Its id is "" if it has no Steam id.
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
			tree := jomini.Parse(text.Text)
			steamID, _ = tree.FindChildValue(0, "steam_id")
			name, _ = tree.FindChildValue(0, "name")
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

// decodeDLCJSON returns the Steam id and the name in a DLC's JSON descriptor,
// with the name localized where the mods' index has its key.
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

// ListTags returns the Workshop tags a listing of the game may carry. It falls
// back to the tags kept in the cache when the fetch fails.
func (s *Service) ListTags() (Tags, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Tags{}, err
	}

	cacheDir := s.Workspace.Config.Dirs.Cache
	declared, err := fetchTags(ws.Game.SteamAppID)
	if err == nil {
		err = app.SaveJSON(resolveTagsPath(cacheDir, ws.Game.SteamAppID), declared)
	} else if saved := loadTags(cacheDir, ws.Game.SteamAppID); hasTags(saved) {
		declared, err = saved, nil
	}
	if err != nil {
		return Tags{}, err
	}

	categories := toCategoryTags(declared.Versions, declared.Categories)
	slices.Sort(categories)

	return Tags{Categories: categories, Versions: declared.Versions}, nil
}

// listVersionTags returns Steam's version tags for a game. With none kept, it
// fetches them at most once per run.
func (s *Service) listVersionTags(appID int) []string {
	cacheDir := s.Workspace.Config.Dirs.Cache
	if saved := loadTags(cacheDir, appID); hasTags(saved) {
		return saved.Versions
	}

	shared, _, _ := s.flight.Do("tags "+strconv.Itoa(appID), func() (any, error) {
		s.mu.Lock()
		tried := s.tagFetches[appID]
		if s.tagFetches == nil {
			s.tagFetches = map[int]bool{}
		}
		s.tagFetches[appID] = true
		s.mu.Unlock()

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

const (
	browsePage = "https://steamcommunity.com/workshop/browse/?appid=%d&l=english"
	pageScript = "valve-ssr-data"
)

// fetchTags fetches the tags a game's Workshop page declares, without those
// only Steam's admins may set. It returns an error if the page declares none.
func fetchTags(appID int) (Tags, error) {
	body, err := app.FetchURL(fmt.Sprintf(browsePage, appID), 30*time.Second)
	if err != nil {
		return Tags{}, err
	}

	page, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return Tags{}, err
	}

	script := ""
	for node := range page.Descendants() {
		if node.Type == html.ElementNode && node.FirstChild != nil &&
			slices.Contains(node.Attr, html.Attribute{Key: "id", Val: pageScript}) {
			script = node.FirstChild.Data
			break
		}
	}

	tags := Tags{Categories: []string{}, Versions: []string{}}
	for _, names := range decodeTagGroups(script) {
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

// decodeTagGroups returns the groups of tags a Workshop page's script declares.
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

func hasTags(tags Tags) bool {
	return len(tags.Categories)+len(tags.Versions) > 0
}

// toCategoryTags returns the tags that name no game version: not one of Steam's
// version tags, and not one that starts with a release number.
func toCategoryTags(versions, tags []string) []string {
	categories := []string{}
	for _, tag := range tags {
		if !slices.Contains(versions, tag) && !releaseNumber.MatchString(tag) {
			categories = append(categories, tag)
		}
	}

	return categories
}

func loadTags(cacheDir string, appID int) Tags {
	tags, _ := app.LoadJSON[Tags](resolveTagsPath(cacheDir, appID))

	return tags
}

func resolveTagsPath(cacheDir string, appID int) string {
	return filepath.Join(cacheDir, "tags-"+strconv.Itoa(appID)+".json")
}

// resolveVersionTag returns the version tag for the release of a supported
// version, such as 1.20 'Crozier' for 1.20.*, or "" if there is none.
func resolveVersionTag(versions []string, supported string) string {
	release := releaseNumber.FindString(supported)
	if release == "" {
		return ""
	}

	for _, tag := range versions {
		if releaseNumber.FindString(tag) == release {
			return tag
		}
	}

	return ""
}
