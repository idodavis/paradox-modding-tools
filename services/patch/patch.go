// Package patch fetches the Modding Co-ops' digests of a game's updates, reads
// them into the game's patches, lists the versions the Patch Center shows, and
// says what the patches mean for each mod of the open workspace.
//
// A game's Co-op keeps one repository of digests, each version in a folder of
// its own. A refresh asks GitHub for the repository's newest commit, and only
// a commit it has not read before downloads the repository as one zip, which
// is read in memory and never written to disk. The patches are saved in the
// game's folder of patches, with the dumps of each version an install is on.
//
// GitHub is asked at most once every 6 hours for a game, as the folder keeps
// the time of the last ask. While the digest has no patch of the installed
// version, a workspace that stays open asks again every 6 hours, until the
// digest has it. A game with no digest is never asked.
//
// The Co-ops' layout, as the package reads it:
//
//   - A version's folder is the one that holds a docs folder. It is flat, as
//     1.15.1, or under a major version's folder, as 1.4/1.4.7, and an open
//     beta's ends in -openbeta. The top folders documentation, current_docs,
//     image and script hold no version.
//   - A version's dump files are the docs folder's .log files and its
//     data_types_*.txt files. A file the folder lacks did not change, so the
//     version takes the newest earlier version's copy.
//   - A version is compared against its base, the latest earlier version that
//     is not an open beta. Vic3's 1.15.0 digest is made against 1.13.11, which
//     is what that rule gives.
//   - changes_files.md lists the files of the game's folder a version added,
//     modified or deleted, one in each line as " - **M** game/common/x.txt".
//   - The notes are breaking_changes.md or changes_breaking.md, features.md,
//     major_features.md or changes_features.md, and discord.md. A file is
//     split at its ## headings, one article each. A heading whose lines only
//     point into the digest's other files is no article.
//   - An article's folders are its links to a type doc, as types/x.md, named
//     by discord.md's list of the type docs, else by the one database of the
//     game's schema whose folder ends in /x. Its keys are the words it writes
//     in backticks outside a code block. A line "- `A` -> `B`" after
//     **Renamed** renames key A to B.
//
// # Robustness
//
// A refresh stops if GitHub does not answer, the zip does not open or the
// repository has no version, and the files it saved before stay. A file of a
// version that cannot be read is left out and named in the error the refresh
// returns after it saved the rest, and a version keeps the older copy of a
// dump file it cannot read. A version with no changes_files.md has no game
// files. A note file under a name the Co-ops have not used before is read as
// plain notes, unless its name starts with changes_. A line of a list of
// changed files that does not read is left out.
//
// An article ties to a mod only through an exact link or key: a folder its type
// docs link that the mod writes files in, or a key it writes in backticks that
// the mod defines or uses. An article that names neither ties to no mod.
package patch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/steam"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service that reads the Co-ops' digests.
type Service struct {
	Workspace *workspace.Service
	Wiki      *wiki.Service
	Health    *health.Service
	Steam     *steam.Service

	// mu keeps two refreshes from running at once.
	mu sync.Mutex

	// stopMu guards stop, which ends the recheck of the open workspace and is
	// nil while none runs.
	stopMu sync.Mutex
	stop   context.CancelFunc
}

// EventUpdated is sent when a refresh finishes. Its data is the error's text,
// or "" if it worked.
const EventUpdated = "patch:updated"

// recheck is how long the digest is left alone after GitHub was asked for its
// newest commit: it is asked again this often while it has no patch of the
// installed version, and never sooner than this after the last time.
const recheck = 6 * time.Hour

func init() {
	application.RegisterEvent[string](EventUpdated)
}

// ServiceStartup refreshes the patches in the background each time a workspace
// opens, and again every recheck while the workspace stays open and the
// digest has no patch of the installed version.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	events := application.Get().Event
	events.On(workspace.EventOpened, func(*application.CustomEvent) {
		watching, stop := context.WithCancel(ctx)
		s.setStop(stop)

		go s.watchDigest(watching)
	})
	events.On(workspace.EventClosed, func(*application.CustomEvent) { s.setStop(nil) })

	return nil
}

// setStop ends the running recheck, if any, and keeps the given function to
// end the next one.
func (s *Service) setStop(stop context.CancelFunc) {
	s.stopMu.Lock()
	running := s.stop
	s.stop = stop
	s.stopMu.Unlock()

	if running != nil {
		running()
	}
}

// watchDigest refreshes the patches, and again every recheck for as long as the
// context lives and the digest has no patch of the installed version.
func (s *Service) watchDigest(ctx context.Context) {
	for {
		_ = s.Refresh()
		if !s.lacksPatch() {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(recheck):
		}
	}
}

// lacksPatch reports whether the open workspace's game has a digest that holds
// no patch of the installed version. It is false where no workspace is open.
func (s *Service) lacksPatch() bool {
	ws, err := s.Workspace.Session()
	if err != nil || ws.Game.Digests == "" {
		return false
	}

	return !slices.ContainsFunc(*ws.Patches.Load(), func(patch workspace.Patch) bool {
		return !patch.Local && game.IsInstalled(patch.Version, ws.Schema.Version)
	})
}

// Refresh reads the digests of the open workspace's game again if its
// repository has a new commit, and sends patch:updated at the end. It does
// nothing for a game that has no digests, and asks GitHub no sooner than
// recheck after it last did. It returns an error if no workspace is open or
// the fetch fails, and the files saved before stay. Where a part of the digest
// could not be read, it saves the rest and returns what it left out.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws, err := s.Workspace.Session()
	if err == nil {
		if ws.Game.Digests == "" {
			return nil
		}

		dir := workspace.ResolvePatchDir(s.Workspace.Config.Dirs.Cache, ws.Game)
		err = fetchPatches(dir, ws.Game, ws.Schema, workspace.ListInstallVersions(s.Workspace.Config.Config(), ws.Game))

		// The patches that were saved are read even where the refresh stopped
		// part way, since its error does not say how far it got.
		err = errors.Join(err, ws.LoadPatches())
	}

	message := ""
	if err != nil {
		message = err.Error()
	}

	application.Get().Event.Emit(EventUpdated, message)

	return err
}

// fetchPatches reads a game's repository into the folder of patches, unless
// GitHub was asked less than recheck ago or the folder is current with the
// newest commit.
func fetchPatches(dir string, row *game.Game, language *lang.Schema, installs []string) error {
	if elapsed := time.Since(loadChecked(dir)); elapsed >= 0 && elapsed < recheck {
		return nil
	}

	if err := files.WriteAtomic(workspace.ResolveCheckedFile(dir), []byte(time.Now().Format(time.RFC3339))); err != nil {
		return err
	}

	head, err := fetchHead(row.Digests)
	if err != nil {
		return err
	}

	if isCurrent(dir, head, installs) {
		return nil
	}

	source, err := fetchArchive(row.Digests)
	if err != nil {
		return err
	}

	patches, kept, skipped := readArchive(source, language, installs)
	if len(patches) == 0 {
		return errors.Join(skipped, fmt.Errorf("the digest %s holds no version", row.Digests))
	}

	if err := saveDigest(dir, head, patches, kept); err != nil {
		return err
	}

	return skipped
}

// loadChecked returns when GitHub was last asked for the digest's newest
// commit. It is the zero time for a folder that was never asked and for a file
// that cannot be read.
func loadChecked(dir string) time.Time {
	text, _ := os.ReadFile(workspace.ResolveCheckedFile(dir))
	checked, _ := time.Parse(time.RFC3339, string(text))

	return checked
}

// isCurrent reports whether the folder of patches was read from the given
// commit and holds the dumps of each version an install is on that the digest
// has, so that nothing is to be downloaded.
func isCurrent(dir, head string, installs []string) bool {
	sha, err := os.ReadFile(workspace.ResolveShaFile(dir))
	digest, ok := files.LoadGob[[]workspace.Patch](workspace.ResolveDigestFile(dir))
	if err != nil || string(sha) != head || !ok {
		return false
	}

	for _, patch := range digest {
		for _, install := range installs {
			if game.IsInstalled(patch.Version, install) && !files.IsFile(workspace.ResolveDumpsFile(dir, install)) {
				return false
			}
		}
	}

	return true
}

// saveDigest saves the patches and the dumps of the versions installs are on,
// and the commit they were read from last, so that a failed save is read again
// by the next refresh. It deletes the dumps of any other version. A version's
// dumps are written only if the folder has none of it yet, or its file cannot
// be read: a published version's dumps never change, and a file written again
// would change its time, which names the install's schema cache and would
// build the schema again.
func saveDigest(dir, head string, patches workspace.Patches, kept map[string]*schema.Dumps) error {
	saved := map[string]bool{}
	for install, dumps := range kept {
		file := workspace.ResolveDumpsFile(dir, install)
		saved[file] = true
		if _, ok := files.LoadGob[*schema.Dumps](file); ok {
			continue
		}

		if err := files.SaveGob(file, dumps); err != nil {
			return err
		}
	}

	// A file that cannot be deleted now goes at a later refresh.
	stale, _ := filepath.Glob(workspace.ResolveDumpsFile(dir, "*"))
	for _, file := range stale {
		if !saved[file] {
			_ = os.Remove(file)
		}
	}

	if err := files.SaveGob(workspace.ResolveDigestFile(dir), []workspace.Patch(patches)); err != nil {
		return err
	}

	return files.WriteAtomic(workspace.ResolveShaFile(dir), []byte(head))
}
