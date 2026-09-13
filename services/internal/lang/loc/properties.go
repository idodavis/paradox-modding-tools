// properties.go lists script properties whose right-hand side is a localization
// key, split into STRICT (unresolved => "missing loc") and BROAD (resolved hint
// only). These are engine property names, not game data.

package loc

import (
	"strings"
	"unicode"
)

// strictProps virtually always hold a loc key.
var strictProps = map[string]bool{
	"title":             true,
	"desc":              true,
	"flavor":            true,
	"custom_tooltip":    true,
	"selection_tooltip": true,
	"confirm_text":      true,
	"confirm_title":     true,
	"prompt":            true,
	"failure_desc":      true,
	"success_desc":      true,
	"war_name":          true,
	"cb_name":           true,
	"notification_text": true,
	// trigger_localization / effect_localization person+tense slots
	"first": true, "third": true, "global": true, "none": true,
	"first_not": true, "third_not": true, "global_not": true, "none_not": true,
	"first_past": true, "third_past": true, "global_past": true,
	"first_neg": true, "third_neg": true, "global_neg": true,
	"first_past_neg": true, "third_past_neg": true, "global_past_neg": true,
}

// broadProps often hold a loc key (superset of strict). These also hold non-loc
// values, so only a resolved key produces a hint.
var broadProps = map[string]bool{
	"name": true, "text": true, "tooltip": true, "first_valid": true,
	"reason": true, "format": true, "header": true, "opinion_text": true,
	"what": true, "who": true, "notification": true,

	// localization_key was strict, and it is the single largest source of false
	// "missing localization" anywhere in the engine. Counted over vanilla, where
	// by definition nothing is missing, its value fails to resolve 2,607 times
	// on CK3, 11,597 on Victoria 3 and 46,481 on EU5 — 99.7% of every unresolved
	// strict property on that game.
	//
	// The reason is visible in the values: `localization_key = CustomLoc_BR_male_`
	// ends in an underscore because customizable localization completes the stem
	// at runtime. No key by that name exists or should. Broad still marks a
	// resolved key used and still shows its text on hover; it just stops
	// asserting that a key the game builds itself is missing.
	"localization_key": true,
}

// Property classifies a script property name as a loc-key holder.
type Property string

const (
	// PropNone is a property that does not hold a loc key.
	PropNone Property = ""
	// PropStrict flags an unresolved value as a missing-loc diagnostic.
	PropStrict Property = "strict"
	// PropBroad shows a resolved-loc hint only, silent when unresolved.
	PropBroad Property = "broad"
)

// Classify returns whether prop holds a loc key. Names match the listed
// lowercase engine fields (`title`, `desc`); ALL_CAPS trigger arguments
// (`TITLE = primary_title`) are not loc properties.
func Classify(prop string) Property {
	if strictProps[prop] {
		return PropStrict
	}
	if broadProps[prop] {
		return PropBroad
	}
	return PropNone
}

// LooksLikeKey reports whether s is a plausible loc key (not yes/no/none,
// not a `scope:` / `character:` prefix, not a built-in scope, not prose).
//
// The prose case is why quoting proves nothing either way. Paradox quotes real
// keys — `war_name = "HRE_CONQUEST_WAR_NAME"` — and also writes display text
// straight into the same properties, which the game shows verbatim:
//
//	desc = "Always make coronations!"
//	desc = "The Red Keep"
//
// What separates them is not the quotes but the space. A localization file is
// `key:0 "value"`, so a key is a single token and can never contain whitespace.
// Without this check every literal string in a strict property was demanded as
// a missing key: 537 of A Game of Thrones' 618 such diagnostics, and EU5's
// "Wrong culture" / "Wrong religion".
func LooksLikeKey(s string) bool {
	if s == "" || strings.Contains(s, ":") {
		return false
	}
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return false
	}
	// A value carrying a macro parameter is not knowable until the macro is
	// called: the key is whatever the caller passed, or a stem the engine
	// completes at runtime. Reporting either as missing tells a modder that
	// correct script is wrong — `desc = $TT$` in a scripted effect is not a key
	// by that name, and no key by that name should exist.
	if strings.Contains(s, "$") {
		return false
	}
	switch strings.ToLower(s) {
	case "yes", "no", "none", "root", "prev", "this", "from":
		return false
	}
	return true
}

// IsLocEngineValue reports a loc $key$ / $key|filter$ that is engine data,
// not a loc-key reuse ($other_key$ / $INDEPENDENCE_WAR_NAME$ / $key|U$).
func IsLocEngineValue(key, filter string) bool {
	if isLocEngineToken(key) {
		return true
	}
	return strings.EqualFold(key, "VALUE") && isNumericLocFilter(filter)
}

// isLocEngineToken reports a single ALL_CAPS token ($ORDER$, $VALUE$, $NAME$).
// SNAKE_CASE ($INDEPENDENCE_WAR_NAME$) is loc-key reuse.
func isLocEngineToken(key string) bool {
	if key == "" || strings.Contains(key, "_") {
		return false
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func isNumericLocFilter(filter string) bool {
	if filter == "" {
		return false
	}
	for _, r := range filter {
		switch r {
		case '=', '+', '-', '%', '.':
		default:
			if !unicode.IsDigit(r) {
				return false
			}
		}
	}
	return true
}
