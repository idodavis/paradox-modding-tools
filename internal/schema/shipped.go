package schema

import (
	"bytes"
	"embed"
	"encoding/gob"
)

//go:generate go run ./gen ck3 vic3 eu5

// shipped holds a backup of what each game's dumps declare, one gob file per game, written by gen/main.go. It
// stands in for a dump file an install lacks. It holds only what the dumps declare, never the game's own words and
// never a built schema.
//
//go:embed shipped/*.gob
var shipped embed.FS

// LoadShipped returns the shipped backup of a game's dumps, or false if none is embedded for that game.
func LoadShipped(gameID string) (*Dumps, bool) {
	data, err := shipped.ReadFile("shipped/" + gameID + ".gob")
	if err != nil {
		return nil, false
	}
	dumps := &Dumps{}
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(dumps); err != nil {
		return nil, false
	}
	return dumps, true
}
