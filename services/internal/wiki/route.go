// route.go decides which wiki pages a file's Guide tabs show: the pages named for its kind or its
// folder, and for a script file the pages of the scripting language.
package wiki

import (
	"strings"

	"paradox-modding-tools/services/internal/game"
)

const guideTabCap = 6

// synonyms are the page stems a kind that is not script also matches.
var synonyms = map[string][]string{
	"gui": {"interface", "gui script"}, "gui_type": {"interface", "gui script"},
	"loc": {"localization", "customizable localization"}, "loc_key": {"localization", "customizable localization"},
	"mod_descriptor": {"mod structure"}, "mod": {"mod structure"}, "meta": {"mod structure"},
}

var languageStems = map[string]bool{"effect": true, "trigger": true, "scope": true, "on action": true}

// Attribution is the Guide / Patch Notes footer line.
func Attribution(gameID string) string {
	info := game.Get(gameID)
	name := "Paradox"
	if info != nil && info.ShortName != "" {
		name = info.ShortName
	}
	return "From " + name + " Wiki (" + License + ")"
}

func wikiBaseURL(gameID string) string {
	info := game.Get(gameID)
	if info == nil || info.WikiAPI == "" {
		return ""
	}
	return strings.TrimSuffix(info.WikiAPI, "api.php")
}

func pageURL(gameID, title string) string {
	base := wikiBaseURL(gameID)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + strings.ReplaceAll(title, " ", "_")
}
