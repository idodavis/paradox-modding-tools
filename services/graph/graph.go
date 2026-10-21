// Package graph is the Event Graph page's service. It builds the graph of what fires what from one root, using the
// index's fires, and the detail of one node. The graph has no coordinates; the page lays it out.
package graph

import (
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/services/workspace"
)

// Service is the Event Graph service.
type Service struct {
	Workspace *workspace.Service
}

// A Node is one node of the event graph: the kept definition of an event, an on_action, a decision or a scripted
// effect, with where it is written.
type Node struct {
	ID       string          `json:"id"` // database:name
	Database string          `json:"database"`
	Name     string          `json:"name"`
	Title    string          `json:"title"` // its localized title; "" if it has none
	Site     index.Site      `json:"site"`
	Place    workspace.Place `json:"place"`
	Line     int             `json:"line"`
	Step     int             `json:"step"` // its distance from the root, negative for a firer
}

// An Edge is one firing the graph draws, from a firer to what it fires, with the delay as written and the scripted
// effects that were spliced out of the line.
type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Days string   `json:"days,omitempty"` // the delay as written: days, months or years
	Via  []string `json:"via,omitempty"`  // the scripted effects passed through, when they are not drawn as nodes
}

// A Filter says what the graph shows: which databases and origins are drawn, how deep and how many nodes, and
// whether a firing through a scripted effect is followed.
type Filter struct {
	Databases []string `json:"databases"`
	Origins   []string `json:"origins"`
	Depth     int      `json:"depth"`
	Limit     int      `json:"limit"`
	Follow    bool     `json:"follow"`
}

// A Graph is the event graph from one root. It has no coordinates; the page lays it out.
type Graph struct {
	Root      string `json:"root"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"` // the limit cut it
}

// BuildGraph builds the graph from a root node under a filter. It goes breadth-first forward over what the root
// fires and backward over what fires it, to the filter's depth and through scripted effects when they are
// followed, and stops at the node limit. A node the filter leaves out is spliced out: its incoming and outgoing
// edges become one edge with the node's name added to Via.
func (gph *Service) BuildGraph(root string, filter Filter) (Graph, error) { panic("stub") }

// FindRoots returns the nodes a query names, up to the given limit: an exact id, a namespace as a prefix, or a
// number within every namespace.
func (gph *Service) FindRoots(query string, limit int) []Node { panic("stub") }

// Defaults returns the filter the page opens with: events, on_actions, decisions and scripted effects, every mod,
// three steps deep, two hundred nodes, scripted effects followed.
func (gph *Service) Defaults() Filter { panic("stub") }
