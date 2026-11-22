import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { apiGet, apiJSON, longTimeoutMs } from "../api/client";

type PipelineList = {
  pipelines: { id: string }[];
};

type CreatedRun = {
  id: string;
};

export default function NewRun() {
  const navigate = useNavigate();
  const [pipelines, setPipelines] = useState<{ id: string }[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [templateId, setTemplateId] = useState("");
  const [issueUrl, setIssueUrl] = useState("");
  const [specPath, setSpecPath] = useState("");
  const [headless, setHeadless] = useState(false);
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const [remote, setRemote] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const data = await apiGet<PipelineList>("/v1/pipelines");
        if (cancelled) return;
        const list = data.pipelines ?? [];
        setPipelines(list);
        setLoadError(null);
        if (list.length > 0) {
          setTemplateId((prev) => prev || list[0].id);
        }
      } catch (err) {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : String(err));
        }
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const issue = issueUrl.trim();
    const spec = specPath.trim();
    const hasIssue = issue !== "";
    const hasSpec = spec !== "";
    if (hasIssue === hasSpec) {
      setError("Provide exactly one of issue URL or absolute spec path.");
      return;
    }
    if (!templateId) {
      setError("Select a template.");
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const created = await apiJSON<CreatedRun>(`/v1/runs`, {
        method: "POST",
        timeoutMs: longTimeoutMs,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          template_id: templateId,
          issue_url: issue || undefined,
          spec_path: spec || undefined,
          headless,
          agent: agent.trim() || undefined,
          model: model.trim() || undefined,
          remote,
        }),
      });
      navigate(`/runs/${created.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main>
      <header className="page-header">
        <div>
          <p className="breadcrumb">
            <Link to="/">Runs</Link> / New
          </p>
          <h1>New run</h1>
        </div>
        <Link to="/">Back to runs</Link>
      </header>

      {loadError && <p className="error">{loadError}</p>}

      <form className="run-form" onSubmit={onSubmit}>
        <label className="field">
          <span className="field-label">Template</span>
          <select
            value={templateId}
            onChange={(e) => setTemplateId(e.target.value)}
            disabled={submitting || pipelines.length === 0}
            required
          >
            {pipelines.length === 0 && <option value="">Loading…</option>}
            {pipelines.map((p) => (
              <option key={p.id} value={p.id}>
                {p.id}
              </option>
            ))}
          </select>
        </label>

        <label className="field">
          <span className="field-label">Issue URL</span>
          <input
            type="url"
            name="issue_url"
            placeholder="https://github.com/org/repo/issues/1"
            value={issueUrl}
            onChange={(e) => setIssueUrl(e.target.value)}
            disabled={submitting}
            autoComplete="off"
          />
        </label>

        <details className="advanced">
          <summary>Advanced</summary>
          <label className="field">
            <span className="field-label">Absolute spec path</span>
            <input
              type="text"
              name="spec_path"
              placeholder="/absolute/path/to/spec.md"
              value={specPath}
              onChange={(e) => setSpecPath(e.target.value)}
              disabled={submitting}
              autoComplete="off"
            />
            <span className="field-help">
              Path on the machine running <code>shuttle serve</code>, not your
              browser.
            </span>
          </label>
        </details>

        <label className="field check">
          <input
            type="checkbox"
            checked={headless}
            onChange={(e) => setHeadless(e.target.checked)}
            disabled={submitting}
          />
          <span>Headless</span>
        </label>

        <label className="field">
          <span className="field-label">Agent</span>
          <input
            type="text"
            name="agent"
            value={agent}
            onChange={(e) => setAgent(e.target.value)}
            disabled={submitting}
            autoComplete="off"
          />
        </label>

        <label className="field">
          <span className="field-label">Model</span>
          <input
            type="text"
            name="model"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            disabled={submitting}
            autoComplete="off"
          />
        </label>

        <label className="field check">
          <input
            type="checkbox"
            checked={remote}
            onChange={(e) => setRemote(e.target.checked)}
            disabled={submitting}
          />
          <span>Remote</span>
        </label>

        {error && <p className="error">{error}</p>}

        <div className="form-actions">
          <button type="submit" disabled={submitting || !templateId}>
            {submitting ? "Creating…" : "Create run"}
          </button>
        </div>
      </form>
    </main>
  );
}
