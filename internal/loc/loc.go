// Package loc reads and writes localization files. It parses a file into its messages, decides the file's
// language, splits a message's text into its markup, and gives the plain text a player sees.
package loc

import "paradox-modding-tools/internal/jomini"

// A Message is one localized message: its key, the text between the quotes, its version number and where it is
// written.
type Message struct {
	Key      string      `json:"key"`
	Text     string      `json:"text"`
	Version  int         `json:"version"`
	Span     jomini.Span `json:"span"`     // the whole line
	KeySpan  jomini.Span `json:"keySpan"`  // the key
	TextSpan jomini.Span `json:"textSpan"` // the text, quotes excluded
}

// A File is one parsed localization file: its language, its messages in order, its parse errors and its line
// index.
type File struct {
	Language string         `json:"language"`
	Messages []Message      `json:"messages"`
	Errors   []jomini.Error `json:"errors"`
	Lines    jomini.Lines   `json:"-"`
}

// Parse reads the text of a localization file. The second argument is the file's path, which decides the language
// when the text does not: the `l_<language>:` header is checked first, then the `_l_<language>` suffix of the file
// name, then the folder's name, all without case. Language is "" if none of them says.
func Parse(text, path string) *File { panic("stub") }

// Encode returns the text of a localization file for one language: the header line, then each message as its
// key, its version and its quoted text, in the form the game reads.
func Encode(language string, messages []Message) string { panic("stub") }
