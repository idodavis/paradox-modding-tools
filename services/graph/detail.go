package graph

import (
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A DetailReference is one name a definition refers to, with where that name is defined.
type DetailReference struct {
	Database string          `json:"database"`
	Name     string          `json:"name"`
	Site     index.Site      `json:"site"`
	Place    workspace.Place `json:"place"`
	Line     int             `json:"line"`
}

// A Detail is everything the detail panel shows for one node: its text, what it fires and what fires it, what it
// refers to, its portraits, its background picture, and the Form view's inputs for its database.
type Detail struct {
	Node       Node              `json:"node"`
	Text       string            `json:"text"` // the event's localized description
	Fires      []Edge            `json:"fires"`
	FiredBy    []Edge            `json:"firedBy"`
	References []DetailReference `json:"references"`
	Portraits  []string          `json:"portraits"`  // the portrait positions written, as the game's row names them
	Background string            `json:"background"` // the path of the picture found; "" if there is none
	Inputs     []schema.Input    `json:"inputs"`
}

// BuildDetail returns the detail of the node with the given id, from its kept definition.
func (gph *Service) BuildDetail(id string) (Detail, error) { panic("stub") }
