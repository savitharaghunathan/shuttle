import { useEffect, useState } from "react";

type Props = {
  runId: string;
  stageId: string | null;
};

export default function LogViewer({ runId, stageId }: Props) {
  const [text, setText] = useState("");
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!stageId) {
      setText("");
      setTruncated(false);
      setError(null);
      setLoading(false);
      return;
    }

    let cancelled = false;
    const ctrl = new AbortController();

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const res = await fetch(
          `/v1/runs/${runId}/stages/${stageId}/log?tail=200`,
          { signal: ctrl.signal },
        );
        const body = await res.text();
        if (!res.ok) {
          let msg = res.statusText || `HTTP ${res.status}`;
          try {
            const parsed = JSON.parse(body) as { error?: string };
            if (parsed.error) msg = parsed.error;
          } catch {
            /* plain text error */
          }
          throw new Error(msg);
        }
        if (cancelled) return;
        setText(body);
        setTruncated(res.headers.get("X-Shuttle-Log-Truncated") === "true");
      } catch (err) {
        if (cancelled || (err instanceof DOMException && err.name === "AbortError")) {
          return;
        }
        setText("");
        setTruncated(false);
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void load();
    return () => {
      cancelled = true;
      ctrl.abort();
    };
  }, [runId, stageId]);

  if (!stageId) {
    return <p className="muted">Select a stage to view its log.</p>;
  }

  return (
    <div className="log-viewer">
      {loading && <p className="muted">Loading log…</p>}
      {error && <p className="error">{error}</p>}
      {truncated && (
        <p className="notice">Log truncated (tail or size cap).</p>
      )}
      {!loading && !error && (
        <pre className="log-body">{text || "(empty)"}</pre>
      )}
    </div>
  );
}
