//go:build windows

// steam_windows.go opens Steam's library with LoadLibrary, and lays out CreateItemResult_t as
// Windows packs it: Steam's callback structs are packed to 8 bytes here, 4 on Linux.

package steam

import "syscall"

func openLibrary(path string) (uintptr, func(), error) {
	handle, err := syscall.LoadLibrary(path)
	if err != nil {
		return 0, nil, err
	}
	return uintptr(handle), func() { _ = syscall.FreeLibrary(handle) }, nil
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}

// createItemResult is CreateItemResult_t at 8-byte packing: the file id starts at 8.
type createItemResult struct {
	Result    int32
	_         [4]byte
	FileID    [8]byte
	NeedLegal bool
	_         [7]byte
}
