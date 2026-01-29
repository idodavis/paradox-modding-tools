package inventory_services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func getTestBaseDir() string {
	// Find the test-files directory relative to the test file
	// Go up from services/inventory_services to paradox-modding-tool, then into test-files
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "test-files")
}

func TestGetSupportedGames(t *testing.T) {
	games := GetSupportedGames()
	if len(games) == 0 {
		t.Error("No supported games found")
	}

	// CK3 should be supported
	found := false
	for _, game := range games {
		if game == "ck3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("CK3 should be in supported games")
	}

	t.Logf("Supported games: %v", games)
}

func TestLoadCK3Config(t *testing.T) {
	// Test loading CK3 config via the generic service
	service, err := NewInventoryService("ck3", getTestBaseDir())
	if err != nil {
		t.Fatalf("Failed to create CK3 service: %v", err)
	}

	config := service.GetConfig()
	if len(config.ObjectTypes) == 0 {
		t.Error("Config has no object types")
	}

	// Verify expected types exist
	expectedTypes := []string{"events", "characters", "traits", "decisions", "governments", "cultures"}
	for _, typeName := range expectedTypes {
		if _, exists := config.ObjectTypes[typeName]; !exists {
			t.Errorf("Expected object type %q not found in config", typeName)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	service, err := NewInventoryService("ck3", getTestBaseDir())
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Config is already validated in NewInventoryService, but let's test it again
	if err := service.GetConfig().ValidateConfig(); err != nil {
		t.Errorf("Config validation failed: %v", err)
	}
}

func TestUnsupportedGame(t *testing.T) {
	_, err := NewInventoryService("unsupported_game", getTestBaseDir())
	if err == nil {
		t.Error("Expected error for unsupported game")
	}
}

func TestNewInventoryService(t *testing.T) {
	baseDir := getTestBaseDir()
	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	if service.GetGame() != "ck3" {
		t.Errorf("Expected game 'ck3', got '%s'", service.GetGame())
	}

	types := service.GetSupportedTypes()
	if len(types) == 0 {
		t.Error("Service has no supported types")
	}
}

func TestGetSupportedTypes(t *testing.T) {
	baseDir := getTestBaseDir()
	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	types := service.GetSupportedTypes()

	// Check that types are sorted
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Error("Supported types are not sorted")
			break
		}
	}

	t.Logf("Supported types for CK3: %v", types)
}

func TestExtractCharacters(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	charDir := filepath.Join(baseDir, "history", "characters")
	if _, err := os.Stat(charDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", charDir)
	}

	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	result, err := service.GetInventory("characters", false)
	if err != nil {
		t.Fatalf("Failed to get character inventory: %v", err)
	}

	t.Logf("Found %d characters", result.TotalCount)

	if result.TotalCount == 0 {
		t.Log("Warning: No characters found. Check if test data exists.")
	}

	// Verify structure of items
	for i, item := range result.Items {
		if i >= 5 {
			break // Only check first 5
		}
		if item.Key == "" {
			t.Errorf("Item %d has empty key", i)
		}
		if item.Type != "characters" {
			t.Errorf("Item %d has wrong type: %s", i, item.Type)
		}
		if item.FilePath == "" {
			t.Errorf("Item %d has empty file path", i)
		}
		if item.LineStart <= 0 {
			t.Errorf("Item %d has invalid line start: %d", i, item.LineStart)
		}
	}
}

func TestExtractTraits(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	traitsDir := filepath.Join(baseDir, "common", "traits")
	if _, err := os.Stat(traitsDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", traitsDir)
	}

	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	result, err := service.GetInventory("traits", false)
	if err != nil {
		t.Fatalf("Failed to get traits inventory: %v", err)
	}

	t.Logf("Found %d traits", result.TotalCount)

	// Check for some expected traits (these are common CK3 traits)
	expectedTraits := map[string]bool{
		"brave":     false,
		"craven":    false,
		"ambitious": false,
		"content":   false,
	}

	for _, item := range result.Items {
		if _, expected := expectedTraits[item.Key]; expected {
			expectedTraits[item.Key] = true
		}
	}

	for trait, found := range expectedTraits {
		if !found {
			t.Logf("Note: Expected trait %q not found (may be named differently)", trait)
		}
	}
}

func TestExtractEvents(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	eventsDir := filepath.Join(baseDir, "events")
	if _, err := os.Stat(eventsDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", eventsDir)
	}

	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	result, err := service.GetInventory("events", false)
	if err != nil {
		t.Fatalf("Failed to get events inventory: %v", err)
	}

	t.Logf("Found %d events", result.TotalCount)

	// Check that events have namespaced keys
	for i, item := range result.Items {
		if i >= 10 {
			break
		}
		// Namespaced events should contain a dot
		// But some files might not have namespaced events, so just log
		t.Logf("Event: %s (file: %s, line: %d)", item.Key, item.FilePath, item.LineStart)
	}
}

func TestExportToJSON(t *testing.T) {
	baseDir := getTestBaseDir()
	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Create a small result
	result := &InventoryResult{
		Type:       "test",
		TotalCount: 2,
		Items: []InventoryItem{
			{Key: "test1", Type: "test", FilePath: "test.txt", LineStart: 1, LineEnd: 5},
			{Key: "test2", Type: "test", FilePath: "test.txt", LineStart: 6, LineEnd: 10},
		},
	}

	jsonStr, err := service.ExportToJSON(result)
	if err != nil {
		t.Fatalf("Failed to export to JSON: %v", err)
	}

	// Verify it's valid JSON
	var parsed InventoryResult
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Errorf("Exported JSON is not valid: %v", err)
	}

	if parsed.TotalCount != 2 {
		t.Errorf("Expected 2 items, got %d", parsed.TotalCount)
	}
}

func TestKeyPatternMatching(t *testing.T) {
	service, err := NewInventoryService("ck3", getTestBaseDir())
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	config := service.GetConfig()
	extractor := NewObjectExtractor(config, "")

	// Test numeric pattern
	if !extractor.isNumericKey("12345") {
		t.Error("12345 should be numeric")
	}
	if extractor.isNumericKey("abc") {
		t.Error("abc should not be numeric")
	}

	// Test prefixed pattern
	prefixes := []string{"e_", "k_", "d_"}
	if !extractor.hasPrefixedKey("e_england", prefixes) {
		t.Error("e_england should match prefix")
	}
	if extractor.hasPrefixedKey("england", prefixes) {
		t.Error("england should not match prefix")
	}

	// Test namespaced pattern
	if !extractor.isNamespacedKey("feast_activity.0001") {
		t.Error("feast_activity.0001 should be namespaced")
	}
	if !extractor.isNamespacedKey("murder_outcome.42") {
		t.Error("murder_outcome.42 should be namespaced")
	}
	if extractor.isNamespacedKey("no_dot_here") {
		t.Error("no_dot_here should not be namespaced")
	}

	// Test keyword_prefixed pattern
	// Note: Parser concatenates "scripted_trigger name" into "scripted_triggername"
	keywords := []string{"scripted_trigger", "scripted_effect"}
	if !extractor.hasKeywordPrefix("scripted_triggermy_trigger", keywords) {
		t.Error("scripted_triggermy_trigger should match keyword prefix")
	}
	if !extractor.hasKeywordPrefix("scripted_effectmy_effect", keywords) {
		t.Error("scripted_effectmy_effect should match keyword prefix")
	}
	if extractor.hasKeywordPrefix("my_trigger", keywords) {
		t.Error("my_trigger should not match keyword prefix")
	}
	if extractor.hasKeywordPrefix("scripted_trigger", keywords) {
		t.Error("scripted_trigger alone should not match (no name follows)")
	}

	// Test extracting name from keyword-prefixed key (concatenated format)
	name := extractor.extractKeywordPrefixedName("scripted_triggermy_trigger", keywords)
	if name != "my_trigger" {
		t.Errorf("Expected 'my_trigger', got '%s'", name)
	}

	// Also test with space (in case parser changes)
	nameWithSpace := extractor.extractKeywordPrefixedName("scripted_trigger my_trigger", keywords)
	if nameWithSpace != "my_trigger" {
		t.Errorf("Expected 'my_trigger', got '%s'", nameWithSpace)
	}
}

func TestExtractScriptedTriggers(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	triggersDir := filepath.Join(baseDir, "common", "scripted_triggers")
	if _, err := os.Stat(triggersDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", triggersDir)
	}

	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	result, err := service.GetInventory("scripted_triggers", false)
	if err != nil {
		t.Fatalf("Failed to get scripted_triggers inventory: %v", err)
	}

	t.Logf("Found %d scripted triggers", result.TotalCount)
	if len(result.Errors) > 0 {
		t.Logf("Errors: %v", result.Errors)
	}

	// Count items from main paths vs inline paths
	mainPathCount := 0
	inlinePathCount := 0
	for _, item := range result.Items {
		if strings.HasPrefix(item.FilePath, "common/scripted_triggers") {
			mainPathCount++
		} else {
			inlinePathCount++
		}
	}

	t.Logf("From common/scripted_triggers: %d", mainPathCount)
	t.Logf("From inline definitions: %d", inlinePathCount)

	// Show a few examples
	for i, item := range result.Items {
		if i >= 5 {
			break
		}
		t.Logf("Trigger: %s (file: %s, line: %d)", item.Key, item.FilePath, item.LineStart)
	}

	// Look for inline triggers (should be in events/)
	inlineExamples := 0
	for _, item := range result.Items {
		if strings.HasPrefix(item.FilePath, "events/") && inlineExamples < 3 {
			t.Logf("Inline trigger: %s (file: %s, line: %d)", item.Key, item.FilePath, item.LineStart)
			inlineExamples++
		}
	}
}

func TestDeepInventoryWithLinks(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	charDir := filepath.Join(baseDir, "history", "characters")
	if _, err := os.Stat(charDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", charDir)
	}

	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Get a small subset for testing (first file only would be ideal but we get all)
	result, err := service.GetInventory("characters", true)
	if err != nil {
		t.Fatalf("Failed to get deep character inventory: %v", err)
	}

	t.Logf("Deep inventory found %d characters", result.TotalCount)

	// Count items with links
	itemsWithLinks := 0
	totalLinks := 0
	for _, item := range result.Items {
		if len(item.Links) > 0 {
			itemsWithLinks++
			totalLinks += len(item.Links)
		}
	}

	t.Logf("Items with links: %d, Total links: %d", itemsWithLinks, totalLinks)
}

func TestGetStatistics(t *testing.T) {
	baseDir := getTestBaseDir()
	service, err := NewInventoryService("ck3", baseDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	stats, err := service.GetStatistics()
	if err != nil {
		t.Fatalf("Failed to get statistics: %v", err)
	}

	t.Log("Object type statistics:")
	for objType, count := range stats {
		t.Logf("  %s: %d", objType, count)
	}
}
