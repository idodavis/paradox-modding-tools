package wiki

// FindPages returns the wiki pages that belong to a file: the pages whose title contains the name of the file's
// database or format, exact matches first, then plurals, then any that contain it. A game's row names the page
// only when that rule finds nothing.
func (wik *Service) FindPages(path string) ([]Summary, error) { panic("stub") }

// A Note is one patch's notes page: its version and whether the page has a Modding section.
type Note struct {
	Version string `json:"version"`
	Title   string `json:"title"`
	Modding bool   `json:"modding"`
}

// ListNotes returns the patch notes the listing knows, newest first.
func (client *Client) ListNotes() ([]Note, error) { panic("stub") }

// ReadNote returns the patch notes page of a version.
func (client *Client) ReadNote(version string) (Page, error) { panic("stub") }
