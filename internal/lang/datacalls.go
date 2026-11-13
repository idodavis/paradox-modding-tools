// datacalls.go follows the data calls written in localization and GUI text,
// as [ROOT.GetName], through the data types the dumps declare.

package lang

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// GlobalType is the data type on which the first step of a data call is
// looked up, where no data context is set.
const GlobalType = "Global"

// AnyScopeType is the data type of a scope of any type. Its functions lead to
// the data type of each scope type.
const AnyScopeType = "Scope"

// ResolveCallTypes returns the data type each step of a data call returns.
// A step no dump declares has "".
func (s *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string {
	_, functions := s.ListCallOwners(call, datacontext)
	types := make([]string, len(functions))
	for i, function := range functions {
		types[i] = function.Returns
	}

	return types
}

// ListCallOwners returns, for each step of a data call, the data type the
// step's function belongs to and the function itself. A first step may be
// the name of a data type, as Character in Character.GetName, and then
// belongs to that type. A step no dump declares gets "" and ends both lists.
func (s *Schema) ListCallOwners(call jomini.Call, datacontext string) ([]string, []DataFunction) {
	var owners []string
	var functions []DataFunction
	current := datacontext
	for i, step := range call.Steps {
		function, ok := s.DataTypes[current].Functions[step.Text]
		if !ok && i == 0 {
			current = GlobalType
			function, ok = s.DataTypes[current].Functions[step.Text]
			if _, isType := s.DataTypes[step.Text]; !ok && isType {
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
// returns false for a step no dump declares, and for a step the call lacks.
func (s *Schema) FindFunction(call jomini.Call, datacontext string, step int) (DataFunction, bool) {
	owners, _ := s.ListCallOwners(call, datacontext)
	if step < 0 || step >= len(owners) || owners[step] == "" {
		return DataFunction{}, false
	}

	function, ok := s.DataTypes[owners[step]].Functions[call.Steps[step].Text]

	return function, ok
}

// dumpUnnamed is what the dumps write as the return type of a function whose
// type they do not name.
const dumpUnnamed = "[unregistered]"

// ReturnType returns the data type a function returns. It returns "" where
// the dumps do not name it.
func ReturnType(function DataFunction) string {
	if function.Returns == dumpUnnamed {
		return ""
	}

	return function.Returns
}

// FindUnlisted returns the first step of a data call that no dump declares,
// and the data type the call had reached before it: the data context's, else
// the global type. It returns false if every step is declared, or if the
// dumps do not name the type before the step.
func (s *Schema) FindUnlisted(call jomini.Call, datacontext string) (int, string, bool) {
	owners, functions := s.ListCallOwners(call, datacontext)
	step := slices.Index(owners, "")
	if step < 0 {
		return 0, "", false
	}

	before := cmp.Or(datacontext, GlobalType)
	if step > 0 {
		before = ReturnType(functions[step-1])
	}

	return step, before, before != ""
}

// ResolveMacro replaces the macros a data call starts with by the calls they
// stand for, with the macro's arguments filled in. A first step that is a
// function of the data context is no macro and stays. A macro that leads back
// to itself gives an empty call.
func (s *Schema) ResolveMacro(call jomini.Call, datacontext string) jomini.Call {
	expanded := map[string]bool{}
	for len(call.Steps) > 0 {
		first := call.Steps[0]
		function := s.DataTypes[GlobalType].Functions[first.Text]
		_, isOwn := s.DataTypes[datacontext].Functions[first.Text]
		expansion, ok := jomini.ParseCall(function.Expansion, 0)
		if isOwn || !ok {
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

// ResolveDatacontext returns the data type that a GUI block's datacontext
// call sets for the calls inside the block. It returns "" where the dumps do
// not say what the call returns.
func (s *Schema) ResolveDatacontext(call jomini.Call, datacontext string) string {
	types := s.ResolveCallTypes(s.ResolveMacro(call, datacontext), datacontext)
	if len(types) == 0 {
		return ""
	}

	return types[len(types)-1]
}

// topScopeType is the data type of the global SCOPE. A function of it that
// leads to a scope reads a saved scope by name, as SCOPE.sC('heir') does
// (data_types_script.txt:797-800 in CK3, :54-57 in Vic3 and EU5).
const topScopeType = "TopScope"

// ListCallReferences returns a reference for each quoted argument of a data
// call, the arguments of calls inside it included. The database an argument
// names is the one GUICallTargets gives for its place. Without a row there it
// is the saved scopes for a function of SCOPE that leads to a scope, else the
// database that the function's return type stands for.
func (s *Schema) ListCallReferences(call jomini.Call, datacontext string) []Reference {
	var references []Reference
	call = s.ResolveMacro(call, datacontext)
	owners, functions := s.ListCallOwners(call, datacontext)
	for i, step := range call.Steps {
		targets, hasRow := GUICallTargets[step.Text]
		fallback := ""
		switch {
		case i >= len(functions) || functions[i].Returns == "":
		case owners[i] == topScopeType && functions[i].Promote && s.isScopeType(functions[i].Returns):
			fallback = SavedScopes
		default:
			fallback, _ = s.ResolveTypeDatabase(functions[i].Returns)
		}

		for position, argument := range step.Arguments {
			database := fallback
			if hasRow {
				database = ""
				if position < len(targets) {
					database = targets[position]
				}
			}

			if argument.Call != nil {
				references = append(references, s.ListCallReferences(*argument.Call, "")...)
			}

			// the definition that holds a nested one is the argument before
			// it, as in GetDefine('NAI', 'X')
			reference := Reference{Database: database, Name: argument.Text, Span: argument.Span}
			if hasRow && position > 0 {
				reference.Name = s.JoinNested(database, step.Arguments[position-1].Text, argument.Text)
			}

			// an argument whose place the row leaves empty names nothing
			if argument.Quoted && !argument.Span.IsEmpty() && (!hasRow || database != "") {
				references = append(references, reference)
			}
		}
	}

	return references
}

// isScopeType reports whether a data type is that of one of the scope types.
func (s *Schema) isScopeType(dataType string) bool {
	for name, getter := range s.DataTypes[AnyScopeType].Functions {
		if getter.Returns == dataType && s.IsScope(strings.ReplaceAll(ToPhrase(DropGetter(name)), " ", "_")) {
			return true
		}
	}

	return false
}

// textDatabases holds the database each kind of name in a text refers to: a
// $key$, an @icon! and a #format.
var textDatabases = map[loc.Markup]string{
	loc.MarkupKey:    Localization,
	loc.MarkupIcon:   GUITextIcons,
	loc.MarkupFormat: GUITextFormats,
}

// WalkMessages reads a localization file as the walk reads script. It returns
// one context for each message: the message's key is the definition, and the
// names its text writes are the references, those of a data call first.
func (s *Schema) WalkMessages(file *loc.File) []Context {
	contexts := make([]Context, len(file.Messages))
	for position, message := range file.Messages {
		context := &contexts[position]
		context.Category, context.Defines = CategoryDefinition, true
		context.Definition = &Copy{Database: Localization, Name: message.Key, Span: message.KeySpan}
		for _, segment := range message.Segments {
			if segment.Markup == loc.MarkupCall {
				context.References = append(context.References, s.listTextCall(segment)...)
			}

			for _, piece := range loc.ListNames(segment) {
				reference := Reference{Database: textDatabases[piece.Markup], Name: piece.Text, Span: piece.TextSpan}
				context.References = append(context.References, reference)
			}
		}

		judge(context.References)
	}

	return contexts
}

// conceptFormat is the format code that makes a data call the name of a game
// concept, as in [faith|E]. It is read in either letter case.
const conceptFormat = "E"

// listTextCall returns the references of a data call in a text: those of its
// arguments, then the game concept it names, if the call is one word with no
// arguments and the concept format. It returns none if the piece of text is
// no call.
func (s *Schema) listTextCall(segment loc.Segment) []Reference {
	call := segment.Call
	if call == nil {
		return nil
	}

	references := s.ListCallReferences(*call, "")

	step := call.Steps[0]
	if len(call.Steps) == 1 && len(step.Arguments) == 0 && strings.EqualFold(call.Suffix, conceptFormat) {
		references = append(references, Reference{Database: s.Concepts, Name: step.Text, Span: step.Span})
	}

	return references
}

// DescribeStep returns one step of a data call in words, followed by its
// quoted arguments. With dropsGetter the word Get or Access is left off the
// step's name. A step whose argument is the name of something, as
// Custom('GetDaughterSon') is, reads as that name: "daughter son".
func DescribeStep(step jomini.Step, dropsGetter bool) string {
	var quoted []string
	for _, argument := range step.Arguments {
		if argument.Quoted && argument.Text != "" {
			quoted = append(quoted, argument.Text)
		}
	}

	if _, hasRow := GUICallTargets[step.Text]; hasRow && len(quoted) > 0 {
		return ToPhrase(DropGetter(quoted[len(quoted)-1]))
	}

	name := step.Text
	if dropsGetter {
		name = DropGetter(name)
	}

	return strings.Join(append([]string{ToPhrase(name)}, quoted...), " ")
}

// DropGetter returns a function's name without the Get or Access it starts
// with, as FirstName for GetFirstName. A name with no capital letter after
// that word, as Getaway, is returned as it is.
func DropGetter(name string) string {
	for _, getter := range GUIGetters {
		if rest, ok := strings.CutPrefix(name, getter); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
			return rest
		}
	}

	return name
}
