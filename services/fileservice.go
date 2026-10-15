// Package services provides backend services for the Paradox Modding Tools application.
package services

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
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

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/draw"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/texture"

	"paradox-modding-tools/services/internal/game"
)

// FileService provides filesystem helpers for the IDE workbench.
type FileService struct{}

// DirEntry is one immediate child of a directory (for lazy file trees).
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// CreateDir creates a directory (parents created as needed).
func (s *FileService) CreateDir(fullPath string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	if fullPath == "." {
		return errors.New("path is required")
	}
	return os.MkdirAll(fullPath, 0o755)
}

// RenamePath renames or moves a file or directory.
func (s *FileService) RenamePath(oldPath, newPath string) error {
	oldPath = filepath.Clean(filepath.FromSlash(oldPath))
	newPath = filepath.Clean(filepath.FromSlash(newPath))
	if oldPath == "." || newPath == "." {
		return errors.New("path is required")
	}
	return move(oldPath, newPath)
}

// TrashPath moves a file or directory into PMT's trash, where an undo of the delete finds it, and returns where it
// went. The trash is emptied when the app starts.
func (s *FileService) TrashPath(fullPath string) (string, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	app, err := appdata.Dir()
	if fullPath == "." || err != nil {
		return "", cmp.Or(err, errors.New("path is required"))
	}
	trashPath := filepath.Join(app, "trash", strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(fullPath))
	return trashPath, move(fullPath, trashPath)
}

// ServiceStartup empties the last run's trash: a delete is undone within the run that made it. Wails calls it.
func (s *FileService) ServiceStartup(context.Context, application.ServiceOptions) error {
	if app, err := appdata.Dir(); err == nil {
		_ = os.RemoveAll(filepath.Join(app, "trash"))
	}
	return nil
}

// move renames source to target, its folder made first; across drives, which no rename crosses, it copies then
// removes.
func move(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	err := os.Rename(source, target)
	if err == nil || !errors.Is(err, syscall.EXDEV) && filepath.VolumeName(source) == filepath.VolumeName(target) {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		err = os.CopyFS(target, os.DirFS(source))
	} else if data, readErr := os.ReadFile(source); readErr != nil {
		err = readErr
	} else {
		err = os.WriteFile(target, data, info.Mode())
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(source)
}

// DeletePath removes a file or directory recursively.
func (s *FileService) DeletePath(fullPath string) error {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	if fullPath == "." {
		return errors.New("path is required")
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
func (s *FileService) StatPath(fullPath string) (PathStat, error) {
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

// textOf is a file's bytes as a FileText, decoded as the engine decodes them (inventory.DecodeText) and
// classified at path (rel: its path in its root, or the whole path where it has none); binary is refused.
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
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	switch {
	case err != nil:
		return nil, false, "", fmt.Errorf("stat file: %w", err)
	case info.IsDir():
		return nil, false, "", errors.New("read file: is a directory")
	case info.Size() > inventory.MaxTextBytes:
		return nil, false, fmt.Sprintf("file too large to open (%d bytes, max %d)", info.Size(), inventory.MaxTextBytes), nil
	}
	data, err = io.ReadAll(file)
	if err != nil {
		return nil, false, "", fmt.Errorf("read file: %w", err)
	}
	return data, false, "", nil
}

// FileRoute is where the page reads an image or font file, on Wails' asset server: ?path= its absolute path.
const FileRoute = draw.FileRoute

// maxImageBytes is the largest image file shown; the games' map data runs past it.
const maxImageBytes = 64 << 20

// fileTypes is the files FileRoute serves, by extension: the images and the fonts a window draws with.
var fileTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
	".ico": true, ".svg": true, ".dds": true, ".tga": true, ".ttf": true, ".otf": true}

// ImageFacts is an image file's header (none where Go reads no header of its kind: SVG, WebP, BMP, ICO), its format as
// the page names it, its size, and its URL at FileRoute, which names when the file changed so a changed file is read
// again; Reject says why it is not shown.
type ImageFacts struct {
	Info   texture.Facts `json:"info"`
	Label  string        `json:"label"` // DDS BC7, TGA 32-bit, PNG
	Bytes  int64         `json:"bytes"`
	URL    string        `json:"url"`
	Reject string        `json:"reject,omitempty"`
}

// ReadImageFacts is an image file's facts, which its pixels at FileRoute do not carry.
func (s *FileService) ReadImageFacts(fullPath string) (ImageFacts, error) {
	fullPath = filepath.Clean(filepath.FromSlash(fullPath))
	stat, err := os.Stat(fullPath)
	if err != nil {
		return ImageFacts{}, err
	}
	facts := ImageFacts{Bytes: stat.Size(), URL: FileRoute + "?path=" + url.QueryEscape(fullPath) + "&v=" + strconv.FormatInt(stat.ModTime().UnixMilli(), 10)}
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

// ServeFiles answers FileRoute ahead of the page's assets (Wails' asset server takes it as its middleware): a DDS or
// TGA decoded and sent as a PNG, as is a frame of any image (&frame=x,y,width,height, a sheet's part a widget draws);
// any other image or font file as it is, which the webview draws and caches itself. Each is read again only when the
// file changed.
func ServeFiles(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FileRoute {
			next.ServeHTTP(w, r)
			return
		}
		path := filepath.Clean(r.URL.Query().Get("path"))
		ext := strings.ToLower(filepath.Ext(path))
		stat, err := os.Stat(path)
		switch {
		case err != nil || stat.IsDir() || !fileTypes[ext]:
			http.NotFound(w, r)
			return
		case stat.Size() > maxImageBytes:
			http.Error(w, "image too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		frame := r.URL.Query().Get("frame")
		if ext != ".dds" && ext != ".tga" && frame == "" {
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
		if frame != "" {
			img = framePart(img, frame)
		}
		var out bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, img); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "image.png", stat.ModTime(), bytes.NewReader(out.Bytes()))
	})
}

// framePart is the part of an image a frame names (x,y,width,height), as much of it as the image holds; the whole image
// where the frame does not read.
func framePart(img image.Image, frame string) image.Image {
	var x, y, width, height int
	cropper, isCroppable := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if _, err := fmt.Sscanf(frame, "%d,%d,%d,%d", &x, &y, &width, &height); err != nil || !isCroppable {
		return img
	}
	return cropper.SubImage(image.Rect(x, y, x+width, y+height).Add(img.Bounds().Min))
}

// ReadText is a file as the editor reads it.
func (s *FileService) ReadText(fullPath string) (FileText, error) {
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

// WriteText is the editor's save: the text in the file's own encoding (inventory.EncodeText), through appdata.Write, so
// a failed save never leaves half a file.
func (s *FileService) WriteText(fullPath, text string, bom, legacy bool) error {
	data, err := inventory.EncodeText(text, bom, legacy)
	if err != nil {
		return err
	}
	return appdata.WriteBytes(filepath.Clean(fullPath), data)
}

// ListDirectory lists immediate children of dirPath (non-recursive).
// showAll lists every name; otherwise skip dotfiles except .metadata.
func (s *FileService) ListDirectory(dirPath string, showAll bool) ([]DirEntry, error) {
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
