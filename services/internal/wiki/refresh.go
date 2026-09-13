package wiki

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
)

var gameLocks sync.Map // gameID -> *sync.Mutex

func lockGame(id string) func() {
	v, _ := gameLocks.LoadOrStore(id, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

func existingByTitle(pages []Page) map[string]Page {
	out := make(map[string]Page, len(pages))
	for _, p := range pages {
		out[normTitle(p.Title)] = p
	}
	return out
}

func filterMembers(members []mwMember, mods map[string]bool) []mwMember {
	var out []mwMember
	seen := map[string]bool{}
	for _, m := range members {
		if !keepMember(m.Title, m.Ns, mods) {
			continue
		}
		k := normTitle(m.Title)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, m)
	}
	return out
}

func modsSet(members []mwMember) map[string]bool {
	out := map[string]bool{}
	for _, m := range members {
		if m.Ns == 0 {
			out[normTitle(m.Title)] = true
		}
	}
	return out
}

func fetchPages(
	ctx context.Context, gameID, api, wikiBase, kind string,
	want []mwMember, prev []Page, progress Progress,
) ([]Page, error) {
	old := existingByTitle(prev)
	titles := make([]string, len(want))
	for i, m := range want {
		titles[i] = m.Title
	}
	info, err := pageInfo(ctx, api, titles)
	if err != nil {
		return nil, err
	}
	out := make([]Page, 0, len(want))
	done := 0
	total := len(want)
	if progress != nil {
		progress(0, total, kind)
	}
	for _, m := range want {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// The progress epilogue runs once per item however the body exits.
		func() {
			defer func() {
				done++
				if progress != nil {
					progress(done, total, kind)
				}
			}()
			inf := info[normTitle(m.Title)]
			revid := inf.LastRevid
			if p, ok := old[normTitle(m.Title)]; ok && p.Revid != 0 && p.Revid == revid && p.HTML != "" {
				out = append(out, p)
				return
			}
			raw, parsedRevid, sections, err := parsePage(ctx, api, m.Title)
			keepOld := func() {
				if p, ok := old[normTitle(m.Title)]; ok {
					out = append(out, p)
				}
			}
			if err != nil {
				keepOld()
				return
			}
			if parsedRevid != 0 {
				revid = parsedRevid
			}
			clean, snippets := sanitize(raw, wikiBase)
			if clean == "" || len(clean) > htmlCap {
				keepOld()
				return
			}
			pageID := m.PageID
			if inf.PageID != 0 {
				pageID = inf.PageID
			}
			p := Page{
				PageID:    pageID,
				Title:     m.Title,
				URL:       pageURL(gameID, m.Title),
				Revid:     revid,
				FetchedAt: nowUTC(),
				License:   License,
				Sections:  sections,
				HTML:      clean,
				Snippets:  snippets,
			}
			if kind == "patches" {
				p.ModdingHTML = extractModding(clean)
			}
			out = append(out, p)
		}()
	}
	return out, nil
}

func applyHeader(h *header, gameID, version string, stampMajorMinor bool) {
	h.FormatVersion = FormatVersion
	h.GameID = gameID
	h.FetchedAt = nowUTC()
	if stampMajorMinor {
		h.FetchedVersion = version
		if maj, min, ok := ParseMajorMinor(version); ok {
			h.FetchedMajor = maj
			h.FetchedMinor = min
		}
	}
}

func refresh(ctx context.Context, gameID, version string, stamp bool, progress Progress) error {
	unlock := lockGame(gameID)
	defer unlock()
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
	modSet := modsSet(mods)

	guideMem, err := categoryMembers(ctx, api, "Category:Modding")
	if err != nil {
		return fmt.Errorf("category modding: %w", err)
	}
	patchMem, err := categoryMembers(ctx, api, "Category:Patches")
	if err != nil {
		return fmt.Errorf("category patches: %w", err)
	}

	var prevDocs []Page
	if old, e := LoadGuides(gameID); e == nil && old != nil {
		prevDocs = old.Pages
	}
	var prevPatches []Page
	if old, e := LoadPatches(gameID); e == nil && old != nil {
		prevPatches = old.Pages
		if !stamp {
			version = old.FetchedVersion
		}
	}

	guides, err := fetchPages(ctx, gameID, api, wikiBase, "guides",
		filterMembers(guideMem, modSet), prevDocs, progress)
	if err != nil {
		return err
	}
	if len(guides) == 0 && len(prevDocs) == 0 {
		return fmt.Errorf("wiki guides empty")
	}
	df := &Sidecar{Pages: guides}
	if old, e := LoadGuides(gameID); e == nil && old != nil && !stamp {
		df.header = old.header
	}
	applyHeader(&df.header, gameID, version, stamp || df.FetchedMajor == 0)
	if err := saveGuides(df); err != nil {
		return err
	}

	patches, err := fetchPages(ctx, gameID, api, wikiBase, "patches",
		filterMembers(patchMem, nil), prevPatches, progress)
	if err != nil {
		return err
	}
	pf := &Sidecar{Pages: patches}
	if old, e := LoadPatches(gameID); e == nil && old != nil && !stamp {
		pf.header = old.header
	}
	applyHeader(&pf.header, gameID, version, stamp || pf.FetchedMajor == 0)
	return savePatches(pf)
}

// RefreshIfNeeded fetches when the sidecar is missing or major/minor-stale.
func RefreshIfNeeded(ctx context.Context, gameID, version string, progress Progress) error {
	if !Needed(gameID, version) {
		return nil
	}
	return refresh(ctx, gameID, version, true, progress)
}

// RefreshExisting incrementally updates every game that already has a sidecar.
func RefreshExisting(ctx context.Context, onDone func(gameID string, err error)) {
	for _, g := range game.All() {
		if ctx.Err() != nil {
			return
		}
		if !HasSidecar(g.ID) {
			continue
		}
		err := refresh(ctx, g.ID, "", false, nil)
		if onDone != nil {
			onDone(g.ID, err)
		}
	}
}
