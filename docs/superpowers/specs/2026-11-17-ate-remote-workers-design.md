# Ate Remote Workers — Design

**Status:** Approved for implementation (2026-11-17)

## Problem

Shuttle invokes coding agents as local CLIs. That works for a single-user laptop, but agent stages are untrusted, idle-heavy, and hard to isolate. [Agent Substrate](https://ate.dev) (ate) already provides sandboxed sticky actors (gVisor/microVM), suspend/resume, and a gRPC control plane. Shuttle should use that for remote agent execution without inventing a second scheduler.

## Goal

Ship a **full, clean** remote-worker path:

```bash
shuttle run --issue … --agent claude --remote --headless
```

With `--remote`, agent stages run **only** on ate. Auth, resume, task, suspend, or git-sync failure fails the stage. **No** silent fallback to local `claude` / other presets.

## Architecture

```
shuttle CLI/API (local control plane)
  └─ pipeline.Engine
       ├─ local workers: intake, shell, exec, test, open_pr, ship, gates
       └─ worker.AteAgent  (agent stages only)
            ├─ gRPC → ate-api-server (Create/Get/Resume/Suspend Actor)
            └─ HTTP  → actor task API (prompt → implement → push branch)
                 └─ go-git fetch/checkout of that branch into registered cwd
```

Ate owns sandbox lifecycle and actor filesystem persistence. Shuttle owns the DAG, SQLite run state, and local ship path (`test` / `open_pr`).

## Decisions

| Topic | Choice |
|---|---|
| Opt-in | `--remote` (CLI) / `"remote": true` (API); requires `ate:` config |
| v1 agent | `claude` only under `--remote`; any other agent is a hard error |
| Sandbox | Pre-provisioned gVisor WorkerPool (shuttle does not install ate) |
| Workload | Shuttle-owned OCI image with HTTP task API + Claude Code |
| Stickiness | Actor name `{owner}-{repo}-claude` in configured atespace |
| Control API | Vendored `ateapi` gRPC stubs (Apache-2.0); no `kubectl ate` |
| Data plane | HTTP task API inside the actor (not env-loop demos) |
| Sync | Actor pushes run branch; shuttle **must** fetch/checkout before success |
| Fallbacks | None |

## Config

`shuttle.yaml` (or defaults file) gains:

```yaml
ate:
  endpoint: "localhost:8443"          # ate-api-server gRPC
  atespace: "shuttle"
  actor_template: "shuttle-claude"    # pre-created ActorTemplate name
  task_url: "http://127.0.0.1:8000" # data-plane base (router/proxy)
  token_file: ""                      # optional bearer token path
  insecure: true                      # local kind TLS skip (explicit)
```

Missing/incomplete `ate:` with `--remote` → error before the run starts.

## Worker contract

`AteAgent.Execute`:

1. Resolve actor id from repo identity + agent (`claude`).
2. `GetActor`; on not-found → `CreateActor` from `actor_template`.
3. `ResumeActor`.
4. `POST {task_url}/v1/tasks` with `{prompt, model, branch, repo_url, readonly}` (header `ate-target-actor: {atespace}/{actor}` when talking through the router).
5. Stream response body / log lines into the stage log.
6. On task success: **fetch and checkout** `branch` into `req.WorkDir` via go-git; failure fails the stage.
7. `SuspendActor`; suspend failure after a successful task **fails** the stage.

## Placement

| Stage | Location |
|---|---|
| Agent stages (worker = `claude` / configured agent preset) | ate |
| `intake`, `shell`, `exec`, `test`, `open_pr`, `ship`, `manual` | local |

## Non-goals (v1)

- microVM pools, remote `cursor`/`codex`/`opencode`
- Moving `test` / `open_pr` into actors
- Embedding an ate cluster installer
- kubectl-based control
- Local CLI fallback on ate errors

## Testing

- Unit: actor naming, gRPC client fake, AteAgent failure paths, fetch helper, CLI `--remote` validation
- Integration against kind+ate: optional, build-tag / env gated; default CI stays local

## Global constraints

- Go module: `github.com/savitharaghunathan/shuttle`
- Prefer MIT/Apache-2.0 deps; ateapi stubs are Apache-2.0
- Prefer go-git over git CLI
- Single SQLite writer per DB; trust boundary for local stages unchanged
