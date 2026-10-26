package steam

import "github.com/ebitengine/purego"

// packBytes is the packing of the structs Steam's library fills in on Linux.
const packBytes = 4

// loadLibrary loads a shared library and returns its handle.
func loadLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// findSymbol returns the address of a function that a loaded shared library exports. It returns an error if the
// library does not export it.
func findSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
