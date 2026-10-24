// Command gen writes the shipped backup of each game's dumps. For each game id on the command line it reads the
// dumps of the install that PMT_TEST_INSTALL_<GAME> names and writes shipped/<game>.gob in the current folder.
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

// main writes the backup of every game named on the command line. It stops with a message if a game is unknown, its
// install variable is not set or its install has no dumps.
func main() {
	for _, id := range os.Args[1:] {
		install := os.Getenv("PMT_TEST_INSTALL_" + strings.ToUpper(id))
		if game.LookupGame(id) == nil || install == "" {
			log.Fatalf("%s: no such game, or PMT_TEST_INSTALL_%s is not set", id, strings.ToUpper(id))
		}
		dumps, err := schema.ReadDumps(game.FindDumpDirs(id, install))
		if err != nil {
			log.Fatal(err)
		}
		if len(dumps.Commands) == 0 || len(dumps.DataTypes) == 0 {
			log.Fatalf("%s: the install's dumps are missing; run script_docs and dump_data_types in the game", id)
		}
		var encoded bytes.Buffer
		if err := gob.NewEncoder(&encoded).Encode(dumps); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("shipped", id+".gob"), encoded.Bytes(), 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d commands, %d data types, %d bytes\n", id, len(dumps.Commands), len(dumps.DataTypes), encoded.Len())
	}
}
