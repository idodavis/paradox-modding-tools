// releaseservice.go is the Release page: a mod's listing read from and written to its own files,
// the description converted between Markdown and Workshop BBCode, the Workshop extras, and the
// publish to Steam.

package services

import (
	"cmp"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/steam"
)

const (
	steamLegalURL    = "https://steamcommunity.com/workshop/workshoplegalagreement"
	steamWorkshopURL = "https://steamcommunity.com/sharedfiles/filedetails/?id="
)

// ServeSteamPublish runs this process as a Workshop publish when it was started as one, and reports
// whether it was. main calls it first: a publish must start nothing else, not even the config or
// the single-instance lock.
func ServeSteamPublish() (exitCode int, served bool) {
	return steam.Serve()
}

// ReleaseService loads, saves and publishes a mod's listing.
type ReleaseService struct {
	App *AppService
}

// Listing is the Release page payload.
type Listing struct {
	WorkspaceID      string          `json:"workspaceId"`
	ModID            string          `json:"modId"`
	GameID           string          `json:"gameId"`
	Root             string          `json:"root"`
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	SupportedVersion string          `json:"supportedVersion"`
	Tags             []string        `json:"tags"`
	ThumbnailRel     string          `json:"thumbnailRel"`
	ThumbnailAbs     string          `json:"thumbnailAbs"`
	Previews         []steam.Preview `json:"previews"`
	RemoteFileID     string          `json:"remoteFileId"`
	ShortDescription string          `json:"shortDescription"`
	ReadmeMd         string          `json:"readmeMd"`
	ReadmeBbcode     string          `json:"readmeBbcode"`
	ChangeNote       string          `json:"changeNote"`
	SteamAppID       int             `json:"steamAppId"`
	DescMdRel        string          `json:"descMdRel"`
	DescBbRel        string          `json:"descBbRel"`
	WorkshopIgnore   string          `json:"workshopIgnore"`
	WorkshopURL      string          `json:"workshopUrl"`
}

// PublishResult is Steam's answer plus the listing saved with the item's id.
type PublishResult struct {
	PublishedFileID     string   `json:"publishedFileId"`
	NeedsLegalAgreement bool     `json:"needsLegalAgreement"`
	LegalURL            string   `json:"legalUrl,omitempty"`
	Listing             *Listing `json:"listing,omitempty"`
}

// UploadFiles is what an upload of a mod sends and leaves out.
type UploadFiles struct {
	Count int      `json:"count"` // the files sent
	Bytes int64    `json:"bytes"` // their size
	Left  []string `json:"left"`  // the files left out, mod-relative with forward slashes
}

// UploadFiles walks a mod as its upload copy does, by its saved ignore text.
func (r *ReleaseService) UploadFiles(workspaceID, modID string) (*UploadFiles, error) {
	_, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	patterns := steam.LoadIgnorePatterns(mod.Path, mod.WorkshopIgnore)
	files := &UploadFiles{Left: []string{}}
	source.Walk(mod.Path, func(file string) {
		rel, err := filepath.Rel(mod.Path, file)
		if err != nil {
			return
		}
		if rel = filepath.ToSlash(rel); steam.Ignored(rel, patterns) {
			files.Left = append(files.Left, rel)
			return
		}
		if info, err := os.Stat(file); err == nil {
			files.Count++
			files.Bytes += info.Size()
		}
	})
	return files, nil
}

// LoadListing reads descriptor + readmes for a workspace mod.
func (r *ReleaseService) LoadListing(workspaceID, modID string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// SaveListing writes the descriptor, description files and changelog.
func (r *ReleaseService) SaveListing(in Listing) (*Listing, error) {
	ws, mod, err := r.workspaceMod(in.WorkspaceID, in.ModID)
	if err != nil {
		return nil, err
	}
	root := mod.Path
	markdown, bbcode, err := steam.DescriptionFiles(root, mod.DescMdRel, mod.DescBbRel)
	if err != nil {
		return nil, err
	}
	fields := local.ListingFields{
		Name: in.Name, Version: in.Version, SupportedVersion: in.SupportedVersion,
		Tags: in.Tags, RemoteFileID: in.RemoteFileID, Picture: filepath.Base(in.ThumbnailRel),
		ShortDescription: cmp.Or(in.ShortDescription, firstParagraph(in.ReadmeMd)),
	}
	if err := local.WriteListingFields(ws.GameID, root, fields); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(markdown)), []byte(in.ReadmeMd), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(bbcode)), []byte(in.ReadmeBbcode), 0o644); err != nil {
		return nil, err
	}
	if in.ChangeNote != "" && in.Version != "" {
		dir := filepath.Join(root, "changelog")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, in.Version+".bbcode"), []byte(in.ChangeNote), 0o644); err != nil {
			return nil, err
		}
	}
	return r.loadFrom(ws, mod)
}

// Convert maps listing markup. UI writes Markdown and asks for "bbcode".
func (r *ReleaseService) Convert(src, to string) steam.Converted {
	if to == "bbcode" {
		return steam.MarkdownToBBCode(src)
	}
	return steam.BBCodeToMarkdown(src)
}

// editWorkshop resolves the mod, applies one Workshop edit and reloads the listing.
func (r *ReleaseService) editWorkshop(workspaceID, modID string, edit func(modRoot string) error) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	if err := edit(mod.Path); err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// AddWorkshopPreview copies an image into workshop/previews (Steam extra cap).
func (r *ReleaseService) AddWorkshopPreview(workspaceID, modID, srcPath string) (*Listing, error) {
	return r.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.AddPreview(modRoot, srcPath)
	})
}

// RemoveWorkshopPreview deletes one extra image under workshop/previews.
func (r *ReleaseService) RemoveWorkshopPreview(workspaceID, modID, rel string) (*Listing, error) {
	return r.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.RemovePreview(modRoot, rel)
	})
}

// AddWorkshopVideo appends a YouTube id from a watch/embed/youtu.be URL or raw id.
func (r *ReleaseService) AddWorkshopVideo(workspaceID, modID, urlOrID string) (*Listing, error) {
	return r.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.AddVideo(modRoot, urlOrID)
	})
}

// RemoveWorkshopVideo drops one YouTube id from workshop/videos.txt.
func (r *ReleaseService) RemoveWorkshopVideo(workspaceID, modID, id string) (*Listing, error) {
	return r.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.RemoveVideo(modRoot, id)
	})
}

// DraftChangelog returns a version stub for the Steam change note field.
func (r *ReleaseService) DraftChangelog(workspaceID, modID, version string) (string, error) {
	if _, _, err := r.workspaceMod(workspaceID, modID); err != nil {
		return "", err
	}
	return "Version " + cmp.Or(version, "0.0.0") + "\n", nil
}

// BumpVersion returns current with its patch number, or with kind "minor" its minor number, one
// higher and every number after it zeroed.
func (r *ReleaseService) BumpVersion(current, kind string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return "1.0.0"
	}
	part := 2
	if kind == "minor" {
		part = 1
	}
	parts := strings.Split(current, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	// Sscanf reads a part's leading digits, so "3beta" counts as 3
	number := 0
	fmt.Sscanf(parts[part], "%d", &number)
	parts[part] = strconv.Itoa(number + 1)
	for i := part + 1; i < len(parts); i++ {
		if _, err := fmt.Sscanf(parts[i], "%d", new(int)); err == nil {
			parts[i] = "0"
		}
	}
	return strings.Join(parts, ".")
}

// CopyThumbnail replaces the mod's listing image with src and returns its mod-relative path.
func (r *ReleaseService) CopyThumbnail(workspaceID, modID, src string) (string, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return "", err
	}
	return local.CopyThumbnail(ws.GameID, mod.Path, src)
}

// PublishSteam saves the listing, publishes to the Workshop, then saves the listing with its id.
func (r *ReleaseService) PublishSteam(in Listing) (result *PublishResult, err error) {
	slog.Info("workshop publish start", "mod", in.Name, "id", in.RemoteFileID)
	defer func() {
		if err != nil {
			slog.Error("workshop publish failed", "mod", in.Name, "error", err)
			return
		}
		slog.Info("workshop publish done", "mod", in.Name, "id", result.PublishedFileID, "legalAgreement", result.NeedsLegalAgreement)
	}()
	listing, err := r.SaveListing(in)
	if err != nil {
		return nil, fmt.Errorf("save listing: %w", err)
	}
	if listing.SteamAppID == 0 {
		return nil, fmt.Errorf("no Steam App ID for game %s", listing.GameID)
	}

	// the child loads the game's own copy of Steam's library, from the install the workspace is on
	installPath := ""
	if _, inst := r.App.workspace(in.WorkspaceID); inst != nil {
		installPath = inst.Path
	}
	library, err := steam.Library(game.Get(listing.GameID), installPath)
	if err != nil {
		return nil, err
	}

	stage, err := os.MkdirTemp("", "pmt-steam-stage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := steam.CopyMod(listing.Root, stage, listing.WorkshopIgnore); err != nil {
		return nil, err
	}
	preview := ""
	if listing.ThumbnailRel != "" {
		staged := filepath.Join(stage, filepath.FromSlash(listing.ThumbnailRel))
		if _, err := os.Stat(staged); err == nil {
			preview = staged
		}
	}
	resp, err := steam.Publish(library, steam.Request{
		AppID: uint32(listing.SteamAppID), Folder: stage,
		Title: listing.Name, Description: listing.ReadmeBbcode,
		Preview: preview, PublishedFileID: listing.RemoteFileID,
		ChangeNote: in.ChangeNote, Visibility: 2,
		ExtraPreviews: steam.StagedPreviewFiles(stage, listing.Previews),
		ExtraVideos:   steam.VideoIDs(listing.Previews),
	})
	if err != nil {
		return nil, err
	}

	listing.RemoteFileID = resp.PublishedFileID
	saved, err := r.SaveListing(*listing)
	if err != nil {
		return nil, fmt.Errorf("save listing after publish: %w", err)
	}
	result = &PublishResult{PublishedFileID: resp.PublishedFileID, NeedsLegalAgreement: resp.NeedsLegalAgreement, Listing: saved}
	if resp.NeedsLegalAgreement {
		result.LegalURL = steamLegalURL
	}
	return result, nil
}

// workspaceMod is findMod for a mod that has a folder to read, write and publish.
func (r *ReleaseService) workspaceMod(workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	ws, mod, err := findMod(r.App, workspaceID, modID)
	if err != nil {
		return nil, nil, err
	}
	if mod.Path == "" {
		return nil, nil, fmt.Errorf("mod has no path")
	}
	return ws, mod, nil
}

func (r *ReleaseService) loadFrom(ws *Workspace, mod *WorkspaceMod) (*Listing, error) {
	root := mod.Path
	fields, err := local.ReadListingFields(ws.GameID, root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	fields.Name = cmp.Or(fields.Name, mod.Name)
	mdRel, bbRel, err := steam.DescriptionFiles(root, mod.DescMdRel, mod.DescBbRel)
	if err != nil {
		return nil, err
	}
	if mdRel == steam.DescMdName && bbRel == steam.DescBbName {
		if err := steam.EnsureDescriptions(root, fields.Name, fields.ShortDescription); err != nil {
			return nil, err
		}
	}
	markdown, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mdRel)))
	if err != nil {
		return nil, err
	}
	bbcode, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(bbRel)))
	note := ""
	if fields.Version != "" {
		// a changelog that is not there reads as no note
		changelog, _ := os.ReadFile(filepath.Join(root, "changelog", fields.Version+".bbcode"))
		note = string(changelog)
	}
	thumbRel, thumbAbs := "", ""
	if rel, found := local.FindThumbnail(ws.GameID, root, fields.Picture); found {
		thumbRel, thumbAbs = rel, filepath.Join(root, filepath.FromSlash(rel))
	}
	appID := 0
	if info := game.Get(ws.GameID); info != nil {
		appID = info.SteamAppID
	}
	workshopURL := ""
	if fields.RemoteFileID != "" {
		workshopURL = steamWorkshopURL + fields.RemoteFileID
	}
	return &Listing{
		WorkspaceID: ws.ID, ModID: mod.ID, GameID: ws.GameID,
		Root: root, Name: fields.Name, Version: fields.Version,
		SupportedVersion: fields.SupportedVersion, Tags: fields.Tags,
		ThumbnailRel: thumbRel, ThumbnailAbs: thumbAbs,
		Previews: steam.ScanPreviews(root), RemoteFileID: fields.RemoteFileID,
		ShortDescription: fields.ShortDescription,
		ReadmeMd:         string(markdown), ReadmeBbcode: string(bbcode), ChangeNote: note,
		SteamAppID: appID, DescMdRel: mdRel, DescBbRel: bbRel,
		WorkshopIgnore: mod.WorkshopIgnore, WorkshopURL: workshopURL,
	}, nil
}

// firstParagraph is the text's first non-empty paragraph, cut to a short description's 200 bytes.
func firstParagraph(text string) string {
	for paragraph := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		if paragraph = strings.TrimSpace(paragraph); paragraph != "" {
			return paragraph[:min(len(paragraph), 200)]
		}
	}
	return ""
}
