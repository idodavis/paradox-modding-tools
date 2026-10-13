// Command gen writes the shipped backup schema of one game version: what the install's dump files
// declare, saved beside schema/shipped.go. Run it from the repo root:
//
//	go run ./services/internal/schema/gen -game ck3 [-dir <install root>]
package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"reflect"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
)

// main writes the shipped backup schema of the game the flags name.
func main() {
	gameID := flag.String("game", "", "game id: ck3, vic3 or eu5")
	installDir := flag.String("dir", "", "install root; the first Steam install found when empty")
	flag.Parse()
	log.SetFlags(0)

	// 1. the folder the file lands in, found from the repo root
	shippedDir := filepath.Join("services", "internal", "schema", "shipped")
	if _, err := os.Stat(filepath.Join(shippedDir, "..", "shipped.go")); err != nil {
		log.Fatalf("%s not found: run from the repo root (go run ./services/internal/schema/gen)", shippedDir)
	}

	// 2. the install and its version
	root, version := installAndVersion(*gameID, *installDir)

	// 3. the dump part alone, refused on a partial dump set; the databases the build starts with are
	// not shipped
	s, err := index.BuildSchema(context.Background(), index.Request{Game: *gameID, Dir: root, Version: version})
	if err != nil {
		log.Fatal(err)
	}
	if s.Sources[schema.SourceDumps].Origin == "none" {
		log.Fatalf("no dump files found for %s: run the game's script_docs and data type dumps first", *gameID)
	}
	s.Databases = nil

	// 4. the stored form, as schema.LoadShipped reads it
	var encoded bytes.Buffer
	if err := appdata.Encode(&encoded, s); err != nil {
		log.Fatal(err)
	}

	// 5. a file of this version stays when it decodes to the same schema (gob map order changes the
	// bytes on every run); both sides are decoded so empty slices and maps compare alike
	writeShipped(filepath.Join(shippedDir, schema.ShippedName(*gameID, version)), encoded.Bytes())
}

// installAndVersion returns the install root of a game, the first Steam install found when dir is empty, and the
// version read there; it exits when either is missing.
func installAndVersion(gameID, dir string) (string, string) {
	info := game.Get(gameID)
	if info == nil {
		log.Fatalf("unknown game %q: pass -game ck3, vic3 or eu5", gameID)
	}
	if dir == "" {
		installs := game.FindInstalls(gameID)
		if len(installs) == 0 {
			log.Fatalf("no Steam install of %s found: pass -dir", gameID)
		}
		dir = installs[0].Path
	}
	version := game.LoadGameVersion(info.ID, dir)
	if version == "" {
		log.Fatalf("no game version found for %s", dir)
	}
	return dir, version
}

// writeShipped saves the stored form at path, unless the file there already decodes to the same schema.
func writeShipped(path string, encoded []byte) {
	decode := func(data []byte) (decoded *schema.Schema, err error) {
		err = appdata.Decode(bytes.NewReader(data), &decoded)
		return decoded, err
	}
	done := "wrote"
	if current, err := os.ReadFile(path); err == nil {
		done = "replaced"
		old, oldErr := decode(current)
		fresh, freshErr := decode(encoded)
		if freshErr != nil {
			log.Fatal(freshErr)
		}
		if oldErr == nil && reflect.DeepEqual(old, fresh) {
			log.Printf("unchanged %s (%d bytes)", path, len(current))
			return
		}
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s %s (%d bytes)", done, path, len(encoded))
}
