// Package workspace holds the workspace that is open in the editor: the install's schema and index, the mods'
// index on top of it, the open files' texts and trees, and the watcher. Only one workspace is open at a time, and
// the service that owns it also keeps the records of games, installs, workspaces and mods.
package workspace

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
)

// A Buffer is the editor's copy of an open file: its text and its tree, parsed once when the text arrives.
type Buffer struct {
	Text string
	Tree *jomini.Tree
}

// A Session is the open workspace as every service reads it: the install's schema and index, the mods' index on
// top of it, and the open files' texts and trees. Readers share one lock.
type Session struct {
	ID       string
	Game     *game.Game
	Schema   *schema.Schema
	Install  *index.Index
	Mods     *index.Index
	Order    []Mod  // in load order
	Language string // the localization language shown beside the game's base language
	mu       sync.RWMutex
	buffers  map[string]*Buffer // by the path's key
	trees    *lru.Cache[string, *jomini.Tree]
	watcher  *Watcher
}

// Open opens a workspace: it loads the install's schema and index from the cache or scans them, scans the mods on
// top, and starts the watcher. The first function is called at each stage of the scan, and the second for each
// file the watcher sees change.
func Open(dirs app.Dirs, config app.Config, workspaceID string, progress func(stage string), change func(path string, deleted bool)) (*Session, error) {
	panic("stub")
}

// Close stops the watcher and drops the buffers.
func (ws *Session) Close() { panic("stub") }

// LoadTree returns a file's tree: the open buffer's while the editor has the file, else the cached one, else the
// file is read and parsed.
func (ws *Session) LoadTree(path string) (*jomini.Tree, error) { panic("stub") }

// SetText stores the editor's text for an open file and parses it.
func (ws *Session) SetText(path, text string) { panic("stub") }

// CloseBuffer drops an open file's buffer.
func (ws *Session) CloseBuffer(path string) { panic("stub") }

// LookupText returns the text of a localization key in the session's language, falling back to the base language,
// together with the message the game keeps: one in a replace folder first, else the first loaded. It returns false
// if no message has the key.
func (ws *Session) LookupText(key string) (text string, site index.Site, ok bool) { panic("stub") }

// LookupTitle returns a definition's title: the text of the message whose key is the definition's name, or for a
// game concept the game's concept prefix followed by the name. It returns "" if there is no such message.
func (ws *Session) LookupTitle(definition schema.Definition) string { panic("stub") }

// FindStatement returns the tree of the file that holds a definition and the index of the statement that writes
// it.
func (ws *Session) FindStatement(site index.Site) (*jomini.Tree, int32, error) { panic("stub") }

// Rescan walks a changed mod file again and updates the mods' index, or drops the file from it when the flag says
// it was deleted.
func (ws *Session) Rescan(path string, deleted bool) error { panic("stub") }
