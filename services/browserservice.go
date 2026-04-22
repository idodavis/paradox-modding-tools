package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ############
// BrowserService
// ############

// BrowserService opens URLs in the system default browser (Windows, macOS, Linux).
type BrowserService struct{}

// OpenURL opens the given URL in the user's default browser.
// Works on Windows (cmd /c start), macOS (open), and Linux (xdg-open).
func (s *BrowserService) OpenURL(url string) error {
	app := application.Get()
	return app.Browser.OpenURL(url)
}
