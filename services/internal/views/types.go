// types.go holds graph DTOs (no coordinates) and shared view helpers.

package views

import (
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

// EventGraphParams selects which definitions to draw.
type EventGraphParams struct {
	Root      string   `json:"root,omitempty"`
	Namespace string   `json:"namespace,omitempty"`
	Origins   []string `json:"origins,omitempty"`
	MaxNodes  int      `json:"maxNodes,omitempty"`
	Expand    []string `json:"expand,omitempty"`
}

// EventGraph is a coordinate-free node/edge payload for frontend layout.
type EventGraph struct {
	Nodes       []EventGraphNode `json:"nodes"`
	Edges       []EventGraphEdge `json:"edges"`
	Truncated   bool             `json:"truncated"`
	EmptyReason string           `json:"emptyReason,omitempty"`
}

// EventGraphNode is one graph card. There is no x/y: layout is frontend dagre.
type EventGraphNode struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin,omitempty"`
	OriginName string `json:"originName,omitempty"`
	Title      string `json:"title,omitempty"`
	Role       string `json:"role,omitempty"`
	Fires      int    `json:"fires,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

// EventGraphEdge is a directed link. Via names the referencing field or hop chain.
type EventGraphEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Via   string `json:"via"`
	Label string `json:"label,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// EventLocField is a resolved localization key.
type EventLocField struct {
	Key  string `json:"key"`
	Text string `json:"text,omitempty"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
}

// EventScriptLine is one rendered pseudo-script line.
type EventScriptLine struct {
	Depth int    `json:"depth"`
	Text  string `json:"text"`
}

// EventStepTarget is an event/on_action a block hands control to.
type EventStepTarget struct {
	Name string `json:"name"`
}

// EventFieldInfo is a top-level scalar in an event or option body.
type EventFieldInfo struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Line   int    `json:"line"`
	Quoted bool   `json:"quoted,omitempty"`
}

// EventSectionInfo is one named script block (trigger, immediate, option gate, …).
type EventSectionInfo struct {
	Name    string            `json:"name"`
	Role    string            `json:"role,omitempty"`
	Lines   []EventScriptLine `json:"lines"`
	Targets []EventStepTarget `json:"targets"`
}

// EventOptionInfo is one option = { } block.
type EventOptionInfo struct {
	Fields   []EventFieldInfo  `json:"fields"`
	Name     *EventLocField    `json:"name,omitempty"`
	Trigger  *EventSectionInfo `json:"trigger,omitempty"`
	AiChance *EventSectionInfo `json:"aiChance,omitempty"`
	Lines    []EventScriptLine `json:"lines"`
	Targets  []EventStepTarget `json:"targets"`
}

// EventRefInfo is a named reference inside an event body.
type EventRefInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	DefFile string `json:"defFile,omitempty"`
	DefLine int    `json:"defLine,omitempty"`
}

// RefKindGroup groups refs by kind for the inspector accordion.
type RefKindGroup struct {
	Kind string         `json:"kind"`
	Refs []EventRefInfo `json:"refs"`
}

// EventDetail is the inspector payload for one resolved def (event or other).
type EventDetail struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind,omitempty"`
	Path       string             `json:"path"`
	Rel        string             `json:"rel,omitempty"`
	Origin     string             `json:"origin,omitempty"`
	OriginName string             `json:"originName,omitempty"`
	Line       int                `json:"line"`
	Fields     []EventFieldInfo   `json:"fields"`
	Type       string             `json:"type,omitempty"`
	Hidden     bool               `json:"hidden,omitempty"`
	Theme      string             `json:"theme,omitempty"`
	Namespace  string             `json:"namespace,omitempty"`
	Title      *EventLocField     `json:"title,omitempty"`
	Desc       *EventLocField     `json:"desc,omitempty"`
	Flavor     *EventLocField     `json:"flavor,omitempty"`
	Sections   []EventSectionInfo `json:"sections"`
	Options    []EventOptionInfo  `json:"options"`
	RefGroups  []RefKindGroup     `json:"refGroups,omitempty"`
	Incoming   []EventGraphEdge   `json:"incoming,omitempty"`
}

// LocLookup is one localization key's resolved english text and site.
type LocLookup struct {
	Key        string `json:"key"`
	Text       string `json:"text"`
	Path       string `json:"path,omitempty"`
	Line       int    `json:"line,omitempty"`
	Origin     string `json:"origin,omitempty"`
	OriginName string `json:"originName,omitempty"`
}

var targetNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

func locValue(s *session.Session, key string) string {
	v, _ := s.DefaultLoc(key)
	return v
}

// eventNamespace is the declared namespace for an event, else the id prefix.
func eventNamespace(s *session.Session, d *index.Def, id string) string {
	if d != nil {
		var last string
		for _, x := range s.DefsInFile(d.Path) {
			if jomini.CanonicalKind(x.Kind) == "namespace" && x.Line <= d.Line {
				last = x.Key
			}
		}
		if last != "" {
			return last
		}
	}
	if i := strings.IndexByte(id, '.'); i > 0 {
		return id[:i]
	}
	return ""
}

func titleOf(s *session.Session, id string) string {
	keys := []string{
		id + ".t",
		strings.ReplaceAll(id, ".", "_") + "_t",
		id + ".title",
		id,
	}
	for _, key := range keys {
		if v := locValue(s, key); v != "" {
			return v
		}
	}
	return ""
}

func originID(origin string) string {
	if origin == "" {
		return game.OriginVanilla
	}
	return origin
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
