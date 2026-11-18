#!/bin/sh
# Handle one HTTP connection on stdin/stdout.
set -eu

WORK_ROOT="${SHUTTLE_ACTOR_WORK:-/workspace}"

json_get() {
  key="$1"
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" | head -n1
}

json_bool_true() {
  key="$1"
  grep -q "\"${key}\"[[:space:]]*:[[:space:]]*true"
}

http_json() {
  code="$1"
  payload="$2"
  len=$(printf '%s' "${payload}" | wc -c | tr -d ' ')
  printf 'HTTP/1.1 %s\r\n' "${code}"
  printf 'Content-Type: application/json\r\n'
  printf 'Content-Length: %s\r\n' "${len}"
  printf 'Connection: close\r\n\r\n'
  printf '%s' "${payload}"
}

method=""
path=""
content_length=0
while IFS= read -r line; do
  line=$(printf '%s' "${line}" | tr -d '\r')
  [ -z "${line}" ] && break
  case "${line}" in
    GET\ *|POST\ *|PUT\ *|DELETE\ *)
      method=$(printf '%s' "${line}" | awk '{print $1}')
      path=$(printf '%s' "${line}" | awk '{print $2}')
      ;;
    [Cc]ontent-[Ll]ength:*)
      content_length=$(printf '%s' "${line}" | awk '{print $2}')
      ;;
  esac
done

body=""
if [ "${content_length}" -gt 0 ] 2>/dev/null; then
  body=$(dd bs=1 count="${content_length}" 2>/dev/null || true)
fi

if [ "${method}" != "POST" ] || [ "${path}" != "/v1/tasks" ]; then
  http_json "404 Not Found" '{"status":"failed","summary":"not found"}'
  exit 0
fi

prompt=$(printf '%s' "${body}" | json_get prompt)
model=$(printf '%s' "${body}" | json_get model)
branch=$(printf '%s' "${body}" | json_get branch)
repo_url=$(printf '%s' "${body}" | json_get repo_url)
readonly=false
if printf '%s' "${body}" | json_bool_true readonly; then
  readonly=true
fi

if [ -z "${prompt}" ] || [ -z "${branch}" ] || [ -z "${repo_url}" ]; then
  http_json "400 Bad Request" '{"status":"failed","summary":"prompt, branch, repo_url required"}'
  exit 0
fi

mkdir -p "${WORK_ROOT}"
repo_dir="${WORK_ROOT}/repo"
if [ ! -d "${repo_dir}/.git" ]; then
  git clone "${repo_url}" "${repo_dir}" >/tmp/clone.log 2>&1 || {
    summary=$(tr '\n' ' ' </tmp/clone.log | sed 's/"/\\"/g' | cut -c1-1500)
    http_json "500 Internal Server Error" "{\"status\":\"failed\",\"summary\":\"clone failed: ${summary}\"}"
    exit 0
  }
else
  git -C "${repo_dir}" fetch origin >/dev/null 2>&1 || true
fi
git -C "${repo_dir}" checkout -B "${branch}" >/dev/null 2>&1 || true

if [ -n "${model}" ]; then
  log=$(claude --model "${model}" -p "${prompt}" 2>&1) || rc=$?
else
  log=$(claude -p "${prompt}" 2>&1) || rc=$?
fi
rc=${rc:-0}
if [ "${rc}" -ne 0 ]; then
  summary=$(printf '%s' "${log}" | tr '\n' ' ' | sed 's/"/\\"/g' | cut -c1-2000)
  http_json "500 Internal Server Error" "{\"status\":\"failed\",\"summary\":\"${summary}\",\"branch\":\"${branch}\"}"
  exit 0
fi

commit=""
if [ "${readonly}" != "true" ]; then
  git -C "${repo_dir}" add -A
  if [ -n "$(git -C "${repo_dir}" status --porcelain)" ]; then
    git -C "${repo_dir}" -c user.name=shuttle-ate -c user.email=shuttle-ate@local \
      commit -m "shuttle ate implement" >/dev/null
  fi
  git -C "${repo_dir}" push -u origin "${branch}" >/tmp/push.log 2>&1 || {
    summary=$(tr '\n' ' ' </tmp/push.log | sed 's/"/\\"/g' | cut -c1-1500)
    http_json "500 Internal Server Error" "{\"status\":\"failed\",\"summary\":\"push failed: ${summary}\",\"branch\":\"${branch}\"}"
    exit 0
  }
  commit=$(git -C "${repo_dir}" rev-parse HEAD)
fi

summary=$(printf '%s' "${log}" | tr '\n' ' ' | sed 's/"/\\"/g' | cut -c1-4000)
http_json "200 OK" "{\"status\":\"succeeded\",\"summary\":\"${summary}\",\"commit\":\"${commit}\",\"branch\":\"${branch}\"}"
