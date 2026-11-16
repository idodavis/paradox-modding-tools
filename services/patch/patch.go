// Package patch is the Patch Center's service. It fetches the Modding Co-ops'
// digests of a game's updates and reads them into the game's patches, lists
// the versions the Patch Center shows, and says which digest articles touch
// each mod of the open workspace. What a patch is, and where patches are
// kept, is the engine's patch package.
//
// A Co-op keeps one repository of digests, a folder for each version. A
// refresh asks GitHub for the newest commit and downloads the repository as
// one zip only for a commit it has not read. GitHub is asked at most once a
// day for a game, and never for a game with no digest.
//
// The Co-ops' layout, as the package reads it:
//
//   - A version's folder is the one that holds a docs folder: flat, as
//     1.15.1, or under a major version's folder, as 1.4/1.4.7. An open
//     beta's ends in -openbeta.
//   - A version's dump files are the docs folder's .log files and its
//     data_types_*.txt files. A file the folder lacks did not change, so the
//     version takes the newest earlier copy.
//   - A version is compared against its base, the latest earlier version
//     that is not an open beta.
//   - changes_files.md lists the game files a version added, modified or
//     deleted, as " - **M** game/common/x.txt".
//   - The notes are breaking_changes.md or changes_breaking.md, features.md,
//     major_features.md or changes_features.md, and discord.md. Each ##
//     heading is one article. A heading whose lines only point into the
//     digest's other files is no article. An article is a breaking change if
//     its file is one of them, or its heading is "Breaking Changes".
//   - An article's folders are its links to a type doc, as types/x.md. Its
//     keys are the words it writes in backticks outside a code block. A line
//     "- `A` -> `B`" after **Renamed** renames key A to B.
//
// # Robustness
//
// A refresh that cannot reach GitHub, open the zip or find a version stops,
// and the files saved before stay. A file of a version that cannot be read
// is left out and named in the error, after the rest is saved. A note file
// under a name the Co-ops have not used before is read as plain notes, unless
// its name starts with changes_.
//
// An article ties to a mod only through an exact link or key. An article that
// names neither ties to no mod.
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

	// stop ends the recheck of the open workspace, nil while none runs.
	stop atomic.Pointer[context.CancelFunc]
}

// EventUpdated is sent when a refresh saved new patches or failed. Its data is
// the error's text, "" for none.
const EventUpdated = "patch:updated"

// recheck is the shortest time between two asks of GitHub for a game.
const recheck = 24 * time.Hour

func init() {
	application.RegisterEvent[string](EventUpdated)
}

// ServiceStartup refreshes the patches in the background when a workspace
// opens, and keeps rechecking while it stays open.
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

// setStop ends the running recheck, if any, and keeps the function that ends
// the next.
func (s *Service) setStop(stop context.CancelFunc) {
	if running := s.stop.Swap(&stop); running != nil && *running != nil {
		(*running)()
	}
}

// watchDigest refreshes the patches, then again every recheck until the
// digest has a patch of the installed version or the context ends.
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

// Refresh reads the digests of the open workspace's game again if their
// repository has a new commit. The session loads new patches, and
// patch:updated is sent when it saved some or failed. Where part of the
// digest could not be read, it saves the rest and returns what it left out.
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

// fetchPatches reads the game's digest repository into its folder of patches,
// and reports whether it saved new patches. It saves nothing if GitHub was
// asked less than recheck ago or the folder is current.
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
