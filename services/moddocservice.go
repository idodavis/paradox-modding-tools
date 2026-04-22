package services

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	internal "paradox-modding-tools/services/internal"
	"paradox-modding-tools/services/internal/repos"

	"github.com/jmoiron/sqlx"
)

type ModDocService struct {
	FileService *FileService
	DB          *sqlx.DB
	repo        *repos.ModDocRepository
}

func (m *ModDocService) getRepo() *repos.ModDocRepository {
	if m.repo == nil {
		m.repo = repos.NewModDocRepository(m.DB)
	}
	return m.repo
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
	files, err := m.FileService.CollectFilesFromPath(gameScriptRoot, filter)
	if err != nil {
		return nil, err
	}

	filesSlice := make([]string, 0, len(files))
	hash := internal.InstallPathHash(installPath)
	fetchedAt := time.Now().UTC().Format(time.RFC3339)

	repo := m.getRepo()
	for relPath, absPath := range files {
		filesSlice = append(filesSlice, relPath)
		content, err := m.FileService.ReadFileContent(absPath)
		if err != nil {
			content = ""
		}
		if err := repo.UpsertDocFile(gameNorm, hash, relPath, absPath, content, fetchedAt); err != nil {
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
	hash := internal.InstallPathHash(installPath)
	files, err := m.getRepo().GetDocFiles(gameNorm, hash)
	if err != nil {
		return nil, err
	}

	var paths []string
	var scannedAt string
	for _, f := range files {
		paths = append(paths, f.RelPath)
		if f.FetchedAt > scannedAt {
			scannedAt = f.FetchedAt
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
	hash := internal.InstallPathHash(installPath)
	return m.getRepo().GetDocContent(gameNorm, hash, relPath)
}
