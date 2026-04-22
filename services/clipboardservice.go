package services

import "github.com/wailsapp/wails/v3/pkg/application"

type ClipboardService struct{}

func (c *ClipboardService) CopyToClipboard(text string) bool {
	return application.Get().Clipboard.SetText(text)
}
