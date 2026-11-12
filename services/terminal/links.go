// links.go finds the files that a line of a shell's output names.

package terminal

import (
	"path/filepath"
	"regexp"
	"strconv"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/terminal"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Link is a file name in a line of a shell's output, with its place and line.
type Link struct {
	// Start and End are where the name and its line start and end, in UTF-16
	// code units.
	Start int `json:"start"`
	End   int `json:"end"`

	// Site is the empty range at the column the line names, or the zero range.
	Site lsp.Site `json:"site"`
}

// linkPattern matches a path with an extension followed by a line, as in
// path:L:C, path(L,C) or path, line L.
var linkPattern = regexp.MustCompile(
	`([A-Za-z]:[\\/](?:[^\\/:*?"<>|\r\n]+[\\/])*[^\\/:*?"<>|\s]+\.\w+|[\\/]?(?:[^\s"'<>|:*?()\[\],]+[\\/])*` +
		`[^\s\\/"'<>|:*?()\[\],]+\.\w+)` +
		`(?::(\d+)(?:[:.](\d+))?|#(\d+)(?::(\d+))?|\((\d+)(?:,\s?(\d+))?\)|\[(\d+)(?:,\s?(\d+))?\]|` +
		`"?,? line (\d+)(?:,? col(?:umn)? (\d+))?)`)

// The limits VS Code's terminal puts on links in a line of output.
const (
	maxLinkLine = 2000
	maxLinks    = 10
)

// FindLinks returns the links in a line of a shell's output. It returns none
// for an unknown shell or a long line.
func (s *Service) FindLinks(id, text string) []Link {
	shell, err := s.findShell(id)
	if err != nil || len(text) > maxLinkLine {
		return nil
	}

	var links []Link
	for _, match := range linkPattern.FindAllStringSubmatchIndex(text, maxLinks) {
		place, ok := s.resolveLink(shell, text[match[2]:match[3]])
		if !ok {
			continue
		}

		link := Link{Start: jomini.CountUnits(text[:match[0]]), End: jomini.CountUnits(text[:match[1]]), Site: lsp.Site{Place: place}}

		// Each form has four offsets, line then column. The first form with a
		// line gives both.
		for group := 4; group < len(match); group += 4 {
			if match[group] >= 0 {
				link.Site.Line, _ = strconv.Atoi(text[match[group]:match[group+1]])
				if match[group+2] >= 0 {
					column, _ := strconv.Atoi(text[match[group+2]:match[group+3]])
					link.Site.Range = jomini.Range{StartLine: link.Site.Line, StartColumn: column, EndLine: link.Site.Line, EndColumn: column}
				}

				break
			}
		}

		links = append(links, link)
	}

	return links
}

// resolveLink returns the place of the file a name means, or false if it is
// outside the workspace.
func (s *Service) resolveLink(shell *terminal.Shell, name string) (workspace.Place, bool) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(shell.Folder, name)
	}

	if files.IsFile(path) {
		place := s.Workspace.Locate(path)

		return place, place.Root != ""
	}

	ws, err := s.Workspace.Session()
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

// ResolveReference returns the text that names a file to an agent, such as
// "@src/a.txt ". It returns an error if no shell has the id.
func (s *Service) ResolveReference(id, path string) (string, error) {
	shell, err := s.findShell(id)
	if err != nil {
		return "", err
	}

	if rel, err := filepath.Rel(shell.Folder, path); err == nil && filepath.IsLocal(rel) {
		return "@" + filepath.ToSlash(rel) + " ", nil
	}

	return "@" + path + " ", nil
}
