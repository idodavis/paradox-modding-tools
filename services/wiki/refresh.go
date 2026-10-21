package wiki

import "time"

// A Summary is one page in the listing: its title and the category it is in.
type Summary struct {
	Title    string `json:"title"`
	Category string `json:"category"`
}

// A Listing is what the wiki has for one game: its modding pages and its patch versions. It is refreshed when the
// app starts.
type Listing struct {
	Pages       []Summary `json:"pages"`
	Versions    []string  `json:"versions"`
	RefreshedAt time.Time `json:"refreshedAt"`
}

// Refresh fetches the listing and every page it names that is missing or stale, calling the progress function
// after each page.
func (client *Client) Refresh(progress func(done, total int)) error { panic("stub") }

// LoadListing returns the cached listing, or false if there is none.
func (client *Client) LoadListing() (Listing, bool) { panic("stub") }
