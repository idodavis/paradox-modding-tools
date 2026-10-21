package steam

// A DLC is one DLC of the game that an item may require.
type DLC struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListDLC returns the DLCs the open workspace's install ships, from the descriptors in its dlc folder.
func (stm *Service) ListDLC() ([]DLC, error) { panic("stub") }

// ListTags returns the Workshop tags the game's items may carry.
func (stm *Service) ListTags() []string { panic("stub") }

// A Rule is one pattern of files an upload leaves out, with the reason shown for it.
type Rule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

// Rules lists what an upload leaves out.
var Rules = []Rule{}
