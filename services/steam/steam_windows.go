package steam

import "syscall"

// packBytes is the packing of the structs Steam's library fills in on Windows.
const packBytes = 8

// loadLibrary loads a DLL and returns its handle.
func loadLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}
