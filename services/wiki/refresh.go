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

// A Summary is one page in the listing: its title and the category it is in.
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

// The wiki's two categories this package lists, the file the listing is cached in, how old a cached page may be and
// how the title of a patch page begins.
const (
	moddingCategory = "Modding"
	patchCategory   = "Patches"
	listingFile     = "listing.json"
	staleAfter      = 7 * 24 * time.Hour
	patchPrefix     = "Patch "
)

// patchTitle matches the title of one patch's page, such as "Patch 1.20", or of a minor version's hotfix notes, such
// as "Patch 1.20.X", and holds the version.
var patchTitle = regexp.MustCompile(`^` + patchPrefix + `(\d+(?:\.\d+)*(?:\.X)?)$`)

// Refresh fetches the listing and every page it names that is missing or stale, calling the given function after each
// page. It returns an error if the listing cannot be fetched or cached. A page that cannot be fetched does not stop
// the others, and the errors of such pages are joined and returned.
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
	for done, title := range stale {
		_, err := client.FetchPage(title)
		errs = append(errs, err)
		progress(done+1, len(stale))
	}
	return errors.Join(errs...)
}

// LoadListing returns the cached listing, or false if there is none.
func (client *Client) LoadListing() (Listing, bool) {
	listing, err := app.LoadJSON[Listing](client.resolveListing())
	return listing, err == nil && !listing.RefreshedAt.IsZero()
}

// fetchListing fetches the modding pages and the patch versions the wiki has. It returns an error if either category
// cannot be fetched.
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

// listCategory returns the titles of the articles in one of the wiki's categories. It returns an error if a request
// to the wiki fails.
func (client *Client) listCategory(category string) ([]string, error) {
	params := url.Values{
		"action":      {"query"},
		"list":        {"categorymembers"},
		"cmtitle":     {"Category:" + category},
		"cmnamespace": {"0"},
		"cmlimit":     {"500"},
	}
	var titles []string
	for {
		var response struct {
			Continue map[string]string `json:"continue"`
			Query    struct {
				CategoryMembers []struct {
					Title string `json:"title"`
				} `json:"categorymembers"`
			} `json:"query"`
		}
		if err := client.fetch(params, &response); err != nil {
			return nil, err
		}
		for _, member := range response.Query.CategoryMembers {
			titles = append(titles, member.Title)
		}
		if response.Continue == nil {
			return titles, nil
		}
		for key, value := range response.Continue {
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
