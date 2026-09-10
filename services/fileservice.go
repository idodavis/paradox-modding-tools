// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
)

const maxOpenBytes = 8 << 20

// FileService provides filesystem helpers for the IDE workbench.
type FileService struct{}

// DirEntry is one immediate child of a directory (for lazy file trees).
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// CreateDir creates a directory (parents created as needed).
func (f *FileService) CreateDir(fullPath string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	if fullPath == "" || fullPath == "." {
		return fmt.Errorf("path is required")
	}
	return os.MkdirAll(fullPath, 0o755)
}

// RenamePath renames or moves a file or directory.
func (f *FileService) RenamePath(oldPath, newPath string) error {
	oldPath = filepath.Clean(filepath.FromSlash(oldPath))
	newPath = filepath.Clean(filepath.FromSlash(newPath))
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	return os.Rename(oldPath, newPath)
}

// DeletePath removes a file or directory recursively.
func (f *FileService) DeletePath(fullPath string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	if fullPath == "" || fullPath == "." {
		return fmt.Errorf("path is required")
	}
	return os.RemoveAll(fullPath)
}

// PathStat describes a filesystem node for the workbench FS bridge.
type PathStat struct {
	Exists  bool  `json:"exists"`
	IsDir   bool  `json:"isDir"`
	Size    int64 `json:"size"`
	MtimeMs int64 `json:"mtimeMs"`
	CtimeMs int64 `json:"ctimeMs"`
}

// StatPath returns metadata for a path (Exists=false when missing, no error).
func (f *FileService) StatPath(fullPath string) (PathStat, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PathStat{Exists: false}, nil
		}
		return PathStat{}, err
	}
	mtime := info.ModTime().UnixMilli()
	return PathStat{
		Exists:  true,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		MtimeMs: mtime,
		CtimeMs: mtime,
	}, nil
}

// FileBytes is a binary file payload for the IDE FS bridge. Missing files are
// Exists=false with no error so the workbench can skip optional paths quietly.
// Reject is set when the file exists but must not be opened (binary / too large);
// that is not a Go error so Wails does not log a binding failure.
type FileBytes struct {
	Exists bool   `json:"exists"`
	B64    string `json:"b64,omitempty"`
	Reject string `json:"reject,omitempty"`
}

// ReadFileBase64 reads raw file bytes as standard base64, or Exists=false if
// missing. Binary content is expected: this is how mod thumbnails and workshop
// previews reach the webview. Oversized files are still refused so a gfx read
// cannot pull the whole file into RAM.
func (f *FileService) ReadFileBase64(fullPath string) (FileBytes, error) {
	return f.readBase64(fullPath, false)
}

// ReadEditableBase64 is ReadFileBase64 for the IDE file bridge, which hands the
// bytes to an editor: it refuses binary the workbench has no viewer for, and
// allows the image formats it does.
//
// The two are separate because one function cannot serve both. The binary guard
// keys on a NUL byte in the first 512, and a PNG carries its IHDR chunk length
// (00 00 00 0D) at byte 8. Sharing the guard rejected every thumbnail, and the
// explorer's per-mod icons quietly disappeared.
func (f *FileService) ReadEditableBase64(fullPath string) (FileBytes, error) {
	return f.readBase64(fullPath, true)
}

func (f *FileService) readBase64(fullPath string, textOnly bool) (FileBytes, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	st, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileBytes{Exists: false}, nil
		}
		return FileBytes{}, fmt.Errorf("stat file: %w", err)
	}
	if st.IsDir() {
		return FileBytes{}, fmt.Errorf("read file: is a directory")
	}
	if msg := rejectOpen(nil, st.Size()); msg != "" {
		return FileBytes{Exists: true, Reject: msg}, nil
	}
	fh, err := os.Open(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileBytes{Exists: false}, nil
		}
		return FileBytes{}, fmt.Errorf("read file: %w", err)
	}
	defer fh.Close()
	headN := min(512, int(st.Size()))
	head := make([]byte, headN)
	if headN > 0 {
		if _, err := io.ReadFull(fh, head); err != nil {
			return FileBytes{}, fmt.Errorf("read file: %w", err)
		}
	}
	if textOnly {
		if msg := rejectBinary(head); msg != "" {
			return FileBytes{Exists: true, Reject: msg}, nil
		}
	}
	if st.Size() == 0 {
		return FileBytes{Exists: true, B64: ""}, nil
	}
	data := head
	if int64(len(head)) < st.Size() {
		data = make([]byte, st.Size())
		copy(data, head)
		if _, err := io.ReadFull(fh, data[len(head):]); err != nil {
			return FileBytes{}, fmt.Errorf("read file: %w", err)
		}
	}
	return FileBytes{Exists: true, B64: base64.StdEncoding.EncodeToString(data)}, nil
}

// rejectOpen refuses a file too large to hold in memory. It applies to every
// read, image or not.
func rejectOpen(_ []byte, size int64) string {
	if size > maxOpenBytes {
		return fmt.Sprintf("file too large to open (%d bytes, max %d)",
			size, maxOpenBytes)
	}
	return ""
}

// RejectImage is the Reject value for a file the workbench cannot open but PMT
// can display: the frontend keys on it to offer a preview instead of an error.
const RejectImage = "image file"

// rejectBinary refuses content no editor here can show. Only the editor bridge
// applies it; images fetched for display go through ReadFileBase64, which does
// not.
//
// Images are refused too, but named rather than lumped in with binary. VS Code
// shows images through a webview custom editor, and monaco-vscode stubs the
// whole webview stack as unsupported — createWebviewElement throws — so an
// image handed to the editor renders as mojibake. Distinguishing the two lets
// the caller offer something useful for the one case it can actually display.
func rejectBinary(head []byte) string {
	if bytes.IndexByte(head, 0) < 0 {
		return "" // text
	}
	if viewableImage(head) {
		return RejectImage
	}
	return fmt.Sprintf("binary file (NUL in first %d bytes)", len(head))
}

// viewableImage reports a raster format the workbench's media preview can show.
//
// Detection is by content, not by extension: the extension is a claim the file
// makes about itself, and honouring it would let anything named .png through to
// an editor that then renders it as mojibake.
//
// DDS and TGA are deliberately absent even though Paradox ships a great many of
// both. No browser decodes them, so admitting them would trade a clear "binary
// file" message for a silently broken image.
func viewableImage(head []byte) bool {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return true
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")): // JPEG
		return true
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return true
	case bytes.HasPrefix(head, []byte("RIFF")) && len(head) >= 12 &&
		bytes.Equal(head[8:12], []byte("WEBP")):
		return true
	case bytes.HasPrefix(head, []byte("BM")): // BMP
		return true
	case bytes.HasPrefix(head, []byte{0x00, 0x00, 0x01, 0x00}): // ICO
		return true
	}
	return false
}

// WriteFileBase64 writes raw bytes from standard base64.
//
// Temp-then-rename, because this is the editor's save path and the only file
// PMT writes that it cannot regenerate. A failed in-place write — disk full,
// permission lost mid-save, the process dying — truncates the modder's script.
// Every derived cache already writes this way; the irreplaceable file did not.
func (f *FileService) WriteFileBase64(fullPath, b64 string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// Same directory, so the rename stays on one filesystem and stays atomic.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(fullPath)+".pmt*")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	// Sync before rename: a rename can be durable while the bytes are not,
	// which would leave a file that exists and is empty.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	// Rename first; remove only if it fails. A Windows replace-rename returns
	// ERROR_ACCESS_DENIED when anything holds the destination open — the game,
	// another editor, an antivirus — while unlinking the name succeeds. Without
	// the fallback the save is simply refused. See services/store.go.
	if err := os.Rename(tmpName, fullPath); err == nil {
		return nil
	}
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmpName, fullPath)
}

// ListDirectory lists immediate children of dirPath (non-recursive).
// showAll lists every name; otherwise skip dotfiles except .metadata.
func (f *FileService) ListDirectory(dirPath string, showAll bool) ([]DirEntry, error) {
	dirPath = filepath.Clean(filepath.FromSlash(dirPath))
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if !showAll && strings.HasPrefix(name, ".") && name != game.MetadataDir {
			continue
		}
		out = append(out, DirEntry{
			Name: name, IsDir: e.IsDir(),
		})
	}
	slices.SortFunc(out, func(a, b DirEntry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}
