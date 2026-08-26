# Import Existing Repo Path — Design

**Status:** Approved direction (2026-08-26)

## Problem

`shuttle import repo` always targets `~/.shuttle/repos/<owner>/<name>` and clones when that directory is missing. Users who already have a local checkout must symlink or copy into `SHUTTLE_HOME`, or abandon issue-driven runs for cwd/`--spec` mode. Neither is acceptable for a local factory control plane.

## Goal

Register an existing git checkout as the agent working directory for issue-driven runs, without cloning or relocating the repo.

```bash
shuttle import repo --path /Users/savee/local_dev/test org/repo
cd /Users/savee/local_dev/test && shuttle import repo .
```

## Non-goals

- Moving or copying repositories into `SHUTTLE_HOME`
- Multiple registered paths per owner/name
- Auto-import on `shuttle run --issue`
- Changing clone defaults for URL-only import

## Architecture

The registry already stores an arbitrary `Path` per `(owner, name)`. Import today always overwrites that path with `home.RepoPath(owner, name)`. This change makes path resolution explicit:

| Input | Path used | Clone? |
|---|---|---|
| URL / `owner/name` only | `~/.shuttle/repos/<owner>/<name>` | Yes, if not already a git repo |
| `--path <dir>` | Absolute path of `<dir>` | Never |
| `.` (or equivalent cwd form) | Absolute path of cwd | Never |

### Layers

| Layer | Change |
|---|---|
| `internal/repo` | Resolve local path; detect remote from git when needed; skip clone when path provided |
| `internal/cli` | `--path` flag; accept `.` as repo arg |
| `README` | Document both flows |

Registry schema unchanged.

## CLI contract

```bash
# Clone into SHUTTLE_HOME (unchanged)
shuttle import repo https://github.com/org/repo
shuttle import repo git@github.com:org/repo.git
shuttle import repo org/repo

# Register existing checkout
shuttle import repo --path /abs/or/rel/path org/repo
shuttle import repo --path /abs/or/rel/path   # owner/name from origin remote

# From inside a clone
shuttle import repo .
```

### Resolution rules

1. **Local mode** when `--path` is set or the sole arg is `.`.
2. In local mode, the path must exist and be a git repo (`IsGitRepo`). Error clearly otherwise. Never clone.
3. Resolve path to absolute before upsert.
4. Resolve `owner`/`name`:
   - From an explicit GitHub URL / `owner/name` argument when present.
   - Else from `git remote get-url origin` in that directory (HTTPS or SSH), via existing `ParseGitHubURL`.
5. If neither arg nor remote yields a GitHub owner/name → error.
6. If both arg and remote are present and disagree → error (refuse silent mismatch).
7. Scaffold `shuttle.yaml` only if missing (unchanged).
8. `reg.Upsert` with the resolved absolute path (re-import retargets path).

### Output

```
imported: org/repo
path: /Users/savee/local_dev/test
```

## Error cases

| Case | Error |
|---|---|
| `--path` missing / not a directory | path does not exist |
| path not a git repo | not a git repository |
| `.` outside a git repo | not a git repository |
| no owner/name and no usable origin | cannot determine GitHub owner/name; pass org/repo |
| arg owner/name ≠ remote owner/name | path remote is X/Y, expected A/B |
| non-GitHub remote | reuse existing parse errors |

## Testing

- `--path` registers absolute path, does not call clone
- `import repo .` from a temp git repo with `origin` set
- owner/name from remote when arg omitted
- mismatch between arg and remote fails
- URL-only import still clones into `home.RepoPath` when empty

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Single SQLite writer per DB file
- Trust boundary unchanged: workers run as invoking user in the registered path
- **Git commits:** `git commit -s` (Signed-off-by) plus body line `Assisted-by: Cursor <cursoragent@cursor.com>`. Do **not** add `Co-authored-by: Cursor <cursoragent@cursor.com>`.
