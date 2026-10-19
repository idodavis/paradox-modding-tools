// draw.go is what both drawn kinds share: the request, the element every drawn thing is, a property as written and
// what nothing found; the calls that answer a GUI entry from one resolve kept between them and a coat from its own;
// and the resolve both start from: the request's entry, the definitions and routes read once, and what is missing.

package draw

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/texture"
	"paradox-modding-tools/services/internal/workspace"
)

// FileRoute is where the page reads a file: ?path= its absolute path and &v= when it changed.
const FileRoute = "/pmt/file"

// Request is what a render is asked for: the file, and the entry's id and database ("" for the first of the id). Its
// texts are in the workspace's language.
type Request struct {
	Path     string `json:"path"`
	ID       string `json:"id"`
	Database string `json:"database"`
}

// Element is one drawn thing of an entry, a GUI widget or a coat layer: where it sits in the tree, how it is named on
// the way down, and where it is written; the page selects, marks and links it to its code by these alone.
type Element struct {
	Parent    int          `json:"parent"` // -1 at the root
	Key       string       `json:"key"`
	Type      string       `json:"type"` // a widget's built-in type; a layer's kind: coat, pattern, emblem, instance, sub
	Name      string       `json:"name"`
	Label     string       `json:"label"` // the key, then the name where it has one of its own
	Path      string       `json:"path"`  // the labels from the root down
	Ancestors []int        `json:"ancestors"`
	Location  lsp.Location `json:"location"`
}

// Property is one property of an element as written, with the type, template or parent coat that wrote it ("" the
// element), where, and where its value is, which an edit replaces.
type Property struct {
	Key           string       `json:"key"`
	Value         string       `json:"value"`
	Writer        string       `json:"writer"`
	Location      lsp.Location `json:"location"`
	ValueLocation lsp.Location `json:"valueLocation"`
}

// Absent is a thing the game's files did not give: the status line's sentence and the element that asked (-1 none).
type Absent struct {
	Text    string `json:"text"`
	Element int    `json:"element"`
}

// Render is the last GUI entry resolved, kept while the session's records and the request are the same.
type Render struct {
	mu      sync.Mutex
	changes int64
	request Request
	last    *guiResolver
}

// resolver is what a resolve of either kind shares: the workspace, the parses made for it, the request, the element
// being resolved, where a thing not found is listed, the definitions and routes read once, and what nothing found.
type resolver struct {
	ws          *workspace.Session
	parses      workspace.Parses
	request     Request
	element     int                  // -1 for none
	definitions map[string]statement // by "caller|database|id"; no assignment for none
	urls        map[string]string    // a file's route by path
	missing     []Absent
}

// maxDepth is the deepest types, templates, blocks and parent coats nest before a cycle is assumed.
const maxDepth = 40

// missingTexts is the status line's sentence for each thing not found, of either kind; %s is what was asked for.
var missingTexts = map[string]string{
	"template": "Template not found: %s", "type": "Type not found: %s",
	"cycle": "Type cycle: %s", "templatecycle": "Template cycle: %s",
	"texture": "Texture not found: %s", fontKey: "Font not found: %s", "fontsize": "Font size missing: %s",
	"format": "Text format not found: %s", "texticon": "Text icon not found: %s", "loc": "Localization key not found: %s",
	"blend": "Blend mode not drawn: %s", "field": "Field size not found in %s",
	"list": "Picked from a list, not drawn: %s", "parent": "Parent coat not found: %s",
	"color": "Named color not found: %s",
}

// List is the request's GUI entry resolved into a list. The caller holds the workspace's read lock.
func (render *Render) List(ws *workspace.Session, request Request) (*List, error) {
	render.mu.Lock()
	defer render.mu.Unlock()
	resolver, err := render.gui(ws, request)
	if err != nil {
		return nil, err
	}
	return resolver.list, nil
}

// Properties is an element's properties, a widget's or a coat layer's: every key that applies to it but a state, its
// last value, its writer and where it is written, in the order the keys are first written. The caller holds the
// workspace's read lock.
func (render *Render) Properties(ws *workspace.Session, request Request, element int) ([]Property, error) {
	render.mu.Lock()
	defer render.mu.Unlock()
	elementProperties, err := render.statementsOf(ws, request)
	if err != nil {
		return nil, err
	}
	if element < 0 || element >= len(elementProperties) {
		return nil, fmt.Errorf("the entry has no element %d", element)
	}
	properties := elementProperties[element]
	rows := []Property{}
	keys := map[string]bool{}
	for _, statement := range properties.statements {
		key := strings.ToLower(statement.assignment.Key.Text)
		if key == stateKey || keys[key] {
			continue
		}
		keys[key] = true
		last, _ := properties.last(key)
		rows = append(rows, Property{
			Key: last.assignment.Key.Text, Value: last.text(), Writer: last.writer,
			Location: lsp.Locate(ws, last.path, last.span()), ValueLocation: lsp.Locate(ws, last.path, last.valueSpan()),
		})
	}
	return rows, nil
}

// CoatOf is the request's coat of arms resolved into its layers. The caller holds the workspace's read lock.
func CoatOf(ws *workspace.Session, request Request) (*Coat, error) {
	resolver, err := newCoatResolver(newResolver(ws, request))
	if err != nil {
		return nil, err
	}
	return resolver.coat, nil
}

// CoatsOf are coats of one file resolved over one resolve's parses, definitions and routes, so a file their parents
// are in is read once; a coat the file does not hold is nil. The caller holds the workspace's read lock.
func CoatsOf(ws *workspace.Session, path string, ids []string) []*Coat {
	shared := newResolver(ws, Request{Path: path, Database: game.CoatDatabases.Coats})
	coats := make([]*Coat, 0, len(ids))
	for _, id := range ids {
		core := *shared
		core.request.ID, core.element, core.missing = id, -1, []Absent{}
		resolver, err := newCoatResolver(&core)
		if err != nil {
			coats = append(coats, nil)
			continue
		}
		coats = append(coats, resolver.coat)
	}
	return coats
}

// gui is the request's GUI resolve: the last where the request and the session's records are the same, else anew.
func (render *Render) gui(ws *workspace.Session, request Request) (*guiResolver, error) {
	if render.last != nil && render.changes == ws.Changes && render.request == request {
		return render.last, nil
	}
	resolver, err := newGUIResolver(newResolver(ws, request))
	if err != nil {
		return nil, err
	}
	render.changes, render.request, render.last = ws.Changes, request, resolver
	return resolver, nil
}

// statementsOf is what applies to each element of the request's entry, by element: the widgets' statements from the
// GUI resolve kept, a coat's layers' from the coat resolved anew.
func (render *Render) statementsOf(ws *workspace.Session, request Request) ([]*properties, error) {
	if request.Database == game.CoatDatabases.Coats {
		resolver, err := newCoatResolver(newResolver(ws, request))
		if err != nil {
			return nil, err
		}
		return resolver.layerProperties, nil
	}
	resolver, err := render.gui(ws, request)
	if err != nil {
		return nil, err
	}
	elementProperties := make([]*properties, len(resolver.widgets))
	for ordinal, widget := range resolver.widgets {
		elementProperties[ordinal] = &widget.properties
	}
	return elementProperties, nil
}

// newResolver is a request's resolve before anything is read.
func newResolver(ws *workspace.Session, request Request) *resolver {
	return &resolver{
		ws: ws, parses: workspace.Parses{}, request: request, element: -1, definitions: map[string]statement{},
		urls: map[string]string{}, missing: []Absent{},
	}
}

// entry is the statement that defines the request's entry in its file, of one of a kind's databases; the request's
// database becomes the entry's. what names the kind's entries for the error that none is there.
func (resolver *resolver) entry(what string, databases []string) (statement, error) {
	request := resolver.request
	if _, _, isInWorkspace := resolver.ws.Locate(request.Path); !isInWorkspace {
		return statement{}, fmt.Errorf("%s is not in the open workspace: it is under neither its %s install nor its mods",
			request.Path, resolver.ws.Game.Name)
	}
	parsed, _, err := resolver.ws.Parsed(resolver.parses, request.Path)
	if err != nil {
		return statement{}, err
	}
	for _, scanned := range resolver.ws.Indexes() {
		for _, def := range scanned.InFile(request.Path) {
			occurrence := def.Occurrence()
			isEntry := occurrence.Role == schema.Defines && occurrence.ID == request.ID &&
				slices.Contains(databases, occurrence.Database) &&
				(request.Database == "" || occurrence.Database == request.Database)
			definition, isAssignment := parsed.AssignmentAt(int(occurrence.Line), int(occurrence.Col)).(*jomini.Assignment)
			if isEntry && isAssignment {
				resolver.request.Database = occurrence.Database
				return statement{assignment: definition, parsed: parsed, path: request.Path}, nil
			}
		}
	}
	return statement{}, fmt.Errorf("%s holds no %s %q", request.Path, what, request.ID)
}

// definition is the statement a reference resolves to, written by its id, read once a render; false for none.
func (resolver *resolver) definition(ref schema.Occurrence) (statement, bool) {
	key := ref.Caller + "|" + ref.Database + "|" + ref.ID
	definition, isKnown := resolver.definitions[key]
	if !isKnown {
		if winner, isDefined := resolver.winner(ref); isDefined {
			parsed, assignment := resolver.ws.Statement(resolver.parses, winner)
			if assignment, isAssignment := assignment.(*jomini.Assignment); isAssignment {
				definition = statement{assignment: assignment, parsed: parsed, path: winner.File().Path, writer: ref.ID}
			}
		}
		resolver.definitions[key] = definition
	}
	return definition, definition.assignment != nil
}

// winner is the definition a reference resolves to: one of this file first (a local type or template), else the
// load order's winner; false for none.
func (resolver *resolver) winner(ref schema.Occurrence) (index.At, bool) {
	ref.Role = schema.Refers
	defs, _ := index.Resolve(resolver.ws.Install.Schema, resolver.ws.Indexes(), ref, resolver.request.Path)
	if local := slices.IndexFunc(defs, func(def index.At) bool { return def.Occurrence().Local }); local >= 0 {
		return defs[local], true
	}
	winner, _, _, _ := resolver.ws.Winner(defs)
	return winner, winner != index.At{}
}

// identify fills how an element is named and where it is written, before its children are, which name it in their
// paths: its name an id ("" for none), the root's the request's; its label its key, then a name of its own; its path
// the labels from the root down. parent is nil at the root.
func (resolver *resolver) identify(element, parent *Element, statement statement, id string) {
	element.Name, element.Label = id, element.Key
	if parent == nil {
		element.Name = resolver.request.ID
	}
	if element.Name != "" && element.Name != element.Key {
		element.Label += " " + element.Name
	}
	element.Path, element.Ancestors = element.Label, []int{}
	if parent != nil {
		element.Path = parent.Path + " › " + element.Label
		element.Ancestors = append(slices.Clone(parent.Ancestors), element.Parent)
	}
	element.Location = lsp.Locate(resolver.ws, statement.path, statement.span())
}

// propertyTexts is every property statements write, by lower-cased key, the last written, its scalars as written, a
// call the preview answers replaced by the entry's value.
func (resolver *resolver) propertyTexts(statements []statement) map[string]string {
	properties := map[string]string{}
	for _, statement := range statements {
		scalars := statement.scalars()
		if len(scalars) == 0 {
			continue
		}
		text := strings.Join(scalars, " ")
		if define, isAnswered := resolver.answered(text); isAnswered {
			text = strings.TrimSuffix(strings.Join(define.scalars(), " "), ";")
		}
		properties[strings.ToLower(statement.assignment.Key.Text)] = text
	}
	return properties
}

// answered is the entry a value that is one call of GUIAnswered names, its last argument the id; false otherwise.
func (resolver *resolver) answered(text string) (statement, bool) {
	steps := jomini.ParseCall(text, 0)
	if !isBinding(text) || len(steps) != 1 || len(steps[0].Args) == 0 {
		return statement{}, false
	}
	database, isAnswered := game.GUIAnswered[steps[0].Name]
	if !isAnswered {
		return statement{}, false
	}
	args := steps[0].Args
	return resolver.definition(schema.Occurrence{Database: database, ID: args[len(args)-1].Text})
}

// addMissing lists a thing not found at the element being resolved, once.
func (resolver *resolver) addMissing(cause, text string) {
	absent := Absent{Text: fmt.Sprintf(missingTexts[cause], text), Element: resolver.element}
	if !slices.Contains(resolver.missing, absent) {
		resolver.missing = append(resolver.missing, absent)
	}
}

// urlOf is the route's URL for a file, made once a render.
func (resolver *resolver) urlOf(path string) string {
	if route, isKnown := resolver.urls[path]; isKnown {
		return route
	}
	route := routeOf(path)
	resolver.urls[path] = route
	return route
}

// fileOf is the file a path written in a GUI file names, from what the scan holds (texture.Resolve).
func (resolver *resolver) fileOf(guiPath string) (string, bool) {
	return texture.Resolve(guiPath, func(rel string) (string, bool) {
		file, isListed := index.FileAt(resolver.ws.Indexes(), rel)
		return file.Path, isListed
	})
}

// routeOf is the route's URL for a file, with when it changed.
func routeOf(path string) string {
	version := "0"
	if stat, err := os.Stat(path); err == nil {
		version = strconv.FormatInt(stat.ModTime().UnixMilli(), 10)
	}
	return FileRoute + "?path=" + url.QueryEscape(path) + "&v=" + version
}
