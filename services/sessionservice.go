// sessionservice.go is the Wails RPC shell for the session pool, install scan,
// and language health. Feature logic stays in session / catalog.

package services

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/session"
	"paradox-modding-tools/services/internal/wiki"
)

// SessionService owns the session pool and install-scan RPC.
type SessionService struct {
	Store *Store

	once sync.Once
	p    *session.Pool

	mu         sync.Mutex
	scanCancel context.CancelFunc
	// autoScanning guards the background rescan so repeated EnsureSession calls
	// — the UI polls health — do not stack scans on top of each other.
	autoScanning bool
	wikiCancel   context.CancelFunc
}

// LanguageHealth is the compact Library/IDE index status strip.
type LanguageHealth struct {
	InstallID         string `json:"installId"`
	InstallOk         bool   `json:"installOk"`
	GameVersion       string `json:"gameVersion"`
	CacheStale        bool   `json:"cacheStale"`
	ScannedAt         string `json:"scannedAt"`
	ScriptDocsEffects int    `json:"scriptDocsEffects"`
	IndexReady        bool   `json:"indexReady"`
	DefCount          int    `json:"defCount"`

	// Schema is the declared type system's state. Without it PMT still resolves
	// definitions, references, conflicts and loc coverage from the install walk;
	// scope-aware completion, typed hover and the scope diagnostics need it.
	Schema ScriptDocsHealth `json:"schema"`
}

// ScriptDocsHealth is structured state for the walkthrough, replacing the old
// free-text DumpHint so the frontend can render a step list rather than parse a
// sentence.
type ScriptDocsHealth struct {
	// Source is "live", "archive" or "missing".
	Source     string `json:"source"`
	Effects    int    `json:"effects"`
	Triggers   int    `json:"triggers"`
	ScopeTypes int    `json:"scopeTypes"`
	Prefixes   int    `json:"prefixes"`
	OnActions  int    `json:"onActions"`
	ReadAt     string `json:"readAt,omitempty"`
	// Archived reports PMT holds its own copy, so deleting the game's costs
	// nothing and a rescan still has a type system.
	Archived bool `json:"archived"`
	// Stale means the install was patched after these dumps were written, so the
	// engine API they describe may no longer match the game.
	Stale bool `json:"stale"`

	// The walkthrough, verified against all three game binaries: the same
	// launch option and console command everywhere, only the folder differs.
	Folder       string `json:"folder,omitempty"`
	LaunchOption string `json:"launchOption"`
	Command      string `json:"command"`
}

func recoverErr(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%v\n%s", r, debug.Stack())
	}
}

func withSession[T any](s *SessionService, workspaceID string, fn func(*session.Session) T) (v T, err error) {
	defer recoverErr(&err)
	sess, e := s.sess(workspaceID)
	if e != nil {
		err = e
		return
	}
	return fn(sess), nil
}

func withSessionDo(s *SessionService, workspaceID string, fn func(*session.Session)) error {
	_, err := withSession(s, workspaceID, func(sess *session.Session) struct{} {
		fn(sess)
		return struct{}{}
	})
	return err
}

func (s *SessionService) pool() *session.Pool {
	s.once.Do(func() {
		s.p = session.NewPool(s.buildSession, emitLang)
	})
	return s.p
}

func emitLang(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}

func (s *SessionService) buildSession(id string) (*session.Session, error) {
	var ws Workspace
	var inst *GameInstall
	var found bool
	s.Store.Read(func(c *Config) {
		if w := findWorkspace(c, id); w != nil {
			ws = cloneWorkspace(*w)
			found = true
			if w.InstallID != "" {
				if i := findInstall(c, w.InstallID); i != nil {
					cp := *i
					inst = &cp
				}
			}
		}
	})
	if !found {
		return nil, fmt.Errorf("workspace not found")
	}
	lang := ws.DefaultLocLang
	if lang == "" {
		lang = "english"
	}
	var cache *catalog.VanillaCache
	var vloc *catalog.VanillaLoc
	if inst != nil {
		ver := inst.Version
		if ver == "" {
			ver = "latest"
		}
		// Stale formatVersion discards the file (no migration). Do not fail
		// the session — that leaves the IDE unmounted until a UI reload.
		cache, _ = catalog.LoadCache(inst.ID, ver)
		vloc, _ = catalog.LoadVanillaLoc(inst.ID, ver, lang)
	}
	inputs := make([]catalog.ModInput, 0, len(ws.Mods))
	mods := append([]WorkspaceMod(nil), ws.Mods...)
	sortWorkspaceMods(mods)
	for i, m := range mods {
		if m.IsBroken {
			continue
		}
		inputs = append(inputs, catalog.ModInput{
			Origin: m.ID, Root: m.Path, Order: i, Name: m.Name,
			Calendar: catalog.ModCalendar{Era: m.Era, EpochOffset: m.EpochOffset},
		})
	}
	sess := session.NewWithLoc(id, ws.GameID, lang, cache, vloc, inputs)
	sess.SetFileNotify(func(path string, deleted bool) {
		emitLang("fs:changed", map[string]any{"path": path, "deleted": deleted})
	})
	_ = sess.StartWatch()
	_, _ = wiki.LoadGuides(ws.GameID)
	return sess, nil
}

func (s *SessionService) sess(id string) (*session.Session, error) {
	return s.pool().EnsureSession(id)
}

// EnsureSession builds or returns the live session and its language health.
func (s *SessionService) EnsureSession(workspaceID string) (_ *LanguageHealth, err error) {
	defer recoverErr(&err)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if _, err = s.sess(workspaceID); err != nil {
		return nil, err
	}
	h, err := s.languageHealth(workspaceID)
	if err == nil {
		s.autoRescan(h)
	}
	return h, err
}

// autoRescan starts an install scan in the background when the cached semantics
// are missing or predate the installed game version.
//
// The session already tolerates a nil or stale cache and serves degraded
// results, so the user keeps working while this runs; progress reaches the UI
// through the same lang:scan-progress event the manual button uses, and the
// button stays as an override. Only one scan runs at a time — scanning is the
// largest allocation in the app, and a second one while a session is live would
// double it.
func (s *SessionService) autoRescan(h *LanguageHealth) {
	if h == nil || h.InstallID == "" || !h.InstallOk {
		return
	}
	// A scan is worth starting when there is no cache at all, or when the cache
	// was built against a different game version. Anything else — a stale
	// script_docs dump, a mod edit — is not a reason to walk the install.
	if h.ScannedAt != "" && !h.CacheStale {
		return
	}
	s.mu.Lock()
	if s.autoScanning || s.scanCancel != nil {
		s.mu.Unlock()
		return
	}
	s.autoScanning = true
	s.mu.Unlock()

	installID := h.InstallID
	go func() {
		defer func() {
			s.mu.Lock()
			s.autoScanning = false
			s.mu.Unlock()
		}()
		// Errors are deliberately swallowed: this was not asked for, so a
		// failure must not surface as one. The health strip already reports the
		// cache as stale, and the manual button remains.
		_, _ = s.RebuildInstallSemantics(installID)
	}()
}

func (s *SessionService) cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scanCancel != nil {
		s.scanCancel()
		s.scanCancel = nil
	}
}

// RebuildInstallSemantics scans an install into VanillaCache + loc sidecar.
func (s *SessionService) RebuildInstallSemantics(installID string) (_ *catalog.VanillaCache, err error) {
	defer recoverErr(&err)
	var inst GameInstall
	var ok bool
	s.Store.Read(func(c *Config) {
		if found := findInstall(c, installID); found != nil {
			inst, ok = *found, true
		}
	})
	if !ok {
		return nil, fmt.Errorf("install not found")
	}
	ver := inst.Version
	if ver == "" {
		ver = "latest"
	}
	s.cancel()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.scanCancel = cancel
	s.mu.Unlock()
	defer cancel()

	c, vloc, err := catalog.Scan(ctx, catalog.ScanRequest{
		InstallID: inst.ID, GameID: inst.GameID, InstallPath: inst.Path,
		Version: ver, LocLang: "english",
		OnProgress: func(pct int, msg string) {
			if pct > 80 {
				pct = 80
			}
			emitLang("lang:scan-progress", map[string]any{
				"installId": installID,
				"pct":       pct,
				"msg":       msg,
			})
		},
	})
	if err != nil {
		return nil, err
	}
	if err := catalog.SaveCache(c); err != nil {
		return nil, err
	}
	if err := catalog.SaveVanillaLoc(inst.ID, ver, "english", vloc); err != nil {
		return nil, err
	}
	detected := game.ReadGameVersion(inst.Path)
	_ = s.Store.Mutate(func(cfg *Config) error {
		if found := findInstall(cfg, installID); found != nil {
			found.VersionDetected = detected
		}
		return nil
	})

	wikiVer := inst.Version
	if wikiVer == "" || strings.EqualFold(wikiVer, "latest") {
		wikiVer = detected
		if wikiVer == "" {
			wikiVer = inst.VersionDetected
		}
	}
	if wiki.Needed(inst.GameID, wikiVer) {
		if !wiki.HasSidecar(inst.GameID) {
			emitLang("wiki:first", map[string]any{"gameId": inst.GameID})
		}
		werr := wiki.RefreshIfNeeded(ctx, inst.GameID, wikiVer, func(done, total int, kind string) {
			pct := 80
			if total > 0 {
				pct = 80 + 20*done/total
			}
			emitLang("lang:scan-progress", map[string]any{
				"installId": installID,
				"pct":       pct,
				"msg":       fmt.Sprintf("Wiki %s %d/%d", kind, done, total),
			})
		})
		wmsg := ""
		if werr != nil {
			wmsg = clipMsg(werr.Error(), 120)
			emitLang("lang:scan-progress", map[string]any{
				"installId": installID,
				"pct":       100,
				"msg":       wmsg,
			})
		}
		emitLang("wiki:updated", map[string]any{"gameId": inst.GameID, "ok": werr == nil, "err": wmsg})
	}

	var workspaceIDs []string
	s.Store.Read(func(cfg *Config) {
		for _, ws := range cfg.Workspaces {
			if ws.InstallID == installID {
				workspaceIDs = append(workspaceIDs, ws.ID)
			}
		}
	})
	for _, wsID := range workspaceIDs {
		live := s.pool().Get(wsID)
		if live == nil {
			continue
		}
		live.ReplaceCache(c)
		lang := live.DefaultLang()
		sloc := vloc
		if lang != "english" {
			sloc, _ = loadVanillaLoc(ctx, inst.ID, inst.GameID, inst.Path, ver, lang)
		}
		live.ReplaceVanillaLoc(sloc)
	}
	emitLang("lang:cache-updated", map[string]any{"installId": installID})
	return c, nil
}

// GetLanguageHealth returns install/cache/session status for the workspace.
func (s *SessionService) GetLanguageHealth(workspaceID string) (*LanguageHealth, error) {
	return s.languageHealth(workspaceID)
}

func (s *SessionService) languageHealth(workspaceID string) (*LanguageHealth, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	h := &LanguageHealth{}
	var inst *GameInstall
	var found bool
	s.Store.Read(func(c *Config) {
		if w := findWorkspace(c, workspaceID); w != nil {
			found = true
			if w.InstallID != "" {
				if i := findInstall(c, w.InstallID); i != nil {
					cp := *i
					inst = &cp
				}
			}
		}
	})
	if !found {
		return nil, fmt.Errorf("workspace not found")
	}
	var liveDetected string
	if inst != nil {
		h.InstallID = inst.ID
		h.GameVersion = inst.Version
		liveDetected = game.ReadGameVersion(inst.Path)
		if fi, e := os.Stat(inst.Path); e == nil && fi.IsDir() {
			h.InstallOk = true
		}
	}
	live := s.pool().Get(workspaceID)
	if live != nil {
		h.IndexReady = true
		h.DefCount = live.DefCount()
		_, scanned, ver, _ := live.CacheInfo()
		if scanned != "" {
			h.ScannedAt = scanned
		}
		if ver != "" && h.GameVersion != "" {
			h.CacheStale = ver != h.GameVersion
		}
	}
	if inst == nil {
		return h, nil
	}
	ver := h.GameVersion
	if ver == "" {
		ver = "latest"
	}
	// The type system is read straight from script_docs rather than out of the
	// model: a couple of megabytes instead of ninety, and health is polled.
	st := catalog.ReadSchemaStatus(inst.GameID, inst.ID, inst.Path, ver)
	h.Schema = ScriptDocsHealth{
		Source: string(st.Source), Effects: st.Effects, Triggers: st.Triggers,
		ScopeTypes: st.ScopeTypes, Prefixes: st.Prefixes, OnActions: st.OnActions,
		ReadAt: st.ReadAt, Archived: st.Archived, Folder: st.Folder,
		Stale:        dumpsPredateInstall(st.ReadAt, inst.Path),
		LaunchOption: "-debug_mode",
		Command:      "script_docs",
	}
	h.ScriptDocsEffects = st.Effects

	if live == nil || h.ScannedAt == "" {
		if c, e := catalog.LoadCache(inst.ID, ver); e == nil && c != nil {
			if h.ScannedAt == "" {
				h.ScannedAt = c.ScannedAt
			}
			if live == nil {
				h.CacheStale = c.GameVersion != h.GameVersion && h.GameVersion != ""
			}
		}
	}
	if liveDetected != "" && inst.VersionDetected != "" &&
		liveDetected != inst.VersionDetected {
		h.CacheStale = true
	}
	return h, nil
}

// dumpsPredateInstall reports that the game was patched after the dumps were
// written. The dumps record no version of their own, so this compares
// timestamps: the newest dump mtime against when the install was last patched.
// An unknown install time means "cannot tell", never stale.
func dumpsPredateInstall(readAt, installPath string) bool {
	if readAt == "" {
		return false
	}
	wrote, err := time.Parse(time.RFC3339, readAt)
	if err != nil {
		return false
	}
	patched := game.InstallUpdatedAt(installPath)
	return !patched.IsZero() && patched.After(wrote)
}

// loadVanillaLoc returns the loc sidecar, harvesting and saving if it is missing.
func loadVanillaLoc(
	ctx context.Context, installID, gameID, installPath, version, lang string,
) (*catalog.VanillaLoc, error) {
	if version == "" {
		version = "latest"
	}
	if lang == "" {
		lang = "english"
	}
	if v, e := catalog.LoadVanillaLoc(installID, version, lang); e == nil {
		return v, nil
	}
	v, err := catalog.HarvestLoc(ctx, gameID, installPath, lang)
	if err != nil {
		return nil, err
	}
	_ = catalog.SaveVanillaLoc(installID, version, lang, v)
	return v, nil
}

// StartWikiWarm runs a silent incremental wiki refresh for sidecars already on disk.
func (s *SessionService) StartWikiWarm(version string) {
	wiki.SetUserAgent(version)
	s.mu.Lock()
	if s.wikiCancel != nil {
		s.wikiCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.wikiCancel = cancel
	s.mu.Unlock()
	go wiki.RefreshExisting(ctx, func(gameID string, err error) {
		msg := ""
		if err != nil {
			msg = clipMsg(err.Error(), 120)
		}
		emitLang("wiki:updated", map[string]any{"gameId": gameID, "ok": err == nil, "err": msg})
	})
}

func clipMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
