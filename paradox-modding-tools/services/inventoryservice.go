package services

import (
	"paradox-modding-tools/services/internal/inventory"
)

// ############
// InventoryService
// ############

// InventoryService exposes inventory functionality to the Wails frontend (supported types, schema, extraction, cancel).
type InventoryService struct{}

// GetSupportedTypes returns the sorted list of object type names for the given game.
func (s *InventoryService) GetSupportedTypes(game string) ([]string, error) {
	return inventory.GetSupportedTypes(game)
}

// GetAttributes returns attribute names for an object type (from schema).
func (s *InventoryService) GetAttributes(game string, typeName string) ([]string, error) {
	return inventory.GetAttributes(game, typeName)
}

// GetFilteredSortedPage returns one page of filtered and sorted inventory items from the stored extract result.
func (s *InventoryService) GetFilteredSortedPage(filterState inventory.FilterState, sortField string, sortOrder int, first, rows int) (*inventory.FilteredSortedPage, error) {
	return inventory.FilterAndSortPage(&filterState, sortField, sortOrder, first, rows), nil
}

// ExtractInventory extracts multiple object types from files with references resolved.
// Returns items keyed by type and any parse errors. Returns inventory.ErrExtractionCancelled if the user cancelled.
func (s *InventoryService) ExtractInventory(game string, files []string, objectTypes []string) error {
	return inventory.ExtractInventory(game, files, objectTypes)
}

// CancelExtraction signals the running ExtractInventory to stop and discard results (immediate, clears in-memory data).
func (s *InventoryService) CancelExtraction() {
	inventory.CancelExtraction()
}
