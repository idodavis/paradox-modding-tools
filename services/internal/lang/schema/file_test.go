package schema

import (
	"reflect"
	"strings"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	want := sample()
	text := Write(want)
	got, err := Read(text)
	if err != nil {
		t.Fatalf("Read: %v\n%s", err, text)
	}

	// the lookups are built on use and are no part of the value, so the exported fields are compared
	if got.Game != want.Game || got.Version != want.Version || !reflect.DeepEqual(got.Drift, want.Drift) {
		t.Errorf("header or drift changed: %+v %+v", got, got.Drift)
	}
	for name, k := range want.Kinds {
		if !reflect.DeepEqual(got.Kinds[name], k) {
			t.Errorf("kind %s changed:\n got %+v\nwant %+v", name, got.Kinds[name], k)
		}
	}
	if len(got.Kinds) != len(want.Kinds) || !reflect.DeepEqual(got.Arguments, want.Arguments) {
		t.Errorf("kinds %d, want %d; arguments %+v", len(got.Kinds), len(want.Kinds), got.Arguments)
	}
	if again := Write(got); again != text {
		t.Errorf("a second write differs from the first:\n%s", again)
	}
}

func TestWriteLayout(t *testing.T) {
	text := Write(sample())
	for _, line := range []string{
		"schema = {\n\tgame = ck3\n\tversion = 1.19.0.6\n}\n",
		"\tparse_errors = {\n\t\t\"common/bad file.txt\" = 2\n\t}\n",
		"history/titles = {\n\tfolders = { history/titles }\n",
		"\tsubject = landed_titles\n",
		"map_data.definition = {\n\tformat = table\n\tfolders = { map_data/definition.csv }\n\tnaming = {\n\t\tform = column\n\t\tcolumn = 0\n\t}\n\tprefixes = { province }\n\tscope_type = province\n",
		"\t\tform = member\n\t\tkeyword = pdxmesh\n\t\tmember = name\n",
		"\tloc = {\n\t\t\"setting_{id}\" = 1\n\t\t\"setting_{id}_desc\" = 0.9743589743589743\n\t}\n",
		"\tfiles = {\n\t\t\"gfx/interface/icons/traits/{id}.dds\" = 0.93\n\t}\n",
		"\tmacro = { effect }\n\tparams = yes\n",
		"\t\t\t\"<date>\" = {\n",
		"\t\t\t\t\t\tops = { \">=\" \"?=\" }\n",
		"\t\t\t\t\t\tbody = { effect math }\n\t\t\t\t\t\tbody_scope = landed_title\n",
		"\t\t\t\taccepts = {\n\t\t\t\t\tenum = {\n\t\t\t\t\t\tcount = 0\n\t\t\t\t\t\twords = { blocks_achievements \"two words\" }\n\t\t\t\t\t}\n\t\t\t\t\tbool = 4\n\t\t\t\t\tnumber = 2\n\t\t\t\t}\n\t\t\t\tdeclared = { enum bool }\n",
		"\t\t\t\t\tref:game_rule_categories = 243\n",
		"\t\tdefines = game_rules.setting\n",
		"\t\t\t\tkeyed_by = goods\n",
		"arguments = {\n\tadd_trait = {\n",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the file does not hold %q\n%s", line, text)
		}
	}

	// kinds come in name order, and a script kind writes no format line
	if a, b := strings.Index(text, "\ngame_rules = {"), strings.Index(text, "\ngame_rules.setting = {"); a < 0 || b < a {
		t.Errorf("kinds are not in name order: %d %d", a, b)
	}
	if strings.Contains(text, "format = script") {
		t.Error("a script kind wrote its format")
	}
}

func TestWriteEmpty(t *testing.T) {
	text := Write(&Schema{})
	if text != "schema = {\n\tgame = \"\"\n\tversion = \"\"\n}\ndrift = {\n}\narguments = {\n}\n" {
		t.Errorf("an empty schema wrote %q", text)
	}
	s, err := Read(text)
	if err != nil || len(s.Kinds) != 0 || len(s.Arguments) != 0 {
		t.Errorf("Read of an empty schema = %+v, %v", s, err)
	}
}

func TestWord(t *testing.T) {
	for text, want := range map[string]string{
		"traits": "traits", "game_rules.setting": "game_rules.setting", "history/titles": "history/titles", "ref:characters": "ref:characters",
		"define:NGame|START": "define:NGame|START", "1.19.0.6": "1.19.0.6", "x-y": "x-y", "Linnéa": "Linnéa",
		"": `""`, "<date>": `"<date>"`, ">=": `">="`, "?=": `"?="`, "trait_{id}": `"trait_{id}"`, "two words": `"two words"`, "a#b": `"a#b"`, "a=b": `"a=b"`, "@x": `"@x"`, "$X$": `"$X$"`,
	} {
		if got := word(text); got != want {
			t.Errorf("word(%q) = %s, want %s", text, got, want)
		}
	}
}

func TestReadRefuses(t *testing.T) {
	head := "schema = {\n\tgame = ck3\n\tversion = 1\n}\ndrift = {\n}\n"
	for name, text := range map[string]string{
		"an empty file":             "",
		"a file that is no schema":  "traits = {\n}\nbrave = {\n}\nx = {\n}\n",
		"no arguments block":        head + "traits = {\n\tnaming = {\n\t\tform = key\n\t}\n}\n",
		"an unclosed brace":         head + "traits = {\narguments = {\n}\n",
		"an unknown kind key":       head + "traits = {\n\tcolour = red\n}\narguments = {\n}\n",
		"an unknown naming form":    head + "traits = {\n\tnaming = {\n\t\tform = guess\n\t}\n}\narguments = {\n}\n",
		"an unknown class":          head + "traits = {\n\tshape = {\n\t\tcount = 1\n\t\tclass = heap\n\t}\n}\narguments = {\n}\n",
		"an unknown value type":     head + "traits = {\n\tshape = {\n\t\tcount = 1\n\t\taccepts = {\n\t\t\tsomething = 1\n\t\t}\n\t}\n}\narguments = {\n}\n",
		"an unknown body class":     head + "traits = {\n\tmacro = { spell }\n}\narguments = {\n}\n",
		"an unknown drift section":  "schema = {\n\tgame = ck3\n\tversion = 1\n}\ndrift = {\n\tweather = {\n\t\tx = 1\n\t}\n}\narguments = {\n}\n",
		"a count that is no number": head + "arguments = {\n\tadd_gold = {\n\t\tcount = many\n\t}\n}\n",
		"a bare word among kinds":   head + "traits\narguments = {\n}\n",
	} {
		if s, err := Read(text); err == nil {
			t.Errorf("%s was read: %+v", name, s)
		}
	}

	// the error names the line
	_, err := Read(head + "traits = {\n\tcolour = red\n}\narguments = {\n}\n")
	if err == nil || !strings.Contains(err.Error(), "line 8") {
		t.Errorf("the error does not name line 8: %v", err)
	}
}
