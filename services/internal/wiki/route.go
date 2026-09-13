// route.go matches sidecar wiki pages to an extract kind + relative path.
package wiki

import (
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
)

const guideTabCap = 6

func isScriptKind(kind string) bool {
	switch jomini.CanonicalKind(kind) {
	case "loc_key", "loc", "gui_type", "gui", "mod_descriptor", "mod", "meta":
		return false
	default:
		return true
	}
}

func foldWord(w string) string {
	if len(w) <= 2 || strings.HasSuffix(w, "ss") {
		return w
	}
	if before, ok := strings.CutSuffix(w, "s"); ok {
		return before
	}
	return w
}

func fold(s string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	for i, w := range fields {
		fields[i] = foldWord(w)
	}
	return strings.Join(fields, " ")
}

func spaced(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

// stemTitle is the folded kind-like core of a wiki title, or empty for hubs.
func stemTitle(title string) string {
	n := normTitle(title)
	n = strings.TrimSuffix(n, " guide")
	n = strings.TrimSuffix(n, " modding")
	n = strings.TrimPrefix(n, "scripted ")
	n = strings.TrimSpace(n)
	if n == "" || n == "modding" {
		return ""
	}
	return fold(n)
}

func isLangStem(stem string) bool {
	switch stem {
	case "effect", "trigger", "scope", "on action":
		return true
	default:
		return false
	}
}

func parentSeg(rel string) string {
	slash := strings.ReplaceAll(rel, "\\", "/")
	slash = strings.Trim(slash, "/")
	i := strings.LastIndex(slash, "/")
	if i < 0 {
		return ""
	}
	dir := slash[:i]
	j := strings.LastIndex(dir, "/")
	if j < 0 {
		return dir
	}
	return dir[j+1:]
}

func addTokens(set map[string]bool, s string) {
	s = fold(spaced(s))
	if s == "" {
		return
	}
	set[s] = true
	for w := range strings.FieldsSeq(s) {
		set[w] = true
	}
}

// fileMatch is the tokenized extract kind + relative path used to score titles.
type fileMatch struct {
	tokens     map[string]bool
	kindFold   string
	parentFold string
	lastWord   string
}

func fileTokens(kind, rel string) fileMatch {
	ck := jomini.CanonicalKind(kind)
	m := fileMatch{
		tokens:     map[string]bool{},
		kindFold:   fold(spaced(ck)),
		parentFold: fold(spaced(parentSeg(rel))),
		lastWord:   fold(spaced(ck)),
	}
	if i := strings.LastIndex(ck, "_"); i >= 0 && i+1 < len(ck) {
		m.lastWord = fold(ck[i+1:])
	}
	addTokens(m.tokens, m.kindFold)
	addTokens(m.tokens, m.parentFold)
	addTokens(m.tokens, m.lastWord)
	switch ck {
	case "gui_type", "gui":
		addTokens(m.tokens, "gui")
		addTokens(m.tokens, "interface")
		addTokens(m.tokens, "gui script")
	case "loc_key", "loc":
		addTokens(m.tokens, "localization")
		addTokens(m.tokens, "loc")
		addTokens(m.tokens, "customizable localization")
	case "mod_descriptor", "mod", "meta":
		addTokens(m.tokens, "mod")
		addTokens(m.tokens, "mod structure")
	}
	return m
}

func scoreTitle(title string, m fileMatch) int {
	st := stemTitle(title)
	if st == "" {
		return 0
	}
	if st == m.kindFold || (m.parentFold != "" && st == m.parentFold) {
		return 3
	}
	if m.lastWord != "" && st == m.lastWord {
		return 2
	}
	words := strings.Fields(st)
	if len(words) == 0 {
		return 0
	}
	for _, w := range words {
		if !m.tokens[w] {
			return 0
		}
	}
	return 1
}

func betterPage(titleA string, scoreA int, titleB string, scoreB int) bool {
	if scoreA != scoreB {
		return scoreA > scoreB
	}
	aMod := strings.Contains(strings.ToLower(titleA), "modding")
	bMod := strings.Contains(strings.ToLower(titleB), "modding")
	if aMod != bMod {
		return aMod
	}
	if len(titleA) != len(titleB) {
		return len(titleA) < len(titleB)
	}
	return titleA < titleB
}

// Attribution is the Guide / Patch Notes footer line.
func Attribution(gameID string) string {
	g := game.Get(gameID)
	name := "Paradox"
	if g != nil && g.ShortName != "" {
		name = g.ShortName
	}
	return "From " + name + " Wiki (" + License + ")"
}

func wikiBaseURL(gameID string) string {
	g := game.Get(gameID)
	if g == nil || g.WikiAPI == "" {
		return ""
	}
	return strings.TrimSuffix(g.WikiAPI, "api.php")
}

func pageURL(gameID, title string) string {
	base := wikiBaseURL(gameID)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + strings.ReplaceAll(title, " ", "_")
}
