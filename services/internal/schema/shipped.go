// shipped.go is the backup for a user missing a dump file: for each game version, a schema built from
// the dumps alone (never the dumps themselves), written by gen beside this file as
// <game>-<version>.gob.gz.

package schema

import (
	"cmp"
	"embed"
	"io/fs"
	"regexp"
	"slices"
	"strconv"

	"paradox-modding-tools/services/internal/appdata"
)

// shippedFiles is the shipped schemas.
//
//go:embed *.gob.gz
var shippedFiles embed.FS

// numbers are the digit runs the newest file is picked by.
var numbers = regexp.MustCompile(`\d+`)

// ShippedName is the shipped file of a game version: <game>-<version>.gob.gz, by appdata.SafeName.
func ShippedName(game, version string) string {
	return appdata.SafeName(game+"-"+version) + ".gob.gz"
}

// LoadShipped is a game's shipped schema: the exact version's file, else the newest by the names' digit
// runs (1.20.0.3 over 1.9.2) with exact false; nil and no error when the game has none.
func LoadShipped(gameID, version string) (sch *Schema, exact bool, err error) {
	names, err := fs.Glob(shippedFiles, gameID+"-*.gob.gz")
	if err != nil || len(names) == 0 {
		return nil, false, err
	}
	name := ShippedName(gameID, version)
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
	defer file.Close()
	if err := appdata.Decode(file, &sch); err != nil {
		return nil, false, err
	}
	return sch, exact, nil
}
