// steam_test.go covers the Markdown and BBCode conversions both ways, the advice for a known
// EResult, the gate over the real installs (each game's copy of Steam's library opens and binds
// every function a publish calls), the YouTube ids a user pastes, what the upload copy keeps,
// the listing's extra images and videos, and the descriptions written only when missing.

package steam

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"paradox-modding-tools/services/internal/game"
)

func TestMarkdownToBBCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		note string
		back bool // BBCodeToMarkdown reads want back as in
	}{
		{"h1", "# Title\n", "[h1]Title[/h1]\n", "", true},
		{"h2", "## Title\n", "[h2]Title[/h2]\n", "", false},
		{"h3", "### Title\n", "[h3]Title[/h3]\n", "", false},
		{"h4", "#### Deep\n", "[b]Deep[/b]\n", "", false},
		{"bold", "**x**\n", "[b]x[/b]\n", "", true},
		{"italic", "*x*\n", "[i]x[/i]\n", "", true},
		{"strike", "~~x~~\n", "[strike]x[/strike]\n", "", true},
		{"link", "[t](https://e)\n", "[url=https://e]t[/url]\n", "", true},
		{"img", "![](https://e/a.png)\n", "[img]https://e/a.png[/img]\n", "", false},
		{"img http", "![](http://e/a.png)\n", "[img]http://e/a.png[/img]\n", "", false},
		{"img local", "![](thumbnail.png)\n", "",
			"images need a public http(s) URL for Steam; local paths dropped", false},
		{"img data", "![](data:image/png;base64,xx)\n", "",
			"images need a public http(s) URL for Steam; local paths dropped", false},
		{"ul", "- a\n- b\n", "[list][*]a[*]b[/list]\n", "", true},
		{"ol", "1. a\n2. b\n", "[olist][*]a[*]b[/olist]\n", "", false},
		{"quote", "> hi\n", "[quote]hi[/quote]\n", "", false},
		{"code", "```\nfoo\n```\n", "[code]foo\n[/code]\n", "", false},
		{"inline", "use `x` please\n", "use [b]x[/b] please\n", "", false},
		{"hr", "---\n", "[hr][/hr]\n", "", false},
		{"table", "| a | b |\n| - | - |\n| 1 | 2 |\n", "", "tables dropped", false},
		{"paragraphs", "# Hello\n\n**bold** and *i* and ~~s~~\n\n[link](https://e)\n\n- a\n- b\n",
			"[h1]Hello[/h1]\n\n[b]bold[/b] and [i]i[/i] and [strike]s[/strike]\n\n[url=https://e]link[/url]\n\n[list][*]a[*]b[/list]\n", "", true},
		{"unknown tag", "keep [color=red]x[/color] please\n", "keep [color=red]x[/color] please\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MarkdownToBBCode(tc.in)
			if strings.TrimSpace(got.Text) != strings.TrimSpace(tc.want) {
				t.Fatalf("text:\n got %q\nwant %q", got.Text, tc.want)
			}
			if tc.note != "" && !slices.Contains(got.Notes, tc.note) {
				t.Fatalf("notes %v want %q", got.Notes, tc.note)
			}
			if back := BBCodeToMarkdown(got.Text); tc.back && strings.TrimSpace(back.Text) != strings.TrimSpace(tc.in) {
				t.Fatalf("read back:\n got %q\nwant %q", back.Text, tc.in)
			}
		})
	}
}

func TestEResultHint(t *testing.T) {
	if hint := eresultHint("SubmitItemUpdate EResult 9"); !strings.Contains(hint, "EResult 9") {
		t.Errorf("eresult 9: %q", hint)
	}
	if hint := eresultHint("SubmitItemUpdate EResult 25"); !strings.Contains(hint, "1 MB") {
		t.Errorf("eresult 25: %q", hint)
	}
	for _, failure := range []string{"SubmitItemUpdate EResult 2", "no code here", "EResult "} {
		if hint := eresultHint(failure); hint != "" {
			t.Errorf("%q has no advice, got %q", failure, hint)
		}
	}
}

// TestInstallsBindSteam is the gate over the real installs: each game's copy of Steam's library
// is where the game table says, and binds every function a publish calls, init included.
// Nothing is called, so Steam need not run. Set PMT_TEST_INSTALL_*, each a game's root folder.
func TestInstallsBindSteam(t *testing.T) {
	if testing.Short() {
		t.Skip("real installs")
	}
	// the one result struct the platforms pack differently must match Steam's size, or
	// GetAPICallResult refuses it
	want := map[string]uintptr{"windows": 24}[runtime.GOOS]
	if want == 0 {
		want = 16
	}
	if size := unsafe.Sizeof(createItemResult{}); size != want {
		t.Errorf("CreateItemResult_t is %d bytes here, want %d", size, want)
	}
	if size := unsafe.Sizeof(submitItemResult{}); size != 16 {
		t.Errorf("SubmitItemUpdateResult_t is %d bytes, want 16", size)
	}

	for _, id := range []string{"ck3", "vic3", "eu5"} {
		t.Run(id, func(t *testing.T) {
			dir := os.Getenv("PMT_TEST_INSTALL_" + strings.ToUpper(id))
			if dir == "" {
				t.Skip("PMT_TEST_INSTALL_" + strings.ToUpper(id) + " is not set")
			}
			info := game.Get(id)
			if _, ok := info.SteamLibrary[runtime.GOOS]; !ok {
				t.Skipf("%s has no %s build", id, runtime.GOOS)
			}
			library, err := Library(info, dir)
			if err != nil {
				t.Fatal(err)
			}
			api, err := openUGC(library)
			if err != nil {
				t.Fatal(err)
			}
			api.unload()
		})
	}
}

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
			got, err := parseYouTubeID(tc.in)
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

// linkDir points link at target, by whatever mechanism this machine allows. os.Symlink to a
// directory needs SeCreateSymbolicLink on Windows, which an ordinary account does not hold; a
// junction needs no privilege and Go reports it as ModeSymlink, the case CopyMod handles.
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

// A linked folder anywhere in a mod tree must not abort the publish: CopyMod follows it
// and copies its files.
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

func TestCopyMod(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("y"), 0o644); err != nil {
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
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Fatal("copied .git")
	}
}

func TestWorkshopExtras(t *testing.T) {
	root := t.TempDir()
	shots := t.TempDir()

	// an image is added, listed and removed
	src := filepath.Join(shots, "shot.png")
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

	// a video is added once, whether as a URL or an id, and removed
	const id = "dQw4w9WgXcQ"
	if err := AddVideo(root, "https://youtu.be/"+id); err != nil {
		t.Fatal(err)
	}
	previews = ScanPreviews(root)
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

	// past the cap neither an image nor a video is added
	for i := range maxWorkshopExtras {
		shot := filepath.Join(shots, "shot-"+string(rune('a'+i))+".png")
		if err := os.WriteFile(shot, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := AddPreview(root, shot); err != nil {
			t.Fatal(err)
		}
	}
	src = filepath.Join(shots, "overflow.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddPreview(root, src); err == nil {
		t.Fatal("want cap error")
	}
	if err := AddVideo(root, id); err == nil {
		t.Fatal("want cap error")
	}
}

func TestNewDescriptionCreatesMissingOnly(t *testing.T) {
	root := t.TempDir()
	if err := NewDescription(root, DescMdName, FromMarkdown, "M", "desc"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DescMdName), []byte("kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewDescription(root, DescMdName, FromMarkdown, "M", "other"); err == nil {
		t.Fatal("want an error for a file that exists")
	}
	md, err := os.ReadFile(filepath.Join(root, DescMdName))
	if err != nil {
		t.Fatal(err)
	}
	if string(md) != "kept\n" {
		t.Fatalf("%q", md)
	}
}
