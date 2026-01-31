package inventory

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

// DefaultWorkerCount returns the number of worker goroutines for parallel file processing.
// Uses all CPU cores for faster extraction; set PMT_EXTRACT_WORKERS to override (e.g. "4").
func DefaultWorkerCount() int {
	cpus := runtime.NumCPU()
	workers := cpus - 1
	if workers < 2 {
		workers = 2
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

	// Process main paths only (top-level objects; inlines are not extracted)
	for _, relPath := range typeConfig.Paths {
		items, errors := e.extractFromPath(relPath, objectType)
		result.Items = append(result.Items, items...)
		result.Errors = append(result.Errors, errors...)
	}

	result.TotalCount = len(result.Items)
	return result, nil
}

// fileJob represents a file to be processed
type fileJob struct {
	path       string
	objectType string
}

// fileResult represents the result of processing a file
type fileResult struct {
	items  []InventoryItem
	errors []string
}

// extractFromPath extracts objects from a directory path using parallel processing (top-level only).
func (e *ObjectExtractor) extractFromPath(relPath string, objectType string) ([]InventoryItem, []string) {
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
				fileItems, extractErr := e.ExtractFromFile(job.path, job.objectType)

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
		jobs <- fileJob{path: path, objectType: objectType}
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

		// Skip keys that match another type's inline pattern so we don't misclassify (e.g. scripted_effectxxx when extracting scripted_triggers)
		if e.keyMatchesOtherTypesInlinePattern(key, objectType) {
			continue
		}

		// Check if this key matches the expected pattern
		if !e.matchesKeyPattern(key, typeConfig) {
			continue
		}

		// Extract display name: keyword_prefixed key pattern, or inline keyword_prefixed (e.g. scripted_triggerfoo -> foo)
		displayKey := key
		if typeConfig.KeyPattern == "keyword_prefixed" {
			displayKey = e.extractKeywordPrefixedName(key, typeConfig.KeyKeywords)
		} else if typeConfig.InlineKeyPattern == "keyword_prefixed" && e.matchesInlineKeyPattern(key, typeConfig) {
			displayKey = e.extractKeywordPrefixedName(key, typeConfig.InlineKeyKeywords)
		}

		// Calculate line end by counting newlines in raw text
		rawText := expr.GetRawText()
		lineStart := expr.Pos.Line
		lineEnd := lineStart + strings.Count(rawText, "\n")

		// Collect potential references from the AST
		potentialRefs := collectIdentifiersFromExpression(expr, displayKey)
		potentialRefs = deduplicateStrings(potentialRefs)

		item := InventoryItem{
			Key:           displayKey,
			Type:          objectType,
			FilePath:      relPath,
			LineStart:     lineStart,
			LineEnd:       lineEnd,
			RawText:       rawText,
			PotentialRefs: potentialRefs,
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

// keyMatchesOtherTypesInlinePattern returns true if the key matches another (not current) type's inline key pattern.
// Used in top-level extraction to skip only keys that belong to a different type (e.g. scripted_effectxxx when extracting scripted_triggers).
func (e *ObjectExtractor) keyMatchesOtherTypesInlinePattern(key string, currentObjectType string) bool {
	for typeName, typeConfig := range e.config.ObjectTypes {
		if typeName == currentObjectType || typeConfig.InlineKeyPattern == "" {
			continue
		}
		if e.matchesInlineKeyPattern(key, &typeConfig) {
			return true
		}
	}
	return false
}

// ExtractFromFiles extracts top-level objects from a specific list of files using parallel processing.
// Note: Callers should use FileService.CollectFilesFromPaths to expand directories first.
func (e *ObjectExtractor) ExtractFromFiles(files []string, objectType string) (*InventoryResult, error) {
	if _, exists := e.config.GetObjectTypeConfig(objectType); !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	if len(files) == 0 {
		return &InventoryResult{Type: objectType, Items: []InventoryItem{}}, nil
	}

	// Process files in parallel (top-level only; inlines are not extracted)
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
				allErrors := []string{}
				if err != nil {
					allErrors = append(allErrors, fmt.Sprintf("error parsing %s: %v", filePath, err))
					items = nil
				}
				if items == nil {
					items = []InventoryItem{}
				}
				results <- fileResult{items: items, errors: allErrors}
			}
		}()
	}

	// Wait and close results (must run so drain can complete on cancel)
	go func() {
		wg.Wait()
		close(results)
	}()

	// Send jobs (check cancel so we can abort during processing)
	for _, file := range files {
		if cancelExtraction.Load() != 0 {
			close(jobs)
			for range results {
			}
			return nil, ErrExtractionCancelled
		}
		jobs <- file
	}
	close(jobs)

	// Collect results and deduplicate by key (check cancel while collecting)
	seen := make(map[string]bool)
	result := &InventoryResult{
		Type:  objectType,
		Items: []InventoryItem{},
	}

	for res := range results {
		if cancelExtraction.Load() != 0 {
			for range results {
			}
			return nil, ErrExtractionCancelled
		}
		for _, item := range res.items {
			if !seen[item.Key] {
				seen[item.Key] = true
				result.Items = append(result.Items, item)
			}
		}
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
	case "identifier_no_dot":
		return e.isIdentifierWithoutDot(key)
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

// isIdentifierWithoutDot checks if the key is a simple identifier without dots
// Used for scripted_triggers/effects which are plain identifiers, not namespaced like events
func (e *ObjectExtractor) isIdentifierWithoutDot(key string) bool {
	// Must be alphanumeric with underscores, no dots
	pattern := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
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

// collectIdentifiersFromExpression extracts all identifiers from an expression's value
// This is used to find potential references to other objects
func collectIdentifiersFromExpression(expr *parser.Expression, selfKey string) []string {
	if expr == nil {
		return nil
	}

	var identifiers []string

	// Collect from literal value
	if expr.Literal != nil {
		identifiers = append(identifiers, collectIdentifiersFromLiteral(expr.Literal)...)
	}

	// Collect from nested object
	if expr.Object != nil {
		identifiers = append(identifiers, collectIdentifiersFromObject(expr.Object, selfKey)...)
	}

	return identifiers
}

// collectIdentifiersFromLiteral extracts identifiers from a literal value
func collectIdentifiersFromLiteral(lit *parser.Literal) []string {
	if lit == nil {
		return nil
	}

	var identifiers []string

	// Direct identifier reference
	if lit.Identifier != nil {
		identifiers = append(identifiers, *lit.Identifier)
	}

	// Numeric values (for character IDs and other numeric references)
	if lit.Number != nil {
		// Convert to integer string if it's a whole number
		num := *lit.Number
		if num == float64(int64(num)) {
			identifiers = append(identifiers, fmt.Sprintf("%d", int64(num)))
		}
	}

	// String values (some references are quoted)
	if lit.String != nil {
		// Remove quotes and add
		s := *lit.String
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			identifiers = append(identifiers, s[1:len(s)-1])
		}
	}

	// Array of literals
	if lit.Array != nil {
		for _, elem := range lit.Array {
			identifiers = append(identifiers, collectIdentifiersFromLiteral(elem)...)
		}
	}

	return identifiers
}

// collectIdentifiersFromObject recursively extracts identifiers from an object
func collectIdentifiersFromObject(obj *parser.Object, selfKey string) []string {
	if obj == nil {
		return nil
	}

	var identifiers []string

	for _, entry := range obj.Entries {
		if entry == nil {
			continue
		}

		// From expression
		if entry.Expression != nil {
			// The key itself might be a reference (e.g., "my_trigger = yes")
			key := entry.Expression.Key
			if key != "" && key != selfKey {
				identifiers = append(identifiers, key)
			}

			// Collect from the value
			identifiers = append(identifiers, collectIdentifiersFromExpression(entry.Expression, selfKey)...)
		}

		// From standalone literal
		if entry.Literal != nil {
			identifiers = append(identifiers, collectIdentifiersFromLiteral(entry.Literal)...)
		}

		// From nested object
		if entry.Object != nil {
			identifiers = append(identifiers, collectIdentifiersFromObject(entry.Object, selfKey)...)
		}
	}

	return identifiers
}

// deduplicateStrings removes duplicate strings while preserving order
func deduplicateStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
