// local_test.go covers the write that leaves no half-file and no leftovers.

package appdata

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")

	if err := Write(path, func(out io.Writer) error { _, err := io.WriteString(out, "first"); return err }); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, func(out io.Writer) error { _, err := io.WriteString(out, "second"); return err }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("got %q, want second", got)
	}

	// the directory holds the file and nothing else: the temporary is gone
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f.txt" {
		t.Fatalf("leftovers: %v", entries)
	}

	// a failing fill keeps the previous file and leaves nothing behind
	boom := errors.New("boom")
	if err := Write(path, func(out io.Writer) error {
		io.WriteString(out, "half")
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("the old file was lost: %q", got)
	}
	entries, _ = os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("a failed write left something behind: %v", entries)
	}
}
