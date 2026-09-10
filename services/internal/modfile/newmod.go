// newmod.go writes a starter mod folder: descriptor + loc stub + empty common/events.

package modfile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"paradox-modding-tools/services/internal/game"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DefaultModParent is UserDataDir/<game>/mod, or "".
func DefaultModParent(gameID string) string {
	ud := game.UserDataDir(gameID, "")
	if ud == "" {
		return ""
	}
	return filepath.Join(ud, "mod")
}

// ModSlug is a filesystem-safe folder name from a display name.
func ModSlug(name string) string {
	var b strings.Builder
	prevUS := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevUS = false
			continue
		}
		if !prevUS {
			b.WriteByte('_')
			prevUS = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "mod"
	}
	return out
}

func pinVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "latest") {
		return ""
	}
	return v
}

func locLangOrEnglish(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "english"
	}
	return lang
}

func descOrName(description, name string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return name
	}
	return description
}

// NewModOpts is the WriteNewMod argument bundle.
type NewModOpts struct {
	GameID, Root, Name, SupportedVersion, LocLang, Description, ThumbnailSrc string
}

// WriteNewMod creates root with a descriptor, empty common/ and events/, loc stub,
// description files, and an optional thumbnail copied in with picture= on the descriptor.
func WriteNewMod(opts NewModOpts) error {
	info := game.Get(opts.GameID)
	if info == nil {
		return fmt.Errorf("unknown game %s", opts.GameID)
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	root := opts.Root
	desc := game.DescriptorPath(opts.GameID, root)
	if _, err := os.Stat(desc); err == nil {
		return fmt.Errorf("mod already exists: %s", desc)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	picture, err := copyThumbnail(opts.GameID, root, opts.ThumbnailSrc)
	if err != nil {
		return err
	}
	if err := writeDescriptor(info, root, name, pinVersion(opts.SupportedVersion), picture); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "common"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "events"), 0o755); err != nil {
		return err
	}
	body := descOrName(opts.Description, name)
	if err := writeDescriptions(root, name, body); err != nil {
		return err
	}
	lang := locLangOrEnglish(opts.LocLang)
	locDir := filepath.Join(root, "localization", lang)
	if err := os.MkdirAll(locDir, 0o755); err != nil {
		return err
	}
	locName := ModSlug(name) + "_l_" + lang + ".yml"
	locBody := append(append([]byte{}, utf8BOM...), []byte("l_"+lang+":\n")...)
	return os.WriteFile(filepath.Join(locDir, locName), locBody, 0o644)
}

func writeDescriptions(root, name, description string) error {
	md := "# " + name + "\n\n" + description + "\n"
	bb := "[b]" + name + "[/b]\n\n" + description + "\n"
	if err := os.WriteFile(filepath.Join(root, DescMdName), []byte(md), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, DescBbName), []byte(bb), 0o644)
}

// copyThumbnail installs src as the mod's listing image and returns the value
// for the descriptor's picture field.
//
// That value is the bare file name, NOT the path written. Every one of the 31
// workshop mods measured declares `"picture": "thumbnail.png"` — including the
// EU5 mods whose file sits in .metadata/ — so the field is read relative to the
// descriptor's own directory and a mod PMT writes must look like the mods the
// games ship. ThumbnailPath is what answers "where is the file".
func copyThumbnail(gameID, root, src string) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(src))
	if !slices.Contains(ThumbnailExts, ext) {
		return "", fmt.Errorf("thumbnail must be one of %s",
			strings.Join(ThumbnailExts, ", "))
	}
	rel, _ := ThumbnailPath(gameID, root, ext)
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(abs)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return path.Base(rel), nil
}

func writeDescriptor(info *game.GameInfo, root, name, supported, picture string) error {
	if info.Descriptor == "metadata" {
		return writeMetadataJSON(root, name, supported, picture)
	}
	return writeDescriptorMod(root, name, supported, picture)
}

func writeDescriptorMod(root, name, supported, picture string) error {
	var b strings.Builder
	b.WriteString("version=\"1.0\"\n")
	fmt.Fprintf(&b, "name=\"%s\"\n", strings.ReplaceAll(name, `"`, `'`))
	if supported != "" {
		fmt.Fprintf(&b, "supported_version=\"%s\"\n", supported)
	}
	if picture != "" {
		fmt.Fprintf(&b, "picture=\"%s\"\n", picture)
	}
	return os.WriteFile(filepath.Join(root, game.DescriptorModFile), []byte(b.String()), 0o644)
}

func writeMetadataJSON(root, name, supported, picture string) error {
	meta := map[string]string{
		"name":    name,
		"id":      ModSlug(name),
		"version": "1.0.0",
	}
	if supported != "" {
		meta["supported_game_version"] = supported
	}
	if picture != "" {
		meta["picture"] = picture
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(root, game.MetadataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, game.MetadataFile), append(raw, '\n'), 0o644)
}
