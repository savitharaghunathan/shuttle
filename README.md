# shuttle

AI software factory control plane. See [docs/plan/design.md](docs/plan/design.md) for architecture and trust boundaries.

## Status

Early scaffolding — config loader and module init only.

## Trust boundary

Workers (`shell`, `exec`, and agent CLIs) run with the invoking user's privileges. Only run trusted pipeline YAML.
