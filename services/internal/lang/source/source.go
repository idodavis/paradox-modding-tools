// Package source knows every file of a game install or a mod: which folders mirror the game
// layout, what each file is relative to, where it came from, and which reader its format needs.
// It interprets no content. One format table serves every game: an extension it does not list is
// Unknown, read as opaque and reported by the learner as drift, so a new kind of file is seen
// rather than guessed at. The walk follows symlinks and Windows junctions (mods are often linked
// folders), keeps every path spelled under its root, refuses a link back to an ancestor, and reads
// folders in name order so two walks of one tree agree. Image headers give a PNG, DDS or TGA's
// dimensions and format without decoding a pixel.
package source

import (
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Format is which reader a file needs.
type Format uint8

const (
	Unknown Format = iota // an extension the table does not list
	Script                // Jomini script: the tree
	Loc                   // localization entries
	Table                 // lines of ; separated columns
	List                  // sound/GUIDs.txt: one name per line
	JSON                  // a decoded value
	Info                  // the prose a game ships beside its script
	Image                 // header facts, read on demand
	Opaque                // a path and nothing more
)

var formatNames = [...]string{"unknown", "script", "loc", "table", "list", "json", "info", "image", "opaque"}

func (format Format) String() string { return formatNames[format] }

// formats is the one format table, by lower-case extension, for every game.
var formats = map[string]Format{
	// script the games and mods define things in
	".txt": Script, ".gui": Script, ".asset": Script, ".gfx": Script, ".font": Script, ".map": Script, ".shortcuts": Script,
	".settings": Script, ".skin": Script, ".lookup": Script, ".dlc": Script, ".particle2": Script, ".particle": Script,

	// script the engine's own tools write: it parses, and a mod may ship it
	".editordata": Script, ".compound": Script, ".schematic": Script, ".animsm": Script, ".aggregate": Script, ".curves": Script,
	".layout": Script, ".lines": Script, ".profile": Script, ".bindings": Script, ".anchor": Script, ".terrain": Script,
	".vertical_borders": Script, ".modifierpack": Script, ".audio2": Script, ".guianimset": Script, ".guistateset": Script,
	".heightmap": Script, ".meta": Script, ".smv": Script, ".shadernodes": Script, ".shadergraph": Script, ".ugraph": Script,
	".dep": Script, ".config": Script,

	".yml":  Loc,
	".csv":  Table,
	".json": JSON,
	".info": Info, ".md": Info,
	".dds": Image, ".png": Image, ".jpg": Image, ".jpeg": Image, ".tga": Image,

	// binary, or text nothing here reads: named in script by path, never opened
	".mesh": Opaque, ".anim": Opaque, ".bank": Opaque, ".bnk": Opaque, ".wem": Opaque, ".bk2": Opaque, ".ogg": Opaque,
	".wav": Opaque, ".mp3": Opaque, ".flac": Opaque, ".ttf": Opaque, ".otf": Opaque, ".fnt": Opaque, ".dat": Opaque,
	".bin": Opaque, ".bmp": Opaque, ".cur": Opaque, ".ani": Opaque, ".shader": Opaque, ".fxh": Opaque, ".swatch": Opaque,
	".splnet": Opaque, ".svg": Opaque, ".ico": Opaque,
}

// ignored is what ships in an install and is no part of the game's content, with the reason.
var ignored = map[string]string{
	".dll": "program code", ".dylib": "program code", ".py": "a build script", ".lnk": "a Windows shortcut left in the install",
	".pdf": "a font licence", ".psd": "source art", ".mb": "source art", ".jpf": "source art", ".obj": "source art",
	".mtl": "source art", ".xml": "Wwise bank metadata; audio is deferred", ".ini": "a desktop.ini",
	".log": "a tool's log", ".tmp": "a tool's leftover", ".cache": "a tool's cache", ".hash": "a tool's cache",
	".dmp": "a crash dump", ".disabled": "a file the game itself switched off", ".gitignore": "version control",
	".xnviewsort": "an image viewer's leftover", "": "no extension: licence texts and tool leftovers",
}

// layout is the folder names that mark a folder as mirroring the game layout.
var layout = []string{"common", "events", "gfx", "gui", "history", "localization", "map_data", "music", "sound", "fonts"}

// File is one file of an install or a mod.
type File struct {
	Path   string `json:"path"`   // absolute, as the walk spelled it
	Rel    string `json:"rel"`    // relative to its root, slash separated: common/traits/00_traits.txt
	Origin string `json:"origin"` // "vanilla", or a mod's id
	Format Format `json:"format"`
}

// Text reads the file as text: BOM stripped, CRLF folded, Latin-1 where it is not UTF-8.
func (file File) Text() (string, error) {
	raw, err := os.ReadFile(file.Path)
	if err != nil {
		return "", err
	}

	text, _ := jomini.Decode(raw)
	return text, nil
}

// documentation is the words a .txt is named by when it is prose the games ship beside their
// script: EU5's ____Info.txt and _advances_template.txt, Vic3's 00_example.txt. A whole word of
// the name, never part of one: rainforest is no info file, 00_event_templates.txt is script.
var documentation = map[string]bool{"readme": true, "info": true, "example": true, "examples": true, "template": true, "help": true, "documentation": true}

// Classify is the format of a file, from its path relative to a root. A .txt named as
// documentation is read to see whether it is: its comment lines outnumber its script lines.
func Classify(abs, rel string) Format {
	name := strings.ToLower(path.Base(rel))

	// two files are what they are by name, not by extension
	if name == "guids.txt" && strings.HasSuffix(strings.ToLower(path.Dir(rel)), "sound") {
		return List
	}
	if strings.HasPrefix(name, "readme") {
		return Info
	}
	format := formats[path.Ext(name)]
	if format == Script && path.Ext(name) == ".txt" {
		words := strings.FieldsFunc(strings.TrimSuffix(name, ".txt"), func(char rune) bool { return char == '_' || char == '-' || char == ' ' || char == '.' })
		if slices.ContainsFunc(words, func(word string) bool { return documentation[word] }) && prose(abs) {
			return Info
		}
	}
	return format
}

// prose reports a text file whose comment lines outnumber its script lines, up to a megabyte.
func prose(abs string) bool {
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) > 1<<20 {
		return false
	}
	text, _ := jomini.Decode(raw)
	script, comment := 0, 0
	for line := range strings.SplitSeq(text, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "":
		case strings.HasPrefix(trimmed, "#"):
			comment++
		default:
			script++
		}
	}
	return comment >= script
}

// Roots is the folders under dir that mirror the game layout, in the game's load order: the
// engine's base layers (clausewitz, then jomini), the game, then each DLC. A root is not searched
// further, except for its dlc folder.
func Roots(dir string) []string {
	var roots []string
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil || depth > 4 {
			return
		}

		// a folder holding one of the layout's folders is a root
		isRoot := slices.ContainsFunc(entries, func(entry fs.DirEntry) bool {
			return entry.IsDir() && slices.Contains(layout, strings.ToLower(entry.Name()))
		})
		if isRoot {
			roots = append(roots, dir)
		}

		for _, entry := range entries {
			if entry.IsDir() && (!isRoot || strings.EqualFold(entry.Name(), "dlc")) {
				visit(filepath.Join(dir, entry.Name()), depth+1)
			}
		}
	}
	visit(dir, 0)

	slices.SortStableFunc(roots, func(first, second string) int { return rootRank(dir, first) - rootRank(dir, second) })
	return roots
}

// rootRank orders roots as the game layers them: clausewitz, jomini, everything else, DLC last.
func rootRank(dir, root string) int {
	rel, _ := filepath.Rel(dir, root)
	rel = strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.Contains("/"+rel+"/", "/dlc/"):
		return 3
	case rel == "clausewitz" || strings.HasPrefix(rel, "clausewitz/"):
		return 0
	case rel == "jomini" || strings.HasPrefix(rel, "jomini/"):
		return 1
	}
	return 2
}

// Inventory is every content file under the roots, in root then path order. A root inside another
// root (a DLC folder) owns its own files.
func Inventory(origin string, roots []string) []File {
	var files []File
	for _, root := range roots {
		Walk(root, func(abs string) {
			owned := slices.ContainsFunc(roots, func(other string) bool {
				return len(other) > len(root) && strings.HasPrefix(abs, other+string(filepath.Separator))
			})
			rel, err := filepath.Rel(root, abs)
			if owned || err != nil {
				return
			}

			rel = filepath.ToSlash(rel)
			if _, skip := ignored[strings.ToLower(path.Ext(rel))]; skip {
				return
			}
			files = append(files, File{Path: abs, Rel: rel, Origin: origin, Format: Classify(abs, rel)})
		})
	}
	return files
}

// Walk calls fn with the path of every file under root.
func Walk(root string, fn func(path string)) {
	info, err := os.Stat(root)
	if err != nil {
		return
	}
	walk(root, []os.FileInfo{info}, fn)
}

// walk reads one folder; ancestors is the folders above it, which a link may not lead back to.
func walk(dir string, ancestors []os.FileInfo, fn func(path string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.Type().IsRegular() {
			fn(path)
			continue
		}

		// a folder, a link or a junction: Stat follows it
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			fn(path)
			continue
		}
		if entry.Name() == ".git" || slices.ContainsFunc(ancestors, func(ancestor os.FileInfo) bool { return os.SameFile(ancestor, info) }) {
			continue
		}
		walk(path, append(ancestors, info), fn)
	}
}

// ImageInfo is an image file's header facts.
type ImageInfo struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"` // DXT5, BC7, RGBA8, png, jpeg, TGA 32-bit
	Mips   int    `json:"mips"`   // mip levels a DDS holds; 0 for the others
}

// d3d names the pre-DX10 formats a DDS writes as a number in place of a FourCC's letters.
var d3d = map[uint32]string{36: "RGBA16", 111: "R16F", 112: "RG16F", 113: "RGBA16F", 114: "R32F", 115: "RG32F", 116: "RGBA32F"}

// dxgi names the DXGI formats the three installs use in a DX10 header.
var dxgi = map[uint32]string{
	2: "RGBA32F", 10: "RGBA16F", 28: "RGBA8", 29: "RGBA8 sRGB", 56: "R16", 61: "R8",
	71: "BC1", 72: "BC1 sRGB", 74: "BC2", 75: "BC2 sRGB", 77: "BC3", 78: "BC3 sRGB", 80: "BC4", 83: "BC5",
	87: "BGRA8", 91: "BGRA8 sRGB", 95: "BC6H", 96: "BC6H", 98: "BC7", 99: "BC7 sRGB",
}

// ReadImageInfo reads the header of the image at path.
func ReadImageInfo(path string) (ImageInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return ImageInfo{}, err
	}
	defer file.Close()

	// by magic, not name: the games ship a PNG named .dds. TGA has no magic, so it goes by name
	var magic [4]byte
	io.ReadFull(file, magic[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ImageInfo{}, err
	}
	switch {
	case string(magic[:]) == "DDS ":
		return ddsInfo(file)
	case strings.EqualFold(filepath.Ext(path), ".tga"):
		return tgaInfo(file)
	}

	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{Width: cfg.Width, Height: cfg.Height, Format: format}, nil
}

// ddsInfo reads the DDS magic, its 124 byte header, and a DX10 header when the FourCC says DX10.
func ddsInfo(in io.Reader) (ImageInfo, error) {
	var head [148]byte
	n, _ := io.ReadFull(in, head[:])
	if n < 128 || string(head[:4]) != "DDS " {
		return ImageInfo{}, fmt.Errorf("not a DDS file")
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(head[off:]) }
	info := ImageInfo{Height: int(u32(12)), Width: int(u32(16)), Mips: int(u32(28))}

	// the pixel format: a FourCC (DX10 for the next header, an older format by number, or
	// letters), else the channels of an uncompressed surface
	flags, fourCC, bits := u32(80), string(head[84:88]), u32(88)
	const hasAlpha, isFourCC, isRGB = 0x1, 0x4, 0x40
	switch {
	case flags&isFourCC != 0 && fourCC == "DX10" && n == 148:
		info.Format = dxgi[u32(128)]
		if info.Format == "" {
			info.Format = fmt.Sprintf("DXGI %d", u32(128))
		}
	case flags&isFourCC != 0 && u32(84) < 256:
		info.Format = d3d[u32(84)]
		if info.Format == "" {
			info.Format = fmt.Sprintf("D3DFMT %d", u32(84))
		}
	case flags&isFourCC != 0:
		info.Format = strings.TrimRight(fourCC, "\x00 ")
	case flags&isRGB != 0 && flags&hasAlpha != 0:
		info.Format = fmt.Sprintf("RGBA%d", bits/4)
	case flags&isRGB != 0 && bits == 32:
		info.Format = "RGBX8"
	case flags&isRGB != 0:
		info.Format = fmt.Sprintf("RGB%d", bits/3)
	default:
		info.Format = fmt.Sprintf("%d-bit", bits)
	}
	return info, nil
}

// tgaInfo reads the 18 byte TGA header.
func tgaInfo(in io.Reader) (ImageInfo, error) {
	var head [18]byte
	if _, err := io.ReadFull(in, head[:]); err != nil {
		return ImageInfo{}, fmt.Errorf("not a TGA file: %w", err)
	}
	width, height := binary.LittleEndian.Uint16(head[12:]), binary.LittleEndian.Uint16(head[14:])
	return ImageInfo{Width: int(width), Height: int(height), Format: fmt.Sprintf("TGA %d-bit", head[16])}, nil
}
