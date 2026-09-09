// releaseservice.go is the Wails Release tool: listing I/O, convert, Steam publish.

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/modfile"
	"paradox-modding-tools/services/internal/release"
	"paradox-modding-tools/services/internal/steamugc"
	"paradox-modding-tools/services/internal/steamugc/binembed"
)

const (
	steamLegalURL    = "https://steamcommunity.com/workshop/workshoplegalagreement"
	steamWorkshopURL = "https://steamcommunity.com/sharedfiles/filedetails/?id="
	steamHelperTO    = 15 * time.Minute
)

// steamPublishHints maps Steam EResult codes to user-facing publish guidance.
var steamPublishHints = map[int]string{
	9: "steam workshop item was not found (EResult 9). if you deleted it, unlink the workshop id and publish again to create a new private item",
	25: "steam workshop limit exceeded (EResult 25). extra preview images must be under 1 MB each " +
		"(png/jpg/gif/webp). shrink files in workshop/previews, wait a few minutes if you hit a rate " +
		"limit, then publish again",
}

// ReleaseService loads and saves mod listing files and execs pmt-steamugc.
type ReleaseService struct {
	Store *Store
}

// Listing is the Release page payload. Display fields are on the DTO.
type Listing struct {
	WorkspaceID      string            `json:"workspaceId"`
	ModID            string            `json:"modId"`
	GameID           string            `json:"gameId"`
	Root             string            `json:"root"`
	Rel              string            `json:"rel"`
	Origin           string            `json:"origin"`
	OriginName       string            `json:"originName"`
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	SupportedVersion string            `json:"supportedVersion"`
	Tags             []string          `json:"tags"`
	ThumbnailRel     string            `json:"thumbnailRel"`
	ThumbnailAbs     string            `json:"thumbnailAbs"`
	Previews         []WorkshopPreview `json:"previews"`
	RemoteFileID     string            `json:"remoteFileId"`
	ShortDescription string            `json:"shortDescription"`
	ReadmeMd         string            `json:"readmeMd"`
	ReadmeBbcode     string            `json:"readmeBbcode"`
	ChangeNote       string            `json:"changeNote"`
	SteamAppID       int               `json:"steamAppId"`
	DescMdRel        string            `json:"descMdRel"`
	DescBbRel        string            `json:"descBbRel"`
	WorkshopIgnore   string            `json:"workshopIgnore"`
	WorkshopURL      string            `json:"workshopUrl"`
}

// WorkshopPreview is one extra Workshop image or YouTube id on the listing.
type WorkshopPreview struct {
	Rel  string `json:"rel"`
	Abs  string `json:"abs"`
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ConvertResult is Markdown → Workshop BBCode (or the inverse for empty MD).
type ConvertResult struct {
	Text  string   `json:"text"`
	Notes []string `json:"notes,omitempty"`
}

// PublishResult is the Steam helper outcome plus the saved listing on success.
type PublishResult struct {
	PublishedFileID     string   `json:"publishedFileId"`
	NeedsLegalAgreement bool     `json:"needsLegalAgreement"`
	LegalURL            string   `json:"legalUrl,omitempty"`
	Listing             *Listing `json:"listing,omitempty"`
}

// LoadListing reads descriptor + readmes for a workspace mod.
func (r *ReleaseService) LoadListing(workspaceID, modID string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// SaveListing writes descriptor, description files, optional thumb, and changelog.
func (r *ReleaseService) SaveListing(in Listing) (*Listing, error) {
	ws, mod, err := r.workspaceMod(in.WorkspaceID, in.ModID)
	if err != nil {
		return nil, err
	}
	root := mod.Path
	mdRel, bbRel, mdAbs, bbAbs, err := descFiles(mod)
	if err != nil {
		return nil, err
	}
	if mdRel == modfile.DescMdName && bbRel == modfile.DescBbName {
		if err := modfile.EnsureDescriptions(root, in.Name, in.ShortDescription); err != nil {
			return nil, err
		}
	}
	fields := modfile.ListingFields{
		Name: in.Name, Version: in.Version, SupportedVersion: in.SupportedVersion,
		Tags: in.Tags, RemoteFileID: in.RemoteFileID, Picture: filepath.Base(in.ThumbnailRel),
		ShortDescription: in.ShortDescription,
	}
	if fields.ShortDescription == "" {
		fields.ShortDescription = modfile.FirstParagraph(in.ReadmeMd)
	}
	if err := modfile.WriteListingFields(ws.GameID, root, fields); err != nil {
		return nil, err
	}
	if err := os.WriteFile(mdAbs, []byte(in.ReadmeMd), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(bbAbs, []byte(in.ReadmeBbcode), 0o644); err != nil {
		return nil, err
	}
	if in.ChangeNote != "" && in.Version != "" {
		dir := filepath.Join(root, "changelog")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		err := os.WriteFile(filepath.Join(dir, in.Version+".bbcode"), []byte(in.ChangeNote), 0o644)
		if err != nil {
			return nil, err
		}
	}
	return r.loadFrom(ws, mod)
}

// Convert maps listing markup. UI writes Markdown and asks for "bbcode".
func (r *ReleaseService) Convert(src, to string) ConvertResult {
	switch to {
	case "bbcode":
		out := release.MarkdownToBBCode(src)
		return ConvertResult{Text: out.Text, Notes: out.Notes}
	default:
		out := release.BBCodeToMarkdown(src)
		return ConvertResult{Text: out.Text, Notes: out.Notes}
	}
}

// AddWorkshopPreview copies an image into workshop/previews (Steam extra cap).
func (r *ReleaseService) AddWorkshopPreview(workspaceID, modID, srcPath string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	if err := release.AddPreview(mod.Path, srcPath); err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// RemoveWorkshopPreview deletes one extra image under workshop/previews.
func (r *ReleaseService) RemoveWorkshopPreview(workspaceID, modID, rel string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	if err := release.RemovePreview(mod.Path, rel); err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// AddWorkshopVideo appends a YouTube id from a watch/embed/youtu.be URL or raw id.
func (r *ReleaseService) AddWorkshopVideo(workspaceID, modID, urlOrID string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	if err := release.AddVideo(mod.Path, urlOrID); err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// RemoveWorkshopVideo drops one YouTube id from workshop/videos.txt.
func (r *ReleaseService) RemoveWorkshopVideo(workspaceID, modID, id string) (*Listing, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return nil, err
	}
	if err := release.RemoveVideo(mod.Path, id); err != nil {
		return nil, err
	}
	return r.loadFrom(ws, mod)
}

// DraftChangelog returns a version stub for the Steam change note field.
func (r *ReleaseService) DraftChangelog(workspaceID, modID, version string) (string, error) {
	_, _, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return "", err
	}
	if version == "" {
		version = "0.0.0"
	}
	return "Version " + version + "\n", nil
}

// BumpVersion returns a bumped descriptor version (kind: patch|minor).
func (r *ReleaseService) BumpVersion(current, kind string) string {
	switch kind {
	case "minor":
		return modfile.BumpMinor(current)
	default:
		return modfile.BumpPatch(current)
	}
}

// CopyThumbnail replaces the mod's listing image with src and returns the path
// it wrote, relative to the mod root.
//
// It replaces in place: a mod that already has a thumbnail keeps where it put
// it. Writing to the game's convention instead left Victoria 3 and EU5 mods with
// two thumbnail files and the descriptor naming the stale one.
func (r *ReleaseService) CopyThumbnail(workspaceID, modID, src string) (string, error) {
	ws, mod, err := r.workspaceMod(workspaceID, modID)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(src))
	if !slices.Contains(modfile.ThumbnailExts, ext) {
		return "", fmt.Errorf("thumbnail must be one of %s",
			strings.Join(modfile.ThumbnailExts, ", "))
	}
	rel, existed := modfile.ThumbnailPath(ws.GameID, mod.Path, ext)
	// Same directory, new extension: drop the file being superseded rather than
	// leaving two for listingThumb to choose between.
	if existed && !strings.EqualFold(path.Ext(rel), ext) {
		_ = os.Remove(filepath.Join(mod.Path, filepath.FromSlash(rel)))
		rel = rel[:len(rel)-len(path.Ext(rel))] + ext
	}
	dest := filepath.Join(mod.Path, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// PublishSteam saves the listing, publishes to Steam, then saves again with the workshop id.
func (r *ReleaseService) PublishSteam(in Listing) (*PublishResult, error) {
	if _, err := r.SaveListing(in); err != nil {
		return nil, fmt.Errorf("save listing: %w", err)
	}
	listing, err := r.LoadListing(in.WorkspaceID, in.ModID)
	if err != nil {
		return nil, err
	}
	if listing.SteamAppID == 0 {
		return nil, fmt.Errorf("no Steam App ID for game %s", listing.GameID)
	}
	helper, lib, err := ensureSteamHelper()
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "pmt-steam-stage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	if err := release.CopyMod(listing.Root, stage, listing.WorkshopIgnore); err != nil {
		return nil, err
	}
	cwd, err := os.MkdirTemp("", "pmt-steam-cwd-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(cwd)
	if err := steamugc.WriteAppID(cwd, uint32(listing.SteamAppID)); err != nil {
		return nil, err
	}
	preview := ""
	if listing.ThumbnailRel != "" {
		p := filepath.Join(stage, filepath.FromSlash(listing.ThumbnailRel))
		if _, err := os.Stat(p); err == nil {
			preview = p
		}
	}
	req := steamugc.Request{
		AppID: uint32(listing.SteamAppID), Folder: stage,
		Title: listing.Name, Description: listing.ReadmeBbcode,
		Preview: preview, PublishedFileID: listing.RemoteFileID,
		ChangeNote: in.ChangeNote, Visibility: 2,
		ExtraPreviews: release.StagedPreviewFiles(stage, releasePreviews(listing.Previews)),
		ExtraVideos:   release.VideoIDs(releasePreviews(listing.Previews)),
	}
	raw, _ := json.Marshal(req)
	ctx, cancel := context.WithTimeout(context.Background(), steamHelperTO)
	defer cancel()
	cmd := exec.CommandContext(ctx, helper)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "PMT_STEAM_API="+lib)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	var resp steamugc.Response
	if len(out) > 0 {
		_ = json.Unmarshal(out, &resp)
	}
	if err != nil && resp.Error == "" {
		if ee, ok := err.(*exec.ExitError); ok {
			resp.Error = strings.TrimSpace(string(ee.Stderr))
		}
		if resp.Error == "" {
			resp.Error = err.Error()
		}
	}
	if resp.Error != "" {
		if msg := steamPublishError(resp.Error); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("%s", resp.Error)
	}
	if resp.PublishedFileID == "" {
		return nil, fmt.Errorf("steam helper returned no publishedFileId")
	}
	listing.RemoteFileID = resp.PublishedFileID
	saved, err := r.SaveListing(*listing)
	if err != nil {
		return nil, fmt.Errorf("save listing after publish: %w", err)
	}
	result := &PublishResult{
		PublishedFileID:     resp.PublishedFileID,
		NeedsLegalAgreement: resp.NeedsLegalAgreement,
		Listing:             saved,
	}
	if resp.NeedsLegalAgreement {
		result.LegalURL = steamLegalURL
	}
	return result, nil
}

func (r *ReleaseService) workspaceMod(workspaceID, modID string) (*Workspace, *WorkspaceMod, error) {
	var ws Workspace
	var mod WorkspaceMod
	var foundWS, foundMod bool
	r.Store.Read(func(c *Config) {
		w := findWorkspace(c, workspaceID)
		if w == nil {
			return
		}
		ws = *w
		foundWS = true
		for i := range w.Mods {
			if w.Mods[i].ID == modID {
				mod = w.Mods[i]
				foundMod = true
				return
			}
		}
	})
	if !foundWS {
		return nil, nil, fmt.Errorf("workspace not found")
	}
	if !foundMod {
		return nil, nil, fmt.Errorf("mod not found")
	}
	if mod.Path == "" {
		return nil, nil, fmt.Errorf("mod has no path")
	}
	return &ws, &mod, nil
}

func (r *ReleaseService) loadFrom(ws *Workspace, mod *WorkspaceMod) (*Listing, error) {
	root := mod.Path
	fields, err := modfile.ReadListingFields(ws.GameID, root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if fields.Name == "" {
		fields.Name = mod.Name
	}
	mdRel, bbRel, mdAbs, bbAbs, err := descFiles(mod)
	if err != nil {
		return nil, err
	}
	if mdRel == modfile.DescMdName && bbRel == modfile.DescBbName {
		if err := modfile.EnsureDescriptions(root, fields.Name, fields.ShortDescription); err != nil {
			return nil, err
		}
	}
	md, err := os.ReadFile(mdAbs)
	if err != nil {
		return nil, err
	}
	bb, _ := os.ReadFile(bbAbs)
	note := ""
	if fields.Version != "" {
		if b, err := os.ReadFile(filepath.Join(root, "changelog", fields.Version+".bbcode")); err == nil {
			note = string(b)
		}
	}
	rel, thumbAbs := listingThumb(ws.GameID, root, fields.Picture)
	info := game.Get(ws.GameID)
	appID := 0
	if info != nil {
		appID = info.SteamAppID
	}
	workshopURL := ""
	if fields.RemoteFileID != "" {
		workshopURL = steamWorkshopURL + fields.RemoteFileID
	}
	return &Listing{
		WorkspaceID: ws.ID, ModID: mod.ID, GameID: ws.GameID,
		Root: root, Rel: filepath.Base(root), Origin: mod.ID, OriginName: mod.Name,
		Name: fields.Name, Version: fields.Version,
		SupportedVersion: fields.SupportedVersion, Tags: fields.Tags,
		ThumbnailRel: rel, ThumbnailAbs: thumbAbs,
		Previews: workshopPreviews(root), RemoteFileID: fields.RemoteFileID,
		ShortDescription: fields.ShortDescription,
		ReadmeMd:         string(md), ReadmeBbcode: string(bb), ChangeNote: note,
		SteamAppID: appID, DescMdRel: mdRel, DescBbRel: bbRel,
		WorkshopIgnore: mod.WorkshopIgnore, WorkshopURL: workshopURL,
	}, nil
}

func descFiles(mod *WorkspaceMod) (mdRel, bbRel, mdAbs, bbAbs string, err error) {
	mdRel = strings.TrimSpace(mod.DescMdRel)
	if mdRel == "" {
		mdRel = modfile.DescMdName
	}
	bbRel = strings.TrimSpace(mod.DescBbRel)
	if bbRel == "" {
		bbRel = modfile.DescBbName
	}
	mdAbs = filepath.Join(mod.Path, filepath.FromSlash(mdRel))
	bbAbs = filepath.Join(mod.Path, filepath.FromSlash(bbRel))
	custom := mdRel != modfile.DescMdName || bbRel != modfile.DescBbName
	if custom {
		if _, err := os.Stat(mdAbs); err != nil {
			return "", "", "", "", fmt.Errorf("description markdown: %w", err)
		}
		if _, err := os.Stat(bbAbs); err != nil {
			return "", "", "", "", fmt.Errorf("description bbcode: %w", err)
		}
	}
	return mdRel, bbRel, mdAbs, bbAbs, nil
}

func workshopPreviews(root string) []WorkshopPreview {
	scanned := release.ScanPreviews(root)
	out := make([]WorkshopPreview, len(scanned))
	for i, p := range scanned {
		out[i] = WorkshopPreview{Rel: p.Rel, Abs: p.Abs, Kind: p.Kind, ID: p.ID}
	}
	return out
}

func releasePreviews(previews []WorkshopPreview) []release.Preview {
	out := make([]release.Preview, len(previews))
	for i, p := range previews {
		out[i] = release.Preview{Rel: p.Rel, Abs: p.Abs, Kind: p.Kind, ID: p.ID}
	}
	return out
}

// listingThumb locates the mod's listing image on disk.
//
// The declared picture is the fallback, not the primary: of 14 Victoria 3
// workshop mods only 2 declare one at all, under two spellings, and one EU5 mod
// declares `"picture": null`. The file itself is discoverable, so it is the
// better source. picture still gets a turn — a mod may name something other than
// `thumbnail.*` — and is tried beside the descriptor as well as at the root,
// because every mod measured writes it as a bare file name whatever directory
// the file is in.
func listingThumb(gameID, root, picture string) (rel, abs string) {
	if rel, found := modfile.ThumbnailPath(gameID, root, ""); found {
		return rel, filepath.Join(root, filepath.FromSlash(rel))
	}
	if picture == "" {
		return "", ""
	}
	for _, dir := range []string{"", ".metadata"} {
		rel = path.Join(dir, filepath.ToSlash(picture))
		abs = filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err == nil {
			return rel, abs
		}
	}
	return "", ""
}

// steamEResultCode parses an EResult integer from a Steam helper error string.
func steamEResultCode(err string) (int, bool) {
	const prefix = "EResult "
	i := strings.Index(err, prefix)
	if i < 0 {
		return 0, false
	}
	n, e := strconv.Atoi(strings.Fields(err[i+len(prefix):])[0])
	return n, e == nil
}

// steamPublishError returns a user-facing message for known Steam publish errors.
func steamPublishError(respErr string) string {
	code, ok := steamEResultCode(respErr)
	if !ok {
		return ""
	}
	return steamPublishHints[code]
}

var (
	steamOnce sync.Once
	steamExe  string
	steamLib  string
	steamErr  error
)

// ensureSteamHelper extracts the embedded pmt-steamugc when binembed.Stamp changes.
func ensureSteamHelper() (helper, lib string, err error) {
	steamOnce.Do(func() {
		hb := binembed.Helper()
		lb := binembed.APILib()
		if len(hb) < 1024 || len(lb) < 1024 {
			steamErr = fmt.Errorf("steam helper not embedded; run task common:fetch:steamapi")
			return
		}
		base, err := os.UserConfigDir()
		if err != nil {
			steamErr = err
			return
		}
		dir := filepath.Join(base, appConfigDirName, "bin")
		hname := "pmt-steamugc"
		if runtime.GOOS == "windows" {
			hname += ".exe"
		}
		hdest := filepath.Join(dir, hname)
		ldest := filepath.Join(dir, binembed.APILibName())
		stamp := filepath.Join(dir, "steam.version")
		if b, err := os.ReadFile(stamp); err == nil && string(b) == binembed.Stamp() {
			if _, err := os.Stat(hdest); err == nil {
				if _, err := os.Stat(ldest); err == nil {
					steamExe, steamLib = hdest, ldest
					return
				}
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			steamErr = err
			return
		}
		if err := os.WriteFile(hdest, hb, 0o755); err != nil {
			steamErr = err
			return
		}
		if err := os.WriteFile(ldest, lb, 0o644); err != nil {
			steamErr = err
			return
		}
		_ = os.WriteFile(stamp, []byte(binembed.Stamp()), 0o644)
		steamExe, steamLib = hdest, ldest
	})
	return steamExe, steamLib, steamErr
}
