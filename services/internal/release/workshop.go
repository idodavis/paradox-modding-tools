// workshop.go is local Steam Workshop extra previews (images + YouTube ids) and stage copy.

package release

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	previewDir        = "workshop/previews"
	videosRel         = "workshop/videos.txt"
	maxWorkshopExtras = 10
	metadataDir       = ".metadata"
)

var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

var previewExt = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// Preview is one extra Workshop image or YouTube id on the listing.
type Preview struct {
	Rel  string
	Abs  string
	Kind string
	ID   string
}

// ScanPreviews lists extra images under workshop/previews and ids from workshop/videos.txt.
func ScanPreviews(root string) []Preview {
	out := []Preview{}
	dir := filepath.Join(root, filepath.FromSlash(previewDir))
	entries, err := os.ReadDir(dir)
	if err == nil {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !previewExt[ext] {
				continue
			}
			names = append(names, e.Name())
		}
		slices.Sort(names)
		for _, name := range names {
			rel := filepath.ToSlash(filepath.Join(previewDir, name))
			out = append(out, Preview{
				Rel: rel, Abs: filepath.Join(root, filepath.FromSlash(rel)),
				Kind: "image",
			})
		}
	}
	for _, id := range readVideoIDs(root) {
		out = append(out, Preview{Kind: "youtube", ID: id})
	}
	return out
}

// AddPreview copies an image into workshop/previews (Steam extra cap).
func AddPreview(modRoot, srcPath string) error {
	if len(ScanPreviews(modRoot)) >= maxWorkshopExtras {
		return fmt.Errorf("workshop extras cap is %d", maxWorkshopExtras)
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	if !previewExt[ext] {
		return fmt.Errorf("unsupported preview type %s", ext)
	}
	dir := filepath.Join(modRoot, filepath.FromSlash(previewDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create previews dir: %w", err)
	}
	dest, err := uniquePreviewDest(dir, srcPath)
	if err != nil {
		return err
	}
	if err := CopyFile(srcPath, dest); err != nil {
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
	got, err := filepath.Rel(modRoot, abs)
	if err != nil || strings.HasPrefix(got, "..") {
		return fmt.Errorf("not a workshop preview")
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove preview: %w", err)
	}
	return nil
}

// AddVideo appends a YouTube id from a watch/embed/youtu.be URL or raw id.
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
	ids := readVideoIDs(modRoot)
	next := slices.DeleteFunc(slices.Clone(ids), func(v string) bool { return v == id })
	return writeVideoIDs(modRoot, next)
}

// VideoIDs returns unique YouTube ids from workshop previews.
func VideoIDs(previews []Preview) []string {
	seen := map[string]bool{}
	var ids []string
	for _, p := range previews {
		if p.Kind != "youtube" || p.ID == "" || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		ids = append(ids, p.ID)
	}
	return ids
}

// StagedPreviewFiles returns unique on-disk extra preview image paths under a stage folder.
func StagedPreviewFiles(stage string, previews []Preview) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range previews {
		if p.Kind == "youtube" || p.Rel == "" {
			continue
		}
		path := filepath.Join(stage, filepath.FromSlash(p.Rel))
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// ParseYouTubeID accepts a raw id or common YouTube URLs.
func ParseYouTubeID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if youtubeIDRe.MatchString(s) {
		return s, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("not a youtube id")
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	if host == "youtu.be" {
		id := strings.Trim(u.Path, "/")
		if i := strings.IndexByte(id, '/'); i >= 0 {
			id = id[:i]
		}
		if youtubeIDRe.MatchString(id) {
			return id, nil
		}
		return "", fmt.Errorf("not a youtube id")
	}
	if !strings.Contains(host, "youtube.com") {
		return "", fmt.Errorf("not a youtube id")
	}
	if v := u.Query().Get("v"); youtubeIDRe.MatchString(v) {
		return v, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 {
		switch parts[0] {
		case "embed", "shorts", "live", "v":
			if youtubeIDRe.MatchString(parts[1]) {
				return parts[1], nil
			}
		}
	}
	return "", fmt.Errorf("not a youtube id")
}

// CopyMod copies a mod tree into dest, applying gitignore + workshop ignore rules.
//
// Symlinked directories are followed and their contents copied. WalkDir reports
// a symlink as a non-directory whatever it points at, so a linked folder went to
// CopyFile, os.Open followed it to a directory, io.Copy returned EISDIR, and the
// error escaped the walk — one linked folder anywhere in the tree aborted the
// entire publish. Steam receives real files, so copying the contents is the only
// answer that both completes and uploads the mod the modder has.
func CopyMod(src, dest, extraIgnore string) error {
	var seen []os.FileInfo
	if fi, err := os.Stat(src); err == nil {
		seen = append(seen, fi)
	}
	return copyTree(src, dest, loadIgnorePatterns(src, extraIgnore), &seen)
}

// copyTree is CopyMod's body. seen holds the directories already entered, so a
// link pointing at one of its own ancestors terminates.
//
// Identity is os.SameFile, not a resolved path. filepath.EvalSymlinks does not
// resolve a Windows junction — it hands back the link's own path — so keying on
// it made every hop through a self-referential junction look like a new
// directory. SameFile compares what the filesystem says the directory is.
func copyTree(src, dest string, patterns []string, seen *[]os.FileInfo) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		// A link is asked what it points at only once the ignore rules have had
		// their say, so an ignored link is never resolved at all.
		if skipStage(rel, d.IsDir(), patterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		out := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		// WalkDir lstats, so anything pointing at a directory arrives here
		// looking like a file. Ask what it IS rather than how it was made: a
		// Unix symlink is ModeSymlink, but a Windows junction — which is what an
		// unprivileged Windows account can actually create, and therefore what
		// modders have — is ModeIrregular. Testing the mode caught only the
		// first, which is the half of the bug that does not happen on Windows.
		if !d.Type().IsRegular() {
			fi, err := os.Stat(p)
			if err != nil {
				return nil // a broken link is not a reason to fail a publish
			}
			if fi.IsDir() {
				return copyLinkedDir(p, out, fi, patterns, seen)
			}
		}
		return CopyFile(p, out)
	})
}

// copyLinkedDir copies the contents of a linked directory into dest.
//
// It enumerates the children itself rather than handing the link to WalkDir:
// WalkDir lstats its root, sees a non-directory, and returns without reading a
// single entry, so walking the link copied nothing at all.
func copyLinkedDir(
	link, dest string, fi os.FileInfo, patterns []string, seen *[]os.FileInfo,
) error {
	for _, s := range *seen {
		if os.SameFile(s, fi) {
			return nil
		}
	}
	*seen = append(*seen, fi)
	ents, err := os.ReadDir(link)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, e := range ents {
		err := copyTree(filepath.Join(link, e.Name()), filepath.Join(dest, e.Name()),
			patterns, seen)
		if err != nil {
			return err
		}
	}
	return nil
}

// CopyFile copies one file, creating parent directories as needed.
func CopyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

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
		id := strings.TrimSpace(line)
		if id == "" || strings.HasPrefix(id, "#") {
			continue
		}
		if youtubeIDRe.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func writeVideoIDs(root string, ids []string) error {
	path := filepath.Join(root, filepath.FromSlash(videosRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workshop dir: %w", err)
	}
	body := strings.Join(ids, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write videos: %w", err)
	}
	return nil
}

func loadIgnorePatterns(root, extra string) []string {
	var out []string
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			out = append(out, sc.Text())
		}
		_ = f.Close()
	}
	sc := bufio.NewScanner(strings.NewReader(extra))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

func skipStage(rel string, isDir bool, patterns []string) bool {
	slash := filepath.ToSlash(rel)
	if slash == "." || slash == "" {
		return false
	}
	base := path.Base(slash)
	if base == ".gitignore" {
		return true
	}
	if skipDot(slash) {
		return true
	}
	return ignoreMatch(slash, isDir, patterns)
}

func skipDot(slash string) bool {
	for i, p := range strings.Split(slash, "/") {
		if p == "" || p == "." || p == ".." {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			continue
		}
		if i == 0 && p == metadataDir {
			continue
		}
		return true
	}
	return false
}

func ignoreMatch(rel string, isDir bool, patterns []string) bool {
	ign := false
	for _, raw := range patterns {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		if neg {
			line = strings.TrimSpace(line[1:])
		}
		if matchGitPattern(line, rel, isDir) {
			ign = !neg
		}
	}
	return ign
}

func matchGitPattern(pat, rel string, isDir bool) bool {
	dirOnly := strings.HasSuffix(pat, "/")
	if dirOnly && !isDir {
		return false
	}
	pat = strings.TrimSuffix(pat, "/")
	anchored := strings.HasPrefix(pat, "/")
	pat = strings.TrimPrefix(pat, "/")
	if pat == "" {
		return false
	}
	if anchored {
		return globOK(pat, rel)
	}
	if globOK(pat, rel) || globOK(pat, path.Base(rel)) {
		return true
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		if globOK(pat, strings.Join(parts[i:], "/")) {
			return true
		}
	}
	return false
}

func globOK(pat, name string) bool {
	ok, err := path.Match(pat, name)
	return err == nil && ok
}
