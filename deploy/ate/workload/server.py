#!/usr/bin/env python3
"""Minimal HTTP task API for shuttle ate actors.

POST /v1/tasks JSON:
  {prompt, model?, branch, repo_url, readonly?}

Clones or updates the repo, checks out branch, runs `claude -p`, commits, pushes.
"""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

PORT = int(os.environ.get("PORT", "8080"))
WORK_ROOT = Path(os.environ.get("SHUTTLE_ACTOR_WORK", "/workspace"))


def run(cmd: list[str], cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, cwd=cwd, check=check, text=True, capture_output=True)


def ensure_repo(repo_url: str, branch: str) -> Path:
    WORK_ROOT.mkdir(parents=True, exist_ok=True)
    repo_dir = WORK_ROOT / "repo"
    if not (repo_dir / ".git").exists():
        run(["git", "clone", repo_url, str(repo_dir)])
    else:
        run(["git", "fetch", "origin"], cwd=repo_dir)
    # Create/reset branch from origin/HEAD if needed
    run(["git", "checkout", "-B", branch], cwd=repo_dir, check=False)
    run(["git", "reset", "--hard", "origin/HEAD"], cwd=repo_dir, check=False)
    run(["git", "checkout", "-B", branch], cwd=repo_dir)
    return repo_dir


def run_claude(repo_dir: Path, prompt: str, model: str, readonly: bool) -> str:
    cmd = ["claude"]
    if model:
        cmd += ["--model", model]
    if not readonly:
        # prefer non-interactive print mode
        pass
    cmd += ["-p", prompt]
    proc = run(cmd, cwd=repo_dir, check=False)
    out = (proc.stdout or "") + (proc.stderr or "")
    if proc.returncode != 0:
        raise RuntimeError(f"claude exited {proc.returncode}: {out[-2000:]}")
    return out


def commit_and_push(repo_dir: Path, branch: str) -> str:
    run(["git", "add", "-A"], cwd=repo_dir)
    status = run(["git", "status", "--porcelain"], cwd=repo_dir)
    if not status.stdout.strip():
        head = run(["git", "rev-parse", "HEAD"], cwd=repo_dir)
        return head.stdout.strip()
    run(
        ["git", "-c", "user.name=shuttle-ate", "-c", "user.email=shuttle-ate@local",
         "commit", "-m", "shuttle ate implement"],
        cwd=repo_dir,
    )
    run(["git", "push", "-u", "origin", branch], cwd=repo_dir)
    head = run(["git", "rev-parse", "HEAD"], cwd=repo_dir)
    return head.stdout.strip()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:
        print(f"[shuttle-actor] {self.address_string()} - {fmt % args}")

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/tasks":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            self.send_error(400, "invalid json")
            return

        prompt = body.get("prompt") or ""
        branch = body.get("branch") or ""
        repo_url = body.get("repo_url") or ""
        model = body.get("model") or ""
        readonly = bool(body.get("readonly"))
        if not prompt or not branch or not repo_url:
            self.send_error(400, "prompt, branch, repo_url required")
            return

        try:
            repo_dir = ensure_repo(repo_url, branch)
            log = run_claude(repo_dir, prompt, model, readonly)
            commit = ""
            if not readonly:
                commit = commit_and_push(repo_dir, branch)
            payload = {
                "status": "succeeded",
                "summary": log[-4000:] if log else "ok",
                "commit": commit,
                "branch": branch,
            }
            data = json.dumps(payload).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception as exc:  # noqa: BLE001
            payload = {"status": "failed", "summary": str(exc), "branch": branch}
            data = json.dumps(payload).encode("utf-8")
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)


def main() -> None:
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"[shuttle-actor] listening on :{PORT}")
    server.serve_forever()


if __name__ == "__main__":
    main()
