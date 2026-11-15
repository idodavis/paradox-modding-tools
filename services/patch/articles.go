// articles.go reads the written notes of a version's folder into articles.

package patch

import (
	"errors"
	"iter"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
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
// under a name that changed in 1.13, and EU5 writes them in discord.md.
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

	// renameLine is a line of a list of renames, as "- `A` -> `B`".
	renameLine = regexp.MustCompile("^[-*] `([^`]+)` -> `([^`]+)`$")
)

// renamed is the line that opens a list of renames.
const renamed = "**Renamed**"

// readNotes reads the notes of a version's folder into articles, one for each
// ## heading of a note file, and the key renames its Renamed lists give. A
// file in the folder under a name that is not in noteFiles is read as plain
// notes, unless the Co-ops generate it, as changes_script_docs.md. A file
// that cannot be read is left out, and the error says so.
func readNotes(source archive, folder string, language *lang.Schema) ([]workspace.Article, []workspace.KeyRename, error) {
	var skipped error
	listed := listNoteFiles(source.listFolder(folder))
	texts := make(map[string]string, len(listed))
	for _, note := range listed {
		text, err := source.read(folder + "/" + note.File)
		skipped = errors.Join(skipped, err)
		texts[note.File] = text
	}

	typeFolders := listTypeFolders(texts[discordFile])
	var articles []workspace.Article
	for _, note := range listed {
		for _, section := range splitSections(texts[note.File]) {
			if article, ok := makeArticle(section, note, typeFolders, language); ok {
				articles = append(articles, article)
			}
		}
	}

	return articles, listRenames(articles), skipped
}

// listNoteFiles returns the note files among the files of a folder: those of
// noteFiles in their order, then the plain ones in the folder's order.
func listNoteFiles(listing []string) []noteFile {
	var listed []noteFile
	for _, note := range noteFiles {
		if slices.Contains(listing, note.File) {
			listed = append(listed, note)
		}
	}

	for _, file := range listing {
		isKnown := slices.ContainsFunc(noteFiles, func(note noteFile) bool { return note.File == file })
		if strings.HasSuffix(file, ".md") && !strings.HasPrefix(file, "changes_") && !isKnown {
			listed = append(listed, noteFile{File: file})
		}
	}

	return listed
}

// listTypeFolders returns, by type doc, the folder discord.md lists it under:
// for "* [common/journal_entries](types/journal_entries.md)", the doc
// journal_entries gives common/journal_entries.
func listTypeFolders(discord string) map[string]string {
	folders := map[string]string{}
	for line := range strings.Lines(discord) {
		if match := typeLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			folders[match[2]] = match[1]
		}
	}

	return folders
}

// listLines yields each line of a markdown text without its line ending, and
// whether the line is in code: a fence of a code block or a line between two.
func listLines(markdown string) iter.Seq2[string, bool] {
	return func(yield func(string, bool) bool) {
		isCode := false
		for line := range strings.Lines(markdown) {
			line = strings.TrimRight(line, "\r\n")
			isFence := strings.HasPrefix(strings.TrimSpace(line), "```")
			if isFence {
				isCode = !isCode
			}

			if !yield(line, isCode || isFence) {
				return
			}
		}
	}
}

// splitSections splits a note file at its ## headings into articles that have
// a Title and the Markdown under it, trimmed. A line before the first heading
// is in no article, and a ## line in a code block is no heading.
func splitSections(markdown string) []workspace.Article {
	var articles []workspace.Article
	for line, isCode := range listLines(markdown) {
		title, isHeading := strings.CutPrefix(line, "## ")
		switch {
		case isHeading && !isCode:
			articles = append(articles, workspace.Article{Title: strings.TrimSpace(title)})
		case len(articles) > 0:
			articles[len(articles)-1].Markdown += line + "\n"
		}
	}

	for position := range articles {
		articles[position].Markdown = strings.TrimSpace(articles[position].Markdown)
	}

	return articles
}

// makeArticle fills the rest of an article that has a Title and its Markdown:
// the kind the note file gives it, and its folders and keys. It returns false
// for an article that is contents, as isLinks says.
func makeArticle(article workspace.Article, note noteFile, typeFolders map[string]string, language *lang.Schema) (workspace.Article, bool) {
	if isLinks(article.Markdown) {
		return article, false
	}

	prose := toProse(article.Markdown)
	article.Breaking, article.New = note.Breaking, note.New
	article.Folders, article.Keys = listFolders(prose, typeFolders, language), listMatches(backtickKey, prose)

	return article, true
}

// isLinks reports whether a markdown text is contents: it has links into the
// digest's own files, as a table of contents does, and every item of its lists
// is one, whatever else it says. A text with nothing in it is too.
func isLinks(markdown string) bool {
	hasLink := false
	for line := range strings.Lines(markdown) {
		line = strings.TrimSpace(line)
		isItem := strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- ")
		switch {
		case linkLine.MatchString(line):
			hasLink = true
		case isItem:
			return false
		}
	}

	return hasLink || strings.TrimSpace(markdown) == ""
}

// toProse returns the markdown without its code blocks, which hold script that
// names keys and links of its own.
func toProse(markdown string) string {
	var prose strings.Builder
	for line, isCode := range listLines(markdown) {
		if !isCode {
			prose.WriteString(line + "\n")
		}
	}

	return prose.String()
}

// listMatches returns, sorted and without repeats, what the first group of the
// pattern matches in a text.
func listMatches(pattern *regexp.Regexp, text string) []string {
	var found []string
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		found = append(found, match[1])
	}

	slices.Sort(found)

	return slices.Compact(found)
}

// listFolders returns the folders of the game's script that a text links the
// type docs of, sorted. A doc that no folder is known for is left out.
func listFolders(prose string, typeFolders map[string]string, language *lang.Schema) []string {
	var folders []string
	for _, doc := range listMatches(typeLink, prose) {
		if folder := resolveFolder(doc, typeFolders, language); folder != "" && !slices.Contains(folders, folder) {
			folders = append(folders, folder)
		}
	}

	slices.Sort(folders)

	return folders
}

// resolveFolder returns the folder of a type doc: the one discord.md lists it
// under, else the folder of the one database of the schema whose folder ends
// in the doc's name. It returns "" if there is none, or if several fit.
func resolveFolder(doc string, typeFolders map[string]string, language *lang.Schema) string {
	if folder, ok := typeFolders[doc]; ok {
		return folder
	}

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

// listRenames returns the keys the Renamed lists of the articles give a new
// key, each once. A list is the lines "- `A` -> `B`" after a line that reads
// **Renamed**, which a blank line does not end.
func listRenames(articles []workspace.Article) []workspace.KeyRename {
	var renames []workspace.KeyRename
	for _, article := range articles {
		isListed := false
		for line, isCode := range listLines(article.Markdown) {
			text := strings.TrimSpace(line)
			match := renameLine.FindStringSubmatch(text)
			switch {
			case isCode || text == "":
			case text == renamed:
				isListed = true
			case match != nil && isListed:
				rename := workspace.KeyRename{Key: match[1], Replacement: match[2]}
				if !slices.Contains(renames, rename) {
					renames = append(renames, rename)
				}
			default:
				isListed = false
			}
		}
	}

	return renames
}
