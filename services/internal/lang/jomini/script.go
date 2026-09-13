// script.go is the Clausewitz/Jomini script grammar that every game built on
// the engine shares: how a folder name becomes a kind, which block names open a
// trigger or an effect slot, and the assignment keys that name an ephemeral
// value (a saved scope, a variable, a `$PARAM$`).
//
// Nothing here branches on a game. Per-game rules — folder-to-extract mapping,
// FIOS, EU5 entry modes — live in internal/game, which imports this package.

package jomini

import "strings"

// ScopePrefix is the saved-scope qualifier (`scope:target`).
const ScopePrefix = "scope"

// CanonicalKind is the stored/compare identity: spaces become underscores.
// Folder name is the kind. Tree roles already map events → event.
func CanonicalKind(kind string) string {
	return strings.ReplaceAll(kind, " ", "_")
}

// IsMacroKind reports a kind whose definitions are invoked by name rather than
// referenced as data: the `scripted_effects` / `scripted_triggers` /
// `scripted_guis` convention. Recognising it by name means a macro that is
// defined but never called is still offered in completion, which a
// usage-derived list cannot do.
func IsMacroKind(kind string) bool {
	return strings.HasPrefix(CanonicalKind(strings.ToLower(kind)), "scripted_")
}

func DeclaresNamedBodies(kind string) bool {
	k := CanonicalKind(strings.ToLower(kind))
	return strings.HasPrefix(k, "scripted_") || strings.HasPrefix(k, "script_value")
}

// eventGateKeys are event-body blocks that gate whether the event runs.
var eventGateKeys = map[string]bool{
	"trigger":              true,
	"cancellation_trigger": true,
	"on_trigger_fail":      true,
}

// EventSectionRole is "gate" for run-conditions, else "effect".
func EventSectionRole(name string) string {
	if eventGateKeys[strings.ToLower(name)] {
		return "gate"
	}
	return "effect"
}

var triggerSlotKeys = map[string]bool{
	// alternative_limit is the else-branch of a limit, so its body is triggers
	// like any other limit's.
	"limit": true, "alternative_limit": true, "potential": true,
	"and": true, "or": true, "not": true, "nor": true, "nand": true,
}

var effectSlotKeys = map[string]bool{
	"immediate": true, "option": true, "after": true, "else": true, "effect": true,
}

// ScriptSlot is the completion bag for a block named key: "trigger", "effect",
// or "" (object-body structure keys). Event gates map to trigger; EventSectionRole's
// default "effect" is not used (title/type are not effect slots).
func ScriptSlot(key string) string {
	k := strings.ToLower(key)
	if EventSectionRole(k) == "gate" {
		return "trigger"
	}
	if triggerSlotKeys[k] {
		return "trigger"
	}
	if effectSlotKeys[k] {
		return "effect"
	}
	switch {
	case strings.HasPrefix(k, "any_"):
		return "trigger"
	case strings.HasPrefix(k, "every_"),
		strings.HasPrefix(k, "random_"),
		strings.HasPrefix(k, "ordered_"):
		return "effect"
	}
	return ""
}

// IsScopeNeutral reports a block key whose body runs in the SAME scope as the
// block around it: Clausewitz control flow, the named sections of an event, and
// the sections of an on_action.
//
// This is language rather than data, so it is the one scope table that cannot
// come from a dump. It is deliberately a list of what does NOT change scope —
// the complement is open-ended and grows with every game patch, while this side
// is the engine's own grammar and does not.
func IsScopeNeutral(key string) bool {
	switch strings.ToLower(key) {
	case "if", "else", "else_if", "while", "switch", "limit", "trigger_if",
		"trigger_else", "trigger_else_if", "alternative_limit",
		"and", "or", "not", "nor", "nand", "potential",
		"trigger", "immediate", "effect", "option", "after",
		"cancellation_trigger", "on_trigger_fail", "random_list", "modifier",
		// on_action sections. An on_action's declared scope is the scope its
		// own body runs in, so an event fired from one of these runs in it too.
		"events", "random_events", "first_valid", "on_actions", "fallback",
		"weight_multiplier":
		return true
	}
	return false
}

// ScriptNameRule describes how an assignment key harvests an ephemeral name.
type ScriptNameRule struct {
	Kind     string // saved_scope, var, global_var, …
	IsDef    bool   // definition site vs use/ref
	InnerKey string // "" = scalar RHS; else look for this field inside a block
}

// IsEphemeral reports language kinds that must not win generic Resolve / outline.
// Unresolved-prefix kinds (flag, cu, p, …) use the prefix as the kind name.
func IsEphemeral(kind string) bool {
	switch CanonicalKind(kind) {
	case "script_param", "saved_scope",
		"var", "global_var", "local_var",
		"flag":
		return true
	default:
		return false
	}
}

// PrefixKind remaps `scope:` to saved_scope. Other language prefixes keep
// the prefix as the kind (`var`, `flag`, …).
func PrefixKind(prefix string) string {
	if prefix == ScopePrefix {
		return "saved_scope"
	}
	return ""
}

// ScriptName returns the ephemeral-name rule for an assignment key, if any.
func ScriptName(key string) (ScriptNameRule, bool) {
	r, ok := scriptNames[strings.ToLower(key)]
	return r, ok
}

// scriptNames are the Jomini effects and triggers that name an ephemeral value
// rather than referencing an object, present on CK3 / Vic3 / EU5 alike.
var scriptNames = map[string]ScriptNameRule{
	"set_variable":           {Kind: "var", IsDef: true, InnerKey: "name"},
	"has_variable":           {Kind: "var"},
	"remove_variable":        {Kind: "var"},
	"change_variable":        {Kind: "var"},
	"set_global_variable":    {Kind: "global_var", IsDef: true, InnerKey: "name"},
	"has_global_variable":    {Kind: "global_var"},
	"remove_global_variable": {Kind: "global_var"},
	"change_global_variable": {Kind: "global_var"},
	"set_local_variable":     {Kind: "local_var", IsDef: true, InnerKey: "name"},
	"has_local_variable":     {Kind: "local_var"},
	"remove_local_variable":  {Kind: "local_var"},
	"change_local_variable":  {Kind: "local_var"},

	"save_scope_as":                 {Kind: "saved_scope", IsDef: true},
	"save_temporary_scope_as":       {Kind: "saved_scope", IsDef: true},
	"save_temporary_value_as":       {Kind: "saved_scope", IsDef: true},
	"save_scope_value_as":           {Kind: "saved_scope", IsDef: true, InnerKey: "name"},
	"save_temporary_scope_value_as": {Kind: "saved_scope", IsDef: true, InnerKey: "name"},
	"clear_saved_scope":             {Kind: "saved_scope"},
}

// IsSaveScopeKey reports a scalar `save_*_scope_as = name` (or temporary value) site.
func IsSaveScopeKey(key string) bool {
	r, ok := ScriptName(key)
	return ok && r.Kind == "saved_scope" && r.IsDef && r.InnerKey == ""
}

// IsSaveScopeValueKey reports a block `save_*_scope_value_as = { name = X }`.
func IsSaveScopeValueKey(key string) bool {
	r, ok := ScriptName(key)
	return ok && r.Kind == "saved_scope" && r.IsDef && r.InnerKey == "name"
}

// ScriptParamSpan reports when off lies inside a $NAME$ macro span in src.
func ScriptParamSpan(src string, off int) (name string, start, end int, ok bool) {
	if off < 0 || off > len(src) {
		return "", 0, 0, false
	}
	sliceEnd := min(off+1, len(src))
	open := strings.LastIndexByte(src[:sliceEnd], '$')
	if open < 0 {
		return "", 0, 0, false
	}
	i := open + 1
	nameStart := i
	for i < len(src) && IsScriptParamByte(src[i], i == nameStart) {
		i++
	}
	if i == nameStart || i >= len(src) || src[i] != '$' {
		return "", 0, 0, false
	}
	name = src[nameStart:i]
	if off < open || off >= i+1 {
		return "", 0, 0, false
	}
	return name, open, i + 1, true
}

func IsScriptParamByte(c byte, first bool) bool {
	if first {
		return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9')
}

var structuralDocs = map[string]string{
	// Gates and conditions.
	"limit":             "Condition block gating the effects around it — an `if`, an iterator, or a `random_list` entry.",
	"trigger":           "Condition block that must pass for the enclosing entry to apply.",
	"potential":         "Condition deciding whether this entry exists for the current scope at all.",
	"valid":             "Condition that must hold for the entry to be selectable; failing it shows the entry but disables it.",
	"is_shown":          "Condition deciding whether the entry is displayed; it may still be invalid to pick.",
	"alternative_limit": "Fallback condition, evaluated only when the preceding `limit` fails.",
	"count":             "How many members of an iterator must satisfy the block: a number, `all`, or a percentage.",
	"percent":           "Share of an iterator's members that must satisfy the block, as a fraction between 0 and 1.",

	// Event and interaction structure.
	"immediate":        "Effects run the moment this fires, before anything is shown to the player.",
	"after":            "Effects run once the window closes, after the player has chosen an option.",
	"option":           "One choice offered to the player, with its own trigger, effects and localization.",
	"selection_effect": "Effects run when the player selects this entry.",
	"type":             "Which variety of object this is — for an event, the window form it is presented in.",
	"name":             "The identifier this block defines, or the one it refers to.",

	// Presentation.
	"title":               "Localization key for the heading.",
	"desc":                "Localization key for the body text; may be a block of `triggered_desc` alternatives.",
	"triggered_desc":      "One description alternative: a trigger plus the `desc` used when it passes.",
	"flavor":              "Localization key for flavour text shown beside the main description.",
	"picture":             "Image asset shown on the window.",
	"theme":               "Named visual theme — background, icons and sound — for the window.",
	"left_portrait":       "Character portrait shown on the left of the window.",
	"right_portrait":      "Character portrait shown on the right of the window.",
	"lower_left_portrait": "Character portrait shown in the lower-left of the window.",

	// Weights and script-value arithmetic.
	"weight":           "Relative chance of this entry being chosen among its siblings.",
	"value":            "The number, or script value, this entry evaluates to.",
	"modifier":         "A weight adjustment: a trigger plus the `factor` or `add` applied when it passes.",
	"factor":           "Multiplier applied to the enclosing weight when the block's trigger passes.",
	"compare_modifier": "Weight adjustment scaled by how far a compared value sits from a target.",
	"opinion_modifier": "Weight adjustment scaled by one character's opinion of another.",
	"add":              "Adds to the running total of the enclosing script value or weight.",
	"subtract":         "Subtracts from the running total of the enclosing script value or weight.",
	"multiply":         "Multiplies the running total of the enclosing script value or weight.",
	"divide":           "Divides the running total of the enclosing script value or weight.",
	"min":              "Lower bound clamped onto the running value.",
	"max":              "Upper bound clamped onto the running value.",

	// List selection.
	"first_valid":  "Picks the first entry whose trigger passes and ignores the rest.",
	"random_valid": "Picks one entry at random from those whose trigger passes.",
}

// StructuralDoc returns PMT's own one-line description of a Clausewitz block
// keyword, or "" when the key is not one. Callers must consult the install's
// prose and the game's script_docs first; this is the last resort.
func StructuralDoc(key string) string {
	return structuralDocs[strings.ToLower(key)]
}

// The PMT-assigned kind sentinels. game.MatchExtract hands these out by file
// shape; every OTHER kind is the parent folder's name and is therefore
// game-derived, so `traits`, `provinces` and `casus_belli_types` appear and
// disappear as a patch adds and removes folders.
//
// Only the sentinels get constants. Enumerating the game-derived kinds would be
// the hand-maintained schema that makes a rival validator lag every patch, and
// not needing one is this project's whole wedge.
const (
	KindLocKey        = "loc_key"
	KindLocValue      = "loc_value"
	KindNamespace     = "namespace"
	KindEvent         = "event"
	KindOnAction      = "on_action"
	KindGUIType       = "gui_type"
	KindModDescriptor = "mod_descriptor"
	KindScriptParam   = "script_param"
)

// IsObjectKind reports a kind whose definitions are things script can point at.
//
// The complement is not one category but three, and they were being excluded one
// at a time, separately, at each place that resolves a name to a thing:
//
//   - ephemerals — a saved scope or a `$PARAM$` exists only while a body runs;
//   - `loc_key` — a caption is not an object, though every object has one, and
//     the games localize a great many words that are also script tokens;
//   - `namespace` — an id prefix for events is not an object, though it shares a
//     name with one often enough that a mod's `namespace = conqueror` outranked
//     vanilla's `conqueror` trait.
//
// Each of those was found as a separate hover bug. Anything resolving a name to
// a thing wants this predicate. Anything asking the different question "is this
// name declared anywhere" does not — a namespace is genuinely declared.
func IsObjectKind(kind string) bool {
	switch CanonicalKind(strings.ToLower(kind)) {
	case "", "loc_key", "namespace":
		return false
	}
	return !IsEphemeral(kind)
}

type DeclKeyword struct {
	Keyword string   // the word before the name
	Kind    string   // the kind this declaration produces
	Local   bool     // the engine resolves the name within its own file
	Games   []string // which games write it; the parser accepts all
	Note    string   // why Local is what it is
}

// DeclKeywords are the forms whose keyword does NOT name their kind. Read by
// DeclKeywordKind and DeclIsLocal.
//
// Any keyword not listed names its own kind: `scripted_trigger foo = { … }`
// declares a scripted_trigger. LocalDeclPrefix then decides whether it is
// file-local.
var DeclKeywords = []DeclKeyword{
	{Keyword: "type", Kind: "gui_type", Local: false,
		Games: []string{"ck3", "vic3", "eu5"},
		Note:  "a GUI type is visible to every .gui file; that is the point"},
	{Keyword: "template", Kind: "gui_type", Local: false,
		Games: []string{"ck3", "vic3", "eu5"},
		Note:  "as type"},
	{Keyword: "local_template", Kind: "gui_type", Local: true,
		Games: []string{"ck3", "vic3", "eu5"},
		Note:  "says so in its name"},
	{Keyword: "types", Kind: "types", Local: false,
		Games: []string{"ck3", "vic3", "eu5"},
		Note:  "a container of declarations, not a declaration"},
}

// LocalDeclPrefix marks the keyword family the engine resolves per file, so a
// name declared under it is invisible to other files and mods repeat it on
// purpose.
//
// GAME: ck3 writes `scripted_trigger foo = { … }` inline in events files — 3,809
// in vanilla, every one under events/. Victoria 3 and EU5 do not use the form.
// See catalog.Def.Local and GAME-SYNTAX 16.
const LocalDeclPrefix = "scripted_"

// DeclKeywordKind is the kind a declaration keyword produces, or "" when the
// word is script grammar rather than a declaration.
func DeclKeywordKind(kw string) string {
	lower := strings.ToLower(kw)
	for _, d := range DeclKeywords {
		if d.Keyword == lower {
			return d.Kind
		}
	}
	if IsStop(kw) {
		return ""
	}
	return kw
}

// DeclIsLocal reports a declaration the engine resolves within its own file.
func DeclIsLocal(kw string) bool {
	lower := strings.ToLower(kw)
	for _, d := range DeclKeywords {
		if d.Keyword == lower {
			return d.Local
		}
	}
	return strings.HasPrefix(lower, LocalDeclPrefix)
}

func AuthorProse(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.Join(strings.Fields(p), " "); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}

func Prose(parts []string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(strings.Join(parts, " ")), " "))
}
