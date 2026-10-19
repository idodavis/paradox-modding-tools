// shipped.go loads the backup for a user missing a dump file: for each game version, a schema built from
// the dumps alone (never the dumps themselves), written by gen beside this file as <game>-<version>.gob.gz.

package schema

import (
	"cmp"
	"embed"
	"io/fs"
	"regexp"
	"slices"
	"strconv"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/inventory"
)

// shippedFiles holds the shipped schemas, kept in their own folder beside the code.
//
//go:embed shipped/*.gob.gz
var shippedFiles embed.FS

// shippedDir is the folder the shipped schemas sit in, inside the package.
const shippedDir = "shipped"

// numbers matches the digit runs the newest shipped file is picked by.
var numbers = regexp.MustCompile(`\d+`)

// ShippedName returns the shipped file of a game version: <game>-<version>.gob.gz, the version as inventory.Slug
// writes it.
func ShippedName(game, version string) string {
	return game + "-" + inventory.Slug(version) + ".gob.gz"
}

// LoadShipped returns a game's shipped schema: the file of the exact version with exact true, else the
// newest file by the digit runs of the names, with exact false; nil and no error when the game has none.
func LoadShipped(gameID, version string) (s *Schema, exact bool, err error) {
	names, err := fs.Glob(shippedFiles, shippedDir+"/"+gameID+"-*.gob.gz")
	if err != nil || len(names) == 0 {
		return nil, false, err
	}
	name := shippedDir + "/" + ShippedName(gameID, version)
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

	file, err := shippedFiles.Open(name)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	if err := appdata.Decode(file, &s); err != nil {
		return nil, false, err
	}
	return s, exact, nil
}
