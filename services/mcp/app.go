// app.go keeps what the windows tell the server and hands the main window what
// an agent asks it to show.

package mcp

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// EventShow is sent when an agent asks the main window to show something. It
// carries a Show.
const EventShow = "mcp:show"

func init() { application.RegisterEvent[Show](EventShow) }

// View is the page a window shows and its chip, as the window sent them last.
type View struct {
	Page  string            `json:"page" jsonschema:"the page's id, such as workspace-ide or health"`
	Query map[string]string `json:"query" jsonschema:"the page's route query, which says what it shows"`
	Chip  Chip              `json:"chip"`
}

// Chip is what the page shown offers an agent's prompt.
type Chip struct {
	Label     string `json:"label" jsonschema:"what the chip reads, such as a file's name or an event's id"`
	Path      string `json:"path" jsonschema:"a file's absolute path; empty for a text"`
	StartLine int    `json:"startLine" jsonschema:"the first selected line, counted from one; 0 for the whole file"`
	EndLine   int    `json:"endLine" jsonschema:"the last selected line"`
	Text      string `json:"text" jsonschema:"what a chip that is not a file carries"`
}

// Page is one page of the app as its catalog describes it.
type Page struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Help  []string `json:"help"`
}

// Show is something an agent asks the main window to bring to the front.
type Show struct {
	Page      string            `json:"page,omitempty" jsonschema:"a page's id, shown with the query"`
	Query     map[string]string `json:"query,omitempty" jsonschema:"what the page shows, as read_guide's pages say"`
	Path      string            `json:"path,omitempty" jsonschema:"a file's absolute path, opened in the IDE"`
	StartLine int               `json:"startLine,omitempty" jsonschema:"the first line selected, counted from one"`
	EndLine   int               `json:"endLine,omitempty" jsonschema:"the last line selected"`
	Drawable  string            `json:"drawable,omitempty" jsonschema:"a window of the .gui file, drawn in the Visual view"`
	Picks     map[string]string `json:"picks,omitempty" jsonschema:"the option each state group of the window is drawn at, or its caption in the States panel, such as Yes"`
	LookUp    *Lookup           `json:"lookUp,omitempty" jsonschema:"what the IDE's Wiki pane looks up"`
}

// Lookup is what the IDE's Wiki pane looks up: a wiki page by its title, or
// a Lexicon entry by its class and name.
type Lookup struct {
	Title string `json:"title,omitempty" jsonschema:"a wiki page's title, as search_pages gives it"`
	Class string `json:"class,omitempty" jsonschema:"a Lexicon entry's class: effect, trigger, link, scope or function"`
	Name  string `json:"name,omitempty" jsonschema:"the Lexicon entry's name"`
}

// SetView keeps the page a window shows and its chip.
func (s *Service) SetView(view View) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.view = view
}

// SetPages keeps the app's pages, which read_guide describes.
func (s *Service) SetPages(pages []Page) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pages = pages
}

// describeView returns the view the user looked at last. It returns an error
// if no window has sent one.
func (s *Service) describeView(struct{}) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.view.Page == "" {
		return View{}, errors.New("PMT shows no page yet")
	}

	return s.view, nil
}

// show sends the main window what an agent asks it to show. It returns an
// error, saying what to give instead, if the request names nothing or names
// something the workspace does not have.
func (s *Service) show(request Show) (string, error) {
	namesNothing := request.Page == "" && request.Path == "" && request.LookUp == nil
	if namesNothing {
		return "", errors.New("name a page, a file or a word to look up")
	}

	err := errors.Join(s.checkPage(request.Page, request.Query), s.checkFile(request), s.checkLookUp(request.LookUp))
	if err != nil {
		return "", err
	}

	application.Get().Event.Emit(EventShow, request)

	return "PMT shows it.", nil
}
