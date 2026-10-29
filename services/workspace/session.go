// Package workspace holds the one open workspace: the install's schema and index, the mods' index on top of it, the
// open files and the watcher. Its service also keeps the records of installs, workspaces and mods.
package workspace

import (
	"cmp"
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
	Effects     int // how many of the schema's commands are declared as effects
	Triggers    int // how many are declared as triggers
	Install     *index.Index
	Mods        atomic.Pointer[index.Index] // on top of Install; a rescan replaces it whole
	Order       []Mod                       // in load order
	Language    string                      // the localization language shown beside the game's base language
	updatedFrom string                      // the install's recorded version when the open found another
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

// openSession opens a workspace: it loads or scans the install, scans the mods and starts the watcher. It returns an
// error if the workspace or its install is not recorded, or a scan fails.
func (wsp *Service) openSession(workspaceID string, progress func(stage string, percent int), change func(changes []FileChange)) (*Session, error) {
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
	if err := ws.loadInstall(install.Path, resolveVersion(install), progress); err != nil {
		return nil, err
	}
	ws.Effects, ws.Triggers = countDeclared(ws.Schema, game.DeclaredEffect), countDeclared(ws.Schema, game.DeclaredTrigger)
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
	return filepath.Join(cacheDir, row.ID, fmt.Sprintf("%s-%d-%s", resolveVersion(install), dumpsAt.Unix(), build)), nil
}

// resolveVersion returns an install's version: the one its files say, else the one its record holds, such as a version
// typed when the install was added.
func resolveVersion(install app.Install) string {
	return cmp.Or(game.LoadGameVersion(install.Game, install.Path), install.Version)
}

// loadInstall sets the session's schema and install index from the caches' folder. It builds what a cache lacks and
// saves it there, a new schema under the given version of the install.
func (ws *Session) loadInstall(installPath, version string, progress func(stage string, percent int)) error {
	schemaCache, indexCache := filepath.Join(ws.caches, schemaFile), filepath.Join(ws.caches, indexFile)
	language, hasSchema := files.LoadGob[*schema.Schema](schemaCache)
	install, hasInstall := index.LoadIndex(indexCache)
	if hasSchema && hasInstall {
		ws.Schema, ws.Install = language, install
		return nil
	}
	progress("listing", 0)
	roots, err := game.ListInstallRoots(installPath)
	if err != nil {
		return err
	}
	listing, err := index.ListFiles(roots)
	if err != nil {
		return err
	}
	if !hasSchema {
		progress("schema", 0)
		if language, err = schema.Build(ws.Game, installPath, listing, index.ParseFile); err != nil {
			return err
		}
		language.Version = version
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
func (ws *Session) scanMods(records []app.Mod, progress func(stage string, percent int)) error {
	for _, record := range records {
		ws.Order = append(ws.Order, readMod(ws.Game, record))
	}
	progress("mods", 0)
	mods, err := ws.ScanOrder(lo.Map(records, func(record app.Mod, _ int) string { return record.ID }))
	if err != nil {
		return err
	}
	ws.Mods.Store(mods)
	return nil
}

// readMod returns a recorded mod with its descriptor and thumbnail, read by the given game's rule. A mod whose folder
// is missing is broken, and one whose descriptor cannot be read has an empty one.
func readMod(row *game.Game, record app.Mod) Mod {
	mod := Mod{Mod: record}
	if files.IsFolder(record.Dir) {
		mod.Descriptor, _ = ReadDescriptor(row, record.Dir)
		mod.Thumbnail = findThumbnail(row, record.Dir, mod.Descriptor.Thumbnail)
	} else {
		mod.Broken = true
	}
	return mod
}

// ScanOrder returns an index of the mods scanned in another load order. It returns an error if the ids do not name
// each mod once.
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
	tree, err := index.ParseFile(path)
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

// LookupText returns a localization key's text and the message the game keeps. It returns false if no message has
// the key.
func (ws *Session) LookupText(mods *index.Index, key string) (text string, winner index.Winner, ok bool) {
	messages := mods.ListMessages(key)
	for _, language := range []string{ws.Language, game.BaseLanguage} {
		candidates := lo.Filter(messages, func(message index.Site, _ int) bool {
			return mods.Language(message) == language && mods.IsLoaded(message.File)
		})
		if len(candidates) == 0 {
			continue
		}
		// every form of the replace folder ranks before a file outside one
		outside := len(ws.Game.LocReplaceFolders)
		rank := func(candidate index.Site) int {
			position, replaces := schema.FindLocReplacement(ws.Game, mods.File(candidate).Rel, language)
			return lo.Ternary(replaces, position, outside)
		}
		winner = index.Winner{Site: slices.MinFunc(candidates, func(a, b index.Site) int { return cmp.Compare(rank(a), rank(b)) })}
		switch {
		case lo.CountBy(candidates, func(candidate index.Site) bool { return rank(candidate) == rank(winner.Site) }) > 1:
			winner.Reason = index.Unsettled
		case rank(winner.Site) < outside:
			winner.Reason = "replace-folder"
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

// LookupTitle returns the title of a definition, or "" if it has none.
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

// FindDelay returns the delay written for a firing, such as "days = 5", as the given index of the mods lists the use
// that fires. It returns "" if there is none or the file cannot be read.
func (ws *Session) FindDelay(mods *index.Index, use index.Site) string {
	tree, err := ws.LoadTree(mods.File(use).Path)
	if err != nil {
		return ""
	}
	// the delay is in the first block, going up from the use, that holds one directly or in a delay block
	for statement := tree.FindStatement(mods.Occurrence(use).Span.Start); statement > 0; statement = tree.Statements[statement].Parent {
		holders := []int32{statement}
		if block, ok := tree.FindChild(statement, game.EventDelay.Block); ok {
			holders = append(holders, block)
		}
		for _, holder := range holders {
			if delay := findDuration(tree, holder); delay != "" {
				return delay
			}
		}
	}
	return ""
}

// findDuration returns the statement that writes a delay in a block, such as "days = 5". It returns "" if there is
// none.
func findDuration(tree *jomini.Tree, block int32) string {
	for _, key := range game.EventDelay.Keys {
		if child, ok := tree.FindChild(block, key); ok {
			return tree.Text(tree.Statements[child].Span)
		}
	}
	return ""
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
	if statement := occurrence.Statement; int(statement) < len(tree.Statements) &&
		tree.Statements[statement].Span.Contains(occurrence.Span.Start) {
		return tree, statement, nil
	}
	return nil, 0, fmt.Errorf("%s has no statement where the index read one", path)
}

// Rescan brings the mods' index up to date with a batch of file changes. It returns the error of a change it cannot
// read, after the others.
func (ws *Session) Rescan(changes []FileChange) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	mods := ws.Mods.Load()
	var changed []files.File
	var failure error
	for _, change := range changes {
		place := ws.Locate(mods, change.Path)
		// the index skips the agents' folders, as it skips every dot folder
		if place.Root == "" || place.ReadOnly || isInAgentFolder(place.Rel) {
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
