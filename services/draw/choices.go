// choices.go works out the values the picks give a window's properties, and
// the pictures and rows its lists draw.

package draw

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// picturesSubject is what the group of a list of pictures asks about.
const picturesSubject = "Pictures"

// Choice is the value the picks give one property of a widget, with the
// text, image file or CSS color it leads to.
type Choice struct {
	Key     string        `json:"key"`
	Value   string        `json:"value"`
	Text    []loc.Segment `json:"text,omitempty"`
	Texture string        `json:"texture,omitempty"`
	Color   string        `json:"color,omitempty"`
}

// Row is one row a data model's list draws, for one definition of the
// database its widgets ask about.
type Row struct {
	Definition string `json:"definition"`

	// Choices holds, by widget of the row, the values the definition gives
	// its properties.
	Choices map[int32][]Choice `json:"choices"`
}

// PictureList says where the pictures are that a picture binding may draw.
// With a Name it lists the images one definition names; without, what the
// binding shows of each definition of a database.
type PictureList struct {
	// Key is the property whose binding draws the pictures.
	Key string `json:"key"`

	Database string `json:"database"`
	Name     string `json:"name"`

	// Type is the kind of object the binding asks about, as its data type or
	// in words, "" for none.
	Type string `json:"type"`

	// Field is the field the binding takes of the object, and Argument the
	// argument it passes as written. Either is "" for none.
	Field    string `json:"field"`
	Argument string `json:"argument"`

	// Folder is the icon folder of a kind whose objects are its subfolders
	// rather than definitions. Each subfolder holds an object's images, named
	// as the argument is.
	Folder string `json:"folder"`

	// Shown is what the binding draws with nothing picked: a definition's or
	// a folder's name, or a file.
	Shown string `json:"shown"`
}

// Picture is one picture a picture binding may draw. Picking it sets Value,
// its file or its definition's name.
type Picture struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// tierFrames is how a game draws a title's tier from a sheet of cells, one
// per tier.
type tierFrames struct {
	// Function gives a title's cell, counting from one. Tiers are in the
	// order of the cells.
	Function string
	Tiers    []string
}

// titleTiers holds, by game, how it draws a title's tier. A game with no row
// draws none.
var titleTiers = map[string]tierFrames{
	game.CK3: {
		Function: "GetTierFrame", Tiers: []string{"unlanded", "baron", "count", "duke", "king", "emperor", "hegemon"},
	},
}

// ListPictures lists the pictures a picture binding of a widget in a file may
// draw, as the given list says. It returns an error if no workspace is open.
func (s *Service) ListPictures(path string, list PictureList) ([]Picture, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := resolveIndex(ws, path)
	pictures := []Picture{}
	if list.Name != "" {
		definition, _ := findKeptClause(ws, mods, lang.Reference{Database: list.Database, Name: list.Name})
		for _, file := range listDefinitionImages(ws, mods, definition) {
			name := filepath.Base(file)
			pictures = append(pictures, Picture{Label: strings.TrimSuffix(name, filepath.Ext(name)), Path: file, Value: file})
		}

		return pictures, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	library, listed := s.findLibrary(ws, mods), map[string]bool{}
	for _, reference := range library.listObjects(list) {
		if file := library.findShown(list, reference); file != "" && !listed[reference.Name] {
			listed[reference.Name] = true
			pictures = append(pictures, Picture{Label: reference.Name, Path: file, Value: reference.Name})
		}
	}

	return pictures, nil
}

// listPictureGroups returns a group for each list of pictures the window's
// widgets draw from, with those widgets as its readers. Widgets that ask
// about the same kind of object share a group.
func listPictureGroups(window Window) []Group {
	var groups table[string, Group]
	for _, widget := range window.Widgets {
		if widget.Pictures == nil {
			continue
		}

		list := *widget.Pictures
		id := strings.TrimSuffix(cmp.Or(list.Database, list.Folder)+":"+list.Name, ":")
		group := addGroup(&groups, id, widget.ID, func() Group {
			label := cmp.Or(lang.ToPhrase(list.Name), lang.DescribeDefinition(list.Database, false), list.Type)

			return Group{ID: id, Subject: picturesSubject, Label: label, Pictures: &list}
		})
		group.Reach = len(group.Readers)
	}

	return groups.values
}

// listTierGroups returns a group for each of the window's data calls that
// gives a title's tier cell, with the widgets that write it as its readers
// and the game's tiers as its options.
func (e *evaluator) listTierGroups(tiers tierFrames) []Group {
	var groups table[string, Group]
	for _, widget := range e.window.Widgets {
		context := e.contexts[widget.ID]
		for _, position := range widget.Properties {
			if !isTierCall(tiers, e.window.calls[position]) {
				continue
			}

			call := context.expand(e.schema, *e.window.calls[position])
			id := toCallText(call)
			group := addGroup(&groups, id, widget.ID, func() Group {
				group := makeGroup(id, call)
				group.Options = tiers.Tiers

				return group
			})
			group.Reach = len(group.Readers)
		}
	}

	return groups.values
}

// pickTiers adds to the choices the cell, counted from one, that each picked
// tier gives its group's readers. A group with no pick keeps its cell as
// written.
func (e *evaluator) pickTiers(tiers tierFrames, groups []Group, picks map[string]string, choices map[int32][]Choice) {
	for _, group := range groups {
		cell := slices.Index(tiers.Tiers, picks[group.ID])
		if cell < 0 {
			continue
		}

		for _, reader := range group.Readers {
			for _, position := range e.window.Widgets[reader].Properties {
				if isTierCall(tiers, e.window.calls[position]) {
					key := e.window.Properties[position].Key
					choices[reader] = append(choices[reader], Choice{Key: key, Value: strconv.Itoa(cell + 1)})
				}
			}
		}
	}
}

// isTierCall reports whether a property's data call, nil for none, ends in
// the function that gives a title's tier cell.
func isTierCall(tiers tierFrames, call *jomini.Call) bool {
	return tiers.Function != "" && call != nil && call.Steps[len(call.Steps)-1].Text == tiers.Function
}

// pickColors adds to the choices the colors the picked definitions give the
// window's color bindings, as they give the pictures of that kind. A binding
// with no pick, or whose definition writes no color, keeps its color.
func pickColors(opened opening, picks map[string]string, choices map[int32][]Choice) {
	// the definition picked, by the database the group picks among
	definitions := map[string]string{}
	for _, group := range opened.pictures {
		if picked := picks[group.ID]; picked != "" && group.Pictures.Name == "" && group.Pictures.Database != "" {
			definitions[group.Pictures.Database] = picked
		}
	}

	for id, picked := range opened.library.pickDefinitions(opened.evaluator.window.Widgets, opened.colorLists, definitions) {
		choices[id] = append(choices[id], picked...)
	}
}

// listRows returns the rows each data model's list draws as the window
// opens, by the model's item: one for each of the first definitions of the
// database the item asks about, up to the window's row count. An item that
// asks about no database is left out, and the page draws sample rows for it.
func listRows(opened opening) map[int32][]Row {
	library, window, contexts := opened.library, &opened.evaluator.window, opened.evaluator.contexts
	items := listItems(window.Widgets)
	members := map[int32][]Widget{}
	for _, widget := range window.Widgets {
		if item := items[widget.ID]; item != 0 {
			members[item] = append(members[item], widget)
		}
	}

	rows := map[int32][]Row{}
	for item, widgets := range members {
		lists, database := map[int32][]PictureList{}, ""
		for _, widget := range widgets {
			lists[widget.ID] = listDatabaseLists(widget, opened.colorLists[widget.ID])
			for _, list := range lists[widget.ID] {
				if library.asksElement(window, widget, list, contexts[widget.ID]) {
					database = cmp.Or(database, list.Database)

					break
				}
			}
		}

		if database == "" {
			continue
		}

		var names []string
		for _, site := range library.mods.ListDefinitions(database) {
			if len(names) == opened.evaluator.rows {
				break
			}

			names = appendNew(names, library.mods.ToReference(site).Name)
		}

		rows[item] = []Row{}
		for _, name := range names {
			choices := library.pickDefinitions(window.Widgets, lists, map[string]string{database: name})
			rows[item] = append(rows[item], Row{Definition: name, Choices: choices})
		}
	}

	return rows
}

// asksElement reports whether the binding a widget writes under a list's key
// asks about an element of a data model: read from the given context, its
// call still starts at the object's type, which no data context leads to.
func (l *library) asksElement(window *Window, widget Widget, list PictureList, context context) bool {
	_, call := window.findCall(widget, list.Key)
	if call == nil {
		return false
	}

	path := context.expand(l.ws.Schema, *call)

	return len(path.Steps) > 1 && path.Steps[0].Text == list.Type
}

// listDatabaseLists returns the lists of a widget's bindings that ask about
// the definitions of a database: its picture binding's, then the given lists
// of its color bindings.
func listDatabaseLists(widget Widget, colorLists []PictureList) []PictureList {
	lists := []PictureList{}
	if widget.Pictures != nil {
		lists = append(lists, *widget.Pictures)
	}

	return slices.DeleteFunc(append(lists, colorLists...), func(list PictureList) bool {
		return list.Database == "" || list.Name != "" || list.Folder != ""
	})
}

// pickDefinitions returns, by widget, the values the given definitions give
// the widgets' properties through their lists. The definitions are keyed by
// the database each is picked among; a list of any other database says
// nothing.
func (l *library) pickDefinitions(
	widgets []Widget, lists map[int32][]PictureList, definitions map[string]string,
) map[int32][]Choice {
	choices := map[int32][]Choice{}
	for id, widgetLists := range lists {
		for _, list := range widgetLists {
			definition, ok := definitions[list.Database]
			if !ok {
				continue
			}

			if choice, ok := l.pickDefinition(widgets[id], list, definition); ok {
				choices[id] = append(choices[id], choice)
			}
		}
	}

	return choices
}

// pickDefinition returns the value a picked definition gives the property a
// list is drawn under: its color, or its picture. It returns false for a
// color the definition does not write.
func (l *library) pickDefinition(widget Widget, list PictureList, definition string) (Choice, bool) {
	if _, isColor := widget.Colors[list.Key]; isColor {
		color := l.findFieldColor(list, lang.Reference{Database: list.Database, Name: definition})

		return Choice{Key: list.Key, Value: definition, Color: color}, color != ""
	}

	return l.pickPicture(list, definition), true
}

// listChoices returns, by widget, the values the groups give its properties
// written as a choice, with the texts and files they lead to. A property
// whose condition is not known is left out.
func (e *evaluator) listChoices(library *library) map[int32][]Choice {
	choices := map[int32][]Choice{}
	for _, widget := range e.window.Widgets {
		for _, choice := range e.listWidgetChoices(widget.ID) {
			text, texture := library.resolveValue(choice.Key, choice.Value)
			choice.Text = library.sampleText(text, e.contexts[widget.ID], e.window.listProperties(widget))
			choice.Texture = texture
			choices[widget.ID] = append(choices[widget.ID], choice)
		}
	}

	return choices
}

// listWidgetChoices returns the values the groups give one widget's
// properties written as a choice, without the texts or files they lead to.
func (e *evaluator) listWidgetChoices(widget int32) []Choice {
	var choices []Choice
	for position := range e.rules[widget].choices {
		choice := &e.rules[widget].choices[position]
		if value, ok := choice.choose(e.values); ok {
			choices = append(choices, Choice{Key: choice.key, Value: value})
		}
	}

	return choices
}

// choose returns the value the selection gives at the groups' values. It
// returns false if its condition is not known, or the value it chooses is a
// data call that is no selection.
func (s *selection) choose(values map[string]string) (string, bool) {
	holds, known := s.condition.evaluate(values)
	if !known {
		return "", false
	}

	chosen := s.branches[1]
	if holds {
		chosen = s.branches[0]
	}

	switch {
	case chosen.nested != nil:
		return chosen.nested.choose(values)
	case chosen.isCall:
		return "", false
	}

	return chosen.value, true
}

// selectPrefix starts the names of the data functions that give their second
// argument if their first holds, else their third, as Select_float does.
const selectPrefix = "Select_"

// splitSelect returns the three arguments of a call that chooses between two
// values, or false if the call is no such choice.
func splitSelect(call jomini.Call) ([]jomini.Argument, bool) {
	if len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 3 || !strings.HasPrefix(call.Steps[0].Text, selectPrefix) {
		return nil, false
	}

	return call.Steps[0].Arguments, true
}

// toWrittenValue returns a typed argument as a GUI file writes the value:
// '(CVector2f)0,140' is 0 140. Any other text is returned as it is.
func toWrittenValue(text string) string {
	typed, isTyped := strings.CutPrefix(text, "(")
	_, value, hasType := strings.Cut(typed, ")")
	if !isTyped || !hasType {
		return text
	}

	return strings.ReplaceAll(value, ",", " ")
}
