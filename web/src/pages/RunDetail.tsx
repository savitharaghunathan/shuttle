import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { apiGet } from "../api/client";
import type { Run } from "../api/types";
import ActionBar from "../components/ActionBar";
import LogViewer from "../components/LogViewer";
import StageList from "../components/StageList";

const TERMINAL = new Set(["succeeded", "failed", "cancelled"]);

export default function RunDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectedStageId, setSelectedStageId] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const data = await apiGet<Run>(`/v1/runs/${id}`);
      setRun(data);
      setError(null);
      setSelectedStageId((prev) => {
        if (prev && data.stages.some((s) => s.id === prev)) return prev;
        if (data.current_stage) {
          const match = data.stages.find((s) => s.id === data.current_stage);
          if (match) return match.id;
        }
        return data.stages[0]?.id ?? null;
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!run || TERMINAL.has(run.status) || busy) return;
    const timer = setInterval(() => {
      void load();
    }, 2000);
    return () => clearInterval(timer);
  }, [run?.status, run?.id, busy, load]);

  if (!id) {
    return (
      <main>
        <p className="error">Missing run id</p>
        <Link to="/">Back to runs</Link>
      </main>
    );
  }

  return (
    <main>
      <header className="page-header">
        <div>
          <p className="breadcrumb">
            <Link to="/">Runs</Link> / {shortId(id)}
          </p>
          <h1>Run {shortId(id)}</h1>
        </div>
        <Link className="button" to="/runs/new">
          New run
        </Link>
      </header>

      {error && <p className="error">{error}</p>}
      {!run && !error && <p className="muted">Loading…</p>}

      {run && (
        <>
          <dl className="meta">
            <div>
              <dt>Status</dt>
              <dd>
                <span className={`status status-${run.status}`}>
                  {run.status}
                </span>
              </dd>
            </div>
            <div>
              <dt>Template</dt>
              <dd>{run.template_id}</dd>
            </div>
            <div>
              <dt>Current stage</dt>
              <dd>{run.current_stage || "—"}</dd>
            </div>
            <div>
              <dt>Agent</dt>
              <dd>
                {[run.agent, run.model].filter(Boolean).join(" / ") || "—"}
              </dd>
            </div>
            <div>
              <dt>Issue</dt>
              <dd>{run.issue_ref || "—"}</dd>
            </div>
            <div>
              <dt>Created</dt>
              <dd>{formatTime(run.created_at)}</dd>
            </div>
            <div>
              <dt>Updated</dt>
              <dd>{formatTime(run.updated_at)}</dd>
            </div>
            {run.parent_run_id && (
              <div>
                <dt>Parent</dt>
                <dd>
                  <Link to={`/runs/${run.parent_run_id}`}>
                    {shortId(run.parent_run_id)}
                  </Link>
                </dd>
              </div>
            )}
          </dl>

          {run.warnings && run.warnings.length > 0 && (
            <ul className="warnings">
              {run.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          )}

          <section>
            <h2>Actions</h2>
            <ActionBar
              run={run}
              busy={busy}
              onBusy={setBusy}
              onUpdated={(updated) => {
                setRun(updated);
                setActionError("");
              }}
              onRetryCreated={(newId) => {
                navigate(`/runs/${newId}`);
              }}
              onError={setActionError}
            />
            {actionError && <p className="error">{actionError}</p>}
            {busy && <p className="muted">Action in progress…</p>}
          </section>

          <section className="detail-grid">
            <div>
              <h2>Stages</h2>
              <StageList
                stages={run.stages}
                selectedId={selectedStageId}
                onSelect={setSelectedStageId}
              />
            </div>
            <div>
              <h2>Log</h2>
              <LogViewer runId={run.id} stageId={selectedStageId} />
            </div>
          </section>
        </>
      )}
    </main>
  );
}

function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
