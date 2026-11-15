// Package patch is the Patch Center's service. It fetches the Modding Co-ops'
// digests of a game's updates and reads them into the game's patches, lists
// the versions the Patch Center shows, and says which of the digests' articles
// touch each mod of the open workspace. What a patch is, and where a game's
// patches are kept, is the engine's patch package.
//
// A game's Co-op keeps one repository of digests, each version in a folder of
// its own. A refresh asks GitHub for the repository's newest commit, and only
// a commit it has not read before downloads the repository as one zip, which
// is read in memory and never written to disk. The patches are saved in the
// game's folder of patches, with the dumps of each version an install is on.
//
// GitHub is asked at most once every 6 hours for a game. While the digest has
// no patch of the installed version, a workspace that stays open asks again
// every 6 hours. A game with no digest is never asked.
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
//     point into the digest's other files is no article. An article is a
//     breaking change if its file is one of them, or if its heading is
//     "Breaking Changes", which is where discord.md writes them for a
//     version that has no file of them.
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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/patch"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service that reads the Co-ops' digests.
type Service struct {
	Workspace *workspace.Service
	Wiki      *wiki.Service

	// mu keeps two refreshes from running at once.
	mu sync.Mutex

	// stop ends the recheck of the open workspace. It holds nil while none
	// runs.
	stop atomic.Pointer[context.CancelFunc]
}

// EventUpdated is sent when a refresh saved new patches or failed. Its data is
// the error's text, or "" if it worked.
const EventUpdated = "patch:updated"

// recheck is how long the digest is left alone after GitHub was asked for its
// newest commit.
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
	if running := s.stop.Swap(&stop); running != nil && *running != nil {
		(*running)()
	}
}

// watchDigest refreshes the patches, and again every recheck for as long as the
// context lives and the digest of the open workspace's game has no patch of
// the installed version.
func (s *Service) watchDigest(ctx context.Context) {
	for {
		_ = s.Refresh()

		ws, err := s.Workspace.Session()
		if err != nil || ws.Game.Digests == "" {
			return
		}

		hasPatch := slices.ContainsFunc(*ws.Patches.Load(), func(gamePatch patch.Patch) bool {
			return !gamePatch.Local && game.IsInstalled(gamePatch.Version, ws.Schema.Version)
		})
		if hasPatch {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(recheck):
		}
	}
}

// Refresh reads the digests of the open workspace's game again if its
// repository has a new commit. If it saved new patches, the session loads
// them, and it sends patch:updated then and when it fails. It does nothing
// for a game that has no digests, and asks GitHub no sooner than recheck after
// it last did. It returns an error if no workspace is open or the fetch fails,
// and the files saved before stay. Where a part of the digest could not be
// read, it saves the rest and returns what it left out.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws, err := s.Workspace.Session()
	isSaved := false
	if err == nil {
		if ws.Game.Digests == "" {
			return nil
		}

		isSaved, err = s.fetchPatches(ws)
	}

	if isSaved {
		ws.LoadPatches()
	}

	if isSaved || err != nil {
		message := ""
		if err != nil {
			message = err.Error()
		}

		application.Get().Event.Emit(EventUpdated, message)
	}

	return err
}

// fetchPatches reads the repository of a session's game into the game's folder
// of patches, and reports whether it saved new patches. It saves nothing if
// GitHub was asked less than recheck ago, or the folder is current with the
// newest commit.
func (s *Service) fetchPatches(ws *workspace.Session) (bool, error) {
	records := s.Workspace.Config
	dir := patch.ResolveDir(records.Dirs.Cache, ws.Game)
	installs := workspace.ListInstallVersions(records.Config(), ws.Game)
	if time.Since(patch.FindAsked(dir)) < recheck {
		return false, nil
	}

	head, err := fetchHead(ws.Game.Digests)
	if err != nil {
		return false, err
	}

	if patch.IsCurrent(dir, head, installs) {
		return false, patch.MarkAsked(dir)
	}

	source, err := fetchArchive(ws.Game.Digests)
	if err != nil {
		return false, err
	}

	patches, dumps, skipped := readArchive(source, ws.Game.Digests, ws.Schema, installs)
	if len(patches) == 0 {
		return false, errors.Join(skipped, fmt.Errorf("the digest %s holds no version", ws.Game.Digests))
	}

	if err := patch.SaveDigest(dir, head, patches, dumps); err != nil {
		return false, err
	}

	return true, skipped
}
