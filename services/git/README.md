# git

The package is the IDE's source control service. It runs the git installed on this machine over each mod's
repository: status, refs and log, a file at a revision, and the commands the Source Control view offers.

It runs the git CLI and reads its machine output. Nothing of git is reimplemented, and no git library is used. Of the
engine it calls `files`, to decode a file as the files on disk are and to compare paths, and it asks the workspace
service for the place of each changed file and each ignored path. A repository is its root folder, as a string.

| file | holds |
|---|---|
| `git.go` | the package comment; the events; `Service`, `Remote`; `Run` and `run`, which run git; `FindRepository`, `ReadFile`, `ListRemotes`; the commands (`Discard`, `Merge`, `Pull`, `DeleteBranch`); the locating of paths |
| `status.go` | `Change`, `Status`, `Ref`; `ReadStatus`, `ListRefs`; the parser of the status records; a full ref's type by its prefix |
| `log.go` | `Commit`; `ListCommits`, `ListCommitFiles` |
| `diff.go` | `LineChange`; `DiffFile`, the editor's change bars |

## Running git

git is found on the PATH on the first call, and kept. `run` runs it in a folder and returns its standard output. Each
line of standard error is sent as a `git:output` event as it arrives, and a nonzero exit is an error that carries the
text of standard error. A missing git is an error too. `FindRepository` gives the root of the repository that holds a
folder; `Discard`, `Merge`, `Pull`, `DeleteBranch`, `ReadStatus`, `ListRemotes` and `ListCommitFiles` take that root,
the others any folder of the repository.

`Run` is the one the frontend calls for every command it names (staging, a commit, a branch, a pull, a push, a
checkout): it calls `run` and then emits `git:changed` with the folder, so what shows a repository's state reads it
again. The package's own reads and commands call `run`, so a read never sends the event; the commands that change a
repository send it themselves.

## Reading

- `ReadStatus` reads `git status --porcelain=v2 -z --branch --ignored`: the branch, its upstream and how far they
  differ, each `Change` with which sides hold it and what was done to it, and the files git ignores. Each change and
  each ignored path gets its `workspace.Place`, from the repository's root, while a workspace is open; the place of a
  path git ends with a slash, a folder, says `isDir`. A path under none of the workspace's roots gets a place with no
  origin.
- `ListRefs` reads `git for-each-ref`: the branches, remote branches and tags, each with the date of its commit and
  whether it is current.
- `ListCommits` reads `git log` with a NUL-separated format: each commit's hashes, parents, date, subject and the refs
  that point at it, newest first. A repository with no commit gives none. The refs come decorated with their full
  names, so each is typed as `ListRefs` types it; a remote's `HEAD` is left out, and any other name, such as a
  detached `HEAD`, is kept with no type.
- `ListRemotes` reads `git remote -v`: each remote once, with the address it fetches from.
- `ListCommitFiles` lists the files one commit changed, a renamed file with its old path.
- `ReadFile` returns a file's text at a revision, as `git show` prints it, decoded as the files on disk are. The
  revision `:0` reads the staged version. The diffs and the conflicts are drawn by the editor from these texts.
- `DiffFile` returns the runs of lines an editor's text changes from its file at the last commit: added, changed, or
  removed, a removed run marked on the line before it. It diffs by go-diff's line mode, `\r\n` read as `\n`, the
  engine's diff for line counts too (`steam`'s compare). A file git does not track is checked first with
  `rev-parse --quiet`, so typing in it prints nothing to git's output; it is an error, and the editor draws no bars.

## Commands

Every command that changes a repository as the Source Control view sends it runs through `Run`, which emits
`git:changed` with the repository's root, also when git fails. `Discard` and `Merge` hold a rule of their own and
emit it too. `Discard` restores the files git tracks and deletes the ones it does not. A merge that conflicts is not
an error: it leaves the markers in the files, for the editor to resolve. `Pull` and `DeleteBranch` hold one each and
run through `Run`: `Pull` splits a remote branch's name into its remote and its branch, and `DeleteBranch` answers
false, with no error, where git refuses a branch with commits no other branch has, which git's English calls "not
fully merged".

## Events

| event | data | sent when |
|---|---|---|
| `git:output` | a line git printed | git writes a line to its standard error |
| `git:changed` | the repository's folder, or the folder `Run` was given | a command PMT ran changed a repository |

## Terms

- **Repository**: a git repository, named by its root folder.
- **Status**: what git status reports for one repository.
- **Change**: one changed file as git status reports it.
- **Ref**: one branch, remote branch or tag.
- **Remote**: one remote of a repository: its label and the address it fetches from.
- **Commit**: one commit of the log, with what the graph draws of it.
