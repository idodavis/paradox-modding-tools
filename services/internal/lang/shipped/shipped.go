// Package shipped is the backup for a user missing a dump file: for each game version, a schema
// built from the dumps alone (never the dumps themselves), written by gen beside this file as
// <game>-<version>.gob.gz.
package shipped

import (
	"cmp"
	"embed"
	"io/fs"
	"regexp"
	"slices"
	"strconv"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/schema"
)

// files is the shipped schemas. The pattern matches this file too, so an empty set still builds.
//
//go:embed *.go*
var files embed.FS

// unsafeName is what Safe replaces; numbers are the digit runs the newest file is picked by.
var unsafeName, numbers = regexp.MustCompile(`[^A-Za-z0-9._-]`), regexp.MustCompile(`\d+`)

// Safe is a name fit for a file name, each character outside [A-Za-z0-9._-] turned into "_":
// "build 24187685" is build_24187685. The shipped files and the install cache are named by it.
func Safe(name string) string {
	return unsafeName.ReplaceAllString(name, "_")
}

// Name is the shipped file of a game version: <game>-<version>.gob.gz, by Safe.
func Name(game, version string) string {
	return Safe(game+"-"+version) + ".gob.gz"
}

// Load is a game's shipped schema: the exact version's file, else the newest by the names' digit
// runs (1.20.0.3 over 1.9.2) with exact false; nil and no error when the game has none.
func Load(gameID, version string) (sch *schema.Schema, exact bool, err error) {
	names, err := fs.Glob(files, gameID+"-*.gob.gz")
	if err != nil || len(names) == 0 {
		return nil, false, err
	}
	name := Name(gameID, version)
	exact = slices.Contains(names, name)
	if !exact {
		name = slices.MaxFunc(names, func(first, second string) int {
			return slices.CompareFunc(numbers.FindAllString(first, -1), numbers.FindAllString(second, -1),
				func(left, right string) int {
					leftNumber, _ := strconv.Atoi(left)
					rightNumber, _ := strconv.Atoi(right)
					return cmp.Compare(leftNumber, rightNumber)
				})
		})
	}

	file, err := files.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	if err := appdata.Decode(file, &sch); err != nil {
		return nil, false, err
	}
	return sch, exact, nil
}
