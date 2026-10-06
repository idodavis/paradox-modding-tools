// Command gen writes the shipped backup schema of one game version: what the install's dump files
// declare, saved beside shipped.go. Run it from the repo root:
//
//	go run ./services/internal/lang/shipped/gen -game ck3 [-dir <install root>]
package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"reflect"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/shipped"
	"paradox-modding-tools/services/internal/local"
)

func main() {
	gameID := flag.String("game", "", "game id: ck3, vic3 or eu5")
	installDir := flag.String("dir", "", "install root; the first Steam install found when empty")
	flag.Parse()
	log.SetFlags(0)

	// 1. the folder the file lands in, found from the repo root
	shippedDir := filepath.Join("services", "internal", "lang", "shipped")
	if _, err := os.Stat(filepath.Join(shippedDir, "shipped.go")); err != nil {
		log.Fatalf("%s not found: run from the repo root (go run ./services/internal/lang/shipped/gen)", shippedDir)
	}

	// 2. the install and its version
	info := game.Get(*gameID)
	if info == nil {
		log.Fatalf("unknown game %q: pass -game ck3, vic3 or eu5", *gameID)
	}
	if *installDir == "" {
		installs := game.FindInstalls(*gameID)
		if len(installs) == 0 {
			log.Fatalf("no Steam install of %s found: pass -dir", *gameID)
		}
		*installDir = installs[0].Path
	}
	version := game.ReadGameVersion(info.ID, *installDir)
	if version == "" {
		log.Fatalf("no game version found for %s", *installDir)
	}

	// 3. the dump part alone, refused on a partial dump set; the databases the build starts with are
	// not shipped
	sch, err := index.BuildSchema(context.Background(), index.Request{Game: *gameID, Dir: *installDir, Version: version})
	if err != nil {
		log.Fatal(err)
	}
	if sch.Sources[schema.SourceDumps].From == "none" {
		log.Fatalf("no dump files found for %s: run the game's script_docs and data type dumps first", *gameID)
	}
	sch.Databases = nil

	// 4. the stored form, as shipped.Load reads it
	var encoded bytes.Buffer
	if err := local.Encode(&encoded, sch); err != nil {
		log.Fatal(err)
	}

	// 5. a file of this version stays when it decodes to the same schema (gob map order changes the
	// bytes on every run); both sides are decoded so empty slices and maps compare alike
	decode := func(data []byte) (decoded *schema.Schema, err error) {
		err = local.Decode(bytes.NewReader(data), &decoded)
		return decoded, err
	}
	path := filepath.Join(shippedDir, shipped.Name(*gameID, version))
	done := "wrote"
	if current, err := os.ReadFile(path); err == nil {
		done = "replaced"
		old, oldErr := decode(current)
		fresh, freshErr := decode(encoded.Bytes())
		if freshErr != nil {
			log.Fatal(freshErr)
		}
		if oldErr == nil && reflect.DeepEqual(old, fresh) {
			log.Printf("unchanged %s (%d bytes)", path, len(current))
			return
		}
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%s %s (%d bytes)", done, path, encoded.Len())
}
