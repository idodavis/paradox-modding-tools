// graph_detail.go is one definition opened: where it is, each key at the top of its block, its options, what it
// fires and what fires it, and what its block names.

package pages

import "paradox-modding-tools/services/internal/inventory"

// Cited is one name a block writes, and where.
type Cited struct {
	Name string         `json:"name"`
	Line int            `json:"line"`
	Col  int            `json:"col"`
	At   inventory.File `json:"at"`
}
