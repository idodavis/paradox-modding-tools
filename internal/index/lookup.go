package index

import (
	"github.com/samber/lo"

	"paradox-modding-tools/internal/schema"
)

// The lookups are built on lo.
var _ = lo.Keys[string, int]

// Resolve returns the definitions that a reference names, in load order. A local definition is found only from
// its own file, which is why the referring file is passed. The result is empty if the name is undefined.
func (index *Index) Resolve(reference schema.Reference, file int32) []Site { panic("stub") }

// ListUses returns every reference to a name in a database, in load order.
func (index *Index) ListUses(database, name string) []Site { panic("stub") }

// ListDefinitions returns every definition in a database, in load order.
func (index *Index) ListDefinitions(database string) []Site { panic("stub") }

// ListMessages returns every message with the given key, one per language and file, in load order.
func (index *Index) ListMessages(key string) []Site { panic("stub") }

// FindDefinition returns the definition that a site sits inside, or false if the site is at the top of its file.
func (index *Index) FindDefinition(site Site) (Site, bool) { panic("stub") }

// FindNearest returns the innermost occurrence in a file that covers the given byte offset, or false if none
// does.
func (index *Index) FindNearest(file int32, offset int) (Site, bool) { panic("stub") }

// CountDefinitions counts a file's definitions by database.
func (index *Index) CountDefinitions(file int32) map[string]int { panic("stub") }

// buildPostings fills the postings from the occurrences after a scan or a load.
func (index *Index) buildPostings() { panic("stub") }
