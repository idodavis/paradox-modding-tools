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
	Extensions  []string // e.g. [".txt", ".lua"]
	FileNames   []string // e.g. ["readme.txt", "mod.lua"]
	Regex       string   // e.g. "^(readme|mod)\.txt$"
	IncludePath string   // regex on rel path, e.g. "events/" - include only if matches
	ExcludePath string   // regex on rel path, e.g. "common/" - exclude if matches
}

// CollectFilesFromPath collects all .txt files from a mix of files and directories
// Returns a map of relativePath -> fullPath
func (f *FileService) CollectFilesFromPath(inputPath string, filter FileCollectorFilter) (map[string]string, error) {
	files := make(map[string]string)

	walkErr := filepath.WalkDir(inputPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		if len(filter.Extensions) > 0 && !slices.Contains(filter.Extensions, filepath.Ext(path)) {
			return nil
		}
		if len(filter.FileNames) > 0 && !slices.Contains(filter.FileNames, filepath.Base(path)) {
			return nil
		}
		rel, err := filepath.Rel(inputPath, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if filter.Regex != "" && !regexp.MustCompile(filter.Regex).MatchString(path) {
			return nil
		}
		if filter.IncludePath != "" {
			if re, err := regexp.Compile(filter.IncludePath); err == nil && !re.MatchString(relSlash) {
				return nil
			}
		}
		if filter.ExcludePath != "" {
			if re, err := regexp.Compile(filter.ExcludePath); err == nil && re.MatchString(relSlash) {
				return nil
			}
		}
		files[relSlash] = path
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return nil, fmt.Errorf("Tree walk error in %s: %w", inputPath, walkErr)
	}

	return files, nil
}

// FileMatch represents a matched path pair
type PathMatch struct {
	PathA string `json:"pathA"`
	PathB string `json:"pathB"`
}

// FindMatchingPaths finds paths that exist in both sets. When matchByFilenameOnly is true,
// matches only by filename (e.g. for zz_mod_file.txt where paths differ).
func (f *FileService) FindMatchingPaths(filesA, filesB map[string]string, matchByFilenameOnly bool) (map[string]PathMatch, error) {
	if matchByFilenameOnly {
		return f.findMatchingByFilenameOnly(filesA, filesB), nil
	}
	return f.findMatchingByPath(filesA, filesB), nil
}

func (f *FileService) findMatchingByFilenameOnly(filesA, filesB map[string]string) map[string]PathMatch {
	matches := make(map[string]PathMatch)
	matchedB := make(map[string]bool)
	filenameToB := make(map[string][]string)
	for k, p := range filesB {
		base := filepath.Base(p)
		filenameToB[base] = append(filenameToB[base], k)
	}
	for keyA, pathA := range filesA {
		base := filepath.Base(pathA)
		for _, keyB := range filenameToB[base] {
			if matchedB[keyB] {
				continue
			}
			pathB := filesB[keyB]
			matchKey := keyA
			if len(keyB) > len(keyA) {
				matchKey = keyB
			}
			matches[matchKey] = PathMatch{PathA: pathA, PathB: pathB}
			matchedB[keyB] = true
			break
		}
	}
	return matches
}

func (f *FileService) findMatchingByPath(filesA, filesB map[string]string) map[string]PathMatch {
	matches := make(map[string]PathMatch)
	matchedA := make(map[string]bool)
	matchedB := make(map[string]bool)

	for keyA, pathA := range filesA {
		if pathB, exists := filesB[keyA]; exists {
			matches[keyA] = PathMatch{PathA: pathA, PathB: pathB}
			matchedA[keyA] = true
			matchedB[keyA] = true
		}
	}

	for keyA, pathA := range filesA {
		if matchedA[keyA] {
			continue
		}
		partsA := strings.Split(keyA, string(filepath.Separator))
		if len(partsA) <= 1 {
			continue
		}
		relStructA := strings.Join(partsA[1:], string(filepath.Separator))
		for keyB, pathB := range filesB {
			if matchedB[keyB] {
				continue
			}
			partsB := strings.Split(keyB, string(filepath.Separator))
			if len(partsB) > 1 {
				relStructB := strings.Join(partsB[1:], string(filepath.Separator))
				if relStructA == relStructB {
					matchKey := keyA
					if len(keyB) > len(keyA) {
						matchKey = keyB
					}
					matches[matchKey] = PathMatch{PathA: pathA, PathB: pathB}
					matchedA[keyA] = true
					matchedB[keyB] = true
					break
				}
			}
		}
	}

	for keyA, pathA := range filesA {
		if matchedA[keyA] {
			continue
		}
		filenameA := filepath.Base(pathA)
		for keyB, pathB := range filesB {
			if matchedB[keyB] {
				continue
			}
			if filepath.Base(pathB) == filenameA {
				matchKey := keyA
				if len(keyB) > len(keyA) {
					matchKey = keyB
				}
				matches[matchKey] = PathMatch{PathA: pathA, PathB: pathB}
				matchedA[keyA] = true
				matchedB[keyB] = true
				break
			}
		}
	}
	return matches
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
