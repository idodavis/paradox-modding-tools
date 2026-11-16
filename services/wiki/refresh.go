// refresh.go fetches the wiki's listing and the pages it names.

package wiki

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/app"
)

// Listing is the modding pages and patch versions the wiki has for one game.
// Pages holds the titles of the pages.
type Listing struct {
	Pages       []string  `json:"pages"`
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
	patchPrefix     = "Patch "

	// titlesPerQuery is how many titles one revision query may name, which is
	// the wiki's limit.
	titlesPerQuery = 50
)

// patchTitle matches a patch page's title, such as "Patch 1.20.X", and holds
// the version.
var patchTitle = regexp.MustCompile(`^` + patchPrefix + `(\d+(?:\.\d+)*(?:\.X)?)$`)

// Refresh fetches the listing and each page that is not cached or has a newer
// revision on the wiki, calling progress after each page. It returns an error
// if the listing or a revision query fails. A page that fails does not stop the
// others.
func (c *Client) Refresh(progress func(done, total int)) error {
	listing, err := c.fetchListing()
	if err != nil {
		return err
	}

	if err := app.SaveJSON(c.resolveListing(), listing); err != nil {
		return err
	}

	changed, err := c.listChanged(listing)
	if err != nil {
		return err
	}

	var errs []error
	for position, title := range changed {
		_, err := c.FetchPage(title)
		errs = append(errs, err)
		progress(position+1, len(changed))
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
	pages, err := c.listCategory(moddingCategory)
	if err != nil {
		return Listing{}, err
	}

	listing.Pages = pages

	titles, err := c.listCategory(patchCategory)
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
		actionParam:   {"query"},
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

// listChanged returns the titles of listed pages that are not cached or whose
// cached revision is not the wiki's latest. A title the wiki gives no revision
// for, such as a moved page, is left out.
func (c *Client) listChanged(listing Listing) ([]string, error) {
	titles := make([]string, 0, len(listing.Pages)+len(listing.Versions))
	titles = append(titles, listing.Pages...)
	for _, version := range listing.Versions {
		titles = append(titles, resolvePatch(version))
	}

	var changed []string
	for group := range slices.Chunk(titles, titlesPerQuery) {
		revisions, err := c.fetchRevisions(group)
		if err != nil {
			return nil, err
		}

		for _, title := range group {
			revision, ok := revisions[title]
			if !ok {
				continue
			}

			if page, ok := c.LoadPage(title); !ok || page.Revision != revision {
				changed = append(changed, title)
			}
		}
	}

	return changed, nil
}

// fetchRevisions asks the wiki for the latest revision of each given page and
// returns them by title. A page the wiki has no revision for is left out.
func (c *Client) fetchRevisions(titles []string) (map[string]int64, error) {
	params := url.Values{actionParam: {"query"}, "prop": {"info"}, "titles": {strings.Join(titles, "|")}}
	var reply struct {
		Query struct {
			Pages []struct {
				Title    string `json:"title"`
				Revision int64  `json:"lastrevid"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := c.fetch(params, &reply); err != nil {
		return nil, err
	}

	revisions := make(map[string]int64, len(titles))
	for _, page := range reply.Query.Pages {
		if page.Revision != 0 {
			revisions[page.Title] = page.Revision
		}
	}

	return revisions, nil
}

func (c *Client) resolveListing() string {
	return filepath.Join(c.dir, listingFile)
}

func resolvePatch(version string) string {
	return patchPrefix + version
}
