# Local ate kind test (kind + podman)

Bring up Agent Substrate on a local kind cluster using **podman** as the kind
provider, build shuttle actor images from **Containerfile**s, and run a shell-only
smoke actor (no Anthropic key).

## Prerequisites

- `podman` (machine running on macOS: `podman machine start`)
- `kind` ≥ 0.20
- `kubectl`, `go`, `git`, `curl`
- Disk/time for ate-system install (several minutes, GHCR image pulls)
- An HTTPS git remote the cluster can clone/push (for smoke)

**Podman notes:**
- Agent Substrate’s `ate-setup` may probe the docker CLI. Scripts prepend a
  `docker`→`podman` shim so those calls work with podman.
- Install uses `--sandbox-classes=gvisor` so kind skips the multi‑GB kata/microVM
  download (shuttle remote workers use gVisor).
- Existing clusters need `make ate-kind-down` then `make ate-kind-up` to pick up
  updated Agent Substrate image pins.

## Quick path

```bash
# from repo root (or the ate-remote-workers worktree)
make ate-kind-up

# configure your smoke target (do not commit secrets or private URLs you care about)
cp deploy/ate/kind/smoke.env.example deploy/ate/kind/smoke.env
# edit: SMOKE_REPO_URL=https://github.com/OWNER/REPO.git

make ate-kind-smoke
make ate-kind-clean    # cluster + registry + generated local state
```

Or pass the remote inline:

```bash
SMOKE_REPO_URL=https://github.com/OWNER/REPO.git make ate-kind-smoke
```

## Smoke config

| Source | How |
|---|---|
| `deploy/ate/kind/smoke.env` | Copied from `smoke.env.example` (gitignored) |
| `ATE_KIND_ENV=/path/to/file` | Alternate env file |
| Process environment | `SMOKE_REPO_URL`, `SMOKE_BRANCH`, … |

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `SMOKE_REPO_URL` | **yes** (for smoke) | _(none)_ | HTTPS git remote for the smoke actor |
| `SMOKE_BRANCH` | no | `shuttle/ate-kind-smoke` | Branch the actor commits/pushes |
| `SMOKE_ACTOR` | no | `shuttle-smoke-1` | Actor name |
| `SHUTTLE_TEST_APP` | no | _(none)_ | Optional local checkout path (docs / serve) |

## Cluster env knobs (see `lib.sh`)

| Variable | Default | Meaning |
|---|---|---|
| `KIND_EXPERIMENTAL_PROVIDER` | `podman` | kind container runtime |
| `KIND_CLUSTER_NAME` | `shuttle-ate` | kind cluster name |
| `KIND_REGISTRY_PORT` | `5001` | local image registry |
| `ATE_SRC` | `~/.cache/shuttle/agent-substrate` | substrate checkout |
| `ATE_REF` | `v1.0.1` | substrate tag |
| `ATE_IMAGE_REPO` | `ghcr.io/agent-substrate/substrate` | prebuilt repo |
| `ATE_IMAGE_TAG` | `$ATE_REF` | prebuilt tag |
| `ATE_WORKER_IMAGE` | _(resolved)_ | optional full digest-pinned worker override |
| `ATE_WORKER_IMAGE_NAME` | `ateworker-gvisor` | image name under the repo |
| `ATE_ATESPACE` | `shuttle` | ate atespace |

## Scripts

| Script | Role |
|---|---|
| `up.sh` | cluster → ate-system → images → WorkerPool → templates |
| `apply-workerpool.sh` | resolve the GHCR `ateworker-gvisor` digest + apply `WorkerPool` |
| `down.sh` | delete cluster + registry |
| `smoke.sh` | resume smoke actor + `POST /v1/tasks` |
| `port-forward.sh` | API `:8443` + actor proxy `:8000` |
| `create-cluster.sh` | kind+podman + local registry only |
| `build-images.sh` | `podman build -f Containerfile` + push |

## Images

- ate-system uses prebuilt release images under
  `ghcr.io/agent-substrate/substrate` with tag `v1.0.1`.
- WorkerPool uses the published `ateworker-gvisor` image under the same repo,
  resolved to an `@sha256:` digest at apply time. Set `ATE_WORKER_IMAGE` to a
  full digest-pinned image ref to override it. The worker is no longer built
  with `ko` by default.
- Claude actor: `deploy/ate/workload/Containerfile` + `server.sh` / `handle.sh`
- Smoke actor: `deploy/ate/kind/smoke-workload/Containerfile` (shell, no LLM)

## shuttle.yaml for local testing

```yaml
ate:
  endpoint: "127.0.0.1:8443"
  atespace: shuttle
  actor_template: shuttle-smoke   # or shuttle-claude
  task_url: "http://127.0.0.1:8000"
  insecure: true
```

Then, with port-forwards running:

```bash
shuttle run --spec ./spec.md --agent claude --remote --headless
```

For Claude, set egress/credentials per https://ate.dev/docs/guides/egress-credential-injection/ and use `actor_template: shuttle-claude`.
