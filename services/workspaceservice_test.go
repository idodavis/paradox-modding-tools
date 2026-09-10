// workspaceservice_test.go covers mod add/reorder/remove and workspace prefs.

package services

import (
	"os"
	"path/filepath"
	"testing"
)

func testWorkspaceService(t *testing.T) *WorkspaceService {
	t.Helper()
	return &WorkspaceService{
		Store: newStore(filepath.Join(t.TempDir(), "config.json")),
	}
}

func seedWorkspace(t *testing.T, s *Store, id, installID string) {
	t.Helper()
	if err := s.Mutate(func(c *Config) error {
		c.Workspaces = append(c.Workspaces, Workspace{
			ID: id, GameID: "ck3", Name: "Test", InstallID: installID,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceService_AddReorderRemoveMods(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	seedWorkspace(t, svc.Store, "ws1", "inst1")
	modDir := t.TempDir()

	a, err := svc.AddWorkspaceMod("ws1", "Alpha", filepath.Join(modDir, "a"), "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddWorkspaceMod("ws1", "Beta", filepath.Join(modDir, "b"), "")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	listed := ws.Mods
	if len(listed) != 2 || listed[0].ID != a.ID || listed[1].ID != b.ID {
		t.Fatalf("initial order: %+v", listed)
	}
	if listed[0].SortOrder != 0 || listed[1].SortOrder != 1 {
		t.Fatalf("sort orders: %d %d", listed[0].SortOrder, listed[1].SortOrder)
	}

	if err := svc.ReorderWorkspaceMods("ws1", []string{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	ws, err = svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	listed = ws.Mods
	if len(listed) != 2 || listed[0].ID != b.ID || listed[1].ID != a.ID {
		t.Fatalf("reordered: %+v", listed)
	}
	all, err := svc.ListWorkspaces("")
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %v %+v", err, all)
	}
	fromList := all[0].Mods
	if len(fromList) != 2 || fromList[0].ID != b.ID || fromList[1].ID != a.ID {
		t.Fatalf("list order: %+v", fromList)
	}

	if err := svc.RemoveWorkspaceMod("ws1", b.ID); err != nil {
		t.Fatal(err)
	}
	ws, err = svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	listed = ws.Mods
	if len(listed) != 1 || listed[0].ID != a.ID {
		t.Fatalf("after remove: %+v", listed)
	}
}

func TestWorkspaceService_ReorderIncomplete(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	seedWorkspace(t, svc.Store, "ws1", "")
	a, err := svc.AddWorkspaceMod("ws1", "A", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", "B", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReorderWorkspaceMods("ws1", []string{a.ID}); err == nil {
		t.Fatal("want incomplete-set error")
	}
}

func TestWorkspaceService_PrefsAndIdeSession(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	seedWorkspace(t, svc.Store, "ws1", "inst1")

	if err := svc.UpdateWorkspacePrefs("ws1", true, "event-graph", "", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "not-a-tool", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "conflicts", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", true, "health", "", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveIdeSession("ws1", []string{"/a.txt"}, "/a.txt"); err != nil {
		t.Fatal(err)
	}
	ws, err := svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	if !ws.ResetIdeOnOpen || ws.DefaultTool != "health" || ws.HideExplorerBinaries {
		t.Fatalf("prefs: %+v", ws)
	}
	if len(ws.IdeOpenFiles) != 1 || ws.IdeActiveFile != "/a.txt" {
		t.Fatalf("ide session: %+v", ws)
	}
}

func TestGetIdeRoots_OrderAndGameTitle(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	install := t.TempDir()
	modDir := t.TempDir()
	if err := svc.Store.Mutate(func(c *Config) error {
		c.Installs = append(c.Installs, GameInstall{
			ID: "inst1", GameID: "ck3", Name: "My CK3 nickname", Path: install,
		})
		c.Workspaces = append(c.Workspaces, Workspace{
			ID: "ws1", GameID: "ck3", Name: "Test",
			InstallID: "inst1",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", "Alpha", modDir, ""); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetIdeRoots("ws1")
	if err != nil {
		t.Fatal(err)
	}
	roots := got.Roots
	if len(roots) != 2 {
		t.Fatalf("len %d %+v", len(roots), roots)
	}
	if roots[0].Kind != "mod" || roots[1].Kind != "game" {
		t.Fatalf("kinds %+v", roots)
	}
	if roots[1].Label != "Crusader Kings III" || roots[1].Origin != "vanilla" {
		t.Fatalf("game root %+v", roots[1])
	}
}

func TestGetIdeRoots_EmptyID(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	if _, err := svc.GetIdeRoots(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkspaceService_DeleteWorkspace(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	if err := svc.DeleteWorkspace(""); err == nil {
		t.Fatal("empty id")
	}
	seedWorkspace(t, svc.Store, "ws1", "inst1")
	if _, err := svc.AddWorkspaceMod("ws1", "A", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", "B", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteWorkspace("ws1"); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListWorkspaces("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed: %+v", listed)
	}
	if err := svc.DeleteWorkspace("ws1"); err == nil {
		t.Fatal("want not found")
	}
}

func TestWorkspaceService_CreateMod(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	parent := t.TempDir()
	root, err := svc.CreateMod("ck3", parent, "Hello Mod", "english", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "descriptor.mod")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMod("ck3", parent, "Hello Mod", "english", "", "", ""); err == nil {
		t.Fatal("want already-exists")
	}
}

func TestWorkspaceService_CountWorkspacesUsingInstall(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	seedWorkspace(t, svc.Store, "ws1", "inst1")
	seedWorkspace(t, svc.Store, "ws2", "inst1")
	seedWorkspace(t, svc.Store, "ws3", "inst2")
	got := svc.CountWorkspacesUsingInstall("inst1")
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

// An empty thumbnail is discovered from the mod on disk, per game.
//
// The Create-mod form used to compute `<root>/thumbnail<ext>` itself. That was a
// second copy of a rule that has since become per-game — EU5 keeps the file in
// .metadata — and GameInfo.ThumbnailDir is backend-only, so the frontend could
// not have computed it even in principle. An EU5 mod created in the app stored a
// path to a file that was not there, and its card showed no image.
func TestAddWorkspaceModDiscoversTheThumbnail(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ game, rel string }{
		{"ck3", "thumbnail.png"},
		{"vic3", "thumbnail.png"},
		{"eu5", ".metadata/thumbnail.png"},
		// A mod that put it somewhere else keeps its own choice.
		{"eu5", "thumbnail.gif"},
	} {
		t.Run(c.game+"/"+c.rel, func(t *testing.T) {
			svc := testWorkspaceService(t)
			if err := svc.Store.Mutate(func(cfg *Config) error {
				cfg.Workspaces = append(cfg.Workspaces, Workspace{
					ID: "ws", GameID: c.game, Name: "T",
				})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			abs := filepath.Join(root, filepath.FromSlash(c.rel))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte("img"), 0o644); err != nil {
				t.Fatal(err)
			}
			mod, err := svc.AddWorkspaceMod("ws", "M", root, "")
			if err != nil {
				t.Fatal(err)
			}
			if mod.Thumbnail != abs {
				t.Errorf("Thumbnail = %q, want %q", mod.Thumbnail, abs)
			}
		})
	}
}

// A mod with no thumbnail must not be given a path to one that is not there.
func TestAddWorkspaceModLeavesThumbnailEmptyWhenThereIsNone(t *testing.T) {
	t.Parallel()
	svc := testWorkspaceService(t)
	seedWorkspace(t, svc.Store, "ws", "inst")
	mod, err := svc.AddWorkspaceMod("ws", "M", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if mod.Thumbnail != "" {
		t.Errorf("Thumbnail = %q, want empty", mod.Thumbnail)
	}
}
