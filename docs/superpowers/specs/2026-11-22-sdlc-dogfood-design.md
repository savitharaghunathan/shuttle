# SDLC Dogfood — Design

**Status:** Approved for planning / execution after plan (2026-11-22, rev 4 — session log). Completes the ROADMAP gate for **Full SDLC pipeline** (implementation already on `main`). Companion track: local web dashboard (`docs/superpowers/specs/2026-11-22-local-web-dashboard-design.md`) on the same branch after this lands.

**Session log:** Append-only chronology at `docs/superpowers/dogfood/2026-11-22-sdlc-session-log.md` (every command + output during dogfood).

## Problem

The embedded `sdlc` / `sdlc-headless` path is implemented and tested, but ROADMAP keeps it under **Next** until a real gated dogfood run completes. Without that pass, “shipped” would mean unit coverage only.

## Goal

Run gated `sdlc` against the **shuttle repo itself**, complete the checklist, then move **Full SDLC pipeline** → **Shipped** in `docs/ROADMAP.md`.

## Decisions

| Topic | Choice |
|---|---|
| Target repo | Shuttle itself (`github.com/savitharaghunathan/shuttle`) |
| Isolation | Disposable git worktree from `main` (e.g. `.worktrees/sdlc-dogfood`); not the planning branch |
| Registry | Isolate with a temporary `SHUTTLE_HOME` for the dogfood worktree so the global registry path for shuttle is **not** overwritten |
| Agent | Cursor (`--agent cursor` → scaffold `cursor-agent`) for `implement`; scaffold `readonly_worker: cursor` also runs plan/design/review — budget **four** agent stages |
| Change type | One focused **unit test** for an existing helper (no production behavior change) |
| PR fate | **Close without merge** (throwaway); delete remote branch after |
| Checklist depth | Full: approve `approve_ship` → open PR → close PR / delete remote branch |
| Headless optional | `sdlc-headless` optional if time; not required to mark Shipped |
| Planning branch | `feat/sdlc-dogfood-and-dashboard` only receives the ROADMAP update + this spec (and later dashboard work) |

## Preflight (before `shuttle run`)

1. `cursor-agent` on `PATH`, authenticated, usable non-interactively (`--trust --yolo` as in scaffold).
2. `gh` authenticated for the shuttle remote.
3. Create worktree from **current `origin/main`** (or local `main` after fetch) at `.worktrees/sdlc-dogfood`. Let `WT` be that worktree’s absolute path.
4. From `$WT`, build `bin/shuttle` and put it first on `PATH` so `{{.ShuttleBin}}` / ship uses that binary.
5. `export SHUTTLE_HOME="$WT/.shuttle-home"` (absolute; gitignored via `.worktrees`).
6. Register with `shuttle import repo --path "$WT"` under that `SHUTTLE_HOME` (existing checkout — do not clone a second copy).
7. Ensure `shuttle.yaml` exists with a **production** `ship.command` (scaffold if missing). Confirm it is not an echo stub (`IsNonProductionShipCommand` would fail `ship`). Treat scaffolded `shuttle.yaml` as **local-only**: the dogfood spec and/or agent instructions must say not to commit it; verify it is absent from the throwaway PR diff.
8. Expect wall time for plan + design + implement + review (all `cursor-agent`) plus `test.command` (often a full Go test suite).
9. Working tree in the dogfood worktree must be clean enough for retry/ship preflight aside from intentional local `shuttle.yaml` (no leftover dogfood branch from a prior attempt, or clean it first).

## Procedure

1. Complete **Preflight**.
2. Author a tiny markdown spec that instructs the agent to add **one** unit test for a chosen existing helper (pick at execution time: prefer a pure, undertested helper) and **not** to modify or commit `shuttle.yaml`. Spec-only run → empty `IssueRef`; `ship` / `pr open` uses `--fill` (commit-derived title/body). That is expected.
3. From the worktree (with `SHUTTLE_HOME` set): `shuttle run -t sdlc --spec <abs-spec> --agent cursor` (gated template).
4. Inspect `.shuttle/runs/<id>/artifacts/{plan,design,implement}/summary`.
5. `shuttle approve <id> approve_ship` and confirm `ship` opens a PR. Optionally re-invoke ship / `shuttle pr open` once to confirm idempotent reuse of the existing PR. Confirm the PR diff does **not** include `shuttle.yaml`.
6. **Close** the dogfood PR without merging; delete the remote feature branch; remove the worktree; drop the disposable `SHUTTLE_HOME` directory.
7. On `feat/sdlc-dogfood-and-dashboard`: move Full SDLC → **Shipped** in ROADMAP; fill **Evidence** below.

## Safety

- Do not dogfood from `feat/sdlc-dogfood-and-dashboard` while it has unrelated WIP.
- Never Upsert the primary (default `SHUTTLE_HOME`) registry entry to the dogfood worktree path.
- Agent may commit/push on the run branch (`shuttle/…`); cleanup is close PR + delete branch + remove worktree + disposable home.
- On stage failure: resume/retry inside the dogfood worktree only.
- Trust boundary unchanged: Shuttle does not checkout/push/reset for the user outside what the agent and `ship.command` already do.
- The unit test is **intentionally discarded** with the closed PR; do not cherry-pick it onto the planning branch unless doing a separate intentional change.
- Do not commit scaffolded `shuttle.yaml` from the dogfood worktree into the throwaway PR or onto `main`.

## Evidence (fill after run)

| Field | Value |
|---|---|
| Date | |
| Run ID | |
| Spec path | |
| Helper / test added | |
| Shuttle binary (path + `git rev-parse HEAD` in worktree) | |
| Worktree path | |
| `SHUTTLE_HOME` used | |
| PR URL | |
| Closed (no merge) | |
| Notes | |

## Out of scope

- Product code changes for SDLC itself (already on `main`).
- Merging the dogfood PR.
- Dashboard implementation (separate spec).
- Requiring `sdlc-headless` for the Shipped gate.
- Remote (`--remote`) dogfood.

## Success criteria

1. Gated `sdlc` run completes through `ship` with an opened PR on shuttle.
2. Artifacts for plan/design/implement inspected and sensible.
3. PR closed **without merge**; remote branch deleted; dogfood worktree and disposable `SHUTTLE_HOME` removed.
4. Default registry still points at the operator’s normal shuttle checkout (unchanged by dogfood).
5. `docs/ROADMAP.md` moves Full SDLC → Shipped with this dogfood cited.
