package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ############
// FileService
// ############

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

// DocFileEntry represents a doc file found under the game script root (JSON-safe for bindings).
type DocFileEntry struct {
	RelativePath string `json:"relativePath"`
	FullPath     string `json:"fullPath"`
}

// ListGameDocFiles walks the game script root and collects .info (CK3) or readme.txt (EU5) files.
func (f *FileService) ListGameDocFiles(game string, installPath string) ([]DocFileEntry, error) {
	root, err := f.GetGameScriptRoot(installPath, game)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("script root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("script root is not a directory: %s", root)
	}
	gameNorm := strings.ToLower(strings.TrimSpace(game))
	var matchExt string
	var matchExact string
	if gameNorm == "ck3" {
		matchExt = ".info"
	} else if gameNorm == "eu5" {
		matchExact = "readme.txt"
	} else {
		return nil, fmt.Errorf("unknown game: %s", game)
	}
	var result []DocFileEntry
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if matchExact != "" {
			if strings.EqualFold(name, matchExact) {
				rel, e := filepath.Rel(root, path)
				if e != nil {
					return e
				}
				result = append(result, DocFileEntry{RelativePath: filepath.ToSlash(rel), FullPath: path})
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(name), matchExt) {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			result = append(result, DocFileEntry{RelativePath: filepath.ToSlash(rel), FullPath: path})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadFileContent reads a file as UTF-8 text.
func (f *FileService) ReadFileContent(fullPath string) (string, error) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// GetScriptRoot returns the game script root directory for the given game and install path.
// CK3: <install>/game, EU5: <install>/game/in_game.
func (f *FileService) GetGameScriptRoot(game string, installPath string) (string, error) {
	switch game {
	case "CK3":
		return filepath.Join(installPath, "game"), nil
	case "EU5":
		return filepath.Join(installPath, "game", "in_game"), nil
	default:
		return "", fmt.Errorf("unknown game: %s", game)
	}
}

// CollectFilesFromPaths collects all .txt files from a mix of files and directories
// Returns a map of relativePath -> fullPath
func (f *FileService) CollectFilesFromPaths(inputPaths []string) (map[string]string, error) {
	files := make(map[string]string)

	for _, inputPath := range inputPaths {
		walkErr := filepath.WalkDir(inputPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".txt" {
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
	for rel, pathA := range filesA {
		if pathB, ok := filesB[rel]; ok {
			matches[rel] = PathMatch{PathA: pathA, PathB: pathB}
		}
	}
	return matches, nil
}
