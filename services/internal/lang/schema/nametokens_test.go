package schema

import (
	"reflect"
	"testing"
)

func TestNameTokensOf(t *testing.T) {
	for token, want := range map[string][]NameToken{
		"has_variable":                  {{Token: "has_variable", Kind: "variables"}},
		"set_variable":                  {{Token: "set_variable", Kind: "variables", Defines: true, Member: "name"}},
		"change_variable":               {{Token: "change_variable", Kind: "variables", Member: "name"}},
		"set_local_variable":            {{Token: "set_local_variable", Kind: "local_variables", Defines: true, Member: "name"}},
		"add_to_global_variable_list":   {{Token: "add_to_global_variable_list", Kind: "global_variables", Defines: true, Member: "name"}},
		"remove_list_local_variable":    {{Token: "remove_list_local_variable", Kind: "local_variables", Member: "name"}},
		"global_variable_list_size":     {{Token: "global_variable_list_size", Kind: "global_variables", Member: "name"}},
		"any_key_in_local_variable_map": {{Token: "any_key_in_local_variable_map", Kind: "local_variables", Member: "variable"}},
		"save_scope_as":                 {{Token: "save_scope_as", Kind: "saved_scopes", Defines: true}},
		"save_temporary_scope_value_as": {{Token: "save_temporary_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"}},
		"clear_saved_scope":             {{Token: "clear_saved_scope", Kind: "saved_scopes"}},
		"add_to_temporary_list":         {{Token: "add_to_temporary_list", Kind: "lists", Defines: true, Member: "name"}},
		"every_in_list":                 {{Token: "every_in_list", Kind: "lists", Member: "list"}, {Token: "every_in_list", Kind: "variables", Member: "variable"}},
		"ordered_in_global_list":        {{Token: "ordered_in_global_list", Kind: "global_variables", Member: "variable"}},
		"add_character_flag":            {{Token: "add_character_flag", Kind: "flags", Defines: true, Member: "flag"}},
		"add_gold":                      nil,
		"random_list":                   nil,
		"has_trait_flag":                nil,
		"":                              nil,
	} {
		if got := NameTokensOf(token); !reflect.DeepEqual(got, want) {
			t.Errorf("NameTokensOf(%q) = %+v, want %+v", token, got, want)
		}
	}
}

func TestNameTokenTable(t *testing.T) {
	seen := map[[2]string]bool{}
	for _, row := range nameTokens {
		if _, ok := ScriptKinds[row.Kind]; !ok {
			t.Errorf("%s names the kind %q, which is no script-made kind", row.Token, row.Kind)
		}
		if at := [2]string{row.Token, row.Member}; seen[at] {
			t.Errorf("%s has two rows for the member %q", row.Token, row.Member)
		} else {
			seen[at] = true
		}
	}
	if len(ScriptKinds) != 6 || ScriptKinds["saved_scopes"] != "scope" || ScriptKinds["variables"] != "var" || ScriptKinds["flags"] != "" {
		t.Errorf("ScriptKinds = %v", ScriptKinds)
	}
}
