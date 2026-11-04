// links.go finds the files that a line of a shell's output names, such as a compiler's file and line, so the page can
// turn each into a link.

package terminal

import (
	"path/filepath"
	"regexp"
	"strconv"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// A Link is a file name in a line of a shell's output, with the file's place in the workspace and the line it names.
type Link struct {
	Start int      `json:"start"` // where the name starts in the line, in UTF-16 code units
	End   int      `json:"end"`   // where the name and its line end, in UTF-16 code units
	Site  lsp.Site `json:"site"`  // the empty range at the column the line names; the zero range where it names none
}

// linkPattern matches a path with a file extension, followed by a line in one of the forms VS Code's terminal links,
// such as path:L:C, path(L,C) and path, line L. A path on a Windows drive may have spaces in its folders. The
// submatches after the path are the line and column of each form, in order.
var linkPattern = regexp.MustCompile(
	`([A-Za-z]:[\\/](?:[^\\/:*?"<>|\r\n]+[\\/])*[^\\/:*?"<>|\s]+\.\w+|[\\/]?(?:[^\s"'<>|:*?()\[\],]+[\\/])*` +
		`[^\s\\/"'<>|:*?()\[\],]+\.\w+)` +
		`(?::(\d+)(?:[:.](\d+))?|#(\d+)(?::(\d+))?|\((\d+)(?:,\s?(\d+))?\)|\[(\d+)(?:,\s?(\d+))?\]|` +
		`"?,? line (\d+)(?:,? col(?:umn)? (\d+))?)`)

// The limits VS Code's terminal puts on looking for links in a line of output.
const (
	maxLinkLine = 2000 // a longer line is no line of a listing
	maxLinks    = 10   // the names looked up in one line
)

// FindLinks returns the links in the given line of the output of the shell with the given id. A name under no root of
// the workspace is left out. It returns none for a line longer than a listing's, or if no shell has the id.
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
		link := Link{Start: lsp.CountUnits(text[:match[0]]), End: lsp.CountUnits(text[:match[1]]), Site: lsp.Site{Place: place}}
		// each form has a line group and a column group, four offsets; the first form whose line matched gives both
		for group := 4; group < len(match); group += 4 {
			if match[group] >= 0 {
				link.Site.Line, _ = strconv.Atoi(text[match[group]:match[group+1]])
				if match[group+2] >= 0 {
					column, _ := strconv.Atoi(text[match[group+2]:match[group+3]])
					link.Site.Range = lsp.Range{StartLine: link.Site.Line, StartColumn: column, EndLine: link.Site.Line, EndColumn: column}
				}
				break
			}
		}
		links = append(links, link)
	}
	return links
}

// resolveLink returns the place of the file a name in a shell's output means: a full path, a path from the shell's
// folder, or a path the workspace loads. It returns false if that file is under no root of the workspace.
func (trm *Service) resolveLink(shell *terminal.Shell, name string) (workspace.Place, bool) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(shell.Folder, name)
	}
	if files.IsFile(path) {
		place := trm.Workspace.Locate(path)
		return place, place.Root != ""
	}
	ws, err := trm.Workspace.Session()
	if err != nil {
		return workspace.Place{}, false
	}
	index := ws.Mods.Load()
	loaded, _, ok := index.FindLoaded(filepath.ToSlash(filepath.Clean(name)))
	if !ok {
		return workspace.Place{}, false
	}
	return ws.Locate(index, loaded.Path), true
}

// ResolveReference returns the text that names a file to an agent in a shell, as Claude Code and Copilot CLI read it:
// an @ and the file's path from the shell's folder, or its full path for a file outside it. It returns an error if no
// shell has the given id.
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
