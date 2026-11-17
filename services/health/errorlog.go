// errorlog.go lists the rows of the game's error log and the mod each is
// about, and watches the log while a workspace is open.

package health

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The ties of a log row to a mod, held in the row's why fact: a line of the
// mod's file, or a name only the mod defines.
const (
	tieFile       = "file"
	tieDefinition = "name"
)

// errorLogRel is the error log's path in the user-data folder.
const errorLogRel = "logs/error.log"

// ListErrorLog lists the messages of the game's error log as findings. A
// message repeated at one place is one row, with its count and the last
// one's time. It returns no findings if there is no log, and an error if the
// log cannot be read.
func (s *Service) ListErrorLog() (GameLog, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return GameLog{}, err
	}

	gameLog := GameLog{Path: resolveErrorLog(ws)}
	text, err := files.ReadText(gameLog.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return gameLog, nil
	}

	if err != nil {
		return GameLog{}, err
	}

	if stat, err := os.Stat(gameLog.Path); err == nil {
		gameLog.WrittenAt = stat.ModTime()
	}

	// Without a debug log, every mod counts as read.
	read := map[string]bool{}
	for _, mod := range *ws.Order.Load() {
		read[mod.ID] = true
	}

	if folders, ok := readMountLog(ws); ok {
		read, gameLog.Copies, gameLog.Unloaded = sortRunMods(ws, folders)
	}

	mods := ws.Mods.Load()
	gameLog.Findings = foldFindings(parseRows(ws, mods, text.Text), func(row Finding) [3]string {
		var site lsp.Site
		if len(row.Sites) > 0 {
			site = row.Sites[0]
		}

		return [3]string{row.Facts["message"], site.Place.Path, strconv.Itoa(site.Line)}
	}, func(first *Finding, repeat Finding) { first.Facts["time"] = repeat.Facts["time"] })
	for position := range gameLog.Findings {
		resolveOrigin(mods, read, &gameLog.Findings[position])
		describeRow(ws, &gameLog.Findings[position])
	}

	return gameLog, nil
}

func resolveErrorLog(ws *workspace.Session) string {
	return files.Join(ws.Game.FindUserDataDir(ws.InstallPath), errorLogRel)
}

// mountLine is the debug log's line that names a folder the game reads.
var mountLine = regexp.MustCompile(`Mounted Data: (.+?)\s*$`)

// readMountLog returns the folders the run that wrote the error log read,
// from the game's debug log. It returns false if there is no debug log.
func readMountLog(ws *workspace.Session) ([]string, bool) {
	text, err := files.ReadText(files.Join(ws.Game.FindUserDataDir(ws.InstallPath), game.DebugLogRel))
	if err != nil {
		return nil, false
	}

	var folders []string
	for line := range strings.Lines(text.Text) {
		if match := mountLine.FindStringSubmatch(line); match != nil {
			folders = append(folders, match[1])
		}
	}

	return folders, true
}

// sortRunMods sorts the workspace's mods into those the game's last run
// read, read another copy of, or did not read.
func sortRunMods(ws *workspace.Session, folders []string) (read map[string]bool, copies map[string]string, unloaded []string) {
	read, copies = map[string]bool{}, map[string]string{}

	// others holds the folder of another copy, by the name its descriptor
	// gives.
	others := map[string]string{}
	order := *ws.Order.Load()
	for _, folder := range folders {
		position := slices.IndexFunc(order, func(mod workspace.Mod) bool {
			return files.ToKey(mod.Dir) == files.ToKey(folder) || files.IsUnder(mod.Dir, folder)
		})
		if position >= 0 {
			read[order[position].ID] = true
		} else if title := readCopyTitle(ws, folder); title != "" {
			others[title] = folder
		}
	}

	for _, mod := range order {
		if folder, ok := others[mod.Descriptor.Name]; ok && !read[mod.ID] {
			copies[mod.ID] = folder
		} else if !read[mod.ID] {
			unloaded = append(unloaded, mod.ID)
		}
	}

	return read, copies, unloaded
}

// readCopyTitle returns the name a mod's descriptor gives, for a folder the
// run read or the mod folder above a stage folder. It returns "" if neither
// is a mod's folder.
func readCopyTitle(ws *workspace.Session, folder string) string {
	for _, dir := range []string{folder, filepath.Dir(folder)} {
		if descriptor, err := workspace.ReadDescriptor(ws.Game, dir); err == nil {
			return descriptor.Name
		}
	}

	return ""
}

// errorLine is how each line of the error log starts: the time, then the
// source file and line that wrote it.
var errorLine = regexp.MustCompile(`^\[(\d\d):(\d\d):(\d\d)\](?:\[([A-Z])\])?\[([A-Za-z0-9_]+\.(?:cpp|h|hpp|inl)):(\d+)\]: ?(.*)$`)

// The forms in which an error log names a line of a script file, with the
// groups path and line. None matches a bare path and line: every line's
// header holds its source as [file.cpp:N], so a path must follow a space.
var (
	// fileLine is CK3's "file: P line: N".
	fileLine = regexp.MustCompile(`file: *(?P<path>\S+) line: *(?P<line>\d+)`)

	// quotedLine is the statement's line that a quoted message names, as in
	// "Unknown effect: x, near line: 3501" in file: "P" near line: 3502,
	// where the last line is where the reader stopped.
	quotedLine = regexp.MustCompile(`near line: (?P<line>\d+)" in file: "(?P<path>[^"]+)"`)

	// nearLine is the reader's in file: "P" near line: N.
	nearLine = regexp.MustCompile(`in file: "(?P<path>[^"]+)" near line: (?P<line>\d+)`)

	// pathLine is P:N after other text, as in "Script location: P:N".
	pathLine = regexp.MustCompile(`(?:^|\s)(?P<path>[^\s\[\]:'"]+\.\w+):(?P<line>\d+)\b`)

	// lineColumn is EU5's "line N and column C in P".
	lineColumn = regexp.MustCompile(`line (?P<line>\d+) and column \d+ in (?P<path>\S+)`)
)

// errorLocations holds the forms each game's error log names a line in, in
// the order they are tried.
var errorLocations = map[string][]*regexp.Regexp{
	game.CK3:  {fileLine, quotedLine, nearLine, pathLine},
	game.Vic3: {quotedLine, nearLine, pathLine},
	game.EU5:  {quotedLine, nearLine, pathLine, lineColumn},
}

// parseRows returns a finding for each message in the error log, in order.
// A message that names a line of a mod's file points at it.
func parseRows(ws *workspace.Session, mods *index.Index, text string) []Finding {
	var findings []Finding
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if match := errorLine.FindStringSubmatch(line); match != nil {
			findings = append(findings, makeFinding(mods, "game-error", map[string]string{
				"time": match[1] + ":" + match[2] + ":" + match[3], "source": match[5] + ":" + match[6], "message": match[7],
			}, nil))
		} else if len(findings) > 0 && strings.TrimSpace(line) != "" {
			findings[len(findings)-1].Facts["message"] += "\n" + line
		}

		// The location may be on a line after the message.
		if last := len(findings) - 1; last >= 0 && len(findings[last].Sites) == 0 {
			if site, ok := findLogSite(ws, mods, errorLocations[ws.Game.ID], line); ok {
				findings[last].Sites = []lsp.Site{site}
			}
		}
	}

	return findings
}

// findLogSite returns the line of a mod's file that a log line names. It
// returns false if it names none, or only the game's files.
func findLogSite(ws *workspace.Session, mods *index.Index, locations []*regexp.Regexp, line string) (lsp.Site, bool) {
	for _, location := range locations {
		for _, match := range location.FindAllStringSubmatch(line, -1) {
			file, _, ok := mods.FindLoaded(match[location.SubexpIndex("path")])
			number, err := strconv.Atoi(match[location.SubexpIndex("line")])
			if ok && err == nil && file.Origin != files.OriginVanilla {
				return lsp.Site{Place: ws.Locate(mods, file.Path), Line: number, Range: jomini.ToLineRange(number)}, true
			}
		}
	}

	return lsp.Site{}, false
}

// resolveOrigin gives a log row to the mod it is about, by file or by name,
// where the last run read that mod. Any other row is the game's.
func resolveOrigin(mods *index.Index, read map[string]bool, row *Finding) {
	if len(row.Sites) > 0 && !read[row.Sites[0].Place.Origin] {
		row.Sites = nil
	}

	if len(row.Sites) > 0 {
		row.Origin, row.Facts["why"], row.Facts["named"] = row.Sites[0].Place.Origin, tieFile, row.Sites[0].Place.Rel
	} else if identifier, origin, ok := findLogOwner(mods, row.Facts["message"]); ok && read[origin] {
		row.Origin, row.Facts["why"], row.Facts["named"] = origin, tieDefinition, identifier
	}

	if row.Origin != "" {
		row.Mods = []string{row.Origin}
	}
}

// identifierPattern matches what may be a definition's name in a log
// message.
var identifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.:]*`)

// findLogOwner returns the first identifier in a message that only one mod
// defines, with that mod. It returns false if there is none.
func findLogOwner(mods *index.Index, message string) (identifier, origin string, ok bool) {
	for _, candidate := range identifierPattern.FindAllString(message, -1) {
		candidate = strings.TrimRight(candidate, ".:")

		// The log is written in sentences, so a plain word is never taken
		// for a name.
		if !strings.ContainsAny(candidate, "_.:0123456789") {
			continue
		}

		for _, database := range mods.ListDatabases(candidate) {
			sites := listLoaded(mods, mods.Resolve(lang.Reference{Database: database, Name: candidate}))
			if origins := listOrigins(mods, sites); len(origins) == 1 && origins[0] != files.OriginVanilla {
				return candidate, origins[0], true
			}
		}
	}

	return "", "", false
}

// rowReasons holds the sentence that says why a log row is a mod's, by its
// tie.
var rowReasons = map[string]string{
	tieFile:       "The line points at {file}, and the game loads that file from {mod}.",
	tieDefinition: "The line names {name}, and only {mod} defines it.",
}

// describeRow words a log row as describeFinding does, but its message is
// the log's lines with the name or file that ties it to a mod set apart, and
// its why says what ties it.
func describeRow(ws *workspace.Session, row *Finding) {
	describeFinding(row)

	var lines []string
	for line := range strings.SplitSeq(row.Facts["message"], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	line, identifier := strings.Join(lines, " · "), row.Facts["named"]
	row.Message = []lsp.Part{{Text: line}}
	if start := strings.Index(line, identifier); identifier != "" && start >= 0 {
		style := "code"
		if row.Facts["why"] == tieFile {
			style = "path"
		}

		row.Message = []lsp.Part{{Text: line[:start]}, {Text: identifier, Style: style}, {Text: line[start+len(identifier):]}}
	}

	row.Why = lsp.SplitMessage(rowReasons[row.Facts["why"]], map[string]string{
		"file": identifier, "name": identifier, "mod": ws.DescribeMod(row.Origin),
	})
}

// watchErrorLog calls the given function once each burst of writes to the
// game's error log ends. It returns a function that ends the watch.
func watchErrorLog(ws *workspace.Session, change func()) (stop func()) {
	target := resolveErrorLog(ws)
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}

	// The log's folder may not exist yet, so the watch starts at the nearest
	// folder above it that does.
	watched := ""
	follow := func() {
		nearest := filepath.Dir(target)
		for !files.IsFolder(nearest) && filepath.Dir(nearest) != nearest {
			nearest = filepath.Dir(nearest)
		}

		if nearest == watched {
			return
		}

		_ = notifier.Remove(watched)
		watched = nearest
		_ = notifier.Add(nearest)
	}
	follow()

	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		defer close(done)
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		for {
			select {
			case event, ok := <-notifier.Events:
				if !ok {
					return
				}

				if event.Has(fsnotify.Create | fsnotify.Remove | fsnotify.Rename) {
					follow()
				}

				if event.Has(fsnotify.Write|fsnotify.Create) && files.ToKey(event.Name) == files.ToKey(target) {
					if timer != nil {
						timer.Stop()
					}

					timer = time.AfterFunc(200*time.Millisecond, change)
				}

			case _, ok := <-notifier.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return func() {
		_ = notifier.Close()
		<-done
	}
}
