// update.go is the app's updates: the check for a newer version, the install of any release, and the list of releases.

package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
	"golang.org/x/mod/semver"
)

const (
	repository    = "idodavis/paradox-modding-tools"
	releasesURL   = "https://api.github.com/repos/" + repository + "/releases" // GitHub lists the newest first
	checksumAsset = "SHA256SUMS"                                               // the file of digests each release carries
	// assetURLKey is the metadata key under which the GitHub provider's Download finds the address of a file.
	assetURLKey = "github.asset.url"
	pageSize    = 100 // the most releases GitHub lists in one page
	apiTimeout  = 15 * time.Second
	// firstRelease is this app's first version. The releases before it are an older app the repository keeps.
	firstRelease = "0.5.0"
)

// An Update is the result of a check for a newer version of the app. It is the data of the app:update event.
type Update struct {
	Version   string `json:"version"`
	Available bool   `json:"available"`
}

// A Release is one published version of the app, with its notes as the Markdown they are written in.
type Release struct {
	Version string    `json:"version"`
	Date    time.Time `json:"date"`
	Notes   string    `json:"notes"`
	Running bool      `json:"running"` // it is the version running
	Older   bool      `json:"older"`   // it is older than the version running
}

// publishedRelease is one release as GitHub's API describes it.
type publishedRelease struct {
	Tag        string           `json:"tag_name"`
	Body       string           `json:"body"`
	Published  time.Time        `json:"published_at"`
	Draft      bool             `json:"draft"`
	Prerelease bool             `json:"prerelease"`
	Assets     []publishedAsset `json:"assets"`
}

// publishedAsset is one file of a published release.
type publishedAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// A releaseSource is the updater's one provider. It offers what the GitHub provider offers, which is the newest
// release when that one is newer than the version running. While the user has picked a version, it offers that
// release instead, whichever way it compares with the version running.
type releaseSource struct {
	*github.Provider
	version string // the version the user picked, or "" for none
}

// Check returns the release to install, or nil if there is none. With no version picked it is the GitHub provider's
// answer. With one, it reads that release, picks its file for this platform and takes the file's digest from the
// release's SHA256SUMS. It returns an error if the release cannot be read, has no file for this platform, or has no
// line for the file in SHA256SUMS.
func (source *releaseSource) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	if source.version == "" {
		return source.Provider.Check(ctx, request)
	}
	published, err := fetchJSON[publishedRelease](releasesURL + "/tags/v" + source.version)
	if err != nil {
		return nil, err
	}
	assets := lo.Map(published.Assets, func(asset publishedAsset, _ int) github.ReleaseAsset {
		return github.ReleaseAsset{Name: asset.Name, Size: asset.Size, URL: asset.URL}
	})
	position := github.DefaultAssetMatcher(request, assets)
	if position < 0 {
		return nil, fmt.Errorf("%s has no file for %s/%s", source.version, request.Platform, request.Arch)
	}
	asset := published.Assets[position]
	digest, err := lookupDigest(published.Assets, asset.Name)
	if err != nil {
		return nil, err
	}
	return &updater.Release{
		Version:      source.version,
		Notes:        published.Body,
		PublishedAt:  published.Published,
		Artifact:     updater.Artifact{Filename: asset.Name, Size: asset.Size},
		Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest},
		Metadata:     map[string]any{assetURLKey: asset.URL},
	}, nil
}

// startUpdater points the updater at the project's GitHub releases. The release the user went back from stays
// skipped while the app runs. It returns an error if the updater refuses its setup.
func (app *Service) startUpdater() error {
	provider, err := github.New(github.Config{Repository: repository, ChecksumAsset: checksumAsset})
	if err != nil {
		return err
	}
	app.source = &releaseSource{Provider: provider}
	pmt := application.Get()
	err = pmt.Updater.Init(updater.Config{CurrentVersion: app.Build, Providers: []updater.Provider{app.source}})
	if err != nil {
		return err
	}
	pmt.Updater.SkipVersion(app.Config.Config().Skipped)
	return nil
}

// CheckUpdate asks the project's GitHub releases whether a newer version exists, and emits app:update if one does.
// It returns an empty Update if none does. A dev build is no release, so it never asks.
func (app *Service) CheckUpdate() (Update, error) {
	app.updating.Lock()
	defer app.updating.Unlock()
	return app.check()
}

// check is CheckUpdate for a caller that holds the update lock.
func (app *Service) check() (Update, error) {
	if app.Build == DevBuild {
		return Update{}, nil
	}
	release, err := application.Get().Updater.Check(context.Background())
	if err != nil || release == nil {
		return Update{}, err
	}
	update := Update{Version: release.Version, Available: true}
	application.Get().Event.Emit(EventAppUpdate, update)
	return update, nil
}

// ApplyUpdate downloads the newer version beside the app and restarts into it. It returns an error if there is
// none.
func (app *Service) ApplyUpdate() error {
	app.updating.Lock()
	defer app.updating.Unlock()
	update, err := app.check()
	if err != nil {
		return err
	}
	if !update.Available {
		return errors.New("no update is available")
	}
	pmt := application.Get()
	if err := pmt.Updater.DownloadAndInstall(context.Background()); err != nil {
		return err
	}
	return pmt.Updater.Restart(context.Background())
}

// InstallRelease installs the release with the given version, newer or older than the one running, and restarts into
// it. Going back to an older one skips the version running until a newer one is out. It returns an error if the
// version is the one running, has no file for this platform or no line in SHA256SUMS, or cannot be downloaded or
// verified.
func (app *Service) InstallRelease(version string) error {
	running := app.running()
	if version == running {
		return fmt.Errorf("%s is the version running", version)
	}
	app.updating.Lock()
	defer app.updating.Unlock()
	app.source.version = version
	defer func() { app.source.version = "" }()
	pmt := application.Get()
	release, err := pmt.Updater.Check(context.Background())
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("%s is not offered", version)
	}
	if err := pmt.Updater.DownloadAndInstall(context.Background()); err != nil {
		return err
	}
	if isOlder(version, running) {
		err := app.Config.Update("settings", func(config *Config) error {
			config.Skipped = running
			return nil
		})
		if err != nil {
			return err
		}
	}
	return pmt.Updater.Restart(context.Background())
}

// ListReleases returns every published release of this app, the newest first, each with its notes. A release before
// firstRelease is left out. A build that is no release, such as a dev build, gets the newest only. It returns an error
// if GitHub cannot be reached or answers with an error.
func (app *Service) ListReleases() ([]Release, error) {
	running := app.running()
	var releases []Release
	for page := 1; ; page++ {
		listed, err := fetchJSON[[]publishedRelease](fmt.Sprintf("%s?per_page=%d&page=%d", releasesURL, pageSize, page))
		if err != nil {
			return nil, err
		}
		for _, published := range listed {
			version := strings.TrimPrefix(published.Tag, "v")
			if published.Draft || published.Prerelease || semver.Compare("v"+version, "v"+firstRelease) < 0 {
				continue
			}
			releases = append(releases, Release{
				Version: version, Date: published.Published, Notes: published.Body,
				Running: version == running, Older: isOlder(version, running),
			})
		}
		if len(listed) < pageSize {
			break
		}
	}
	if !slices.ContainsFunc(releases, func(release Release) bool { return release.Running }) {
		return releases[:min(len(releases), 1)], nil
	}
	return releases, nil
}

// running returns the version running, without the v that a version given to the build may start with.
func (app *Service) running() string { return strings.TrimPrefix(app.Build, "v") }

// isOlder reports whether a version is older than the version running. It is false for a dev build, which is no
// version.
func isOlder(version, running string) bool {
	return semver.Compare("v"+version, "v"+running) < 0
}

// fetchJSON downloads a web address and decodes the JSON it answers into a value. It returns an error if the
// download fails or the answer is not that JSON.
func fetchJSON[T any](address string) (T, error) {
	var value T
	body, err := FetchURL(address, apiTimeout)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(body, &value)
	return value, err
}

// lookupDigest returns the SHA-256 digest that a release's SHA256SUMS lists for the file with the given name. It
// returns an error if the release has no SHA256SUMS, the file has no line in it, or the line is no digest.
func lookupDigest(assets []publishedAsset, name string) ([]byte, error) {
	position := slices.IndexFunc(assets, func(asset publishedAsset) bool { return asset.Name == checksumAsset })
	if position < 0 {
		return nil, fmt.Errorf("the release has no %s", checksumAsset)
	}
	listing, err := FetchURL(assets[position].URL, apiTimeout)
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return hex.DecodeString(fields[0])
		}
	}
	return nil, fmt.Errorf("%s has no line for %s", checksumAsset, name)
}
