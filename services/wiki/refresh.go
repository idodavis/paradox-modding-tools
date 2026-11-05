// refresh.go fetches the wiki's listing, then every page it names that the cache lacks or holds stale.

package wiki

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"time"

	"github.com/samber/lo"

	"paradox-modding-tools/services/app"
)

// A Summary is one page in the listing.
type Summary struct {
	Title    string `json:"title"`
	Category string `json:"category"`
}

// A Listing is what the wiki has for one game: its modding pages and its patch versions.
type Listing struct {
	Pages       []Summary `json:"pages"`
	Versions    []string  `json:"versions"`
	RefreshedAt time.Time `json:"refreshedAt"`
}

// An answer is one reply of the wiki's API to a query for a category's members, and how to ask for the rest.
type answer struct {
	Continue map[string]string `json:"continue"`
	Query    struct {
		CategoryMembers []struct {
			Title string `json:"title"`
		} `json:"categorymembers"`
	} `json:"query"`
}

// The wiki's two listed categories, the listing's cache file, the age at which a cached page is stale and the start of
// a patch page's title.
const (
	moddingCategory = "Modding"
	patchCategory   = "Patches"
	listingFile     = "listing.json"
	staleAfter      = 7 * 24 * time.Hour
	patchPrefix     = "Patch "
)

// patchTitle matches the title of a patch's page, such as "Patch 1.20" or "Patch 1.20.X", and holds the version.
var patchTitle = regexp.MustCompile(`^` + patchPrefix + `(\d+(?:\.\d+)*(?:\.X)?)$`)

// Refresh fetches the listing and every page it names that is missing or stale, calling the given function after each
// page. It returns an error if the listing fails. A page that fails does not stop the others; their errors are joined.
func (client *Client) Refresh(progress func(done, total int)) error {
	listing, err := client.fetchListing()
	if err != nil {
		return err
	}
	if err := app.SaveJSON(client.resolveListing(), listing); err != nil {
		return err
	}
	stale := client.listStale(listing)
	var errs []error
	for position, title := range stale {
		_, err := client.FetchPage(title)
		errs = append(errs, err)
		progress(position+1, len(stale))
	}
	return errors.Join(errs...)
}

// LoadListing returns the cached listing, or false if there is none.
func (client *Client) LoadListing() (Listing, bool) {
	listing, err := app.LoadJSON[Listing](client.resolveListing())
	return listing, err == nil && !listing.RefreshedAt.IsZero()
}

// fetchListing fetches the modding pages and the patch versions the wiki has.
func (client *Client) fetchListing() (Listing, error) {
	listing := Listing{RefreshedAt: time.Now()}
	titles, err := client.listCategory(moddingCategory)
	if err != nil {
		return Listing{}, err
	}
	listing.Pages = lo.Map(titles, func(title string, _ int) Summary {
		return Summary{Title: title, Category: moddingCategory}
	})
	titles, err = client.listCategory(patchCategory)
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

// listCategory returns the titles of the articles in one of the wiki's categories.
func (client *Client) listCategory(category string) ([]string, error) {
	params := url.Values{
		"action":      {"query"},
		"list":        {"categorymembers"},
		"cmtitle":     {"Category:" + category},
		"cmnamespace": {"0"},
		"cmlimit":     {"500"},
	}
	var titles []string
	err := client.queryAll(params, func(reply answer) {
		for _, member := range reply.Query.CategoryMembers {
			titles = append(titles, member.Title)
		}
	})
	return titles, err
}

// queryAll sends a query to the wiki's API, then each query for the rest of its answer, and hands every reply to the
// given function. It returns an error if a request fails.
func (client *Client) queryAll(params url.Values, each func(reply answer)) error {
	for {
		var reply answer
		if err := client.fetch(params, &reply); err != nil {
			return err
		}
		each(reply)
		if reply.Continue == nil {
			return nil
		}
		for key, value := range reply.Continue {
			params.Set(key, value)
		}
	}
}

// listStale returns the titles of the pages of a listing that are not cached or whose cached copy is too old.
func (client *Client) listStale(listing Listing) []string {
	titles := lo.Map(listing.Pages, func(summary Summary, _ int) string { return summary.Title })
	titles = append(titles, lo.Map(listing.Versions, func(version string, _ int) string {
		return resolvePatch(version)
	})...)
	return lo.Filter(titles, func(title string, _ int) bool {
		page, ok := client.LoadPage(title)
		return !ok || time.Since(page.FetchedAt) > staleAfter
	})
}

// resolveListing returns the file the listing is cached in.
func (client *Client) resolveListing() string {
	return filepath.Join(client.dir, listingFile)
}

// resolvePatch returns the title of the page of one patch version.
func resolvePatch(version string) string {
	return patchPrefix + version
}
