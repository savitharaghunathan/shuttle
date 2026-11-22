import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { apiGet } from "../api/client";

export type RunSummary = {
  id: string;
  template_id: string;
  status: string;
  agent?: string;
  model?: string;
  issue_ref?: string;
  created_at: string;
  updated_at: string;
  current_stage: string;
};

export default function RunsList() {
  const [runs, setRuns] = useState<RunSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const data = await apiGet<RunSummary[]>("/v1/runs");
        if (!cancelled) {
          setRuns(data);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
          setRuns([]);
        }
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main>
      <header className="page-header">
        <h1>Shuttle</h1>
        <Link className="button" to="/runs/new">
          New run
        </Link>
      </header>

      {error && <p className="error">{error}</p>}

      {runs === null && !error && <p className="muted">Loading runs…</p>}

      {runs !== null && runs.length === 0 && !error && (
        <p className="empty">
          No runs yet.{" "}
          <Link to="/runs/new">Create a run</Link> to get started.
        </p>
      )}

      {runs !== null && runs.length > 0 && (
        <div className="table-wrap">
          <table className="runs-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Template</th>
                <th>Status</th>
                <th>Stage</th>
                <th>Agent</th>
                <th>Issue</th>
                <th>Updated</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id}>
                  <td>
                    <Link to={`/runs/${run.id}`}>{shortId(run.id)}</Link>
                  </td>
                  <td>{run.template_id}</td>
                  <td>
                    <span className={`status status-${run.status}`}>
                      {run.status}
                    </span>
                  </td>
                  <td>{run.current_stage}</td>
                  <td>
                    {[run.agent, run.model].filter(Boolean).join(" / ") || "—"}
                  </td>
                  <td className="issue">{run.issue_ref || "—"}</td>
                  <td>{formatTime(run.updated_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
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
