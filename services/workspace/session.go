// Package workspace holds the one open workspace: the install's schema and index, the mods' index on top of it, the
// open files and the watcher. Its service also keeps the records of installs, workspaces and mods.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	Order       []Mod                       // in load order
	Language    string                      // the localization language shown beside the game's base language
	mu          sync.RWMutex                // over the buffers and the trees, and one rescan at a time
	buffers     map[string]*jomini.Tree     // the editor's open files, by the path's key
	trees       *lru.Cache[string, *jomini.Tree]
	watcher     *Watcher
	caches      string // the folder the install's schema and index were loaded from or saved in
}

// The names of the two caches in the caches' folder.
const (
	schemaFile = "schema.gob"
	indexFile  = "index.gob"
)

// openSession opens the workspace with the given id. It loads the install's schema and index from the caches or scans
// them, scans the mods on top and starts the watcher. The given progress function is called at each stage of the scan.
// The given change function is called with each batch of files that change. It returns an error if the workspace, its
// install or the install's game is not recorded or not supported, or if a scan fails.
func (wsp *Service) openSession(workspaceID string, progress func(stage string), change func(changes []FileChange)) (*Session, error) {
	record, install, row, err := findRecords(wsp.Config.Config(), workspaceID)
	if err != nil {
		return nil, err
	}
	folder, err := resolveCaches(wsp.Config.Dirs.Cache, row, install, wsp.Build)
	if err != nil {
		return nil, err
	}
	trees, err := lru.New[string, *jomini.Tree](256)
	if err != nil {
		return nil, err
	}
	ws := &Session{
		ID: workspaceID, Game: row, InstallPath: install.Path, Language: record.Language,
		buffers: map[string]*jomini.Tree{}, trees: trees, caches: folder,
	}
	if err := ws.loadInstall(install.Path, progress); err != nil {
		return nil, err
	}
	deleteOlderCaches(folder)
	if err := ws.scanMods(record.Mods, progress); err != nil {
		return nil, err
	}
	if err := ws.openWatcher(change); err != nil {
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

// resolveCaches returns the folder of an install's caches. Its name changes with the install's version, the game's
// dumps and the app's version. It returns an error if the dumps cannot be listed.
func resolveCaches(cacheDir string, row *game.Game, install app.Install, build string) (string, error) {
	dumpsAt, err := schema.FindDumpsAt(game.FindDumpDirs(row.ID, install.Path))
	if err != nil {
		return "", err
	}
	version := game.LoadGameVersion(row.ID, install.Path)
	return filepath.Join(cacheDir, row.ID, fmt.Sprintf("%s-%d-%s", version, dumpsAt.Unix(), build)), nil
}

// loadInstall sets the session's schema and install index from the caches' folder. It builds what a cache lacks and
// saves it there.
func (ws *Session) loadInstall(installPath string, progress func(stage string)) error {
	schemaCache, indexCache := filepath.Join(ws.caches, schemaFile), filepath.Join(ws.caches, indexFile)
	language, hasSchema := files.LoadGob[*schema.Schema](schemaCache)
	install, hasInstall := index.LoadIndex(indexCache)
	if hasSchema && hasInstall {
		ws.Schema, ws.Install = language, install
		return nil
	}
	progress("listing")
	roots, err := game.ListInstallRoots(installPath)
	if err != nil {
		return err
	}
	listing, err := index.ListFiles(roots)
	if err != nil {
		return err
	}
	if !hasSchema {
		progress("schema")
		if language, err = schema.Build(ws.Game, installPath, listing, parseFile); err != nil {
			return err
		}
		if err = files.SaveGob(schemaCache, language); err != nil {
			return err
		}
	}
	if !hasInstall {
		install = index.Scan(language, roots, listing, progress)
		if err = files.SaveGob(indexCache, install); err != nil {
			return err
		}
	}
	ws.Schema, ws.Install = language, install
	return nil
}

// parseFile reads the file with the given path and parses it as script. It returns an error if the file cannot be
// read.
func parseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return nil, err
	}
	return jomini.Parse(text.Text), nil
}

// deleteOlderCaches deletes every folder beside the given caches' folder, which are the game's caches of other
// versions. A file there stays, and so does a folder that cannot be deleted.
func deleteOlderCaches(folder string) {
	entries, err := os.ReadDir(filepath.Dir(folder))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != filepath.Base(folder) {
			_ = os.RemoveAll(filepath.Join(filepath.Dir(folder), entry.Name()))
		}
	}
}

// scanMods sets the session's mods from the workspace's records and scans them on top of the install. A mod whose
// folder is missing is broken and is not scanned. A mod whose descriptor cannot be read is scanned with an empty one.
func (ws *Session) scanMods(records []app.Mod, progress func(stage string)) error {
	for _, record := range records {
		mod := Mod{Mod: record}
		if files.IsFolder(record.Dir) {
			mod.Descriptor, _ = ReadDescriptor(ws.Game, record.Dir)
		} else {
			mod.Broken = true
		}
		ws.Order = append(ws.Order, mod)
	}
	progress("mods")
	mods, err := ws.ScanOrder(lo.Map(records, func(record app.Mod, _ int) string { return record.ID }))
	if err != nil {
		return err
	}
	ws.Mods.Store(mods)
	return nil
}

// ScanOrder scans the workspace's mods on top of the install's index as if the game loaded them in the given
// order, each mod named by its id. The session keeps its own index, so nothing changes for other readers. A broken
// mod is left out. It returns an error if the ids do not name each of the workspace's mods once.
func (ws *Session) ScanOrder(modIDs []string) (*index.Index, error) {
	ordered, err := reorderMods(ws.Order, modIDs, func(mod Mod) string { return mod.ID })
	if err != nil {
		return nil, err
	}
	var roots []files.Root
	var replaced [][]string
	for _, mod := range ordered {
		if !mod.Broken {
			roots = append(roots, files.Root{Dir: mod.Dir, Origin: mod.ID})
			replaced = append(replaced, mod.Descriptor.ReplacePaths)
		}
	}
	return index.ScanMods(ws.Schema, ws.Install, roots, replaced)
}

// openWatcher starts the session's watcher over the mods' folders. Each batch of changes is scanned again, and then
// reported to the given function.
func (ws *Session) openWatcher(change func(changes []FileChange)) error {
	roots := ws.Mods.Load().Roots[len(ws.Install.Roots):]
	watcher, err := startWatcher(lo.Map(roots, func(root files.Root, _ int) string { return root.Dir }),
		func(changes []FileChange) {
			// a file that cannot be scanned again keeps its old entries until the next change
			_ = ws.Rescan(changes)
			change(changes)
		})
	if err != nil {
		return err
	}
	ws.watcher = watcher
	return nil
}

// Close stops the watcher and drops the open files' buffers and trees.
func (ws *Session) Close() {
	// the watcher's loop may be inside a rescan that waits for the lock, so it is stopped before the lock is taken
	if ws.watcher != nil {
		ws.watcher.Close()
		ws.watcher = nil
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	clear(ws.buffers)
	ws.trees.Purge()
}

// LoadTree returns the tree of the file with the given path. It is the buffer's while the editor has the file open,
// else the cached one, else the file is read and parsed. It returns an error if the file cannot be read.
func (ws *Session) LoadTree(path string) (*jomini.Tree, error) {
	key := files.ToKey(path)
	// the lock is held through the parse, so that no rescan runs between the parse and the tree's place in the cache
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	if tree, open := ws.buffers[key]; open {
		return tree, nil
	}
	if tree, ok := ws.trees.Get(key); ok {
		return tree, nil
	}
	tree, err := parseFile(path)
	if err != nil {
		return nil, err
	}
	ws.trees.Add(key, tree)
	return tree, nil
}

// SetText parses the editor's text of an open file and keeps it as the file's buffer, replacing the last.
func (ws *Session) SetText(path, text string) {
	tree := jomini.Parse(text)
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.buffers[files.ToKey(path)] = tree
}

// CloseBuffer drops an open file's buffer. A file with none is left alone.
func (ws *Session) CloseBuffer(path string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.buffers, files.ToKey(path))
}

// LookupBuffer returns the editor's text for an open file. It returns false if the editor does not have the file
// open.
func (ws *Session) LookupBuffer(path string) (string, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	tree, open := ws.buffers[files.ToKey(path)]
	if !open {
		return "", false
	}
	return tree.Src, true
}

// LookupText returns the text of a localization key in the session's language, else in the base language, together
// with the message the game keeps, as the given index of the mods lists it. A message in a replace folder wins, for the
// reason "replace". Otherwise the first loaded wins, for the reason "order". It returns false if no message has it.
func (ws *Session) LookupText(mods *index.Index, key string) (text string, winner index.Winner, ok bool) {
	messages := mods.ListMessages(key)
	for _, language := range []string{ws.Language, game.BaseLanguage} {
		candidates := lo.Filter(messages, func(message index.Site, _ int) bool {
			return mods.Language(message) == language && mods.IsLoaded(message.File)
		})
		if len(candidates) == 0 {
			continue
		}
		winner = index.Winner{Site: candidates[0], Reason: "order"}
		if position := slices.IndexFunc(candidates, func(candidate index.Site) bool {
			return schema.IsLocReplacement(ws.Game, mods.File(candidate).Rel, language)
		}); position >= 0 {
			winner = index.Winner{Site: candidates[position], Reason: "replace"}
		}
		return ws.findText(mods, winner.Site, language), winner, true
	}
	return "", index.Winner{}, false
}

// findText returns the text of the message at a site, in the given language, as the given index of the mods lists it.
// It returns "" if the file cannot be read or no longer has the message at that line.
func (ws *Session) findText(mods *index.Index, site index.Site, language string) string {
	file := mods.File(site)
	if file.Origin != game.OriginVanilla || language == game.BaseLanguage {
		return mods.Text(site)
	}
	tree, err := ws.LoadTree(file.Path)
	if err != nil {
		return ""
	}
	number := int(mods.Occurrence(site).Line)
	if number >= tree.Lines.CountLines() {
		return ""
	}
	line := tree.Lines.LineSpan(number)
	message, ok := loc.ParseMessage(tree.Src[line.Start:line.End], line.Start)
	if !ok || mods.ToCaseless(game.Localization, message.Key) != mods.ToReference(site).Name {
		return ""
	}
	return message.Text
}

// LookupTitle returns the title of a definition, as the given index of the mods lists it. It is the text of the message
// that the definition's title key names, as an event has. Any other definition has the message named like it, or for a
// game concept the concept prefix and its name. It returns "" if there is no such message.
func (ws *Session) LookupTitle(mods *index.Index, site index.Site) string {
	if key, ok := ws.FindTitleKey(mods, site); ok {
		if text, _, ok := ws.LookupText(mods, key); ok {
			return text
		}
	}
	reference := mods.ToReference(site)
	key := reference.Name
	if reference.Database == ws.Schema.Concepts {
		key = ws.Game.ConceptPrefix + key
	}
	text, _, _ := ws.LookupText(mods, key)
	return text
}

// FindTitleKey returns the localization key that a definition writes as its title, as the given index of the mods lists
// the definition. It returns false if the definition writes none or its statement cannot be found.
func (ws *Session) FindTitleKey(mods *index.Index, site index.Site) (string, bool) {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return "", false
	}
	return tree.FindChildValue(statement, game.TitleKey)
}

// FindStatement returns the tree of the file that holds a site, as the given index of the mods lists it, and the
// statement that writes it. The statement is 0, the file itself, for a definition the file names, a message and a row.
// It returns an error if the file cannot be read or no longer has a statement at the site.
func (ws *Session) FindStatement(mods *index.Index, site index.Site) (*jomini.Tree, int32, error) {
	path := mods.File(site).Path
	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, 0, err
	}
	occurrence := mods.Occurrence(site)
	if statement := occurrence.Statement; int(statement) < len(tree.Statements) &&
		tree.Statements[statement].Span.Contains(occurrence.Span.Start) {
		return tree, statement, nil
	}
	return nil, 0, fmt.Errorf("%s has no statement where the index read one", path)
}

// Rescan brings the mods' index up to date with the given batch of file changes. It scans the changed files again and
// drops the deleted ones. A change to a file of the game or to a path under no root is skipped. A change that cannot
// be listed does not stop the others, and its error is returned.
func (ws *Session) Rescan(changes []FileChange) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	mods := ws.Mods.Load()
	var changed []files.File
	var failure error
	for _, change := range changes {
		place := ws.Locate(mods, change.Path)
		if place.Root == "" || place.ReadOnly {
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
	for _, file := range changed {
		ws.trees.Remove(file.Key)
	}
	return failure
}

// listChanged returns the files a changed path stands for: the file itself, or every file under it where the path is
// a folder. For a deleted path they are the files of the mods' index that were the path or under it.
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
