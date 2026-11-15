// articles.go reads the written notes of a version's folder into articles.

package patch

import (
	"errors"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/services/app"
)

// noteFile is one of the files in which the Co-ops write a version's notes,
// and what the articles of it are.
type noteFile struct {
	File string

	// Breaking and New say the articles are breaking changes or new features.
	// Both are false for plain notes.
	Breaking bool
	New      bool
}

// discordFile is the notes the Co-ops post to Discord. It lists the folder of
// each type doc.
const discordFile = "discord.md"

// noteFiles are the names the Co-ops have given the files of their notes.
// Vic3 writes breaking changes and features each in a file of their own,
// under a name that changed in 1.13, and EU5 writes them in discord.md,
// where breakingTitle finds the breaking ones.
var noteFiles = []noteFile{
	{File: "breaking_changes.md", Breaking: true},
	{File: "changes_breaking.md", Breaking: true},
	{File: "features.md", New: true},
	{File: "major_features.md", New: true},
	{File: "changes_features.md", New: true},
	{File: discordFile},
}

var (
	// linkLine is a line that only points into another file of the digest, as
	// "* [Effects](changes_script_docs.md#effects)" or "**Link:** [File
	// Changes](changes_files.md)" does.
	linkLine = regexp.MustCompile(`^(?:[*-] )?\[[^\]]*\]\((?:\./)?[^):]+\)$|^\*\*Link:\*\*`)

	// typeLine is a line of discord.md that lists the folder of a type doc, as
	// "* [common/journal_entries](types/journal_entries.md)".
	typeLine = regexp.MustCompile(`^[*-] \[([^\]]+)\]\((?:\./)?types/([^)#/]+)\.md\)`)

	// typeLink is a link to a type doc, as "(types/journal_entries.md)".
	typeLink = regexp.MustCompile(`\]\((?:\./)?types/([^)#/]+)\.md`)

	// backtickKey is a key in backticks, as `STATE_GALICH`.
	backtickKey = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.:]*)`")

	// breakingTitle is the heading the Co-ops write breaking changes under in
	// discord.md where a version has no file of them: 8 of EU5's 18 versions
	// and 5 of Vic3's 38. An article under it is breaking whatever its file.
	breakingTitle = regexp.MustCompile(`(?i)^breaking changes?$`)

	// renameLine is a line of a list of renames, as "- `A` -> `B`".
	renameLine = regexp.MustCompile("^[-*] `([^`]+)` -> `([^`]+)`$")
)

// renamed is the line that opens a list of renames.
const renamed = "**Renamed**"

// readNotes reads the notes of a version's folder into articles, one for each
// ## heading of a note file, and the key renames their Renamed lists give,
// each once. A file in the folder under a name that is not in noteFiles is
// read as plain notes, unless the Co-ops generate it, as
// changes_script_docs.md. A heading that is only contents is no article. A
// file that cannot be read is left out, and the error says so.
func readNotes(source archive, digests, folder string, language *lang.Schema) ([]patch.Article, []patch.KeyRename, error) {
	var skipped error
	notes := listNoteFiles(source.listFolder(folder))
	texts := make(map[string]string, len(notes))
	for _, note := range notes {
		text, err := source.read(folder + "/" + note.File)
		skipped = errors.Join(skipped, err)
		texts[note.File] = text
	}

	typeFolders := map[string]string{}
	for line := range strings.Lines(texts[discordFile]) {
		if match := typeLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			typeFolders[match[2]] = match[1]
		}
	}

	var articles []patch.Article
	var renames []patch.KeyRename
	for _, note := range notes {
		for _, section := range splitSections(texts[note.File]) {
			if isContents(section.prose) {
				continue
			}

			prose := strings.Join(section.prose, "\n")
			id := "article-" + strconv.Itoa(len(articles))
			heading := `<h2 id="` + id + `">` + html.EscapeString(section.title) + "</h2>"
			articles = append(articles, patch.Article{
				ID: id, Title: section.title, New: note.New,
				Breaking: note.Breaking || breakingTitle.MatchString(section.title),
				HTML:     heading + app.RenderMarkdown(section.markdown, makeLinkRule(digests, folder)),
				Folders:  listFolders(prose, typeFolders, language),
				Keys:     listMatches(backtickKey, prose),
			})

			for _, rename := range listRenames(section.prose) {
				if !slices.Contains(renames, rename) {
					renames = append(renames, rename)
				}
			}
		}
	}

	return articles, renames, skipped
}

// listNoteFiles returns the note files among the files of a folder: those of
// noteFiles in their order, then the plain ones in the folder's order.
func listNoteFiles(listing []string) []noteFile {
	var notes []noteFile
	for _, note := range noteFiles {
		if slices.Contains(listing, note.File) {
			notes = append(notes, note)
		}
	}

	for _, file := range listing {
		isKnown := slices.ContainsFunc(noteFiles, func(note noteFile) bool { return note.File == file })
		if strings.HasSuffix(file, ".md") && !strings.HasPrefix(file, "changes_") && !isKnown {
			notes = append(notes, noteFile{File: file})
		}
	}

	return notes
}

// generatedTabs are the files of a version's folder that the Co-ops generate,
// and the tab of the version's page that shows what each lists.
var generatedTabs = map[string]string{
	"changes_script_docs.md": "script", "changes_data_types.md": "data", "changes_files.md": "files",
}

// makeLinkRule returns the link rule of the notes in a version's folder of a
// digest, given as owner/name. A link inside the article and a web address
// stay as they are. A link to a file the Co-ops generate leads to the tab of
// the version's page that shows the same, as data-tab. Any other link into
// the digest leads to the file on GitHub.
func makeLinkRule(digests, folder string) app.LinkRule {
	return func(target string) []html.Attribute {
		isWeb := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
		file, _, _ := strings.Cut(strings.TrimPrefix(target, "./"), "#")
		tab, isGenerated := generatedTabs[file]
		switch {
		case isWeb, strings.HasPrefix(target, "#"):
		case isGenerated:
			return []html.Attribute{{Key: "data-tab", Val: tab}, {Key: "href", Val: "#"}}
		default:
			target = "https://github.com/" + digests + "/blob/HEAD/" + path.Join(folder, target)
		}

		return []html.Attribute{{Key: "href", Val: target}}
	}
}

// section is one ## heading of a note file with what is under it.
type section struct {
	// title is the heading, and markdown the lines under it, trimmed.
	title    string
	markdown string

	// prose holds the lines that are not in a code block, which holds script
	// with keys and links of its own.
	prose []string
}

// splitSections splits a note file at its ## headings. A line before the first
// heading is in no section, and a ## line in a code block is no heading.
func splitSections(markdown string) []section {
	var sections []section
	isCode := false
	for line := range strings.Lines(markdown) {
		line = strings.TrimRight(line, "\r\n")
		isFence := strings.HasPrefix(strings.TrimSpace(line), "```")
		if isFence {
			isCode = !isCode
		}

		title, isHeading := strings.CutPrefix(line, "## ")
		switch {
		case isHeading && !isCode && !isFence:
			sections = append(sections, section{title: strings.TrimSpace(title)})

		case len(sections) > 0:
			last := &sections[len(sections)-1]
			last.markdown += line + "\n"
			if !isCode && !isFence {
				last.prose = append(last.prose, line)
			}
		}
	}

	return sections
}

// isContents reports whether a section's prose is contents: it has links into
// the digest's own files, as a table of contents does, and every item of its
// lists is one, whatever else it says. A section with nothing in it is too.
func isContents(prose []string) bool {
	hasLink, hasText := false, false
	for _, line := range prose {
		line = strings.TrimSpace(line)
		isItem := strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- ")
		switch {
		case linkLine.MatchString(line):
			hasLink = true
		case isItem:
			return false
		}

		hasText = hasText || line != ""
	}

	return hasLink || !hasText
}

// listMatches returns, sorted and without repeats, what the first group of the
// pattern matches in a text.
func listMatches(pattern *regexp.Regexp, text string) []string {
	var matches []string
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		matches = append(matches, match[1])
	}

	slices.Sort(matches)

	return slices.Compact(matches)
}

// listFolders returns the folders of the game's script that a text links the
// type docs of, sorted. A doc's folder is the one discord.md lists it under,
// else the one findFolder gives. A doc with no folder is left out.
func listFolders(prose string, typeFolders map[string]string, language *lang.Schema) []string {
	var folders []string
	for _, doc := range listMatches(typeLink, prose) {
		folder := typeFolders[doc]
		if folder == "" {
			folder = findFolder(doc, language)
		}

		if folder != "" && !slices.Contains(folders, folder) {
			folders = append(folders, folder)
		}
	}

	slices.Sort(folders)

	return folders
}

// findFolder returns the folder of the one database of the schema whose
// folder ends in a type doc's name. It returns "" if none does, or several.
func findFolder(doc string, language *lang.Schema) string {
	var folders []string
	for _, database := range language.Databases {
		folder := database.Declaration.Folder
		if strings.HasSuffix(folder, "/"+doc) && !slices.Contains(folders, folder) {
			folders = append(folders, folder)
		}
	}

	if len(folders) != 1 {
		return ""
	}

	return folders[0]
}

// listRenames returns the keys the Renamed lists of a section's prose give a
// new key. A list is the lines "- `A` -> `B`" after a line that reads
// **Renamed**, which a blank line does not end.
func listRenames(prose []string) []patch.KeyRename {
	var renames []patch.KeyRename
	isListed := false
	for _, line := range prose {
		text := strings.TrimSpace(line)
		match := renameLine.FindStringSubmatch(text)
		switch {
		case text == "":
		case text == renamed:
			isListed = true
		case match != nil && isListed:
			renames = append(renames, patch.KeyRename{Key: match[1], Replacement: match[2]})
		default:
			isListed = false
		}
	}

	return renames
}
