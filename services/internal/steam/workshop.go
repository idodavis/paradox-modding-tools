// workshop.go is what a mod keeps for its Steam Workshop page: the two description files
// (Markdown, and the Workshop's BBCode), the extra preview images under workshop/previews, the
// YouTube ids in workshop/videos.txt, the .workshop-ignore export, and the copy of the mod that
// is uploaded, with the mod's .gitignore and the workspace's ignore text applied.

package steam

import (
	"bufio"
	"cmp"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/local"
)

const (
	DescMdName = "mod-description.md"     // the default description in Markdown
	DescBbName = "mod-description.bbcode" // the default description in Workshop BBCode
	IgnoreFile = ".workshop-ignore"       // where the workspace's ignore text is exported beside the mod

	previewDir        = "workshop/previews"
	videosRel         = "workshop/videos.txt"
	maxWorkshopExtras = 10
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

// EnsureDescriptions writes the default description files that are missing, leaving existing ones.
func EnsureDescriptions(root, name, description string) error {
	body := cmp.Or(strings.TrimSpace(description), name)
	markdown := filepath.Join(root, DescMdName)
	bbcode := filepath.Join(root, DescBbName)
	if _, err := os.Stat(markdown); os.IsNotExist(err) {
		if err := os.WriteFile(markdown, []byte("# "+name+"\n\n"+body+"\n"), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(bbcode); os.IsNotExist(err) {
		if err := os.WriteFile(bbcode, []byte("[b]"+name+"[/b]\n\n"+body+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// DescriptionFiles is the mod's two description files relative to root: the ones the
// workspace chose, which must exist, else the defaults.
func DescriptionFiles(root, markdownChoice, bbcodeChoice string) (markdown, bbcode string, err error) {
	markdown = cmp.Or(strings.TrimSpace(markdownChoice), DescMdName)
	bbcode = cmp.Or(strings.TrimSpace(bbcodeChoice), DescBbName)
	if markdown == DescMdName && bbcode == DescBbName {
		return markdown, bbcode, nil
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(markdown))); err != nil {
		return "", "", fmt.Errorf("description markdown: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(bbcode))); err != nil {
		return "", "", fmt.Errorf("description bbcode: %w", err)
	}
	return markdown, bbcode, nil
}

// DescriptionRel is a chosen description file as the workspace stores it: relative to the mod
// root, which it must be inside, and "" for a default name or no choice.
func DescriptionRel(root, chosen string) (string, error) {
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return "", nil
	}
	abs := chosen
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(chosen))
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("description file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("description path is a directory")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("description file must be inside the mod folder")
	}
	if slash := filepath.ToSlash(rel); slash != DescMdName && slash != DescBbName {
		return slash, nil
	}
	return "", nil
}

// ScanPreviews lists the extra images under workshop/previews and the ids in workshop/videos.txt.
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

// RemovePreview deletes one extra image under workshop/previews.
func RemovePreview(modRoot, rel string) error {
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, previewDir+"/") {
		return fmt.Errorf("not a workshop preview")
	}
	abs := filepath.Join(modRoot, filepath.FromSlash(rel))
	inside, err := filepath.Rel(modRoot, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("not a workshop preview")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove preview: %w", err)
	}
	return nil
}

// AddVideo appends a YouTube id, given as a watch, embed or youtu.be URL or as the id itself.
func AddVideo(modRoot, urlOrID string) error {
	id, err := ParseYouTubeID(urlOrID)
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

// RemoveVideo drops one YouTube id from workshop/videos.txt.
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

// StagedPreviewFiles is where the listing's extra images are inside the upload copy, each
// once and only where it exists.
func StagedPreviewFiles(stage string, previews []Preview) []string {
	seen := map[string]bool{}
	var out []string
	for _, preview := range previews {
		if preview.Kind == "youtube" || preview.Rel == "" {
			continue
		}
		staged := filepath.Join(stage, filepath.FromSlash(preview.Rel))
		if _, err := os.Stat(staged); err != nil || seen[staged] {
			continue
		}
		seen[staged] = true
		out = append(out, staged)
	}
	return out
}

// ParseYouTubeID accepts a raw id or the common YouTube URLs.
func ParseYouTubeID(urlOrID string) (string, error) {
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

// CopyMod copies a mod into dest for upload, leaving out what its .gitignore and the extra
// ignore text name. It walks the way the engine does, following linked folders and junctions,
// because Steam receives real files and mods are often linked from elsewhere.
func CopyMod(src, dest, extraIgnore string) error {
	patterns := loadIgnorePatterns(src, extraIgnore)
	var failed error
	source.Walk(src, func(file string) {
		if failed != nil {
			return
		}
		rel, err := filepath.Rel(src, file)
		if err != nil {
			failed = err
			return
		}
		if !ignored(filepath.ToSlash(rel), patterns) {
			failed = local.CopyFile(file, filepath.Join(dest, rel))
		}
	})
	return failed
}

// ignored reports a file the upload leaves out: a .gitignore, anything under a dot-named
// folder but a top-level .metadata, or a match of the ignore rules on the file or any folder
// above it, which no later rule can take back.
func ignored(rel string, patterns []string) bool {
	parts := strings.Split(rel, "/")
	if parts[len(parts)-1] == ".gitignore" {
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
	videos := filepath.Join(root, filepath.FromSlash(videosRel))
	if err := os.MkdirAll(filepath.Dir(videos), 0o755); err != nil {
		return fmt.Errorf("create workshop dir: %w", err)
	}
	body := strings.Join(ids, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(videos, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write videos: %w", err)
	}
	return nil
}

func loadIgnorePatterns(root, extra string) []string {
	var out []string
	if file, err := os.Open(filepath.Join(root, ".gitignore")); err == nil {
		lines := bufio.NewScanner(file)
		for lines.Scan() {
			out = append(out, lines.Text())
		}
		_ = file.Close()
	}
	lines := bufio.NewScanner(strings.NewReader(extra))
	for lines.Scan() {
		out = append(out, lines.Text())
	}
	return out
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

func matchGitPattern(pattern, rel string, isDir bool) bool {
	if strings.HasSuffix(pattern, "/") && !isDir {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "/")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if pattern == "" {
		return false
	}
	if anchored {
		return globMatch(pattern, rel)
	}
	if globMatch(pattern, rel) || globMatch(pattern, path.Base(rel)) {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		if globMatch(pattern, strings.Join(parts[i:], "/")) {
			return true
		}
	}
	return false
}

func globMatch(pattern, name string) bool {
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}
