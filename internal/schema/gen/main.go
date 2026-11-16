// Command gen keeps what the app ships of each game's past up to date. For
// each game id on the command line it reads the dumps of the install that
// PMT_TEST_INSTALL_<GAME> names and, where they are newer than the backup or
// of another version, writes the backup again with the install's version and
// adds what that version changed to the game's history of patches. It says for
// each game what it did.
//
// It writes from the current folder, which is internal/schema:
//   - shipped/<game>.gob, the backup of the game's dumps;
//   - ../patch/shipped/<game>.gob, the history of patches;
//   - gen/state/<game>.gob, the snapshot of the install the backup was made
//     from: its defined names and file hashes. The app never reads it: the
//     next run compares the new install with it. It stays on the machine that
//     runs gen, out of git, so a run on another machine finds none and adds a
//     patch of the dumps' changes alone.
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
// is not a version number, as 1.6.1~2, is left out and said.
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
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/internal/schema"
)

// makeDumps says how to make a game write its dumps.
const makeDumps = "start the game with its debug flag (PMT's Launch does) and run script_docs and dump_data_types " +
	"in its console, then run this again"

// The folders gen writes, from the folder it runs in.
const (
	dumpsDir   = "shipped"
	historyDir = "../patch/shipped"
	stateDir   = "gen/state"
)

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

// lookupInstall returns a game's row, the name of the variable that holds its
// install, and the install, "" where the variable is not set. It returns an
// error for a game that is not one of PMT's.
func lookupInstall(id string) (row *game.Game, variable, install string, err error) {
	if row = game.LookupGame(id); row == nil {
		return nil, "", "", errors.New("no such game")
	}

	variable = "PMT_TEST_INSTALL_" + strings.ToUpper(id)

	return row, variable, os.Getenv(variable), nil
}

// refresh writes a game's backup from its install's dumps where they are
// newer than the shipped one or of another version, and returns a line saying
// what it did. Dumps older than the install's last update are refused, since
// they describe the version before.
func refresh(id string) (string, error) {
	row, variable, install, err := lookupInstall(id)
	switch {
	case err != nil:
		return "", err
	case install == "":
		return "skipped: " + variable + " is not set", nil
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
// The backup it replaces, nil for none, is what the patch is compared against,
// with the state of the run that wrote it.
func rebuild(row *game.Game, install string, dumps, shipped *schema.Dumps) (string, error) {
	language, mounts, err := buildSchema(row, install, dumps.Version)
	if err != nil {
		return "", err
	}

	history, err := patch.LoadHistory(row.ID)
	if err != nil {
		return "", err
	}

	stateFile := filepath.Join(stateDir, row.ID+".gob")
	old, ok := files.LoadGob[patch.Snapshot](stateFile)
	if !ok && files.IsFile(stateFile) {
		return "", fmt.Errorf("cannot read %s; delete it to start the state again", stateFile)
	}

	current := patch.TakeSnapshot(dumps, index.Scan(language, mounts, func(string, int) {}))
	was := "none"
	if shipped != nil {
		was = fmt.Sprintf("version %q, dumps of %s", shipped.Version, shipped.WrittenAt.Format(time.DateOnly))
	}

	line := fmt.Sprintf("rebuilt from dumps of %s, version %s (was %s): %d commands, %d data types",
		dumps.WrittenAt.Format(time.DateOnly), dumps.Version, was, len(dumps.Commands), len(dumps.DataTypes))
	if shipped != nil && shipped.Version != "" && shipped.Version != dumps.Version {
		old.Dumps = shipped
		gamePatch := patch.CompareSnapshots(shipped.Version, dumps.Version, old, current, language)
		history = patch.Put(history, gamePatch)
		line += fmt.Sprintf("; patch of %s: %d commands, %d defined names, %d files", gamePatch.Version,
			len(gamePatch.DumpChanges), len(gamePatch.DefinitionChanges), len(gamePatch.GameFiles))
	}

	// The folders are this machine's, which the backup does not carry, and the
	// state needs no second copy of the dumps.
	dumps.Folders, current.Dumps = nil, nil

	return line, errors.Join(
		writeGob(filepath.Join(dumpsDir, row.ID+".gob"), dumps),
		writeGob(filepath.Join(historyDir, row.ID+".gob"), history),
		files.SaveGob(stateFile, current),
	)
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
	row, variable, install, err := lookupInstall(id)
	switch {
	case err != nil:
		return "", err
	case install == "":
		return "", errors.New(variable + " is not set")
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

	history, err := patch.LoadHistory(id)
	if err != nil {
		return "", err
	}

	var before patch.Snapshot
	for position, version := range versions {
		contents, err := readPastFiles(folder, entries, version)
		if err != nil {
			return "", err
		}

		current := patch.Snapshot{Dumps: schema.ParseDumps(contents)}
		if position > 0 {
			gamePatch := patch.CompareSnapshots(versions[position-1], version, before, current, language)
			history = patch.Put(history, gamePatch)
		}

		before = current
	}

	if err = writeGob(filepath.Join(historyDir, id+".gob"), history); err != nil {
		return "", err
	}

	return fmt.Sprintf("added %d patches, %s to %s, to a history of %d; left out, not version numbers: %s",
		len(versions)-1, versions[1], versions[len(versions)-1], len(history), strings.Join(skipped, ", ")), nil
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

// readPastFiles returns the bytes of each dump file a folder holds of one
// version, by the file's name without the version. It returns an error for a
// file that cannot be read, since a version read in part would show the rest
// as removed.
func readPastFiles(folder string, entries []os.DirEntry, version string) (map[string][]byte, error) {
	contents := map[string][]byte{}
	for _, entry := range entries {
		file, ok := strings.CutPrefix(entry.Name(), version+"_")
		if !ok || !schema.IsDumpFile(file) {
			continue
		}

		content, err := files.ReadContent(filepath.Join(folder, entry.Name()))
		if err != nil {
			return nil, err
		}

		contents[file] = content
	}

	return contents, nil
}
