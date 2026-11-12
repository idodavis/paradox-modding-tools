// mounts.go decides which of the files of a list of roots the game reads at
// each path, and in what order it loads them.

package files

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
)

// Mounts is the files of a list of roots in load order, and which one the
// game reads at each path. A file keeps its position in Files for the life of
// the mounts and of every copy Clone and Narrow make, so a position names one
// file in each.
type Mounts struct {
	// Roots are the roots in load order.
	Roots []Root

	// Files are the files of the roots, root by root. A file Unlist dropped
	// keeps its position, and IsListed tells it apart.
	Files []File

	// byKey holds the position in Files of each listed file, by its key.
	byKey map[string]int32

	// groups holds the positions of the files that share a path from a stage,
	// in load order, by the group key toGroupKey gives that path.
	groups map[string][]int32

	// placements holds where the game mounts each file, as Files lists them.
	placements []placement

	// rootKeys holds the key of each root's folder, ending in a slash, as
	// Roots lists them.
	rootKeys []string

	// origins holds the ids of the mods Narrow kept. It is nil for mounts
	// that answer for every mod.
	origins map[string]struct{}

	// gameFolders holds the first folder of each of the game's own files,
	// from the file's stage and in lower case.
	gameFolders map[string]struct{}
}

// placement is where the game mounts one file.
type placement struct {
	// root is the position in Roots of the file's root.
	root int

	// stage is the rank of the file's stage: 0 for none, then 1 and up in the
	// order the root mounts its stages.
	stage int

	// key is the key of the file's group: its path from its stage, in lower
	// case.
	key string

	// unmounted is set for a file the game never mounts.
	unmounted bool
}

// Mount lists the files under the roots, root by root in the order given, and
// returns their mounts. It is Mounts.Mount on empty mounts.
func Mount(roots []Root) (*Mounts, error) {
	return new(Mounts).Mount(roots)
}

// Mount lists the files under the given roots, root by root in the order
// given, and returns new mounts of these mounts' roots and files followed by
// the new ones, so a file's position means the same in both. A file below a
// given root inside another given one is listed once, under the inner root.
// It returns an error if a root cannot be walked.
func (m *Mounts) Mount(roots []Root) (*Mounts, error) {
	keys := listRootKeys(roots)
	var list []File

	for position, root := range roots {
		walked, err := Walk(root)
		if err != nil {
			return nil, err
		}

		for _, file := range walked {
			if owner, ok := findRoot(keys, file.Key); ok && owner == position {
				list = append(list, file)
			}
		}
	}

	return NewMounts(slices.Concat(m.Roots, roots), slices.Concat(m.Files, list)), nil
}

// listRootKeys returns the key of each root's folder, ending in the slash
// that opens the paths below it.
func listRootKeys(roots []Root) []string {
	keys := make([]string, len(roots))
	for position, root := range roots {
		keys[position] = ToKey(root.Dir) + "/"
	}

	return keys
}

// findRoot returns the position of the innermost root that holds a file,
// given the keys of the roots' folders and the file's key. It returns false
// if no root holds the file.
func findRoot(rootKeys []string, key string) (int, bool) {
	position, length, ok := 0, 0, false

	for candidate, rootKey := range rootKeys {
		if len(rootKey) > length && strings.HasPrefix(key, rootKey) {
			position, length, ok = candidate, len(rootKey), true
		}
	}

	return position, ok
}

// NewMounts returns the mounts of the given roots and of their files, listed
// root by root as Mount lists them. It builds every lookup from the two,
// so it also makes the mounts again from the roots and files a gob cache
// keeps.
func NewMounts(roots []Root, list []File) *Mounts {
	mounts := &Mounts{
		Roots: roots, Files: list, rootKeys: listRootKeys(roots),
		byKey: make(map[string]int32, len(list)), groups: make(map[string][]int32, len(list)),
		placements: make([]placement, len(list)), gameFolders: map[string]struct{}{},
	}

	for position, file := range list {
		mounts.byKey[file.Key] = int32(position)
		mounts.buildGroup(int32(position))

		// a file listed from a root that holds a root of its own, as the
		// game's folder holds each DLC's, has its path from the outer one and
		// names no folder of the game's
		place := mounts.placements[position]
		folder, _, nested := strings.Cut(place.key, "/")
		inner := mounts.rootKeys[place.root]+strings.ToLower(file.Rel) == file.Key
		if nested && inner && file.Origin == OriginVanilla {
			mounts.gameFolders[folder] = struct{}{}
		}
	}

	return mounts
}

// buildGroup works out where the game mounts the file at a position, and adds
// the file to the group of files at its path, in load order. The group is
// replaced, not changed, since a clone may still read it.
func (m *Mounts) buildGroup(position int32) {
	file := m.Files[position]
	root, _ := findRoot(m.rootKeys, file.Key)
	stages := m.Roots[root].Stages

	place := placement{root: root, key: strings.ToLower(file.StageRel()), unmounted: isUnmounted(file, stages)}
	if file.Stage != "" {
		place.stage = 1 + slices.IndexFunc(stages, func(stage string) bool { return strings.EqualFold(stage, file.Stage) })
	}

	m.placements[position] = place

	group := m.groups[place.key]
	insertion, _ := slices.BinarySearchFunc(group, position, m.compareLoaded)
	m.groups[place.key] = slices.Concat(group[:insertion], []int32{position}, group[insertion:])
}

// isUnmounted reports whether the game never mounts a file: a mod's file
// outside every stage folder, below a root that has stage folders (EU5
// debug.log:80-83, 5392; the wiki's Mod structure, l.46-48).
func isUnmounted(file File, stages []string) bool {
	return len(stages) > 0 && file.Origin != OriginVanilla && file.Stage == ""
}

// compareLoaded orders two files, given by their positions in Files, the way
// the game loads them: by their roots in load order, then by their stages,
// then as listed.
func (m *Mounts) compareLoaded(first, second int32) int {
	firstPlace, secondPlace := m.placements[first], m.placements[second]

	return cmp.Or(cmp.Compare(firstPlace.root, secondPlace.root), cmp.Compare(firstPlace.stage, secondPlace.stage),
		cmp.Compare(first, second))
}

// FindFile returns the position in Files of the listed file with the given
// path. It returns false if no listed file has that path.
func (m *Mounts) FindFile(path string) (int32, bool) {
	position, ok := m.byKey[ToKey(path)]

	return position, ok
}

// IsListed reports whether the file at a position in Files is still listed.
// It is not once Unlist has dropped it, as for a file deleted since the scan.
func (m *Mounts) IsListed(position int32) bool {
	listed, ok := m.byKey[m.Files[position].Key]

	return ok && listed == position
}

// FindRoot returns the innermost root that holds a path, since a DLC's folder
// is a root inside the game's. It returns false if no root holds the path.
func (m *Mounts) FindRoot(path string) (Root, bool) {
	position, ok := findRoot(m.rootKeys, ToKey(path)+"/")
	if !ok {
		return Root{}, false
	}

	return m.Roots[position], true
}

// Hiding is why the game does not read a file. The zero Hiding is a file
// nothing hides.
type Hiding string

// The ways a file is hidden from the game.
const (
	// HidingShadowed is a file at the same path later in the load order,
	// which the game reads in its place.
	HidingShadowed Hiding = "shadowed"

	// HidingDropped is a mod's replace_path that names the file's folder.
	HidingDropped Hiding = "dropped"

	// HidingUnmounted is a file the game never mounts.
	HidingUnmounted Hiding = "unmounted"
)

// FindLoaded returns the file the game reads at a path from a root, and the
// files it hides, the ones a replace_path drops among them. It returns false
// if the game reads none.
func (m *Mounts) FindLoaded(rel string) (loaded File, hidden []File, ok bool) {
	key := m.toGroupKey(rel)
	winner, ok := m.lookupLoaded(key)

	for _, position := range m.groups[key] {
		if position != winner && m.isMounted(position) {
			hidden = append(hidden, m.Files[position])
		}
	}

	if !ok {
		return File{}, hidden, false
	}

	return m.Files[winner], hidden, true
}

// lookupLoaded returns the position in Files of the file the game reads
// among the files of a group, given the group's key: the last in load order
// that is mounted and that no replace_path drops. It returns false if the
// game reads none.
func (m *Mounts) lookupLoaded(key string) (int32, bool) {
	for _, position := range slices.Backward(m.groups[key]) {
		if _, dropped := m.FindReplacingMod(position); !dropped && m.isMounted(position) {
			return position, true
		}
	}

	return -1, false
}

// isMounted reports whether the file at a position can be read at all: it is
// still listed, the game mounts it, and the mounts answer for it.
func (m *Mounts) isMounted(position int32) bool {
	return m.IsListed(position) && !m.IsUnmounted(position) && m.Covers(position)
}

// FindHiding returns why the game does not read the file at a position, and
// the root of the mod that hides it. It returns "" for a file the game reads,
// and for one that is no longer listed or that the mounts do not answer for.
// An unmounted file has no hider.
func (m *Mounts) FindHiding(position int32) (Hiding, Root) {
	switch {
	case m.IsUnmounted(position):
		return HidingUnmounted, Root{}
	case !m.isMounted(position):
		return "", Root{}
	}

	if mod, dropped := m.FindReplacingMod(position); dropped {
		return HidingDropped, mod
	}

	if winner, ok := m.lookupLoaded(m.placements[position].key); ok && winner != position {
		return HidingShadowed, m.Roots[m.placements[winner].root]
	}

	return "", Root{}
}

// toGroupKey returns the key of the group a path from a root belongs to: the
// path in lower case, less its stage folder, so that a path shared by two
// stages is one path. The path names no root, so it is split by the first
// root's stages, which holds because every root of one game mounts the same
// stages.
func (m *Mounts) toGroupKey(rel string) string {
	var stages []string
	if len(m.Roots) > 0 {
		stages = m.Roots[0].Stages
	}

	_, rest := SplitStage(strings.ToLower(rel), stages)

	return rest
}

// Covers reports whether the mounts answer for the file at a position: the
// game's own, or a mod's that Narrow kept. Mounts never narrowed answer for
// every file.
func (m *Mounts) Covers(position int32) bool {
	origin := m.Files[position].Origin

	return origin == OriginVanilla || m.hasMod(origin)
}

// hasMod reports whether the mounts answer for a mod. Mounts never narrowed
// answer for every mod.
func (m *Mounts) hasMod(origin string) bool {
	_, ok := m.origins[origin]

	return m.origins == nil || ok
}

// FindReplacingMod returns the root of the mod whose replace_path drops the
// file at a position: a game file in a folder that a mod's replace_path
// names. It returns false if no replace_path drops the file.
func (m *Mounts) FindReplacingMod(position int32) (Root, bool) {
	if m.Files[position].Origin != OriginVanilla {
		return Root{}, false
	}

	replacer, ok := m.findReplacer(m.placements[position].key, 0)
	if !ok {
		return Root{}, false
	}

	return m.Roots[replacer], true
}

// IsReplacedLater reports whether the file at a position is a mod's file in a
// folder that a later mod's replace_path names. No source says whether that
// replace_path drops it, so the game is taken to read it.
func (m *Mounts) IsReplacedLater(position int32) bool {
	if m.Files[position].Origin == OriginVanilla {
		return false
	}

	place := m.placements[position]
	_, ok := m.findReplacer(place.key, place.root+1)

	return ok
}

// findReplacer returns the position in Roots of the first mod, from the given
// position in Roots on, whose replace_path names the folder of a file, given
// the key of the file's group. A replace_path names one folder exactly, not
// the folders below it. It returns false if no mod the mounts answer for
// names the folder.
func (m *Mounts) findReplacer(key string, start int) (int, bool) {
	folder := path.Dir(key)
	if folder == "." {
		folder = ""
	}

	for position := start; position < len(m.Roots); position++ {
		if m.hasMod(m.Roots[position].Origin) && replaces(m.Roots[position], folder) {
			return position, true
		}
	}

	return 0, false
}

// replaces reports whether a root's replace_path names a folder, given as a
// path from its stage in lower case.
func replaces(root Root, folder string) bool {
	for _, replacePath := range root.ReplacePaths {
		if _, rest := SplitStage(strings.ToLower(strings.Trim(replacePath, "/")), root.Stages); rest == folder {
			return true
		}
	}

	return false
}

// IsLoaded reports whether the game reads the file at a position in Files.
func (m *Mounts) IsLoaded(position int32) bool {
	winner, ok := m.lookupLoaded(m.placements[position].key)

	return ok && winner == position
}

// IsUnmounted reports whether the game never mounts the file at a position,
// as for a mod's file outside the stage folders of a game that has them.
func (m *Mounts) IsUnmounted(position int32) bool {
	return m.placements[position].unmounted
}

// IsGameFolder reports whether the game's own files lie under the first
// folder of the path of the file at a position, from the file's stage. A
// file in no folder lies under none.
func (m *Mounts) IsGameFolder(position int32) bool {
	folder, _, nested := strings.Cut(m.placements[position].key, "/")
	_, ok := m.gameFolders[folder]

	return nested && ok
}

// ListImages returns the image files the game reads below a folder, given as
// a path from a stage in any letter case. An empty folder gives every image.
func (m *Mounts) ListImages(folder string) []File {
	folder = strings.ToLower(folder)
	var images []File

	for position, file := range m.Files {
		if file.Format == FormatImage && strings.HasPrefix(m.placements[position].key, folder) &&
			m.IsLoaded(int32(position)) {
			images = append(images, file)
		}
	}

	return images
}

// CompareRoots orders two files, given by their positions in Files, by their
// roots in load order. It returns 0 for two files of one root.
func (m *Mounts) CompareRoots(first, second int32) int {
	return cmp.Compare(m.placements[first].root, m.placements[second].root)
}

// ComparePaths orders two files, given by their positions in Files, by their
// paths from their stages, as CompareLoadOrder orders paths.
func (m *Mounts) ComparePaths(first, second int32) int {
	return CompareLoadOrder(m.Files[first].StageRel(), m.Files[second].StageRel())
}

// CompareLoadOrder orders two paths the way the game loads files: a folder's
// files before its subfolders', and names byte by byte, with case. It returns
// a negative number if the first path loads first, a positive one if it loads
// last, and 0 if they are equal.
func CompareLoadOrder(first, second string) int {
	for {
		firstHead, firstRest, firstMore := strings.Cut(first, "/")
		secondHead, secondRest, secondMore := strings.Cut(second, "/")

		switch {
		case firstMore != secondMore:
			if firstMore {
				return 1
			}

			return -1

		case firstHead != secondHead:
			return strings.Compare(firstHead, secondHead)

		case !firstMore:
			return 0
		}

		first, second = firstRest, secondRest
	}
}

// Narrow returns a copy of the mounts that answers for the game's files and
// the given mods' only: the other mods' files are not read, and hide and drop
// no file. It shares everything else with the original.
func (m *Mounts) Narrow(origins []string) *Mounts {
	narrowed := *m
	narrowed.origins = make(map[string]struct{}, len(origins))

	for _, origin := range origins {
		narrowed.origins[origin] = struct{}{}
	}

	return &narrowed
}

// Add lists a file the mounts do not list, and returns its position in Files.
// A file at a path listed before, and unlisted since, takes its old position
// again. Any other is added at the end of Files.
func (m *Mounts) Add(file File) int32 {
	position := int32(slices.IndexFunc(m.Files, func(other File) bool { return other.Key == file.Key }))
	if position < 0 {
		m.Files = append(m.Files, file)
		m.placements = append(m.placements, placement{})
		position = int32(len(m.Files) - 1)
		m.buildGroup(position)
	}

	m.byKey[file.Key] = position

	return position
}

// Unlist drops the file with the given key from the listed files. It keeps
// its position in Files, which a file at the same path takes again.
func (m *Mounts) Unlist(key string) {
	delete(m.byKey, key)
}

// Clone returns a copy of the mounts that Add and Unlist can change while
// the original is still being read. It shares the rest with the original.
func (m *Mounts) Clone() *Mounts {
	next := *m
	next.Files = slices.Clone(m.Files)
	next.placements = slices.Clone(m.placements)
	next.byKey = maps.Clone(m.byKey)
	next.groups = maps.Clone(m.groups)

	return &next
}
