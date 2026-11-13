// files.go says where a file sits in the workspace, and reads, writes, moves
// and serves the workspace's files.

package workspace

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/app"
)

// Place is where one file sits in the workspace, with the root it is under
// and what the workspace knows about it.
type Place struct {
	files.File
	Name string `json:"name"`

	// Folder is "" for a file at its root.
	Folder   string `json:"folder"`
	Root     string `json:"root"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"`

	// Position is where the index lists the file, -1 if it does not.
	Position int32 `json:"-"`
	IsDir    bool  `json:"isDir"`

	// Drawable is true for a file the Visual view draws.
	Drawable bool `json:"drawable"`

	// Readme is the path of the readme of the file's database, "" if none.
	Readme string `json:"readme"`

	// Language is the editor language, plain text for a format with none.
	Language string `json:"language"`

	// Hiding is why the game does not read the file, "" for a file it reads.
	// Hider is the id of the mod that hides it, "" where no mod does.
	Hiding files.Hiding `json:"hiding"`
	Hider  string       `json:"hider"`
}

// FileText is a file's text with its place in the workspace.
type FileText struct {
	Place Place      `json:"place"`
	Text  files.Text `json:"text"`

	// Draft is the editor's unsaved text, "" if none or the same as the disk.
	Draft  string `json:"draft"`
	Binary bool   `json:"binary"`
}

const (
	plainLanguage  = "plaintext"
	scriptLanguage = "paradox-script"
)

var editorLanguages = map[files.Format]string{
	files.FormatScript: scriptLanguage, files.FormatAsset: scriptLanguage, files.FormatFont: scriptLanguage,
	files.FormatGFX: scriptLanguage, files.FormatGUI: "paradox-gui", files.FormatLocalization: "paradox-localization",
	files.FormatReadme: "paradox-readme", files.FormatMarkdown: "markdown",
}

// Locate returns the place of a file in an index of the mods. A path under no
// root gets only its path, key and name.
func (s *Session) Locate(mods *index.Index, path string) Place {
	place := Place{
		File: files.File{Path: path, Key: files.ToKey(path)}, Name: filepath.Base(path), Position: -1, Language: plainLanguage,
	}
	root, ok := mods.FindRoot(path)
	if !ok {
		return place
	}

	// A root's own folder has no rel.
	place.File, _ = files.NewFile(root, path)
	place.Language = cmp.Or(editorLanguages[place.Format], plainLanguage)
	if folder, _, ok := strings.CutLast(place.Rel, "/"); ok {
		place.Folder = folder
	}

	place.Root = root.Dir
	place.ReadOnly = root.Origin == files.OriginVanilla ||
		slices.ContainsFunc(*s.Order.Load(), func(mod Mod) bool { return mod.ID == root.Origin && mod.Workshop })
	if database, ok := s.Schema.FindDatabase(place.File); ok {
		place.Database = database.Name
		if database.Readme != "" {
			place.Readme = files.Join(s.InstallPath, database.Readme)
		}
	}

	place.Drawable = place.Format == files.FormatGUI || place.Database == lang.CoatDatabase
	if position, ok := mods.FindFile(path); ok {
		hiding, hider := mods.FindHiding(position)
		place.Position, place.Hiding, place.Hider = position, hiding, hider.Origin
	}

	return place
}

// Locate returns the place of a file or folder, empty if no workspace is open.
func (s *Service) Locate(path string) Place {
	ws, err := s.Session()
	if err != nil {
		return Place{}
	}

	place := ws.Locate(ws.Mods.Load(), path)
	place.IsDir = files.IsFolder(path)

	return place
}

// ListCopies returns every copy of a file in load order, the game's first.
func (s *Service) ListCopies(path string) []Place {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	loaded, copies, ok := mods.FindLoaded(ws.Locate(mods, path).Rel)
	if ok {
		copies = append(slices.Clip(copies), loaded)
	}

	places := make([]Place, len(copies))
	for position, file := range copies {
		places[position] = ws.Locate(mods, file.Path)
	}

	return places
}

// ListRoots returns the places of the workspace's roots: the mods' in load
// order, then the game's own folder.
func (s *Service) ListRoots() []Place {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	roots := slices.Clone(mods.Roots[len(ws.Install.Roots):])
	for _, root := range ws.Install.Roots {
		if filepath.Base(root.Dir) == game.ScriptRoot {
			roots = append(roots, root)

			break
		}
	}

	places := make([]Place, len(roots))
	for position, root := range roots {
		places[position] = ws.Locate(mods, root.Dir)
		places[position].IsDir = true
	}

	return places
}

// ListFolder returns the places of a folder's entries, folders first, without
// what the settings hide.
func (s *Service) ListFolder(dir string) ([]Place, error) {
	ws, err := s.Session()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	hideBinaries, _ := s.Config.LookupSetting(ws.ID, "ide.hideBinaries")
	hideDots, _ := s.Config.LookupSetting(ws.ID, "ide.hideDotFolders")
	mods := ws.Mods.Load()
	folders, others := []Place{}, []Place{}
	for _, entry := range entries {
		place := ws.Locate(mods, filepath.Join(dir, entry.Name()))
		place.IsDir = entry.IsDir()
		switch {
		case place.IsDir && isListed(entry.Name(), hideDots == "true"):
			folders = append(folders, place)
		case !place.IsDir && (hideBinaries != "true" || place.Format.IsText()):
			others = append(others, place)
		}
	}

	return append(folders, others...), nil
}

func isListed(name string, hideDots bool) bool {
	return name != ".git" && (!hideDots || !files.IsHiddenDir(name))
}

// Read returns a file's text, its place and the editor's draft of it. A file
// that is not text comes back as binary.
func (s *Service) Read(path string) (FileText, error) {
	ws, err := s.Session()
	if err != nil {
		return FileText{}, err
	}

	place := ws.Locate(ws.Mods.Load(), path)
	text, err := files.ReadText(path)
	if errors.Is(err, files.ErrBinary) {
		return FileText{Place: place, Binary: true}, nil
	}

	if err == nil && text.Text == "" && text.Encoding == files.EncodingUTF8 {
		text.Encoding = place.Format.DefaultEncoding()
	}

	draft, open := ws.LookupBuffer(path)
	if !open || draft == text.Text {
		draft = ""
	}

	return FileText{Place: place, Text: text, Draft: draft}, err
}

// ReadHeader returns an image file's header.
func (s *Service) ReadHeader(path string) (texture.Header, error) {
	ws, err := s.Session()
	if err != nil {
		return texture.Header{}, err
	}

	if _, ok := ws.Mods.Load().FindRoot(path); !ok {
		return texture.Header{}, fmt.Errorf("%s is outside the workspace", path)
	}

	return texture.ReadHeader(path)
}

// FindWritable returns the open session if every path is in one of its mods. It
// fails for the game's own files and a Workshop mod's.
//
//wails:ignore
func (s *Service) FindWritable(paths ...string) (*Session, error) {
	ws, err := s.Session()
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	for _, path := range paths {
		switch place := ws.Locate(mods, path); {
		case place.Root == "":
			return nil, fmt.Errorf("%s is outside the workspace", path)
		case place.Origin == files.OriginVanilla:
			return nil, errors.New("the game's own files are not changed")
		case place.ReadOnly:
			return nil, fmt.Errorf("%s is Steam's copy of a Workshop mod; Steam replaces it when its author updates it",
				ws.DescribeMod(place.Origin))
		}
	}

	return ws, nil
}

// Write writes a file's text. It fails outside the workspace's mods.
func (s *Service) Write(path string, text files.Text) error { return s.WriteBytes(path, text.Encode()) }

// WriteBytes writes a file's bytes. It fails outside the workspace's mods.
//
//wails:ignore
func (s *Service) WriteBytes(path string, bytes []byte) error {
	if _, err := s.FindWritable(path); err != nil {
		return err
	}

	return files.WriteAtomic(path, bytes)
}

// CreateCopy copies a file to the same place in a mod and returns the copy's
// path. It fails if the mod has a file there.
func (s *Service) CreateCopy(path, modID string) (string, error) {
	ws, err := s.Session()
	if err != nil {
		return "", err
	}

	mod, ok := s.LookupMod(modID)
	if !ok || mod.Broken {
		return "", fmt.Errorf("the mod %s is not in the workspace or has no folder", modID)
	}

	place := ws.Locate(ws.Mods.Load(), path)
	if place.Root == "" {
		return "", fmt.Errorf("%s is outside the workspace", path)
	}

	target := files.Join(mod.Dir, place.Rel)
	if files.IsFile(target) {
		return "", fmt.Errorf("%s has %s already", ws.DescribeMod(modID), place.Rel)
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return target, s.WriteBytes(target, bytes)
}

// CreateFile makes an empty file and the folders it goes in. It fails if the
// file exists.
func (s *Service) CreateFile(path string) error {
	if err := s.makeParent(path); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	return file.Close()
}

// CreateFolder makes a folder and the folders it goes in. It fails if the
// folder exists.
func (s *Service) CreateFolder(path string) error {
	if err := s.makeParent(path); err != nil {
		return err
	}

	return os.Mkdir(path, 0o755)
}

// makeParent makes the folder a new file or folder goes in. It fails outside
// the workspace's mods.
func (s *Service) makeParent(path string) error {
	if _, err := s.FindWritable(path); err != nil {
		return err
	}

	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// Rename moves a file or a folder. It fails if the target exists.
func (s *Service) Rename(source, target string) error {
	ws, err := s.FindWritable(source, target)
	if err != nil {
		return err
	}

	return ws.watcher.Move(source, target)
}

// Delete moves a file or a folder to PMT's trash and returns where it went. A
// mod's folder takes the mod out of the workspace too.
func (s *Service) Delete(path string) (string, error) {
	ws, err := s.FindWritable(path)
	if err != nil {
		return "", err
	}

	trashed := filepath.Join(s.Config.Dirs.Trash, strconv.FormatInt(time.Now().UnixNano(), 10), filepath.Base(path))
	if err := ws.watcher.Move(path, trashed); err != nil {
		return "", err
	}

	for _, mod := range *ws.Order.Load() {
		if files.ToKey(mod.Dir) == files.ToKey(path) {
			return trashed, s.RemoveMod(ws.ID, mod.ID)
		}
	}

	return trashed, nil
}

// Restore moves a file or a folder back from PMT's trash. It fails if the
// target exists.
func (s *Service) Restore(trashed, path string) error {
	if !files.IsUnder(s.Config.Dirs.Trash, trashed) {
		return fmt.Errorf("%s is not in the trash", trashed)
	}

	ws, err := s.FindWritable(path)
	if err != nil {
		return err
	}

	return ws.watcher.Move(trashed, path)
}

// move moves a file or a folder, making the folder it goes into. It fails if
// the target exists, since Linux would replace a file there. Across drives it
// copies, then deletes the original.
func move(source, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s exists already", target)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	err := os.Rename(source, target)
	crossesDrives := errors.Is(err, syscall.EXDEV) || filepath.VolumeName(source) != filepath.VolumeName(target)
	if err == nil || !crossesDrives {
		return err
	}

	if files.IsFolder(source) {
		err = os.CopyFS(target, os.DirFS(source))
	} else {
		var content []byte
		if content, err = os.ReadFile(source); err == nil {
			err = os.WriteFile(target, content, 0o644)
		}
	}

	if err != nil {
		return err
	}

	return os.RemoveAll(source)
}

// ServeHTTP serves the file a request names. A DDS or TGA, or a request for a
// frame or a mip level, is served as PNG. A file outside the installs, mods and
// Workshop folders is refused.
func (s *Service) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	path := query.Get("path")
	records := s.Config.Config()

	var dirs []string
	for _, install := range records.Installs {
		dirs = append(dirs, install.Path)
	}

	for _, record := range records.Workspaces {
		for _, mod := range record.Mods {
			dirs = append(dirs, mod.Dir)
		}
	}

	// The Workshop picker shows a mod's thumbnail before the mod is added. The
	// Workshop folders are looked up last, since that reads Steam's files.
	isServed := slices.ContainsFunc(dirs, func(dir string) bool {
		_, ok := files.ToRel(dir, path)

		return ok
	}) || slices.ContainsFunc(records.Installs, func(install app.Install) bool {
		row := game.LookupGame(install.Game)

		return row != nil && game.IsWorkshopFolder(row.ListWorkshopRoots(), path)
	})
	if !isServed {
		http.Error(writer, "the path is outside every recorded install and mod", http.StatusForbidden)

		return
	}

	isCut := query.Has("framesize") || query.Has("framegrid") || query.Has("level")
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".dds" && ext != ".tga" && !isCut {
		http.ServeFile(writer, request, path)

		return
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusNotFound)

		return
	}

	level, _ := strconv.Atoi(query.Get("level"))
	picture, err := texture.Decode(bytes, level)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusUnprocessableEntity)

		return
	}

	writer.Header().Set("Content-Type", "image/png")
	_ = png.Encode(writer, cutFrame(picture, query))
}

// cutFrame returns the frame of a sprite sheet that a query names, counted from
// 1 across and then down. A frame past the end is the last. It returns the
// whole image if the query gives no usable frame size.
func cutFrame(picture image.Image, query url.Values) image.Image {
	bounds := picture.Bounds()
	var width, height, columns, rows float64
	if _, err := fmt.Sscan(query.Get("framesize"), &width, &height); err == nil {
		density, err := strconv.ParseFloat(query.Get("density"), 64)
		if err != nil || density <= 0 {
			density = 1
		}

		width, height = width*density, height*density
	} else if _, err := fmt.Sscan(query.Get("framegrid"), &columns, &rows); err == nil && columns >= 1 && rows >= 1 {
		width, height = float64(bounds.Dx())/columns, float64(bounds.Dy())/rows
	}

	size := image.Pt(int(width), int(height))
	sheet, ok := picture.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok || size.X < 1 || size.Y < 1 || size.X > bounds.Dx() || size.Y > bounds.Dy() {
		return picture
	}

	frame, _ := strconv.Atoi(query.Get("frame"))
	rowFrames := bounds.Dx() / size.X
	position := min(max(frame, 1), rowFrames*(bounds.Dy()/size.Y)) - 1
	corner := bounds.Min.Add(image.Pt(position%rowFrames*size.X, position/rowFrames*size.Y))

	return sheet.SubImage(image.Rectangle{Min: corner, Max: corner.Add(size)})
}
