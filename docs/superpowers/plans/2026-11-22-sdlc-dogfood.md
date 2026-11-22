# SDLC Dogfood Execution Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or subagent-driven-development) to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. This is an **ops / verification** plan, not a product-code change set.

**Goal:** Complete the gated `sdlc` dogfood checklist on the shuttle repo itself, close the throwaway PR without merging, then mark **Full SDLC pipeline** as Shipped on `feat/sdlc-dogfood-and-dashboard`.

**Architecture:** Disposable git worktree from `main` + disposable `SHUTTLE_HOME` so the primary registry is untouched. Cursor (`cursor-agent`) runs plan/design/implement/review; `ship` opens a PR via scaffolded `ship.command`; PR is closed without merge.

**Tech Stack:** Shuttle CLI on `main`, `cursor-agent`, `gh`, git worktrees.

**Spec:** [`docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md`](../specs/2026-11-22-sdlc-dogfood-design.md) (rev 3)

## Global Constraints

- Do **not** dogfood from `feat/sdlc-dogfood-and-dashboard` while it has WIP; run only inside the dogfood worktree
- Disposable `SHUTTLE_HOME` only — never Upsert the default registry to the worktree path
- Close dogfood PR **without merge**; do not cherry-pick the unit test onto the planning branch
- Do not commit scaffolded `shuttle.yaml`
- Ask the user for attribution before any commit on the planning branch; no `Co-authored-by: Cursor`
- Dashboard implementation is out of scope for this plan
- **Session log (required):** Append every command, exit code, and relevant stdout/stderr to `docs/superpowers/dogfood/2026-11-22-sdlc-session-log.md` as work proceeds. Also keep per-task SDD reports under `.superpowers/sdd/task-N-report.md`. Do not delete prior log entries. Redact secrets.

---

## File Map

| File | Responsibility |
|------|----------------|
| `.worktrees/sdlc-dogfood/` | Disposable worktree (gitignored; not committed) |
| `$WT/.shuttle-home/` | Disposable `SHUTTLE_HOME` |
| `$WT/shuttle.yaml` | Local scaffold only — never commit |
| `$WT/docs/dogfood-sdlc-spec.md` | Tiny run spec for the agent |
| `docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md` | Fill Evidence table after run |
| `docs/superpowers/dogfood/2026-11-22-sdlc-session-log.md` | Append-only chronology of all dogfood commands/outputs |
| `docs/ROADMAP.md` | Move Full SDLC → Shipped (on planning branch only) |

**Chosen helper for the throwaway unit test:** `issue.isValidName` in `internal/issue/ref.go` (same-package tests in `internal/issue/ref_test.go`). Agent adds `TestIsValidName` covering empty string, valid `acme.ops`, and invalid `acme/ops`.

---

### Task 1: Preflight — tools and worktree

**Files:** none in git (ops only)

- [ ] **Step 1: Verify tools**

```bash
command -v cursor-agent
command -v gh
gh auth status
```

Expected: both commands resolve; `gh` authenticated for github.com.

- [ ] **Step 2: Fetch main and create worktree**

From the primary shuttle checkout (any branch):

```bash
git fetch origin main
REPO_ROOT="$(git rev-parse --show-toplevel)"
WT="$REPO_ROOT/.worktrees/sdlc-dogfood"
mkdir -p "$REPO_ROOT/.worktrees"
git worktree add "$WT" origin/main
echo "$WT"
```

Expected: worktree created; `echo` prints an absolute path.

- [ ] **Step 3: Build shuttle inside the worktree**

```bash
cd "$WT"
make build
export PATH="$WT/bin:$PATH"
command -v shuttle
shuttle version 2>/dev/null || shuttle --help | head -5
```

Expected: `bin/shuttle` exists and is first on `PATH`.

- [ ] **Step 4: Disposable home + import**

```bash
export SHUTTLE_HOME="$WT/.shuttle-home"
mkdir -p "$SHUTTLE_HOME"
shuttle import repo --path "$WT"
shuttle repos list
```

Expected: `imported: savitharaghunathan/shuttle` (or the origin owner/name); `repos list` shows **only** this home’s entry pointing at `$WT`.

- [ ] **Step 5: Scaffold and verify ship.command**

```bash
cd "$WT"
# Import already scaffolds if missing; if shuttle.yaml absent:
#   shuttle import repo --path "$WT"   # idempotent enough, or rely on WriteShuttleYAMLIfAbsent via import
test -f shuttle.yaml
grep -A2 '^ship:' shuttle.yaml
```

Expected: `ship.command` uses `pr open` (not `echo` stub). If still echo, re-scaffold via import path or write production ship block matching `internal/repo/scaffold.go`.

- [ ] **Step 6: Confirm default registry untouched**

In a **new shell** without `SHUTTLE_HOME`:

```bash
unset SHUTTLE_HOME
shuttle repos list || true
```

Expected: either empty, or shuttle still pointing at the operator’s normal checkout — **not** `$WT`.

---

### Task 2: Author spec and run gated sdlc

**Files:**
- Create (worktree only): `docs/dogfood-sdlc-spec.md`

- [ ] **Step 1: Write the run spec**

```bash
cd "$WT"
mkdir -p docs
cat > docs/dogfood-sdlc-spec.md <<'EOF'
# Dogfood: unit test for issue.isValidName

Add a focused unit test for the existing helper `isValidName` in package `issue`
(`internal/issue/ref.go`). Put tests in `internal/issue/ref_test.go` as `TestIsValidName`.

Requirements:
- Test empty string → false
- Test `"acme.ops"` → true
- Test `"acme/ops"` → false (slash invalid)
- Do NOT change production code in `ref.go` unless a compile fix is required
- Do NOT create, modify, or commit `shuttle.yaml`
- Do NOT edit unrelated files
- Use branch {{.Branch}} exactly (Shuttle injects this). Create it from the default
  branch if needed, commit only the test file, and push to origin.

This is a throwaway dogfood change; keep the diff minimal.
EOF
```

- [ ] **Step 2: Start gated sdlc**

```bash
cd "$WT"
export SHUTTLE_HOME="$WT/.shuttle-home"
export PATH="$WT/bin:$PATH"
SPEC="$WT/docs/dogfood-sdlc-spec.md"
shuttle run -t sdlc --spec "$SPEC" --agent cursor
```

Expected: run starts; plan/design/implement/test/review execute (long). Run blocks on `approve_ship` or prints waiting. Note the **run ID** from output.

- [ ] **Step 3: On failure, resume/retry only in the worktree**

```bash
shuttle resume <run-id>    # or shuttle retry <run-id> per failure mode
```

Do not continue from the planning-branch checkout.

---

### Task 3: Inspect artifacts, approve, ship, verify PR

- [ ] **Step 1: Inspect artifacts**

```bash
cd "$WT"
RUN_ID="<paste-id>"
ls -la ".shuttle/runs/$RUN_ID/artifacts/plan/"
ls -la ".shuttle/runs/$RUN_ID/artifacts/design/"
ls -la ".shuttle/runs/$RUN_ID/artifacts/implement/"
# Summaries may be files named summary or under stage dirs — print whatever exists:
find ".shuttle/runs/$RUN_ID/artifacts" -type f | head -50
wc -c ".shuttle/runs/$RUN_ID/artifacts/plan/"* ".shuttle/runs/$RUN_ID/artifacts/design/"* ".shuttle/runs/$RUN_ID/artifacts/implement/"* 2>/dev/null || true
```

Expected: non-empty plan/design/implement artifacts.

- [ ] **Step 2: Approve ship**

```bash
export SHUTTLE_HOME="$WT/.shuttle-home"
export PATH="$WT/bin:$PATH"
cd "$WT"
shuttle approve "$RUN_ID" approve_ship
```

Expected: `ship` stage runs; output includes `pr: https://github.com/...`

- [ ] **Step 3: Confirm idempotent pr open**

```bash
# Capture branch from run status / implement summary, or:
BRANCH="$(gh pr list --head "$(git -C "$WT" rev-parse --abbrev-ref HEAD)" --json headRefName -q '.[0].headRefName' 2>/dev/null || true)"
# Safer: list open PRs from this dogfood:
gh pr list --repo savitharaghunathan/shuttle --state open --limit 5
# Re-run pr open against the run branch (from status):
shuttle status "$RUN_ID"
# If branch known:
shuttle pr open --branch "<run-branch>" --cwd "$WT"
```

Expected: second open prints the **same** PR URL (idempotent).

- [ ] **Step 4: Confirm PR does not include shuttle.yaml**

```bash
PR_URL="<paste>"
gh pr diff "$PR_URL" | head -200
gh pr diff "$PR_URL" --name-only
```

Expected: files are only the test (e.g. `internal/issue/ref_test.go`); **no** `shuttle.yaml`.

---

### Task 4: Close PR and tear down dogfood environment

- [ ] **Step 1: Close PR without merge**

```bash
gh pr close "$PR_URL" --comment "SDLC dogfood throwaway — closing without merge per docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md"
# Delete remote branch if still present:
gh pr view "$PR_URL" --json headRefName -q .headRefName
git push origin --delete "<head-branch>" || true
```

Expected: PR closed; remote branch deleted.

- [ ] **Step 2: Remove worktree and disposable home**

From `$REPO_ROOT`:

```bash
git worktree remove --force "$WT"
rm -rf "$WT" 2>/dev/null || true
# If SHUTTLE_HOME was under WT it is already gone; if not:
rm -rf "$REPO_ROOT/.worktrees/sdlc-dogfood"
git worktree prune
```

Expected: `git worktree list` no longer shows sdlc-dogfood.

- [ ] **Step 3: Re-check default registry**

```bash
unset SHUTTLE_HOME
shuttle repos list || true
```

Expected: unchanged from Task 1 Step 6 (not pointing at removed worktree).

---

### Task 5: Record evidence and mark ROADMAP Shipped

**Files:**
- Modify: `docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md` (Evidence table)
- Modify: `docs/ROADMAP.md`

**Interfaces:**
- Consumes: run id, PR URL, worktree path, `SHUTTLE_HOME`, helper name from Tasks 1–4
- Produces: ROADMAP Full SDLC under **Shipped**

- [ ] **Step 1: Switch to planning branch and fill Evidence**

```bash
cd "$REPO_ROOT"
git checkout feat/sdlc-dogfood-and-dashboard
```

Edit `docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md` Evidence table with real values (Date, Run ID, Spec path, Helper / test added, binary + HEAD, Worktree path, SHUTTLE_HOME, PR URL, Closed yes, Notes).

- [ ] **Step 2: Move Full SDLC to Shipped in ROADMAP**

In `docs/ROADMAP.md`:

1. Cut the entire `### Full SDLC pipeline` section from **Next** (including dogfood checklist bullets).
2. Paste under **Shipped** as a completed capability, shortened to shipped voice, and cite the dogfood spec + PR URL / date.
3. Leave **Local web dashboard** as the first **Next** item.

Example shipped blurb:

```markdown
### Full SDLC pipeline
Embedded `sdlc` / `sdlc-headless` with artifact injection, strict outputs, readonly stages, and ship guard.
Dogfood completed 2026-11-22 (gated run on shuttle; throwaway PR closed): see
`docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md`.
```

- [ ] **Step 3: Commit on planning branch (ask attribution first)**

Ask the user what attribution (if any) to include. Then:

```bash
git add docs/ROADMAP.md docs/superpowers/specs/2026-11-22-sdlc-dogfood-design.md
git status
git commit -m "$(cat <<'EOF'
Mark Full SDLC pipeline shipped after dogfood.

EOF
)"
```

Do **not** add `Co-authored-by: Cursor`.

- [ ] **Step 4: Verify success criteria**

- [ ] Gated sdlc completed through ship with PR
- [ ] Artifacts inspected
- [ ] PR closed without merge; worktree/home removed
- [ ] Default registry OK
- [ ] ROADMAP updated

---

## Self-review (plan vs spec)

| Spec requirement | Task |
|------------------|------|
| Worktree from main | Task 1 |
| Disposable SHUTTLE_HOME + import --path | Task 1 |
| cursor-agent / four agent stages / wall time | Task 1–2 |
| Unit test for existing helper; no shuttle.yaml in PR | Task 2–3 |
| Approve → ship → idempotent PR | Task 3 |
| Close without merge; teardown | Task 4 |
| Evidence + ROADMAP Shipped | Task 5 |
| No dashboard work | Global constraints |

No TBD placeholders. Optional `sdlc-headless` intentionally omitted (spec: not required).
