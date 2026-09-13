// loccoverage.go reports missing, orphan, and untranslated loc keys per
// language from the loc maps plus vanilla sidecar inherit. DefaultLocLang is
// not used here (IDE/LSP only). Untranslated compares to english.

package views

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

// locSourceLang is the game inherit / authoring language for untranslated.
const locSourceLang = "english"

const healthLocCap = 500

type locSite struct {
	key, value, file string
	line             int
}

type locGID struct{ lang, kind, name, origin string }

// Coverage returns per-language loc KPIs and grouped Health rows.
func Coverage(s *session.Session) ([]HealthLang, []HealthRow) {
	// Reshape in one pass rather than through LocByLang, which hands back a deep
	// copy of every language. Copying it and then building this map held three
	// copies of the whole loc corpus, which on a total conversion is most of the
	// memory this function uses.
	byLang := map[string]map[string]locSite{}
	s.EachLoc(func(lang, k string, v derived.LocEntry) {
		entries := byLang[lang]
		if entries == nil {
			entries = map[string]locSite{}
			byLang[lang] = entries
		}
		entries[k] = locSite{key: k, value: v.Value, file: v.Path, line: v.Line}
	})
	if len(byLang) == 0 {
		return nil, nil
	}

	referenced := map[string][]locSite{}
	used := s.UsedLocKeys()
	for _, r := range s.LocRefs() {
		if r.Kind != "loc" {
			continue
		}
		if skipLocIssueFile(s, r.Path) {
			continue
		}
		referenced[r.Key] = append(referenced[r.Key], locSite{
			key: r.Key, file: r.Path, line: r.Line,
		})
	}

	source := byLang[locSourceLang]
	if source == nil {
		source = map[string]locSite{}
	}

	langs := make([]string, 0, len(byLang))
	for lang := range byLang {
		langs = append(langs, lang)
	}
	slices.Sort(langs)

	groups := map[locGID]*HealthRow{}
	var order []locGID
	// KPI counts every distinct issue; only the first healthLocCap per language
	// become rows. Counting separately from materialising is what lets the row
	// budget stop the file reads — before, the cap was applied at the end, so a
	// conversion paid for every issue and then threw nearly all of them away.
	seenID := map[locGID]bool{}
	kpi := map[locGID]int{}
	kept := map[string]int{}
	// Building a site reads the file and splits it into lines, so it happens
	// only for sites actually kept. A total conversion produces tens of
	// thousands of issues across a dozen languages, of which healthLocCap keeps
	// 500 per language — doing the read up front meant paying for every one of
	// the rest, and it was most of the cost of Workspace Health.
	addIssue := func(lang, kind string, site locSite) {
		origin, _, _ := s.Locate(site.file)
		origin = originID(origin)
		makeSite := func() OverrideSite {
			os := OverrideSite{
				Origin: origin, OriginName: s.OriginName(origin),
				Path: site.file, Rel: s.DisplayRel(site.file), Line: site.line,
			}
			fillSnippet(s, &os)
			return os
		}
		id := locGID{lang, kind, site.key, origin}
		if g := groups[id]; g != nil {
			if len(g.Sites) < maxRowSites {
				g.Sites = append(g.Sites, makeSite())
			}
			g.Refs++
			return
		}
		if seenID[id] {
			return // counted already; this language is past its row budget
		}
		seenID[id] = true
		kpi[locGID{lang: lang, kind: kind}]++
		if kept[lang] >= healthLocCap {
			return
		}
		kept[lang]++
		os := makeSite()
		groups[id] = &HealthRow{
			Type: kind, Name: site.key, Language: lang,
			Origin: origin, OriginName: s.OriginName(origin),
			From: origin, FromName: s.OriginName(origin),
			Rel: os.Rel, Sites: []OverrideSite{os}, Refs: 1,
		}
		order = append(order, id)
	}

	// Memoised across languages: a key's owner does not depend on which
	// language file it sits in, and a mod shipping a dozen translations would
	// otherwise decompose every key a dozen times.
	byConvention := map[string]bool{}
	hasOwner := func(key string) bool {
		if v, done := byConvention[key]; done {
			return v
		}
		ok := s.LocKeyExplained(key)
		byConvention[key] = ok
		return ok
	}

	outLangs := make([]HealthLang, 0, len(langs))
	// Once for every language, not once per language: the base key set does not
	// depend on the language, and this ran on every drag of the load-order
	// preview (LOC.4).
	inherited := s.InheritedLocKeys(langs)
	for _, lang := range langs {
		entries := byLang[lang]
		outLangs = append(outLangs, HealthLang{Language: lang, Defined: len(entries)})
		// Per language deliberately: a translator wants to see which keys they
		// have not done yet, which is what TestCoverageMissingNotSuppressedBy-
		// DefaultLang pins. What must not happen is reporting a key vanilla
		// already provides — that is inheritedKeys' job, and it was failing for
		// every non-default language.
		for key, sites := range referenced {
			if _, ok := entries[key]; ok || inherited.Has(lang, key) {
				continue
			}
			for _, site := range sites {
				addIssue(lang, "missing", site)
			}
		}
		for key, site := range entries {
			if used[key] || inherited.Has(lang, key) {
				continue
			}
			// A key nothing cites can still be consumed by name: the engine
			// reads `ACHIEVEMENT_DESC_<id>` for an achievement and
			// `notification_<id>_tooltip` for a message without either
			// appearing in script. Asked here rather than expanded into stored
			// references, because it is only ever asked of the few keys that
			// got this far.
			if hasOwner(key) {
				continue
			}
			addIssue(lang, "orphaned", site)
		}
		if lang == locSourceLang {
			continue
		}
		for key, site := range entries {
			src, ok := source[key]
			if !ok {
				continue
			}
			isCopy := src.value == site.value && strings.TrimSpace(site.value) != ""
			blank := strings.TrimSpace(site.value) == ""
			if !isCopy && !blank {
				continue
			}
			addIssue(lang, "untranslated", site)
		}
	}

	byKPI := map[string]*HealthLang{}
	for i := range outLangs {
		byKPI[outLangs[i].Language] = &outLangs[i]
	}
	// From the counter, not from order: order is capped to the row budget, so
	// counting it would report "500 missing" for a mod that has thousands.
	for id, n := range kpi {
		hl := byKPI[id.lang]
		if hl == nil {
			continue
		}
		switch id.kind {
		case "missing":
			hl.Missing += n
		case "orphaned":
			hl.Orphaned += n
		case "untranslated":
			hl.Untranslated += n
		}
	}
	slices.SortFunc(order, func(a, b locGID) int {
		if c := cmp.Compare(a.lang, b.lang); c != 0 {
			return c
		}
		if c := cmp.Compare(a.name, b.name); c != 0 {
			return c
		}
		return cmp.Compare(a.kind, b.kind)
	})
	// order is already within budget — addIssue stopped materialising groups
	// once a language hit healthLocCap.
	rows := make([]HealthRow, 0, len(order))
	for _, id := range order {
		rows = append(rows, *groups[id])
	}
	slices.SortFunc(rows, healthRowLess)
	return outLangs, rows
}

func skipLocIssueFile(s *session.Session, path string) bool {
	if origin, _, ok := s.Locate(path); ok && origin == game.OriginVanilla {
		return true
	}
	switch s.KindFor(path) {
	case "mod", "meta":
		return true
	default:
		return false
	}
}

// Lookup returns the default-lang loc text and winning site for key, or nil.
func Lookup(s *session.Session, key string) *LocLookup {
	if key == "" {
		return nil
	}
	text := locValue(s, key)
	hit := &LocLookup{Key: key, Text: text}
	if file, line, _, origin, ok := s.LocSite(key); ok {
		hit.Path, hit.Line, hit.Origin = file, line, originID(origin)
		hit.OriginName = s.OriginName(hit.Origin)
	}
	if hit.Text == "" && hit.Path == "" {
		return nil
	}
	return hit
}
