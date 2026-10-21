package lsp

import "paradox-modding-tools/internal/game"

// A Fold is one foldable region: its first and last lines, counted from one.
type Fold struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// A Hint is a text drawn inline beside the code, such as a date read in the mod's calendar.
type Hint struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

// A ColorSite is one color written in a file and the color it makes.
type ColorSite struct {
	Range Range     `json:"range"`
	Color game.RGBA `json:"color"`
}

// A Signature is the parameter list of a scripted definition at a call, with the parameter the position is on.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Active     int      `json:"active"`
}

// ListFolds returns a file's foldable regions: every block that spans more than one line.
func (lsp *Service) ListFolds(path string) ([]Fold, error) { panic("stub") }

// ListHints returns a file's inlay hints: each date in the file read in the mod's calendar.
func (lsp *Service) ListHints(path string) ([]Hint, error) { panic("stub") }

// ListColorSites returns every color written in a file, with the color it makes.
func (lsp *Service) ListColorSites(path string) ([]ColorSite, error) { panic("stub") }

// PresentColor returns the text that writes a color back into a file in the same form as the block at the given
// range, for the color picker.
func (lsp *Service) PresentColor(path string, within Range, color game.RGBA) (string, error) {
	panic("stub")
}

// Signature returns the signature help for a call at a position in a file, or nil if the position is not inside
// a call.
func (lsp *Service) Signature(path string, line, column int) (*Signature, error) { panic("stub") }
