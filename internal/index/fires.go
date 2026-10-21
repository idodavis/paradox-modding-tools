package index

// A Fire is one firing: one definition fires another, with the delay written on it and the scripted effects the
// firing passes through.
type Fire struct {
	Source Site     `json:"source"`
	Target Site     `json:"target"`
	Days   string   `json:"days,omitempty"` // as written: a number or a range
	Via    []string `json:"via,omitempty"`  // the scripted effects entered between them, in order
}

// ListFires returns what a definition fires: the events, on_actions and decisions it names in a firing position.
// With the flag set, it also follows the scripted effects the definition calls.
func (index *Index) ListFires(site Site, follow bool) []Fire { panic("stub") }

// ListFiredBy returns what fires a definition, following scripted effects when the flag is set.
func (index *Index) ListFiredBy(site Site, follow bool) []Fire { panic("stub") }
