# Shuttle + Agent Substrate (ate)

Remote agent stages (`shuttle run --remote`) require a pre-provisioned ate cluster.
Shuttle does **not** install ate; it talks to `ate-api-server` over gRPC and posts
tasks to the actor HTTP API.

## Prerequisites

1. Agent Substrate installed (kind quickstart or GKE): https://ate.dev/docs/get-started/quickstart/
2. A gVisor `WorkerPool` with capacity for your actors
3. An atespace (example: `shuttle`)
4. Actor template `shuttle-claude` built from this directory’s workload image
5. Egress policy allowing Anthropic + your git host; credentials injected via ate egress (preferred)

## Build & push the workload image

```bash
export KO_DOCKER_REPO=...   # or your registry
docker buildx build --platform linux/amd64 -t ${KO_DOCKER_REPO}/shuttle-claude-actor:latest ./workload
docker push ${KO_DOCKER_REPO}/shuttle-claude-actor:latest
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
  endpoint: "127.0.0.1:8443"   # port-forward svc/api -n ate-system
  atespace: shuttle
  actor_template: shuttle-claude
  task_url: "http://127.0.0.1:8000"  # kubectl ate proxy / router
  insecure: true
```

## Run

```bash
shuttle run --issue https://github.com/org/repo/issues/1 --agent claude --remote --headless
```

Unsupported agents under `--remote` fail immediately (no local fallback).
