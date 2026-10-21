package wiki

import "golang.org/x/net/html"

// The sanitizer is built on x/net/html.
var _ = html.Parse

// Sanitize strips a wiki page's HTML down to what the article pane shows: headings, paragraphs, lists, tables and
// code, with links and images made absolute. Scripts, styles, navigation and edit links are dropped.
func Sanitize(html string) string { panic("stub") }

// ToText flattens sanitized HTML to plain text, for search and for the script examples a page holds.
func ToText(html string) string { panic("stub") }
