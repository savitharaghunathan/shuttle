#!/usr/bin/env bash
# API smoke: create → resume → retry against SHUTTLE_TEST_APP + a GitHub issue.
#
#   export SHUTTLE_TEST_APP=~/local_dev/test
#   export ISSUE_URL=https://github.com/org/repo/issues/2   # optional
#   examples/scripts/serve-test-app.sh   # terminal 1
#   examples/scripts/smoke-test-app.sh   # terminal 2
#
# Uses pr-factory-headless + cursor on implement (slow — real agent).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SHUTTLE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TEST_APP="${SHUTTLE_TEST_APP:-$HOME/local_dev/test}"
SHUTTLE_BIN="${SHUTTLE_BIN:-$SHUTTLE_ROOT/bin/shuttle}"
BASE="${BASE:-http://127.0.0.1:8080}"
ISSUE_URL="${ISSUE_URL:-https://github.com/savitharaghunathan/test/issues/2}"
TEMPLATE="${TEMPLATE:-pr-factory-headless}"
AGENT="${AGENT:-cursor}"
ISSUE_SPEC="$TEST_APP/.shuttle/smoke-issue-spec.md"

# savitharaghunathan/test#2 from default ISSUE_URL
ISSUE_REF="${ISSUE_REF:-$(python3 -c "
import os, re
m = re.search(r'github\\.com/([^/]+/[^/]+)/issues/(\\d+)', os.environ['ISSUE_URL'])
print(f'{m.group(1)}#{m.group(2)}' if m else '')
" ISSUE_URL="$ISSUE_URL")}"

json_field() {
  python3 -c "import json,sys; d=json.load(sys.stdin); print(d['$1'])"
}

json_status() {
  python3 -c "import json,sys; print(json.load(sys.stdin).get('status',''))"
}

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing $1"; exit 1; }; }
need curl
need python3
need gh

if [[ ! -d "$TEST_APP" ]]; then
  echo "error: SHUTTLE_TEST_APP not found: $TEST_APP" >&2
  exit 1
fi
if ! curl -sf "$BASE/v1/pipelines" >/dev/null 2>&1; then
  echo "error: no server at $BASE — run examples/scripts/serve-test-app.sh first" >&2
  exit 1
fi

GH_REPO="${GH_REPO:-$(python3 -c "
import os, re
m = re.search(r'github\\.com/([^/]+/[^/]+)/issues/(\\d+)', os.environ['ISSUE_URL'])
print(m.group(1) if m else '')
" ISSUE_URL="$ISSUE_URL")}"
ISSUE_NUM="${ISSUE_NUM:-$(python3 -c "
import os, re
m = re.search(r'github\\.com/[^/]+/[^/]+/issues/(\\d+)', os.environ['ISSUE_URL'])
print(m.group(1) if m else '')
" ISSUE_URL="$ISSUE_URL")}"

mkdir -p "$TEST_APP/.shuttle"
echo "Fetching issue spec from GitHub ($GH_REPO#$ISSUE_NUM)..."
gh issue view "$ISSUE_NUM" --repo "$GH_REPO" --json title,body \
  -q '"# " + .title + "\n\n" + .body' > "$ISSUE_SPEC"

echo "test_app=$TEST_APP issue=$ISSUE_URL template=$TEMPLATE agent=$AGENT"
echo "spec=$ISSUE_SPEC (gitignored under your test app)"
echo "serve must run from $TEST_APP — use examples/scripts/serve-test-app.sh"
echo

echo "== 1. create run =="
CREATE_BODY=$(TEMPLATE="$TEMPLATE" ISSUE_SPEC="$ISSUE_SPEC" TEST_APP="$TEST_APP" AGENT="$AGENT" ISSUE_URL="$ISSUE_URL" python3 <<'PY'
import json, os
print(json.dumps({
    "template_id": os.environ["TEMPLATE"],
    "spec_path": os.environ["ISSUE_SPEC"],
    "cwd": os.environ["TEST_APP"],
    "agent": os.environ["AGENT"],
    "issue_url": os.environ["ISSUE_URL"],
}))
PY
)
CREATE=$(curl -s -X POST "$BASE/v1/runs" \
  -H 'Content-Type: application/json' \
  -d "$CREATE_BODY")
echo "$CREATE" | python3 -m json.tool

RUN_ID=$(printf '%s' "$CREATE" | json_field id)
STATUS=$(printf '%s' "$CREATE" | json_field status)
RUN_DIR=$(printf '%s' "$CREATE" | json_field run_dir)
PREFIX="${RUN_ID:0:8}"

echo "prefix=$PREFIX status=$STATUS issue_ref=$(printf '%s' "$CREATE" | python3 -c "import json,sys; print(json.load(sys.stdin).get('issue_ref',''))" 2>/dev/null || true)"
echo "run_dir=$RUN_DIR"
if [[ "$RUN_DIR" != "$TEST_APP/.shuttle/runs/"* ]]; then
  echo "ERROR: run_dir is under the wrong work root." >&2
  echo "  got:  $RUN_DIR" >&2
  echo "  want: $TEST_APP/.shuttle/runs/..." >&2
  echo "  → kill \$(lsof -ti :8080) && examples/scripts/serve-test-app.sh" >&2
  exit 1
fi

if [[ "$STATUS" == "blocked" ]]; then
  echo
  echo "== 2. reject gate =="
  curl -s -X POST "$BASE/v1/runs/$PREFIX/reject" \
    -H 'Content-Type: application/json' \
    -d '{"stage_id":"approve_pr","message":"smoke reject"}' | python3 -m json.tool || true
elif [[ "$STATUS" == "failed" ]]; then
  echo
  echo "== 2. failed before gate — skip reject =="
  python3 -c "import json,sys; d=json.load(sys.stdin); f=[s for s in d['stages'] if s['status']=='failed']; print('failed:', f[0]['id'], (f[0].get('summary') or '')[:120] if f else '?')" <<<"$CREATE"
elif [[ "$STATUS" == "succeeded" ]]; then
  echo
  echo "== 2. succeeded — retry will use force =="
fi

echo
echo "== 3. resume =="
RESUME=$(curl -s -X POST "$BASE/v1/runs/$PREFIX/resume")
echo "$RESUME" | python3 -m json.tool
RESUME_STATUS=$(printf '%s' "$RESUME" | json_status)

echo
echo "== 4. retry =="
FORCE=false
if [[ "$STATUS" == "succeeded" || "$RESUME_STATUS" == "succeeded" ]]; then
  FORCE=true
fi
RETRY=$(curl -s -X POST "$BASE/v1/runs/$PREFIX/retry" \
  -H 'Content-Type: application/json' \
  -d "{\"agent\":\"$AGENT\",\"force\":$FORCE}")
echo "$RETRY" | python3 -m json.tool

echo
echo "== 5. list runs =="
if [[ -n "$ISSUE_REF" ]]; then
  ENC_ISSUE=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$ISSUE_REF'))")
  curl -s "$BASE/v1/runs?limit=5&issue=$ENC_ISSUE" | python3 -m json.tool
else
  curl -s "$BASE/v1/runs?limit=5" | python3 -m json.tool
fi

echo
echo "CLI (from $TEST_APP):"
echo "  cd $TEST_APP"
echo "  $SHUTTLE_BIN run -t $TEMPLATE --issue '$ISSUE_URL' --agent $AGENT"
