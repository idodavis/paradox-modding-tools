// Command gen keeps the shipped backup of each game's dumps up to date. For each game id on the command line it reads
// the dumps of the install that PMT_TEST_INSTALL_<GAME> names and, where they are newer than the backup, writes
// shipped/<game>.gob in the current folder. It says for each game what it did.
package main

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// dateLayout is how a line names a day.
const dateLayout = "2006-01-02"

// makeDumps says how to make a game write its dumps.
const makeDumps = "start the game with its debug flag (PMT's Launch does) and run script_docs and dump_data_types " +
	"in its console, then run this again"

// main refreshes the backup of every game named on the command line and prints one line for each. It stops with a
// message only if a backup cannot be written.
func main() {
	for _, id := range os.Args[1:] {
		line, err := refresh(id)
		if err != nil {
			log.Fatalf("%s: %v", id, err)
		}
		fmt.Printf("%s: %s\n", id, line)
	}
}

// refresh writes a game's backup from its install's dumps if they are newer than the shipped one, and returns what it
// did. A game whose install variable is not set, whose install has no dumps, or whose dumps are older than its last
// update is left as it is, and the line says what to do. It returns an error if the backup cannot be encoded or
// written.
func refresh(id string) (string, error) {
	variable := "PMT_TEST_INSTALL_" + strings.ToUpper(id)
	install, row := os.Getenv(variable), game.LookupGame(id)
	switch {
	case row == nil:
		return "", fmt.Errorf("no such game")
	case install == "":
		return "skipped: " + variable + " is not set", nil
	}
	dumps, err := schema.ReadDumps(row.FindDumpDirs(install))
	if err != nil || len(dumps.Commands) == 0 || len(dumps.DataTypes) == 0 {
		return "no dumps: " + makeDumps, nil
	}
	if updated := game.ReadInstallUpdatedAt(install); updated.After(dumps.WrittenAt) {
		return fmt.Sprintf("the game was updated on %s, after its dumps of %s: %s", updated.Format(dateLayout),
			dumps.WrittenAt.Format(dateLayout), makeDumps), nil
	}
	shipped, ok := schema.LoadShipped(id)
	if ok && !dumps.WrittenAt.After(shipped.WrittenAt) {
		return "up to date, dumps of " + shipped.WrittenAt.Format(dateLayout), nil
	}
	// the folders are this machine's, which the backup does not carry
	dumps.Folders = nil
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(dumps); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join("shipped", id+".gob"), encoded.Bytes(), 0o644); err != nil {
		return "", err
	}
	was := "none"
	if ok {
		was = shipped.WrittenAt.Format(dateLayout)
	}
	return fmt.Sprintf("rebuilt from dumps of %s (was %s): %d commands, %d data types", dumps.WrittenAt.Format(dateLayout),
		was, len(dumps.Commands), len(dumps.DataTypes)), nil
}
