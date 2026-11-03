// errorlog.go lists the rows of the game's error log and the mod each is about. It also watches the log while a
// workspace is open.

package health

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// identifierPattern matches a word in a log message that may be the name of a definition.
var identifierPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.:]*`)

// The ties of a log row to a mod, which the row's why fact holds: a file of the mod the row points at, or a definition
// the row names that only the mod defines.
const (
	tieFile       = "file"
	tieDefinition = "name"
)

// rowReasons holds the sentence that says why a log row is a mod's, by what ties it to the mod.
var rowReasons = map[string]string{
	tieFile:       "The line points at {file}, and the game loads that file from {mod}.",
	tieDefinition: "The line names {name}, and only {mod} defines it.",
}

// ListErrorLog lists the messages of the game's error log as findings. A message repeated at one place is one row with
// its count and the last one's time. It returns no findings if there is no log, and an error if the log cannot be
// read.
func (hlt *Service) ListErrorLog() (GameLog, error) {
	ws, err := hlt.Workspace.Session()
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
	// without a debug log, every mod counts as read
	read := lo.SliceToMap(*ws.Order.Load(), func(mod workspace.Mod) (string, bool) { return mod.ID, true })
	if folders, ok := readMountLog(ws); ok {
		read, gameLog.Copies, gameLog.Unloaded = sortRunMods(ws, folders)
	}
	mods := ws.Mods.Load()
	gameLog.Findings = foldFindings(parseRows(ws, mods, text.Text), func(row Finding) [3]string {
		site := lo.FirstOr(row.Sites, lsp.Site{})
		return [3]string{row.Facts["message"], site.Place.Path, strconv.Itoa(site.Line)}
	}, func(first *Finding, repeat Finding) { first.Facts["time"] = repeat.Facts["time"] })
	for position := range gameLog.Findings {
		resolveOrigin(mods, read, &gameLog.Findings[position])
		describeRow(ws, &gameLog.Findings[position])
	}
	return gameLog, nil
}

// readMountLog returns the folders that the run which wrote the error log read, from the game's debug log. It returns
// false if there is no debug log.
func readMountLog(ws *workspace.Session) ([]string, bool) {
	text, err := files.ReadText(files.Join(ws.Game.FindUserDataDir(ws.InstallPath), game.MountLog.Rel))
	if err != nil {
		return nil, false
	}
	var folders []string
	for line := range strings.Lines(text.Text) {
		if match := game.MountLog.Line.FindStringSubmatch(line); match != nil {
			folders = append(folders, match[1])
		}
	}
	return folders, true
}

// sortRunMods sorts the workspace's mods into those the game's last run read, read from another copy, or did not read.
func sortRunMods(ws *workspace.Session, folders []string) (read map[string]bool, copies map[string]string, unloaded []string) {
	read, copies = map[string]bool{}, map[string]string{}
	others := map[string]string{} // a folder of another copy, by the name its descriptor gives
	order := *ws.Order.Load()
	for _, folder := range folders {
		mod, ok := lo.Find(order, func(mod workspace.Mod) bool {
			return files.ToKey(mod.Dir) == files.ToKey(folder) || files.IsUnder(mod.Dir, folder)
		})
		if ok {
			read[mod.ID] = true
		} else if name := readCopyName(ws, folder); name != "" {
			others[name] = folder
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

// readCopyName returns the name that a mod's descriptor gives it, for a folder a run read or the mod folder that holds
// it as a stage folder. It returns "" if neither is a mod's folder.
func readCopyName(ws *workspace.Session, folder string) string {
	for _, dir := range []string{folder, filepath.Dir(folder)} {
		if descriptor, err := workspace.ReadDescriptor(ws.Game, dir); err == nil {
			return descriptor.Name
		}
	}
	return ""
}

// resolveErrorLog returns the path of the game's error log.
func resolveErrorLog(ws *workspace.Session) string {
	dir := ws.Game.FindUserDataDir(ws.InstallPath)
	return files.Join(dir, game.ErrorLog.Rel)
}

// parseRows returns a finding for each message in the error log's text, in order. A message that names a line of a
// mod's file points at it.
func parseRows(ws *workspace.Session, mods *index.Index, text string) []Finding {
	var findings []Finding
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if match := game.ErrorLog.Line.FindStringSubmatch(line); match != nil {
			findings = append(findings, makeFinding(mods, "game-error", map[string]string{
				"time": match[1] + ":" + match[2] + ":" + match[3], "source": match[5] + ":" + match[6], "message": match[7],
			}, nil))
		} else if len(findings) > 0 && strings.TrimSpace(line) != "" {
			findings[len(findings)-1].Facts["message"] += "\n" + line
		}
		// the location may be on a line after the message
		if last := len(findings) - 1; last >= 0 && len(findings[last].Sites) == 0 {
			if site, ok := findLogSite(ws, mods, ws.Game.ErrorLocations, line); ok {
				findings[last].Sites = []lsp.Site{site}
			}
		}
	}
	return findings
}

// findLogSite returns the line of a mod's file that a line of the log names, in the first of the given forms that
// names one. It returns false if the line names none, or only files of the game.
func findLogSite(ws *workspace.Session, mods *index.Index, locations []*regexp.Regexp, line string) (lsp.Site, bool) {
	for _, location := range locations {
		for _, match := range location.FindAllStringSubmatch(line, -1) {
			file, _, ok := mods.FindLoaded(match[location.SubexpIndex("path")])
			number, err := strconv.Atoi(match[location.SubexpIndex("line")])
			if ok && err == nil && file.Origin != game.OriginVanilla {
				return lsp.Site{Place: ws.Locate(mods, file.Path), Line: number, Range: lsp.ToLineRange(number)}, true
			}
		}
	}
	return lsp.Site{}, false
}

// resolveOrigin gives a log row to the mod it is about, by file or by name. Any other row is the game's.
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

// findLogOwner returns the first name in a message that only one mod defines, with that mod. It returns false if there
// is none.
func findLogOwner(mods *index.Index, message string) (identifier, origin string, ok bool) {
	for _, candidate := range lo.Uniq(identifierPattern.FindAllString(message, -1)) {
		candidate = strings.TrimRight(candidate, ".:")
		// the log is written in sentences, so a word of letters alone is never taken for a definition's name
		if !strings.ContainsAny(candidate, "_.:0123456789") {
			continue
		}
		for _, database := range mods.ListDatabases(candidate) {
			sites := listLoaded(mods, mods.Resolve(schema.Reference{Database: database, Name: candidate}))
			origins := lo.Uniq(lo.Map(sites, func(site index.Site, _ int) string { return mods.File(site).Origin }))
			if len(origins) == 1 && origins[0] != game.OriginVanilla {
				return candidate, origins[0], true
			}
		}
	}
	return "", "", false
}

// describeRow fills in a log row's words as describeFinding does, but for two. Its message is the line the game logged,
// its lines trimmed and joined by a dot, with the name or file that ties the row to a mod drawn apart where the line
// writes it. Its why says what ties the row to its mod.
func describeRow(ws *workspace.Session, row *Finding) {
	describeFinding(row)
	lines := lo.Map(strings.Split(row.Facts["message"], "\n"), func(line string, _ int) string {
		return strings.TrimSpace(line)
	})
	line, identifier := strings.Join(lo.Compact(lines), " · "), row.Facts["named"]
	row.Message = []lsp.Part{{Text: line}}
	if start := strings.Index(line, identifier); identifier != "" && start >= 0 {
		style := lo.Ternary(row.Facts["why"] == tieFile, "path", "code")
		row.Message = []lsp.Part{{Text: line[:start]}, {Text: identifier, Style: style}, {Text: line[start+len(identifier):]}}
	}
	row.Why = lsp.SplitMessage(rowReasons[row.Facts["why"]], map[string]string{
		"file": row.Facts["named"], "name": row.Facts["named"], "mod": ws.DescribeMod(row.Origin),
	})
}

// watchErrorLog calls a function each time the game's error log grows. It returns a function that ends the watch.
func watchErrorLog(ws *workspace.Session, change func()) (stop func()) {
	target := resolveErrorLog(ws)
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return func() {}
	}
	watched := ""
	// the log's folder may not exist yet, so the watch starts at the nearest folder above it
	follow := func() {
		nearest := findNearestFolder(filepath.Dir(target))
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
					// writes come in bursts, so the change is passed on once they stop
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

// findNearestFolder returns the nearest existing folder at or above a folder. It returns "" if none exists.
func findNearestFolder(dir string) string {
	for {
		if files.IsFolder(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
