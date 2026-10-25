// choices.go works out the values that the picks give a window's properties: the one of two values that a condition
// chooses, with the text or the image file that value leads to.

package draw

import (
	"cmp"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
)

// picturesSubject is what the group of a list of pictures asks about.
const picturesSubject = "Pictures"

// A Choice is the value that the picks give one property of a widget, with what it leads to: the text where its key
// writes a text, the image file where it names a picture, and the CSS color where it binds a definition's color. The
// text is as Widget.Texts holds one.
type Choice struct {
	Key     string        `json:"key"`
	Value   string        `json:"value"`
	Text    []loc.Segment `json:"text,omitempty"`
	Texture string        `json:"texture,omitempty"`
	Color   string        `json:"color,omitempty"`
}

// A Row is one row that a data model's list draws for one definition of the database its widgets read: the
// definition, and the values it gives the properties of the widgets in the row, by widget.
type Row struct {
	Definition string             `json:"definition"`
	Choices    map[int32][]Choice `json:"choices"`
}

// A PictureList is where the pictures are listed that a picture binding may draw: the images that one definition
// writes, or, where Name is "", what the binding shows of each definition of a database, which a pick of one sets.
type PictureList struct {
	Key      string `json:"key"` // the key of the property whose binding draws them
	Database string `json:"database"`
	Name     string `json:"name"`
	// Type is the kind of object the binding reads, as its data type or its words; Field is the field it reads of one
	// (realm_frame), and Argument the argument it passes as written (62), each "" for none.
	Type     string `json:"type"`
	Field    string `json:"field"`
	Argument string `json:"argument"`
	// Folder is, for a kind whose objects are the subfolders of its icon folder rather than definitions, that folder:
	// each subfolder holds the object's images, named by the argument (graphical_cultures/european_gfx).
	Folder string `json:"folder"`
	Shown  string `json:"shown"` // what it draws with nothing picked: a definition's or a folder's name, or a file
}

// A Picture is one picture that a picture binding may draw: its image file, labeled by its file or its definition, and
// the value that picking it sets: its file, or its definition's name.
type Picture struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// ListPictures lists the pictures that a picture binding of a widget in the given file may draw, as the widget's
// Pictures says where: each image that the named definition writes, labeled by its file, or, for a list with no name,
// what the binding shows of each definition of the database, labeled by the definition. It returns an error if no
// workspace is open.
func (drw *Service) ListPictures(path string, list PictureList) ([]Picture, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := resolveIndex(ws, path)
	if list.Name == "" {
		return (&library{ws: ws, mods: mods}).listShown(list), nil
	}
	definition, _ := findKeptClause(ws, mods, schema.Reference{Database: list.Database, Name: list.Name})
	return lo.Map(listDefinitionImages(ws, mods, definition), func(file string, _ int) Picture {
		return Picture{Label: toFileLabel(file), Path: file, Value: file}
	}), nil
}

// listPictureGroups returns a group for each list of pictures that the window's widgets draw from, in the order first
// drawn, with those widgets as its readers: one per definition whose images are listed, and one per database whose
// definitions are, which every widget reading that kind of object shares.
func listPictureGroups(window Window) []Group {
	var groups []Group
	positions := map[string]int{}
	for _, widget := range window.Widgets {
		if widget.Pictures == nil {
			continue
		}
		list := *widget.Pictures
		id := strings.TrimSuffix(cmp.Or(list.Database, list.Folder)+":"+list.Name, ":")
		position, known := positions[id]
		if !known {
			position, positions[id] = len(groups), len(groups)
			label := cmp.Or(schema.ToPhrase(list.Name), game.DescribeDefinition(list.Database, false), list.Type)
			groups = append(groups, Group{ID: id, Subject: picturesSubject, Label: label, Pictures: &list})
		}
		groups[position].Readers = append(groups[position].Readers, widget.ID)
		groups[position].Reach = len(groups[position].Readers)
	}
	return groups
}

// toFileLabel returns the label of an image file: its name without its extension.
func toFileLabel(file string) string {
	name := path.Base(filepath.ToSlash(file))
	return strings.TrimSuffix(name, path.Ext(name))
}

// listTierGroups returns a group for each data call of the window that gives the cell of a title's tier in a sheet of
// cells (Game.GUITiers), by the call written from the window down, with the widgets that write it as its readers. Its
// options are the game's tiers.
func (evaluator *evaluator) listTierGroups(tiers game.TierFrames) []Group {
	groups := []Group{}
	positions := map[string]int{}
	for _, widget := range evaluator.window.Widgets {
		evaluator.context = evaluator.contexts[widget.ID]
		for property := range evaluator.window.listProperties(widget) {
			call, ok := findTierCall(tiers, property)
			if !ok {
				continue
			}
			id := toCallText(evaluator.reach(call))
			position, known := positions[id]
			if !known {
				position, positions[id] = len(groups), len(groups)
				group := makeGroup(id)
				group.Options = tiers.Tiers
				groups = append(groups, group)
			}
			group := &groups[position]
			group.Readers, group.Reach = append(group.Readers, widget.ID), len(group.Readers)+1
		}
	}
	return groups
}

// pickTiers adds to the given choices the cells that the tiers picked in the given picks give the readers of the
// given tier groups: each reader's property that calls the tier function gets the picked tier's cell, counted from
// one. A group that no pick sets draws its cell as written.
func (evaluator *evaluator) pickTiers(tiers game.TierFrames, groups []Group, picks map[string]string, choices map[int32][]Choice) {
	for _, group := range groups {
		cell := slices.Index(tiers.Tiers, picks[group.ID])
		if cell < 0 {
			continue
		}
		for _, reader := range group.Readers {
			for property := range evaluator.window.listProperties(evaluator.window.Widgets[reader]) {
				if _, ok := findTierCall(tiers, property); ok {
					choices[reader] = append(choices[reader], Choice{Key: property.Key, Value: strconv.Itoa(cell + 1)})
				}
			}
		}
	}
}

// findTierCall returns the data call that a property writes where its last step is the function that gives a title's
// tier its cell (game.TierFrames.Function). The last result is false for any other property, and for every property
// in a game with no such function.
func findTierCall(tiers game.TierFrames, property Property) (jomini.Call, bool) {
	if tiers.Function == "" || !strings.Contains(property.Value, tiers.Function) {
		return jomini.Call{}, false
	}
	call, ok := jomini.ParseBracketed(property.Value, 0)
	return call, ok && call.Steps[len(call.Steps)-1].Text == tiers.Function
}

// pickColors adds to the given choices the colors that the values picked for the given picture groups in the given
// picks give the window's bindings of a definition's color. A binding of the kind of object that a group of a
// database picks among takes the color of the definition picked, as the pictures of that kind take its picture. A
// binding that no group picks for, or whose picked definition writes no color, keeps the color it has.
func (evaluator *evaluator) pickColors(library *library, groups []Group, picks map[string]string, choices map[int32][]Choice) {
	definitions := map[string]string{} // the definition picked, by the database the group picks among
	for _, group := range groups {
		if picked := picks[group.ID]; picked != "" && group.Pictures.Name == "" && group.Pictures.Database != "" {
			definitions[group.Pictures.Database] = picked
		}
	}
	if len(definitions) == 0 {
		return
	}
	for _, widget := range evaluator.window.Widgets {
		for _, list := range library.listDatabaseLists(&evaluator.window, widget, evaluator.contexts[widget.ID]) {
			_, isColor := widget.Colors[list.Key]
			definition, ok := definitions[list.Database]
			if !isColor || !ok {
				continue
			}
			if choice, chosen := library.pickDefinition(widget, list, definition); chosen {
				choices[widget.ID] = append(choices[widget.ID], choice)
			}
		}
	}
}

// listRows returns, by the item of a data model in a window, the rows its list draws: one for each of the first
// definitions of the database of the objects the list holds, up to the given count, in load order and each name once.
// That is the database of the first list, among those listDatabaseLists finds for the item's widgets, its nested
// items' apart, whose binding reads its object as an element, as readsElement says. Each row gives the widgets that
// read the database the pictures and colors of its definition. An item that reads no such list is left out, and its
// list draws the page's sample rows.
func listRows(library *library, window Window, count int) map[int32][]Row {
	contexts := listContexts(library.ws.Schema, window)
	items := listItems(window.Widgets)
	rows := map[int32][]Row{}
	for item, members := range lo.GroupBy(window.Widgets, func(widget Widget) int32 { return items[widget.ID] }) {
		if item == 0 {
			continue
		}
		lists, database := map[int32][]PictureList{}, ""
		for _, widget := range members {
			lists[widget.ID] = library.listDatabaseLists(&window, widget, contexts[widget.ID])
			if element, ok := lo.Find(lists[widget.ID], func(list PictureList) bool {
				return library.readsElement(&window, widget, list, contexts[widget.ID])
			}); ok {
				database = cmp.Or(database, element.Database)
			}
		}
		if database != "" {
			rows[item] = library.makeRows(database, lists, window.Widgets, count)
		}
	}
	return rows
}

// readsElement reports whether the binding under a list's key of one of a window's widgets, read in the given data
// context, reads its object as an element of a data model: the binding starts at the object's type, which no data
// context reaches.
func (library *library) readsElement(window *Window, widget Widget, list PictureList, context context) bool {
	call, _ := jomini.ParseBracketed(findProperty(window.listProperties(widget), list.Key).Value, 0)
	path := context.expand(library.ws.Schema, call)
	return len(path.Steps) > 1 && path.Steps[0].Text == list.Type
}

// makeRows returns a row for each of the first definitions of a database, up to the given count, in load order and
// each name once. A row holds the values that its definition gives the window's widgets through those of their given
// lists that read the database, by widget.
func (library *library) makeRows(database string, lists map[int32][]PictureList, widgets []Widget, count int) []Row {
	names := lo.Uniq(lo.Map(library.mods.ListDefinitions(database), func(site index.Site, _ int) string {
		return library.mods.ToReference(site).Name
	}))
	names = names[:min(count, len(names))]
	reading := lo.MapValues(lists, func(read []PictureList, _ int32) []PictureList {
		return lo.Filter(read, func(list PictureList, _ int) bool { return list.Database == database })
	})
	rows := make([]Row, len(names))
	for position, name := range names {
		rows[position] = Row{Definition: name, Choices: map[int32][]Choice{}}
		for id, read := range reading {
			for _, list := range read {
				if choice, ok := library.pickDefinition(widgets[id], list, name); ok {
					rows[position].Choices[id] = append(rows[position].Choices[id], choice)
				}
			}
		}
	}
	return rows
}

// listDatabaseLists returns the lists of the bindings of one of a window's widgets that read the definitions of a
// database, read in the given data context of the widget: the list of its picture binding, then that of each of its
// bindings of a definition's color, by property key. A list that names its definition, or whose objects are folders,
// reads no database.
func (library *library) listDatabaseLists(window *Window, widget Widget, context context) []PictureList {
	var lists []PictureList
	if widget.Pictures != nil {
		lists = append(lists, *widget.Pictures)
	}
	for _, key := range slices.Sorted(maps.Keys(widget.Colors)) {
		call, _ := jomini.ParseBracketed(findProperty(window.listProperties(widget), key).Value, 0)
		list, _ := library.makePictureList(key, context.expand(library.ws.Schema, call))
		lists = append(lists, list)
	}
	return lo.Filter(lists, func(list PictureList, _ int) bool {
		return list.Database != "" && list.Name == "" && list.Folder == ""
	})
}

// pickDefinition returns the value that picking a definition gives the property a list is read under: the
// definition's color for a binding of a definition's color, else its picture. It returns false for a color that the
// definition does not write.
func (library *library) pickDefinition(widget Widget, list PictureList, definition string) (Choice, bool) {
	if _, isColor := widget.Colors[list.Key]; isColor {
		color := library.findFieldColor(list, schema.Reference{Database: list.Database, Name: definition})
		return Choice{Key: list.Key, Value: definition, Color: color}, color != ""
	}
	return library.pickPicture(list, definition), true
}

// listChoices returns, by widget, the values that the evaluator's values give the properties written as a choice
// between two values. A text a value leads to has its data calls' samples, as sampleText makes them. A property whose
// condition is unknown chooses nothing and is left out, and so is a widget with no choice.
func (evaluator *evaluator) listChoices(library *library) map[int32][]Choice {
	choices := map[int32][]Choice{}
	for _, widget := range evaluator.window.Widgets {
		for _, choice := range evaluator.listWidgetChoices(widget.ID) {
			text, texture := library.resolveValue(choice.Key, choice.Value)
			choice.Text = library.sampleText(text, evaluator.contexts[widget.ID], evaluator.window.listProperties(widget))
			choice.Texture = texture
			choices[widget.ID] = append(choices[widget.ID], choice)
		}
	}
	return choices
}

// listWidgetChoices returns the values that the evaluator's values give one widget's properties written as a choice
// between two values, without the texts or files they lead to. A property whose condition is unknown is left out.
func (evaluator *evaluator) listWidgetChoices(widget int32) []Choice {
	evaluator.context = evaluator.contexts[widget]
	var choices []Choice
	for property := range evaluator.window.listProperties(evaluator.window.Widgets[widget]) {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		if value, ok := evaluator.choose(call); isCall && ok {
			choices = append(choices, Choice{Key: property.Key, Value: value})
		}
	}
	return choices
}

// choose returns the value that a choice between two values gives under the evaluator's values: the second argument
// where the first holds, else the third, a choice nested in that argument worked out the same way. The last result
// is false if the call is no such choice, its condition is unknown, or the value it chooses is another data call.
func (evaluator *evaluator) choose(call jomini.Call) (string, bool) {
	arguments, ok := splitSelect(call)
	if !ok || arguments[0].Call == nil {
		return "", false
	}
	holds, known := evaluator.evaluateCall(*arguments[0].Call)
	if !known {
		return "", false
	}
	branch := arguments[lo.Ternary(holds, 1, 2)]
	switch {
	case branch.Call != nil:
		return evaluator.choose(*branch.Call)
	case branch.Quoted:
		return toWrittenValue(branch.Text), true
	}
	return branch.Text, true
}

// listChoiceConditions returns the conditions that the choices between two values of one of a window's widgets are made
// by, those of a choice nested in another's argument too.
func listChoiceConditions(window *Window, widget Widget) []jomini.Call {
	var conditions []jomini.Call
	for property := range window.listProperties(widget) {
		if call, ok := jomini.ParseBracketed(property.Value, 0); ok {
			conditions = append(conditions, listSelectConditions(call)...)
		}
	}
	return conditions
}

// listSelectConditions returns the condition of a choice between two values, then those of the choices nested in its
// arguments. It returns none for a call that is no such choice.
func listSelectConditions(call jomini.Call) []jomini.Call {
	arguments, ok := splitSelect(call)
	if !ok {
		return nil
	}
	var conditions []jomini.Call
	if arguments[0].Call != nil {
		conditions = append(conditions, *arguments[0].Call)
	}
	for _, argument := range arguments[1:] {
		if argument.Call != nil {
			conditions = append(conditions, listSelectConditions(*argument.Call)...)
		}
	}
	return conditions
}

// splitSelect returns the condition and the two values of a call that chooses between two values, as its three
// arguments. The last result is false if the call is no such choice.
func splitSelect(call jomini.Call) ([]jomini.Argument, bool) {
	if len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 3 || !strings.HasPrefix(call.Steps[0].Text, game.GUI.SelectPrefix) {
		return nil, false
	}
	return call.Steps[0].Arguments, true
}

// toWrittenValue converts a quoted argument to the value as a GUI file writes it. A value written with its type in
// parentheses loses the type, and the parts of a vector are set apart by spaces: '(CVector2f)0,140' is 0 140.
func toWrittenValue(text string) string {
	typed, isTyped := strings.CutPrefix(text, "(")
	_, value, hasType := strings.Cut(typed, ")")
	if !isTyped || !hasType {
		return text
	}
	return strings.ReplaceAll(value, ",", " ")
}
