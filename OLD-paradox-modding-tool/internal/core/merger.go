package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	parser "paradox-modding-tool/internal/interpreter"
)

// ############
// Merge: file write and content
// ############

// WriteMergedFile writes content to outputPath, creating parent directories if needed.
func WriteMergedFile(outputPath string, content string) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("error creating output directory: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		return fmt.Errorf("error writing to output file: %w", err)
	}

	return nil
}

// collectObjectsAndComments collects entries with objects and comments matching prefix into a keyed map.
// Also grabs comments if they contain the specified prefix
func collectObjectsAndComments(entries []*parser.Entry, prefix string) map[string]ModdedObject {
	objects := make(map[string]ModdedObject)
	var pendingComments []string

	for _, entry := range entries {
		// Grabbing custom comments
		// These will be attached to the next object block found
		if c := entry.Comment; c != "" {
			if prefix == "" || strings.HasPrefix(c, prefix) {
				pendingComments = append(pendingComments, c)
			}
			continue
		}

		// Grabbing objects
		if expr := entry.Expression; expr != nil {
			key := expr.Key
			if expr.Object != nil {
				comment := ""
				if len(pendingComments) > 0 {
					comment = strings.Join(pendingComments, "\n") + "\n"
					pendingComments = nil
				}
				objects[key] = ModdedObject{expr, comment}
			}
		}
	}

	return objects
}

// GetMergedContent parses both files, merges objects according to options, and returns merged string and MergeResult.
func GetMergedContent(fileAPath, fileBPath string, options MergeOptions) (string, *MergeResult, error) {
	result := &MergeResult{
		FilePath:       fileAPath,
		EntriesAdded:   []string{},
		EntriesRemoved: []string{},
		EntriesChanged: []string{},
		EntriesKept:    []string{},
	}

	// Parse both files
	fileA, err := parser.ParseFile(fileAPath)
	if err != nil {
		result.Error = fmt.Errorf("error parsing file A: %w", err)
		return "", result, result.Error
	}

	fileB, err := parser.ParseFile(fileBPath)
	if err != nil {
		result.Error = fmt.Errorf("error parsing file B: %w", err)
		return "", result, result.Error
	}

	// Collect objects from both files
	fileAObjects := collectObjectsAndComments(fileA.Entries, options.CustomCommentPrefix)
	fileBObjects := collectObjectsAndComments(fileB.Entries, options.CustomCommentPrefix)

	// Build key sets for tracking
	fileAKeys := make(map[string]struct{})
	fileBKeys := make(map[string]struct{})
	for key := range fileAObjects {
		fileAKeys[key] = struct{}{}
	}
	for key := range fileBObjects {
		fileBKeys[key] = struct{}{}
	}

	// Build key list set for quick lookup
	keyListSet := make(map[string]struct{})
	for _, key := range options.KeyList {
		keyListSet[key] = struct{}{}
	}

	// Determine base file - always use A as base, B as other
	// A's entries take precedence in conflicts (default: use A)
	baseFile := fileA
	otherObjects := fileBObjects
	baseKeys := fileAKeys
	useOtherVersion := make(map[string]bool)

	// By default, use A's version (base takes precedence)
	// Key list will override this below
	for key := range keyListSet {
		useOtherVersion[key] = true
	}

	// Track entries that need to be added (from other file, not in base)
	entriesToAdd := make(map[string]ModdedObject)
	for key, obj := range otherObjects {
		if _, existsInBase := baseKeys[key]; !existsInBase {
			if options.AddAdditionalEntries {
				entriesToAdd[key] = obj
			}
		}
	}

	// Build output
	var output strings.Builder

	// Preserve BOM from base file (fileA)
	if fileA.BOM != "" {
		output.WriteString(fileA.BOM)
	}

	// Process base file entries
	for _, entry := range baseFile.Entries {
		if expr := entry.Expression; expr != nil {
			key := expr.Key
			if expr.Object != nil {
				// Check if we should use the other file's version
				if useOtherVersion[key] {
					if otherObj, ok := otherObjects[key]; ok {
						// Write other file's comment if exists
						if otherObj.Comment != "" {
							output.WriteString(otherObj.Comment)
						}
						output.WriteString(otherObj.ObjectExpr.GetRawText())

						// This is always a change since we're replacing a base file entry
						result.EntriesChanged = append(result.EntriesChanged, key)
						continue
					}
				}
				// Keep from base file
				output.WriteString(entry.GetRawText())
				result.EntriesKept = append(result.EntriesKept, key)
				continue
			}
		}
		// Write non-object entries as-is
		output.WriteString(entry.GetRawText())
	}

	// Add entries from other file that weren't in base
	if options.AddAdditionalEntries {
		switch options.EntryPlacement {
		case "bottom":
			// Add sectional comment if we have entries to add
			if len(entriesToAdd) > 0 {
				sectionComment := ""
				if options.CustomCommentPrefix != "" {
					sectionComment = options.CustomCommentPrefix + " Additional entries from B\n"
				} else {
					sectionComment = "# Additional entries from B\n"
				}
				output.WriteString(sectionComment)
			}

			// Add all entries at the bottom
			for key, modObject := range entriesToAdd {
				if modObject.Comment != "" {
					output.WriteString(modObject.Comment)
				}
				output.WriteString(modObject.ObjectExpr.GetRawText())
				result.EntriesAdded = append(result.EntriesAdded, key)
			}
		case "preserve_order":
			// Experimental: try to preserve original order
			// Add them at the end but in the order they appeared in file B
			var orderedKeys []string
			for _, entry := range fileB.Entries {
				if expr := entry.Expression; expr != nil {
					if expr.Object != nil {
						key := expr.Key
						if _, shouldAdd := entriesToAdd[key]; shouldAdd {
							orderedKeys = append(orderedKeys, key)
						}
					}
				}
			}

			// Add entries in their original order
			for _, key := range orderedKeys {
				if modObject, ok := entriesToAdd[key]; ok {
					if modObject.Comment != "" {
						output.WriteString(modObject.Comment)
					}
					output.WriteString(modObject.ObjectExpr.GetRawText())
					result.EntriesAdded = append(result.EntriesAdded, key)
				}
			}
		}
	}

	result.FilePath = fileAPath
	return output.String(), result, nil
}
