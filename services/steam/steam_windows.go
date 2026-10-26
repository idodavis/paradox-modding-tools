package steam

import "syscall"

// packBytes is the packing of the structs Steam's library fills in on Windows.
const packBytes = 8

// loadLibrary loads a DLL and returns its handle.
func loadLibrary(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}

// findSymbol returns the address of a function that a loaded DLL exports. It returns an error if the DLL does not
// export it.
func findSymbol(handle uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(handle), name)
}
