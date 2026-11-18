#!/bin/sh
# ate-setup hardcodes `docker info` / docker CLI. When kind uses podman, put a
# directory containing this script (named `docker`) first on PATH.
exec podman "$@"
