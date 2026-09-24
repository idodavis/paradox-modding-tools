// refresh.go fetches a game's wiki pages again and keeps them in one sidecar file per game under
// local.CacheDir, reusing every page whose revision has not changed.
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
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/local"
)

var (
	cacheDirFn = local.CacheDir
	refreshMu  sync.Mutex // one refresh at a time
	memMu      sync.Mutex
	mem        = map[string]*Sidecar{}
)

func filterMembers(members []mwMember, mods map[string]bool) []mwMember {
	var out []mwMember
	seen := map[string]bool{}
	for _, member := range members {
		if !keepMember(member.Title, mods) {
			continue
		}
		key := normTitle(member.Title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, member)
	}
	return out
}

func modsSet(members []mwMember) map[string]bool {
	out := map[string]bool{}
	for _, member := range members {
		out[normTitle(member.Title)] = true
	}
	return out
}

// fetchPages parses each wanted page whose revision differs from the one kept before; a page
// that fails to fetch or clean keeps its previous copy.
func fetchPages(ctx context.Context, gameID, api, wikiBase string, patches bool, wanted []mwMember, prev []Page) ([]Page, error) {
	old := make(map[string]Page, len(prev))
	for _, page := range prev {
		old[normTitle(page.Title)] = page
	}
	out := make([]Page, 0, len(wanted))
	for _, want := range wanted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prior, known := old[normTitle(want.Title)]
		if known && prior.Revid != 0 && prior.Revid == want.LastRevid && prior.HTML != "" {
			out = append(out, prior)
			continue
		}
		raw, revid, err := parsePage(ctx, api, want.Title)
		var page Page
		if err == nil {
			page.HTML, page.Sections, page.ModdingHTML, page.ModdingSections = clean(raw, wikiBase)
		}
		if page.HTML == "" || len(page.HTML) > htmlCap {
			if known {
				out = append(out, prior)
			}
			continue
		}
		page.Title, page.URL, page.Revid = want.Title, pageURL(gameID, want.Title), cmp.Or(revid, want.LastRevid)
		if !patches {
			page.ModdingHTML, page.ModdingSections = "", nil
		}
		out = append(out, page)
	}
	return out, nil
}

// Refresh fetches gameID's guides and patch notes again and saves them.
func Refresh(ctx context.Context, gameID string) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	api := wikiAPI(gameID)
	if api == "" {
		return fmt.Errorf("no wiki api for %s", gameID)
	}
	wikiBase := wikiBaseURL(gameID)
	if apiOverride != "" {
		wikiBase = strings.TrimRight(apiOverride, "/") + "/"
	}

	mods, err := categoryMembers(ctx, api, "Category:Mods")
	if err != nil {
		return fmt.Errorf("category mods: %w", err)
	}
	guideMembers, err := categoryMembers(ctx, api, "Category:Modding")
	if err != nil {
		return fmt.Errorf("category modding: %w", err)
	}
	patchMembers, err := categoryMembers(ctx, api, "Category:Patches")
	if err != nil {
		return fmt.Errorf("category patches: %w", err)
	}

	prev, err := load(gameID)
	if err != nil {
		prev = &Sidecar{}
	}
	guides, err := fetchPages(ctx, gameID, api, wikiBase, false, filterMembers(guideMembers, modsSet(mods)), prev.Guides)
	if err != nil {
		return err
	}
	if len(guides) == 0 && len(prev.Guides) == 0 {
		return fmt.Errorf("wiki guides empty")
	}
	patches, err := fetchPages(ctx, gameID, api, wikiBase, true, filterMembers(patchMembers, nil), prev.Patches)
	if err != nil {
		return err
	}
	return save(gameID, &Sidecar{Guides: guides, Patches: patches})
}

func sidecarPath(gameID string) (string, error) {
	dir, err := cacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wiki-"+gameID+".json"), nil
}

// ForgetAll drops every sidecar held in memory; Reset data calls it after deleting the cache.
func ForgetAll() {
	memMu.Lock()
	defer memMu.Unlock()
	mem = map[string]*Sidecar{}
}

// load reads gameID's sidecar, from memory once read; a missing or unreadable file is an error.
func load(gameID string) (*Sidecar, error) {
	memMu.Lock()
	cached := mem[gameID]
	memMu.Unlock()
	if cached != nil {
		return cached, nil
	}
	path, err := sidecarPath(gameID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sidecar Sidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		return nil, err
	}
	memMu.Lock()
	mem[gameID] = &sidecar
	memMu.Unlock()
	return &sidecar, nil
}

func save(gameID string, sidecar *Sidecar) error {
	path, err := sidecarPath(gameID)
	if err != nil {
		return err
	}
	if err := local.Write(path, func(out io.Writer) error { return json.NewEncoder(out).Encode(sidecar) }); err != nil {
		return err
	}
	memMu.Lock()
	mem[gameID] = sidecar
	memMu.Unlock()
	return nil
}

var patchTitleRe = regexp.MustCompile(`(?i)^Patch\s+(\d+\.\d+(?:\.(?:\d+|[Xx]))?)`)

// patchVer is a parsed "Patch 1.16.2" / "Patch 1.16.X" title.
type patchVer struct {
	Major, Minor, Patch int
	Wildcard            bool
	Label               string
}

func parsePatchTitle(title string) (patchVer, bool) {
	match := patchTitleRe.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return patchVer{}, false
	}
	label := match[1]
	parts := strings.Split(label, ".")
	if len(parts) < 2 {
		return patchVer{}, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return patchVer{}, false
	}
	pv := patchVer{Major: major, Minor: minor, Label: label}
	if len(parts) >= 3 {
		if strings.EqualFold(parts[2], "x") {
			pv.Wildcard = true
			pv.Patch = 0
		} else if n, err := strconv.Atoi(parts[2]); err == nil {
			pv.Patch = n
		}
	}
	return pv, true
}

func cmpPatch(first, second patchVer) int {
	if first.Major != second.Major {
		return first.Major - second.Major
	}
	if first.Minor != second.Minor {
		return first.Minor - second.Minor
	}
	return first.Patch - second.Patch
}
