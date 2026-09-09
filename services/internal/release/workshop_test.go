package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseYouTubeID(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"raw", id, true},
		{"watch", "https://www.youtube.com/watch?v=" + id, true},
		{"watch t", "https://youtube.com/watch?v=" + id + "&t=12", true},
		{"embed", "https://www.youtube.com/embed/" + id, true},
		{"short", "https://youtu.be/" + id, true},
		{"shorts", "https://www.youtube.com/shorts/" + id, true},
		{"mobile", "https://m.youtube.com/watch?v=" + id, true},
		{"bad", "nope", false},
		{"http img", "https://example.com/a.png", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseYouTubeID(tc.in)
			if tc.ok {
				if err != nil || got != id {
					t.Fatalf("got %q %v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got %q", got)
			}
		})
	}
}

func TestCopyModSkipsGit(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyMod(src, dest, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Fatal("copied .git")
	}
}

// linkDir points link at target, by whatever mechanism this machine allows.
//
// os.Symlink to a directory needs SeCreateSymbolicLink on Windows, which an
// ordinary account does not hold, so these tests skipped on the platform most
// PMT users are on — and the bug they cover is one a publish hits. A junction
// needs no privilege and Go reports it as ModeSymlink, which is the case
// CopyMod actually has to handle.
func linkDir(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS != "windows" {
		t.Skipf("cannot link %s -> %s", link, target)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("no symlink privilege and mklink /J failed: %v: %s", err, out)
	}
}

// One symlinked folder anywhere in a mod tree used to abort the whole publish:
// WalkDir calls a symlink a file, CopyFile's os.Open follows it to a directory,
// and io.Copy returns EISDIR straight out of the walk.
func TestCopyModFollowsLinkedDirectories(t *testing.T) {
	src, dest, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(
		filepath.Join(outside, "shared.txt"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir(t, outside, filepath.Join(src, "assets"))
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyMod(src, dest, ""); err != nil {
		t.Fatalf("a linked folder aborted the publish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Error("ordinary file missing:", err)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "assets", "shared.txt"))
	if err != nil {
		t.Fatalf("linked folder's contents not copied: %v", err)
	}
	if string(raw) != "shared" {
		t.Errorf("contents = %q, want shared", raw)
	}
}

// A link pointing at its own ancestor must terminate, not recurse forever.
func TestCopyModStopsAtALinkCycle(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir(t, src, filepath.Join(src, "loop"))
	done := make(chan error, 1)
	go func() { done <- CopyMod(src, dest, "") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CopyMod: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("CopyMod did not terminate on a self-referential link")
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Error("ordinary file missing:", err)
	}
}

func TestCopyModIgnoreAndMetadata(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skip.log"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(src, ".metadata")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "metadata.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "secret.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyMod(src, dest, "secret.txt\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".metadata", "metadata.json")); err != nil {
		t.Fatal("want .metadata")
	}
	if _, err := os.Stat(filepath.Join(dest, "skip.log")); err == nil {
		t.Fatal("copied gitignored log")
	}
	if _, err := os.Stat(filepath.Join(dest, ".gitignore")); err == nil {
		t.Fatal("copied .gitignore")
	}
	if _, err := os.Stat(filepath.Join(dest, "secret.txt")); err == nil {
		t.Fatal("copied extra-ignore file")
	}
}

func TestAddRemovePreview(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddPreview(root, src); err != nil {
		t.Fatal(err)
	}
	previews := ScanPreviews(root)
	if len(previews) != 1 || previews[0].Kind != "image" {
		t.Fatalf("%+v", previews)
	}
	rel := previews[0].Rel
	if !strings.HasPrefix(rel, previewDir+"/") {
		t.Fatalf("rel %s", rel)
	}
	if err := RemovePreview(root, rel); err != nil {
		t.Fatal(err)
	}
	if len(ScanPreviews(root)) != 0 {
		t.Fatal("preview not removed")
	}
}

func TestAddRemoveVideo(t *testing.T) {
	root := t.TempDir()
	const id = "dQw4w9WgXcQ"
	if err := AddVideo(root, "https://youtu.be/"+id); err != nil {
		t.Fatal(err)
	}
	previews := ScanPreviews(root)
	if len(previews) != 1 || previews[0].ID != id {
		t.Fatalf("%+v", previews)
	}
	if err := AddVideo(root, id); err != nil {
		t.Fatal(err)
	}
	if len(ScanPreviews(root)) != 1 {
		t.Fatal("duplicate video added")
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(videosRel)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != id {
		t.Fatalf("%q", raw)
	}
	if err := RemoveVideo(root, id); err != nil {
		t.Fatal(err)
	}
	if len(ScanPreviews(root)) != 0 {
		t.Fatal("video not removed")
	}
}

func TestStagedPreviewFilesDedupe(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workshop", "previews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	previews := []Preview{
		{Kind: "image", Rel: "workshop/previews/a.png"},
		{Kind: "image", Rel: "workshop/previews/a.png"},
	}
	got := StagedPreviewFiles(root, previews)
	if len(got) != 1 {
		t.Fatalf("want 1 path, got %v", got)
	}
}

func TestVideoIDsDedupe(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	got := VideoIDs([]Preview{
		{Kind: "youtube", ID: id},
		{Kind: "youtube", ID: id},
	})
	if len(got) != 1 || got[0] != id {
		t.Fatalf("%v", got)
	}
}

func TestWorkshopExtrasCap(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	for i := 0; i < maxWorkshopExtras; i++ {
		src := filepath.Join(dir, "shot-"+string(rune('a'+i))+".png")
		if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := AddPreview(root, src); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(dir, "overflow.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddPreview(root, src); err == nil {
		t.Fatal("want cap error")
	}
	if err := AddVideo(root, "dQw4w9WgXcQ"); err == nil {
		t.Fatal("want cap error")
	}
}
