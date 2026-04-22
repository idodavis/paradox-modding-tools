package interpreter

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// ############
// Lexer (used for parsing Paradox script input)
// ############
var (
	paradoxLexer = lexer.MustSimple([]lexer.SimpleRule{
		{Name: "BOM", Pattern: "\uFEFF"},
		{Name: "Comment", Pattern: `#[^\r\n]*`},
		{Name: "WS", Pattern: `[\s\r\n]+`},

		{Name: "LBRACE", Pattern: `{`},
		{Name: "RBRACE", Pattern: `}`},
		{Name: "COMMA", Pattern: `,`},
		{Name: "ColorModel", Pattern: `(hsv360|hsv|rgb)`},

		{Name: "Boolean", Pattern: `\b(yes|no)\b`},

		{Name: "LTE", Pattern: `<=`},
		{Name: "GTE", Pattern: `>=`},
		{Name: "NEQ", Pattern: `!=`},
		{Name: "QEQ", Pattern: `\?=`},
		{Name: "EQEQ", Pattern: `==`},

		{Name: "VariableCallBracketed", Pattern: `@\[[^\]]+\]`},
		{Name: "VariableCallSimple", Pattern: `@[\p{L}\p{N}_.$:'&|%/\\-]+`},
		// Date (year.month.day or year.month.day.hour) for history start-date keys; must be before Number; optional leading minus
		{Name: "Date", Pattern: `[-]?\d+\.\d+\.\d+(\.\d+)?`},
		{Name: "Number", Pattern: `[-+]?(\d*\.)?\d+\b`},
		{Name: "Ident", Pattern: `[\p{L}\p{N}_.$:'&|%/\\-]+`},
		{Name: "String", Pattern: `"(\\"|[^"])*"`},

		{Name: "EQ", Pattern: `=`},
		{Name: "LT", Pattern: `<`},
		{Name: "GT", Pattern: `>`},
	})

	paradoxParser = participle.MustBuild[ParadoxFile](
		participle.Lexer(paradoxLexer),
		participle.UseLookahead(participle.MaxLookahead),
	)
)

// ############
// Parser structs (types produced by the parser)
// ############

// Node is the base struct for all parser nodes (position and raw tokens).
type Node struct {
	Pos    lexer.Position
	Tokens []lexer.Token
}

type Color struct {
	Node

	Model       *string  `parser:"@ColorModel?"`
	RHue        float64  `parser:"WS? '{' WS? @Number"`
	GSaturation float64  `parser:"WS @Number"`
	BValue      float64  `parser:"WS @Number"`
	A           *float64 `parser:"( WS @Number)? WS? '}'"`
}

type Literal struct {
	Node

	Identifier   *string    `parser:"@Ident"`
	String       *string    `parser:"| @String"`
	Number       *float64   `parser:"| @Number"`
	Date         *string    `parser:"| @Date"` // year.month.day or year.month.day.hour (e.g. 867.1.1)
	Boolean      *string    `parser:"| @Boolean"`
	VariableCall *string    `parser:"| (@VariableCallSimple | @VariableCallBracketed)"`
	Color        *Color     `parser:"| @@"`
	Array        []*Literal `parser:"| '{' WS? ((@@ WS) | (@@ WS* ',' WS*))+ WS? '}'"`
	Operator     *string    `parser:"| @(LTE|GTE|NEQ|QEQ|EQEQ|LT|GT)"`
}

type ObjectEntry struct {
	Node

	Expression *Expression `parser:"@@"`
	Literal    *Literal    `parser:"| @@"`
	Object     *Object     `parser:"| @@"`
	Comment    string      `parser:"| @Comment"`
}

type Object struct {
	Node

	Entries []*ObjectEntry `parser:"'{' WS* (@@ WS*)* WS* '}'"`
}

// Expressions will require context of surrounding script to know the difference.
// Also Expressions of things like scopes can contain the qualifier comparison,
// even without the '=' operator.
type Expression struct {
	Node

	Key                    string   `parser:"((@Ident WS* (@Ident)?) | @String | @Number | @Date | @Boolean | @VariableCallSimple)"`
	Operator               *string  `parser:"WS* @(LTE|GTE|NEQ|QEQ|EQEQ|EQ|LT|GT) WS*"`
	OptionalCommentAfterEq string   `parser:"@Comment?"` // allow comment between = and value (e.g. "key = # comment\n{")
	OptionalWSAfterComment string   `parser:"@WS*"`
	Literal                *Literal `parser:"( @@"`
	Object                 *Object  `parser:"| @@ )"`
}

type Namespace struct {
	Node

	Key   string `parser:"'namespace' WS? @EQ"`
	Value string `parser:"WS? @Ident"`
}

type Entry struct {
	Node

	Namespace  *Namespace  `parser:"@@"`
	Expression *Expression `parser:"| @@"`
	Comment    string      `parser:"| @Comment"`
	Whitespace string      `parser:"| @WS"`
}

type ParadoxFile struct {
	Node

	BOM     string   `parser:"@BOM?"`
	Entries []*Entry `parser:"@@*"`
}

// ////////////////////////////////////////////////////
// Raw text method - extract original text from source
// ////////////////////////////////////////////////////
func (node *Node) GetRawText() string {
	var sb strings.Builder
	for _, tok := range node.Tokens {
		sb.WriteString(tok.Value)
	}
	return sb.String()
}

// /////////////////////////////////////
// Pretty formatted methods (JSON-like)
// /////////////////////////////////////
func (l *Literal) ToPrettyString(indent string) string {
	switch {
	case l.Boolean != nil:
		return *l.Boolean
	case l.Identifier != nil:
		return *l.Identifier
	case l.String != nil:
		return fmt.Sprintf("%q", *l.String)
	case l.Number != nil:
		return fmt.Sprintf("%v", *l.Number)
	case l.Date != nil:
		return *l.Date
	case l.VariableCall != nil:
		return *l.VariableCall
	case l.Color != nil:
		if l.Color.Model == nil {
			if l.Color.A == nil {
				return fmt.Sprintf("{ %v %v %v }", l.Color.RHue, l.Color.GSaturation, l.Color.BValue)
			} else {
				return fmt.Sprintf("{ %v %v %v %v }", l.Color.RHue, l.Color.GSaturation, l.Color.BValue, *l.Color.A)
			}
		} else {
			if l.Color.A == nil {
				return fmt.Sprintf("%s{ %v %v %v }", *l.Color.Model, l.Color.RHue, l.Color.GSaturation, l.Color.BValue)
			} else {
				return fmt.Sprintf("%s{ %v %v %v %v }", *l.Color.Model, l.Color.RHue, l.Color.GSaturation, l.Color.BValue, *l.Color.A)
			}
		}
	case l.Array != nil:
		parts := make([]string, len(l.Array))
		for i, e := range l.Array {
			parts[i] = e.ToPrettyString(indent)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case l.Operator != nil:
		return *l.Operator
	}
	return "null"
}

func (o *Object) ToPrettyString(indent string) string {
	if o == nil {
		return "{}"
	}
	if len(o.Entries) == 0 {
		return "{}"
	}

	nextIndent := indent + " " // Adds indent for the next line
	parts := make([]string, 0, len(o.Entries))

	for _, e := range o.Entries {
		if e.Expression != nil {
			parts = append(parts, nextIndent+e.Expression.ToPrettyString(nextIndent))
		} else if e.Literal != nil {
			parts = append(parts, nextIndent+e.Literal.ToPrettyString(nextIndent))
		} else if e.Object != nil {
			parts = append(parts, nextIndent+e.Object.ToPrettyString(nextIndent))
		} else if e.Comment != "" {
			parts = append(parts, nextIndent+"# "+strings.TrimPrefix(e.Comment, "#"))
		}
	}

	return "{\n" + strings.Join(parts, ",\n") + "\n" + indent + "}"
}

func (expr *Expression) ToPrettyString(indent string) string {
	if expr == nil {
		return ""
	}
	op := "="
	if expr.Operator != nil {
		op = *expr.Operator
	}

	key := expr.Key
	value := ""
	if expr.Literal != nil {
		value = expr.Literal.ToPrettyString(indent)
	} else if expr.Object != nil {
		value = expr.Object.ToPrettyString(indent) // Pass indent to nested objects
	}

	return fmt.Sprintf("%s %s %s", key, op, value)
}

func (n *Namespace) ToPrettyString(indent string) string {
	return fmt.Sprintf("namespace = %s", n.Value)
}

func (e *Entry) ToPrettyString(indent string) string {
	if e.Expression != nil {
		return e.Expression.ToPrettyString(indent)
	}
	if e.Namespace != nil {
		return e.Namespace.ToPrettyString(indent)
	}
	if e.Comment != "" {
		return "# " + strings.TrimPrefix(e.Comment, "#")
	}
	return ""
}

// ############
// ParseFile
// ############

// ParseFile parses a Paradox script file and returns its AST (*ParadoxFile) or an error.
func ParseFile(filename string) (*ParadoxFile, error) {
	r, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	paradoxScript, err := paradoxParser.Parse("", r)
	if err != nil {
		return nil, err
	}
	return paradoxScript, nil
}
