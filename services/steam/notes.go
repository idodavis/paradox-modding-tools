// notes.go drafts the change note of a listing's next upload, from the mod's git history and from its changelog file.

package steam

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/workspace"
)

// A ChangeNote is one draft of a change note.
type ChangeNote struct {
	Source string    `json:"source"` // git, or the name of the changelog file
	Text   string    `json:"text"`
	Count  int       `json:"count"` // the number of commits it is drafted from
	Since  time.Time `json:"since"` // the time of the upload it starts after, or zero for every commit
}

// noteGit is the source of a change note drafted from the mod's git history.
const noteGit = "git"

// changelogFiles are the names of the changelog file a mod's root may hold, in the order they are looked for.
var changelogFiles = []string{"CHANGELOG.md", "CHANGELOG.txt", "changelog.md", "changelog.txt"}

// heading matches a Markdown heading, with its marks and its text.
var heading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// ListChangeNotes returns drafts of the next change note, from git and from the changelog file. It returns an error
// if there is no such mod or listing, git fails, or a file cannot be read.
func (stm *Service) ListChangeNotes(modID, key string) ([]ChangeNote, error) {
	ws, mod, err := stm.findMod(modID)
	if err != nil {
		return nil, err
	}
	listing, err := stm.ReadListing(modID, key)
	if err != nil {
		return nil, err
	}
	// no commit is listed without git, outside a repository or in one with no commit
	commits, err := stm.Git.ListCommits(mod.Dir, 1, nil)
	if err != nil {
		return nil, err
	}
	notes := []ChangeNote{}
	if len(commits) > 0 {
		note, err := stm.buildGitNote(mod.Dir, listing.Version, stm.resolveSince(ws, listing.WorkshopID))
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	note, ok, err := findChangelogNote(mod.Dir, listing.Version)
	if ok {
		notes = append(notes, note)
	}
	return notes, err
}

// resolveSince returns when Steam last updated a Workshop item. It returns the zero time for no item, or if neither
// Steam nor PMT's copy knows.
func (stm *Service) resolveSince(ws *workspace.Session, workshopID string) time.Time {
	if workshopID == "" {
		return time.Time{}
	}
	if answer := stm.resolveAnswer(ws, workshopID); answer.Steam != nil {
		return answer.Steam.UpdatedAt
	}
	snapshot, _ := LoadSnapshot(stm.Workspace.Config.Dirs.Cache, workshopID)
	return snapshot.UpdatedAt
}

// buildGitNote drafts a change note from the subjects of the commits that changed a mod's folder since a time. A zero
// time takes every commit. It returns an error if git fails.
func (stm *Service) buildGitNote(dir, version string, since time.Time) (ChangeNote, error) {
	args := []string{"log", "--format=%s"}
	if !since.IsZero() {
		args = append(args, "--since="+since.Format(time.RFC3339))
	}
	stdout, err := stm.Git.Run(dir, append(args, "--", ".")...)
	if err != nil {
		return ChangeNote{}, err
	}
	subjects := lo.Compact(strings.Split(stdout, "\n"))
	lines := append([]string{"Version " + version}, lo.Map(subjects, func(subject string, _ int) string {
		return "- " + subject
	})...)
	return ChangeNote{Source: noteGit, Text: strings.Join(lines, "\n"), Count: len(subjects), Since: since}, nil
}

// findChangelogNote returns the section of a mod's changelog file for a version. It returns false if there is no such
// file or section, or the version is "". It returns an error if the file cannot be read.
func findChangelogNote(dir, version string) (ChangeNote, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil || version == "" {
		return ChangeNote{}, false, err
	}
	name, ok := lo.Find(changelogFiles, func(name string) bool {
		return slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == name })
	})
	if !ok {
		return ChangeNote{}, false, nil
	}
	text, err := files.ReadText(files.Join(dir, name))
	if err != nil {
		return ChangeNote{}, false, err
	}
	section, ok := findSection(text.Text, version)
	return ChangeNote{Source: name, Text: section}, ok, nil
}

// findSection returns the section of a Markdown text under the first heading that holds a version. Version 1.2 is in
// "## 1.2 - March" but not in "## 1.21". It returns false if no heading holds the version.
func findSection(text, version string) (string, bool) {
	// no digit or dot before the version, and none after it but a dot that ends the heading or a sentence
	whole := regexp.MustCompile(`(?:^|[^\d.])` + regexp.QuoteMeta(version) + `\.?(?:$|[^\d.])`)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start, end, level := -1, len(lines), 0
	for position, line := range lines {
		match := heading.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		if start < 0 && whole.MatchString(match[2]) {
			start, level = position, len(match[1])
		} else if start >= 0 && len(match[1]) <= level {
			end = position
			break
		}
	}
	if start < 0 {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[start+1:end], "\n")), true
}
