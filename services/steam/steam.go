package steam

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The events the Workshop service sends.
const (
	EventProgress       = "steam:progress"  // an upload's progress; the data is a Progress
	EventListingChanged = "release:changed" // a mod's listing files were written; the data is the mod's id
)

func init() {
	application.RegisterEvent[Progress](EventProgress)
	application.RegisterEvent[string](EventListingChanged)
}

// Service is the Workshop service behind the Publish page: a mod's listing, the files its next upload sends, the
// compare with Steam, and publishing itself.
type Service struct {
	Config    *app.ConfigFile // the cache folder and the mods' records
	Workspace *workspace.Service
}

// ServePublish runs the publishing process if the app was started as one, with the --steam-publish flag. It
// reports whether it did and the exit code to exit with.
func ServePublish(args []string) (exitCode int, served bool) { panic("stub") }

// CreateDescriptionFile creates a mod's description file in the given source format, markdown or bbcode, if the
// mod has none yet.
func (stm *Service) CreateDescriptionFile(modID, source string) error { panic("stub") }

// AddPreview adds a preview to a mod: a picture is copied into the mod's previews folder, a video is recorded by
// its URL.
func (stm *Service) AddPreview(modID string, preview Preview) error { panic("stub") }

// RemovePreview deletes a preview from the mod's previews folder and its listing.
func (stm *Service) RemovePreview(modID string, preview Preview) error { panic("stub") }

// CompareUpload compares what a mod's next upload would send with the last upload and with what Steam has.
func (stm *Service) CompareUpload(modID string) (Comparison, error) { panic("stub") }

// Publish uploads a mod to the Workshop in a second copy of the app. The steam:progress event reports until it
// ends. For a mod with no item yet, the new item's id is written to the mod's record.
func (stm *Service) Publish(modID string) error { panic("stub") }

// ConvertToBBCode converts Markdown to BBCode, for the Paradox Mods copy.
func (stm *Service) ConvertToBBCode(markdown string) string { panic("stub") }

// ListRules returns the rules that say what an upload leaves out.
func (stm *Service) ListRules() []Rule { panic("stub") }
