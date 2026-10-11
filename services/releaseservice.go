// releaseservice.go is the Release page: a mod's listing read from and written to its own files,
// the description converted between Markdown and Workshop BBCode, the Workshop extras, the publish
// to Steam, and the mod compared with the copy Steam has.

package services

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/modfiles"
	"paradox-modding-tools/services/internal/steam"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	config *ConfigFile
}

// NewReleaseService is the Release page's service over the config.
func NewReleaseService(config *ConfigFile) *ReleaseService {
	return &ReleaseService{config: config}
}

// Listing is the Release page payload.
type Listing struct {
	WorkspaceID      string          `json:"workspaceId"`
	ModID            string          `json:"modId"`
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
	ReadmeMd         string          `json:"readmeMd"`     // the description, from a Markdown file
	ReadmeBbcode     string          `json:"readmeBbcode"` // the description, from a BBCode file
	ChangeNote       string          `json:"changeNote"`
	DescMdRel        string          `json:"descMdRel"`
	DescBbRel        string          `json:"descBbRel"`
	WorkshopURL      string          `json:"workshopUrl"`
	Descriptor       string          `json:"descriptor"` // the descriptor's path in the mod
	Saved            string          `json:"saved"`      // when the descriptor was last written (RFC 3339)
	LegalURL         string          `json:"legalUrl"`   // the Workshop agreement an upload accepts
}

// ListingSummary is a mod's listing as Settings shows it, read without writing.
type ListingSummary struct {
	RemoteFileID string `json:"remoteFileId"`
	ThumbnailRel string `json:"thumbnailRel"`
	ThumbnailAbs string `json:"thumbnailAbs"`
	DescSource   string `json:"descSource"` // markdown, bbcode or steam
	DescRel      string `json:"descRel"`    // the source's file in the mod; "" for steam
	DescExists   bool   `json:"descExists"`
	SteamVersion string `json:"steamVersion,omitempty"` // the version in PMT's last copy of the item
}

// PublishResult is Steam's answer.
type PublishResult struct {
	PublishedFileID     string `json:"publishedFileId"`
	NeedsLegalAgreement bool   `json:"needsLegalAgreement"`
	LegalURL            string `json:"legalUrl,omitempty"`
}

// UploadFiles is what an upload of a mod sends and leaves out.
type UploadFiles struct {
	Count int      `json:"count"` // the files sent
	Bytes int64    `json:"bytes"` // their size
	Left  []string `json:"left"`  // the files left out, mod-relative with forward slashes
}

// SteamCompare is a mod in the workspace against the copy Steam has: the snapshot PMT kept at its
// last upload, or a download from Steam.
type SteamCompare struct {
	Item       string             `json:"item"`       // the Workshop id; "" when the mod is not published
	From       string             `json:"from"`       // "snapshot", "download", or "" with neither
	Version    string             `json:"version"`    // the version in Steam's copy of the descriptor
	UploadedAt string             `json:"uploadedAt"` // RFC 3339
	Title      string             `json:"title"`
	Root       string             `json:"root"` // Steam's copy of the mod's files
	Listing    []ListingChange    `json:"listing"`
	Files      []steam.FileChange `json:"files"`
	Changes    int                `json:"changes"` // the listing fields and files an upload changes
	// Steam's extra images and videos the mod does not keep: an upload removes them when the mod keeps
	// extras of its own, and leaves them when it keeps none
	OnlyOnSteam []string `json:"onlyOnSteam"`
}

// ListingChange is one field of the listing as Steam has it and as an upload would send it.
type ListingChange struct {
	Field     string   `json:"field"`
	Steam     string   `json:"steam"`
	Workspace string   `json:"workspace"`
	Changed   bool     `json:"changed"`
	Added     []string `json:"added,omitempty"`   // of a list field (the tags): what an upload adds
	Removed   []string `json:"removed,omitempty"` // and what it takes away
	// the lines a change adds and removes (steam.LineCounts)
	LinesAdded   int `json:"linesAdded"`
	LinesRemoved int `json:"linesRemoved"`
}

// snapshotPage is a snapshot's listing.json: the item's page beside the files Steam has.
type snapshotPage struct {
	From        string        `json:"from"` // snapshot or download
	Title       string        `json:"title"`
	Description string        `json:"description"` // BBCode
	At          string        `json:"at"`          // uploaded, or Steam's last update (RFC 3339)
	Extras      []steam.Extra `json:"extras,omitempty"`
}

// UploadFiles walks a mod as its upload copy does, by its saved ignore text.
func (s *ReleaseService) UploadFiles(workspaceID, modID string) (*UploadFiles, error) {
	_, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	sent, skipped := steam.UploadFiles(mod.Path, mod.WorkshopIgnore)
	files := &UploadFiles{Left: append([]string{}, skipped...)}
	for _, rel := range sent {
		if info, err := os.Stat(filepath.Join(mod.Path, filepath.FromSlash(rel))); err == nil {
			files.Count++
			files.Bytes += info.Size()
		}
	}
	return files, nil
}

// EventSteamProgress is a publish's upload moving: data is steam.Progress.
const EventSteamProgress = "steam:progress"

// registers the event so the bindings generator types it
func init() { application.RegisterEvent[steam.Progress](EventSteamProgress) }

// SteamStatus says why Steam cannot take an upload for a workspace's game now (not running, no account that owns
// the game signed in, offline); nil when it can. It starts Steam's API in the publish's child, as an upload does.
func (s *ReleaseService) SteamStatus(workspaceID string) error {
	library, appID, err := s.steamLibrary(workspaceID)
	if err != nil {
		return err
	}
	_, err = steam.Run(library, steam.Request{AppID: appID, Status: true}, nil)
	return err
}

// ExportZip writes a workspace mod's upload copy (its ignore text applied) as a zip at dest, for Paradox Mods.
func (s *ReleaseService) ExportZip(workspaceID, modID, dest string) error {
	_, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return err
	}
	return steam.ZipMod(mod.Path, dest, mod.WorkshopIgnore)
}

// LoadListing reads a workspace mod's descriptor and description, writing nothing.
func (s *ReleaseService) LoadListing(workspaceID, modID string) (*Listing, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	return s.loadFrom(ws, mod)
}

// ListingSummary reads a mod's Workshop id, listing image and description source, writing nothing.
func (s *ReleaseService) ListingSummary(workspaceID, modID string) (*ListingSummary, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	fields, err := modfiles.LoadDescriptor(ws.GameID, mod.Path)
	if err != nil {
		return nil, err
	}
	return summarize(ws.GameID, mod, fields), nil
}

// SaveListing writes the descriptor, the description to its source's file where the mod has one,
// and the changelog.
func (s *ReleaseService) SaveListing(in Listing) error {
	ws, mod, err := findMod(s.config, in.WorkspaceID, in.ModID)
	if err != nil {
		return err
	}
	root := mod.Path
	fields := modfiles.ListingFields{
		Name: in.Name, Version: in.Version, SupportedVersion: in.SupportedVersion,
		Tags: in.Tags, RemoteFileID: in.RemoteFileID, Picture: picture(in.ThumbnailRel),
		ShortDescription: cmp.Or(in.ShortDescription, firstParagraph(in.ReadmeMd)),
	}
	if err := modfiles.WriteDescriptor(ws.GameID, root, fields); err != nil {
		return err
	}
	if err := writeDescription(mod, in); err != nil {
		return err
	}
	// the changelog holds the latest change note; no note, no file
	changelog := filepath.Join(root, filepath.FromSlash(steam.ChangelogName))
	if in.ChangeNote != "" {
		return appdata.WriteBytes(changelog, []byte(in.ChangeNote))
	}
	if err := os.Remove(changelog); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// writeDescription writes the listing's description to the file of its source, where the mod has that file.
func writeDescription(mod *WorkspaceMod, in Listing) error {
	source, rel := description(mod)
	file := filepath.Join(mod.Path, filepath.FromSlash(rel))
	if rel == "" || !exists(file) {
		return nil
	}
	text := in.ReadmeMd
	if source == steam.FromBBCode {
		text = in.ReadmeBbcode
	}
	return appdata.WriteBytes(file, []byte(text))
}

// CreateDescriptionFile writes the mod's missing description file from its name and short
// description, in its source's markup.
func (s *ReleaseService) CreateDescriptionFile(workspaceID, modID string) error {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return err
	}
	source, rel := description(mod)
	if rel == "" {
		return errors.New("steam's own text has no file to create")
	}
	fields, err := modfiles.LoadDescriptor(ws.GameID, mod.Path)
	if err != nil {
		return err
	}
	return steam.NewDescription(mod.Path, rel, source, cmp.Or(fields.Name, mod.Name), fields.ShortDescription)
}

// SetListingLink writes the descriptor's Workshop id and, where thumbnailRel names one, its picture,
// and nothing else of the listing; it returns the summary they make.
func (s *ReleaseService) SetListingLink(workspaceID, modID, remoteFileID, thumbnailRel string) (*ListingSummary, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	fields, err := modfiles.LoadDescriptor(ws.GameID, mod.Path)
	if err != nil {
		return nil, err
	}
	fields.RemoteFileID, fields.Picture = remoteFileID, cmp.Or(picture(thumbnailRel), fields.Picture)
	if err := modfiles.WriteDescriptor(ws.GameID, mod.Path, fields); err != nil {
		return nil, err
	}
	return summarize(ws.GameID, mod, fields), nil
}

// Convert maps listing markup: the text as BBCode where markup is "bbcode", else as Markdown.
func (s *ReleaseService) Convert(text, markup string) steam.Converted {
	if markup == "bbcode" {
		return steam.MarkdownToBBCode(text)
	}
	return steam.BBCodeToMarkdown(text)
}

// editWorkshop applies one edit to the mod's Workshop extras.
func (s *ReleaseService) editWorkshop(workspaceID, modID string, edit func(modRoot string) error) error {
	_, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return err
	}
	return edit(mod.Path)
}

// AddWorkshopPreview copies an image into workshop/previews (Steam extra cap).
func (s *ReleaseService) AddWorkshopPreview(workspaceID, modID, srcPath string) error {
	return s.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.AddPreview(modRoot, srcPath)
	})
}

// RemoveWorkshopPreview deletes one extra image under workshop/previews.
func (s *ReleaseService) RemoveWorkshopPreview(workspaceID, modID, rel string) error {
	return s.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.RemovePreview(modRoot, rel)
	})
}

// AddWorkshopVideo appends a YouTube id from a watch/embed/youtu.be URL or raw id.
func (s *ReleaseService) AddWorkshopVideo(workspaceID, modID, urlOrID string) error {
	return s.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.AddVideo(modRoot, urlOrID)
	})
}

// RemoveWorkshopVideo drops one YouTube id from workshop/previews/videos.txt.
func (s *ReleaseService) RemoveWorkshopVideo(workspaceID, modID, id string) error {
	return s.editWorkshop(workspaceID, modID, func(modRoot string) error {
		return steam.RemoveVideo(modRoot, id)
	})
}

// PublishSteam publishes the mod, as the workspace has it saved, to the Workshop, saves the item's
// id, and keeps what it sent as the item's snapshot.
func (s *ReleaseService) PublishSteam(workspaceID, modID string) (*PublishResult, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	listing, err := s.loadFrom(ws, mod)
	if err != nil {
		return nil, err
	}
	slog.Info("workshop publish start", "mod", listing.Name, "id", listing.RemoteFileID)
	text, sent := sentDescription(mod, listing)
	if _, rel := description(mod); sent && !exists(filepath.Join(mod.Path, filepath.FromSlash(rel))) {
		return nil, fmt.Errorf("the description file %s is missing: create it, or keep Steam's own text", rel)
	}
	library, appID, err := s.steamLibrary(ws.ID)
	if err != nil {
		return nil, err
	}

	work, err := newWork()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	stage := filepath.Join(work, "files")
	if err := steam.CopyMod(listing.Root, stage, mod.WorkshopIgnore); err != nil {
		return nil, err
	}
	resp, err := steam.Run(library, steam.Request{
		AppID: appID, Folder: stage,
		Title: listing.Name, Description: text, KeepDescription: !sent,
		Preview: stagedPreview(stage, listing), PublishedFileID: listing.RemoteFileID,
		ChangeNote:    listing.ChangeNote,
		ExtraPreviews: steam.PreviewFiles(listing.Previews),
		ExtraVideos:   steam.VideoIDs(listing.Previews),
		KeepExtras:    len(listing.Previews) == 0,
	}, func(moved steam.Progress) { emit(EventSteamProgress, moved) })
	if err != nil {
		return nil, err
	}

	if err := keepSnapshot(work, resp.PublishedFileID, uploadedPage(listing, text, sent, resp.PublishedFileID)); err != nil {
		slog.Warn("workshop snapshot not kept", "id", resp.PublishedFileID, "error", err)
	}
	if _, err := s.SetListingLink(workspaceID, modID, resp.PublishedFileID, listing.ThumbnailRel); err != nil {
		return nil, fmt.Errorf("save the workshop id after publish: %w", err)
	}
	slog.Info("workshop publish done", "mod", listing.Name, "id", resp.PublishedFileID, "legalAgreement", resp.NeedsLegalAgreement)
	result := &PublishResult{PublishedFileID: resp.PublishedFileID, NeedsLegalAgreement: resp.NeedsLegalAgreement}
	if resp.NeedsLegalAgreement {
		result.LegalURL = steamLegalURL
	}
	return result, nil
}

// stagedPreview is the listing image's file in the upload copy; "" where the listing has none or the copy lacks it.
func stagedPreview(stage string, listing *Listing) string {
	if listing.ThumbnailRel == "" {
		return ""
	}
	staged := filepath.Join(stage, filepath.FromSlash(listing.ThumbnailRel))
	if !exists(staged) {
		return ""
	}
	return staged
}

// uploadedPage is the snapshot page of an upload: what it sent, and where the upload left Steam's own description
// or extras as they were, what the last copy of the item says Steam had.
func uploadedPage(listing *Listing, text string, sent bool, item string) snapshotPage {
	page := snapshotPage{
		From: "snapshot", Title: listing.Name, Description: text, At: time.Now().UTC().Format(time.RFC3339),
		Extras: steam.ExtrasOf(listing.Previews),
	}
	if _, kept := loadSnapshot(item); kept != nil {
		if !sent {
			page.Description = kept.Description
		}
		if len(listing.Previews) == 0 {
			page.Extras = kept.Extras
		}
	}
	return page
}

// CompareWithSteam compares the mod with Steam's copy by its snapshot alone: it never downloads.
func (s *ReleaseService) CompareWithSteam(workspaceID, modID string) (*SteamCompare, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	listing, err := s.loadFrom(ws, mod)
	if err != nil {
		return nil, err
	}
	out := &SteamCompare{Item: listing.RemoteFileID, Listing: []ListingChange{}, Files: []steam.FileChange{}, OnlyOnSteam: []string{}}
	dir, page := loadSnapshot(listing.RemoteFileID)
	if page == nil {
		return out, nil
	}
	out.From, out.UploadedAt, out.Title, out.Root = page.From, page.At, page.Title, filepath.Join(dir, "files")
	theirs, err := modfiles.LoadDescriptor(ws.GameID, out.Root)
	if err != nil {
		return nil, err
	}
	out.Version = theirs.Version
	out.Files = steam.Changes(out.Root, mod.Path, mod.WorkshopIgnore)

	changed := map[string]bool{}
	for _, file := range out.Files {
		changed[file.Rel] = true
	}
	text, sent := sentDescription(mod, listing)
	if !sent {
		text = page.Description
	}
	thumbnail, _ := modfiles.Thumbnail(ws.GameID, out.Root, theirs.Picture)
	theirExtras, ourExtras := extraLabels(page.Extras), extraLabels(steam.ExtrasOf(listing.Previews))
	out.OnlyOnSteam = append(out.OnlyOnSteam, without(theirExtras, ourExtras)...)
	// a mod with no extras of its own leaves Steam's as they are
	if len(ourExtras) == 0 {
		ourExtras = theirExtras
	}
	// a field whose value reads the same still changes with its file
	out.Listing = []ListingChange{
		{Field: "Title", Steam: page.Title, Workspace: listing.Name},
		{Field: "Description", Steam: page.Description, Workspace: text},
		{Field: "Tags", Steam: strings.Join(theirs.Tags, ", "), Workspace: strings.Join(listing.Tags, ", "),
			Added: without(listing.Tags, theirs.Tags), Removed: without(theirs.Tags, listing.Tags)},
		{Field: "Version", Steam: theirs.Version, Workspace: listing.Version},
		{Field: "Thumbnail", Steam: thumbnail, Workspace: listing.ThumbnailRel, Changed: changed[listing.ThumbnailRel]},
		{Field: "Screenshots and video", Steam: strings.Join(theirExtras, ", "), Workspace: strings.Join(ourExtras, ", "),
			Changed: slices.ContainsFunc(listing.Previews, func(extra steam.Preview) bool { return changed[extra.Rel] })},
	}
	out.Changes = len(out.Files) + countChanges(out.Listing)
	return out, nil
}

// extraLabels is the names the page's extra images and videos are listed by.
func extraLabels(extras []steam.Extra) []string {
	names := []string{}
	for _, extra := range extras {
		names = append(names, extra.Label())
	}
	return names
}

// without is the tags that others lack, in order.
func without(tags, others []string) []string {
	return slices.DeleteFunc(slices.Clone(tags), func(tag string) bool { return slices.Contains(others, tag) })
}

// countChanges marks each row that changed, by its flag or by its two sides reading otherwise, with the lines it
// adds and removes, and counts them.
func countChanges(rows []ListingChange) int {
	count := 0
	for i := range rows {
		row := &rows[i]
		if row.Changed = row.Changed || row.Steam != row.Workspace; row.Changed {
			count++
			row.LinesAdded, row.LinesRemoved = steam.LineCounts(row.Steam, row.Workspace)
		}
	}
	return count
}

// DownloadFromSteam replaces the mod's snapshot with Steam's own copy of the item, then compares.
func (s *ReleaseService) DownloadFromSteam(workspaceID, modID string) (*SteamCompare, error) {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return nil, err
	}
	item, resp, err := s.fetchItem(ws, mod, true)
	if err != nil {
		return nil, err
	}
	work, err := newWork()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := steam.CopyMod(resp.Folder, filepath.Join(work, "files"), ""); err != nil {
		return nil, err
	}
	page := snapshotPage{
		From: "download", Title: resp.Title, Description: resp.Description,
		At: time.Unix(resp.Updated, 0).UTC().Format(time.RFC3339), Extras: resp.Extras,
	}
	if err := keepSnapshot(work, item, page); err != nil {
		return nil, err
	}
	return s.CompareWithSteam(workspaceID, modID)
}

// ImportSteamExtras brings the extra images and videos the mod's Workshop item has and the mod lacks into
// its workshop folder, so an upload keeps them.
func (s *ReleaseService) ImportSteamExtras(workspaceID, modID string) error {
	ws, mod, err := findMod(s.config, workspaceID, modID)
	if err != nil {
		return err
	}
	_, resp, err := s.fetchItem(ws, mod, false)
	if err != nil {
		return err
	}
	return steam.SaveExtras(mod.Path, resp.Extras)
}

// fetchItem asks Steam for the mod's Workshop item: its page, and with files its files downloaded; it
// returns the item's id.
func (s *ReleaseService) fetchItem(ws *Workspace, mod *WorkspaceMod, files bool) (string, steam.Response, error) {
	fields, err := modfiles.LoadDescriptor(ws.GameID, mod.Path)
	if err != nil {
		return "", steam.Response{}, err
	}
	if fields.RemoteFileID == "" {
		return "", steam.Response{}, fmt.Errorf("%s is not on the Workshop", mod.Name)
	}
	library, appID, err := s.steamLibrary(ws.ID)
	if err != nil {
		return "", steam.Response{}, err
	}
	resp, err := steam.Run(library, steam.Request{AppID: appID, PublishedFileID: fields.RemoteFileID, Download: true, PageOnly: !files}, nil)
	return fields.RemoteFileID, resp, err
}

// steamLibrary is where the workspace's install keeps Steam's library, which the publishing child
// loads, and the game's app id.
func (s *ReleaseService) steamLibrary(workspaceID string) (string, uint32, error) {
	ws, inst, err := s.config.workspace(workspaceID)
	if err != nil {
		return "", 0, err
	}
	info := game.Get(ws.GameID)
	if info == nil || info.SteamAppID == 0 {
		return "", 0, fmt.Errorf("no Steam App ID for game %s", ws.GameID)
	}
	installPath := ""
	if inst != nil {
		installPath = inst.Path
	}
	library, err := steam.Library(info, installPath)
	return library, uint32(info.SteamAppID), err
}

// loadFrom reads a mod's listing: its descriptor, its description in its source's file, its changelog and its previews.
func (s *ReleaseService) loadFrom(ws *Workspace, mod *WorkspaceMod) (*Listing, error) {
	root := mod.Path
	fields, err := modfiles.LoadDescriptor(ws.GameID, root)
	if err != nil {
		return nil, err
	}
	fields.Name = cmp.Or(fields.Name, mod.Name)
	summary := summarize(ws.GameID, mod, fields)
	// a description file that is not there reads as no text
	text := ""
	if summary.DescRel != "" {
		raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(summary.DescRel)))
		text = string(raw)
	}
	markdown, bbcode := text, ""
	if summary.DescSource == steam.FromBBCode {
		markdown, bbcode = "", text
	}
	// a changelog that is not there reads as no note
	note, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(steam.ChangelogName)))
	workshopURL := ""
	if fields.RemoteFileID != "" {
		workshopURL = steamWorkshopURL + fields.RemoteFileID
	}
	saved := ""
	if info, err := os.Stat(game.DescriptorPath(ws.GameID, root)); err == nil {
		saved = info.ModTime().UTC().Format(time.RFC3339)
	}
	return &Listing{
		WorkspaceID: ws.ID, ModID: mod.ID,
		Root: root, Name: fields.Name, Version: fields.Version,
		SupportedVersion: fields.SupportedVersion, Tags: fields.Tags,
		ThumbnailRel: summary.ThumbnailRel, ThumbnailAbs: summary.ThumbnailAbs,
		Previews: steam.ScanPreviews(root), RemoteFileID: fields.RemoteFileID,
		ShortDescription: fields.ShortDescription,
		ReadmeMd:         markdown, ReadmeBbcode: bbcode, ChangeNote: string(note),
		DescMdRel: cmp.Or(mod.DescMdRel, steam.DescMdName), DescBbRel: cmp.Or(mod.DescBbRel, steam.DescBbName),
		WorkshopURL: workshopURL,
		Descriptor:  game.DescriptorFile(ws.GameID), Saved: saved, LegalURL: steamLegalURL,
	}, nil
}

// summarize is what a mod's descriptor fields and folder say of its Workshop id, its listing image
// and its description's file.
func summarize(gameID string, mod *WorkspaceMod, fields modfiles.ListingFields) *ListingSummary {
	summary := &ListingSummary{RemoteFileID: fields.RemoteFileID}
	if rel, known := modfiles.Thumbnail(gameID, mod.Path, fields.Picture); known {
		summary.ThumbnailRel, summary.ThumbnailAbs = rel, filepath.Join(mod.Path, filepath.FromSlash(rel))
	}
	summary.DescSource, summary.DescRel = description(mod)
	summary.DescExists = summary.DescRel != "" && exists(filepath.Join(mod.Path, filepath.FromSlash(summary.DescRel)))
	if dir, page := loadSnapshot(fields.RemoteFileID); page != nil {
		theirs, _ := modfiles.LoadDescriptor(gameID, filepath.Join(dir, "files"))
		summary.SteamVersion = theirs.Version
	}
	return summary
}

// description is where the mod's Workshop description comes from, and its file relative to
// the mod ("" for Steam's own text). With none chosen, it is the file the mod has, Markdown first,
// else Steam's own text, so a mod with neither file has nothing written for it.
func description(mod *WorkspaceMod) (source, rel string) {
	files := map[string]string{
		steam.FromMarkdown: cmp.Or(mod.DescMdRel, steam.DescMdName),
		steam.FromBBCode:   cmp.Or(mod.DescBbRel, steam.DescBbName),
	}
	if mod.DescSource != "" {
		return mod.DescSource, files[mod.DescSource]
	}
	for _, source := range []string{steam.FromMarkdown, steam.FromBBCode} {
		if exists(filepath.Join(mod.Path, filepath.FromSlash(files[source]))) {
			return source, files[source]
		}
	}
	return steam.FromSteam, ""
}

// sentDescription is the BBCode an upload of the listing sends, or false where the upload leaves
// Steam's own text.
func sentDescription(mod *WorkspaceMod, listing *Listing) (string, bool) {
	switch source, _ := description(mod); source {
	case steam.FromMarkdown:
		return steam.MarkdownToBBCode(listing.ReadmeMd).Text, true
	case steam.FromBBCode:
		return listing.ReadmeBbcode, true
	}
	return "", false
}

// newWork is a folder for one upload or download beside the snapshots, so that it can become one.
func newWork() (string, error) {
	cache, err := appdata.CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "steam")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "work-")
}

// snapshotDir is where the Workshop item's snapshot is kept: the files Steam has under files/,
// and listing.json beside them.
func snapshotDir(item string) (string, error) {
	if _, err := strconv.ParseUint(item, 10, 64); err != nil {
		return "", fmt.Errorf("not a workshop item id: %q", item)
	}
	cache, err := appdata.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "steam", item), nil
}

// keepSnapshot makes work, whose files/ is now the item on Steam, the item's snapshot, replacing
// the last one.
func keepSnapshot(work, item string, page snapshotPage) error {
	dir, err := snapshotDir(item)
	if err != nil {
		return err
	}
	data, err := json.Marshal(page)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "listing.json"), data, 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(work, dir)
}

// loadSnapshot is the item's snapshot folder and page, or no page when there is none.
func loadSnapshot(item string) (string, *snapshotPage) {
	dir, err := snapshotDir(item)
	if err != nil {
		return "", nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, "listing.json"))
	var page snapshotPage
	if err != nil || json.Unmarshal(raw, &page) != nil {
		return "", nil
	}
	return dir, &page
}

// picture is the descriptor's picture for a listing image at rel: its file name, "" for none.
func picture(rel string) string {
	if rel == "" {
		return ""
	}
	return path.Base(rel)
}

// exists is whether a file or folder is at the path.
func exists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
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
