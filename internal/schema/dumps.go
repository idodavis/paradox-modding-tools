package schema

import "time"

// A DumpCommand is one effect, trigger or link as its dump lists it. The usage lines in its description become
// the command's arguments.
type DumpCommand struct {
	Name        string
	Declared    string // game.DeclaredEffect, DeclaredTrigger, DeclaredLink
	Scopes      []string
	Target      string
	Description string
}

// A DumpScope is one scope type as the dumps list it.
type DumpScope struct {
	Name      string
	Variables bool
	Triggers  bool
	Effects   bool
}

// Dumps is everything a game's script_docs and data types dumps declare.
type Dumps struct {
	Commands  []DumpCommand
	Scopes    []DumpScope
	Saved     []string
	Modifiers []Modifier
	OnActions []OnAction
	CustomLoc []CustomLoc
	DataTypes map[string]DataType
	WrittenAt time.Time // the oldest dump file's time
}

// ReadDumps reads every dump file under the given folders. A missing file is simply left out, so the caller can
// fill it from the shipped backup. It returns an error only for a file that exists and cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) { panic("stub") }

// cutUsage splits a dump description into its prose and its usage, the lines written as `<scope> = { … }`. The
// usage becomes a command's arguments and never appears in a blurb.
func cutUsage(description string) (blurb string, usage string) { panic("stub") }
