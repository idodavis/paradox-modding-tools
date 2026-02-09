package services

import "github.com/wailsapp/wails/v3/pkg/application"

type ClipboardService struct {
	app *application.App
}

func (c *ClipboardService) CopyToClipboard(text string) bool {
	return c.app.Clipboard.SetText(text)
}
