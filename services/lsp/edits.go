// edits.go formats a script file, offers quick fixes and builds override
// copies, as edits in Monaco's ranges.

package lsp

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// Fix is one quick fix, with its edits grouped by file.
type Fix struct {
	Title string `json:"title"`
	Code  string `json:"code"`

	Edits map[string][]Edit `json:"edits"`

	// Creates is a file made first. It is "" for none.
	Creates string `json:"creates,omitempty"`

	// Reveal is where the caret goes after the fix, or nil.
	Reveal *Site `json:"reveal,omitempty"`

	// Note says what the fix leaves the game doing. It is "" for none.
	Note string `json:"note,omitempty"`
}

// Draft is the file New file makes in a folder of a mod.
type Draft struct {
	Name string `json:"name"`

	// Text is the header of a localization file, the namespace line in an
	// events folder, else empty.
	Text files.Text `json:"text"`
}

// Format returns the edits that format a script file, keeping its
// indentation. It returns none for a read-only file, a non-script file or one
// that does not parse.
func (s *Service) Format(path string, tabSize int, insertSpaces bool) ([]Edit, error) {
	ws, tree, err := s.loadTree(path)
	if err != nil {
		return nil, err
	}

	if place := ws.Locate(ws.Mods.Load(), path); place.ReadOnly || !place.Format.IsScript() || len(tree.Problems) > 0 {
		return []Edit{}, nil
	}

	unit := tree.Indentation()
	if unit == "" {
		unit = "\t"
		if insertSpaces && tabSize > 0 {
			unit = strings.Repeat(" ", tabSize)
		}
	}

	return ToEdits(tree.Src, tree.Lines, tree.Format(unit)), nil
}

// DescribeIndentation returns what a file indents with, "Tabs" or "4 spaces",
// or "" for none.
func (s *Service) DescribeIndentation(path string) (string, error) {
	_, tree, err := s.loadTree(path)
	if err != nil {
		return "", err
	}

	switch unit := tree.Indentation(); {
	case unit == "":
		return "", nil
	case unit[0] == '\t':
		return "Tabs", nil
	default:
		return strconv.Itoa(len(unit)) + " spaces", nil
	}
}

// ListFixes returns a quick fix for each problem that has one.
func (s *Service) ListFixes(path string, diagnostics []Diagnostic) ([]Fix, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	walk := func(diagnostic Diagnostic) cursor {
		return findCursor(document, jomini.ToOffset(tree.Src, tree.Lines, diagnostic.Range.StartLine, diagnostic.Range.StartColumn))
	}

	fixes := []Fix{}
	for _, diagnostic := range diagnostics {
		var title string
		var edits []Edit
		switch diagnostic.Code {
		case index.ProblemUnresolved:
			title, edits = buildReferenceFix(ws.Schema, mods, walk(diagnostic), diagnostic)
		case index.ProblemRequired:
			title, edits = buildMissingFix(ws.Schema, walk(diagnostic), diagnostic)
		case lang.ProblemMode:
			title, edits = "Remove mode", []Edit{{Range: diagnostic.Range}}
		}

		if len(edits) > 0 {
			fixes = append(fixes, Fix{Title: title, Code: diagnostic.Code, Edits: map[string][]Edit{path: edits}})
		}
	}

	if position := slices.IndexFunc(diagnostics, func(diagnostic Diagnostic) bool { return keyFacts[diagnostic.Code] != "" }); position >= 0 {
		if fix, ok := s.buildKeysFix(ws, mods, place, diagnostics[position].Code); ok {
			fixes = append(fixes, fix)
		}
	}

	return fixes, nil
}

// buildKeysFix returns the fix that adds the file's keys that lack
// base-language text to its mod's localization file, or false if there are
// none or nowhere to write them.
func (s *Service) buildKeysFix(
	ws *workspace.Session, mods *index.Index, place workspace.Place, code string,
) (Fix, bool) {
	mod, ok := s.Workspace.LookupMod(place.Origin)
	if !ok || place.ReadOnly || mod.Descriptor.Name == "" {
		return Fix{}, false
	}

	target, ok := workspace.FindLocalizationFile(ws.Game, mod.Dir, mod.Descriptor.Name, loc.BaseLanguage)
	if !ok {
		return Fix{}, false
	}

	diagnostics, _ := DiagnoseFile(ws, mods, place.Path)
	var keys []string
	for _, diagnostic := range s.listEnabled(ws.ID, diagnostics) {
		fact, ok := keyFacts[diagnostic.Code]
		if key := diagnostic.Facts[fact]; ok && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return Fix{}, false
	}

	text, _ := ws.LoadSource(target)
	subject := keys[0]
	if len(keys) > 1 {
		subject = fmt.Sprintf("%d missing keys", len(keys))
	}

	title, creates := fmt.Sprintf("Add %s to %s", subject, filepath.Base(target)), ""
	if strings.TrimSpace(text) == "" {
		creates, title, text = target, title+", a new file", loc.Encode(loc.BaseLanguage, nil)
	}

	messages := make([]loc.Message, len(keys))
	for position, key := range keys {
		messages[position] = loc.Message{Key: key}
	}

	addition := loc.EncodeAddition(text, messages)
	fix := buildAppendFix(ws, mods, target, text, addition, strings.Index(addition, `""`)+1)
	fix.Code, fix.Title, fix.Creates = code, title, creates

	return fix, true
}

// buildAppendFix returns the fix that appends text to a file, with the caret
// the given number of characters into it.
func buildAppendFix(ws *workspace.Session, mods *index.Index, target, source, addition string, caret int) Fix {
	end := jomini.Span{Start: len(source), End: len(source)}
	whole := source + addition
	point := jomini.Span{Start: len(source) + caret, End: len(source) + caret}
	reveal := jomini.ToRange(whole, jomini.NewLines(whole), point)

	return Fix{
		Edits:  map[string][]Edit{target: {{Range: jomini.ToRange(source, jomini.NewLines(source), end), Text: addition}}},
		Reveal: &Site{Place: ws.Locate(mods, target), Line: reveal.StartLine, Range: reveal},
	}
}

// fixDistance is the edit distance a fix must be under.
const fixDistance = 3

// buildReferenceFix returns the fix that changes a reference nothing defines
// to the nearest name its database defines, with the block's commands for a
// callable database. The edits are empty if none is near.
func buildReferenceFix(
	language *lang.Schema, mods *index.Index, cursor cursor, diagnostic Diagnostic,
) (string, []Edit) {
	diff := diffmatchpatch.New()
	best, nearest := fixDistance, ""
	for _, reference := range cursor.context.References {
		if reference.Span.Start != cursor.offset {
			continue
		}

		name := mods.ToCaseless(reference.Database, reference.Name)
		length := utf8.RuneCountInString(name)

		var keys []string
		for _, site := range mods.ListDefinitions(reference.Database) {
			keys = append(keys, mods.ToReference(site).Name)
		}
		if database := language.Databases[reference.Database]; database != nil && database.Declaration.Command() != "" {
			for _, dump := range commandDatabases {
				if cursor.context.Parent.Takes(dump) {
					keys = slices.Concat(keys, language.ListKeys(dump))
				}
			}
		}

		tried := map[string]bool{}
		for _, key := range keys {
			if tried[key] {
				continue
			}

			tried[key] = true

			// a length gap of best or more is too far
			if max(utf8.RuneCountInString(key)-length, length-utf8.RuneCountInString(key)) >= best {
				continue
			}

			if distance := diff.DiffLevenshtein(diff.DiffMain(name, key, false)); distance > 0 && distance < best {
				best, nearest = distance, key
			}
		}
	}

	if nearest == "" {
		return "", nil
	}

	return "Change to " + nearest, []Edit{{Range: diagnostic.Range, Text: nearest}}
}

// buildMissingFix returns the fix that adds a key the definition must write,
// with its default, or no edits if it has none.
func buildMissingFix(schema *lang.Schema, cursor cursor, diagnostic Diagnostic) (string, []Edit) {
	tree, definition, key := cursor.tree, cursor.context.Definition, diagnostic.Facts["key"]
	if definition == nil || !tree.Statements[definition.Statement].IsBlock() {
		return "", nil
	}

	database := schema.Databases[definition.Database]
	if database == nil || database.Field.Children[key] == nil || !database.Field.Children[key].Default.IsStated() {
		return "", nil
	}

	statement := key + " = " + database.Field.Children[key].Default.Value
	insertion := tree.BuildInsertion(definition.Statement, []string{statement})

	return "Add " + statement, ToEdits(tree.Src, tree.Lines, []jomini.Edit{insertion})
}

// hasFolder reports whether a database's entries are written in a folder of
// files, which a mod can add a file to, rather than in one file.
func hasFolder(database *lang.Database) bool {
	return database != nil && database.Declaration.Folder != "" && path.Ext(database.Declaration.Folder) == ""
}

// DraftFile returns the file New file makes in a folder of a mod. It returns
// an error if the folder is not in a writable mod.
func (s *Service) DraftFile(folder string) (Draft, error) {
	ws, err := s.Workspace.FindWritable(folder)
	if err != nil {
		return Draft{}, err
	}

	place := ws.Locate(ws.Mods.Load(), folder)
	mod, ok := s.Workspace.LookupMod(place.Origin)
	if !ok {
		return Draft{}, fmt.Errorf("the mod %s is not in the workspace", place.Origin)
	}

	if database := ws.Schema.Databases[place.Database]; hasFolder(database) {
		// the mod's file stem is its event namespace
		namespace := buildNamespace(database, resolveModStem(mod), "")
		base := path.Base(resolveEntryRel(mod, place.Rel, ""))

		return makeDraft(place.Rel, findFreeName(folder, base, path.Ext(base)), namespace), nil
	}

	if language := s.findLanguage(place.Rel); language != "" {
		ending := "_" + loc.ToLanguageKey(language) + ".yml"

		return makeDraft(place.Rel, findFreeName(folder, resolveModStem(mod)+ending, ending), loc.Encode(language, nil)), nil
	}

	extension := findScriptExtension(ws, place.Rel)

	return makeDraft(place.Rel, findFreeName(folder, "new_file"+extension, extension), ""), nil
}

func makeDraft(folderRel, name, text string) Draft {
	encoding := files.Classify(path.Join(folderRel, name)).DefaultEncoding()

	return Draft{Name: name, Text: files.Text{Text: text, Encoding: encoding}}
}

// findFreeName returns the given name, else numbered from 2 before the ending,
// until the folder lacks it.
func findFreeName(folder, base, ending string) string {
	stem := strings.TrimSuffix(base, ending)
	free := base
	for count := 2; files.IsFile(files.Join(folder, free)) || files.IsFolder(files.Join(folder, free)); count++ {
		free = stem + "_" + strconv.Itoa(count) + ending
	}

	return free
}

// findLanguage returns the language of a localization folder, or "" if the
// install lacks it.
func (s *Service) findLanguage(folder string) string {
	language := loc.FindFolderLanguage(folder + "/")
	languages, _ := s.Workspace.ListLanguages(s.Workspace.State().Install.ID)
	if slices.ContainsFunc(languages, func(candidate loc.Language) bool { return candidate.Folder == language }) {
		return language
	}

	return ""
}

// findScriptExtension returns the extension the game's files use in a folder,
// or .txt if they use none or several.
func findScriptExtension(ws *workspace.Session, folder string) string {
	parent := cmp.Or(folder, ".")

	var extensions []string
	for _, file := range ws.Install.Files {
		extension := strings.ToLower(path.Ext(file.Rel))
		_, isScript := lang.FileTypes[extension]
		if isScript && strings.EqualFold(path.Dir(file.Rel), parent) && !slices.Contains(extensions, extension) {
			extensions = append(extensions, extension)
		}
	}

	if len(extensions) == 1 {
		return extensions[0]
	}

	return ".txt"
}

// resolveEntryRel returns the path of the file a mod keeps its entries of a
// folder in.
func resolveEntryRel(mod workspace.Mod, folder, prefix string) string {
	return path.Join(folder, prefix+resolveModStem(mod)+"_"+path.Base(folder)+".txt")
}

// resolveModStem returns a mod's name for its files, as a slug.
func resolveModStem(mod workspace.Mod) string { return files.ToSlug(filepath.Base(mod.Dir)) }

// buildNamespace returns the line that declares an event key's namespace. It
// returns "" if the file declares it already, or the events do not apply.
func buildNamespace(database *lang.Database, key, source string) string {
	if database == nil || database.Declaration.Contents != lang.ContentsEvents {
		return ""
	}

	namespace, _, _ := strings.Cut(key, ".")
	tree := jomini.Parse(source)
	for statement := range tree.Children(0) {
		current := tree.Statements[statement]
		if tree.Unquoted(current.Key) == lang.EventNamespaceKey && tree.Unquoted(current.Value) == namespace {
			return ""
		}
	}

	return lang.EventNamespaceKey + " = " + namespace + "\n\n"
}

// BuildOverride returns the fix that copies the entry at a position into a mod
// so that the game keeps the copy, or nil if no entry is there. It returns an
// error if the mod has the entry already, the game joins every copy, or no
// file name makes the copy win.
//
// With no mod id it tries the first other writable mod. The editor then only
// asks whether an override is possible, so a refusal is nil, not an error.
func (s *Service) BuildOverride(path string, line, column int, modID string) (fix *Fix, err error) {
	defer func() {
		if modID == "" {
			err = nil
		}
	}()

	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	definition := findEntry(ws, document, jomini.ToOffset(tree.Src, tree.Lines, line, column))
	if definition == nil {
		return nil, nil
	}

	database := ws.Schema.Databases[definition.Database]
	mod, err := s.findOverrideMod(place, modID)
	if err != nil {
		return nil, err
	}

	sites := mods.Resolve(lang.Reference{Database: definition.Database, Name: definition.Name})
	if slices.ContainsFunc(sites, func(site index.Site) bool { return mods.File(site).Origin == mod.ID }) {
		return nil, fmt.Errorf("%s defines the %s %s already", ws.DescribeMod(mod.ID),
			lang.DescribeDefinition(definition.Database, false), definition.Name)
	}

	entry, side, err := writeOverride(tree, mods, definition, database.Precedence, sites)
	if err != nil {
		return nil, err
	}

	rivals := listRivals(mods, sites, database.Precedence)
	folder := resolveOverrideFolder(mods, place, database, rivals, side)
	prefix, ok := findPrefix(ws.Game, mods, mod, folder, side, rivals)
	if !ok {
		order := "after"
		if side < 0 {
			order = "before"
		}

		return nil, fmt.Errorf("no file name in %s makes the copy load %s the other copies", folder, order)
	}

	target := files.Join(mod.Dir, resolveEntryRel(mod, folder, prefix))
	source, _ := ws.LoadSource(target)
	override := buildEntryFix(ws, mods, target, source, buildNamespace(database, definition.Name, source)+entry)
	override.Title = "Override " + definition.Name + " in " + ws.DescribeMod(mod.ID)
	override.Note = database.Precedence.Describe()

	return &override, nil
}

// findEntry returns the innermost definition at an offset that a mod can
// override, or nil. A name script saves on the way, such as a scope, is no
// entry.
func findEntry(ws *workspace.Session, document lang.Document, offset int) *lang.Copy {
	context := findCursor(document, offset).context
	for context != nil && (!context.Defines || context.Category != lang.CategoryDefinition) {
		context = context.Parent
	}

	if context == nil || context.Definition.Statement == 0 || !hasFolder(ws.Schema.Databases[context.Definition.Database]) {
		return nil
	}

	return context.Definition
}

// findOverrideMod returns the mod with the given id, else the first writable
// mod that is not the file's own. It returns an error if there is none.
func (s *Service) findOverrideMod(place workspace.Place, modID string) (workspace.Mod, error) {
	mods := s.Workspace.ListMods()
	if modID == "" {
		position := slices.IndexFunc(mods, func(mod workspace.Mod) bool {
			return !mod.Broken && !mod.Workshop && mod.ID != place.Origin
		})
		if position < 0 {
			return workspace.Mod{}, errors.New("the workspace has no other mod that can hold the copy")
		}

		return mods[position], nil
	}

	position := slices.IndexFunc(mods, func(mod workspace.Mod) bool { return mod.ID == modID })
	if position < 0 || mods[position].Broken {
		return workspace.Mod{}, fmt.Errorf("the workspace has no mod %s with a folder", modID)
	}

	mod := mods[position]
	_, err := s.Workspace.FindWritable(mod.Dir)

	return mod, err
}

// writeOverride returns the text of a copy that overrides a definition, and
// where its file must load: 1 after the other copies, -1 before, 0 anywhere. It
// returns an error if the game joins every copy.
func writeOverride(
	tree *jomini.Tree, mods *index.Index, definition *lang.Copy, precedence lang.Precedence, sites []index.Site,
) (string, int, error) {
	entry := tree.Text(tree.Statements[definition.Statement].Span)
	switch {
	// an entry mode replaces the copies loaded before it
	case precedence.Modes != "":
		return buildModeCopy(tree, definition, precedence), 1, nil

	case precedence.Keeps == lang.KeepFirst:
		return entry, -1, nil

	case precedence.KeepsEvery():
		return "", 0, errors.New("the game joins every copy of an entry of this kind, so a mod adds to it instead of overriding it")

	// the highest priority wins wherever its file loads
	case precedence.Keeps == lang.KeepHighest:
		highest := 0
		for position, site := range sites {
			if priority := int(mods.Occurrence(site).Priority); position == 0 || priority > highest {
				highest = priority
			}
		}

		return buildPriorityCopy(tree, definition, precedence, highest+1), 0, nil
	}

	return entry, 1, nil
}

// listRivals returns the copies an override must load after or before: those
// with an entry mode where the database takes modes, since modes act after
// every plain copy, else all.
func listRivals(mods *index.Index, sites []index.Site, precedence lang.Precedence) []index.Site {
	if precedence.Modes == "" {
		return sites
	}

	rivals := []index.Site{}
	for _, site := range sites {
		if mods.Occurrence(site).Mode != 0 {
			rivals = append(rivals, site)
		}
	}

	return rivals
}

// resolveOverrideFolder returns the folder for a copy of an entry. A copy that
// must load after its rivals goes beside the last, since a folder's files load
// before its subfolders'.
func resolveOverrideFolder(
	mods *index.Index, place workspace.Place, database *lang.Database, rivals []index.Site, side int,
) string {
	if side > 0 && len(rivals) > 0 {
		last := slices.MaxFunc(rivals, func(first, second index.Site) int {
			return mods.ComparePaths(first.File, second.File)
		})

		return path.Dir(mods.File(last).Rel)
	}

	return path.Join(place.Stage, database.Declaration.Folder)
}

const maxPrefix = 8

// findPrefix returns the shortest prefix that loads a mod's file before every
// rival if the side is negative, or after every one if positive. It returns ""
// if none is needed, and false if no prefix up to maxPrefix characters does.
func findPrefix(
	row *game.Game, mods *index.Index, mod workspace.Mod, folder string, side int, rivals []index.Site,
) (string, bool) {
	if side == 0 {
		return "", true
	}

	// the game orders files by their paths from their stages, with case
	others := make([]string, len(rivals))
	for position, site := range rivals {
		others[position] = mods.File(site).StageRel()
	}

	fill := "z"
	if side < 0 {
		fill = "0"
	}

	for count := range maxPrefix + 1 {
		prefix := ""
		if count > 0 {
			prefix = strings.Repeat(fill, count) + "_"
		}

		_, candidate := files.SplitStage(resolveEntryRel(mod, folder, prefix), row.Stages)
		if !slices.ContainsFunc(others, func(other string) bool { return files.CompareLoadOrder(candidate, other)*side <= 0 }) {
			return prefix, true
		}
	}

	return "", false
}

// buildEntryFix returns the fix that adds an entry to the end of a mod's file.
func buildEntryFix(ws *workspace.Session, mods *index.Index, target, source, entry string) Fix {
	gap := ""
	if strings.TrimSpace(source) != "" {
		trailing := source[len(strings.TrimRight(source, "\r\n")):]
		gap = strings.Repeat("\n", max(2-strings.Count(trailing, "\n"), 0))
	}

	fix := buildAppendFix(ws, mods, target, source, gap+entry, len(gap))
	if !files.IsFile(target) {
		fix.Creates = target
	}

	return fix
}

// buildModeCopy returns an entry written with the REPLACE mode, wrapped in its
// holder where the game writes the mode in a block.
func buildModeCopy(tree *jomini.Tree, definition *lang.Copy, precedence lang.Precedence) string {
	statement := tree.Statements[definition.Statement]
	entry := "REPLACE:" + tree.Src[definition.Span.Start:statement.Span.End]
	if precedence.Modes != lang.ModePlaceWrapped || statement.Parent == 0 {
		return entry
	}

	holder := tree.Statements[statement.Parent]

	return tree.Text(holder.Key) + " = {\n" + cmp.Or(tree.Indentation(), "\t") + entry + "\n}"
}

// buildPriorityCopy returns a copy of an entry with its priority key set to
// the given number. Where a higher priority replaces only some keys, the copy
// holds those alone.
func buildPriorityCopy(tree *jomini.Tree, definition *lang.Copy, precedence lang.Precedence, priority int) string {
	statement := tree.Statements[definition.Statement]
	if len(precedence.Overrides) > 0 {
		unit := cmp.Or(tree.Indentation(), "\t")
		lines := []string{tree.Text(statement.Key) + " = {", unit + precedence.PriorityKey + " = " + strconv.Itoa(priority)}
		for child := range tree.Children(definition.Statement) {
			if slices.Contains(precedence.Overrides, tree.Unquoted(tree.Statements[child].Key)) {
				lines = append(lines, unit+tree.Text(tree.Statements[child].Span))
			}
		}

		return strings.Join(append(lines, "}"), "\n")
	}

	edit := tree.BuildInsertion(definition.Statement, []string{precedence.PriorityKey + " = " + strconv.Itoa(priority)})
	if child, ok := tree.FindChild(definition.Statement, precedence.PriorityKey); ok {
		edit = jomini.Edit{Span: tree.Statements[child].Value, Text: strconv.Itoa(priority)}
	}

	text := jomini.ApplyEdits(tree.Src, []jomini.Edit{edit})

	return text[statement.Span.Start : statement.Span.End+len(edit.Text)-(edit.Span.End-edit.Span.Start)]
}
