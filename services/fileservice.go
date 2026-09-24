// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"paradox-modding-tools/services/internal/local"
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

// FileBytes is a binary file payload for the IDE FS bridge. A missing file is Exists=false with no
// error, so optional paths are skipped quietly. Reject says why an existing file must not be
// opened; it is not a Go error, so Wails does not log a binding failure.
type FileBytes struct {
	Exists bool   `json:"exists"`
	B64    string `json:"b64,omitempty"`
	Reject string `json:"reject,omitempty"`
}

// ReadFileBase64 reads raw file bytes as standard base64, or Exists=false if missing. Binary is
// expected (thumbnails and workshop previews); oversized files are refused.
func (f *FileService) ReadFileBase64(fullPath string) (FileBytes, error) {
	return f.readBase64(fullPath, false)
}

// ReadEditableBase64 is ReadFileBase64 for the IDE bridge, which hands the bytes to an editor, so
// it refuses binary too. It stays apart because every PNG has a NUL in its first 512 bytes.
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

// rejectOpen refuses a file too large to hold in memory, on every read, image or not.
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

// rejectBinary refuses content no editor here can show; only the editor bridge applies it. Images
// are refused as RejectImage rather than as binary: monaco-vscode has no webviews, so an image in
// the editor renders as mojibake, but the caller can still offer a preview.
func rejectBinary(head []byte) string {
	if bytes.IndexByte(head, 0) < 0 {
		return "" // text
	}
	if viewableImage(head) {
		return RejectImage
	}
	return fmt.Sprintf("binary file (NUL in first %d bytes)", len(head))
}

// viewableImage reports a raster format the workbench's media preview can show, by content, not by
// extension. DDS and TGA are absent though Paradox ships many: no browser decodes them.
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

// WriteFileBase64 writes raw bytes from standard base64. It is the editor's save path, the only
// file PMT writes that it cannot regenerate, so it goes through local.Write: a failed save leaves
// the modder's script as it was.
func (f *FileService) WriteFileBase64(fullPath, b64 string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return local.Write(fullPath, func(out io.Writer) error {
		_, err := out.Write(data)
		return err
	})
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
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			continue
		}
		if !showAll && strings.HasPrefix(name, ".") && name != game.MetadataDir {
			continue
		}
		out = append(out, DirEntry{
			Name: name, IsDir: entry.IsDir(),
		})
	}
	slices.SortFunc(out, func(first, second DirEntry) int {
		if first.IsDir != second.IsDir {
			if first.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(first.Name), strings.ToLower(second.Name))
	})
	return out, nil
}
