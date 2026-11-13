// mounts.go decides which file the game reads at each path.

package files

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
)

// Mounts is the files of a list of roots in load order, and which one the
// game reads at each path. A file keeps its position in Files across Clone and
// Narrow, so a position names one file in each.
type Mounts struct {
	Roots []Root

	// Files are the files of the roots, root by root. A file Unlist dropped
	// keeps its position, and IsListed tells it apart.
	Files []File

	// byKey holds the position in Files of each listed file, by key.
	byKey map[string]int32

	// groups holds the positions of files sharing a path below their stage, in
	// load order, by toGroupKey.
	groups map[string][]int32

	placements []placement

	rootKeys []string

	// origins holds the ids of the mods Narrow kept. It is nil for every mod.
	origins map[string]struct{}

	// gameFolders holds the first folder of each game file below its stage, in
	// lower case.
	gameFolders map[string]struct{}
}

// placement is where the game mounts one file.
type placement struct {
	root int

	// stage is 0 for none, else 1 and up in mount order.
	stage int

	// key is the file's group key: its path below its stage, in lower case.
	key string

	unmounted bool
}

// Mount is Mounts.Mount on empty mounts.
func Mount(roots []Root) (*Mounts, error) {
	return new(Mounts).Mount(roots)
}

// Mount returns new mounts of these roots and files followed by those under
// the given roots, so a file's position means the same in both. A file below a
// root inside another given root is listed once, under the inner one. It
// returns an error if a root cannot be walked.
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

func listRootKeys(roots []Root) []string {
	keys := make([]string, len(roots))
	for position, root := range roots {
		keys[position] = ToKey(root.Dir) + "/"
	}

	return keys
}

// findRoot returns the position of the innermost root holding a key, or false.
func findRoot(rootKeys []string, key string) (int, bool) {
	position, length, ok := 0, 0, false

	for candidate, rootKey := range rootKeys {
		if len(rootKey) > length && strings.HasPrefix(key, rootKey) {
			position, length, ok = candidate, len(rootKey), true
		}
	}

	return position, ok
}

// NewMounts returns the mounts for the given roots and their files, as Mount
// lists them. It also restores mounts from a gob cache.
func NewMounts(roots []Root, list []File) *Mounts {
	mounts := &Mounts{
		Roots: roots, Files: list, rootKeys: listRootKeys(roots),
		byKey: make(map[string]int32, len(list)), groups: make(map[string][]int32, len(list)),
		placements: make([]placement, len(list)), gameFolders: map[string]struct{}{},
	}

	for position, file := range list {
		mounts.byKey[file.Key] = int32(position)
		mounts.buildGroup(int32(position))

		// A file with its path from an outer root, as a DLC's from the game's,
		// names no game folder.
		place := mounts.placements[position]
		folder, _, nested := strings.Cut(place.key, "/")
		inner := mounts.rootKeys[place.root]+strings.ToLower(file.Rel) == file.Key
		if nested && inner && file.Origin == OriginVanilla {
			mounts.gameFolders[folder] = struct{}{}
		}
	}

	return mounts
}

// buildGroup places one file and adds it to its group. The group is replaced,
// since a clone may read it.
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
// outside every stage of a root that has stages (EU5 debug.log:80-83, 5392;
// the wiki's Mod structure, l.46-48).
func isUnmounted(file File, stages []string) bool {
	return len(stages) > 0 && file.Origin != OriginVanilla && file.Stage == ""
}

// compareLoaded orders two files as the game loads them.
func (m *Mounts) compareLoaded(first, second int32) int {
	firstPlace, secondPlace := m.placements[first], m.placements[second]

	return cmp.Or(cmp.Compare(firstPlace.root, secondPlace.root), cmp.Compare(firstPlace.stage, secondPlace.stage),
		cmp.Compare(first, second))
}

// FindFile returns the position of the listed file at a path, or false.
func (m *Mounts) FindFile(path string) (int32, bool) {
	position, ok := m.byKey[ToKey(path)]

	return position, ok
}

// IsListed reports whether Unlist has not dropped a file.
func (m *Mounts) IsListed(position int32) bool {
	listed, ok := m.byKey[m.Files[position].Key]

	return ok && listed == position
}

// FindRoot returns the innermost root that holds a path, or false.
func (m *Mounts) FindRoot(path string) (Root, bool) {
	position, ok := findRoot(m.rootKeys, ToKey(path)+"/")
	if !ok {
		return Root{}, false
	}

	return m.Roots[position], true
}

// Hiding is why the game does not read a file. The zero Hiding is none.
type Hiding string

const (
	// HidingShadowed is a file at the same path later in load order.
	HidingShadowed Hiding = "shadowed"

	// HidingDropped is a mod's replace_path that names the file's folder.
	HidingDropped Hiding = "dropped"

	// HidingUnmounted is a file the game never mounts.
	HidingUnmounted Hiding = "unmounted"
)

// FindLoaded returns the file the game reads at a path, and the files it
// hides. It returns false if the game reads none.
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

// lookupLoaded returns the position of the last mounted, undropped file of a
// group, or false.
func (m *Mounts) lookupLoaded(key string) (int32, bool) {
	for _, position := range slices.Backward(m.groups[key]) {
		if _, dropped := m.FindReplacingMod(position); !dropped && m.isMounted(position) {
			return position, true
		}
	}

	return -1, false
}

func (m *Mounts) isMounted(position int32) bool {
	return m.IsListed(position) && !m.IsUnmounted(position) && m.Covers(position)
}

// FindHiding returns why the game does not read a file, and the root that
// hides it. It returns "" for a file the game reads or one not listed. An
// unmounted file has no hider.
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

// toGroupKey returns a path's lower-case group key, without its stage folder.
// It splits by the first root's stages, as every root of a game has the same.
func (m *Mounts) toGroupKey(rel string) string {
	var stages []string
	if len(m.Roots) > 0 {
		stages = m.Roots[0].Stages
	}

	_, rest := SplitStage(strings.ToLower(rel), stages)

	return rest
}

// Covers reports whether the mounts answer for a file: the game's own, or a
// mod's that Narrow kept. Mounts never narrowed cover every file.
func (m *Mounts) Covers(position int32) bool {
	origin := m.Files[position].Origin

	return origin == OriginVanilla || m.hasMod(origin)
}

func (m *Mounts) hasMod(origin string) bool {
	_, ok := m.origins[origin]

	return m.origins == nil || ok
}

// FindReplacingMod returns the root whose replace_path drops a game file, or
// false.
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

// IsReplacedLater reports whether a mod's file is in a folder a later mod's
// replace_path names. No source says whether that drops it, so the game is
// taken to read it.
func (m *Mounts) IsReplacedLater(position int32) bool {
	if m.Files[position].Origin == OriginVanilla {
		return false
	}

	place := m.placements[position]
	_, ok := m.findReplacer(place.key, place.root+1)

	return ok
}

// findReplacer returns the position of the first covered mod, from a given
// root on, whose replace_path names the key's folder exactly, not its
// subfolders. It returns false for none.
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

// replaces reports whether a root's replace_path names a folder.
func replaces(root Root, folder string) bool {
	for _, replacePath := range root.ReplacePaths {
		if _, rest := SplitStage(strings.ToLower(strings.Trim(replacePath, "/")), root.Stages); rest == folder {
			return true
		}
	}

	return false
}

// IsLoaded reports whether the game reads a file.
func (m *Mounts) IsLoaded(position int32) bool {
	winner, ok := m.lookupLoaded(m.placements[position].key)

	return ok && winner == position
}

// IsUnmounted reports whether the game never mounts a file.
func (m *Mounts) IsUnmounted(position int32) bool {
	return m.placements[position].unmounted
}

// IsGameFolder reports whether the game's own files lie under a file's first
// folder below its stage. A file in no folder lies under none.
func (m *Mounts) IsGameFolder(position int32) bool {
	folder, _, nested := strings.Cut(m.placements[position].key, "/")
	_, ok := m.gameFolders[folder]

	return nested && ok
}

// ListImages returns the image files the game reads below a folder. An empty
// folder gives every image.
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

// CompareRoots orders two files by root in load order. It is 0 for one root.
func (m *Mounts) CompareRoots(first, second int32) int {
	return cmp.Compare(m.placements[first].root, m.placements[second].root)
}

// ComparePaths orders two files by path below their stages.
func (m *Mounts) ComparePaths(first, second int32) int {
	return CompareLoadOrder(m.Files[first].StageRel(), m.Files[second].StageRel())
}

// CompareLoadOrder orders two paths as the game loads them: a folder's files
// before its subfolders', then names byte by byte, with case. It returns a
// negative number, 0 or a positive one.
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

// Narrow returns a copy that answers for the game's files and the given mods'
// only. It shares the rest with the original.
func (m *Mounts) Narrow(origins []string) *Mounts {
	narrowed := *m
	narrowed.origins = make(map[string]struct{}, len(origins))

	for _, origin := range origins {
		narrowed.origins[origin] = struct{}{}
	}

	return &narrowed
}

// Add lists a file and returns its position in Files. A path listed before and
// unlisted since takes its old position again; any other goes at the end.
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

// Unlist drops the file with a key from the listed files.
func (m *Mounts) Unlist(key string) {
	delete(m.byKey, key)
}

// Clone returns a copy that Add and Unlist can change while the original is
// read.
func (m *Mounts) Clone() *Mounts {
	next := *m
	next.Files = slices.Clone(m.Files)
	next.placements = slices.Clone(m.placements)
	next.byKey = maps.Clone(m.byKey)
	next.groups = maps.Clone(m.groups)

	return &next
}
