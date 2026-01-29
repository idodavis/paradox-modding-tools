package inventory_services

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

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

// InventoryService provides inventory functionality for Paradox game objects
// It is generic and can work with any supported game
type InventoryService struct {
	game        string
	config      *InventoryConfig
	baseDir     string
	extractor   *ObjectExtractor
	linkTracker *LinkTracker
}

// NewInventoryService creates a new inventory service for the specified game
// game: game identifier (e.g., "ck3", "eu5")
// baseDir: path to the root of the game files
func NewInventoryService(game string, baseDir string) (*InventoryService, error) {
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

	return &InventoryService{
		game:        game,
		config:      config,
		baseDir:     baseDir,
		extractor:   NewObjectExtractor(config, baseDir),
		linkTracker: NewLinkTracker(config, baseDir),
	}, nil
}

// NewInventoryServiceWithCustomConfig creates a service with a custom configuration file
// Useful for modders who want to define their own object types
func NewInventoryServiceWithCustomConfig(game string, baseDir string, configPath string) (*InventoryService, error) {
	config, err := LoadConfigFromFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from file: %w", err)
	}

	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &InventoryService{
		game:        game,
		config:      config,
		baseDir:     baseDir,
		extractor:   NewObjectExtractor(config, baseDir),
		linkTracker: NewLinkTracker(config, baseDir),
	}, nil
}

// GetGame returns the game identifier for this service
func (s *InventoryService) GetGame() string {
	return s.game
}

// GetSupportedTypes returns a sorted list of all supported object types for this game
func (s *InventoryService) GetSupportedTypes() []string {
	types := s.config.GetSupportedTypes()
	sort.Strings(types)
	return types
}

// GetObjectTypeInfo returns detailed information about a specific object type
func (s *InventoryService) GetObjectTypeInfo(objectType string) (*ObjectTypeConfig, error) {
	config, exists := s.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}
	return config, nil
}

// GetInventory extracts all objects of the given type from configured directories
// If deep is true, also tracks references to/from other objects
func (s *InventoryService) GetInventory(objectType string, deep bool) (*InventoryResult, error) {
	result, err := s.extractor.ExtractFromDirectory(objectType)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
	}

	if deep && len(result.Items) > 0 {
		if err := s.linkTracker.EnrichWithLinks(result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("link tracking error: %v", err))
		}
	}

	return result, nil
}

// GetInventoryForFiles extracts objects from specific files
// Useful when the user has selected specific files to analyze
func (s *InventoryService) GetInventoryForFiles(files []string, objectType string, deep bool) (*InventoryResult, error) {
	result, err := s.extractor.ExtractFromFiles(files, objectType)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
	}

	if deep && len(result.Items) > 0 {
		if err := s.linkTracker.EnrichWithLinks(result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("link tracking error: %v", err))
		}
	}

	return result, nil
}

// inventoryJob represents a type to process
type inventoryJob struct {
	objectType string
	deep       bool
}

// inventoryJobResult represents the result of processing a type
type inventoryJobResult struct {
	objectType string
	result     *InventoryResult
	err        error
}

// GetInventoryMultipleTypes extracts objects of multiple types at once using parallel processing
func (s *InventoryService) GetInventoryMultipleTypes(objectTypes []string, deep bool) (map[string]*InventoryResult, error) {
	if len(objectTypes) == 0 {
		return map[string]*InventoryResult{}, nil
	}

	// Process types in parallel
	numWorkers := DefaultWorkerCount()
	if len(objectTypes) < numWorkers {
		numWorkers = len(objectTypes)
	}

	jobs := make(chan inventoryJob, len(objectTypes))
	results := make(chan inventoryJobResult, len(objectTypes))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result, err := s.GetInventory(job.objectType, job.deep)
				results <- inventoryJobResult{
					objectType: job.objectType,
					result:     result,
					err:        err,
				}
			}
		}()
	}

	// Send jobs
	for _, objType := range objectTypes {
		jobs <- inventoryJob{objectType: objType, deep: deep}
	}
	close(jobs)

	// Wait and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	resultMap := make(map[string]*InventoryResult)
	for res := range results {
		if res.err != nil {
			return nil, fmt.Errorf("failed to get inventory for %s: %w", res.objectType, res.err)
		}
		resultMap[res.objectType] = res.result
	}

	return resultMap, nil
}

// ExportToJSON converts an inventory result to a formatted JSON string
func (s *InventoryService) ExportToJSON(result *InventoryResult) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal to JSON: %w", err)
	}
	return string(data), nil
}

// ExportMultipleToJSON converts multiple inventory results to a formatted JSON string
func (s *InventoryService) ExportMultipleToJSON(results map[string]*InventoryResult) (string, error) {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal to JSON: %w", err)
	}
	return string(data), nil
}

// SearchByKey searches for an object by its key across all types or a specific type
// Returns all matching items
func (s *InventoryService) SearchByKey(key string, objectType string) ([]InventoryItem, error) {
	var typesToSearch []string

	if objectType != "" {
		typesToSearch = []string{objectType}
	} else {
		typesToSearch = s.GetSupportedTypes()
	}

	var matches []InventoryItem

	for _, objType := range typesToSearch {
		result, err := s.GetInventory(objType, false)
		if err != nil {
			continue // Skip types that fail
		}

		for _, item := range result.Items {
			if item.Key == key {
				matches = append(matches, item)
			}
		}
	}

	return matches, nil
}

// statResult represents the result of getting stats for a type
type statResult struct {
	objectType string
	count      int
}

// GetStatistics returns counts of objects by type using parallel processing
func (s *InventoryService) GetStatistics() (map[string]int, error) {
	types := s.GetSupportedTypes()
	if len(types) == 0 {
		return map[string]int{}, nil
	}

	// Process types in parallel
	numWorkers := DefaultWorkerCount()
	if len(types) < numWorkers {
		numWorkers = len(types)
	}

	jobs := make(chan string, len(types))
	results := make(chan statResult, len(types))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for objType := range jobs {
				result, err := s.GetInventory(objType, false)
				count := -1 // Indicate error
				if err == nil {
					count = result.TotalCount
				}
				results <- statResult{objectType: objType, count: count}
			}
		}()
	}

	// Send jobs
	for _, objType := range types {
		jobs <- objType
	}
	close(jobs)

	// Wait and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	stats := make(map[string]int)
	for res := range results {
		stats[res.objectType] = res.count
	}

	return stats, nil
}

// SetBaseDir updates the base directory for file scanning
func (s *InventoryService) SetBaseDir(baseDir string) {
	s.baseDir = baseDir
	s.extractor = NewObjectExtractor(s.config, baseDir)
	s.linkTracker = NewLinkTracker(s.config, baseDir)
}

// GetBaseDir returns the current base directory
func (s *InventoryService) GetBaseDir() string {
	return s.baseDir
}

// GetConfig returns the current configuration (read-only)
func (s *InventoryService) GetConfig() *InventoryConfig {
	return s.config
}
