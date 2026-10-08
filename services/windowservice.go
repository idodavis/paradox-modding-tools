// windowservice.go opens the app's other windows (O-R24, the MultiWindow board): a page, an editor group, the
// game's files or the panel, each the frontend at its own route. The windows tell each other what they hold through
// the events below; Go opens, shows and closes them (the runtime acts only on the window that calls it: Wails sets a
// call's window to its caller's), and holds a closing one until it has handed its tabs back.

package services

import (
	"net/url"
	"sync"

	"paradox-modding-tools/services/internal/lsp"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	EventWindowHeld    = "window:held"    // a window tells the others what it holds: data is WindowHeld
	EventWindowHello   = "window:hello"   // a window opened and asks the others what they hold: data is its name
	EventWindowTabs    = "window:tabs"    // tabs handed to a window: data is WindowTabs
	EventWindowGive    = "window:give"    // a window asked to hand a tab to another (a tab dragged there): data is WindowGive
	EventWindowShow    = "window:show"    // a window asked to the front, at a route or a file: data is WindowShow
	EventWindowResults = "window:results" // the Search results another window found, told to the main window's sidebar: data is PanelResults
	EventWindowClosing = "window:closing" // a window asked to hand its tabs back before it closes: data is its name
	EventWindowClosed  = "window:closed"  // a window closed: data is its name
	EventWindowChanged = "window:changed" // a window saved what the others read: data is its name
)

func init() {
	application.RegisterEvent[WindowHeld](EventWindowHeld)
	application.RegisterEvent[string](EventWindowHello)
	application.RegisterEvent[WindowTabs](EventWindowTabs)
	application.RegisterEvent[WindowGive](EventWindowGive)
	application.RegisterEvent[WindowShow](EventWindowShow)
	application.RegisterEvent[PanelResults](EventWindowResults)
	application.RegisterEvent[string](EventWindowClosing)
	application.RegisterEvent[string](EventWindowClosed)
	application.RegisterEvent[string](EventWindowChanged)
}

// WindowHeld is what a window holds: its title, the folders its explorer shows (their files open there), its tabs,
// the files among them with unsaved edits (which the main window's close asks about), and their files' problems.
type WindowHeld struct {
	Name     string                      `json:"name"`
	Title    string                      `json:"title"`
	Roots    []string                    `json:"roots"`
	Tabs     []string                    `json:"tabs"`
	Unsaved  []string                    `json:"unsaved"`
	Problems map[string][]lsp.Diagnostic `json:"problems"`
}

// WindowTabs is tabs handed to a window, the one shown last.
type WindowTabs struct {
	To     string      `json:"to"`
	Tabs   []WindowTab `json:"tabs"`
	Active string      `json:"active"`
}

// WindowTab is a tab (a file's path, or a diff tab) and, when its file has unsaved edits, its text.
type WindowTab struct {
	Tab     string `json:"tab"`
	Unsaved bool   `json:"unsaved"`
	Text    string `json:"text"`
}

// WindowGive asks the window holding a tab to hand it to another.
type WindowGive struct {
	To     string `json:"to"`
	Tab    string `json:"tab"`
	Window string `json:"window"`
}

// WindowShow asks a window to come to the front, at a route (a link followed in a window that shows one thing),
// or showing a file at a 0-based line and UTF-8 byte column; a line of -1 keeps where the file was.
type WindowShow struct {
	To    string `json:"to"`
	Route string `json:"route"`
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
}

// PanelResults is what the sidebar's Search results show: what was looked for, its rows so far, and whether it is
// done or stopped at the cap.
type PanelResults struct {
	Title  string          `json:"title"`
	Rows   []TextSearchHit `json:"rows"`
	Done   bool            `json:"done"`
	Capped bool            `json:"capped"`
}

// WindowService opens the other windows.
type WindowService struct{}

// the windows asked about their close (to hand their tabs back, the main window whether to quit), whose next close
// goes through: a window that cannot answer still closes on a second try
var held = struct {
	sync.Mutex
	asked map[string]bool
}{asked: map[string]bool{}}

// HoldClose holds a window's first close and asks it (window:closing); the close it then asks for goes through.
func HoldClose(window *application.WebviewWindow) {
	name := window.Name()
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		held.Lock()
		defer held.Unlock()
		if held.asked[name] {
			delete(held.asked, name)
			return
		}
		held.asked[name] = true
		event.Cancel()
		emit(EventWindowClosing, name)
	})
}

// QuitAnswer is the main window's answer to its close: quit, or stay, its next close asking again.
func (s *WindowService) QuitAnswer(quit bool) {
	if quit {
		application.Get().Quit()
		return
	}
	held.Lock()
	defer held.Unlock()
	delete(held.asked, "main")
}

// Show brings the window with a name to the front.
func (s *WindowService) Show(name string) {
	if window, ok := application.Get().Window.GetByName(name); ok {
		window.Show()
		window.Focus()
	}
}

// Close closes the window with a name; another than the main window hands its tabs back first.
func (s *WindowService) Close(name string) {
	if window, ok := application.Get().Window.GetByName(name); ok {
		window.Close()
	}
}

// Open shows the window with a name, made the first time at a route (a hash route: /workspace/<id>/graph).
func (s *WindowService) Open(route, name, title string) {
	app := application.Get()
	if _, ok := app.Window.GetByName(name); ok {
		s.Show(name)
		return
	}
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   name,
		Title:  title,
		Width:  1000,
		Height: 760,
		URL:    "/?window=" + url.QueryEscape(name) + "#" + route,
	})
	// its first close is held and the window asked to hand its tabs back
	HoldClose(window)
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { emit(EventWindowClosed, name) })
}
