// nametokens.go is the one hand table of the engine, shared by the three games: the effects and triggers that make a name while script runs, or read one. A saved scope, a variable, a list and a character flag have no folder and no definition file, and no file of any game states which tokens create them, so this cannot be learned. The rows were read out of the three games' script_docs dumps on 2026-09-20, and the gate checks that every token here is one some game still declares. The learner reports, as drift, a token that behaves like one of these and is not listed.
package schema

import "strings"

// NameToken is one token and one place in it that holds a name. A token written with one word takes that word as the name; Member says where the name is when the token takes a block.
type NameToken struct {
	Token   string // set_variable, has_variable, save_scope_as, add_to_list, every_in_list
	Kind    string // the script-made kind the name belongs to, one of scriptKinds
	Defines bool   // the token creates the name. false: it reads, changes or removes one
	Member  string // the member that holds the name when the token takes a block: name, list, variable, flag. "" when it never takes one
}

// scriptKinds is the script-made kinds, each with the declared prefix that cites its names, "" when none does.
var scriptKinds = map[string]string{
	"saved_scopes": "scope", "variables": "var", "local_variables": "local_var", "global_variables": "global_var", "lists": "", "flags": "",
}

// variableTokens is the tokens of a variable scope, spelled for the plain one. The local and global scopes spell each with their word before "variable": set_local_variable, add_to_global_variable_list. A variable list and a variable map are variables that hold several targets, so their names are variables too.
var variableTokens = []NameToken{
	{Token: "set_variable", Defines: true, Member: "name"}, {Token: "change_variable", Member: "name"}, {Token: "clamp_variable", Member: "name"}, {Token: "round_variable", Member: "name"},
	{Token: "has_variable"}, {Token: "remove_variable"},

	{Token: "add_to_variable_list", Defines: true, Member: "name"}, {Token: "remove_list_variable", Member: "name"}, {Token: "clear_variable_list"}, {Token: "has_variable_list"},
	{Token: "variable_list_size", Member: "name"}, {Token: "is_target_in_variable_list", Member: "name"}, {Token: "sort_variable_list", Member: "name"},

	{Token: "add_to_variable_map", Defines: true, Member: "name"}, {Token: "remove_from_variable_map", Member: "name"}, {Token: "clear_variable_map"}, {Token: "has_variable_map"},
	{Token: "variable_map_size", Member: "name"}, {Token: "is_key_in_variable_map", Member: "name"}, {Token: "is_value_in_variable_map", Member: "name"},
	{Token: "any_key_in_variable_map", Member: "variable"}, {Token: "every_key_in_variable_map", Member: "variable"}, {Token: "ordered_key_in_variable_map", Member: "variable"}, {Token: "random_key_in_variable_map", Member: "variable"},
}

// nameTokens is the whole table, and nameTokensByToken the same rows by token.
var nameTokens, nameTokensByToken = func() ([]NameToken, map[string][]NameToken) {
	rows := []NameToken{
		{Token: "save_scope_as", Kind: "saved_scopes", Defines: true}, {Token: "save_temporary_scope_as", Kind: "saved_scopes", Defines: true},
		{Token: "save_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"}, {Token: "save_temporary_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "save_opinion_value_as", Kind: "saved_scopes", Defines: true, Member: "name"}, {Token: "save_temporary_opinion_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "clear_saved_scope", Kind: "saved_scopes"},

		{Token: "add_to_list", Kind: "lists", Defines: true}, {Token: "add_to_temporary_list", Kind: "lists", Defines: true, Member: "name"},
		{Token: "remove_from_list", Kind: "lists"}, {Token: "is_in_list", Kind: "lists"},

		// CK3 alone: the variables of the dead, several names under one token, and character flags
		{Token: "set_dead_character_variable", Kind: "variables", Defines: true, Member: "name"}, {Token: "has_dead_character_variable", Kind: "variables"}, {Token: "remove_dead_character_variable", Kind: "variables"},
		{Token: "has_all_variables", Kind: "variables", Member: "name"}, {Token: "has_none_of_variables", Kind: "variables", Member: "name"},
		{Token: "add_character_flag", Kind: "flags", Defines: true, Member: "flag"}, {Token: "has_character_flag", Kind: "flags"}, {Token: "remove_character_flag", Kind: "flags"},
		{Token: "add_dead_character_flag", Kind: "flags", Defines: true, Member: "flag"}, {Token: "has_dead_character_flag", Kind: "flags"},
	}

	// every variable token, in its three scopes
	for _, scope := range []string{"variable", "local_variable", "global_variable"} {
		for _, row := range variableTokens {
			row.Token, row.Kind = strings.Replace(row.Token, "variable", scope, 1), scope+"s"
			rows = append(rows, row)
		}
	}

	// a list iterator reads a list at list = name, and a variable list at variable = name; the local and global ones read only variable lists
	for _, prefix := range iteratorPrefixes {
		rows = append(rows,
			NameToken{Token: prefix + "in_list", Kind: "lists", Member: "list"}, NameToken{Token: prefix + "in_list", Kind: "variables", Member: "variable"},
			NameToken{Token: prefix + "in_local_list", Kind: "local_variables", Member: "variable"}, NameToken{Token: prefix + "in_global_list", Kind: "global_variables", Member: "variable"})
	}

	byToken := map[string][]NameToken{}
	for _, row := range rows {
		byToken[row.Token] = append(byToken[row.Token], row)
	}
	return rows, byToken
}()

// NameTokensOf is the rows of a token, one per member that holds a name; none when the token makes and reads no name.
func NameTokensOf(token string) []NameToken {
	return nameTokensByToken[token]
}
