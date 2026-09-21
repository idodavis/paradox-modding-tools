package schema

import (
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/source"
)

// sample is one schema holding every field of every type, and every spelling the file writer must quote.
func sample() *Schema {
	return &Schema{
		Game:    "ck3",
		Version: "1.19.0.6",
		Kinds: map[string]*Kind{
			"game_rules": {
				Name: "game_rules", Format: source.Script, Folders: []string{"common/game_rules"}, Naming: Naming{Form: "key"},
				LocTemplates: []loc.Convention{{Template: "rule_{id}", Coverage: 1}},
				Shape: &Field{Count: 81, Class: ClassStructure, Defines: "game_rules.setting", Members: map[string]*Field{
					"categories": {Name: "categories", Count: 81, Class: ClassList, Accepts: []Accept{{Type: TypeRef, Kind: "game_rule_categories", Count: 243}}},
					"default":    {Name: "default", Count: 81, Class: ClassValue, Accepts: []Accept{{Type: TypeRef, Kind: "game_rules.setting", Count: 81}}},
				}},
			},
			"game_rules.setting": {
				Name: "game_rules.setting", Format: source.Script, Parent: "game_rules", Naming: Naming{Form: "key"},
				LocTemplates: []loc.Convention{{Template: "setting_{id}", Coverage: 1}, {Template: "setting_{id}_desc", Coverage: 38.0 / 39.0}},
				Shape: &Field{Count: 312, Class: ClassStructure, Members: map[string]*Field{
					"flag": {Name: "flag", Count: 36, Class: ClassValue, Accepts: []Accept{
						{Type: TypeEnum, Enum: []string{"blocks_achievements", "two words"}, Count: 0, Declared: true},
						{Type: TypeBool, Count: 4, Declared: true},
						{Type: TypeNumber, Count: 2},
					}},
				}},
			},
			"history/titles": {
				Name: "history/titles", Format: source.Script, Folders: []string{"history/titles"}, Naming: Naming{Form: "key"}, Subject: "landed_titles",
				Shape: &Field{Count: 5901, Class: ClassStructure, Members: map[string]*Field{
					"<date>": {Name: "<date>", Count: 92364, Class: ClassStructure, Members: map[string]*Field{
						"holder": {Name: "holder", Count: 60112, Class: ClassValue, Ops: []string{">=", "?="}, Accepts: []Accept{{Type: TypeRef, Kind: "characters", Count: 58240}, {Type: TypeNumber, Count: 1872}}},
						"effect": {Name: "effect", Count: 12, Class: ClassBody, Body: BodyEffect | BodyMath, BodyScope: "landed_title"},
					}},
				}},
			},
			"map_data.definition": {
				Name: "map_data.definition", Format: source.Table, Folders: []string{"map_data/definition.csv"}, Naming: Naming{Form: "column", Column: 0},
				Prefixes: []string{"province"}, ScopeType: "province",
				Shape: &Field{Count: 9000, Class: ClassStructure, Members: map[string]*Field{"<4>": {Name: "<4>", Count: 9000, Class: ClassValue, Accepts: []Accept{{Type: TypeString, Count: 9000}}}}},
			},
			"models.pdxmesh": {
				Name: "models.pdxmesh", Format: source.Script, Folders: []string{"gfx/models"}, Naming: Naming{Form: "member", Keyword: "pdxmesh", Member: "name"},
				Shape: &Field{Count: 2344, Class: ClassStructure, Members: map[string]*Field{"file": {Name: "file", Count: 2344, Class: ClassValue, Accepts: []Accept{{Type: TypeFile, Count: 2344}}}}},
			},
			"models.entity": {Name: "models.entity", Format: source.Script, Folders: []string{"gfx/models"}, Naming: Naming{Form: "member", Keyword: "entity", Member: "name"}},
			"saved_scopes":  {Name: "saved_scopes", Format: source.Script, Naming: Naming{Form: "token"}, Prefixes: []string{"scope"}},
			"scripted_effects": {
				Name: "scripted_effects", Format: source.Script, Folders: []string{"common/scripted_effects"}, Naming: Naming{Form: "key", Keyword: "scripted_effect"},
				Macro: BodyEffect, Params: true, Shape: &Field{Count: 900, Class: ClassBody, Body: BodyEffect},
			},
			"traits": {
				Name: "traits", Format: source.Script, Folders: []string{"common/traits"}, Naming: Naming{Form: "key"}, Prefixes: []string{"trait"}, ScopeType: "trait", LabelMember: "name",
				FileTemplates: []FileTemplate{{Pattern: "gfx/interface/icons/traits/{id}.dds", Coverage: 0.93}},
				Shape:         &Field{Count: 600, Class: ClassStructure, Members: map[string]*Field{"goods": {Name: "goods", Count: 3, Class: ClassDatabase, KeyedBy: "goods"}}},
			},
		},
		Arguments: map[string]*Field{
			"add_trait": {Name: "add_trait", Count: 4100, Class: ClassValue, Accepts: []Accept{{Type: TypeRef, Kind: "traits", Count: 4000}, {Type: TypeScopePath, Count: 100}}},
			"if":        {Name: "if", Count: 90000, Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": {Name: "limit", Count: 89000, Class: ClassBody, Body: BodyTrigger}}},
		},
		Drift: Drift{
			ParseErrors: []Tally{{Name: "common/bad file.txt", Count: 2}},
			Prefixes:    []Tally{{Name: "oddity", Count: 7}, {Name: "other", Count: 1}},
			NameTokens:  []Tally{{Name: "set_thing", Count: 3}},
		},
	}
}

func TestKindField(t *testing.T) {
	k := sample().Kinds["history/titles"]
	if f := k.Field("<date>", "holder"); f == nil || f.Count != 60112 {
		t.Errorf(`Field("<date>", "holder") = %+v`, f)
	}
	if f := k.Field(); f != k.Shape {
		t.Error("Field() is not the shape")
	}
	if f := k.Field("<date>", "missing", "deeper"); f != nil {
		t.Errorf("a path the install never wrote gave %+v", f)
	}
	if f := sample().Kinds["models.entity"].Field("file"); f != nil {
		t.Errorf("a kind with no shape gave %+v", f)
	}
}

func TestLookups(t *testing.T) {
	s := sample()
	names := func(kinds []*Kind) []string {
		var out []string
		for _, k := range kinds {
			out = append(out, k.Name)
		}
		return out
	}

	for _, c := range []struct {
		rel  string
		want []string
	}{
		{"common/traits/00_traits.txt", []string{"traits"}},
		{"common/traits/my_mod/more/x.txt", []string{"traits"}},
		{"Common/Traits/X.TXT", []string{"traits"}},
		{"gfx/models/units/horse/horse.asset", []string{"models.entity", "models.pdxmesh"}},
		{"map_data/definition.csv", []string{"map_data.definition"}},
		{"map_data/adjacencies.csv", nil},
		{"common/x.txt", nil},
		{"x.txt", nil},
		{"", nil},
	} {
		if got := names(s.KindsAt(c.rel)); !slices.Equal(got, c.want) {
			t.Errorf("KindsAt(%q) = %v, want %v", c.rel, got, c.want)
		}
	}

	if got := names(s.ByPrefix("trait")); !slices.Equal(got, []string{"traits"}) {
		t.Errorf(`ByPrefix("trait") = %v`, got)
	}
	if got := s.ByPrefix("flag"); got != nil {
		t.Errorf(`ByPrefix("flag") = %v`, names(got))
	}
	if k := s.ByKeyword("scripted_effect"); k == nil || k.Name != "scripted_effects" {
		t.Errorf(`ByKeyword("scripted_effect") = %+v`, k)
	}
	if k := s.ByKeyword("nothing"); k != nil {
		t.Errorf(`ByKeyword("nothing") = %+v`, k)
	}
}
