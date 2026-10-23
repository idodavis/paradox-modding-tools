// Package workspace holds the workspace that is open in the editor: the install's schema and index, the mods'
// index on top of it, the open files' texts and trees, and the watcher. Only one workspace is open at a time, and
// the service that owns it also keeps the records of games, installs, workspaces and mods.
package workspace

import (
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

// A Buffer is the editor's copy of an open file: its text and its tree, parsed once when the text arrives.
type Buffer struct {
	Text string
	Tree *jomini.Tree
}

// A Session is the open workspace as every service reads it: the install's schema and index, the mods' index on
// top of it, and the open files' texts and trees. A request loads the mods' index once and reads that copy to its
// end, since a rescan puts a new copy in place rather than changing the one being read. The open files' texts sit
// behind the session's own lock.
type Session struct {
	ID       string
	Game     *game.Game
	Schema   *schema.Schema
	Install  *index.Index
	Mods     atomic.Pointer[index.Index] // on top of Install; a rescan replaces it whole
	Order    []Mod                       // in load order
	Language string                      // the localization language shown beside the game's base language
	mu       sync.RWMutex                // over the buffers and the trees, and one rescan at a time
	buffers  map[string]*Buffer          // by the path's key
	trees    *lru.Cache[string, *jomini.Tree]
	watcher  *Watcher
	caches   string // the folder the install's schema and index were loaded from or saved in
}

// The names of the two caches in the caches' folder.
const (
	schemaFile = "schema.gob"
	indexFile  = "index.gob"
)

// Open opens a workspace: it loads the install's schema and index from the caches or scans them, scans the mods on
// top, and starts the watcher. The caches' folder is named by the given app version, among other things. The first
// function is called at each stage of the scan, and the second for each file the watcher sees change.
func Open(config *app.ConfigFile, build, workspaceID string, progress func(stage string), change func(path string, deleted bool)) (*Session, error) {
	record, install, row, err := findRecords(config.Config(), workspaceID)
	if err != nil {
		return nil, err
	}
	folder, err := resolveCaches(config.Dirs.Cache, row, install, build)
	if err != nil {
		return nil, err
	}
	trees, err := lru.New[string, *jomini.Tree](256)
	if err != nil {
		return nil, err
	}
	ws := &Session{
		ID: workspaceID, Game: row, Language: record.Language, buffers: map[string]*Buffer{}, trees: trees, caches: folder,
	}
	if err := ws.loadSchema(install.Path, progress); err != nil {
		return nil, err
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

// findRecords returns a workspace's record, the record of its install and the install's game. It returns an error if
// any of them is missing.
func findRecords(records app.Config, workspaceID string) (app.Workspace, app.Install, *game.Game, error) {
	record, ok := lo.Find(records.Workspaces, func(record app.Workspace) bool { return record.ID == workspaceID })
	if !ok {
		return app.Workspace{}, app.Install{}, nil, fmt.Errorf("the workspace %s is not recorded", workspaceID)
	}
	install, row, err := findInstall(records, record.Install)
	return record, install, row, err
}

// findInstall returns an install's record and its game. It returns an error if the install is not recorded or its
// game is not supported.
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

// resolveCaches returns the folder of an install's caches. Its name holds the install's version, the time of the
// game's dumps, which is zero where it has none, and the app's version.
func resolveCaches(cacheDir string, row *game.Game, install app.Install, build string) (string, error) {
	dumps, err := schema.ReadDumps(game.FindDumpDirs(row.ID, install.Path))
	if err != nil {
		return "", err
	}
	return index.CachePath(cacheDir, row.ID, game.LoadGameVersion(row.ID, install.Path), dumps.WrittenAt, build), nil
}

// loadSchema sets the session's schema from the caches' folder, or builds it from the install's files and saves it
// there.
func (ws *Session) loadSchema(installPath string, progress func(stage string)) error {
	cache := filepath.Join(ws.caches, schemaFile)
	if language, ok := app.LoadGob[*schema.Schema](cache); ok {
		ws.Schema = language
		return nil
	}
	progress("schema")
	// The DLC folders sit inside the game's, so each file is given once.
	var paths []string
	for _, root := range listInstallRoots(installPath) {
		listing, err := files.Walk(root, nil)
		if err != nil {
			return err
		}
		paths = append(paths, lo.Map(listing, func(file files.File, _ int) string { return file.Path })...)
	}
	language, err := schema.Build(ws.Game, installPath, paths, parseFile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ws.caches, 0o755); err != nil {
		return err
	}
	if err := app.SaveGob(cache, language); err != nil {
		return err
	}
	ws.Schema = language
	return nil
}

// listInstallRoots returns the folders of an install that the game mounts and the install has, in the order the
// game mounts them.
func listInstallRoots(installPath string) []files.Root {
	var roots []files.Root
	for _, folder := range game.InstallRoots {
		if dir := filepath.Join(installPath, folder); files.IsFolder(dir) {
			roots = append(roots, files.Root{Dir: dir, Origin: game.OriginVanilla})
		}
	}
	return roots
}

// parseFile reads a file and parses it as Paradox script.
func parseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return nil, err
	}
	return jomini.Parse(text.Text), nil
}

// loadInstall sets the session's install index from the caches' folder, or scans the install's files and saves it
// there.
func (ws *Session) loadInstall(installPath string, progress func(stage string)) error {
	cache := filepath.Join(ws.caches, indexFile)
	if install, ok := index.LoadIndex(cache); ok {
		ws.Install = install
		return nil
	}
	dlcFolder := filepath.Join(installPath, game.ScriptRoot, game.DLCFolder)
	dlcs, err := os.ReadDir(dlcFolder)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	roots := listInstallRoots(installPath)
	for _, dlc := range dlcs {
		if dlc.IsDir() {
			roots = append(roots, files.Root{Dir: filepath.Join(dlcFolder, dlc.Name()), Origin: game.OriginVanilla})
		}
	}
	install, err := index.Scan(ws.Game, ws.Schema, roots, progress)
	if err != nil {
		return err
	}
	if err := index.SaveIndex(cache, install); err != nil {
		return err
	}
	ws.Install = install
	return nil
}

// deleteOlderCaches deletes every folder beside the given caches' folder. They are the game's caches of other
// versions. Only folders are deleted, and one that cannot be deleted is left.
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
	return index.ScanMods(ws.Game, ws.Schema, ws.Install, roots, replaced)
}

// openWatcher starts the session's watcher over the mods' folders. Each change is scanned again, and then reported
// to the given function.
func (ws *Session) openWatcher(change func(path string, deleted bool)) error {
	roots := ws.Mods.Load().Roots[len(ws.Install.Roots):]
	watcher, err := startWatcher(lo.Map(roots, func(root files.Root, _ int) string { return root.Dir }),
		func(path string, deleted bool) {
			// a file that cannot be scanned again keeps its old entries until the next change
			_ = ws.Rescan(path, deleted)
			change(path, deleted)
		})
	if err != nil {
		return err
	}
	ws.watcher = watcher
	return nil
}

// Close stops the watcher and drops the buffers.
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

// LoadTree returns a file's tree: the open buffer's while the editor has the file, else the cached one, else the
// file is read and parsed.
func (ws *Session) LoadTree(path string) (*jomini.Tree, error) {
	key := files.ToKey(path)
	// the lock is held through the parse, so that no rescan runs between the parse and the tree's place in the cache
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	if buffer, open := ws.buffers[key]; open {
		return buffer.Tree, nil
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

// SetText stores the editor's text for an open file and parses it.
func (ws *Session) SetText(path, text string) {
	buffer := &Buffer{Text: text, Tree: jomini.Parse(text)}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.buffers[files.ToKey(path)] = buffer
}

// CloseBuffer drops an open file's buffer.
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
	buffer, open := ws.buffers[files.ToKey(path)]
	if !open {
		return "", false
	}
	return buffer.Text, true
}

// LookupText returns the text of a localization key in the session's language, falling back to the base language,
// together with the message the game keeps in the given index of the mods: one in a replace folder first, else the
// first loaded. It returns false if no message has the key.
func (ws *Session) LookupText(mods *index.Index, key string) (text string, site index.Site, ok bool) {
	messages := mods.ListMessages(key)
	for _, language := range []string{ws.Language, game.BaseLanguage} {
		candidates := lo.Filter(messages, func(message index.Site, _ int) bool {
			return mods.Language(message) == language && mods.IsLoaded(message.File)
		})
		if len(candidates) == 0 {
			continue
		}
		site = candidates[0]
		if position := slices.IndexFunc(candidates, func(candidate index.Site) bool {
			return schema.IsLocReplacement(ws.Game, mods.File(candidate).Rel, language)
		}); position >= 0 {
			site = candidates[position]
		}
		return findText(mods, site, key, language), site, true
	}
	return "", index.Site{}, false
}

// findText returns the text of the message at a site, whose key and language are given. The install keeps the texts
// of the base language only, so another language's text is read from its file. It returns "" if the file has no such
// message.
func findText(mods *index.Index, site index.Site, key, language string) string {
	file := mods.File(site)
	if file.Origin != game.OriginVanilla || language == game.BaseLanguage {
		return mods.Text(site)
	}
	text, err := files.ReadText(file.Path)
	if err != nil {
		return ""
	}
	for _, message := range loc.Parse(text.Text, file.Path).Messages {
		if message.Key == key {
			return message.Text
		}
	}
	return ""
}

// LookupTitle returns the title of a definition in the given index of the mods. A definition that writes a title
// key, as an event does, takes the text of the message that key names. Any other takes the message whose key is its
// name, or for a game concept the game's concept prefix followed by its name. It returns "" if there is no such
// message.
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

// FindTitleKey returns the localization key a definition in the given index of the mods writes as its title, as it is
// written. It returns false if the definition writes none or its statement is not found.
func (ws *Session) FindTitleKey(mods *index.Index, site index.Site) (string, bool) {
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return "", false
	}
	child, ok := tree.FindChild(statement, game.TitleKey)
	if !ok {
		return "", false
	}
	return tree.Unquoted(tree.Statements[child].Value), true
}

// FindStatement returns the tree of the file that holds an occurrence in the given index of the mods, and the
// statement that writes it. That is statement 0, the file, for a definition the file names, a message and a row. It
// returns an error if the file cannot be read, or if the editor's text of the file no longer has a statement that
// covers the occurrence.
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

// Rescan walks a changed mod file again, or drops it when the flag says it was deleted, in a new copy of the mods'
// index that then takes the old one's place. A request that is still reading the old copy finishes on it.
func (ws *Session) Rescan(path string, deleted bool) error {
	place := ws.Locate(path)
	if place.Root == "" || place.ReadOnly {
		return nil
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	mods := ws.Mods.Load()
	changed, err := ws.listChanged(mods, place, deleted)
	if err != nil || len(changed) == 0 {
		return err
	}
	next := mods.Clone()
	if err := next.ScanFiles(ws.Schema, changed); err != nil {
		return err
	}
	ws.Mods.Store(next)
	for _, file := range changed {
		ws.trees.Remove(file.Key)
	}
	return nil
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
		listing, err := files.Walk(files.Root{Dir: place.Path}, nil)
		return lo.Map(listing, func(file files.File, _ int) files.File { return ws.Locate(file.Path).File }), err
	}
	return []files.File{place.File}, nil
}
