package terminal

import (
	"path/filepath"
	"regexp"
	"strconv"
	"unicode/utf16"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/workspace"
)

// A Link is a file that a line of a shell's output names at a line, with where it sits in the workspace.
type Link struct {
	Start  int             `json:"start"` // where the name starts in the line, in UTF-16 code units
	End    int             `json:"end"`   // where the name and its line end, in UTF-16 code units
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"`
	Column int             `json:"column"` // 0 when the line names no column
}

// linkPattern matches a path with a file extension, followed by a line in one of the forms VS Code's terminal links:
// path:L, path:L:C, path:L.C, path#L, path#L:C, path(L), path(L,C), path[L, C], "path", line L and path, line L,
// column C. A path on a Windows drive may have spaces in its folders; any other has none. The submatches after the
// path are the line and column of each form, in that order.
var linkPattern = regexp.MustCompile(
	`([A-Za-z]:[\\/](?:[^\\/:*?"<>|\r\n]+[\\/])*[^\\/:*?"<>|\s]+\.\w+|[\\/]?(?:[^\s"'<>|:*?()\[\],]+[\\/])*` +
		`[^\s\\/"'<>|:*?()\[\],]+\.\w+)` +
		`(?::(\d+)(?:[:.](\d+))?|#(\d+)(?::(\d+))?|\((\d+)(?:,\s?(\d+))?\)|\[(\d+)(?:,\s?(\d+))?\]|` +
		`"?,? line (\d+)(?:,? col(?:umn)? (\d+))?)`)

// The limits on a line of output that links are looked for in: VS Code's own.
const (
	maxLinkLine = 2000 // a longer line is no line of a listing
	maxLinks    = 10   // the names looked up in one line
)

// FindLinks returns the files that the given line of a shell's output names at a line. A name resolves as a full
// path, then as a path from the shell's folder, then as a path the workspace loads. A name under no root of the
// workspace is left out. It returns none for a line longer than a listing's, or if no shell has the given id.
func (trm *Service) FindLinks(id, text string) []Link {
	shell, err := trm.findShell(id)
	if err != nil || len(text) > maxLinkLine {
		return nil
	}
	var links []Link
	for _, match := range linkPattern.FindAllStringSubmatchIndex(text, maxLinks) {
		place, ok := trm.resolveLink(shell, text[match[2]:match[3]])
		if !ok {
			continue
		}
		link := Link{Start: countUnits(text[:match[0]]), End: countUnits(text[:match[1]]), Place: place}
		// the first form whose line matched gives the line and its column
		for group := 4; group < len(match); group += 4 {
			if match[group] >= 0 {
				link.Line, _ = strconv.Atoi(text[match[group]:match[group+1]])
				if match[group+2] >= 0 {
					link.Column, _ = strconv.Atoi(text[match[group+2]:match[group+3]])
				}
				break
			}
		}
		links = append(links, link)
	}
	return links
}

// resolveLink returns the place of the file a name in a shell's output means: a full path, a path from the shell's
// folder, or a path the workspace loads. It returns false if the name means no file under a root of the workspace.
func (trm *Service) resolveLink(shell *terminal.Shell, name string) (workspace.Place, bool) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(shell.Folder, name)
	}
	if files.IsFile(path) {
		place := trm.Workspace.Locate(path)
		return place, place.Root != ""
	}
	session, err := trm.Workspace.Session()
	if err != nil {
		return workspace.Place{}, false
	}
	index := session.Mods.Load()
	loaded, _, ok := index.FindLoaded(filepath.ToSlash(filepath.Clean(name)))
	if !ok {
		return workspace.Place{}, false
	}
	return session.Locate(index, loaded.Path), true
}

// countUnits returns the length of a text in UTF-16 code units, the units the page counts a line's text in.
func countUnits(text string) int {
	units := 0
	for _, char := range text {
		units += utf16.RuneLen(char)
	}
	return units
}

// ResolveReference returns the text that names a file to an agent in a shell, as Claude Code and Copilot CLI read it:
// `@`, the file's path from the shell's folder with forward slashes, or its full path for a file outside that folder,
// and a space. It returns an error if no shell has the given id.
func (trm *Service) ResolveReference(id, path string) (string, error) {
	shell, err := trm.findShell(id)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(shell.Folder, path); err == nil && filepath.IsLocal(rel) {
		return "@" + filepath.ToSlash(rel) + " ", nil
	}
	return "@" + path + " ", nil
}
