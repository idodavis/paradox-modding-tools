package lang

import (
	"testing"

	"paradox-modding-tools/internal/jomini"
)

// TestResolveMacroReplacesParameters checks that a call to a macro expands
// with its parameters replaced.
func TestResolveMacroReplacesParameters(t *testing.T) {
	macros := &Schema{DataTypes: map[string]DataType{GlobalType: {Functions: map[string]DataFunction{
		"IsZero": {Name: "IsZero", Arguments: []string{"Value"}, Expansion: "EqualTo_int32(Value, '(int32)0')"},
	}}}}

	call, ok := jomini.ParseCall("IsZero(GetPlayer.GetGold)", 0)
	if !ok {
		t.Fatal("call not parsed")
	}

	expanded := macros.ResolveMacro(call, "")
	if len(expanded.Steps) != 1 || expanded.Steps[0].Text != "EqualTo_int32" {
		t.Fatalf("steps: %+v", expanded.Steps)
	}

	argument := expanded.Steps[0].Arguments[0]
	if argument.Call == nil || argument.Call.Steps[0].Text != "GetPlayer" || argument.Span.IsEmpty() {
		t.Errorf("first argument: %+v", argument)
	}
}
