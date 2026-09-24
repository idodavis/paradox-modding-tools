//go:build !windows

// steam_unix.go opens Steam's library with dlopen, and lays out CreateItemResult_t as Linux
// and macOS pack it: Steam's callback structs are packed to 4 bytes here and to 8 on Windows.

package steam

import "github.com/ebitengine/purego"

func openLibrary(path string) (uintptr, func(), error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW)
	if err != nil {
		return 0, nil, err
	}
	return handle, func() { _ = purego.Dlclose(handle) }, nil
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

// createItemResult is CreateItemResult_t at 4-byte packing: the file id starts at 4.
type createItemResult struct {
	Result    int32
	FileID    [8]byte
	NeedLegal bool
	_         [3]byte
}
