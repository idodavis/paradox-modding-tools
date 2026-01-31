package inventory

import (
	_ "embed"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
)

// ErrExtractionCancelled is returned when extraction is cancelled via CancelExtraction.
var ErrExtractionCancelled = errors.New("extraction cancelled")

// cancelExtraction is set to 1 when the user requests cancel; checked at each type boundary.
var cancelExtraction atomic.Uint32

// Embedded game configurations
// Each game's config is embedded from its respective subfolder

//go:embed ck3/ck3_object_types.json
var ck3Config []byte

// Add more game configs here as needed:
// //go:embed eu5/eu5_object_types.json
// var eu5Config []byte

// gameConfigs maps game identifiers to their embedded configurations
var gameConfigs = map[string][]byte{
	"ck3": ck3Config,
	// "eu5": eu5Config,
}

// GetSupportedGames returns a list of all supported game identifiers
func GetSupportedGames() []string {
	games := make([]string, 0, len(gameConfigs))
	for game := range gameConfigs {
		games = append(games, game)
	}
	sort.Strings(games)
	return games
}

// GetConfigForGame loads and returns the configuration for a specific game
func GetConfigForGame(game string) (*InventoryConfig, error) {
	configData, exists := gameConfigs[game]
	if !exists {
		return nil, fmt.Errorf("unsupported game: %s (supported: %v)", game, GetSupportedGames())
	}

	config, err := LoadConfigFromBytes(configData)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s config: %w", game, err)
	}

	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid %s config: %w", game, err)
	}

	return config, nil
}

// GetSupportedTypes returns a sorted list of object types available for a game
func GetSupportedTypes(game string) ([]string, error) {
	config, err := GetConfigForGame(game)
	if err != nil {
		return nil, err
	}
	return config.GetSupportedTypes(), nil
}

// ExtractFromFiles extracts objects of a single type from files (without reference resolution)
// Used internally and for testing. For the full API with references, use ExtractInventory.
func ExtractFromFiles(game string, files []string, objectType string) (*InventoryResult, error) {
	config, err := GetConfigForGame(game)
	if err != nil {
		return nil, err
	}

	extractor := NewObjectExtractor(config, "")
	return extractor.ExtractFromFiles(files, objectType)
}

// CancelExtraction requests that the current ExtractInventory run stop at the next type boundary.
// Safe to call from another goroutine (e.g. frontend).
func CancelExtraction() {
	cancelExtraction.Store(1)
}

// ExtractInventory extracts multiple object types from files with references resolved.
// The graph is built on demand in the frontend for the selected item.
// Returns ErrExtractionCancelled if CancelExtraction was called during the run.
func ExtractInventory(game string, files []string, objectTypes []string) (*ExtractResult, error) {
	cancelExtraction.Store(0)
	config, err := GetConfigForGame(game)
	if err != nil {
		return nil, err
	}

	extractor := NewObjectExtractor(config, "")
	inventories := make(map[string]*InventoryResult)

	// Extract all types
	for _, objectType := range objectTypes {
		if cancelExtraction.Load() != 0 {
			return nil, ErrExtractionCancelled
		}
		result, err := extractor.ExtractFromFiles(files, objectType)
		if err != nil {
			if errors.Is(err, ErrExtractionCancelled) {
				return nil, ErrExtractionCancelled
			}
			// Log error but continue with other types
			result = &InventoryResult{
				Type:   objectType,
				Items:  []InventoryItem{},
				Errors: []string{err.Error()},
			}
		}
		if result != nil && len(result.Items) > 0 {
			inventories[objectType] = result
		}
	}

	// Resolve references across all types
	if len(inventories) > 0 {
		EnrichAllWithReferences(inventories)
	}

	return &ExtractResult{Inventory: inventories}, nil
}
