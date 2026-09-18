// catalog_test.go verifies the shipped-documentation reader.
//
// Test data lives here, never in catalog.go: docShapeChecks pairs each row of
// DocShapes with a sample the parser must handle and a pattern that finds the
// shape in a real install. The rows themselves carry only the facts.

package game

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docShapeCheck is how one DocShapes row is verified, keyed by its Summary.
type docShapeCheck struct {
	// sample is verbatim from an install; ParseDocFile must return doc for key.
	sample, key, doc string
	// detect matches ONE line written in this shape, for the attribution scan.
	// Nil where a single line cannot identify it.
	detect *regexp.Regexp
}

var docShapeChecks = map[string]docShapeCheck{
	"comment ABOVE the key": {
		sample: "# The trait category\ncategory = personality\n",
		key:    "category", doc: "trait category",
		// No detect: this is two lines, and a comment above a key looks like
		// every game's plain prose, so nothing single-line identifies it.
	},
	"comment TRAILING the key, same line": {
		sample: "#\ttype = country_event\t\t# Defines the scope\n",
		key:    "type", doc: "Defines the scope",
		detect: regexp.MustCompile(`=\s*[^#\n]*\S\s+#\s*\S`),
	},
	"`# - key: <type> desc` attribute bullet": {
		sample: "# ATTRIBUTES\n# - build_time: <integer> building time in days\n",
		key:    "build_time", doc: "building time",
		detect: regexp.MustCompile(`(?m)^\s*#\s*-\s*[A-Za-z_][A-Za-z0-9_]*\s*:`),
	},
	"`key = <type> desc`, no hash between type and prose": {
		sample: "#<name> = {\n" +
			"#\tbreak_on_war = <yes/no> ends the relationship on war\n#}\n",
		key: "break_on_war", doc: "ends the relationship on war",
		detect: regexp.MustCompile(`=\s*<[^<>]+>\s*\S`),
	},
	"`key = { <prose> }`, the shape documented inside its own braces": {
		sample: "#\tshow_if = { <trigger to see if this stage is needed}\n",
		key:    "show_if", doc: "trigger to see if this stage is needed",
		detect: regexp.MustCompile(`(?m)=\s*\{\s*<[^=\n]*$`),
	},
	"`### brief: name ( type )` header, prose, then a lone `#`": {
		sample: "### brief: create_cadet_branches ( bool )\n" +
			"# Should rulers be able to create cadet branches\n" +
			"#\ncreate_cadet_branches = no\n",
		key: "create_cadet_branches", doc: "Should rulers be able to create cadet",
		detect: regexp.MustCompile(`(?m)^\s*#+\s*brief\s*:`),
	},
}

// TestDocShapes covers DocShapes in isolation and against the real installs.
func TestDocShapes(t *testing.T) {
	// Every listed shape must parse, and every listed shape must be checked.
	t.Run("parse", func(t *testing.T) {
		if len(DocShapes) == 0 {
			t.Fatal("DocShapes is empty")
		}
		for _, s := range DocShapes {
			t.Run(s.Summary, func(t *testing.T) {
				c, ok := docShapeChecks[s.Summary]
				if !ok {
					t.Fatalf("no docShapeChecks case; add one when adding a row")
				}
				if len(s.Games) == 0 {
					t.Error("names no games")
				}
				for _, id := range s.Games {
					if Get(id) == nil {
						t.Errorf("names game %q, which the registry does not have", id)
					}
				}
				docs, _, _ := ParseDocFile(c.sample)
				if !strings.Contains(docs[c.key], c.doc) {
					t.Errorf("ParseDocFile()[%q] = %q, want substring %q",
						c.key, docs[c.key], c.doc)
				}
			})
		}
		for summary := range docShapeChecks {
			found := false
			for _, s := range DocShapes {
				if s.Summary == summary {
					found = true
				}
			}
			if !found {
				t.Errorf("docShapeChecks has %q, which DocShapes does not list", summary)
			}
		}
	})

	// The Games column against the installs: listed for a game must appear there, and unlisted must not.
	t.Run("attribution", func(t *testing.T) {
		installs := map[string]string{
			"ck3":  os.Getenv("PMT_TEST_INSTALL_CK3"),
			"vic3": os.Getenv("PMT_TEST_INSTALL_VIC3"),
			"eu5":  os.Getenv("PMT_TEST_INSTALL_EU5"),
		}
		seen := map[string]map[string]bool{} // game -> shape summary -> found
		for id, root := range installs {
			if root == "" {
				continue
			}
			seen[id] = map[string]bool{}
			_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !IsDocFile(d.Name()) {
					return nil
				}
				raw, err := os.ReadFile(p)
				if err != nil {
					return nil
				}
				for summary, c := range docShapeChecks {
					if c.detect != nil && c.detect.Match(raw) {
						seen[id][summary] = true
					}
				}
				return nil
			})
		}
		if len(seen) == 0 {
			corpusEnv(t, "set PMT_TEST_INSTALL_{CK3,VIC3,EU5} to check attribution")
		}
		for _, s := range DocShapes {
			if docShapeChecks[s.Summary].detect == nil {
				continue
			}
			listed := map[string]bool{}
			for _, g := range s.Games {
				listed[g] = true
			}
			for id := range seen {
				switch {
				case listed[id] && !seen[id][s.Summary]:
					t.Errorf("%q lists %s, but no %s doc file writes it", s.Summary, id, id)
				case !listed[id] && seen[id][s.Summary]:
					t.Errorf("%q does not list %s, but %s doc files write it", s.Summary, id, id)
				}
			}
		}
	})
}

// TestParseDocFile covers the reader's behaviour beyond one line per shape:
// which comment attaches to which key, and where a block ends.
func TestParseDocFile(t *testing.T) {
	tests := []struct {
		name, src, key, want string
	}{
		{
			name: "below block comment",
			src:  "allow = {\n\t# must also pass category\n}\n",
			key:  "allow", want: "must also pass",
		},
		{
			name: "below does not steal next above",
			src: "# Event presentation type\ntype = character_event\n" +
				"# Dynamic loc key\ntitle = loc_key\n",
			key: "title", want: "Dynamic",
		},
		{
			name: "inline plus below block",
			src:  "chance = { # weight\n\t# extra lines\n}\n",
			key:  "chance", want: "weight extra lines",
		},
		{
			// A lone `#` separates prose from the key it describes; a genuinely
			// blank line ends the block.
			name: "blank line ends the block",
			src:  "# File header prose about the whole format\n\nsome_key = 1\n",
			key:  "some_key", want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := ParseDocFile(tt.src)
			// want == "" means "no prose attaches to this key", which is a rule
			// in its own right — Contains(s, "") is always true, so the one row
			// asserting a negative was the one row this loop could not check.
			if tt.want == "" {
				if got[tt.key] != "" {
					t.Fatalf("ParseDocFile()[%q] = %q, want no prose at all",
						tt.key, got[tt.key])
				}
				return
			}
			if !strings.Contains(got[tt.key], tt.want) {
				t.Fatalf("ParseDocFile()[%q] = %q, want substring %q (all=%v)",
					tt.key, got[tt.key], tt.want, got)
			}
		})
	}
	t.Run("prose above survives the key below it", func(t *testing.T) {
		got, _, _ := ParseDocFile(
			"# Event presentation type\ntype = character_event\n" +
				"# Dynamic loc key\ntitle = loc_key\n",
		)
		if !strings.Contains(got["type"], "presentation") {
			t.Fatalf("type = %q, want presentation", got["type"])
		}
	})
}

// corpusEnv skips when the real-install tier was not requested, and FAILS when it was requested but the environment cannot serve it.
func corpusEnv(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PMT_TEST_TIER") == "full" {
		t.Fatalf("PMT_TEST_TIER=full but "+format, args...)
	}
	t.Skipf(format, args...)
}
