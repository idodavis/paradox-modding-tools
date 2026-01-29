package inventory_services

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tool/internal/parser"
)

// maxProcsSet tracks whether we've already limited GOMAXPROCS
var maxProcsSet bool

// DefaultWorkerCount returns the number of worker goroutines for parallel processing.
// Limits GOMAXPROCS to ~50% of CPU cores to keep system responsive.
func DefaultWorkerCount() int {
	cpus := runtime.NumCPU()
	workers := (cpus + 1) / 2 // ~50% of cores, rounded up
	if workers < 2 {
		workers = 2
	}

	// Limit actual OS threads used by Go runtime (only set once)
	if !maxProcsSet {
		runtime.GOMAXPROCS(workers)
		maxProcsSet = true
	}

	return workers
}

// ObjectExtractor handles extraction of game objects from parsed files
type ObjectExtractor struct {
	config  *InventoryConfig
	baseDir string
}

// NewObjectExtractor creates a new extractor with the given configuration
func NewObjectExtractor(config *InventoryConfig, baseDir string) *ObjectExtractor {
	return &ObjectExtractor{
		config:  config,
		baseDir: baseDir,
	}
}

// ExtractFromDirectory extracts all objects of the given type from configured directories
func (e *ObjectExtractor) ExtractFromDirectory(objectType string) (*InventoryResult, error) {
	typeConfig, exists := e.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	result := &InventoryResult{
		Type:  objectType,
		Items: []InventoryItem{},
	}

	// Process main paths with the primary key pattern
	for _, relPath := range typeConfig.Paths {
		items, errors := e.extractFromPath(relPath, objectType, false)
		result.Items = append(result.Items, items...)
		result.Errors = append(result.Errors, errors...)
	}

	// Process inline paths with the inline key pattern (if configured)
	if len(typeConfig.InlinePaths) > 0 && typeConfig.InlineKeyPattern != "" {
		for _, relPath := range typeConfig.InlinePaths {
			items, errors := e.extractFromPath(relPath, objectType, true)
			result.Items = append(result.Items, items...)
			result.Errors = append(result.Errors, errors...)
		}
	}

	result.TotalCount = len(result.Items)
	return result, nil
}

// fileJob represents a file to be processed
type fileJob struct {
	path             string
	objectType       string
	useInlinePattern bool
}

// fileResult represents the result of processing a file
type fileResult struct {
	items  []InventoryItem
	errors []string
}

// extractFromPath extracts objects from a directory path using parallel processing
func (e *ObjectExtractor) extractFromPath(relPath string, objectType string, useInlinePattern bool) ([]InventoryItem, []string) {
	dirPath := filepath.Join(e.baseDir, relPath)

	// Check if directory exists
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return nil, []string{fmt.Sprintf("directory not found: %s", relPath)}
	}

	// Collect all .txt files first
	var filePaths []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".txt") {
			filePaths = append(filePaths, path)
		}
		return nil
	})

	if err != nil {
		return nil, []string{fmt.Sprintf("error walking directory %s: %v", relPath, err)}
	}

	if len(filePaths) == 0 {
		return []InventoryItem{}, nil
	}

	// Process files in parallel using worker pool
	numWorkers := DefaultWorkerCount()
	if len(filePaths) < numWorkers {
		numWorkers = len(filePaths)
	}

	jobs := make(chan fileJob, len(filePaths))
	results := make(chan fileResult, len(filePaths))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				var fileItems []InventoryItem
				var extractErr error

				if job.useInlinePattern {
					fileItems, extractErr = e.ExtractFromFileWithInlinePattern(job.path, job.objectType)
				} else {
					fileItems, extractErr = e.ExtractFromFile(job.path, job.objectType)
				}

				result := fileResult{items: fileItems}
				if extractErr != nil {
					result.errors = []string{fmt.Sprintf("error parsing %s: %v", job.path, extractErr)}
				}
				results <- result
			}
		}()
	}

	// Send jobs
	for _, path := range filePaths {
		jobs <- fileJob{
			path:             path,
			objectType:       objectType,
			useInlinePattern: useInlinePattern,
		}
	}
	close(jobs)

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var allItems []InventoryItem
	var allErrors []string
	for result := range results {
		allItems = append(allItems, result.items...)
		allErrors = append(allErrors, result.errors...)
	}

	return allItems, allErrors
}

// ExtractFromFile extracts objects of the given type from a single file
func (e *ObjectExtractor) ExtractFromFile(filePath string, objectType string) ([]InventoryItem, error) {
	typeConfig, exists := e.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	// Parse the file
	parsed, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Calculate relative path for metadata
	relPath, err := filepath.Rel(e.baseDir, filePath)
	if err != nil {
		relPath = filePath
	}

	items := []InventoryItem{}

	// Process each entry in the file
	for _, entry := range parsed.Entries {
		// Skip non-expression entries (comments, whitespace, namespaces)
		if entry.Expression == nil {
			continue
		}

		expr := entry.Expression
		key := expr.Key

		// Check if this key matches the expected pattern
		if !e.matchesKeyPattern(key, typeConfig) {
			continue
		}

		// For keyword_prefixed patterns, extract the actual name
		displayKey := key
		if typeConfig.KeyPattern == "keyword_prefixed" {
			displayKey = e.extractKeywordPrefixedName(key, typeConfig.KeyKeywords)
		}

		// Calculate line end by counting newlines in raw text
		rawText := expr.GetRawText()
		lineStart := expr.Pos.Line
		lineEnd := lineStart + strings.Count(rawText, "\n")

		item := InventoryItem{
			Key:       displayKey,
			Type:      objectType,
			FilePath:  relPath,
			LineStart: lineStart,
			LineEnd:   lineEnd,
			RawText:   rawText,
		}

		items = append(items, item)
	}

	return items, nil
}

// ExtractFromFileWithInlinePattern extracts objects using the inline key pattern
func (e *ObjectExtractor) ExtractFromFileWithInlinePattern(filePath string, objectType string) ([]InventoryItem, error) {
	typeConfig, exists := e.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	if typeConfig.InlineKeyPattern == "" {
		return nil, fmt.Errorf("object type %s has no inline key pattern", objectType)
	}

	// Parse the file
	parsed, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Calculate relative path for metadata
	relPath, err := filepath.Rel(e.baseDir, filePath)
	if err != nil {
		relPath = filePath
	}

	items := []InventoryItem{}

	// Process each entry in the file
	for _, entry := range parsed.Entries {
		if entry.Expression == nil {
			continue
		}

		expr := entry.Expression
		key := expr.Key

		// Check if this key matches the inline pattern
		if !e.matchesInlineKeyPattern(key, typeConfig) {
			continue
		}

		// Extract the actual name from keyword-prefixed keys
		displayKey := key
		if typeConfig.InlineKeyPattern == "keyword_prefixed" {
			displayKey = e.extractKeywordPrefixedName(key, typeConfig.InlineKeyKeywords)
		}

		rawText := expr.GetRawText()
		lineStart := expr.Pos.Line
		lineEnd := lineStart + strings.Count(rawText, "\n")

		item := InventoryItem{
			Key:       displayKey,
			Type:      objectType,
			FilePath:  relPath,
			LineStart: lineStart,
			LineEnd:   lineEnd,
			RawText:   rawText,
		}

		items = append(items, item)
	}

	return items, nil
}

// matchesInlineKeyPattern checks if a key matches the inline pattern
func (e *ObjectExtractor) matchesInlineKeyPattern(key string, typeConfig *ObjectTypeConfig) bool {
	switch typeConfig.InlineKeyPattern {
	case "keyword_prefixed":
		return e.hasKeywordPrefix(key, typeConfig.InlineKeyKeywords)
	case "numeric":
		return e.isNumericKey(key)
	case "prefixed":
		return e.hasPrefixedKey(key, typeConfig.KeyPrefixes)
	case "namespaced":
		return e.isNamespacedKey(key)
	case "any":
		return true
	default:
		return false
	}
}

// ExtractFromFiles extracts objects from a specific list of files using parallel processing
func (e *ObjectExtractor) ExtractFromFiles(files []string, objectType string) (*InventoryResult, error) {
	_, exists := e.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	if len(files) == 0 {
		return &InventoryResult{Type: objectType, Items: []InventoryItem{}}, nil
	}

	// Process files in parallel
	numWorkers := DefaultWorkerCount()
	if len(files) < numWorkers {
		numWorkers = len(files)
	}

	jobs := make(chan string, len(files))
	results := make(chan fileResult, len(files))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range jobs {
				fullPath := filePath
				if !filepath.IsAbs(filePath) {
					fullPath = filepath.Join(e.baseDir, filePath)
				}

				items, err := e.ExtractFromFile(fullPath, objectType)
				result := fileResult{items: items}
				if err != nil {
					result.errors = []string{fmt.Sprintf("error parsing %s: %v", filePath, err)}
				}
				results <- result
			}
		}()
	}

	// Send jobs
	for _, file := range files {
		jobs <- file
	}
	close(jobs)

	// Wait and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	result := &InventoryResult{
		Type:  objectType,
		Items: []InventoryItem{},
	}

	for res := range results {
		result.Items = append(result.Items, res.items...)
		result.Errors = append(result.Errors, res.errors...)
	}

	result.TotalCount = len(result.Items)
	return result, nil
}

// matchesKeyPattern checks if a key matches the expected pattern for an object type
func (e *ObjectExtractor) matchesKeyPattern(key string, typeConfig *ObjectTypeConfig) bool {
	switch typeConfig.KeyPattern {
	case "numeric":
		return e.isNumericKey(key)
	case "prefixed":
		return e.hasPrefixedKey(key, typeConfig.KeyPrefixes)
	case "namespaced":
		return e.isNamespacedKey(key)
	case "keyword_prefixed":
		return e.hasKeywordPrefix(key, typeConfig.KeyKeywords)
	case "any":
		return true
	default:
		return false
	}
}

// hasKeywordPrefix checks if the key starts with any of the allowed keywords
// Note: The parser concatenates "keyword name" into "keywordname" (no space)
func (e *ObjectExtractor) hasKeywordPrefix(key string, keywords []string) bool {
	for _, keyword := range keywords {
		// Check both with and without space (parser may or may not include space)
		if strings.HasPrefix(key, keyword+" ") || strings.HasPrefix(key, keyword) && len(key) > len(keyword) {
			// Make sure it's not just a partial match (key must be longer than keyword)
			if len(key) > len(keyword) {
				return true
			}
		}
	}
	return false
}

// extractKeywordPrefixedName extracts the actual name from a keyword-prefixed key
// Handles both "scripted_trigger my_trigger" and "scripted_triggermy_trigger" formats
func (e *ObjectExtractor) extractKeywordPrefixedName(key string, keywords []string) string {
	for _, keyword := range keywords {
		// Try with space first
		prefixWithSpace := keyword + " "
		if strings.HasPrefix(key, prefixWithSpace) {
			return strings.TrimPrefix(key, prefixWithSpace)
		}
		// Then without space (parser concatenates)
		if strings.HasPrefix(key, keyword) && len(key) > len(keyword) {
			return strings.TrimPrefix(key, keyword)
		}
	}
	return key
}

// isNumericKey checks if the key is a numeric identifier
func (e *ObjectExtractor) isNumericKey(key string) bool {
	_, err := strconv.ParseInt(key, 10, 64)
	return err == nil
}

// hasPrefixedKey checks if the key starts with any of the allowed prefixes
func (e *ObjectExtractor) hasPrefixedKey(key string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// isNamespacedKey checks if the key follows the namespace.id pattern (e.g., "feast_activity.0001")
func (e *ObjectExtractor) isNamespacedKey(key string) bool {
	// Pattern: word characters, then a dot, then more characters (can include numbers and dots)
	pattern := regexp.MustCompile(`^[\w]+\.[\w.]+$`)
	return pattern.MatchString(key)
}

// GetAllKeys extracts just the keys from an inventory result (useful for link tracking)
func GetAllKeys(result *InventoryResult) []string {
	keys := make([]string, len(result.Items))
	for i, item := range result.Items {
		keys[i] = item.Key
	}
	return keys
}

// BuildKeyIndex creates a map for fast key lookup
func BuildKeyIndex(result *InventoryResult) map[string]*InventoryItem {
	index := make(map[string]*InventoryItem, len(result.Items))
	for i := range result.Items {
		index[result.Items[i].Key] = &result.Items[i]
	}
	return index
}
