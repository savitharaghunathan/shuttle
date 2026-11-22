/** Sole waiting_approval stage id, or null if none/multiple (CLI inference). */
export function waitingGateStageId(
  stages: { id: string; status: string }[],
): string | null {
  const waiting = stages.filter((s) => s.status === "waiting_approval");
  if (waiting.length !== 1) {
    return null;
  }
  return waiting[0].id;
}

/** ResumeRun allows failed | cancelled only. */
export function canResume(status: string): boolean {
  return status === "failed" || status === "cancelled";
}

/**
 * Retry without UI force: failed | cancelled.
 * Succeeded requires force (not exposed in the dashboard).
 */
export function canRetry(status: string): boolean {
  return status === "failed" || status === "cancelled";
}

/**
 * Cancel targets active runs. Stage waiting_approval maps to run status blocked.
 */
export function canCancel(status: string): boolean {
  return status === "running" || status === "blocked";
}
