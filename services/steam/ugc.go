package steam

import "github.com/ebitengine/purego"

// The client binds Steam's functions through purego, without cgo.
var _ = purego.RegisterLibFunc

// A Progress is one step of an upload, the data of the steam:progress event.
type Progress struct {
	Stage string `json:"stage"` // preparing, uploading, committing, done
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// A Client is the game's own copy of Steam's library, loaded in the publishing process.
type Client struct {
	handle uintptr
	appID  int
}

// Open loads Steam's library from the given path and initializes it for the game's app id. Steam must be running
// and signed in.
func Open(libraryPath string, appID int) (*Client, error) { panic("stub") }

// Close shuts the library down.
func (client *Client) Close() { panic("stub") }

// FetchDetails reads an item's details from Steam.
func (client *Client) FetchDetails(workshopID string) (Details, error) { panic("stub") }

// CreateItem creates a new Workshop item for the app and returns its id.
func (client *Client) CreateItem() (string, error) { panic("stub") }

// Submit uploads a folder and a listing to an item, calling the progress function until Steam commits.
func (client *Client) Submit(workshopID string, listing Listing, uploadDir string, progress func(Progress)) error {
	panic("stub")
}
