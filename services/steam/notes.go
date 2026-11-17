// notes.go drafts a listing's next change note.

package steam

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/services/workspace"
)

// ChangeNote is one draft of the note an upload sends.
type ChangeNote struct {
	// Source is "git", or the name of the changelog file.
	Source string `json:"source"`

	Text string `json:"text"`

	// Count is the number of commits the draft is made from.
	Count int `json:"count"`

	// Since is the time of the upload the draft starts after, or zero for every
	// commit.
	Since time.Time `json:"since"`
}

const noteGit = "git"

var changelogFiles = []string{"CHANGELOG.md", "CHANGELOG.txt", "changelog.md", "changelog.txt"}

var heading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// ListChangeNotes returns the drafts of the next change note, from git and from
// the changelog file. It returns an error if git fails or a file is unreadable.
func (s *Service) ListChangeNotes(modID, key string) ([]ChangeNote, error) {
	ws, mod, listing, err := s.findListing(modID, key)
	if err != nil {
		return nil, err
	}

	// Git lists no commit outside a repository or in an empty one.
	commits, err := s.Git.ListCommits(mod.Dir, 1, nil)
	if err != nil {
		return nil, err
	}

	notes := []ChangeNote{}
	if len(commits) > 0 {
		cutoff := s.resolveSince(ws, listing.WorkshopID)
		subjects, err := s.Git.ListSubjects(mod.Dir, cutoff)
		if err != nil {
			return nil, err
		}

		lines := []string{"Version " + listing.Version}
		for _, subject := range subjects {
			lines = append(lines, "- "+subject)
		}

		notes = append(notes, ChangeNote{Source: noteGit, Text: strings.Join(lines, "\n"), Count: len(subjects), Since: cutoff})
	}

	note, ok, err := findChangelogNote(mod.Dir, listing.Version)
	if ok {
		notes = append(notes, note)
	}

	return notes, err
}

// resolveSince returns when Steam last updated a Workshop item, or the zero
// time if neither Steam nor PMT's copy knows.
func (s *Service) resolveSince(ws *workspace.Session, workshopID string) time.Time {
	if workshopID == "" {
		return time.Time{}
	}

	if answer := s.resolveAnswer(ws, workshopID); answer.Steam != nil {
		return answer.Steam.UpdatedAt
	}

	snapshot, _ := lookupSnapshot(s.Workspace.Config.Dirs.Cache, workshopID)

	return snapshot.UpdatedAt
}

// findChangelogNote returns the section of a mod's changelog file for a
// version, or false if there is none.
func findChangelogNote(dir, version string) (ChangeNote, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil || version == "" {
		return ChangeNote{}, false, err
	}

	index := slices.IndexFunc(changelogFiles, func(candidate string) bool {
		return slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == candidate })
	})
	if index < 0 {
		return ChangeNote{}, false, nil
	}

	file := changelogFiles[index]
	text, err := files.ReadText(files.Join(dir, file))
	if err != nil {
		return ChangeNote{}, false, err
	}

	section, ok := findSection(text.Text, version)

	return ChangeNote{Source: file, Text: section}, ok, nil
}

// findSection returns the section under the first Markdown heading that holds a
// version. Version 1.2 is in "## 1.2 - March" but not in "## 1.21".
func findSection(text, version string) (string, bool) {
	// A digit or dot beside the version makes another version.
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
