// Package session holds the live state of one open workspace: the derived.Model
// built from the vanilla cache and the mods, plus the editor buffers layered
// over it. lsp and views read a Session under its RWMutex, one lock per
// request, and call the embedded Model directly; there is no query layer.
//
// Files: session.go is the Session type, its buffers, and the write methods.
// lifecycle.go is the Pool (one live session per workspace) and the file Watcher.
package session
