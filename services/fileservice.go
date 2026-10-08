// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/local"

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
	return move(oldPath, newPath)
}

// TrashPath moves a file or directory into PMT's trash, where an undo of the delete finds it, and returns where it
// went. The trash is emptied when the app starts.
func (f *FileService) TrashPath(fullPath string) (string, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	app, err := local.AppDir()
	if fullPath == "." || err != nil {
		return "", cmp.Or(err, errors.New("path is required"))
	}
	to := filepath.Join(app, "trash", strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(fullPath))
	return to, move(fullPath, to)
}

// ServiceStartup empties the last run's trash: a delete is undone within the run that made it. Wails calls it.
func (f *FileService) ServiceStartup(context.Context, application.ServiceOptions) error {
	if app, err := local.AppDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(app, "trash"))
	}
	return nil
}

// move renames from to to, its folder made first; across drives, which no rename crosses, it copies then removes.
func move(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	err := os.Rename(from, to)
	if err == nil || !errors.Is(err, syscall.EXDEV) && filepath.VolumeName(from) == filepath.VolumeName(to) {
		return err
	}
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		err = os.CopyFS(to, os.DirFS(from))
	} else if data, read := os.ReadFile(from); read != nil {
		err = read
	} else {
		err = os.WriteFile(to, data, info.Mode())
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(from)
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

// FileText is a text file as the editor and source control read it: its text without the byte order mark and with
// its line ends, whether it had the mark, whether it was Windows-1252 (saved back so), and the format the engine
// reads it as (source.Format's name: the editor's language follows it). Reject says why there is no text: the file
// is missing, binary or too large.
type FileText struct {
	Text    string `json:"text"`
	BOM     bool   `json:"bom"`
	Format  string `json:"format"`
	Legacy  bool   `json:"legacy,omitempty"`
	Binary  bool   `json:"binary,omitempty"`
	Missing bool   `json:"missing,omitempty"`
	Reject  string `json:"reject,omitempty"`
}

// textOf is a file's bytes as a FileText, decoded as the engine decodes them (jomini.Decode) and classified at path
// (rel: its path in its root, or the whole path where it has none); binary is refused.
func textOf(data []byte, path, rel string) FileText {
	if source.Binary(data) {
		return FileText{Binary: true, Reject: "binary file: the editor opens script, localization and other text files only"}
	}
	text, bom, legacy := jomini.Decode(data)
	return FileText{Text: text, BOM: bom, Legacy: legacy, Format: source.Classify(path, filepath.ToSlash(rel)).String()}
}

// readCapped is a file's bytes, or missing where it is not there, or why it is refused (too large to hold in
// memory, on every read, image or not).
func readCapped(fullPath string) (data []byte, missing bool, reject string, err error) {
	file, err := os.Open(filepath.Clean(fullPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, "", nil
	}
	if err != nil {
		return nil, false, "", fmt.Errorf("read file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	switch {
	case err != nil:
		return nil, false, "", fmt.Errorf("stat file: %w", err)
	case info.IsDir():
		return nil, false, "", fmt.Errorf("read file: is a directory")
	case info.Size() > maxOpenBytes:
		return nil, false, fmt.Sprintf("file too large to open (%d bytes, max %d)", info.Size(), maxOpenBytes), nil
	}
	data, err = io.ReadAll(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("read file: %w", err)
	}
	return data, false, "", nil
}

// ReadFileBase64 reads raw file bytes as standard base64, or Exists=false if missing: images (thumbnails and
// workshop previews); oversized files are refused.
func (f *FileService) ReadFileBase64(fullPath string) (FileBytes, error) {
	data, missing, reject, err := readCapped(fullPath)
	if err != nil || missing || reject != "" {
		return FileBytes{Exists: !missing && err == nil, Reject: reject}, err
	}
	return FileBytes{Exists: true, B64: base64.StdEncoding.EncodeToString(data)}, nil
}

// ReadText is a file as the editor reads it.
func (f *FileService) ReadText(fullPath string) (FileText, error) {
	data, missing, reject, err := readCapped(fullPath)
	switch {
	case err != nil:
		return FileText{}, err
	case missing:
		return FileText{Missing: true, Reject: "the file is not there"}, nil
	case reject != "":
		return FileText{Reject: reject}, nil
	}
	return textOf(data, fullPath, fullPath), nil
}

// WriteText is the editor's save: the text in the file's own encoding (jomini.Encode), through local.Write, so a
// failed save never leaves half a file.
func (f *FileService) WriteText(fullPath, text string, bom, legacy bool) error {
	data, err := jomini.Encode(text, bom, legacy)
	if err != nil {
		return err
	}
	return local.WriteBytes(filepath.Clean(fullPath), data)
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
		// a link or a junction is a folder when what it leads to is one
		isDir := entry.IsDir()
		if !isDir && !entry.Type().IsRegular() {
			info, err := os.Stat(filepath.Join(dirPath, name))
			isDir = err == nil && info.IsDir()
		}
		out = append(out, DirEntry{Name: name, IsDir: isDir})
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
