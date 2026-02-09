package services

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ModDocService struct {
	fileService *FileService
}

// DocPathCache is the cached doc path list for a game+install path (JSON-safe for bindings).
type DocPathCache struct {
	Paths            []string `json:"paths"`
	ScannedAt        string   `json:"scannedAt"`
	InstallPath      string   `json:"installPath"`
	LastSeenUpdateId string   `json:"lastSeenUpdateId,omitempty"`
}

// DocPathCacheSet is the payload for setting the doc path cache.
type DocPathCacheSet struct {
	Paths            []string `json:"paths"`
	ScannedAt        string   `json:"scannedAt"`
	LastSeenUpdateId string   `json:"lastSeenUpdateId,omitempty"`
}

// TODO: Look Into Moving Cache to it's own service and migrate settings/patchnotes caches there as well.
// TODO: Add a way to clear the cache.

func (m *ModDocService) Scan(game string, installPath string) ([]string, error) {
	// Check if the cache is valid
	cache, err := m.GetDocPathCache(game, installPath)
	if err != nil {
		return nil, err
	}

	if cache != nil {
		scannedAt, err := time.Parse(time.RFC3339, cache.ScannedAt)
		if err != nil {
			return nil, err
		}
		if time.Since(scannedAt) < time.Hour*24*7 {
			return cache.Paths, nil
		}
	}

	// If cache is empty, scan the game script root
	gameScriptRoot, err := m.fileService.GetGameScriptRoot(game, installPath)
	if err != nil {
		return nil, err
	}
	var filter FileCollectorFilter
	switch game {
	case "CK3":
		filter = FileCollectorFilter{
			Extensions: []string{".info"},
		}
	case "EU5":
		filter = FileCollectorFilter{
			FileNames: []string{"readme.txt"},
		}
	default:
		return nil, errors.New("unknown game: " + game)
	}
	files, err := m.fileService.CollectFilesFromPaths([]string{gameScriptRoot}, filter)
	if err != nil {
		return nil, err
	}

	// Convert the files map to a slice of strings
	filesSlice := make([]string, 0, len(files))
	for relPath := range files {
		filesSlice = append(filesSlice, relPath)
	}

	// Set/update the cache
	err = m.setDocPathCache(game, installPath, DocPathCacheSet{
		Paths:            filesSlice,
		ScannedAt:        time.Now().Format(time.RFC3339),
		LastSeenUpdateId: uuid.New().String(),
	})
	if err != nil {
		return nil, err
	}

	return filesSlice, nil
}

// pathHash returns a hash of the path matching the frontend (32-bit, base36).
func pathHash(path string) string {
	var h uint32
	for _, c := range path {
		h = h*31 + uint32(c)
	}
	return strconv.FormatUint(uint64(h), 36)
}

func docpathsCacheKey(game, installPath string) string {
	return strings.ToLower(strings.TrimSpace(game)) + "_" + pathHash(installPath)
}

func loadDocpathsCache() (map[string]*DocPathCache, error) {
	var c map[string]*DocPathCache
	_ = readConfigFile(docpathsCacheFileName, &c)
	if c == nil {
		c = make(map[string]*DocPathCache)
	}
	return c, nil
}

// GetDocPathCache returns the cached doc path list for the game+install path, or nil if not found.
func (m *ModDocService) GetDocPathCache(game, installPath string) (*DocPathCache, error) {
	game = strings.ToLower(strings.TrimSpace(game))
	installPath = strings.TrimSpace(installPath)
	if game == "" || installPath == "" {
		return nil, nil
	}
	c, err := loadDocpathsCache()
	if err != nil {
		return nil, err
	}
	entry := c[docpathsCacheKey(game, installPath)]
	if entry == nil || len(entry.Paths) == 0 {
		return nil, nil
	}
	return entry, nil
}

// SetDocPathCache writes the doc path cache for the game+install path to disk (same dir as settings).
func (m *ModDocService) setDocPathCache(game, installPath string, data DocPathCacheSet) error {
	game = strings.ToLower(strings.TrimSpace(game))
	installPath = strings.TrimSpace(installPath)
	if game == "" || installPath == "" {
		return nil
	}
	c, err := loadDocpathsCache()
	if err != nil {
		return err
	}
	key := docpathsCacheKey(game, installPath)
	c[key] = &DocPathCache{
		Paths:       data.Paths,
		ScannedAt:   data.ScannedAt,
		InstallPath: installPath,
	}
	return writeConfigFile(docpathsCacheFileName, c)
}
