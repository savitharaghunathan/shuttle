#!/bin/sh
set -eu
PORT="${PORT:-8080}"
DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
echo "[shuttle-smoke] listening on :${PORT}" >&2
while true; do
  busybox nc -l -p "${PORT}" -e "${DIR}/handle.sh"
done
