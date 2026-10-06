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
	if fullPath == "." {
		return fmt.Errorf("path is required")
	}
	return os.MkdirAll(fullPath, 0o755)
}

// RenamePath renames or moves a file or directory.
func (f *FileService) RenamePath(oldPath, newPath string) error {
	oldPath = filepath.Clean(filepath.FromSlash(oldPath))
	newPath = filepath.Clean(filepath.FromSlash(newPath))
	if oldPath == "." || newPath == "." {
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
	if fullPath == "." {
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
	return PathStat{Exists: true, IsDir: info.IsDir(), Size: info.Size(), MtimeMs: info.ModTime().UnixMilli()}, nil
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
	file, err := os.Open(filepath.Clean(filepath.FromSlash(fullPath)))
	if errors.Is(err, os.ErrNotExist) {
		return FileBytes{Exists: false}, nil
	}
	if err != nil {
		return FileBytes{}, fmt.Errorf("read file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	switch {
	case err != nil:
		return FileBytes{}, fmt.Errorf("stat file: %w", err)
	case info.IsDir():
		return FileBytes{}, fmt.Errorf("read file: is a directory")
	case info.Size() > maxOpenBytes:
		// too large to hold in memory, on every read, image or not
		return FileBytes{Exists: true, Reject: fmt.Sprintf("file too large to open (%d bytes, max %d)", info.Size(), maxOpenBytes)}, nil
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return FileBytes{}, fmt.Errorf("read file: %w", err)
	}
	// only the editor bridge refuses binary, images included: monaco-vscode has no webviews, so an
	// image in the editor renders as mojibake
	if textOnly && bytes.IndexByte(data[:min(512, len(data))], 0) >= 0 {
		return FileBytes{Exists: true, Reject: "binary file: the editor opens script, localization and other text files only"}, nil
	}
	return FileBytes{Exists: true, B64: base64.StdEncoding.EncodeToString(data)}, nil
}

// WriteFileBase64 writes raw bytes from standard base64. It is the editor's save path, so it goes
// through local.Write: a failed save never leaves half a file.
func (f *FileService) WriteFileBase64(fullPath, b64 string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return local.WriteBytes(fullPath, data)
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
