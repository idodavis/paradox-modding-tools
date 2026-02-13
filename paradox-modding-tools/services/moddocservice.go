package services

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type ModDocService struct {
	FileService *FileService
	DB          *sql.DB
}

// DocPathCache is the cached doc path list for a game+install path (JSON-safe for bindings).
type DocPathCache struct {
	Paths       []string `json:"paths"`
	ScannedAt   string   `json:"scannedAt"`
	InstallPath string   `json:"installPath"`
}

func (m *ModDocService) modDocGame(game string) string {
	return strings.ToLower(strings.TrimSpace(game))
}

func (m *ModDocService) Scan(game string, installPath string) ([]string, error) {
	gameNorm := m.modDocGame(game)
	installPath = strings.TrimSpace(installPath)
	if gameNorm == "" || installPath == "" {
		return nil, errors.New("game and install path required")
	}

	gameScriptRoot, err := m.FileService.GetGameScriptRoot(game, installPath)
	if err != nil {
		return nil, err
	}
	var filter FileCollectorFilter
	switch game {
	case "CK3":
		filter = FileCollectorFilter{Extensions: []string{".info"}}
	case "EU5":
		filter = FileCollectorFilter{FileNames: []string{"readme.txt"}}
	default:
		return nil, errors.New("unknown game: " + game)
	}
	files, err := m.FileService.CollectFilesFromPaths([]string{gameScriptRoot}, filter)
	if err != nil {
		return nil, err
	}

	filesSlice := make([]string, 0, len(files))
	hash := installPathHash(installPath)
	fetchedAt := time.Now().UTC().Format(time.RFC3339)

	for relPath, absPath := range files {
		filesSlice = append(filesSlice, relPath)
		if m.DB == nil {
			continue
		}
		content, err := m.FileService.ReadFileContent(absPath)
		if err != nil {
			content = ""
		}
		_, err = m.DB.Exec(`INSERT INTO doc_files (game, install_path_hash, rel_path, abs_path, content, fetched_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(game, install_path_hash, rel_path) DO UPDATE SET abs_path=excluded.abs_path, content=excluded.content, fetched_at=excluded.fetched_at`,
			gameNorm, hash, relPath, absPath, content, fetchedAt)
		if err != nil {
			return nil, err
		}
	}

	return filesSlice, nil
}

// GetDocPathCache returns the doc path list from doc_files for the game+install path.
func (m *ModDocService) GetDocPathCache(game, installPath string) (*DocPathCache, error) {
	gameNorm := m.modDocGame(game)
	installPath = strings.TrimSpace(installPath)
	if gameNorm == "" || installPath == "" {
		return nil, nil
	}
	if m.DB == nil {
		return nil, nil
	}
	hash := installPathHash(installPath)
	rows, err := m.DB.Query(`SELECT rel_path, fetched_at FROM doc_files WHERE game = ? AND install_path_hash = ? ORDER BY rel_path`, gameNorm, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	var scannedAt string
	for rows.Next() {
		var relPath, ft string
		if err := rows.Scan(&relPath, &ft); err != nil {
			return nil, err
		}
		paths = append(paths, relPath)
		if ft > scannedAt {
			scannedAt = ft
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return &DocPathCache{Paths: paths, ScannedAt: scannedAt, InstallPath: installPath}, nil
}

// GetDocContent returns the content of a doc file from doc_files.
func (m *ModDocService) GetDocContent(game, installPath, relPath string) (string, error) {
	gameNorm := m.modDocGame(game)
	installPath = strings.TrimSpace(installPath)
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if gameNorm == "" || installPath == "" || relPath == "" {
		return "", nil
	}
	if m.DB == nil {
		return "", nil
	}
	hash := installPathHash(installPath)
	var content string
	err := m.DB.QueryRow(`SELECT content FROM doc_files WHERE game = ? AND install_path_hash = ? AND rel_path = ?`, gameNorm, hash, relPath).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return content, nil
}
