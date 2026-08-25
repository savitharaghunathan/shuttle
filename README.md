# shuttle

AI software factory control plane. See [docs/plan/design.md](docs/plan/design.md) for architecture and trust boundaries.

## Status

Early scaffolding — config loader and module init only.

## Trust boundary

Workers (`shell`, `exec`, and agent CLIs) run with the invoking user's privileges. Only run trusted pipeline YAML.

## Local HTTP API

`shuttle serve` starts an unauthenticated API on `127.0.0.1:8080` for local use only.
