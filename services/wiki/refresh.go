// refresh.go fetches the wiki's listing and the pages it names.

package wiki

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"paradox-modding-tools/services/app"
)

// Summary is one page in the listing.
type Summary struct {
	Title    string `json:"title"`
	Category string `json:"category"`
}

// Listing is the modding pages and patch versions the wiki has for one game.
type Listing struct {
	Pages       []Summary `json:"pages"`
	Versions    []string  `json:"versions"`
	RefreshedAt time.Time `json:"refreshedAt"`
}

// answer is one reply of the wiki's API to a category query.
type answer struct {
	Continue map[string]string `json:"continue"`
	Query    struct {
		CategoryMembers []struct {
			Title string `json:"title"`
		} `json:"categorymembers"`
	} `json:"query"`
}

const (
	moddingCategory = "Modding"
	patchCategory   = "Patches"
	listingFile     = "listing.json"
	staleAfter      = 7 * 24 * time.Hour
	patchPrefix     = "Patch "
)

// patchTitle matches a patch page's title, such as "Patch 1.20.X", and holds
// the version.
var patchTitle = regexp.MustCompile(`^` + patchPrefix + `(\d+(?:\.\d+)*(?:\.X)?)$`)

// Refresh fetches the listing and each page that is missing or stale, calling
// progress after each page. It returns an error if the listing fails. A page
// that fails does not stop the others.
func (c *Client) Refresh(progress func(done, total int)) error {
	listing, err := c.fetchListing()
	if err != nil {
		return err
	}

	if err := app.SaveJSON(c.resolveListing(), listing); err != nil {
		return err
	}

	stale := c.listStale(listing)
	var errs []error
	for position, title := range stale {
		_, err := c.FetchPage(title)
		errs = append(errs, err)
		progress(position+1, len(stale))
	}

	return errors.Join(errs...)
}

// LoadListing returns the cached listing, or false if there is none.
func (c *Client) LoadListing() (Listing, bool) {
	listing, err := app.LoadJSON[Listing](c.resolveListing())

	return listing, err == nil && !listing.RefreshedAt.IsZero()
}

func (c *Client) fetchListing() (Listing, error) {
	listing := Listing{RefreshedAt: time.Now()}
	titles, err := c.listCategory(moddingCategory)
	if err != nil {
		return Listing{}, err
	}

	listing.Pages = make([]Summary, len(titles))
	for position, title := range titles {
		listing.Pages[position] = Summary{Title: title, Category: moddingCategory}
	}

	titles, err = c.listCategory(patchCategory)
	if err != nil {
		return Listing{}, err
	}

	for _, title := range titles {
		if match := patchTitle.FindStringSubmatch(title); match != nil {
			listing.Versions = append(listing.Versions, match[1])
		}
	}

	return listing, nil
}

func (c *Client) listCategory(category string) ([]string, error) {
	params := url.Values{
		"action":      {"query"},
		"list":        {"categorymembers"},
		"cmtitle":     {"Category:" + category},
		"cmnamespace": {"0"},
		"cmlimit":     {"500"},
	}
	var titles []string
	for {
		var reply answer
		if err := c.fetch(params, &reply); err != nil {
			return nil, err
		}

		for _, member := range reply.Query.CategoryMembers {
			titles = append(titles, member.Title)
		}

		if reply.Continue == nil {
			return titles, nil
		}

		// A long category comes in parts, and continue asks for the next.
		for key, value := range reply.Continue {
			params.Set(key, value)
		}
	}
}

// listStale returns the titles of listed pages that are not cached or are too
// old.
func (c *Client) listStale(listing Listing) []string {
	titles := make([]string, 0, len(listing.Pages)+len(listing.Versions))
	for _, summary := range listing.Pages {
		titles = append(titles, summary.Title)
	}

	for _, version := range listing.Versions {
		titles = append(titles, resolvePatch(version))
	}

	return slices.DeleteFunc(titles, func(title string) bool {
		page, ok := c.LoadPage(title)

		return ok && time.Since(page.FetchedAt) <= staleAfter
	})
}

func (c *Client) resolveListing() string {
	return filepath.Join(c.dir, listingFile)
}

func resolvePatch(version string) string {
	return patchPrefix + version
}
