// digest.go reads a digest repository into a game's patches.

package patch

import (
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// otherFolders are the top folders of a digest that hold no version: its own
// copy of the newest dumps, its pictures and its scripts.
var otherFolders = []string{"documentation", "current_docs", "image", "script"}

// readArchive reads every version of a digest into a patch, oldest first, and
// returns the dumps of each version an install is on, by the install's version.
// It compares a version's dumps with its base's, and reads past a file it
// cannot read: that is joined into the error it returns beside the patches.
func readArchive(source archive, language *lang.Schema, installs []string) (workspace.Patches, map[string]*schema.Dumps, error) {
	var patches workspace.Patches
	var skipped error
	var baseDumps *schema.Dumps
	kept := map[string]*schema.Dumps{}
	latest := map[string]string{}

	for _, folder := range source.listVersions() {
		version := path.Base(folder)
		skipped = errors.Join(skipped, source.readDumps(folder, latest))
		dumps := schema.ParseDumps(latest)
		dumps.Version = version

		patch, err := readPatch(source, folder, patches.ResolveBase(version), language)
		skipped = errors.Join(skipped, err)
		if patch.Base != "" {
			patch.DumpChanges = schema.CompareDumps(baseDumps, dumps, language)
		}

		for _, install := range installs {
			if game.IsInstalled(version, install) {
				kept[install] = toShipped(dumps)
			}
		}

		if !game.IsBeta(version) {
			baseDumps = dumps
		}

		patches = append(patches, patch)
	}

	return patches, kept, skipped
}

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
// file, and the status of the game file each stands for.
var gitStatuses = map[string]string{"M": workspace.FileModified, "A": workspace.FileAdded, "D": workspace.FileDeleted}

// changedLine is a line of changes_files.md, as " - **M** game/common/x.txt".
var changedLine = regexp.MustCompile(`^ - \*\*([A-Z])\*\* (.+)$`)

// readPatch reads what a version's folder says about the version: its game
// files and its written notes. The patch has no dump changes, which the caller
// compares. It reads past a file it cannot read, and returns what it skipped.
func readPatch(source archive, folder, base string, language *lang.Schema) (workspace.Patch, error) {
	gameFiles, err := readGameFiles(source, folder)
	articles, renames, notesErr := readNotes(source, folder, language)
	patch := workspace.Patch{
		Version: path.Base(folder), Base: base, GameFiles: gameFiles, KeyRenames: renames, Articles: articles,
	}

	return patch, errors.Join(err, notesErr)
}

// readGameFiles reads the game files a version's changes_files.md lists. A file
// outside the game's own folder, as caligula_rev.txt is, is left out. It
// returns none for a version that has no such list.
func readGameFiles(source archive, folder string) ([]workspace.GameFile, error) {
	file := folder + "/changes_files.md"
	if !source.holds(file) {
		return nil, nil
	}

	text, err := source.read(file)
	if err != nil {
		return nil, err
	}

	var changed []workspace.GameFile
	for line := range strings.Lines(text) {
		match := changedLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if match == nil {
			continue
		}

		rel, inGame := strings.CutPrefix(match[2], "game/")
		if status, known := gitStatuses[match[1]]; inGame && known {
			changed = append(changed, workspace.GameFile{Rel: rel, Status: status})
		}
	}

	return changed, nil
}
