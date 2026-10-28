# git

The package is the IDE's source control service. It runs the git installed on this machine over each mod's
repository: status, refs and log, a file at a revision, and the commands the Source Control view offers.

It runs the git CLI and reads its machine output. Nothing of git is reimplemented, and no git library is used. Of the
engine it calls `files`, to decode a file as the files on disk are and to compare paths, and it asks the workspace
service for the place of each changed file. A repository is its root folder, as a string.

| file | holds |
|---|---|
| `git.go` | the package comment; the events; `Service`; `Run`, which runs git; `FindRepository`, `ReadFile`; the commands (`Stage`, `Unstage`, `Discard`, `CreateCommit`, `CreateBranch`, `SwitchBranch`, `Merge`) |
| `status.go` | `Change`, `Status`, `Ref`; `ReadStatus`, `ListRefs`; the parser of the status records |
| `log.go` | `Commit`; `ListCommits`, `ListCommitFiles` |
| `diff.go` | `LineChange`; `DiffFile`, the editor's change bars |

## Running git

git is found on the PATH on the first call, and kept. `Run` runs it in a folder and returns its standard output. Each
line of standard error is sent as a `git:output` event as it arrives, and a nonzero exit is an error that carries the
text of standard error. A missing git is an error too. Every method takes a folder and works on the repository that
holds it; `FindRepository` returns that repository's root.

## Reading

- `ReadStatus` reads `git status --porcelain=v2 -z --branch --ignored`: the branch, its upstream and how far they
  differ, each `Change` with which sides hold it and what was done to it, and the files git ignores. Each change gets
  its file's `workspace.Place`, from the repository's root, while a workspace is open.
- `ListRefs` reads `git for-each-ref`: the branches, remote branches and tags, each with the date of its commit and
  whether it is current.
- `ListCommits` reads `git log` with a NUL-separated format: each commit's hashes, parents, author, date, subject and
  the refs that point at it, newest first. A repository with no commit gives none.
- `ListCommitFiles` lists the files one commit changed, a renamed file with its old path.
- `ReadFile` returns a file's text at a revision, as `git show` prints it, decoded as the files on disk are. The
  revision `:0` reads the staged version. The diffs and the conflicts are drawn by the editor from these texts.
- `DiffFile` returns the runs of lines an editor's text changes from its file at the last commit: added, changed, or
  removed, a removed run marked on the line before it. It diffs by go-diff's line mode, `\r\n` read as `\n`, the
  engine's diff for line counts too (`steam`'s compare). A file git does not track is checked first with
  `rev-parse --quiet`, so typing in it prints nothing to git's output; it is an error, and the editor draws no bars.

## Commands

`Stage`, `Unstage`, `Discard`, `CreateCommit`, `CreateBranch`, `SwitchBranch` and `Merge` each run one git command and
emit `git:changed` with the repository's folder. `Discard` restores the files git tracks and deletes the ones it does
not. A merge that conflicts is not an error: it leaves the markers in the files, for the editor to resolve.

## Events

| event | data | sent when |
|---|---|---|
| `git:output` | a line git printed | git writes a line to its standard error |
| `git:changed` | the repository's folder | a command PMT ran changed a repository |

## Terms

- **Repository**: a git repository, named by its root folder.
- **Status**: what git status reports for one repository.
- **Change**: one changed file as git status reports it.
- **Ref**: one branch, remote branch or tag.
- **Commit**: one commit of the log, with what the graph draws of it.
