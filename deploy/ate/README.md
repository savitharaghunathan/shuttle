# Shuttle + Agent Substrate (ate)

Remote agent stages (`shuttle run --remote`) require a pre-provisioned ate cluster.
Shuttle does **not** install ate; it talks to `ate-api-server` over gRPC and posts
tasks to the actor HTTP API.

## Local test setup (kind + podman)

Preferred path for development:

```bash
make ate-kind-up
cp deploy/ate/kind/smoke.env.example deploy/ate/kind/smoke.env
# set SMOKE_REPO_URL=https://github.com/OWNER/REPO.git
make ate-kind-smoke
make ate-kind-down
```

Or: `SMOKE_REPO_URL=https://github.com/OWNER/REPO.git make ate-kind-smoke`

Details: [kind/README.md](kind/README.md).

## Prerequisites (manual / non-kind)

1. Agent Substrate installed: https://ate.dev/docs/get-started/quickstart/
2. A gVisor `WorkerPool` with capacity for your actors
3. An atespace (example: `shuttle`)
4. Actor template built from this directory’s workload image
5. Egress policy allowing Anthropic + your git host

## Build & push the workload image

Images use **Containerfile** (podman-friendly) and **shell** entrypoints (no Python).

```bash
export KO_DOCKER_REPO=...   # or localhost:5001 for kind
podman build -f workload/Containerfile -t ${KO_DOCKER_REPO}/shuttle-claude-actor:latest ./workload
podman push --tls-verify=false ${KO_DOCKER_REPO}/shuttle-claude-actor:latest
```

Pin the digest in `actor-template.yaml` before applying.

## Create template + egress

```bash
kubectl ate create atespace shuttle   # if needed
kubectl ate create actor-template -f actor-template.yaml
# attach egress for api.anthropic.com and github.com per ate docs
```

## shuttle.yaml

```yaml
ate:
  endpoint: "127.0.0.1:8443"          # port-forward svc/api -n ate-system
  atespace: shuttle
  actor_template: shuttle-claude
  task_url: "http://127.0.0.1:8000" # kubectl ate proxy / router
  insecure: true
```

## Run

```bash
shuttle run --issue https://github.com/org/repo/issues/1 --agent claude --remote --headless
```

Unsupported agents under `--remote` fail immediately (no local fallback).
