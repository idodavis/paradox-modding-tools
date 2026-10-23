package steam

import "github.com/ebitengine/purego"

// packBytes is the packing of the structs Steam's library fills in on Linux.
const packBytes = 4

// loadLibrary loads a shared library and returns its handle.
func loadLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}
