package steam

// Params is everything the publishing process needs: the game, the mod's upload folder, the item and the listing.
type Params struct {
	Game        string  `json:"game"`
	LibraryPath string  `json:"libraryPath"`
	AppID       int     `json:"appId"`
	UploadDir   string  `json:"uploadDir"`
	WorkshopID  string  `json:"workshopId"` // "" creates the item
	Listing     Listing `json:"listing"`
}

// StartPublish starts a second copy of the app as the publishing process, hands it the parameters on standard
// input, and reports its progress lines until it exits. It returns the item's id, which the process creates when
// the parameters name none.
func StartPublish(executable string, params Params, progress func(Progress)) (workshopID string, err error) {
	panic("stub")
}

// RunPublish is the main function of the publishing process. It reads the parameters, opens Steam's library,
// creates the item if none is named, submits the upload, and writes progress as JSON lines. It returns the exit
// code.
func RunPublish() int { panic("stub") }
