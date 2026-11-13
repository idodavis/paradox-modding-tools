// session.go opens and serves the open workspace's session.

package workspace

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// Session is the open workspace as every service reads it. A reader loads the
// mods' index once and keeps that copy, since a rescan swaps in a new one.
type Session struct {
	ID   string
	Game *game.Game

	// InstallPath is the install's folder, which holds the game's folders.
	InstallPath string
	Schema      *lang.Schema
	Install     *index.Index

	// Mods is the index of the mods on top of Install. A rescan replaces it
	// whole, and a descriptor read again replaces Order whole.
	Mods  atomic.Pointer[index.Index]
	Order atomic.Pointer[[]Mod]
	Edits atomic.Int64

	// Language is shown beside the base language.
	Language string

	// updatedFrom is the install's recorded version, when the open found
	// another.
	updatedFrom string

	// mu guards the buffers, the documents and the load order.
	mu sync.RWMutex

	// buffers holds the editor's open files, by the path's key.
	buffers   map[string]lang.Document
	documents *lru.Cache[string, lang.Document]
	watcher   *Watcher
	caches    string
}

const (
	schemaFile = "schema.gob"
	indexFile  = "index.gob"

	// translationFile is the cache of one language beside the base one.
	translationFile = "index-%s.gob"
)

// openSession opens a workspace and starts its watcher.
func (s *Service) openSession(workspaceID string, progress func(stage string, percent int), change func(changes []FileChange)) (*Session, error) {
	record, install, row, err := findRecords(s.Config.Config(), workspaceID)
	if err != nil {
		return nil, err
	}

	version := resolveVersion(row, install)
	folder, err := resolveCaches(s.Config.Dirs.Cache, row, install.Path, version, s.Build)
	if err != nil {
		return nil, err
	}

	documents, err := lru.New[string, lang.Document](256)
	if err != nil {
		return nil, err
	}

	ws := &Session{
		ID: workspaceID, Game: row, InstallPath: install.Path, Language: record.Language,
		buffers: map[string]lang.Document{}, documents: documents, caches: folder,
	}
	if err := ws.loadInstall(install.Path, version, progress); err != nil {
		return nil, err
	}

	// Older caches go. One that cannot be deleted now goes at a later open.
	_ = removeEntries(filepath.Dir(folder), filepath.Base(folder))

	order := make([]Mod, len(record.Mods))
	modIDs := make([]string, len(record.Mods))
	for position, mod := range record.Mods {
		order[position] = readMod(row, mod)
		modIDs[position] = mod.ID
	}
	ws.Order.Store(&order)

	const stage = "Reading your mods"
	progress(stage, 0)

	mods, err := ws.ScanOrder(modIDs, func(percent int) { progress(stage, percent) })
	if err != nil {
		return nil, err
	}

	ws.Mods.Store(mods)

	roots := mods.Roots[len(ws.Install.Roots):]
	dirs := make([]string, len(roots))
	for position, root := range roots {
		dirs[position] = root.Dir
	}

	ws.watcher, err = startWatcher(dirs, func(changes []FileChange) {
		// A file that cannot be scanned keeps its old entries.
		_ = ws.Rescan(changes)
		change(changes)
	})
	if err != nil {
		return nil, err
	}

	return ws, nil
}

// findRecords returns the record of a workspace, its install and the install's
// game. It fails if any is missing.
func findRecords(records app.Config, workspaceID string) (app.Workspace, app.Install, *game.Game, error) {
	position := slices.IndexFunc(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
	if position < 0 {
		return app.Workspace{}, app.Install{}, nil, fmt.Errorf("the workspace %s is not recorded", workspaceID)
	}

	record := records.Workspaces[position]
	install, row, err := findInstall(records, record.Install)

	return record, install, row, err
}

// findInstall returns the record of an install and its game. It fails if the
// install is not recorded or its game is not supported.
func findInstall(records app.Config, installID string) (app.Install, *game.Game, error) {
	position := slices.IndexFunc(records.Installs, func(install app.Install) bool { return install.ID == installID })
	if position < 0 {
		return app.Install{}, nil, fmt.Errorf("the install %s is not recorded", installID)
	}

	install := records.Installs[position]
	row := game.LookupGame(install.Game)
	if row == nil {
		return app.Install{}, nil, fmt.Errorf("the game %s is not supported", install.Game)
	}

	return install, row, nil
}

// resolveCaches returns the folder of an install's caches, new for each
// install version, dumps and app build.
func resolveCaches(cacheDir string, row *game.Game, installPath, version, build string) (string, error) {
	dumpsAt, err := schema.FindDumpsAt(row.FindDumpDirs(installPath))
	if err != nil {
		return "", err
	}

	folder := fmt.Sprintf("%s-%s-%d-%s", version, game.LoadChecksum(installPath), dumpsAt.Unix(), build)

	return filepath.Join(cacheDir, row.ID, folder), nil
}

// resolveVersion returns the version the install's files say, else the
// record's.
func resolveVersion(row *game.Game, install app.Install) string {
	return cmp.Or(row.LoadVersion(install.Path), install.Version)
}

// loadInstall sets the schema and install index from the caches, building and
// saving what a cache lacks.
func (s *Session) loadInstall(installPath, version string, progress func(stage string, percent int)) error {
	schemaCache, indexCache := filepath.Join(s.caches, schemaFile), filepath.Join(s.caches, indexFile)
	language, hasSchema := files.LoadGob[*lang.Schema](schemaCache)
	install, hasInstall := index.LoadIndex(indexCache)
	if hasSchema && hasInstall {
		s.Schema, s.Install = language, install

		return s.loadTranslation(progress)
	}

	progress("Listing the game's files", -1)

	roots, err := s.Game.ListInstallRoots(installPath)
	if err != nil {
		return err
	}

	mounts, err := files.Mount(roots)
	if err != nil {
		return err
	}

	if !hasSchema {
		progress("Reading the game's documentation", -1)

		dialect, dumpDirs := lang.LookupDialect(s.Game.ID), s.Game.FindDumpDirs(installPath)
		if language, err = schema.Build(dialect, dumpDirs, installPath, mounts.Files, index.ParseFile); err != nil {
			return err
		}

		language.Version = version
		if err = files.SaveGob(schemaCache, language); err != nil {
			return err
		}

		// The built schema's names slice the game files' text, which would stay in
		// memory. The cache's copy does not.
		if language, hasSchema = files.LoadGob[*lang.Schema](schemaCache); !hasSchema {
			return fmt.Errorf("cannot read back the schema cache %s", schemaCache)
		}
	}

	if !hasInstall {
		install = index.Scan(language, mounts, progress)
		if err = files.SaveGob(indexCache, install); err != nil {
			return err
		}
	}

	s.Schema, s.Install = language, install

	return s.loadTranslation(progress)
}

// loadTranslation adds the workspace's language to the install's index, unless
// it is the base language. It scans it if its cache is missing or stale.
func (s *Session) loadTranslation(progress func(stage string, percent int)) error {
	if len(s.Languages()) < 2 {
		return nil
	}

	cache := filepath.Join(s.caches, fmt.Sprintf(translationFile, s.Language))
	if translation, ok := files.LoadGob[index.Translation](cache); ok && s.Install.Apply(translation) {
		return nil
	}

	progress("Reading the game's "+loc.DescribeLanguage(s.Language)+" texts", -1)

	return files.SaveGob(cache, s.Install.ScanTranslation(s.Schema, s.Language))
}

// removeEntries deletes every entry of a folder but the one with the given
// name, or every entry for "". It goes on past a failure and returns them all.
func removeEntries(dir, base string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var failure error
	for _, entry := range entries {
		if entry.Name() != base {
			failure = errors.Join(failure, os.RemoveAll(filepath.Join(dir, entry.Name())))
		}
	}

	return failure
}

// readMod returns a recorded mod with its descriptor and thumbnail. A missing
// folder makes it broken, and an unreadable descriptor leaves it empty.
func readMod(row *game.Game, record app.Mod) Mod {
	mod := Mod{Workshop: row.IsWorkshopFolder(record.Dir)}.withRecord(record)
	if files.IsFolder(record.Dir) {
		mod.Descriptor, _ = ReadDescriptor(row, record.Dir)
		mod.Thumbnail = findThumbnail(row, record.Dir, mod.Descriptor.Thumbnail)
	} else {
		mod.Broken = true
	}

	return mod
}

// refreshMod reads a mod's descriptor and thumbnail again.
func (s *Session) refreshMod(modID string) {
	current := *s.Order.Load()
	order := make([]Mod, len(current))
	for position, mod := range current {
		if mod.ID == modID {
			mod = readMod(s.Game, mod.Mod)
		}

		order[position] = mod
	}

	s.Order.Store(&order)
}

// ScanOrder returns an index of the mods scanned in another load order. It
// fails unless the ids name each mod once.
func (s *Session) ScanOrder(modIDs []string, progress func(percent int)) (*index.Index, error) {
	ordered, err := reorderMods(*s.Order.Load(), modIDs, func(mod Mod) string { return mod.ID })
	if err != nil {
		return nil, err
	}

	var roots []files.Root
	for _, mod := range ordered {
		if !mod.Broken {
			roots = append(roots, files.Root{
				Dir: mod.Dir, Origin: mod.ID, Stages: s.Game.Stages, ReplacePaths: mod.Descriptor.ReplacePaths,
			})
		}
	}

	return index.ScanMods(s.Schema, s.Install, roots, progress)
}

// Close stops the watcher and drops the open files' buffers and documents.
func (s *Session) Close() {
	// The watcher may be inside a rescan that waits for the lock, so it stops
	// first.
	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	clear(s.buffers)
	s.documents.Purge()
}

// WalkText parses a file's text and walks it.
func (s *Session) WalkText(place Place, text string) lang.Document {
	return s.walk(parseDocument(place, text))
}

// walk gives a script or localization document its contexts, and a script
// document the problems its walk found.
func (s *Session) walk(document lang.Document) lang.Document {
	switch {
	case document.Messages != nil:
		document.MessageContexts = s.Schema.WalkMessages(document.Messages)
	case document.File.Format.IsScript():
		document.Contexts, document.Problems = s.Schema.Walk(document.Tree, document.File, nil)
	}

	return document
}

func parseDocument(place Place, text string) lang.Document {
	if place.Format == files.FormatLocalization {
		return lang.Document{File: place.File, Tree: jomini.NewTree(text), Messages: loc.Parse(text, place.Path)}
	}

	return lang.Document{File: place.File, Tree: jomini.Parse(text)}
}

// WalkDocument returns a file's document as LoadDocument finds it, walked. A
// cached one is walked only here, since most readers want just the tree.
func (s *Session) WalkDocument(path string) (lang.Document, error) {
	// The lock stays through the walk, so no rescan runs before it is cached.
	s.mu.RLock()
	defer s.mu.RUnlock()

	document, err := s.findDocument(path)
	if err != nil || document.Contexts != nil || document.MessageContexts != nil {
		return document, err
	}

	document = s.walk(document)
	s.documents.Add(files.ToKey(path), document)

	return document, nil
}

// LoadDocument returns a file's document: the editor's while the editor has the
// file open, else the cached one, else the file read and parsed.
func (s *Session) LoadDocument(path string) (lang.Document, error) {
	// The lock stays through the parse, so no rescan runs before it is cached.
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.findDocument(path)
}

func (s *Session) findDocument(path string) (lang.Document, error) {
	key := files.ToKey(path)
	if document, open := s.buffers[key]; open {
		return document, nil
	}
	if document, ok := s.documents.Get(key); ok {
		return document, nil
	}

	text, err := files.ReadText(path)
	if err != nil {
		return lang.Document{}, err
	}

	document := parseDocument(s.Locate(s.Mods.Load(), path), text.Text)
	s.documents.Add(key, document)

	return document, nil
}

// LoadTree returns a file's tree, as LoadDocument finds it.
func (s *Session) LoadTree(path string) (*jomini.Tree, error) {
	document, err := s.LoadDocument(path)

	return document.Tree, err
}

// SetText keeps the editor's text of an open file as the file's buffer.
func (s *Session) SetText(path, text string) {
	document := s.WalkText(s.Locate(s.Mods.Load(), path), text)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buffers[files.ToKey(path)] = document
	s.Edits.Add(1)
}

// CloseBuffer drops an open file's buffer.
func (s *Session) CloseBuffer(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.buffers, files.ToKey(path))
	s.Edits.Add(1)
}

// LookupBuffer returns the editor's text of a file. It returns false if the
// editor does not have the file open.
func (s *Session) LookupBuffer(path string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	document, open := s.buffers[files.ToKey(path)]
	if !open {
		return "", false
	}

	return document.Tree.Src, true
}

// LoadSource returns a file's text as LoadDocument finds it, without parsing
// one. A missing file reads as empty.
func (s *Session) LoadSource(path string) (string, error) {
	if source, open := s.LookupBuffer(path); open {
		return source, nil
	}
	if document, cached := s.documents.Get(files.ToKey(path)); cached {
		return document.Tree.Src, nil
	}

	text, err := files.LoadText(path)

	return text.Text, err
}

// Languages returns the languages a text is looked up in: the workspace's own,
// then the base language.
func (s *Session) Languages() []string {
	if s.Language == "" || s.Language == loc.BaseLanguage {
		return []string{loc.BaseLanguage}
	}

	return []string{s.Language, loc.BaseLanguage}
}

// LookupText returns a localization key's text in the first language that has
// it, with the message the game shows.
func (s *Session) LookupText(mods *index.Index, key string) (text string, winner index.Winner, ok bool) {
	if winner, ok = mods.FindText(key, s.Languages()); !ok {
		return "", index.Winner{}, false
	}

	return mods.Text(winner.Site), winner, true
}

// LookupDefinitionText returns a definition's title or description, as key
// says: lang.TitleKey or lang.DescriptionKey. It is the text of the key the
// definition writes, else of the key the game builds from its name.
func (s *Session) LookupDefinitionText(mods *index.Index, site index.Site, key string) string {
	textKey, _ := mods.FindTextKey(site, key)

	return s.resolveText(mods, textKey, mods.ToReference(site), key)
}

// LookupStatementText does what LookupDefinitionText does for a statement of a
// tree the index has not scanned. The reference names the definition.
func (s *Session) LookupStatementText(
	mods *index.Index, tree *jomini.Tree, statement int32, reference lang.Reference, key string,
) string {
	textKey := ""
	if position, ok := lang.FindTextKey(tree, statement, key); ok {
		textKey = tree.Unquoted(tree.Statements[position].Value)
	}

	return s.resolveText(mods, textKey, reference, key)
}

func (s *Session) resolveText(mods *index.Index, textKey string, reference lang.Reference, key string) string {
	if textKey != "" {
		if text, _, ok := s.LookupText(mods, textKey); ok {
			return text
		}
	}

	pattern, ok := lang.LookupDialect(s.Game.ID).LookupTextKey(reference.Database, key)
	if !ok {
		return ""
	}

	text, _, _ := s.LookupText(mods, fmt.Sprintf(pattern, reference.Name))

	return text
}

// FindStatement returns the tree of a site's file and the statement that writes
// it.
func (s *Session) FindStatement(mods *index.Index, site index.Site) (*jomini.Tree, int32, error) {
	path := mods.File(site).Path
	tree, err := s.LoadTree(path)
	if err != nil {
		return nil, 0, err
	}

	occurrence := mods.Occurrence(site)
	statement := occurrence.Statement
	if int(statement) < len(tree.Statements) && tree.Statements[statement].Span.Contains(occurrence.Span.Start) {
		return tree, statement, nil
	}

	return nil, 0, fmt.Errorf("%s has no statement where the index read one", path)
}

// Rescan brings the mods' index up to date with a batch of file changes, and
// marks the changes to descriptors in it. It scans past a change it cannot read
// and returns its error.
func (s *Session) Rescan(changes []FileChange) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mods := s.Mods.Load()
	var changed []files.File
	var failure error
	for position, change := range changes {
		place := s.Locate(mods, change.Path)
		if descriptor, _ := resolveDescriptor(s.Game, place.Root); place.Root != "" && files.ToKey(descriptor) == place.Key {
			s.refreshMod(place.Origin)
			changes[position].Descriptor = true
		}

		// The index skips dot folders, the watched ones too.
		isWatched := slices.ContainsFunc(strings.Split(place.Rel, "/"), func(folder string) bool {
			return slices.Contains(watchedFolders, folder)
		})
		if place.Root == "" || place.Origin == files.OriginVanilla || isWatched {
			continue
		}

		listed, err := s.listChanged(mods, place, change.Deleted)
		changed, failure = append(changed, listed...), errors.Join(failure, err)
	}

	seen := map[string]bool{}
	var unique []files.File
	for _, file := range changed {
		if !seen[file.Key] {
			seen[file.Key] = true
			unique = append(unique, file)
		}
	}
	if len(unique) == 0 {
		return failure
	}

	next := mods.Clone()
	if err := next.ScanFiles(s.Schema, unique); err != nil {
		return errors.Join(failure, err)
	}

	s.Mods.Store(next)

	// What the mods declare may have changed, so the walks are made again: a
	// cached file's when it is next loaded, an open file's now.
	s.documents.Purge()
	for key, document := range s.buffers {
		if document.Contexts != nil {
			s.buffers[key] = s.walk(document)
		}
	}

	return failure
}

// listChanged returns the files a changed path stands for: the file, or every
// file under a folder. For a deleted path, they are the files the index had
// there.
func (s *Session) listChanged(mods *index.Index, place Place, deleted bool) ([]files.File, error) {
	switch {
	case deleted:
		var listed []files.File
		for _, position := range mods.ListOwnFiles() {
			file := mods.Files[position]
			if file.Key == place.Key || files.IsUnder(place.Path, file.Path) {
				listed = append(listed, file)
			}
		}

		return listed, nil
	case files.IsFolder(place.Path):
		listing, err := files.Walk(files.Root{Dir: place.Path})
		listed := make([]files.File, len(listing))
		for position, file := range listing {
			listed[position] = s.Locate(mods, file.Path).File
		}

		return listed, err
	}

	return []files.File{place.File}, nil
}
