// Package workspace holds the one open workspace: the install's schema and index, the mods' index on top of it, the
// open files and the watcher. Its service also keeps the records of installs, workspaces and mods.
package workspace

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// A Session is the open workspace as every service reads it. A reader loads the mods' index once and keeps that copy,
// because a rescan puts a new copy in place.
type Session struct {
	ID          string
	Game        *game.Game
	InstallPath string // the install's folder, which holds the game's folders
	Schema      *schema.Schema
	Install     *index.Index
	Mods        atomic.Pointer[index.Index] // on top of Install; a rescan replaces it whole
	Order       atomic.Pointer[[]Mod]       // the mods in load order; a descriptor read again puts a new copy in place
	Edits       atomic.Int64                // how many times the editor's buffers have changed
	Language    string                      // the localization language shown beside the game's base language
	updatedFrom string                      // the install's recorded version when the open found another
	mu          sync.RWMutex                // over the buffers, the documents and the load order, one rescan at a time
	buffers     map[string]schema.Document  // the editor's open files, by the path's key
	documents   *lru.Cache[string, schema.Document]
	watcher     *Watcher
	caches      string // the folder the install's schema and index were loaded from or saved in
}

// The names of the two caches in the caches' folder.
const (
	schemaFile = "schema.gob"
	indexFile  = "index.gob"

	// translationFile is the cache of the game's localization in one
	// language beside the base one, with the language at %s.
	translationFile = "index-%s.gob"
)

// openSession opens a workspace and starts its watcher. It returns an error if the workspace or its install is not
// recorded, or a scan fails.
func (wsp *Service) openSession(workspaceID string, progress func(stage string, percent int), change func(changes []FileChange)) (*Session, error) {
	record, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return nil, err
	}
	folder, err := resolveCaches(wsp.Config.Dirs.Cache, row, install, wsp.Build)
	if err != nil {
		return nil, err
	}
	documents, err := lru.New[string, schema.Document](256)
	if err != nil {
		return nil, err
	}
	ws := &Session{
		ID: workspaceID, Game: row, InstallPath: install.Path, Language: record.Language,
		buffers: map[string]schema.Document{}, documents: documents, caches: folder,
	}
	if err := ws.loadInstall(install.Path, resolveVersion(row, install), progress); err != nil {
		return nil, err
	}
	// the caches of the game's other versions go; one that cannot be deleted now goes at a later open
	_ = removeEntries(filepath.Dir(folder), filepath.Base(folder))
	order := lo.Map(record.Mods, func(mod app.Mod, _ int) Mod { return readMod(row, mod) })
	ws.Order.Store(&order)
	const stage = "Reading your mods"
	progress(stage, 0)
	mods, err := ws.ScanOrder(lo.Map(record.Mods, func(mod app.Mod, _ int) string { return mod.ID }),
		func(percent int) { progress(stage, percent) })
	if err != nil {
		return nil, err
	}
	ws.Mods.Store(mods)
	roots := mods.Roots[len(ws.Install.Roots):]
	ws.watcher, err = startWatcher(lo.Map(roots, func(root files.Root, _ int) string { return root.Dir }),
		func(changes []FileChange) {
			// a file that cannot be scanned again keeps its old entries until the next change
			_ = ws.Rescan(changes)
			change(changes)
		})
	if err != nil {
		return nil, err
	}
	return ws, nil
}

// findRecords returns the record of the workspace with the given id, the record of its install and the install's game.
// It returns an error if any of them is missing.
func findRecords(records app.Config, workspaceID string) (app.Workspace, app.Install, *game.Game, error) {
	record, ok := lo.Find(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
	if !ok {
		return app.Workspace{}, app.Install{}, nil, fmt.Errorf("the workspace %s is not recorded", workspaceID)
	}
	install, row, err := findInstall(records, record.Install)
	return record, install, row, err
}

// findInstall returns the record of the install with the given id and its game. It returns an error if the install is
// not recorded or its game is not supported.
func findInstall(records app.Config, installID string) (app.Install, *game.Game, error) {
	install, ok := lo.Find(records.Installs, func(install app.Install) bool { return install.ID == installID })
	if !ok {
		return app.Install{}, nil, fmt.Errorf("the install %s is not recorded", installID)
	}
	row := game.LookupGame(install.Game)
	if row == nil {
		return app.Install{}, nil, fmt.Errorf("the game %s is not supported", install.Game)
	}
	return install, row, nil
}

// resolveCaches returns the folder of an install's caches. A new version of the install, the dumps or the app gets a
// new folder. It returns an error if the dumps cannot be listed.
func resolveCaches(cacheDir string, row *game.Game, install app.Install, build string) (string, error) {
	dumpsAt, err := schema.FindDumpsAt(row.FindDumpDirs(install.Path))
	if err != nil {
		return "", err
	}
	folder := fmt.Sprintf("%s-%s-%d-%s", resolveVersion(row, install), game.LoadChecksum(install.Path), dumpsAt.Unix(), build)
	return filepath.Join(cacheDir, row.ID, folder), nil
}

// resolveVersion returns an install's version: the one its files say, else the one its record holds.
func resolveVersion(row *game.Game, install app.Install) string {
	return cmp.Or(row.LoadVersion(install.Path), install.Version)
}

// loadInstall sets the session's schema and install index from the caches' folder. It builds what a cache lacks and
// saves it there. A schema it builds gets the given version.
func (ws *Session) loadInstall(installPath, version string, progress func(stage string, percent int)) error {
	schemaCache, indexCache := filepath.Join(ws.caches, schemaFile), filepath.Join(ws.caches, indexFile)
	language, hasSchema := files.LoadGob[*schema.Schema](schemaCache)
	install, hasInstall := index.LoadIndex(indexCache)
	if hasSchema && hasInstall {
		ws.Schema, ws.Install = language, install
		return ws.loadTranslation(progress)
	}
	progress("Listing the game's files", -1)
	roots, err := ws.Game.ListInstallRoots(installPath)
	if err != nil {
		return err
	}
	mounts, err := files.Mount(roots)
	if err != nil {
		return err
	}
	if !hasSchema {
		progress("Reading the game's documentation", -1)
		if language, err = schema.Build(ws.Game, installPath, mounts.Files, index.ParseFile); err != nil {
			return err
		}
		language.Version = version
		if err = files.SaveGob(schemaCache, language); err != nil {
			return err
		}
		// The built schema's names are slices of the game files' text, which they would keep in memory; the cache's
		// copy holds none of it, and is what a later open reads.
		if language, hasSchema = files.LoadGob[*schema.Schema](schemaCache); !hasSchema {
			return fmt.Errorf("cannot read back the schema cache %s", schemaCache)
		}
	}
	if !hasInstall {
		install = index.Scan(language, mounts, progress)
		if err = files.SaveGob(indexCache, install); err != nil {
			return err
		}
	}
	ws.Schema, ws.Install = language, install
	return ws.loadTranslation(progress)
}

// loadTranslation adds the game's localization in the workspace's language
// to the install's index, where that is not the base language. It reads the
// language's cache, and scans the language and writes the cache where the
// cache is missing or was made on another scan of the install.
func (ws *Session) loadTranslation(progress func(stage string, percent int)) error {
	if len(ws.Languages()) < 2 {
		return nil
	}

	cache := filepath.Join(ws.caches, fmt.Sprintf(translationFile, ws.Language))
	if translation, ok := files.LoadGob[index.Translation](cache); ok && ws.Install.Apply(translation) {
		return nil
	}

	progress("Reading the game's "+loc.DescribeLanguage(ws.Language)+" texts", -1)

	return files.SaveGob(cache, ws.Install.ScanTranslation(ws.Schema, ws.Language))
}

// removeEntries deletes every file and folder in a folder but the one of the given base name, or every one for "". It
// goes on past one it cannot delete and returns each failure. It returns an error if the folder cannot be read.
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

// readMod returns a recorded mod with its descriptor and thumbnail, read by the given game's rule. A mod whose folder
// is missing is broken, and one whose descriptor cannot be read has an empty one.
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

// refreshMod reads a mod's descriptor and thumbnail again and puts a new copy of the load order in place.
func (ws *Session) refreshMod(modID string) {
	order := lo.Map(*ws.Order.Load(), func(mod Mod, _ int) Mod {
		if mod.ID == modID {
			return readMod(ws.Game, mod.Mod)
		}
		return mod
	})
	ws.Order.Store(&order)
}

// ScanOrder returns an index of the mods scanned in another load order, calling the given function with the percent of
// their files scanned. It returns an error if the ids do not name each mod once.
func (ws *Session) ScanOrder(modIDs []string, progress func(percent int)) (*index.Index, error) {
	ordered, err := reorderMods(*ws.Order.Load(), modIDs, func(mod Mod) string { return mod.ID })
	if err != nil {
		return nil, err
	}
	var roots []files.Root
	for _, mod := range ordered {
		if !mod.Broken {
			roots = append(roots, files.Root{
				Dir: mod.Dir, Origin: mod.ID, Stages: ws.Game.Stages, ReplacePaths: mod.Descriptor.ReplacePaths,
			})
		}
	}
	return index.ScanMods(ws.Schema, ws.Install, roots, progress)
}

// Close stops the watcher and drops the open files' buffers and documents.
func (ws *Session) Close() {
	// the watcher may be inside a rescan that waits for the lock, so it stops first
	if ws.watcher != nil {
		ws.watcher.Close()
		ws.watcher = nil
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	clear(ws.buffers)
	ws.documents.Purge()
}

// WalkText parses the given text of a file in the dialect of its format and
// walks it.
func (ws *Session) WalkText(place Place, text string) schema.Document {
	return ws.walk(parseDocument(place, text))
}

// walk gives a document what the walk decides: the context of each statement
// of a script file, or of each message of a localization file. Any other
// document comes back as it is.
func (ws *Session) walk(document schema.Document) schema.Document {
	switch {
	case document.Messages != nil:
		document.MessageContexts = ws.Schema.WalkMessages(document.Messages)
	case document.File.Format.IsScript():
		document.Contexts = ws.Schema.Walk(document.Tree, document.File, nil)
	}
	return document
}

// parseDocument parses the given text of a file in the dialect of its
// format: a localization file's as messages, any other as script.
func parseDocument(place Place, text string) schema.Document {
	if place.Format == files.FormatLocalization {
		return schema.Document{File: place.File, Tree: jomini.NewTree(text), Messages: loc.Parse(text, place.Path)}
	}
	return schema.Document{File: place.File, Tree: jomini.Parse(text)}
}

// WalkDocument returns the document of the file with the given path,
// as LoadDocument finds it, with the walk's contexts if it is a script file.
// A cached file is walked the first time it is asked for here, since most
// readers of the cache read only the tree.
func (ws *Session) WalkDocument(path string) (schema.Document, error) {
	// the lock is held through the walk, so no rescan runs before the
	// walked document is cached
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	document, err := ws.findDocument(path)
	if err != nil || document.Contexts != nil || document.MessageContexts != nil {
		return document, err
	}

	document = ws.walk(document)
	ws.documents.Add(files.ToKey(path), document)

	return document, nil
}

// LoadDocument returns the document of the file with the given path. It is the editor's while the editor has the
// file open, else the cached one, else the file is read and parsed. It returns an error if the file cannot be read.
func (ws *Session) LoadDocument(path string) (schema.Document, error) {
	// the lock is held through the parse, so no rescan runs before the document is cached
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.findDocument(path)
}

// findDocument does what LoadDocument does, for a caller that holds the
// session's lock.
func (ws *Session) findDocument(path string) (schema.Document, error) {
	key := files.ToKey(path)
	if document, open := ws.buffers[key]; open {
		return document, nil
	}
	if document, ok := ws.documents.Get(key); ok {
		return document, nil
	}
	text, err := files.ReadText(path)
	if err != nil {
		return schema.Document{}, err
	}
	document := parseDocument(ws.Locate(ws.Mods.Load(), path), text.Text)
	ws.documents.Add(key, document)
	return document, nil
}

// LoadTree returns the tree of the file with the given path, as LoadDocument finds it. It returns an error if the file
// cannot be read.
func (ws *Session) LoadTree(path string) (*jomini.Tree, error) {
	document, err := ws.LoadDocument(path)
	return document.Tree, err
}

// SetText parses the editor's text of an open file and keeps it as the file's buffer, replacing the last.
func (ws *Session) SetText(path, text string) {
	document := ws.WalkText(ws.Locate(ws.Mods.Load(), path), text)
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.buffers[files.ToKey(path)] = document
	ws.Edits.Add(1)
}

// CloseBuffer drops an open file's buffer. A file with none is left alone.
func (ws *Session) CloseBuffer(path string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.buffers, files.ToKey(path))
	ws.Edits.Add(1)
}

// LookupBuffer returns the editor's text for an open file. It returns false if the editor does not have the file
// open.
func (ws *Session) LookupBuffer(path string) (string, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	document, open := ws.buffers[files.ToKey(path)]
	if !open {
		return "", false
	}
	return document.Tree.Src, true
}

// LoadSource returns the text of the file with the given path: the editor's
// while the editor has the file open, else a cached document's, else the one
// on disk. A missing file reads as empty. It returns an error if the file
// cannot be read.
func (ws *Session) LoadSource(path string) (string, error) {
	if source, open := ws.LookupBuffer(path); open {
		return source, nil
	}
	if document, cached := ws.documents.Get(files.ToKey(path)); cached {
		return document.Tree.Src, nil
	}
	text, err := files.LoadText(path)
	return text.Text, err
}

// Languages returns the languages a text is looked up in, in order: the
// workspace's own, then the base language. Every reader of a text follows
// this order.
func (ws *Session) Languages() []string {
	if ws.Language == "" || ws.Language == game.BaseLanguage {
		return []string{game.BaseLanguage}
	}

	return []string{ws.Language, game.BaseLanguage}
}

// LookupText returns a localization key's text in the first of the session's
// languages that has it, with the message the game shows. It returns false
// if no file the game reads has the key in either.
func (ws *Session) LookupText(mods *index.Index, key string) (text string, winner index.Winner, ok bool) {
	if winner, ok = mods.FindText(key, ws.Languages()); !ok {
		return "", index.Winner{}, false
	}

	return mods.Text(winner.Site), winner, true
}

// LookupDefinitionText returns a definition's text under the given key, game.TitleKey for its title or
// game.DescriptionKey for its description. It is the text of the localization key the definition writes there, else
// of the key the game builds from its name for its database. It returns "" if no such text exists.
func (ws *Session) LookupDefinitionText(mods *index.Index, site index.Site, key string) string {
	written, _ := mods.FindTextKey(site, key)
	return ws.resolveText(mods, written, mods.ToReference(site), key)
}

// LookupStatementText returns the text of the definition that a statement of
// a tree makes, under the given key, as LookupDefinitionText finds it, for a
// text the index has not scanned. The reference names the definition.
func (ws *Session) LookupStatementText(
	mods *index.Index, tree *jomini.Tree, statement int32, reference schema.Reference, key string,
) string {
	written := ""
	if position, ok := schema.FindTextKey(tree, statement, key); ok {
		written = tree.Unquoted(tree.Statements[position].Value)
	}
	return ws.resolveText(mods, written, reference, key)
}

// resolveText returns the text of the localization key a definition writes,
// "" for none, else of the key the game builds from the definition's name
// for its database. It returns "" if no such text exists.
func (ws *Session) resolveText(mods *index.Index, written string, reference schema.Reference, key string) string {
	if written != "" {
		if text, _, ok := ws.LookupText(mods, written); ok {
			return text
		}
	}
	pattern, ok := ws.Game.LookupTextKey(reference.Database, key)
	if !ok {
		return ""
	}
	text, _, _ := ws.LookupText(mods, fmt.Sprintf(pattern, reference.Name))
	return text
}

// FindStatement returns the tree of a site's file and the statement that writes it. It returns an error if the file
// cannot be read or has no statement there.
func (ws *Session) FindStatement(mods *index.Index, site index.Site) (*jomini.Tree, int32, error) {
	path := mods.File(site).Path
	tree, err := ws.LoadTree(path)
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

// Rescan brings the mods' index up to date with a batch of file changes. A change to a mod's descriptor reads the
// descriptor again and is marked in the batch. It returns the errors of the changes it cannot read, after the others.
func (ws *Session) Rescan(changes []FileChange) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	mods := ws.Mods.Load()
	var changed []files.File
	var failure error
	for position, change := range changes {
		place := ws.Locate(mods, change.Path)
		if descriptor, _ := resolveDescriptor(ws.Game, place.Root); place.Root != "" && files.ToKey(descriptor) == place.Key {
			ws.refreshMod(place.Origin)
			changes[position].Descriptor = true
		}
		// the index skips the watched dot folders, as it skips every dot folder
		if place.Root == "" || place.Origin == files.OriginVanilla || lo.Some(strings.Split(place.Rel, "/"), watchedFolders) {
			continue
		}
		listed, err := ws.listChanged(mods, place, change.Deleted)
		changed, failure = append(changed, listed...), errors.Join(failure, err)
	}
	changed = lo.UniqBy(changed, func(file files.File) string { return file.Key })
	if len(changed) == 0 {
		return failure
	}
	next := mods.Clone()
	if err := next.ScanFiles(ws.Schema, changed); err != nil {
		return errors.Join(failure, err)
	}
	ws.Mods.Store(next)
	// what the mods declare may have changed, so every kept walk is made
	// again: a cached file's when it is next loaded, an open file's now
	ws.documents.Purge()
	for key, document := range ws.buffers {
		if document.Contexts != nil {
			document.Contexts = ws.Schema.Walk(document.Tree, document.File, nil)
			ws.buffers[key] = document
		}
	}
	return failure
}

// listChanged returns the files a changed path stands for: the file itself, or every file under it for a folder. For
// a deleted path they are the files the mods' index had at or under it.
func (ws *Session) listChanged(mods *index.Index, place Place, deleted bool) ([]files.File, error) {
	switch {
	case deleted:
		return lo.FilterMap(mods.ListOwnFiles(), func(position int32, _ int) (files.File, bool) {
			file := mods.Files[position]
			return file, file.Key == place.Key || files.IsUnder(place.Path, file.Path)
		}), nil
	case files.IsFolder(place.Path):
		listing, err := files.Walk(files.Root{Dir: place.Path})
		return lo.Map(listing, func(file files.File, _ int) files.File { return ws.Locate(mods, file.Path).File }), err
	}
	return []files.File{place.File}, nil
}
