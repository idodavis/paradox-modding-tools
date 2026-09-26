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
	"bytes"
	"cmp"
	"io"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Info is an install's prose beside its script, by folder from a root, lower-cased: common/traits.
type Info map[string]*FolderInfo

// FolderInfo is what the info files of one folder say.
type FolderInfo struct {
	Prose       string               // about the whole kind: what a file says before its first key, four lines at most
	Fields      map[string]InfoField // by key, and by parent.key inside a parent's block; the first file to document one wins
	Definitions map[string]string    // a definition's blurb from its own file's header list (# name - text), by name
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

// ReadInfo reads every info file of an inventory, then the comment header of every script file
// (header): its template lines document the folder, after the info files, which win.
func ReadInfo(files []source.File) Info {
	info := Info{}
	folderOf := func(file source.File) *FolderInfo {
		folder := strings.ToLower(path.Dir(file.Rel))
		if info[folder] == nil {
			info[folder] = &FolderInfo{Fields: map[string]InfoField{}, Definitions: map[string]string{}}
		}
		return info[folder]
	}
	for _, file := range files {
		if stat, err := os.Stat(file.Path); file.Format != source.Info || err != nil || stat.Size() > infoMaxBytes {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		folderOf(file).read(text, strings.HasSuffix(strings.ToLower(file.Rel), ".md"))
	}

	// the headers, a reader per CPU, merged in file order so the first file to speak wins
	type head struct {
		templates string
		blurbs    map[string]string
	}
	heads, gate := make([]head, len(files)), make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, file := range files {
		if file.Format == source.Script && strings.HasSuffix(strings.ToLower(file.Rel), ".txt") {
			gate <- struct{}{}
			wg.Go(func() {
				heads[i].templates, heads[i].blurbs = header(file.Path)
				<-gate
			})
		}
	}
	wg.Wait()
	for i, got := range heads {
		if got.templates == "" && len(got.blurbs) == 0 {
			continue
		}
		fi := folderOf(files[i])
		fi.read(got.templates, false)
		for name, blurb := range got.blurbs {
			fi.Definitions[name] = cmp.Or(fi.Definitions[name], blurb)
		}
	}
	return info
}

// headerBytes is how much of a script file's top is read for its header: the longest vanilla
// header is under 8 KB.
const headerBytes = 64 << 10

// header reads the comments at the top of a script file, before its first line of script. Of
// them, a key line whose value is a template (<path>, <trigger>, yes/no, a | b | c) documents the
// folder, as Vic3's alert_types does; a commented-out line of real script documents nothing. A
// line name - text is a blurb for a definition this file makes (CK3's scripted effects list
// theirs so); a name the file does not define is a remark, and skipped.
func header(abs string) (templates string, blurbs map[string]string) {
	file, err := os.Open(abs)
	if err != nil {
		return "", nil
	}
	head := make([]byte, headerBytes)
	n, _ := io.ReadFull(file, head[:4<<10])
	if bytesHeader(head[:n]) {
		more, _ := io.ReadFull(file, head[n:])
		n += more
	}
	file.Close()
	text, _ := jomini.Decode(head[:n])

	var lines []string
	listed := map[string]string{}
	for raw := range strings.SplitSeq(text, "\n") {
		body, hashed := jomini.PeelComment(raw)
		if !hashed && strings.TrimSpace(raw) != "" {
			break
		}
		if name, blurb, ok := listLine(body); ok {
			listed[name] = blurb
			continue
		}
		if line, _, ok := infoKeyLine(body, true); ok && template(line, body) {
			lines = append(lines, body)
		}
	}
	if len(listed) == 0 {
		return strings.Join(lines, "\n"), nil
	}

	// a listed name counts only when this file defines it
	raw, err := os.ReadFile(abs)
	if err != nil || len(raw) > infoMaxBytes {
		return strings.Join(lines, "\n"), nil
	}
	whole, _ := jomini.Decode(raw)
	blurbs = map[string]string{}
	for _, statement := range jomini.Parse(whole).Root.Statements {
		if assignment, ok := statement.(*jomini.Assignment); ok {
			_, name := jomini.Mode(assignment.Key.Text)
			if blurb, listed := listed[strings.ToLower(name)]; listed {
				blurbs[strings.ToLower(name)] = blurb
			}
		}
	}
	return strings.Join(lines, "\n"), blurbs
}

// bytesHeader reports the first bytes of a file holding no line of script yet, so its header may
// run on past them.
func bytesHeader(raw []byte) bool {
	for line := range strings.SplitSeq(string(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}

// listLine reads name - text, or name – text, from a header: a definition's name, a spaced dash,
// and a few words.
func listLine(body string) (name, blurb string, ok bool) {
	body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "- "))
	for _, dash := range []string{" - ", " – "} {
		if before, after, found := strings.Cut(body, dash); found && jomini.IsDefName(before) && len(strings.Fields(after)) >= 2 {
			return strings.ToLower(before), strings.TrimSpace(after), true
		}
	}
	return "", "", false
}

// template reports a documented key line whose value stands for any value of a type: <type>, a
// type word, yes/no, or a closed set a/b/c or a | b | c. A concrete value is script, commented out.
func template(line infoLine, body string) bool {
	if line.typ != "" {
		return true
	}
	_, rest, found := strings.Cut(body, "=")
	if !found {
		_, rest, _ = strings.Cut(body, ":")
	}
	value, _, _ := strings.Cut(rest, "#")
	value = strings.ReplaceAll(strings.TrimSpace(value), " ", "")
	if strings.HasPrefix(value, "<") {
		return true
	}
	_, closed := (InfoField{Type: value}).Declared()
	return closed
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
