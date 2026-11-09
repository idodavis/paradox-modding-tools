// datacalls.go follows the data calls that GUI files and localization texts
// write, through the data types the dumps declare, and lists what a
// localization text refers to.

package schema

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// globalType is the data type a data call's first step is looked up on.
const globalType = "Global"

// anyScopeType is the data type of a scope of any type. Its getters give each
// type of scope its own data type, as Scope.GetCountry gives Country.
const anyScopeType = "Scope"

// ResolveCallTypes returns the type each step of a data call leads to. It
// stops at a step no dump declares, which gets "".
func (s *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string {
	_, functions := s.listCallOwners(call, datacontext)
	types := make([]string, len(functions))
	for i, function := range functions {
		types[i] = function.Returns
	}

	return types
}

// listCallOwners returns the data type that declares each step of a data
// call, and the function each step calls. It stops at a step no dump
// declares, which gets "" and an empty function. A first step that names a
// data type is owned by that type and leads to it, as a function returning it
// would, though it calls no function.
func (s *Schema) listCallOwners(call jomini.Call, datacontext string) ([]string, []DataFunction) {
	var owners []string
	var functions []DataFunction
	current := datacontext
	for i, step := range call.Steps {
		function, ok := s.DataTypes[current].Functions[step.Text]
		if !ok && i == 0 {
			current = globalType
			function, ok = s.DataTypes[current].Functions[step.Text]
			if _, named := s.DataTypes[step.Text]; !ok && named {
				function, ok, current = DataFunction{Returns: step.Text}, true, step.Text
			}
		}

		if !ok {
			return append(owners, ""), append(functions, DataFunction{})
		}

		owners, functions = append(owners, current), append(functions, function)
		current = function.Returns
	}

	return owners, functions
}

// FindFunction returns the data function one step of a data call calls. It
// returns false if the dumps declare none.
func (s *Schema) FindFunction(call jomini.Call, datacontext string, step int) (DataFunction, bool) {
	owners, _ := s.listCallOwners(call, datacontext)
	if step < 0 || step >= len(owners) || owners[step] == "" {
		return DataFunction{}, false
	}

	function, ok := s.DataTypes[owners[step]].Functions[call.Steps[step].Text]

	return function, ok
}

// ReturnType returns the name of the type a data function returns. It returns
// "" if the dumps do not name the type.
func ReturnType(function DataFunction) string {
	if function.Returns == dumpPhrases.Unnamed {
		return ""
	}

	return function.Returns
}

// FindUnlisted returns the position of the first step of a data call that the
// dumps do not declare, and the type before it, which for the first step is
// the data context's type, else Global. It returns false if the dumps declare
// every step, or do not name the type before the step.
func (s *Schema) FindUnlisted(call jomini.Call, datacontext string) (int, string, bool) {
	owners, functions := s.listCallOwners(call, datacontext)
	step := slices.Index(owners, "")
	if step < 0 {
		return 0, "", false
	}

	before := cmp.Or(datacontext, globalType)
	if step > 0 {
		before = ReturnType(functions[step-1])
	}

	return step, before, before != ""
}

// ResolveMacro returns a data call with its leading macros expanded. The data
// context's own functions are never macros. A macro that expands into itself
// gives an empty call.
func (s *Schema) ResolveMacro(call jomini.Call, datacontext string) jomini.Call {
	expanded := map[string]bool{}
	for len(call.Steps) > 0 {
		first := call.Steps[0]
		function := s.DataTypes[globalType].Functions[first.Text]
		_, own := s.DataTypes[datacontext].Functions[first.Text]
		expansion, ok := jomini.ParseCall(function.Expansion, 0)
		if own || !ok {
			return call
		}

		if expanded[first.Text] {
			return jomini.Call{}
		}

		expanded[first.Text] = true
		bindings := map[string]jomini.Argument{}
		for i, parameter := range function.Arguments {
			if i < len(first.Arguments) {
				bindings[parameter] = first.Arguments[i]
			}
		}

		next := jomini.SubstituteCall(expansion, bindings)
		next.Steps = append(next.Steps, call.Steps[1:]...)
		next.Span, next.Suffix = call.Span, call.Suffix
		call = next
	}

	return call
}

// ResolveDatacontext returns the data context type of a GUI block, given the
// call its datacontext key writes and the data context around it. It returns
// "" if the dumps do not resolve the call.
func (s *Schema) ResolveDatacontext(call jomini.Call, datacontext string) string {
	types := s.ResolveCallTypes(s.ResolveMacro(call, datacontext), datacontext)
	if len(types) == 0 {
		return ""
	}

	return types[len(types)-1]
}

// topScopeType is the data type the SCOPE global gives. Its promotes read the
// scope saved under a name, as SCOPE.sC('heir') does: data_types_script.txt
// :797-800 in CK3's logs, :54-57 in Vic3's and EU5's.
const topScopeType = "TopScope"

// ListCallReferences returns a reference for each quoted argument of a data
// call, calls inside its arguments included. Its database is the one
// game.GUI.CallTargets lists for the argument's position, or none, else the
// saved scopes for a promote of topScopeType that leads to a scope, else the
// step's type's.
func (s *Schema) ListCallReferences(call jomini.Call, datacontext string) []Reference {
	var references []Reference
	call = s.ResolveMacro(call, datacontext)
	owners, functions := s.listCallOwners(call, datacontext)
	for i, step := range call.Steps {
		targets, listed := game.GUI.CallTargets[step.Text]
		typed := ""
		switch {
		case i >= len(functions) || functions[i].Returns == "":
		case owners[i] == topScopeType && functions[i].Promote && s.isScopeType(functions[i].Returns):
			typed = game.SavedScopes
		default:
			typed, _ = s.ResolveTypeDatabase(functions[i].Returns)
		}

		for position, argument := range step.Arguments {
			database := typed
			if listed {
				database = ""
				if position < len(targets) {
					database = targets[position]
				}
			}

			if argument.Call != nil {
				references = append(references, s.ListCallReferences(*argument.Call, "")...)
			}

			// a nested definition's holder is the argument before it, as in
			// GetDefine('NAI', 'X')
			reference := Reference{Database: database, Name: argument.Text, Span: argument.Span}
			if listed && position > 0 {
				reference.Name = s.JoinNested(database, step.Arguments[position-1].Text, argument.Text)
			}

			// a position the row leaves empty names nothing
			if argument.Quoted && !argument.Span.IsEmpty() && (!listed || database != "") {
				references = append(references, reference)
			}
		}
	}

	return references
}

// isScopeType reports whether a data type is the type of a scope the dumps
// declare, as Character is for the getter Scope.GetCharacter.
func (s *Schema) isScopeType(dataType string) bool {
	for name, getter := range s.DataTypes[anyScopeType].Functions {
		if getter.Returns == dataType && s.isScope(strings.ReplaceAll(ToPhrase(DropGetter(name)), " ", "_")) {
			return true
		}
	}

	return false
}

// textDatabases maps the markup of a name that a localization text writes to
// the database that defines the name.
var textDatabases = map[loc.Markup]string{
	loc.MarkupKey:    game.Localization,
	loc.MarkupIcon:   game.GUI.TextIcons,
	loc.MarkupFormat: game.GUI.TextFormats,
}

// ListTextReferences returns what the text of one message refers to, in the
// order the text writes it. For each piece of the text, a data call's
// references come first, then the names the piece writes: a key refers to a
// message, an icon to a text icon and a part of a format to a text format.
func (s *Schema) ListTextReferences(message loc.Message) []Reference {
	var references []Reference
	for _, segment := range loc.Segments(message.Text, message.TextSpan.Start) {
		if segment.Markup == loc.MarkupCall {
			references = append(references, s.listTextCall(segment)...)
		}

		for _, piece := range loc.ListNames(segment) {
			reference := Reference{Database: textDatabases[piece.Markup], Name: piece.Text, Span: piece.TextSpan}
			references = append(references, reference)
		}
	}

	return references
}

// conceptFormat is the format a data call in a localization text carries
// after its bar when its one step is the name of a game concept, as in
// [faith|E]. It is written in either case.
const conceptFormat = "E"

// listTextCall returns the references of a data call in a localization text:
// those its steps make, then the game concept it names. Only a call of one
// step with no arguments and the concept format after its bar names one, as
// [faith|E] names the concept faith. It returns nil if the text is no call.
func (s *Schema) listTextCall(segment loc.Segment) []Reference {
	call, ok := jomini.ParseCall(segment.Text, segment.TextSpan.Start)
	if !ok {
		return nil
	}

	references := s.ListCallReferences(call, "")

	step := call.Steps[0]
	if len(call.Steps) == 1 && len(step.Arguments) == 0 && strings.EqualFold(call.Suffix, conceptFormat) {
		references = append(references, Reference{Database: s.Concepts, Name: step.Text, Span: step.Span})
	}

	return references
}

// DescribeStep returns one step of a data call in words, followed by its
// quoted arguments. The flag says whether to drop the getter from the step's
// name. A step that takes a name, as Custom('GetDaughterSon') does, reads as
// its last quoted argument without the getter: "daughter son".
func DescribeStep(step jomini.Step, reaches bool) string {
	var quoted []string
	for _, argument := range step.Arguments {
		if argument.Quoted && argument.Text != "" {
			quoted = append(quoted, argument.Text)
		}
	}

	if _, named := game.GUI.CallTargets[step.Text]; named && len(quoted) > 0 {
		return ToPhrase(DropGetter(quoted[len(quoted)-1]))
	}

	name := step.Text
	if reaches {
		name = DropGetter(name)
	}

	return strings.Join(append([]string{ToPhrase(name)}, quoted...), " ")
}

// DropGetter returns a function's name without the getter it starts with,
// such as FirstName for GetFirstName. A name that is only the getter, or where
// the getter is followed by a lower-case letter, is returned as it is.
func DropGetter(name string) string {
	for _, getter := range game.GUI.Getters {
		if rest, ok := strings.CutPrefix(name, getter); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
			return rest
		}
	}

	return name
}
