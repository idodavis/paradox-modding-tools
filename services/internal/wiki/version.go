package wiki

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	verRe        = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+|[Xx]))?`)
	patchTitleRe = regexp.MustCompile(`(?i)^Patch\s+(\d+\.\d+(?:\.(?:\d+|[Xx]))?)`)
)

// ParseMajorMinor returns the first two numeric components of a game version.
// "latest", empty, and unparseable strings return ok=false.
func ParseMajorMinor(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "latest") {
		return 0, 0, false
	}
	m := verRe.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(m[1])
	min, err2 := strconv.Atoi(m[2])
	return maj, min, err1 == nil && err2 == nil
}

// Needed reports whether a scan at scanVersion should hit the wiki.
// Missing sidecar → true. Unparseable version → false when a sidecar exists.
// Hotfix (same major.minor) → false. Newer major/minor → true. Older → false.
func stale(f *Sidecar, maj, min int) bool {
	if f == nil {
		return true
	}
	if maj > f.FetchedMajor {
		return true
	}
	return maj == f.FetchedMajor && min > f.FetchedMinor
}

// Needed reports whether a scan at scanVersion should hit the wiki.
// Missing either sidecar → true. Unparseable version → false when both exist.
// Hotfix (same major.minor) → false. Newer major/minor on either sidecar → true.
func Needed(gameID, scanVersion string) bool {
	guides, gerr := LoadGuides(gameID)
	patches, perr := LoadPatches(gameID)
	if gerr != nil || perr != nil {
		return true
	}
	maj, min, ok := ParseMajorMinor(scanVersion)
	if !ok {
		return false
	}
	return stale(guides, maj, min) || stale(patches, maj, min)
}

// patchVer is a parsed "Patch 1.16.2" / "Patch 1.16.X" title.
type patchVer struct {
	Major, Minor, Patch int
	X                   bool
	Label               string
}

func parsePatchTitle(title string) (patchVer, bool) {
	m := patchTitleRe.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return patchVer{}, false
	}
	label := m[1]
	parts := strings.Split(label, ".")
	if len(parts) < 2 {
		return patchVer{}, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return patchVer{}, false
	}
	pv := patchVer{Major: maj, Minor: min, Label: label}
	if len(parts) >= 3 {
		if strings.EqualFold(parts[2], "x") {
			pv.X = true
			pv.Patch = 0
		} else if n, err := strconv.Atoi(parts[2]); err == nil {
			pv.Patch = n
		}
	}
	return pv, true
}

func cmpPatch(a, b patchVer) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	return a.Patch - b.Patch
}
