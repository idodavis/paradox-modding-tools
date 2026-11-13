// choices.go works out the values the picks give a window's properties, and the pictures and rows its lists draw.

package draw

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// picturesSubject is what the group of a list of pictures asks about.
const picturesSubject = "Pictures"

// A Choice is the value the picks give one property of a widget, with the text, image file or CSS color it leads to.
type Choice struct {
	Key     string        `json:"key"`
	Value   string        `json:"value"`
	Text    []loc.Segment `json:"text,omitempty"`
	Texture string        `json:"texture,omitempty"`
	Color   string        `json:"color,omitempty"`
}

// A Row is one row a data model's list draws for one definition of the database its widgets read, with the values the
// definition gives the properties of the widgets in the row, by widget.
type Row struct {
	Definition string             `json:"definition"`
	Choices    map[int32][]Choice `json:"choices"`
}

// A PictureList says where the pictures a picture binding may draw are listed. With a Name it lists the images one
// definition writes. With no Name it lists what the binding shows of each definition of a database.
type PictureList struct {
	Key      string `json:"key"` // the key of the property whose binding draws them
	Database string `json:"database"`
	Name     string `json:"name"`
	Type     string `json:"type"`     // the kind of object the binding reads, as its data type or its words; "" for none
	Field    string `json:"field"`    // the field it reads of one; "" for none
	Argument string `json:"argument"` // the argument it passes, as written; "" for none
	Folder   string `json:"folder"`   // for a kind whose objects are the subfolders of its icon folder rather than definitions, that folder; each subfolder holds the object's images, named by the argument
	Shown    string `json:"shown"`    // what it draws with nothing picked: a definition's or a folder's name, or a file
}

// A Picture is one picture a picture binding may draw, labeled by its file or its definition. Picking it sets its file,
// or its definition's name.
type Picture struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

// tierFrames is how a game draws the tier of a title from a sheet of cells, a
// cell for each tier.
type tierFrames struct {
	// Function is the data function that gives a title's cell, counting from
	// one, and Tiers the tiers in the order of the cells.
	Function string
	Tiers    []string
}

// titleTiers is how each game draws a title's tier from a sheet of cells, by
// the game's id. A game with no row draws none.
var titleTiers = map[string]tierFrames{
	// seven tiers: cell 3 for a count, 4 for a duke, from 5 for a king
	// or an emperor
	game.CK3: {
		Function: "GetTierFrame", Tiers: []string{"unlanded", "baron", "count", "duke", "king", "emperor", "hegemon"},
	},
}

// ListPictures lists the pictures a picture binding of a widget in the given file may draw, as the given list says. It
// returns an error if no workspace is open.
func (drw *Service) ListPictures(path string, list PictureList) ([]Picture, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := resolveIndex(ws, path)
	if list.Name == "" {
		library := makeLibrary(ws, mods)
		pictures := []Picture{}
		for _, reference := range library.listObjects(list) {
			if file := library.findShown(list, reference); file != "" {
				pictures = append(pictures, Picture{Label: reference.Name, Path: file, Value: reference.Name})
			}
		}
		return lo.UniqBy(pictures, func(picture Picture) string { return picture.Label }), nil
	}
	definition, _ := findKeptClause(ws, mods, lang.Reference{Database: list.Database, Name: list.Name})
	return lo.Map(listDefinitionImages(ws, mods, definition), func(file string, _ int) Picture {
		name := filepath.Base(file)
		return Picture{Label: strings.TrimSuffix(name, filepath.Ext(name)), Path: file, Value: file}
	}), nil
}

// listPictureGroups returns a group for each list of pictures the window's widgets draw from, with those widgets as its
// readers. Every widget reading the same kind of object shares a group.
func listPictureGroups(window Window) []Group {
	var groups table[string, Group]
	for _, widget := range window.Widgets {
		if widget.Pictures == nil {
			continue
		}
		list := *widget.Pictures
		id := strings.TrimSuffix(cmp.Or(list.Database, list.Folder)+":"+list.Name, ":")
		position := groups.add(id, func() Group {
			label := cmp.Or(lang.ToPhrase(list.Name), lang.DescribeDefinition(list.Database, false), list.Type)
			return Group{ID: id, Subject: picturesSubject, Label: label, Pictures: &list}
		})
		group := &groups.values[position]
		group.Readers = append(group.Readers, widget.ID)
		group.Reach = len(group.Readers)
	}
	return groups.values
}

// listTierGroups returns a group for each data call of the window that gives the cell of a title's tier in a sheet of
// cells (titleTiers), with the widgets that write it as its readers. Its options are the game's tiers.
func (evaluator *evaluator) listTierGroups(tiers tierFrames) []Group {
	var groups table[string, Group]
	for _, widget := range evaluator.window.Widgets {
		context := evaluator.contexts[widget.ID]
		for property := range evaluator.window.listProperties(widget) {
			call, ok := findTierCall(tiers, property)
			if !ok {
				continue
			}
			id := toCallText(context.expand(evaluator.schema, call))
			position := groups.add(id, func() Group {
				group := makeGroup(id)
				group.Options = tiers.Tiers
				return group
			})
			group := &groups.values[position]
			group.Readers, group.Reach = append(group.Readers, widget.ID), len(group.Readers)+1
		}
	}
	return groups.values
}

// pickTiers adds to the given choices the cells the picked tiers give the readers of the given tier groups, counted
// from one. A group no pick sets draws its cell as written.
func (evaluator *evaluator) pickTiers(tiers tierFrames, groups []Group, picks map[string]string, choices map[int32][]Choice) {
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

// findTierCall returns the data call a property writes where its last step is the function that gives a title's tier
// its cell. The last result is false for any other property.
func findTierCall(tiers tierFrames, property Property) (jomini.Call, bool) {
	if tiers.Function == "" || !strings.Contains(property.Value, tiers.Function) {
		return jomini.Call{}, false
	}
	call, ok := jomini.ParseBracketed(property.Value, 0)
	return call, ok && call.Steps[len(call.Steps)-1].Text == tiers.Function
}

// pickColors adds to the given choices the colors the picked definitions give the window's bindings of a definition's
// color, as they give the pictures of that kind. A binding with no pick, or whose picked definition writes no color,
// keeps its color.
func pickColors(opened opening, picks map[string]string, choices map[int32][]Choice) {
	definitions := map[string]string{} // the definition picked, by the database the group picks among
	for _, group := range opened.pictures {
		if picked := picks[group.ID]; picked != "" && group.Pictures.Name == "" && group.Pictures.Database != "" {
			definitions[group.Pictures.Database] = picked
		}
	}
	for id, picked := range opened.library.pickDefinitions(opened.evaluator.window.Widgets, opened.colorLists, definitions) {
		choices[id] = append(choices[id], picked...)
	}
}

// listRows returns, by the item of a data model in a window as it opens, the rows its list draws: one for each of the
// first definitions of the item's database, up to the number of rows the window opens with. An item that reads no
// database is left out, and its list draws the page's sample rows.
func listRows(opened opening) map[int32][]Row {
	library, window, contexts := opened.library, &opened.evaluator.window, opened.evaluator.contexts
	items := listItems(window.Widgets)
	rows := map[int32][]Row{}
	for item, members := range lo.GroupBy(window.Widgets, func(widget Widget) int32 { return items[widget.ID] }) {
		if item == 0 {
			continue
		}
		lists, database := map[int32][]PictureList{}, ""
		for _, widget := range members {
			lists[widget.ID] = listDatabaseLists(widget, opened.colorLists[widget.ID])
			for _, list := range lists[widget.ID] {
				if library.readsElement(window, widget, list, contexts[widget.ID]) {
					database = cmp.Or(database, list.Database)
					break
				}
			}
		}
		if database != "" {
			names := lo.Uniq(lo.Map(library.mods.ListDefinitions(database), func(site index.Site, _ int) string {
				return library.mods.ToReference(site).Name
			}))
			rows[item] = lo.Map(names[:min(opened.evaluator.rows, len(names))], func(name string, _ int) Row {
				choices := library.pickDefinitions(window.Widgets, lists, map[string]string{database: name})
				return Row{Definition: name, Choices: choices}
			})
		}
	}
	return rows
}

// readsElement reports whether a widget's binding under a list's key, read in the given data context, reads its object
// as an element of a data model: it starts at the object's type, which no data context reaches.
func (library *library) readsElement(window *Window, widget Widget, list PictureList, context context) bool {
	call, _ := jomini.ParseBracketed(findProperty(window.listProperties(widget), list.Key).Value, 0)
	path := context.expand(library.ws.Schema, call)
	return len(path.Steps) > 1 && path.Steps[0].Text == list.Type
}

// listDatabaseLists returns the lists of one widget's bindings that read the definitions of a database: its picture
// binding's, then the given lists of its color bindings.
func listDatabaseLists(widget Widget, colorLists []PictureList) []PictureList {
	lists := colorLists
	if widget.Pictures != nil {
		lists = slices.Concat([]PictureList{*widget.Pictures}, colorLists)
	}
	return lo.Filter(lists, func(list PictureList, _ int) bool {
		return list.Database != "" && list.Name == "" && list.Folder == ""
	})
}

// pickDefinitions returns, by widget, the values the given definitions, by the database each is picked among, give the
// properties of the given widgets through the given lists. A list of a database with no definition gives nothing.
func (library *library) pickDefinitions(widgets []Widget, lists map[int32][]PictureList,
	definitions map[string]string) map[int32][]Choice {
	choices := map[int32][]Choice{}
	for id, widgetLists := range lists {
		for _, list := range widgetLists {
			definition, ok := definitions[list.Database]
			if !ok {
				continue
			}
			if choice, ok := library.pickDefinition(widgets[id], list, definition); ok {
				choices[id] = append(choices[id], choice)
			}
		}
	}
	return choices
}

// pickDefinition returns the value picking a definition gives the property a list is read under, its color or its
// picture. It returns false for a color the definition does not write.
func (library *library) pickDefinition(widget Widget, list PictureList, definition string) (Choice, bool) {
	if _, isColor := widget.Colors[list.Key]; isColor {
		color := library.findFieldColor(list, lang.Reference{Database: list.Database, Name: definition})
		return Choice{Key: list.Key, Value: definition, Color: color}, color != ""
	}
	return library.pickPicture(list, definition), true
}

// listChoices returns, by widget, the values the evaluator's values give the properties written as a choice between
// two values, with the texts and files they lead to. A property whose condition is unknown is left out.
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

// listWidgetChoices returns the values the evaluator's values give one widget's properties written as a choice between
// two values, without the texts or files they lead to.
func (evaluator *evaluator) listWidgetChoices(widget int32) []Choice {
	context := evaluator.contexts[widget]
	var choices []Choice
	for property := range evaluator.window.listProperties(evaluator.window.Widgets[widget]) {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		if value, ok := evaluator.choose(context, call); isCall && ok {
			choices = append(choices, Choice{Key: property.Key, Value: value})
		}
	}
	return choices
}

// choose returns the value a choice between two values, read in the given context, gives under the evaluator's values.
// The last result is false if the call is no such choice, its condition is unknown, or the value it chooses is another
// data call.
func (evaluator *evaluator) choose(context context, call jomini.Call) (string, bool) {
	arguments, ok := splitSelect(call)
	if !ok || arguments[0].Call == nil {
		return "", false
	}
	holds, known := evaluator.evaluateCall(context, *arguments[0].Call)
	if !known {
		return "", false
	}
	branch := arguments[lo.Ternary(holds, 1, 2)]
	switch {
	case branch.Call != nil:
		return evaluator.choose(context, *branch.Call)
	case branch.Quoted:
		return toWrittenValue(branch.Text), true
	}
	return branch.Text, true
}

// listSelectConditions returns the condition of a choice between two values, then those of the choices nested in it.
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

// selectPrefix is the start of the data functions that give their second
// argument if their first holds, else their third, as Select_float.
//
// the dumps' global functions Select_float( Arg0, Arg1, Arg2 ) and the
// rest
const selectPrefix = "Select_"

// splitSelect returns the three arguments of a call that chooses between two values: its condition and the values. The
// last result is false if the call is no such choice.
func splitSelect(call jomini.Call) ([]jomini.Argument, bool) {
	if len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 3 || !strings.HasPrefix(call.Steps[0].Text, selectPrefix) {
		return nil, false
	}
	return call.Steps[0].Arguments, true
}

// toWrittenValue converts a quoted argument to the value as a GUI file writes it: '(CVector2f)0,140' is 0 140.
func toWrittenValue(text string) string {
	typed, isTyped := strings.CutPrefix(text, "(")
	_, value, hasType := strings.Cut(typed, ")")
	if !isTyped || !hasType {
		return text
	}
	return strings.ReplaceAll(value, ",", " ")
}
