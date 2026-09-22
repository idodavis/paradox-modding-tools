// eventdetail.go is one definition opened: where it is, what its block holds at the top, its options, and the fires in and out of it. The block is read from the file when the detail is built and kept nowhere.
package views

import (
	"path/filepath"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

// Detail is one definition, opened.
type Detail struct {
	index.Found
	Namespace string         `json:"namespace,omitempty"`
	Title     string         `json:"title,omitempty"`
	Sections  []Section      `json:"sections"`
	Options   []Option       `json:"options"`
	Fires     []index.Firing `json:"fires"`
	FiredBy   []index.Firing `json:"firedBy"`
	Sites     []index.Found  `json:"sites"` // every definition of the name, in load order
	Reason    string         `json:"reason,omitempty"`
}

// Section is one key at the top of the definition's block, with its value where that is one word.
type Section struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Line  int    `json:"line"`
}

// Option is one option block of an event: its name key and the text that key has.
type Option struct {
	Name string `json:"name"`
	Text string `json:"text,omitempty"`
	Line int    `json:"line"`
}

// EventDetail opens a definition by name: the one the game keeps.
func EventDetail(s *session.Session, id string) *Detail {
	s.RLock()
	defer s.RUnlock()
	found := s.Defs(id)
	if len(found) == 0 {
		return &Detail{Reason: "Nothing defines " + id + "."}
	}
	kept, _ := s.Winner(found)
	d := &Detail{Found: kept, Namespace: namespace(id), Title: title(s, kept), Sites: found}
	for _, x := range s.All() {
		d.Fires, d.FiredBy = append(d.Fires, x.Fires(id)...), append(d.FiredBy, x.FiredBy(id)...)
	}

	// the block, read from the file: the statement at the definition's line
	parsed, _, err := s.Parsed(filepath.Clean(kept.At.Path))
	if err != nil {
		return d
	}
	lines := parsed.Lines()
	for _, st := range parsed.Root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || lines.PositionAt(a.Key.Range.Start).Line != int(kept.Line) {
			continue
		}
		block := jomini.BlockOf(a.Value)
		if block == nil {
			break
		}
		for _, inner := range block.Statements {
			m, ok := inner.(*jomini.Assignment)
			if !ok {
				continue
			}
			section := Section{Key: m.Key.Text, Line: lines.PositionAt(m.Key.Range.Start).Line}
			if scalar, ok := m.Value.(*jomini.Scalar); ok {
				section.Value = scalar.Text
			}
			d.Sections = append(d.Sections, section)
			if m.Key.Text == "option" {
				if b := jomini.BlockOf(m.Value); b != nil {
					d.Options = append(d.Options, option(s, b, section.Line))
				}
			}
		}
		break
	}
	return d
}

// option reads an option block's name and the text it has.
func option(s *session.Session, block *jomini.Block, line int) Option {
	o := Option{Line: line}
	for _, st := range block.Statements {
		if a, ok := st.(*jomini.Assignment); ok && a.Key.Text == "name" {
			if scalar, ok := a.Value.(*jomini.Scalar); ok {
				o.Name = scalar.Text
				if said, ok := s.Loc(scalar.Text, ""); ok {
					o.Text = said.Value
				}
			}
		}
	}
	return o
}
