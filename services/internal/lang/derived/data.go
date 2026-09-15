// data.go is the data-row tier: the databases a game defines once per instance
// rather than once per rule — history, map, dynasty and portrait tables. On CK3
// they are 60% of the model by def count and almost none of what an editor ever
// asks about, so they leave VanillaCache at scan time, persist in a file of
// their own, and are indexed only when a name the script cache cannot place
// turns up. game.GameInfo.DataKinds names them per game.

package derived

import (
	"slices"

	"paradox-modding-tools/services/internal/game"
)

// splitData divides scanned defs into script constructs and data rows.
func splitData(gameID string, defs []Def) (script, data []Def) {
	for _, d := range defs {
		if !game.IsDataKind(gameID, d.Kind) {
			script = append(script, d)
			continue
		}
		// A row keeps its identity and its site; the rest describes script.
		d.Doc, d.Value, d.OwnerKey, d.Nested = "", "", "", false
		data = append(data, d)
	}
	return script, data
}

// SaveData writes the tier beside the script cache, as a VanillaCache holding nothing but defs so that it shares the path interning.
func SaveData(installID, version string, defs []Def) error {
	path, err := vanillaJSON("vanilla-data", installID, version)
	if err != nil {
		return err
	}
	return saveCacheFile(path, &VanillaCache{
		FormatVersion: CacheFormatVersion,
		InstallID:     installID,
		GameVersion:   version,
		Defs:          defs,
	})
}

// LoadData reads the data tier for installID + version.
func LoadData(installID, version string) ([]Def, error) {
	path, err := vanillaJSON("vanilla-data", installID, version)
	if err != nil {
		return nil, err
	}
	c, err := loadCacheFile(path)
	if err != nil {
		return nil, err
	}
	return c.Defs, nil
}

// ensureData indexes the tier once, from the scan that made it or from disk; it needs no lock under the session's READ lock because sync.Once serialises the first load and orders every later reader after the finished maps.
func (m *Model) ensureData() {
	m.dataOnce.Do(func() {
		var defs []Def
		switch {
		case m.cache == nil:
		case m.cache.data != nil:
			defs = m.cache.data
		case m.cache.InstallID != "":
			// No install id means the cache was never persisted, so no tier is due.
			loaded, err := LoadData(m.cache.InstallID, m.cache.GameVersion)
			if err != nil {
				msg := err.Error()
				m.dataErr.Store(&msg)
			}
			defs = loaded
		}
		byKey := make(map[string][]Def, len(defs))
		for _, d := range defs {
			byKey[d.Key] = append(byKey[d.Key], d)
		}
		keys := make([]string, 0, len(byKey))
		for k := range byKey {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		m.dataByKey, m.dataKeys = byKey, keys
	})
}

// DataError reports why the data tier could not be read, empty when it could.
func (m *Model) DataError() string {
	if msg := m.dataErr.Load(); msg != nil {
		return *msg
	}
	return ""
}
