// refresh.go fetches a game's wiki pages again and keeps them in one sidecar file per game under the client's cache
// folder, reusing every page whose revision has not changed, and holds the title filter that decides which pages are
// worth fetching at all.

package wiki

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
)

// pageSet is one kind of page a refresh fetches: the articles its wiki category lists after the title filter, the
// pages the sidecar kept of that kind, and whether the pages are patch notes, whose Modding mark counts.
type pageSet struct {
	wanted  []mwMember
	saved   []Page
	patches bool
}

var (
	listOfRe  = regexp.MustCompile(`(?i)^List of\b`)
	listEndRe = regexp.MustCompile(`(?i)\blist$`)
	userRe    = regexp.MustCompile(`(?i)^User:`)
)

// denyExact is the titles no pattern above denies.
var denyExact = map[string]bool{"pdx deepl": true, "modding tools": true, "exporters": true}

// Refresh fetches gameID's guides and patch notes again and saves them, telling progress each page
// it fetches: the stage (guides, patch notes), how many are done and how many there are to fetch.
func (client *Client) Refresh(ctx context.Context, gameID string, progress func(stage string, done, total int)) error {
	client.refreshing.Lock()
	defer client.refreshing.Unlock()
	api := client.api(gameID)
	if api == "" {
		return fmt.Errorf("no wiki api for %s", gameID)
	}

	mods, err := client.categoryMembers(ctx, api, "Category:Mods")
	if err != nil {
		return fmt.Errorf("category mods: %w", err)
	}
	guideMembers, err := client.categoryMembers(ctx, api, "Category:Modding")
	if err != nil {
		return fmt.Errorf("category modding: %w", err)
	}
	patchMembers, err := client.categoryMembers(ctx, api, "Category:Patches")
	if err != nil {
		return fmt.Errorf("category patches: %w", err)
	}

	prev, err := client.load(gameID)
	if err != nil {
		prev = &Sidecar{}
	}
	guides, err := client.fetchPages(ctx, api,
		pageSet{wanted: filterMembers(guideMembers, modsSet(mods)), saved: prev.Guides},
		func(done, total int) { progress("guides", done, total) })
	if err != nil {
		return err
	}
	if len(guides) == 0 && len(prev.Guides) == 0 {
		return fmt.Errorf("wiki guides empty")
	}
	patches, err := client.fetchPages(ctx, api,
		pageSet{wanted: filterMembers(patchMembers, nil), saved: prev.Patches, patches: true},
		func(done, total int) { progress("patch notes", done, total) })
	if err != nil {
		return err
	}
	return client.save(gameID, &Sidecar{Guides: guides, Patches: patches})
}

// fetchPages parses each wanted page of set whose revision differs from the one kept before, telling progress how
// many of them are done; a page that fails to fetch or clean keeps its previous copy.
func (client *Client) fetchPages(ctx context.Context, api string, set pageSet, progress func(done, total int)) ([]Page, error) {
	// the folder api.php is in, which page links and page URLs are read against
	wikiBase := strings.TrimRight(strings.TrimSuffix(api, "api.php"), "/") + "/"
	previous := make(map[string]Page, len(set.saved))
	for _, page := range set.saved {
		previous[NormTitle(page.Title)] = page
	}
	unchanged := func(member mwMember) bool {
		prior := previous[NormTitle(member.Title)]
		return prior.Revid != 0 && prior.Revid == member.LastRevid && prior.HTML != ""
	}
	total, done := 0, 0
	for _, member := range set.wanted {
		if !unchanged(member) {
			total++
		}
	}
	out := make([]Page, 0, len(set.wanted))
	for _, member := range set.wanted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prior, known := previous[NormTitle(member.Title)]
		if unchanged(member) {
			out = append(out, prior)
			continue
		}
		progress(done, total)
		done++
		raw, revid, err := client.parsePage(ctx, api, member.Title)
		var page Page
		if err == nil {
			page.HTML, page.Sections, page.Modding = clean(raw, wikiBase)
		}
		if page.HTML == "" || len(page.HTML) > htmlCap {
			if known {
				out = append(out, prior)
			}
			continue
		}
		page.Title, page.URL, page.Revid = member.Title, wikiBase+strings.ReplaceAll(member.Title, " ", "_"), cmp.Or(revid, member.LastRevid)
		page.Modding = page.Modding && set.patches
		out = append(out, page)
	}
	return out, nil
}

// filterMembers keeps the first article of each title that is not denied and not one of modTitles' mods.
func filterMembers(members []mwMember, modTitles map[string]bool) []mwMember {
	var out []mwMember
	seen := map[string]bool{}
	for _, member := range members {
		if !keepMember(member.Title, modTitles) {
			continue
		}
		key := NormTitle(member.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, member)
	}
	return out
}

// modsSet is the normalized titles of the articles the Mods category lists.
func modsSet(members []mwMember) map[string]bool {
	out := map[string]bool{}
	for _, member := range members {
		out[NormTitle(member.Title)] = true
	}
	return out
}

// denied reports whether an article title is skipped (tools, dumps, user pages).
func denied(title string) bool {
	return denyExact[NormTitle(title)] || userRe.MatchString(title) || listOfRe.MatchString(title) ||
		listEndRe.MatchString(strings.TrimSpace(title))
}

// keepMember reports whether an article is worth fetching: its title is not denied and is neither a mod's page nor
// one of its subpages, which modTitles names.
func keepMember(title string, modTitles map[string]bool) bool {
	page, _, _ := strings.Cut(title, "/") // a mod's subpage (Way of Kings/Religions) goes with it
	return !denied(title) && !modTitles[NormTitle(page)]
}

// SidecarPath is gameID's wiki sidecar file in the cache folder.
func (client *Client) SidecarPath(gameID string) string {
	return filepath.Join(client.cacheDir, "wiki-"+gameID+".json")
}

// ForgetAll drops every sidecar held in memory; Reset data calls it after deleting the cache.
func (client *Client) ForgetAll() {
	client.memMu.Lock()
	defer client.memMu.Unlock()
	client.mem = map[string]*Sidecar{}
}

// load reads gameID's sidecar, from memory once read; a missing or unreadable file is an error.
func (client *Client) load(gameID string) (*Sidecar, error) {
	client.memMu.Lock()
	cached := client.mem[gameID]
	client.memMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	raw, err := os.ReadFile(client.SidecarPath(gameID))
	if err != nil {
		return nil, err
	}
	var sidecar Sidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return nil, err
	}
	client.memMu.Lock()
	client.mem[gameID] = &sidecar
	client.memMu.Unlock()
	return &sidecar, nil
}

// save writes gameID's sidecar whole and keeps it in memory.
func (client *Client) save(gameID string, sidecar *Sidecar) error {
	err := appdata.Write(client.SidecarPath(gameID), func(out io.Writer) error {
		return json.NewEncoder(out).Encode(sidecar)
	})
	if err != nil {
		return err
	}
	client.memMu.Lock()
	client.mem[gameID] = sidecar
	client.memMu.Unlock()
	return nil
}
