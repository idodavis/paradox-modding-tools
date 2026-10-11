// workshop.go is what a mod keeps for its Steam Workshop page, under workshop/: its description file
// (Markdown, or the Workshop's BBCode), the latest change note, the extra preview images and the
// YouTube ids in workshop/previews (never uploaded as the mod's files), the .workshop-ignore
// export, and the copy of the mod that is uploaded, with the mod's .gitignore and the workspace's
// ignore text applied.

package steam

import (
	"archive/zip"
	"bytes"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/modfiles"
)

// The files under workshop/ a mod keeps for its Workshop page.
const (
	DescMdName    = "workshop/mod-description.md"     // the default Markdown description
	DescBbName    = "workshop/mod-description.bbcode" // the default BBCode description
	ChangelogName = "workshop/changelog.bbcode"       // the latest change note
)

// IgnoreFile is where the workspace's ignore text is exported beside the mod.
const IgnoreFile = ".workshop-ignore"

// Where a mod keeps its extra previews and how many of them Steam takes.
const (
	previewDir        = "workshop/previews"
	videosRel         = "workshop/previews/videos.txt"
	maxWorkshopExtras = 10
)

// The two kinds of a Preview.
const (
	kindImage   = "image"
	kindYouTube = "youtube"
)

// Where a mod's Workshop description comes from.
const (
	FromMarkdown = "markdown" // a Markdown file, converted to BBCode in the upload
	FromBBCode   = "bbcode"   // a BBCode file, uploaded as written
	FromSteam    = "steam"    // Steam's own text, which an upload leaves alone
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// previewExts is what the Workshop shows as an extra preview: what a thumbnail may be, and webp.
var previewExts = append(slices.Clone(modfiles.ThumbnailExts), ".webp")

// Preview is one extra Workshop image or YouTube id on the listing.
type Preview struct {
	Rel  string `json:"rel"`
	Abs  string `json:"abs"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// NewDescription writes the description file rel, missing until now, for a mod called name: its
// name as a heading, then description, in the markup of source.
func NewDescription(root, rel, source, name, description string) error {
	file := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s already exists", rel)
	}
	body := cmp.Or(strings.TrimSpace(description), name)
	text := "# " + name + "\n\n" + body + "\n"
	if source == FromBBCode {
		text = "[b]" + name + "[/b]\n\n" + body + "\n"
	}
	return appdata.WriteBytes(file, []byte(text))
}

// DescriptionRel is a chosen description file as the workspace stores it: relative to the mod
// root, which it must be inside, and "" for a default name or no choice. It need not exist yet.
func DescriptionRel(root, choice string) (string, error) {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return "", nil
	}
	abs := choice
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(choice))
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return "", fmt.Errorf("description path is a directory")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("description file must be inside the mod folder")
	}
	if slash := filepath.ToSlash(rel); slash != DescMdName && slash != DescBbName {
		return slash, nil
	}
	return "", nil
}

// ScanPreviews lists the extra images in workshop/previews, then the ids in its videos.txt.
func ScanPreviews(root string) []Preview {
	out := []Preview{}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(previewDir)))
	if err == nil {
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() && slices.Contains(previewExts, strings.ToLower(filepath.Ext(entry.Name()))) {
				names = append(names, entry.Name())
			}
		}
		slices.Sort(names)
		for _, name := range names {
			rel := previewDir + "/" + name
			out = append(out, Preview{Rel: rel, Abs: filepath.Join(root, filepath.FromSlash(rel)), Kind: kindImage})
		}
	}
	for _, id := range readVideoIDs(root) {
		out = append(out, Preview{Kind: kindYouTube, ID: id})
	}
	return out
}

// Extra is one of an item's extra previews on Steam: an image's address and file name, or a YouTube id.
type Extra struct {
	URL   string `json:"url,omitempty"`
	Name  string `json:"name,omitempty"`
	Video string `json:"video,omitempty"`
}

// Label is how an extra is known on both sides: an image by its file name, a video by its id.
func (extra Extra) Label() string {
	return cmp.Or(extra.Video, extra.Name)
}

// ExtrasOf is a listing's previews as the extras an upload sends.
func ExtrasOf(previews []Preview) []Extra {
	var extras []Extra
	for _, preview := range previews {
		if preview.Kind == kindYouTube {
			extras = append(extras, Extra{Video: preview.ID})
		} else {
			extras = append(extras, Extra{Name: path.Base(preview.Rel)})
		}
	}
	return extras
}

// imageExts is the extension an image Steam serves without one in its name gets, by its type.
var imageExts = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// SaveExtras adds Steam's extras the mod lacks: a video's id to workshop/previews/videos.txt, an
// image downloaded into workshop/previews unless one of its name is there, within Steam's cap.
func SaveExtras(modRoot string, extras []Extra) error {
	have := map[string]bool{}
	for _, extra := range ExtrasOf(ScanPreviews(modRoot)) {
		have[extra.Label()] = true
	}
	for _, extra := range extras {
		if extra.Video != "" {
			if err := AddVideo(modRoot, extra.Video); err != nil {
				return err
			}
			continue
		}
		if have[extra.Name] || extra.URL == "" {
			continue
		}
		if err := atExtrasCap(modRoot); err != nil {
			return err
		}
		if err := saveImage(filepath.Join(modRoot, filepath.FromSlash(previewDir)), extra); err != nil {
			return err
		}
	}
	return nil
}

var imageClient = &http.Client{Timeout: time.Minute}

// atExtrasCap returns the error that refuses another extra where the mod already keeps as many as Steam takes.
func atExtrasCap(modRoot string) error {
	if len(ScanPreviews(modRoot)) >= maxWorkshopExtras {
		return fmt.Errorf("workshop extras cap is %d", maxWorkshopExtras)
	}
	return nil
}

// saveImage downloads the extra's image into dir under its file name, and its type's extension where the
// name has none Steam shows. The file is whole or not there at all.
func saveImage(dir string, extra Extra) error {
	resp, err := imageClient.Get(extra.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", extra.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", extra.Name, resp.Status)
	}
	name := cmp.Or(extra.Name, "steam-preview")
	if !slices.Contains(previewExts, strings.ToLower(filepath.Ext(name))) {
		name += cmp.Or(imageExts[resp.Header.Get("Content-Type")], ".png")
	}
	dest, err := uniquePreviewDest(dir, name)
	if err != nil {
		return err
	}
	// whole or not at all: a half image would pass for one already imported
	return appdata.Write(dest, func(out io.Writer) error {
		_, err := io.Copy(out, resp.Body)
		return err
	})
}

// AddPreview copies an image into workshop/previews, within Steam's cap on extras.
func AddPreview(modRoot, srcPath string) error {
	if err := atExtrasCap(modRoot); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	if !slices.Contains(previewExts, ext) {
		return fmt.Errorf("unsupported preview type %s", ext)
	}
	dest, err := uniquePreviewDest(filepath.Join(modRoot, filepath.FromSlash(previewDir)), srcPath)
	if err != nil {
		return err
	}
	if err := appdata.CopyFile(srcPath, dest); err != nil {
		return fmt.Errorf("copy preview: %w", err)
	}
	return nil
}

// RemovePreview deletes one extra image in workshop/previews, and nothing else.
func RemovePreview(modRoot, rel string) error {
	rel = path.Clean(filepath.ToSlash(rel))
	if path.Dir(rel) != previewDir || !slices.Contains(previewExts, strings.ToLower(path.Ext(rel))) {
		return fmt.Errorf("not a workshop preview")
	}
	if err := os.Remove(filepath.Join(modRoot, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove preview: %w", err)
	}
	return nil
}

// AddVideo appends a YouTube id, given as a watch, embed or youtu.be URL or as the id itself.
func AddVideo(modRoot, urlOrID string) error {
	id, err := parseYouTubeID(urlOrID)
	if err != nil {
		return err
	}
	ids := VideoIDs(ScanPreviews(modRoot))
	if slices.Contains(ids, id) {
		return nil
	}
	if err := atExtrasCap(modRoot); err != nil {
		return err
	}
	return writeVideoIDs(modRoot, append(ids, id))
}

// RemoveVideo drops one YouTube id from workshop/previews/videos.txt.
func RemoveVideo(modRoot, id string) error {
	kept := slices.DeleteFunc(readVideoIDs(modRoot), func(stored string) bool { return stored == id })
	return writeVideoIDs(modRoot, kept)
}

// VideoIDs is the listing's YouTube ids, each once.
func VideoIDs(previews []Preview) []string {
	seen := map[string]bool{}
	var ids []string
	for _, preview := range previews {
		if preview.Kind != kindYouTube || preview.ID == "" || seen[preview.ID] {
			continue
		}
		seen[preview.ID] = true
		ids = append(ids, preview.ID)
	}
	return ids
}

// PreviewFiles is the listing's extra images in the mod, which the upload copy leaves out.
func PreviewFiles(previews []Preview) []string {
	var out []string
	for _, preview := range previews {
		if preview.Kind != kindYouTube {
			out = append(out, preview.Abs)
		}
	}
	return out
}

// parseYouTubeID accepts a raw id or the common YouTube URLs.
func parseYouTubeID(urlOrID string) (string, error) {
	urlOrID = strings.TrimSpace(urlOrID)
	if youtubeID.MatchString(urlOrID) {
		return urlOrID, nil
	}
	link, err := url.Parse(urlOrID)
	if err != nil {
		return "", fmt.Errorf("not a youtube id")
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(link.Host), "www."), "m.")
	if host == "youtu.be" {
		id, _, _ := strings.Cut(strings.Trim(link.Path, "/"), "/")
		if youtubeID.MatchString(id) {
			return id, nil
		}
		return "", fmt.Errorf("not a youtube id")
	}
	if !strings.Contains(host, "youtube.com") {
		return "", fmt.Errorf("not a youtube id")
	}
	if videoID := link.Query().Get("v"); youtubeID.MatchString(videoID) {
		return videoID, nil
	}
	parts := strings.Split(strings.Trim(link.Path, "/"), "/")
	if len(parts) >= 2 {
		switch parts[0] {
		case "embed", "shorts", "live", "v":
			if youtubeID.MatchString(parts[1]) {
				return parts[1], nil
			}
		}
	}
	return "", fmt.Errorf("not a youtube id")
}

// UploadFiles is the mod at root as an upload takes it, relative to root with forward slashes: the
// files it sends, and those its .gitignore and the extra ignore text leave out (skipped). It walks
// the way the engine does, following linked folders and junctions, because Steam receives real
// files and mods are often linked from elsewhere.
func UploadFiles(root, extraIgnore string) (sent, skipped []string) {
	gitignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	patterns := strings.Split(string(gitignore)+"\n"+extraIgnore, "\n")
	_ = inventory.Walk(root, func(file string) error {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil
		}
		if rel = filepath.ToSlash(rel); ignored(rel, patterns) {
			skipped = append(skipped, rel)
		} else {
			sent = append(sent, rel)
		}
		return nil
	})
	return sent, skipped
}

// CopyMod copies what an upload of the mod at src sends into dest, a fresh folder nothing reads until
// the copy is whole: plain writes, no flush per file (a mod has thousands).
func CopyMod(src, dest, extraIgnore string) error {
	sent, _ := UploadFiles(src, extraIgnore)
	for _, rel := range sent {
		data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ZipMod writes what an upload of the mod at src sends into a zip at dest, for a store with no upload of ours
// (Paradox Mods): whole or not at all (appdata.Write), streamed, the files at the zip's root.
func ZipMod(src, dest, extraIgnore string) error {
	sent, _ := UploadFiles(src, extraIgnore)
	return appdata.Write(dest, func(out io.Writer) error {
		archive := zip.NewWriter(out)
		for _, rel := range sent {
			data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			entry, err := archive.Create(rel)
			if err != nil {
				return err
			}
			if _, err := entry.Write(data); err != nil {
				return err
			}
		}
		return archive.Close()
	})
}

// FileChange is one file an upload changes on Steam: A added, M modified, D deleted, with the lines
// it adds and removes (LineCounts) unless it is binary.
type FileChange struct {
	Rel    string `json:"rel"`
	Status string `json:"status"`
	// no line diff: a NUL in its first 512 bytes on either side, as the editor's reads judge
	Binary       bool   `json:"binary"`
	Steam        string `json:"steam"`     // its path in Steam's copy, which a new file lacks
	Workspace    string `json:"workspace"` // its path in the mod, which a removed file lacks
	LinesAdded   int    `json:"linesAdded"`
	LinesRemoved int    `json:"linesRemoved"`
}

// LineCounts is how many lines a change from oldText to newText adds and removes, by the shortest
// edit between their lines (Myers): each line compared with its line break, as a diff draws them.
func LineCounts(oldText, newText string) (added, removed int) {
	oldLines, newLines := slices.Collect(strings.Lines(oldText)), slices.Collect(strings.Lines(newText))
	oldCount, newCount := len(oldLines), len(newLines)
	mid := oldCount + newCount
	far := make([]int, 2*mid+2) // the furthest old line reached on each diagonal k, at far[mid+k]
	for edits := 0; ; edits++ {
		for k := -edits; k <= edits; k += 2 {
			oldPos := far[mid+k+1] // down from the diagonal above, else right from the one below
			if k != -edits && (k == edits || far[mid+k-1] >= far[mid+k+1]) {
				oldPos = far[mid+k-1] + 1
			}
			for newPos := oldPos - k; oldPos < oldCount && newPos < newCount && oldLines[oldPos] == newLines[newPos]; newPos++ {
				oldPos++
			}
			if far[mid+k] = oldPos; oldPos >= oldCount && oldPos-k >= newCount {
				return (newCount - oldCount + edits) / 2, (oldCount - newCount + edits) / 2
			}
		}
	}
}

// Changes is what an upload of the mod at root changes in the copy of it Steam has at steamRoot, by
// path.
func Changes(steamRoot, root, extraIgnore string) []FileChange {
	contents := func(dir, rel string) []byte {
		data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		return data
	}
	changeOf := func(rel, status string, theirs, ours []byte) FileChange {
		change := FileChange{Rel: rel, Status: status, Binary: inventory.Binary(theirs) || inventory.Binary(ours)}
		if status != "A" {
			change.Steam = filepath.Join(steamRoot, filepath.FromSlash(rel))
		}
		if status != "D" {
			change.Workspace = filepath.Join(root, filepath.FromSlash(rel))
		}
		if !change.Binary {
			change.LinesAdded, change.LinesRemoved = LineCounts(string(theirs), string(ours))
		}
		return change
	}
	steamRels, _ := UploadFiles(steamRoot, "")
	gone := map[string]bool{}
	for _, rel := range steamRels {
		gone[rel] = true
	}
	modRels, _ := UploadFiles(root, extraIgnore)
	changes := []FileChange{}
	for _, rel := range modRels {
		ours, theirs, status := contents(root, rel), []byte(nil), "A"
		if gone[rel] {
			delete(gone, rel)
			if theirs = contents(steamRoot, rel); bytes.Equal(ours, theirs) {
				continue
			}
			status = "M"
		}
		changes = append(changes, changeOf(rel, status, theirs, ours))
	}
	for rel := range gone {
		changes = append(changes, changeOf(rel, "D", contents(steamRoot, rel), nil))
	}
	slices.SortFunc(changes, func(first, second FileChange) int { return strings.Compare(first.Rel, second.Rel) })
	return changes
}

// ignored reports a file the upload leaves out: a .gitignore, the Workshop page's extras in
// workshop/previews (Steam takes them as the page's gallery), anything under a dot-named folder
// but a top-level .metadata, or a match of the ignore rules on the file or any folder above it,
// which no later rule can take back.
func ignored(rel string, patterns []string) bool {
	parts := strings.Split(rel, "/")
	if parts[len(parts)-1] == ".gitignore" || strings.HasPrefix(rel, previewDir+"/") {
		return true
	}
	for depth, part := range parts {
		if strings.HasPrefix(part, ".") && (depth != 0 || part != game.MetadataDir) {
			return true
		}
		if ignoreMatch(strings.Join(parts[:depth+1], "/"), depth < len(parts)-1, patterns) {
			return true
		}
	}
	return false
}

// uniquePreviewDest is src's name inside dir, numbered where that name is taken.
func uniquePreviewDest(dir, src string) (string, error) {
	base := filepath.Base(src)
	dest := filepath.Join(dir, base)
	if _, err := os.Stat(dest); err != nil {
		return dest, nil
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 100; i++ {
		dest = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(dest); err != nil {
			return dest, nil
		}
	}
	return "", fmt.Errorf("preview name collision")
}

// readVideoIDs is the YouTube ids in the mod's videos.txt, one a line, skipping any line that is not an id.
func readVideoIDs(root string) []string {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(videosRel)))
	if err != nil {
		return nil
	}
	var ids []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if id := strings.TrimSpace(line); youtubeID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// writeVideoIDs replaces the mod's videos.txt with ids, one a line, whole or not at all.
func writeVideoIDs(root string, ids []string) error {
	body := strings.Join(ids, "\n")
	if body != "" {
		body += "\n"
	}
	return appdata.WriteBytes(filepath.Join(root, filepath.FromSlash(videosRel)), []byte(body))
}

// ignoreMatch is whether the ignore rules, in order, leave rel out: the last rule that matches it
// decides, and a rule that starts with ! takes a match back. Blank lines and # comments are no rule.
func ignoreMatch(rel string, isDir bool, patterns []string) bool {
	matched := false
	for _, raw := range patterns {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimSpace(line[1:])
		}
		if matchGitPattern(line, rel, isDir) {
			matched = !negated
		}
	}
	return matched
}

// matchGitPattern is whether one ignore rule matches rel: a trailing / matches folders only, a leading / anchors it to
// the mod's root, any other rule matches at any depth; ** spans folders (doublestar, as search's globs).
func matchGitPattern(pattern, rel string, isDir bool) bool {
	if strings.HasSuffix(pattern, "/") && !isDir {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "/")
	if rest, anchored := strings.CutPrefix(pattern, "/"); anchored {
		pattern = rest
	} else if pattern != "" {
		pattern = "**/" + pattern
	}
	matched, err := doublestar.Match(pattern, rel)
	return pattern != "" && err == nil && matched
}
