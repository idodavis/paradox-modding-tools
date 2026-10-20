// signature.go is a command's arguments, shown while its block is typed.

package lsp

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/workspace"
)

// Signature is a command's arguments.
type Signature struct {
	Label      string   `json:"label"`          // the command and its arguments
	Parameters []string `json:"parameters"`     // the keys its block holds
	Docs       string   `json:"docs,omitempty"` // its blurb
}

// SignatureHelp returns the arguments of the command whose block holds the cursor, from a declared source; nil
// elsewhere. Only the block the cursor is directly in counts.
func SignatureHelp(ws *workspace.Session, path string, line, col int) (*Signature, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil || file.place.Format != inventory.Script {
		return nil, err
	}
	cursor := file.walkTo(file.parsed.Lines().OffsetAt(line, col))
	holder := cursor.blockAt()
	if holder < 0 {
		return nil, nil
	}
	at := cursor.chain[holder]
	assign, isAssign := at.statement.(*jomini.Assignment)
	command := at.command(ws.Install.Schema)
	if !isAssign || command == nil || slices.Contains(command.Declared, game.DeclaredLink) {
		return nil, nil
	}
	names, stated := positionKeys(command.Args)
	if names = names[:stated]; len(names) == 0 {
		return nil, nil
	}
	label := assign.Key.Text + " = { " + strings.Join(names, " ") + " }"
	return &Signature{Label: label, Parameters: names, Docs: command.Blurb}, nil
}
