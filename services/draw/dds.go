// dds.go saves the pictures of a mod as the DDS files the game reads.

package draw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// A PictureFiles is the pictures Save as DDS saves, and for each the DDS whose settings it keeps, by the picture's
// path.
type PictureFiles struct {
	Pictures   []workspace.Place          `json:"pictures"`
	References map[string]workspace.Place `json:"references"`
}

// FindPictureFiles returns the pictures that Save as DDS saves for the given path: the picture itself, or for a folder
// the pictures directly in it, by name, which are its images that are not DDS files. Each picture keeps the settings
// of the DDS of the same name beside it, else of the game's own DDS at the place its DDS will have; a picture with
// neither has no reference. It returns an error if no workspace is open, or a folder is outside it or cannot be read.
func (drw *Service) FindPictureFiles(path string) (PictureFiles, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return PictureFiles{}, err
	}
	mods := ws.Mods.Load()
	pictures := []workspace.Place{ws.Locate(mods, path)}
	if files.IsFolder(path) {
		if pictures, err = listPictureFiles(ws, mods, path); err != nil {
			return PictureFiles{}, err
		}
	}
	isGame := func(place workspace.Place) bool { return place.Origin == game.OriginVanilla }
	references := map[string]workspace.Place{}
	for _, picture := range pictures {
		target := toDDSPath(picture.Path)
		if files.IsFile(target) {
			references[picture.Path] = ws.Locate(mods, target)
		} else if place, ok := lo.Find(drw.Workspace.ListCopies(target), isGame); ok {
			references[picture.Path] = place
		}
	}
	return PictureFiles{Pictures: pictures, References: references}, nil
}

// WriteDDS writes each picture beside itself as a DDS, in BC3 blocks when compressed and else uncompressed, with or
// without its mip levels, replacing a DDS of that name. It returns the files written. On an error it returns the ones
// written before it, and the error names the picture and how many were written.
func (drw *Service) WriteDDS(paths []string, compressed, mipmaps bool) ([]string, error) {
	ddsPaths := []string{}
	for _, path := range paths {
		dds, err := encodePicture(path, compressed, mipmaps)
		if err == nil {
			err = drw.Workspace.WriteBytes(toDDSPath(path), dds)
		}
		if err != nil {
			return ddsPaths, fmt.Errorf("%s was not saved, after %d saved: %w", filepath.Base(path), len(ddsPaths), err)
		}
		ddsPaths = append(ddsPaths, toDDSPath(path))
	}
	return ddsPaths, nil
}

// listPictureFiles returns the pictures directly in a folder, by name: its images that are not DDS files. It returns an
// error if the folder is outside the workspace or cannot be read.
func listPictureFiles(ws *workspace.Session, mods *index.Index, folder string) ([]workspace.Place, error) {
	if _, ok := mods.FindRoot(folder); !ok {
		return nil, fmt.Errorf("%s is outside the workspace", folder)
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	pictures := []workspace.Place{}
	for _, entry := range entries {
		if !entry.IsDir() && texture.IsImage(entry.Name()) && !strings.EqualFold(filepath.Ext(entry.Name()), ".dds") {
			pictures = append(pictures, ws.Locate(mods, filepath.Join(folder, entry.Name())))
		}
	}
	return pictures, nil
}

// toDDSPath returns the path of the DDS a picture is saved as: the picture's, with .dds for its extension.
func toDDSPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".dds"
}

// encodePicture reads a picture file and encodes it as a DDS. It returns an error if the file cannot be read or
// decoded, or the picture cannot be a DDS.
func encodePicture(path string, compressed, mipmaps bool) ([]byte, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	picture, err := texture.Decode(file, 0)
	if err != nil {
		return nil, err
	}
	return texture.EncodeDDS(picture, compressed, mipmaps)
}
