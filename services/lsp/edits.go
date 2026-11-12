// edits.go formats a script file and offers quick fixes for its problems. It also builds the copy a mod makes to
// override an entry. Each is returned as the edits that apply it, in Monaco's ranges.

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

	"github.com/samber/lo"
	"github.com/sergi/go-diff/diffmatchpatch"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Fix is one quick fix for a problem. It carries the edits that apply it, grouped by file.
type Fix struct {
	Title   string            `json:"title"`
	Code    string            `json:"code"` // the diagnostic it fixes
	Edits   map[string][]Edit `json:"edits"`
	Creates string            `json:"creates,omitempty"` // a file written first: a localization file has only its header
	Reveal  *Site             `json:"reveal,omitempty"`  // where the caret goes once the fix is applied

	// Note says in plain words what the fix leaves the game doing, as an
	// override says which copy the game keeps. It is "" for none.
	Note string `json:"note,omitempty"`
}

// A Draft is the file New file makes in a folder of a mod: the name it suggests and the text the file starts with.
type Draft struct {
	Name string     `json:"name"`
	Text files.Text `json:"text"` // the header of a localization file, the namespace line in an events folder, else empty
}

// fixDistance is how many edits away a name may be and still be offered as the fix for an unresolved reference.
const fixDistance = 3

// maxPrefix is how many characters of z or 0 a file's name may start with to load after or before the others.
const maxPrefix = 8

// Format returns the edits that format a file's text, keeping the file's own
// indentation, else a tab or as many spaces as the given tab size. It returns
// no edits for a read-only file and for one that is not script or does not
// parse.
func (lsp *Service) Format(path string, tabSize int, insertSpaces bool) ([]Edit, error) {
	ws, tree, err := lsp.loadTree(path)
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

// DescribeIndentation returns what a file indents one level with, in words: "Tabs" or "4 spaces". It returns "" if the
// file indents nothing.
func (lsp *Service) DescribeIndentation(path string) (string, error) {
	_, tree, err := lsp.loadTree(path)
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

// ListFixes returns the quick fixes for the given problems of a file, one for each problem that has a fix.
func (lsp *Service) ListFixes(path string, diagnostics []Diagnostic) ([]Fix, error) {
	ws, document, err := lsp.loadDocument(path)
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
		case index.ProblemMode:
			title, edits = "Remove mode", []Edit{{Range: diagnostic.Range}}
		}
		if len(edits) > 0 {
			fixes = append(fixes, Fix{Title: title, Code: diagnostic.Code, Edits: map[string][]Edit{path: edits}})
		}
	}
	if problem, ok := lo.Find(diagnostics, func(diagnostic Diagnostic) bool { return keyFacts[diagnostic.Code] != "" }); ok {
		if fix, ok := lsp.buildKeysFix(ws, mods, place, problem.Code); ok {
			fixes = append(fixes, fix)
		}
	}
	return fixes, nil
}

// buildKeysFix returns the fix that adds an empty message for each localization key the file uses but has no
// base-language text for, in the localization file PMT makes for the file's mod. It returns false for a read-only
// file, a mod with no name, a game with no localization folder, or a file that lacks no text.
func (lsp *Service) buildKeysFix(
	ws *workspace.Session, mods *index.Index, place workspace.Place, code string,
) (Fix, bool) {
	mod, ok := lsp.Workspace.LookupMod(place.Origin)
	if !ok || place.ReadOnly || mod.Descriptor.Name == "" {
		return Fix{}, false
	}
	target, ok := workspace.FindLocalizationFile(ws.Game, mod.Dir, mod.Descriptor.Name, game.BaseLanguage)
	if !ok {
		return Fix{}, false
	}
	diagnostics, _ := DiagnoseFile(ws, mods, place.Path)
	keys := lo.Uniq(lo.FilterMap(lsp.listEnabled(ws.ID, diagnostics), func(diagnostic Diagnostic, _ int) (string, bool) {
		fact, ok := keyFacts[diagnostic.Code]
		return diagnostic.Facts[fact], ok
	}))
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
		creates, title, text = target, title+", a new file", loc.Encode(game.BaseLanguage, nil)
	}
	messages := lo.Map(keys, func(key string, _ int) loc.Message { return loc.Message{Key: key} })
	addition := loc.EncodeAddition(text, messages)
	fix := buildAppendFix(ws, mods, target, text, addition, strings.Index(addition, `""`)+1)
	fix.Code, fix.Title, fix.Creates = code, title, creates
	return fix, true
}

// buildAppendFix returns the fix that adds text to the end of a file's text, with the caret the given number of
// characters into the added text.
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

// buildReferenceFix returns the fix for a reference nothing defines: a change to the nearest name its database defines.
// A reference to a callable database also draws on the commands the block it is written in takes. The edits are empty
// if no name is near enough.
func buildReferenceFix(
	language *schema.Schema, mods *index.Index, cursor cursor, diagnostic Diagnostic,
) (string, []Edit) {
	diff := diffmatchpatch.New()
	best, nearest := fixDistance, ""
	for _, reference := range cursor.context.References {
		if reference.Span.Start != cursor.offset {
			continue
		}
		name := mods.ToCaseless(reference.Database, reference.Name)
		length := utf8.RuneCountInString(name)
		keys := lo.Map(mods.ListDefinitions(reference.Database), func(site index.Site, _ int) string {
			return mods.ToReference(site).Name
		})
		if database := language.Databases[reference.Database]; database != nil && database.Declaration.Command() != "" {
			for _, dump := range commandDatabases {
				if cursor.context.Parent.Takes(dump) {
					keys = slices.Concat(keys, language.ListKeys(dump))
				}
			}
		}
		for _, key := range lo.Uniq(keys) {
			// names that differ in length by the best distance so far or more are that far apart
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

// buildMissingFix returns the fix for a definition that lacks a key it must write: a line that adds the key with its
// default. The edits are empty if the key has no default.
func buildMissingFix(schema *schema.Schema, cursor cursor, diagnostic Diagnostic) (string, []Edit) {
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

// hasFolder reports whether a database has a folder of its own, one that holds many files, not a single file.
func hasFolder(database *schema.Database) bool {
	return database != nil && database.Declaration.Folder != "" && path.Ext(database.Declaration.Folder) == ""
}

// DraftFile returns the file New file makes in a folder of a mod: the name it suggests and the text it starts with. It
// returns an error if no workspace is open or the folder is not in a mod that can be written to.
func (lsp *Service) DraftFile(folder string) (Draft, error) {
	ws, err := lsp.Workspace.FindWritable(folder)
	if err != nil {
		return Draft{}, err
	}
	place := ws.Locate(ws.Mods.Load(), folder)
	mod, ok := lsp.Workspace.LookupMod(place.Origin)
	if !ok {
		return Draft{}, fmt.Errorf("the mod %s is not in the workspace", place.Origin)
	}
	if database := ws.Schema.Databases[place.Database]; hasFolder(database) {
		// the mod's own name for its files is its namespace, as the part before an event's dot is
		namespace := buildNamespace(database, resolveModStem(mod), "")
		base := path.Base(resolveEntryRel(mod, place.Rel, ""))
		return makeDraft(place.Rel, findFreeName(folder, base, path.Ext(base)), namespace), nil
	}
	if language := lsp.findLanguage(place.Rel); language != "" {
		ending := "_" + loc.ToLanguageKey(language) + ".yml"
		return makeDraft(place.Rel, findFreeName(folder, resolveModStem(mod)+ending, ending), loc.Encode(language, nil)), nil
	}
	extension := findScriptExtension(ws, place.Rel)
	return makeDraft(place.Rel, findFreeName(folder, "new_file"+extension, extension), ""), nil
}

// makeDraft returns the draft of a new file of the given name in a mod's folder, the folder given from the mod's root,
// with the given text in the encoding its format is written in.
func makeDraft(folderRel, name, text string) Draft {
	encoding := files.Classify(path.Join(folderRel, name)).DefaultEncoding()
	return Draft{Name: name, Text: files.Text{Text: text, Encoding: encoding}}
}

// findFreeName returns a name for a new file in a folder that nothing in the folder has: the given name, else numbered
// from 2 before the given ending, the part the game reads at the end of a name, such as _l_english.yml.
func findFreeName(folder, base, ending string) string {
	stem := strings.TrimSuffix(base, ending)
	free := base
	for count := 2; files.IsFile(files.Join(folder, free)) || files.IsFolder(files.Join(folder, free)); count++ {
		free = stem + "_" + strconv.Itoa(count) + ending
	}
	return free
}

// findLanguage returns the localization language of a folder, given by its path from its root. It returns "" for a
// folder in no language folder or in a language the install lacks.
func (lsp *Service) findLanguage(folder string) string {
	language := loc.FindFolderLanguage(folder + "/")
	languages, _ := lsp.Workspace.ListLanguages(lsp.Workspace.State().Install.ID)
	if lo.ContainsBy(languages, func(candidate loc.Language) bool { return candidate.Folder == language }) {
		return language
	}
	return ""
}

// findScriptExtension returns the script extension the game's files in a folder use, the folder given by its path from
// its root. It returns .txt if they use none or more than one.
func findScriptExtension(ws *workspace.Session, folder string) string {
	extensions := lo.Uniq(lo.FilterMap(ws.Install.Files, func(file files.File, _ int) (string, bool) {
		extension := strings.ToLower(path.Ext(file.Rel))
		_, isScript := game.FileTypes[extension]
		return extension, isScript && strings.EqualFold(path.Dir(file.Rel), cmp.Or(folder, "."))
	}))
	if len(extensions) == 1 {
		return extensions[0]
	}
	return ".txt"
}

// resolveEntryRel returns the path, from a mod's folder, of the file the mod keeps its entries of a folder in: the
// prefix, the mod's own name for its files, an underscore and the folder's last part.
func resolveEntryRel(mod workspace.Mod, folder, prefix string) string {
	return path.Join(folder, prefix+resolveModStem(mod)+"_"+path.Base(folder)+".txt")
}

// resolveModStem returns a mod's own name for its files: its folder's name made a file name, in lower case with its
// words joined by underscores.
func resolveModStem(mod workspace.Mod) string { return files.ToSlug(filepath.Base(mod.Dir)) }

// buildNamespace returns the line that declares the namespace of an event with the given key, and a blank line after
// it. It returns "" if the given text of the file already declares it, and for a database whose definitions are not
// events.
func buildNamespace(database *schema.Database, key, source string) string {
	if database == nil || database.Declaration.Contents != game.ContentsEvents {
		return ""
	}
	namespace, _, _ := strings.Cut(key, ".")
	tree := jomini.Parse(source)
	for statement := range tree.Children(0) {
		current := tree.Statements[statement]
		if tree.Unquoted(current.Key) == game.EventNamespaceKey && tree.Unquoted(current.Value) == namespace {
			return ""
		}
	}
	return game.EventNamespaceKey + " = " + namespace + "\n\n"
}

// buildEntryFix returns the fix that adds an entry to the end of a mod's script file, with the caret at the start of
// the new text. It creates the file if it does not exist.
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

// BuildOverride returns the fix that overrides the entry at a position of a
// file. The fix copies the entry into a mod, written so that the game keeps
// the copy. The mod is the one with the given id, or the first other mod
// that can be written to if the id is "". It returns nil if the position is
// in no entry.
//
// It returns an error if the mod defines the entry already, the game joins
// every copy of that kind of entry, or no file name makes the copy win. With
// no mod id, those cases return nil and no error.
func (lsp *Service) BuildOverride(path string, line, column int, modID string) (fix *Fix, err error) {
	defer func() {
		if modID == "" {
			err = nil
		}
	}()

	ws, document, err := lsp.loadDocument(path)
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
	mod, err := lsp.findOverrideMod(place, modID)
	if err != nil {
		return nil, err
	}
	sites := mods.Resolve(schema.Reference{Database: definition.Database, Name: definition.Name})
	if lo.SomeBy(sites, func(site index.Site) bool { return mods.File(site).Origin == mod.ID }) {
		return nil, fmt.Errorf("%s defines the %s %s already", ws.DescribeMod(mod.ID),
			game.DescribeDefinition(definition.Database, false), definition.Name)
	}
	entry, side, err := writeOverride(tree, mods, definition, database.Precedence, sites)
	if err != nil {
		return nil, err
	}
	rivals := listRivals(mods, sites, database.Precedence)
	folder := resolveOverrideFolder(mods, place, database, rivals, side)
	prefix, ok := findPrefix(ws.Game, mods, mod, folder, side, rivals)
	if !ok {
		return nil, fmt.Errorf("no file name in %s makes the copy load %s the other copies", folder,
			lo.Ternary(side < 0, "before", "after"))
	}
	target := files.Join(mod.Dir, resolveEntryRel(mod, folder, prefix))
	source, _ := ws.LoadSource(target)
	override := buildEntryFix(ws, mods, target, source, buildNamespace(database, definition.Name, source)+entry)
	override.Title = "Override " + definition.Name + " in " + ws.DescribeMod(mod.ID)
	override.Note = database.Precedence.Describe()
	return &override, nil
}

// writeOverride returns the text of a copy that overrides a definition, and
// where the copy's file must load: 1 for after the other copies, -1 for
// before them, 0 for anywhere. The sites are the other copies. It returns an
// error if the game joins every copy.
func writeOverride(
	tree *jomini.Tree, mods *index.Index, definition *schema.Copy, precedence game.Precedence, sites []index.Site,
) (string, int, error) {
	entry := tree.Text(tree.Statements[definition.Statement].Span)
	switch {
	// an entry mode replaces the copies loaded before it
	case precedence.Modes != "":
		return buildModeCopy(tree, definition, precedence), 1, nil

	case precedence.Keeps == game.KeepFirst:
		return entry, -1, nil

	case precedence.KeepsEvery():
		return "", 0, errors.New("the game joins every copy of an entry of this kind, so a mod adds to it instead of overriding it")

	// the highest priority wins wherever its file loads
	case precedence.Keeps == game.KeepHighest:
		highest := lo.Max(lo.Map(sites, func(site index.Site, _ int) int { return int(mods.Occurrence(site).Priority) }))

		return buildPriorityCopy(tree, definition, precedence, highest+1), 0, nil
	}

	return entry, 1, nil
}

// findEntry returns the definition at an offset of a document that a mod can
// override: the innermost one that holds the offset, if it is written in a
// folder of its database. A name script saves on the way, such as a scope,
// is no entry. It returns nil if the offset is in none.
func findEntry(ws *workspace.Session, document schema.Document, offset int) *schema.Copy {
	context := findCursor(document, offset).context
	for context != nil && (!context.Defines || context.Category != schema.CategoryDefinition) {
		context = context.Parent
	}

	if context == nil || context.Definition.Statement == 0 || !hasFolder(ws.Schema.Databases[context.Definition.Database]) {
		return nil
	}

	return context.Definition
}

// listRivals returns the copies of a name that an override has to load after or before: the ones written with an
// entry mode where the database takes modes, since the modes act after every plain copy, and else all of them.
func listRivals(mods *index.Index, sites []index.Site, precedence game.Precedence) []index.Site {
	if precedence.Modes == "" {
		return sites
	}
	return lo.Filter(sites, func(site index.Site, _ int) bool { return mods.Occurrence(site).Mode != 0 })
}

// resolveOverrideFolder returns the folder, from a mod's folder, that a copy of an entry goes in. A copy that must load
// after the given rivals goes beside the one that loads last, since a folder's files load before its subfolders'. Any
// other goes in the entry's database folder, under the entry's stage folder.
func resolveOverrideFolder(
	mods *index.Index, place workspace.Place, database *schema.Database, rivals []index.Site, side int,
) string {
	if side > 0 && len(rivals) > 0 {
		last := slices.MaxFunc(rivals, func(first, second index.Site) int {
			return mods.ComparePaths(first.File, second.File)
		})
		return path.Dir(mods.File(last).Rel)
	}
	return path.Join(place.Stage, database.Declaration.Folder)
}

// findOverrideMod returns the mod that holds the copy an override makes: the one with the given id, or else the first
// that can be written to and is not the file's own. It returns an error if there is none.
func (lsp *Service) findOverrideMod(place workspace.Place, modID string) (workspace.Mod, error) {
	mods := lsp.Workspace.ListMods()
	if modID == "" {
		mod, ok := lo.Find(mods, func(mod workspace.Mod) bool {
			return !mod.Broken && !mod.Workshop && mod.ID != place.Origin
		})
		if !ok {
			return workspace.Mod{}, errors.New("the workspace has no other mod that can hold the copy")
		}
		return mod, nil
	}
	mod, ok := lo.Find(mods, func(mod workspace.Mod) bool { return mod.ID == modID })
	if !ok || mod.Broken {
		return workspace.Mod{}, fmt.Errorf("the workspace has no mod %s with a folder", modID)
	}
	_, err := lsp.Workspace.FindWritable(mod.Dir)
	return mod, err
}

// findPrefix returns the shortest prefix for a mod's file in a folder that makes the game load it before every one of
// the given rivals if the side is negative, or after every one if it is positive. A file that already loads so, or a
// side of zero, gets "". It returns false if no prefix of up to maxPrefix characters does.
func findPrefix(
	row *game.Game, mods *index.Index, mod workspace.Mod, folder string, side int, rivals []index.Site,
) (string, bool) {
	if side == 0 {
		return "", true
	}
	// the game orders files by their paths from their stages, with case
	others := lo.Map(rivals, func(site index.Site, _ int) string { return mods.File(site).StageRel() })
	for count := range maxPrefix + 1 {
		prefix := ""
		if count > 0 {
			prefix = strings.Repeat(lo.Ternary(side < 0, "0", "z"), count) + "_"
		}
		_, candidate := files.SplitStage(resolveEntryRel(mod, folder, prefix), row.Stages)
		if lo.EveryBy(others, func(other string) bool { return files.CompareLoadOrder(candidate, other)*side > 0 }) {
			return prefix, true
		}
	}
	return "", false
}

// buildModeCopy returns an entry written with the REPLACE entry mode. Where the game writes the mode inside a block,
// the copy is wrapped in the block that holds the entry.
func buildModeCopy(tree *jomini.Tree, definition *schema.Copy, precedence game.Precedence) string {
	statement := tree.Statements[definition.Statement]
	entry := "REPLACE:" + tree.Src[definition.Span.Start:statement.Span.End]
	if precedence.Modes != game.ModePlaceWrapped || statement.Parent == 0 {
		return entry
	}
	holder := tree.Statements[statement.Parent]
	return tree.Text(holder.Key) + " = {\n" + cmp.Or(tree.Indentation(), "\t") + entry + "\n}"
}

// buildPriorityCopy returns a copy of an entry that sets the key holding its override priority to the given number.
// Where a higher priority replaces only some of the entry's keys, the copy holds those and no others.
func buildPriorityCopy(tree *jomini.Tree, definition *schema.Copy, precedence game.Precedence, priority int) string {
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
	changed := jomini.ApplyEdits(tree.Src, []jomini.Edit{edit})
	return changed[statement.Span.Start : statement.Span.End+len(edit.Text)-(edit.Span.End-edit.Span.Start)]
}
