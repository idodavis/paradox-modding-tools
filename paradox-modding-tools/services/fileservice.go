package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ############
// FileService
// ############

// FileService provides directory/file selection dialogs and game script root / doc path discovery.
type FileService struct{}

// FileMatch represents a matched file pair
type FileMatch struct {
	FileAPath string `json:"fileAPath"`
	FileBPath string `json:"fileBPath"`
}

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

// GetScriptRoot returns the game script root directory for the given game and install path.
// CK3: <install>/game, EU5: <install>/game/in_game.
func (f *FileService) GetScriptRoot(installPath, game string) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(game))
	switch norm {
	case "ck3":
		return filepath.Join(installPath, "game"), nil
	case "eu5":
		return filepath.Join(installPath, "game", "in_game"), nil
	default:
		return "", fmt.Errorf("unknown game: %s", game)
	}
}

// DocFileEntry represents a doc file found under the game script root (JSON-safe for bindings).
type DocFileEntry struct {
	RelativePath string `json:"relativePath"`
	FullPath     string `json:"fullPath"`
}

// ListGameDocFiles walks the game script root and collects .info (CK3) or readme.txt (EU5) files.
func (f *FileService) ListGameDocFiles(game string, installPath string) ([]DocFileEntry, error) {
	root, err := f.GetScriptRoot(installPath, game)
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

// SaveFile sets where to save a file via dialog and writes the content to the file.
// Returns ("", nil) when the user cancels so no error dialog is shown.
func (s *FileService) SaveFile(defaultName, fileType string, content string) (string, error) {
	path, err := application.Get().Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter(fileType, "*."+fileType).
		PromptForSingleSelection()
	if err != nil {
		return "", nil
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		err = fmt.Errorf("error writing file %s: %w", path, err)
	}

	return path, err
}

// ExportInventoryFromBackend uses the stored extraction result, applies filterState, and saves to a file chosen by the user.
// Returns the path of the written file, or ("", nil) if the user cancels, or ("", err) if no inventory or write failure.
// func (s *FileService) ExportInventoryFromBackend(filterState inventory.FilterState, format string, includeRawText bool) (string, error) {
// 	exportData := inventory.FilterForExport(&filterState)
// 	if len(exportData) == 0 {
// 		return "", fmt.Errorf("no inventory data to export")
// 	}

// 	var content string
// 	var fileType string
// 	if format == "csv" {
// 		fileType = "csv"
// 		var buf strings.Builder
// 		w := csv.NewWriter(&buf)
// 		header := []string{"type", "key", "filePath", "lineStart", "lineEnd"}
// 		if includeRawText {
// 			header = append(header, "rawText")
// 		}
// 		w.Write(header)
// 		for _, result := range exportData {
// 			for _, item := range result.Items {
// 				row := []string{item.Type, item.Key, item.FilePath, fmt.Sprintf("%d", item.LineStart), fmt.Sprintf("%d", item.LineEnd)}
// 				if includeRawText {
// 					row = append(row, item.RawText)
// 				}
// 				w.Write(row)
// 			}
// 		}
// 		w.Flush()
// 		content = buf.String()
// 	} else {
// 		fileType = "json"
// 		if !includeRawText {
// 			for _, result := range exportData {
// 				for i := range result.Items {
// 					result.Items[i].RawText = ""
// 				}
// 			}
// 		}
// 		jsonBytes, _ := json.MarshalIndent(exportData, "", "  ")
// 		content = string(jsonBytes)
// 	}

// 	timestamp := time.Now().Format("2006-01-02_15-04-05")
// 	return s.SaveFile(fmt.Sprintf("inventory_export_%s.%s", timestamp, fileType), fileType, content)
// }

// OpenFolder opens the given folder path in the system file manager (e.g. Explorer on Windows, Finder on macOS).
func (s *FileService) OpenFolder(folderPath string) error {
	if folderPath == "" {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", filepath.ToSlash(folderPath))
	case "darwin":
		cmd = exec.Command("open", folderPath)
	default:
		cmd = exec.Command("xdg-open", folderPath)
	}
	return cmd.Start()
}
