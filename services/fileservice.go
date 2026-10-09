// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/texture"

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

// FileText is a text file as the editor and source control read it: its text without the byte order mark and with
// its line ends, whether it had the mark, whether it was Windows-1252 (saved back so), and the format the engine
// reads it as (inventory.Format's name: the editor's language follows it). Reject says why there is no text: the file
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

// textOf is a file's bytes as a FileText, decoded as the engine decodes them (inventory.DecodeText) and classified at path
// (rel: its path in its root, or the whole path where it has none); binary is refused.
func textOf(data []byte, path, rel string) FileText {
	if inventory.Binary(data) {
		return FileText{Binary: true, Reject: "binary file: the editor opens script, localization and other text files only"}
	}
	text, bom, legacy := inventory.DecodeText(data)
	return FileText{Text: text, BOM: bom, Legacy: legacy, Format: inventory.Classify(path, filepath.ToSlash(rel)).String()}
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

// ImageRoute is where the page reads an image file, on Wails' asset server: ?path= its absolute path.
const ImageRoute = "/pmt/image"

// maxImageBytes is the largest image file shown; the games' map data runs past it.
const maxImageBytes = 64 << 20

// imageTypes is the files ImageRoute serves, by extension.
var imageTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
	".ico": true, ".svg": true, ".dds": true, ".tga": true}

// ImageFacts is an image file's header (none where Go reads no header of its kind: SVG, WebP, BMP, ICO), its format as
// the page names it, its size, and its URL at ImageRoute, which names when the file changed so a changed file is read
// again; Reject says why it is not shown.
type ImageFacts struct {
	Info   texture.Facts `json:"info"`
	Label  string        `json:"label"` // DDS BC7, TGA 32-bit, PNG
	Bytes  int64         `json:"bytes"`
	URL    string        `json:"url"`
	Reject string        `json:"reject,omitempty"`
}

// ReadImageFacts is an image file's facts, which its pixels at ImageRoute do not carry.
func (f *FileService) ReadImageFacts(fullPath string) (ImageFacts, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	stat, err := os.Stat(fullPath)
	if err != nil {
		return ImageFacts{}, err
	}
	facts := ImageFacts{Bytes: stat.Size(), URL: ImageRoute + "?path=" + url.QueryEscape(fullPath) + "&v=" + strconv.FormatInt(stat.ModTime().UnixMilli(), 10)}
	if stat.Size() > maxImageBytes {
		facts.Reject = fmt.Sprintf("larger than %d MB: not shown", maxImageBytes>>20)
	}
	facts.Info, _ = texture.Info(fullPath)
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(fullPath), "."))
	switch format := facts.Info.Format; {
	case ext == "DDS" && format != "png":
		facts.Label = strings.TrimSpace("DDS " + format)
	case strings.HasPrefix(format, "TGA"):
		facts.Label = format
	default:
		facts.Label = strings.ToUpper(cmp.Or(format, ext))
	}
	return facts, nil
}

// ServeImages answers ImageRoute ahead of the page's assets (Wails' asset server takes it as its middleware): a DDS or
// TGA decoded and sent as a PNG, any other image file as it is, which the webview draws and caches itself. Each is
// read again only when the file changed.
func ServeImages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ImageRoute {
			next.ServeHTTP(w, r)
			return
		}
		path := filepath.Clean(r.URL.Query().Get("path"))
		ext := strings.ToLower(filepath.Ext(path))
		stat, err := os.Stat(path)
		switch {
		case err != nil || stat.IsDir() || !imageTypes[ext]:
			http.NotFound(w, r)
			return
		case stat.Size() > maxImageBytes:
			http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if ext != ".dds" && ext != ".tga" {
			http.ServeFile(w, r, path)
			return
		}
		if since, err := http.ParseTime(r.Header.Get("If-Modified-Since")); err == nil && !stat.ModTime().Truncate(time.Second).After(since) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		img, err := texture.Decode(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
			return
		}
		var out bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, img); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "image.png", stat.ModTime(), bytes.NewReader(out.Bytes()))
	})
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

// WriteText is the editor's save: the text in the file's own encoding (inventory.EncodeText), through local.Write, so a
// failed save never leaves half a file.
func (f *FileService) WriteText(fullPath, text string, bom, legacy bool) error {
	data, err := inventory.EncodeText(text, bom, legacy)
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
