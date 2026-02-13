package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ############
// FileService
// ############

const (
	scriptRootFolderCK3 = "game"
	scriptRootFolderEU5 = "game/in_game"
)

// FileService provides directory/file selection dialogs and game script root / doc path discovery.
type FileService struct{}

// SelectDirectory opens a directory selection dialog.
// Returns ("", nil) when the user cancels so no error dialog is shown.
func (f *FileService) SelectDirectory(title string) (string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", nil
	}
	return path, err
}

// SelectDirectories opens a directory selection dialog allowing multiple selections.
// Returns (nil, nil) when the user cancels so no error dialog is shown.
func (f *FileService) SelectDirectories(title string) ([]string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseDirectories(true)
	paths, err := dialog.PromptForMultipleSelection()
	if err != nil {
		return nil, nil
	}
	return paths, err
}

// SelectSingleFile opens a file selection dialog for a single file.
// Returns ("", nil) when the user cancels so no error dialog is shown.
func (f *FileService) SelectSingleFile(title, filter string) (string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseFiles(true)
	dialog.CanChooseDirectories(false)
	if filter != "" {
		dialog.AddFilter(filter, filter)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", nil
	}
	return path, err
}

// SelectFiles opens a file selection dialog allowing multiple selections.
// Returns (nil, nil) when the user cancels so no error dialog is shown.
func (f *FileService) SelectFiles(title, filter string) ([]string, error) {
	app := application.Get()
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle(title)
	dialog.CanChooseFiles(true)
	if filter != "" {
		dialog.AddFilter(filter, filter)
	}
	paths, err := dialog.PromptForMultipleSelection()
	if err != nil {
		return nil, nil
	}
	return paths, err
}

// ReadFileContent reads a file as UTF-8 text.
func (f *FileService) ReadFileContent(fullPath string) (string, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// SaveFile opens a save-file dialog, then writes content to the chosen path as UTF-8.
// Returns ("", nil) when the user cancels so no error dialog is shown.
func (f *FileService) SaveFile(title, defaultName, content, ext string) (string, error) {
	app := application.Get()
	dialog := app.Dialog.SaveFile()
	if title != "" {
		dialog.SetMessage(title)
	}
	if defaultName != "" {
		dialog.SetFilename(defaultName)
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", nil
	}
	fullPath := filepath.Clean(filepath.FromSlash(path))

	wantExt := ext
	if wantExt != "" && !strings.HasPrefix(wantExt, ".") {
		wantExt = "." + wantExt
	}
	if wantExt != "" && filepath.Ext(fullPath) != wantExt {
		fullPath = fullPath + wantExt
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fullPath, nil
}

// GetScriptRoot returns the game script root directory for the given game and install path.
// CK3: <install>/game, EU5: <install>/game/in_game.
func (f *FileService) GetGameScriptRoot(game string, installPath string) (string, error) {
	switch game {
	case "CK3":
		return filepath.Join(installPath, scriptRootFolderCK3), nil
	case "EU5":
		return filepath.Join(installPath, scriptRootFolderEU5), nil
	default:
		return "", fmt.Errorf("unknown game: %s", game)
	}
}

type FileCollectorFilter struct {
	Extensions []string // e.g. [".txt", ".lua"]
	FileNames  []string // e.g. ["readme.txt", "mod.lua"]
	Regex      string   // e.g. "^(readme|mod)\.txt$"
}

// CollectFilesFromPaths collects all .txt files from a mix of files and directories
// Returns a map of relativePath -> fullPath
func (f *FileService) CollectFilesFromPaths(inputPaths []string, filter FileCollectorFilter) (map[string]string, error) {
	files := make(map[string]string)

	for _, inputPath := range inputPaths {
		walkErr := filepath.WalkDir(inputPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			// Apply filters if provided
			if len(filter.Extensions) > 0 && !slices.Contains(filter.Extensions, filepath.Ext(path)) {
				return nil
			}
			if len(filter.FileNames) > 0 && !slices.Contains(filter.FileNames, filepath.Base(path)) {
				return nil
			}
			if filter.Regex != "" && !regexp.MustCompile(filter.Regex).MatchString(path) {
				return nil
			}

			// grab relative path from inputPath to
			rel, err := filepath.Rel(inputPath, path)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = path
			return nil
		})
		if walkErr != nil && walkErr != fs.SkipAll {
			return nil, fmt.Errorf("Tree walk error in %s: %w", inputPath, walkErr)
		}
	}

	return files, nil
}

// FileMatch represents a matched path pair
type PathMatch struct {
	PathA string `json:"pathA"`
	PathB string `json:"pathB"`
}

// FindMatchingPaths finds paths that exist in both sets by matching relative paths
// Returns a map of relativePath -> PathMatch
func (f *FileService) FindMatchingPaths(filesA, filesB map[string]string) (map[string]PathMatch, error) {
	matches := make(map[string]PathMatch)
	matchedA := make(map[string]bool)
	matchedB := make(map[string]bool)

	// First, try to match files by their relative path keys (exact match)
	for keyA, pathA := range filesA {
		if pathB, exists := filesB[keyA]; exists {
			matches[keyA] = PathMatch{
				PathA: pathA,
				PathB: pathB,
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
						matches[matchKey] = PathMatch{
							PathA: pathA,
							PathB: pathB,
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
				matches[matchKey] = PathMatch{
					PathA: pathA,
					PathB: pathB,
				}
				matchedA[keyA] = true
				matchedB[keyB] = true
				break
			}
		}
	}

	return matches, nil
}

type TreeNode struct {
	RelPath  string     `json:"relPath"`
	Name     string     `json:"name"`
	Children []TreeNode `json:"children"`
}

// BuildTree builds a file tree from a list of paths
func (f *FileService) BuildTree(paths []string) []TreeNode {
	var tree []TreeNode
	for _, path := range paths {
		tree = AddToTree(tree, path, strings.Split(filepath.ToSlash(path), "/"))
	}
	return tree
}

// AddToTree adds a node to the tree recursively
func AddToTree(root []TreeNode, relPath string, nodeNames []string) []TreeNode {
	if len(nodeNames) > 0 {
		var i int
		for i = 0; i < len(root); i++ {
			if root[i].Name == nodeNames[0] { // already in tree
				break
			}
		}
		if i == len(root) {
			root = append(root, TreeNode{RelPath: relPath, Name: nodeNames[0]})
		}
		root[i].Children = AddToTree(root[i].Children, relPath, nodeNames[1:])
	}
	return root
}
