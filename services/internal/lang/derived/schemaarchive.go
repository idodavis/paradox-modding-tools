// schemaarchive.go keeps a copy of the game's script_docs dumps inside the PMT
// cache. The game only writes those files when a user runs `script_docs` in the
// console, and nothing stops them being cleaned up afterwards. Archiving means a
// user runs that command once per game version rather than once per accident.
//
// Raw files are archived rather than only the parsed declared.Schema so that a future
// CacheFormatVersion bump can re-derive the type system without asking the user
// to launch the game again.

package derived

import (
	"os"
	"paradox-modding-tools/services/internal/lang/declared"
	"path/filepath"
	"strings"
)

// archiveCap bounds one archived file. The real dumps are well under this
// (CK3 ~1.5 MB in total, Vic3 ~2.6 MB, EU5 ~1.9 MB); the cap only stops a
// pathological file from filling the cache.
const archiveCap = 32 << 20

// schemaArchiveDir is where dumps for one install + version are kept.
func schemaArchiveDir(installID, version string) (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir,
		"script-docs-"+sanitize(installID)+"-"+sanitize(version)), nil
}

// archiveScriptDocs copies every recognized dump under dirs into the cache,
// replacing whatever was there. Failure is not fatal: the archive is a
// convenience, and a scan that cannot write it still has the live dumps.
func archiveScriptDocs(installID, version string, dirs []string) (int, error) {
	dst, err := schemaArchiveDir(installID, version)
	if err != nil {
		return 0, err
	}
	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !declared.IsDumpFile(name) {
				return nil
			}
			if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".md") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > archiveCap {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			// Several dirs are searched per game; the first copy of a name wins,
			// matching the order declared.ReadSchema walks them in.
			out := filepath.Join(tmp, name)
			if _, err := os.Stat(out); err == nil {
				return nil
			}
			if os.WriteFile(out, raw, 0o644) == nil {
				n++
			}
			return nil
		})
	}
	if n == 0 {
		_ = os.RemoveAll(tmp)
		return 0, nil
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return 0, err
	}
	return n, nil
}

// loadSchema resolves the game's type system: live dumps first, then PMT's
// archive. A live read refreshes the archive so the copy tracks the newest
// dumps the user has generated.
func loadSchema(installID, version string, dirs []string) (*declared.Schema, declared.SchemaSource) {
	if s := declared.ReadSchema(dirs); s != nil {
		if _, err := archiveScriptDocs(installID, version, dirs); err == nil {
			// Archive refreshed; nothing else to do.
			_ = err
		}
		s.Source = declared.SchemaLive
		return s, declared.SchemaLive
	}
	dir, err := schemaArchiveDir(installID, version)
	if err != nil {
		return nil, declared.SchemaMissing
	}
	if s := declared.ReadSchema([]string{dir}); s != nil {
		s.Source = declared.SchemaArchive
		return s, declared.SchemaArchive
	}
	return nil, declared.SchemaMissing
}

// hasSchemaArchive reports whether a cached copy exists for install + version.
func hasSchemaArchive(installID, version string) bool {
	dir, err := schemaArchiveDir(installID, version)
	if err != nil {
		return false
	}
	ents, err := os.ReadDir(dir)
	return err == nil && len(ents) > 0
}

// SchemaStatus is what the Workspace Health strip needs to know about the
// declared type system, read without loading the model. script_docs is a couple
// of megabytes; the model is ninety, and health is polled.
type SchemaStatus struct {
	Source     declared.SchemaSource `json:"source"`
	Effects    int                   `json:"effects"`
	Triggers   int                   `json:"triggers"`
	ScopeTypes int                   `json:"scopeTypes"`
	Prefixes   int                   `json:"prefixes"`
	OnActions  int                   `json:"onActions"`
	// ReadAt is the newest dump mtime (RFC3339), for staleness against the install.
	ReadAt string `json:"readAt,omitempty"`
	// Archived reports that PMT holds a copy, so deleting the game's own dumps
	// costs nothing.
	Archived bool `json:"archived"`
	// Folder is where this game writes its dumps, for the walkthrough.
	Folder string `json:"folder,omitempty"`
}

// ReadSchemaStatus reports the type system's state for one install. Unlike the
// scan path it never refreshes the archive: health is polled, and a read should
// not copy files as a side effect.
func ReadSchemaStatus(gameID, installID, installPath, version string) SchemaStatus {
	version = CacheVersion(version)
	dirs := scriptDocsDirs(gameID, installPath)
	st := SchemaStatus{Source: declared.SchemaMissing, Archived: hasSchemaArchive(installID, version)}
	for _, d := range dirs {
		if _, err := os.Stat(d); err == nil {
			st.Folder = d
			break
		}
	}
	if st.Folder == "" && len(dirs) > 0 {
		st.Folder = dirs[0]
	}
	sc := declared.ReadSchema(dirs)
	if sc != nil {
		st.Source = declared.SchemaLive
	} else if dir, err := schemaArchiveDir(installID, version); err == nil {
		if sc = declared.ReadSchema([]string{dir}); sc != nil {
			st.Source = declared.SchemaArchive
		}
	}
	if sc == nil {
		return st
	}
	st.Effects, st.Triggers = len(sc.Effects), len(sc.Triggers)
	st.ScopeTypes, st.OnActions = len(sc.Scopes), len(sc.OnActions)
	st.Prefixes = len(sc.Prefixes())
	st.ReadAt = sc.ReadAt
	return st
}
