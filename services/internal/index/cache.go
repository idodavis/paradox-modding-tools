// cache.go is step 1 and step 6: the store loaded again while nothing changed, and saved as one file.
// The file is named so that any change to the engine, the install or the dumps is another file.

package index

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
)

// fromCache is the install a request's cache file holds, if it has a readable one; a request with no
// app version never caches.
func fromCache(req Request) (*Install, bool) {
	if req.App == "" {
		return nil, false
	}
	install, err := LoadInstall(req)
	return install, err == nil
}

// devKey is a dev build's app part of a cache name: dev- and a hash of the engine's code, the files
// of the packages game, jomini, loc, inventory, schema, index and wiki in the repo the build sits
// in (tests and READMEs aside), so only an engine change rescans. Outside a repo it hashes the
// executable's path, size and time.
var devKey = sync.OnceValues(func() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "", err
	}
	sum, engine := sha256.New(), false
	for repo := filepath.Dir(executable); repo != filepath.Dir(repo) && !engine; repo = filepath.Dir(repo) {
		for _, dir := range []string{"game", "jomini", "loc", "inventory", "schema", "index", "wiki"} {
			_ = inventory.Walk(filepath.Join(repo, "services", "internal", dir), func(path string) error {
				if text, err := os.ReadFile(path); err == nil && !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".md") {
					engine = true
					_, _ = fmt.Fprintf(sum, "%s|%d|", filepath.Base(path), len(text))
					sum.Write(text)
				}
				return nil
			})
		}
	}
	if !engine {
		_, _ = fmt.Fprintf(sum, "%s|%d|%d", executable, info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("dev-%x", sum.Sum(nil)[:6]), nil
})

// cachePath is a request's cache file, install-<game>-<folder>-<version>-<app>-<state>.gob.gz, and
// its prefix; the state hashes the language, install update time, dump names and dates.
func cachePath(req Request) (path, ofFolder string, err error) {
	dir, err := appdata.CacheDir()
	if err != nil {
		return "", "", err
	}
	version, app := appdata.SafeName(req.Version), appdata.SafeName(req.App)
	if req.App == "dev" {
		if app, err = devKey(); err != nil {
			return "", "", err
		}
	}
	state := sha256.New()
	_, _ = fmt.Fprintf(state, "%s|%d", req.Language, game.InstallUpdatedAt(req.Dir).UnixNano())
	for _, file := range inventory.Files("dumps", game.DumpDirs(req.Game, req.Dir)) {
		name := filepath.Base(file.Path)
		if info, err := os.Stat(file.Path); err == nil && slices.Contains(schema.DumpFiles, name) {
			_, _ = fmt.Fprintf(state, "|%s|%d", name, info.ModTime().UnixNano())
		}
	}
	folder := sha256.Sum256([]byte(filepath.Clean(req.Dir)))
	ofFolder = filepath.Join(dir, fmt.Sprintf("install-%s-%x-", appdata.SafeName(req.Game), folder[:6]))
	return fmt.Sprintf("%s%s-%s-%x.gob.gz", ofFolder, version, app, state.Sum(nil)[:6]), ofFolder, nil
}

// forgetCaches deletes the cache files whose path begins with prefix, except keep, and gives the
// version in the name of the first (a version has no dash).
func forgetCaches(prefix, keep string) (version string, err error) {
	dir := filepath.Dir(prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if path == keep || !strings.HasPrefix(path, prefix) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return version, err
		}
		if version == "" {
			version, _, _ = strings.Cut(strings.TrimPrefix(path, prefix), "-")
		}
	}
	return version, nil
}

// SaveInstall writes the install to its cache file whole, then deletes its install folder's older
// cache files (Previous takes the first one's version) and every cache file no reader names
// (neither install- nor wiki-), and the temporary files a run that ended mid-write left
// (appdata.Leftover); a write in flight keeps its own.
func SaveInstall(req Request, install *Install) error {
	path, ofFolder, err := cachePath(req)
	if err != nil {
		return err
	}
	err = appdata.Write(path, func(out io.Writer) error { return appdata.Encode(out, install) })
	if err != nil {
		return err
	}
	install.Previous, err = forgetCaches(ofFolder, path)
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		name, full := entry.Name(), filepath.Join(filepath.Dir(path), entry.Name())
		if !strings.HasPrefix(name, "install-") && !strings.HasPrefix(name, "wiki-") && (!strings.HasPrefix(name, ".") || appdata.Leftover(full)) {
			_ = os.Remove(full)
		}
	}
	return err
}

// CachedAt is when a request's cache file was written, by its file time; zero where there is none.
func CachedAt(req Request) time.Time {
	if path, _, err := cachePath(req); err == nil {
		if info, err := os.Stat(path); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}

// LoadInstall reads a request's cache file, with the index ready to query.
func LoadInstall(req Request) (*Install, error) {
	path, _, err := cachePath(req)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	install := &Install{}
	if err := appdata.Decode(file, install); err != nil {
		return nil, err
	}
	install.Index.Rebuild()
	return install, nil
}

// DropInstall deletes every cache file of a request's game and install folder.
func DropInstall(req Request) error {
	_, ofFolder, err := cachePath(req)
	if err != nil {
		return err
	}
	_, err = forgetCaches(ofFolder, "")
	return err
}
