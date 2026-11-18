#!/bin/sh
# Listen for HTTP task requests and hand each connection to handle.sh.
set -eu

PORT="${PORT:-8080}"
DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"

echo "[shuttle-actor] listening on :${PORT}" >&2
while true; do
  # netcat-traditional supports -e; required for one-shot connection handling.
  nc -l -p "${PORT}" -e "${DIR}/handle.sh"
done
