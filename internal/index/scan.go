package index

import (
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// The scan's parallel walk is built on errgroup.
var _ errgroup.Group

// Scan builds an index of every file under the given roots, in parallel. A script, GUI or asset file goes through
// the schema's walk with the collecting visitor, and a localization file through the loc parser. The roots are
// the game's, then each DLC's, then each mod's, in load order, and the progress function is called at each stage.
func Scan(game *game.Game, schema *schema.Schema, roots []files.Root, progress func(stage string)) (*Index, error) {
	panic("stub")
}

// ScanMods builds the index of a workspace's mods on top of the install's index, so that a lookup in the mods'
// index falls through to the install's. The index records every language the mods write, even one the install
// does not.
func ScanMods(game *game.Game, schema *schema.Schema, install *Index, roots []files.Root, replaced []string) (*Index, error) {
	panic("stub")
}

// ScanFile walks one file again after it changed and replaces its occurrences and postings. A nil tree means the
// file was deleted, and its occurrences are dropped.
func (index *Index) ScanFile(schema *schema.Schema, file files.File, tree *jomini.Tree) error {
	panic("stub")
}

// A collector is the scan's visitor. It turns the definition and the references of each statement into
// occurrences.
type collector struct {
	index       *Index
	file        int32
	occurrences []Occurrence
}

// Enter records the statement's definition and references.
func (collector *collector) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	panic("stub")
}

// Leave does nothing: an occurrence is complete when it is entered.
func (collector *collector) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}
