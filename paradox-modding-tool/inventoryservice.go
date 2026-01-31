package main

import (
	"paradox-modding-tool/internal/inventory"
)

// InventoryService provides inventory functionality for Paradox game objects
// Exposed to the Wails frontend
type InventoryService struct{}

// GetSupportedGames returns a list of supported game identifiers
func (s *InventoryService) GetSupportedGames() []string {
	return inventory.GetSupportedGames()
}

// GetSupportedTypes returns object types available for a specific game
func (s *InventoryService) GetSupportedTypes(game string) ([]string, error) {
	return inventory.GetSupportedTypes(game)
}

// ExtractInventory extracts multiple object types from files with references resolved
// and precomputes the reference graph.
// Returns ExtractResult with Inventory (map of type -> InventoryResult) and Graph (nodes + links).
func (s *InventoryService) ExtractInventory(game string, files []string, objectTypes []string) (*inventory.ExtractResult, error) {
	return inventory.ExtractInventory(game, files, objectTypes)
}
