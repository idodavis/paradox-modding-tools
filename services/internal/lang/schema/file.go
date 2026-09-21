// file.go writes a Schema as text and reads it back. The text is script, so the engine's own parser reads it and a person reads it with script highlighting: one assignment per line, indented by depth, kinds and members in name order, and only a list of plain words on one line, so a learner change shows as a diff of the lines that changed. The file holds every field of the schema types and nothing else: Read(Write(s)) gives s back. Its first block is the header, its second the drift report, its last the arguments, and every block between is a kind; the three are found by position, so a kind may carry any name. A file this reader does not understand is an error, never a guess: the caller scans again.
package schema

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/source"
)

// driftLabels names the sections of the drift block, in the order of Drift's fields.
var driftLabels = []string{"parse_errors", "extensions", "folders", "entry_modes", "prefixes", "dump_labels", "name_tokens", "unclassified"}

// sections is the tallies of a drift report, in the order of driftLabels.
func (d *Drift) sections() []*[]Tally {
	return []*[]Tally{&d.ParseErrors, &d.Extensions, &d.Folders, &d.EntryModes, &d.Prefixes, &d.DumpLabels, &d.NameTokens, &d.Unclassified}
}

// Write is the schema as text.
func Write(s *Schema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "schema = {\n\tgame = %s\n\tversion = %s\n}\n", word(s.Game), word(s.Version))

	// the drift report: a section only where something was seen
	b.WriteString("drift = {\n")
	for i, tallies := range s.Drift.sections() {
		if len(*tallies) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\t%s = {\n", driftLabels[i])
		for _, t := range *tallies {
			fmt.Fprintf(&b, "\t\t%s = %d\n", word(t.Name), t.Count)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")

	for _, name := range slices.Sorted(maps.Keys(s.Kinds)) {
		writeKind(&b, s.Kinds[name])
	}

	b.WriteString("arguments = {\n")
	for _, name := range slices.Sorted(maps.Keys(s.Arguments)) {
		fmt.Fprintf(&b, "\t%s = {\n", word(name))
		writeField(&b, 2, s.Arguments[name])
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// writeKind writes one kind: its facts in a fixed order, each only when set, then its shape.
func writeKind(b *strings.Builder, k *Kind) {
	fmt.Fprintf(b, "%s = {\n", word(k.Name))
	if k.Format != source.Script {
		fmt.Fprintf(b, "\tformat = %s\n", k.Format)
	}
	if len(k.Folders) > 0 {
		fmt.Fprintf(b, "\tfolders = %s\n", list(k.Folders))
	}

	fmt.Fprintf(b, "\tnaming = {\n\t\tform = %s\n", word(k.Naming.Form))
	if k.Naming.Keyword != "" {
		fmt.Fprintf(b, "\t\tkeyword = %s\n", word(k.Naming.Keyword))
	}
	if k.Naming.Member != "" {
		fmt.Fprintf(b, "\t\tmember = %s\n", word(k.Naming.Member))
	}
	if k.Naming.Form == "column" {
		fmt.Fprintf(b, "\t\tcolumn = %d\n", k.Naming.Column)
	}
	b.WriteString("\t}\n")

	for _, fact := range [][2]string{{"parent", k.Parent}, {"subject", k.Subject}} {
		if fact[1] != "" {
			fmt.Fprintf(b, "\t%s = %s\n", fact[0], word(fact[1]))
		}
	}
	if len(k.Prefixes) > 0 {
		fmt.Fprintf(b, "\tprefixes = %s\n", list(k.Prefixes))
	}
	for _, fact := range [][2]string{{"scope_type", k.ScopeType}, {"label_member", k.LabelMember}} {
		if fact[1] != "" {
			fmt.Fprintf(b, "\t%s = %s\n", fact[0], word(fact[1]))
		}
	}

	// the templates, each with its coverage
	if len(k.LocTemplates) > 0 {
		b.WriteString("\tloc = {\n")
		for _, t := range k.LocTemplates {
			fmt.Fprintf(b, "\t\t%s = %s\n", word(string(t.Template)), strconv.FormatFloat(t.Coverage, 'f', -1, 64))
		}
		b.WriteString("\t}\n")
	}
	if len(k.FileTemplates) > 0 {
		b.WriteString("\tfiles = {\n")
		for _, t := range k.FileTemplates {
			fmt.Fprintf(b, "\t\t%s = %s\n", word(t.Pattern), strconv.FormatFloat(t.Coverage, 'f', -1, 64))
		}
		b.WriteString("\t}\n")
	}

	if k.Macro != 0 {
		fmt.Fprintf(b, "\tmacro = %s\n", list(k.Macro.words()))
	}
	if k.Params {
		b.WriteString("\tparams = yes\n")
	}
	if k.Shape != nil {
		b.WriteString("\tshape = {\n")
		writeField(b, 2, k.Shape)
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
}

// writeField writes the body of a field at a depth of tabs: the count, then each fact only when set, then the members in name order.
func writeField(b *strings.Builder, depth int, f *Field) {
	tabs := strings.Repeat("\t", depth)
	fmt.Fprintf(b, "%scount = %d\n", tabs, f.Count)
	if f.Class != ClassNone {
		fmt.Fprintf(b, "%sclass = %s\n", tabs, f.Class)
	}
	if f.Body != 0 {
		fmt.Fprintf(b, "%sbody = %s\n", tabs, list(f.Body.words()))
	}
	if f.BodyScope != "" {
		fmt.Fprintf(b, "%sbody_scope = %s\n", tabs, word(f.BodyScope))
	}

	// every accept with its count, in order; the declared ones are named again in one list
	if len(f.Accepts) > 0 {
		var declared []string
		fmt.Fprintf(b, "%saccepts = {\n", tabs)
		for _, a := range f.Accepts {
			name := a.Type.String()
			if a.Type == TypeRef {
				name = "ref:" + a.Kind
			}
			if a.Declared {
				declared = append(declared, name)
			}
			if a.Type == TypeEnum {
				fmt.Fprintf(b, "%s\tenum = {\n%s\t\tcount = %d\n%s\t\twords = %s\n%s\t}\n", tabs, tabs, a.Count, tabs, list(a.Enum), tabs)
				continue
			}
			fmt.Fprintf(b, "%s\t%s = %d\n", tabs, word(name), a.Count)
		}
		fmt.Fprintf(b, "%s}\n", tabs)
		if len(declared) > 0 {
			fmt.Fprintf(b, "%sdeclared = %s\n", tabs, list(declared))
		}
	}

	if len(f.Ops) > 0 {
		fmt.Fprintf(b, "%sops = %s\n", tabs, list(f.Ops))
	}
	if f.Defines != "" {
		fmt.Fprintf(b, "%sdefines = %s\n", tabs, word(f.Defines))
	}
	if f.KeyedBy != "" {
		fmt.Fprintf(b, "%skeyed_by = %s\n", tabs, word(f.KeyedBy))
	}
	if len(f.Members) > 0 {
		fmt.Fprintf(b, "%smembers = {\n", tabs)
		for _, name := range slices.Sorted(maps.Keys(f.Members)) {
			fmt.Fprintf(b, "%s\t%s = {\n", tabs, word(name))
			writeField(b, depth+2, f.Members[name])
			fmt.Fprintf(b, "%s\t}\n", tabs)
		}
		fmt.Fprintf(b, "%s}\n", tabs)
	}
}

// words is the names of the classes set in a body class.
func (c BodyClass) words() []string {
	var out []string
	for i, name := range bodyNames {
		if c&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// word writes one word so the lexer reads the same word back: bare when it is letters, digits and _ . : / - | only, quoted otherwise ("<date>", ">=", "trait_{id}", ""). A name never holds a double quote or a line break, because the lexer cannot produce one in a key.
func word(text string) string {
	plain := text != ""
	for _, r := range text {
		if !(r == '_' || r == '.' || r == ':' || r == '/' || r == '-' || r == '|' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 127) {
			plain = false
		}
	}
	if plain {
		return text
	}
	return `"` + text + `"`
}

// list writes words as one line: { a b "c d" }.
func list(words []string) string {
	var b strings.Builder
	b.WriteString("{")
	for _, w := range words {
		b.WriteString(" " + word(w))
	}
	b.WriteString(" }")
	return b.String()
}

// fileReader reads a schema file's tree and keeps the first thing it did not understand.
type fileReader struct {
	lines *jomini.LineIndex
	err   error
}

// Read is the schema a text holds.
func Read(text string) (*Schema, error) {
	parsed := jomini.Parse(text)
	r := &fileReader{lines: parsed.Lines()}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("schema file line %d: %s", r.lines.PositionAt(parsed.Errors[0].Range.Start).Line+1, parsed.Errors[0].Message)
	}

	// the header, the drift report and the arguments are found by position; every block between is a kind
	stmts := parsed.Root.Statements
	if len(stmts) < 3 {
		return nil, errors.New("schema file: the header, drift and arguments blocks are not all there")
	}
	s := &Schema{Kinds: map[string]*Kind{}, Arguments: map[string]*Field{}}
	r.each(r.named(stmts[0], "schema"), func(key string, v jomini.Value) {
		switch key {
		case "game":
			s.Game = r.scalar(v)
		case "version":
			s.Version = r.scalar(v)
		default:
			r.fail(v, "unknown header key %q", key)
		}
	})

	sections := s.Drift.sections()
	r.each(r.named(stmts[1], "drift"), func(label string, v jomini.Value) {
		i := slices.Index(driftLabels, label)
		if i < 0 {
			r.fail(v, "unknown drift section %q", label)
			return
		}
		r.each(v, func(name string, count jomini.Value) {
			*sections[i] = append(*sections[i], Tally{Name: name, Count: r.int(count)})
		})
	})

	for _, st := range stmts[2 : len(stmts)-1] {
		if a, ok := st.(*jomini.Assignment); ok {
			s.Kinds[a.Key.Text] = r.kind(a.Key.Text, a.Value)
		} else {
			r.fail(st, "a kind is written name = { }")
		}
	}
	r.each(r.named(stmts[len(stmts)-1], "arguments"), func(name string, v jomini.Value) {
		s.Arguments[name] = r.field(name, v)
	})
	return s, r.err
}

// kind reads one kind block.
func (r *fileReader) kind(name string, block jomini.Value) *Kind {
	k := &Kind{Name: name, Format: source.Script}
	r.each(block, func(key string, v jomini.Value) {
		switch key {
		case "format":
			k.Format = source.Unknown
			for f := source.Unknown; f <= source.Opaque; f++ {
				if f.String() == r.scalar(v) {
					k.Format = f
				}
			}
		case "folders":
			k.Folders = r.words(v)
		case "naming":
			r.each(v, func(key string, v jomini.Value) {
				switch key {
				case "form":
					if k.Naming.Form = r.scalar(v); !slices.Contains(namingForms, k.Naming.Form) {
						r.fail(v, "unknown naming form %q", k.Naming.Form)
					}
				case "keyword":
					k.Naming.Keyword = r.scalar(v)
				case "member":
					k.Naming.Member = r.scalar(v)
				case "column":
					k.Naming.Column = r.int(v)
				default:
					r.fail(v, "unknown naming key %q", key)
				}
			})
		case "parent":
			k.Parent = r.scalar(v)
		case "subject":
			k.Subject = r.scalar(v)
		case "prefixes":
			k.Prefixes = r.words(v)
		case "scope_type":
			k.ScopeType = r.scalar(v)
		case "label_member":
			k.LabelMember = r.scalar(v)
		case "loc":
			r.each(v, func(template string, v jomini.Value) {
				k.LocTemplates = append(k.LocTemplates, loc.Convention{Template: loc.Template(template), Coverage: r.float(v)})
			})
		case "files":
			r.each(v, func(pattern string, v jomini.Value) {
				k.FileTemplates = append(k.FileTemplates, FileTemplate{Pattern: pattern, Coverage: r.float(v)})
			})
		case "macro":
			k.Macro = r.bodyClass(v)
		case "params":
			k.Params = r.scalar(v) == "yes"
		case "shape":
			k.Shape = r.field("", v)
		default:
			r.fail(v, "unknown kind key %q", key)
		}
	})
	return k
}

// field reads the body of one field, and its members below it.
func (r *fileReader) field(name string, block jomini.Value) *Field {
	f := &Field{Name: name}
	var declared []string
	r.each(block, func(key string, v jomini.Value) {
		switch key {
		case "count":
			f.Count = r.int(v)
		case "class":
			if i := slices.Index(classNames[:], r.scalar(v)); i > 0 {
				f.Class = Class(i)
			} else {
				r.fail(v, "unknown class %q", r.scalar(v))
			}
		case "body":
			f.Body = r.bodyClass(v)
		case "body_scope":
			f.BodyScope = r.scalar(v)
		case "accepts":
			r.each(v, func(name string, v jomini.Value) {
				a := Accept{}
				kind, isRef := strings.CutPrefix(name, "ref:")
				switch i := slices.Index(typeNames[:], name); {
				case isRef:
					a.Type, a.Kind, a.Count = TypeRef, kind, r.int(v)
				case name == "enum":
					a.Type = TypeEnum
					r.each(v, func(key string, v jomini.Value) {
						switch key {
						case "count":
							a.Count = r.int(v)
						case "words":
							a.Enum = r.words(v)
						default:
							r.fail(v, "unknown enum key %q", key)
						}
					})
				case i >= 0:
					a.Type, a.Count = ValueType(i), r.int(v)
				default:
					r.fail(v, "unknown value type %q", name)
				}
				f.Accepts = append(f.Accepts, a)
			})
		case "declared":
			declared = r.words(v)
		case "ops":
			f.Ops = r.words(v)
		case "defines":
			f.Defines = r.scalar(v)
		case "keyed_by":
			f.KeyedBy = r.scalar(v)
		case "members":
			f.Members = map[string]*Field{}
			r.each(v, func(name string, v jomini.Value) {
				f.Members[name] = r.field(name, v)
			})
		default:
			r.fail(v, "unknown field key %q", key)
		}
	})

	// the declared list names accepts written above it
	for i, a := range f.Accepts {
		name := a.Type.String()
		if a.Type == TypeRef {
			name = "ref:" + a.Kind
		}
		f.Accepts[i].Declared = slices.Contains(declared, name)
	}
	return f
}

// named is the block of a statement written key = { }, which must carry that key.
func (r *fileReader) named(st jomini.Statement, key string) jomini.Value {
	a, ok := st.(*jomini.Assignment)
	if !ok || a.Key.Text != key {
		r.fail(st, "expected the %s block here", key)
		return nil
	}
	return a.Value
}

// each calls fn for every key = value of a block, and fails on anything else in it.
func (r *fileReader) each(block jomini.Value, fn func(key string, v jomini.Value)) {
	b := jomini.BlockOf(block)
	if b == nil {
		if block != nil {
			r.fail(block, "expected a block")
		}
		return
	}
	for _, st := range b.Statements {
		if a, ok := st.(*jomini.Assignment); ok && a.Value != nil {
			fn(a.Key.Text, a.Value)
		} else {
			r.fail(st, "expected key = value")
		}
	}
}

// words is the bare words of a one-line list.
func (r *fileReader) words(block jomini.Value) []string {
	b := jomini.BlockOf(block)
	if b == nil {
		r.fail(block, "expected a list")
		return nil
	}
	var out []string
	for _, st := range b.Statements {
		if v, ok := st.(*jomini.ValueStmt); ok {
			out = append(out, r.scalar(v.Value))
		} else {
			r.fail(st, "expected a word")
		}
	}
	return out
}

// bodyClass is the bits of a list of body class names.
func (r *fileReader) bodyClass(block jomini.Value) BodyClass {
	var c BodyClass
	for _, w := range r.words(block) {
		if i := slices.Index(bodyNames[:], w); i >= 0 {
			c |= 1 << i
		} else {
			r.fail(block, "unknown body class %q", w)
		}
	}
	return c
}

// scalar is the text of one written word.
func (r *fileReader) scalar(v jomini.Value) string {
	if s, ok := v.(*jomini.Scalar); ok {
		return s.Text
	}
	r.fail(v, "expected one word")
	return ""
}

// int is a whole number.
func (r *fileReader) int(v jomini.Value) int {
	n, err := strconv.Atoi(r.scalar(v))
	if err != nil {
		r.fail(v, "expected a whole number")
	}
	return n
}

// float is a coverage.
func (r *fileReader) float(v jomini.Value) float64 {
	n, err := strconv.ParseFloat(r.scalar(v), 64)
	if err != nil {
		r.fail(v, "expected a number")
	}
	return n
}

// fail keeps the first error, with the line it is on.
func (r *fileReader) fail(at jomini.Node, format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("schema file line %d: %s", r.lines.PositionAt(at.NodeRange().Start).Line+1, fmt.Sprintf(format, args...))
	}
}
