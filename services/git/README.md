# git

[Architecture](../../docs/ARCHITECTURE.md) › [Services](../../docs/ARCHITECTURE.md#the-services) › git

The git service is the IDE's source control. It runs the git installed on this machine over each mod's repository and
reads git's machine output. Nothing of git is reimplemented, and no git library is used. The
[Source Control view](../../frontend/src/README.md) shows each repository's status, history and branches from its
reads, and sends every command it names through `Run`.

Every method names a repository by its root folder, as a string. The [workspace](../workspace/README.md) service says
which mods are open and gives the place of each changed file, so a page can show a file as the explorer does.

**Uses** [files](../../internal/files/README.md), [workspace](../workspace/README.md) · **Used by**
[steam](../steam/README.md), which drafts a change note from a mod's commits.

## Running git

git is found on the PATH on the first call, and kept. `IsInstalled` reports whether it is there. A missing git is an
error on every call.

`run` runs git in a folder and returns its standard output, on a failure too. Each line of standard error is sent as a
`git:output` event as it arrives, and a nonzero exit is an error that carries the text of standard error. Git runs with
English messages, since the package reads them, with no optional locks, so a read never takes the index lock, and with
no prompt for a password.

`Run` is the one the frontend calls for every command it names (staging, a commit, a branch, a push, a checkout). It
calls `run` and then sends `git:changed` with the folder, also when git failed, since a failed command can leave the
repository changed. The package's own reads call `run`, so a read never sends the event. The commands the package holds
itself send it too.

## Reading

- `ListRepositories` lists the repositories the open workspace's mods are in, each with its root and its mods, in the
  load order of their first mod. A mod in none, or whose folder is missing, is left out.
- `ReadStatus` reads the porcelain status of one repository. Each change and each ignored path gets its
  `workspace.Place` while a workspace is open. A path git ends with a slash is a folder, and its place says so.
- `ListRefs` reads the branches, remote branches and tags, each with the date of its commit and whether it is current.
- `ListCommits` reads the log, newest first, up to a limit, from the given refs or from HEAD. A repository with no
  commit gives none. A remote's `HEAD` is left out of a commit's refs, and any other name, such as a detached `HEAD`, is
  kept with no type.
- `ListCommitFiles` lists the files one commit changed, a renamed file with its old path.
- `ListRemotes` lists each remote once, with the address it fetches from.
- `ReadFile` returns a file's text at a revision, decoded as the files on disk are. The revision `:0` reads the staged
  version. The editor draws the diffs and the conflicts from these texts.
- `DiffFile` returns the runs of lines an editor's text changes from its file at the last commit, for the editor's
  change bars: added, changed, or removed. A removed run is marked on the line before it. A file git does not track is
  an error, and the editor draws no bars.

## Commands

Besides `Run`, three commands hold a rule of their own:

- `Discard` restores the files git tracks and deletes the ones it does not.
- `Merge` treats a conflict as no error. It leaves the markers in the files, for the editor to resolve.
- `DeleteBranch` answers false, with no error, where git refuses a branch with commits no other branch has, unless it is
  forced. `Pull` takes a remote branch's name, such as `origin/main`, and splits it into its remote and its branch.

## Files

| file | what it holds |
|---|---|
| `git.go` | The service, running git, repositories, remotes and the commands, and where a path lies in the workspace. |
| `git_windows.go`, `git_linux.go` | Starting git with no console window, which Windows opens for each run otherwise. |
| `status.go` | The status of a repository and its refs. |
| `log.go` | The commits of the log and the files a commit changed. |
| `diff.go` | The change bars: the lines a text changes from its file's last commit. |

## Events

| event | data | sent when |
|---|---|---|
| `git:output` | a line git printed | git writes a line to its standard error |
| `git:changed` | the repository's folder, or the folder `Run` was given | a command PMT ran changed a repository |

## Terms

- **Repository**: a git repository, named by its root folder, with the workspace's mods in it.
- **Status**: what git status reports for one repository.
- **Change**: one changed file as git status reports it.
- **Ref**: one branch, remote branch or tag.
- **Remote**: one remote of a repository: its label and the address it fetches from.
- **Commit**: one commit of the log.
