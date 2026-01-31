package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func getTestBaseDir() string {
	// Find the test-files directory relative to the test file
	// Go up from internal/inventory to paradox-modding-tool, then into test-files
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "test-files")
}

// Helper to collect files from a directory
func collectFilesFromDir(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".txt") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
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

func TestGetConfigForGame(t *testing.T) {
	config, err := GetConfigForGame("ck3")
	if err != nil {
		t.Fatalf("Failed to get CK3 config: %v", err)
	}

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

func TestUnsupportedGame(t *testing.T) {
	_, err := GetConfigForGame("unsupported_game")
	if err == nil {
		t.Error("Expected error for unsupported game")
	}
}

func TestExtractFromFiles(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	charDir := filepath.Join(baseDir, "history", "characters")
	if _, err := os.Stat(charDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", charDir)
	}

	// Collect character files
	files, err := collectFilesFromDir(charDir)
	if err != nil {
		t.Fatalf("Failed to collect files: %v", err)
	}

	if len(files) == 0 {
		t.Skip("No character files found")
	}

	result, err := ExtractFromFiles("ck3", files, "characters")
	if err != nil {
		t.Fatalf("Failed to extract characters: %v", err)
	}

	t.Logf("Found %d characters from %d files", result.TotalCount, len(files))

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
		if item.LineStart <= 0 {
			t.Errorf("Item %d has invalid line start: %d", i, item.LineStart)
		}
		// Check RawText is populated
		if item.RawText == "" {
			t.Errorf("Item %d has empty RawText", i)
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

	files, err := collectFilesFromDir(traitsDir)
	if err != nil || len(files) == 0 {
		t.Skip("No trait files found")
	}

	result, err := ExtractFromFiles("ck3", files, "traits")
	if err != nil {
		t.Fatalf("Failed to get traits inventory: %v", err)
	}

	t.Logf("Found %d traits", result.TotalCount)

	// Check for some expected traits
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

	files, err := collectFilesFromDir(eventsDir)
	if err != nil || len(files) == 0 {
		t.Skip("No event files found")
	}

	result, err := ExtractFromFiles("ck3", files, "events")
	if err != nil {
		t.Fatalf("Failed to get events inventory: %v", err)
	}

	t.Logf("Found %d events", result.TotalCount)

	// Log some examples
	for i, item := range result.Items {
		if i >= 10 {
			break
		}
		t.Logf("Event: %s (file: %s, line: %d)", item.Key, item.FilePath, item.LineStart)
	}
}

func TestKeyPatternMatching(t *testing.T) {
	config, err := GetConfigForGame("ck3")
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

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

	// Test extracting name from keyword-prefixed key
	name := extractor.extractKeywordPrefixedName("scripted_triggermy_trigger", keywords)
	if name != "my_trigger" {
		t.Errorf("Expected 'my_trigger', got '%s'", name)
	}

	nameWithSpace := extractor.extractKeywordPrefixedName("scripted_trigger my_trigger", keywords)
	if nameWithSpace != "my_trigger" {
		t.Errorf("Expected 'my_trigger', got '%s'", nameWithSpace)
	}
}

func TestExtractScriptedTriggers(t *testing.T) {
	baseDir := getTestBaseDir()

	triggersDir := filepath.Join(baseDir, "common", "scripted_triggers")
	if _, err := os.Stat(triggersDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", triggersDir)
	}

	// Top-level only: collect files from scripted_triggers path
	files, _ := collectFilesFromDir(triggersDir)
	if len(files) == 0 {
		t.Skip("No trigger files found")
	}

	result, err := ExtractFromFiles("ck3", files, "scripted_triggers")
	if err != nil {
		t.Fatalf("Failed to get scripted_triggers inventory: %v", err)
	}

	t.Logf("Found %d scripted triggers (top-level only)", result.TotalCount)
	for i, item := range result.Items {
		if i >= 5 {
			break
		}
		t.Logf("Trigger: %s (file: %s, line: %d)", item.Key, item.FilePath, item.LineStart)
	}
}

func TestExtractWithReferences(t *testing.T) {
	baseDir := getTestBaseDir()

	// Check if test data exists
	charDir := filepath.Join(baseDir, "history", "characters")
	titlesDir := filepath.Join(baseDir, "history", "titles")
	if _, err := os.Stat(charDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", charDir)
	}
	if _, err := os.Stat(titlesDir); os.IsNotExist(err) {
		t.Skip("Test data directory not found:", titlesDir)
	}

	// Collect all files
	charFiles, _ := collectFilesFromDir(charDir)
	titleFiles, _ := collectFilesFromDir(titlesDir)
	allFiles := append(charFiles, titleFiles...)

	if len(allFiles) == 0 {
		t.Skip("No test files found")
	}

	// Extract both types with references in one call
	result, err := ExtractInventory("ck3", allFiles, []string{"characters", "titles"})
	if err != nil {
		t.Fatalf("Failed to extract inventory: %v", err)
	}

	inventories := result.Inventory
	charResult := inventories["characters"]
	titleResult := inventories["titles"]

	t.Logf("Extracted %d characters and %d titles", charResult.TotalCount, titleResult.TotalCount)

	// Count references on characters (titles that reference them)
	totalRefs := 0
	keysWithRefs := 0
	for _, item := range charResult.Items {
		if len(item.References) > 0 {
			keysWithRefs++
			totalRefs += len(item.References)
			if keysWithRefs <= 3 {
				t.Logf("Character %s has %d references", item.Key, len(item.References))
			}
		}
	}

	t.Logf("Found %d references to %d character keys", totalRefs, keysWithRefs)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

