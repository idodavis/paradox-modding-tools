// Package steam publishes mods to the Steam Workshop. It reads and writes a mod's listing, lists the files an
// upload sends, converts the description to BBCode, compares an upload with Steam, and publishes through the
// game's own copy of Steam's library in a second copy of the app.
package steam

import (
	"time"

	"paradox-modding-tools/internal/files"
)

// A Preview is one picture or video in the Workshop page's gallery.
type Preview struct {
	Path  string `json:"path"`
	Type  string `json:"type"` // image, video
	URL   string `json:"url"`  // a video's
	Order int    `json:"order"`
}

// A Localized is a title and a description in one language.
type Localized struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// A Listing is what a mod's Workshop page shows, as the mod's own files hold it.
type Listing struct {
	WorkshopID      string               `json:"workshopId"`
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	Source          string               `json:"source"` // markdown, bbcode, steam
	DescriptionPath string               `json:"descriptionPath"`
	Changelog       string               `json:"changelog"`
	ChangelogPath   string               `json:"changelogPath"`
	Thumbnail       string               `json:"thumbnail"`
	Previews        []Preview            `json:"previews"`
	Tags            []string             `json:"tags"`
	DLC             []string             `json:"dlc"`
	Languages       map[string]Localized `json:"languages"`
	Visibility      string               `json:"visibility"` // private, friends, public, unlisted
}

// ReadListing reads a mod's listing from its descriptor and its workshop folder.
func (stm *Service) ReadListing(modID string) (Listing, error) { panic("stub") }

// WriteListing writes a mod's listing back to its files: the descriptor's fields and the workshop folder's.
func (stm *Service) WriteListing(modID string, listing Listing) error { panic("stub") }

// An Upload is the set of files a mod's next upload sends, with their total size and what the rules left out.
type Upload struct {
	Files    []files.File `json:"files"`
	Bytes    int64        `json:"bytes"`
	Excluded []string     `json:"excluded"` // the rels the rules left out
}

// ListUpload returns the files a mod's next upload sends: the mod's files except dotfiles, the metadata folder,
// the previews and whatever the mod's exclusions cover.
func (stm *Service) ListUpload(modID string) (Upload, error) { panic("stub") }

// A Snapshot records what the last upload sent: its files' hashes and its listing, so that the next compare knows
// what changed.
type Snapshot struct {
	WorkshopID string            `json:"workshopId"`
	UploadedAt time.Time         `json:"uploadedAt"`
	Files      map[string]string `json:"files"` // by relative path: the file's hash
	Listing    Listing           `json:"listing"`
}

// LoadSnapshot reads the snapshot of a Workshop item, or returns false if there is none.
func LoadSnapshot(cacheDir, workshopID string) (Snapshot, bool) { panic("stub") }

// SaveSnapshot writes an upload's snapshot.
func SaveSnapshot(cacheDir string, snapshot Snapshot) error { panic("stub") }

// Details is what Steam reports about an item.
type Details struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Visibility  string    `json:"visibility"`
	Tags        []string  `json:"tags"`
	Subscribers int       `json:"subscribers"`
}

// A Comparison says what the next upload changes against the last one, and how the listing differs from Steam's.
type Comparison struct {
	Added   []string `json:"added"`
	Changed []string `json:"changed"`
	Removed []string `json:"removed"`
	Listing []string `json:"listing"` // the listing's fields that differ: title, description, tags …
	Steam   *Details `json:"steam,omitempty"`
}

// Compare compares a listing and an upload with the last snapshot and with Steam's details.
func Compare(listing Listing, upload Upload, snapshot Snapshot, details *Details) Comparison {
	panic("stub")
}
