// choices.go works out the values that the picks give a window's properties: the one of two values that a condition
// chooses, with the text or the image file that value leads to.

package draw

import (
	"cmp"
	"path"
	"path/filepath"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// picturesSubject is what the group of a list of pictures asks about.
const picturesSubject = "Pictures"

// A Choice is the value that the picks give one property of a widget, with the text or the image file it leads to
// where its key writes a text or names a picture.
type Choice struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Text    string `json:"text,omitempty"`
	Texture string `json:"texture,omitempty"`
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

// ListPictures lists the pictures that a picture binding may draw, as a widget's Pictures says where: each image that
// the named definition writes, labeled by its file, or, for a list with no name, what the binding shows of each
// definition of the database, labeled by the definition. It returns an error if no workspace is open.
func (drw *Service) ListPictures(list PictureList) ([]Picture, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
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
// definitions are, which every widget reading that kind of object shares. Each is at the value picked for it in the
// given picks, captioned by its file or its definition, else at "", which draws what the list shows.
func listPictureGroups(window Window, picks map[string]string) []Group {
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
			group := Group{ID: id, Subject: picturesSubject, Label: label, Picked: picks[id], Pictures: &list}
			if group.Picked != "" {
				group.Captions = map[string]string{group.Picked: toFileLabel(group.Picked)}
			}
			groups = append(groups, group)
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

// listChoices returns, by widget, the values that the evaluator's values give the properties written as a choice
// between two values. A property whose condition is unknown chooses nothing and is left out, and so is a widget with
// no choice.
func (evaluator *evaluator) listChoices(library *library) map[int32][]Choice {
	choices := map[int32][]Choice{}
	for _, widget := range evaluator.window.Widgets {
		for _, choice := range evaluator.listWidgetChoices(widget.ID) {
			choice.Text, choice.Texture = library.resolveValue(choice.Key, choice.Value)
			choices[widget.ID] = append(choices[widget.ID], choice)
		}
	}
	return choices
}

// listWidgetChoices returns the values that the evaluator's values give one widget's properties written as a choice
// between two values, without the texts or files they lead to. A property whose condition is unknown is left out.
func (evaluator *evaluator) listWidgetChoices(widget int32) []Choice {
	evaluator.context = evaluator.contexts[widget]
	return lo.FilterMap(evaluator.window.Widgets[widget].Properties, func(property Property, _ int) (Choice, bool) {
		call, isCall := jomini.ParseBracketed(property.Value, 0)
		value, ok := evaluator.choose(call)
		return Choice{Key: property.Key, Value: value}, isCall && ok
	})
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

// listChoiceConditions returns the conditions that a widget's choices between two values are made by, those of a choice
// nested in another's argument too.
func listChoiceConditions(widget Widget) []jomini.Call {
	return lo.FlatMap(widget.Properties, func(property Property, _ int) []jomini.Call {
		call, ok := jomini.ParseBracketed(property.Value, 0)
		return lo.Ternary(ok, listSelectConditions(call), nil)
	})
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
