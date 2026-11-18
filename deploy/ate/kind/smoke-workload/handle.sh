#!/bin/sh
# Smoke actor: no LLM. Clone/update repo, write marker, commit, push.
set -eu

WORK_ROOT="${SHUTTLE_ACTOR_WORK:-/workspace}"

json_get() {
  key="$1"
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" | head -n1
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
branch=$(printf '%s' "${body}" | json_get branch)
repo_url=$(printf '%s' "${body}" | json_get repo_url)
token=$(printf '%s' "${body}" | json_get token)

if [ -z "${branch}" ] || [ -z "${repo_url}" ]; then
  http_json "400 Bad Request" '{"status":"failed","summary":"branch and repo_url required"}'
  exit 0
fi

# Prefer MITM+system roots projected by ActorTemplate; fall back to image CAs.
if [ -f /run/ate/egress-trust-bundle/trust-bundle.pem ]; then
  export SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem
  export GIT_SSL_CAINFO=/run/ate/egress-trust-bundle/trust-bundle.pem
fi
export GIT_TERMINAL_PROMPT=0

auth_url="${repo_url}"
if [ -n "${token}" ]; then
  # https://github.com/org/repo.git → https://x-access-token:TOKEN@github.com/org/repo.git
  auth_url=$(printf '%s' "${repo_url}" | sed "s#https://#https://x-access-token:${token}@#")
fi

mkdir -p "${WORK_ROOT}"
repo_dir="${WORK_ROOT}/repo"
if [ ! -d "${repo_dir}/.git" ]; then
  git clone "${auth_url}" "${repo_dir}" >/tmp/clone.log 2>&1 || {
    summary=$(tr '\n' ' ' </tmp/clone.log | sed 's/"/\\"/g' | cut -c1-1500)
    http_json "500 Internal Server Error" "{\"status\":\"failed\",\"summary\":\"clone failed: ${summary}\"}"
    exit 0
  }
else
  git -C "${repo_dir}" fetch origin >/dev/null 2>&1 || true
fi
git -C "${repo_dir}" checkout -B "${branch}" >/dev/null 2>&1 || true
if [ -n "${token}" ]; then
  git -C "${repo_dir}" remote set-url origin "${auth_url}" >/dev/null 2>&1 || true
fi

printf 'smoke ok\nprompt=%s\n' "${prompt}" >"${repo_dir}/.shuttle-ate-smoke"
git -C "${repo_dir}" add -A
git -C "${repo_dir}" -c user.name=shuttle-smoke -c user.email=shuttle-smoke@local \
  commit -m "shuttle ate kind smoke" >/dev/null 2>&1 || true

summary="smoke succeeded"
if ! git -C "${repo_dir}" push -u origin "${branch}" >/tmp/push.log 2>&1; then
  summary="smoke committed locally; push failed: $(tr '\n' ' ' </tmp/push.log | sed 's/"/\\"/g' | cut -c1-800)"
fi
commit=$(git -C "${repo_dir}" rev-parse HEAD)
http_json "200 OK" "{\"status\":\"succeeded\",\"summary\":\"${summary}\",\"commit\":\"${commit}\",\"branch\":\"${branch}\"}"
