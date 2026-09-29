// file.go writes a Schema as script text and reads it back with the engine's own parser. One
// assignment per line, kinds and members in name order, and only plain word lists on one line,
// so a learner change diffs as the lines that changed. Read(Write(s)) gives s back. The first
// block is the header, the second the drift report, the last the arguments, and every block
// between is a kind; they are found by position, so a kind may carry any name. A file this
// reader does not understand is an error, never a guess: the caller scans again.

package schema

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/source"
)

// fileReader reads a schema file's tree and keeps the first thing it did not understand.
type fileReader struct {
	lines *jomini.LineIndex
	err   error
}

// driftLabels names the sections of the drift block, in the order of Drift's fields.
var driftLabels = []string{
	"parse_errors", "extensions", "folders", "entry_modes", "prefixes", "dump_labels", "doc_lines", "name_tokens",
	"unclassified",
}

// Write is the schema as text.
func Write(schema *Schema) string {
	var out strings.Builder
	fmt.Fprintf(&out, "schema = {\n\tgame = %s\n\tversion = %s\n}\n", word(schema.Game), word(schema.Version))

	// the drift report: a section only where something was seen
	out.WriteString("drift = {\n")
	for i, tallies := range schema.Drift.sections() {
		if len(*tallies) == 0 {
			continue
		}
		fmt.Fprintf(&out, "\t%s = {\n", driftLabels[i])
		for _, entry := range *tallies {
			fmt.Fprintf(&out, "\t\t%s = %d\n", word(entry.Name), entry.Count)
		}
		out.WriteString("\t}\n")
	}
	out.WriteString("}\n")

	for _, name := range slices.Sorted(maps.Keys(schema.Kinds)) {
		writeKind(&out, schema.Kinds[name])
	}

	out.WriteString("arguments = {\n")
	for _, name := range slices.Sorted(maps.Keys(schema.Arguments)) {
		fmt.Fprintf(&out, "\t%s = {\n", word(name))
		writeField(&out, 2, schema.Arguments[name])
		out.WriteString("\t}\n")
	}
	out.WriteString("}\n")
	return out.String()
}

// sections is the tallies of a drift report, in the order of driftLabels.
func (drift *Drift) sections() []*[]Tally {
	return []*[]Tally{
		&drift.ParseErrors, &drift.Extensions, &drift.Folders, &drift.EntryModes, &drift.Prefixes, &drift.DumpLabels,
		&drift.DocLines, &drift.NameTokens, &drift.Unclassified,
	}
}

// writeKind writes one kind: its facts in a fixed order, each only when set, then its shape.
func writeKind(out *strings.Builder, kind *Kind) {
	fmt.Fprintf(out, "%s = {\n", word(kind.Name))
	if kind.Format != source.Script {
		fmt.Fprintf(out, "\tformat = %s\n", kind.Format)
	}
	if len(kind.Folders) > 0 {
		fmt.Fprintf(out, "\tfolders = %s\n", list(kind.Folders))
	}

	fmt.Fprintf(out, "\tnaming = {\n\t\tform = %s\n", word(kind.Naming.Form))
	if kind.Naming.Keyword != "" {
		fmt.Fprintf(out, "\t\tkeyword = %s\n", word(kind.Naming.Keyword))
	}
	if kind.Naming.Member != "" {
		fmt.Fprintf(out, "\t\tmember = %s\n", word(kind.Naming.Member))
	}
	if kind.Naming.Form == "column" {
		fmt.Fprintf(out, "\t\tcolumn = %d\n", kind.Naming.Column)
	}
	out.WriteString("\t}\n")

	for _, fact := range [][2]string{{"parent", kind.Parent}, {"subject", kind.Subject}} {
		if fact[1] != "" {
			fmt.Fprintf(out, "\t%s = %s\n", fact[0], word(fact[1]))
		}
	}
	if len(kind.Prefixes) > 0 {
		fmt.Fprintf(out, "\tprefixes = %s\n", list(kind.Prefixes))
	}
	for _, fact := range [][2]string{{"scope_type", kind.ScopeType}, {"label_member", kind.LabelMember}} {
		if fact[1] != "" {
			fmt.Fprintf(out, "\t%s = %s\n", fact[0], word(fact[1]))
		}
	}

	// the templates, each with its coverage
	if len(kind.LocTemplates) > 0 {
		out.WriteString("\tloc = {\n")
		for _, locTemplate := range kind.LocTemplates {
			coverage := strconv.FormatFloat(locTemplate.Coverage, 'f', -1, 64)
			fmt.Fprintf(out, "\t\t%s = %s\n", word(string(locTemplate.Template)), coverage)
		}
		out.WriteString("\t}\n")
	}
	if len(kind.FileTemplates) > 0 {
		out.WriteString("\tfiles = {\n")
		for _, fileTemplate := range kind.FileTemplates {
			coverage := strconv.FormatFloat(fileTemplate.Coverage, 'f', -1, 64)
			fmt.Fprintf(out, "\t\t%s = %s\n", word(fileTemplate.Pattern), coverage)
		}
		out.WriteString("\t}\n")
	}

	if kind.Macro != 0 {
		fmt.Fprintf(out, "\tmacro = %s\n", list(kind.Macro.words()))
	}
	if kind.Params {
		out.WriteString("\tparams = yes\n")
	}
	if kind.Shape != nil {
		out.WriteString("\tshape = {\n")
		writeField(out, 2, kind.Shape)
		out.WriteString("\t}\n")
	}
	out.WriteString("}\n")
}

// writeField writes a field's body, depth tabs in: the count, each set fact, the members by name.
func writeField(out *strings.Builder, depth int, field *Field) {
	tabs := strings.Repeat("\t", depth)
	fmt.Fprintf(out, "%scount = %d\n", tabs, field.Count)
	if field.Evidence != Observed {
		fmt.Fprintf(out, "%sevidence = %s\n", tabs, field.Evidence)
	}
	if field.Doc != "" {
		fmt.Fprintf(out, "%sdoc = %s\n", tabs, text(field.Doc))
	}
	if field.Default != "" {
		fmt.Fprintf(out, "%sdefault = %s\n", tabs, text(field.Default))
	}
	if field.Need.Level != NeedUnstated {
		fmt.Fprintf(out, "%sneed = %s\n%sneed_evidence = %s\n", tabs, field.Need.Level, tabs, field.Need.Evidence)
	}
	if field.Need.When != "" {
		fmt.Fprintf(out, "%sneed_when = %s\n", tabs, text(field.Need.When))
	}
	if field.Class != ClassNone {
		fmt.Fprintf(out, "%sclass = %s\n", tabs, field.Class)
	}
	if field.Body != 0 {
		fmt.Fprintf(out, "%sbody = %s\n", tabs, list(field.Body.words()))
	}
	if field.BodyScope != "" {
		fmt.Fprintf(out, "%sbody_scope = %s\n", tabs, word(field.BodyScope))
	}

	// every accept with its count, in order; the declared and the inferred ones are named again,
	// each in one list by its evidence
	if len(field.Accepts) > 0 {
		byEvidence := map[Evidence][]string{}
		fmt.Fprintf(out, "%saccepts = {\n", tabs)
		for _, accept := range field.Accepts {
			byEvidence[accept.Evidence] = append(byEvidence[accept.Evidence], accept.name())
			if accept.Type == TypeEnum {
				fmt.Fprintf(out, "%s\tenum = {\n%s\t\tcount = %d\n", tabs, tabs, accept.Count)
				fmt.Fprintf(out, "%s\t\twords = %s\n%s\t}\n", tabs, list(accept.Enum), tabs)
				continue
			}
			fmt.Fprintf(out, "%s\t%s = %d\n", tabs, word(accept.name()), accept.Count)
		}
		fmt.Fprintf(out, "%s}\n", tabs)
		for _, evidence := range []Evidence{Declared, Inferred} {
			if len(byEvidence[evidence]) > 0 {
				fmt.Fprintf(out, "%s%s = %s\n", tabs, evidence, list(byEvidence[evidence]))
			}
		}
	}

	if len(field.Ops) > 0 {
		fmt.Fprintf(out, "%sops = %s\n", tabs, list(field.Ops))
	}
	if field.Defines != "" {
		fmt.Fprintf(out, "%sdefines = %s\n", tabs, word(field.Defines))
	}
	if field.KeyedBy != "" {
		fmt.Fprintf(out, "%skeyed_by = %s\n", tabs, word(field.KeyedBy))
	}
	if len(field.Members) > 0 {
		fmt.Fprintf(out, "%smembers = {\n", tabs)
		for _, name := range slices.Sorted(maps.Keys(field.Members)) {
			fmt.Fprintf(out, "%s\t%s = {\n", tabs, word(name))
			writeField(out, depth+2, field.Members[name])
			fmt.Fprintf(out, "%s\t}\n", tabs)
		}
		fmt.Fprintf(out, "%s}\n", tabs)
	}
}

// name is how the schema file names an accept: its type, or ref: and the kind it names.
func (accept Accept) name() string {
	if accept.Type == TypeRef {
		return "ref:" + accept.Kind
	}
	return accept.Type.String()
}

// words is the names of the classes set in a body class.
func (bodyClass BodyClass) words() []string {
	var out []string
	for i, name := range bodyNames {
		if bodyClass&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// word writes one word so the lexer reads it back: bare when only letters, digits and _ . : / - |,
// quoted otherwise. A name never holds a double quote or a line break; the lexer cannot make one.
func word(text string) string {
	quoted := text == "" || strings.ContainsFunc(text, func(char rune) bool {
		return char < 128 && !unicode.IsLetter(char) && !unicode.IsDigit(char) && !strings.ContainsRune("_.:/-|", char)
	})
	if quoted {
		return `"` + text + `"`
	}
	return text
}

// text writes prose so the lexer reads it back whole: quoted, its quotes, backslashes and line
// breaks escaped.
func text(prose string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(prose) + `"`
}

// list writes words as one line: { a b "c d" }.
func list(words []string) string {
	var out strings.Builder
	out.WriteString("{")
	for _, text := range words {
		out.WriteByte(' ')
		out.WriteString(word(text))
	}
	out.WriteString(" }")
	return out.String()
}

// Read is the schema a text holds.
func Read(text string) (*Schema, error) {
	parsed := jomini.Parse(text)
	fileReader := &fileReader{lines: parsed.Lines()}
	if len(parsed.Errors) > 0 {
		line := fileReader.lines.PositionAt(parsed.Errors[0].Range.Start).Line + 1
		return nil, fmt.Errorf("schema file line %d: %s", line, parsed.Errors[0].Message)
	}

	// the header, drift and arguments blocks by position; every block between is a kind
	stmts := parsed.Root.Statements
	if len(stmts) < 3 {
		return nil, errors.New("schema file: the header, drift and arguments blocks are not all there")
	}
	schema := &Schema{Kinds: map[string]*Kind{}, Arguments: map[string]*Field{}}
	fileReader.each(fileReader.named(stmts[0], "schema"), func(key string, value jomini.Value) {
		switch key {
		case "game":
			schema.Game = fileReader.scalar(value)
		case "version":
			schema.Version = fileReader.scalar(value)
		default:
			fileReader.fail(value, "unknown header key %q", key)
		}
	})

	sections := schema.Drift.sections()
	fileReader.each(fileReader.named(stmts[1], "drift"), func(label string, value jomini.Value) {
		i := slices.Index(driftLabels, label)
		if i < 0 {
			fileReader.fail(value, "unknown drift section %q", label)
			return
		}
		fileReader.each(value, func(name string, count jomini.Value) {
			*sections[i] = append(*sections[i], Tally{Name: name, Count: fileReader.int(count)})
		})
	})

	for _, st := range stmts[2 : len(stmts)-1] {
		if assignment, ok := st.(*jomini.Assignment); ok {
			schema.Kinds[assignment.Key.Text] = fileReader.kind(assignment.Key.Text, assignment.Value)
		} else {
			fileReader.fail(st, "a kind is written name = { }")
		}
	}
	fileReader.each(fileReader.named(stmts[len(stmts)-1], "arguments"), func(name string, value jomini.Value) {
		schema.Arguments[name] = fileReader.field(name, value)
	})
	return schema, fileReader.err
}

// kind reads one kind block.
func (fileReader *fileReader) kind(name string, block jomini.Value) *Kind {
	kind := &Kind{Name: name, Format: source.Script}
	fileReader.each(block, func(key string, value jomini.Value) {
		switch key {
		case "format":
			kind.Format = source.Unknown
			for format := source.Unknown; format <= source.Opaque; format++ {
				if format.String() == fileReader.scalar(value) {
					kind.Format = format
				}
			}
		case "folders":
			kind.Folders = fileReader.words(value)
		case "naming":
			fileReader.each(value, func(key string, value jomini.Value) {
				switch key {
				case "form":
					if kind.Naming.Form = fileReader.scalar(value); !slices.Contains(namingForms, kind.Naming.Form) {
						fileReader.fail(value, "unknown naming form %q", kind.Naming.Form)
					}
				case "keyword":
					kind.Naming.Keyword = fileReader.scalar(value)
				case "member":
					kind.Naming.Member = fileReader.scalar(value)
				case "column":
					kind.Naming.Column = fileReader.int(value)
				default:
					fileReader.fail(value, "unknown naming key %q", key)
				}
			})
		case "parent":
			kind.Parent = fileReader.scalar(value)
		case "subject":
			kind.Subject = fileReader.scalar(value)
		case "prefixes":
			kind.Prefixes = fileReader.words(value)
		case "scope_type":
			kind.ScopeType = fileReader.scalar(value)
		case "label_member":
			kind.LabelMember = fileReader.scalar(value)
		case "loc":
			fileReader.each(value, func(template string, value jomini.Value) {
				convention := loc.Convention{Template: loc.Template(template), Coverage: fileReader.float(value)}
				kind.LocTemplates = append(kind.LocTemplates, convention)
			})
		case "files":
			fileReader.each(value, func(pattern string, value jomini.Value) {
				fileTemplate := FileTemplate{Pattern: pattern, Coverage: fileReader.float(value)}
				kind.FileTemplates = append(kind.FileTemplates, fileTemplate)
			})
		case "macro":
			kind.Macro = fileReader.bodyClass(value)
		case "params":
			kind.Params = fileReader.scalar(value) == "yes"
		case "shape":
			kind.Shape = fileReader.field("", value)
		default:
			fileReader.fail(value, "unknown kind key %q", key)
		}
	})
	return kind
}

// field reads the body of one field, and its members below it.
func (fileReader *fileReader) field(name string, block jomini.Value) *Field {
	field := &Field{Name: name}
	byEvidence := map[Evidence][]string{}
	fileReader.each(block, func(key string, value jomini.Value) {
		switch key {
		case "count":
			field.Count = fileReader.int(value)
		case "evidence":
			if i := slices.Index(evidenceNames[:], fileReader.scalar(value)); i > 0 {
				field.Evidence = Evidence(i)
			} else {
				fileReader.fail(value, "unknown evidence %q", fileReader.scalar(value))
			}
		case "doc":
			field.Doc = fileReader.text(value)
		case "default":
			field.Default = fileReader.text(value)
		case "need":
			if i := slices.Index(needNames[:], fileReader.scalar(value)); i > 0 {
				field.Need.Level = NeedLevel(i)
			} else {
				fileReader.fail(value, "unknown need %q", fileReader.scalar(value))
			}
		case "need_evidence":
			if i := slices.Index(evidenceNames[:], fileReader.scalar(value)); i >= 0 {
				field.Need.Evidence = Evidence(i)
			} else {
				fileReader.fail(value, "unknown evidence %q", fileReader.scalar(value))
			}
		case "need_when":
			field.Need.When = fileReader.text(value)
		case "class":
			if i := slices.Index(classNames[:], fileReader.scalar(value)); i > 0 {
				field.Class = Class(i)
			} else {
				fileReader.fail(value, "unknown class %q", fileReader.scalar(value))
			}
		case "body":
			field.Body = fileReader.bodyClass(value)
		case "body_scope":
			field.BodyScope = fileReader.scalar(value)
		case "accepts":
			fileReader.each(value, func(name string, value jomini.Value) {
				accept := Accept{}
				kind, isRef := strings.CutPrefix(name, "ref:")
				switch i := slices.Index(typeNames[:], name); {
				case isRef:
					accept.Type, accept.Kind, accept.Count = TypeRef, kind, fileReader.int(value)
				case name == "enum":
					accept.Type = TypeEnum
					fileReader.each(value, func(key string, value jomini.Value) {
						switch key {
						case "count":
							accept.Count = fileReader.int(value)
						case "words":
							accept.Enum = fileReader.words(value)
						default:
							fileReader.fail(value, "unknown enum key %q", key)
						}
					})
				case i >= 0:
					accept.Type, accept.Count = ValueType(i), fileReader.int(value)
				default:
					fileReader.fail(value, "unknown value type %q", name)
				}
				field.Accepts = append(field.Accepts, accept)
			})
		case "declared":
			byEvidence[Declared] = fileReader.words(value)
		case "inferred":
			byEvidence[Inferred] = fileReader.words(value)
		case "ops":
			field.Ops = fileReader.words(value)
		case "defines":
			field.Defines = fileReader.scalar(value)
		case "keyed_by":
			field.KeyedBy = fileReader.scalar(value)
		case "members":
			field.Members = map[string]*Field{}
			fileReader.each(value, func(name string, value jomini.Value) {
				field.Members[name] = fileReader.field(name, value)
			})
		default:
			fileReader.fail(value, "unknown field key %q", key)
		}
	})

	// the lists by evidence name accepts written above them
	for i, accept := range field.Accepts {
		for evidence, names := range byEvidence {
			if slices.Contains(names, accept.name()) {
				field.Accepts[i].Evidence = evidence
			}
		}
	}
	return field
}

// named is the block of a statement written key = { }, which must carry that key.
func (fileReader *fileReader) named(st jomini.Statement, key string) jomini.Value {
	assignment, ok := st.(*jomini.Assignment)
	if !ok || assignment.Key.Text != key {
		fileReader.fail(st, "expected the %s block here", key)
		return nil
	}
	return assignment.Value
}

// each calls fn for every key = value of a block, and fails on anything else in it.
func (fileReader *fileReader) each(value jomini.Value, fn func(key string, value jomini.Value)) {
	block := jomini.BlockOf(value)
	if block == nil {
		if value != nil {
			fileReader.fail(value, "expected a block")
		}
		return
	}
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok && assignment.Value != nil {
			fn(assignment.Key.Text, assignment.Value)
		} else {
			fileReader.fail(st, "expected key = value")
		}
	}
}

// words is the bare words of a one-line list.
func (fileReader *fileReader) words(value jomini.Value) []string {
	block := jomini.BlockOf(value)
	if block == nil {
		fileReader.fail(value, "expected a list")
		return nil
	}
	var out []string
	for _, st := range block.Statements {
		if bare, ok := st.(*jomini.ValueStmt); ok {
			out = append(out, fileReader.scalar(bare.Value))
		} else {
			fileReader.fail(st, "expected a word")
		}
	}
	return out
}

// bodyClass is the bits of a list of body class names.
func (fileReader *fileReader) bodyClass(value jomini.Value) BodyClass {
	var class BodyClass
	for _, word := range fileReader.words(value) {
		if i := slices.Index(bodyNames[:], word); i >= 0 {
			class |= 1 << i
		} else {
			fileReader.fail(value, "unknown body class %q", word)
		}
	}
	return class
}

// text is prose written by text, its escapes undone.
func (fileReader *fileReader) text(value jomini.Value) string {
	return strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\n`, "\n").Replace(fileReader.scalar(value))
}

// scalar is the text of one written word.
func (fileReader *fileReader) scalar(value jomini.Value) string {
	if scalar, ok := value.(*jomini.Scalar); ok {
		return scalar.Text
	}
	fileReader.fail(value, "expected one word")
	return ""
}

// int is a whole number.
func (fileReader *fileReader) int(value jomini.Value) int {
	number, err := strconv.Atoi(fileReader.scalar(value))
	if err != nil {
		fileReader.fail(value, "expected a whole number")
	}
	return number
}

// float is a coverage.
func (fileReader *fileReader) float(value jomini.Value) float64 {
	number, err := strconv.ParseFloat(fileReader.scalar(value), 64)
	if err != nil {
		fileReader.fail(value, "expected a number")
	}
	return number
}

// fail keeps the first error, with the line it is on.
func (fileReader *fileReader) fail(at jomini.Node, format string, args ...any) {
	if fileReader.err == nil {
		line := fileReader.lines.PositionAt(at.NodeRange().Start).Line + 1
		fileReader.err = fmt.Errorf("schema file line %d: %s", line, fmt.Sprintf(format, args...))
	}
}
