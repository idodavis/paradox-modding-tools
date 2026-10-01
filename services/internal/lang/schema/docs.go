// docs.go reads the documentation a game ships beside its script (CK3's _traits.info, EU5's
// readme.txt, Vic3's buildings.md) and the comment headers of its script files, as notes on the
// positions of one entry: the prose written for a key, the type its value is written as, and what
// the prose says of whether it may be left out, its default and the scope its block runs in. The
// code lines make a skeleton that jomini parses for the nesting; each statement takes the comment on
// its line, else the notes above it. No prose is dropped, and a form the reader does not know is
// unread.

package schema

import (
	"cmp"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// docsMaxBytes keeps data out: EU5 ships 5 MB .info terrain caches; real prose stays under 25 KB.
const docsMaxBytes = 1 << 20

// Docs is an install's documentation, by folder from a root, lower-cased: common/traits.
type Docs map[string]*FolderDocs

// FolderDocs is what the documentation of one folder says.
type FolderDocs struct {
	About   string            // what the docs say of the whole database, by paragraph; a titled section is headed by its title
	Entry   *Key              // one entry as the docs write it; a database in it is named by its folder
	Blurbs  map[string]string // a word's or an entry's blurb: a header list (# name - text), a value list's items
	LocKeys []string          // the loc key templates the docs write, {id} for the entry's name: rule_{id}
}

// lineKind is what one doc line is, whatever style wrote it.
type lineKind uint8

const (
	lineNote    lineKind = iota // a sentence, a label (Default: 0), a bullet of prose
	lineBreak                   // a lone #: parts the paragraphs of the notes around it
	lineBlank                   // nothing: gives the notes above to what is open
	lineRule                    // ======, #####, a fence
	lineTitle                   // == Lease Hierarchies ==, TITLE, a markdown heading
	lineSection                 // a markdown heading naming a key: ## key (default: x)
	lineBrief                   // ### Brief: key ( type ), ### key (type), ### a/b effects
	lineCode                    // key = value, - key: <type> prose, <key> = {, 1.1.1 = {, c:USA ?= {
	lineClose                   // } and nothing else
	lineItem                    // a placeholder alone: <effects>
	lineWord                    // a value list's item: - word: prose, a one-word line, a table row, name - blurb
	lineSkip                    // an example comparison, a line read into the one above: nothing to read
	lineUnread                  // a table row, a form no rule reads: unread
)

// docLine is one line of a doc as a record, the same whatever style wrote it.
type docLine struct {
	kind       lineKind
	number     int      // its line in the file
	col        int      // its indentation with comment marks left out: tab 4, space 1
	marks      int      // the # marks before it: a markdown heading's level
	bullet     bool     // written after - * + or a number
	label      bool     // a note that is a label: six words or fewer ending in :, Example
	keys       []string // a code line's keys, several where the doc joins them with /; a brief's; an item's word
	op         string   // = or :; any other operator makes an example statement
	rest       string   // what a code line writes after its operator, its comment cut off; an item
	typ        string   // a type the line names: <type>, ( type ), a type word before its prose
	note       string   // its prose: a sentence, a label, a code line's comment or the words after its type
	textCol    int      // where its text starts, with tab stops, for the continuation of a comment above
	commentCol int      // where a code line's comment text starts, with tab stops
	opens      int      // blocks a code line opens less those it closes; a close line's count, negative
	parent     bool     // a bullet the more-indented lines below nest in
}

// docReader reads one documentation file into its folder's template.
type docReader struct {
	docs      *FolderDocs
	file      string          // its path from a root, for the unread report
	tops      map[string]bool // what vanilla writes in the folder: its entry names,
	inner     map[string]bool // its keys at any depth,
	entryTops map[string]bool // and the keys at its entries' top
	wrappers  []string        // the keys that only wrap entries in the folder
	nested    []game.Nested   // the rows naming the folder's nested positions
	roots     map[string]bool // the install's top folders, which a path starts with
	scripts   *Scripts
	dumps     *Dumps
	unread    map[string]int
	every     map[*Key]Required // the blocks whose prose gives every key one: "All tiers are required"
	lines     []docLine
	at        *jomini.LineIndex
	pending   []string          // notes waiting for the statement below them
	saved     map[string]string // saved scopes waiting for the block below them: scope:actor = character
	values    map[string]bool   // the words the doc has written as a key's value: left_portrait = X
	open      []opened          // positions whose notes may still follow them
	section   bool              // open is a markdown heading, whose paragraphs are its notes
	headings  []heading         // the markdown headings naming keys that hold the lines below
	title     string            // the title the notes below are under
	titled    bool              // its paragraph of About is written
	label     string            // the label the lines below follow
	wrap      bool              // the doc writes one entry's template
}

// opened is a position whose notes may still follow: a block's opener, a brief, a markdown heading.
type opened struct {
	key   *Key
	prose string
}

// heading is a markdown heading naming a key, and its level.
type heading struct {
	level int
	key   *Key
}

// stated is what a doc's words for a type say: what a block holds, or what a word may be.
type stated struct {
	block  Block
	takes  []Take
	stated bool
}

// aliases are the misspellings the games' docs write for the words the reader reads, each found in
// a shipped doc (CK3 _bookmark_groups.info: Breif); a line is read with them corrected.
var aliases = strings.NewReplacer(
	"Breif", "Brief", "breif", "brief", "Brienf", "Brief", "brienf", "brief",
	"Defualt", "Default", "defualt", "default", "Defult", "Default", "defult", "default",
	"Defaut", "Default", "defaut", "default",
	"Sctipted", "Scripted", "sctipted", "scripted", "Scipted", "Scripted", "scipted", "scripted",
	"effets", "effects", "descriotion", "description", "descriptopn", "description",
	"Exmaple", "Example", "exmaple", "example", "casus belly", "casus belli", "procuction", "production",
)

// docLabels is the words a doc writes before a colon to say what a line is, not naming a key.
var docLabels = []string{
	"default", "defaults", "scope", "scopes", "root", "note", "notes", "optional", "example", "examples", "warning",
	"important", "syntax", "format", "values", "options", "usage", "localization", "errors", "targets", "enums",
	"fallback", "ex", "eg",
}

// The forms of a doc line: a key line, a brief, a rule, a title between rules, a type word leading
// its prose, a need written before a type, a name - blurb line, and a table's separator row.
var (
	keyPattern = `[A-Za-z_$@][\w.@$\-|/]*(?::[\w.@$\-|]+)*`
	keyLineRe  = regexp.MustCompile("^(?P<bullet>(?:[-*+•]|\\d+[.)])[ \\t]+)?(?P<tick>`?)(?P<key>" + keyPattern +
		")`?[ \\t]*(?P<op>\\?=|<=|>=|==|!=|=|:|<|>)(?P<rest>.*)$")
	briefRe   = regexp.MustCompile(`(?i)^(?:[-*+][ \t]+)?brief[ \t]*:[ \t]*(.*)$`)
	bareBrief = regexp.MustCompile(`^(` + keyPattern + `)\s*\(([^()]*)\)\s*$`)
	ruleRe    = regexp.MustCompile(`^[=\-*_~+#. ]{3,}$`)
	titleRule = regexp.MustCompile(`^[=\-*~#]{2,}[ \t]*(.+?)[ \t]*[=\-*~#]{2,}$`)
	typeFirst = regexp.MustCompile(`(?i)^(\[?[\w.\-]+(?:\s*[/|]\s*[\w.\-]+)+\]?|bool(?:ean)?|int(?:eger|32|64)?|` +
		`float|number|fixed[ _]?point|script ?value|scripted value|string|text|loc(?:alization)?[ _]?key|date|` +
		`triggers?|effects?|modifiers?)\s*[:;,\-–(]?\s+\S`)
	needLead  = regexp.MustCompile(`(?i)^(?:[(\[<]\s*(?:optional|required)\s*[)\]>]|optional\b)\s*`)
	blurbLine = regexp.MustCompile(`^([a-z_][a-z0-9_]*) [-–] (\S+ .+)$`)
	tableRule = regexp.MustCompile(`^\|?[\s:|-]+\|?$`)
	pathSaid  = regexp.MustCompile(`\b(?:game/)?(?:(?:in_game|main_menu|loading_screen)/)?((?:common|events|gfx|gui|history|map_data|music|sound|setup)(?:/[a-z0-9_]+)+)/?`)
	locSaid   = regexp.MustCompile("(?:^|[\\s`'\"(])((?:[a-z0-9_]*_)?<[a-z_ ]+>(?:_[a-z0-9_]*)?)(?:$|[\\s`'\".,;)])")
	keyPlus   = regexp.MustCompile(`\bkey \+ (_[a-z_]+)`)
	chooses   = regexp.MustCompile(`(?i)\barbitrary\b|\bname of (?:the |this |an? )?entry\b|\bchoose a (?:key|name)\b`)
	valueType = regexp.MustCompile(`(?i)^[a-z0-9_]*_(key|id|name|tag|type)$`)
)

// needWord, needIf, needAll and needOmitted find whether prose lets its position be left out:
// "Optional; will cause", "This field is required.", "must be set", "Required for coastal land
// regions only", "Error if it's not set", "If the type is lifestyle this is required", "All tiers
// are required", "if not set".
var (
	needWord = regexp.MustCompile(`^(?:(?:this|the) (?:field|parameter|key|value) is |it is |it's |this is )?` +
		`(optional)\b`)
	needSet     = regexp.MustCompile(`\b(?:is |are )?(required|mandatory|must be (?:set|specified|defined|present))\b\s*(\S*)\s*(.*)$`)
	needError   = regexp.MustCompile(`\berror if (?:it's|it is|this is) not (?:set|specified|defined)\b`)
	needIf      = regexp.MustCompile(`^(if .+?),? (?:this|it) is (?:required|mandatory)$`)
	needAll     = regexp.MustCompile(`^all [a-z_ ]{1,30}? are (optional|required|mandatory)\b`)
	needOmitted = regexp.MustCompile(`\bif (?:omitted|not (?:set|specified|defined|present)|left (?:empty|out|blank))\b|` +
		`\b(?:can|may) be (?:omitted|left out)\b`)
	scopeOptional = regexp.MustCompile(`(?:scope:\w+|\b\w+)\s*\(optional\)(?:\s*=)?`)
)

// defaultSaid and rootSaid find a default and a root scope in prose: "Defaults to all", "(default
// 0)", "Default: 0", "[Default = yes]", "root = the character", "root: ( Character )", "Scopes:
// root ( Province )". A saved scope, scope:x, is never the root.
var (
	defaultSaid = regexp.MustCompile("(?i)(?:\\bdefaults?\\b(?:\\s*(?:to|is|=|:)\\s*|\\s+)|\\[default\\s*=\\s*)" +
		"(NDefines::\\w+::\\w+|\"[^\"]*\"|`[^`]*`|<[^<>]*>|[\\w.\\-]+)")
	rootSaid  = regexp.MustCompile(`(?i)\b(?:root|scope)(?:\s+scope)?(?:\s*(?:is|[=(])|:\s)\s*\(?\s*(?:the |a |an )?([a-z_]+(?: [a-z_]+){0,3})`)
	scopeSaid = regexp.MustCompile(`(?i)\b(?:in |\()?([a-z_]+) scope\b`)
	savedSaid = regexp.MustCompile(`^scope:(\w+)$`)
)

// typeSpellings are the words the docs write for a type, each with what it says.
var (
	statedTrigger  = stated{block: BlockTriggers, stated: true}
	statedEffect   = stated{block: BlockEffects, stated: true}
	statedModifier = stated{block: BlockModifiers, stated: true}
	statedMath     = stated{block: BlockValues, takes: []Take{{Type: WordNumber, Source: SourceDocs}}, stated: true}
	statedBool     = stated{takes: []Take{{Type: WordBool, Source: SourceDocs}}, stated: true}
	statedNumber   = stated{takes: []Take{{Type: WordNumber, Source: SourceDocs}}, stated: true}
	statedString   = stated{takes: []Take{{Type: WordString, Source: SourceDocs}}, stated: true}
	statedLoc      = stated{takes: []Take{{Type: WordLoc, Source: SourceDocs}}, stated: true}
	statedDate     = stated{takes: []Take{{Type: WordDate, Source: SourceDocs}}, stated: true}
	statedFile     = stated{takes: []Take{{Type: WordFile, Source: SourceDocs}}, stated: true}
	statedColor    = stated{takes: []Take{{Type: WordColor, Source: SourceDocs}}, stated: true}
	statedWord     = stated{takes: []Take{{Type: WordUnknown, Source: SourceDocs}}, stated: true}
	statedEnum     = stated{takes: []Take{{Type: WordEnum, Source: SourceDocs}}, stated: true}
	typeSpellings  = map[string]stated{
		"trigger": statedTrigger, "triggers": statedTrigger, "jomini trigger": statedTrigger,
		"trigger definition": statedTrigger, "condition": statedTrigger, "conditions": statedTrigger,
		"effect": statedEffect, "effects": statedEffect, "jomini effect": statedEffect, "effect definition": statedEffect,
		"modifier": statedModifier, "modifiers": statedModifier, "modifier definition": statedModifier,
		"script value": statedMath, "scripted value": statedMath, "script_value": statedMath, "scriptvalue": statedMath,
		"scripted_value": statedMath, "scripted math": statedMath, "scripted maths": statedMath, "script math": statedMath,
		"mtth": statedMath, "math": statedMath, "maths": statedMath, "scripted value int32": statedMath, "script value int": statedMath,
		"int/script value": statedMath, "int/scripted value": statedMath, "float/scripted value": statedMath,
		"float script value": statedMath, "script value fixed point": statedMath, "days/scripted value": statedMath,
		"bool": statedBool, "boolean": statedBool, "yes/no": statedBool, "yes|no": statedBool,
		"int": statedNumber, "ints": statedNumber, "int32": statedNumber, "int64": statedNumber, "integer": statedNumber,
		"float": statedNumber, "double": statedNumber, "num": statedNumber, "number": statedNumber,
		"fixed_point": statedNumber, "fixed point": statedNumber, "fixedpoint": statedNumber, "cfixedpoint": statedNumber,
		"decimal": statedNumber, "percentage": statedNumber, "value": statedNumber, "days": statedNumber,
		"months": statedNumber, "years": statedNumber,
		"string": statedString, "strings": statedString, "text": statedString,
		"loc key": statedLoc, "localization key": statedLoc, "localisation key": statedLoc, "loc_key": statedLoc,
		"localization_key": statedLoc, "localisation_key": statedLoc, "dynamic description": statedLoc,
		"dynamic_description": statedLoc, "dynamic desc": statedLoc,
		"date": statedDate, "path": statedFile, "file": statedFile, "file path": statedFile, "texture path": statedFile,
		"color": statedColor, "colour": statedColor, "jomini color": statedColor, "named color": statedColor,
		"flag": statedWord, "enum": statedEnum,
	}
)

// ReadDocs reads the documentation of an inventory, and the headers of the one read of vanilla where
// no doc covers their folder. Of two docs at one rel, only the later layer's is read. The lines no
// form reads come back as unread.
func ReadDocs(files []source.File, scripts *Scripts, dumps *Dumps) (Docs, []Tally) {
	docs, roots, latest := Docs{}, map[string]bool{}, map[string]int{}
	for i, file := range files {
		first, _, _ := strings.Cut(strings.ToLower(file.Rel), "/")
		roots[first] = true
		if file.Format == source.Info {
			latest[strings.ToLower(file.Rel)] = i
		}
	}

	unread := map[string]int{}
	docFolders := map[string]bool{}
	for i, file := range files {
		if file.Format != source.Info || latest[strings.ToLower(file.Rel)] != i {
			continue
		}
		if stat, err := os.Stat(file.Path); err != nil || stat.Size() > docsMaxBytes {
			continue
		}
		if text, err := file.Text(); err == nil {
			folder := documented(file.Rel, scripts)
			docFolders[folder] = true
			newDocReader(docs, folder, file.Rel, roots, scripts, dumps, unread).read(text, strings.HasSuffix(strings.ToLower(file.Rel), ".md"))
		}
	}

	// a header of three key lines or more is a template where no doc covers its folder; its name
	// list gives blurbs to the names the folder defines
	for _, rel := range slices.Sorted(maps.Keys(scripts.Headers)) {
		text, folder := scripts.Headers[rel], strings.ToLower(path.Dir(rel))
		keyLines, blurbs := 0, map[string]string{}
		for line := range strings.SplitSeq(text, "\n") {
			if readLine(line, false).kind == lineCode {
				keyLines++
			}
			item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")), "- "))
			name, blurb, dashed := strings.Cut(item, " - ")
			if !dashed {
				name, blurb, dashed = strings.Cut(item, " – ")
			}
			if name = strings.ToLower(name); dashed && scripts.Tops[folder][name] && len(strings.Fields(blurb)) >= 2 {
				blurbs[name] = cmp.Or(blurbs[name], strings.TrimSpace(blurb))
			}
		}
		if keyLines < 3 && len(blurbs) == 0 {
			continue
		}
		doc := newDocReader(docs, folder, rel, roots, scripts, dumps, unread)
		for name, blurb := range blurbs {
			doc.docs.Blurbs[name] = cmp.Or(doc.docs.Blurbs[name], blurb)
		}
		if keyLines >= 3 && !docFolders[folder] {
			doc.read(text, false)
		}
	}

	return docs, tallies(unread)
}

// newDocReader is a reader of one doc, or one wiki code block, into its folder's docs.
func newDocReader(docs Docs, folder, file string, roots map[string]bool, scripts *Scripts, dumps *Dumps, unread map[string]int) *docReader {
	if docs[folder] == nil {
		docs[folder] = &FolderDocs{Entry: &Key{Source: SourceDocs}, Blurbs: map[string]string{}}
	}
	doc := &docReader{
		docs: docs[folder], file: file, roots: roots, scripts: scripts, dumps: dumps, unread: unread, values: map[string]bool{},
		every: map[*Key]Required{}, tops: map[string]bool{}, inner: map[string]bool{}, entryTops: map[string]bool{},
	}

	// what vanilla writes in the doc's folder and below it, and the rows that hold there
	for written, names := range scripts.Tops {
		if written == folder || strings.HasPrefix(written, folder+"/") {
			maps.Copy(doc.tops, names)
			maps.Copy(doc.inner, scripts.Inner[written])
		}
	}
	for database, written := range scripts.Groups {
		for _, lower := range written {
			if lower = strings.ToLower(lower); (lower == folder || strings.HasPrefix(lower, folder+"/")) && scripts.Entries[database] != nil {
				for key := range scripts.Entries[database].Keys {
					doc.entryTops[key] = true
				}
			}
		}
	}
	if info := scripts.info; info != nil {
		row, _ := FolderRow(info, folder)
		doc.wrappers = row.Wrappers
		for _, nested := range info.Nested {
			if parent := strings.ToLower(nested.Parent); folder == parent || strings.HasPrefix(folder, parent+"/") || strings.HasPrefix(parent, folder+"/") {
				doc.nested = append(doc.nested, nested)
			}
		}
	}
	return doc
}

// documented is the folder a doc documents: its own, or the subfolder it is named after
// (history/_characters.info), or the folder of the script file it is named after, a leading number
// aside (audio_persistent_objects.md documents common/defines/00_audio_persistent_objects.txt).
func documented(rel string, scripts *Scripts) string {
	folder := strings.ToLower(path.Dir(rel))
	stem := strings.Trim(strings.ToLower(strings.TrimSuffix(path.Base(rel), path.Ext(rel))), "_")
	if _, ok := scripts.Tops[folder+"/"+stem]; ok {
		return folder + "/" + stem
	}
	for _, script := range slices.Sorted(maps.Keys(scripts.Headers)) {
		lower := strings.ToLower(script)
		name := strings.TrimSuffix(path.Base(lower), path.Ext(lower))
		name = strings.TrimPrefix(strings.TrimLeft(name, "0123456789"), "_")
		if strings.HasPrefix(lower, folder+"/") && name == stem {
			return path.Dir(lower)
		}
	}
	return folder
}

// Key is what the documentation writes at key inside a block named parent ("" for an entry's own
// key), its shallowest mention; where anywhere, else at key under any parent. The zero Key where
// none does.
func (docs Docs) Key(folders []string, parent, key string, anywhere bool) Key {
	for _, folder := range folders {
		folderDocs := docs[strings.ToLower(folder)]
		if folderDocs == nil {
			continue
		}
		for _, anyParent := range []bool{false, anywhere} {
			for level := []*Key{folderDocs.Entry}; len(level) > 0; {
				var next []*Key
				for _, at := range level {
					if key := at.Keys[key]; key != nil && (anyParent || at.Name == parent) {
						return *key
					}
					for _, name := range slices.Sorted(maps.Keys(at.Keys)) {
						next = append(next, at.Keys[name])
					}
				}
				level = next
			}
		}
	}
	return Key{}
}

// visual is how wide a run of text is: a tab 4, anything else 1.
func visual(text string) int {
	return len([]rune(text)) + 3*strings.Count(text, "\t")
}

// column is where the text after a run starts, with tab stops every 4 columns as an editor shows it.
func column(text string) int {
	at := 0
	for _, char := range text {
		if char == '\t' {
			at += 4 - at%4
		} else {
			at++
		}
	}
	return at
}

// readLine reads one doc line into its record, its typos read as their words. markdown makes a # at
// column 0 a heading.
func readLine(raw string, markdown bool) docLine {
	raw = aliases.Replace(strings.TrimRight(raw, "\r"))
	body, marks, after := strings.TrimLeft(raw, " \t"), 0, ""
	indent := raw[:len(raw)-len(body)]
	for strings.HasPrefix(body, "#") {
		trimmed := strings.TrimLeft(body[1:], " \t")
		after, body, marks = body[1:len(body)-len(trimmed)], trimmed, marks+1
	}
	line := docLine{marks: marks, col: visual(indent) + visual(after), textCol: column(raw[:len(raw)-len(body)])}
	body = strings.TrimSpace(body)
	fields, title, brief := strings.Fields(body), titleRule.FindStringSubmatch(body), bareBrief.FindStringSubmatch(body)
	letters := strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) {
			return char
		}
		return -1
	}, body)

	// what only parts or ends the notes, the headings and briefs that name keys, a table row, a close
	switch labeled := briefRe.FindStringSubmatch(body); {
	case body == "" && marks > 0 && marks < 4:
		line.kind = lineBreak
	case body == "" && marks == 0:
		line.kind = lineBlank
	case body == "" || ruleRe.MatchString(body) || strings.HasPrefix(body, "```"):
		line.kind = lineRule
	case markdown && indent == "" && marks > 0 && after != "":
		line.kind, line.note = lineTitle, body
		if name := strings.Trim(body, "`"); jomini.IsLowerName(name) {
			line.kind, line.keys = lineSection, []string{name}
		} else if brief != nil && jomini.IsLowerName(brief[1]) {
			line.kind, line.keys, line.typ = lineSection, []string{brief[1]}, strings.TrimSpace(brief[2])
		}
	case title != nil && strings.ContainsFunc(title[1], unicode.IsLetter):
		line.kind, line.note = lineTitle, title[1]
	case labeled != nil:
		// a brief names keys only when they are keys, then a type in parentheses; a brief of words
		// is a title
		name, rest, _ := strings.Cut(strings.TrimSpace(labeled[1]), " ")
		rest = strings.TrimSpace(rest)
		keys := joinedKeys(name)
		if name == "" || !slices.ContainsFunc(keys, jomini.IsDefName) || rest != "" && !strings.HasPrefix(rest, "(") && !strings.HasPrefix(rest, "-") {
			line.kind, line.note = lineTitle, strings.TrimSpace(labeled[1])
			break
		}
		line.kind, line.keys = lineBrief, keys
		line.typ = strings.TrimSpace(strings.Trim(rest, "()-– "))
	case brief != nil && marks >= 2:
		line.kind, line.keys, line.typ = lineBrief, joinedKeys(brief[1]), strings.TrimSpace(brief[2])
	case marks >= 2 && len(fields) >= 2 && strings.Contains(fields[0], "/") &&
		typeWords(strings.Join(fields[1:], " ")).stated:
		line.kind, line.keys, line.typ = lineBrief, joinedKeys(fields[0]), strings.Join(fields[1:], " ")
	case strings.HasPrefix(body, "|"):
		line.kind, line.rest = lineUnread, body
	case strings.HasPrefix(body, "}") && strings.Trim(strings.Split(body, "#")[0], "} \t") == "":
		line.kind, line.opens = lineClose, -strings.Count(strings.Split(body, "#")[0], "}")
	case len(letters) >= 2 && strings.ToUpper(letters) == letters && !strings.ContainsAny(body, "=:"),
		marks >= 2 && len(fields) <= 6 && !strings.HasSuffix(body, ".") && !strings.HasSuffix(body, ":") && !strings.Contains(body, "="):
		line.kind, line.note = lineTitle, body
	}
	if line.kind != lineNote {
		return line
	}

	// a name - blurb line, for a value list or the folder's blurbs
	if blurb := blurbLine.FindStringSubmatch(body); blurb != nil {
		line.kind, line.keys, line.note = lineWord, []string{blurb[1]}, strings.TrimSpace(blurb[2])
		return line
	}

	// a key line: key = value # comment, - key: <type> prose, <key> = {, 1.1.1 = {; any operator but
	// = and : makes an example statement, whose braces still count
	key, op, rest, bullet, tick := "", "", "", "", ""
	if closed := strings.IndexByte(body, '>'); strings.HasPrefix(body, "<") && closed > 0 {
		if value, found := strings.CutPrefix(strings.TrimSpace(body[closed+1:]), "="); found {
			key, op, rest = body[:closed+1], "=", value
		}
	}
	if left, value, ok := strings.Cut(body, "="); key == "" && ok {
		if literal := jomini.Literal(strings.TrimSpace(left)); literal == jomini.LitNumber || literal == jomini.LitDate {
			key, op, rest = strings.TrimSpace(left), "=", value
		}
	}
	if match := keyLineRe.FindStringSubmatch(body); key == "" && match != nil {
		bullet, tick, key, op, rest = match[1], match[2], strings.TrimRight(match[3], ".-"), match[4], match[5]
	}
	line.bullet = bullet != "" || slices.Contains([]string{"-", "*", "+", "•"}, fields[0])
	if key != "" {
		line.kind, line.keys, line.op = lineCode, joinedKeys(key), op
		text := strings.TrimSpace(rest)
		switch op {
		case "=":
			line.rest, line.note = cutComment(text)
			if keyAt := strings.Index(raw, key); keyAt >= 0 {
				if hash := strings.IndexByte(raw[keyAt+len(key):], '#'); hash >= 0 {
					after := raw[keyAt+len(key)+hash+1:]
					line.commentCol = column(raw[:len(raw)-len(strings.TrimLeft(after, " \t"))])
				}
			}
			if tick != "" {
				line.rest = strings.TrimSuffix(line.rest, "`")
			}
			line.opens = strings.Count(line.rest, "{") - strings.Count(line.rest, "}")
			if typ, tail := typeOf(line.rest); typ != "" {
				line.typ, line.note = typ, joinProse([]string{tail, line.note})
			}
		case ":":
			line.rest, line.note = text, text
			if typ, tail := typeOf(text); typ != "" {
				line.typ, line.note = typ, tail
			}
			// a label, a capitalised word, or in live script or with few words, a colon is prose (-
			// Travel: the character is travelling, scope: character)
			label := slices.Contains(docLabels, strings.ToLower(key)) && bullet == ""
			if label || line.typ == "" && (!jomini.IsLowerName(key) ||
				bullet == "" && tick == "" && (line.marks == 0 && !markdown || len(strings.Fields(text)) < 3)) {
				line.kind, line.keys, line.op, line.note = lineNote, nil, "", strings.TrimPrefix(body, bullet)
				line.label = text == "" && len(fields) <= 6
			}
		default:
			value, _ := cutComment(text)
			line.opens = strings.Count(value, "{") - strings.Count(value, "}")
		}
		return line
	}

	// a placeholder alone, bare example values ("text", { a b }), labels, sentences, and a word alone
	trimmed := strings.TrimRight(body, ".")
	value, comment := cutComment(body)
	switch {
	case strings.HasPrefix(value, "<") && strings.Count(value, "<") == 1 && strings.Count(value, ">") == 1:
		line.kind, line.rest, line.note = lineItem, value, comment
	case strings.ContainsRune(`"{`, rune(body[0])) && strings.ContainsRune(`"}`, rune(trimmed[len(trimmed)-1])):
		line.kind = lineSkip
	case strings.HasSuffix(body, ":") && len(fields) <= 6, body == "Example", body == "Examples":
		line.note, line.label = body, true
	case len(fields) >= 2 && len(letters) >= 2:
		line.note = body
		if slices.Contains([]string{"-", "*", "+", "•"}, fields[0]) {
			line.note = strings.TrimSpace(body[len(fields[0]):])
		}
	case len(fields) == 1:
		line.kind, line.keys = lineSkip, []string{strings.Trim(body, "`'\".,")}
	default:
		line.kind = lineUnread
	}
	return line
}

// joinedKeys reads a/b/c as each key. A part without _ beside a part with one takes the place of that
// part's first segment where the part with one is after it (random/every/any_x), of its last where it
// is before (scheme_target_character/title).
func joinedKeys(key string) []string {
	parts := strings.Split(key, "/")
	if len(parts) == 1 {
		return parts
	}
	var out []string
	for i, part := range parts {
		if strings.Contains(part, "_") || part == "" {
			out = append(out, part)
			continue
		}
		joined := part
		for j := i + 1; j < len(parts); j++ {
			if _, rest, ok := strings.Cut(parts[j], "_"); ok {
				joined = part + "_" + rest
				break
			}
		}
		if joined == part {
			for j := i - 1; j >= 0; j-- {
				if at := strings.LastIndexByte(parts[j], '_'); at >= 0 {
					joined = parts[j][:at] + "_" + part
					break
				}
			}
		}
		out = append(out, joined)
	}
	return out
}

// cutComment splits a value from the # comment behind it.
func cutComment(text string) (value, comment string) {
	value, comment, _ = strings.Cut(text, "#")
	comment = strings.TrimSpace(strings.TrimLeft(comment, "# "))
	if !strings.ContainsFunc(comment, unicode.IsLetter) {
		comment = ""
	}
	return strings.TrimSpace(value), comment
}

// typeOf is the type a value names and the prose after it: <type> prose, { <type> } prose, (type)
// prose, a type word, a/b/c, or a type word leading its prose (yes/no by default yes). An unclosed
// <type how much… names its first word.
func typeOf(value string) (typ, prose string) {
	value = strings.TrimSpace(value)

	// a need written before the type, (optional) <trigger>, stays in the prose
	if lead := needLead.FindString(value); lead != "" {
		if typ, _ = typeOf(value[len(lead):]); typ != "" {
			return typ, value
		}
		return "", ""
	}
	for _, open := range []string{"<", "{"} {
		inner, ok := strings.CutPrefix(value, open)
		if !ok {
			continue
		}
		inner = strings.TrimSpace(inner)
		name, rest, closed := strings.Cut(strings.TrimPrefix(inner, "<"), ">")
		if closed && (strings.HasPrefix(inner, "<") || open == "<") {
			return strings.TrimSpace(name), strings.TrimSpace(strings.TrimLeft(rest, "} "))
		}
		if open == "<" && !closed {
			first, rest, _ := strings.Cut(inner, " ")
			return first, strings.TrimSpace(rest)
		}
	}
	if inner, ok := strings.CutPrefix(value, "("); ok {
		if typ, rest, closed := strings.Cut(inner, ")"); closed && typeWords(typ).stated {
			return strings.TrimSpace(typ), strings.TrimSpace(rest)
		}
	}
	if typeWords(value).stated {
		return value, ""
	}
	if first := typeFirst.FindStringSubmatch(value); first != nil && typeWords(first[1]).stated {
		return first[1], strings.TrimSpace(strings.TrimLeft(value[len(first[1]):], " :;,-–"))
	}
	return "", ""
}

// read adds what one doc says. markdown makes a # at column 0 a heading.
func (doc *docReader) read(text string, markdown bool) {
	for raw := range strings.SplitSeq(text, "\n") {
		line := readLine(raw, markdown)
		line.number = len(doc.lines) + 1
		doc.lines = append(doc.lines, line)
	}
	doc.shape()
	parsed := jomini.Parse(skeleton(doc.lines, markdown))
	doc.at = parsed.Lines()
	doc.locKeys(text)

	// a doc is one entry's template where an outermost opener is a placeholder, or the only opener
	// there with no key beside it (_traits.info writes keys, then tracks = {)
	openers, keys := 0, 0
	for _, st := range parsed.Root.Statements {
		assignment, ok := st.(*jomini.Assignment)
		line := doc.lines[doc.at.PositionAt(st.NodeRange().Start).Line]
		switch {
		case !ok || len(line.keys) == 0:
		case jomini.BlockOf(assignment.Value) != nil && doc.entryTops[strings.ToLower(line.keys[0])]:
			// a key vanilla writes at its entries' top is no template's own opener
			keys++
		case jomini.BlockOf(assignment.Value) != nil:
			openers++
			doc.wrap = doc.wrap || doc.marker(line.keys[0], line, true)
		case line.rest != "":
			keys++
		}
	}
	doc.wrap = doc.wrap || openers == 1 && keys == 0
	doc.walk(parsed.Root.Statements, doc.docs.Entry, 0, len(doc.lines), true)
	doc.settle(false)

	// "All fields are optional" about the database, "All tiers are required" about a block, once
	// the doc has written the keys; folder prose never gives the entry a default
	doc.required(doc.docs.Entry, doc.docs.About)
	for key, required := range doc.every {
		for _, key := range key.Keys {
			setRequired(key, required, "")
		}
	}

	// the events doc says where its entries run on its scope key's default
	if scope := doc.docs.Entry.Keys["scope"]; scope != nil && scope.Default != "" {
		doc.root(doc.docs.Entry, "root = "+scope.Default)
	}
}

// shape reads what the lines say together, before the skeleton: a line continuing the comment of
// the code line above it, a bullet the more-indented lines below nest in, a run of value-list items
// after a label or under a key, and a markdown table naming words.
func (doc *docReader) shape() {
	lines := doc.lines
	for i := 1; i < len(lines); i++ {
		// a note at the comment's column, or starting lower-case below a comment that runs on
		above := i - 1
		for above > 0 && lines[above].kind == lineSkip && len(lines[above].keys) == 0 {
			above--
		}
		prev := &lines[above]
		if lines[i].kind == lineNote && !lines[i].label && prev.kind == lineCode && prev.note != "" {
			runsOn := !strings.HasSuffix(prev.note, ".") && !strings.HasSuffix(prev.note, ":") && strings.IndexFunc(lines[i].note, unicode.IsLower) == 0
			if lines[i].textCol == prev.commentCol || runsOn {
				prev.note += " " + lines[i].note
				lines[i].kind, lines[i].keys = lineSkip, nil
			}
		}
	}

	// a bullet key line followed by a more-indented code line holds it
	for i, line := range lines {
		if line.kind != lineCode || !line.bullet || line.opens != 0 || line.op != ":" {
			continue
		}
		for _, next := range lines[i+1:] {
			if next.kind == lineCode {
				lines[i].parent = next.col > line.col
				break
			}
		}
	}

	// value-list items: bullets with no type, one-word lines, after a label or under a key
	for i := 1; i < len(lines); i++ {
		prev := i - 1
		for prev > 0 && (lines[prev].kind == lineBlank || lines[prev].kind == lineBreak) {
			prev--
		}
		after := lines[prev].label || lines[prev].kind == lineWord ||
			lines[prev].kind == lineCode && lines[prev].opens == 0 && lines[i].col > lines[prev].col
		switch line := &lines[i]; {
		case !after:
		case line.kind == lineCode && line.bullet && line.typ == "" && line.opens == 0 && !line.parent && len(line.keys) == 1:
			line.kind, line.note = lineWord, line.rest
		case line.kind == lineNote && line.bullet && !strings.Contains(line.note, "="):
			word, prose := itemOf(line.note)
			if word != "" {
				line.kind, line.keys, line.note = lineWord, []string{word}, prose
			}
		case line.kind == lineSkip && len(line.keys) == 1 && line.keys[0] != "":
			line.kind = lineWord
		}
	}

	// a markdown table whose first column is names: its rows are words, the second column each blurb
	for i := 0; i+2 < len(lines); i++ {
		if lines[i].kind != lineUnread || !tableRule.MatchString(lines[i+1].rest) {
			continue
		}
		end := i + 2
		for end < len(lines) && lines[end].kind == lineUnread && strings.HasPrefix(lines[end].rest, "|") {
			end++
		}
		rows := lines[i+2 : end]
		named := len(rows) > 0 && !slices.ContainsFunc(rows, func(row docLine) bool { return !jomini.IsDefName(cells(row.rest)[0]) })
		if !named {
			continue
		}
		lines[i].kind, lines[i+1].kind = lineSkip, lineSkip
		for j := range rows {
			row := cells(rows[j].rest)
			rows[j].kind, rows[j].keys = lineWord, []string{row[0]}
			if len(row) > 1 {
				rows[j].note = row[1]
			}
		}
		i = end - 1
	}
}

// itemOf reads a bullet item: `word`: prose, word: prose, word # prose, word - prose, or a word
// alone; a word followed by more words is prose.
func itemOf(text string) (word, prose string) {
	text = strings.TrimSpace(text)
	end := strings.IndexFunc(text, func(char rune) bool { return unicode.IsSpace(char) || char == ':' || char == '#' })
	if end < 0 {
		end = len(text)
	}
	word, rest := strings.Trim(text[:end], "`'\""), strings.TrimSpace(text[end:])
	separated := rest == "" || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "#") || strings.HasPrefix(rest, "- ") || strings.HasPrefix(rest, "– ")
	if !jomini.IsDefName(word) || !separated {
		return "", ""
	}
	return word, strings.TrimSpace(strings.TrimLeft(rest, " :#-–`"))
}

// cells is a table row's cells, trimmed, backticks off.
func cells(row string) []string {
	var out []string
	for cell := range strings.SplitSeq(strings.Trim(strings.TrimSpace(row), "|"), "|") {
		out = append(out, strings.Trim(strings.TrimSpace(cell), "`"))
	}
	return out
}

// locKeys keeps the loc key templates a doc writes: a word with one <…> joined by _ (rule_<key>,
// setting_<key>_desc), or key + _name.
func (doc *docReader) locKeys(text string) {
	var found []string
	for _, match := range locSaid.FindAllStringSubmatch(text, -1) {
		if word := match[1]; strings.Count(word, "<") == 1 && !(strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">")) {
			open, close := strings.IndexByte(word, '<'), strings.IndexByte(word, '>')
			found = append(found, word[:open]+"{id}"+word[close+1:])
		}
	}
	for _, match := range keyPlus.FindAllStringSubmatch(text, -1) {
		found = append(found, "{id}"+match[1])
	}
	for _, template := range found {
		if !slices.Contains(doc.docs.LocKeys, template) {
			doc.docs.LocKeys = append(doc.docs.LocKeys, template)
		}
	}
}

// skeleton writes a doc's code lines as bare script, line for line, for jomini to nest: k = v,
// k = {, }, v. Where the braces do not balance, a missing } is written where a line returns to its
// opener's column or a title starts at column 0 (owner), and an extra one is dropped. A bullet that
// holds the more-indented lines below opens a block they close by returning to its column.
func skeleton(lines []docLine, markdown bool) string {
	depth, balanced := 0, true
	for _, line := range lines {
		if line.kind == lineCode || line.kind == lineClose {
			depth += line.opens
			balanced = balanced && depth >= 0
		}
	}
	balanced = balanced && depth == 0

	var out strings.Builder
	var open, held []int // the columns of the blocks open where the braces do not balance, and of the bullets holding
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		heading := line.kind == lineTitle && line.col == 0 && (markdown || line.marks == 0)
		for len(held) > 0 && (heading || line.kind == lineCode && line.col <= held[len(held)-1]) {
			out.WriteString("} ")
			held = held[:len(held)-1]
		}
		for !balanced && len(open) > 0 && (heading || line.kind == lineCode && line.col <= open[len(open)-1]) {
			out.WriteString("} ")
			open = open[:len(open)-1]
		}
		switch {
		case line.kind == lineCode && line.parent:
			out.WriteString("k = {")
			held = append(held, line.col)
		case line.kind == lineCode && line.opens > 0:
			out.WriteString("k =")
			for range line.opens {
				out.WriteString(" {")
				open = append(open, line.col)
			}
		case line.kind == lineCode && balanced:
			out.WriteString("k = v")
			out.WriteString(strings.Repeat(" }", -line.opens))
		case line.kind == lineCode:
			out.WriteString("k = v")
		case line.kind == lineClose && balanced:
			out.WriteString(strings.Repeat("} ", -line.opens))
		case line.kind == lineClose:
			closes := min(-line.opens, len(open))
			out.WriteString(strings.Repeat("} ", closes))
			open = open[:len(open)-closes]
		case line.kind == lineItem:
			out.WriteString("v")
		}
	}
	out.WriteString(strings.Repeat(" }", len(held)))
	if !balanced {
		out.WriteString(strings.Repeat(" }", len(open)))
	}
	return out.String()
}

// walk reads the statements of one block, lines from up to to, into the positions below parent;
// top is the doc's outermost level, where an opener may be the entry itself.
func (doc *docReader) walk(stmts []jomini.Statement, parent *Key, from, to int, top bool) {
	for _, st := range stmts {
		first, last := doc.at.PositionAt(st.NodeRange().Start).Line, doc.at.PositionAt(st.NodeRange().End).Line
		doc.gap(from, first, parent)
		from = last + 1
		line, bare := doc.lines[first], false
		var value jomini.Value
		switch st := st.(type) {
		case *jomini.Assignment:
			value = st.Value
		case *jomini.ValueStmt:
			value, bare = st.Value, true
		}
		block := jomini.BlockOf(value)
		if bare {
			// a line opening two blocks nests the second in the first; <effects> alone inside a block
			// says what the block holds, its comment is a note on the block, and a placeholder no type
			// reads is unread
			if block != nil {
				doc.walk(block.Statements, parent, first+1, last, false)
				continue
			}
			if typ, _ := typeOf(line.rest); top || typ == "" {
				doc.unread[doc.file+":"+strconv.Itoa(line.number)]++
			} else {
				doc.typed(parent, typ, line.number)
			}
			if line.note != "" {
				doc.pending = append(doc.pending, line.note)
			}
			continue
		}

		// any operator but = and : is an example statement, whose block makes nothing; root = x and
		// scope:actor = x are notes about the position below, a saved scope kept for its block
		if line.op != "=" && line.op != ":" {
			continue
		}
		key := line.keys[0]
		lower := strings.ToLower(key)
		doc.values[strings.TrimSpace(line.rest)] = true
		if saved := savedSaid.FindStringSubmatch(lower); saved != nil && block == nil {
			if doc.saved == nil {
				doc.saved = map[string]string{}
			}
			doc.saved[saved[1]] = doc.scopeOf(line.rest)
			doc.pending = append(doc.pending, joinProse([]string{key + " " + line.op + " " + line.rest, line.note}))
			continue
		}
		if (lower == "root" || lower == "scope" && !doc.inner[lower]) && block == nil {
			doc.pending = append(doc.pending, joinProse([]string{key + " " + line.op + " " + line.rest, line.note}))
			continue
		}
		doc.settle(!doc.section)
		above := joinProse(doc.pending)
		doc.pending = nil

		// a wrapper's keys are entries; a top-level opener is the entry itself where the doc wraps
		// one, where vanilla writes the word as an entry's name, or where its block holds keys
		// vanilla writes at its entries' top; otherwise it is a key
		if block != nil && top && slices.Contains(doc.wrappers, lower) {
			doc.walk(block.Statements, parent, first+1, last, true)
			continue
		}
		if block != nil && top && (doc.wrap || doc.marker(key, line, true) || doc.tops[lower] || doc.holdsEntryKeys(block)) {
			doc.open = []opened{{key: doc.docs.Entry, prose: joinProse([]string{above, line.note})}}
			doc.walk(block.Statements, doc.docs.Entry, first+1, last, false)
			continue
		}

		// a placeholder inside a block is a position of names; a number or a date, a position of
		// numbers or dates
		var children []*Key
		for _, name := range line.keys {
			switch {
			case doc.marker(name, line, block != nil) && block != nil && !strings.HasPrefix(name, "<"):
				// a placeholder opening a block marks a nested position, its word kept to name it
				name = "<" + name + ">"
			case doc.marker(name, line, block != nil) && !strings.HasPrefix(name, "<"):
				name = "<name>"
			case doc.marker(name, line, block != nil):
			case jomini.Literal(name) == jomini.LitNumber:
				name = "<number>"
			case jomini.Literal(name) == jomini.LitDate:
				name = "<date>"
			}
			children = append(children, parent.child(name))
		}
		child := children[0]
		prose := joinProse([]string{above, doc.value(child, line)})
		for _, other := range children[1:] {
			doc.value(other, line)
		}
		if block == nil {
			for _, other := range children {
				doc.note(other, prose)
			}
			continue
		}
		if doc.saved != nil {
			child.Saved, doc.saved = doc.saved, nil
		}

		// a block of declared commands, event targets, prefixed names or parameters is an example
		// body: it makes no keys, and says what the block holds
		if held, ok := doc.example(block); ok {
			child.Written, child.Block = WrittenBlock, cmp.Or(child.Block, held)
			doc.note(child, prose)
			continue
		}
		doc.open = []opened{{key: child, prose: prose}}
		doc.walk(block.Statements, child, first+1, last, false)
		for _, other := range children[1:] {
			other.Written, other.Block, other.Keys = child.Written, child.Block, child.Keys
		}
	}
	doc.gap(from, to, parent)
	if !top && to < len(doc.lines) && doc.lines[to].kind == lineClose && !doc.section {
		doc.settle(false)
	}
}

// holdsEntryKeys reports a block holding a key vanilla writes at the top of the folder's entries.
func (doc *docReader) holdsEntryKeys(block *jomini.Block) bool {
	return slices.ContainsFunc(block.Statements, func(st jomini.Statement) bool {
		line := doc.lines[doc.at.PositionAt(st.NodeRange().Start).Line]
		return len(line.keys) > 0 && doc.entryTops[strings.ToLower(line.keys[0])]
	})
}

// example reports a block whose keys are all declared commands, event targets, prefixed names or
// parameters, and what its commands make it: effects, triggers or modifiers.
func (doc *docReader) example(block *jomini.Block) (Block, bool) {
	held, keys := BlockNone, 0
	for _, st := range block.Statements {
		line := doc.lines[doc.at.PositionAt(st.NodeRange().Start).Line]
		if len(line.keys) == 0 {
			continue
		}
		keys++
		name := strings.ToLower(line.keys[0])
		_, effect := doc.dumps.Effects[name]
		_, trigger := doc.dumps.Triggers[name]
		_, modifier := doc.dumps.Modifier(name)
		_, target := doc.dumps.EventTargets[name]
		_, prefixed := jomini.SplitPrefixed(name)
		switch {
		case effect && !trigger:
			held = BlockEffects
		case trigger && !effect:
			held = cmp.Or(held, BlockTriggers)
		case modifier && !effect && !trigger:
			held = cmp.Or(held, BlockModifiers)
		case effect, trigger, target, prefixed, len(jomini.Interpolations(name)) > 0:
		default:
			return BlockNone, false
		}
	}
	return held, keys > 0
}

// gap reads the lines between two statements: notes wait for the statement below them; a blank line
// or a title gives them to what is open, else to the title's paragraph of About; a brief opens the
// positions it names below parent, a markdown heading naming a key its section, and a value list's
// items give the open key its words.
func (doc *docReader) gap(from, to int, parent *Key) {
	for _, line := range doc.lines[from:max(from, to)] {
		switch line.kind {
		case lineNote:
			if line.label {
				doc.label = line.note
			}
			if !doc.section || line.col == 0 {
				doc.pending = append(doc.pending, line.note)
			}
		case lineBreak, lineBlank:
			if doc.section || line.kind == lineBreak {
				doc.pending = append(doc.pending, "")
			} else {
				doc.settle(false)
			}
		case lineRule:
			if !doc.section {
				doc.settle(false)
			}
		case lineTitle, lineSection:
			doc.settle(false)
			doc.label = ""
			if line.kind == lineTitle {
				doc.title, doc.titled = line.note, false
				doc.nestedTitle(line.note)
				continue
			}
			for len(doc.headings) > 0 && doc.headings[len(doc.headings)-1].level >= line.marks {
				doc.headings = doc.headings[:len(doc.headings)-1]
			}
			holder := doc.docs.Entry
			if len(doc.headings) > 0 {
				holder = doc.headings[len(doc.headings)-1].key
			}
			key := holder.child(line.keys[0])
			doc.typed(key, line.typ, line.number)
			doc.need(key, line.typ)
			doc.headings = append(doc.headings, heading{level: line.marks, key: key})
			doc.open, doc.section = []opened{{key: key}}, true
		case lineBrief:
			// a brief's notes are the ones above it, unless something before it is still open
			doc.settle(!doc.section && len(doc.open) == 0)
			above := joinProse(doc.pending)
			doc.pending = nil
			for _, name := range line.keys {
				if jomini.IsDefName(name) {
					key := parent.child(name)
					doc.typed(key, line.typ, line.number)
					doc.need(key, line.typ)
					doc.open = append(doc.open, opened{key: key, prose: above})
				}
			}
		case lineWord:
			doc.word(line, parent)
		case lineUnread, lineSkip:
			if line.kind == lineUnread || len(line.keys) == 1 && line.keys[0] != "" {
				doc.unread[doc.file+":"+strconv.Itoa(line.number)]++
			}
		}
	}
}

// word reads a value list's item: a word the open key takes, else the key the label names; with no
// key, a name - blurb line is the folder's blurb for the name.
func (doc *docReader) word(line docLine, parent *Key) {
	word := line.keys[0]
	keys := make([]*Key, 0, len(doc.open))
	for _, got := range doc.open {
		keys = append(keys, got.key)
	}
	if len(keys) == 0 {
		if named := doc.labelKey(parent); named != nil {
			keys = append(keys, named)
		}
	}
	if line.note != "" {
		doc.docs.Blurbs[strings.ToLower(word)] = cmp.Or(doc.docs.Blurbs[strings.ToLower(word)], line.note)
	}
	if len(keys) == 0 && line.note == "" {
		doc.unread[doc.file+":"+strconv.Itoa(line.number)]++
	}
	for _, key := range keys {
		mergeTake(key, Take{Type: WordEnum, Enum: []string{word}, Source: SourceDocs})
	}
}

// labelKey is the key a label's words name, among the keys of parent and of the entry: Valid
// categories: names category, Scope can be one of: names scope.
func (doc *docReader) labelKey(parent *Key) *Key {
	for _, word := range strings.Fields(strings.ToLower(strings.TrimSuffix(doc.label, ":"))) {
		for _, name := range []string{word, strings.TrimSuffix(word, "s"), strings.TrimSuffix(word, "ies") + "y"} {
			for _, holder := range []*Key{parent, doc.docs.Entry} {
				if key := holder.Keys[name]; key != nil {
					return key
				}
			}
		}
	}
	return nil
}

// nestedTitle opens the nested position a title names, by a Nested row's word for its database
// (EU5 laws' Policies:).
func (doc *docReader) nestedTitle(title string) {
	for _, word := range strings.Fields(strings.ToLower(strings.TrimSuffix(title, ":"))) {
		for _, nested := range doc.nested {
			if nested.Database == "" || !slices.Contains([]string{nested.Database, nested.Database + "s", strings.TrimSuffix(nested.Database, "y") + "ies"}, word) {
				continue
			}
			key := doc.docs.Entry
			for _, segment := range strings.Split(nested.Path, "/") {
				if segment != "" && segment != "*" {
					key = key.child(segment)
				}
			}
			doc.open = []opened{{key: key.child("<name>")}}
			return
		}
	}
}

// settle gives the positions still open the notes waiting, unless a statement below claims them;
// with nothing open, the notes are the title's paragraph of About.
func (doc *docReader) settle(claimed bool) {
	for _, got := range doc.open {
		if claimed {
			doc.note(got.key, got.prose)
		} else {
			doc.note(got.key, joinProse([]string{got.prose, joinProse(doc.pending)}))
		}
	}
	if !claimed {
		if len(doc.open) == 0 {
			doc.about(joinProse(doc.pending))
		}
		doc.pending = nil
	}
	doc.open, doc.section = nil, false
}

// about adds a paragraph to the folder's About, headed by the title it is under.
func (doc *docReader) about(text string) {
	if text == "" {
		return
	}
	if doc.title != "" && !doc.titled {
		text, doc.titled = doc.title+"\n"+text, true
	}
	if !strings.Contains(doc.docs.About, text) {
		doc.docs.About = strings.TrimSpace(doc.docs.About + "\n\n" + text)
	}
}

// marker reports a placeholder: <…> or <a>.<b>; a name with a my, example, some, your, new, another,
// unique, name_of or test word, or ending in _ and a number; a capital letter the doc has not written
// as a value (left_portrait = X describes X, the value); a name ending _name or
// _key that opens a block; or an opener whose own comment calls it a name the modder chooses. A word
// vanilla writes as a key in the folder is none, and _type, _id and _tag keys are real.
func (doc *docReader) marker(key string, line docLine, opens bool) bool {
	if strings.HasPrefix(key, "<") {
		return true
	}
	lower := strings.ToLower(key)
	if doc.inner[lower] || strings.HasSuffix(lower, "_type") || strings.HasSuffix(lower, "_id") || strings.HasSuffix(lower, "_tag") {
		return false
	}
	words := strings.Split(lower, "_")
	switch {
	case len(key) == 1 && unicode.IsUpper(rune(key[0])):
		return !doc.values[key]
	case slices.ContainsFunc(words, func(word string) bool {
		return slices.Contains([]string{"my", "example", "some", "your", "new", "another", "unique", "test"}, word)
	}), strings.Contains(lower, "name_of"):
		return true
	case len(words) > 1 && jomini.Literal(words[len(words)-1]) == jomini.LitNumber:
		return true
	case opens && (strings.HasSuffix(lower, "_name") || strings.HasSuffix(lower, "_key")):
		return true
	case opens && chooses.MatchString(line.note):
		return true
	}
	return false
}

// child is the key of that name in the key's block, made where it is first written.
func (key *Key) child(name string) *Key {
	if key.Keys == nil {
		key.Keys = map[string]*Key{}
	}
	if key.Keys[name] == nil {
		key.Keys[name] = &Key{Name: name, Source: SourceDocs}
	}
	return key.Keys[name]
}

// note gives a position the prose written for it, a second prose as a paragraph after the first, and
// the need, default, root scope and folder the prose states.
func (doc *docReader) note(key *Key, prose string) {
	if prose == "" {
		return
	}
	if !strings.Contains(key.Doc, prose) {
		key.Doc = strings.TrimSpace(key.Doc + "\n\n" + prose)
	}

	// where nothing states a type, the prose's first words do
	if typ := proseType(prose); typ != "" && key != doc.docs.Entry && len(key.Takes) == 0 && key.Written == WrittenNone {
		doc.typed(key, typ, 0)
	}
	doc.need(key, prose)
	doc.root(key, prose)
	for _, match := range pathSaid.FindAllStringSubmatch(strings.ToLower(prose), -1) {
		if _, ok := doc.scripts.Tops[match[1]]; ok {
			mergeTake(key, Take{Type: WordReference, Database: match[1], Source: SourceDocs})
		}
	}
}

// need reads whether prose lets its position be left out, and the default it takes then.
func (doc *docReader) need(key *Key, prose string) {
	if said := defaultSaid.FindStringSubmatch(prose); said != nil && key.Default == "" {
		if word := strings.TrimRight(said[1], "."); doc.isDefault(key, word) {
			key.Default = strings.Trim(word, "\"`")
		}
	}
	doc.required(key, prose)
}

// isDefault reports a word a default may be: a literal, one of the key's words, a declared scope, a
// name with _ or a digit, or NDefines::A::B; never an English word.
func (doc *docReader) isDefault(key *Key, word string) bool {
	trimmed := strings.Trim(word, "\"`")
	enum := slices.ContainsFunc(key.Takes, func(take Take) bool { return slices.Contains(take.Enum, trimmed) }) || doc.dumps.IsScope(word)
	return strings.HasPrefix(word, "\"") || strings.HasPrefix(word, "`") || strings.HasPrefix(word, "<") ||
		strings.HasPrefix(word, "NDefines::") || jomini.Literal(word) != jomini.NotLiteral || word == "yes" || word == "no" ||
		enum || strings.ContainsFunc(word, func(char rune) bool { return char == '_' || unicode.IsDigit(char) })
}

// required reads, clause by clause, whether prose lets its position be left out. Optional is a clause
// opening with the word, the doc's own adjective (optional list of, [Optional]); required is read
// anywhere in a clause, with the condition that follows it. (optional) after a scope's name is the
// scope's. A default, or what happens when it is omitted, makes it optional by inference.
func (doc *docReader) required(key *Key, prose string) {
	low := scopeOptional.ReplaceAllStringFunc(strings.ToLower(prose), func(match string) string {
		if word := strings.Fields(match)[0]; strings.HasPrefix(word, "scope:") || doc.dumps.IsScope(word) && strings.HasSuffix(match, "=") {
			return strings.Replace(match, "(optional)", "", 1)
		}
		return match
	})
	parts := func(char rune) bool { return strings.ContainsRune(".;!?,()[]<>", char) }
	for clause := range strings.FieldsFuncSeq(low, parts) {
		clause = strings.TrimSpace(clause)
		if all := needAll.FindStringSubmatch(clause); all != nil {
			doc.every[key] = RequiredAlways
			if all[1] == "optional" {
				doc.every[key] = RequiredOptional
			}
			continue
		}
		if cond := needIf.FindStringSubmatch(clause); cond != nil {
			setRequired(key, RequiredWhen, cond[1])
			continue
		}
		if needWord.MatchString(clause) {
			setRequired(key, RequiredOptional, "")
			continue
		}
		if needError.MatchString(clause) {
			setRequired(key, RequiredAlways, "")
			continue
		}
		word := needSet.FindStringSubmatch(clause)
		switch {
		case word == nil:
		case word[2] == "" || slices.Contains([]string{"and", "but", "here", "always"}, word[2]) ||
			word[2] == "to" && strings.HasPrefix(word[3], "be "):
			setRequired(key, RequiredAlways, "")
		case slices.Contains([]string{"for", "if", "when", "in", "on", "only", "unless", "with"}, word[2]):
			setRequired(key, RequiredWhen, strings.TrimSpace(word[2]+" "+word[3]))
		}
	}
	if key.Default != "" || needOmitted.MatchString(low) {
		setRequired(key, RequiredOptional, "")
	}
}

// setRequired gives a key a Required: of two, the stronger stays, so an optional that a default
// implies only fills a silence.
func setRequired(key *Key, required Required, when string) {
	if required > key.Required {
		key.Required, key.When = required, when
	}
}

// root sets where a block's statements run from what a line says: root: ( Character ), root = the
// phrase whose last word is a declared scope (owning country), or X scope in the key's own prose.
func (doc *docReader) root(key *Key, said string) {
	if key.Scope != "" {
		return
	}
	if match := rootSaid.FindStringSubmatch(said); match != nil {
		words := strings.Fields(strings.ToLower(match[1]))
		if last := words[len(words)-1]; doc.dumps.IsScope(last) {
			key.Scope = last
			return
		}
	}
	for _, match := range scopeSaid.FindAllStringSubmatch(said, -1) {
		if scope := strings.ToLower(match[1]); doc.dumps.IsScope(scope) {
			key.Scope = scope
			return
		}
	}
}

// scopeOf is the declared scope a saved scope's line names by its last word, "" for none.
func (doc *docReader) scopeOf(text string) string {
	words := strings.Fields(strings.ToLower(strings.Trim(text, "<>()")))
	for n := len(words); n > 0; n-- {
		if doc.dumps.IsScope(words[n-1]) {
			return words[n-1]
		}
	}
	return ""
}

// value reads the type a key line writes after its key into the position, and gives back its prose:
// its comment, or the sentence written as its value. Where the value states no type, the prose's
// first words do; { <X> } whose X is no type is a block of X's contents.
func (doc *docReader) value(key *Key, line docLine) string {
	prose := line.note
	switch {
	case line.typ != "":
		doc.typed(key, line.typ, line.number)
	case strings.HasPrefix(line.rest, "{") && strings.Contains(line.rest, "<") && strings.Contains(line.rest, "}"):
		inner := strings.TrimSpace(strings.Trim(line.rest[:strings.LastIndexByte(line.rest, '}')+1], "{} "))
		if !typeWords(inner).stated {
			key.Written = WrittenBlock
		} else {
			doc.typed(key, inner, line.number)
		}
		prose = joinProse([]string{line.rest[strings.LastIndexByte(line.rest, '}')+1:], line.note})
	case strings.HasPrefix(line.rest, "{") && strings.Contains(line.rest, "}"):
		prose = joinProse([]string{line.rest[strings.LastIndexByte(line.rest, '}')+1:], line.note})
	case valueType.MatchString(line.rest) || strings.HasPrefix(line.rest, "<"):
		doc.typed(key, line.rest, line.number)
	case line.op == "=" && strings.ContainsAny(line.rest, " \t") && !strings.HasPrefix(line.rest, "{"):
		prose = joinProse([]string{line.rest, line.note})
	}
	return prose
}

// proseType is the type prose's first words name: [Trigger], Trigger in character scope, yes/no, …,
// Integer., script value for …; "" where they name none.
func proseType(prose string) string {
	words := strings.Fields(strings.TrimLeft(prose, "[("))
	for n := min(3, len(words)); n > 0; n-- {
		phrase := strings.TrimRight(strings.Join(words[:n], " "), ".,:;])")
		if typeWords(phrase).stated {
			return phrase
		}
	}
	return ""
}

// typed reads a type the doc names into the position, and reports whether it named one: type words,
// else words naming a database, kept as written for the build to join (government key, <unit>), or a
// folder path. A type the reader cannot read is unread.
func (doc *docReader) typed(key *Key, text string, number int) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	said := typeWords(text)
	if !said.stated {
		lower := strings.ToLower(strings.TrimSpace(text))
		if first, _, found := strings.Cut(strings.TrimPrefix(lower, "/"), "/"); found && doc.roots[first] {
			if match := pathSaid.FindStringSubmatch(lower); match != nil {
				said = stated{takes: []Take{{Type: WordReference, Database: match[1], Source: SourceDocs}}, stated: true}
			}
		} else if words := referenceWords(text); words != "" {
			said = stated{takes: []Take{{Type: WordReference, Database: words, Source: SourceDocs}}, stated: true}
		}
	}
	if !said.stated {
		doc.unread[doc.file+":"+strconv.Itoa(number)]++
		return false
	}
	if said.block != BlockNone {
		key.Written, key.Block = WrittenBlock, cmp.Or(key.Block, said.block)
	}
	for _, take := range said.takes {
		mergeTake(key, take)
	}
	return true
}

// referenceWords is a type's words as a name for a database: lower-cased, <> and _ off, one to five
// words of letters; "" where the text is none.
func referenceWords(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if open := strings.IndexByte(text, '('); open > 0 {
		text = text[:open]
	}
	text = strings.Trim(strings.ReplaceAll(text, "_", " "), "<>{}[] ")
	words := strings.Fields(text)
	if len(words) == 0 || len(words) > 5 || slices.ContainsFunc(words, func(word string) bool {
		return strings.ContainsFunc(word, func(char rune) bool { return !unicode.IsLetter(char) })
	}) {
		return ""
	}
	return strings.Join(words, " ")
}

// mergeTake adds a take to a key: enum words join the key's one enum take, and a take it has is not
// added twice.
func mergeTake(key *Key, take Take) {
	for i, had := range key.Takes {
		if had.Type != take.Type || had.Database != take.Database {
			continue
		}
		for _, word := range take.Enum {
			if !slices.Contains(had.Enum, word) {
				key.Takes[i].Enum = append(key.Takes[i].Enum, word)
			}
		}
		key.Takes[i].Guessed = had.Guessed && take.Guessed
		return
	}
	key.Takes = append(key.Takes, take)
}

// typeWords reads the words a doc writes for a type: a spelling of the table, X block as X, a plural
// as its singular, a/b/c or a|b|c whose parts are spellings (those types; other parts beside them go),
// else a set of words (open where it ends in etc), list of X. "optional" and the like are no part of
// it.
func typeWords(text string) stated {
	var parts []string
	for part := range strings.SplitSeq(strings.ToLower(strings.Trim(strings.TrimSpace(text), "<>{}[] ")), ",") {
		part = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(part), "optional "), "required ")
		if !slices.Contains([]string{"optional", "situational", "required", "opt", "mandatory"}, part) {
			parts = append(parts, part)
		}
	}
	word := strings.Join(parts, ", ")
	for _, plural := range []string{"array of ", "list of "} {
		if rest, ok := strings.CutPrefix(word, plural); ok {
			word = strings.TrimSuffix(rest, "s")
		}
	}
	for _, form := range []string{word, strings.TrimSuffix(word, " block"), strings.TrimSuffix(word, "s")} {
		if spelled, ok := typeSpellings[form]; ok {
			return spelled
		}
	}

	// a/b/c or a|b|c: the spellings among the parts are those types; else lower-case words, two or
	// more, or one where the last is etc
	words := strings.FieldsFunc(word, func(char rune) bool { return char == '/' || char == '|' })
	for i := range words {
		words[i] = strings.TrimSpace(words[i])
	}
	var union stated
	for i := 0; i < len(words); i++ {
		part := words[i]
		if part == "yes" && i+1 < len(words) && words[i+1] == "no" {
			part, i = "yes/no", i+1
		}
		if spelled, ok := typeSpellings[strings.TrimSuffix(part, " block")]; ok {
			union.stated, union.block = true, cmp.Or(union.block, spelled.block)
			union.takes = append(union.takes, spelled.takes...)
		}
	}
	if union.stated {
		return union
	}
	open := len(words) > 0 && slices.Contains([]string{"etc", "etc.", "..."}, words[len(words)-1])
	if open {
		words = words[:len(words)-1]
	}
	tooFew := len(words) < 2 && !(open && len(words) == 1)
	if tooFew || slices.ContainsFunc(words, func(word string) bool { return !jomini.IsLowerName(word) }) {
		return stated{}
	}
	enum := stated{takes: []Take{{Type: WordEnum, Enum: words, Source: SourceDocs}}, stated: true}
	if open {
		enum.takes[0].Guessed = true
	}
	return enum
}

// joinProse joins lines with a space and paragraphs, marked by an empty line, with a blank line.
func joinProse(lines []string) string {
	var out strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "" && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n"):
			out.WriteString("\n\n")
		case line == "":
		case out.Len() > 0 && !strings.HasSuffix(out.String(), "\n"):
			out.WriteByte(' ')
			out.WriteString(line)
		default:
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}
