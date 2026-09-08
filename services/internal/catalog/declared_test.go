// declared_test.go pins what the games' own type words are taken to mean. Every
// string here was measured out of a shipped install; none is invented.

package catalog

import (
	"slices"
	"testing"
)

func TestClassifyDeclaredType(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		values    []string
		prim      string
		array     bool
	}{
		// Closed sets, which are the half a diagnostic can judge.
		{"eu5 yes/no", "yes/no", []string{"yes", "no"}, "", false},
		{"eu5 four-way", "none/mutual/giving/receiving",
			[]string{"none", "mutual", "giving", "receiving"}, "", false},
		{"eu5 relation type", "diplomacy/subject/union",
			[]string{"diplomacy", "subject", "union"}, "", false},

		// Primitives. Whole and decimal stay apart on purpose: only a
		// distinction can ever say 2.5 does not belong in an integer field.
		{"ck3 bool", "bool", nil, "yes/no value", false},
		{"ck3 bool optional", "bool, optional", nil, "yes/no value", false},
		{"eu5 float", "float", nil, "decimal number", false},
		{"eu5 int", "int", nil, "whole number", false},
		{"eu5 integer", "integer", nil, "whole number", false},
		{"ck3 num", "num", nil, "whole number", false},
		{"eu5 string", "string", nil, "text value", false},
		{"ck3 loc key", "localization_key", nil, "localization key", false},
		{"vic3 loc key spaced", "loc key", nil, "localization key", false},

		// Declined: script_docs already declares these and names the database
		// rather than a word, so answering here would rank a weaker source over
		// a stronger one.
		{"ck3 trigger", "trigger", nil, "", false},
		{"ck3 effect", "effect", nil, "", false},
		{"eu5 script value", "script value", nil, "", false},
		{"ck3 mtth", "mtth", nil, "", false},

		// Declined: names no particular database.
		{"ck3 database key", "database key", nil, "", false},
		{"ck3 database key optional", "database key, optional", nil, "", false},
		{"ck3 enum", "enum", nil, "", false},

		// Declined: holds a loc key but also accepts a first_valid block, so
		// the plain phrase would teach a rule that is too narrow.
		{"ck3 dynamic desc", "dynamic desc", nil, "", false},
		{"ck3 dynamic description", "dynamic description, optional", nil, "", false},

		// Prose wearing the same brackets. These are the reason the allow-list
		// exists rather than a shape rule.
		{"eu5 prose", "name of global variable list with the list of candidates in",
			nil, "", false},
		{"vic3 placeholder", "locator_name_1", nil, "", false},
		{"prose with a slash", "the giver/receiver of the relation", nil, "", false},

		// Array carries through even where the element type declines, so a
		// later addition to declaredPrim gets the plural for free.
		{"ck3 array of keys", "array of database keys", nil, "", true},
		{"list of ints", "list of int", nil, "whole number", true},

		{"empty", "", nil, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyDeclaredType(tt.raw)
			if !slices.Equal(got.Values, tt.values) {
				t.Errorf("Values = %v, want %v", got.Values, tt.values)
			}
			if got.Prim != tt.prim {
				t.Errorf("Prim = %q, want %q", got.Prim, tt.prim)
			}
			if got.Array != tt.array {
				t.Errorf("Array = %v, want %v", got.Array, tt.array)
			}
			if want := len(tt.values) > 0 || tt.prim != ""; got.Known() != want {
				t.Errorf("Known() = %v, want %v", got.Known(), want)
			}
		})
	}
}
