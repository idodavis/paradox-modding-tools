// shipped.go loads the backup of each game's dumps that the app ships. It
// is used where an install's own dump files are missing.

package schema

import (
	"bytes"
	"embed"
	"encoding/gob"
)

//go:generate go run ./gen ck3 vic3 eu5

// shipped holds one backup file for each game: what the game's dumps
// declare, as the gen command read it. It holds none of the game's text and
// no built schema.
//
//go:embed shipped/*.gob
var shipped embed.FS

// LoadShipped returns the backup of a game's dumps. It returns false if the
// app ships none for the game, or if the backup cannot be decoded.
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
