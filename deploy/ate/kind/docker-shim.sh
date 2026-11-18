#!/usr/bin/env bash
# ate-setup hardcodes the docker CLI (including `docker buildx build --push`).
# When kind uses podman, put a directory containing this script (named `docker`)
# first on PATH.
#
# Podman rejects docker-buildx flags such as --push (and --metadata-file in
# remote/machine mode). Translate the buildx invocation ate-setup uses into
# podman build + podman push, and write a buildx-compatible metadata file.
set -euo pipefail

if [[ "${1:-}" == "buildx" && "${2:-}" == "build" ]]; then
  shift 2
  build_args=()
  metadata_file=""
  tag=""
  do_push=0
  # Drop buildx-only / unsupported flags; keep the rest for `podman build`.
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --push)
        do_push=1
        shift
        ;;
      --metadata-file)
        metadata_file="${2:-}"
        shift 2
        ;;
      --metadata-file=*)
        metadata_file="${1#*=}"
        shift
        ;;
      -t|--tag)
        tag="${2:-}"
        build_args+=("$1" "$2")
        shift 2
        ;;
      -t=*|--tag=*)
        tag="${1#*=}"
        build_args+=("$1")
        shift
        ;;
      --cache-from|--cache-to)
        # GHA/registry cache specs are docker-buildx specific; skip pair.
        shift 2
        ;;
      --cache-from=*|--cache-to=*)
        shift
        ;;
      *)
        build_args+=("$1")
        shift
        ;;
    esac
  done

  if [[ -z "${tag}" ]]; then
    echo "docker-shim: buildx build requires -t/--tag" >&2
    exit 1
  fi

  podman build "${build_args[@]}"

  if [[ "${do_push}" -eq 1 ]]; then
    digest_file="$(mktemp)"
    # kind local registry is plain HTTP on localhost.
    if [[ "${tag}" == localhost:* || "${tag}" == 127.0.0.1:* ]]; then
      podman push --tls-verify=false --digestfile="${digest_file}" "${tag}"
    else
      podman push --digestfile="${digest_file}" "${tag}"
    fi
    digest="$(tr -d '[:space:]' <"${digest_file}")"
    rm -f "${digest_file}"
    if [[ -z "${digest}" ]]; then
      echo "docker-shim: podman push wrote an empty digest for ${tag}" >&2
      exit 1
    fi
    # ate-setup expects buildx metadata JSON with containerimage.digest.
    if [[ -n "${metadata_file}" ]]; then
      printf '{"containerimage.digest":"%s","image.name":"%s"}\n' "${digest}" "${tag}" >"${metadata_file}"
    fi
  elif [[ -n "${metadata_file}" ]]; then
    echo "docker-shim: --metadata-file without --push is not supported" >&2
    exit 1
  fi
  exit 0
fi

exec podman "$@"
