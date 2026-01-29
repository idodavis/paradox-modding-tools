package inventory_services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"paradox-modding-tool/internal/parser"
)

// LinkTracker finds references between game objects using AST analysis
type LinkTracker struct {
	config    *InventoryConfig
	baseDir   string
	extractor *ObjectExtractor
}

// NewLinkTracker creates a new link tracker
func NewLinkTracker(config *InventoryConfig, baseDir string) *LinkTracker {
	return &LinkTracker{
		config:    config,
		baseDir:   baseDir,
		extractor: NewObjectExtractor(config, baseDir),
	}
}

// linkSearchJob represents a file to search for links
type linkSearchJob struct {
	filePath    string
	relatedType string
}

// linkSearchResult represents the result of searching a file for links
type linkSearchResult struct {
	links map[string][]ObjectLink
}

// FindLinksForItems finds all references to the given items in related type directories using parallel processing
func (lt *LinkTracker) FindLinksForItems(items []InventoryItem, objectType string) (map[string][]ObjectLink, error) {
	typeConfig, exists := lt.config.GetObjectTypeConfig(objectType)
	if !exists {
		return nil, fmt.Errorf("unknown object type: %s", objectType)
	}

	// Build a set of keys to search for
	keySet := make(map[string]bool, len(items))
	for _, item := range items {
		keySet[item.Key] = true
	}

	// Result map: item key -> links found
	linkMap := make(map[string][]ObjectLink)
	for _, item := range items {
		linkMap[item.Key] = []ObjectLink{}
	}

	// Collect all files to search
	var filesToSearch []linkSearchJob
	for _, relatedType := range typeConfig.RelatedTypes {
		relatedConfig, exists := lt.config.GetObjectTypeConfig(relatedType)
		if !exists {
			continue
		}

		for _, relPath := range relatedConfig.Paths {
			dirPath := filepath.Join(lt.baseDir, relPath)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				continue
			}

			filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if strings.HasSuffix(strings.ToLower(path), ".txt") {
					filesToSearch = append(filesToSearch, linkSearchJob{
						filePath:    path,
						relatedType: relatedType,
					})
				}
				return nil
			})
		}
	}

	if len(filesToSearch) == 0 {
		return linkMap, nil
	}

	// Process files in parallel
	numWorkers := DefaultWorkerCount()
	if len(filesToSearch) < numWorkers {
		numWorkers = len(filesToSearch)
	}

	jobs := make(chan linkSearchJob, len(filesToSearch))
	results := make(chan linkSearchResult, len(filesToSearch))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				links, err := lt.searchFileForReferences(job.filePath, keySet, job.relatedType)
				if err == nil && len(links) > 0 {
					results <- linkSearchResult{links: links}
				} else {
					results <- linkSearchResult{links: nil}
				}
			}
		}()
	}

	// Send jobs
	for _, job := range filesToSearch {
		jobs <- job
	}
	close(jobs)

	// Wait and close results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results (need mutex since we're merging maps)
	var mu sync.Mutex
	for res := range results {
		if res.links != nil {
			mu.Lock()
			for key, keyLinks := range res.links {
				linkMap[key] = append(linkMap[key], keyLinks...)
			}
			mu.Unlock()
		}
	}

	return linkMap, nil
}

// searchFileForReferences parses a file and searches for references to any of the target keys
func (lt *LinkTracker) searchFileForReferences(filePath string, targetKeys map[string]bool, sourceType string) (map[string][]ObjectLink, error) {
	parsed, err := parser.ParseFile(filePath)
	if err != nil {
		return nil, err
	}

	relPath, err := filepath.Rel(lt.baseDir, filePath)
	if err != nil {
		relPath = filePath
	}

	links := make(map[string][]ObjectLink)

	// Walk through all entries
	for _, entry := range parsed.Entries {
		if entry.Expression == nil {
			continue
		}

		// Get the top-level object key (e.g., title key like "k_england")
		topKey := entry.Expression.Key

		// Search within this expression for references
		lt.searchExpressionForReferences(entry.Expression, topKey, relPath, sourceType, targetKeys, links)
	}

	return links, nil
}

// searchExpressionForReferences recursively searches an expression for references to target keys
func (lt *LinkTracker) searchExpressionForReferences(expr *parser.Expression, contextKey string, filePath string, sourceType string, targetKeys map[string]bool, links map[string][]ObjectLink) {
	if expr == nil {
		return
	}

	// Check if the expression's value references a target key
	if expr.Literal != nil {
		lt.checkLiteralForReference(expr.Literal, expr.Key, contextKey, filePath, sourceType, expr.Pos.Line, targetKeys, links)
	}

	// If the expression has an object, recurse into it
	if expr.Object != nil {
		lt.searchObjectForReferences(expr.Object, contextKey, filePath, sourceType, targetKeys, links)
	}
}

// searchObjectForReferences recursively searches an object for references
func (lt *LinkTracker) searchObjectForReferences(obj *parser.Object, contextKey string, filePath string, sourceType string, targetKeys map[string]bool, links map[string][]ObjectLink) {
	if obj == nil {
		return
	}

	for _, entry := range obj.Entries {
		if entry.Expression != nil {
			lt.searchExpressionForReferences(entry.Expression, contextKey, filePath, sourceType, targetKeys, links)
		}
		if entry.Literal != nil {
			lt.checkLiteralForReference(entry.Literal, "", contextKey, filePath, sourceType, entry.Pos.Line, targetKeys, links)
		}
		if entry.Object != nil {
			lt.searchObjectForReferences(entry.Object, contextKey, filePath, sourceType, targetKeys, links)
		}
	}
}

// checkLiteralForReference checks if a literal value references any target key
func (lt *LinkTracker) checkLiteralForReference(lit *parser.Literal, propertyName string, contextKey string, filePath string, sourceType string, line int, targetKeys map[string]bool, links map[string][]ObjectLink) {
	if lit == nil {
		return
	}

	// Check identifier
	if lit.Identifier != nil {
		if targetKeys[*lit.Identifier] {
			lt.addLink(links, *lit.Identifier, contextKey, sourceType, propertyName, filePath, line)
		}
	}

	// Check string value (strip quotes)
	if lit.String != nil {
		strVal := strings.Trim(*lit.String, "\"")
		if targetKeys[strVal] {
			lt.addLink(links, strVal, contextKey, sourceType, propertyName, filePath, line)
		}
	}

	// Check number (for character IDs)
	if lit.Number != nil {
		numStr := fmt.Sprintf("%d", int64(*lit.Number))
		if targetKeys[numStr] {
			lt.addLink(links, numStr, contextKey, sourceType, propertyName, filePath, line)
		}
		// Also try the float representation in case of decimals
		numStrFloat := fmt.Sprintf("%v", *lit.Number)
		if numStrFloat != numStr && targetKeys[numStrFloat] {
			lt.addLink(links, numStrFloat, contextKey, sourceType, propertyName, filePath, line)
		}
	}

	// Check array elements
	if lit.Array != nil {
		for _, elem := range lit.Array {
			lt.checkLiteralForReference(elem, propertyName, contextKey, filePath, sourceType, line, targetKeys, links)
		}
	}
}

// addLink adds a link to the links map
func (lt *LinkTracker) addLink(links map[string][]ObjectLink, targetKey string, contextKey string, sourceType string, propertyName string, filePath string, line int) {
	context := propertyName
	if context == "" {
		context = "value"
	}

	link := ObjectLink{
		TargetKey:  contextKey,
		TargetType: sourceType,
		Context:    context,
		SourceFile: filePath,
		SourceLine: line,
	}

	links[targetKey] = append(links[targetKey], link)
}

// EnrichWithLinks adds link information to inventory items
func (lt *LinkTracker) EnrichWithLinks(result *InventoryResult) error {
	if len(result.Items) == 0 {
		return nil
	}

	linkMap, err := lt.FindLinksForItems(result.Items, result.Type)
	if err != nil {
		return err
	}

	// Add links to each item
	for i := range result.Items {
		if links, exists := linkMap[result.Items[i].Key]; exists {
			result.Items[i].Links = links
		}
	}

	return nil
}
