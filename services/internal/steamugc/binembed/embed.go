// Package binembed embeds the pmt-steamugc helper and the GOOS Steam API lib.
package binembed

import (
	"embed"
	"runtime"
)

// Version is the Steamworks SDK redistributable pin (matches steam.stamp).
const Version = "165"

// HelperRev is bumped when pmt-steamugc behavior changes so the extract is refreshed.
const HelperRev = "5"

// Stamp is written next to the extracted helper.
func Stamp() string { return Version + "." + HelperRev }

//go:embed all:bin
var binFS embed.FS

// Helper returns the embedded pmt-steamugc payload, or nil until it is built.
func Helper() []byte {
	name := "bin/pmt-steamugc"
	if runtime.GOOS == "windows" {
		name = "bin/pmt-steamugc.exe"
	}
	b, err := binFS.ReadFile(name)
	if err != nil {
		return nil
	}
	return b
}

// APILib returns the embedded steam_api redistributable for this GOOS.
func APILib() []byte {
	b, err := binFS.ReadFile("bin/" + apiLibName())
	if err != nil {
		return nil
	}
	return b
}

// APILibName is the filename extracted next to the helper.
func APILibName() string { return apiLibName() }

func apiLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "steam_api64.dll"
	default:
		return "libsteam_api.so"
	}
}
