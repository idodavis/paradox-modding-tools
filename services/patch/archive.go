// archive.go asks GitHub for a digest and reads its zip.

package patch

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/app"
)

const (
	headTimeout = 30 * time.Second
	zipTimeout  = 5 * time.Minute

	// fileLimit is the most a file of the zip may hold when read. A digest's
	// largest file is a few megabytes.
	fileLimit = 64 << 20
)

// archive is the files of a digest's zip, by path from the digest's own
// folder. Nothing is written to disk, so a path in the zip is only ever a key.
type archive map[string]*zip.File

// fetchHead returns the id of the newest commit of a GitHub repository, given
// as owner/name. It is one small request, so an unchanged digest costs no
// download.
func fetchHead(digests string) (string, error) {
	body, err := app.FetchURL("https://api.github.com/repos/"+digests+"/commits/HEAD", headTimeout)
	if err != nil {
		return "", err
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", fmt.Errorf("GitHub's answer for %s is not a commit: %w", digests, err)
	}

	if commit.SHA == "" {
		return "", fmt.Errorf("GitHub gave no commit for %s", digests)
	}

	return commit.SHA, nil
}

// fetchArchive downloads the newest zip of a GitHub repository, given as
// owner/name, and opens it in memory. It returns an error if the zip does not
// open or its files are not all in one folder, as GitHub's zips are.
func fetchArchive(digests string) (archive, error) {
	body, err := app.FetchURL("https://codeload.github.com/"+digests+"/zip/HEAD", zipTimeout)
	if err != nil {
		return nil, err
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("the zip of %s does not open: %w", digests, err)
	}

	top := files.FindZipFolder(reader)
	if top == "" {
		return nil, fmt.Errorf("the zip of %s has no single folder of files", digests)
	}

	contents := archive{}
	for _, entry := range reader.File {
		if !entry.FileInfo().IsDir() {
			contents[strings.TrimPrefix(entry.Name, top+"/")] = entry
		}
	}

	return contents, nil
}

// read returns the text of a file of the archive, which it decodes as the
// game's own files are. It returns an error for a file the archive does not
// have, or one that does not read or is over fileLimit.
func (a archive) read(file string) (string, error) {
	entry, ok := a[file]
	if !ok {
		return "", fmt.Errorf("the digest has no %s", file)
	}

	reader, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}

	content, err := io.ReadAll(io.LimitReader(reader, fileLimit+1))
	if err = errors.Join(err, reader.Close()); err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}

	if len(content) > fileLimit {
		return "", fmt.Errorf("%s is over %d bytes", file, fileLimit)
	}

	return files.DecodeText(content).Text, nil
}

// listFolder returns the files directly in a folder of the archive, sorted and
// without the folder.
func (a archive) listFolder(folder string) []string {
	var listing []string
	for file := range a {
		if path.Dir(file) == folder {
			listing = append(listing, path.Base(file))
		}
	}

	slices.Sort(listing)

	return listing
}

// holds reports whether the archive has a file.
func (a archive) holds(file string) bool {
	_, ok := a[file]

	return ok
}
