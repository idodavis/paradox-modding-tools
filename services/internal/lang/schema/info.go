// info.go reads the prose a game ships beside its script: CK3's _traits.info, EU5's readme.txt, Vic3's buildings.md. Measured over the three installs on 2026-09-20 (372 files), they document a field the same way, as a line of script: key = <type or example> # prose. The line is live in CK3 and in Vic3's indented markdown, commented out in EU5, and EU5 also writes it as a bullet, # key: <type> prose. So there is one line reader: a key line gives the field its type and the prose behind it, the comment lines directly above it are its prose too, and so are the comments directly inside the block it opens when no key follows them. A markdown heading that names a key gives that key the paragraphs below it. What the file says before its first key is about the whole kind. Nesting is read from indentation, because a readme closes its example braces only sometimes. The result is declared, like the vocabulary: the learner merges a field's type into its accepts, and a card quotes the prose.
package schema

import (
	"os"
	"path"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/source"
)

// Info is the prose an install ships beside its script, by the folder the info file sits in: from a root, lower-cased, common/traits.
type Info map[string]*FolderInfo

// FolderInfo is what the info files of one folder say.
type FolderInfo struct {
	Prose  string               // about the whole kind: what a file says before its first key, four lines at most
	Fields map[string]InfoField // by key, and by parent.key for a key written inside a parent's block; the first to document one wins
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
	section, brief          bool // a markdown heading, whose every paragraph below is the key's; a brief header, whose comment lines below are
}

// infoMaxBytes keeps data out: EU5's terrain cache is four .info files of 5 to 6 MB, and the largest file of prose in the three installs is 24 KB.
const infoMaxBytes = 1 << 20

// ReadInfo reads every info file of an inventory.
func ReadInfo(files []source.File) Info {
	info := Info{}
	for _, f := range files {
		if stat, err := os.Stat(f.Path); f.Format != source.Info || err != nil || stat.Size() > infoMaxBytes {
			continue
		}
		text, err := f.Text()
		if err != nil {
			continue
		}

		folder := strings.ToLower(path.Dir(f.Rel))
		if info[folder] == nil {
			info[folder] = &FolderInfo{Fields: map[string]InfoField{}}
		}
		info[folder].read(text, strings.HasSuffix(strings.ToLower(f.Rel), ".md"))
	}
	return info
}

// Kind is what the game says about the kind whose definitions are in folders.
func (i Info) Kind(folders []string) string {
	for _, folder := range folders {
		if fi := i[strings.ToLower(folder)]; fi != nil && fi.Prose != "" {
			return fi.Prose
		}
	}
	return ""
}

// Field is what the game says about a key written inside parent's block, else about the key wherever it is written. parent is "" for a definition's own member.
func (i Info) Field(folders []string, parent, key string) InfoField {
	for _, folder := range folders {
		if fi := i[strings.ToLower(folder)]; fi != nil {
			if f, ok := fi.Fields[parent+"."+key]; ok && parent != "" {
				return f
			}
			if f, ok := fi.Fields[key]; ok {
				return f
			}
		}
	}
	return InfoField{}
}

// read adds what one file says. markdown makes a # at column 0 a heading.
func (fi *FolderInfo) read(text string, markdown bool) {
	var pending []string   // prose waiting for the key below it
	var parents []infoLine // the blocks the line is inside, by indentation
	var open *infoLine     // a line whose own prose may still follow it: a block's opener, a brief header, a markdown heading
	var kindProse []string
	kindDone, own := fi.Prose != "", map[string]int{}

	// settle records the open line. The waiting prose is its own, unless a key line has come to claim it
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

	for _, raw := range strings.Split(text, "\n") {
		body, hashed := jomini.PeelComment(raw)
		indent := 0
		for _, r := range raw {
			if r == '\t' {
				indent += 4
			} else if r == ' ' || r == '#' {
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

		// a rule (=== Notes ===), a fence, a closing brace and a placeholder opener (<name> = {) are neither key nor prose
		if strings.HasPrefix(body, "```") || strings.HasPrefix(body, "}") || strings.HasPrefix(body, "<") || strings.HasPrefix(body, "==") || strings.HasSuffix(body, "==") {
			if !inSection {
				settle(false)
			}
			continue
		}

		// a heading (ATTRIBUTES, Valid categories:) is about the lines below it as a whole, and is no key's prose
		line, opens, isKey := infoKeyLine(body, hashed)
		if !isKey && (strings.HasSuffix(body, ":") || !strings.ContainsFunc(body, unicode.IsLower)) {
			if !inSection {
				settle(false)
			}
			continue
		}
		if !isKey {
			// prose: about the kind before the first key, and waiting for the key below; under a heading only what is written at column 0
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

// add records what a line says about a key, under its parent and on its own. What is already recorded stays, and only its empty parts are filled; but on its own a key is its shallowest mention in the file, so an event's type is not the type of a widget inside it. own is the depth at which this file first recorded each key.
func (fi *FolderInfo) add(l infoLine, own map[string]int) {
	if depth, mine := own[l.key]; mine && l.depth < depth {
		delete(fi.Fields, l.key)
	}
	if _, seen := fi.Fields[l.key]; !seen {
		own[l.key] = l.depth
	}

	names := []string{l.key}
	if l.parent != "" {
		names = []string{l.parent + "." + l.key, l.key}
	}
	for _, name := range names {
		f := fi.Fields[name]
		if f.Prose == "" {
			f.Prose = l.prose
		}
		if f.Type == "" {
			f.Type = l.typ
		}
		fi.Fields[name] = f
	}
}

// infoKeyLine reads one line with its comment marks peeled, when it documents a key: key = value # prose, key = <type> prose, key = { <prose> }, - key: prose, key: <type> prose (only in a comment, and only with a type or three words, so "scope: character" stays prose), and CK3's brief: key ( type ). opens reports a block left open on the line.
func infoKeyLine(body string, hashed bool) (l infoLine, opens, ok bool) {
	if len(body) > 6 && strings.EqualFold(body[:6], "brief:") {
		key, typ, _ := strings.Cut(body[6:], "(")
		l.key, l.typ, l.brief = strings.TrimSpace(key), strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typ), ")")), true
		return l, false, jomini.IsDefName(l.key)
	}

	// the key, then = or :
	text, bullet := strings.CutPrefix(body, "- ")
	text = strings.TrimSpace(text)
	end := strings.IndexFunc(text+" ", func(r rune) bool {
		return !(r == '_' || r == '.' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	key, rest := strings.TrimRight(text[:end], ".-"), strings.TrimSpace(text[end:])
	if !jomini.IsDefName(key) || unicode.IsDigit(rune(key[0])) || rest == "" || (rest[0] != '=' && rest[0] != ':') || strings.HasPrefix(rest, "==") {
		return l, false, false
	}

	// a colon makes a key only of a lower-case word, and outside a bullet only in a comment, with a type or three words behind it
	colon := rest[0] == ':'
	rest = strings.TrimSpace(rest[1:])
	if colon && (!jomini.IsLowerName(key) || (!bullet && (!hashed || (len(strings.Fields(rest)) < 3 && !strings.HasPrefix(rest, "<"))))) {
		return l, false, false
	}

	// the value, and the comment behind it
	value, comment, _ := strings.Cut(rest, "#")
	value, comment = strings.TrimSpace(value), strings.TrimSpace(strings.TrimLeft(comment, "# "))
	typ, prose := "", ""
	switch {
	case strings.HasPrefix(value, "<"):
		// <type> prose
		if t, p, closed := strings.Cut(value[1:], ">"); closed && len(t) <= 120 {
			typ, prose = t, p
		}
	case strings.HasPrefix(value, "{"):
		// { <type> }, { <prose }, or prose behind the closing brace
		inner := strings.TrimSpace(value[1:])
		if i := strings.LastIndexByte(inner, '}'); i >= 0 {
			inner, prose = strings.TrimSpace(inner[:i]), inner[i+1:]
		}
		if t, _, closed := strings.Cut(strings.TrimPrefix(inner, "<"), ">"); closed && strings.HasPrefix(inner, "<") && len(t) <= 120 {
			typ = t
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
	l.key, l.typ, l.prose = key, strings.TrimSpace(typ), prose
	return l, strings.Count(value, "{") > strings.Count(value, "}"), true
}

// joinProse joins lines with a space and paragraphs, marked by an empty line, with a blank line.
func joinProse(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		switch {
		case line == "":
			b.WriteString("\n\n")
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "\n"):
			b.WriteString(" " + line)
		default:
			b.WriteString(line)
		}
	}
	return strings.TrimSpace(b.String())
}

// Declared is the field's type as an accept, when it is one of the few the games write that say what a value is: bool and yes/no; int, integer, num, number, float, double and fixed_point; string; localization_key and loc key; and a/b/c, a closed set of bare words. "optional" and its like are dropped first, and "list of" names the same type, because how many values a field takes is its class. Everything else declines: a trigger, a database key and a sentence in brackets say nothing a value can be checked against, and refusing beats guessing.
func (f InfoField) Declared() (Accept, bool) {
	var parts []string
	for _, part := range strings.Split(strings.ToLower(f.Type), ",") {
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
	words := strings.FieldsFunc(word, func(r rune) bool { return r == '/' || r == '|' })
	ok := len(words) >= 2 && len(words) <= 12
	for _, w := range words {
		ok = ok && len(w) <= 24 && jomini.IsLowerName(w)
	}
	if !ok {
		return Accept{}, false
	}
	return Accept{Type: TypeEnum, Enum: words, Declared: true}, true
}
