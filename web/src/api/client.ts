export const longTimeoutMs = 30 * 60 * 1000;

export async function apiJSON<T>(
  path: string,
  init: RequestInit & { timeoutMs?: number } = {},
): Promise<T> {
  const { timeoutMs: timeoutOpt, ...fetchInit } = init;
  const timeoutMs = timeoutOpt ?? 30_000;
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(path, { ...fetchInit, signal: ctrl.signal });
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) {
      throw new Error(
        (data && typeof data === "object" && "error" in data
          ? String((data as { error?: unknown }).error)
          : undefined) ?? res.statusText,
      );
    }
    return data as T;
  } finally {
    clearTimeout(t);
  }
}

export function apiGet<T>(path: string, timeoutMs?: number): Promise<T> {
  return apiJSON<T>(path, { method: "GET", timeoutMs });
}

export function apiPost<T>(
  path: string,
  body?: unknown,
  timeoutMs?: number,
): Promise<T> {
  const init: RequestInit & { timeoutMs?: number } = {
    method: "POST",
    timeoutMs,
  };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  return apiJSON<T>(path, init);
}
