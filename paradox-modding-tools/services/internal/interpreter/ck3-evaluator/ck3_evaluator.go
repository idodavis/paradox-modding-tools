package ck3evaluator

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed ck3_objects.json
var ck3ObjectsJSON []byte

// Schema defines how to identify a type of CK3 script object. JSON-driven; no logic beyond interpretation.
type Schema struct {
	Name              string   `json:"name"`
	Paths             []string `json:"paths"`
	KeyPattern        string   `json:"keyPattern"`
	KeyPrefixes       []string `json:"keyPrefixes,omitempty"`
	Attributes        []string `json:"attributes,omitempty"`
	InlineKeyPattern  string   `json:"inlineKeyPattern,omitempty"`
	InlineKeyKeywords []string `json:"inlineKeyKeywords,omitempty"`
}

type schemaFile struct {
	Schemas map[string]Schema `json:"schemas"`
}

var schemas map[string]Schema

// Pattern registry: name → (key, params) → (displayKey, ok). Only place key logic lives.
var matchers map[string]func(key string, p struct{ Keywords, Prefixes []string }) (displayKey string, ok bool)

func init() {
	var f schemaFile
	if err := json.Unmarshal(ck3ObjectsJSON, &f); err != nil {
		panic("ck3-evaluator: load ck3_objects.json: " + err.Error())
	}
	schemas = f.Schemas
	matchers = map[string]func(key string, p struct{ Keywords, Prefixes []string }) (displayKey string, ok bool){
		"numeric": func(key string, _ struct{ Keywords, Prefixes []string }) (string, bool) {
			_, err := strconv.ParseInt(key, 10, 64)
			return key, err == nil
		},
		"prefixed": func(key string, p struct{ Keywords, Prefixes []string }) (string, bool) {
			for _, pre := range p.Prefixes {
				if strings.HasPrefix(key, pre) {
					return key, true
				}
			}
			return "", false
		},
		"namespaced": func(key string, _ struct{ Keywords, Prefixes []string }) (string, bool) {
			return key, namespacedRE.MatchString(key)
		},
		"keyword_prefixed": matcherKeywordPrefixed,
		"identifier_no_dot": func(key string, _ struct{ Keywords, Prefixes []string }) (string, bool) {
			return key, identifierRE.MatchString(key)
		},
		"any": func(key string, _ struct{ Keywords, Prefixes []string }) (string, bool) { return key, true },
	}
}

func matcherKeywordPrefixed(key string, p struct{ Keywords, Prefixes []string }) (string, bool) {
	for _, kw := range p.Keywords {
		if (strings.HasPrefix(key, kw+" ") || (len(key) > len(kw) && strings.HasPrefix(key, kw))) && len(key) > len(kw) {
			displayKey := strings.TrimPrefix(key, kw+" ")
			if displayKey == key {
				displayKey = strings.TrimPrefix(key, kw)
			}
			if displayKey != "" {
				return displayKey, true
			}
		}
	}
	return "", false
}

var (
	namespacedRE = regexp.MustCompile(`^[\w]+\.[\w.]+$`)
	identifierRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// GetSchema returns the schema for a type name, or false if unknown.
func GetSchema(typeName string) (Schema, bool) {
	s, ok := schemas[typeName]
	return s, ok
}

// GetSchemaNames returns all CK3 object type names.
func GetSchemaNames() []string {
	names := make([]string, 0, len(schemas))
	for n := range schemas {
		names = append(names, n)
	}
	return names
}

// ApplicableTypesForPath returns type names whose schema paths match filePath (path-driven, schema-only).
// Matches when filePath is under the schema path, so both "events/foo.txt" and "game/events/foo.txt" match schema "events".
func ApplicableTypesForPath(filePath string) []string {
	norm := filepath.ToSlash(filepath.Clean(filePath))
	pSlash := func(p string) string { return filepath.ToSlash(p) }
	var out []string
	for name, s := range schemas {
		for _, p := range s.Paths {
			q := pSlash(p)
			// Exact or direct prefix (e.g. "events" or "events/foo.txt")
			if norm == q || strings.HasPrefix(norm, q+"/") {
				out = append(out, name)
				break
			}
			// Path contains schema as segment (e.g. "game/events/foo.txt" for schema "events")
			if strings.Contains(norm, "/"+q+"/") || strings.HasSuffix(norm, "/"+q) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// MatchKey returns (displayKey, true) if key matches the schema pattern (inline uses InlineKeyPattern).
// Single-schema classification; use ClassifyKey for multiple types.
func MatchKey(key string, schema *Schema, inline bool) (displayKey string, ok bool) {
	pattern := schema.KeyPattern
	params := struct{ Keywords, Prefixes []string }{schema.InlineKeyKeywords, schema.KeyPrefixes}
	if inline && schema.InlineKeyPattern != "" {
		pattern = schema.InlineKeyPattern
	}
	fn, ok := matchers[pattern]
	if !ok {
		return "", false
	}
	displayKey, ok = fn(key, params)
	if !ok {
		return "", false
	}
	if schema.KeyPattern == "keyword_prefixed" {
		displayKey = strings.TrimSpace(displayKey)
		if displayKey == key || displayKey == "" || !identifierRE.MatchString(displayKey) {
			return "", false
		}
	}
	for _, kw := range schema.InlineKeyKeywords {
		if displayKey == kw {
			return "", false
		}
	}
	return displayKey, true
}

// otherInline returns whether key matches another type's inline pattern (schema-driven).
func otherInline(key, excludeType string) bool {
	for name, s := range schemas {
		if name == excludeType || s.InlineKeyPattern == "" {
			continue
		}
		if _, ok := MatchKey(key, &s, true); ok {
			return true
		}
	}
	return false
}

// ClassifyKey returns (typeName, displayKey, true) if key matches one of applicableTypes (first match).
// Caller uses this from a visitor: ctx.Depth==0 → top-level types, ctx.Depth>0 → inline types.
func ClassifyKey(key string, hasObject bool, applicableTypes []string, inline bool) (typeName, displayKey string, ok bool) {
	for _, t := range applicableTypes {
		schema, has := GetSchema(t)
		if !has || otherInline(key, t) {
			continue
		}
		_, inlineMatch := MatchKey(key, &schema, true)
		needObj := schema.KeyPattern == "keyword_prefixed" || (schema.InlineKeyPattern == "keyword_prefixed" && inlineMatch)
		if needObj && !hasObject {
			continue
		}
		displayKey, matchOk := MatchKey(key, &schema, inline)
		if !matchOk {
			if schema.InlineKeyPattern == "keyword_prefixed" && inlineMatch {
				displayKey, _ = MatchKey(key, &schema, true)
			}
			if displayKey == "" {
				continue
			}
		}
		return t, displayKey, true
	}
	return "", "", false
}

// InlineTypesFor returns type names from the given list that have InlineKeyPattern set (for use when walking objects).
func InlineTypesFor(objectTypes []string) []string {
	var out []string
	for _, t := range objectTypes {
		s, ok := GetSchema(t)
		if ok && s.InlineKeyPattern != "" {
			out = append(out, t)
		}
	}
	return out
}
