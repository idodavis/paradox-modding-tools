package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// FileService provides file selection and collection operations
type FileService struct{}

// FileMatch represents a matched file pair
type FileMatch struct {
	FileAPath string `json:"fileAPath"`
	FileBPath string `json:"fileBPath"`
}

// SelectDirectory opens a directory selection dialog
func (f *FileService) SelectDirectory(title string) (string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)
	return dialog.PromptForSingleSelection()
}

// SelectMultipleFiles opens a file selection dialog allowing multiple file selection
func (f *FileService) SelectMultipleFiles(title, filter string) ([]string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseFiles(true)
	dialog.CanChooseDirectories(false)
	if filter != "" {
		dialog.AddFilter(filter, filter)
	}
	return dialog.PromptForMultipleSelection()
}

// CollectFilesFromPaths collects all .txt files from a mix of files and directories
// Returns a map of relativePath -> fullPath
// For individual files, uses the filename as the relative path
// For directories, uses the relative path from the directory root
func (f *FileService) CollectFilesFromPaths(paths []string) (map[string]string, error) {
	files := make(map[string]string)

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("error accessing path %s: %w", path, err)
		}

		if info.IsDir() {
			// Walk directory and collect all .txt files
			err := filepath.Walk(path, func(filePath string, fileInfo os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fileInfo.IsDir() {
					return nil
				}
				if strings.HasSuffix(strings.ToLower(filePath), ".txt") {
					relPath, err := filepath.Rel(path, filePath)
					if err != nil {
						return err
					}
					// Use directory path + relative path as key to handle multiple directories
					key := filepath.Join(filepath.Base(path), relPath)
					files[key] = filePath
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("error walking directory %s: %w", path, err)
			}
		} else {
			// Individual file - use filename as key
			if strings.HasSuffix(strings.ToLower(path), ".txt") {
				key := filepath.Base(path)
				// If file already exists, append parent dir to make unique
				if _, exists := files[key]; exists {
					parentDir := filepath.Base(filepath.Dir(path))
					key = filepath.Join(parentDir, key)
				}
				files[key] = path
			}
		}
	}

	return files, nil
}

// FindMatchingFiles finds files that exist in both sets by matching relative paths
// Returns a map of relativePath -> FileMatch
func (f *FileService) FindMatchingFiles(filesA, filesB map[string]string) (map[string]FileMatch, error) {
	matches := make(map[string]FileMatch)
	matchedA := make(map[string]bool)
	matchedB := make(map[string]bool)

	// First, try to match files by their relative path keys (exact match)
	for keyA, pathA := range filesA {
		if pathB, exists := filesB[keyA]; exists {
			matches[keyA] = FileMatch{
				FileAPath: pathA,
				FileBPath: pathB,
			}
			matchedA[keyA] = true
			matchedB[keyA] = true
		}
	}

	// Then, try matching by relative path structure (e.g., "events/file.txt")
	// This handles cases where files are in the same subdirectory structure but different roots
	for keyA, pathA := range filesA {
		if matchedA[keyA] {
			continue
		}

		// Extract the relative path structure (everything after the first directory component)
		partsA := strings.Split(keyA, string(filepath.Separator))
		if len(partsA) > 1 {
			relStructA := strings.Join(partsA[1:], string(filepath.Separator))

			for keyB, pathB := range filesB {
				if matchedB[keyB] {
					continue
				}

				partsB := strings.Split(keyB, string(filepath.Separator))
				if len(partsB) > 1 {
					relStructB := strings.Join(partsB[1:], string(filepath.Separator))
					if relStructA == relStructB {
						// Use the more descriptive key
						matchKey := keyA
						if len(keyB) > len(keyA) {
							matchKey = keyB
						}
						matches[matchKey] = FileMatch{
							FileAPath: pathA,
							FileBPath: pathB,
						}
						matchedA[keyA] = true
						matchedB[keyB] = true
						break
					}
				}
			}
		}
	}

	// Finally, try matching by just the filename (for cases where directory structure differs)
	for keyA, pathA := range filesA {
		if matchedA[keyA] {
			continue
		}

		filenameA := filepath.Base(pathA)
		for keyB, pathB := range filesB {
			if matchedB[keyB] {
				continue
			}

			filenameB := filepath.Base(pathB)
			if filenameA == filenameB {
				// Use the more descriptive key
				matchKey := keyA
				if len(keyB) > len(keyA) {
					matchKey = keyB
				}
				matches[matchKey] = FileMatch{
					FileAPath: pathA,
					FileBPath: pathB,
				}
				matchedA[keyA] = true
				matchedB[keyB] = true
				break
			}
		}
	}

	return matches, nil
}
