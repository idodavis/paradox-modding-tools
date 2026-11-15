// digest.go downloads a digest repository from GitHub and reads its versions
// into a game's patches.

package patch

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/internal/schema"
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
	if err := json.Unmarshal(body, &commit); err != nil || commit.SHA == "" {
		return "", errors.Join(fmt.Errorf("GitHub gave no commit for %s", digests), err)
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

// readArchive reads every version of a digest, given as owner/name, into a
// patch, oldest first, and returns the dumps of each version an install is
// on, by the install's version. It compares a version's dumps with its base's,
// and reads past a file it cannot read: that is joined into the error it
// returns beside the patches.
func readArchive(
	source archive, digests string, language *lang.Schema, installs []string,
) (patch.Patches, map[string]*schema.Dumps, error) {
	var patches patch.Patches
	var skipped error
	var baseDumps *schema.Dumps
	installDumps := map[string]*schema.Dumps{}
	latest := map[string]string{}

	for _, folder := range source.listVersions() {
		version := path.Base(folder)
		skipped = errors.Join(skipped, source.readDumps(folder, latest))
		dumps := schema.ParseDumps(latest)
		dumps.Version = version

		gameFiles, filesErr := readGameFiles(source, folder)
		articles, renames, notesErr := readNotes(source, digests, folder, language)
		skipped = errors.Join(skipped, filesErr, notesErr)
		gamePatch := patch.Patch{
			Version: version, Base: patches.ResolveBase(version), GameFiles: gameFiles, KeyRenames: renames,
			Articles: articles,
		}
		if gamePatch.Base != "" {
			gamePatch.DumpChanges = schema.CompareDumps(baseDumps, dumps, language)
		}

		for _, install := range installs {
			if game.IsInstalled(version, install) {
				installDumps[install] = toShipped(dumps)
			}
		}

		if !game.IsBeta(version) {
			baseDumps = dumps
		}

		patches = append(patches, gamePatch)
	}

	return patches, installDumps, skipped
}

// otherFolders are the top folders of a digest that hold no version: its own
// copy of the newest dumps, its pictures and its scripts.
var otherFolders = []string{"documentation", "current_docs", "image", "script"}

// listVersions returns the folder of each version of the digest, oldest first.
// A version's folder is the one that holds a docs folder, whether it is flat,
// as 1.15.1, or under a major version's folder, as 1.4/1.4.7.
func (a archive) listVersions() []string {
	var folders []string
	for file := range a {
		folder, ok := strings.CutSuffix(path.Dir(file), "/docs")
		top, _, _ := strings.Cut(folder, "/")
		if ok && !slices.Contains(otherFolders, top) && !slices.Contains(folders, folder) {
			folders = append(folders, folder)
		}
	}

	slices.SortFunc(folders, func(first, second string) int {
		return game.CompareVersions(path.Base(first), path.Base(second))
	})

	return folders
}

// readDumps reads the dump files of a version's folder into the latest texts,
// by file name. The Co-ops leave out a file that did not change, so a version
// that lacks one keeps the text of the newest version before it that has it.
// A file that cannot be read keeps the older text, and the error says so.
func (a archive) readDumps(folder string, latest map[string]string) error {
	var skipped error
	for _, file := range a.listFolder(folder + "/docs") {
		if !schema.IsDumpFile(file) {
			continue
		}

		text, err := a.read(folder + "/docs/" + file)
		if err != nil {
			skipped = errors.Join(skipped, err)

			continue
		}

		latest[file] = text
	}

	return skipped
}

// toShipped returns the dumps as the shipped backup carries them: no command
// has the file and line it was read from, since those are the digest's and
// not on this machine.
func toShipped(dumps *schema.Dumps) *schema.Dumps {
	for position := range dumps.Commands {
		dumps.Commands[position].Path, dumps.Commands[position].Line = "", 0
	}

	return dumps
}

// gitStatuses are the letters a digest's list of changed files writes before a
// file, and what each says the version did to the file.
var gitStatuses = map[string]schema.Status{
	"M": schema.StatusChanged, "A": schema.StatusAdded, "D": schema.StatusRemoved,
}

// changedLine is a line of changes_files.md, as " - **M** game/common/x.txt".
var changedLine = regexp.MustCompile(`^ - \*\*([A-Z])\*\* (.+)$`)

// readGameFiles reads the game files a version's changes_files.md lists,
// sorted by path. A file outside the game's own folder, as caligula_rev.txt
// is, is left out. It returns none for a version that has no such list.
func readGameFiles(source archive, folder string) ([]patch.GameFile, error) {
	file := folder + "/changes_files.md"
	if _, ok := source[file]; !ok {
		return nil, nil
	}

	text, err := source.read(file)
	if err != nil {
		return nil, err
	}

	var changed []patch.GameFile
	for line := range strings.Lines(text) {
		match := changedLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if match == nil {
			continue
		}

		rel, inGame := strings.CutPrefix(match[2], "game/")
		if status, known := gitStatuses[match[1]]; inGame && known {
			changed = append(changed, patch.GameFile{Rel: rel, Status: status})
		}
	}

	slices.SortFunc(changed, patch.CompareFiles)

	return changed, nil
}
