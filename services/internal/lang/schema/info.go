// info.go reads the prose a game ships beside its script: CK3's _traits.info, EU5's readme.txt,
// Vic3's buildings.md. All three document a field as a line of script, key = <type or example>
// # prose, live or commented out, or as an EU5 bullet, # key: <type> prose. The comment lines
// directly above a key line are its prose too, as are those directly inside the block it opens
// when no key follows them; a markdown heading naming a key gives it the paragraphs below. What
// a file says before its first key is about the whole kind. Nesting is read from indentation,
// because a readme closes its example braces only sometimes. The result is declared: the learner
// merges a field's type into its accepts, and a card quotes the prose.
package schema

import (
	"cmp"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Info is an install's prose beside its script, by folder from a root, lower-cased: common/traits.
type Info map[string]*FolderInfo

// FolderInfo is what the info files of one folder say.
type FolderInfo struct {
	Prose  string               // about the whole kind: what a file says before its first key, four lines at most
	Fields map[string]InfoField // by key, and by parent.key inside a parent's block; the first file to document one wins
}

// InfoField is what the game says about one field.
type InfoField struct {
	Prose string
	Type  string // the type as the game wrote it: yes/no, int, script value. "" when it wrote an example or nothing
}

// infoLine is what one line says about a key, and where the line was.
type infoLine struct {
	parent, key, prose, typ string
	indent, depth           int  // the column it was written at, and how many blocks it is inside
	section, brief          bool // a markdown heading owns the paragraphs below; a brief header, the comment lines below
}

// infoMaxBytes keeps data out: EU5 ships 5 MB .info terrain caches; real prose stays under 25 KB.
const infoMaxBytes = 1 << 20

// ReadInfo reads every info file of an inventory.
func ReadInfo(files []source.File) Info {
	info := Info{}
	for _, file := range files {
		if stat, err := os.Stat(file.Path); file.Format != source.Info || err != nil || stat.Size() > infoMaxBytes {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}

		folder := strings.ToLower(path.Dir(file.Rel))
		if info[folder] == nil {
			info[folder] = &FolderInfo{Fields: map[string]InfoField{}}
		}
		info[folder].read(text, strings.HasSuffix(strings.ToLower(file.Rel), ".md"))
	}
	return info
}

// Kind is what the game says about the kind whose definitions are in folders.
func (info Info) Kind(folders []string) string {
	for _, folder := range folders {
		if fi := info[strings.ToLower(folder)]; fi != nil && fi.Prose != "" {
			return fi.Prose
		}
	}
	return ""
}

// Field is what the game says about key inside parent's block, else about key anywhere. parent is
// "" for a definition's own member.
func (info Info) Field(folders []string, parent, key string) InfoField {
	for _, folder := range folders {
		if fi := info[strings.ToLower(folder)]; fi != nil {
			if field, ok := fi.Fields[parent+"."+key]; ok && parent != "" {
				return field
			}
			if field, ok := fi.Fields[key]; ok {
				return field
			}
		}
	}
	return InfoField{}
}

// read adds what one file says. markdown makes a # at column 0 a heading.
func (fi *FolderInfo) read(text string, markdown bool) {
	var pending []string   // prose waiting for the key below it
	var parents []infoLine // the blocks the line is inside, by indentation
	var open *infoLine     // a line whose prose may still follow: a block's opener, a brief header, a markdown heading
	var kindProse []string
	kindDone, own := fi.Prose != "", map[string]int{}

	// settle records the open line; the waiting prose is its own unless a key line claims it
	settle := func(claimed bool) {
		if open != nil && !claimed {
			open.prose = joinProse([]string{open.prose, joinProse(pending)})
		}
		if open != nil {
			fi.add(*open, own)
		}
		if open = nil; !claimed {
			pending = nil
		}
	}

	for raw := range strings.SplitSeq(text, "\n") {
		body, hashed := jomini.PeelComment(raw)
		indent := 0
		for _, char := range raw {
			if char == '\t' {
				indent += 4
			} else if char == ' ' || char == '#' {
				indent++
			} else {
				break
			}
		}
		inSection := open != nil && open.section

		// a markdown heading names a key, or ends whatever came before it
		if markdown && strings.HasPrefix(raw, "#") {
			settle(false)
			kindDone = kindDone || len(kindProse) > 0
			if jomini.IsLowerName(body) {
				open = &infoLine{key: body, section: true}
			}
			continue
		}

		// a blank line and a lone # end waiting prose; under a heading they only part its paragraphs
		if body == "" {
			kindDone = kindDone || len(kindProse) > 0
			if inSection {
				pending = append(pending, "")
			} else {
				settle(false)
			}
			continue
		}

		// rules (=== x ===), fences, closing braces and <name> = { openers are neither key nor prose
		if strings.HasPrefix(body, "```") || strings.HasPrefix(body, "}") || strings.HasPrefix(body, "<") || strings.HasPrefix(body, "==") || strings.HasSuffix(body, "==") {
			if !inSection {
				settle(false)
			}
			continue
		}

		// a heading (ATTRIBUTES, Valid categories:) covers the lines below as a whole: no key's prose
		line, opens, isKey := infoKeyLine(body, hashed)
		if !isKey && (strings.HasSuffix(body, ":") || !strings.ContainsFunc(body, unicode.IsLower)) {
			if !inSection {
				settle(false)
			}
			continue
		}
		if !isKey {
			// prose: about the kind before the first key, and waiting for the key below; under a
			// heading only what is written at column 0
			if !kindDone && len(kindProse) < 4 && !strings.HasPrefix(body, "- ") {
				kindProse = append(kindProse, body)
			}
			if !inSection || indent == 0 {
				pending = append(pending, body)
			}
			continue
		}
		kindDone = true
		settle(!inSection)

		// the key's parent is the nearest block opened at a smaller indentation
		for len(parents) > 0 && indent <= parents[len(parents)-1].indent {
			parents = parents[:len(parents)-1]
		}
		if len(parents) > 0 {
			line.parent = parents[len(parents)-1].key
		}
		line.prose, line.indent, line.depth, pending = joinProse(append(pending, line.prose)), indent, len(parents), nil
		if opens || line.brief {
			open = &line
		} else {
			fi.add(line, own)
		}
		if opens {
			parents = append(parents, line)
		}
	}
	settle(false)
	if fi.Prose == "" {
		fi.Prose = joinProse(kindProse)
	}
}

// add records what a line says about a key, under its parent and on its own, filling only the
// empty parts of what is recorded. On its own a key is its shallowest mention in the file, so an
// event's type is not the type of a widget inside it. own is the depth at which this file first
// recorded each key.
func (fi *FolderInfo) add(line infoLine, own map[string]int) {
	if depth, mine := own[line.key]; mine && line.depth < depth {
		delete(fi.Fields, line.key)
	}
	if _, seen := fi.Fields[line.key]; !seen {
		own[line.key] = line.depth
	}

	names := []string{line.key}
	if line.parent != "" {
		names = []string{line.parent + "." + line.key, line.key}
	}
	for _, name := range names {
		field := fi.Fields[name]
		field.Prose = cmp.Or(field.Prose, line.prose)
		field.Type = cmp.Or(field.Type, line.typ)
		fi.Fields[name] = field
	}
}

// infoKeyLine reads a line, comment marks peeled, when it documents a key: key = value # prose,
// key = <type> prose, key = { <prose> }, - key: prose, key: <type> prose (in a comment, with a
// type or three words, so "scope: character" stays prose), and CK3's brief: key ( type ). opens
// reports a block left open on the line.
func infoKeyLine(body string, hashed bool) (line infoLine, opens, ok bool) {
	if len(body) > 6 && strings.EqualFold(body[:6], "brief:") {
		key, typ, _ := strings.Cut(body[6:], "(")
		line.key, line.typ, line.brief = strings.TrimSpace(key), strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typ), ")")), true
		return line, false, jomini.IsDefName(line.key)
	}

	// the key, then = or :
	text, bullet := strings.CutPrefix(body, "- ")
	text = strings.TrimSpace(text)
	end := strings.IndexFunc(text+" ", func(char rune) bool {
		return !(char == '_' || char == '.' || char == '-' || unicode.IsLetter(char) || unicode.IsDigit(char))
	})
	key, rest := strings.TrimRight(text[:end], ".-"), strings.TrimSpace(text[end:])
	if !jomini.IsDefName(key) || unicode.IsDigit(rune(key[0])) || rest == "" || (rest[0] != '=' && rest[0] != ':') || strings.HasPrefix(rest, "==") {
		return line, false, false
	}

	// a colon makes a key only of a lower-case word, and outside a bullet only in a comment with
	// a type or three words behind it
	colon := rest[0] == ':'
	rest = strings.TrimSpace(rest[1:])
	if colon && (!jomini.IsLowerName(key) || (!bullet && (!hashed || (len(strings.Fields(rest)) < 3 && !strings.HasPrefix(rest, "<"))))) {
		return line, false, false
	}

	// the value, and the comment behind it
	value, comment, _ := strings.Cut(rest, "#")
	value, comment = strings.TrimSpace(value), strings.TrimSpace(strings.TrimLeft(comment, "# "))
	typ, prose := "", ""
	switch {
	case strings.HasPrefix(value, "<"):
		// <type> prose
		if declared, rest, closed := strings.Cut(value[1:], ">"); closed && len(declared) <= 120 {
			typ, prose = declared, rest
		}
	case strings.HasPrefix(value, "{"):
		// { <type> }, { <prose }, or prose behind the closing brace
		inner := strings.TrimSpace(value[1:])
		if i := strings.LastIndexByte(inner, '}'); i >= 0 {
			inner, prose = strings.TrimSpace(inner[:i]), inner[i+1:]
		}
		if declared, _, closed := strings.Cut(strings.TrimPrefix(inner, "<"), ">"); closed && strings.HasPrefix(inner, "<") && len(declared) <= 120 {
			typ = declared
		} else if said := strings.TrimSpace(strings.Trim(inner, "<>")); strings.TrimSpace(prose) == "" && !strings.ContainsAny(said, "={<") {
			prose = said
		}
	case colon:
		prose = value
	default:
		// a bare value is a type only when it is a type word; anything else is an example
		if _, isType := (InfoField{Type: value}).Declared(); (isType && !strings.Contains(value, "/")) || value == "yes/no" {
			typ = value
		}
	}

	prose = strings.TrimSpace(strings.TrimSpace(prose) + " " + comment)
	if !strings.ContainsFunc(prose, unicode.IsLetter) {
		prose = ""
	}
	line.key, line.typ, line.prose = key, strings.TrimSpace(typ), prose
	return line, strings.Count(value, "{") > strings.Count(value, "}"), true
}

// joinProse joins lines with a space and paragraphs, marked by an empty line, with a blank line.
func joinProse(lines []string) string {
	var out strings.Builder
	for _, line := range lines {
		switch {
		case line == "":
			out.WriteString("\n\n")
		case out.Len() > 0 && !strings.HasSuffix(out.String(), "\n"):
			out.WriteString(" " + line)
		default:
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Declared is the field's type as an accept, when it is one of the few that say what a value is:
// bool, yes/no; int, integer, num, number, float, double, fixed_point; string; localization_key,
// loc key; and a/b/c, a closed set of bare words. "optional" and the like are dropped and "list
// of" names the same type, since how many values a field takes is its class. Anything else (a
// trigger, a database key, a sentence) says nothing a value can be checked against: it declines.
func (infoField InfoField) Declared() (Accept, bool) {
	var parts []string
	for part := range strings.SplitSeq(strings.ToLower(infoField.Type), ",") {
		if part = strings.TrimSpace(part); !slices.Contains([]string{"optional", "situational", "required", "opt"}, part) {
			parts = append(parts, part)
		}
	}
	word := strings.Join(parts, ", ")
	for _, plural := range []string{"array of ", "list of "} {
		word = strings.TrimPrefix(word, plural)
	}

	switch word {
	case "bool", "yes/no":
		return Accept{Type: TypeBool, Declared: true}, true
	case "int", "integer", "num", "number", "float", "double", "fixed_point":
		return Accept{Type: TypeNumber, Declared: true}, true
	case "string":
		return Accept{Type: TypeString, Declared: true}, true
	case "localization_key", "loc key":
		return Accept{Type: TypeLoc, Declared: true}, true
	}

	// a closed set, a/b/c or a|b|c: two to twelve bare lower-case words, none longer than 24
	words := strings.FieldsFunc(word, func(char rune) bool { return char == '/' || char == '|' })
	ok := len(words) >= 2 && len(words) <= 12
	for _, word := range words {
		ok = ok && len(word) <= 24 && jomini.IsLowerName(word)
	}
	if !ok {
		return Accept{}, false
	}
	return Accept{Type: TypeEnum, Enum: words, Declared: true}, true
}
