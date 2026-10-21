package wiki

import "paradox-modding-tools/services/workspace"

// A LexiconRow is one effect, trigger, link, scope or data function the game's dumps declare, with the dump's text
// and example uses from the index.
type LexiconRow struct {
	Class    string    `json:"class"` // effect, trigger, link, scope, function
	Name     string    `json:"name"`
	Blurb    string    `json:"blurb"`
	Usage    string    `json:"usage"`
	Scopes   []string  `json:"scopes,omitempty"`
	Target   string    `json:"target,omitempty"`
	Examples []Example `json:"examples"`
}

// An Example is one place where the game's own files use a lexicon row's name.
type Example struct {
	Place workspace.Place `json:"place"`
	Line  int             `json:"line"`
}

// BuildLexicon returns the lexicon rows of one class, for the Wiki pane's Lexicon tab.
func (wik *Service) BuildLexicon(class string) ([]LexiconRow, error) { panic("stub") }
