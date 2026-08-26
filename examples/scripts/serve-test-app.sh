#!/usr/bin/env bash
# Start shuttle serve from your local test-app checkout (not the shuttle repo).
#
#   export SHUTTLE_TEST_APP=~/local_dev/test    # your checkout
#   make -C "$(dirname "$0")/../.." build
#   examples/scripts/serve-test-app.sh
#
# Stop any other server on :8080 first:
#   kill $(lsof -ti :8080)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SHUTTLE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_APP="${SHUTTLE_TEST_APP:-$HOME/local_dev/test}"
SHUTTLE_BIN="${SHUTTLE_BIN:-$SHUTTLE_ROOT/bin/shuttle}"

if [[ ! -d "$TEST_APP" ]]; then
  echo "error: SHUTTLE_TEST_APP not found: $TEST_APP" >&2
  exit 1
fi
if [[ ! -x "$SHUTTLE_BIN" ]]; then
  echo "error: build shuttle first: make -C $SHUTTLE_ROOT build" >&2
  exit 1
fi
if lsof -i :8080 -sTCP:LISTEN >/dev/null 2>&1; then
  echo "error: port 8080 already in use:" >&2
  lsof -i :8080 -sTCP:LISTEN >&2 || true
  echo "  kill \$(lsof -ti :8080)" >&2
  exit 1
fi

echo "serving $TEST_APP on http://127.0.0.1:8080"
cd "$TEST_APP"
exec "$SHUTTLE_BIN" serve
