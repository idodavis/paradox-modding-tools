package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileMatch holds a pair of file paths (A and B) for a matched relative path.
type FileMatch struct {
	FileAPath string
	FileBPath string
}

// CollectFromPaths collects all .txt files from a mix of files and directories.
// Returns a map of relativePath -> fullPath. For individual files, uses the filename as the relative path.
// For directories, uses the relative path from the directory root.
func CollectFromPaths(paths []string) (map[string]string, error) {
	files := make(map[string]string)

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("error accessing path %s: %w", path, err)
		}

		if info.IsDir() {
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
					key := filepath.Join(filepath.Base(path), relPath)
					files[key] = filePath
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("error walking directory %s: %w", path, err)
			}
		} else {
			if strings.HasSuffix(strings.ToLower(path), ".txt") {
				key := filepath.Base(path)
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

// FindMatching returns files that exist in both sets by matching relative paths.
// Returns a map of relativePath -> FileMatch.
func FindMatching(filesA, filesB map[string]string) map[string]FileMatch {
	matches := make(map[string]FileMatch)
	for keyA, pathA := range filesA {
		if pathB, exists := filesB[keyA]; exists {
			matches[keyA] = FileMatch{FileAPath: pathA, FileBPath: pathB}
		}
	}

	// Only exact relative-path matches: files must have the same path+filename in both sets.
	return matches
}
