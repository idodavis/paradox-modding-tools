// walk.go is the one file walk. Mods are often folders linked from somewhere else, so it follows symlinks and Windows junctions, keeps every path spelled under the root it was given, and refuses a link that leads back to one of its own ancestors. Folders are read in name order, so two walks of one tree agree.
package source

import (
	"os"
	"path/filepath"
	"slices"
)

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

	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.Type().IsRegular() {
			fn(p)
			continue
		}

		// a folder, a link or a junction: Stat follows it
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			fn(p)
			continue
		}
		if e.Name() == ".git" || slices.ContainsFunc(ancestors, func(a os.FileInfo) bool { return os.SameFile(a, info) }) {
			continue
		}
		walk(p, append(ancestors, info), fn)
	}
}
