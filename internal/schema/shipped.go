// shipped.go loads the backup of each game's dumps that is embedded in the app.

package schema

import (
	"bytes"
	"embed"
	"encoding/gob"
)

//go:generate go run ./gen ck3 vic3 eu5

// shipped holds one gob backup of each game's dumps, for an install that lacks a dump file. It holds no game text and
// no built schema.
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
