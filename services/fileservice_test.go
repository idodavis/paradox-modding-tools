// fileservice_test.go covers explorer listing and the two byte readers: raw for
// images, editor-only for text.

package services

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirectoryMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &FileService{}
	hidden, err := fs.ListDirectory(root, false)
	if err != nil {
		t.Fatal(err)
	}
	names := dirNames(hidden)
	if !names[".metadata"] || !names["a.txt"] || names[".git"] {
		t.Fatalf("hide binaries list=%v", names)
	}
	all, err := fs.ListDirectory(root, true)
	if err != nil {
		t.Fatal(err)
	}
	names = dirNames(all)
	if !names[".git"] || !names[".metadata"] {
		t.Fatalf("show all list=%v", names)
	}
}

// Only the editor bridge refuses binary content; it is the one caller that puts
// the bytes in a text editor.
func TestReadEditableBase64RejectsBinary(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "x.bin")
	raw := make([]byte, 16)
	raw[3] = 0
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*FileService)(nil).ReadEditableBase64(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reject == "" || !strings.Contains(got.Reject, "binary") || got.B64 != "" {
		t.Fatalf("got=%+v", got)
	}
}

// ReadFileBase64 is how mod thumbnails and workshop previews reach the webview,
// so binary content must come back whole. A PNG carries its IHDR chunk length
// (00 00 00 0D) at byte 8, so sharing the editor's NUL-byte guard rejected every
// thumbnail and the explorer's per-mod icons silently disappeared.
func TestReadFileBase64KeepsImageBytes(t *testing.T) {
	t.Parallel()
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
	}
	p := filepath.Join(t.TempDir(), "thumbnail.png")
	if err := os.WriteFile(p, png, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*FileService)(nil).ReadFileBase64(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || got.Reject != "" {
		t.Fatalf("got=%+v", got)
	}
	back, err := base64.StdEncoding.DecodeString(got.B64)
	if err != nil || !bytes.Equal(back, png) {
		t.Fatalf("round trip = %v (err %v), want the PNG bytes", back, err)
	}
}

func TestReadFileBase64Text(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*FileService)(nil).ReadFileBase64(p)
	if err != nil || !got.Exists || got.Reject != "" || got.B64 == "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestReadFileBase64RejectsLarge(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "big.txt")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOpenBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := (*FileService)(nil).ReadFileBase64(p)
	if err != nil || got.Reject == "" || !strings.Contains(got.Reject, "too large") ||
		got.B64 != "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func dirNames(ents []DirEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range ents {
		out[e.Name] = true
	}
	return out
}

// The IDE bridge refuses everything binary, but names images separately so the
// caller can offer a preview instead of an error. It cannot simply open them:
// VS Code renders images in a webview custom editor, and monaco-vscode stubs the
// entire webview stack as unsupported.
//
// Detection is by content, not extension: a .dds renamed to .png is still a .dds.
func TestReadEditableBase64NamesImagesSeparately(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		head  []byte
		image bool
	}{
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D}, true},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, true},
		{"gif", append([]byte("GIF89a"), 0, 0, 0), true},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0), true},
		{"bmp", append([]byte("BM"), 0, 0, 0, 0), true},
		{"ico", []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00}, true},
		// Paradox ships a lot of these; no browser decodes either.
		{"dds", append([]byte("DDS "), 0, 0, 0, 0x7C), false},
		{"tga", []byte{0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00}, false},
		{"exe", append([]byte("MZ"), 0, 0, 0, 0), false},
		{"arbitrary", []byte{0x13, 0x37, 0x00, 0x42}, false},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), c.name+".bin")
		if err := os.WriteFile(p, c.head, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := (*FileService)(nil).ReadEditableBase64(p)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case got.Reject == "":
			t.Errorf("%s: opened in the editor; nothing binary may be", c.name)
		case c.image && got.Reject != RejectImage:
			t.Errorf("%s: reject %q, want %q so the UI can offer a preview",
				c.name, got.Reject, RejectImage)
		case !c.image && got.Reject == RejectImage:
			t.Errorf("%s: named an image, but nothing here can display it", c.name)
		}
		// Whatever the editor refuses, the raw reader still returns whole.
		raw, err := (*FileService)(nil).ReadFileBase64(p)
		if err != nil {
			t.Fatal(err)
		}
		back, derr := base64.StdEncoding.DecodeString(raw.B64)
		if raw.Reject != "" || derr != nil || !bytes.Equal(back, c.head) {
			t.Errorf("%s: raw read did not round-trip (%+v)", c.name, raw)
		}
	}
	// Every one of these still reaches the webview through the raw reader.
	p := filepath.Join(t.TempDir(), "x.dds")
	if err := os.WriteFile(p, append([]byte("DDS "), 0, 0, 0, 0x7C), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := (*FileService)(nil).ReadFileBase64(p); got.Reject != "" || got.B64 == "" {
		t.Errorf("raw reader must not refuse a dds: %+v", got)
	}
}

// TestWriteFileBase64IsAtomic pins that the editor's save path never truncates
// the file it is replacing. This is the one file PMT writes that a user cannot
// regenerate; every derived cache already wrote temp-then-rename.
func TestWriteFileBase64IsAtomic(t *testing.T) {
	f := &FileService{}
	dir := t.TempDir()
	path := filepath.Join(dir, "00_traits.txt")

	if err := f.WriteFileBase64(path, base64.StdEncoding.EncodeToString([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFileBase64(path, base64.StdEncoding.EncodeToString([]byte("second"))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want %q", got, "second")
	}

	// No temp files survive a successful write; a stray .pmt* beside the user's
	// script would show up in the explorer and in a published mod.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Fatalf("want only the target file, got %v", names)
	}

	// A failed write leaves the previous contents intact rather than truncating.
	if err := f.WriteFileBase64(path, "not-valid-base64!!"); err == nil {
		t.Fatal("want a decode error")
	}
	if got, _ := os.ReadFile(path); string(got) != "second" {
		t.Fatalf("a failed write damaged the file: %q", got)
	}
}
