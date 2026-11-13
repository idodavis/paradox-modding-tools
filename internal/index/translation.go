// translation.go adds the game's localization in a second language to an
// install's index, and finds the message the game shows for a key.

package index

import (
	"slices"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/lang"
)

// Translation is the game's localization files in one language other than
// the base one, as scanned on an install's index. It is kept in a cache of
// its own, so that a workspace in that language scans them once.
type Translation struct {
	Language string

	// Stamp is the Stamp of the install's index it was scanned on. Its
	// occurrences hold ordinals of that index's tables, which another scan
	// numbers in another order.
	Stamp int64

	// Added holds the texts the scan added to each of the index's tables, as
	// listTables orders them, in the order it added them.
	Added [4][]string

	// Positions, Occurrences and Texts hold, side by side, where Files lists
	// each file scanned, the file's occurrences and its messages' texts.
	Positions   []int32
	Occurrences [][]Occurrence
	Texts       [][]string
}

// ScanTranslation scans the game's localization files in the given language
// into an install's index, and returns what it added. It is for a language
// other than the base one, which Scan reads.
func (i *Index) ScanTranslation(schema *lang.Schema, language string) Translation {
	translation := Translation{Language: language, Stamp: i.Stamp}
	for position, file := range i.Files {
		if file.Format == files.FormatLocalization && findLanguage(file) == language {
			translation.Positions = append(translation.Positions, int32(position))
		}
	}

	tables := i.listTables()
	var counts [4]int
	for position, table := range tables {
		counts[position] = len(table.Texts)
	}

	i.scanFiles(schema, translation.Positions, func(int) {})
	i.buildPostings()

	for position, table := range tables {
		translation.Added[position] = slices.Clone(table.Texts[counts[position]:])
	}

	for _, position := range translation.Positions {
		translation.Occurrences = append(translation.Occurrences, i.Occurrences[position])
		translation.Texts = append(translation.Texts, i.Texts[position])
	}

	return translation
}

// Apply adds a translation loaded from its cache to an install's index. It
// returns false, and changes nothing, if the translation was scanned on
// another scan of the install.
func (i *Index) Apply(translation Translation) bool {
	if translation.Stamp != i.Stamp {
		return false
	}

	for position, table := range i.listTables() {
		for _, text := range translation.Added[position] {
			table.Intern(text)
		}
	}

	for order, position := range translation.Positions {
		i.Occurrences[position], i.Texts[position] = translation.Occurrences[order], translation.Texts[order]
	}

	i.buildPostings()

	return true
}

// FindText returns the message the game shows for a localization key, in
// the first of the given languages that has one. It returns false if no
// file the game reads has the key in any of them.
func (i *Index) FindText(key string, languages []string) (Winner, bool) {
	for _, language := range languages {
		if winner, ok := i.FindMessage(key, language); ok {
			return winner, true
		}
	}

	return Winner{}, false
}

// FindMessage returns the message the game shows for a localization key in a
// language: the one in a file the game reads, and of several, the one in the
// replace folder that ranks first. Two in folders of one rank leave the
// choice open, and the first is given as unsettled. It returns false if no
// file the game reads has the key in that language.
func (i *Index) FindMessage(key, language string) (Winner, bool) {
	unreplaced := len(i.dialect.LocReplaceFolders)
	var winner Winner
	best, tied := unreplaced+1, 0

	for _, site := range i.ListMessages(key) {
		if i.Language(site) != language || !i.IsLoaded(site.File) {
			continue
		}

		rank, replaces := i.dialect.FindLocReplacement(i.Files[site.File].StageRel(), language)
		if !replaces {
			rank = unreplaced
		}

		switch {
		case rank < best:
			winner.Site, best, tied = site, rank, 1
		case rank == best:
			tied++
		}
	}

	switch {
	case tied == 0:
		return Winner{}, false
	case tied > 1:
		winner.Reason = lang.ReasonUnsettled
	case best < unreplaced:
		winner.Reason = lang.ReasonReplaceFolder
	}

	return winner, true
}
