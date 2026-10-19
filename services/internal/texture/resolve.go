// resolve.go is what a path written in script, GUI or asset text names: the file the game loads for it.

package texture

import (
	"path"
	"slices"
	"strings"
)

// imageExtensions are the image extensions the game loads a written path under, in the order it tries them.
var imageExtensions = []string{".dds", ".png", ".tga"}

// Resolve is the file a path written in script, GUI or asset text names. lookup gives the path of the file a rel names
// (the index's, which reads a rel in any case): Resolve asks it for the written rel cleaned of doubled slashes and a
// leading one (Vic3 writes /gfx/...), then for the same rel under each of imageExtensions in turn, so a name written
// without its extension still finds its image.
func Resolve(reference string, lookup func(rel string) (string, bool)) (string, bool) {
	rel := strings.TrimPrefix(path.Clean("/"+reference), "/")
	if file, hasFile := lookup(rel); hasFile {
		return file, true
	}
	stem := rel
	if ext := path.Ext(rel); slices.Contains(imageExtensions, strings.ToLower(ext)) {
		stem = strings.TrimSuffix(rel, ext)
	}
	for _, ext := range imageExtensions {
		if file, hasFile := lookup(stem + ext); hasFile {
			return file, true
		}
	}
	return "", false
}
