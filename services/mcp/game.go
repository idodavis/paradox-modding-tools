// game.go defines the tools that reach past the editor: the game itself, its
// pictures and drawn files, the copies of a file, PMT's own fixes, and a mod's
// Steam page.

package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"maps"
	"os"
	"slices"
	"strconv"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/steam"
	"paradox-modding-tools/services/workspace"
)

// drawable is something a file draws: a window of a GUI file, or a coat of
// arms.
type drawable struct {
	Path  string            `json:"path" jsonschema:"the file's absolute path"`
	Name  string            `json:"name" jsonschema:"the window's or the coat's name, as list_symbols gives it"`
	Picks map[string]string `json:"picks,omitempty" jsonschema:"for a coat, the option each of its random lists is drawn at"`
}

// copying is a game definition or a whole game file to copy into a mod.
type copying struct {
	Path   string `json:"path" jsonschema:"the absolute path of the game's file, or another mod's"`
	Line   int    `json:"line,omitempty" jsonschema:"the line of the definition to copy; leave it out to copy the whole file"`
	Column int    `json:"column,omitempty" jsonschema:"the column of the definition's name; 1 if left out"`
	Mod    string `json:"mod" jsonschema:"the id of the mod the copy goes in"`
	Token  string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// fixing is a file whose problems PMT fixes, all listed or one made.
type fixing struct {
	Path  string `json:"path" jsonschema:"the file's absolute path"`
	Line  int    `json:"line,omitempty" jsonschema:"the line of the problem to fix; leave it out to list every fix the file has"`
	Token string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// formatting is a file to format.
type formatting struct {
	Path  string `json:"path" jsonschema:"the file's absolute path"`
	Token string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// listingKey names one of a mod's Steam pages.
type listingKey struct {
	Mod string `json:"mod" jsonschema:"the mod's id"`
	Key string `json:"key,omitempty" jsonschema:"the game version of an older page; leave it out for the mod's own"`
}

// listingTexts is the texts to write to one of a mod's Steam pages.
type listingTexts struct {
	listingKey
	Title       string `json:"title,omitempty" jsonschema:"the new title; leave it out to keep the title"`
	Description string `json:"description,omitempty" jsonschema:"the new description, in BBCode; leave it out to keep it"`
	Changelog   string `json:"changelog,omitempty" jsonschema:"the next change note, in BBCode; leave it out to keep it"`
	Token       string `json:"token,omitempty" jsonschema:"the preview's token, which makes the change"`
}

// listingNotes is one of a mod's Steam pages with the drafts of its next
// change note.
type listingNotes struct {
	steam.Listing
	ChangeNotes []steam.ChangeNote `json:"changeNotes"`
}

// fixRow is one quick fix a file has, with the line of its problem.
type fixRow struct {
	Line  int    `json:"line"`
	Title string `json:"title"`
	Code  string `json:"code"`
}

// imageSide is the longest side of a picture an agent is sent, in pixels.
// Claude scales a longer one down itself, so more would be sent for nothing.
const imageSide = 1568

func (s *Service) addGameTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "read_error_log", Title: "Read the game's error log", Annotations: readsOnly,
		Description: "Reads the game's error log from its last run, as Health lists it: each line as a finding, with " +
			"the file and line it names, when the game wrote the log, and the mods that run did not load. The log " +
			"is long, so give a query, such as a file's or an event's name, to read the lines that hold it."},
		answer(s.readErrorLog))
	sdk.AddTool(server, &sdk.Tool{Name: "launch_game", Title: "Start the game",
		Description: "Starts the game with its debug flag, as Steam starts it, and answers once it has started. " +
			"The game takes a while to load; read_error_log reads what it wrote after."},
		answer(func(struct{}) (string, error) {
			return "The game is starting. Its error log is written as it loads.", s.Workspace.Launch()
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "view_image", Title: "Look at a picture", Annotations: readsOnly,
		Description: "Shows a DDS, PNG or TGA file of the workspace or the game as a picture."}, s.viewImage)
	sdk.AddTool(server, &sdk.Tool{Name: "resolve_window", Title: "Read a GUI window as it is drawn",
		Annotations: readsOnly, Description: "Gives one window of a .gui file as PMT's Visual view draws it: its types " +
			"and templates applied, its texts looked up and its textures found."},
		answer(func(request drawable) (draw.Window, error) { return s.Draw.ResolveWindow(request.Path, request.Name) }))
	sdk.AddTool(server, &sdk.Tool{Name: "resolve_coat", Title: "Read a coat of arms as it is drawn",
		Annotations: readsOnly, Description: "Gives one coat of arms of a file as PMT's Visual view draws it: its " +
			"pattern, colors and emblems, layer by layer."},
		answer(func(request drawable) (draw.Coat, error) {
			return s.Draw.ResolveCoat(request.Path, request.Name, request.Picks)
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_copies", Title: "List a file's copies", Annotations: readsOnly,
		Description: "Lists every copy of a file in load order, the game's first: the game's file and each mod's at " +
			"the same path. The last one listed is the one the game keeps."},
		answer(func(request document) ([]workspace.Place, error) { return s.Workspace.ListCopies(request.Path), nil }))
	sdk.AddTool(server, &sdk.Tool{Name: "list_folder", Title: "List a folder", Annotations: readsOnly,
		Description: "Lists what a folder of the workspace or the game holds, folders first."},
		answer(func(request document) ([]workspace.Place, error) { return s.Workspace.ListFolder(request.Path) }))
	sdk.AddTool(server, &sdk.Tool{Name: "find_files", Title: "Find files by name", Annotations: readsOnly,
		Description: "Finds the files of the workspace and the game whose path holds the query's characters in " +
			"order, the best match first."},
		answer(func(request textQuery) ([]workspace.Place, error) {
			return s.Search.SearchFiles(request.Query, resolveLimit(request.Limit, defaultLimit)), nil
		}))
	sdk.AddTool(server, &sdk.Tool{Name: "list_dlc", Title: "List the game's DLC", Annotations: readsOnly,
		Description: "Lists the DLCs of the workspace's game install, each with the name has_dlc takes."},
		answer(func(struct{}) ([]steam.DLC, error) { return s.Steam.ListDLC() }))
}

func (s *Service) addChangeTools(server *sdk.Server) {
	sdk.AddTool(server, &sdk.Tool{Name: "override", Title: "Copy the game's definition into a mod",
		Description: "Copies a definition of the game, or a whole game file, into a mod so that the game keeps the " +
			"mod's copy. Without a token it answers with the change and a token; called again with the token, it " +
			"makes it."}, answer(s.override))
	sdk.AddTool(server, &sdk.Tool{Name: "fix", Title: "Apply PMT's quick fix",
		Description: "Lists the quick fixes PMT has for a file's problems, or with a line makes the fix of the " +
			"problem there. With a line and no token it answers with the edits and a token; called again with the " +
			"token, it makes them."}, answer(s.fix))
	sdk.AddTool(server, &sdk.Tool{Name: "format", Title: "Format a script file",
		Description: "Formats a script file as PMT's editor does, keeping its indentation. Without a token it " +
			"answers with the edits and a token; called again with the token, it makes them."}, answer(s.format))
	sdk.AddTool(server, &sdk.Tool{Name: "read_listing", Title: "Read a mod's Steam page", Annotations: readsOnly,
		Description: "Reads a mod's Steam Workshop page as PMT keeps it: its title, description and tags, and the " +
			"drafts of its next change note, from git and from the mod's changelog file."}, answer(s.readListing))
	sdk.AddTool(server, &sdk.Tool{Name: "write_listing", Title: "Write a mod's Steam page",
		Description: "Writes the title, the description or the next change note of a mod's Steam Workshop page, as " +
			"PMT keeps them in the mod's folder. Nothing is published: the user does that in PMT. Without a token " +
			"it answers with the texts and a token; called again with the token, it writes them."},
		answer(s.writeListing))
}

// readErrorLog returns the lines of the game's error log whose message holds
// the query, up to the limit. With no query it gives the log's first lines.
func (s *Service) readErrorLog(request textQuery) (health.GameLog, error) {
	gameLog, err := s.Health.ListErrorLog()
	gameLog.Findings = pickRows(gameLog.Findings, request.Query, request.Limit, func(finding health.Finding) string {
		return lsp.JoinParts(finding.Message)
	})

	return gameLog, err
}

// viewImage returns the file as a PNG whose longest side fits an agent's
// picture. A DDS file gives the largest of its own mip levels that fits. A
// picture with no level small enough, as a PNG or a TGA has none, is shrunk.
// It returns an error if the file is in neither the workspace nor the game, or
// is no picture PMT decodes.
func (s *Service) viewImage(_ context.Context, _ *sdk.CallToolRequest, request document) (
	*sdk.CallToolResult, any, error) {
	if _, err := s.locateFile(request.Path); err != nil {
		return nil, nil, err
	}

	file, err := os.ReadFile(request.Path)
	if err != nil {
		return nil, nil, err
	}

	header, err := texture.ReadHeader(request.Path)
	if err != nil {
		return nil, nil, err
	}

	// A DDS file's own smaller copies are taken first: the file's author
	// made them, and nothing is decoded only to be thrown away.
	level := 0
	for level+1 < header.Mipmaps && max(header.Width, header.Height)>>level > imageSide {
		level++
	}

	picture, err := texture.Decode(file, level)
	if err != nil {
		return nil, nil, err
	}

	if longest := max(picture.Bounds().Dx(), picture.Bounds().Dy()); longest > imageSide {
		picture = texture.Shrink(picture, float64(imageSide)/float64(longest))
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		return nil, nil, err
	}

	content := &sdk.ImageContent{Data: buffer.Bytes(), MIMEType: "image/png"}

	return &sdk.CallToolResult{Content: []sdk.Content{content}}, nil, nil
}

// override previews copying a game definition, or a whole file where the
// request has no line, into a mod, or makes the change a token names. It
// returns an error if the mod is not the user's, no definition is at the
// position, or the mod has the copy already.
func (s *Service) override(request copying) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	if err := s.checkMod(request.Mod); err != nil {
		return nil, err
	}

	if request.Line == 0 {
		return s.preview(nil, request, func() (any, error) { return s.Workspace.CreateCopy(request.Path, request.Mod) })
	}

	fix, err := s.LSP.BuildOverride(request.Path, request.Line, max(request.Column, 1), request.Mod)
	if err != nil || fix == nil {
		return nil, errors.Join(err, errors.New("no definition the game keeps one copy of is at this position"))
	}

	return s.previewFix(*fix)
}

// fix lists the quick fixes of a file's problems, or with a line previews the
// fix of the first problem there that has one, or makes the change a token
// names. It returns an error if no problem on the line has a fix.
func (s *Service) fix(request fixing) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	diagnostics, err := s.LSP.Diagnose(request.Path)
	if err != nil {
		return nil, err
	}

	if request.Line > 0 {
		diagnostics = slices.DeleteFunc(diagnostics, func(diagnostic lsp.Diagnostic) bool {
			return diagnostic.Range.StartLine != request.Line
		})
	}

	fixes, err := s.LSP.ListFixes(request.Path, diagnostics)
	if err != nil {
		return nil, err
	}

	if request.Line == 0 {
		rows := make([]fixRow, len(fixes))
		for position, fix := range fixes {
			rows[position] = fixRow{Title: fix.Title, Code: fix.Code, Line: findLine(diagnostics, fix.Code)}
		}

		return rows, nil
	}

	if len(fixes) == 0 {
		return nil, fmt.Errorf("PMT has no quick fix for a problem on line %d; diagnose lists the problems", request.Line)
	}

	return s.previewFix(fixes[0])
}

// findLine returns the line of the first diagnostic with the given code, 0 if
// none has it.
func findLine(diagnostics []lsp.Diagnostic, code string) int {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return diagnostic.Range.StartLine
		}
	}

	return 0
}

// previewFix keeps a quick fix under a token and returns it with the token.
// Made, the fix first creates the file it names, if any, then writes its
// edits.
func (s *Service) previewFix(fix lsp.Fix) (any, error) {
	return s.preview(slices.Collect(maps.Keys(fix.Edits)), fix, func() (any, error) {
		if fix.Creates != "" && !isFile(fix.Creates) {
			if err := s.Workspace.CreateFile(fix.Creates); err != nil {
				return nil, err
			}
		}

		return s.writeEdits(fix.Edits)
	})
}

// isFile reports whether a path is a file on disk.
func isFile(path string) bool {
	stat, err := os.Stat(path)

	return err == nil && !stat.IsDir()
}

// format previews formatting a script file with the editor's indent settings,
// or makes the change a token names. It returns an error if the file needs no
// change, is no script, or does not parse.
func (s *Service) format(request formatting) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	size, _ := s.Workspace.Config.LookupSetting("", "ide.indentation")
	unit, _ := s.Workspace.Config.LookupSetting("", "ide.indentWith")
	tabSize, _ := strconv.Atoi(size)

	edits, err := s.LSP.Format(request.Path, tabSize, unit == "spaces")
	if err != nil {
		return nil, err
	}

	if len(edits) == 0 {
		return nil, errors.New("the file needs no formatting, or is no script file PMT can parse")
	}

	change := map[string][]lsp.Edit{request.Path: edits}

	return s.preview([]string{request.Path}, change, func() (any, error) { return s.writeEdits(change) })
}

// readListing returns one of a mod's Steam pages and the drafts of its next
// change note. A failure to read the drafts, as where the mod has no git,
// leaves them out.
func (s *Service) readListing(request listingKey) (listingNotes, error) {
	listing, err := s.Steam.ReadListing(request.Mod, request.Key)
	if err != nil {
		return listingNotes{}, err
	}

	notes, _ := s.Steam.ListChangeNotes(request.Mod, request.Key)

	return listingNotes{Listing: listing, ChangeNotes: notes}, nil
}

// writeListing previews writing the texts the request gives to one of a
// mod's Steam pages, or makes the change a token names. It returns an error
// if the mod has no such page or the request gives no text.
func (s *Service) writeListing(request listingTexts) (any, error) {
	if request.Token != "" {
		return s.confirm(request.Token)
	}

	listing, err := s.Steam.ReadListing(request.Mod, request.Key)
	if err != nil {
		return nil, err
	}

	if request.Title == "" && request.Description == "" && request.Changelog == "" {
		return nil, errors.New("give a title, a description or a changelog to write")
	}

	if request.Title != "" {
		listing.Title = request.Title
	}

	if request.Description != "" {
		listing.Description = request.Description
	}

	if request.Changelog != "" {
		listing.Changelog = request.Changelog
	}

	return s.preview(nil, request, func() (any, error) {
		return "written", s.Steam.WriteListing(request.Mod, request.Key, listing)
	})
}
