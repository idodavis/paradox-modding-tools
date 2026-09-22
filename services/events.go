// events.go is the language engine's events to the frontend, named once here: the session is live, a scan moved, a cache changed, a mod's calendar changed.
package services

import "github.com/wailsapp/wails/v3/pkg/application"

const (
	EventLangReady           = "lang:ready"            // a workspace's session is built: data is its id
	EventLangScanProgress    = "lang:scan-progress"    // an install scan moved: data is ScanProgress
	EventLangCacheUpdated    = "lang:cache-updated"    // an install's cache was written: data is the install id
	EventLangCalendarChanged = "lang:calendar-changed" // a mod's calendar changed: data is the workspace id
)

// ScanProgress is one step of an install scan.
type ScanProgress struct {
	InstallID string `json:"installId"`
	Percent   int    `json:"percent"`
	Stage     string `json:"stage"`
}

// emitLang sends an event to the frontend when the app is running; the tests run with no app.
func emitLang(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}
