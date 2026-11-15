// Command gen keeps what the app ships of each game's past up to date. For
// each game id on the command line it reads the dumps of the install that
// PMT_TEST_INSTALL_<GAME> names and, where they are newer than the backup or
// of another version, writes the backup again with the install's version and
// adds what that version changed to the game's history of patches. It says for
// each game what it did.
//
// It writes from the current folder, which is internal/schema:
//   - shipped/<game>.gob, the backup of the game's dumps;
//   - ../../services/workspace/shipped/<game>.gob, the history of patches;
//   - gen/state/<game>.gob, the docs keys, the defined names and the file
//     hashes of the install the backup was made from. The app never reads it:
//     the next run compares the new install with it. It stays on the machine
//     that runs gen, out of git, so a run on another machine finds none and
//     adds no patch.
//
// "gen past <game> <folder>" adds the patches of versions older than any the
// history holds. The folder holds one dump file per line of a version, named
// <version>_<dump file>, as OldEnt's repository of Crusader Kings III dumps
// has them: 1.10.2_effects.log. It compares each version's dumps with the one
// before and adds the changes of each as a patch.
//
// # Robustness
//
// gen refuses a game whose install is not named, whose dumps are older than
// the install's last update, or whose history or state it cannot read, since
// writing over them would lose the past. A name in a folder of past dumps that
// is not a version number, as 1.6.1~2, is left out and said. Of two past
// versions it compares the data types and the scope types only where both
// versions list some, since the reader takes none from the oldest dumps, and
// the modifiers only where both write them in one form, plain names or
// templates.
package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// makeDumps says how to make a game write its dumps.
const makeDumps = "start the game with its debug flag (PMT's Launch does) and run script_docs and dump_data_types " +
	"in its console, then run this again"

// The folders gen writes, from the folder it runs in.
const (
	dumpsDir   = "shipped"
	historyDir = "../../services/workspace/shipped"
	stateDir   = "gen/state"
)

// snapshot is what gen keeps of the install a backup was made from: its docs
// keys and defined names, and the hash of each of its files.
type snapshot struct {
	workspace.VersionKeys

	Hashes map[string]string
}

// pastVersion is a name that is a version number, as 1.9.0.4.
var pastVersion = regexp.MustCompile(`^\d+(\.\d+)*$`)

func main() {
	args := os.Args[1:]
	if len(args) == 3 && args[0] == "past" {
		line, err := addPast(args[1], args[2])
		finish(args[1], line, err)

		return
	}

	for _, id := range args {
		line, err := refresh(id)
		finish(id, line, err)
	}
}

// finish prints what gen did for a game, or stops with the error.
func finish(id, line string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", id, err)
	}

	fmt.Printf("%s: %s\n", id, line)
}

// installVariable returns the name of the variable that holds a game's install.
func installVariable(id string) string {
	return "PMT_TEST_INSTALL_" + strings.ToUpper(id)
}

// lookupInstall returns a game's row and the install its variable names, ""
// for none. It returns an error for a game that is not one of PMT's.
func lookupInstall(id string) (*game.Game, string, error) {
	row := game.LookupGame(id)
	if row == nil {
		return nil, "", errors.New("no such game")
	}

	return row, os.Getenv(installVariable(id)), nil
}

// refresh writes a game's backup from its install's dumps where they are
// newer than the shipped one or of another version, and returns a line saying
// what it did. Dumps older than the install's last update are refused, since
// they describe the version before.
func refresh(id string) (string, error) {
	row, install, err := lookupInstall(id)
	switch {
	case err != nil:
		return "", err
	case install == "":
		return "skipped: " + installVariable(id) + " is not set", nil
	}

	dumps, err := schema.ReadDumps(row.FindDumpDirs(install))
	if err != nil || len(dumps.Commands) == 0 || len(dumps.DataTypes) == 0 {
		return "no dumps: " + makeDumps, nil
	}

	if updated := game.ReadInstallUpdatedAt(install); updated.After(dumps.WrittenAt) {
		return fmt.Sprintf("the game was updated on %s, after its dumps of %s: %s", updated.Format(time.DateOnly),
			dumps.WrittenAt.Format(time.DateOnly), makeDumps), nil
	}

	dumps.Version = row.LoadVersion(install)
	if dumps.Version == "" {
		return "skipped: the install's version is not known", nil
	}

	shipped, _ := schema.LoadShipped(id)
	if shipped != nil && shipped.Version == dumps.Version && !dumps.WrittenAt.After(shipped.WrittenAt) {
		return "up to date, dumps of " + shipped.WrittenAt.Format(time.DateOnly), nil
	}

	return rebuild(row, install, dumps, shipped)
}

// rebuild writes a game's backup from its dumps, its history with the patch of
// the dumps' version, and its state, and returns a line saying what it did.
// The backup it replaces, nil for none, is what the patch is compared against.
func rebuild(row *game.Game, install string, dumps, shipped *schema.Dumps) (string, error) {
	language, mounts, err := buildSchema(row, install, dumps.Version)
	if err != nil {
		return "", err
	}

	scanned := index.Scan(language, mounts, func(string, int) {})
	current := &snapshot{
		VersionKeys: workspace.VersionKeys{Docs: language.ListDocKeys(), Defined: scanned.ListDefinedKeys()},
		Hashes:      workspace.ListHashes(scanned),
	}

	history, err := workspace.LoadHistory(row.ID)
	if err != nil {
		return "", err
	}

	before, err := loadSnapshot(row.ID)
	if err != nil {
		return "", err
	}

	line := fmt.Sprintf("rebuilt from dumps of %s, version %s (was %s): %d commands, %d data types",
		dumps.WrittenAt.Format(time.DateOnly), dumps.Version, describeShipped(shipped), len(dumps.Commands),
		len(dumps.DataTypes))
	if shipped != nil && shipped.Version != "" && shipped.Version != dumps.Version {
		patch := makePatch(shipped, dumps, language, scanned, before, current)
		history = putPatch(history, patch)
		line += fmt.Sprintf("; patch of %s: %d commands, %d docs keys, %d defined names, %d files", patch.Version,
			len(patch.DumpChanges), len(patch.DocChanges), len(patch.DefinitionChanges), len(patch.GameFiles))
	}

	// the folders are this machine's, which the backup does not carry
	dumps.Folders = nil

	return line, errors.Join(
		writeGob(filepath.Join(dumpsDir, row.ID+".gob"), dumps),
		writeGob(filepath.Join(historyDir, row.ID+".gob"), history),
		files.SaveGob(filepath.Join(stateDir, row.ID+".gob"), current),
	)
}

// describeShipped returns what a backup was, for the line gen says.
func describeShipped(shipped *schema.Dumps) string {
	switch {
	case shipped == nil:
		return "none"
	case shipped.Version == "":
		return "version not known, dumps of " + shipped.WrittenAt.Format(time.DateOnly)
	}

	return "version " + shipped.Version
}

// buildSchema returns the schema of a game's install, and the mounts of its
// files that the index scan reads.
func buildSchema(row *game.Game, install, version string) (*lang.Schema, *files.Mounts, error) {
	roots, err := row.ListInstallRoots(install)
	if err != nil {
		return nil, nil, err
	}

	mounts, err := files.Mount(roots)
	if err != nil {
		return nil, nil, err
	}

	dumpDirs := row.FindDumpDirs(install)
	language, _, err := schema.Build(lang.LookupDialect(row.ID), dumpDirs, nil, install, version, mounts.Files, index.ParseFile)

	return language, mounts, err
}

// makePatch returns what a new version changed against the backup of the
// version before: its commands from the two dumps, and, where the state of the
// version before is kept, its docs keys, defined names and files.
func makePatch(
	shipped, dumps *schema.Dumps, language *lang.Schema, scanned *index.Index, before, current *snapshot,
) workspace.Patch {
	patch := workspace.Patch{
		Version: dumps.Version, Base: shipped.Version, Local: true,
		DumpChanges: schema.CompareDumps(shipped, dumps, language),
	}

	if before != nil {
		patch.DocChanges = schema.CompareKeys(before.Docs, current.Docs)
		patch.DefinitionChanges = schema.CompareKeys(before.Defined, current.Defined)
		patch.GameFiles = workspace.CompareHashes(scanned, before.Hashes, current.Hashes)
	}

	return patch
}

// putPatch returns the history with a patch added, in place of the patch of
// the same version where it has one, oldest version first.
func putPatch(history []workspace.Patch, patch workspace.Patch) []workspace.Patch {
	history = slices.DeleteFunc(slices.Clone(history), func(other workspace.Patch) bool {
		return other.Version == patch.Version
	})

	history = append(history, patch)
	slices.SortFunc(history, func(first, second workspace.Patch) int {
		return game.CompareVersions(first.Version, second.Version)
	})

	return history
}

// loadSnapshot returns the state gen kept of a game, nil for none. It returns
// an error for a file that is there and cannot be read.
func loadSnapshot(id string) (*snapshot, error) {
	file := filepath.Join(stateDir, id+".gob")
	state, ok := files.LoadGob[*snapshot](file)
	if !ok && files.IsFile(file) {
		return nil, fmt.Errorf("cannot read %s; delete it to start the state again", file)
	}

	return state, nil
}

// writeGob writes a value to a file as gob, which the app decodes as it is.
func writeGob(file string, value any) error {
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(value); err != nil {
		return err
	}

	return files.WriteAtomic(file, encoded.Bytes())
}

// addPast adds the patches of the versions a folder of dumps holds to a game's
// history, and returns a line saying what it did. The first version has no
// patch, since nothing is before it to compare with. A version the history has
// already is replaced.
func addPast(id, folder string) (string, error) {
	row, install, err := lookupInstall(id)
	switch {
	case err != nil:
		return "", err
	case install == "":
		return "", errors.New(installVariable(id) + " is not set")
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", err
	}

	versions, skipped := listPastVersions(entries)
	if len(versions) < 2 {
		return "", fmt.Errorf("%s holds the dumps of %d versions, and a patch needs two", folder, len(versions))
	}

	language, _, err := buildSchema(row, install, row.LoadVersion(install))
	if err != nil {
		return "", err
	}

	history, err := workspace.LoadHistory(id)
	if err != nil {
		return "", err
	}

	var before *schema.Dumps
	for position, version := range versions {
		texts, err := readPastTexts(folder, entries, version)
		if err != nil {
			return "", err
		}

		dumps := schema.ParseDumps(texts)
		if before != nil {
			changes := schema.CompareDumps(before, dumps, language)
			history = putPatch(history, workspace.Patch{
				Version: version, Base: versions[position-1], Local: true,
				DumpChanges: dropIncomparable(changes, before, dumps),
			})
		}

		before = dumps
	}

	if err = writeGob(filepath.Join(historyDir, id+".gob"), history); err != nil {
		return "", err
	}

	return fmt.Sprintf("added %d patches, %s to %s, to a history of %d; left out, not version numbers: %s",
		len(versions)-1, versions[1], versions[len(versions)-1], len(history), strings.Join(skipped, ", ")), nil
}

// dropIncomparable returns the changes of two dumps without those of a kind of
// entry that the two versions cannot be compared in. A kind is left out in
// two cases. One dump lists none of it: that is a file the reader does not
// read, not a version without any. The first dumps have no data types, and
// the dumps before 1.9 write their scope types as a plain list the reader does
// not take. Or the versions write it in two forms: modifiers.log lists plain
// names such as beja_opinion until 1.8, and templates such as
// $CULTURE$_opinion from 1.9, so one version's keys include a template and the
// other's none. Either way the changes would be false additions and removals.
func dropIncomparable(changes []schema.DumpChange, old, current *schema.Dumps) []schema.DumpChange {
	return slices.DeleteFunc(changes, func(change schema.DumpChange) bool {
		switch change.Database {
		case "":
			return len(old.DataTypes) == 0 || len(current.DataTypes) == 0
		case lang.DumpTypes:
			return len(old.Scopes) == 0 || len(current.Scopes) == 0
		case lang.DumpModifiers:
			return hasTemplate(old.Modifiers) != hasTemplate(current.Modifiers)
		}

		return false
	})
}

// hasTemplate returns whether one of the keys is a template, which has a name
// between dollar signs, as $CULTURE$_opinion.
func hasTemplate(keys map[string]bool) bool {
	for key := range keys {
		if strings.Contains(key, "$") {
			return true
		}
	}

	return false
}

// listPastVersions returns the versions a folder of dumps holds files of,
// oldest first, and the names it left out for not being version numbers.
func listPastVersions(entries []os.DirEntry) (versions, skipped []string) {
	for _, entry := range entries {
		version, file, ok := strings.Cut(entry.Name(), "_")
		if !ok || !schema.IsDumpFile(file) || slices.Contains(versions, version) || slices.Contains(skipped, version) {
			continue
		}

		if pastVersion.MatchString(version) {
			versions = append(versions, version)
		} else {
			skipped = append(skipped, version)
		}
	}

	slices.SortFunc(versions, game.CompareVersions)

	return versions, skipped
}

// readPastTexts returns the text of each dump file a folder holds of one
// version, by the file's name without the version. It returns an error for a
// file that cannot be read, since a version read in part would show the rest
// as removed.
func readPastTexts(folder string, entries []os.DirEntry, version string) (map[string]string, error) {
	texts := map[string]string{}
	for _, entry := range entries {
		file, ok := strings.CutPrefix(entry.Name(), version+"_")
		if !ok || !schema.IsDumpFile(file) {
			continue
		}

		text, err := files.ReadText(filepath.Join(folder, entry.Name()))
		if err != nil {
			return nil, err
		}

		texts[file] = text.Text
	}

	return texts, nil
}
