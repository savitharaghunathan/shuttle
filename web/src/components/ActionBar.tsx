import {
  canCancel,
  canResume,
  canRetry,
  waitingGateStageId,
} from "../api/actions";
import { apiJSON, longTimeoutMs } from "../api/client";
import type { Run } from "../api/types";

type Props = {
  run: Run;
  busy: boolean;
  onBusy: (busy: boolean) => void;
  onUpdated: (run: Run) => void;
  onRetryCreated: (id: string) => void;
  onError: (message: string) => void;
};

export default function ActionBar({
  run,
  busy,
  onBusy,
  onUpdated,
  onRetryCreated,
  onError,
}: Props) {
  const gateId = waitingGateStageId(run.stages);
  const showResume = canResume(run.status);
  const showRetry = canRetry(run.status);
  const showCancel = canCancel(run.status);
  const showGate = gateId !== null;

  async function runAction(
    label: string,
    fn: () => Promise<void>,
  ): Promise<void> {
    onBusy(true);
    onError("");
    try {
      await fn();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      onError(msg || `${label} failed`);
      // Timeout / network may leave run state indeterminate — parent should refetch.
      try {
        const fresh = await apiJSON<Run>(`/v1/runs/${run.id}`);
        onUpdated(fresh);
      } catch {
        /* ignore refetch errors */
      }
    } finally {
      onBusy(false);
    }
  }

  return (
    <div className="action-bar" role="group" aria-label="Run actions">
      {showResume && (
        <button
          type="button"
          disabled={busy}
          onClick={() =>
            runAction("Resume", async () => {
              const updated = await apiJSON<Run>(`/v1/runs/${run.id}/resume`, {
                method: "POST",
                timeoutMs: longTimeoutMs,
              });
              onUpdated(updated);
            })
          }
        >
          Resume
        </button>
      )}
      {showRetry && (
        <button
          type="button"
          disabled={busy}
          onClick={() =>
            runAction("Retry", async () => {
              const created = await apiJSON<{ id: string }>(
                `/v1/runs/${run.id}/retry`,
                {
                  method: "POST",
                  timeoutMs: longTimeoutMs,
                },
              );
              onRetryCreated(created.id);
            })
          }
        >
          Retry
        </button>
      )}
      {showCancel && (
        <button
          type="button"
          disabled={busy}
          onClick={() =>
            runAction("Cancel", async () => {
              const updated = await apiJSON<Run>(`/v1/runs/${run.id}/cancel`, {
                method: "POST",
              });
              onUpdated(updated);
            })
          }
        >
          Cancel
        </button>
      )}
      {showGate && (
        <>
          <button
            type="button"
            disabled={busy || gateId === null}
            onClick={() =>
              runAction("Approve", async () => {
                const updated = await apiJSON<Run>(
                  `/v1/runs/${run.id}/approve`,
                  {
                    method: "POST",
                    timeoutMs: longTimeoutMs,
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ stage_id: gateId }),
                  },
                );
                onUpdated(updated);
              })
            }
          >
            Approve
          </button>
          <button
            type="button"
            disabled={busy || gateId === null}
            onClick={() =>
              runAction("Reject", async () => {
                const updated = await apiJSON<Run>(
                  `/v1/runs/${run.id}/reject`,
                  {
                    method: "POST",
                    timeoutMs: longTimeoutMs,
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ stage_id: gateId }),
                  },
                );
                onUpdated(updated);
              })
            }
          >
            Reject
          </button>
        </>
      )}
      {!showResume && !showRetry && !showCancel && !showGate && (
        <span className="muted">No actions available</span>
      )}
    </div>
  );
}
