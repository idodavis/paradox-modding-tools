// health_localization.go is each language's gaps: the keys a mod writes in the workspace language and not in a
// language it also ships.

package pages

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// locFiles is what the workspace's loaded localization files write: the keys each language has, the files each
// origin ships in each language, and each entry written in the workspace's language with its file.
type locFiles struct {
	workspaceLanguage string
	languages         map[string]bool
	has               map[string]map[string]bool     // by language: the keys the workspace has
	filesIn           map[[2]string][]inventory.File // by origin and language: its loc files
	own               []locEntry                     // the entries in the workspace language
}

// locEntry is one localization entry and the file it is written in.
type locEntry struct {
	file  inventory.File
	entry index.At
}

// localization is each language's gaps: in a language a mod ships other than the workspace's, each key of its
// workspace-language files no workspace file in that language has, once, and the mod's file in that language it
// goes in.
func localization(ws *workspace.Session, sources *sourceIndex) []Language {
	loc := locFilesOf(ws)
	var out []Language
	for _, language := range slices.Sorted(maps.Keys(loc.languages)) {
		coverage := Language{Language: language}
		for _, own := range loc.own {
			key := own.entry.Occurrence().ID
			shipped := len(loc.filesIn[[2]string{own.file.Origin, language}]) > 0
			if language != loc.workspaceLanguage && shipped && !loc.has[language][key] {
				coverage.Untranslated = append(coverage.Untranslated, LocRow{
					Key: key, File: sources.index(own.file), Line: int(own.entry.Occurrence().Line),
					Target: loc.target(own.file, language, sources),
				})
			}
		}
		out = append(out, coverage)
	}
	return out
}

// locFilesOf reads the workspace's loaded files once for what localization they write.
func locFilesOf(ws *workspace.Session) locFiles {
	loc := locFiles{
		workspaceLanguage: strings.ToLower(ws.Language), languages: map[string]bool{},
		has: map[string]map[string]bool{}, filesIn: map[[2]string][]inventory.File{},
	}
	loc.languages[loc.workspaceLanguage] = true
	for _, file := range loadedFiles(ws) {
		fileLanguage := ""
		for _, entry := range ws.Workspace.InFile(file.Path) {
			occurrence := entry.Occurrence()
			if occurrence.Role != schema.Localizes {
				continue
			}
			language := entry.Language()
			loc.languages[language] = true
			fileLanguage = cmp.Or(fileLanguage, language)
			if loc.has[language] == nil {
				loc.has[language] = map[string]bool{}
			}
			loc.has[language][occurrence.ID] = true
			if language == loc.workspaceLanguage {
				loc.own = append(loc.own, locEntry{file: file, entry: entry})
			}
		}
		if fileLanguage != "" {
			key := [2]string{file.Origin, fileLanguage}
			loc.filesIn[key] = append(loc.filesIn[key], file)
		}
	}
	return loc
}

// target is where a key goes in another language: of the files its origin ships in it, the one named as the key's
// file is with the language put for the workspace's, else the first.
func (loc locFiles) target(file inventory.File, language string, sources *sourceIndex) int {
	candidates := loc.filesIn[[2]string{file.Origin, language}]
	want := strings.ReplaceAll(strings.ToLower(file.Rel), loc.workspaceLanguage, language)
	i := slices.IndexFunc(candidates, func(candidate inventory.File) bool { return strings.ToLower(candidate.Rel) == want })
	return sources.index(candidates[max(i, 0)])
}
