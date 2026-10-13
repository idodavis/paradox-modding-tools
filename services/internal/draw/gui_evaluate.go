// gui_evaluate.go answers a binding with no game running: a literal, a fact the user gave, the state a window opens
// in, a computed function over its arguments, else a stand-in that says it is not known. Every question it could not
// answer from the game's own rules is kept as a fact, in the order first asked.

package draw

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// maxMacroDepth is how many macros one answer may expand through.
const maxMacroDepth = 20

// answer is what the evaluator makes of a call: its kind (bool, number, text, list, object), the value for its kind,
// and whether the value is known.
type answer struct {
	kind   string
	yes    bool
	number float64
	text   string
	count  int
	known  bool
}

// macro is one data_binding entry: the parameters its definition names, and the text they replace it with.
type macro struct {
	parameters []string
	replace    string
}

// evaluator answers bindings over the data types, the facts, the computed rows and the macros, and keeps every fact
// asked. Its context and row are where the binding being answered is written.
type evaluator struct {
	schema  *schema.Schema
	facts   map[string]string
	asked   map[string]Fact
	order   []string // the keys of asked, in the order first asked
	touched []string // the keys of the facts the binding being answered asked, in order
	missing map[string]bool
	macro   func(name string) (macro, bool)
	loc     func(key string) (string, bool)
	context string // the data type the binding reads in; "" the global functions
	row     int    // the data model row the binding is drawn for; -1 for none
	depth   int    // how many macros the answer is inside
}

// newEvaluator is an evaluator over a workspace's data types and macros for a language's texts.
func newEvaluator(ws *workspace.Session, facts map[string]string, language string) *evaluator {
	macros := map[string]*macro{}
	parses := workspace.Parses{}
	return &evaluator{
		schema: ws.Install.Schema, facts: facts, asked: map[string]Fact{}, missing: map[string]bool{}, row: -1,
		macro: func(name string) (macro, bool) {
			if _, loaded := macros[name]; !loaded {
				macros[name] = macroOf(ws, parses, name)
			}
			if macros[name] == nil {
				return macro{}, false
			}
			return *macros[name], true
		},
		loc: func(key string) (string, bool) {
			entry, known := ws.Loc(key, language)
			return entry.Text(), known
		},
	}
}

// macroOf is the data_binding entry named for a macro: its definition's parameters and what it is replaced with;
// nil where none is.
func macroOf(ws *workspace.Session, parses workspace.Parses, name string) *macro {
	database := ws.Install.Schema.Functions
	if database == "" {
		return nil
	}
	for _, def := range index.Lookup(ws.Indexes(), schema.Defines, database, name) {
		parsed, _, err := ws.Parsed(parses, def.File().Path)
		if err != nil {
			continue
		}
		block := jomini.ChildBlock(parsed.AssignmentAt(int(def.Occurrence().Line), int(def.Occurrence().Col)))
		definition, replacement := jomini.ScalarOf(block, "definition"), jomini.ScalarOf(block, "replace_with")
		if definition == nil || replacement == nil {
			continue
		}
		_, inside, _ := strings.Cut(strings.TrimSuffix(definition.Text, ")"), "(")
		defined := &macro{replace: replacement.Text}
		for parameter := range strings.SplitSeq(inside, ",") {
			defined.parameters = append(defined.parameters, strings.TrimSpace(parameter))
		}
		return defined
	}
	return nil
}

// evaluate answers one call text, brackets stripped, at the evaluator's context and row; want is the kind the
// property it is read for takes, which a function the dumps do not type is read as. A literal answers itself, a
// macro is replaced by what it stands for, and any other call is answered by answerCall.
func (evaluator *evaluator) evaluate(call, want string) answer {
	call = strings.TrimSpace(call)
	if literal, isLiteral := literalOf(call); isLiteral {
		return literal
	}
	steps := jomini.ParseCall(call, 0)
	if len(steps) == 0 {
		return standIn("", cmp.Or(want, game.DataObject))
	}
	if replacement, isMacro := evaluator.expandMacro(steps[0]); isMacro && evaluator.depth < maxMacroDepth {
		evaluator.depth++
		defer func() { evaluator.depth-- }()
		return evaluator.evaluate(replacement, want)
	}
	return evaluator.answerCall(call, steps, want)
}

// answerCall answers a call whose last step names a function: one the user gave a fact for, one that reads what is
// unset as a window opens, a computed one, else a fact asked of the game. A step no type declares is unknown.
func (evaluator *evaluator) answerCall(call string, steps []jomini.CallStep, want string) answer {
	last := len(steps) - 1
	owner, function, known := evaluator.schema.CallStep(steps, last, evaluator.context)
	name := steps[last].Name
	if !known {
		return evaluator.unknown(call, steps, owner, want)
	}
	kind, id := kindOf(function.Returns, want), owner+"."+name
	if row, isComputed := computedRow(name, owner == game.GlobalType); isComputed && !slices.Contains(game.UnsetAtOpen, name) {
		return evaluator.computed(row, steps[last].Args, kind)
	}
	fact := evaluator.record(call, id, kind)
	switch {
	case fact.Known:
		return factAnswer(fact)
	case slices.Contains(game.UnsetAtOpen, name):
		return unsetAnswer(kind)
	}
	return standIn(name, kind)
}

// unknown answers a call whose last step names no function of the type the steps before it lead to: the fact the
// user gave for its text, else a stand-in; the call is kept under Missing unless the dumps gave an earlier step no
// type to follow it in.
func (evaluator *evaluator) unknown(call string, steps []jomini.CallStep, owner, want string) answer {
	if value, given := evaluator.facts[fold(call)]; given {
		return factAnswer(evaluator.record(call, "", valueKind(value)))
	}
	if owner != game.DumpWords.Unregistered {
		evaluator.missing["data function "+callText(steps)] = true
	}
	return standIn(steps[len(steps)-1].Name, cmp.Or(want, game.DataObject))
}

// record keeps the question a call asks, once, as the fact the user may answer: its answer is the fact given for the
// exact call, else the one given for its function.
func (evaluator *evaluator) record(call, function, kind string) Fact {
	key := fold(call)
	evaluator.touched = append(evaluator.touched, key)
	if fact, asked := evaluator.asked[key]; asked {
		return fact
	}
	value, known := evaluator.facts[key]
	if !known && function != "" {
		value, known = evaluator.facts[function]
	}
	fact := Fact{Call: key, Function: function, Returns: kind, Value: value, Known: known}
	evaluator.asked[key] = fact
	evaluator.order = append(evaluator.order, key)
	return fact
}

// literalOf reads a call text that is its own answer: yes, no, a cast, a quoted text or a number.
func literalOf(call string) (answer, bool) {
	switch strings.ToLower(call) {
	case yes, "true":
		return answer{kind: game.DataBool, yes: true, known: true}, true
	case no, "false":
		return answer{kind: game.DataBool, known: true}, true
	}
	if _, value, isCast := jomini.Cast(call); isCast {
		return valueAnswer(value), true
	}
	if text, isQuoted := unquote(call); isQuoted {
		if _, value, isCast := jomini.Cast(text); isCast {
			return valueAnswer(value), true
		}
		return answer{kind: game.DataText, text: text, known: true}, true
	}
	if jomini.Literal(call) == jomini.LitNumber {
		return valueAnswer(call), true
	}
	return answer{}, false
}

// valueAnswer is the answer a literal's value is: a number, a bool, else the text.
func valueAnswer(value string) answer {
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return answer{kind: game.DataNumber, number: number, known: true}
	}
	switch strings.ToLower(value) {
	case "true", yes:
		return answer{kind: game.DataBool, yes: true, known: true}
	case "false", no:
		return answer{kind: game.DataBool, known: true}
	}
	return answer{kind: game.DataText, text: value, known: true}
}

// valueKind is the kind a fact's written value reads as where nothing declares one.
func valueKind(value string) string {
	return valueAnswer(value).kind
}

// unquote is a text in single or double quotes without them.
func unquote(text string) (string, bool) {
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		return text[1 : len(text)-1], true
	}
	return text, false
}

// expandMacro is the text a macro call is replaced with, its arguments in place of its parameters; false where the
// step names no macro or passes the wrong number of arguments.
func (evaluator *evaluator) expandMacro(step jomini.CallStep) (string, bool) {
	defined, isMacro := evaluator.macro(step.Name)
	if !isMacro || len(step.Args) != len(defined.parameters) {
		return "", false
	}
	replacement := defined.replace
	for i, parameter := range defined.parameters {
		text := step.Args[i].Text
		if step.Args[i].Quoted {
			text = "'" + text + "'"
		}
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(parameter) + `\b`)
		replacement = pattern.ReplaceAllLiteralString(replacement, text)
	}
	return replacement, true
}

// unsetAnswer is the answer of a function that reads what is unset: no, or an empty text.
func unsetAnswer(kind string) answer {
	if kind == game.DataText {
		return answer{kind: game.DataText, known: true}
	}
	return answer{kind: game.DataBool, known: true}
}

// factAnswer is a known fact's answer: its value read as its kind.
func factAnswer(fact Fact) answer {
	switch fact.Returns {
	case game.DataBool:
		return answer{kind: game.DataBool, yes: strings.EqualFold(fact.Value, yes) || strings.EqualFold(fact.Value, "true"), known: true}
	case game.DataNumber:
		number, _ := strconv.ParseFloat(fact.Value, 64)
		return answer{kind: game.DataNumber, number: number, known: true}
	case game.DataList:
		count, _ := strconv.Atoi(fact.Value)
		return answer{kind: game.DataList, count: count, known: true}
	case game.DataObject:
		return answer{kind: game.DataObject, known: true}
	}
	return answer{kind: game.DataText, text: fact.Value, known: true}
}

// standIn is the answer for a function nobody answered, which says it is not known: a bool and a number unknown, a
// text the function's words, a list of three, an object that exists.
func standIn(function, kind string) answer {
	switch kind {
	case game.DataBool, game.DataNumber:
		return answer{kind: kind}
	case game.DataText:
		return answer{kind: kind, text: labelOf(function)}
	case game.DataList:
		return answer{kind: kind, count: 3}
	}
	return answer{kind: game.DataObject}
}

// kindOf is what a return type is: a primitive's by the table, a type the dumps do not name as the property reads
// it, any other type an object, which a data model reads as a list.
func kindOf(returns, want string) string {
	if kind, isPrimitive := game.DataKinds[returns]; isPrimitive {
		return kind
	}
	if returns == "" || returns == game.DumpWords.Unregistered {
		return cmp.Or(want, game.DataObject)
	}
	if want == game.DataList {
		return game.DataList
	}
	return game.DataObject
}

// labelOf is a function's name spaced into words with the Get dropped (GetName is Name, GetNumberOfKids Number Of Kids).
func labelOf(function string) string {
	function = strings.TrimPrefix(function, "Get")
	var parts []string
	start := 0
	for i, char := range function {
		if i > start && unicode.IsUpper(char) {
			parts, start = append(parts, function[start:i]), i
		}
	}
	return strings.Join(append(parts, function[start:]), " ")
}

// fold is a call text with its spaces taken out, the key of the fact it asks.
func fold(call string) string {
	return strings.Join(strings.Fields(call), "")
}

// callText is the steps of a call written as a dotted name.
func callText(steps []jomini.CallStep) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = step.Name
	}
	return strings.Join(names, ".")
}

// computedRow is the computed function a step's name is: an exact name first, then a prefix where the step is a
// global function.
func computedRow(name string, global bool) (game.ComputedFunction, bool) {
	for _, row := range game.ComputedFunctions {
		if !row.Prefix && row.Name == name {
			return row, true
		}
	}
	for _, row := range game.ComputedFunctions {
		if row.Prefix && global && strings.HasPrefix(name, row.Name) {
			return row, true
		}
	}
	return game.ComputedFunction{}, false
}

// computed is a computed function's answer over its arguments, each answered at the evaluator's context; kind is
// the kind the call is read as.
func (evaluator *evaluator) computed(row game.ComputedFunction, arguments []jomini.CallArg, kind string) answer {
	args := make([]answer, len(arguments))
	for i, argument := range arguments {
		text := argument.Text
		if argument.Quoted {
			text = "'" + text + "'"
		}
		args[i] = evaluator.evaluate(text, "")
	}
	switch row.Computation {
	case game.ComputeAnd, game.ComputeOr:
		return logic(args, row.Computation == game.ComputeAnd)
	case game.ComputeNot:
		return negate(args)
	case game.ComputeCompare:
		return compare(row.Name, args)
	case game.ComputeArithmetic:
		return arithmetic(row.Name, args)
	case game.ComputeParity:
		return parity(row.Name, args)
	case game.ComputeListCount:
		return listCount(row.Name, args)
	case game.ComputeTextEmpty:
		return answer{kind: game.DataBool, yes: len(args) == 1 && args[0].known && args[0].text == "", known: true}
	case game.ComputeValid:
		return answer{kind: game.DataBool, yes: true, known: true}
	case game.ComputeSelect:
		return choose(args)
	case game.ComputeLocalize:
		return evaluator.localize(args)
	case game.ComputeConcatenate:
		return concatenate(args)
	case game.ComputeConvert:
		return convert(args)
	}
	return evaluator.rowIndex()
}

// rowIndex is GetIndexInDataModel: the row the widget is drawn for, where it is drawn for one.
func (evaluator *evaluator) rowIndex() answer {
	if evaluator.row < 0 {
		return answer{kind: game.DataNumber}
	}
	return answer{kind: game.DataNumber, number: float64(evaluator.row), known: true}
}

// localize is the text of a localization key in the language, else english, else the key.
func (evaluator *evaluator) localize(args []answer) answer {
	if len(args) != 1 || !args[0].known {
		return answer{kind: game.DataText}
	}
	if text, localized := evaluator.loc(args[0].text); localized {
		return answer{kind: game.DataText, text: text, known: true}
	}
	return answer{kind: game.DataText, text: args[0].text}
}

// logic is And or Or over bool answers: decided by one known side that decides it, else by every side known.
func logic(args []answer, and bool) answer {
	decisive, all := false, len(args) > 0
	for _, arg := range args {
		decisive = decisive || arg.known && arg.yes != and
		all = all && arg.known
	}
	switch {
	case decisive:
		return answer{kind: game.DataBool, yes: !and, known: true}
	case all:
		return answer{kind: game.DataBool, yes: and, known: true}
	}
	return answer{kind: game.DataBool}
}

// negate is Not: the opposite of a known bool.
func negate(args []answer) answer {
	if len(args) == 1 && args[0].known {
		return answer{kind: game.DataBool, yes: !args[0].yes, known: true}
	}
	return answer{kind: game.DataBool}
}

// compare is a comparison of two known answers by the function's verb: numbers by order, texts by equality.
func compare(verb string, args []answer) answer {
	if len(args) != 2 || !args[0].known || !args[1].known {
		return answer{kind: game.DataBool}
	}
	var order int
	if args[0].kind == game.DataNumber && args[1].kind == game.DataNumber {
		order = cmp.Compare(args[0].number, args[1].number)
	} else {
		order = strings.Compare(args[0].text, args[1].text)
	}
	yes := order == 0
	switch strings.TrimSuffix(verb, "_") {
	case "GreaterThan":
		yes = order > 0
	case "GreaterThanOrEqualTo":
		yes = order >= 0
	case "LessThan":
		yes = order < 0
	case "LessThanOrEqualTo":
		yes = order <= 0
	case "NotEqualTo":
		yes = order != 0
	}
	return answer{kind: game.DataBool, yes: yes, known: true}
}

// arithmetic is two known numbers combined by the function's verb (one for Abs); unknown otherwise.
func arithmetic(verb string, args []answer) answer {
	unknown := answer{kind: game.DataNumber}
	for _, arg := range args {
		if !arg.known {
			return unknown
		}
	}
	verb = strings.TrimSuffix(verb, "_")
	switch {
	case verb == "Abs" && len(args) == 1:
		return answer{kind: game.DataNumber, number: max(args[0].number, -args[0].number), known: true}
	case len(args) != 2:
		return unknown
	}
	first, second := args[0].number, args[1].number
	number := 0.0
	switch verb {
	case "Add":
		number = first + second
	case "Subtract":
		number = first - second
	case "Multiply":
		number = first * second
	case "Divide":
		if second == 0 {
			return unknown
		}
		number = first / second
	case "Min":
		number = min(first, second)
	case "Max":
		number = max(first, second)
	default:
		return unknown
	}
	return answer{kind: game.DataNumber, number: number, known: true}
}

// parity is IsOdd or IsEven of a known number.
func parity(verb string, args []answer) answer {
	if len(args) != 1 || !args[0].known {
		return answer{kind: game.DataBool}
	}
	odd := int(args[0].number)%2 != 0
	return answer{kind: game.DataBool, yes: odd == strings.HasPrefix(verb, "IsOdd"), known: true}
}

// listCount is IsDataModelEmpty, DataModelHasItems and GetDataModelSize of a list: the first two are decided as a
// window opens with items, the size is the list's count where it is known.
func listCount(verb string, args []answer) answer {
	switch verb {
	case "IsDataModelEmpty":
		return answer{kind: game.DataBool, known: true}
	case "DataModelHasItems":
		return answer{kind: game.DataBool, yes: true, known: true}
	}
	if len(args) == 1 {
		return answer{kind: game.DataNumber, number: float64(args[0].count), known: args[0].known}
	}
	return answer{kind: game.DataNumber}
}

// choose is Select over a condition and two branches: the branch a known condition picks, else the first, unknown.
func choose(args []answer) answer {
	if len(args) != 3 {
		return answer{kind: game.DataText}
	}
	picked := args[1]
	switch {
	case args[0].known && !args[0].yes:
		picked = args[2]
	case !args[0].known:
		picked.known = false
	}
	return picked
}

// concatenate is the texts of the arguments joined; known where every one is.
func concatenate(args []answer) answer {
	joined := answer{kind: game.DataText, known: true}
	for _, arg := range args {
		joined.text += arg.text
		joined.known = joined.known && arg.known
	}
	return joined
}

// convert is FixedPointToFloat and the other conversions: the number itself.
func convert(args []answer) answer {
	if len(args) == 1 {
		return args[0]
	}
	return answer{kind: game.DataNumber}
}
