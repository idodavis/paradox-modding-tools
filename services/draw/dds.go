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
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// ListPictureFiles returns the pictures directly in a folder, by name: its images that are not DDS files. It returns
// an error if no workspace is open, the folder is outside it, or the folder cannot be read.
func (drw *Service) ListPictureFiles(folder string) ([]workspace.Place, error) {
	session, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := session.Mods.Load()
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
			pictures = append(pictures, session.Locate(mods, filepath.Join(folder, entry.Name())))
		}
	}
	return pictures, nil
}

// FindReferences returns, for each picture, the DDS whose settings it keeps when it is saved as a DDS: the DDS of
// the same name beside it, else the game's own DDS at the place the picture's DDS will have. A picture with neither
// is left out. It returns an error if no workspace is open.
func (drw *Service) FindReferences(paths []string) (map[string]workspace.Place, error) {
	session, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	isGame := func(place workspace.Place) bool { return place.Origin == game.OriginVanilla }
	references := map[string]workspace.Place{}
	for _, path := range paths {
		target := toDDSPath(path)
		if files.IsFile(target) {
			references[path] = session.Locate(session.Mods.Load(), target)
		} else if place, ok := lo.Find(drw.Workspace.ListCopies(target), isGame); ok {
			references[path] = place
		}
	}
	return references, nil
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
