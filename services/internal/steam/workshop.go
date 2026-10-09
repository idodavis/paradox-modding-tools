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

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/local"
)

const (
	DescMdName    = "workshop/mod-description.md"     // the default Markdown description
	DescBbName    = "workshop/mod-description.bbcode" // the default BBCode description
	ChangelogName = "workshop/changelog.bbcode"       // the latest change note
	IgnoreFile    = ".workshop-ignore"                // where the workspace's ignore text is exported beside the mod

	previewDir        = "workshop/previews"
	videosRel         = "workshop/previews/videos.txt"
	maxWorkshopExtras = 10
)

// Where a mod's Workshop description comes from.
const (
	FromMarkdown = "markdown" // a Markdown file, converted to BBCode in the upload
	FromBBCode   = "bbcode"   // a BBCode file, uploaded as written
	FromSteam    = "steam"    // Steam's own text, which an upload leaves alone
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// previewExts is what the Workshop shows as an extra preview: what a thumbnail may be, and webp.
var previewExts = append(slices.Clone(local.ThumbnailExts), ".webp")

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
	return local.WriteBytes(file, []byte(text))
}

// DescriptionRel is a chosen description file as the workspace stores it: relative to the mod
// root, which it must be inside, and "" for a default name or no choice. It need not exist yet.
func DescriptionRel(root, chosen string) (string, error) {
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return "", nil
	}
	abs := chosen
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(chosen))
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
			out = append(out, Preview{Rel: rel, Abs: filepath.Join(root, filepath.FromSlash(rel)), Kind: "image"})
		}
	}
	for _, id := range readVideoIDs(root) {
		out = append(out, Preview{Kind: "youtube", ID: id})
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
		if preview.Kind == "youtube" {
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
		if len(ScanPreviews(modRoot)) >= maxWorkshopExtras {
			return fmt.Errorf("workshop extras cap is %d", maxWorkshopExtras)
		}
		if err := saveImage(filepath.Join(modRoot, filepath.FromSlash(previewDir)), extra); err != nil {
			return err
		}
	}
	return nil
}

var imageClient = &http.Client{Timeout: time.Minute}

func saveImage(dir string, extra Extra) error {
	resp, err := imageClient.Get(extra.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", extra.Name, err)
	}
	defer resp.Body.Close()
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
	return local.Write(dest, func(out io.Writer) error {
		_, err := io.Copy(out, resp.Body)
		return err
	})
}

// AddPreview copies an image into workshop/previews, within Steam's cap on extras.
func AddPreview(modRoot, srcPath string) error {
	if len(ScanPreviews(modRoot)) >= maxWorkshopExtras {
		return fmt.Errorf("workshop extras cap is %d", maxWorkshopExtras)
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	if !slices.Contains(previewExts, ext) {
		return fmt.Errorf("unsupported preview type %s", ext)
	}
	dest, err := uniquePreviewDest(filepath.Join(modRoot, filepath.FromSlash(previewDir)), srcPath)
	if err != nil {
		return err
	}
	if err := local.CopyFile(srcPath, dest); err != nil {
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
	previews := ScanPreviews(modRoot)
	ids := VideoIDs(previews)
	if slices.Contains(ids, id) {
		return nil
	}
	if len(previews) >= maxWorkshopExtras {
		return fmt.Errorf("workshop extras cap is %d", maxWorkshopExtras)
	}
	return writeVideoIDs(modRoot, append(ids, id))
}

// RemoveVideo drops one YouTube id from workshop/previews/videos.txt.
func RemoveVideo(modRoot, id string) error {
	kept := slices.DeleteFunc(readVideoIDs(modRoot), func(listed string) bool { return listed == id })
	return writeVideoIDs(modRoot, kept)
}

// VideoIDs is the listing's YouTube ids, each once.
func VideoIDs(previews []Preview) []string {
	seen := map[string]bool{}
	var ids []string
	for _, preview := range previews {
		if preview.Kind != "youtube" || preview.ID == "" || seen[preview.ID] {
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
		if preview.Kind != "youtube" {
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
	if watched := link.Query().Get("v"); youtubeID.MatchString(watched) {
		return watched, nil
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

// Upload is the mod at root as an upload takes it, relative to root with forward slashes: the
// files it sends, and those its .gitignore and the extra ignore text leave out. It walks the way
// the engine does, following linked folders and junctions, because Steam receives real files and
// mods are often linked from elsewhere.
func Upload(root, extraIgnore string) (sent, left []string) {
	gitignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	patterns := strings.Split(string(gitignore)+"\n"+extraIgnore, "\n")
	_ = inventory.Walk(root, func(file string) error {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil
		}
		if rel = filepath.ToSlash(rel); ignored(rel, patterns) {
			left = append(left, rel)
		} else {
			sent = append(sent, rel)
		}
		return nil
	})
	return sent, left
}

// CopyMod copies what an upload of the mod at src sends into dest, a fresh folder nothing reads until
// the copy is whole: plain writes, no flush per file (a mod has thousands).
func CopyMod(src, dest, extraIgnore string) error {
	sent, _ := Upload(src, extraIgnore)
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
// (Paradox Mods): whole or not at all (local.Write), streamed, the files at the zip's root.
func ZipMod(src, dest, extraIgnore string) error {
	sent, _ := Upload(src, extraIgnore)
	return local.Write(dest, func(out io.Writer) error {
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
	Rel          string `json:"rel"`
	Status       string `json:"status"`
	Binary       bool   `json:"binary"`    // no line diff: a NUL in its first 512 bytes on either side, as the editor's reads judge
	Steam        string `json:"steam"`     // its path in Steam's copy, which a new file lacks
	Workspace    string `json:"workspace"` // its path in the mod, which a removed file lacks
	LinesAdded   int    `json:"linesAdded"`
	LinesRemoved int    `json:"linesRemoved"`
}

// LineCounts is how many lines a change from before to after adds and removes, by the shortest
// edit between their lines (Myers): each line compared with its line break, as a diff draws them.
func LineCounts(before, after string) (added, removed int) {
	old, now := slices.Collect(strings.Lines(before)), slices.Collect(strings.Lines(after))
	n, m := len(old), len(now)
	far := make([]int, 2*(n+m)+2) // the furthest x reached on each diagonal k, at far[n+m+k]
	for edits := 0; ; edits++ {
		for k := -edits; k <= edits; k += 2 {
			x := far[n+m+k+1] // down from the diagonal above, else right from the one below
			if k != -edits && (k == edits || far[n+m+k-1] >= far[n+m+k+1]) {
				x = far[n+m+k-1] + 1
			}
			for y := x - k; x < n && y < m && old[x] == now[y]; y++ {
				x++
			}
			if far[n+m+k] = x; x >= n && x-k >= m {
				return (m - n + edits) / 2, (n - m + edits) / 2
			}
		}
	}
}

// Changes is what an upload of the mod at root changes in the copy of it Steam has at steamRoot, by
// path.
func Changes(steamRoot, root, extraIgnore string) []FileChange {
	read := func(dir, rel string) []byte {
		data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		return data
	}
	change := func(rel, status string, before, after []byte) FileChange {
		c := FileChange{Rel: rel, Status: status, Binary: inventory.Binary(before) || inventory.Binary(after)}
		if status != "A" {
			c.Steam = filepath.Join(steamRoot, filepath.FromSlash(rel))
		}
		if status != "D" {
			c.Workspace = filepath.Join(root, filepath.FromSlash(rel))
		}
		if !c.Binary {
			c.LinesAdded, c.LinesRemoved = LineCounts(string(before), string(after))
		}
		return c
	}
	theirs, _ := Upload(steamRoot, "")
	gone := map[string]bool{}
	for _, rel := range theirs {
		gone[rel] = true
	}
	ours, _ := Upload(root, extraIgnore)
	changes := []FileChange{}
	for _, rel := range ours {
		data, before, status := read(root, rel), []byte(nil), "A"
		if gone[rel] {
			delete(gone, rel)
			if before = read(steamRoot, rel); bytes.Equal(data, before) {
				continue
			}
			status = "M"
		}
		changes = append(changes, change(rel, status, before, data))
	}
	for rel := range gone {
		changes = append(changes, change(rel, "D", read(steamRoot, rel), nil))
	}
	slices.SortFunc(changes, func(a, b FileChange) int { return strings.Compare(a.Rel, b.Rel) })
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
		if strings.HasPrefix(part, ".") && !(depth == 0 && part == game.MetadataDir) {
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

func writeVideoIDs(root string, ids []string) error {
	body := strings.Join(ids, "\n")
	if body != "" {
		body += "\n"
	}
	return local.WriteBytes(filepath.Join(root, filepath.FromSlash(videosRel)), []byte(body))
}

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
	if after, anchored := strings.CutPrefix(pattern, "/"); anchored {
		pattern = after
	} else if pattern != "" {
		pattern = "**/" + pattern
	}
	matched, err := doublestar.Match(pattern, rel)
	return pattern != "" && err == nil && matched
}
