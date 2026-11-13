// update.go checks for newer versions, installs releases and lists them.

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

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
	"golang.org/x/mod/semver"
)

// Release is one published version of the app, with its notes in Markdown.
type Release struct {
	Version string    `json:"version"`
	Date    time.Time `json:"date"`
	Notes   string    `json:"notes"`
	Running bool      `json:"running"`
	Older   bool      `json:"older"`

	// Installable is false for a release older than firstInstallable, whose
	// zip the updater cannot install.
	Installable bool `json:"installable"`
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

type publishedAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// releaseSource is the updater's one provider: GitHub's, or the release the
// user picked.
type releaseSource struct {
	*github.Provider

	// version is the version the user picked, or "" for none.
	version string

	// refusal is why the last check failed, or nil if it worked.
	refusal error
}

const (
	repository    = "idodavis/paradox-modding-tools"
	releasesURL   = "https://api.github.com/repos/" + repository + "/releases"
	checksumAsset = "SHA256SUMS"
	apiTimeout    = 15 * time.Second
)

// Check returns the release to install, or nil if there is none. It keeps its
// error as the refusal, which says why better than the updater's own error.
func (r *releaseSource) Check(ctx context.Context, request updater.CheckRequest) (*updater.Release, error) {
	check := r.Provider.Check
	if r.version != "" {
		check = r.find
	}

	release, err := check(ctx, request)
	if err != nil && strings.Contains(err.Error(), "rate limit") {
		err = errRateLimited
	}
	r.refusal = err

	return release, err
}

// assetURLKey is where the GitHub provider's Download finds a file's address.
const assetURLKey = "github.asset.url"

// find returns the release the user picked, with this platform's file and its
// digest from SHA256SUMS. It returns an error if any of those is missing.
func (r *releaseSource) find(_ context.Context, request updater.CheckRequest) (*updater.Release, error) {
	published, err := fetchJSON[publishedRelease](releasesURL + "/tags/v" + r.version)
	if err != nil {
		return nil, err
	}

	assets := make([]github.ReleaseAsset, len(published.Assets))
	for position, asset := range published.Assets {
		assets[position] = github.ReleaseAsset{Name: asset.Name, Size: asset.Size, URL: asset.URL}
	}

	position := github.DefaultAssetMatcher(request, assets)
	if position < 0 {
		return nil, fmt.Errorf("%s has no file for %s/%s", r.version, request.Platform, request.Arch)
	}

	asset := published.Assets[position]
	digest, err := lookupDigest(published.Assets, asset.Name)
	if err != nil {
		return nil, err
	}

	return &updater.Release{
		Version:      r.version,
		Notes:        published.Body,
		PublishedAt:  published.Published,
		Artifact:     updater.Artifact{Filename: asset.Name, Size: asset.Size},
		Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest},
		Metadata:     map[string]any{assetURLKey: asset.URL},
	}, nil
}

// startUpdater points the updater at the project's GitHub releases and skips
// the release the user went back from.
func (s *Service) startUpdater() error {
	provider, err := github.New(github.Config{Repository: repository, ChecksumAsset: checksumAsset})
	if err != nil {
		return err
	}

	s.source = &releaseSource{Provider: provider}
	pmt := application.Get()
	err = pmt.Updater.Init(updater.Config{CurrentVersion: s.Build, Providers: []updater.Provider{s.source}})
	if err != nil {
		return err
	}

	pmt.Updater.SkipVersion(s.Config.Config().Skipped)

	return nil
}

// CheckUpdate asks GitHub for a newer version and returns it, or "" if there
// is none.
func (s *Service) CheckUpdate() (string, error) {
	s.updating.Lock()
	defer s.updating.Unlock()

	release, err := s.checkRelease()
	if err != nil || release == nil {
		return "", err
	}

	return release.Version, nil
}

// checkRelease returns the release the source offers, or nil. A dev build is
// no release, so it never asks. A failed check returns the source's refusal,
// which says why.
func (s *Service) checkRelease() (*updater.Release, error) {
	if s.Build == DevBuild {
		return nil, nil
	}

	release, err := application.Get().Updater.Check(context.Background())
	if err != nil && s.source.refusal != nil {
		return nil, s.source.refusal
	}

	return release, err
}

// ApplyUpdate downloads the newer version and restarts into it. It returns an
// error if there is none.
func (s *Service) ApplyUpdate() error {
	s.updating.Lock()
	defer s.updating.Unlock()

	release, err := s.checkRelease()
	if err != nil {
		return err
	}

	if release == nil {
		return errors.New("no update is available")
	}

	pmt := application.Get()
	if err := pmt.Updater.DownloadAndInstall(context.Background()); err != nil {
		return err
	}

	return pmt.Updater.Restart(context.Background())
}

// InstallRelease installs a release, newer or older than the running one, and
// restarts into it. Going back skips the running version until a newer one is
// out. It refuses a dev build and the running version.
func (s *Service) InstallRelease(version string) error {
	if s.Build == DevBuild {
		return errors.New("a dev build installs no release; build or download one instead")
	}

	running := s.running()
	if version == running {
		return fmt.Errorf("%s is the version running", version)
	}

	s.updating.Lock()
	defer s.updating.Unlock()

	s.source.version = version
	defer func() { s.source.version = "" }()

	// A check offers no version the user went back from, and that version
	// may be picked here.
	pmt := application.Get()
	skipped := pmt.Updater.SkippedVersion()
	pmt.Updater.SkipVersion("")
	defer pmt.Updater.SkipVersion(skipped)

	release, err := s.checkRelease()
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
		err := s.Config.Update("settings", func(config *Config) error {
			config.Skipped = running

			return nil
		})
		if err != nil {
			return err
		}
	}

	return pmt.Updater.Restart(context.Background())
}

const (
	pageSize = 100

	// firstRelease is this app's first version.
	firstRelease = "0.5.0"

	// firstInstallable is the first release whose zip holds only the binary.
	firstInstallable = "0.5.2"
)

// ListReleases returns the published releases of this app, newest first, with
// their notes. A build that is no release gets only the newest.
func (s *Service) ListReleases() ([]Release, error) {
	running := s.running()

	var releases []Release
	for page := 1; ; page++ {
		batch, err := fetchJSON[[]publishedRelease](fmt.Sprintf("%s?per_page=%d&page=%d", releasesURL, pageSize, page))
		if err != nil {
			return nil, err
		}

		for _, published := range batch {
			version := strings.TrimPrefix(published.Tag, "v")
			if published.Draft || published.Prerelease || semver.Compare("v"+version, "v"+firstRelease) < 0 {
				continue
			}

			releases = append(releases, Release{
				Version: version, Date: published.Published, Notes: published.Body,
				Running: version == running, Older: isOlder(version, running),
				Installable: semver.Compare("v"+version, "v"+firstInstallable) >= 0,
			})
		}

		if len(batch) < pageSize {
			break
		}
	}

	if !slices.ContainsFunc(releases, func(release Release) bool { return release.Running }) {
		return releases[:min(len(releases), 1)], nil
	}

	return releases, nil
}

// running returns the version running, without a leading v.
func (s *Service) running() string { return strings.TrimPrefix(s.Build, "v") }

func fetchJSON[T any](address string) (T, error) {
	var value T

	body, err := FetchURL(address, apiTimeout)
	if err != nil {
		return value, err
	}

	err = json.Unmarshal(body, &value)

	return value, err
}

// isOlder reports whether a version is older than the running one.
func isOlder(version, running string) bool {
	return semver.Compare("v"+version, "v"+running) < 0
}

// lookupDigest returns the digest that a release's SHA256SUMS lists for a file.
// It returns an error if the release has no SHA256SUMS or no line for the file.
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
