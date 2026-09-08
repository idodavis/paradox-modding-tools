// inlay.go renders a mod's own era beside the dates its script is written in.
//
// A reading aid and nothing else: the hint is drawn by the editor, never written
// to the file, and a mod that declares no calendar produces none at all.
package lsp

import (
	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/session"
)

// InlayHint is text the editor draws inline at Range.End without editing the
// buffer. Columns are UTF-8 bytes, like every other range PMT returns.
type InlayHint struct {
	Range   Range  `json:"range"`
	Label   string `json:"label"`
	Tooltip string `json:"tooltip,omitempty"`
}

// InlayHints returns the era form of every date literal in a file.
//
// A total conversion offsets the year so the engine's date arithmetic still
// works -- A Game of Thrones writes 8082.1.1 for what its players call 300 AC --
// and a modder reading that file otherwise subtracts 7,782 in their head on
// every line.
func InlayHints(s *session.Session, path string) []InlayHint {
	// Cheap discriminator first. Almost no mod declares a calendar, and the
	// ones that do are the enormous ones, so asking the workspace before
	// parsing anything keeps this free for everybody else.
	cal := s.CalendarFor(path)
	if !cal.Declared() {
		return nil
	}
	res := s.Parsed(path)
	if res.Root == nil {
		return nil
	}
	li := res.Lines()
	var out []InlayHint
	add := func(sc jomini.Scalar) {
		if sc.Quoted || !jomini.IsDateLiteral(sc.Text) {
			return
		}
		era := catalog.EraDate(sc.Text, cal)
		if era == "" {
			return
		}
		out = append(out, InlayHint{
			Range:   byteRange(li, sc.Range.Start, sc.Range.End),
			Label:   " " + era,
			Tooltip: sc.Text + " in this mod's calendar is " + era + ".",
		})
	}
	jomini.Walk(res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		switch n := st.(type) {
		case *jomini.Assignment:
			// Both sides. History files key their blocks BY a date --
			// `867.1.1 = { … }` -- so hinting only values would leave the one
			// date a reader most needs converted untouched.
			add(n.Key)
			if v, ok := n.Value.(*jomini.Scalar); ok {
				add(*v)
			}
		case *jomini.ValueStmt:
			if v, ok := n.Value.(*jomini.Scalar); ok {
				add(*v)
			}
		}
		return true
	})
	return out
}
