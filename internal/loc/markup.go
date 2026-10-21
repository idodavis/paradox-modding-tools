package loc

import "paradox-modding-tools/internal/jomini"

// Markup says what kind of piece a segment of a localization text is.
type Markup uint8

const (
	MarkupText   Markup = iota // plain text the player reads
	MarkupKey                  // `$key$`: another message's text, or a value the game fills in
	MarkupCall                 // `[call]`: a data call
	MarkupFormat               // `#bold … #!`: a text format and its end
	MarkupIcon                 // `@icon!`: an icon
	MarkupBreak                // `\n`
)

// A Segment is one piece of a localization text, with its span in the file.
type Segment struct {
	Markup Markup      `json:"markup"`
	Text   string      `json:"text"` // the piece without its markers: the key, the call, the format's name, the icon
	Span   jomini.Span `json:"span"`
}

// Segments splits a message's text into its pieces. The second argument is the offset of the text's first byte in
// its file, so the spans come out in the file's offsets.
func Segments(text string, base int) []Segment { panic("stub") }

// ToPlain returns the text a player reads: the plain pieces joined together, the markup dropped, and each break
// written as a newline.
func ToPlain(segments []Segment) string { panic("stub") }
